package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/minioclient"
	"github.com/caffeaun/marc/internal/ollama"
	"github.com/caffeaun/marc/internal/sqlitedb"
)

// ---------------------------------------------------------------------------
// Test doubles
// ---------------------------------------------------------------------------

// fakeOllama is a manual spy for ollama.Client.
type fakeOllama struct {
	mu        sync.Mutex
	callCount int
	result    *ollama.DenoiseResult
	err       error
}

func (f *fakeOllama) Denoise(_ context.Context, _, _ string) (*ollama.DenoiseResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.err != nil {
		return nil, f.err
	}
	if f.result != nil {
		return f.result, nil
	}
	return &ollama.DenoiseResult{
		UserText:      "user text",
		AssistantText: "assistant text",
		Summary:       "summary",
		HasDecision:   false,
	}, nil
}

func (f *fakeOllama) Ping(_ context.Context) error { return nil }
func (f *fakeOllama) Close() error                 { return nil }

func (f *fakeOllama) CallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.callCount
}

// fakeClickHouse is a manual spy for clickhouse.Client.
type fakeClickHouse struct {
	mu     sync.Mutex
	events []clickhouse.Event
	err    error
}

func (f *fakeClickHouse) InsertEvent(_ context.Context, e clickhouse.Event) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

func (f *fakeClickHouse) QueryEvents(_ context.Context, _ string, _ ...any) ([]map[string]any, error) {
	return nil, nil
}
func (f *fakeClickHouse) Exec(_ context.Context, _ string, _ ...any) error { return nil }
func (f *fakeClickHouse) Ping(_ context.Context) error                     { return nil }
func (f *fakeClickHouse) Close() error                                     { return nil }

func (f *fakeClickHouse) Events() []clickhouse.Event {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]clickhouse.Event, len(f.events))
	copy(cp, f.events)
	return cp
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// openTempDB opens a real SQLite database in t.TempDir.
func openTempDB(t *testing.T) *sqlitedb.DB {
	t.Helper()
	db, err := sqlitedb.Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// discardLogger returns a *slog.Logger that throws away all output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// makeDaemon constructs a test daemon with the given fakes.
func makeDaemon(t *testing.T, db *sqlitedb.DB, mc minioclient.Client, ch clickhouse.Client, ol *fakeOllama) *daemon {
	t.Helper()
	return &daemon{
		cfg:          &config.ServerConfig{MachineName: "test-machine"},
		db:           db,
		mc:           mc,
		ch:           ch,
		ollama:       ol,
		stagingDir:   t.TempDir(),
		denoiseModel: "qwen3:8b",
		machine:      "test-machine",
		logger:       discardLogger(),
	}
}

// putJSONL writes events as a JSONL object to the fake MinIO at key.
// md5hex is passed as "" so Fake skips the integrity check.
func putJSONL(t *testing.T, mc *minioclient.Fake, key string, events []map[string]any) {
	t.Helper()
	var sb strings.Builder
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	body := sb.String()
	// Use empty md5hex — Fake skips the check when md5hex == "".
	if err := mc.PutObject(context.Background(), key, strings.NewReader(body), int64(len(body)), ""); err != nil {
		t.Fatalf("putJSONL: %v", err)
	}
}

// captureEvent returns a minimal CaptureEvent map for JSON marshaling.
func captureEvent(eventID string, isInternal bool) map[string]any {
	return map[string]any{
		"event_id":     eventID,
		"machine":      "test-machine",
		"captured_at":  time.Now().UTC().Format(time.RFC3339),
		"source":       "anthropic_api",
		"is_internal":  isInternal,
		"request":      json.RawMessage(`{"model":"claude-3","system":"hello world"}`),
		"response":     json.RawMessage(`{"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`),
		"error":        nil,
		"session_hint": nil,
	}
}

// buildRunOptions constructs Options with all fakes injected; suitable for Run().
func buildRunOptions(t *testing.T, db *sqlitedb.DB, mc minioclient.Client, ch *fakeClickHouse, ol *fakeOllama) Options {
	t.Helper()
	return Options{
		Config: &config.ServerConfig{
			MachineName: "test-machine",
			MinIO:       config.ServerMinIO{StagingDir: t.TempDir()},
			Ollama:      config.OllamaConfig{DenoiseModel: "qwen3:8b"},
		},
		PollInterval: 50 * time.Millisecond,
		Out:          io.Discard,
		SQLiteDB:     db,
		NewMinioClient: func(_ minioclient.Config) (minioclient.Client, error) {
			return mc, nil
		},
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
			return ch, nil
		},
		NewOllamaClient: func(_ config.OllamaConfig) ollama.Client {
			return ol
		},
	}
}

