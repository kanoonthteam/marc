// Package selftest implements `marc proxy --self-test`: it stands the proxy
// up on an ephemeral port, sends a real Anthropic request through it, and
// verifies the response, the capture file, and that auth headers are NOT
// captured. It is the single source of truth for "marc proxy works".
//
// The test sequence (each step produces one check-mark line):
//  1. proxy started on :NNNNN
//  2. proxy health endpoint responsive
//  3. test request sent
//  4. response status 200
//  5. response body valid (parseable Anthropic completion)
//  6. capture file received event
//  7. auth headers forwarded but not captured
//
// Exits 0 on success, 1 on failure. On failure, prints the failed step plus
// a hint at the likely cause.
package selftest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/jsonl"
	"github.com/caffeaun/marc/internal/proxy"
)

// Options controls a self-test run.
type Options struct {
	// Config holds proxy settings from ~/.marc/config.toml. ListenAddr is
	// overridden — the self-test always binds to 127.0.0.1:0.
	Config proxy.Config

	// APIKey, when non-empty, makes the self-test send the test request
	// directly via http.Client with x-api-key + Authorization headers. Used
	// only by the CI/offline test (paired with UpstreamOverride). In normal
	// operation, leave this empty and let Run shell out to `claude -p`,
	// which uses whatever credential Claude Code has already authenticated.
	APIKey string

	// UpstreamOverride, if non-empty, replaces Config.UpstreamURL. Used by the
	// CI-friendly test which points at a fake httptest server.
	UpstreamOverride string

	// ClaudeBinary names the Claude CLI to spawn for the real test. Defaults
	// to "claude" (looked up on PATH). Tests inject a custom name.
	ClaudeBinary string

	// HealthTimeout caps the time spent polling /_marc/health. Defaults to 5s.
	HealthTimeout time.Duration

	// RequestTimeout caps the test HTTP request. Defaults to 30s.
	RequestTimeout time.Duration

	// Stdout receives the human-readable check-mark report.
	Stdout io.Writer
}

// Result reports the outcome.
type Result struct {
	Success bool
	Steps   []Step
	// FailedStep is the name of the first failed step, "" on success.
	FailedStep string
	// FailedReason is a short human-readable failure summary.
	FailedReason string
	// Hint suggests a likely cause / next action.
	Hint string
}

// Step is a single named check.
type Step struct {
	Name   string
	Pass   bool
	Detail string // freeform extra info (e.g., "on :12345")
}

// fixed step labels — used for both reporting and as keys in tests.
const (
	StepProxyStarted   = "proxy started"
	StepHealthOK       = "proxy health endpoint responsive"
	StepRequestSent    = "test request sent"
	StepStatus200      = "response status 200"
	StepBodyValid      = "response body valid"
	StepCaptureWritten = "capture file received event"
	StepAuthRedacted   = "auth headers forwarded but not captured"
)

