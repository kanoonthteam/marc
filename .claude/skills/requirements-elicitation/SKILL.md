---
name: requirements-elicitation
description: Requirements gathering techniques including interviews, workshops, user story mapping, and prioritization frameworks
---

# Requirements Elicitation

## Overview

Requirements elicitation is the process of discovering, understanding, and documenting what stakeholders need from a system. Effective elicitation combines multiple techniques to capture explicit requirements, uncover implicit needs, and validate assumptions before development begins.

## Interview Techniques

### Structured Interviews

Pre-defined questions, consistent across all interviewees. Best for comparing responses across stakeholders.

```markdown
## Stakeholder Interview Guide - [Project Name]

### Interviewee: [Name, Role]
### Date: [YYYY-MM-DD]
### Interviewer: [Name]

### Section 1: Current State
1. Describe your current workflow for [process].
2. What tools do you currently use?
3. How much time do you spend on [activity] per week?
4. What are the biggest pain points in your current process?

### Section 2: Desired State
5. If you could change one thing about [process], what would it be?
6. What does success look like for this project?
7. What metrics would you use to measure improvement?

### Section 3: Constraints
8. Are there regulatory or compliance requirements?
9. What systems must this integrate with?
10. What is your timeline expectation?

### Section 4: Priorities
11. Rank these capabilities from most to least important:
    - [ ] Speed of processing
    - [ ] Data accuracy
    - [ ] User experience
    - [ ] Reporting capabilities
    - [ ] Integration with existing tools

### Notes
[Free-form notes from the interview]
```

### Semi-Structured Interviews

Starting questions with freedom to explore interesting threads. Best for discovery and understanding context.

```markdown
## Semi-Structured Interview Guide

### Opening Questions
1. Tell me about your role and how you interact with [system/process].
2. Walk me through a typical day involving [process].

### Exploration Prompts (use as conversation leads)
- "You mentioned [X]. Can you tell me more about that?"
- "What happens when [edge case]?"
- "How do you handle [exception/error]?"
- "Who else is involved when [scenario] occurs?"
- "What would happen if you could not do [action]?"

### Closing
- "Is there anything I should have asked but didn't?"
- "Who else should I talk to about this?"
```

### Interview Best Practices

1. **Record with permission** -- capture exact words, not paraphrased notes
2. **Ask "show me"** -- have users demonstrate their workflow
3. **Listen for workarounds** -- these reveal unmet needs
4. **Separate problem from solution** -- users often suggest solutions; dig for the underlying problem
5. **Interview diverse roles** -- managers, frontline users, and IT see different problems

## Workshop Facilitation

### JAD Sessions (Joint Application Design)

Structured workshops with business and IT stakeholders to define requirements collaboratively.

```markdown
## JAD Session Plan

**Objective**: Define requirements for Order Management Module
**Duration**: 4 hours (with breaks)
**Participants**: Product Owner, 2 Business Analysts, 3 Developers, 2 End Users, 1 QA
**Facilitator**: [Name]

### Agenda

| Time | Activity | Output |
|------|----------|--------|
| 9:00 | Introductions + ground rules | Alignment |
| 9:15 | Current process walkthrough | As-is understanding |
| 9:45 | Pain points identification (sticky notes) | Problem list |
| 10:15 | Break | |
| 10:30 | Priority voting (dot voting) | Top 10 problems |
| 10:45 | Solution brainstorming (top 5 problems) | Solution ideas |
| 11:30 | User story writing (groups of 3) | Draft user stories |
| 12:00 | Story review + estimation | Sized backlog |
| 12:30 | Next steps + action items | Plan |

### Ground Rules
- All ideas are valid during brainstorming
- One conversation at a time
- Decisions by consensus; facilitator breaks ties
- Park off-topic items in the "parking lot"
- Phones on silent
```

### Design Thinking Workshop

```markdown
## Design Thinking Workshop (Half Day)

### Phase 1: Empathize (45 min)
- Share user research findings
- Create empathy maps
  - What does the user SAY? THINK? FEEL? DO?
- Identify unspoken needs

### Phase 2: Define (30 min)
- Synthesize empathy maps into problem statements
- Format: "[User persona] needs [need] because [insight]"
- Vote on top 3 problem statements

### Phase 3: Ideate (45 min)
- Crazy 8s: 8 ideas in 8 minutes per person
- Share and cluster similar ideas
- Dot vote on most promising solutions

### Phase 4: Prototype (60 min)
- Paper prototypes of top 2 ideas
- Groups of 3-4 people per idea
- Focus on the core user flow

### Phase 5: Test (30 min)
- Each group tests with another group
- Capture feedback: what works, what confuses
- Iterate on the prototype
```

### User Story Mapping

```
                  Walking Skeleton (MVP)
                  ─────────────────────
User Activity:   Browse Products → Add to Cart → Checkout → Order Confirm
                      │                │           │           │
Release 1:       [View list]    [Add item]    [Basic form] [Email confirm]
(MVP)            [Search]       [View cart]   [Card payment]
                      │                │           │           │
Release 2:       [Filters]     [Update qty]  [Save address] [Order tracking]
                 [Categories]  [Wishlist]    [PayPal]       [PDF receipt]
                      │                │           │           │
Release 3:       [Recommendations][Gift wrap] [Split payment][Returns]
                 [Reviews]     [Save for later][Apple Pay] [Reorder]
```

