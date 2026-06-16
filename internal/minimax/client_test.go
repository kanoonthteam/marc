package minimax

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/ollama"
)

func TestDenoise_RetriesOn529ThenSucceeds(t *testing.T) {
	// Shrink backoff so the test is fast; restore after.
	orig := retryBaseDelay
	retryBaseDelay = time.Millisecond
	defer func() { retryBaseDelay = orig }()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 { // fail twice with 529, succeed on the 3rd
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"{\"summary\":\"ok\",\"has_decision\":true}"}]}`))
	}))
	defer srv.Close()

	c := New(config.MiniMaxConfig{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	res, err := c.Denoise(context.Background(), "m", "event")
	if err != nil {
		t.Fatalf("Denoise after retries: %v", err)
	}
	if res.Summary != "ok" {
		t.Errorf("summary = %q", res.Summary)
	}
	if got := calls.Load(); got != 3 {
		t.Errorf("server calls = %d, want 3 (2 retries)", got)
	}
}

func TestDenoise_ExhaustsRetriesAnd529Fails(t *testing.T) {
	orig := retryBaseDelay
	retryBaseDelay = time.Millisecond
	defer func() { retryBaseDelay = orig }()

	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(529)
	}))
	defer srv.Close()

	c := New(config.MiniMaxConfig{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if _, err := c.Denoise(context.Background(), "m", "event"); err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if got := calls.Load(); got != maxAttempts {
		t.Errorf("server calls = %d, want %d", got, maxAttempts)
	}
}

func TestDenoise_NonRetryableStatusFailsFast(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusUnauthorized) // 401 — permanent, no retry
	}))
	defer srv.Close()

	c := New(config.MiniMaxConfig{BaseURL: srv.URL, APIKey: "k", Model: "m"})
	if _, err := c.Denoise(context.Background(), "m", "event"); err == nil {
		t.Fatal("expected error on 401")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("server calls = %d, want 1 (no retry on 401)", got)
	}
}

func TestParseDenoise_Valid(t *testing.T) {
	raw := []byte(`{
		"content": [{"type":"text","text":"{\"user_text\":\"u\",\"assistant_text\":\"a\",\"summary\":\"s\",\"has_decision\":true,\"skip_reason\":\"\"}"}],
		"stop_reason": "end_turn"
	}`)
	got, err := parseDenoise(raw)
	if err != nil {
		t.Fatalf("parseDenoise: %v", err)
	}
	if got.UserText != "u" || got.AssistantText != "a" || got.Summary != "s" || !got.HasDecision {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestParseDenoise_FencedJSON(t *testing.T) {
	// Some models wrap the object in a ```json fence despite instructions.
	raw := []byte(`{"content":[{"type":"text","text":"` + "```json\\n{\\\"summary\\\":\\\"s\\\",\\\"has_decision\\\":false}\\n```" + `"}]}`)
	got, err := parseDenoise(raw)
	if err != nil {
		t.Fatalf("parseDenoise fenced: %v", err)
	}
	if got.Summary != "s" || got.HasDecision {
		t.Errorf("unexpected fenced result: %+v", got)
	}
}

func TestParseDenoise_TextBlocksOnly(t *testing.T) {
	// thinking blocks (if any) are ignored; only text blocks form the payload.
	raw := []byte(`{"content":[
		{"type":"thinking","thinking":"let me reason"},
		{"type":"text","text":"{\"summary\":\"ok\",\"has_decision\":true}"}
	]}`)
	got, err := parseDenoise(raw)
	if err != nil {
		t.Fatalf("parseDenoise: %v", err)
	}
	if got.Summary != "ok" {
		t.Errorf("want summary ok, got %q", got.Summary)
	}
}

func TestParseDenoise_BaseRespError(t *testing.T) {
	raw := []byte(`{"content":[],"base_resp":{"status_code":1004,"status_msg":"auth failed"}}`)
	_, err := parseDenoise(raw)
	if err == nil {
		t.Fatal("expected error on base_resp.status_code != 0")
	}
	if errors.Is(err, ollama.ErrUnparseableModelOutput) {
		t.Error("api error should not be classified as unparseable output")
	}
}

func TestParseDenoise_Unparseable(t *testing.T) {
	raw := []byte(`{"content":[{"type":"text","text":"this is not json at all"}]}`)
	_, err := parseDenoise(raw)
	if !errors.Is(err, ollama.ErrUnparseableModelOutput) {
		t.Errorf("want ErrUnparseableModelOutput, got %v", err)
	}
}

func TestExtractJSONObject(t *testing.T) {
	cases := map[string]string{
		`{"a":1}`:                      `{"a":1}`,
		"```json\n{\"a\":1}\n```":      `{"a":1}`,
		"```\n{\"a\":1}\n```":          `{"a":1}`,
		"sure, here you go: {\"a\":1}": `{"a":1}`,
	}
	for in, want := range cases {
		if got := extractJSONObject(in); got != want {
			t.Errorf("extractJSONObject(%q) = %q, want %q", in, got, want)
		}
	}
}
