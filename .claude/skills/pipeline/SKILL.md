---
name: pipeline
description: Launch the development pipeline — PM plans, BA refines, Architect reviews, devs implement, QA verifies
invocation: /pipeline
---

# Pipeline Skill

Launch the claude-squad development pipeline.

## Usage

```
/pipeline <feature description or BRD>
```

## What Happens

1. **Planning Phase** (PM → BA → Architect loop):
   - PM reads your feature request and creates a structured task breakdown in `tasks.json`
   - BA reviews the breakdown, adds domain detail, scope boundaries, and acceptance criteria
   - PM revises based on BA feedback
   - Architect reviews for technical feasibility and system design
   - BA updates based on architect feedback
   - You approve the final plan

2. **Implementation Phase**:
   - Tasks are assigned to dev/devops agents based on their tags
   - Multiple agents can work in parallel on independent tasks
   - Each agent implements their task and marks it done
   - **Auto-advances** to integration when all tasks are done

3. **Integration Phase**:
   - Integration agent writes E2E tests for new user flows
   - Updates CHANGELOG, README, API docs, migration guides
   - Validates cross-feature integration points
   - **Auto-advances** to verification when complete

4. **Verification Phase**:
   - QA agent verifies each completed task against acceptance criteria
   - Bugs are reported and routed back to dev agents
   - Phase completes when all tasks pass QA

The pipeline tracks progress via `pipelinePhase` in `tasks.json`. After context compaction, it reads this field to resume from where it left off. When complete, it sets `pipelinePhase` to `"completed"` and returns to normal interaction.

## Multi-Phase Features

For large features, the PM breaks work into phases. Each phase completes its full cycle (implement → integrate → verify) before the next begins.

## Customization

- Remove agents you don't need from `.claude/pipeline/agents/`
- Edit `.claude/agents/ba-agent.md` to add your domain expertise
- Edit `.claude/pipeline/config.json` to adjust settings

## Examples

```
/pipeline Add user authentication with email/password and Google OAuth
/pipeline Implement a product catalog with search, filtering, and pagination
/pipeline Set up CI/CD pipeline with staging and production environments
```
