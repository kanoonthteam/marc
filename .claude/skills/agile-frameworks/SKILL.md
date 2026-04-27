---
name: agile-frameworks
description: Scrum ceremonies, sprint planning, retrospectives, Kanban flow metrics, and scaling frameworks
---

# Agile Frameworks

## Overview

Agile is a set of values and principles for iterative, incremental software delivery. Scrum and Kanban are the most widely adopted frameworks. This skill covers ceremonies, planning techniques, metrics, retrospective formats, and scaling approaches for teams and organizations.

## Scrum Framework

### Roles

| Role | Responsibilities |
|------|-----------------|
| Product Owner | Owns the product backlog, prioritizes by value, accepts/rejects work |
| Scrum Master | Facilitates ceremonies, removes impediments, coaches the team |
| Developers | Self-organizing team that delivers the increment |

### Sprint Structure

```
Sprint (1-4 weeks, typically 2 weeks)
├── Sprint Planning (Day 1, 2-4 hours)
│   ├── WHAT: Select items from backlog
│   └── HOW: Break items into tasks, create sprint backlog
├── Daily Standup (15 min, every day)
│   ├── What did I do yesterday?
│   ├── What will I do today?
│   └── Any blockers?
├── Development Work (throughout sprint)
├── Sprint Review (Last day, 1-2 hours)
│   ├── Demo completed work to stakeholders
│   └── Gather feedback, update backlog
└── Sprint Retrospective (Last day, 1-1.5 hours)
    ├── What went well?
    ├── What needs improvement?
    └── Action items for next sprint
```

## Sprint Planning

### Capacity Planning

```markdown
## Sprint Capacity Calculation

Team: 5 developers
Sprint length: 10 working days

### Step 1: Available Days
| Developer | Days Off | Available Days |
|-----------|----------|----------------|
| Alice     | 1 (PTO) | 9              |
| Bob       | 0       | 10             |
| Carol     | 2 (conf)| 8              |
| Dave      | 0       | 10             |
| Eve       | 1 (PTO) | 9              |
| **Total** |         | **46 days**    |

### Step 2: Focus Factor
- Meetings, support, reviews: ~25% overhead
- Focus factor: 0.75

### Step 3: Sprint Capacity
- Capacity: 46 * 0.75 = 34.5 ideal days
- In story points (if velocity ~40 points/sprint): plan for 38-42 points
```

### Velocity Tracking

```markdown
## Velocity History

| Sprint | Committed | Completed | Velocity |
|--------|-----------|-----------|----------|
| S-1    | 45        | 38        | 38       |
| S-2    | 40        | 42        | 42       |
| S-3    | 42        | 40        | 40       |
| S-4    | 40        | 41        | 41       |
| S-5    | 42        | 39        | 39       |

Rolling average (last 3): 40 points
Range: 38-42 points
Recommendation: Commit to 38-40 points next sprint
```

### Commitment vs Forecast

| Approach | Description | When to Use |
|----------|-------------|-------------|
| Commitment | "We will deliver these items" | Mature team, stable velocity, well-refined backlog |
| Forecast | "We expect to deliver these items" | New team, uncertain scope, external dependencies |

**Best Practice**: Use forecasting language. Present a range: "We will likely complete 8-10 items; we are confident in the top 8."

## Estimation Techniques

### Story Points (Fibonacci)

```
1  - Trivial (config change, copy update)
2  - Small (well-understood, few files)
3  - Medium (some complexity, known pattern)
5  - Large (multiple components, some unknowns)
8  - Very large (significant unknowns, many components)
13 - Epic-sized (should be split)
```

### Planning Poker Process

1. Product Owner presents the user story
2. Team asks clarifying questions
3. Each member privately selects a card (1, 2, 3, 5, 8, 13)
4. All cards revealed simultaneously
5. Highest and lowest explain their reasoning
6. Re-vote if needed (usually converges in 2 rounds)
7. Team agrees on final estimate

### T-Shirt Sizing (for Roadmap-Level)

| Size | Relative Effort | Example |
|------|----------------|---------|
| XS | Half a day | Bug fix, config change |
| S | 1-2 days | Small feature, well-defined |
| M | 3-5 days | Feature with moderate complexity |
| L | 1-2 weeks | Cross-cutting feature |
| XL | 2-4 weeks | Major feature, should be broken down |

## Burndown & Burnup Charts

### Burndown Chart

```
Story Points Remaining
50 |*
   | *
40 |  *
   |   *  *         <- Actual (jagged, real-world)
30 |    *   *
   |     ----*       <- Ideal (straight line)
20 |          *
   |            * *
10 |              *
   |               *
 0 |________________*
   D1 D2 D3 D4 D5 D6 D7 D8 D9 D10
```

