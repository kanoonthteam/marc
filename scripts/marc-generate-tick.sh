#!/usr/bin/env bash
#
# marc-generate-tick.sh — hourly invocation of `marc-server generate`.
#
# Why a wrapper instead of an inline crontab entry:
#   - cron has no PATH; the wrapper resolves marc-server (prefer the system
#     install at /usr/local/bin, fall back to the dev tree).
#   - claude -p subprocess needs ANTHROPIC_BASE_URL=http://127.0.0.1:18082 so
#     the call goes through marc proxy and is captured with X-Marc-Internal=true.
#   - wraps logging into a single rotating file so the cron output isn't mailed.
#
# Schedule: install via crontab, e.g.
#     5 * * * *  ~/kanoonth/scripts/marc-generate-tick.sh
# (at :05 every hour, so the marc-bot-tick at :00 / :30 sees fresh rows)

set -u
set -o pipefail

# Cron uses /usr/bin:/bin only — restore a sensible PATH so the claude CLI
# (installed under ~/.local/bin) and any user-installed tooling resolve.
export PATH="${HOME}/.local/bin:/usr/local/bin:/usr/bin:/bin"

LOG_DIR="${HOME}/kanoonth/logs"
LOG_FILE="${LOG_DIR}/marc-generate.log"
mkdir -p "${LOG_DIR}"

# Resolve the marc-server binary.
MARC_SERVER=""
for candidate in \
    /usr/local/bin/marc-server \
    "${HOME}/projects/marc/dist/marc-server" ; do
    if [[ -x "${candidate}" ]]; then
        MARC_SERVER="${candidate}"
        break
    fi
done
if [[ -z "${MARC_SERVER}" ]]; then
    echo "[$(date -Iseconds)] ERROR marc-server binary not found in /usr/local/bin or ~/projects/marc/dist" >> "${LOG_FILE}"
    exit 1
fi

CONFIG="${MARC_SERVER_CONFIG:-${HOME}/.marc/server.toml}"
if [[ ! -f "${CONFIG}" ]]; then
    echo "[$(date -Iseconds)] ERROR config not found at ${CONFIG} — set MARC_SERVER_CONFIG or place it at ~/.marc/server.toml" >> "${LOG_FILE}"
    exit 1
fi

# Route claude -p through marc proxy so the call gets X-Marc-Internal-tagged
# (so it doesn't pollute the corpus with self-loop captures). The proxy default
# is 18082; override via ANTHROPIC_BASE_URL in the env if you've moved it.
export ANTHROPIC_BASE_URL="${ANTHROPIC_BASE_URL:-http://127.0.0.1:18082}"

# Fire one cycle. Output (structured JSON logs from slog) goes to the rotating file.
{
    echo "=== $(date -Iseconds) marc-server generate (binary=${MARC_SERVER}, config=${CONFIG}) ==="
    "${MARC_SERVER}" generate --config "${CONFIG}"
    rc=$?
    echo "=== exit=${rc} ==="
} >> "${LOG_FILE}" 2>&1
