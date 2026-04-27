// Package sqlitedb provides SQLite helpers and schema management for marc-server.
// It is server-binary-only — it must not be imported by cmd/marc.
package sqlitedb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3" // CGO SQLite driver; required by database/sql
)

// DB wraps *sql.DB with marc-specific helpers.
type DB struct {
	db *sql.DB
}

// PendingQuestion mirrors all columns of the pending_questions table.
type PendingQuestion struct {
	QuestionID        int64
	ProjectID         string
	SeedEventID       string
	RetrievedEventIDs []string // marshaled as JSON array in the DB column
	Situation         string
	Question          string
	OptionA           string
	OptionB           string
	PrincipleTested   string
	DurabilityScore   int
	ObviousnessScore  int
	Status            string // 'ready' | 'sent' | 'answered' | 'skipped' | 'discarded'
	GeneratedAt       time.Time
	SentAt            *time.Time
	AnsweredAt        *time.Time
	TelegramMessageID *int64
	AnswerEventID     string
}

// validStatuses is the set of allowed values for pending_questions.status.
var validStatuses = map[string]bool{
	"ready":     true,
	"sent":      true,
	"answered":  true,
	"skipped":   true,
	"discarded": true,
}

// Open opens (or creates) the SQLite database at path, applies all required
// pragmas (each individually verified), creates the schema if it does not
// exist, and seeds question_gen_cursor.
//
// On a fresh database file the permissions are set to 0600 before any data is
// written.  Open is idempotent: re-opening an existing database applies the
// same pragmas but does not recreate tables.
func Open(path string) (*DB, error) {
	// Create parent directory if needed.
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("sqlitedb: create parent dir %s: %w", dir, err)
	}

	// Detect whether this is a fresh file so we can set 0600 after creation.
	freshFile := false
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		freshFile = true
	}

	// Use the mattn/go-sqlite3 driver registered as "sqlite3".
	sqlDB, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: open %s: %w", path, err)
	}

	// SQLite allows only one writer at a time; limit the connection pool to 1
	// so that WAL mode can be used without the overhead of connection-level
	// mutex management in the application layer.
	sqlDB.SetMaxOpenConns(1)

	// Apply and verify all four pragmas before any schema work.
	if err := applyPragmas(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	// Create schema (idempotent).
	if err := applySchema(sqlDB); err != nil {
		_ = sqlDB.Close()
		return nil, err
	}

	// Set 0600 on a freshly-created file after the DB is valid.
	if freshFile {
		if err := os.Chmod(path, 0o600); err != nil {
			_ = sqlDB.Close()
			return nil, fmt.Errorf("sqlitedb: chmod %s: %w", path, err)
		}
	}

	return &DB{db: sqlDB}, nil
}

// applyPragmas sets the four required pragmas and verifies each one by querying
// it back from SQLite.  Returns an error if any pragma cannot be set or does
// not report the expected value.
func applyPragmas(db *sql.DB) error {
	type pragmaCheck struct {
		set     string
		verify  string
		wantStr string // expected string value (empty → use wantInt)
		wantInt int64  // expected integer value (used when wantStr == "")
		useStr  bool   // true → compare as string; false → compare as int64
	}

	checks := []pragmaCheck{
		{
			set:     "PRAGMA journal_mode = WAL",
			verify:  "PRAGMA journal_mode",
			wantStr: "wal",
			useStr:  true,
		},
		{
			set:     "PRAGMA busy_timeout = 5000",
			verify:  "PRAGMA busy_timeout",
			wantInt: 5000,
			useStr:  false,
		},
		{
			set:     "PRAGMA foreign_keys = ON",
			verify:  "PRAGMA foreign_keys",
			wantInt: 1,
			useStr:  false,
		},
		{
			set:     "PRAGMA synchronous = NORMAL",
			verify:  "PRAGMA synchronous",
			wantInt: 1, // NORMAL is stored as 1 internally
			useStr:  false,
		},
	}

	for _, c := range checks {
		// Set the pragma.
		if _, err := db.Exec(c.set); err != nil {
			return fmt.Errorf("sqlitedb: set pragma %q: %w", c.set, err)
		}

		// Verify the pragma took effect.
		row := db.QueryRow(c.verify)
		if c.useStr {
			var got string
			if err := row.Scan(&got); err != nil {
				return fmt.Errorf("sqlitedb: verify pragma %q: %w", c.verify, err)
			}
			if got != c.wantStr {
				return fmt.Errorf("sqlitedb: pragma %q: got %q, want %q", c.verify, got, c.wantStr)
			}
		} else {
			var got int64
			if err := row.Scan(&got); err != nil {
				return fmt.Errorf("sqlitedb: verify pragma %q: %w", c.verify, err)
			}
			if got != c.wantInt {
				return fmt.Errorf("sqlitedb: pragma %q: got %d, want %d", c.verify, got, c.wantInt)
			}
		}
	}

	return nil
}

