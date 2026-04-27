#!/bin/bash
# Structural quality checks for SKILL.md files
# Usage:
#   bash scripts/skill-test.sh                       # Test all skills
#   bash scripts/skill-test.sh rails-models           # Test a single skill
#   bash scripts/skill-test.sh --category dev          # Test all dev skills
#   bash scripts/skill-test.sh --category devops       # Test all devops skills
#   bash scripts/skill-test.sh --category qa           # Test all QA skills
#   bash scripts/skill-test.sh --category planning     # Test all planning skills

set -euo pipefail

# --- Auto-detect base directory (claude-squad root) ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

SKILLS_DIR="$BASE_DIR/skills"
AGENTS_DIR="$BASE_DIR/agents"
PIPELINE_DIR="$BASE_DIR/pipeline"
RESULTS_DIR="$SCRIPT_DIR/skill-test-results"

mkdir -p "$RESULTS_DIR"

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m' # No Color

# --- Counters ---
TOTAL=0
PASSED=0
FAILED=0
SKIPPED=0
FAILURES=""

# --- Skill categorization ---
categorize_skill() {
  local skill="$1"
  case "$skill" in
    rails-*|react-*|flutter-*|node-*|odoo-*|salesforce-*|ml-*|researcher-*|parser-*|graph-*|cli-design|build-systems|export-formats|design-tool-apis|project-tool-apis|python-*|dotnet-*|threejs-*|gltf-*|opencascade-*|cad-*|computational-*|esp32-*|vpn-protocols|firewall-routing|dns-dhcp|network-monitoring|kicad-*|signal-interfacing|pcb-bom|rust-*|go-*|svelte-*|martech-*|sec-*|git-workflow|code-review-practices)
      echo "dev"
      ;;
    aws-*|azure-*|gcloud-*|firebase-*|flyio-*|devops-*|terraform-patterns|kubernetes-patterns|observability-practices|incident-management)
      echo "devops"
      ;;
    testing-*|playwright-testing|performance-testing|accessibility-testing|chaos-engineering)
      echo "qa"
      ;;
    task-*|domain-*|design-*|legal-*|agile-frameworks|stakeholder-communication|requirements-elicitation|process-modeling|architecture-documentation|security-architecture|api-design|api-security)
      echo "planning"
      ;;
    pipeline)
      echo "planning"
      ;;
    pipeline-status|review)
      echo "utility"
      ;;
    *)
      echo "unknown"
      ;;
  esac
}

# --- Determine minimum thresholds by category ---
min_lines_for_category() {
  local cat="$1"
  case "$cat" in
    dev|devops|qa|architect) echo 300 ;;
    planning)                echo 200 ;;
    utility)                 echo 0 ;;
    *)                       echo 200 ;;
  esac
}

min_sources_for_category() {
  local cat="$1"
  case "$cat" in
    dev|devops|qa|architect) echo 5 ;;
    planning)                echo 3 ;;
    utility)                 echo 0 ;;
    *)                       echo 3 ;;
  esac
}

min_codeblocks_for_category() {
  local cat="$1"
  case "$cat" in
    dev|devops|qa|architect) echo 3 ;;
    planning)                echo 1 ;;
    utility)                 echo 0 ;;
    *)                       echo 1 ;;
  esac
}

# --- Required sections per category ---
required_sections_for_category() {
  local cat="$1"
  case "$cat" in
    dev)
      echo "Best Practices|Anti-Patterns|Sources & References"
      ;;
    devops)
      echo "Best Practices|Sources & References"
      ;;
    qa)
      echo "Best Practices|Sources & References"
      ;;
    planning)
      echo "Sources & References"
      ;;
    utility)
      echo ""
      ;;
    *)
      echo "Sources & References"
      ;;
  esac
}

# --- Individual check functions ---

