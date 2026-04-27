#!/bin/bash
# Hook: Warn when editing agent definitions or skills
# Prevents accidental modification of pipeline configuration

WATCHED_PATHS=(
  ".claude/agents/"
  ".claude/skills/"
  ".claude/pipeline/"
)

CHANGED_FILES="$@"

for path in "${WATCHED_PATHS[@]}"; do
  if echo "$CHANGED_FILES" | grep -q "$path"; then
    echo "Warning: You are modifying pipeline configuration files in $path"
    echo "These files define agent behavior and pipeline flow."
    echo "Make sure this change is intentional."
    exit 0  # Warn but don't block
  fi
done

exit 0
