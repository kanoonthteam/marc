---
name: pipeline-status
description: Show pipeline progress — kanban board, agent activity, task status
invocation: /pipeline-status
---

# Pipeline Status

Show the current state of the development pipeline.

## Usage

```
/pipeline-status
```

## What It Shows

### Kanban Board

Displays tasks organized by status columns:

```
┌─────────────┬─────────────┬─────────────┬─────────────┐
│ TODO        │ IN PROGRESS │ REVIEW      │ DONE        │
├─────────────┼─────────────┼─────────────┼─────────────┤
│ T003: Auth  │ T001: DB    │ T002: API   │             │
│   [rails]   │   [rails]   │   [node]    │             │
│ T004: UI    │             │             │             │
│   [react]   │             │             │             │
└─────────────┴─────────────┴─────────────┴─────────────┘
```

### Progress Summary

- Total tasks, completed, remaining
- Current phase
- Active agents and their assignments
- Blocked tasks and reasons

## Implementation

Read `tasks.json` from the project root and display:

1. Parse the task board
2. Group tasks by status
3. Show a formatted kanban view
4. Calculate and display progress percentage
5. List any blocked or failed tasks

If no `tasks.json` exists, show a message indicating no pipeline is active.