**How to build a story map**:
1. List user activities across the top (the "backbone")
2. Break each activity into user tasks
3. Arrange tasks vertically by priority (highest = top)
4. Draw horizontal lines to define releases
5. The top slice is your MVP

## Observation & Ethnographic Research

### Contextual Inquiry

Observe users in their actual work environment performing real tasks.

```markdown
## Contextual Inquiry Plan

**Observer**: [Name]
**User**: [Name, Role]
**Location**: [Their workspace]
**Duration**: 2-3 hours

### Protocol
1. Ask user to perform their normal tasks
2. Observe without interrupting
3. Note: what they do, tools used, workarounds, frustrations
4. Ask clarifying questions during natural pauses
5. At the end, walk through your observations for validation

### Observation Notes Template

| Time | Action | Tool | Notes/Quote |
|------|--------|------|-------------|
| 9:05 | Opened spreadsheet | Excel | "I track everything here" |
| 9:10 | Copy-pasted data from email | Outlook + Excel | Manual process, 5 min |
| 9:15 | Searched for customer record | CRM | "Search never works, I use filters" |
| 9:22 | Called colleague for info | Phone | "Only Sarah knows this process" |
```

### Key Observations to Look For

- **Workarounds**: Users working around system limitations
- **Handoffs**: Where data moves between people/systems (error-prone)
- **Bottlenecks**: Where users wait or get stuck
- **Shadow systems**: Spreadsheets, sticky notes, personal tools
- **Communication patterns**: Who talks to whom, how often

## Prototyping for Validation

### Low-Fidelity Wireframes

```markdown
## Wireframe Review Checklist

- [ ] Shows all key user flows (not just happy path)
- [ ] Includes error states and empty states
- [ ] Labels all interactive elements
- [ ] Shows navigation between screens
- [ ] Reviewed with at least 3 stakeholders
- [ ] Feedback captured and categorized (must-have vs nice-to-have)
```

### Clickable Prototypes (Figma, Sketch)

```markdown
## Prototype Testing Plan

### Objective
Validate the checkout flow redesign with 5 users.

### Tasks for Users
1. "Find a laptop under $1000 and add it to your cart"
2. "Apply the discount code SAVE20 to your order"
3. "Complete the purchase using a saved payment method"

### Metrics to Capture
- Task completion rate (per task)
- Time to complete (per task)
- Number of errors/wrong clicks
- User satisfaction (1-5 rating)
- Verbal feedback (think-aloud protocol)

### Results Template
| User | Task 1 | Task 2 | Task 3 | Satisfaction | Key Feedback |
|------|--------|--------|--------|-------------|--------------|
| U1   | Pass (45s) | Pass (30s) | Pass (60s) | 4 | "Discount field hard to find" |
| U2   | Pass (50s) | Fail | Pass (90s) | 3 | "Expected discount on cart page" |
```

## Requirements Traceability Matrix (RTM)

```markdown
## Requirements Traceability Matrix

| Req ID | Requirement | Source | Priority | Design Doc | Test Case | Status |
|--------|-------------|--------|----------|-----------|-----------|--------|
| FR-001 | User can search products by keyword | Interview #3 | Must | DD-2.1 | TC-010 | Implemented |
| FR-002 | Search results load within 2 seconds | SLA doc | Must | DD-2.2 | TC-011 | In Progress |
| FR-003 | User can filter by price range | Workshop #1 | Should | DD-2.3 | TC-012 | Not Started |
| FR-004 | Search suggestions appear after 3 chars | Usability test | Could | DD-2.4 | TC-013 | Not Started |
| NFR-001 | System handles 1000 concurrent users | SLA doc | Must | DD-5.1 | TC-050 | Not Started |
| NFR-002 | Data encrypted at rest (AES-256) | Security policy | Must | DD-6.1 | TC-060 | Implemented |
```

**RTM Purpose**:
- Ensures every requirement has a source (why do we need this?)
- Links requirements to design and test artifacts
- Tracks implementation status
- Identifies untested or unimplemented requirements

## Prioritization Frameworks

### MoSCoW

| Category | Description | % of Scope |
|----------|-------------|------------|
| **Must have** | Non-negotiable, system fails without these | ~60% |
| **Should have** | Important but system works without them | ~20% |
| **Could have** | Nice to have, include if time permits | ~15% |
| **Won't have (this time)** | Explicitly out of scope for this release | ~5% |

### Kano Model

```
Customer Satisfaction
        ^
        |     * Delighters (unexpected features)
        |    *   → Not expected, but create excitement
        |   *    → Absence does not cause dissatisfaction
        |  *
   ─────+────────────────────→ Implementation
        |          *
        |        *   Performance (more is better)
        |      *     → Proportional to satisfaction
        |    *       → "How fast is the search?"
        |
        |  * * * * * * * * * *
        |                       Basic (must-have)
        |                       → Expected, absence = dissatisfaction
        |                       → "Can I log in?"
        v
```

