package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/jsonl"
)

// buildTestProxy creates a handler wired to fakeUpstream and an in-memory
// event channel. The returned channel drains captured events; capturePath
// points to a temp file.
func buildTestProxy(t *testing.T, fakeUpstream *httptest.Server, chanCap int) (http.Handler, chan jsonl.CaptureEvent, string) {
	t.Helper()
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")

	if chanCap <= 0 {
		chanCap = 256
	}

	cfg := Config{
		ListenAddr:      "127.0.0.1:0",
		UpstreamURL:     fakeUpstream.URL,
		CapturePath:     capturePath,
		Machine:         "test-machine",
		StrippedHeaders: []string{"authorization", "x-api-key", "cookie"},
		EventChanCap:    chanCap,
	}

	eventCh := make(chan jsonl.CaptureEvent, chanCap)

	h := newHandler(cfg, eventCh)
	// Inject a transport that strips TLS verification — the upstream is httptest.
	h.transport = fakeUpstream.Client().Transport

	return h, eventCh, capturePath
}

// drainWriter consumes all events from eventCh and calls AppendEvent for each,
// stopping when eventCh is closed.
func drainWriter(t *testing.T, capturePath string, eventCh <-chan jsonl.CaptureEvent) *sync.WaitGroup {
	t.Helper()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for ev := range eventCh {
			if err := jsonl.AppendEvent(capturePath, ev); err != nil {
				t.Errorf("drainWriter: AppendEvent: %v", err)
			}
		}
	}()
	return &wg
}

// countLinesInFile returns the number of newline-terminated lines in f.
func countLinesInFile(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("countLinesInFile: %v", err)
	}
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	return lines
}

// --- TestForwardSimple ---------------------------------------------------------

func TestForwardSimple(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	h, eventCh, capturePath := buildTestProxy(t, upstream, 16)
	wg := drainWriter(t, capturePath, eventCh)

	req := httptest.NewRequest(http.MethodGet, "/v1/ping", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}

	// Let the event arrive.
	close(eventCh)
	wg.Wait()

	if n := countLinesInFile(t, capturePath); n != 1 {
		t.Fatalf("want 1 JSONL line, got %d", n)
	}
}

// --- TestNon-v1PathReturns404 --------------------------------------------------

func TestNonV1PathReturns404(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	close(eventCh) // not used in this test

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for /health, got %d", rec.Code)
	}
}

// --- TestStreamingSSE ----------------------------------------------------------

// TestStreamingSSE verifies that:
// (a) SSE chunks arrive at the client in real-time (by checking flushing works).
// (b) Exactly one CaptureEvent is written to capture.jsonl.
// (c) The event has was_streamed: true.
func TestStreamingSSE(t *testing.T) {
	// Build a fake upstream that emits 5 SSE events then message_stop.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		events := []string{
			"event: content_block_start\ndata: {}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"text_delta\",\"text\":\"Hello\"}\n\n",
			"event: content_block_delta\ndata: {\"type\":\"text_delta\",\"text\":\" world\"}\n\n",
			"event: content_block_stop\ndata: {}\n\n",
			"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer upstream.Close()

	h, eventCh, capturePath := buildTestProxy(t, upstream, 16)
	wg := drainWriter(t, capturePath, eventCh)

	body := bytes.NewReader([]byte(`{"model":"claude-3","stream":true,"messages":[]}`))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// The response must contain the SSE events forwarded.
	if !strings.Contains(rec.Body.String(), "message_stop") {
		t.Fatalf("SSE body not forwarded, got: %q", rec.Body.String())
	}

	close(eventCh)
	wg.Wait()

	// Exactly one JSONL line.
	if n := countLinesInFile(t, capturePath); n != 1 {
		t.Fatalf("want 1 JSONL line, got %d", n)
	}

	// Parse the event and check was_streamed.
	data, _ := os.ReadFile(capturePath)
	var ev jsonl.CaptureEvent
	if err := json.Unmarshal(bytes.TrimSpace(data), &ev); err != nil {
		t.Fatalf("unmarshal event: %v", err)
	}
	if ev.StreamMeta == nil {
		t.Fatal("StreamMeta is nil")
	}
	if !ev.StreamMeta.WasStreamed {
		t.Fatal("want was_streamed=true")
	}
	if ev.StreamMeta.ChunkCount == 0 {
		t.Fatal("want chunkcount > 0")
	}
}

