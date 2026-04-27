---
name: stakeholder-communication
description: Executive reporting, risk communication, RACI matrix, meeting facilitation, and change management
---

# Stakeholder Communication

## Overview

Effective stakeholder communication is the difference between successful project delivery and organizational friction. This skill covers executive reporting, risk management, status updates, meeting facilitation, and managing expectations across diverse stakeholder groups.

## Executive Reporting

### Status Dashboard (RAG Status)

```markdown
## Project Status Report - Week 12

**Project**: Payment Platform Migration
**Date**: 2025-03-21
**PM**: Jane Smith
**Overall Status**: 🟡 AMBER

### Module Status

| Module          | Status | Progress | Notes                          |
|-----------------|--------|----------|--------------------------------|
| API Migration   | 🟢     | 85%      | On track, 2 sprints remaining  |
| Data Migration  | 🟡     | 60%      | 1 week behind due to schema issues |
| UI Redesign     | 🟢     | 90%      | Feature complete, in QA        |
| Auth Integration| 🔴     | 30%      | Blocked on SSO provider setup  |

### RAG Definitions
- 🟢 GREEN: On track, no issues
- 🟡 AMBER: At risk, mitigation in progress
- 🔴 RED: Off track, escalation needed
```

### Milestone Tracking

```markdown
## Milestone Report

| Milestone                  | Planned Date | Forecast Date | Status |
|----------------------------|-------------|---------------|--------|
| M1: Architecture approved  | 2025-01-15  | 2025-01-15    | Done   |
| M2: API v1 complete        | 2025-02-28  | 2025-02-28    | Done   |
| M3: Data migration tested  | 2025-03-15  | 2025-03-22    | 🟡 1 week late |
| M4: UAT sign-off           | 2025-04-15  | 2025-04-22    | 🟡 At risk |
| M5: Production launch      | 2025-05-01  | 2025-05-08    | 🟡 At risk |

### Impact Assessment
M3 delay cascades to M4 and M5. Mitigation: Add 2 engineers
to data migration team for 2 weeks. Revised M5 date: May 8.
```

### Executive Summary Template

```markdown
## Executive Summary - [Project Name]
### Date: [YYYY-MM-DD]

**TL;DR**: [One sentence summarizing current state and any asks]

### Progress This Period
- [Achievement 1]
- [Achievement 2]
- [Achievement 3]

### Key Risks
1. **[Risk]**: [Impact]. Mitigation: [Action]. Owner: [Name].
2. **[Risk]**: [Impact]. Mitigation: [Action]. Owner: [Name].

### Decisions Needed
1. [Decision needed, options, recommendation]

### Budget Status
- Spent: $X / $Y budgeted (X%)
- Forecast: On track / Over by $Z

### Next Period Plan
- [Priority 1]
- [Priority 2]
- [Priority 3]
```

## Risk Communication

### Risk Register

```markdown
## Risk Register

| ID | Risk | Probability | Impact | Score | Mitigation | Owner | Status |
|----|------|-------------|--------|-------|-----------|-------|--------|
| R1 | SSO provider delays | High | High | 9 | Parallel implementation with fallback auth | Alice | Active |
| R2 | Data volume exceeds capacity | Medium | High | 6 | Load test at 2x projected volume | Bob | Monitoring |
| R3 | Key developer leaves | Low | High | 4 | Knowledge sharing sessions, documentation | Carol | Active |
| R4 | Regulatory change impacts scope | Low | Medium | 3 | Monthly regulatory review with legal | Dave | Watching |

### Scoring Matrix
          Impact
          Low(1) Med(2) High(3)
Prob High  3      6      9
     Med   2      4      6
     Low   1      2      3
```

### Escalation Criteria

```markdown
## When to Escalate

### To Engineering Manager
- Technical blocker unresolved for > 2 days
- Resource conflict between projects
- Quality gate failure on critical path

### To Director/VP
- Milestone at risk of > 2 week delay
- Budget overrun > 10%
- Dependency on another team unresolved for > 1 week

### To C-Level/Steering Committee
- Project completion date at risk
- Budget overrun > 20%
- Major scope change needed
- Regulatory or compliance risk identified

### Escalation Template
**What**: [Describe the issue]
**Impact**: [What happens if unresolved]
**Options**: [List 2-3 options with trade-offs]
**Recommendation**: [Your preferred option and why]
**Decision Needed By**: [Date]
```

## RACI Matrix

```markdown
## RACI Matrix - Payment Platform Migration

| Activity                    | PM   | Tech Lead | Dev Team | QA   | Product | Exec |
|-----------------------------|------|-----------|----------|------|---------|------|
| Architecture decisions      | C    | R         | C        | I    | C       | I    |
| Sprint planning             | A    | R         | R        | C    | C       | I    |
| Code development            | I    | C         | R        | I    | I       | -    |
| Code review                 | I    | A         | R        | I    | -       | -    |
| Test planning               | C    | C         | C        | R    | I       | -    |
| Test execution              | I    | I         | C        | R    | I       | -    |
| Release approval            | A    | R         | I        | R    | C       | I    |
| Stakeholder communication   | R    | C         | I        | I    | C       | A    |
| Budget management           | R    | C         | I        | I    | C       | A    |
| Risk management             | R    | C         | C        | C    | C       | I    |

### Legend
- **R** = Responsible (does the work)
- **A** = Accountable (owns the outcome, one per activity)
- **C** = Consulted (provides input before decision)
- **I** = Informed (notified after decision)
```

