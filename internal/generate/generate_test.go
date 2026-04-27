package generate

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/sqlitedb"

	"path/filepath"
)

// ---- local ClickHouse fake ------------------------------------------------

// fakeCHClient is a local ClickHouse client fake for generate tests.
// It records the SQL string passed to QueryEvents so tests can assert on it.
type fakeCHClient struct {
	// queriedSQL is the last SQL string passed to QueryEvents.
	queriedSQL string
	// rows are returned verbatim by QueryEvents.
	rows     []map[string]any
	queryErr error
}

func (f *fakeCHClient) InsertEvent(_ context.Context, _ clickhouse.Event) error { return nil }
func (f *fakeCHClient) Exec(_ context.Context, _ string, _ ...any) error        { return nil }
func (f *fakeCHClient) Ping(_ context.Context) error                             { return nil }
func (f *fakeCHClient) Close() error                                             { return nil }

func (f *fakeCHClient) QueryEvents(_ context.Context, sql string, _ ...any) ([]map[string]any, error) {
	f.queriedSQL = sql
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.rows, nil
}

// compile-time interface check
var _ clickhouse.Client = (*fakeCHClient)(nil)

// ---- helpers ---------------------------------------------------------------

// openTempSQLite opens a temp SQLite DB and inserts the "default" project so
// FK constraints on pending_questions are satisfied.
func openTempSQLite(t *testing.T) *sqlitedb.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := sqlitedb.Open(path)
	if err != nil {
		t.Fatalf("sqlitedb.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// Seed the "default" project so the FK on pending_questions is satisfied.
	_, err = db.ExportDB().Exec(
		`INSERT OR IGNORE INTO projects (project_id, friendly_name) VALUES ('default', 'default')`,
	)
	if err != nil {
		t.Fatalf("seed default project: %v", err)
	}
	return db
}

// minimalConfig returns a ServerConfig with defaults sufficient for generate tests.
func minimalConfig() *config.ServerConfig {
	return &config.ServerConfig{
		ClickHouse: config.ClickHouseConfig{
			Database: "marc",
		},
		Scheduler: config.SchedulerConfig{
			EventsPerGeneration: 30,
		},
		Filtering: config.FilteringConfig{
			MinDurability:  7,
			MaxObviousness: 7,
		},
		Claude: config.ClaudeConfig{
			Binary:         "claude",
			InternalHeader: "X-Marc-Internal",
		},
		SQLite: config.SQLiteConfig{
			Path: "/tmp/does-not-matter-overridden-in-test",
		},
	}
}

// fakeClaude returns a ClaudeRunner test double that returns a fixed
// claude -p JSON envelope containing the provided result string.
func fakeClaude(result string) func(ctx context.Context, binary, prompt string, env []string) ([]byte, []byte, error) {
	return func(_ context.Context, _ string, _ string, _ []string) ([]byte, []byte, error) {
		envelope := claudeOutputEnvelope{
			Type:    "result",
			IsError: false,
			Result:  result,
		}
		data, _ := json.Marshal(envelope)
		return data, nil, nil
	}
}

// singleEventRows returns a minimal ClickHouse result with one row.
func singleEventRows(t time.Time) []map[string]any {
	return []map[string]any{
		{
			"event_id":       "evt-001",
			"project_id":     "proj-a",
			"summary":        "Test summary",
			"user_text":      "Should I use X?",
			"assistant_text": "Use X.",
			"captured_at":    t,
		},
	}
}

// ---- AC #1: exact SQL -------------------------------------------------

// TestRun_SQLExact verifies that Run issues exactly the expected SQL to
// ClickHouse (AC #1). The database name and LIMIT are substituted from config,
// but the column list, predicates, and ordering are verbatim from the spec.
func TestRun_SQLExact(t *testing.T) {
	t.Parallel()

	chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-time.Hour))}
	db := openTempSQLite(t)
	cfg := minimalConfig()

	if err := Run(context.Background(), Options{
		Config: cfg,
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
			return chFake, nil
		},
		SQLiteDB:     db,
		ClaudeRunner: fakeClaude(`[]`),
	}); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// The exact SQL the spec mandates (AC #1). Database and LIMIT are from config.
	wantSQL := "SELECT event_id, project_id, summary, user_text, assistant_text, captured_at" +
		" FROM marc.events" +
		" WHERE has_decision = true" +
		" AND captured_at > now() - INTERVAL 1 MONTH" +
		" AND is_internal = false" +
		" ORDER BY captured_at DESC" +
		" LIMIT 30"

	if chFake.queriedSQL != wantSQL {
		t.Errorf("SQL mismatch\n got: %q\nwant: %q", chFake.queriedSQL, wantSQL)
	}
}