// Run executes the self-test and returns a Result. It also prints check-mark
// lines to opts.Stdout as steps complete.
//
// Test request strategy: by default, we shell out to `claude -p "say hello"`
// with ANTHROPIC_BASE_URL pointing at our ephemeral proxy. This lets the
// self-test ride on whatever credential Claude Code already has — OAuth
// session, API key, whatever — instead of demanding a separate
// ANTHROPIC_API_KEY in the environment.
//
// CI/offline mode: when opts.UpstreamOverride is set (pointing at a fake
// httptest server), we skip claude and send a direct HTTP request, optionally
// using opts.APIKey as a fake bearer.
func Run(ctx context.Context, opts Options) Result {
	if opts.HealthTimeout == 0 {
		opts.HealthTimeout = 5 * time.Second
	}
	if opts.RequestTimeout == 0 {
		opts.RequestTimeout = 30 * time.Second
	}
	if opts.Stdout == nil {
		opts.Stdout = io.Discard
	}

	// Decide which path to take.
	useClaude := opts.UpstreamOverride == "" && opts.APIKey == ""
	apiKey := opts.APIKey

	if useClaude {
		claudeBin := opts.ClaudeBinary
		if claudeBin == "" {
			claudeBin = "claude"
		}
		if _, err := exec.LookPath(claudeBin); err != nil {
			return failResult("anthropic credentials",
				"claude CLI not found on PATH (looked for "+claudeBin+")",
				"install Claude Code (https://claude.com/claude-code) and run `claude /login` once; "+
					"or set ANTHROPIC_API_KEY=sk-ant-... and re-run to use the direct-request path")
		}
	}

	cfg := opts.Config
	if opts.UpstreamOverride != "" {
		cfg.UpstreamURL = opts.UpstreamOverride
	}
	if cfg.UpstreamURL == "" {
		cfg.UpstreamURL = "https://api.anthropic.com"
	}
	// Always run the self-test against an isolated capture path so we don't
	// mix test events into the user's real capture.jsonl. This also means we
	// always know the baseline line count is zero.
	tmpDir, err := os.MkdirTemp("", "marc-selftest-*")
	if err != nil {
		return failResult("setup",
			"could not create temp dir: "+err.Error(),
			"check permissions on $TMPDIR")
	}
	defer os.RemoveAll(tmpDir)
	cfg.CapturePath = filepath.Join(tmpDir, "capture.jsonl")
	if cfg.Machine == "" {
		cfg.Machine = "marc-selftest"
	}
	if cfg.EventChanCap == 0 {
		cfg.EventChanCap = 16
	}
	if len(cfg.StrippedHeaders) == 0 {
		cfg.StrippedHeaders = []string{"authorization", "x-api-key", "cookie"}
	}

	// 1. Bind to an ephemeral port up front so we know what port the rest of
	//    the test will hit.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return failResult(StepProxyStarted,
			"could not bind to 127.0.0.1:0: "+err.Error(),
			"another process may be holding all ephemeral ports; check `ss -tln`")
	}
	addr := listener.Addr().String()
	cfg.ListenAddr = addr
	cfg.Listener = listener

	proxyCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	proxyDone := make(chan error, 1)
	go func() {
		proxyDone <- proxy.Run(proxyCtx, cfg)
	}()

	res := Result{Success: true}
	emit := func(s Step) {
		res.Steps = append(res.Steps, s)
		if opts.Stdout != nil {
			if s.Pass {
				fmt.Fprintf(opts.Stdout, "✓ %s", s.Name)
			} else {
				fmt.Fprintf(opts.Stdout, "✗ %s", s.Name)
			}
			if s.Detail != "" {
				fmt.Fprintf(opts.Stdout, " %s", s.Detail)
			}
			fmt.Fprintln(opts.Stdout)
		}
	}
	fail := func(name, reason, hint string) Result {
		emit(Step{Name: name, Pass: false, Detail: reason})
		// Cancel the proxy and wait briefly so the daemon log lines are
		// flushed before the caller exits.
		cancel()
		select {
		case <-proxyDone:
		case <-time.After(2 * time.Second):
		}
		res.Success = false
		res.FailedStep = name
		res.FailedReason = reason
		res.Hint = hint
		return res
	}

	emit(Step{Name: StepProxyStarted, Pass: true, Detail: "on " + addr})

	// 2. Poll /_marc/health until status=ok or HealthTimeout elapses.
	healthURL := "http://" + addr + "/_marc/health"
	healthClient := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(opts.HealthTimeout)
	var lastHealthErr error
	healthOK := false
	for time.Now().Before(deadline) {
		resp, hErr := healthClient.Get(healthURL)
		if hErr != nil {
			lastHealthErr = hErr
			time.Sleep(100 * time.Millisecond)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			lastHealthErr = fmt.Errorf("status %d body=%s", resp.StatusCode, body)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		var snap map[string]any
		if err := json.Unmarshal(body, &snap); err != nil {
			lastHealthErr = fmt.Errorf("non-JSON body: %s", body)
			time.Sleep(100 * time.Millisecond)
			continue
		}
		if status, _ := snap["status"].(string); status == "ok" {
			healthOK = true
			break
		}
		lastHealthErr = fmt.Errorf("health status=%v", snap["status"])
		time.Sleep(100 * time.Millisecond)
	}
	if !healthOK {
		reason := "timed out waiting for /_marc/health=ok"
		if lastHealthErr != nil {
			reason += ": " + lastHealthErr.Error()
		}
		return fail(StepHealthOK, reason,
			"the proxy bound the port but the HTTP server is not responding; check journalctl -u marc-proxy for crash output")
	}
	emit(Step{Name: StepHealthOK, Pass: true})

	if useClaude {
		// Claude path: spawn `claude -p "say hello"` with ANTHROPIC_BASE_URL
		// pointed at our ephemeral proxy. The CLI uses whatever credential
		// it has (OAuth or API key) so the self-test inherits the user's
		// existing Claude Code auth without a separate config knob.
		claudeBin := opts.ClaudeBinary
		if claudeBin == "" {
			claudeBin = "claude"
		}
		claudeCtx, claudeCancel := context.WithTimeout(ctx, opts.RequestTimeout)
		defer claudeCancel()

		cmd := exec.CommandContext(claudeCtx, claudeBin, "-p", "say only the single word: ok")
		cmd.Env = withEnv(os.Environ(), "ANTHROPIC_BASE_URL", "http://"+addr)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()

		if runErr != nil {
			tail := lastLines(stderr.String(), 6)
			return fail(StepRequestSent,
				fmt.Sprintf("`%s -p` exited with error: %v", claudeBin, runErr),
				"the proxy started but claude could not complete the round trip through it — "+
					"common causes: claude not logged in (run `claude /login`), "+
					"network firewall blocking outbound HTTPS, or the proxy is mis-routing. "+
					"stderr tail:\n"+tail)
		}
		emit(Step{Name: StepRequestSent, Pass: true})

		// claude exiting 0 implies the upstream returned 200 (any other
		// status would have made claude fail).
		emit(Step{Name: StepStatus200, Pass: true})

		// Validate claude printed something. Empty stdout would mean the
		// upstream returned 200 but with an empty body.
		out := strings.TrimSpace(stdout.String())
		if out == "" {
			return fail(StepBodyValid,
				"claude printed nothing despite exiting 0",
				"the upstream returned 200 but the response body was empty — verify upstream_url in config")
		}
		emit(Step{Name: StepBodyValid, Pass: true, Detail: "claude said: " + truncate([]byte(firstLine(out)), 60)})
	} else {
		// Direct-request path (CI/offline). Used when an upstream override
		// or an explicit APIKey is provided.
		reqBody := map[string]any{
			"model":      "claude-haiku-4-5-20251001",
			"max_tokens": 10,
			"messages": []map[string]string{
				{"role": "user", "content": "say hello"},
			},
		}
		rawReq, _ := json.Marshal(reqBody)

		httpClient := &http.Client{Timeout: opts.RequestTimeout}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"http://"+addr+"/v1/messages", bytes.NewReader(rawReq))
		if err != nil {
			return fail(StepRequestSent, "build request: "+err.Error(), "this is a programming error in the self-test; report it")
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("anthropic-version", "2023-06-01")
		if apiKey != "" {
			httpReq.Header.Set("x-api-key", apiKey)
			httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		}

		resp, err := httpClient.Do(httpReq)
		if err != nil {
			return fail(StepRequestSent, "transport error: "+err.Error(),
				"the proxy accepted /_marc/health but failed mid-request; "+
					"check journalctl for ERROR-level lines containing the request_id")
		}
		defer resp.Body.Close() //nolint:errcheck
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return fail(StepRequestSent, "read response body: "+readErr.Error(),
				"the proxy started returning data then errored; check the proxy log for upstream/SSE failures")
		}
		emit(Step{Name: StepRequestSent, Pass: true})

		if resp.StatusCode != http.StatusOK {
			return fail(StepStatus200,
				fmt.Sprintf("got %d: %s", resp.StatusCode, truncate(respBody, 300)),
				fmt.Sprintf("upstream rejected the request — verify ANTHROPIC_API_KEY is valid and that the model %q is available on your account",
					"claude-haiku-4-5-20251001"))
		}
		emit(Step{Name: StepStatus200, Pass: true})

		if err := validateAnthropicCompletion(respBody); err != nil {
			return fail(StepBodyValid,
				err.Error()+"; first 300 bytes: "+truncate(respBody, 300),
				"the upstream returned 200 but the body shape isn't an Anthropic completion — is the proxy pointed at the right upstream_url?")
		}
		emit(Step{Name: StepBodyValid, Pass: true})
	}

	// 6. Capture file got at least one new line. (The direct-request path
	//    expects exactly 1 — a single /v1/messages call. The claude path
	//    may issue auxiliary calls, so we accept >=1 there.)
	var captured []byte
	captureDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(captureDeadline) {
		data, rerr := os.ReadFile(cfg.CapturePath)
		if rerr == nil && len(bytes.TrimSpace(data)) > 0 {
			captured = data
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if len(captured) == 0 {
		return fail(StepCaptureWritten, "capture.jsonl is empty after a successful forward",
			"the proxy returned 200 but never enqueued a capture event — check for ERROR lines from the writer goroutine")
	}
	lines := bytes.Split(bytes.TrimRight(captured, "\n"), []byte("\n"))
	if !useClaude && len(lines) != 1 {
		return fail(StepCaptureWritten,
			fmt.Sprintf("capture.jsonl has %d lines, want exactly 1", len(lines)),
			"more than one event was captured during the direct-request test — likely a logic bug")
	}
	emit(Step{Name: StepCaptureWritten, Pass: true, Detail: fmt.Sprintf("(%d event%s)", len(lines), pluralS(len(lines)))})

	// 7. Verify a successful /v1/messages event exists with: is_internal:false,
	//    NO authorization header value, NO x-api-key header value.
	var sawSuccess bool
	for i, raw := range lines {
		var ev jsonl.CaptureEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			return fail(StepAuthRedacted, fmt.Sprintf("event %d is not valid JSON: %v", i, err),
				"the capture writer corrupted output — file a bug")
		}
		if ev.IsInternal {
			return fail(StepAuthRedacted, fmt.Sprintf("event %d has is_internal=true", i),
				"the self-test request must not be marked internal")
		}
		rawLine := string(raw)
		// Defend the capture schema: no header keys should appear in the JSON.
		if strings.Contains(strings.ToLower(rawLine), `"authorization"`) ||
			strings.Contains(strings.ToLower(rawLine), `"x-api-key"`) {
			return fail(StepAuthRedacted,
				fmt.Sprintf("event %d contains an authorization or x-api-key key", i),
				"the capture schema must not include request headers — verify internal/proxy/capture.go")
		}
		// In direct-request mode, also check the api-key value is absent.
		if apiKey != "" && strings.Contains(rawLine, apiKey) {
			return fail(StepAuthRedacted,
				"the api key appears verbatim in the captured event",
				"the proxy is leaking auth into capture.jsonl — security bug, do not ship")
		}
		// Track whether we saw at least one /v1/messages success.
		if ev.Path == "/v1/messages" && (ev.Error == nil) {
			sawSuccess = true
		}
	}
	if !sawSuccess {
		return fail(StepAuthRedacted,
			"no /v1/messages capture event with status 200 found",
			"the request was sent but the proxy didn't record a successful forward — verify the upstream URL")
	}
	emit(Step{Name: StepAuthRedacted, Pass: true})

	// Tear down: cancel the proxy and wait for it to exit.
	cancel()
	select {
	case err := <-proxyDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			// Don't fail the self-test for shutdown noise — we've already
			// proven the forward path works.
			fmt.Fprintf(opts.Stdout, "  (proxy shutdown returned: %v)\n", err)
		}
	case <-time.After(5 * time.Second):
		fmt.Fprintln(opts.Stdout, "  (warning: proxy did not shut down within 5s; leaking goroutine)")
	}

	return res
}