### Burnup Chart (Preferred)

```
Story Points
50 |                    ___--- Total Scope (may increase)
   |               ___--
40 |          ___---
   |     ___--
30 |  __--
   |_--               **** Completed Work
20 |              ****
   |         ****
10 |    ****
   | ***
 0 |*___________________
   D1 D2 D3 D4 D5 D6 D7 D8 D9 D10
```

The burnup chart shows both scope changes AND progress, making it more informative than burndown for stakeholders.

## Retrospective Formats

### Start / Stop / Continue

```markdown
## Sprint 5 Retrospective

### Start (Things we should begin doing)
- Pair programming for complex features
- Writing acceptance criteria before estimation
- Celebrating wins at sprint review

### Stop (Things we should stop doing)
- Working on items not in the sprint backlog
- Skipping code review for "quick fixes"
- Having meetings without agendas

### Continue (Things working well, keep doing)
- Daily standups at 9:30am (good attendance)
- Using feature flags for large changes
- Post-deployment smoke tests
```

### 4Ls (Liked, Learned, Lacked, Longed For)

```markdown
## 4Ls Retrospective

### Liked
- Collaborative debugging session on the auth bug
- New CI pipeline reduced build time by 50%

### Learned
- How to use OpenTelemetry for distributed tracing
- That our staging environment does not match production

### Lacked
- Clear acceptance criteria for user stories 34 and 37
- Time for technical debt reduction

### Longed For
- Automated database migrations in CI
- Better test data management
```

### Sailboat

```
          ☀️ SUN (Vision/Goal)
          "Ship v2.0 by Q2"

  ⛵ BOAT (The team)

💨 WIND (Things pushing us forward)     ⚓ ANCHOR (Things holding us back)
- Great collaboration                    - Legacy code complexity
- Automated testing                      - Manual deployment process
- Clear product vision                   - Unclear requirements

🪨 ROCKS (Risks ahead)
- Key team member leaving in 2 months
- Major dependency upgrade needed
- Peak traffic season approaching
```

### Action Item Tracking

```markdown
| Action Item | Owner | Due | Status |
|-------------|-------|-----|--------|
| Set up automated staging deployment | Alice | Sprint 6 | In Progress |
| Add acceptance criteria template to Jira | Bob (SM) | Sprint 6 | Done |
| Schedule pairing sessions for complex work | Carol | Ongoing | Done |
```

**Rule**: Maximum 3 action items per retrospective. More = nothing gets done.

## Backlog Refinement

### Definition of Ready (DoR)

A story is "ready" for sprint planning when:

```markdown
- [ ] User story follows format: "As a [role], I want [feature], so that [benefit]"
- [ ] Acceptance criteria defined (3-7 criteria)
- [ ] Story estimated by the team
- [ ] Dependencies identified and resolved (or plan in place)
- [ ] UX mockups attached (if UI work)
- [ ] Technical approach discussed
- [ ] Story fits within a single sprint
```

### Definition of Done (DoD)

A story is "done" when:

```markdown
- [ ] Code complete and passes all tests
- [ ] Code reviewed and approved (minimum 1 reviewer)
- [ ] Unit tests written (>= 80% coverage for new code)
- [ ] Integration tests passing
- [ ] Documentation updated (if applicable)
- [ ] Deployed to staging and verified
- [ ] Product Owner accepted
- [ ] No known defects
```

### INVEST Criteria for User Stories

| Criterion | Description |
|-----------|-------------|
| **I**ndependent | Can be developed in any order |
| **N**egotiable | Details can be discussed |
| **V**aluable | Delivers value to the user/customer |
| **E**stimable | Team can estimate the effort |
| **S**mall | Fits within a single sprint |
| **T**estable | Clear acceptance criteria |

## Kanban

### Core Principles

1. **Visualize the workflow** -- make all work visible on the board
2. **Limit Work in Progress (WIP)** -- stop starting, start finishing
3. **Manage flow** -- optimize throughput, reduce cycle time
4. **Make policies explicit** -- everyone knows the rules
5. **Implement feedback loops** -- regular reviews and retrospectives
6. **Improve collaboratively** -- evolve the process incrementally

### Kanban Board with WIP Limits

```
| Backlog | To Do (3) | In Progress (4) | Review (2) | Done |
|---------|-----------|-----------------|------------|------|
| Story H | Story E   | Story B [Alice] | Story A    | Story X |
| Story I | Story F   | Story C [Bob]   | Story D    | Story Y |
| Story J | Story G   | Story C [Carol] |            | Story Z |
|         |           |                 |            |       |
```

WIP limit violation: If "In Progress" already has 4 items and someone wants to start a new one, they must help finish an existing item first.