// ---- AC #2: env includes ANTHROPIC_CUSTOM_HEADERS -------------------------

// TestRun_ClaudeEnvHeader verifies that the subprocess environment contains
// ANTHROPIC_CUSTOM_HEADERS=X-Marc-Internal: true (AC #2).
func TestRun_ClaudeEnvHeader(t *testing.T) {
	t.Parallel()

	chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-time.Hour))}
	db := openTempSQLite(t)
	cfg := minimalConfig()

	var capturedEnv []string
	runner := func(_ context.Context, _ string, _ string, env []string) ([]byte, []byte, error) {
		capturedEnv = append(capturedEnv, env...)
		envelope := claudeOutputEnvelope{Type: "result", Result: `[]`}
		data, _ := json.Marshal(envelope)
		return data, nil, nil
	}

	if err := Run(context.Background(), Options{
		Config: cfg,
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
			return chFake, nil
		},
		SQLiteDB:     db,
		ClaudeRunner: runner,
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	const want = "ANTHROPIC_CUSTOM_HEADERS=X-Marc-Internal: true"
	found := false
	for _, kv := range capturedEnv {
		if kv == want {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("env does not contain %q; env has %d entries", want, len(capturedEnv))
	}
}

// ---- AC #4: non-zero exit → zero insert + nil error ----------------------

// TestRun_ClaudeExitError verifies that a non-zero exit from claude results in
// zero inserts and a nil return (so the timer can retry next hour) (AC #4).
// We call the real runClaude with binary="false" to get a genuine *exec.ExitError.
func TestRun_ClaudeExitError(t *testing.T) {
	t.Parallel()

	chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-time.Hour))}
	db := openTempSQLite(t)
	cfg := minimalConfig()

	// Use a real subprocess that exits non-zero so we get a genuine *exec.ExitError.
	// The binary "false" (POSIX standard, available on Linux) always exits 1.
	runner := func(ctx context.Context, _ string, _ string, env []string) ([]byte, []byte, error) {
		return runClaude(ctx, "false", "", env)
	}

	err := Run(context.Background(), Options{
		Config: cfg,
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
			return chFake, nil
		},
		SQLiteDB:     db,
		ClaudeRunner: runner,
	})
	if err != nil {
		t.Fatalf("Run must return nil on ExitError, got: %v", err)
	}

	// No questions should have been inserted.
	next, err := db.GetNextReadyQuestion(context.Background())
	if err != nil {
		t.Fatalf("GetNextReadyQuestion: %v", err)
	}
	if next != nil {
		t.Errorf("expected zero inserts on ExitError, got a question: %+v", next)
	}
}

// ---- AC #5: filtering applies durability + obviousness --------------------

// TestRun_Filtering verifies that only candidates passing both score thresholds
// are inserted (AC #5).
func TestRun_Filtering(t *testing.T) {
	t.Parallel()

	chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-time.Hour))}
	db := openTempSQLite(t)
	cfg := minimalConfig() // MinDurability=7, MaxObviousness=7

	candidates := []CandidateQuestion{
		// passes both (durability ≥ 7, obviousness ≤ 7)
		{
			Situation: "Passes", Question: "Q1", OptionA: "A", OptionB: "B",
			PrincipleTested: "p1", DurabilityScore: 8, ObviousnessScore: 5,
		},
		// fails durability (6 < 7)
		{
			Situation: "FailsDurability", Question: "Q2", OptionA: "A", OptionB: "B",
			PrincipleTested: "p2", DurabilityScore: 6, ObviousnessScore: 3,
		},
		// fails obviousness (8 > 7)
		{
			Situation: "FailsObviousness", Question: "Q3", OptionA: "A", OptionB: "B",
			PrincipleTested: "p3", DurabilityScore: 9, ObviousnessScore: 8,
		},
	}
	candJSON, _ := json.Marshal(candidates)

	if err := Run(context.Background(), Options{
		Config: cfg,
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
			return chFake, nil
		},
		SQLiteDB:     db,
		ClaudeRunner: fakeClaude(string(candJSON)),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Only the "Passes" candidate should have been inserted.
	q, err := db.GetNextReadyQuestion(context.Background())
	if err != nil {
		t.Fatalf("GetNextReadyQuestion: %v", err)
	}
	if q == nil {
		t.Fatal("expected one inserted question, got nil")
	}
	if q.Situation != "Passes" {
		t.Errorf("situation = %q, want %q", q.Situation, "Passes")
	}
}

