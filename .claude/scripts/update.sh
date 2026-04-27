#!/usr/bin/env bash
# Update installed .claude configs in a project from claude-squad source
#
# Usage:
#   ./scripts/update.sh /path/to/project                  # Update all installed configs
#   ./scripts/update.sh /path/to/project agents            # Agents only
#   ./scripts/update.sh /path/to/project skills            # Skills only
#   ./scripts/update.sh /path/to/project pipeline          # Pipeline configs only
#   ./scripts/update.sh /path/to/project hooks             # Hooks only
#   ./scripts/update.sh /path/to/project scripts           # Scripts only
#   ./scripts/update.sh /path/to/project --dry-run         # Show what would change
#   ./scripts/update.sh /path/to/project --all             # Sync all source files (even uninstalled)
#
# Only updates files that already exist in the target project (unless --all is used).
# Shows diffs and asks for confirmation before overwriting changed files.
# Safe to run repeatedly — identical files are skipped.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
SQUAD_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Counters
CREATED=0
UPDATED=0
SKIPPED=0
UNCHANGED=0

# Flags
DRY_RUN=false
SYNC_ALL=false
CATEGORY=""
TARGET=""

# ─── Usage ───────────────────────────────────────────────────────────────────

usage() {
  echo "Usage: ./scripts/update.sh /path/to/project [agents|skills|pipeline|hooks|scripts] [--dry-run] [--all]"
  echo ""
  echo "Update installed .claude configs from claude-squad source."
  echo "Only updates files that already exist in the target (unless --all is used)."
  echo ""
  echo "Categories:"
  echo "  agents     Update agent definitions only"
  echo "  skills     Update skill definitions only"
  echo "  pipeline   Update pipeline configs only"
  echo "  hooks      Update hook scripts only"
  echo "  scripts    Update utility scripts only"
  echo ""
  echo "Options:"
  echo "  --dry-run  Show what would change without applying"
  echo "  --all      Sync all source files, even ones not currently installed"
  echo "  --help     Show this help message"
}

# ─── Parse arguments ─────────────────────────────────────────────────────────

for arg in "$@"; do
  case "$arg" in
    --dry-run) DRY_RUN=true ;;
    --all) SYNC_ALL=true ;;
    --help|-h) usage; exit 0 ;;
    agents|skills|pipeline|hooks|scripts) CATEGORY="$arg" ;;
    --*)
      echo "Unknown flag: $arg"
      usage
      exit 1
      ;;
    *)
      if [ -z "$TARGET" ]; then
        TARGET="$arg"
      else
        echo "Unknown argument: $arg"
        usage
        exit 1
      fi
      ;;
  esac
done

if [ -z "$TARGET" ]; then
  echo "Error: Project path is required."
  echo ""
  usage
  exit 1
fi

# Resolve to .claude directory
TARGET_DIR="$TARGET/.claude"

if [ ! -d "$TARGET_DIR" ] && [ "$SYNC_ALL" = false ]; then
  echo "Error: $TARGET_DIR does not exist."
  echo "Run setup.sh first to do an initial install, or use --all to create all files."
  exit 1
fi

# ─── Display relative path ───────────────────────────────────────────────────

rel_path() {
  local path="$1"
  echo ".claude/${path#$TARGET_DIR/}"
}

# ─── sync_file ───────────────────────────────────────────────────────────────

sync_file() {
  local src="$1"     # source file in claude-squad
  local dest="$2"    # target file in project/.claude/
  local make_exec="${3:-false}"  # make executable after copy

  if [ ! -f "$dest" ]; then
    if [ "$SYNC_ALL" = true ]; then
      # --all mode: create missing files
      if [ "$DRY_RUN" = true ]; then
        echo "  [dry-run] Would create: $(rel_path "$dest")"
        CREATED=$((CREATED + 1))
        return
      fi
      mkdir -p "$(dirname "$dest")"
      cp "$src" "$dest"
      if [ "$make_exec" = "true" ]; then
        chmod +x "$dest"
      fi
      echo "  Created: $(rel_path "$dest")"
      CREATED=$((CREATED + 1))
    else
      # Normal mode: skip files not installed
      SKIPPED=$((SKIPPED + 1))
    fi
    return
  fi

  if diff -q "$src" "$dest" > /dev/null 2>&1; then
    # Identical — skip
    echo "  Up to date: $(rel_path "$dest")"
    UNCHANGED=$((UNCHANGED + 1))
    return
  fi

  # Different — show diff, ask
  echo ""
  echo "  $(rel_path "$dest") has local changes:"
  diff --color=always --label "current" --label "claude-squad" -u "$dest" "$src" | head -40 || true
  echo ""

  if [ "$DRY_RUN" = true ]; then
    echo "  [dry-run] Would update: $(rel_path "$dest")"
    UPDATED=$((UPDATED + 1))
    return
  fi

  read -rp "  Apply update? [y/N/v] " choice
  case "$choice" in
    y|Y)
      cp "$src" "$dest"
      if [ "$make_exec" = "true" ]; then
        chmod +x "$dest"
      fi
      echo "  Updated."
      UPDATED=$((UPDATED + 1))
      ;;
    v|V)
      echo ""
      echo "--- Full source content ---"
      cat "$src"
      echo ""
      echo "--- End ---"
      echo ""
      read -rp "  Apply? [y/N] " c2
      if [ "$c2" = "y" ] || [ "$c2" = "Y" ]; then
        cp "$src" "$dest"
        if [ "$make_exec" = "true" ]; then
          chmod +x "$dest"
        fi
        echo "  Updated."
        UPDATED=$((UPDATED + 1))
      else
        echo "  Skipped."
        SKIPPED=$((SKIPPED + 1))
      fi
      ;;
    *)
      echo "  Skipped."
      SKIPPED=$((SKIPPED + 1))
      ;;
  esac
}

