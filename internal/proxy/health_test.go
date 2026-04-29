package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// decodeHealth hits /_marc/health on h and returns (statusCode, parsed JSON, raw body).
// Parsing into map[string]any preserves null vs missing distinction so the test
// can assert "key is present and explicitly null".
func decodeHealth(t *testing.T, h http.Handler) (int, map[string]any, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/_marc/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK {
		return rec.Code, nil, body
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("want Content-Type application/json, got %q", ct)
	}
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decodeHealth: unmarshal: %v\nbody=%s", err, body)
	}
	return rec.Code, parsed, body
}

// requireKeys fails the test if any expected key is missing from m.
// Keys may map to nil (JSON null) — only absence is an error.
func requireKeys(t *testing.T, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("response missing required key %q", k)
		}
	}
}

func TestHealthEndpointShape(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	close(eventCh) // /_marc/health does not enqueue capture events
	// Inject a known version + listen addr into the handler's config so we can
	// assert they round-trip into the snapshot.
	hh := h.(*handler)
	hh.cfg.Version = "test-1.2.3"
	hh.cfg.ListenAddr = "127.0.0.1:8082"

	code, body, raw := decodeHealth(t, h)
	if code != http.StatusOK {
		t.Fatalf("want 200, got %d (body=%s)", code, raw)
	}

	requireKeys(t, body,
		"status",
		"last_successful_forward_at",
		"last_error_at",
		"last_error_message",
		"requests_forwarded_total",
		"requests_failed_total",
		"upstream_url",
		"listen_addr",
		"version",
	)

	if got := body["status"]; got != "ok" {
		t.Errorf("initial status: want ok, got %v", got)
	}
	if got := body["last_successful_forward_at"]; got != nil {
		t.Errorf("last_successful_forward_at: want null, got %v", got)
	}
	if got := body["last_error_at"]; got != nil {
		t.Errorf("last_error_at: want null, got %v", got)
	}
	if got := body["last_error_message"]; got != nil {
		t.Errorf("last_error_message: want null, got %v", got)
	}
	// json.Unmarshal into any decodes numbers as float64 — assert via cast.
	if v, ok := body["requests_forwarded_total"].(float64); !ok || v != 0 {
		t.Errorf("requests_forwarded_total: want 0, got %v", body["requests_forwarded_total"])
	}
	if v, ok := body["requests_failed_total"].(float64); !ok || v != 0 {
		t.Errorf("requests_failed_total: want 0, got %v", body["requests_failed_total"])
	}
	if got := body["upstream_url"]; got != upstream.URL {
		t.Errorf("upstream_url: want %s, got %v", upstream.URL, got)
	}
	if got := body["listen_addr"]; got != "127.0.0.1:8082" {
		t.Errorf("listen_addr: want 127.0.0.1:8082, got %v", got)
	}
	if got := body["version"]; got != "test-1.2.3" {
		t.Errorf("version: want test-1.2.3, got %v", got)
	}
}

func TestHealthEndpointDoesNotForwardOrCapture(t *testing.T) {
	var upstreamHits atomic.Uint64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamHits.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	h, eventCh, capturePath := buildTestProxy(t, upstream, 16)
	wg := drainWriter(t, capturePath, eventCh)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/_marc/health", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("health hit %d: got %d", i, rec.Code)
		}
	}

	close(eventCh)
	wg.Wait()

	if got := upstreamHits.Load(); got != 0 {
		t.Errorf("/_marc/health must not forward upstream; got %d hits", got)
	}
	if n := countLinesInFile(t, capturePath); n != 0 {
		t.Errorf("/_marc/health must not write capture events; got %d lines", n)
	}
}

func TestHealthEndpointMethodNotAllowed(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	close(eventCh)

	req := httptest.NewRequest(http.MethodPost, "/_marc/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /_marc/health: want 405, got %d", rec.Code)
	}
}

