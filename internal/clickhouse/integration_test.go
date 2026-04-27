//go:build integration

package clickhouse_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/caffeaun/marc/internal/clickhouse"
)

// dialOrSkip attempts to connect to the local ClickHouse at 127.0.0.1:19000.
// If the server is unreachable it calls t.Skip so the test is silently omitted
// in environments without ClickHouse (unit CI). Only the integration build tag
// causes these tests to compile at all.
func dialOrSkip(t *testing.T) clickhouse.Client {
	t.Helper()
	cfg := configForAddr("127.0.0.1:19000")
	client, err := clickhouse.Connect(cfg)
	if err != nil {
		t.Skipf("ClickHouse unreachable (Connect error): %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		_ = client.Close()
		t.Skipf("ClickHouse unreachable (Ping error): %v", err)
	}

	t.Cleanup(func() { _ = client.Close() })
	return client
}

// TestIntegration_Ping verifies Ping returns nil when ClickHouse is reachable
// with valid credentials (acceptance criterion 4).
func TestIntegration_Ping(t *testing.T) {
	client := dialOrSkip(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

// TestIntegration_InsertAndSelect verifies that a row inserted with InsertEvent
// is immediately queryable via QueryEvents (acceptance criterion 2).
func TestIntegration_InsertAndSelect(t *testing.T) {
	client := dialOrSkip(t)
	ctx := context.Background()

	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)

	e := clickhouse.Event{
		EventID:    id,
		Machine:    "integration-test-machine",
		ProjectID:  fmt.Sprintf("integ-proj-%s", id.String()[:8]),
		CapturedAt: now,
		Source:     "anthropic_api",
		IsInternal: false,
		// Leave all other fields at their zero values.
	}

	if err := client.InsertEvent(ctx, e); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	// ClickHouse makes rows visible immediately after INSERT (no commit required).
	rows, err := client.QueryEvents(ctx,
		"SELECT event_id, machine, project_id FROM marc.events WHERE event_id = ?",
		id,
	)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("expected 1 row after insert, got 0")
	}

	gotMachine, _ := rows[0]["machine"].(string)
	if gotMachine != e.Machine {
		t.Errorf("machine: got %q, want %q", gotMachine, e.Machine)
	}
}

// TestIntegration_Dedup verifies that re-inserting the same event_id does not
// produce duplicate rows after OPTIMIZE TABLE marc.events FINAL
// (acceptance criterion 3).
//
// ReplacingMergeTree deduplication note:
//
//	ClickHouse's ReplacingMergeTree engine removes duplicate rows (same ORDER BY
//	key) during background merge operations or when OPTIMIZE TABLE ... FINAL is
//	executed. Duplicates ARE visible between the insert and the next merge, which
//	is intentional — OPTIMIZE forces the merge and makes dedup observable in tests.
//	In production, background merges handle this automatically; callers should not
//	depend on immediate dedup without OPTIMIZE.
func TestIntegration_Dedup(t *testing.T) {
	client := dialOrSkip(t)
	ctx := context.Background()

	id := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	projID := fmt.Sprintf("integ-dedup-%s", id.String()[:8])

	base := clickhouse.Event{
		EventID:    id,
		Machine:    "dedup-machine-v1",
		ProjectID:  projID,
		CapturedAt: now,
		Source:     "anthropic_api",
	}

	// First insert.
	if err := client.InsertEvent(ctx, base); err != nil {
		t.Fatalf("first InsertEvent: %v", err)
	}

	// Re-insert with a different Machine value to produce a "newer" version.
	updated := base
	updated.Machine = "dedup-machine-v2"
	if err := client.InsertEvent(ctx, updated); err != nil {
		t.Fatalf("second InsertEvent: %v", err)
	}

	// Force a merge so duplicates are collapsed.
	if err := client.Exec(ctx, "OPTIMIZE TABLE marc.events FINAL"); err != nil {
		t.Fatalf("OPTIMIZE TABLE: %v", err)
	}

	rows, err := client.QueryEvents(ctx,
		"SELECT event_id, machine FROM marc.events WHERE event_id = ? AND project_id = ?",
		id, projID,
	)
	if err != nil {
		t.Fatalf("QueryEvents after OPTIMIZE: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("expected exactly 1 row after dedup, got %d", len(rows))
	}

	// After ReplacingMergeTree dedup, only the last-inserted version survives.
	gotMachine, _ := rows[0]["machine"].(string)
	if gotMachine != "dedup-machine-v2" {
		t.Errorf("expected last-written machine %q, got %q", "dedup-machine-v2", gotMachine)
	}
}

// TestIntegration_ConnectError verifies that Connect returns a clear error for
// an unreachable address (acceptance criterion 1). This test does NOT require a
// live ClickHouse — it only needs the integration tag so it runs alongside the
// other integration tests and the error message is human-visible.
func TestIntegration_ConnectError(t *testing.T) {
	cfg := configForAddr("127.0.0.1:1") // port 1 is always unreachable
	client, openErr := clickhouse.Connect(cfg)

	if openErr != nil {
		// Connect itself returned an error — that satisfies the criterion.
		t.Logf("Connect returned clear error: %v", openErr)
		return
	}
	defer client.Close() //nolint:errcheck // best-effort cleanup

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := client.Ping(ctx); err != nil {
		t.Logf("Ping returned clear error: %v", err)
		return
	}

	t.Fatal("expected an error connecting to 127.0.0.1:1 but got none")
}