# ─── Category sync functions ─────────────────────────────────────────────────

sync_agents() {
  echo ""
  echo "[agents]"
  local src_dir="$SQUAD_DIR/agents"
  local dest_dir="$TARGET_DIR/agents"

  if [ ! -d "$src_dir" ]; then
    echo "  (no source agents found)"
    return
  fi

  # Core agents that should always be present (auto-added even without --all)
  local core_agents="pipeline-agent pm-agent ba-agent designer-agent architect-agent integration-agent qa-agent"

  for src in "$src_dir"/*.md; do
    [ -f "$src" ] || continue
    local fname
    fname=$(basename "$src")
    # Skip skill-tester-agent (dev-only, not installed by setup.sh)
    if [ "$fname" = "skill-tester-agent.md" ]; then
      continue
    fi
    local agent_name="${fname%.md}"

    # Auto-add missing core agents (they should always exist)
    if [ ! -f "$dest_dir/$fname" ]; then
      local is_core=false
      for ca in $core_agents; do
        if [ "$ca" = "$agent_name" ]; then
          is_core=true
          break
        fi
      done
      if [ "$is_core" = true ]; then
        if [ "$DRY_RUN" = true ]; then
          echo "  [dry-run] Would create (new core): $(rel_path "$dest_dir/$fname")"
          CREATED=$((CREATED + 1))
        else
          mkdir -p "$dest_dir"
          cp "$src" "$dest_dir/$fname"
          echo "  Created (new core): $(rel_path "$dest_dir/$fname")"
          CREATED=$((CREATED + 1))
        fi
        continue
      fi
    fi

    sync_file "$src" "$dest_dir/$fname" "false"
  done
}

sync_skills() {
  echo ""
  echo "[skills]"
  local src_dir="$SQUAD_DIR/skills"
  local dest_dir="$TARGET_DIR/skills"

  if [ ! -d "$src_dir" ]; then
    echo "  (no source skills found)"
    return
  fi

  for skill_dir in "$src_dir"/*/; do
    [ -d "$skill_dir" ] || continue
    local skill_name
    skill_name=$(basename "$skill_dir")
    local src_file="$skill_dir/SKILL.md"
    [ -f "$src_file" ] || continue
    sync_file "$src_file" "$dest_dir/$skill_name/SKILL.md" "false"
  done
}

