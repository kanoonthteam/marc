// Package initdb implements the marc-server schema initializer for ClickHouse and SQLite.
// It is server-binary-only — it must not be imported from cmd/marc or any client package.
package initdb

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/sqlitedb"
)

// Options controls the behavior of Run.
type Options struct {
	// Config is the loaded server configuration. Required.
	Config *config.ServerConfig

	// Check, when true, compares the live schema against the expected spec
	// columns and exits non-zero with a drift description if they differ.
	Check bool

	// Out is the destination for status lines. Defaults to os.Stdout when nil.
	Out io.Writer
}

// expectedColumns lists every column in marc.events in declaration order,
// exactly matching the spec DDL in docs/marc-spec-v1.md §"ClickHouse".
// The type strings match what ClickHouse reports in system.columns.
var expectedColumns = []struct{ name, chType string }{
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

// Run applies DDL to ClickHouse and SQLite, or (with Check=true) compares
// the live schema against the expected spec columns.
//
// Every step prints a status line to opts.Out (os.Stdout when nil).
// Returns a non-nil error on any failure; callers should treat a non-nil
// error as fatal.
func Run(ctx context.Context, opts Options) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}

	// Bootstrap connection: target database may not exist yet on a fresh ClickHouse,
	// and clickhouse-go validates Auth.Database on the first query. Connect against
	// the "default" database (always present) so CREATE DATABASE IF NOT EXISTS can
	// run. Subsequent statements address the target DB explicitly via "<db>.events"
	// so the bootstrap connection can serve both apply and check modes without a
	// reconnect.
	bootstrapCfg := opts.Config.ClickHouse
	bootstrapCfg.Database = "default"

	chClient, err := clickhouse.Connect(bootstrapCfg)
	if err != nil {
		return fmt.Errorf("initdb: connect clickhouse: %w", err)
	}
	defer chClient.Close()

	return runWithClient(ctx, out, opts, chClient)
}

// RunWithClient is identical to Run but accepts an externally-provided
// clickhouse.Client. It is used by unit tests to inject a fake without
// dialing a real ClickHouse server.
func RunWithClient(ctx context.Context, opts Options, client clickhouse.Client) error {
	out := opts.Out
	if out == nil {
		out = os.Stdout
	}
	return runWithClient(ctx, out, opts, client)
}

// runWithClient dispatches to runApply or runCheck depending on opts.Check.
func runWithClient(ctx context.Context, out io.Writer, opts Options, chClient clickhouse.Client) error {
	if opts.Check {
		return runCheck(ctx, out, chClient, opts.Config)
	}
	return runApply(ctx, out, chClient, opts.Config)
}

// runApply creates the ClickHouse database and table (idempotent) then opens
// (and immediately closes) the SQLite database so its schema is bootstrapped.
func runApply(ctx context.Context, out io.Writer, chClient clickhouse.Client, cfg *config.ServerConfig) error {
	db := cfg.ClickHouse.Database

	// 1. CREATE DATABASE IF NOT EXISTS
	createDB := fmt.Sprintf("CREATE DATABASE IF NOT EXISTS %s", db)
	if err := chClient.Exec(ctx, createDB); err != nil {
		return fmt.Errorf("initdb: create clickhouse database: %w", err)
	}
	fmt.Fprintf(out, "[ok] ClickHouse database %s\n", db)

	// 2. CREATE TABLE IF NOT EXISTS
	createTable := buildCreateTableSQL(db)
	if err := chClient.Exec(ctx, createTable); err != nil {
		return fmt.Errorf("initdb: create clickhouse table: %w", err)
	}
	fmt.Fprintf(out, "[ok] ClickHouse table %s.events\n", db)

	// 3. Open SQLite — Open() applies pragmas, creates all four tables, and seeds
	//    question_gen_cursor. No extra DDL is needed here; closing immediately is fine.
	sqlDB, err := sqlitedb.Open(cfg.SQLite.Path)
	if err != nil {
		return fmt.Errorf("initdb: open sqlite: %w", err)
	}
	_ = sqlDB.Close()
	fmt.Fprintln(out, "[ok] SQLite state.db tables")

	return nil
}

