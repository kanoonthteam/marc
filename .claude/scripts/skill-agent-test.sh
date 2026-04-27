#!/bin/bash
# Agent-based skill quality test runner
# For each skill, creates a temporary agent with only that skill loaded,
# sends a prompt, and passes the response to skill-tester-agent for scoring.
#
# Usage:
#   bash scripts/skill-agent-test.sh                       # Test all non-utility skills
#   bash scripts/skill-agent-test.sh rails-models           # Test a single skill
#   bash scripts/skill-agent-test.sh --category dev          # Test all dev skills
#   bash scripts/skill-agent-test.sh --category devops       # Test all devops skills
#   bash scripts/skill-agent-test.sh --category qa           # Test all QA skills
#   bash scripts/skill-agent-test.sh --category planning     # Test all planning skills

set -euo pipefail

# --- Auto-detect base directory (claude-squad root) ---
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

SKILLS_DIR="$BASE_DIR/skills"
AGENTS_DIR="$BASE_DIR/agents"
PROMPTS_DIR="$SCRIPT_DIR/skill-prompts"
ANSWERS_DIR="$SCRIPT_DIR/skill-answers"
RESULTS_DIR="$SCRIPT_DIR/skill-agent-results"
TESTER_AGENT="$AGENTS_DIR/skill-tester-agent.md"

mkdir -p "$RESULTS_DIR"

# --- Colors ---
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

# --- Counters ---
TOTAL=0
PASSED=0
FAILED=0
SKIPPED=0
FAILURES=""

# --- Skill categorization (mirrors skill-test.sh) ---
categorize_skill() {
  local skill="$1"
  case "$skill" in
    rails-*|react-*|flutter-*|node-*|odoo-*|salesforce-*|git-workflow|code-review-practices)
      echo "dev"
      ;;
    aws-*|azure-*|gcloud-*|firebase-*|flyio-*|devops-*|terraform-patterns|kubernetes-patterns|observability-practices|incident-management)
      echo "devops"
      ;;
    testing-*|playwright-testing|performance-testing|accessibility-testing|chaos-engineering)
      echo "qa"
      ;;
    task-*|domain-*|design-*|agile-frameworks|stakeholder-communication|requirements-elicitation|process-modeling|architecture-documentation|security-architecture|api-design|api-security)
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

# --- Create temporary agent with a single skill ---
create_temp_agent() {
  local skill="$1"
  local cat="$2"
  local tmp_agent
  tmp_agent=$(mktemp /tmp/skill-test-agent-XXXXXX.md)

  local role_desc
  case "$cat" in
    dev)      role_desc="a senior software developer" ;;
    devops)   role_desc="a senior DevOps engineer" ;;
    qa)       role_desc="a senior QA engineer" ;;
    planning) role_desc="a senior technical lead handling planning and architecture" ;;
    *)        role_desc="a senior software engineer" ;;
  esac

  cat > "$tmp_agent" <<EOF
---
name: skill-test-${skill}
description: Temporary agent for testing the ${skill} skill in isolation
tools: Read, Glob, Grep
model: sonnet
maxTurns: 3
skills: ${skill}
---

# Skill Test Agent

You are ${role_desc}. You have been given a single skill to use.
Respond to the prompt using only the knowledge and patterns from your loaded skill.
Provide specific, actionable, production-ready guidance with code examples where appropriate.
Do not ask clarifying questions -- make reasonable assumptions and proceed.
EOF

  echo "$tmp_agent"
}