// Close closes the underlying *sql.DB.
func (d *DB) Close() error {
	return d.db.Close()
}

// ExportDB returns the underlying *sql.DB for test code that needs direct SQL
// access (e.g., to read back cursor values or seed rows).
// Production code should use the typed helpers instead.
func (d *DB) ExportDB() *sql.DB {
	return d.db
}

// CountTables returns the number of user-created tables in sqlite_master that
// belong to the four expected marc state tables. Used by initdb --check.
func (d *DB) CountTables(ctx context.Context) (int, error) {
	const q = `SELECT count(*) FROM sqlite_master
	           WHERE type = 'table'
	           AND name IN ('projects','processor_cursors','question_gen_cursor','pending_questions')`
	var n int
	if err := d.db.QueryRowContext(ctx, q).Scan(&n); err != nil {
		return 0, fmt.Errorf("sqlitedb: CountTables: %w", err)
	}
	return n, nil
}

// UpsertCursor inserts or replaces the cursor for the given machine.
// key is the last_object_key value (for shipper cursors stored in
// processor_cursors).
func (d *DB) UpsertCursor(ctx context.Context, machine, key string) error {
	const q = `INSERT INTO processor_cursors (machine, last_object_key, last_processed_at)
	           VALUES (?, ?, datetime('now'))
	           ON CONFLICT(machine) DO UPDATE SET
	               last_object_key   = excluded.last_object_key,
	               last_processed_at = excluded.last_processed_at`
	if _, err := d.db.ExecContext(ctx, q, machine, key); err != nil {
		return fmt.Errorf("sqlitedb: UpsertCursor machine=%q: %w", machine, err)
	}
	return nil
}

// GetCursor returns the last_object_key for machine.
// Returns ("", nil) when no cursor exists yet.
func (d *DB) GetCursor(ctx context.Context, machine string) (string, error) {
	const q = `SELECT last_object_key FROM processor_cursors WHERE machine = ?`
	var key string
	err := d.db.QueryRowContext(ctx, q, machine).Scan(&key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("sqlitedb: GetCursor machine=%q: %w", machine, err)
	}
	return key, nil
}