# Check 1: Valid YAML frontmatter
check_frontmatter() {
  local file="$1"
  local skill="$2"
  local first_line
  first_line=$(head -n 1 "$file")
  if [[ "$first_line" != "---" ]]; then
    echo "FAIL"
    return
  fi
  # Find closing ---
  local closing
  closing=$(tail -n +2 "$file" | grep -n "^---$" | head -n 1 | cut -d: -f1)
  if [[ -z "$closing" ]]; then
    echo "FAIL"
    return
  fi
  # Check for name and description fields
  local frontmatter
  frontmatter=$(head -n $((closing + 1)) "$file" | tail -n +2 | head -n "$closing")
  local has_name has_desc
  has_name=$(echo "$frontmatter" | grep -c "^name:" || true)
  has_desc=$(echo "$frontmatter" | grep -c "^description:" || true)
  if [[ "$has_name" -ge 1 && "$has_desc" -ge 1 ]]; then
    echo "PASS"
  else
    echo "FAIL"
  fi
}

# Check 2: Minimum line count
check_line_count() {
  local file="$1"
  local min_lines="$2"
  local count
  count=$(wc -l < "$file" | tr -d ' ')
  if [[ "$count" -ge "$min_lines" ]]; then
    echo "PASS ($count lines)"
  else
    echo "FAIL ($count lines, need >= $min_lines)"
  fi
}

# Check 3: Sources & References section
check_sources_section() {
  local file="$1"
  if grep -qi "^#.*sources\|^#.*references" "$file"; then
    echo "PASS"
  else
    echo "FAIL"
  fi
}

# Check 4: Minimum source URLs
check_source_urls() {
  local file="$1"
  local min_sources="$2"
  local url_count
  url_count=$(grep -cE "https?://" "$file" || true)
  if [[ "$url_count" -ge "$min_sources" ]]; then
    echo "PASS ($url_count URLs)"
  else
    echo "FAIL ($url_count URLs, need >= $min_sources)"
  fi
}

# Check 5: Code examples present
check_code_blocks() {
  local file="$1"
  local min_blocks="$2"
  local block_count
  block_count=$(grep -c '^\`\`\`' "$file" || true)
  # Each code block has open + close, so divide by 2
  local actual_blocks=$(( block_count / 2 ))
  if [[ "$actual_blocks" -ge "$min_blocks" ]]; then
    echo "PASS ($actual_blocks code blocks)"
  else
    echo "FAIL ($actual_blocks code blocks, need >= $min_blocks)"
  fi
}

# Check 6: No orphaned skill references (skill is referenced by at least 1 agent)
check_not_orphaned() {
  local skill="$1"
  local cat="$2"
  # Utility skills may not be in agent frontmatter directly
  if [[ "$cat" == "utility" ]]; then
    echo "SKIP"
    return
  fi
  local found=0
  for agent_file in "$AGENTS_DIR"/*.md; do
    if grep -q "$skill" "$agent_file" 2>/dev/null; then
      found=1
      break
    fi
  done
  if [[ "$found" -eq 1 ]]; then
    echo "PASS"
  else
    echo "FAIL (not referenced by any agent)"
  fi
}

# Check 7: Agent references valid (all agent skill refs point to existing skill dirs)
# This is a global check, run once
check_agent_refs_valid() {
  local errors=""
  for agent_file in "$AGENTS_DIR"/*.md; do
    local agent_name
    agent_name=$(basename "$agent_file" .md)
    # Extract skills line from frontmatter
    local skills_line
    skills_line=$(grep "^skills:" "$agent_file" 2>/dev/null || true)
    if [[ -z "$skills_line" ]]; then
      continue
    fi
    # Parse comma-separated skill names
    local skills_csv
    skills_csv=$(echo "$skills_line" | sed 's/^skills: *//' | tr ',' '\n' | sed 's/^ *//;s/ *$//')
    while IFS= read -r s; do
      s=$(echo "$s" | tr -d ' ')
      if [[ -z "$s" ]]; then continue; fi
      if [[ ! -d "$SKILLS_DIR/$s" ]]; then
        errors="${errors}  Agent $agent_name references non-existent skill: $s\n"
      fi
    done <<< "$skills_csv"
  done
  if [[ -z "$errors" ]]; then
    echo "PASS"
  else
    echo "FAIL"
    echo -e "$errors"
  fi
}

