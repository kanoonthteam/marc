// Package generate implements the marc-server hourly question-generation runner.
//
// Run performs one generation cycle:
//  1. Queries ClickHouse for recent decision-bearing events.
//  2. Assembles a prompt from question_gen.md + the serialized event list.
//  3. Invokes `claude -p --output-format json` as a subprocess.
//  4. Parses the JSON output as []CandidateQuestion.
//  5. Filters by DurabilityScore / ObviousnessScore thresholds.
//  6. Inserts surviving questions into SQLite pending_questions.
//  7. Updates the question_gen_cursor to the latest captured_at seen.
//
// The systemd timer (T017) drives the hourly cadence — this package is
// invoked once per hour and returns.
package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/caffeaun/marc/internal/clickhouse"
	"github.com/caffeaun/marc/internal/config"
	"github.com/caffeaun/marc/internal/sqlitedb"
)

// CandidateQuestion is the struct the model returns for each question it
// proposes. Fields are tagged to match the JSON keys the prompt instructs
// the model to emit.
type CandidateQuestion struct {
	Situation        string `json:"situation"`
	Question         string `json:"question"`
	OptionA          string `json:"option_a"`
	OptionB          string `json:"option_b"`
	PrincipleTested  string `json:"principle_tested"`
	DurabilityScore  int    `json:"durability_score"`
	ObviousnessScore int    `json:"obviousness_score"`
	// SeedEventID is optional — the model may emit it if it identifies the
	// most relevant source event.
	SeedEventID string `json:"seed_event_id"`
	// RetrievedEventIDs is optional — the model may emit the IDs it drew on.
	RetrievedEventIDs []string `json:"retrieved_event_ids"`
}

// claudeOutputEnvelope is the JSON shape emitted by `claude -p --output-format json`.
// Verified against claude CLI version 2.1.119.
type claudeOutputEnvelope struct {
	Type    string `json:"type"`
	IsError bool   `json:"is_error"`
	// Result contains the raw text the model produced.
	Result string `json:"result"`
}

// Options configure a single call to Run. Production wiring passes nil for the
// injection-point fields and uses the real implementations. Tests override them.
type Options struct {
	Config *config.ServerConfig
	// Out is the writer for structured log output. Defaults to os.Stdout.
	Out io.Writer

	// Test injection points — leave nil in production.

	// NewClickHouseConn, if set, is used instead of clickhouse.Connect.
	NewClickHouseConn func(config.ClickHouseConfig) (clickhouse.Client, error)
	// SQLiteDB, if set, is used instead of opening cfg.SQLite.Path.
	SQLiteDB *sqlitedb.DB
	// ClaudeRunner, if set, is called instead of launching the real subprocess.
	ClaudeRunner func(ctx context.Context, binary, prompt string, env []string) (stdout, stderr []byte, err error)
	// NowFn, if set, replaces time.Now for deterministic tests.
	NowFn func() time.Time
}

