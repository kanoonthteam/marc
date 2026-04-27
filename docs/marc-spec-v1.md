# marc — System Specification v1

**Status**: Plan locked, ready to build
**Owner**: Kanoon
**Date**: 2026-04-26

> *marc* — French for the spent grounds left after brewing coffee or pressing grapes; also the name of the brandy distilled from those leftovers. The system takes the residue of your Claude Code sessions and distills it into something more concentrated.

## Purpose

Capture every Claude Code conversation across all your machines, denoise it, and use it as the corpus for an active-learning loop: hourly question generation via `claude -p`, with retrieval over your full history. Questions are sent to Telegram every 30 minutes during workdays. Your answers accumulate as a personal corpus of engineering invariants.

**v1 scope**: capture → denoise → retrieve → ask → store answers.
**Out of scope for v1**: feeding distilled practices back into Claude Code via MCP, semantic vector retrieval, offsite disaster recovery.

## Stack at a glance

| Component | Tech | Where | Pre-existing? |
|---|---|---|---|
| Client daemons & CLI | Go binary `marc` | Every machine using Claude Code | New |
| Server daemons & CLI | Go binary `marc-server` | Ubuntu only | New |
| Object storage | MinIO | artifacts.kanolab.io | ✓ |
| OLAP (raw + denoised text) | ClickHouse | Ubuntu | New |
| Transactional state | SQLite (WAL mode) | Ubuntu file | New |
| LLM (denoise) | Ollama qwen3:8b | Ubuntu | ✓ |
| LLM (question gen) | `claude -p` headless | Ubuntu | ✓ CLI |
| Snapshots / backups | ZFS | Ubuntu data disk | New |

**Two binaries, one language, one repo.** The split is by deployment surface, not by component:

`marc` (client, ~10 MB, all platforms) — only what a client machine needs:

```
marc proxy        # HTTPS proxy for Anthropic API
marc ship         # rotate capture.jsonl, upload to MinIO
marc configure    # interactive setup
marc install      # generate systemd/launchd units, start services
marc version
```

`marc-server` (server, ~30 MB, Linux only) — heavyweight server-side daemons:

```
marc-server process    # poll MinIO, denoise via Ollama, write to ClickHouse
marc-server generate   # hourly question generation via claude -p
marc-server bot        # Telegram bot + scheduler
marc-server configure  # interactive server setup
marc-server install    # install all server systemd units
marc-server init       # initialize ClickHouse + SQLite schemas
marc-server version
```

**Why two binaries**:
- Client machines download a small binary with only proxy/ship code — no unused ClickHouse driver, no Telegram lib, no SQLite
- Server is Linux-only, so no macOS cross-compilation pain for the heavyweight binary
- Independent release cadence — fix a proxy bug without re-releasing the server, and vice versa
- Operator can't accidentally run server commands on a MacBook (the subcommands literally don't exist on `marc`)

**Ubuntu installs both**: when Claude Code runs on Ubuntu directly, Ubuntu needs `marc` (for its own proxy + ship) AND `marc-server` (for the processor/bot/generator). The two binaries share only `/var/lib/marc/capture.jsonl` (file path) and the MinIO bucket — they don't import each other.

## End-to-end flow

```
Claude Code session (any machine)
   │ ANTHROPIC_BASE_URL=http://localhost:8082
   ↓
marc proxy
   ├─ forwards to api.anthropic.com (streams response back)
   └─ appends event → ~/.marc/capture.jsonl
                                │
                                │ marc ship polls every 30s
                                │ when ≥ 5 MB:
                                │   mv → capture.jsonl.shipping
                                │   PUT → MinIO
                                │   verify ETag
                                │   rm local
                                ↓
              MinIO marc/raw/<machine>/<date>/<hour>/...jsonl   (3d TTL)
                                │
                                │ marc-server process polls every 60s
                                ↓
                ┌─────────────────────────────────────┐
                │ marc-server process:                │
                │  for each event (skip is_internal): │
                │    denoise via Ollama qwen3        │
                │    insert → ClickHouse (raw +      │
                │      denoised in one row)          │
                │  move raw → processed/raw-archive/ │
                └─────────────────────────────────────┘
                                │
                                │ marc-server generate runs hourly (0 * * * *)
                                ↓
                ┌─────────────────────────────────────┐
                │ marc-server generate:               │
                │  query ClickHouse for recent       │
                │    decision-bearing events         │
                │    across all projects             │
                │  build prompt with serialized      │
                │    events                          │
                │  invoke claude -p                  │
                │    (header X-Marc-Internal: true)  │
                │  parse + filter candidates         │
                │  insert → SQLite pending_questions │
                └─────────────────────────────────────┘
                                │
                                │ marc-server bot scheduler fires
                                │ */30 9-18 M-F (Asia/Bangkok)
                                ↓
                ┌─────────────────────────────────────┐
                │ marc-server bot:                    │
                │  pick oldest ready question        │
                │  send → Telegram with A/B/Other/Skip│
                │  on response:                       │
                │    write answer event →            │
                │      /var/lib/marc/capture.jsonl ──┐│
                │    UPDATE pending_questions       │ │
                └───────────────────────────────────┼─┘
                                                    │
                  (answer events ride the same      │
                   pipeline as Claude Code          │
                   captures: shipper → MinIO →      │
                   processor → denoise →            │
                   ClickHouse)                      │
                                                    │
                  ┌─────────────────────────────────┘
                  ↓
              (back to shipper at top of diagram)
```