// runCheck queries the live ClickHouse column list and the SQLite table count
// and compares them against the spec. Exits non-zero on any drift.
func runCheck(ctx context.Context, out io.Writer, chClient clickhouse.Client, cfg *config.ServerConfig) error {
	db := cfg.ClickHouse.Database

	// --- ClickHouse schema check ---
	q := "SELECT name, type FROM system.columns WHERE database = ? AND table = 'events' ORDER BY position"
	rows, err := chClient.QueryEvents(ctx, q, db)
	if err != nil {
		return fmt.Errorf("initdb: query system.columns: %w", err)
	}

	// Build a position-indexed list of live columns.
	type colInfo struct{ name, typ string }
	live := make([]colInfo, 0, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		typ, _ := row["type"].(string)
		live = append(live, colInfo{name, typ})
	}

	var drifts []string

	// Check expected columns exist with correct types.
	liveByName := make(map[string]string, len(live))
	for _, c := range live {
		liveByName[c.name] = c.typ
	}
	for _, exp := range expectedColumns {
		liveType, found := liveByName[exp.name]
		if !found {
			drifts = append(drifts, fmt.Sprintf("missing column: %s", exp.name))
			continue
		}
		if liveType != exp.chType {
			drifts = append(drifts, fmt.Sprintf("type mismatch for column %s: got %s, want %s", exp.name, liveType, exp.chType))
		}
	}

	// Check for extra columns not in spec.
	expByName := make(map[string]struct{}, len(expectedColumns))
	for _, exp := range expectedColumns {
		expByName[exp.name] = struct{}{}
	}
	for _, c := range live {
		if _, found := expByName[c.name]; !found {
			drifts = append(drifts, fmt.Sprintf("extra column: %s %s", c.name, c.typ))
		}
	}

	if len(drifts) > 0 {
		fmt.Fprintln(out, "[drift] ClickHouse schema mismatch:")
		for _, d := range drifts {
			fmt.Fprintf(out, "  - %s\n", d)
		}
		return fmt.Errorf("initdb: schema drift: %s", strings.Join(drifts, "; "))
	}

	// --- SQLite table count check ---
	sqlDB, err := sqlitedb.Open(cfg.SQLite.Path)
	if err != nil {
		return fmt.Errorf("initdb: open sqlite for check: %w", err)
	}
	defer sqlDB.Close()

	count, err := sqlDB.CountTables(ctx)
	if err != nil {
		return fmt.Errorf("initdb: count sqlite tables: %w", err)
	}
	const wantTables = 4
	if count != wantTables {
		return fmt.Errorf("initdb: sqlite table count: got %d, want %d", count, wantTables)
	}

	fmt.Fprintln(out, "[ok] schema matches expected")
	return nil
}

// buildCreateTableSQL returns the verbatim CREATE TABLE DDL from the spec,
// parameterised on the database name from config.
func buildCreateTableSQL(db string) string {
	return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.events (
    event_id             UUID,
    machine              String,
    project_id           String,
    captured_at          DateTime64(3),
    source               String,
    is_internal          Bool,
    session_hint         String,
    raw_request_body     String,
    raw_response_body    String,
    response_status      UInt16,
    response_stop_reason String,
    request_model        String,
    input_tokens         UInt32,
    output_tokens        UInt32,
    cache_read_tokens    UInt32,
    cache_write_tokens   UInt32,
    first_chunk_ms       UInt32,
    total_ms             UInt32,
    error_type           String,
    error_message        String,
    user_text            String,
    assistant_text       String,
    summary              String,
    has_decision         Bool,
    skip_reason          String,
    denoised_at          DateTime64(3),
    denoise_model        String
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(captured_at)
ORDER BY (project_id, captured_at, event_id)
SETTINGS index_granularity = 8192`, db)
}
