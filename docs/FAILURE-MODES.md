# marc — failure modes and how to diagnose them

When something feels wrong, **the first command to run is `marc doctor`**. It
inspects every part of the setup (config, systemd units, port, health
endpoint, capture file, MinIO) and prints one ✓/⚠/✗ line per check. Most of
the scenarios below are diagnosed in seconds by reading its output.

If `marc doctor` does not pinpoint the problem, this document has the
playbooks for the failure modes we have actually seen.

---

## 1. Claude Code shows API errors after enabling marc

**What you see**: After `marc install` and `export ANTHROPIC_BASE_URL=...`,
every Claude Code request fails with an API error — even though `marc proxy`
appears to be running.

### First step: `marc doctor`

```bash
marc doctor
```

Read the output. The most common diagnoses are:

| Doctor line that fails | What it means | Fix |
|---|---|---|
| `✗ config schema parses` | `~/.marc/config.toml` is empty / malformed | Re-run `marc configure`. Empty file? Run `marc configure --print-default > ~/.marc/config.toml`, fill in placeholder values, then `chmod 0600 ~/.marc/config.toml`. |
| `✗ marc-proxy.service state — inactive` | systemd unit isn't running | `sudo systemctl start marc-proxy && sudo systemctl status marc-proxy` |
| `✗ marc-proxy.service state — failed` | unit crashed | `journalctl -u marc-proxy -n 100` to see the crash; usually a config-load error |
| `✗ proxy port listening` | nothing accepting on the configured port | the daemon is running but bound to a different port — re-check `[proxy].listen_addr` in config |
| `✗ /_marc/health responds ok` with `status=failed` | proxy is running but every forward has failed | usually means upstream (Anthropic) is unreachable or the API key is invalid; see step 2 below |
| `⚠ ANTHROPIC_BASE_URL env var — not set` | Claude Code isn't going through marc | `export ANTHROPIC_BASE_URL=http://127.0.0.1:8082` and **restart Claude Code completely** (export only affects new processes) |
| `✗ MinIO authentication works` | shipper will fail to upload | re-run `marc configure`; check MinIO credentials |

### Second step: bypass marc temporarily to confirm Anthropic itself works

If `marc doctor` doesn't immediately reveal the problem, take marc out of the
path to isolate whether the issue is marc or Anthropic:

```bash
unset ANTHROPIC_BASE_URL
# Start a fresh Claude Code session and try again.
```

- **If Claude Code now works**: the problem is in marc. Continue to scenario 2 below.
- **If Claude Code still fails**: the problem is upstream — your API key, your
  network, or Anthropic itself. marc is not the cause.

Re-enable capture once the problem is fixed:

```bash
export ANTHROPIC_BASE_URL=http://127.0.0.1:8082
# Restart Claude Code.
```

---

## 2. `marc proxy` starts but doesn't forward

The systemd unit is `active` and the port is listening, but every request
through it returns an error.

This is the failure mode that originally motivated the observability work:
the proxy accepted connections but produced API errors for every request,
with no signal that anything was wrong.

### Step A: hit `/_marc/health`

```bash
curl -s http://127.0.0.1:8082/_marc/health | jq .
```

What you're looking for in the JSON:

- **`"status": "ok"`** with a recent `last_successful_forward_at` → the
  proxy IS forwarding successfully. Your problem is elsewhere (probably
  scenario 3 below).
