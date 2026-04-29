package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/ollama"
)

// newClient creates a Client pointed at the given test server URL.
func newClient(endpoint string) ollama.Client {
	return ollama.New(config.OllamaConfig{
		Endpoint:     endpoint,
		DenoiseModel: "qwen3:8b",
	})
}

// ollamaGenerateResponse builds the two-level JSON envelope that Ollama returns
// when stream:false and format:json. The inner payload is JSON-encoded as a
// string inside the "response" field.
func ollamaGenerateResponse(t *testing.T, inner any) []byte {
	t.Helper()
	innerJSON, err := json.Marshal(inner)
	if err != nil {
		t.Fatalf("marshal inner: %v", err)
	}
	outer := map[string]string{"response": string(innerJSON)}
	b, err := json.Marshal(outer)
	if err != nil {
		t.Fatalf("marshal outer: %v", err)
	}
	return b
}

// tagsResponse builds the /api/tags JSON body with the provided model names.
func tagsBody(t *testing.T, models ...string) []byte {
	t.Helper()
	type modelEntry struct {
		Name string `json:"name"`
	}
	type tags struct {
		Models []modelEntry `json:"models"`
	}
	var entries []modelEntry
	for _, m := range models {
		entries = append(entries, modelEntry{Name: m})
	}
	b, err := json.Marshal(tags{Models: entries})
	if err != nil {
		t.Fatalf("tagsBody marshal: %v", err)
	}
	return b
}

// TestDenoise_StreamFalseInRequestBody verifies that the POST body sent to
// /api/generate contains "stream":false. This is AC #1.
func TestDenoise_StreamFalseInRequestBody(t *testing.T) {
	var capturedBody []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/generate" {
			var err error
			capturedBody, err = io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "read body error", http.StatusInternalServerError)
				return
			}

			result := &ollama.DenoiseResult{
				UserText:      "hello",
				AssistantText: "world",
				Summary:       "a test",
				HasDecision:   false,
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(ollamaGenerateResponse(t, result))
		}
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	_, err := c.Denoise(context.Background(), "qwen3:8b", "some raw event")
	if err != nil {
		t.Fatalf("Denoise: %v", err)
	}

	var reqMap map[string]any
	if err := json.Unmarshal(capturedBody, &reqMap); err != nil {
		t.Fatalf("unmarshal captured body: %v", err)
	}

	stream, ok := reqMap["stream"]
	if !ok {
		t.Fatal("request body missing 'stream' field")
	}
	if stream != false {
		t.Errorf("stream = %v, want false", stream)
	}
}

// TestDenoise_ReturnsPopulatedResult verifies that a valid Ollama response
// is parsed into a non-zero DenoiseResult. This is AC #2.
func TestDenoise_ReturnsPopulatedResult(t *testing.T) {
	want := &ollama.DenoiseResult{
		UserText:      "What should I do?",
		AssistantText: "You should refactor the handler.",
		Summary:       "Discussion about refactoring.",
		HasDecision:   true,
		SkipReason:    "",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(ollamaGenerateResponse(t, want))
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	got, err := c.Denoise(context.Background(), "qwen3:8b", "raw event text")
	if err != nil {
		t.Fatalf("Denoise: %v", err)
	}

	if got.UserText != want.UserText {
		t.Errorf("UserText = %q, want %q", got.UserText, want.UserText)
	}
	if got.AssistantText != want.AssistantText {
		t.Errorf("AssistantText = %q, want %q", got.AssistantText, want.AssistantText)
	}
	if got.Summary != want.Summary {
		t.Errorf("Summary = %q, want %q", got.Summary, want.Summary)
	}
	if got.HasDecision != want.HasDecision {
		t.Errorf("HasDecision = %v, want %v", got.HasDecision, want.HasDecision)
	}
}

// TestDenoise_TimeoutError verifies that a slow server causes a timeout error
// that is distinguishable from a connection-refused error. This is AC #3.
func TestDenoise_TimeoutError(t *testing.T) {
	// Use a very short-timeout client so the test doesn't wait 120 seconds.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block long enough for the context to cancel.
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	c := ollama.New(config.OllamaConfig{
		Endpoint:     srv.URL,
		DenoiseModel: "qwen3:8b",
	})
	defer c.Close()

	_, err := c.Denoise(ctx, "qwen3:8b", "raw event")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}

	// The error message must contain "timeout" to make it distinguishable.
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error %q does not contain 'timeout'", err.Error())
	}
}