# --- Test a single skill ---
test_skill() {
  local skill="$1"
  local cat
  cat=$(categorize_skill "$skill")

  # Skip utility skills
  if [[ "$cat" == "utility" ]]; then
    echo -e "  ${YELLOW}SKIP${NC} $skill (utility skill)"
    SKIPPED=$((SKIPPED + 1))
    return
  fi

  # Check for prompt file
  local prompt_file="$PROMPTS_DIR/${skill}.txt"
  if [[ ! -f "$prompt_file" ]]; then
    echo -e "  ${YELLOW}SKIP${NC} $skill (no prompt file at $prompt_file)"
    SKIPPED=$((SKIPPED + 1))
    return
  fi

  # Check for skill directory
  if [[ ! -d "$SKILLS_DIR/$skill" ]]; then
    echo -e "  ${RED}FAIL${NC} $skill (skill directory not found)"
    FAILED=$((FAILED + 1))
    TOTAL=$((TOTAL + 1))
    FAILURES="${FAILURES}\n- $skill: skill directory not found"
    return
  fi

  TOTAL=$((TOTAL + 1))
  local prompt
  prompt=$(cat "$prompt_file")

  echo -e "\n${BOLD}Testing: $skill${NC} (category: $cat)"
  echo "  Prompt: $prompt"

  # Step 1: Create temporary agent
  local tmp_agent
  tmp_agent=$(create_temp_agent "$skill" "$cat")

  # Step 2: Run claude CLI with the temp agent and prompt
  echo -e "  ${CYAN}Running agent with skill...${NC}"
  local response_file
  response_file=$(mktemp /tmp/skill-response-XXXXXX.txt)

  if ! claude --agent "$tmp_agent" --print --output-format text "$prompt" > "$response_file" 2>/dev/null; then
    echo -e "  ${RED}FAIL${NC} claude CLI invocation failed"
    FAILED=$((FAILED + 1))
    FAILURES="${FAILURES}\n- $skill: claude CLI invocation failed"
    rm -f "$tmp_agent" "$response_file"
    return
  fi

  local response
  response=$(cat "$response_file")

  if [[ -z "$response" ]]; then
    echo -e "  ${RED}FAIL${NC} empty response from agent"
    # Save failure result
    cat > "$RESULTS_DIR/${skill}.json" <<ENDJSON
{
  "skill": "$skill",
  "scores": { "relevance": 1, "depth": 1, "accuracy": 1, "completeness": 1 },
  "average": 1.0,
  "pass": false,
  "notes": "Agent produced empty response"
}
ENDJSON
    FAILED=$((FAILED + 1))
    FAILURES="${FAILURES}\n- $skill: empty response"
    rm -f "$tmp_agent" "$response_file"
    return
  fi

  # Step 3: Load answer guidelines if available
  local answer_file="$ANSWERS_DIR/${skill}.txt"
  local answer_section=""
  if [[ -f "$answer_file" ]]; then
    answer_section="

--- ANSWER GUIDELINES ---
$(cat "$answer_file")
--- END GUIDELINES ---

IMPORTANT: Evaluate the response against these guidelines. Check each 'Must Cover' point,
verify no 'Must NOT Do' violations, and confirm required code examples are present.
Include guideline_coverage in your JSON output."
    echo -e "  ${CYAN}Answer guideline loaded${NC}"
  fi

  # Step 4: Pass response to skill-tester-agent for scoring
  echo -e "  ${CYAN}Scoring with skill-tester-agent...${NC}"
  local scorer_prompt
  scorer_prompt="Evaluate the following skill response.

Skill name: $skill
Category: $cat
Original prompt: $prompt

--- RESPONSE START ---
$response
--- RESPONSE END ---
${answer_section}
Score this response according to your evaluation criteria and output the JSON result."

  local score_file
  score_file=$(mktemp /tmp/skill-score-XXXXXX.txt)

  if ! claude --agent "$TESTER_AGENT" --print --output-format text "$scorer_prompt" > "$score_file" 2>/dev/null; then
    echo -e "  ${RED}FAIL${NC} skill-tester-agent invocation failed"
    FAILED=$((FAILED + 1))
    FAILURES="${FAILURES}\n- $skill: scorer invocation failed"
    rm -f "$tmp_agent" "$response_file" "$score_file"
    return
  fi

  # Extract JSON from scorer output
  local score_output
  score_output=$(cat "$score_file")

  # Try to extract JSON block
  local json_result
  json_result=$(echo "$score_output" | python3 -c "
import sys, re, json
text = sys.stdin.read()
# Find JSON block (possibly inside markdown code fence)
patterns = [
    r'\`\`\`json\s*\n(.*?)\n\`\`\`',
    r'\`\`\`\s*\n(.*?)\n\`\`\`',
    r'(\{[^{}]*\"skill\"[^{}]*\"scores\"[^{}]*\})'
]
for p in patterns:
    m = re.search(p, text, re.DOTALL)
    if m:
        try:
            obj = json.loads(m.group(1))
            print(json.dumps(obj, indent=2))
            sys.exit(0)
        except:
            pass
# Fallback: try parsing the whole thing
try:
    obj = json.loads(text)
    print(json.dumps(obj, indent=2))
except:
    print('{}')
" 2>/dev/null || echo "{}")

  if [[ "$json_result" == "{}" ]]; then
    echo -e "  ${RED}FAIL${NC} could not parse scorer JSON output"
    cat > "$RESULTS_DIR/${skill}.json" <<ENDJSON
{
  "skill": "$skill",
  "scores": { "relevance": 1, "depth": 1, "accuracy": 1, "completeness": 1 },
  "average": 1.0,
  "pass": false,
  "notes": "Could not parse scorer output"
}
ENDJSON
    FAILED=$((FAILED + 1))
    FAILURES="${FAILURES}\n- $skill: unparseable scorer output"
    rm -f "$tmp_agent" "$response_file" "$score_file"
    return
  fi

  # Save result
  echo "$json_result" > "$RESULTS_DIR/${skill}.json"

  # Check pass/fail
  local did_pass
  did_pass=$(echo "$json_result" | python3 -c "import sys,json; d=json.load(sys.stdin); print('true' if d.get('pass',False) else 'false')" 2>/dev/null || echo "false")
  local avg
  avg=$(echo "$json_result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('average','?'))" 2>/dev/null || echo "?")
  local notes
  notes=$(echo "$json_result" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('notes',''))" 2>/dev/null || echo "")

  if [[ "$did_pass" == "true" ]]; then
    echo -e "  ${GREEN}PASS${NC} (avg: $avg) $notes"
    PASSED=$((PASSED + 1))
  else
    echo -e "  ${RED}FAIL${NC} (avg: $avg) $notes"
    FAILED=$((FAILED + 1))
    FAILURES="${FAILURES}\n- $skill (avg: $avg): $notes"
  fi

  # Cleanup temp files
  rm -f "$tmp_agent" "$response_file" "$score_file"
}

# --- Collect skills to test ---
collect_skills() {
  local filter_category="${1:-}"
  local skills=()

  for skill_dir in "$SKILLS_DIR"/*/; do
    local skill
    skill=$(basename "$skill_dir")
    local cat
    cat=$(categorize_skill "$skill")

    # Skip utility
    if [[ "$cat" == "utility" ]]; then
      continue
    fi

    if [[ -n "$filter_category" && "$cat" != "$filter_category" ]]; then
      continue
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
        echo "This script:"
        echo "  1. Creates a temporary agent with only the target skill loaded"
        echo "  2. Sends a realistic prompt from scripts/skill-prompts/{skill}.txt"
        echo "  3. Passes the response to skill-tester-agent for scoring"
        echo "  4. Saves JSON results to scripts/skill-agent-results/{skill}.json"
        echo ""
        echo "Prerequisites:"
        echo "  - claude CLI must be installed and authenticated"
        echo "  - python3 must be available (for JSON parsing)"
        echo ""
        echo "Examples:"
        echo "  $0                        # Test all non-utility skills"
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

  # Verify prerequisites
  if ! command -v claude &> /dev/null; then
    echo -e "${RED}ERROR${NC}: claude CLI not found. Install it first."
    exit 1
  fi
  if ! command -v python3 &> /dev/null; then
    echo -e "${RED}ERROR${NC}: python3 not found. Required for JSON parsing."
    exit 1
  fi
  if [[ ! -f "$TESTER_AGENT" ]]; then
    echo -e "${RED}ERROR${NC}: skill-tester-agent not found at $TESTER_AGENT"
    exit 1
  fi

  echo -e "${BOLD}================================================${NC}"
  echo -e "${BOLD} Skill Agent Quality Test${NC}"
  echo -e "${BOLD}================================================${NC}"
  echo -e "Base directory: $BASE_DIR"
  echo -e "Prompts directory: $PROMPTS_DIR"
  echo -e "Results directory: $RESULTS_DIR"

  if [[ -n "$single_skill" ]]; then
    echo -e "Testing: ${CYAN}$single_skill${NC}"
    test_skill "$single_skill"
  elif [[ -n "$filter_category" ]]; then
    echo -e "Category filter: ${CYAN}$filter_category${NC}"
    local skills
    skills=$(collect_skills "$filter_category")
    for skill in $skills; do
      test_skill "$skill"
    done
  else
    echo -e "Testing: ${CYAN}all non-utility skills${NC}"
    local skills
    skills=$(collect_skills "")
    for skill in $skills; do
      test_skill "$skill"
    done
  fi

  # --- Summary ---
  echo -e "\n${BOLD}================================================${NC}"
  echo -e "${BOLD} Summary${NC}"
  echo -e "${BOLD}================================================${NC}"
  echo -e "Total skills tested: $TOTAL"
  echo -e "${GREEN}Passed: $PASSED${NC}"
  echo -e "${RED}Failed: $FAILED${NC}"
  if [[ "$SKIPPED" -gt 0 ]]; then
    echo -e "${YELLOW}Skipped: $SKIPPED${NC}"
  fi

  if [[ -n "$FAILURES" ]]; then
    echo -e "\n${RED}Failed skills:${NC}"
    echo -e "$FAILURES"
  fi

  # --- Write SUMMARY.md ---
  local summary_file="$RESULTS_DIR/SUMMARY.md"
  {
    echo "# Skill Agent Test Results"
    echo ""
    echo "**Date:** $(date '+%Y-%m-%d %H:%M:%S')"
    echo "**Base directory:** $BASE_DIR"
    if [[ -n "$single_skill" ]]; then
      echo "**Filter:** Single skill: $single_skill"
    elif [[ -n "$filter_category" ]]; then
      echo "**Filter:** Category: $filter_category"
    else
      echo "**Filter:** All non-utility skills"
    fi
    echo ""
    echo "## Summary"
    echo ""
    echo "| Metric | Count |"
    echo "|--------|-------|"
    echo "| Total tested | $TOTAL |"
    echo "| Passed | $PASSED |"
    echo "| Failed | $FAILED |"
    echo "| Skipped | $SKIPPED |"
    echo ""
    echo "## Results by Skill"
    echo ""
    echo "| Skill | Relevance | Depth | Accuracy | Completeness | Average | Pass |"
    echo "|-------|-----------|-------|----------|--------------|---------|------|"

    for result_file in "$RESULTS_DIR"/*.json; do
      if [[ ! -f "$result_file" ]]; then continue; fi
      local row
      row=$(python3 -c "
import json, sys
try:
    d = json.load(open('$result_file'))
    s = d.get('scores', {})
    print('| {} | {} | {} | {} | {} | {} | {} |'.format(
        d.get('skill', '?'),
        s.get('relevance', '?'),
        s.get('depth', '?'),
        s.get('accuracy', '?'),
        s.get('completeness', '?'),
        d.get('average', '?'),
        'PASS' if d.get('pass') else 'FAIL'
    ))
except:
    pass
" 2>/dev/null || true)
      if [[ -n "$row" ]]; then
        echo "$row"
      fi
    done

    echo ""
    if [[ -n "$FAILURES" ]]; then
      echo "## Failed Skills"
      echo ""
      echo -e "$FAILURES"
      echo ""
    fi
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