## Per-machine components (every machine)

The same `marc` binary runs everywhere. Two subcommands run as long-running daemons on every machine that uses Claude Code:

### marc proxy

**Endpoint**: Listens on `http://localhost:8082`. Wired via `ANTHROPIC_BASE_URL=http://localhost:8082` in shell profile.

**Forwards everything**: All paths under `/v1/*` proxied to `https://api.anthropic.com`. Headers untouched (including `Authorization`, `x-api-key`).

**Streaming**: SSE responses streamed back to Claude Code in real-time via `http.Response.Body` as an `io.Reader`, copied to client connection while teeing into a per-request `bytes.Buffer`. Goroutine per request, chunks forwarded immediately.

**Capture timing**: On `message_stop` SSE event (or non-streaming response complete), construct the event JSON and append one line to `~/.marc/capture.jsonl`.

**Auth handling**: `Authorization` and `x-api-key` headers are forwarded but never logged. Constant `var strippedHeaders = []string{"authorization", "x-api-key", "cookie", ...}` lists what gets redacted in logs.

**Internal-call detection**: `marc-server generate` (server side) sets header `X-Marc-Internal: true` on its `claude -p` invocations. Proxy captures with `is_internal: true`. Processor skips internal events at denoise.

**Failure isolation**:
- Capture write fails → log + drop event, request flow unaffected
- Anthropic returns error → forward unchanged, capture event with error fields
- Proxy crash → systemd/launchd restarts; user can `unset ANTHROPIC_BASE_URL` for emergency direct-API fallback

### marc ship

**Loop** (every 30 seconds):

1. **Crash recovery**: if `capture.jsonl.shipping` exists from previous crash, upload it now
2. Stat `capture.jsonl`; if size < 5 MB, sleep
3. Atomic rename: `os.Rename("capture.jsonl", "capture.jsonl.shipping")`
4. Proxy detects inode change on next write → opens fresh `capture.jsonl` lazily
5. Compute MD5 of `capture.jsonl.shipping`
6. PUT to `s3://marc/raw/<machine>/<YYYY>/<MM>/<DD>/<HH>/<machine>-<unix_ts>-<uuid>.jsonl` (writer key)
7. Verify HTTP 200 + ETag matches MD5
8. On verified success: `os.Remove("capture.jsonl.shipping")`
9. On failure: leave file in place, retry next loop

**Invariant**: At any moment, exactly one `capture.jsonl` exists, plus optionally one `capture.jsonl.shipping`. Never more.

**Inode detection in proxy**: Each write checks `os.Stat("capture.jsonl").Sys().(*syscall.Stat_t).Ino`; if the cached inode differs, close old fd, open new file, update cache. Standard log-rotation pattern (`copytruncate=no` semantics).

### Operator subcommands

These are user-facing, run interactively, not daemonized. The client binary (`marc`) and server binary (`marc-server`) each have their own subcommand surface.

#### `marc configure` (client)

Writes `~/.marc/config.toml` (mode 0600). Interactive prompts when no flags given; non-interactive when flags provided. Validates the result by hitting MinIO with a test PUT/DELETE.

```bash
$ marc configure                    # full interactive wizard
$ marc configure --check            # validate existing config without changes
$ marc configure --reset            # wipe and start over
$ marc configure --print-default    # dump template TOML to stdout
$ marc configure \                  # non-interactive
    --machine-name macbook-kanoon \
    --minio-endpoint https://artifacts.kanolab.io \
    --minio-access-key AKIA... \
    --minio-secret-key "$MARC_SECRET" \
    --bucket marc
```

Validation steps run after writing:
- DNS resolves MinIO endpoint
- TLS certificate verifies (or skipped if `verify_tls = false`)
- Credentials authenticate
- Bucket exists and is writable (test PUT + DELETE on a `_marc-config-test/` key)

#### `marc install` (client)

Generates platform-appropriate service unit files for `marc proxy` + `marc ship`, loads them with the service manager, and starts them. Auto-detects Linux (systemd) vs macOS (launchd). Idempotent.

```bash
$ marc install              # install client services
$ marc install --uninstall  # stop and remove services
$ marc install --dry-run    # print what would be done, change nothing
```

#### `marc-server configure` (server)

Writes `/etc/marc/server.toml` (mode 0600). Same interactive pattern as `marc configure` but covers MinIO + ClickHouse + Ollama + Telegram.

```bash
$ sudo marc-server configure        # full interactive wizard
$ sudo marc-server configure --check
$ sudo marc-server configure --print-default
```

Validates connectivity to MinIO, ClickHouse, Ollama, and Telegram bot before exiting.

#### `marc-server install` (server)

Generates systemd units for `marc-server process`, `marc-server bot`, and the `marc-server generate` timer. Loads, enables, starts. Linux only.

```bash
$ sudo marc-server install              # install all server services
$ sudo marc-server install --uninstall
$ sudo marc-server install --dry-run
```

#### `marc-server init` (server)

Initializes ClickHouse and SQLite schemas. Idempotent — safe to re-run.

```bash
$ sudo marc-server init             # creates ClickHouse 'marc' DB + tables, SQLite state.db with all tables
$ sudo marc-server init --check     # verify schemas match expected, exit non-zero if drift
```

