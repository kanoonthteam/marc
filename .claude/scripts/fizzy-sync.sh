#!/bin/bash
# Fizzy sync — push tasks.json to a Fizzy kanban board
#
# Usage:
#   bash .claude/scripts/fizzy-sync.sh           # Push all tasks to Fizzy
#
# Like kanban.sh but pushes tasks to your Fizzy board instead of
# displaying them in the terminal. Creates new cards for unsynced
# tasks and updates existing ones.
#
# Standard columns: Todo, In Progress, Review
# The board is enforced to have exactly these 3 columns — extra
# columns are deleted, missing ones are created.
# Done tasks close the card instead of moving to a column.
#
# Card description includes: task metadata, description,
# acceptance criteria, and dependencies.
# Card tags are synced via the taggings endpoint (epic + task tags).
#
# Configuration: .claude/pipeline/config.json → fizzy section
# Environment:   FIZZY_TOKEN (overrides config token)

set -e

CONFIG=".claude/pipeline/config.json"
TASKS_FILE="tasks.json"

# ─── Standard Columns ──────────────────────────────────────────────────────

COL_TODO="Todo"
COL_IN_PROGRESS="In Progress"
COL_REVIEW="Review"

# ─── Prerequisites ───────────────────────────────────────────────────────────

if [ ! -f "$CONFIG" ]; then
  echo "Error: $CONFIG not found. Run from project root."
  exit 1
fi

if ! command -v jq &> /dev/null; then
  echo "Error: jq is required. Install with: brew install jq (macOS) or apt install jq (Linux)"
  exit 1
fi

if [ ! -f "$TASKS_FILE" ]; then
  echo "No $TASKS_FILE found. Run /pipeline first."
  exit 1
fi

# ─── Read Fizzy Config ───────────────────────────────────────────────────────

FIZZY_URL=$(jq -r '.fizzy.url // empty' "$CONFIG")
ACCOUNT_SLUG=$(jq -r '.fizzy.accountSlug // empty' "$CONFIG")
FIZZY_SYNC=$(jq -r '.fizzy.sync // false' "$CONFIG")
BOARD_ID=$(jq -r '.fizzy.boardId // empty' "$CONFIG")

# Token: env var takes precedence
CONFIG_TOKEN=$(jq -r '.fizzy.token // empty' "$CONFIG")
if [ -n "${FIZZY_TOKEN:-}" ]; then
  TOKEN="$FIZZY_TOKEN"
elif [ "$CONFIG_TOKEN" != '${FIZZY_TOKEN}' ] && [ -n "$CONFIG_TOKEN" ]; then
  TOKEN="$CONFIG_TOKEN"
else
  TOKEN=""
fi

# ─── Validate ────────────────────────────────────────────────────────────────

ERRORS=0

if [ "$FIZZY_SYNC" != "true" ]; then
  echo "Fizzy sync is disabled."
  echo "Enable it by setting fizzy.sync to true in $CONFIG"
  echo "  or configure Fizzy during setup: ./setup.sh /path/to/project"
  exit 0
fi

if [ -z "$FIZZY_URL" ]; then
  echo "Error: fizzy.url not set in $CONFIG"
  ERRORS=$((ERRORS + 1))
fi
if [ -z "$ACCOUNT_SLUG" ]; then
  echo "Error: fizzy.accountSlug not set in $CONFIG"
  ERRORS=$((ERRORS + 1))
fi
if [ -z "$TOKEN" ]; then
  echo "Error: No Fizzy token. Set FIZZY_TOKEN env var."
  ERRORS=$((ERRORS + 1))
fi
if [ -z "$BOARD_ID" ]; then
  echo "Error: fizzy.boardId not set in $CONFIG"
  ERRORS=$((ERRORS + 1))
fi

if [ "$ERRORS" -gt 0 ]; then
  exit 1
fi

# ─── API Helper ──────────────────────────────────────────────────────────────

HTTP_STATUS=""
RESPONSE_FILE=$(mktemp)

fizzy_api() {
  local method="$1"
  local endpoint="$2"
  local data="${3:-}"
  local url="${FIZZY_URL}/${ACCOUNT_SLUG}${endpoint}"

  local curl_args=(
    -s -w "%{http_code}"
    -H "Authorization: Bearer $TOKEN"
    -H "Content-Type: application/json"
    -H "Accept: application/json"
    -o "$RESPONSE_FILE"
  )

  if [ "$method" != "GET" ]; then
    curl_args+=(-X "$method")
    if [ -n "$data" ]; then
      curl_args+=(-d "$data")
    fi
  fi

  HTTP_STATUS=$(curl "${curl_args[@]}" "$url")
}