// TestHealthAfterSuccess sends a /v1 request that succeeds, then checks the
// health endpoint reflects the success: counter incremented, timestamp set,
// status remains "ok".
func TestHealthAfterSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	defer close(eventCh)
	go func() {
		for range eventCh {
			// drain — we don't care about captured events here
		}
	}()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("/v1 request: want 200, got %d", rec.Code)
	}

	_, body, raw := decodeHealth(t, h)
	if v, ok := body["requests_forwarded_total"].(float64); !ok || v != 1 {
		t.Errorf("requests_forwarded_total: want 1, got %v\nbody=%s", body["requests_forwarded_total"], raw)
	}
	if v, ok := body["requests_failed_total"].(float64); !ok || v != 0 {
		t.Errorf("requests_failed_total: want 0, got %v", body["requests_failed_total"])
	}
	ts, ok := body["last_successful_forward_at"].(string)
	if !ok {
		t.Fatalf("last_successful_forward_at: want ISO 8601 string, got %v", body["last_successful_forward_at"])
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("last_successful_forward_at not RFC3339Nano: %q (%v)", ts, err)
	}
	if got := body["status"]; got != "ok" {
		t.Errorf("status after success: want ok, got %v", got)
	}
	if got := body["last_error_at"]; got != nil {
		t.Errorf("last_error_at: want null after a clean success, got %v", got)
	}
}

// TestHealthAfterFailure sends a /v1 request that the upstream returns 500
// for, then checks health reflects the failure: failure counter incremented,
// last_error_at + last_error_message set, status="failed" (no prior success).
func TestHealthAfterFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"boom"}}`))
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	defer close(eventCh)
	go func() {
		for range eventCh {
		}
	}()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	_, body, raw := decodeHealth(t, h)
	if v, ok := body["requests_failed_total"].(float64); !ok || v != 1 {
		t.Errorf("requests_failed_total: want 1, got %v\nbody=%s", body["requests_failed_total"], raw)
	}
	if v, ok := body["requests_forwarded_total"].(float64); !ok || v != 0 {
		t.Errorf("requests_forwarded_total: want 0, got %v", body["requests_forwarded_total"])
	}
	if got := body["status"]; got != "failed" {
		t.Errorf("status after only-failures: want failed, got %v", got)
	}
	if msg, ok := body["last_error_message"].(string); !ok || msg == "" {
		t.Errorf("last_error_message must be a non-empty string, got %v", body["last_error_message"])
	}
	ts, ok := body["last_error_at"].(string)
	if !ok {
		t.Fatalf("last_error_at: want ISO 8601 string, got %v", body["last_error_at"])
	}
	if _, err := time.Parse(time.RFC3339Nano, ts); err != nil {
		t.Errorf("last_error_at not RFC3339Nano: %q (%v)", ts, err)
	}
}

// TestHealthDegradedAfterMixedTraffic verifies status moves from ok -> degraded
// when an error follows a success.
func TestHealthDegradedAfterMixedTraffic(t *testing.T) {
	var failNext atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if failNext.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"type":"api_error","message":"boom"}}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	defer close(eventCh)
	go func() {
		for range eventCh {
		}
	}()

	send := func() {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	send() // success
	if _, body, _ := decodeHealth(t, h); body["status"] != "ok" {
		t.Errorf("after success: want ok, got %v", body["status"])
	}

	// Ensure the second request is timestamped strictly after the first so the
	// "lastError.After(lastSuccess)" check fires deterministically.
	time.Sleep(2 * time.Millisecond)

	failNext.Store(true)
	send() // failure
	if _, body, _ := decodeHealth(t, h); body["status"] != "degraded" {
		t.Errorf("after success-then-fail: want degraded, got %v", body["status"])
	}
}

// TestHealthNoAuthRequired: any request — even with no headers at all — gets
// a 200 from /_marc/health. There is no auth gate.
func TestHealthNoAuthRequired(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	close(eventCh)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "/_marc/health", nil)
	// Deliberately set no Authorization, no x-api-key.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200 with no auth headers, got %d", rec.Code)
	}
}