### Client config file format

`~/.marc/config.toml`:

```toml
machine_name = "macbook-kanoon"

[paths]
capture_file = "~/.marc/capture.jsonl"
log_file = "~/.marc/marc.log"

[proxy]
listen_addr = "127.0.0.1:8082"
upstream_url = "https://api.anthropic.com"
stripped_headers = ["authorization", "x-api-key", "cookie"]

[shipper]
rotate_size_mb = 5
ship_interval_seconds = 30

[minio]
endpoint = "https://artifacts.kanolab.io"
bucket = "marc"
access_key = "AKIA..."
secret_key = "MIGHTY..."
verify_tls = true
```

`marc configure` enforces mode 0600 on this file. Same plaintext-with-permissions pattern as `aws-cli`'s `~/.aws/credentials`.

## Server config file format

The Ubuntu server uses a richer config (path: `/etc/marc/server.toml`):

```toml
machine_name = "ubuntu-server"

[paths]
capture_file = "/var/lib/marc/capture.jsonl"   # for marc proxy/ship if Ubuntu also runs Claude Code
log_file = "/var/log/marc.log"

[minio]
endpoint = "https://artifacts.kanolab.io"
bucket = "marc"
access_key = "AKIA..."
secret_key = "MIGHTY..."
verify_tls = true
staging_dir = "/var/lib/marc/staging"

[clickhouse]
addr = "127.0.0.1:9000"
database = "marc"
user = "default"
password = ""

[sqlite]
path = "/var/lib/marc/state/state.db"

[ollama]
endpoint = "http://127.0.0.1:11434"
denoise_model = "qwen3:8b"

[claude]
binary = "claude"                              # path or 'claude' if on PATH
internal_header = "X-Marc-Internal"

[scheduler]
question_gen_cron = "0 * * * *"
telegram_send_cron = "*/30 9-18 * * 1-5"
timezone = "Asia/Bangkok"
events_per_generation = 30                     # how many recent events to feed claude per run

[telegram]
bot_token = "..."
chat_id = 123456789

[filtering]
min_durability = 7
max_obviousness = 7

[projects]
# Map directory hashes to friendly names. Edit after first ingest.
# "abc123hash" = "sliplotto"
# "def456hash" = "flowrent"
```

## Server components (Ubuntu)

All written in Go, all subcommands of the same `marc` binary. Each runs as its own systemd unit so they can be stopped/started independently.

### marc-server process

**Loop** (every 60 seconds):

1. List `s3://marc/raw/` for objects with key > `processor_cursors.last_object_key` per machine
2. For each new object, download to `/var/lib/marc/staging/`
3. Parse JSONL lines into events
4. For each event:
   - Skip if `is_internal == true`
   - Denoise via Ollama qwen3:8b → `(user_text, assistant_text, summary, has_decision, skip_reason)`
   - Insert into ClickHouse `events` table (raw + denoised columns in one row)
5. Move source raw object: `marc/raw/...` → `marc/processed/raw-archive/...`
6. Update SQLite `processor_cursors.last_object_key` per machine

**Concurrency**: Single-threaded for the v1 implementation. Throughput bounded by Ollama inference (likely 5-30 events/sec). Add a worker pool only if observed throughput is insufficient.

**Idempotency**: All writes keyed on `event_id`. ClickHouse uses `ReplacingMergeTree` engine which deduplicates rows with identical sort key during background merges. The cursor advances only after the entire batch's writes succeed.

**Failure handling**:
- Single event denoise fails → log + skip, continue batch
- Ollama down → halt batch, retry next cycle
- ClickHouse write fails → halt + log, retry next cycle (re-processes from same MinIO file)
- MinIO unreachable → halt poll, retry next cycle

### marc-server generate

Runs hourly via systemd timer (`OnCalendar=hourly`). Each invocation:

1. Query ClickHouse for a sample of recent decision-bearing events:
   ```sql
   SELECT event_id, project_id, summary, user_text, assistant_text, captured_at
   FROM marc.events
   WHERE has_decision = true
     AND captured_at > now() - INTERVAL 1 MONTH
     AND is_internal = false
   ORDER BY captured_at DESC
   LIMIT 30
   ```
   The 30-event window across all projects gives Claude varied material. No semantic retrieval — Claude does the synthesis.

2. Build the prompt: serialize the events with their metadata, append the generation rules from `prompts/question_gen.md`.

3. Invoke `claude -p` as subprocess:
   ```go
   cmd := exec.Command(cfg.Claude.Binary, "-p",
       "--append-system-prompt-file", "/etc/marc/prompts/question_gen.md",
       "--output-format", "json",
       "--max-turns", "1")
   cmd.Env = append(os.Environ(), 
       "ANTHROPIC_CUSTOM_HEADERS=X-Marc-Internal: true")
   cmd.Stdin = strings.NewReader(prompt)
   out, err := cmd.Output()
   ```

4. Parse JSON output: a list of candidate questions, each with `situation`, `question`, `option_a`, `option_b`, `principle_tested`, `durability_score`, `obviousness_score`.

5. Filter: `durability_score >= 7` and `obviousness_score <= 7`.

6. Insert surviving candidates into SQLite `pending_questions` with `status='ready'`.

7. Update `question_gen_cursor.last_event_ts` to the latest event seen.

