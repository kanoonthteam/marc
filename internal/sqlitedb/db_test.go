package sqlitedb

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openTempDB opens a SQLite database backed by a fresh file under t.TempDir().
// In-memory databases (:memory:) cannot use WAL mode — SQLite stores WAL
// state in a sidecar file, which has no in-memory equivalent — so production
// SQLite is always file-backed and tests follow suit.
func openTempDB(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// sampleQuestion returns a minimal valid PendingQuestion for insertion.
// project must already exist in the projects table.
func sampleQuestion(project string) PendingQuestion {
	return PendingQuestion{
		ProjectID:         project,
		SeedEventID:       "seed-event-001",
		RetrievedEventIDs: []string{"evt-a", "evt-b"},
		Situation:         "You are reviewing a pull request.",
		Question:          "Do you validate at the boundary or at the use site?",
		OptionA:           "Validate at the boundary",
		OptionB:           "Validate at the use site",
		PrincipleTested:   "defense-in-depth",
		DurabilityScore:   8,
		ObviousnessScore:  4,
		GeneratedAt:       time.Now().UTC().Truncate(time.Second),
	}
}

// insertProject inserts a row into projects so FK constraints are satisfied.
func insertProject(t *testing.T, db *DB, projectID string) {
	t.Helper()
	_, err := db.db.Exec(
		`INSERT OR IGNORE INTO projects (project_id, friendly_name) VALUES (?, ?)`,
		projectID, projectID,
	)
	if err != nil {
		t.Fatalf("insert project %q: %v", projectID, err)
	}
}

// ----- Acceptance criterion 1: Open() creates all four tables and the index -----

func TestOpen_CreatesSchema(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()

	tables := []string{"projects", "processor_cursors", "question_gen_cursor", "pending_questions"}
	for _, tbl := range tables {
		var name string
		err := db.db.QueryRowContext(ctx,
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}

	var idxName string
	err := db.db.QueryRowContext(ctx,
		`SELECT name FROM sqlite_master WHERE type='index' AND name='idx_pending_status'`,
	).Scan(&idxName)
	if err != nil {
		t.Errorf("index idx_pending_status not found: %v", err)
	}
}

// ----- Acceptance criterion 2: Open() is idempotent -----

func TestOpen_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	insertProject(t, db1, "proj")
	if _, err := db1.InsertQuestion(context.Background(), sampleQuestion("proj")); err != nil {
		t.Fatalf("InsertQuestion: %v", err)
	}
	_ = db1.Close()

	// Second open must succeed and the row inserted above must still be there.
	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second Open (idempotent): %v", err)
	}
	defer db2.Close()

	q, err := db2.GetNextReadyQuestion(context.Background())
	if err != nil {
		t.Fatalf("GetNextReadyQuestion after reopen: %v", err)
	}
	if q == nil {
		t.Fatal("expected a ready question after reopen, got nil")
	}
}

// ----- Acceptance criterion 3: pragmas applied and individually verified -----

func TestOpen_Pragmas(t *testing.T) {
	db := openTempDB(t)

	checks := []struct {
		pragma  string
		wantStr string
		wantInt int64
		useStr  bool
	}{
		{pragma: "PRAGMA journal_mode", wantStr: "wal", useStr: true},
		{pragma: "PRAGMA busy_timeout", wantInt: 5000},
		{pragma: "PRAGMA foreign_keys", wantInt: 1},
		{pragma: "PRAGMA synchronous", wantInt: 1},
	}

	for _, c := range checks {
		row := db.db.QueryRow(c.pragma)
		if c.useStr {
			var got string
			if err := row.Scan(&got); err != nil {
				t.Errorf("%s scan: %v", c.pragma, err)
				continue
			}
			if got != c.wantStr {
				t.Errorf("%s = %q, want %q", c.pragma, got, c.wantStr)
			}
		} else {
			var got int64
			if err := row.Scan(&got); err != nil {
				t.Errorf("%s scan: %v", c.pragma, err)
				continue
			}
			if got != c.wantInt {
				t.Errorf("%s = %d, want %d", c.pragma, got, c.wantInt)
			}
		}
	}
}