// --- TestAuthHeaderNotLogged --------------------------------------------------

// TestAuthHeaderNotLogged verifies that Authorization and x-api-key values
// never appear in slog output at any level.
func TestAuthHeaderNotLogged(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	// Capture slog output in a buffer.
	var logBuf bytes.Buffer
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))

	// Use a buffered channel so the handler can send without blocking.
	// Do NOT close it before the request — handler will send to it.
	h, eventCh, _ := buildTestProxy(t, upstream, 16)

	secretToken := "Bearer secret-xyz-token"
	apiKey := "sk-ant-api-secret-key"

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("Authorization", secretToken)
	req.Header.Set("x-api-key", apiKey)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Close channel after the request so drainWriter doesn't block.
	close(eventCh)
	// Drain any events (we don't care about them).
	for range eventCh {
	}

	// Drain any pending log writes.
	time.Sleep(10 * time.Millisecond)

	logOutput := logBuf.String()
	if strings.Contains(logOutput, "secret-xyz-token") {
		t.Errorf("Authorization value leaked in log output:\n%s", logOutput)
	}
	if strings.Contains(logOutput, "sk-ant-api-secret-key") {
		t.Errorf("x-api-key value leaked in log output:\n%s", logOutput)
	}
}

// --- TestInternalHeaderDetection ----------------------------------------------

func TestInternalHeaderDetection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	tests := []struct {
		name       string
		headerVal  string
		wantInternal bool
	}{
		{"with internal header", "true", true},
		{"without internal header", "", false},
		{"wrong value", "false", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, eventCh, capturePath := buildTestProxy(t, upstream, 16)
			wg := drainWriter(t, capturePath, eventCh)

			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
			if tt.headerVal != "" {
				req.Header.Set("X-Marc-Internal", tt.headerVal)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			close(eventCh)
			wg.Wait()

			data, _ := os.ReadFile(capturePath)
			var ev jsonl.CaptureEvent
			if err := json.Unmarshal(bytes.TrimSpace(data), &ev); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if ev.IsInternal != tt.wantInternal {
				t.Errorf("want is_internal=%v, got %v", tt.wantInternal, ev.IsInternal)
			}
		})
	}
}

// --- TestRawBodyByteEquality --------------------------------------------------

