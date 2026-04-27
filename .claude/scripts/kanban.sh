#!/bin/bash
# Terminal kanban board viewer for tasks.json
# Usage: bash .claude/scripts/kanban.sh [tasks.json path]

TASKS_FILE="${1:-tasks.json}"

if [ ! -f "$TASKS_FILE" ]; then
  echo "No tasks.json found. Run /pipeline to start."
  exit 0
fi

# Check for jq
if ! command -v jq &> /dev/null; then
  echo "Error: jq is required. Install with: brew install jq (macOS) or apt install jq (Linux)"
  exit 1
fi

PROJECT=$(jq -r '.project // "Untitled"' "$TASKS_FILE")
STATUS=$(jq -r '.status // "unknown"' "$TASKS_FILE")

echo ""
echo "╔══════════════════════════════════════════════════════════════════╗"
echo "║  $PROJECT"
echo "║  Status: $STATUS"
echo "╚══════════════════════════════════════════════════════════════════╝"
echo ""

# Collect tasks by status
TODO=$(jq -r '.phases[].tasks[] | select(.status == "todo") | "  \(.id): \(.title) [\(.tags | join(", "))]"' "$TASKS_FILE" 2>/dev/null)
IN_PROGRESS=$(jq -r '.phases[].tasks[] | select(.status == "in_progress") | "  \(.id): \(.title) [\(.tags | join(", "))]"' "$TASKS_FILE" 2>/dev/null)
REVIEW=$(jq -r '.phases[].tasks[] | select(.status == "review") | "  \(.id): \(.title) [\(.tags | join(", "))]"' "$TASKS_FILE" 2>/dev/null)
DONE=$(jq -r '.phases[].tasks[] | select(.status == "done") | "  \(.id): \(.title) [\(.tags | join(", "))]"' "$TASKS_FILE" 2>/dev/null)

# Count tasks
TOTAL=$(jq '[.phases[].tasks[]] | length' "$TASKS_FILE" 2>/dev/null)
DONE_COUNT=$(jq '[.phases[].tasks[] | select(.status == "done")] | length' "$TASKS_FILE" 2>/dev/null)

echo "┌──────────────────┬──────────────────┬──────────────────┬──────────────────┐"
echo "│ TODO             │ IN PROGRESS      │ REVIEW           │ DONE             │"
echo "├──────────────────┼──────────────────┼──────────────────┼──────────────────┤"

# Simple output (not perfectly aligned, but functional)
echo "│"
if [ -n "$TODO" ]; then
  echo "$TODO" | while IFS= read -r line; do echo "│ $line"; done
else
  echo "│   (empty)"
fi
echo "│"
echo "│ IN PROGRESS:"
if [ -n "$IN_PROGRESS" ]; then
  echo "$IN_PROGRESS" | while IFS= read -r line; do echo "│ $line"; done
else
  echo "│   (empty)"
fi
echo "│"
echo "│ REVIEW:"
if [ -n "$REVIEW" ]; then
  echo "$REVIEW" | while IFS= read -r line; do echo "│ $line"; done
else
  echo "│   (empty)"
fi
echo "│"
echo "│ DONE:"
if [ -n "$DONE" ]; then
  echo "$DONE" | while IFS= read -r line; do echo "│ $line"; done
else
  echo "│   (empty)"
fi
echo "│"
echo "└──────────────────────────────────────────────────────────────────────────┘"
echo ""
echo "Progress: $DONE_COUNT / $TOTAL tasks completed"
echo ""
