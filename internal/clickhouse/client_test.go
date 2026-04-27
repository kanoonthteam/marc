package clickhouse_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
)

// configForAddr builds a ClickHouseConfig pointing at the given host:port.
// Shared by unit and integration tests (integration_test.go is only compiled
// with -tags integration, so this definition lives here where it is always
// available).
func configForAddr(addr string) config.ClickHouseConfig {
	return config.ClickHouseConfig{
		Addr:     addr,
		Database: "marc",
		User:     "default",
		Password: "",
	}
}

// fakeClient is a manual in-memory implementation of clickhouse.Client.
// It stores inserted events by event_id and supports error injection.
// It is safe for concurrent use (guarded by mu).
type fakeClient struct {
	mu     sync.Mutex
	events map[string]clickhouse.Event

	insertErr error
	queryErr  error
	pingErr   error
	closeErr  error
}

func newFakeClient() *fakeClient {
	return &fakeClient{
		events: make(map[string]clickhouse.Event),
	}
}

func (f *fakeClient) InsertEvent(_ context.Context, e clickhouse.Event) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// ReplacingMergeTree semantics: re-inserting the same event_id overwrites.
	// The key is the full ORDER BY tuple (project_id, captured_at, event_id)
	// but for the fake we key on event_id for simplicity.
	f.events[e.EventID.String()] = e
	return nil
}

func (f *fakeClient) QueryEvents(_ context.Context, _ string, _ ...any) ([]map[string]any, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	var rows []map[string]any
	for _, e := range f.events {
		rows = append(rows, map[string]any{
			"event_id":   e.EventID.String(),
			"machine":    e.Machine,
			"project_id": e.ProjectID,
			"source":     e.Source,
		})
	}
	return rows, nil
}

func (f *fakeClient) Exec(_ context.Context, _ string, _ ...any) error {
	return nil
}

func (f *fakeClient) Ping(_ context.Context) error {
	return f.pingErr
}

func (f *fakeClient) Close() error {
	return f.closeErr
}

// len returns the number of events currently stored.
func (f *fakeClient) len() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// get returns the event stored under id, or false if absent.
func (f *fakeClient) get(id uuid.UUID) (clickhouse.Event, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.events[id.String()]
	return e, ok
}

// --- tests ---

// TestConnectError_Unreachable verifies that Connect returns a non-nil
// error when ClickHouse is unreachable. Port 1 is guaranteed to be closed.
func TestConnectError_Unreachable(t *testing.T) {
	t.Parallel()
	// We test this via the interface contract: a Connect against a dead address
	// must produce a Client whose Ping fails. The native driver does lazy-connect,
	// so we verify on Ping rather than on Open.
	//
	// Alternatively, some configs return an error immediately on Open —
	// either way the caller gets a clear error before any work is done.
	//
	// Because the native driver may return the error on Open or on first use,
	// we accept an error from either Connect or Ping.
	cfg := configForAddr("127.0.0.1:1") // port 1 is guaranteed unreachable
	client, openErr := clickhouse.Connect(cfg)
	if openErr != nil {
		// Connect itself surfaced the error — acceptable.
		t.Logf("Connect returned error immediately: %v", openErr)
		return
	}
	defer client.Close() //nolint:errcheck // best-effort cleanup in test

	// Driver lazy-connects; Ping forces actual dial.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	pingErr := client.Ping(ctx)
	if pingErr == nil {
		t.Fatal("expected Ping to fail for unreachable address, got nil")
	}
	t.Logf("Ping returned expected error: %v", pingErr)
}

// TestFakeClient_InsertEvent checks the happy path using the fake.
func TestFakeClient_InsertEvent(t *testing.T) {
	t.Parallel()
	f := newFakeClient()
	ctx := context.Background()

	e := clickhouse.Event{
		EventID:    uuid.New(),
		Machine:    "test-machine",
		ProjectID:  "proj-abc",
		CapturedAt: time.Now().Truncate(time.Millisecond),
		Source:     "anthropic_api",
	}

	if err := f.InsertEvent(ctx, e); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	if f.len() != 1 {
		t.Fatalf("expected 1 event, got %d", f.len())
	}

	got, ok := f.get(e.EventID)
	if !ok {
		t.Fatal("event not found after insert")
	}
	if got.Machine != e.Machine {
		t.Errorf("machine: got %q, want %q", got.Machine, e.Machine)
	}
}