// Run executes one question-generation cycle and returns.  A nil error means
// the cycle completed (even if zero questions were inserted).  Permanent errors
// (binary not found, context deadline) are returned as non-nil so the caller
// (systemd timer) can record them in the journal.
func Run(ctx context.Context, opts Options) error {
	if opts.Out == nil {
		opts.Out = os.Stdout
	}
	now := time.Now
	if opts.NowFn != nil {
		now = opts.NowFn
	}

	logger := slog.New(slog.NewJSONHandler(opts.Out, nil))
	cfg := opts.Config

	// --- 1. Open ClickHouse ---
	var chClient clickhouse.Client
	var err error
	if opts.NewClickHouseConn != nil {
		chClient, err = opts.NewClickHouseConn(cfg.ClickHouse)
	} else {
		chClient, err = clickhouse.Connect(cfg.ClickHouse)
	}
	if err != nil {
		return fmt.Errorf("generate: clickhouse connect: %w", err)
	}
	defer chClient.Close() //nolint:errcheck // best-effort cleanup

	// --- 2. Open SQLite ---
	var db *sqlitedb.DB
	if opts.SQLiteDB != nil {
		db = opts.SQLiteDB
	} else {
		db, err = sqlitedb.Open(cfg.SQLite.Path)
		if err != nil {
			return fmt.Errorf("generate: sqlite open: %w", err)
		}
		defer db.Close() //nolint:errcheck // best-effort cleanup
	}

	// --- 3. Query ClickHouse ---
	// The SQL below is verbatim from the spec (T018 AC #1). The database name
	// and LIMIT are interpolated from config, but the column list, predicates,
	// and ordering are fixed.
	dbName := cfg.ClickHouse.Database
	limit := cfg.Scheduler.EventsPerGeneration
	sql := fmt.Sprintf(
		"SELECT event_id, project_id, summary, user_text, assistant_text, captured_at"+
			" FROM %s.events"+
			" WHERE has_decision = true"+
			" AND captured_at > now() - INTERVAL 1 MONTH"+
			" AND is_internal = false"+
			" ORDER BY captured_at DESC"+
			" LIMIT %d",
		dbName, limit,
	)

	rows, err := chClient.QueryEvents(ctx, sql)
	if err != nil {
		return fmt.Errorf("generate: query events: %w", err)
	}

	if len(rows) == 0 {
		logger.Info("generate: no decision-bearing events in last month — skipping")
		return nil
	}

	// Find the most-recent captured_at for the cursor update.
	var maxCapturedAt time.Time
	var mostRecentEventID string
	for _, row := range rows {
		if ts, ok := row["captured_at"].(time.Time); ok {
			if ts.After(maxCapturedAt) {
				maxCapturedAt = ts
				if id, ok := row["event_id"].(string); ok {
					mostRecentEventID = id
				}
			}
		}
	}

	// --- 4. Build prompt ---
	prompt := questionGenPrompt()

	// Active-learning feedback channel: append the user's most recent skipped
	// questions as anti-examples (the user marked them low quality) and the
	// most recent answered ones as positive examples. This nudges future
	// generations toward the user's actual quality bar without requiring a
	// new UI surface — Skip on Telegram already produces the negative signal.
	const feedbackLimit = 8
	skipped, _ := db.GetRecentByStatus(ctx, "skipped", feedbackLimit)
	answered, _ := db.GetRecentByStatus(ctx, "answered", feedbackLimit)
	if len(skipped) > 0 || len(answered) > 0 {
		fbJSON, err := json.Marshal(map[string]any{
			"skipped_examples":  shapeFeedbackExamples(skipped),
			"answered_examples": shapeFeedbackExamples(answered),
		})
		if err == nil {
			prompt = prompt + "\n\n## User feedback on prior questions\n\n" +
				"Below is JSON containing the most recent questions the user has skipped " +
				"(low-quality / fabricated / off-topic — DO NOT generate more like these) " +
				"and answered (the user found these worth their time — match this quality bar):\n\n" +
				string(fbJSON)
		}
	}

	// Serialize events as a JSON array appended to the prompt.
	eventsJSON, err := json.Marshal(rows)
	if err != nil {
		return fmt.Errorf("generate: marshal events: %w", err)
	}
	prompt = prompt + "\n\n## Source events\n\n" + string(eventsJSON)

	// --- 5. Invoke claude -p ---
	//
	// NOTE on --max-turns: The original spec included --max-turns 1, but this
	// flag does not exist in `claude -p` version 2.1.119 (Claude Code). The
	// `-p` flag already makes claude print a single response and exit, so
	// --max-turns is redundant and has been dropped.
	//
	// NOTE on ANTHROPIC_CUSTOM_HEADERS: Verified (via a capture proxy) that
	// `claude -p` version 2.1.119 faithfully forwards
	// ANTHROPIC_CUSTOM_HEADERS=X-Marc-Internal: true in every POST to
	// /v1/messages. No HTTP fallback path is needed; AC #3 (fallback) is moot
	// for this CLI version.
	binary := cfg.Claude.Binary
	if binary == "" {
		binary = "claude"
	}

	// Build environment: inherit all host env vars, then override/add ours.
	// We do NOT force ANTHROPIC_BASE_URL — operators may point elsewhere via
	// their own env.  If it is already set in the process environment it will
	// be inherited unchanged.
	env := os.Environ()
	env = append(env, fmt.Sprintf("ANTHROPIC_CUSTOM_HEADERS=%s: true", cfg.Claude.InternalHeader))

	var stdout, stderr []byte
	var runErr error
	if opts.ClaudeRunner != nil {
		stdout, stderr, runErr = opts.ClaudeRunner(ctx, binary, prompt, env)
	} else {
		stdout, stderr, runErr = runClaude(ctx, binary, prompt, env)
	}

	// --- 6. Handle claude exit ---
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			// Non-zero exit from claude: log + zero-insert + nil (retry next hour).
			logger.Error("generate: claude -p exited with non-zero status",
				slog.String("stderr", string(stderr)),
				slog.Int("exit_code", exitErr.ExitCode()),
			)
			return nil
		}
		// Binary not found, context deadline, or other OS-level failure.
		return fmt.Errorf("generate: run claude: %w", runErr)
	}

	// --- 7. Parse claude output ---
	var envelope claudeOutputEnvelope
	if err := json.Unmarshal(stdout, &envelope); err != nil {
		logger.Error("generate: failed to parse claude JSON envelope",
			slog.String("error", err.Error()),
			slog.String("stdout_sample", truncate(string(stdout), 200)),
		)
		return nil // soft failure: retry next hour
	}
	if envelope.IsError {
		logger.Error("generate: claude returned is_error=true",
			slog.String("result", envelope.Result),
		)
		return nil // soft failure: retry next hour
	}

	var candidates []CandidateQuestion
	if err := json.Unmarshal([]byte(envelope.Result), &candidates); err != nil {
		logger.Error("generate: failed to parse candidates JSON from model output",
			slog.String("error", err.Error()),
			slog.String("result_sample", truncate(envelope.Result, 200)),
		)
		return nil // soft failure: retry next hour
	}

	// --- 8. Filter ---
	minDurability := cfg.Filtering.MinDurability
	maxObviousness := cfg.Filtering.MaxObviousness
	var survivors []CandidateQuestion
	for _, c := range candidates {
		if c.DurabilityScore >= minDurability && c.ObviousnessScore <= maxObviousness {
			survivors = append(survivors, c)
		}
	}

	logger.Info("generate: filtering complete",
		slog.Int("candidates", len(candidates)),
		slog.Int("survivors", len(survivors)),
		slog.Int("min_durability", minDurability),
		slog.Int("max_obviousness", maxObviousness),
	)

	// Collect all event IDs from the query result.
	var allEventIDs []string
	for _, row := range rows {
		if id, ok := row["event_id"].(string); ok {
			allEventIDs = append(allEventIDs, id)
		}
	}

	// --- 9. Insert survivors ---
	genAt := now().UTC()
	for _, c := range survivors {
		seedID := c.SeedEventID
		if seedID == "" {
			seedID = mostRecentEventID
		}

		retrievedIDs := c.RetrievedEventIDs
		if len(retrievedIDs) == 0 {
			retrievedIDs = allEventIDs
		}

		pq := sqlitedb.PendingQuestion{
			ProjectID:         "default", // project is not yet per-question; use default
			SeedEventID:       seedID,
			RetrievedEventIDs: retrievedIDs,
			Situation:         c.Situation,
			Question:          c.Question,
			OptionA:           c.OptionA,
			OptionB:           c.OptionB,
			PrincipleTested:   c.PrincipleTested,
			DurabilityScore:   c.DurabilityScore,
			ObviousnessScore:  c.ObviousnessScore,
			GeneratedAt:       genAt,
		}

		if _, err := db.InsertQuestion(ctx, pq); err != nil {
			logger.Error("generate: insert question failed",
				slog.String("error", err.Error()),
				slog.String("situation", c.Situation),
			)
			// Continue: insert as many as we can.
		}
	}

	// --- 10. Update cursor ---
	if !maxCapturedAt.IsZero() {
		if err := db.UpdateQuestionGenCursor(ctx, maxCapturedAt); err != nil {
			logger.Error("generate: update question_gen_cursor failed",
				slog.String("error", err.Error()),
			)
			// Non-fatal: cursor update failure is recoverable next hour.
		}
	}

	logger.Info("generate: cycle complete",
		slog.Int("inserted", len(survivors)),
	)
	return nil
}