**Kano Categories**:
- **Basic**: Must work (login, save data). Users only notice when they fail.
- **Performance**: Linear relationship (faster search = happier users).
- **Delighters**: Unexpected features that create "wow" (AI suggestions).
- **Indifferent**: Users do not care either way.
- **Reverse**: Features some users actively dislike.

### Weighted Scoring

```markdown
| Feature | Business Value (1-5) | User Impact (1-5) | Effort (1-5, inverted) | Risk (1-5, inverted) | Score |
|---------|---------------------|-------------------|----------------------|---------------------|-------|
| Search | 5 | 5 | 3 | 4 | 17 |
| Export | 3 | 2 | 4 | 5 | 14 |
| Dashboard | 4 | 4 | 2 | 3 | 13 |
| Notifications | 4 | 3 | 3 | 4 | 14 |
```

## Document Analysis

### Sources to Analyze

- Existing system documentation
- Business process documents
- Regulatory/compliance requirements
- Support tickets and bug reports (top complaints = requirements)
- Competitor product reviews
- Industry standards (ISO, WCAG)
- Previous project lessons learned

### Analysis Template

```markdown
## Document Analysis Summary

**Document**: [Title]
**Source**: [Author/Department]
**Date Analyzed**: [YYYY-MM-DD]

### Extracted Requirements
| # | Requirement | Type | Confidence | Notes |
|---|-------------|------|------------|-------|
| 1 | Must support PDF export | Functional | High | Explicitly stated |
| 2 | Audit trail required | Compliance | High | Regulatory requirement |
| 3 | Users prefer dark mode | UX | Medium | Inferred from feedback survey |

### Conflicts/Gaps Found
- Document mentions "real-time updates" but does not define latency threshold
- Conflict between marketing requirements (customizable) and security (locked-down)
```

## Requirements Specification Template

```markdown
# Software Requirements Specification

## 1. Introduction
### 1.1 Purpose
### 1.2 Scope
### 1.3 Definitions and Acronyms
### 1.4 References

## 2. Overall Description
### 2.1 Product Perspective
### 2.2 User Classes and Characteristics
### 2.3 Operating Environment
### 2.4 Constraints
### 2.5 Assumptions and Dependencies

## 3. Functional Requirements
### 3.1 [Feature Area 1]
#### 3.1.1 [Specific Requirement]
- **ID**: FR-001
- **Description**: [Clear, testable description]
- **Input**: [What the user provides]
- **Processing**: [What the system does]
- **Output**: [What the user sees/receives]
- **Priority**: Must/Should/Could
- **Acceptance Criteria**:
  1. Given [context], when [action], then [outcome]
  2. Given [context], when [action], then [outcome]

## 4. Non-Functional Requirements
### 4.1 Performance
### 4.2 Security
### 4.3 Reliability
### 4.4 Usability
### 4.5 Scalability

## 5. Interface Requirements
### 5.1 User Interfaces
### 5.2 API Interfaces
### 5.3 External System Interfaces

## Appendices
- A: User Interview Summaries
- B: Wireframes
- C: Glossary
```

## Best Practices

1. **Use multiple techniques** -- no single method captures all requirements
2. **Talk to real users**, not just managers who represent them
3. **Document the "why"** behind each requirement (the source, the problem)
4. **Validate requirements** with prototypes before building
5. **Keep a glossary** -- ensure everyone uses the same terms
6. **Capture non-functional requirements early** -- performance, security, accessibility
7. **Prioritize ruthlessly** -- everything cannot be a "Must Have"
8. **Trace requirements** from source through design to test
9. **Review and refine** requirements iteratively (they evolve)
10. **Record rejected requirements** with reasoning for future reference

## Anti-Patterns

1. **Requirements by committee** -- trying to please everyone results in bloated scope
2. **Solution masquerading as requirement** -- "We need a dropdown" vs "User needs to select a country"
3. **Gold plating** -- adding features nobody asked for
4. **Ambiguous requirements** -- "The system should be fast" (how fast?)
5. **No stakeholder sign-off** -- requirements change without formal agreement
6. **Requirement amnesia** -- losing track of why a requirement exists
7. **Skipping non-functional requirements** -- leads to performance and security surprises
8. **Analysis paralysis** -- perfect requirements never happen; iterate

## Sources & References

- https://www.iiba.org/business-analysis-resources/babok-guide/ -- BABOK Guide (IIBA)
- https://www.modernanalyst.com/Resources/Articles.aspx -- Modern Analyst resources
- https://www.jpattonassociates.com/user-story-mapping/ -- Jeff Patton's User Story Mapping
- https://www.interaction-design.org/literature/topics/design-thinking -- Design Thinking overview
- https://www.scrumalliance.org/community/articles/2014/march/stories-versus-themes-versus-epics -- Story/Epic/Theme definitions
- https://foldingburritos.com/blog/kano-model/ -- Kano Model explanation
- https://www.volere.org/templates/volere-requirements-specification-template/ -- Volere SRS template