// ----- Acceptance criterion 4: file mode 0600 on fresh creation -----

func TestOpen_FileMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.db")

	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	_ = db.Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("os.Stat: %v", err)
	}
	mode := info.Mode().Perm()
	if mode != 0o600 {
		t.Errorf("file mode = %o, want 0600", mode)
	}
}

// ----- Acceptance criterion 5: UpsertCursor/GetCursor round-trip -----

func TestCursorRoundTrip(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()

	// No cursor yet → empty string.
	got, err := db.GetCursor(ctx, "machine-a")
	if err != nil {
		t.Fatalf("GetCursor missing: %v", err)
	}
	if got != "" {
		t.Errorf("GetCursor empty: got %q, want %q", got, "")
	}

	// Insert cursor.
	if err := db.UpsertCursor(ctx, "machine-a", "marc/raw/machine-a/2026/04/27/10/obj.jsonl"); err != nil {
		t.Fatalf("UpsertCursor: %v", err)
	}
	got, err = db.GetCursor(ctx, "machine-a")
	if err != nil {
		t.Fatalf("GetCursor after upsert: %v", err)
	}
	want := "marc/raw/machine-a/2026/04/27/10/obj.jsonl"
	if got != want {
		t.Errorf("GetCursor = %q, want %q", got, want)
	}

	// Update cursor.
	if err := db.UpsertCursor(ctx, "machine-a", "marc/raw/machine-a/2026/04/27/11/obj.jsonl"); err != nil {
		t.Fatalf("UpsertCursor update: %v", err)
	}
	got, err = db.GetCursor(ctx, "machine-a")
	if err != nil {
		t.Fatalf("GetCursor after update: %v", err)
	}
	want = "marc/raw/machine-a/2026/04/27/11/obj.jsonl"
	if got != want {
		t.Errorf("GetCursor after update = %q, want %q", got, want)
	}

	// Separate machine retains independent cursor.
	got, err = db.GetCursor(ctx, "machine-b")
	if err != nil {
		t.Fatalf("GetCursor machine-b: %v", err)
	}
	if got != "" {
		t.Errorf("GetCursor machine-b = %q, want empty", got)
	}
}

// ----- Acceptance criterion 6: GetNextReadyQuestion FIFO by generated_at -----

func TestGetNextReadyQuestion_FIFO(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertProject(t, db, "proj")

	now := time.Now().UTC().Truncate(time.Second)

	// Insert three questions with different generated_at values.
	// Middle timestamp first, then newest, then oldest — order of INSERT should
	// not matter; FIFO by generated_at must hold.
	q2 := sampleQuestion("proj")
	q2.Situation = "Second"
	q2.GeneratedAt = now.Add(1 * time.Minute)

	q3 := sampleQuestion("proj")
	q3.Situation = "Third"
	q3.GeneratedAt = now.Add(2 * time.Minute)

	q1 := sampleQuestion("proj")
	q1.Situation = "First"
	q1.GeneratedAt = now

	for _, q := range []PendingQuestion{q2, q3, q1} {
		if _, err := db.InsertQuestion(ctx, q); err != nil {
			t.Fatalf("InsertQuestion: %v", err)
		}
	}

	// First call should return "First" (oldest generated_at).
	next, err := db.GetNextReadyQuestion(ctx)
	if err != nil {
		t.Fatalf("GetNextReadyQuestion: %v", err)
	}
	if next == nil {
		t.Fatal("expected a question, got nil")
	}
	if next.Situation != "First" {
		t.Errorf("FIFO: got situation %q, want %q", next.Situation, "First")
	}

	// Mark it answered, then next should be "Second".
	if err := db.UpdateQuestionStatus(ctx, next.QuestionID, "answered", nil); err != nil {
		t.Fatalf("UpdateQuestionStatus answered: %v", err)
	}
	next, err = db.GetNextReadyQuestion(ctx)
	if err != nil {
		t.Fatalf("GetNextReadyQuestion 2nd: %v", err)
	}
	if next.Situation != "Second" {
		t.Errorf("FIFO 2nd: got situation %q, want %q", next.Situation, "Second")
	}
}

