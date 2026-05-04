package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/caffeaun/marc/internal/jsonl"
)

// TestProfileRouting verifies that /<profile>/v1/* routes to the right
// upstream and that the profile prefix is stripped before the request is
// forwarded.
func TestProfileRouting(t *testing.T) {
	var (
		anthropicHits int
		minimaxHits   int
		minimaxAuth   string
		minimaxKey    string
		anthropicPath string
		minimaxPath   string
	)

	anthropic := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		anthropicHits++
		anthropicPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"who":"anthropic"}`))
	}))
	defer anthropic.Close()

	minimax := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		minimaxHits++
		minimaxPath = r.URL.Path
		minimaxAuth = r.Header.Get("Authorization")
		minimaxKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"who":"minimax"}`))
	}))
	defer minimax.Close()

	tmp := t.TempDir()
	capturePath := filepath.Join(tmp, "capture.jsonl")

	cfg := Config{
		ListenAddr:      "127.0.0.1:0",
		CapturePath:     capturePath,
		Machine:         "test",
		StrippedHeaders: []string{"authorization", "x-api-key"},
		EventChanCap:    16,
		DefaultProfile:  "anthropic",
		Profiles: map[string]ProxyProfile{
			"anthropic": {
				Name:      "anthropic",
				BaseURL:   anthropic.URL,
				AuthStyle: "x-api-key",
			},
			"minimax": {
				Name:      "minimax",
				BaseURL:   minimax.URL,
				AuthStyle: "bearer",
				HeaderOverrides: map[string]string{
					"X-Test-Override": "yes",
				},
			},
		},
	}
	eventCh := make(chan jsonl.CaptureEvent, 16)
	h := newHandler(cfg, eventCh)
	h.transport = anthropic.Client().Transport

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range eventCh {
			_ = jsonl.AppendEvent(capturePath, ev)
		}
	}()

	t.Run("legacy /v1/ → default profile", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/messages", nil)
		req.Header.Set("x-api-key", "anth-key")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d  body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "anthropic") {
			t.Fatalf("expected anthropic upstream, got %s", rec.Body.String())
		}
		if anthropicPath != "/v1/messages" {
			t.Fatalf("anthropic upstream got path %q, want /v1/messages", anthropicPath)
		}
	})

	t.Run("/anthropic/v1/* → anthropic upstream, x-api-key passthrough", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/anthropic/v1/messages", strings.NewReader(`{}`))
		req.Header.Set("x-api-key", "anth-key")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "anthropic") {
			t.Fatalf("expected anthropic upstream, got %s", rec.Body.String())
		}
		if anthropicPath != "/v1/messages" {
			t.Fatalf("upstream path %q, want /v1/messages (prefix stripped)", anthropicPath)
		}
	})

	t.Run("/minimax/v1/* → minimax upstream, bearer auth, header override", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/minimax/v1/messages", strings.NewReader(`{}`))
		req.Header.Set("x-api-key", "mini-key")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "minimax") {
			t.Fatalf("expected minimax upstream, got %s", rec.Body.String())
		}
		if minimaxPath != "/v1/messages" {
			t.Fatalf("minimax upstream got %q, want /v1/messages", minimaxPath)
		}
		if minimaxAuth != "Bearer mini-key" {
			t.Fatalf("minimax auth = %q, want Bearer mini-key", minimaxAuth)
		}
		if minimaxKey != "" {
			t.Fatalf("minimax x-api-key should have been stripped, got %q", minimaxKey)
		}
	})

	t.Run("unknown profile → 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/openai/v1/messages", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("want 404 for unknown profile, got %d", rec.Code)
		}
	})

	t.Run("profile prefix without /v1/ → 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/anthropic/something", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("want 404 (not /v1 path), got %d", rec.Code)
		}
	})

	close(eventCh)
	wg.Wait()

	if anthropicHits < 2 {
		t.Errorf("expected ≥2 anthropic hits, got %d", anthropicHits)
	}
	if minimaxHits != 1 {
		t.Errorf("expected 1 minimax hit, got %d", minimaxHits)
	}
}