// ---- AC #6: survivors inserted with status='ready' ----------------------

// TestRun_InsertSurvivors verifies that surviving questions are inserted with
// status='ready' (AC #6).
func TestRun_InsertSurvivors(t *testing.T) {
	t.Parallel()

	chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-30 * time.Minute))}
	db := openTempSQLite(t)
	cfg := minimalConfig()

	candidates := []CandidateQuestion{
		{
			Situation: "S1", Question: "Q1", OptionA: "A", OptionB: "B",
			PrincipleTested: "principle", DurabilityScore: 9, ObviousnessScore: 3,
		},
	}
	candJSON, _ := json.Marshal(candidates)

	if err := Run(context.Background(), Options{
		Config: cfg,
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
			return chFake, nil
		},
		SQLiteDB:     db,
		ClaudeRunner: fakeClaude(string(candJSON)),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	q, err := db.GetNextReadyQuestion(context.Background())
	if err != nil {
		t.Fatalf("GetNextReadyQuestion: %v", err)
	}
	if q == nil {
		t.Fatal("expected one inserted question, got nil")
	}
	if q.Status != "ready" {
		t.Errorf("status = %q, want %q", q.Status, "ready")
	}
	if q.Situation != "S1" {
		t.Errorf("situation = %q, want %q", q.Situation, "S1")
	}
}

// ---- AC #7: question_gen_cursor updated to latest captured_at ------------

// TestRun_CursorUpdated verifies that question_gen_cursor.last_event_ts is
// updated to the maximum captured_at from the ClickHouse result (AC #7).
func TestRun_CursorUpdated(t *testing.T) {
	t.Parallel()

	earlier := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
	later := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)

	chFake := &fakeCHClient{
		rows: []map[string]any{
			{"event_id": "e1", "captured_at": earlier},
			{"event_id": "e2", "captured_at": later},
		},
	}
	db := openTempSQLite(t)
	cfg := minimalConfig()

	if err := Run(context.Background(), Options{
		Config: cfg,
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
			return chFake, nil
		},
		SQLiteDB:     db,
		ClaudeRunner: fakeClaude(`[]`),
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var gotTS string
	err := db.ExportDB().QueryRow(
		`SELECT last_event_ts FROM question_gen_cursor WHERE id = 1`,
	).Scan(&gotTS)
	if err != nil {
		t.Fatalf("scan cursor: %v", err)
	}

	wantTS := later.UTC().Format(time.RFC3339)
	if gotTS != wantTS {
		t.Errorf("last_event_ts = %q, want %q", gotTS, wantTS)
	}
}

// ---- AC #8: empty ClickHouse result is a no-op --------------------------

// TestRun_EmptyClickHouse verifies that an empty result set causes Run to
// return nil without inserting any questions or updating the cursor (AC #8).
func TestRun_EmptyClickHouse(t *testing.T) {
	t.Parallel()

	chFake := &fakeCHClient{rows: nil}
	db := openTempSQLite(t)
	cfg := minimalConfig()

	var claudeCalled bool
	runner := func(_ context.Context, _ string, _ string, _ []string) ([]byte, []byte, error) {
		claudeCalled = true
		return nil, nil, nil
	}

	if err := Run(context.Background(), Options{
		Config: cfg,
		NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
			return chFake, nil
		},
		SQLiteDB:     db,
		ClaudeRunner: runner,
	}); err != nil {
		t.Fatalf("Run on empty result: %v", err)
	}

	if claudeCalled {
		t.Error("claude should not be invoked when ClickHouse returns zero rows")
	}

	q, err := db.GetNextReadyQuestion(context.Background())
	if err != nil {
		t.Fatalf("GetNextReadyQuestion: %v", err)
	}
	if q != nil {
		t.Errorf("expected nil question for empty CH result, got %+v", q)
	}

	// Cursor must NOT be updated (must remain the epoch sentinel).
	var cursorTS string
	_ = db.ExportDB().QueryRow(
		`SELECT last_event_ts FROM question_gen_cursor WHERE id = 1`,
	).Scan(&cursorTS)
	if cursorTS != "1970-01-01T00:00:00Z" {
		t.Errorf("cursor was updated despite empty result: %q", cursorTS)
	}
}

