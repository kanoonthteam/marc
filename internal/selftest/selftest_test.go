package selftest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/proxy"
)

// fakeAnthropic builds an httptest server that returns a minimal valid
// Anthropic /v1/messages response for any POST. It records each inbound
// request body + headers so tests can assert what the proxy forwarded.
func fakeAnthropic(t *testing.T) (*httptest.Server, *capturedRequests) {
	t.Helper()
	captured := &capturedRequests{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured.add(r.Header.Clone(), body, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("request-id", "req_test_1")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "msg_test_1",
			"type": "message",
			"role": "assistant",
			"content": [{"type":"text","text":"hello"}],
			"model": "claude-haiku-4-5-20251001",
			"stop_reason": "end_turn",
			"usage": {"input_tokens":3,"output_tokens":1}
		}`))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

type capturedRequests struct {
	headers []http.Header
	bodies  [][]byte
	paths   []string
}

func (c *capturedRequests) add(h http.Header, b []byte, p string) {
	c.headers = append(c.headers, h)
	c.bodies = append(c.bodies, b)
	c.paths = append(c.paths, p)
}

func (c *capturedRequests) count() int { return len(c.bodies) }

// TestSelfTestPassesAgainstFakeUpstream is the CI-friendly version: no real
// Anthropic, just a stub that returns a valid completion shape. The test
// asserts every step passed and that the report contains all check-mark lines.
func TestSelfTestPassesAgainstFakeUpstream(t *testing.T) {
	upstream, captured := fakeAnthropic(t)

	var stdout bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res := Run(ctx, Options{
		Config:           proxy.Config{Machine: "ci-machine"},
		APIKey:           "sk-ant-test-fake-key-shouldnt-leak",
		UpstreamOverride: upstream.URL,
		Stdout:           &stdout,
		HealthTimeout:    3 * time.Second,
		RequestTimeout:   5 * time.Second,
	})

	if !res.Success {
		t.Fatalf("self-test FAILED: step=%q reason=%q hint=%q\nstdout:\n%s",
			res.FailedStep, res.FailedReason, res.Hint, stdout.String())
	}

	want := []string{
		StepProxyStarted,
		StepHealthOK,
		StepRequestSent,
		StepStatus200,
		StepBodyValid,
		StepCaptureWritten,
		StepAuthRedacted,
	}
	if got := len(res.Steps); got != len(want) {
		t.Fatalf("want %d steps, got %d: %+v", len(want), got, res.Steps)
	}
	for i, s := range res.Steps {
		if s.Name != want[i] {
			t.Errorf("step %d: want %q, got %q", i, want[i], s.Name)
		}
		if !s.Pass {
			t.Errorf("step %q failed: %s", s.Name, s.Detail)
		}
	}

	out := stdout.String()
	for _, name := range want {
		if !strings.Contains(out, "✓ "+name) {
			t.Errorf("stdout missing checkmark for step %q\nfull stdout:\n%s", name, out)
		}
	}

	if captured.count() != 1 {
		t.Errorf("upstream got %d requests, want 1", captured.count())
	} else {
		// Verify the proxy forwarded the auth header AND the right path.
		h := captured.headers[0]
		if h.Get("x-api-key") != "sk-ant-test-fake-key-shouldnt-leak" {
			t.Errorf("upstream did not see x-api-key forwarded; got %q", h.Get("x-api-key"))
		}
		if got := captured.paths[0]; got != "/v1/messages" {
			t.Errorf("upstream got path %q, want /v1/messages", got)
		}
		var reqBody map[string]any
		if err := json.Unmarshal(captured.bodies[0], &reqBody); err != nil {
			t.Errorf("upstream body not JSON: %v", err)
		} else {
			if reqBody["model"] != "claude-haiku-4-5-20251001" {
				t.Errorf("upstream model=%v, want claude-haiku-4-5-20251001", reqBody["model"])
			}
		}
	}
}

// TestSelfTestFailsOnNon200 verifies the self-test reports failure (and which
// step) when the upstream returns 401, simulating a bad API key.
func TestSelfTestFailsOnNon200(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer upstream.Close()

	res := Run(context.Background(), Options{
		Config:           proxy.Config{Machine: "ci-machine"},
		APIKey:           "sk-ant-bogus",
		UpstreamOverride: upstream.URL,
		Stdout:           io.Discard,
		HealthTimeout:    2 * time.Second,
		RequestTimeout:   3 * time.Second,
	})

	if res.Success {
		t.Fatal("self-test should have failed on 401, but reported success")
	}
	if res.FailedStep != StepStatus200 {
		t.Errorf("FailedStep=%q, want %q", res.FailedStep, StepStatus200)
	}
	if !strings.Contains(res.FailedReason, "401") {
		t.Errorf("FailedReason should mention 401, got %q", res.FailedReason)
	}
	if res.Hint == "" {
		t.Errorf("Hint should be non-empty on failure")
	}
}

// TestSelfTestFailsOnInvalidUpstreamShape verifies the self-test catches an
// upstream that returns 200 but with garbage body — distinguishing "proxy
// works" from "upstream actually returns Anthropic completions".
func TestSelfTestFailsOnInvalidUpstreamShape(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"this":"is not an Anthropic completion"}`))
	}))
	defer upstream.Close()

	res := Run(context.Background(), Options{
		Config:           proxy.Config{Machine: "ci-machine"},
		APIKey:           "sk-ant-test",
		UpstreamOverride: upstream.URL,
		Stdout:           io.Discard,
		HealthTimeout:    2 * time.Second,
		RequestTimeout:   3 * time.Second,
	})

	if res.Success {
		t.Fatal("self-test should have failed on bad body shape")
	}
	if res.FailedStep != StepBodyValid {
		t.Errorf("FailedStep=%q, want %q", res.FailedStep, StepBodyValid)
	}
}

// TestSelfTestClaudeBinaryNotFound: in default (claude-path) mode, if the
// claude binary isn't on PATH the test must fail early with a hint pointing
// at `claude /login` or the API-key fallback.
func TestSelfTestClaudeBinaryNotFound(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "") // ensure direct-path is also unavailable

	res := Run(context.Background(), Options{
		Config:       proxy.Config{Machine: "ci-machine"},
		ClaudeBinary: "definitely-no-such-binary-marc-test-xyz",
		Stdout:       io.Discard,
	})

	if res.Success {
		t.Fatal("self-test should have failed when claude is not on PATH and no API key is set")
	}
	if !strings.Contains(strings.ToLower(res.FailedReason), "claude") {
		t.Errorf("FailedReason should mention claude, got %q", res.FailedReason)
	}
	if !strings.Contains(res.Hint, "claude /login") && !strings.Contains(res.Hint, "ANTHROPIC_API_KEY") {
		t.Errorf("Hint should mention `claude /login` or ANTHROPIC_API_KEY, got %q", res.Hint)
	}
}