# Read last API response body (use after fizzy_api call)
fizzy_response() {
  cat "$RESPONSE_FILE"
}

trap 'rm -f "$RESPONSE_FILE"' EXIT

# ─── Resolve Columns ────────────────────────────────────────────────────────

COLUMN_CACHE=""

resolve_columns() {
  fizzy_api GET "/boards/$BOARD_ID/columns"

  if [ "$HTTP_STATUS" != "200" ]; then
    echo "Error: Failed to fetch columns for board $BOARD_ID (HTTP $HTTP_STATUS)"
    exit 1
  fi

  COLUMN_CACHE=$(fizzy_response | jq -r '.[]? | "\(.name):\(.id)"')
}

get_column_id() {
  echo "$COLUMN_CACHE" | grep "^${1}:" | head -1 | cut -d: -f2
}

# Map task status to a column name ("__close__" = close the card)
status_to_column_name() {
  case "$1" in
    todo)        echo "$COL_TODO" ;;
    in_progress) echo "$COL_IN_PROGRESS" ;;
    review)      echo "$COL_REVIEW" ;;
    done)        echo "__close__" ;;
    *)           echo "$COL_TODO" ;;
  esac
}

# ─── Enforce Standard Columns ─────────────────────────────────────────────

create_column() {
  local name="$1"
  local color="${2:-#6B7280}"
  local data
  data=$(jq -n --arg n "$name" --arg c "$color" '{"column": {"name": $n, "color": $c}}')
  fizzy_api POST "/boards/$BOARD_ID/columns" "$data"
  if [ "$HTTP_STATUS" = "201" ] || [ "$HTTP_STATUS" = "200" ]; then
    echo "  + Created column: $name"
    return 0
  else
    echo "  Error creating column '$name' (HTTP $HTTP_STATUS)"
    return 1
  fi
}

delete_column() {
  local col_id="$1"
  local col_name="$2"
  fizzy_api DELETE "/boards/$BOARD_ID/columns/$col_id"
  if [ "$HTTP_STATUS" = "204" ] || [ "$HTTP_STATUS" = "200" ]; then
    echo "  - Removed column: $col_name"
    return 0
  else
    echo "  Error removing column '$col_name' (HTTP $HTTP_STATUS)"
    return 1
  fi
}

enforce_columns() {
  local changed=0

  # Create missing standard columns
  for col_name in "$COL_TODO" "$COL_IN_PROGRESS" "$COL_REVIEW"; do
    local col_id
    col_id=$(get_column_id "$col_name")
    if [ -z "$col_id" ]; then
      create_column "$col_name" || true
      changed=$((changed + 1))
    fi
  done

  # Delete non-standard columns
  while IFS=: read -r name id; do
    [ -z "$name" ] && continue
    case "$name" in
      "$COL_TODO"|"$COL_IN_PROGRESS"|"$COL_REVIEW") ;;
      *) delete_column "$id" "$name" || true; changed=$((changed + 1)) ;;
    esac
  done <<< "$COLUMN_CACHE"

  # Re-fetch columns if anything changed
  if [ "$changed" -gt 0 ]; then
    resolve_columns
  fi
}

# ─── Card Close/Reopen ──────────────────────────────────────────────────────

close_card() {
  local card_id="$1"
  fizzy_api POST "/cards/$card_id/closure"
  # 204 = closed, 200 = already closed
  if [ "$HTTP_STATUS" = "204" ] || [ "$HTTP_STATUS" = "200" ]; then
    return 0
  else
    return 1
  fi
}

reopen_card() {
  local card_id="$1"
  fizzy_api DELETE "/cards/$card_id/closure"
  # 204 = reopened, 200/404 = already open
  return 0
}

# ─── Tag Sync ───────────────────────────────────────────────────────────────