// TestFakeClient_InsertEvent_ErrorWrapping verifies that errors from InsertEvent
// are returned as-is (or wrapped) so callers can use errors.Is.
func TestFakeClient_InsertEvent_ErrorWrapping(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("injected insert error")
	f := newFakeClient()
	f.insertErr = sentinel

	err := f.InsertEvent(context.Background(), clickhouse.Event{EventID: uuid.New()})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected errors.Is(err, sentinel), but err = %v", err)
	}
}

// TestFakeClient_Dedup verifies that re-inserting the same event_id does not
// grow the row count. This mirrors the ReplacingMergeTree dedup contract:
// after a merge (or OPTIMIZE TABLE FINAL in integration tests), duplicates
// are collapsed. The fake collapses immediately on insert.
func TestFakeClient_Dedup(t *testing.T) {
	t.Parallel()
	f := newFakeClient()
	ctx := context.Background()

	id := uuid.New()
	base := clickhouse.Event{
		EventID:    id,
		Machine:    "original",
		ProjectID:  "proj-x",
		CapturedAt: time.Now().Truncate(time.Millisecond),
	}

	if err := f.InsertEvent(ctx, base); err != nil {
		t.Fatalf("first insert: %v", err)
	}

	updated := base
	updated.Machine = "updated"
	if err := f.InsertEvent(ctx, updated); err != nil {
		t.Fatalf("second insert: %v", err)
	}

	if f.len() != 1 {
		t.Fatalf("expected 1 row after re-insert of same event_id, got %d", f.len())
	}
	got, _ := f.get(id)
	if got.Machine != "updated" {
		t.Errorf("expected machine %q after re-insert, got %q", "updated", got.Machine)
	}
}

// TestFakeClient_QueryEvents_Shape verifies that QueryEvents returns
// results shaped as []map[string]any keyed by column name.
func TestFakeClient_QueryEvents_Shape(t *testing.T) {
	t.Parallel()
	f := newFakeClient()
	ctx := context.Background()

	// Insert two distinct events.
	for i := 0; i < 2; i++ {
		e := clickhouse.Event{
			EventID:   uuid.New(),
			Machine:   fmt.Sprintf("machine-%d", i),
			ProjectID: "proj-q",
		}
		if err := f.InsertEvent(ctx, e); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	rows, err := f.QueryEvents(ctx, "SELECT * FROM marc.events WHERE project_id = ?", "proj-q")
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Each row must contain an event_id key.
	for i, row := range rows {
		if _, ok := row["event_id"]; !ok {
			t.Errorf("row %d missing event_id key", i)
		}
	}
}

// TestFakeClient_QueryEvents_Error verifies error propagation from QueryEvents.
func TestFakeClient_QueryEvents_Error(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("injected query error")
	f := newFakeClient()
	f.queryErr = sentinel

	_, err := f.QueryEvents(context.Background(), "SELECT 1")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is: want sentinel, got %v", err)
	}
}

// TestFakeClient_Ping_Success verifies that Ping returns nil when no error is injected.
func TestFakeClient_Ping_Success(t *testing.T) {
	t.Parallel()
	f := newFakeClient()
	if err := f.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: unexpected error: %v", err)
	}
}

// TestFakeClient_Ping_Error verifies that Ping surfaces injected errors.
func TestFakeClient_Ping_Error(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("injected ping error")
	f := newFakeClient()
	f.pingErr = sentinel

	err := f.Ping(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is: want sentinel, got %v", err)
	}
}

// TestFakeClient_Close verifies Close with error injection.
func TestFakeClient_Close(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("injected close error")
	f := newFakeClient()
	f.closeErr = sentinel

	err := f.Close()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("errors.Is: want sentinel, got %v", err)
	}
}

// Ensure fakeClient satisfies the Client interface at compile time.
var _ clickhouse.Client = (*fakeClient)(nil)