// ---------------------------------------------------------------------------
// AC#8 — project_id stub
// ---------------------------------------------------------------------------

func TestProjectIDFromRawRequest(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantLen int
	}{
		{name: "empty raw", raw: "", want: "unknown"},
		{name: "missing system", raw: `{"model":"claude-3"}`, want: "unknown"},
		{name: "null system", raw: `{"system":null}`, want: "unknown"},
		{name: "empty string system", raw: `{"system":""}`, want: "unknown"},
		{name: "empty array system", raw: `{"system":[]}`, want: "unknown"},
		{name: "non-empty string system", raw: `{"system":"hello world"}`, wantLen: 8},
		{name: "object system", raw: `{"system":{"type":"text","text":"hi"}}`, wantLen: 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := projectIDFromRawRequest(tt.raw)
			if tt.want != "" && got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if tt.wantLen > 0 {
				if len(got) != tt.wantLen {
					t.Errorf("len = %d, want %d (got %q)", len(got), tt.wantLen, got)
				}
				for _, c := range got {
					if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
						t.Errorf("not lowercase hex: %q", got)
						break
					}
				}
			}
		})
	}
}

func TestProjectIDDeterministic(t *testing.T) {
	raw := `{"system":"determinism check"}`
	first := projectIDFromRawRequest(raw)
	for i := range 10 {
		got := projectIDFromRawRequest(raw)
		if got != first {
			t.Errorf("iteration %d: got %q, want %q", i, got, first)
		}
	}
}

func TestProjectIDFirst64BytesBoundary(t *testing.T) {
	// Two inputs that differ only after byte 64 must hash to the same result.
	s64 := strings.Repeat("x", 64)
	s65a := strings.Repeat("x", 64) + "A"
	s65b := strings.Repeat("x", 64) + "B"
	raw64 := fmt.Sprintf(`{"system":%q}`, s64)
	raw65a := fmt.Sprintf(`{"system":%q}`, s65a)
	raw65b := fmt.Sprintf(`{"system":%q}`, s65b)

	id64 := projectIDFromRawRequest(raw64)
	id65a := projectIDFromRawRequest(raw65a)
	id65b := projectIDFromRawRequest(raw65b)

	// The 65-byte variants truncate to the same 64 bytes, so they match each other.
	if id65a != id65b {
		t.Errorf("expected same id for 65-byte variants differing only in byte 65: %q vs %q", id65a, id65b)
	}
	// But the 64-byte and 65-byte variants have different first-64 representations
	// because the raw JSON bytes differ (trailing quote position changes).
	_ = id64 // just ensure it computes without panic
}

// ---------------------------------------------------------------------------
// AC#2 — is_internal skip
// ---------------------------------------------------------------------------

func TestInternalEventSkip(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	mc := minioclient.NewFake()
	ch := &fakeClickHouse{}
	ol := &fakeOllama{}

	key := "raw/test-machine/2026/04/27/00/mixed.jsonl"
	putJSONL(t, mc, key, []map[string]any{
		captureEvent("internal-001", true),
		captureEvent("external-001", false),
	})

	d := makeDaemon(t, db, mc, ch, ol)
	d.processMachine(context.Background(), "test-machine")

	// Ollama must be called exactly once (for the non-internal event).
	if got := ol.CallCount(); got != 1 {
		t.Errorf("ollama call count = %d, want 1 (internal event must not call Denoise)", got)
	}

	// ClickHouse must have exactly one row.
	if got := len(ch.Events()); got != 1 {
		t.Errorf("clickhouse events = %d, want 1", got)
	}
}

// ---------------------------------------------------------------------------
// AC#3 — cursor advance and archive
// ---------------------------------------------------------------------------