sync_pipeline() {
  echo ""
  echo "[pipeline]"
  local src_dir="$SQUAD_DIR/pipeline"
  local dest_dir="$TARGET_DIR/pipeline"

  if [ ! -d "$src_dir" ]; then
    echo "  (no source pipeline configs found)"
    return
  fi

  # Core pipeline configs that should always be present
  local core_configs="pm ba designer architect integration qa"

  # Root-level pipeline files (config.json) — always sync
  for src in "$src_dir"/*.json; do
    [ -f "$src" ] || continue
    local fname
    fname=$(basename "$src")
    local dest="$dest_dir/$fname"

    # Root pipeline files (config.json) are always core — auto-add if missing
    if [ ! -f "$dest" ]; then
      if [ "$DRY_RUN" = true ]; then
        echo "  [dry-run] Would create (core): $(rel_path "$dest")"
        CREATED=$((CREATED + 1))
      else
        mkdir -p "$dest_dir"
        cp "$src" "$dest"
        echo "  Created (core): $(rel_path "$dest")"
        CREATED=$((CREATED + 1))
      fi
      continue
    fi

    # Preserve user's fizzy config when comparing config.json
    if [ "$fname" = "config.json" ] && command -v jq > /dev/null 2>&1; then
      local user_fizzy
      user_fizzy=$(jq '.fizzy // empty' "$dest" 2>/dev/null || true)
      if [ -n "$user_fizzy" ]; then
        local tmp_src
        tmp_src=$(mktemp)
        jq --argjson fizzy "$user_fizzy" '.fizzy = $fizzy' "$src" > "$tmp_src"
        sync_file "$tmp_src" "$dest" "false"
        rm -f "$tmp_src"
        continue
      fi
    fi

    sync_file "$src" "$dest" "false"
  done

  # Agent pipeline configs (preserve user-customized "count")
  if [ -d "$src_dir/agents" ]; then
    for src in "$src_dir/agents"/*.json; do
      [ -f "$src" ] || continue
      local fname
      fname=$(basename "$src")
      local config_name="${fname%.json}"
      local dest="$dest_dir/agents/$fname"

      # Auto-add missing core pipeline configs
      if [ ! -f "$dest" ]; then
        local is_core=false
        for cc in $core_configs; do
          if [ "$cc" = "$config_name" ]; then
            is_core=true
            break
          fi
        done
        if [ "$is_core" = true ]; then
          if [ "$DRY_RUN" = true ]; then
            echo "  [dry-run] Would create (new core): $(rel_path "$dest")"
            CREATED=$((CREATED + 1))
          else
            mkdir -p "$dest_dir/agents"
            cp "$src" "$dest"
            echo "  Created (new core): $(rel_path "$dest")"
            CREATED=$((CREATED + 1))
          fi
          continue
        fi
      fi

      # If dest exists and has a custom count, build a temp source
      # with the user's count so we don't overwrite it
      if [ -f "$dest" ] && command -v jq > /dev/null 2>&1; then
        local user_count
        user_count=$(jq -r '.count // empty' "$dest" 2>/dev/null || true)
        if [ -n "$user_count" ]; then
          local tmp_src
          tmp_src=$(mktemp)
          jq --argjson c "$user_count" '.count = $c' "$src" > "$tmp_src"
          sync_file "$tmp_src" "$dest" "false"
          rm -f "$tmp_src"
          continue
        fi
      fi

      sync_file "$src" "$dest" "false"
    done
  fi
}

sync_hooks() {
  echo ""
  echo "[hooks]"
  local src_dir="$SQUAD_DIR/hooks"
  local dest_dir="$TARGET_DIR/hooks"

  if [ ! -d "$src_dir" ]; then
    echo "  (no source hooks found)"
    return
  fi

  for src in "$src_dir"/*.sh; do
    [ -f "$src" ] || continue
    local fname
    fname=$(basename "$src")
    sync_file "$src" "$dest_dir/$fname" "true"
  done
}

sync_scripts() {
  echo ""
  echo "[scripts]"
  local src_dir="$SQUAD_DIR/scripts"
  local dest_dir="$TARGET_DIR/scripts"

  if [ ! -d "$src_dir" ]; then
    echo "  (no source scripts found)"
    return
  fi

  for src in "$src_dir"/*.sh; do
    [ -f "$src" ] || continue
    local fname
    fname=$(basename "$src")
    # Skip dev-only scripts
    case "$fname" in
      test-setup.sh|update.sh|skill-test.sh|skill-agent-test.sh) continue ;;
    esac
    sync_file "$src" "$dest_dir/$fname" "true"
  done
}

# ─── Main ─────────────────────────────────────────────────────────────────────

echo "=== claude-squad update ==="
echo "Source: $SQUAD_DIR"
echo "Target: $TARGET_DIR"
if [ "$DRY_RUN" = true ]; then
  echo "(dry run — no changes will be made)"
fi
if [ "$SYNC_ALL" = true ]; then
  echo "(--all: will create missing files)"
fi

# Run sync
if [ -n "$CATEGORY" ]; then
  case "$CATEGORY" in
    agents)   sync_agents ;;
    skills)   sync_skills ;;
    pipeline) sync_pipeline ;;
    hooks)    sync_hooks ;;
    scripts)  sync_scripts ;;
  esac
else
  sync_agents
  sync_skills
  sync_pipeline
  sync_hooks
  sync_scripts
fi

# Summary
echo ""
echo "--- Summary ---"
if [ "$SYNC_ALL" = true ]; then
  echo "  Created:    $CREATED"
fi
echo "  Updated:    $UPDATED"
echo "  Up to date: $UNCHANGED"
echo "  Skipped:    $SKIPPED"
echo "  Total:      $((CREATED + UPDATED + UNCHANGED + SKIPPED))"

if [ "$DRY_RUN" = true ]; then
  echo ""
  echo "(dry run — run without --dry-run to apply changes)"
fi
