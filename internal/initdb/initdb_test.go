package initdb_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/initdb"
)

// fakeClickHouse is a manual fake that records Exec calls and lets tests
// control QueryEvents output for --check scenarios.
type fakeClickHouse struct {
	execCalls   []string
	execErr     error
	queryResult []map[string]any
	queryErr    error
}

func (f *fakeClickHouse) Exec(_ context.Context, sql string, _ ...any) error {
	f.execCalls = append(f.execCalls, sql)
	return f.execErr
}

func (f *fakeClickHouse) QueryEvents(_ context.Context, _ string, _ ...any) ([]map[string]any, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.queryResult, nil
}

func (f *fakeClickHouse) InsertEvent(_ context.Context, _ clickhouse.Event) error { return nil }
func (f *fakeClickHouse) Ping(_ context.Context) error                            { return nil }
func (f *fakeClickHouse) Close() error                                            { return nil }

// buildExpectedQueryResult returns the full set of 27 expected columns as
// query rows to simulate a healthy schema.
func buildExpectedQueryResult() []map[string]any {
	columns := []struct{ name, typ string }{
		{"event_id", "UUID"},
		{"machine", "String"},
		{"project_id", "String"},
		{"captured_at", "DateTime64(3)"},
		{"source", "String"},
		{"is_internal", "Bool"},
		{"session_hint", "String"},
		{"raw_request_body", "String"},
		{"raw_response_body", "String"},
		{"response_status", "UInt16"},
		{"response_stop_reason", "String"},
		{"request_model", "String"},
		{"input_tokens", "UInt32"},
		{"output_tokens", "UInt32"},
		{"cache_read_tokens", "UInt32"},
		{"cache_write_tokens", "UInt32"},
		{"first_chunk_ms", "UInt32"},
		{"total_ms", "UInt32"},
		{"error_type", "String"},
		{"error_message", "String"},
		{"user_text", "String"},
		{"assistant_text", "String"},
		{"summary", "String"},
		{"has_decision", "Bool"},
		{"skip_reason", "String"},
		{"denoised_at", "DateTime64(3)"},
		{"denoise_model", "String"},
	}
	rows := make([]map[string]any, len(columns))
	for i, c := range columns {
		rows[i] = map[string]any{"name": c.name, "type": c.typ}
	}
	return rows
}

// newTestConfig returns a minimal ServerConfig pointing SQLite at a temp path.
func newTestConfig(t *testing.T) *config.ServerConfig {
	t.Helper()
	return &config.ServerConfig{
		MachineName: "test-host",
		ClickHouse: config.ClickHouseConfig{
			Addr:     "127.0.0.1:19000",
			Database: "marc",
			User:     "default",
			Password: "",
		},
		SQLite: config.SQLiteConfig{
			Path: filepath.Join(t.TempDir(), "state.db"),
		},
		MinIO: config.ServerMinIO{
			Endpoint:   "http://127.0.0.1:9000",
			Bucket:     "marc",
			AccessKey:  "stub",
			SecretKey:  "stub",
			StagingDir: t.TempDir(),
		},
		Ollama: config.OllamaConfig{
			Endpoint:     "http://127.0.0.1:11434",
			DenoiseModel: "qwen3:8b",
		},
		Claude: config.ClaudeConfig{
			Binary:         "claude",
			InternalHeader: "X-Marc-Internal",
		},
		Scheduler: config.SchedulerConfig{
			Timezone: "Asia/Bangkok",
		},
		Telegram: config.TelegramConfig{
			BotToken: "stub-token",
			ChatID:   1,
		},
	}
}