### RACI Rules

1. Every row must have exactly one A
2. Minimize the number of R's per row (ideally 1-2)
3. Too many C's slow decisions down
4. If everyone is R, nobody is accountable
5. Review quarterly and update as team composition changes

## Status Update Templates

### Weekly Status Update

```markdown
## Weekly Status - [Project Name]
### Week of [Date]

**Overall**: 🟢 On Track

### Completed This Week
- [x] Completed user authentication API endpoints
- [x] Passed security review for data encryption module
- [x] Resolved performance bottleneck in search (p95 from 2s to 200ms)

### In Progress
- [ ] Data migration scripts (70% complete, on track)
- [ ] UI component library update (50% complete, on track)

### Blockers / Risks
- **Blocker**: Waiting for SSL certificate from IT (raised 3 days ago)
  - **Impact**: Delays staging deployment by 2 days
  - **Mitigation**: Escalated to IT manager, expected resolution by Wednesday

### Plan for Next Week
- Complete data migration testing
- Begin UAT with pilot users
- Deploy to staging environment

### Metrics
| Metric | This Week | Last Week | Trend |
|--------|-----------|-----------|-------|
| Velocity | 42 pts | 38 pts | Up |
| Bug count | 3 | 7 | Down |
| Test coverage | 82% | 78% | Up |
```

### Monthly Executive Report

```markdown
## Monthly Report - [Project Name]
### Period: [Month Year]

### Executive Summary
[2-3 sentences: progress, challenges, outlook]

### Key Achievements
1. [Achievement with measurable outcome]
2. [Achievement with measurable outcome]
3. [Achievement with measurable outcome]

### Financial Summary
| Category | Budget | Actual | Variance |
|----------|--------|--------|----------|
| Personnel | $120K | $115K | -$5K (under) |
| Infrastructure | $30K | $35K | +$5K (over) |
| Licenses | $15K | $15K | On budget |
| **Total** | **$165K** | **$165K** | **On budget** |

### Risks & Issues (Top 3)
[See risk register for full list]

### Decisions for Steering Committee
1. **Approve scope change**: Add mobile support (+4 weeks, +$40K)
   - Option A: Add to current project (delays launch)
   - Option B: Phase 2 project (separate timeline)
   - **Recommendation**: Option B

### Next Month Plan
- [Key activity 1]
- [Key activity 2]
- [Key activity 3]
```

### Steering Committee Deck Outline

```
Slide 1: Title + Overall RAG Status
Slide 2: Progress vs Plan (milestone chart)
Slide 3: Budget Status (actual vs forecast)
Slide 4: Top 3 Risks with Mitigation Status
Slide 5: Decisions Needed (clear ask)
Slide 6: Next Period Priorities
```

## Meeting Facilitation

### Agenda Design

```markdown
## Meeting: Sprint Review - Payment Team
**Date**: 2025-03-21, 2:00-3:00 PM
**Attendees**: Dev team, Product, Design, Stakeholders
**Goal**: Review sprint deliverables, gather feedback

### Agenda

| Time | Topic | Owner | Type |
|------|-------|-------|------|
| 2:00 | Welcome + sprint goal recap | SM | Info (2 min) |
| 2:02 | Demo: Payment flow redesign | Alice | Demo (15 min) |
| 2:17 | Demo: Error handling improvements | Bob | Demo (10 min) |
| 2:27 | Stakeholder Q&A + feedback | All | Discussion (15 min) |
| 2:42 | Metrics review (velocity, quality) | SM | Info (5 min) |
| 2:47 | Upcoming priorities + dependencies | PO | Info (8 min) |
| 2:55 | Action items + wrap-up | SM | Decision (5 min) |

### Pre-Read
- [Link to sprint board]
- [Link to demo recording if asynchronous]
```

### Decision Tracking

```markdown
## Decision Log

| ID | Date | Decision | Context | Decided By | Impact |
|----|------|----------|---------|------------|--------|
| D1 | 2025-01-15 | Use PostgreSQL over MongoDB | Need ACID transactions for payments | Tech Lead + Architect | Database choice locked |
| D2 | 2025-02-01 | 2-week sprint cadence | Team preferred shorter feedback loops | Team + SM | Sprint schedule set |
| D3 | 2025-03-10 | Defer mobile support to Phase 2 | Budget constraint, web-first strategy | PO + Exec sponsor | Scope reduced, timeline held |
```

### Action Item Tracking

```markdown
## Action Items

| ID | Action | Owner | Due | Status | Source |
|----|--------|-------|-----|--------|--------|
| A1 | Schedule SSO vendor meeting | Jane | 03/25 | Open | Sprint Review 3/21 |
| A2 | Share load test results | Bob | 03/23 | Done | Risk review 3/20 |
| A3 | Update architecture diagram | Alice | 03/28 | In Progress | Design review 3/19 |
```