// TestProfileBaseURLWithPath verifies that when a profile's base_url itself
// has a path component (e.g. "https://api.minimax.io/anthropic"), the proxy
// JOINS it with the request's restPath instead of overwriting. Regression
// test for v0.4.0 → v0.4.1.
func TestProfileBaseURLWithPath(t *testing.T) {
	var seenPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	tmp := t.TempDir()
	cfg := Config{
		ListenAddr:     "127.0.0.1:0",
		CapturePath:    filepath.Join(tmp, "capture.jsonl"),
		Machine:        "test",
		EventChanCap:   16,
		DefaultProfile: "anthropic",
		Profiles: map[string]ProxyProfile{
			"anthropic": {Name: "anthropic", BaseURL: upstream.URL, AuthStyle: "x-api-key"},
			// Critical bit: base_url has a /anthropic suffix path.
			"minimax": {Name: "minimax", BaseURL: upstream.URL + "/anthropic", AuthStyle: "bearer"},
		},
	}
	eventCh := make(chan jsonl.CaptureEvent, 16)
	h := newHandler(cfg, eventCh)
	h.transport = upstream.Client().Transport
	defer close(eventCh)

	req := httptest.NewRequest(http.MethodPost, "/minimax/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("x-api-key", "k")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, body=%s", rec.Code, rec.Body.String())
	}
	if seenPath != "/anthropic/v1/messages" {
		t.Fatalf("upstream got path %q, want /anthropic/v1/messages (base_url path was joined)", seenPath)
	}
}

// TestProfileModelRewrite verifies that when a profile has a Model set,
// the proxy rewrites the request body's "model" field before forwarding,
// while leaving the ORIGINAL body intact for capture.
func TestProfileModelRewrite(t *testing.T) {
	var seenBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"ok"}`))
	}))
	defer upstream.Close()

	tmp := t.TempDir()
	cfg := Config{
		ListenAddr:     "127.0.0.1:0",
		CapturePath:    filepath.Join(tmp, "capture.jsonl"),
		Machine:        "test",
		EventChanCap:   16,
		DefaultProfile: "anthropic",
		Profiles: map[string]ProxyProfile{
			"anthropic": {Name: "anthropic", BaseURL: upstream.URL, AuthStyle: "x-api-key"},
			"minimax": {
				Name:      "minimax",
				BaseURL:   upstream.URL,
				AuthStyle: "bearer",
				Model:     "MiniMax-M2.7",
			},
		},
	}
	eventCh := make(chan jsonl.CaptureEvent, 16)
	h := newHandler(cfg, eventCh)
	h.transport = upstream.Client().Transport

	originalBody := `{"model":"claude-sonnet-4","max_tokens":50,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/minimax/v1/messages", strings.NewReader(originalBody))
	req.Header.Set("x-api-key", "key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rec.Code, rec.Body.String())
	}

	// Upstream should have seen the rewritten body.
	if !strings.Contains(string(seenBody), `"model":"MiniMax-M2.7"`) {
		t.Fatalf("upstream body did not contain rewritten model:\n%s", seenBody)
	}
	if strings.Contains(string(seenBody), `"claude-sonnet-4"`) {
		t.Fatalf("upstream body still contains original model:\n%s", seenBody)
	}

	// Other fields preserved.
	if !strings.Contains(string(seenBody), `"max_tokens":50`) {
		t.Fatalf("max_tokens lost in rewrite: %s", seenBody)
	}
	if !strings.Contains(string(seenBody), `"messages"`) {
		t.Fatalf("messages array lost in rewrite: %s", seenBody)
	}

	// Capture event MUST keep the ORIGINAL model (unbiased corpus).
	close(eventCh)
	for ev := range eventCh {
		if !strings.Contains(string(ev.Request), `"claude-sonnet-4"`) {
			t.Errorf("capture should have original model 'claude-sonnet-4', got: %s", ev.Request)
		}
		if strings.Contains(string(ev.Request), `"MiniMax-M2.7"`) {
			t.Errorf("capture leaked rewritten model name: %s", ev.Request)
		}
	}
}

// TestProfileModelRewriteSkipped verifies that body without a "model" field
// (e.g. /v1/models endpoint) is forwarded unchanged.
func TestProfileModelRewriteSkipped(t *testing.T) {
	var seenBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	tmp := t.TempDir()
	cfg := Config{
		ListenAddr:     "127.0.0.1:0",
		CapturePath:    filepath.Join(tmp, "capture.jsonl"),
		Machine:        "test",
		EventChanCap:   16,
		DefaultProfile: "minimax",
		Profiles: map[string]ProxyProfile{
			"minimax": {Name: "minimax", BaseURL: upstream.URL, Model: "MiniMax-M2.7"},
		},
	}
	eventCh := make(chan jsonl.CaptureEvent, 16)
	h := newHandler(cfg, eventCh)
	h.transport = upstream.Client().Transport
	defer close(eventCh)

	original := `{"unrelated":"data"}`
	req := httptest.NewRequest(http.MethodPost, "/minimax/v1/something", strings.NewReader(original))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if string(seenBody) != original {
		t.Fatalf("body should pass through when no model field; got %s", seenBody)
	}
}