# Fizzy tags are managed via a toggle endpoint (POST /cards/:id/taggings).
# We fetch the card's current tags, then toggle to reach the desired state.
sync_tags() {
  local card_id="$1"
  shift
  local desired=("$@")

  # Get current tags on the card
  fizzy_api GET "/cards/$card_id"
  local current_tags=()
  if [ "$HTTP_STATUS" = "200" ]; then
    while IFS= read -r _tag_line; do
      [ -n "$_tag_line" ] && current_tags+=("$_tag_line")
    done <<< "$(fizzy_response | jq -r '.tags[]? // empty')"
  fi

  # Add missing tags (toggle on)
  for tag in "${desired[@]}"; do
    local found=false
    for ct in "${current_tags[@]}"; do
      if [ "$ct" = "$tag" ]; then
        found=true
        break
      fi
    done
    if [ "$found" = false ]; then
      fizzy_api POST "/cards/$card_id/taggings" "{\"tag_title\": \"$tag\"}"
    fi
  done

  # Remove extra tags (toggle off)
  for ct in "${current_tags[@]}"; do
    local found=false
    for tag in "${desired[@]}"; do
      if [ "$tag" = "$ct" ]; then
        found=true
        break
      fi
    done
    if [ "$found" = false ]; then
      fizzy_api POST "/cards/$card_id/taggings" "{\"tag_title\": \"$ct\"}"
    fi
  done
}

# ─── Push Tasks ──────────────────────────────────────────────────────────────

resolve_columns
enforce_columns

# Get first column ID as fallback for creating cards that will be immediately closed
FIRST_COLUMN_ID=$(echo "$COLUMN_CACHE" | head -1 | cut -d: -f2)

PROJECT=$(jq -r '.project // "Untitled"' "$TASKS_FILE")
TASK_COUNT=$(jq '[.phases[].tasks[]] | length' "$TASKS_FILE")

echo ""
echo "Fizzy sync: $PROJECT"
echo "════════════════════════════════════"
echo "Pushing $TASK_COUNT tasks to $FIZZY_URL ..."
echo ""

CREATED=0
UPDATED=0
CLOSED=0
SKIPPED=0

PHASES=$(jq -r '.phases | length' "$TASKS_FILE")

for ((p=0; p<PHASES; p++)); do
  PHASE_NAME=$(jq -r ".phases[$p].name // \"Phase $((p+1))\"" "$TASKS_FILE")
  TASKS=$(jq -r ".phases[$p].tasks | length" "$TASKS_FILE")

  for ((t=0; t<TASKS; t++)); do
    TASK_ID=$(jq -r ".phases[$p].tasks[$t].id" "$TASKS_FILE")
    TASK_TITLE=$(jq -r ".phases[$p].tasks[$t].title" "$TASKS_FILE")
    TASK_STATUS=$(jq -r ".phases[$p].tasks[$t].status" "$TASKS_FILE")
    TASK_TAGS=$(jq -r ".phases[$p].tasks[$t].tags // [] | join(\", \")" "$TASKS_FILE")
    TASK_DESC=$(jq -r ".phases[$p].tasks[$t].description // empty" "$TASKS_FILE")
    TASK_AC=$(jq -r ".phases[$p].tasks[$t].acceptance_criteria // [] | map(\"- \" + .) | join(\"\n\")" "$TASKS_FILE")
    TASK_DEPS=$(jq -r ".phases[$p].tasks[$t].dependencies // [] | join(\", \")" "$TASKS_FILE")
    TASK_EPIC=$(jq -r ".phases[$p].tasks[$t].epic // empty" "$TASKS_FILE")
    FIZZY_CARD=$(jq -r ".phases[$p].tasks[$t].fizzyCard // empty" "$TASKS_FILE")

    COL_NAME=$(status_to_column_name "$TASK_STATUS")
    SHOULD_CLOSE=false

    if [ "$COL_NAME" = "__close__" ]; then
      SHOULD_CLOSE=true
      COLUMN_ID="$FIRST_COLUMN_ID"
    else
      COLUMN_ID=$(get_column_id "$COL_NAME")
    fi

    if [ -z "$COLUMN_ID" ]; then
      echo "  SKIP  $TASK_ID: $TASK_TITLE (no column for '$TASK_STATUS')"
      SKIPPED=$((SKIPPED + 1))
      continue
    fi

    # Build card description with all task details
    DESC="**Task:** $TASK_ID
**Phase:** $PHASE_NAME"
    if [ -n "$TASK_TAGS" ]; then
      DESC="$DESC
**Tags:** $TASK_TAGS"
    fi
    if [ -n "$TASK_DEPS" ]; then
      DESC="$DESC