// runWithFake calls initdb.Run but substitutes a pre-built ClickHouse fake so
// the test never dials a real database. It drives the package via its exported
// surface only.
//
// Because initdb.Run calls clickhouse.Connect internally, we cannot inject the
// fake directly through the public API. Instead these unit tests exercise the
// package logic by calling the internal helpers indirectly through a thin
// integration shim that replaces the ClickHouse connection step. For true
// isolation the initdb package exposes RunWithClient (see below).
//
// The SQLite path still exercises the real sqlitedb.Open against a temp file.
func TestRunApply_StatusLines(t *testing.T) {
	cfg := newTestConfig(t)
	fake := &fakeClickHouse{}
	var out bytes.Buffer

	if err := initdb.RunWithClient(context.Background(), initdb.Options{
		Config: cfg,
		Check:  false,
		Out:    &out,
	}, fake); err != nil {
		t.Fatalf("RunWithClient returned unexpected error: %v", err)
	}

	output := out.String()
	if !strings.Contains(output, "[ok] ClickHouse database marc") {
		t.Errorf("expected '[ok] ClickHouse database marc' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "[ok] ClickHouse table marc.events") {
		t.Errorf("expected '[ok] ClickHouse table marc.events' in output, got:\n%s", output)
	}
	if !strings.Contains(output, "[ok] SQLite state.db tables") {
		t.Errorf("expected '[ok] SQLite state.db tables' in output, got:\n%s", output)
	}
}

func TestRunApply_ExecCallsAreIdempotent(t *testing.T) {
	cfg := newTestConfig(t)
	fake := &fakeClickHouse{}

	// First run.
	if err := initdb.RunWithClient(context.Background(), initdb.Options{Config: cfg, Out: &bytes.Buffer{}}, fake); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	calls1 := len(fake.execCalls)

	// Second run — simulate idempotency: a different fake (to reset call list)
	// but the same SQLite file.
	fake2 := &fakeClickHouse{}
	if err := initdb.RunWithClient(context.Background(), initdb.Options{Config: cfg, Out: &bytes.Buffer{}}, fake2); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	calls2 := len(fake2.execCalls)

	if calls1 != calls2 {
		t.Errorf("Exec call count changed between runs: %d vs %d", calls1, calls2)
	}
}

func TestRunApply_ClickHouseError(t *testing.T) {
	cfg := newTestConfig(t)
	fake := &fakeClickHouse{execErr: fmt.Errorf("connection refused")}

	err := initdb.RunWithClient(context.Background(), initdb.Options{Config: cfg, Out: &bytes.Buffer{}}, fake)
	if err == nil {
		t.Fatal("expected error from ClickHouse Exec, got nil")
	}
}

func TestRunCheck_Match(t *testing.T) {
	cfg := newTestConfig(t)

	// Bootstrap SQLite first so --check can open it.
	fake := &fakeClickHouse{}
	if err := initdb.RunWithClient(context.Background(), initdb.Options{Config: cfg, Out: &bytes.Buffer{}}, fake); err != nil {
		t.Fatalf("setup apply failed: %v", err)
	}

	checkFake := &fakeClickHouse{queryResult: buildExpectedQueryResult()}
	var out bytes.Buffer
	err := initdb.RunWithClient(context.Background(), initdb.Options{
		Config: cfg,
		Check:  true,
		Out:    &out,
	}, checkFake)
	if err != nil {
		t.Fatalf("--check returned error on matching schema: %v", err)
	}
	if !strings.Contains(out.String(), "[ok] schema matches expected") {
		t.Errorf("expected '[ok] schema matches expected', got:\n%s", out.String())
	}
}

func TestRunCheck_MissingColumn(t *testing.T) {
	cfg := newTestConfig(t)

	// Bootstrap SQLite.
	fake := &fakeClickHouse{}
	if err := initdb.RunWithClient(context.Background(), initdb.Options{Config: cfg, Out: &bytes.Buffer{}}, fake); err != nil {
		t.Fatalf("setup apply failed: %v", err)
	}

	// Remove denoise_model from the query result to simulate drift.
	rows := buildExpectedQueryResult()
	filtered := make([]map[string]any, 0, len(rows)-1)
	for _, r := range rows {
		if r["name"] != "denoise_model" {
			filtered = append(filtered, r)
		}
	}

	checkFake := &fakeClickHouse{queryResult: filtered}
	var out bytes.Buffer
	err := initdb.RunWithClient(context.Background(), initdb.Options{
		Config: cfg,
		Check:  true,
		Out:    &out,
	}, checkFake)
	if err == nil {
		t.Fatal("--check should return error on missing column, got nil")
	}
	if !strings.Contains(out.String(), "missing column: denoise_model") {
		t.Errorf("expected missing column mention, got:\n%s", out.String())
	}
}

func TestRunCheck_TypeMismatch(t *testing.T) {
	cfg := newTestConfig(t)

	// Bootstrap SQLite.
	fake := &fakeClickHouse{}
	if err := initdb.RunWithClient(context.Background(), initdb.Options{Config: cfg, Out: &bytes.Buffer{}}, fake); err != nil {
		t.Fatalf("setup apply failed: %v", err)
	}

	// Replace captured_at type with wrong value.
	rows := buildExpectedQueryResult()
	for i, r := range rows {
		if r["name"] == "captured_at" {
			rows[i]["type"] = "DateTime"
		}
	}

	checkFake := &fakeClickHouse{queryResult: rows}
	var out bytes.Buffer
	err := initdb.RunWithClient(context.Background(), initdb.Options{
		Config: cfg,
		Check:  true,
		Out:    &out,
	}, checkFake)
	if err == nil {
		t.Fatal("--check should return error on type mismatch, got nil")
	}
	if !strings.Contains(out.String(), "type mismatch") {
		t.Errorf("expected type mismatch mention, got:\n%s", out.String())
	}
}

func TestRunCheck_ExtraColumn(t *testing.T) {
	cfg := newTestConfig(t)

	// Bootstrap SQLite.
	fake := &fakeClickHouse{}
	if err := initdb.RunWithClient(context.Background(), initdb.Options{Config: cfg, Out: &bytes.Buffer{}}, fake); err != nil {
		t.Fatalf("setup apply failed: %v", err)
	}

	// Add an unexpected column to the result.
	rows := buildExpectedQueryResult()
	rows = append(rows, map[string]any{"name": "mystery_col", "type": "String"})

	checkFake := &fakeClickHouse{queryResult: rows}
	var out bytes.Buffer
	err := initdb.RunWithClient(context.Background(), initdb.Options{
		Config: cfg,
		Check:  true,
		Out:    &out,
	}, checkFake)
	if err == nil {
		t.Fatal("--check should return error on extra column, got nil")
	}
	if !strings.Contains(out.String(), "extra column: mystery_col") {
		t.Errorf("expected extra column mention, got:\n%s", out.String())
	}
}

func TestRunCheck_QueryError(t *testing.T) {
	cfg := newTestConfig(t)
	checkFake := &fakeClickHouse{queryErr: errors.New("network error")}
	err := initdb.RunWithClient(context.Background(), initdb.Options{
		Config: cfg,
		Check:  true,
		Out:    &bytes.Buffer{},
	}, checkFake)
	if err == nil {
		t.Fatal("expected error when QueryEvents fails")
	}
}

func TestRunApply_CreateTableSQL_ContainsDatabaseName(t *testing.T) {
	cfg := newTestConfig(t)
	cfg.ClickHouse.Database = "custom_db"

	fake := &fakeClickHouse{}
	if err := initdb.RunWithClient(context.Background(), initdb.Options{Config: cfg, Out: &bytes.Buffer{}}, fake); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	for _, call := range fake.execCalls {
		if strings.Contains(call, "CREATE TABLE") && !strings.Contains(call, "custom_db.events") {
			t.Errorf("CREATE TABLE statement does not reference custom_db.events:\n%s", call)
		}
	}
}