func TestCursorAdvancesAndObjectArchived(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	mc := minioclient.NewFake()
	ch := &fakeClickHouse{}
	ol := &fakeOllama{}

	key := "raw/test-machine/2026/04/27/00/batch.jsonl"
	putJSONL(t, mc, key, []map[string]any{captureEvent("event-001", false)})

	d := makeDaemon(t, db, mc, ch, ol)
	d.processMachine(context.Background(), "test-machine")

	// Cursor must be set to the processed key.
	cursor, err := db.GetCursor(context.Background(), "test-machine")
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if cursor != key {
		t.Errorf("cursor = %q, want %q", cursor, key)
	}

	// Source key must no longer exist in MinIO.
	for _, k := range mc.Keys() {
		if k == key {
			t.Errorf("source key %q still present after successful batch", k)
		}
	}

	// Archive key must exist.
	archKey := archiveKey(key)
	found := false
	for _, k := range mc.Keys() {
		if k == archKey {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("archive key %q not found; all keys: %v", archKey, mc.Keys())
	}
}

// ---------------------------------------------------------------------------
// AC#4 — Ollama failure halts batch, cursor not advanced
// ---------------------------------------------------------------------------

func TestOllamaFailureHaltsBatch(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	mc := minioclient.NewFake()
	ch := &fakeClickHouse{}
	ol := &fakeOllama{err: errors.New("ollama: connection refused")}

	key := "raw/test-machine/2026/04/27/00/ol-fail.jsonl"
	putJSONL(t, mc, key, []map[string]any{captureEvent("event-001", false)})

	d := makeDaemon(t, db, mc, ch, ol)
	d.processMachine(context.Background(), "test-machine")

	cursor, err := db.GetCursor(context.Background(), "test-machine")
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if cursor != "" {
		t.Errorf("cursor = %q, want empty (not advanced after ollama failure)", cursor)
	}

	if got := len(ch.Events()); got != 0 {
		t.Errorf("clickhouse events = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// AC#5 — ClickHouse failure halts batch, cursor not advanced
// ---------------------------------------------------------------------------

func TestClickHouseFailureHaltsBatch(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	mc := minioclient.NewFake()
	ch := &fakeClickHouse{err: errors.New("clickhouse: network error")}
	ol := &fakeOllama{}

	key := "raw/test-machine/2026/04/27/00/ch-fail.jsonl"
	putJSONL(t, mc, key, []map[string]any{captureEvent("event-001", false)})

	d := makeDaemon(t, db, mc, ch, ol)
	d.processMachine(context.Background(), "test-machine")

	cursor, err := db.GetCursor(context.Background(), "test-machine")
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if cursor != "" {
		t.Errorf("cursor = %q, want empty (not advanced after clickhouse failure)", cursor)
	}
}

// ---------------------------------------------------------------------------
// AC#6 — MinIO list failure: log, cursor unchanged, no panic
// ---------------------------------------------------------------------------

func TestMinioListFailureNoPanic(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	mc := minioclient.NewFake()
	mc.ListErr = errors.New("minio: connection reset")
	ch := &fakeClickHouse{}
	ol := &fakeOllama{}

	d := makeDaemon(t, db, mc, ch, ol)
	// Must not panic.
	d.processMachine(context.Background(), "test-machine")

	cursor, err := db.GetCursor(context.Background(), "test-machine")
	if err != nil {
		t.Fatalf("GetCursor: %v", err)
	}
	if cursor != "" {
		t.Errorf("cursor = %q, want empty after list failure", cursor)
	}
}

// ---------------------------------------------------------------------------
// AC#7 — staging cleanup
// ---------------------------------------------------------------------------

func TestStagingCleanedAfterSuccess(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	mc := minioclient.NewFake()
	ch := &fakeClickHouse{}
	ol := &fakeOllama{}
	stagingDir := t.TempDir()

	key := "raw/test-machine/2026/04/27/00/staging.jsonl"
	putJSONL(t, mc, key, []map[string]any{captureEvent("event-001", false)})

	d := &daemon{
		cfg:          &config.ServerConfig{MachineName: "test-machine"},
		db:           db,
		mc:           mc,
		ch:           ch,
		ollama:       ol,
		stagingDir:   stagingDir,
		denoiseModel: "qwen3:8b",
		machine:      "test-machine",
		logger:       discardLogger(),
	}
	d.processMachine(context.Background(), "test-machine")

	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Errorf("staging dir not empty after success: %v", names)
	}
}

func TestCrashRecoveryCleansStagingFiles(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	mc := minioclient.NewFake()
	ch := &fakeClickHouse{}
	ol := &fakeOllama{}
	stagingDir := t.TempDir()

	// Plant a stale staging file simulating a previous crash.
	stale := filepath.Join(stagingDir, "stale.jsonl")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	d := &daemon{
		cfg:          &config.ServerConfig{MachineName: "test-machine"},
		db:           db,
		mc:           mc,
		ch:           ch,
		ollama:       ol,
		stagingDir:   stagingDir,
		denoiseModel: "qwen3:8b",
		machine:      "test-machine",
		logger:       discardLogger(),
	}
	d.tick(context.Background())

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale staging file was not cleaned up: %v", err)
	}
}

// ---------------------------------------------------------------------------
// AC#9 — Run() context cancellation
// ---------------------------------------------------------------------------

func TestRunCancellationReturnsNil(t *testing.T) {
	t.Parallel()

	db := openTempDB(t)
	mc := minioclient.NewFake()
	ch := &fakeClickHouse{}
	ol := &fakeOllama{}

	opts := buildRunOptions(t, db, mc, ch, ol)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- Run(ctx, opts) }()

	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("Run() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
