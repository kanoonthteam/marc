# marc

Capture every Claude Code conversation across all your machines, denoise it, and use it as the corpus for an active-learning loop: hourly question generation via `claude -p`, with retrieval over your full history. Questions are sent to Telegram every 30 minutes during workdays. Your answers accumulate as a personal corpus of engineering invariants.

**v1 scope**: capture → denoise → retrieve → ask → store answers.

See [CHANGELOG.md](CHANGELOG.md) for a full record of shipped changes.

---

## Architecture

```
Claude Code session (any machine)
   | ANTHROPIC_BASE_URL=http://localhost:8082
   v
marc proxy
   +- forwards to api.anthropic.com (streams response back)
   +- appends event -> ~/.marc/capture.jsonl
                                |
                                | marc ship polls every 30s
                                | when >= 5 MB:
                                |   mv -> capture.jsonl.shipping
                                |   PUT -> MinIO
                                |   verify ETag
                                |   rm local
                                v
              MinIO marc/raw/<machine>/<date>/<hour>/...jsonl   (3d TTL)
                                |
                                | marc-server process polls every 60s
                                v
                +-------------------------------------+
                | marc-server process:                |
                |  for each event (skip is_internal): |
                |    denoise via Ollama qwen3         |
                |    insert -> ClickHouse (raw +      |
                |      denoised in one row)           |
                |  move raw -> processed/raw-archive/ |
                +-------------------------------------+
                                |
                                | marc-server generate runs hourly
                                v
                +-------------------------------------+
                | marc-server generate:               |
                |  query ClickHouse for recent        |
                |    decision-bearing events          |
                |    across all projects              |
                |  build prompt with serialized       |
                |    events                           |
                |  invoke claude -p                   |
                |    (header X-Marc-Internal: true)   |
                |  parse + filter candidates          |
                |  insert -> SQLite pending_questions |
                +-------------------------------------+
                                |
                                | system cron fires
                                | */30 9-18 M-F (Asia/Bangkok)
                                v
                +-------------------------------------+
                | marc-bot-tick.py (Python):          |
                |  pick oldest ready question         |
                |  send -> Telegram with A/B/Other/Skip|
                | telegram-commands.py (Python):      |
                |   on inline tap or `a <id> ...` text:|
                |    write answer event ->            |
                |      /var/lib/marc/capture.jsonl    |
                |    UPDATE pending_questions         |
                +-------------------------------------+
                                |
                  (answer events ride the same
                   pipeline as Claude Code
                   captures: shipper -> MinIO ->
                   processor -> denoise ->
                   ClickHouse)
```

---

## Stack

| Component | Tech | Where | Pre-existing? |
|---|---|---|---|
| Client daemons and CLI | Go binary `marc` | Every machine using Claude Code | New |
| Server daemons and CLI | Go binary `marc-server` | Ubuntu only | New |
| Object storage | MinIO | artifacts.kanolab.io | Yes |
| OLAP (raw + denoised text) | ClickHouse | Ubuntu | New |
| Transactional state | SQLite (WAL mode) | Ubuntu file | New |
| LLM (denoise) | Ollama qwen3:8b | Ubuntu | Yes |
| LLM (question gen) | `claude -p` headless | Ubuntu | Yes (CLI) |
| Snapshots / backups | ZFS | Ubuntu data disk | New |

**Two binaries, one language, one repo.** The split is by deployment surface, not by component.

---

## Quick start: client