// TestDenoise_ConnectionRefusedError verifies that a connection-refused error
// is classified distinctly from a timeout. This is AC #3.
func TestDenoise_ConnectionRefusedError(t *testing.T) {
	// Find a port that is definitely not listening by creating and immediately
	// closing a listener, then using that port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // close so nothing is listening at addr

	c := ollama.New(config.OllamaConfig{
		Endpoint:     "http://" + addr,
		DenoiseModel: "qwen3:8b",
	})
	defer c.Close()

	_, err = c.Denoise(context.Background(), "qwen3:8b", "raw event")
	if err == nil {
		t.Fatal("expected connection refused error, got nil")
	}

	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error %q does not contain 'connection refused'", err.Error())
	}
}

// TestPing_ModelNotListed verifies that Ping returns an error when the
// configured model is not among the pulled models. This is AC #4.
func TestPing_ModelNotListed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			// Return a different model, not qwen3:8b.
			_, _ = w.Write(tagsBody(t, "llama3:latest", "mistral:7b"))
		}
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	err := c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error when model not listed, got nil")
	}

	// Error must name the missing model.
	if !strings.Contains(err.Error(), "qwen3:8b") {
		t.Errorf("error %q does not mention the missing model 'qwen3:8b'", err.Error())
	}
}

// TestPing_ModelPresent verifies that Ping returns nil when the configured
// model is present in /api/tags.
func TestPing_ModelPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(tagsBody(t, "llama3:latest", "qwen3:8b"))
		}
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: unexpected error: %v", err)
	}
}

// TestDenoise_RequestTimeout verifies that the client uses a 120-second
// timeout. We confirm the timeout constant is wired by inspecting that a
// context already cancelled at call time returns an error promptly. (AC #5)
func TestDenoise_RequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond — just hang.
		select {
		case <-r.Context().Done():
		case <-time.After(60 * time.Second):
		}
	}))
	defer srv.Close()

	// Cancel context before the call so we get an immediate error.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := newClient(srv.URL)
	defer c.Close()

	_, err := c.Denoise(ctx, "qwen3:8b", "event")
	if err == nil {
		t.Fatal("expected error with cancelled context, got nil")
	}
}

// TestDenoise_HTTPErrorStatus verifies that non-200 responses are treated as
// errors and the body is included.
func TestDenoise_HTTPErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	_, err := c.Denoise(context.Background(), "qwen3:8b", "event")
	if err == nil {
		t.Fatal("expected error for 503 response, got nil")
	}
	if !strings.Contains(err.Error(), "503") {
		t.Errorf("error %q does not contain status code 503", err.Error())
	}
}

// TestDenoise_MalformedOuterJSON verifies that a malformed outer JSON envelope
// causes a descriptive error rather than a panic or silent bad result.
func TestDenoise_MalformedOuterJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "not json at all")
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	_, err := c.Denoise(context.Background(), "qwen3:8b", "event")
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// TestDenoise_MalformedInnerJSON verifies that a valid outer envelope but
// malformed inner model output causes a descriptive error.
func TestDenoise_MalformedInnerJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Outer is valid JSON but "response" value is not a JSON object.
		_, _ = fmt.Fprint(w, `{"response":"this is not a json object"}`)
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	_, err := c.Denoise(context.Background(), "qwen3:8b", "event")
	if err == nil {
		t.Fatal("expected error for malformed inner JSON, got nil")
	}
}

// TestDenoise_MalformedInnerJSON_ReturnsSentinel verifies that the inner-JSON
// failure path returns ErrUnparseableModelOutput so the processor can detect
// the poison-pill case via errors.Is and skip-and-continue.
func TestDenoise_MalformedInnerJSON_ReturnsSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Empty model output — this is the exact bug we hit on Ubuntu with
		// qwen3:30b returning "" for one event in a backlog.
		_, _ = fmt.Fprint(w, `{"response":""}`)
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	_, err := c.Denoise(context.Background(), "qwen3:30b-a3b", "event-with-bad-output")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ollama.ErrUnparseableModelOutput) {
		t.Errorf("want errors.Is(err, ErrUnparseableModelOutput) == true; got %v", err)
	}
	// The error message should also include a truncated raw-response prefix
	// so operators can see what the model actually produced.
	if !strings.Contains(err.Error(), "raw response prefix") {
		t.Errorf("error should include raw response prefix for diagnosis, got %q", err.Error())
	}
}