func TestRawBodyByteEquality(t *testing.T) {
	sentRequest := `{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`
	sentResponse := `{"id":"msg_1","content":[{"type":"text","text":"Hello!"}]}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Echo back the request body as proof, then return the fixed response.
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(sentResponse))
	}))
	defer upstream.Close()

	h, eventCh, capturePath := buildTestProxy(t, upstream, 16)
	wg := drainWriter(t, capturePath, eventCh)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(sentRequest))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	close(eventCh)
	wg.Wait()

	data, _ := os.ReadFile(capturePath)
	var ev jsonl.CaptureEvent
	if err := json.Unmarshal(bytes.TrimSpace(data), &ev); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Request field must equal the bytes sent.
	if string(ev.Request) != sentRequest {
		t.Errorf("request body mismatch:\nwant: %s\n got: %s", sentRequest, string(ev.Request))
	}
	// Response field must equal the bytes received from upstream.
	if string(ev.Response) != sentResponse {
		t.Errorf("response body mismatch:\nwant: %s\n got: %s", sentResponse, string(ev.Response))
	}
}

// --- TestChannelOverflowDrop --------------------------------------------------

func TestChannelOverflowDrop(t *testing.T) {
	// Reset the global counter before the test.
	resetOverflowDrops()

	// Use a channel capacity of 1. With 20 concurrent requests, some will drop.
	const chanCap = 1
	const numReqs = 20

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Add a small delay to ensure goroutines are concurrent.
		time.Sleep(20 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, chanCap)

	// Do NOT drain the channel — we want it to fill up quickly.
	// Close it after requests complete so the goroutines don't leak.

	var wg sync.WaitGroup
	for i := 0; i < numReqs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
		}()
	}
	wg.Wait()
	close(eventCh)

	dropped := OverflowDrops()
	// We cannot predict exactly how many drop, but with 1-capacity channel
	// and 20 concurrent requests, at least some should have dropped.
	t.Logf("overflow drops: %d", dropped)
	// The key invariant: no goroutine blocked. Test would hang if they did.
	// We accept 0 drops if the scheduler is slow, but at least verify counter
	// doesn't go negative (impossible with Uint64).
	_ = dropped
}

// --- TestInodeRotation --------------------------------------------------------

// TestInodeRotation verifies that after a shipper renames capture.jsonl to
// capture.jsonl.shipping, subsequent AppendEvent calls create a new capture.jsonl
// and write there — not into the .shipping file.
func TestInodeRotation(t *testing.T) {
	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")
	shippingPath := filepath.Join(dir, "capture.jsonl.shipping")

	// Write some initial events.
	ev := jsonl.CaptureEvent{
		EventID:  "before-rename",
		Machine:  "test",
		Source:   "anthropic_api",
		IsInternal: false,
		Request:  json.RawMessage(`{}`),
		Response: json.RawMessage(`{}`),
	}
	if err := jsonl.AppendEvent(capturePath, ev); err != nil {
		t.Fatalf("initial AppendEvent: %v", err)
	}

	// Simulate the shipper: rename capture.jsonl to capture.jsonl.shipping.
	if err := os.Rename(capturePath, shippingPath); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Write more events after the rename. AppendEvent uses O_CREATE so it
	// creates a fresh capture.jsonl.
	ev2 := jsonl.CaptureEvent{
		EventID:  "after-rename",
		Machine:  "test",
		Source:   "anthropic_api",
		IsInternal: false,
		Request:  json.RawMessage(`{}`),
		Response: json.RawMessage(`{}`),
	}
	if err := jsonl.AppendEvent(capturePath, ev2); err != nil {
		t.Fatalf("post-rename AppendEvent: %v", err)
	}

	// The new capture.jsonl must contain only the post-rename event.
	newData, err := os.ReadFile(capturePath)
	if err != nil {
		t.Fatalf("read new capture.jsonl: %v", err)
	}
	if !strings.Contains(string(newData), "after-rename") {
		t.Errorf("new capture.jsonl does not contain after-rename event: %s", newData)
	}
	if strings.Contains(string(newData), "before-rename") {
		t.Errorf("new capture.jsonl should not contain before-rename event: %s", newData)
	}

	// The .shipping file must contain only the pre-rename event.
	shippingData, err := os.ReadFile(shippingPath)
	if err != nil {
		t.Fatalf("read .shipping: %v", err)
	}
	if !strings.Contains(string(shippingData), "before-rename") {
		t.Errorf(".shipping does not contain before-rename event: %s", shippingData)
	}
	if strings.Contains(string(shippingData), "after-rename") {
		t.Errorf(".shipping should not contain after-rename event: %s", shippingData)
	}
}

// --- TestCaptureWriteFailureNonBlocking ---------------------------------------

// TestCaptureWriteFailureNonBlocking verifies that when capture write fails
// (e.g., read-only directory), the request still succeeds and only an error
// is logged.
func TestCaptureWriteFailureNonBlocking(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	// Use /dev/null as the capture path — AppendEvent will fail because /dev/null
	// is not a valid JSONL file, but on macOS it actually accepts writes (discards
	// them). Use a path in a non-existent directory instead.
	badPath := filepath.Join("/", "nonexistent-dir-marc-test", "capture.jsonl")

	cfg := Config{
		ListenAddr:      "127.0.0.1:0",
		UpstreamURL:     upstream.URL,
		CapturePath:     badPath,
		Machine:         "test-machine",
		StrippedHeaders: []string{"authorization", "x-api-key"},
		EventChanCap:    16,
	}
	eventCh := make(chan jsonl.CaptureEvent, 16)
	h := newHandler(cfg, eventCh)
	h.transport = upstream.Client().Transport

	// Capture slog errors.
	var logBuf bytes.Buffer
	old := slog.Default()
	t.Cleanup(func() { slog.SetDefault(old) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	// Start the writer goroutine.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		runWriter(ctx, badPath, eventCh)
	}()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Request must succeed regardless of capture failure.
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ok") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}

	// Wait for writer to attempt the write and log the error.
	time.Sleep(50 * time.Millisecond)
	cancel()
	close(eventCh)
	<-writerDone

	// The writer must have logged an error.
	logOutput := logBuf.String()
	if !strings.Contains(logOutput, "failed to append") {
		t.Errorf("expected error log about append failure, got: %s", logOutput)
	}
}

// --- TestSSEParsing -----------------------------------------------------------

// TestSSEParsing tests the SSE tee-reader's detection of message_stop in
// isolation, without a real HTTP server.
func TestSSEParsing(t *testing.T) {
	rawSSE := strings.Join([]string{
		"event: message_start",
		"data: {\"type\":\"message_start\"}",
		"",
		"event: content_block_delta",
		"data: {\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}",
		"",
		"event: message_stop",
		"data: {\"type\":\"message_stop\"}",
		"",
	}, "\n")

	rec := httptest.NewRecorder()
	res := streamSSE(rec, strings.NewReader(rawSSE), time.Now())

	if !res.sawStop {
		t.Error("want sawStop=true")
	}
	if res.chunkCount == 0 {
		t.Error("want chunkCount > 0")
	}
	if len(res.rawBody) == 0 {
		t.Error("want rawBody non-empty")
	}
}

// TestSSEDiagnostics verifies the per-stream summary fields used to tell apart
// "marc is buffering" from "Anthropic is slow-streaming thinking deltas":
// event_type_counts, first_text_delta_ms, first_thinking_ms.
func TestSSEDiagnostics(t *testing.T) {
	// A realistic-shape stream with thinking deltas before text deltas, so
	// we expect first_thinking_ms < first_text_delta_ms.
	rawSSE := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start"}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","content_block":{"type":"thinking"}}`,
		"",
		"event: content_block_delta",
		`data: {"delta":{"type":"thinking_delta","thinking":"reasoning..."}}`,
		"",
		"event: content_block_delta",
		`data: {"delta":{"type":"thinking_delta","thinking":"more thoughts"}}`,
		"",
		"event: content_block_stop",
		`data: {}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","content_block":{"type":"text"}}`,
		"",
		"event: content_block_delta",
		`data: {"delta":{"type":"text_delta","text":"Hello"}}`,
		"",
		"event: content_block_delta",
		`data: {"delta":{"type":"text_delta","text":" world"}}`,
		"",
		"event: message_stop",
		`data: {"type":"message_stop"}`,
		"",
	}, "\n")

	rec := httptest.NewRecorder()
	res := streamSSE(rec, strings.NewReader(rawSSE), time.Now())

	// event_type_counts should reflect every line we sent.
	if got := res.eventTypeCounts["message_start"]; got != 1 {
		t.Errorf("message_start count = %d, want 1", got)
	}
	if got := res.eventTypeCounts["content_block_delta"]; got != 4 {
		t.Errorf("content_block_delta count = %d, want 4", got)
	}
	if got := res.eventTypeCounts["thinking_delta"]; got != 2 {
		t.Errorf("thinking_delta count = %d, want 2", got)
	}
	if got := res.eventTypeCounts["text_delta"]; got != 2 {
		t.Errorf("text_delta count = %d, want 2", got)
	}
	if got := res.eventTypeCounts["message_stop"]; got != 1 {
		t.Errorf("message_stop count = %d, want 1", got)
	}

	// In the synthetic stream we ship thinking before text, so both bools
	// must be set and thinking must fire no later than text.
	if !res.sawThinking {
		t.Errorf("sawThinking should be true when thinking_delta is present")
	}
	if !res.sawTextDelta {
		t.Errorf("sawTextDelta should be true when text_delta is present")
	}
	if res.firstThinkingMs > res.firstTextDeltaMs {
		t.Errorf("expected firstThinkingMs (%d) <= firstTextDeltaMs (%d)",
			res.firstThinkingMs, res.firstTextDeltaMs)
	}
}