// validateAnthropicCompletion checks that body is a JSON object with the
// fields we expect from an Anthropic /v1/messages response: id, type, role,
// content (array). We do NOT require non-empty content, since the offline
// CI test uses a stub.
func validateAnthropicCompletion(body []byte) error {
	if !json.Valid(body) {
		return errors.New("response body is not valid JSON")
	}
	var parsed struct {
		ID      string `json:"id"`
		Type    string `json:"type"`
		Role    string `json:"role"`
		Content []any  `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return fmt.Errorf("response body did not parse as an Anthropic completion: %w", err)
	}
	if parsed.ID == "" {
		return errors.New("response missing 'id' field")
	}
	if parsed.Type != "message" {
		return fmt.Errorf("response type=%q, want \"message\"", parsed.Type)
	}
	if parsed.Role != "assistant" {
		return fmt.Errorf("response role=%q, want \"assistant\"", parsed.Role)
	}
	return nil
}

func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "...(truncated)"
}

func failResult(step, reason, hint string) Result {
	return Result{
		Success:      false,
		Steps:        []Step{{Name: step, Pass: false, Detail: reason}},
		FailedStep:   step,
		FailedReason: reason,
		Hint:         hint,
	}
}

// LoadAPIKey returns the API key the self-test should use, in priority order:
//  1. cfg.Anthropic.APIKey (if cfg is non-nil)
//  2. ANTHROPIC_API_KEY env var
//
// Returns "" if neither is set, in which case Run uses the claude CLI path
// (no key needed). The caller does not need to do anything special for "".
func LoadAPIKey(cfg *config.ClientConfig) string {
	if cfg != nil && cfg.Anthropic.APIKey != "" {
		return cfg.Anthropic.APIKey
	}
	return os.Getenv("ANTHROPIC_API_KEY")
}

// withEnv returns a copy of env with the given key set to value, replacing
// any existing definition. Used to inject ANTHROPIC_BASE_URL into the env of
// the spawned claude process.
func withEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, e := range env {
		if !strings.HasPrefix(e, prefix) {
			out = append(out, e)
		}
	}
	return append(out, prefix+value)
}

// firstLine returns the first newline-delimited segment of s (or s itself
// if there's no newline). Used to summarise claude's output in the report.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// lastLines returns the last n newline-delimited lines of s, joined by \n.
// Used to print a stderr tail when claude exits non-zero.
func lastLines(s string, n int) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