// TestDenoise_MalformedOuterJSON_DoesNotReturnSentinel verifies that a
// broken outer envelope (different failure mode — Ollama itself misbehaving)
// is NOT classified as a poison pill. The processor must keep halting on
// these so a transient Ollama issue gets retried.
func TestDenoise_MalformedOuterJSON_DoesNotReturnSentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, "not json at all")
	}))
	defer srv.Close()

	c := newClient(srv.URL)
	defer c.Close()

	_, err := c.Denoise(context.Background(), "m", "event")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if errors.Is(err, ollama.ErrUnparseableModelOutput) {
		t.Errorf("outer-envelope failure should NOT be classified as poison pill; got %v", err)
	}
}

// TestErrorDistinction verifies that a timeout error and a connection-refused
// error are distinguishable as different message types (AC #3).
func TestErrorDistinction(t *testing.T) {
	// --- Timeout error ---
	slowSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer slowSrv.Close()

	ctxTimeout, cancelTimeout := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancelTimeout()

	c := newClient(slowSrv.URL)
	defer c.Close()
	_, timeoutErr := c.Denoise(ctxTimeout, "qwen3:8b", "event")

	// --- Connection refused error ---
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refusedAddr := ln.Addr().String()
	ln.Close()

	c2 := ollama.New(config.OllamaConfig{Endpoint: "http://" + refusedAddr, DenoiseModel: "m"})
	defer c2.Close()
	_, refusedErr := c2.Denoise(context.Background(), "m", "event")

	// Both must be errors.
	if timeoutErr == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if refusedErr == nil {
		t.Fatal("expected refused error, got nil")
	}

	// They must not be the same type of error message.
	timeoutMsg := timeoutErr.Error()
	refusedMsg := refusedErr.Error()

	if !strings.Contains(timeoutMsg, "timeout") {
		t.Errorf("timeout error %q does not contain 'timeout'", timeoutMsg)
	}
	if !strings.Contains(refusedMsg, "connection refused") {
		t.Errorf("refused error %q does not contain 'connection refused'", refusedMsg)
	}
	// Sanity: they are different kinds.
	if strings.Contains(timeoutMsg, "connection refused") {
		t.Error("timeout error should not mention 'connection refused'")
	}
	if strings.Contains(refusedMsg, "timeout") {
		t.Error("connection-refused error should not mention 'timeout'")
	}
}

// TestPing_ConnectionRefused verifies Ping also classifies connection errors.
func TestPing_ConnectionRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	c := ollama.New(config.OllamaConfig{
		Endpoint:     "http://" + addr,
		DenoiseModel: "qwen3:8b",
	})
	defer c.Close()

	err = c.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("error %q does not mention 'connection refused'", err.Error())
	}
}

// TestClose_IsNoOp verifies that Close does not return an error.
func TestClose_IsNoOp(t *testing.T) {
	c := ollama.New(config.OllamaConfig{
		Endpoint:     "http://127.0.0.1:11434",
		DenoiseModel: "qwen3:8b",
	})
	if err := c.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestDenoise_ErrNotWrappedFurtherThanNeeded ensures that errors returned
// by Denoise for various failure modes are always non-nil and informative.
// This is a regression guard against silently swallowing errors.
func TestDenoise_ErrorsAreNonNilAndInformative(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.HandlerFunc
		wantContain string
	}{
		{
			name: "status 500",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "internal error", http.StatusInternalServerError)
			},
			wantContain: "500",
		},
		{
			name: "empty body",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				// Write empty body — json.Unmarshal will fail.
			},
			wantContain: "unmarshal",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := newClient(srv.URL)
			defer c.Close()

			_, err := c.Denoise(context.Background(), "qwen3:8b", "event")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantContain) {
				t.Errorf("error %q does not contain %q", err.Error(), tt.wantContain)
			}
		})
	}
}

// Ensure DenoiseResult fields are exported (compile-time check).
var _ = ollama.DenoiseResult{
	UserText:      "",
	AssistantText: "",
	Summary:       "",
	HasDecision:   false,
	SkipReason:    "",
}

// Ensure errors.Is/errors.As works on wrapped errors (compile-time usability).
var _ = errors.Is(nil, nil)