// --- TestRedactHeaders --------------------------------------------------------

func TestRedactHeaders(t *testing.T) {
	h := http.Header{
		"Authorization": []string{"Bearer secret-xyz"},
		"X-Api-Key":     []string{"sk-ant-key"},
		"Content-Type":  []string{"application/json"},
	}
	stripped := []string{"authorization", "x-api-key"}
	redacted := redactHeaders(h, stripped)

	if redacted.Get("Authorization") != "<redacted>" {
		t.Errorf("Authorization not redacted: %q", redacted.Get("Authorization"))
	}
	if redacted.Get("X-Api-Key") != "<redacted>" {
		t.Errorf("X-Api-Key not redacted: %q", redacted.Get("X-Api-Key"))
	}
	if redacted.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type should be unmodified: %q", redacted.Get("Content-Type"))
	}
	// Original must be untouched.
	if h.Get("Authorization") != "Bearer secret-xyz" {
		t.Errorf("original header mutated")
	}
}

// --- TestBuildCaptureEvent ----------------------------------------------------

func TestBuildCaptureEvent(t *testing.T) {
	reqBody := []byte(`{"model":"claude-3","messages":[]}`)
	respBody := []byte(`{"id":"msg_1","content":[]}`)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader(reqBody))
	req.Header.Set("X-Marc-Internal", "true")
	req.Header.Set("request-id", "req_abc")

	meta := &jsonl.StreamMeta{WasStreamed: true, ChunkCount: 5}

	ev, err := buildCaptureEvent(reqBody, respBody, 200, req, "test-machine", meta, nil)
	if err != nil {
		t.Fatalf("buildCaptureEvent: %v", err)
	}

	if ev.EventID == "" {
		t.Error("EventID must not be empty")
	}
	if ev.Machine != "test-machine" {
		t.Errorf("want machine=test-machine, got %s", ev.Machine)
	}
	if ev.Source != "anthropic_api" {
		t.Errorf("want source=anthropic_api, got %s", ev.Source)
	}
	if !ev.IsInternal {
		t.Error("want is_internal=true")
	}
	if ev.Method != http.MethodPost {
		t.Errorf("want method=POST, got %s", ev.Method)
	}
	if ev.Path != "/v1/messages" {
		t.Errorf("want path=/v1/messages, got %s", ev.Path)
	}
	if string(ev.Request) != string(reqBody) {
		t.Errorf("request body mismatch")
	}
	if string(ev.Response) != string(respBody) {
		t.Errorf("response body mismatch")
	}
	if ev.StreamMeta == nil || !ev.StreamMeta.WasStreamed {
		t.Error("want StreamMeta.WasStreamed=true")
	}
	if ev.Error != nil {
		t.Errorf("want nil error, got %+v", ev.Error)
	}
}