**Target throughput**: ~1-3 surviving questions per hour. With 24 hours × 1-3 = 24-72 questions/day generated, vs 18 sends/day during work hours, queue stays comfortably positive.

### marc-server bot

Single long-running daemon. Two responsibilities under one process: a Telegram long-poll loop and an internal scheduler.

**Scheduler** (cron `*/30 9-18 * * 1-5` Asia/Bangkok). On fire:
1. `SELECT ... FROM pending_questions WHERE status='ready' ORDER BY generated_at LIMIT 1`
2. Format with HTML, send to your chat with inline keyboard `[A] [B] [Other] [Skip]`
3. `UPDATE pending_questions SET status='sent', sent_at=now(), telegram_message_id=?`

**Bot handlers**:
- **Inline button tap** or **text reply to question message**:
  1. Construct an "answer event" in the same JSONL format as proxy captures (machine = `"telegram"`, see schema below)
  2. Append the event to `/var/lib/marc/capture.jsonl` (Ubuntu) — same file `marc proxy` writes to
  3. `UPDATE pending_questions SET status='answered', answered_at=now()` in SQLite
  4. Edit Telegram message to show choice locked
- The shipper picks up the answer event with the next rotation. Processor denoises it like any other event. It ends up in ClickHouse `events` alongside Claude Code captures, and becomes available to the next hourly question generation run.
- `/stats`: queue depth + count of answered questions (from SQLite)
- `/next`: trigger immediate send (testing)

**Why answers go through the same pipeline**: an answer is a conversation with yourself, just as a Claude Code session is a conversation with Claude. Both are "decisions you made about your work." Same format, same denoise, same retrieval. Future question generation runs see your past Telegram answers as natural context — no special-casing.

**Library choice**: `gopkg.in/telebot.v4` — mature, idiomatic Go, supports inline keyboards and reply-handling cleanly.

**Scheduler library**: `github.com/go-co-op/gocron/v2` for cron expression handling.

## Data schemas

### Local capture JSONL (one event per line)

Two event variants share this format. Same writer (proxy or bot), same shipper, same processor.

**Variant A — Claude Code API capture** (written by `marc proxy`):

```json
{
  "event_id": "uuid4",
  "machine": "macbook",
  "captured_at": "2026-04-26T10:23:45.123Z",
  "source": "anthropic_api",
  "request_id": "req_xxx",
  "method": "POST",
  "path": "/v1/messages",
  "is_internal": false,
  "request": {
    "model": "claude-sonnet-4-7-20260301",
    "system": "...",
    "messages": [...],
    "tools": [...],
    "max_tokens": 8192,
    "stream": true
  },
  "response": {
    "status": 200,
    "stop_reason": "end_turn",
    "content": [...],
    "usage": {"input_tokens": 1234, "output_tokens": 567, "cache_read_input_tokens": 0, "cache_creation_input_tokens": 0},
    "model": "claude-sonnet-4-7-20260301",
    "id": "msg_..."
  },
  "stream_meta": {
    "was_streamed": true,
    "first_chunk_ms": 234,
    "total_ms": 4521,
    "chunk_count": 142
  },
  "error": null,
  "session_hint": null,
  "project_hint": null
}
```

**Variant B — Telegram answer** (written by `marc-server bot`):

```json
{
  "event_id": "uuid4",
  "machine": "telegram",
  "captured_at": "2026-04-26T10:53:12.456Z",
  "source": "telegram_answer",
  "is_internal": false,
  "request": {
    "model": "marc-question",
    "messages": [
      {"role": "system", "content": "principle_tested: validate at boundary vs validate at use-site"},
      {"role": "user", "content": "Situation: <situation>\nQuestion: <question>\nA) <option_a>\nB) <option_b>"}
    ]
  },
  "response": {
    "status": 200,
    "stop_reason": "user_choice",
    "content": [
      {"type": "text", "text": "B"},
      {"type": "text", "text": "<freeform reply if any>"}
    ],
    "id": "marc-question-117"
  },
  "session_hint": "marc-question-117",
  "project_hint": "sliplotto"
}
```

When the processor denoises and writes both variants, they flow through the same code path. Past Claude Code reasoning AND past Telegram answers both end up queryable in ClickHouse `events`.

### ClickHouse

One unified table holds both raw capture data (JSON blobs from the proxy) and denoised columns added by the processor. Single row per event. Denoised columns are populated atomically with the raw insert.

```sql
CREATE DATABASE IF NOT EXISTS marc;

CREATE TABLE marc.events (
    -- Identity & metadata
    event_id        UUID,
    machine         String,
    project_id      String,
    captured_at     DateTime64(3),
    source          String,            -- 'anthropic_api' | 'telegram_answer'
    is_internal     Bool,
    session_hint    String,

    -- Raw capture from proxy (JSON blobs as strings)
    raw_request_body    String,
    raw_response_body   String,
    response_status     UInt16,
    response_stop_reason String,
    request_model       String,

    -- Token & timing metrics
    input_tokens         UInt32,
    output_tokens        UInt32,
    cache_read_tokens    UInt32,
    cache_write_tokens   UInt32,
    first_chunk_ms       UInt32,
    total_ms             UInt32,
    error_type           String,
    error_message        String,

    -- Denoised by processor
    user_text       String,
    assistant_text  String,
    summary         String,
    has_decision    Bool,
    skip_reason     String,
    denoised_at     DateTime64(3),
    denoise_model   String
)
ENGINE = ReplacingMergeTree
PARTITION BY toYYYYMM(captured_at)
ORDER BY (project_id, captured_at, event_id)
SETTINGS index_granularity = 8192;
```