# Check 8: Pipeline config matches (pipeline .json "skills" arrays match agent skills)
check_pipeline_config_matches() {
  local errors=""
  if [[ ! -d "$PIPELINE_DIR/agents" ]]; then
    echo "SKIP (no pipeline/agents directory)"
    return
  fi
  for pipeline_file in "$PIPELINE_DIR/agents"/*.json; do
    local pipeline_name
    pipeline_name=$(basename "$pipeline_file" .json)

    # Get agent name from pipeline JSON
    local agent_ref
    agent_ref=$(python3 -c "import json; d=json.load(open('$pipeline_file')); print(d.get('agent',''))" 2>/dev/null || true)
    if [[ -z "$agent_ref" ]]; then
      continue
    fi

    local agent_file="$AGENTS_DIR/${agent_ref}.md"
    if [[ ! -f "$agent_file" ]]; then
      errors="${errors}  Pipeline $pipeline_name references non-existent agent: $agent_ref\n"
      continue
    fi

    # Get skills from pipeline JSON
    local pipeline_skills
    pipeline_skills=$(python3 -c "import json; d=json.load(open('$pipeline_file')); print(' '.join(sorted(d.get('skills',[]))))" 2>/dev/null || true)

    # Get skills from agent frontmatter
    local agent_skills_line
    agent_skills_line=$(grep "^skills:" "$agent_file" 2>/dev/null || true)
    local agent_skills
    agent_skills=$(echo "$agent_skills_line" | sed 's/^skills: *//' | tr ',' '\n' | sed 's/^ *//;s/ *$//' | sort | tr '\n' ' ' | sed 's/ *$//')

    if [[ "$pipeline_skills" != "$agent_skills" ]]; then
      errors="${errors}  Pipeline $pipeline_name skills mismatch with agent $agent_ref:\n"
      errors="${errors}    Pipeline: [$pipeline_skills]\n"
      errors="${errors}    Agent:    [$agent_skills]\n"
    fi
  done
  if [[ -z "$errors" ]]; then
    echo "PASS"
  else
    echo "FAIL"
    echo -e "$errors"
  fi
}

# Check 9: Required sections exist per category
check_required_sections() {
  local file="$1"
  local cat="$2"
  local required
  required=$(required_sections_for_category "$cat")
  if [[ -z "$required" ]]; then
    echo "SKIP"
    return
  fi
  local missing=""
  IFS='|' read -ra sections <<< "$required"
  for section in "${sections[@]}"; do
    if ! grep -qi "^#.*${section}" "$file"; then
      missing="${missing}  Missing section: $section\n"
    fi
  done
  if [[ -z "$missing" ]]; then
    echo "PASS"
  else
    echo "FAIL"
    echo -e "$missing"
  fi
}

# --- Print a check result ---
print_check() {
  local check_name="$1"
  local result="$2"
  if [[ "$result" == PASS* ]]; then
    echo -e "  ${GREEN}PASS${NC} $check_name ${CYAN}${result#PASS}${NC}"
    return 0
  elif [[ "$result" == SKIP* ]]; then
    echo -e "  ${YELLOW}SKIP${NC} $check_name ${result#SKIP}"
    return 2
  else
    echo -e "  ${RED}FAIL${NC} $check_name ${result#FAIL}"
    return 1
  fi
}

