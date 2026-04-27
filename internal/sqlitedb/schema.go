// Package sqlitedb provides SQLite helpers and schema management for marc-server.
package sqlitedb

import (
	"database/sql"
	"fmt"
)

// DDL constants for the four marc SQLite tables and the required index.
// The schema matches the spec exactly (docs/marc-spec-v1.md §SQLite).
const (
	sqlCreateProjects = `CREATE TABLE IF NOT EXISTS projects (
    project_id   TEXT PRIMARY KEY,
    friendly_name TEXT NOT NULL,
    description  TEXT,
    created_at   TEXT NOT NULL DEFAULT (datetime('now'))
);`

	sqlCreateProcessorCursors = `CREATE TABLE IF NOT EXISTS processor_cursors (
    machine            TEXT PRIMARY KEY,
    last_object_key    TEXT NOT NULL,
    last_processed_at  TEXT NOT NULL DEFAULT (datetime('now'))
);`

	sqlCreateQuestionGenCursor = `CREATE TABLE IF NOT EXISTS question_gen_cursor (
    id            INTEGER PRIMARY KEY CHECK (id = 1),
    last_event_ts TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    updated_at    TEXT NOT NULL DEFAULT (datetime('now'))
);`

	sqlSeedQuestionGenCursor = `INSERT OR IGNORE INTO question_gen_cursor (id) VALUES (1);`

	// sqlSeedDefaultProject creates the "default" project so newly-generated
	// questions can be inserted before an operator has configured per-project
	// project_id mappings. Without it, pending_questions.project_id (which
	// references projects.project_id) has no valid value to point at and
	// every InsertQuestion fails the FK constraint.
	sqlSeedDefaultProject = `INSERT OR IGNORE INTO projects (project_id, friendly_name) VALUES ('default', 'default');`

	sqlCreatePendingQuestions = `CREATE TABLE IF NOT EXISTS pending_questions (
    question_id         INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id          TEXT NOT NULL REFERENCES projects(project_id),
    seed_event_id       TEXT,
    retrieved_event_ids TEXT,
    situation           TEXT NOT NULL,
    question            TEXT NOT NULL,
    option_a            TEXT NOT NULL,
    option_b            TEXT NOT NULL,
    principle_tested    TEXT NOT NULL,
    durability_score    INTEGER NOT NULL,
    obviousness_score   INTEGER NOT NULL,
    status              TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready','sent','answered','skipped','discarded')),
    generated_at        TEXT NOT NULL DEFAULT (datetime('now')),
    sent_at             TEXT,
    answered_at         TEXT,
    telegram_message_id INTEGER,
    answer_event_id     TEXT
);`

	sqlCreateIdxPendingStatus = `CREATE INDEX IF NOT EXISTS idx_pending_status
    ON pending_questions(status, generated_at);`
)

// applySchema creates all four tables, the index, and seeds question_gen_cursor.
// It is idempotent — all statements use IF NOT EXISTS / INSERT OR IGNORE.
func applySchema(db *sql.DB) error {
	stmts := []struct {
		name string
		sql  string
	}{
		{"create projects", sqlCreateProjects},
		{"seed default project", sqlSeedDefaultProject},
		{"create processor_cursors", sqlCreateProcessorCursors},
		{"create question_gen_cursor", sqlCreateQuestionGenCursor},
		{"seed question_gen_cursor", sqlSeedQuestionGenCursor},
		{"create pending_questions", sqlCreatePendingQuestions},
		{"create idx_pending_status", sqlCreateIdxPendingStatus},
	}

	for _, s := range stmts {
		if _, err := db.Exec(s.sql); err != nil {
			return fmt.Errorf("sqlitedb: %s: %w", s.name, err)
		}
	}
	return nil
}