Query patterns:
- Standard analytics on denoised text — fast columnar scans on `user_text`, `assistant_text`, `has_decision`, `project_id`
- Raw introspection — `JSONExtractString(raw_request_body, 'system')` etc. when you need to look at exactly what was sent
- Token/cost analytics — sum/group by month from the metrics columns
- Question generator's seed query — recent decision-bearing events across all projects

There is no separate raw table. ClickHouse holds everything queryable. MinIO `processed/raw-archive/` keeps the original JSONL files as cheap cold backup in case ClickHouse needs to be rebuilt.

### SQLite (`/var/lib/marc/state/state.db`)

```sql
PRAGMA journal_mode = WAL;
PRAGMA busy_timeout = 5000;
PRAGMA foreign_keys = ON;
PRAGMA synchronous = NORMAL;

CREATE TABLE projects (
    project_id TEXT PRIMARY KEY,
    friendly_name TEXT NOT NULL,
    description TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE processor_cursors (
    machine TEXT PRIMARY KEY,
    last_object_key TEXT NOT NULL,
    last_processed_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE question_gen_cursor (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    last_event_ts TEXT NOT NULL DEFAULT '1970-01-01T00:00:00Z',
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT OR IGNORE INTO question_gen_cursor (id) VALUES (1);

CREATE TABLE pending_questions (
    question_id INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id TEXT NOT NULL REFERENCES projects(project_id),
    seed_event_id TEXT,           -- UUID of the primary inspirational event
    retrieved_event_ids TEXT,     -- JSON array of UUIDs (events shown to Claude as context)
    situation TEXT NOT NULL,
    question TEXT NOT NULL,
    option_a TEXT NOT NULL,
    option_b TEXT NOT NULL,
    principle_tested TEXT NOT NULL,
    durability_score INTEGER NOT NULL,
    obviousness_score INTEGER NOT NULL,
    status TEXT NOT NULL DEFAULT 'ready' CHECK (status IN ('ready','sent','answered','skipped','discarded')),
    generated_at TEXT NOT NULL DEFAULT (datetime('now')),
    sent_at TEXT,
    answered_at TEXT,
    telegram_message_id INTEGER,
    answer_event_id TEXT          -- UUID of the JSONL event written when user answered; null until answered
);
CREATE INDEX idx_pending_status ON pending_questions(status, generated_at);
```

Note: there is no separate `answers` table. The substantive content of an answer (choice, freeform text, full context) lives as a JSONL event in the same pipeline as Claude Code captures. The `pending_questions` table only tracks queue state; `answer_event_id` links to the event in MinIO/ClickHouse if you ever need to fetch the full answer payload.

## MinIO setup

**Bucket**: `marc`

**Layout**:
```
marc/
├── raw/<machine>/<YYYY>/<MM>/<DD>/<HH>/<machine>-<unix_ts>-<uuid>.jsonl
└── processed/
    └── raw-archive/<machine>/<YYYY>/<MM>/<DD>/<HH>/...jsonl
```

Two prefixes only. `raw/` is the ingestion buffer; the processor moves objects to `processed/raw-archive/` after they're successfully written to ClickHouse. The archive is cold backup — used only if ClickHouse needs to be rebuilt from scratch.

**Lifecycle policies**:
- `raw/*` → expire after 3 days (safety net only; processor polls every 60s and moves objects within minutes — anything sitting longer indicates a real bug)
- `processed/raw-archive/*` → expire after 7 days (cold backup of input JSONL, useful only for "ClickHouse needs rebuild" scenarios; ClickHouse is the canonical record)

**Auth**: One key (writer key) used by all `marc` daemons. Per the earlier decision: `marc-server process` runs on the same trusted host, so single-key simplicity wins over scoping.

Permissions on the key:
- `s3:PutObject` on `marc/raw/*` and `marc/processed/*`
- `s3:GetObject` on `marc/raw/*` and `marc/processed/*`
- `s3:DeleteObject` on `marc/raw/*` (for move-to-archive)
- `s3:ListBucket` on `marc`

## Schedules

| Job | Frequency | Purpose |
|---|---|---|
| `marc ship` rotate-and-ship | every 30s | Ship 5MB+ files to MinIO |
| `marc-server process` poll | every 60s | Pull new raw objects from MinIO, denoise, write to ClickHouse |
| `marc-server generate` (systemd timer) | `0 * * * *` | Generate next batch of candidate questions via claude -p |
| `marc-server bot` send job | `*/30 9-18 * * 1-5` Asia/Bangkok | Send next ready question |
| ZFS snapshot | weekly Sunday 3am | Atomic snapshot of marc-data pool |

## Failure mode matrix