// GetNextReadyQuestion returns nil when the queue is empty.
func TestGetNextReadyQuestion_Empty(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()

	got, err := db.GetNextReadyQuestion(ctx)
	if err != nil {
		t.Fatalf("GetNextReadyQuestion empty queue: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for empty queue, got %+v", got)
	}
}

// ----- Acceptance criterion 7: UpdateQuestionStatus status enum validation -----

func TestUpdateQuestionStatus_InvalidStatus(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertProject(t, db, "proj")

	id, err := db.InsertQuestion(ctx, sampleQuestion("proj"))
	if err != nil {
		t.Fatalf("InsertQuestion: %v", err)
	}

	badStatuses := []string{"", "pending", "READY", "done", "open"}
	for _, s := range badStatuses {
		if err := db.UpdateQuestionStatus(ctx, id, s, nil); err == nil {
			t.Errorf("UpdateQuestionStatus(%q) should return error, got nil", s)
		}
	}
}

func TestUpdateQuestionStatus_ValidStatuses(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertProject(t, db, "proj")

	statuses := []string{"sent", "answered", "skipped", "discarded", "ready"}
	for _, s := range statuses {
		id, err := db.InsertQuestion(ctx, sampleQuestion("proj"))
		if err != nil {
			t.Fatalf("InsertQuestion for status %q: %v", s, err)
		}
		if err := db.UpdateQuestionStatus(ctx, id, s, nil); err != nil {
			t.Errorf("UpdateQuestionStatus(%q) unexpected error: %v", s, err)
		}
	}
}

// ----- Acceptance criterion 8: question_gen_cursor seed row -----

func TestQuestionGenCursorSeed(t *testing.T) {
	db := openTempDB(t)
	var ts string
	err := db.db.QueryRow(`SELECT last_event_ts FROM question_gen_cursor WHERE id = 1`).Scan(&ts)
	if err != nil {
		t.Fatalf("question_gen_cursor seed: %v", err)
	}
	if ts != "1970-01-01T00:00:00Z" {
		t.Errorf("seed last_event_ts = %q, want %q", ts, "1970-01-01T00:00:00Z")
	}
}

// ----- InsertQuestion / GetNextReadyQuestion full round-trip -----

func TestInsertAndRetrieveQuestion(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertProject(t, db, "sliplotto")

	q := sampleQuestion("sliplotto")
	id, err := db.InsertQuestion(ctx, q)
	if err != nil {
		t.Fatalf("InsertQuestion: %v", err)
	}
	if id == 0 {
		t.Fatal("InsertQuestion returned id=0")
	}

	got, err := db.GetNextReadyQuestion(ctx)
	if err != nil {
		t.Fatalf("GetNextReadyQuestion: %v", err)
	}
	if got == nil {
		t.Fatal("GetNextReadyQuestion returned nil")
	}
	if got.QuestionID != id {
		t.Errorf("question_id = %d, want %d", got.QuestionID, id)
	}
	if got.ProjectID != q.ProjectID {
		t.Errorf("project_id = %q, want %q", got.ProjectID, q.ProjectID)
	}
	if got.Situation != q.Situation {
		t.Errorf("situation = %q, want %q", got.Situation, q.Situation)
	}
	if len(got.RetrievedEventIDs) != len(q.RetrievedEventIDs) {
		t.Errorf("retrieved_event_ids len = %d, want %d", len(got.RetrievedEventIDs), len(q.RetrievedEventIDs))
	}
	for i, ev := range q.RetrievedEventIDs {
		if got.RetrievedEventIDs[i] != ev {
			t.Errorf("retrieved_event_ids[%d] = %q, want %q", i, got.RetrievedEventIDs[i], ev)
		}
	}
}