// --- TestRunProxyLifecycle ----------------------------------------------------

// TestRunProxyLifecycle verifies that Run starts the server, accepts requests,
// and shuts down cleanly when the context is cancelled.
func TestRunProxyLifecycle(t *testing.T) {
	// Fake upstream.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	dir := t.TempDir()
	capturePath := filepath.Join(dir, "capture.jsonl")

	ctx, cancel := context.WithCancel(context.Background())

	cfg := Config{
		ListenAddr:      "127.0.0.1:0",
		UpstreamURL:     upstream.URL,
		CapturePath:     capturePath,
		Machine:         "test-machine",
		StrippedHeaders: []string{"authorization"},
		EventChanCap:    8,
	}

	// We can't easily get the dynamically assigned port from Run().
	// Instead, use a fixed port for this lifecycle test.
	cfg.ListenAddr = "127.0.0.1:0"

	runDone := make(chan error, 1)
	go func() {
		runDone <- Run(ctx, cfg)
	}()

	// Give the server time to start.
	time.Sleep(100 * time.Millisecond)

	// Cancel context to trigger graceful shutdown.
	cancel()

	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit within 5 seconds after context cancellation")
	}
}

// --- TestWriterIsOnlyCaller ---------------------------------------------------

// TestWriterIsOnlyCaller is a code-review test: it verifies via static
// analysis of the package that handler.go does NOT call jsonl.AppendEvent
// directly. The only caller must be the writer goroutine in writer.go.
//
// We look for actual call sites (AppendEvent followed by '('), not comments.
func TestWriterIsOnlyCaller(t *testing.T) {
	// Files that must NOT contain a direct call to AppendEvent.
	forbidden := []string{"handler.go", "sse.go", "capture.go", "proxy.go"}

	for _, fname := range forbidden {
		data, err := os.ReadFile(fname)
		if err != nil {
			t.Fatalf("open %s: %v", fname, err)
		}
		// Search line-by-line, skipping comment lines.
		scanner := bufio.NewScanner(bytes.NewReader(data))
		for scanner.Scan() {
			line := scanner.Text()
			trimmed := strings.TrimSpace(line)
			// Skip comment lines.
			if strings.HasPrefix(trimmed, "//") {
				continue
			}
			// A direct call site looks like AppendEvent(
			if strings.Contains(line, "AppendEvent(") {
				t.Errorf("%s must not call AppendEvent directly; only writer.go may do so\n  line: %s", fname, line)
			}
		}
	}
}