- **`"status": "failed"`** → no forwards have succeeded since startup.
  `last_error_message` tells you why. Common values:
  - `invalid x-api-key` → the API key Claude Code sent is invalid (or
    your `--self-test` is using a stale key).
  - `Internal Server Error` → upstream Anthropic outage. Check
    [status.anthropic.com](https://status.anthropic.com/).
  - `dial tcp ... connection refused` → the proxy can't reach
    `api.anthropic.com`. Check DNS and outbound HTTPS.
- **`"status": "degraded"`** → some forwards succeed, some fail. The
  `last_error_message` shows the most recent failure.

### Step B: tail `journalctl` for ERROR-level lines

The proxy emits structured JSON logs. Each request gets a UUID; every line
about that request includes the same `request_id`. To find the failure for
the last request:

```bash
sudo journalctl -u marc-proxy -n 200 --output=json-pretty | jq -r 'select(.LEVEL=="ERROR") | .MESSAGE' | tail
```

Or, if you know the time it happened:

```bash
sudo journalctl -u marc-proxy --since="5 min ago" -p err
```

### Step C: run the self-test

```bash
marc proxy --self-test
```

This stands the proxy up on a random port, sends one real Anthropic request
through it (via `claude -p`, using whatever credential Claude Code is already
logged in with), and verifies every step:

```
✓ proxy started on 127.0.0.1:NNNNN
✓ proxy health endpoint responsive
✓ test request sent
✓ response status 200
✓ response body valid claude said: ok
✓ capture file received event (2 events)
✓ auth headers forwarded but not captured
```

If any step fails, the output names the step, gives a one-line reason, and
suggests a likely cause. The self-test runs the same code path as the
production proxy, so a passing self-test rules out "the binary is broken".

`--self-test` is also what `marc install` runs before declaring success — a
failing self-test makes `marc install` roll back the unit files.

**Auth note**: by default the self-test spawns `claude -p` with
`ANTHROPIC_BASE_URL` pointed at the ephemeral proxy. No separate API key is
needed; whatever auth Claude Code already has (OAuth or API key) flows
through. If `claude` is not installed, set `ANTHROPIC_API_KEY=sk-ant-...` in
the environment and the self-test will fall back to a direct request path.

---

## 3. Self-test passes but Claude Code still fails

The proxy works (you proved it with `--self-test`), but Claude Code still
gets errors. The problem is on the Claude Code side: it isn't actually
using the proxy.

### Step A: verify `ANTHROPIC_BASE_URL` is in Claude Code's environment

The variable must be set in the shell that **launches Claude Code**, not
just in your interactive shell.

```bash
# In a NEW terminal, before starting Claude Code:
echo "$ANTHROPIC_BASE_URL"
# Want: http://127.0.0.1:8082
```

If Claude Code is launched by a desktop launcher / dock / IDE, it inherits
the environment of whatever started **that** — which is usually the desktop
session, not your `~/.zshrc`. You may need to:

- Set `ANTHROPIC_BASE_URL` system-wide (e.g. in `~/.config/environment.d/`
  on Linux, or in a launchctl `setenv` plist on macOS), or
- Launch Claude Code from the terminal where the variable is set.

After changing the variable, **fully quit and restart Claude Code**. A
running process does not pick up new environment variables — even
"reload window" usually doesn't.

### Step B: verify the URL points at the right port

The default is `http://127.0.0.1:8082`. If you changed `[proxy].listen_addr`
in `~/.marc/config.toml`, `ANTHROPIC_BASE_URL` must match.

```bash
grep listen_addr ~/.marc/config.toml
# proxy listens here
echo $ANTHROPIC_BASE_URL
# Claude Code dials here — must match
```

### Step C: confirm Claude Code is actually hitting marc

`marc doctor` reports `last_successful_forward_at` from `/_marc/health`. If
you've sent a Claude Code request in the last minute and that timestamp is
older than 5 minutes, Claude Code is going somewhere else — probably
straight to `api.anthropic.com`.

```bash
# Send a query in Claude Code, then immediately:
curl -s http://127.0.0.1:8082/_marc/health | jq .last_successful_forward_at
```

If the timestamp didn't update, `ANTHROPIC_BASE_URL` is not effective in
Claude Code's process.

---

## Cheat sheet

| Symptom | First command |
|---|---|
| Anything looks wrong | `marc doctor` |
| Proxy seems up but requests fail | `curl http://127.0.0.1:8082/_marc/health` |
| Need to confirm the binary forwards correctly | `marc proxy --self-test` |
| Recent error details | `journalctl -u marc-proxy --since="5 min ago" -p err` |
| Take marc out of the path | `unset ANTHROPIC_BASE_URL`, restart Claude Code |
| Restore marc | `export ANTHROPIC_BASE_URL=http://127.0.0.1:8082`, restart Claude Code |