| Component down | What happens | Recovery |
|---|---|---|
| MinIO unreachable | `*.shipping` files accumulate on each machine. Proxy unaffected. | Auto-recovers when MinIO returns. |
| ClickHouse down | Processor halts. New events buffer in MinIO `raw/`. | Restart ClickHouse; processor catches up. |
| SQLite lock contention | Bot/scheduler/generator retry with WAL `busy_timeout=5000`. | Auto-resolves. |
| Ollama down | Processor halts at denoise step. | Restart Ollama; processor retries. |
| Anthropic API down | Claude Code shows errors. Proxy logs error events. | Wait for Anthropic. |
| Proxy crash | Claude Code can't reach Anthropic. **User must restart proxy or `unset ANTHROPIC_BASE_URL`.** | Systemd/launchd auto-restart. |
| Telegram down | Sends fail; questions sit in `ready` status. | Backlog drains naturally when Telegram returns. |
| Question gen fails | No new questions for that hour. | Retries next hour. Queue may run dry — increase `events_per_generation` in config if persistent. |
| ZFS snapshot fails | Cron job logs error, no snapshot taken. Live data unaffected. | Investigate; manually run snapshot. |

## Self-loop prevention

The risk: `marc-server generate` runs `claude -p`, that traffic goes through the proxy, gets captured, gets denoised, becomes a seed for the next question, infinite recursion.

**Solution chain:**
1. `marc-server generate` invokes `claude -p` as a subprocess with `ANTHROPIC_CUSTOM_HEADERS="X-Marc-Internal: true"` set in the subprocess environment.
2. Proxy sees `X-Marc-Internal: true` on incoming request → writes capture event with `is_internal: true`.
3. Shipper ships normally (we still archive these for debugging).
4. Processor sees `is_internal: true` → skips denoise step, no ClickHouse insert.
5. Generator never sees its own outputs.

If `claude -p` does not honor `ANTHROPIC_CUSTOM_HEADERS` (verify during build), fallback is to call the Anthropic API directly from `marc-server generate` using `httpx`-equivalent Go HTTP client. The request still goes through the proxy via `ANTHROPIC_BASE_URL`, so capture still happens and the header is set explicitly.

## Code organization

Single Go module, **two binaries**, shared internal packages:

```
github.com/caffeaun/marc
├── cmd/
│   ├── marc/main.go               # client binary entry (cobra root for marc)
│   └── marc-server/main.go        # server binary entry (cobra root for marc-server)
├── internal/
│   │ # used by marc (client)
│   ├── proxy/                     # marc proxy daemon
│   ├── ship/                      # marc ship daemon
│   │
│   │ # used by marc-server
│   ├── process/                   # marc-server process daemon
│   ├── generate/                  # marc-server generate (timer-triggered)
│   ├── bot/                       # marc-server bot daemon
│   ├── initdb/                    # marc-server init (schema setup)
│   ├── clickhouse/                # ClickHouse client wrapper
│   ├── sqlitedb/                  # SQLite helpers + migrations
│   ├── ollama/                    # Ollama client wrapper
│   ├── telegram/                  # Telegram bot handlers
│   │
│   │ # shared between both binaries
│   ├── configure/                 # interactive setup (different prompts per binary)
│   ├── install/                   # systemd/launchd unit generator
│   ├── config/                    # config loading + types
│   ├── jsonl/                     # JSONL event types and serialization
│   └── minioclient/               # S3 client wrapper
├── prompts/                       # embedded via go:embed in marc-server
│   ├── denoise.md
│   └── question_gen.md
├── systemd/                       # service unit templates
├── launchd/                       # plist templates
├── README.md
└── .goreleaser.yaml               # builds both binaries
```

**Build size matters here**: the client binary should NOT import ClickHouse, SQLite, Ollama, or Telegram packages, because those each pull in a few MB of code. Keep `internal/proxy/`, `internal/ship/`, and the shared packages strictly free of server-specific imports. Verify with `go build -ldflags="-s -w" ./cmd/marc && du -h marc` — should be under 15 MB.

**Recommended Go libraries**:
- HTTP/SSE (both): stdlib `net/http`
- S3/MinIO (both): `github.com/minio/minio-go/v7`
- ClickHouse (server only): `github.com/ClickHouse/clickhouse-go/v2`
- SQLite (server only): `github.com/mattn/go-sqlite3` (CGO) — fall back to `modernc.org/sqlite` (pure Go) if CGO complexity bites
- CLI (both): `github.com/spf13/cobra`
- Config (both): `github.com/BurntSushi/toml`
- Logging (both): `log/slog` (stdlib, structured)
- Telegram bot (server only): `gopkg.in/telebot.v4`
- Cron (server only): `github.com/go-co-op/gocron/v2`
- File watching for inode detection (client only): `golang.org/x/sys/unix` for stat

**Prompt iteration pattern**: prompts live in `prompts/` as plain text, embedded via `go:embed` into `marc-server` at build time but readable at runtime from `/etc/marc/prompts/*.md` if present (for live tuning). Editing a prompt + `systemctl restart marc-generate.timer` is a 2-second iteration cycle.

## Backup discipline (ZFS snapshots)

Short MinIO retention (3-day raw, 7-day raw-archive) means **ClickHouse is the only long-term canonical record**. If ClickHouse loses data older than 7 days, it's gone.

**Approach: ZFS snapshots on the Ubuntu data disk.**

A dedicated disk (or partition) is formatted as ZFS. All marc state lives on it. Weekly snapshots are taken automatically; copy-on-write means each snapshot costs only the delta since the last one. Realistic disk overhead at marc's scale: ~10 GB after a year of weekly snapshots, ~20 GB after two years. No automatic pruning needed.

### One-time ZFS setup

