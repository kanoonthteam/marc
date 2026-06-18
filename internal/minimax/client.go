// Package minimax provides an Anthropic-Messages-compatible denoise client
// backed by MiniMax (https://api.minimax.io/anthropic).
//
// It implements the same ollama.Client contract as the local Ollama denoiser,
// so internal/process can use either interchangeably via the [denoise]
// provider switch. The denoise prompt and DenoiseResult shape are shared with
// the ollama package (ollama.DenoisePrompt, ollama.DenoiseResult), so both
// backends produce identical structured output.
package minimax

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/ollama"
)

const (
	defaultTimeout   = 120 * time.Second
	anthropicVersion = "2023-06-01"
	// maxTokens bounds the denoise response. Denoise output is a small JSON
	// object plus the stripped user/assistant text; process.go caps input at
	// 80KB, so 8192 output tokens comfortably covers normal events. An event
	// whose output would exceed this truncates to invalid JSON and is skipped
	// as ErrUnparseableModelOutput — the same graceful per-event skip the
	// Ollama path uses for poison pills.
	maxTokens = 8192
)

// client is the production MiniMax denoise implementation.
type client struct {
	baseURL string
	apiKey  string
	model   string
	http    *http.Client
}

// New constructs a MiniMax denoise client satisfying ollama.Client.
func New(cfg config.MiniMaxConfig) ollama.Client {
	return &client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   cfg.Model,
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

type messagesRequest struct {
	Model     string         `json:"model"`
	MaxTokens int            `json:"max_tokens"`
	Messages  []messageParam `json:"messages"`
}

type messageParam struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	// BaseResp is MiniMax-specific: the HTTP status can be 200 while a logical
	// error is reported here (e.g. auth, rate limit). status_code 0 == success.
	BaseResp *struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp,omitempty"`
}

// Denoise sends rawEvent through the shared denoise prompt to MiniMax and
// parses the Anthropic Messages response into a DenoiseResult. The model arg
// overrides the configured model when non-empty (process.go passes the
// configured model, so they normally match).
func (c *client) Denoise(ctx context.Context, model, rawEvent string) (*ollama.DenoiseResult, error) {
	if model == "" {
		model = c.model
	}
	prompt := ollama.DenoisePrompt() + "\n\n" + rawEvent

	body, err := json.Marshal(messagesRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []messageParam{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return nil, fmt.Errorf("minimax: marshal request: %w", err)
	}

	raw, err := c.do(ctx, body)
	if err != nil {
		return nil, err
	}
	return parseDenoise(raw)
}

// Retry tuning. retryBaseDelay is a var (not const) so tests can shrink it.
const maxAttempts = 5

var retryBaseDelay = 1 * time.Second

// do POSTs body to /v1/messages and returns the raw response bytes on HTTP 200.
// Transient statuses (429 + 5xx incl. MiniMax's 529 "overloaded") are retried
// with exponential backoff + jitter; under backlog load the endpoint throttles
// hard, and retrying inline keeps the denoise/generation cycle progressing
// instead of halting it. Transport errors and non-retryable statuses fail fast.
func (c *client) do(ctx context.Context, body []byte) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		raw, status, err := c.doOnce(ctx, body)
		if err != nil {
			return nil, err // transport error — not retried
		}
		if status == http.StatusOK {
			return raw, nil
		}
		lastErr = fmt.Errorf("minimax: unexpected status %d: %s", status, truncateForError(string(raw), 300))
		if !isRetryableStatus(status) || attempt == maxAttempts {
			return nil, lastErr
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoffDelay(attempt)):
		}
	}
	return nil, lastErr
}

// doOnce performs a single POST and returns the body, HTTP status, and any
// transport error (a non-2xx status is NOT an error here — the caller decides).
func (c *client) doOnce(ctx context.Context, body []byte) ([]byte, int, error) {
	url := c.baseURL + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("minimax: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, classifyHTTPError(err, c.baseURL)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("minimax: read response body: %w", err)
	}
	return raw, resp.StatusCode, nil
}

// isRetryableStatus reports whether an HTTP status is a transient server
// overload worth retrying with backoff. 529 is MiniMax's "overloaded_error".
//
// 429 is deliberately NOT retried: on MiniMax it signals a Token-Plan/quota
// limit ("usage limit reached … purchase Credits"), which short backoff won't
// fix — retrying just burns attempts. A genuine per-minute rate limit is also
// better served by failing fast and letting the next 60s poll cycle retry than
// by ~15s of in-call backoff.
func isRetryableStatus(code int) bool {
	switch code {
	case http.StatusInternalServerError, // 500
		http.StatusBadGateway,         // 502
		http.StatusServiceUnavailable, // 503
		http.StatusGatewayTimeout,     // 504
		529:                           // MiniMax overloaded_error
		return true
	}
	return false
}