### Flow Metrics

| Metric | Definition | Target |
|--------|-----------|--------|
| **Lead Time** | Time from request to delivery | Minimize |
| **Cycle Time** | Time from work started to done | Track trend |
| **Throughput** | Items completed per time period | Stable or increasing |
| **WIP** | Number of items in progress | Within limits |
| **Block Time** | Time items spend blocked | Minimize |

### Cumulative Flow Diagram (CFD)

```
Items
80 |                              ____------- Done
   |                         ___--
60 |                    ___--     ___-------- Review
   |               ___--    ___--
40 |          ___--    ___--   ___----------- In Progress
   |     ___--    ___--   ___--
20 | ___--    ___--   ___--   ___------------ To Do
   |---------======----- (bands show WIP)
 0 |__________________________________________
   W1  W2  W3  W4  W5  W6  W7  W8  W9  W10
```

**Reading the CFD**:
- **Band width** = WIP in that stage
- **Horizontal distance** between bands = lead time
- **Slope** of Done line = throughput
- **Growing bands** = bottleneck forming

## Scaling Frameworks

### SAFe (Scaled Agile Framework)

```
Portfolio Level:  Epic Owners, Lean Portfolio Management
                      │
Program Level:    ART (Agile Release Train), PI Planning
                      │
Team Level:       Scrum/Kanban teams (5-11 people each)
```

**PI (Program Increment) Planning**: 2-day event every 8-12 weeks where all teams align on objectives, dependencies, and capacity.

### LeSS (Large-Scale Scrum)

- Same Scrum, just scaled with shared product backlog
- One Product Owner, multiple teams
- Joint Sprint Planning, separate Daily Standups, joint Sprint Review

### Nexus

- Framework for 3-9 Scrum teams
- Nexus Integration Team (NIT) manages cross-team dependencies
- Nexus Sprint Planning + Nexus Sprint Retrospective

### Comparison

| Aspect | SAFe | LeSS | Nexus |
|--------|------|------|-------|
| Complexity | High | Low | Medium |
| Teams | 50-125+ | 2-8 | 3-9 |
| Overhead | Significant | Minimal | Moderate |
| Best for | Large enterprises | Scaling pure Scrum | Mid-size scaling |

## Hybrid Approaches (Scrumban)

Combine Scrum ceremonies with Kanban flow:
- **From Scrum**: Sprint boundaries, retrospectives, planning
- **From Kanban**: WIP limits, flow metrics, pull-based work
- **Result**: Regular cadence + continuous improvement + focus on flow

## Best Practices

1. **Time-box ceremonies** -- Sprint Planning < 4 hours, Standup = 15 min, Retro < 90 min
2. **Limit WIP** -- whether Scrum or Kanban, too much in-progress work kills throughput
3. **Refine continuously** -- backlog refinement is ongoing, not a one-time event
4. **Measure velocity/throughput trends**, not individual sprint numbers
5. **Retrospective actions must be tracked** -- assign owner, due date, follow up
6. **Definition of Done is non-negotiable** -- do not lower the bar for velocity
7. **Sprint goals > story points** -- the goal gives the sprint meaning
8. **Protect the team from mid-sprint changes** -- unless truly urgent
9. **Demonstrate working software** -- not slides, not designs, working software
10. **Adapt the framework** to the team, not the team to the framework

## Anti-Patterns

1. **Zombie Scrum** -- doing all ceremonies but delivering no value
2. **Velocity as a performance metric** -- velocity measures capacity, not productivity
3. **Sprint commitment death march** -- punishing teams for not completing everything
4. **Skipping retrospectives** -- "we're too busy" = you are too busy not to retrospect
5. **100% utilization** -- leaves no slack for innovation, learning, or unexpected work
6. **Estimation precision theater** -- debating whether a story is 5 or 8 points for 20 minutes
7. **Backlog grooming as a spectator sport** -- whole team should participate
8. **No WIP limits** -- Kanban without WIP limits is just a task board, not Kanban

## Sources & References

- https://scrumguides.org/scrum-guide.html -- The Scrum Guide (2020)
- https://www.scrum.org/resources/what-is-scrum -- Scrum.org overview
- https://www.atlassian.com/agile/scrum -- Atlassian Scrum guide
- https://www.atlassian.com/agile/kanban -- Atlassian Kanban guide
- https://kanbanize.com/kanban-resources/getting-started/what-is-kanban -- Kanban introduction
- https://www.scaledagileframework.com/ -- SAFe framework reference
- https://less.works/ -- LeSS framework
- https://www.scrum.org/resources/nexus-guide -- Nexus Guide
- https://retromat.org/ -- Retrospective activity ideas
