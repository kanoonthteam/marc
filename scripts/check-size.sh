#!/usr/bin/env bash
# usage: check-size.sh <binary-path> <limit-mb> <name>
set -euo pipefail

BIN="$1"
LIMIT_MB="$2"
NAME="$3"

if [ ! -f "$BIN" ]; then
  echo "FAIL: $NAME binary not found at $BIN" >&2
  exit 2
fi

# Use stat for portable byte-size: BSD stat (-f %z) on darwin, GNU (-c %s) on Linux.
if stat --version >/dev/null 2>&1; then
  SIZE_BYTES=$(stat -c %s "$BIN")
else
  SIZE_BYTES=$(stat -f %z "$BIN")
fi

LIMIT_BYTES=$(( LIMIT_MB * 1024 * 1024 ))
SIZE_MB=$(awk "BEGIN { printf \"%.2f\", $SIZE_BYTES / 1024 / 1024 }")

echo "$NAME: ${SIZE_MB} MB (limit ${LIMIT_MB} MB)"

if [ "$SIZE_BYTES" -gt "$LIMIT_BYTES" ]; then
  echo "FAIL: $NAME exceeds size limit (${SIZE_MB} MB > ${LIMIT_MB} MB). Likely cause: a client→server-only import was added (clickhouse / sqlitedb / ollama / telegram). Run 'go list -deps ./cmd/marc' to investigate." >&2
  exit 1
fi