**Depends on:** $TASK_DEPS"
    fi
    if [ -n "$TASK_DESC" ]; then
      DESC="$DESC

$TASK_DESC"
    fi
    if [ -n "$TASK_AC" ]; then
      DESC="$DESC

**Acceptance Criteria:**
$TASK_AC"
    fi

    # Build desired tags list: epic (if set) + task tags
    DESIRED_TAGS=()
    if [ -n "$TASK_EPIC" ]; then
      DESIRED_TAGS+=("$TASK_EPIC")
    fi
    if [ -n "$TASK_TAGS" ]; then
      while IFS= read -r tag; do
        tag=$(echo "$tag" | sed 's/^ *//;s/ *$//')
        [ -n "$tag" ] && DESIRED_TAGS+=("$tag")
      done <<< "$(echo "$TASK_TAGS" | tr ',' '\n')"
    fi

    if [ -n "$FIZZY_CARD" ]; then
      # Update existing card
      if [ "$SHOULD_CLOSE" = true ]; then
        # Update content, then close
        UPDATE_DATA=$(jq -n \
          --arg title "$TASK_TITLE" \
          --arg desc "$DESC" \
          '{"card": {"title": $title, "description": $desc}}')

        fizzy_api PATCH "/cards/$FIZZY_CARD" "$UPDATE_DATA"
        sync_tags "$FIZZY_CARD" "${DESIRED_TAGS[@]}"

        if close_card "$FIZZY_CARD"; then
          echo "  CLOSE   #$FIZZY_CARD  $TASK_TITLE"
          CLOSED=$((CLOSED + 1))
        else
          echo "  ERROR   #$FIZZY_CARD  $TASK_TITLE  (close failed, HTTP $HTTP_STATUS)"
          SKIPPED=$((SKIPPED + 1))
        fi
      else
        # Update content + column, reopen if previously closed
        UPDATE_DATA=$(jq -n \
          --arg title "$TASK_TITLE" \
          --arg desc "$DESC" \
          --arg col "$COLUMN_ID" \
          '{"card": {"title": $title, "description": $desc, "column_id": $col}}')

        fizzy_api PATCH "/cards/$FIZZY_CARD" "$UPDATE_DATA"

        if [ "$HTTP_STATUS" = "200" ]; then
          reopen_card "$FIZZY_CARD"
          sync_tags "$FIZZY_CARD" "${DESIRED_TAGS[@]}"
          echo "  UPDATE  #$FIZZY_CARD  $TASK_TITLE  [$TASK_STATUS]"
          UPDATED=$((UPDATED + 1))
        else
          echo "  ERROR   #$FIZZY_CARD  $TASK_TITLE  (HTTP $HTTP_STATUS)"
          SKIPPED=$((SKIPPED + 1))
        fi
      fi
    else
      # Create new card (POST /boards/:board_id/cards)
      CREATE_DATA=$(jq -n \
        --arg title "$TASK_TITLE" \
        --arg desc "$DESC" \
        --arg col "$COLUMN_ID" \
        '{"card": {"title": $title, "description": $desc, "column_id": $col}}')

      fizzy_api POST "/boards/$BOARD_ID/cards" "$CREATE_DATA"

      if [ "$HTTP_STATUS" = "201" ] || [ "$HTTP_STATUS" = "200" ]; then
        CARD_NUM=$(fizzy_response | jq -r '.number')

        # Save card number back to tasks.json
        TMP=$(mktemp)
        jq ".phases[$p].tasks[$t].fizzyCard = $CARD_NUM" "$TASKS_FILE" > "$TMP"
        mv "$TMP" "$TASKS_FILE"

        sync_tags "$CARD_NUM" "${DESIRED_TAGS[@]}"

        if [ "$SHOULD_CLOSE" = true ]; then
          close_card "$CARD_NUM"
          echo "  CREATE  #$CARD_NUM  $TASK_TITLE  [closed]"
        else
          echo "  CREATE  #$CARD_NUM  $TASK_TITLE  [$TASK_STATUS]"
        fi
        CREATED=$((CREATED + 1))
      else
        echo "  ERROR   $TASK_TITLE  (HTTP $HTTP_STATUS)"
        SKIPPED=$((SKIPPED + 1))
      fi
    fi
  done
done

echo ""
echo "Done: $CREATED created, $UPDATED updated, $CLOSED closed, $SKIPPED skipped"
echo ""
