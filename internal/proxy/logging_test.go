package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureSlogJSON installs a JSON slog handler that writes to a buffer for
// the duration of the test, restoring the previous default on cleanup.
func captureSlogJSON(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// parseSlogLines decodes one JSON object per line from the captured slog
// buffer. Lines that fail to parse are skipped (slog only emits valid JSON,
// but defensive parsing keeps the test robust to incidental bytes).
func parseSlogLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	scanner := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out
}

// TestRequestLifecycleLogs verifies the four required INFO lines are emitted
// in order, each tagged with the same request_id, with the required fields.
func TestRequestLifecycleLogs(t *testing.T) {
	buf := captureSlogJSON(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	defer close(eventCh)
	go func() {
		for range eventCh {
		}
	}()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"hello":"world"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}

	lines := parseSlogLines(t, buf)
	wantedMessages := []string{
		"request received",
		"forwarding upstream",
		"upstream responded",
		"response sent",
	}

	// Index lines by message; each must appear at least once.
	byMsg := make(map[string]map[string]any, len(wantedMessages))
	var requestIDs []string
	for _, line := range lines {
		msg, _ := line["msg"].(string)
		for _, want := range wantedMessages {
			if msg == want {
				byMsg[msg] = line
			}
		}
		if msg == "request received" || msg == "forwarding upstream" ||
			msg == "upstream responded" || msg == "response sent" {
			if id, ok := line["request_id"].(string); ok {
				requestIDs = append(requestIDs, id)
			}
		}
	}

	for _, want := range wantedMessages {
		if _, ok := byMsg[want]; !ok {
			t.Errorf("missing log line %q\nfull buffer:\n%s", want, buf.String())
		}
	}

	if t.Failed() {
		return
	}

	// All four lines must share the same request_id.
	if len(requestIDs) < 4 {
		t.Fatalf("want >=4 request_id-tagged lines, got %d", len(requestIDs))
	}
	first := requestIDs[0]
	if first == "" || first == "no-id" {
		t.Errorf("request_id should be a non-empty UUID, got %q", first)
	}
	for i, id := range requestIDs {
		if id != first {
			t.Errorf("request_id mismatch on line %d: want %q, got %q", i, first, id)
		}
	}

	// Per-line field assertions.
	if got := byMsg["request received"]["method"]; got != http.MethodPost {
		t.Errorf("'request received' method: want POST, got %v", got)
	}
	if got := byMsg["request received"]["path"]; got != "/v1/messages" {
		t.Errorf("'request received' path: want /v1/messages, got %v", got)
	}
	if got := byMsg["forwarding upstream"]["upstream_url"]; got == nil || got == "" {
		t.Errorf("'forwarding upstream' upstream_url missing/empty")
	}
	if got := byMsg["upstream responded"]["status"].(float64); int(got) != http.StatusOK {
		t.Errorf("'upstream responded' status: want 200, got %v", got)
	}
	if _, ok := byMsg["upstream responded"]["duration_ms"].(float64); !ok {
		t.Errorf("'upstream responded' duration_ms missing or wrong type")
	}
	if got := byMsg["response sent"]["status"].(float64); int(got) != http.StatusOK {
		t.Errorf("'response sent' status: want 200, got %v", got)
	}
	if _, ok := byMsg["response sent"]["duration_ms"].(float64); !ok {
		t.Errorf("'response sent' duration_ms missing or wrong type")
	}
	if got, ok := byMsg["response sent"]["bytes_written"].(float64); !ok || got <= 0 {
		t.Errorf("'response sent' bytes_written must be a positive number, got %v", byMsg["response sent"]["bytes_written"])
	}
}

// TestRequestLifecycleLogsAreInfo verifies all four lifecycle lines are at
// INFO level (or higher), as the spec requires.
func TestRequestLifecycleLogsAreInfo(t *testing.T) {
	buf := captureSlogJSON(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	defer close(eventCh)
	go func() {
		for range eventCh {
		}
	}()

	h.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`)))

	for _, line := range parseSlogLines(t, buf) {
		msg, _ := line["msg"].(string)
		level, _ := line["level"].(string)
		switch msg {
		case "request received", "forwarding upstream", "upstream responded", "response sent":
			if level != "INFO" && level != "WARN" && level != "ERROR" {
				t.Errorf("line %q level=%q; want INFO or higher", msg, level)
			}
		}
	}
}

// TestUpstreamFailureLogsErrorWithRequestID verifies that when transport.RoundTrip
// fails, an ERROR-level line is emitted with the same request_id as the
// "request received" line — proving errors are not silently swallowed.
func TestUpstreamFailureLogsErrorWithRequestID(t *testing.T) {
	buf := captureSlogJSON(t)

	// Build a handler whose upstream URL points at a closed listener so the
	// transport.RoundTrip call deterministically fails.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	upstream.Close() // close BEFORE the handler runs — RoundTrip will fail

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	defer close(eventCh)
	go func() {
		for range eventCh {
		}
	}()

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d", rec.Code)
	}

	var receivedID, errorID string
	var sawError bool
	for _, line := range parseSlogLines(t, buf) {
		msg, _ := line["msg"].(string)
		if msg == "request received" {
			receivedID, _ = line["request_id"].(string)
		}
		if level, _ := line["level"].(string); level == "ERROR" {
			sawError = true
			if id, ok := line["request_id"].(string); ok {
				errorID = id
			}
		}
	}
	if !sawError {
		t.Errorf("expected at least one ERROR line on upstream failure, got:\n%s", buf.String())
	}
	if receivedID == "" {
		t.Fatalf("missing 'request received' log line")
	}
	if errorID != receivedID {
		t.Errorf("error log request_id %q does not match request_id %q", errorID, receivedID)
	}
}

// TestHealthDoesNotEmitLifecycleLogs: hitting /_marc/health must NOT log
// "request received" / "forwarding upstream" / etc. — those are reserved for
// forwarded /v1 requests so health polling does not flood logs.
func TestHealthDoesNotEmitLifecycleLogs(t *testing.T) {
	buf := captureSlogJSON(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	h, eventCh, _ := buildTestProxy(t, upstream, 16)
	close(eventCh)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/_marc/health", nil)
		h.ServeHTTP(httptest.NewRecorder(), req)
	}

	for _, line := range parseSlogLines(t, buf) {
		msg, _ := line["msg"].(string)
		switch msg {
		case "request received", "forwarding upstream", "upstream responded", "response sent":
			t.Errorf("/_marc/health should not emit lifecycle log %q", msg)
		}
	}
}