Download the `marc` binary for your platform from the [Releases page](https://github.com/caffeaun/marc/releases/latest).

```bash
# macOS arm64
curl -LO https://github.com/caffeaun/marc/releases/download/vX.Y.Z/marc-darwin-arm64
chmod +x marc-darwin-arm64
sudo mv marc-darwin-arm64 /usr/local/bin/marc

# Linux amd64
curl -LO https://github.com/caffeaun/marc/releases/download/vX.Y.Z/marc-linux-amd64
chmod +x marc-linux-amd64
sudo mv marc-linux-amd64 /usr/local/bin/marc
```

Configure and start the client services:

```bash
marc configure   # interactive: machine name, MinIO endpoint, access key, secret key
                 # validates MinIO connectivity before writing ~/.marc/config.toml (mode 0600)
marc install     # writes systemd (Linux) or launchd (macOS) units, starts marc-proxy + marc-ship
echo 'export ANTHROPIC_BASE_URL=http://localhost:8082' >> ~/.zshrc
# restart your shell or source ~/.zshrc, then restart Claude Code
```

Client config is written to `~/.marc/config.toml`:

```toml
machine_name = "macbook-kanoon"

[proxy]
listen_addr = "127.0.0.1:8082"
upstream_url = "https://api.anthropic.com"

[shipper]
rotate_size_mb = 5
ship_interval_seconds = 30

[minio]
endpoint = "https://artifacts.kanolab.io"
bucket = "marc"
access_key = "AKIA..."
secret_key = "..."
verify_tls = true
```

---

## Quick start: server bootstrap

Run these steps once, in order, on the Ubuntu host.

```bash
# 0. ZFS data pool (see ZFS Snapshots section for full details)
sudo apt install zfsutils-linux
sudo zpool create -o ashift=12 marc-data /dev/disk/by-id/<dedicated-disk>
sudo zfs set compression=lz4 marc-data
sudo zfs create marc-data/clickhouse
sudo zfs create marc-data/state
# Optional: cap ARC via /etc/modprobe.d/zfs.conf

# 1. ClickHouse
sudo apt install clickhouse-server clickhouse-client
# Configure data_path to /marc-data/clickhouse before starting service
sudo systemctl restart clickhouse-server

# 2. Ollama models
ollama pull qwen3:8b

# 3. Telegram bot
# Create bot via @BotFather, get token; @userinfobot for chat ID
# Note these values for step 5

# 4. Install both binaries on Ubuntu
curl -LO https://github.com/caffeaun/marc/releases/latest/download/marc-server-linux-amd64
chmod +x marc-server-linux-amd64
sudo mv marc-server-linux-amd64 /usr/local/bin/marc-server

curl -LO https://github.com/caffeaun/marc/releases/latest/download/marc-linux-amd64
chmod +x marc-linux-amd64
sudo mv marc-linux-amd64 /usr/local/bin/marc

# 5. Configure server
sudo mkdir -p /etc/marc
sudo marc-server configure   # interactive: MinIO, ClickHouse, Ollama, Telegram
                              # writes /etc/marc/server.toml (mode 0600)
                              # tests connectivity to all four services before exiting

# 6. Initialize ClickHouse + SQLite schemas
sudo marc-server init        # idempotent; safe to re-run

# 7. Install server services
sudo marc-server install     # systemd units: marc-process, marc-generate.service, marc-generate.timer

# 7a. Telegram delivery (Python, runs in the existing telegram-commands.service):
#     - Add a crontab entry to send the oldest ready question on schedule:
#       */30 9-18 * * 1-5  TZ=Asia/Bangkok ~/shared-venv/bin/python ~/kanoonth/scripts/marc-bot-tick.py
#     - The proxy at ~/kanoonth/scripts/telegram-commands.py handles inbound:
#       text commands `q [list|<id>]` and `a [<id>] A|B|S` / `a [<id>] O <reason>`,
#       plus inline-button taps with callback data prefix `marc:`.
#     - After amendments to telegram-commands.py: systemctl --user restart telegram-commands.service

# 7b. Optional: configure + install client on Ubuntu if you run Claude Code here
marc configure
marc install

# 8. Weekly ZFS snapshot cron
echo '0 3 * * 0  root  /usr/sbin/zfs snapshot -r marc-data@weekly-$(date +\%Y\%m\%d)' \
  | sudo tee /etc/cron.d/marc-snapshots

# 9. Verify
sudo systemctl status marc-process marc-generate.timer
sudo journalctl -u marc-process -f
# Telegram side runs under the user-mode telegram-commands.service:
systemctl --user status telegram-commands.service
tail -f ~/kanoonth/logs/marc-bot-tick.log
sudo zfs list -t snapshot   # empty until next Sunday 3am
```

Server config lives at `/etc/marc/server.toml`. The `clickhouse.addr` field controls the ClickHouse native TCP endpoint. The spec default is `127.0.0.1:9000`; choose whichever port your host has free (on hosts where MinIO already occupies 9000, a common alternative is 19000). Set it to match your installation:

```toml
[clickhouse]
addr = "127.0.0.1:9000"   # adjust if another service owns port 9000
database = "marc"
user = "default"
password = ""
```

---

## First-use timeline

After both halves are bootstrapped and Claude Code is restarted on a client machine:

- **~30-60 min**: first `capture.jsonl` rotation when 5 MB of conversation data accumulates
- **immediately after rotation**: first MinIO upload by `marc ship`
- **within ~2 min of upload**: first denoised events appear in ClickHouse (processor polls every 60s, Ollama denoise adds a few seconds per event)
- **next top of the hour**: first question batch generated by `marc-server generate` (cron `0 * * * *`)
- **next `*/30` mark within 09:00-18:00 Mon-Fri Asia/Bangkok**: first question sent to Telegram

Answer events written by the bot flow back through the same shipper → MinIO → processor → ClickHouse pipeline. They become available as context for the next question generation run. No special-casing.

---

## Security pre-deployment checklist

The following checklist is taken verbatim from the spec. Complete it before exposing the system to any production data.

- [ ] MinIO TLS enabled
- [ ] MinIO bucket policy is private (no public read)
- [ ] ClickHouse listens on `127.0.0.1` only
- [ ] SQLite file owned by service user, mode 0600
- [ ] Ubuntu disk encryption enabled (LUKS on the OS disk; ZFS native encryption optional on the data pool)
- [ ] Weekly ZFS snapshot cron is enabled and verified producing snapshots

**Note on ClickHouse network binding**: raw request and response bodies — which may contain customer data, secrets in prompts, or financial information — are written to ClickHouse. Restricting ClickHouse to `127.0.0.1` is a hard requirement, not a suggestion. Verify with `ss -tlnp | grep 9000` (or whichever port you configured) and confirm the listen address is `127.0.0.1`, not `0.0.0.0` or `::`.

**Note on auth headers**: `Authorization` and `x-api-key` headers are never written by the proxy to capture files. Raw request/response bodies are captured. All storage is on your own infrastructure (MinIO, Ubuntu).

---

## ZFS snapshots

### One-time setup

```bash
# Install ZFS userspace tools (kernel module is in stock Ubuntu 24.04)
sudo apt install zfsutils-linux

# Create pool on a dedicated disk (replace with your actual disk ID)
sudo zpool create -o ashift=12 marc-data /dev/disk/by-id/<disk-id>

# Create datasets — one per logical store for granular snapshots
sudo zfs create marc-data/clickhouse
sudo zfs create marc-data/state

# Enable LZ4 compression (fast, transparent, helps with ClickHouse text data)
sudo zfs set compression=lz4 marc-data

# Repoint mount points:
#   /var/lib/clickhouse -> marc-data/clickhouse
#   /var/lib/marc/state -> marc-data/state
# Stop services, move data, configure ClickHouse data_path, restart services.
```

**ARC tuning**: ZFS's adaptive replacement cache competes with ClickHouse's own page cache. Cap it to a sensible fraction of RAM. For a 32 GB host, 8 GB is a reasonable starting point. Add to `/etc/modprobe.d/zfs.conf`:

```
options zfs zfs_arc_max=8589934592
```

### Weekly snapshot cron

```cron
# /etc/cron.d/marc-snapshots
0 3 * * 0  root  /usr/sbin/zfs snapshot -r marc-data@weekly-$(date +\%Y\%m\%d)
```

Recursive (`-r`) takes atomic, consistent snapshots across all datasets simultaneously. Sunday 3 AM local time. No pruning — keep all snapshots.

### Recovery scenarios

| Scenario | Command |
|---|---|
| Corrupted SQLite | `zfs rollback marc-data/state@<snapshot>` |
| ClickHouse data corruption | Stop ClickHouse first, then `zfs rollback marc-data/clickhouse@<snapshot>` |
| Accidental table drop | Restore from any prior snapshot into a clone dataset; copy rows into live |
| Disk failure | No recovery in v1. Entire system is rebuilt from scratch and starts capturing fresh. Offsite disaster recovery is deferred to v2. |

### What ZFS snapshots do NOT cover

Snapshots live on the same physical disk as live data. A disk failure loses everything. This risk is accepted for v1. Offsite recovery via `zfs send | zfs receive` is a v2 item.

---

## project_id mapping workflow

After the first ingest cycle, query ClickHouse to discover what project identifiers the processor has derived from your sessions:

```sql
SELECT project_id, uniqExact(event_id) AS event_count
FROM marc.events
GROUP BY project_id
ORDER BY event_count DESC;
```

The `project_id` column is currently a short hash derived from the session system prompt. Map each hash to a human-readable name by editing the `[projects]` section of `/etc/marc/server.toml`:

```toml
[projects]
"abc123hash" = "sliplotto"
"def456hash" = "flowrent"
```

After editing, restart the processor so it picks up the new names:

```bash
sudo systemctl restart marc-process
```

Friendly names appear in Telegram question messages and in analytical queries against ClickHouse.

---

## Operations

### Log locations

| Service | Log |
|---|---|
| `marc-proxy` | `~/.marc/marc.log` (client) or `/var/log/marc.log` (server) |
| `marc-ship` | same log file as proxy |
| `marc-process` | `journalctl -u marc-process` |
| `marc-generate` | `journalctl -u marc-generate` |
| `telegram-commands` (Python proxy — handles inbound) | `journalctl --user -u telegram-commands.service` |
| `marc-bot-tick` (cron-driven outbound) | `~/kanoonth/logs/marc-bot-tick.log` |

### Service management

```bash
# Restart a specific service
sudo systemctl restart marc-process
sudo systemctl restart marc-generate.timer

# Telegram side runs under the user-mode proxy, restart it after editing marc handlers:
systemctl --user restart telegram-commands.service

# Check status
sudo systemctl status marc-process marc-generate.timer
systemctl --user status telegram-commands.service

# Follow logs
sudo journalctl -u marc-process -f
tail -f ~/kanoonth/logs/marc-bot-tick.log
```

### Prompt iteration

Prompts for denoise and question generation are embedded in the `marc-server` binary at build time from `prompts/denoise.md` and `prompts/question_gen.md`. To override without rebuilding, place edited files at:

```
/etc/marc/prompts/denoise.md
/etc/marc/prompts/question_gen.md
```

The runtime loader checks these paths first. Edit the file and restart the affected service — a 2-second iteration cycle:

```bash
sudo systemctl restart marc-generate.timer
```

### Schema drift detection

```bash
sudo marc-server init --check
# Exits 0 if schemas match expected; exits 1 with column-level diff on drift.
```

Run this after upgrading the binary before restarting services.

---

## Update procedure

To update either binary on any machine:

```bash
# Download the new binary (same URL pattern as initial install)
curl -LO https://github.com/caffeaun/marc/releases/latest/download/marc-linux-amd64
chmod +x marc-linux-amd64
sudo mv marc-linux-amd64 /usr/local/bin/marc

# Restart services
sudo systemctl restart marc-proxy marc-ship
# or on macOS: launchctl unload + load the plist
```

For the server binary:

```bash
curl -LO https://github.com/caffeaun/marc/releases/latest/download/marc-server-linux-amd64
chmod +x marc-server-linux-amd64
sudo mv marc-server-linux-amd64 /usr/local/bin/marc-server

sudo systemctl restart marc-process marc-generate.timer
```

No `git pull`. No rebuild. No migration scripts unless `marc-server init --check` reports schema drift.

---

## Emergency fallback

If `marc proxy` is down and the systemd/launchd `Restart=on-failure` auto-restart has not yet fired, Claude Code will be unable to reach the Anthropic API. To bypass the proxy immediately:

```bash
unset ANTHROPIC_BASE_URL
# Claude Code now contacts api.anthropic.com directly
# No captures are written during this interval
```

Re-enable capture once the proxy is back:

```bash
export ANTHROPIC_BASE_URL=http://localhost:8082
# or restart your shell to pick it up from ~/.zshrc
```

The systemd unit for `marc-proxy` includes `Restart=on-failure`, so most crash scenarios recover automatically within a few seconds without manual intervention.

---

## Subcommand reference

### `marc` (client binary, all platforms)

| Subcommand | Description |
|---|---|
| `marc proxy` | Long-running daemon. HTTPS proxy for the Anthropic API; appends captures to `capture.jsonl`. |
| `marc ship` | Long-running daemon. Polls `capture.jsonl` every 30s; uploads to MinIO when file reaches 5 MB. |
| `marc configure` | Interactive setup wizard. Writes `~/.marc/config.toml` (mode 0600). Validates MinIO connectivity. |
| `marc install` | Generates and starts systemd (Linux) or launchd (macOS) service units for proxy and ship. |
| `marc version` | Prints binary version and commit hash. |

`marc configure` flags: `--check`, `--reset`, `--print-default`, and non-interactive flags `--machine-name`, `--minio-endpoint`, `--minio-access-key`, `--minio-secret-key`, `--bucket`.

`marc install` flags: `--uninstall`, `--dry-run`.

### `marc-server` (server binary, Linux only)

| Subcommand | Description |
|---|---|
| `marc-server process` | Long-running daemon. Polls MinIO every 60s; denoises events via Ollama; writes to ClickHouse. |
| `marc-server generate` | Single-run (systemd timer, hourly). Queries ClickHouse, invokes `claude -p`, inserts questions into SQLite. |
| `marc-server configure` | Interactive setup wizard. Writes `/etc/marc/server.toml` (mode 0600). Validates MinIO, ClickHouse, Ollama, Telegram. |
| `marc-server install` | Generates and starts systemd units for `marc-process`, `marc-generate.service`, and `marc-generate.timer`. |
| `marc-server init` | Applies ClickHouse and SQLite DDL. Idempotent. `--check` flag detects schema drift. |
| `marc-server version` | Prints binary version and commit hash. |

Telegram is delivered separately by Python — the existing `~/kanoonth/scripts/telegram-commands.py` proxy (handles inbound: `q [list|<id>]` to list/show questions, `a [<id>] A|B|S` or `a [<id>] O <reason>` to answer, plus inline-button taps) and `~/kanoonth/scripts/marc-bot-tick.py` (cron-driven outbound: pick oldest ready question, send to Telegram).

`marc-server configure` flags: `--check`, `--print-default`.

`marc-server install` flags: `--uninstall`, `--dry-run`.

`marc-server init` flags: `--check`.