// backoffDelay returns an exponentially increasing, jittered delay for the
// given 1-based attempt: ~base, 2·base, 4·base, … capped, with full jitter in
// [d/2, d] to avoid synchronized retries.
func backoffDelay(attempt int) time.Duration {
	const cap = 16 * time.Second
	d := retryBaseDelay << (attempt - 1)
	if d > cap || d <= 0 {
		d = cap
	}
	half := d / 2
	return half + time.Duration(rand.Int63n(int64(half)+1))
}

// parseDenoise extracts the text content from an Anthropic Messages response
// and unmarshals it into a DenoiseResult.
func parseDenoise(raw []byte) (*ollama.DenoiseResult, error) {
	var mr messagesResponse
	if err := json.Unmarshal(raw, &mr); err != nil {
		return nil, fmt.Errorf("minimax: unmarshal response envelope: %w", err)
	}
	if mr.BaseResp != nil && mr.BaseResp.StatusCode != 0 {
		return nil, fmt.Errorf("minimax: api error status_code=%d: %s", mr.BaseResp.StatusCode, mr.BaseResp.StatusMsg)
	}

	var sb strings.Builder
	for _, b := range mr.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}

	text := extractJSONObject(sb.String())
	var result ollama.DenoiseResult
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		// Reuse the ollama sentinel so process.go skips this event without
		// halting the batch (identical handling to the Ollama path).
		return nil, fmt.Errorf("%w: %v (text prefix: %q)",
			ollama.ErrUnparseableModelOutput, err, truncateForError(text, 200))
	}
	return &result, nil
}

// extractJSONObject tolerantly recovers the JSON object from a model reply that
// may be wrapped in a ```json fence or stray prose. It strips a leading/trailing
// code fence, then falls back to the outermost {...} span.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	if json.Valid([]byte(s)) {
		return s
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

// Ping verifies connectivity and auth with a minimal 1-token request. It is
// called only by configure/doctor, never on the hot path.
func (c *client) Ping(ctx context.Context) error {
	body, err := json.Marshal(messagesRequest{
		Model:     c.model,
		MaxTokens: 1,
		Messages:  []messageParam{{Role: "user", Content: "ping"}},
	})
	if err != nil {
		return fmt.Errorf("minimax: ping marshal: %w", err)
	}
	raw, err := c.do(ctx, body)
	if err != nil {
		return err
	}
	var mr messagesResponse
	if err := json.Unmarshal(raw, &mr); err == nil && mr.BaseResp != nil && mr.BaseResp.StatusCode != 0 {
		return fmt.Errorf("minimax: ping api error status_code=%d: %s", mr.BaseResp.StatusCode, mr.BaseResp.StatusMsg)
	}
	return nil
}

// Close is a no-op; the HTTP transport holds no persistent resources.
func (c *client) Close() error { return nil }

// genMaxTokens bounds a question-generation reply. A batch of candidate
// questions (situation/question/options/principle each) is larger than a
// denoise result, so this is well above maxTokens.
const genMaxTokens = 16384

// Generate sends a freeform prompt to MiniMax and returns the raw text reply
// (concatenated text blocks). Unlike Denoise it does not parse a DenoiseResult;
// the caller (question generator) parses the JSON array itself, exactly as it
// parses Claude's output. A one-off client is constructed per call since
// generation runs hourly, not in a hot loop.
func Generate(ctx context.Context, cfg config.MiniMaxConfig, model, prompt string) (string, error) {
	c := &client{
		baseURL: strings.TrimRight(cfg.BaseURL, "/"),
		apiKey:  cfg.APIKey,
		model:   model,
		http:    &http.Client{Timeout: defaultTimeout},
	}
	body, err := json.Marshal(messagesRequest{
		Model:     model,
		MaxTokens: genMaxTokens,
		Messages:  []messageParam{{Role: "user", Content: prompt}},
	})
	if err != nil {
		return "", fmt.Errorf("minimax: marshal generate request: %w", err)
	}
	raw, err := c.do(ctx, body)
	if err != nil {
		return "", err
	}
	var mr messagesResponse
	if err := json.Unmarshal(raw, &mr); err != nil {
		return "", fmt.Errorf("minimax: unmarshal generate response: %w", err)
	}
	if mr.BaseResp != nil && mr.BaseResp.StatusCode != 0 {
		return "", fmt.Errorf("minimax: generate api error status_code=%d: %s", mr.BaseResp.StatusCode, mr.BaseResp.StatusMsg)
	}
	var sb strings.Builder
	for _, b := range mr.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String(), nil
}

func truncateForError(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// classifyHTTPError distinguishes timeouts and connection-refused failures so
// process.go can log a meaningful cause (mirrors the ollama classifier).
func classifyHTTPError(err error, endpoint string) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("minimax: timeout after %s: %w", defaultTimeout, err)
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("minimax: timeout after %s: %w", defaultTimeout, err)
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && errors.Is(opErr.Err, syscall.ECONNREFUSED) {
		return fmt.Errorf("minimax: connection refused at %s: %w", endpoint, err)
	}
	return fmt.Errorf("minimax: request failed: %w", err)
}