// InsertQuestion inserts a new pending question with status='ready' and returns
// the auto-assigned question_id.
func (d *DB) InsertQuestion(ctx context.Context, q PendingQuestion) (int64, error) {
	eventIDs, err := json.Marshal(q.RetrievedEventIDs)
	if err != nil {
		return 0, fmt.Errorf("sqlitedb: InsertQuestion marshal retrieved_event_ids: %w", err)
	}

	const stmt = `INSERT INTO pending_questions (
	    project_id, seed_event_id, retrieved_event_ids,
	    situation, question, option_a, option_b,
	    principle_tested, durability_score, obviousness_score,
	    status, generated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ready', ?)`

	genAt := q.GeneratedAt.UTC().Format(time.RFC3339)
	res, err := d.db.ExecContext(ctx, stmt,
		q.ProjectID, q.SeedEventID, string(eventIDs),
		q.Situation, q.Question, q.OptionA, q.OptionB,
		q.PrincipleTested, q.DurabilityScore, q.ObviousnessScore,
		genAt,
	)
	if err != nil {
		return 0, fmt.Errorf("sqlitedb: InsertQuestion: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("sqlitedb: InsertQuestion LastInsertId: %w", err)
	}
	return id, nil
}

// UpdateQuestionStatus changes the status of a question and optionally sets
// additional fields supplied via extras.  Valid extras keys:
//
//   - "sent_at"             → string or *time.Time
//   - "answered_at"         → string or *time.Time
//   - "telegram_message_id" → int64 or *int64
//   - "answer_event_id"     → string
//
// Returns an error for any unrecognised status value.
func (d *DB) UpdateQuestionStatus(ctx context.Context, id int64, status string, extras map[string]any) error {
	if !validStatuses[status] {
		return fmt.Errorf("sqlitedb: invalid status %q", status)
	}

	// Build the SET clause dynamically so callers only set what they need.
	setCols := []string{"status = ?"}
	args := []any{status}

	for key, val := range extras {
		switch key {
		case "sent_at", "answered_at":
			switch v := val.(type) {
			case string:
				setCols = append(setCols, key+" = ?")
				args = append(args, v)
			case *time.Time:
				if v == nil {
					setCols = append(setCols, key+" = NULL")
				} else {
					setCols = append(setCols, key+" = ?")
					args = append(args, v.UTC().Format(time.RFC3339))
				}
			case time.Time:
				setCols = append(setCols, key+" = ?")
				args = append(args, v.UTC().Format(time.RFC3339))
			default:
				return fmt.Errorf("sqlitedb: UpdateQuestionStatus: unsupported type for %q: %T", key, val)
			}
		case "telegram_message_id":
			switch v := val.(type) {
			case int64:
				setCols = append(setCols, key+" = ?")
				args = append(args, v)
			case *int64:
				if v == nil {
					setCols = append(setCols, key+" = NULL")
				} else {
					setCols = append(setCols, key+" = ?")
					args = append(args, *v)
				}
			default:
				return fmt.Errorf("sqlitedb: UpdateQuestionStatus: unsupported type for %q: %T", key, val)
			}
		case "answer_event_id":
			switch v := val.(type) {
			case string:
				setCols = append(setCols, key+" = ?")
				args = append(args, v)
			default:
				return fmt.Errorf("sqlitedb: UpdateQuestionStatus: unsupported type for %q: %T", key, val)
			}
		default:
			return fmt.Errorf("sqlitedb: UpdateQuestionStatus: unknown extra field %q", key)
		}
	}

	// Append the WHERE argument.
	args = append(args, id)

	query := "UPDATE pending_questions SET "
	for i, col := range setCols {
		if i > 0 {
			query += ", "
		}
		query += col
	}
	query += " WHERE question_id = ?"

	res, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("sqlitedb: UpdateQuestionStatus id=%d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlitedb: UpdateQuestionStatus RowsAffected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("sqlitedb: UpdateQuestionStatus: no row with question_id=%d", id)
	}
	return nil
}

// GetNextReadyQuestion returns the oldest question with status='ready' (FIFO
// by generated_at).  Returns (nil, nil) when there are no ready questions.
func (d *DB) GetNextReadyQuestion(ctx context.Context) (*PendingQuestion, error) {
	const q = `SELECT
	    question_id, project_id, seed_event_id, retrieved_event_ids,
	    situation, question, option_a, option_b,
	    principle_tested, durability_score, obviousness_score,
	    status, generated_at,
	    sent_at, answered_at, telegram_message_id, answer_event_id
	FROM pending_questions
	WHERE status = 'ready'
	ORDER BY generated_at ASC
	LIMIT 1`

	row := d.db.QueryRowContext(ctx, q)
	pq, err := scanPendingQuestion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: GetNextReadyQuestion: %w", err)
	}
	return pq, nil
}