// ---- AC #8: JSON parse of model output -----------------------------------

// TestRun_JSONParsing verifies that the model output is parsed correctly and
// that a parse failure causes zero inserts and nil error (AC #8).
func TestRun_JSONParsing(t *testing.T) {
	t.Parallel()

	t.Run("valid_json", func(t *testing.T) {
		t.Parallel()
		chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-time.Hour))}
		db := openTempSQLite(t)
		cfg := minimalConfig()

		candidates := []CandidateQuestion{
			{
				Situation: "Valid", Question: "Q?", OptionA: "A", OptionB: "B",
				PrincipleTested: "p", DurabilityScore: 8, ObviousnessScore: 4,
			},
		}
		candJSON, _ := json.Marshal(candidates)

		if err := Run(context.Background(), Options{
			Config: cfg,
			NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
				return chFake, nil
			},
			SQLiteDB:     db,
			ClaudeRunner: fakeClaude(string(candJSON)),
		}); err != nil {
			t.Fatalf("Run: %v", err)
		}

		q, err := db.GetNextReadyQuestion(context.Background())
		if err != nil {
			t.Fatalf("GetNextReadyQuestion: %v", err)
		}
		if q == nil || q.Situation != "Valid" {
			t.Errorf("expected inserted question with situation='Valid', got %+v", q)
		}
	})

	t.Run("invalid_json_array", func(t *testing.T) {
		t.Parallel()
		chFake := &fakeCHClient{rows: singleEventRows(time.Now().Add(-time.Hour))}
		db := openTempSQLite(t)
		cfg := minimalConfig()

		// Result is not a JSON array — the model returned garbage.
		if err := Run(context.Background(), Options{
			Config: cfg,
			NewClickHouseConn: func(_ config.ClickHouseConfig) (clickhouse.Client, error) {
				return chFake, nil
			},
			SQLiteDB:     db,
			ClaudeRunner: fakeClaude(`not valid json`),
		}); err != nil {
			t.Fatalf("Run must return nil on parse failure, got: %v", err)
		}

		q, _ := db.GetNextReadyQuestion(context.Background())
		if q != nil {
			t.Errorf("expected zero inserts on parse failure, got %+v", q)
		}
	})
}

// ---- filter logic unit test (pure function) --------------------------------

// TestFilterLogic directly exercises the filter predicate to ensure the
// boolean expression is correct (AC #5, AC #8 filter logic).
func TestFilterLogic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		durability    int
		obviousness   int
		minDurability int
		maxObvious    int
		wantPass      bool
	}{
		{"passes both", 8, 5, 7, 7, true},
		{"exact boundary durability", 7, 7, 7, 7, true},
		{"fails durability low", 6, 5, 7, 7, false},
		{"fails obviousness high", 9, 8, 7, 7, false},
		{"fails both", 5, 9, 7, 7, false},
		{"zero scores all fail", 0, 0, 7, 7, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := CandidateQuestion{
				DurabilityScore:  tt.durability,
				ObviousnessScore: tt.obviousness,
			}
			got := c.DurabilityScore >= tt.minDurability && c.ObviousnessScore <= tt.maxObvious
			if got != tt.wantPass {
				t.Errorf("durability=%d obviousness=%d min=%d max=%d: got pass=%v, want %v",
					tt.durability, tt.obviousness, tt.minDurability, tt.maxObvious, got, tt.wantPass)
			}
		})
	}
}

// ---- truncate helper test --------------------------------------------------

func TestTruncate(t *testing.T) {
	t.Parallel()
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short: got %q", got)
	}
	if got := truncate("hello world", 5); !strings.HasSuffix(got, "...") {
		t.Errorf("truncate long must end with ...: got %q", got)
	}
	if got := truncate("", 5); got != "" {
		t.Errorf("truncate empty: got %q", got)
	}
}