# --- Run all checks for a single skill ---
test_skill() {
  local skill="$1"
  local skill_dir="$SKILLS_DIR/$skill"
  local skill_file="$skill_dir/SKILL.md"

  if [[ ! -f "$skill_file" ]]; then
    echo -e "${RED}ERROR${NC}: Skill file not found: $skill_file"
    FAILED=$((FAILED + 1))
    TOTAL=$((TOTAL + 1))
    FAILURES="${FAILURES}\n- $skill: SKILL.md not found"
    return
  fi

  local cat
  cat=$(categorize_skill "$skill")

  echo -e "\n${BOLD}$skill${NC} (category: $cat)"
  echo "  ────────────────────────────────────"

  local skill_pass=true
  local skill_results=""

  # Skip most checks for utility skills
  if [[ "$cat" == "utility" ]]; then
    local r1
    r1=$(check_frontmatter "$skill_file" "$skill")
    print_check "YAML frontmatter" "$r1" || skill_pass=false
    skill_results="${skill_results}| Frontmatter | $r1 |\n"
    TOTAL=$((TOTAL + 1))
    SKIPPED=$((SKIPPED + 1))
    echo -e "  ${YELLOW}SKIP${NC} Remaining checks (utility skill)"
    if [[ "$skill_pass" == true ]]; then
      PASSED=$((PASSED + 1))
    else
      FAILED=$((FAILED + 1))
      FAILURES="${FAILURES}\n- $skill: frontmatter check failed"
    fi
    return
  fi

  local min_lines min_sources min_blocks
  min_lines=$(min_lines_for_category "$cat")
  min_sources=$(min_sources_for_category "$cat")
  min_blocks=$(min_codeblocks_for_category "$cat")

  TOTAL=$((TOTAL + 1))

  # Check 1: Frontmatter
  local r1
  r1=$(check_frontmatter "$skill_file" "$skill")
  print_check "1. YAML frontmatter" "$r1" || skill_pass=false
  skill_results="${skill_results}| Frontmatter | $r1 |\n"

  # Check 2: Line count
  local r2
  r2=$(check_line_count "$skill_file" "$min_lines")
  print_check "2. Line count (>= $min_lines)" "$r2" || skill_pass=false
  skill_results="${skill_results}| Line count | $r2 |\n"

  # Check 3: Sources section
  local r3
  r3=$(check_sources_section "$skill_file")
  print_check "3. Sources & References section" "$r3" || skill_pass=false
  skill_results="${skill_results}| Sources section | $r3 |\n"

  # Check 4: Source URLs
  local r4
  r4=$(check_source_urls "$skill_file" "$min_sources")
  print_check "4. Source URLs (>= $min_sources)" "$r4" || skill_pass=false
  skill_results="${skill_results}| Source URLs | $r4 |\n"

  # Check 5: Code blocks
  local r5
  r5=$(check_code_blocks "$skill_file" "$min_blocks")
  print_check "5. Code examples (>= $min_blocks blocks)" "$r5" || skill_pass=false
  skill_results="${skill_results}| Code blocks | $r5 |\n"

  # Check 6: Not orphaned
  local r6
  r6=$(check_not_orphaned "$skill" "$cat")
  print_check "6. Referenced by agent" "$r6" || skill_pass=false
  skill_results="${skill_results}| Agent reference | $r6 |\n"

  # Check 9: Required sections
  local r9
  r9=$(check_required_sections "$skill_file" "$cat")
  print_check "9. Required sections" "$r9" || { [[ "$r9" != SKIP* ]] && skill_pass=false; }
  skill_results="${skill_results}| Required sections | $r9 |\n"

  if [[ "$skill_pass" == true ]]; then
    PASSED=$((PASSED + 1))
  else
    FAILED=$((FAILED + 1))
    FAILURES="${FAILURES}\n- $skill ($cat)"
  fi
}

