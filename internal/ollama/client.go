// Package ollama provides an HTTP client wrapper for the Ollama inference API.
//
// The primary entry point is New, which returns a Client capable of calling
// /api/generate (via Denoise) and /api/tags (via Ping). All requests use
// a 120-second HTTP timeout. No CGO is required.
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/caffeaun/marc/internal/config"
)

const defaultTimeout = 120 * time.Second

// DenoiseResult holds the structured output from Ollama after the LLM has
// processed a raw capture event through the denoise prompt.
type DenoiseResult struct {
	UserText      string `json:"user_text"`
	AssistantText string `json:"assistant_text"`
	Summary       string `json:"summary"`
	HasDecision   bool   `json:"has_decision"`
	SkipReason    string `json:"skip_reason"`
}

// Client is the surface other packages depend on. All methods accept a
// context.Context so callers can enforce their own deadlines in addition to
// the built-in 120-second HTTP timeout.
type Client interface {
	// Denoise sends rawEvent to the configured Ollama model using the denoise
	// prompt and returns a structured DenoiseResult. It distinguishes timeout
	// errors from connection-refused errors.
	Denoise(ctx context.Context, model, rawEvent string) (*DenoiseResult, error)

	// Ping calls GET /api/tags and asserts that cfg.DenoiseModel is present in
	// the list of pulled models. Returns an error if the model is absent.
	Ping(ctx context.Context) error

	// Close releases any resources held by the client (currently a no-op for
	// the HTTP transport, but required by the interface for forward compatibility).
	Close() error
}

// ollamaClient is the production implementation of Client.
type ollamaClient struct {
	endpoint string
	model    string
	http     *http.Client
}

// New constructs a production Client from cfg.
//
// The HTTP client is configured with a 120-second timeout. No CGO is used.
func New(cfg config.OllamaConfig) Client {
	return &ollamaClient{
		endpoint: strings.TrimRight(cfg.Endpoint, "/"),
		model:    cfg.DenoiseModel,
		http: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// generateRequest is the JSON body sent to /api/generate.
type generateRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
	Stream bool   `json:"stream"`
	Format string `json:"format"`
}

// generateResponse is the outer JSON envelope returned by Ollama when
// stream is false. The LLM's actual JSON output is nested inside Response as
// a string.
type generateResponse struct {
	Response string `json:"response"`
}

// Denoise assembles a prompt from the embedded (or overridden) denoise.md and
// rawEvent, calls POST /api/generate with stream:false, and parses the
// two-level JSON response into a DenoiseResult.
func (c *ollamaClient) Denoise(ctx context.Context, model, rawEvent string) (*DenoiseResult, error) {
	prompt := denoisePrompt() + "\n\n" + rawEvent

	reqBody := generateRequest{
		Model:  model,
		Prompt: prompt,
		Stream: false, // CRITICAL: prevents line-delimited streaming output
		Format: "json",
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal request: %w", err)
	}

	url := c.endpoint + "/api/generate"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("ollama: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, classifyHTTPError(err, c.endpoint)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ollama: read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	// Two-stage parse: outer envelope → inner DenoiseResult JSON string.
	var outer generateResponse
	if err := json.Unmarshal(raw, &outer); err != nil {
		return nil, fmt.Errorf("ollama: unmarshal outer response: %w", err)
	}

	var result DenoiseResult
	if err := json.Unmarshal([]byte(outer.Response), &result); err != nil {
		return nil, fmt.Errorf("ollama: unmarshal DenoiseResult from model output: %w", err)
	}

	return &result, nil
}

// tagsResponse is the JSON body returned by GET /api/tags.
type tagsResponse struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// Ping calls GET /api/tags and verifies that cfg.DenoiseModel is listed.
// Returns a descriptive error if the model is absent or if the request fails.
func (c *ollamaClient) Ping(ctx context.Context) error {
	url := c.endpoint + "/api/tags"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("ollama: ping build request: %w", err)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return classifyHTTPError(err, c.endpoint)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("ollama: ping read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ollama: ping unexpected status %d: %s", resp.StatusCode, string(raw))
	}

	var tags tagsResponse
	if err := json.Unmarshal(raw, &tags); err != nil {
		return fmt.Errorf("ollama: ping unmarshal tags: %w", err)
	}

	var pulled []string
	for _, m := range tags.Models {
		if m.Name == c.model {
			return nil
		}
		pulled = append(pulled, m.Name)
	}

	return fmt.Errorf("ollama: model %q not loaded; pulled models: %v", c.model, pulled)
}

// Close is a no-op for the HTTP transport. It is included so that callers can
// defer client.Close() in anticipation of future implementations that hold
// persistent connections or background goroutines.
func (c *ollamaClient) Close() error {
	return nil
}

// classifyHTTPError converts a net/http client error into a typed error that
// distinguishes a timeout from a connection-refused failure.
func classifyHTTPError(err error, endpoint string) error {
	// context.DeadlineExceeded wraps as url.Error with Timeout() == true.
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("ollama: timeout after %s: %w", defaultTimeout, err)
	}

	// net.Error with Timeout() covers both HTTP-client timeouts and OS-level
	// deadline-exceeded errors surfaced through the net stack.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("ollama: timeout after %s: %w", defaultTimeout, err)
	}

	// syscall.ECONNREFUSED is wrapped inside *net.OpError when the server is
	// not listening. Walk the error chain to find it.
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if errors.Is(opErr.Err, syscall.ECONNREFUSED) {
			return fmt.Errorf("ollama: connection refused at %s: %w", endpoint, err)
		}
	}

	return fmt.Errorf("ollama: request failed: %w", err)
}