## Influence Without Authority

### Techniques

1. **Build relationships before you need them** -- regular 1:1s with key stakeholders
2. **Speak their language** -- executives care about revenue/risk, engineers care about architecture
3. **Bring data, not opinions** -- "Users report 40% longer checkout times" vs "I think it's slow"
4. **Make it easy to say yes** -- present clear options with a recommendation
5. **Acknowledge constraints** -- "I understand your team is at capacity, here's a minimal ask"
6. **Find mutual wins** -- frame requests as benefiting both parties
7. **Follow through** -- deliver on promises to build credibility
8. **Escalate constructively** -- "I need your help removing this blocker" not "Your team is blocking us"

### Stakeholder Mapping

```
                    High Influence
                         |
    Keep Satisfied ------+------ Manage Closely
    (Exec sponsor,       |      (Product Owner,
     CFO)                |       CTO, key users)
                         |
    Low Interest --------+-------- High Interest
                         |
    Monitor              |      Keep Informed
    (Legal, IT support)  |      (Dev team, QA,
                         |       adjacent teams)
                         |
                    Low Influence
```

## Managing Expectations

### Setting Expectations

```markdown
## Expectations Setting Checklist

- [ ] Scope clearly documented and agreed (what is IN and OUT)
- [ ] Timeline communicated as a range, not a single date
- [ ] Dependencies and assumptions documented
- [ ] Risk factors communicated upfront
- [ ] Definition of "done" agreed with stakeholders
- [ ] Communication cadence established (weekly updates, monthly reviews)
- [ ] Escalation path defined
- [ ] Change request process explained
```

### Change Request Process

```markdown
## Change Request Form

**CR Number**: CR-023
**Requested By**: [Name]
**Date**: [YYYY-MM-DD]

### Change Description
[What is being requested]

### Justification
[Why this change is needed]

### Impact Assessment
- **Timeline**: +X weeks / No impact
- **Budget**: +$X / No impact
- **Scope**: [What is added/removed]
- **Quality**: [Any trade-offs]
- **Resources**: [Additional people needed]

### Options
1. [Option A]: [pros and cons]
2. [Option B]: [pros and cons]
3. [Reject]: [consequences]

### Recommendation
[Recommended option with reasoning]

### Approval
- [ ] Product Owner
- [ ] Project Sponsor
- [ ] Technical Lead
```

## Communication Plan Template

```markdown
## Communication Plan - [Project Name]

| Audience | Content | Frequency | Channel | Owner |
|----------|---------|-----------|---------|-------|
| Steering Committee | Executive summary, decisions needed | Monthly | Meeting + Slides | PM |
| Product Owner | Sprint progress, blockers | Weekly | Standup + Slack | SM |
| Development Team | Technical decisions, priorities | Daily | Standup + Slack | Tech Lead |
| Stakeholders | Sprint demos, feature updates | Bi-weekly | Sprint Review | PM + PO |
| End Users | Release notes, feature announcements | Per release | Email + In-app | Product Marketing |
| Support Team | Known issues, workarounds | Weekly | Confluence + Slack | QA Lead |
| Executive Sponsor | RAG status, budget, risks | Bi-weekly | 1:1 meeting | PM |
```

## Best Practices

1. **Adapt your message to the audience** -- executives want outcomes, engineers want details
2. **Lead with the headline** -- state the conclusion first, provide detail if asked
3. **Be transparent about risks** -- surprises erode trust faster than bad news
4. **Use visuals** -- charts and diagrams communicate faster than paragraphs
5. **Follow up in writing** -- verbal agreements are forgotten; written records are referenced
6. **Regular cadence builds trust** -- consistent updates prevent "what's going on?" escalations
7. **Acknowledge what you don't know** -- "I'll find out" is better than guessing
8. **Celebrate milestones** -- acknowledge team achievements publicly
9. **Manage up proactively** -- your manager should never be surprised
10. **Over-communicate during risk** -- increase frequency when things are going wrong

## Anti-Patterns

1. **Hiding bad news** -- it always comes out, and trust is destroyed
2. **Status reports nobody reads** -- if no one reads it, change the format or stop writing it
3. **RACI without accountability** -- a matrix on paper means nothing without enforcement
4. **Meeting without agenda** -- wastes everyone's time
5. **Decisions without documentation** -- leads to "I thought we agreed on X"
6. **Stakeholder surprises** -- the launch is delayed and they find out the day of
7. **One-size-fits-all communication** -- same report for engineers and executives
8. **No escalation path** -- issues fester when people don't know how to escalate

## Sources & References

- https://www.pmi.org/learning/library/stakeholder-communication-project-success-6066 -- PMI stakeholder communication
- https://www.atlassian.com/team-playbook/plays/daci -- Atlassian DACI framework
- https://www.smartsheet.com/raci-matrix -- RACI matrix guide
- https://hbr.org/topic/subject/communication -- Harvard Business Review on communication
- https://www.svpg.com/inspired-how-to-create-tech-products-customers-love/ -- SVPG product management
- https://www.mountaingoatsoftware.com/agile/user-stories -- User stories and communication