# --- Collect skills to test ---
collect_skills() {
  local filter_category="${1:-}"
  local skills=()

  for skill_dir in "$SKILLS_DIR"/*/; do
    local skill
    skill=$(basename "$skill_dir")
    if [[ -n "$filter_category" ]]; then
      local cat
      cat=$(categorize_skill "$skill")
      if [[ "$cat" != "$filter_category" ]]; then
        continue
      fi
    fi
    skills+=("$skill")
  done

  echo "${skills[@]}"
}

# --- Main ---
main() {
  local single_skill=""
  local filter_category=""

  # Parse arguments
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --category)
        filter_category="$2"
        shift 2
        ;;
      --help|-h)
        echo "Usage: $0 [skill-name] [--category dev|devops|qa|planning]"
        echo ""
        echo "Options:"
        echo "  skill-name        Test a single skill by name"
        echo "  --category CAT    Test all skills in a category (dev, devops, qa, planning)"
        echo ""
        echo "Examples:"
        echo "  $0                        # Test all skills"
        echo "  $0 rails-models           # Test only rails-models"
        echo "  $0 --category dev         # Test all dev skills"
        exit 0
        ;;
      *)
        single_skill="$1"
        shift
        ;;
    esac
  done

  echo -e "${BOLD}=====================================${NC}"
  echo -e "${BOLD} Skill Structural Quality Checks${NC}"
  echo -e "${BOLD}=====================================${NC}"
  echo -e "Base directory: $BASE_DIR"
  echo -e "Skills directory: $SKILLS_DIR"

  if [[ -n "$single_skill" ]]; then
    echo -e "Testing: ${CYAN}$single_skill${NC}"
  elif [[ -n "$filter_category" ]]; then
    echo -e "Category filter: ${CYAN}$filter_category${NC}"
  else
    echo -e "Testing: ${CYAN}all skills${NC}"
  fi

  # --- Global checks (7 & 8) ---
  echo -e "\n${BOLD}Global Checks${NC}"
  echo "  ────────────────────────────────────"

  local g7
  g7=$(check_agent_refs_valid)
  local g7_status
  g7_status=$(echo "$g7" | head -n 1)
  print_check "7. Agent skill references valid" "$g7_status" || true
  if [[ "$g7_status" == "FAIL" ]]; then
    echo "$g7" | tail -n +2
  fi

  local g8
  g8=$(check_pipeline_config_matches)
  local g8_status
  g8_status=$(echo "$g8" | head -n 1)
  print_check "8. Pipeline config matches agents" "$g8_status" || true
  if [[ "$g8_status" == "FAIL" ]]; then
    echo "$g8" | tail -n +2
  fi

  # --- Per-skill checks ---
  if [[ -n "$single_skill" ]]; then
    test_skill "$single_skill"
  else
    local skills
    skills=$(collect_skills "$filter_category")
    for skill in $skills; do
      test_skill "$skill"
    done
  fi

  # --- Summary ---
  echo -e "\n${BOLD}=====================================${NC}"
  echo -e "${BOLD} Summary${NC}"
  echo -e "${BOLD}=====================================${NC}"
  echo -e "Total skills tested: $TOTAL"
  echo -e "${GREEN}Passed: $PASSED${NC}"
  echo -e "${RED}Failed: $FAILED${NC}"
  if [[ "$SKIPPED" -gt 0 ]]; then
    echo -e "${YELLOW}Skipped (utility): $SKIPPED${NC}"
  fi

  if [[ -n "$FAILURES" ]]; then
    echo -e "\n${RED}Failed skills:${NC}"
    echo -e "$FAILURES"
  fi

  # --- Write SUMMARY.md ---
  local summary_file="$RESULTS_DIR/SUMMARY.md"
  {
    echo "# Skill Structural Test Results"
    echo ""
    echo "**Date:** $(date '+%Y-%m-%d %H:%M:%S')"
    echo "**Base directory:** $BASE_DIR"
    if [[ -n "$single_skill" ]]; then
      echo "**Filter:** Single skill: $single_skill"
    elif [[ -n "$filter_category" ]]; then
      echo "**Filter:** Category: $filter_category"
    else
      echo "**Filter:** All skills"
    fi
    echo ""
    echo "## Summary"
    echo ""
    echo "| Metric | Count |"
    echo "|--------|-------|"
    echo "| Total tested | $TOTAL |"
    echo "| Passed | $PASSED |"
    echo "| Failed | $FAILED |"
    echo "| Skipped (utility) | $SKIPPED |"
    echo ""
    echo "## Global Checks"
    echo ""
    echo "| Check | Result |"
    echo "|-------|--------|"
    echo "| Agent skill references valid | $g7_status |"
    echo "| Pipeline config matches agents | $g8_status |"
    echo ""
    if [[ -n "$FAILURES" ]]; then
      echo "## Failed Skills"
      echo ""
      echo -e "$FAILURES"
      echo ""
    fi
    echo "## Checks Performed"
    echo ""
    echo "1. Valid YAML frontmatter (has \`---\` block with \`name\` and \`description\` fields)"
    echo "2. Minimum line count (>=300 for dev/devops/qa/architect, >=200 for pm/ba)"
    echo "3. Sources & References section exists"
    echo "4. Minimum source URLs (>=5 for dev/devops/qa/architect, >=3 for pm/ba)"
    echo "5. Code examples present (>=3 code blocks for dev/devops/qa/architect, >=1 for pm/ba)"
    echo "6. No orphaned skill references (referenced by at least 1 agent)"
    echo "7. Agent references valid (all agent skill refs point to existing skill dirs)"
    echo "8. Pipeline config matches (pipeline .json skills arrays match agent skills)"
    echo "9. Required sections exist per category"
  } > "$summary_file"

  echo -e "\nResults written to: $summary_file"

  # --- Exit code ---
  if [[ "$FAILED" -gt 0 ]]; then
    exit 1
  else
    exit 0
  fi
}

main "$@"