// ----- GetStats -----

func TestGetStats(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertProject(t, db, "proj")

	// Empty DB.
	queued, answered, err := db.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats empty: %v", err)
	}
	if queued != 0 || answered != 0 {
		t.Errorf("GetStats empty: got queued=%d answered=%d, want 0 0", queued, answered)
	}

	// Insert two questions.
	id1, _ := db.InsertQuestion(ctx, sampleQuestion("proj"))
	id2, _ := db.InsertQuestion(ctx, sampleQuestion("proj"))

	queued, answered, err = db.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats after insert: %v", err)
	}
	if queued != 2 || answered != 0 {
		t.Errorf("GetStats after insert: got queued=%d answered=%d, want 2 0", queued, answered)
	}

	// Answer one.
	if err := db.UpdateQuestionStatus(ctx, id1, "answered", nil); err != nil {
		t.Fatalf("UpdateQuestionStatus answered: %v", err)
	}
	// Skip the other.
	if err := db.UpdateQuestionStatus(ctx, id2, "skipped", nil); err != nil {
		t.Fatalf("UpdateQuestionStatus skipped: %v", err)
	}

	queued, answered, err = db.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats final: %v", err)
	}
	if queued != 0 || answered != 1 {
		t.Errorf("GetStats final: got queued=%d answered=%d, want 0 1", queued, answered)
	}
}

// ----- UpdateQuestionStatus extras (sent_at, telegram_message_id, answer_event_id) -----

func TestUpdateQuestionStatus_Extras(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertProject(t, db, "proj")

	id, err := db.InsertQuestion(ctx, sampleQuestion("proj"))
	if err != nil {
		t.Fatalf("InsertQuestion: %v", err)
	}

	msgID := int64(98765)
	sentAt := time.Now().UTC().Truncate(time.Second)
	extras := map[string]any{
		"sent_at":             sentAt,
		"telegram_message_id": msgID,
	}
	if err := db.UpdateQuestionStatus(ctx, id, "sent", extras); err != nil {
		t.Fatalf("UpdateQuestionStatus sent: %v", err)
	}

	// Verify stored values by reading back directly.
	var (
		status   string
		sentAtDB string
		tgID     int64
	)
	err = db.db.QueryRow(
		`SELECT status, sent_at, telegram_message_id FROM pending_questions WHERE question_id=?`, id,
	).Scan(&status, &sentAtDB, &tgID)
	if err != nil {
		t.Fatalf("scan after update: %v", err)
	}
	if status != "sent" {
		t.Errorf("status = %q, want %q", status, "sent")
	}
	if tgID != msgID {
		t.Errorf("telegram_message_id = %d, want %d", tgID, msgID)
	}
}

// ----- UpdateQuestionStatus: unknown extra field must error -----

func TestUpdateQuestionStatus_UnknownExtra(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()
	insertProject(t, db, "proj")

	id, _ := db.InsertQuestion(ctx, sampleQuestion("proj"))
	err := db.UpdateQuestionStatus(ctx, id, "sent", map[string]any{"bogus_field": "value"})
	if err == nil {
		t.Fatal("UpdateQuestionStatus with unknown extra field should error")
	}
}

// ----- UpdateQuestionStatus: non-existent question_id must error -----

func TestUpdateQuestionStatus_NotFound(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()

	err := db.UpdateQuestionStatus(ctx, 9999, "sent", nil)
	if err == nil {
		t.Fatal("UpdateQuestionStatus for missing id should error")
	}
}

// ----- Foreign key enforcement -----

func TestInsertQuestion_FKViolation(t *testing.T) {
	db := openTempDB(t)
	ctx := context.Background()

	// project "nonexistent" is not in the projects table.
	q := sampleQuestion("nonexistent")
	_, err := db.InsertQuestion(ctx, q)
	if err == nil {
		t.Fatal("InsertQuestion with unknown project_id should fail (FK constraint)")
	}
}