// runClaude executes the real `claude -p --output-format json` subprocess,
// passing prompt on stdin and env as the process environment.
// It enforces a 600-second timeout via the context.
// shapeFeedbackExamples projects PendingQuestion rows down to just the fields
// that matter for prompt feedback — situation, question, options, principle —
// stripping internal IDs, scores, timestamps, and other tracking-only fields
// that would just inflate token cost and confuse the model.
func shapeFeedbackExamples(qs []sqlitedb.PendingQuestion) []map[string]any {
	out := make([]map[string]any, 0, len(qs))
	for _, q := range qs {
		out = append(out, map[string]any{
			"situation":        q.Situation,
			"question":         q.Question,
			"option_a":         q.OptionA,
			"option_b":         q.OptionB,
			"principle_tested": q.PrincipleTested,
		})
	}
	return out
}

func runClaude(ctx context.Context, binary, prompt string, env []string) (stdout, stderr []byte, err error) {
	// Apply a hard 600-second timeout on top of whatever the caller's ctx has.
	ctx, cancel := context.WithTimeout(ctx, 600*time.Second)
	defer cancel()

	// Args: -p triggers non-interactive (print) mode.
	// --output-format json returns a structured envelope rather than raw text.
	// --max-turns is NOT passed: it does not exist in claude 2.1.119 and -p is
	// already single-turn by design (prints response and exits).
	cmd := exec.CommandContext(ctx, binary, "-p", "--output-format", "json")
	cmd.Env = env
	cmd.Stdin = bytes.NewBufferString(prompt)

	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	err = cmd.Run()
	return outBuf.Bytes(), errBuf.Bytes(), err
}

// truncate returns s truncated to at most maxLen runes, with "..." appended
// when truncation occurs. Used for safe log output of potentially long strings.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}