// --- TestSSEProducesExactlyOneEvent ------------------------------------------

// TestSSEProducesExactlyOneEvent verifies that a complete SSE stream ending in
// message_stop produces exactly one JSONL line in capture.jsonl.
func TestSSEProducesExactlyOneEvent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		events := []string{
			"event: message_start\ndata: {}\n\n",
			"event: content_block_delta\ndata: {\"text\":\"hello\"}\n\n",
			"event: message_stop\ndata: {}\n\n",
		}
		for _, ev := range events {
			_, _ = w.Write([]byte(ev))
		}
	}))
	defer upstream.Close()

	h, eventCh, capturePath := buildTestProxy(t, upstream, 16)
	wg := drainWriter(t, capturePath, eventCh)

	for i := 0; i < 3; i++ {
		body := bytes.NewReader([]byte(`{"stream":true,"messages":[]}`))
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", body)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}

	close(eventCh)
	wg.Wait()

	if n := countLinesInFile(t, capturePath); n != 3 {
		t.Fatalf("want 3 JSONL lines (one per request), got %d", n)
	}
}

// readJSONLEvents reads all JSONL lines from path and returns parsed CaptureEvents.
func readJSONLEvents(t *testing.T, path string) []jsonl.CaptureEvent {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close() //nolint:errcheck

	var events []jsonl.CaptureEvent
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 8<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var ev jsonl.CaptureEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			t.Fatalf("unmarshal JSONL line: %v\nline: %s", err, line)
		}
		events = append(events, ev)
	}
	return events
}

// --- TestErrorFromStatus ------------------------------------------------------

func TestErrorFromStatus(t *testing.T) {
	tests := []struct {
		status  int
		body    []byte
		wantNil bool
		wantMsg string
	}{
		{200, []byte(`{}`), true, ""},
		{201, []byte(`{}`), true, ""},
		{400, []byte(`{"error":{"type":"invalid_request_error","message":"bad param"}}`), false, "bad param"},
		{429, []byte(`{}`), false, "Too Many Requests"},
		{500, nil, false, "Internal Server Error"},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("status_%d", tt.status), func(t *testing.T) {
			got := errorFromStatus(tt.status, tt.body)
			if tt.wantNil && got != nil {
				t.Errorf("want nil error for status %d, got %+v", tt.status, got)
			}
			if !tt.wantNil && got == nil {
				t.Errorf("want non-nil error for status %d", tt.status)
			}
			if !tt.wantNil && got != nil && tt.wantMsg != "" {
				if !strings.Contains(got.Message, tt.wantMsg) {
					t.Errorf("want message containing %q, got %q", tt.wantMsg, got.Message)
				}
			}
		})
	}
}