```bash
# Install ZFS userspace tools (kernel module is in stock Ubuntu 24.04)
sudo apt install zfsutils-linux

# Create pool on a dedicated disk (replace /dev/sdb with your actual disk)
sudo zpool create -o ashift=12 marc-data /dev/disk/by-id/<disk-id>

# Create datasets — one per logical store, so snapshots are granular
sudo zfs create marc-data/clickhouse
sudo zfs create marc-data/state

# Set compression (LZ4 is fast, transparent, helps with ClickHouse text data)
sudo zfs set compression=lz4 marc-data

# Stop services, move existing data if any, repoint mount points:
#   /var/lib/clickhouse → marc-data/clickhouse
#   /var/lib/marc/state → marc-data/state
```

**ARC tuning**: ZFS's adaptive replacement cache competes with ClickHouse's own page cache for memory. Cap ARC to a sensible fraction of system RAM. For a 32 GB box dedicated to marc, `zfs_arc_max=8589934592` (8 GB) is a reasonable starting point. Add to `/etc/modprobe.d/zfs.conf`:

```
options zfs zfs_arc_max=8589934592
```

### Weekly snapshot cron

```cron
# /etc/cron.d/marc-snapshots
0 3 * * 0  root  /usr/sbin/zfs snapshot -r marc-data@weekly-$(date +\%Y\%m\%d)
```

Recursive (`-r`) takes atomic, consistent snapshots across all datasets at once. Sunday 3 AM local time. No pruning — keep them all.

### What this gives us

- **Atomic point-in-time snapshots** of ClickHouse + SQLite together
- **Near-zero disk cost per snapshot** (only delta since previous)
- **Restore is trivial**: `zfs rollback marc-data/clickhouse@weekly-20260512` (or clone the snapshot to a separate dataset for inspection)
- **No special tooling**: stock ZFS

### What this does NOT give us

- **Disaster recovery if the disk dies**: snapshots live on the same physical disk as live data. A disk failure loses everything. Mitigations deferred to v2.
- **Off-machine recovery**: if Ubuntu itself is unrecoverable, snapshots are inaccessible.

### Recovery scenarios

- Corrupted SQLite → `zfs rollback marc-data/state@<snapshot>`
- ClickHouse data corruption → `zfs rollback marc-data/clickhouse@<snapshot>` (stop ClickHouse first)
- Accidental table drop → restore from any prior snapshot, recover the table, copy rows into live
- Disk failure → no recovery in v1; entire system is rebuilt from scratch and starts capturing fresh

## Bootstrap order

### Server side (one-time, on Ubuntu)

```bash
# 0. ZFS data pool (see Backup discipline section for full details)
sudo apt install zfsutils-linux
sudo zpool create -o ashift=12 marc-data /dev/disk/by-id/<dedicated-disk>
sudo zfs set compression=lz4 marc-data
sudo zfs create marc-data/clickhouse
sudo zfs create marc-data/state
# Optional: cap ARC via /etc/modprobe.d/zfs.conf

# 1. ClickHouse (apt or Docker)
sudo apt install clickhouse-server clickhouse-client
# Configure data_path to /marc-data/clickhouse before starting service
sudo systemctl restart clickhouse-server

# 2. Ollama models
ollama pull qwen3:8b

# 3. Telegram: create bot via @BotFather, get token; @userinfobot for chat ID
# (note these for step 5 below)

# 4. Install both binaries on Ubuntu
# marc-server for the heavyweight daemons:
curl -LO https://github.com/caffeaun/marc/releases/latest/download/marc-server-linux-amd64
chmod +x marc-server-linux-amd64
sudo mv marc-server-linux-amd64 /usr/local/bin/marc-server

# marc for capturing Ubuntu's own Claude Code use (optional but recommended):
curl -LO https://github.com/caffeaun/marc/releases/latest/download/marc-linux-amd64
chmod +x marc-linux-amd64
sudo mv marc-linux-amd64 /usr/local/bin/marc

# 5. Configure server side
sudo mkdir -p /etc/marc
sudo marc-server configure   # interactive: MinIO, ClickHouse, Ollama, Telegram
# Writes /etc/marc/server.toml (mode 0600)
# Tests connectivity to MinIO, ClickHouse, Ollama before exiting

# 6. Initialize ClickHouse + SQLite schemas
sudo marc-server init                  # runs DDL against both stores

# 7. Install server services (process + bot + generate timer)
sudo marc-server install               # systemd units for marc-process, marc-bot, marc-generate.timer

# 7b. (Optional) Configure + install client side on Ubuntu, if you also use Claude Code here
marc configure
marc install                           # systemd units for marc-proxy, marc-ship

# 8. Snapshot cron
echo '0 3 * * 0  root  /usr/sbin/zfs snapshot -r marc-data@weekly-$(date +\%Y\%m\%d)' \
  | sudo tee /etc/cron.d/marc-snapshots

# 9. Verify
sudo systemctl status marc-process marc-bot marc-generate.timer
sudo journalctl -u marc-process -u marc-bot -f
sudo zfs list -t snapshot   # empty until next Sunday 3am
```

To update the server later: download the new `marc-server` binary, replace `/usr/local/bin/marc-server`, restart services. No `git pull`, no rebuild.

### Client side bootstrap (per machine)

The `marc` binary is published as a public GitHub Release. Download, chmod, configure, install.