// UpdateQuestionGenCursor writes ts as the new last_event_ts for the singleton
// cursor row (id = 1). It is called after each successful generation cycle to
// record the timestamp of the newest event that was processed.
func (d *DB) UpdateQuestionGenCursor(ctx context.Context, ts time.Time) error {
	const q = `UPDATE question_gen_cursor
	           SET last_event_ts = ?, updated_at = datetime('now')
	           WHERE id = 1`
	tsStr := ts.UTC().Format(time.RFC3339)
	res, err := d.db.ExecContext(ctx, q, tsStr)
	if err != nil {
		return fmt.Errorf("sqlitedb: UpdateQuestionGenCursor: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("sqlitedb: UpdateQuestionGenCursor RowsAffected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("sqlitedb: UpdateQuestionGenCursor: cursor row (id=1) not found")
	}
	return nil
}

// GetQuestionByID returns the pending question with the given question_id.
// Returns an error wrapping sql.ErrNoRows when not found.
func (d *DB) GetQuestionByID(ctx context.Context, id int64) (*PendingQuestion, error) {
	const q = `SELECT
	    question_id, project_id, seed_event_id, retrieved_event_ids,
	    situation, question, option_a, option_b,
	    principle_tested, durability_score, obviousness_score,
	    status, generated_at,
	    sent_at, answered_at, telegram_message_id, answer_event_id
	FROM pending_questions
	WHERE question_id = ?`

	row := d.db.QueryRowContext(ctx, q, id)
	pq, err := scanPendingQuestion(row)
	if err != nil {
		return nil, fmt.Errorf("sqlitedb: GetQuestionByID %d: %w", id, err)
	}
	return pq, nil
}

// GetStats returns the count of questions in status='ready' (queued) and
// status='answered'.
func (d *DB) GetStats(ctx context.Context) (queued, answered int, err error) {
	const q = `SELECT
	    SUM(CASE WHEN status='ready'    THEN 1 ELSE 0 END),
	    SUM(CASE WHEN status='answered' THEN 1 ELSE 0 END)
	FROM pending_questions`

	row := d.db.QueryRowContext(ctx, q)
	var queuedNull, answeredNull sql.NullInt64
	if err := row.Scan(&queuedNull, &answeredNull); err != nil {
		return 0, 0, fmt.Errorf("sqlitedb: GetStats: %w", err)
	}
	return int(queuedNull.Int64), int(answeredNull.Int64), nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows so scanPendingQuestion
// can be used with a single-row query result.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanPendingQuestion scans a single row from pending_questions into a
// PendingQuestion struct.
func scanPendingQuestion(row rowScanner) (*PendingQuestion, error) {
	var (
		pq            PendingQuestion
		retrievedRaw  string
		generatedAt   string
		sentAt        sql.NullString
		answeredAt    sql.NullString
		telegramMsgID sql.NullInt64
		answerEventID sql.NullString
		seedEventID   sql.NullString
	)

	err := row.Scan(
		&pq.QuestionID,
		&pq.ProjectID,
		&seedEventID,
		&retrievedRaw,
		&pq.Situation,
		&pq.Question,
		&pq.OptionA,
		&pq.OptionB,
		&pq.PrincipleTested,
		&pq.DurabilityScore,
		&pq.ObviousnessScore,
		&pq.Status,
		&generatedAt,
		&sentAt,
		&answeredAt,
		&telegramMsgID,
		&answerEventID,
	)
	if err != nil {
		return nil, err
	}

	if seedEventID.Valid {
		pq.SeedEventID = seedEventID.String
	}

	// Unmarshal the JSON array of retrieved event IDs.
	if retrievedRaw != "" {
		if err := json.Unmarshal([]byte(retrievedRaw), &pq.RetrievedEventIDs); err != nil {
			return nil, fmt.Errorf("unmarshal retrieved_event_ids: %w", err)
		}
	}

	// Parse time fields stored as RFC3339 strings.
	if t, err := time.Parse(time.RFC3339, generatedAt); err == nil {
		pq.GeneratedAt = t
	}
	if sentAt.Valid {
		if t, err := time.Parse(time.RFC3339, sentAt.String); err == nil {
			pq.SentAt = &t
		}
	}
	if answeredAt.Valid {
		if t, err := time.Parse(time.RFC3339, answeredAt.String); err == nil {
			pq.AnsweredAt = &t
		}
	}
	if telegramMsgID.Valid {
		v := telegramMsgID.Int64
		pq.TelegramMessageID = &v
	}
	if answerEventID.Valid {
		pq.AnswerEventID = answerEventID.String
	}

	return &pq, nil
}