```bash
# Pick the right binary for your platform from
# https://github.com/caffeaun/marc/releases/latest

# macOS arm64 example:
curl -LO https://github.com/caffeaun/marc/releases/latest/download/marc-darwin-arm64
chmod +x marc-darwin-arm64
sudo mv marc-darwin-arm64 /usr/local/bin/marc

# Linux amd64 example:
curl -LO https://github.com/caffeaun/marc/releases/latest/download/marc-linux-amd64
chmod +x marc-linux-amd64
sudo mv marc-linux-amd64 /usr/local/bin/marc

# Configure and install services
marc configure              # interactive: machine name, MinIO endpoint, keys
                            # (validates MinIO connection before writing config)
marc install                # writes systemd or launchd units, starts marc-proxy + marc-ship
echo 'export ANTHROPIC_BASE_URL=http://localhost:8082' >> ~/.zshrc

# Restart Claude Code
```

**To update later**: download the new binary, replace it at `/usr/local/bin/marc`, restart services. No self-update mechanism needed.

### First-use timeline

After both halves are bootstrapped and Claude Code is restarted on a client:
- First `capture.jsonl` rotation when ≥ 5 MB accumulates (~30-60 min of normal use)
- First MinIO upload immediately after rotation
- First denoised events in ClickHouse within ~2 min of upload
- First question generated at the next top of the hour
- First Telegram send at the next `*/30` minute mark within 9-18 Mon-Fri Bangkok time

### Release pipeline (binaries)

GitHub Actions builds and publishes both binaries on every git tag matching `v*`, using GoReleaser. The client binary (`marc`) is built for four targets; the server binary (`marc-server`) is Linux-only.

Artifacts per release:
- `marc-linux-amd64`
- `marc-linux-arm64`
- `marc-darwin-amd64`
- `marc-darwin-arm64`
- `marc-server-linux-amd64`
- `marc-server-linux-arm64`
- `checksums.txt`

`.goreleaser.yaml`:
```yaml
project_name: marc
builds:
  # Client binary: all platforms, no CGO (proxy + ship are pure Go)
  - id: marc
    main: ./cmd/marc
    binary: marc
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    env: [CGO_ENABLED=0]
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}

  # Server binary: Linux only, CGO for SQLite
  - id: marc-server
    main: ./cmd/marc-server
    binary: marc-server
    goos: [linux]
    goarch: [amd64, arm64]
    env: [CGO_ENABLED=1]
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}

archives:
  - id: marc
    builds: [marc]
    format: binary
    name_template: 'marc-{{ .Os }}-{{ .Arch }}'
  - id: marc-server
    builds: [marc-server]
    format: binary
    name_template: 'marc-server-{{ .Os }}-{{ .Arch }}'

checksum:
  name_template: 'checksums.txt'

release:
  github:
    owner: caffeaun
    name: marc
```

Note on CGO: only the server binary needs CGO (for `mattn/go-sqlite3`). The client binary stays pure Go and cross-compiles trivially. If CGO complexity on the server becomes painful, swap to `modernc.org/sqlite` (pure-Go SQLite, slightly slower but no CGO required).

To publish a new version:
```bash
git tag v0.1.0
git push --tags
# GitHub Actions builds + publishes to the Releases page automatically
```

## Project mapping

After first ingest, query ClickHouse for discovered project hints (from session metadata):

```sql
SELECT project_id, count(*) FROM marc.events GROUP BY project_id ORDER BY count(*) DESC;
```

Edit `[projects]` section of `/etc/marc/server.toml` to map them to friendly names. Friendly names appear in Telegram messages and analytical queries.

## Security posture

- Auth headers (`Authorization`, `x-api-key`) are **never written** by the proxy
- Raw request/response bodies **are** captured untouched (per the earlier decision); these may contain customer data, secrets-in-prompts, financial info, etc.
- All storage is on your infrastructure (MinIO, Ubuntu)
- Pre-deployment checklist:
  - [ ] MinIO TLS enabled
  - [ ] MinIO bucket policy is private (no public read)
  - [ ] ClickHouse listens on `127.0.0.1` only
  - [ ] SQLite file owned by service user, mode 0600
  - [ ] Ubuntu disk encryption enabled (LUKS on the OS disk; ZFS native encryption optional on the data pool)
  - [ ] Weekly ZFS snapshot cron is enabled and verified producing snapshots

## Open items deferred to v2

- MCP server exposing distilled practices to Claude Code
- Convergence detector (cluster questions, surface contradictions, auto-promote settled principles)
- Voice-note answers via Whisper
- Web dashboard for browsing answers
- Semantic / vector retrieval (LanceDB, Qdrant, sqlite-vec, or similar) if recency-sample retrieval proves insufficient
- Offsite disaster recovery via `zfs send | zfs receive` to external target (Tailscale-attached machine, USB drive, B2/Storage Box) — currently v1 accepts the disk-failure-resets-everything risk

## Open items resolved at build time

- Whether `claude -p` honors `ANTHROPIC_CUSTOM_HEADERS` env var (preferred) or we call the Anthropic API directly from `marc-server generate`
- Whether `mattn/go-sqlite3` (CGO) or `modernc.org/sqlite` (pure-Go) — start with mattn, swap if CGO complexity bites
- Heuristic for extracting `project_id` from request payloads (needs real captures to design)
- Initial denoise prompt and question generation prompt (will iterate after first data lands)
