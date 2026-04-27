---
name: domain-requirements
description: BA skill — scope analysis, BDD acceptance criteria, user story mapping, and requirements traceability
---

# Domain Requirements

## Purpose

Guide the BA agent in refining requirements through scope analysis, writing BDD acceptance criteria, user story mapping, traceability, and identifying non-functional requirements. Ensures feature specifications are complete, unambiguous, and testable.

## Scope Analysis

### Scope Document Template

Every feature should begin with a scope analysis that clearly defines boundaries, assumptions, and risks.

```markdown
## Feature: Bookmark Collections

### Summary
Allow users to organize bookmarks into named collections with optional descriptions.
Collections act as folders -- each bookmark can belong to at most one collection.

### In Scope
- Create, read, update, delete collections
- Assign bookmarks to a collection
- Remove bookmarks from a collection
- List bookmarks filtered by collection
- Collection name uniqueness per user

### Out of Scope
- Nested collections (folders within folders) -- deferred to v2
- Shared collections between users -- requires auth system first
- Collection templates -- nice-to-have, not MVP
- Drag-and-drop ordering within collections -- frontend concern, separate task

### Assumptions
- A bookmark can belong to at most one collection (not many)
- Collections are user-scoped (multi-user support assumed)
- Deleting a collection does NOT delete its bookmarks (bookmarks become uncollected)
- Collection names are case-insensitive for uniqueness ("Work" === "work")

### Risks
| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| Users want nested collections | High | Medium | Design schema to support later (parentId nullable) |
| Performance with many collections | Low | Low | Pagination, max 100 collections per user |
| Name conflicts during import | Medium | Low | Auto-suffix duplicates: "Work (2)" |

### Dependencies
- Requires existing bookmark CRUD (Ch1-Ch2)
- Auth system needed for user-scoped collections (Phase 3)

### Success Metrics
- Users can organize 100+ bookmarks into collections
- CRUD operations respond in < 200ms
- Zero data loss when deleting a collection
```

### Scope Analysis Checklist

```markdown
## Scope Analysis Review
- [ ] In-scope items are specific and actionable
- [ ] Out-of-scope items are documented (not just forgotten)
- [ ] Assumptions are stated explicitly
- [ ] Risks have mitigation strategies
- [ ] Dependencies are identified
- [ ] Success metrics are measurable
- [ ] No ambiguous terms ("easy to use", "performant", "secure")
```

## BDD Acceptance Criteria

### Given/When/Then Format

BDD (Behavior-Driven Development) acceptance criteria use the Given/When/Then pattern to describe behavior from the user's perspective.

```gherkin
Feature: Bookmark Collections

  Scenario: Create a new collection
    Given the user is authenticated
    When the user creates a collection with name "Work Resources"
    Then a collection is created with:
      | field     | value           |
      | name      | Work Resources  |
      | bookmarks | []              |
    And the response status is 201

  Scenario: Create collection with duplicate name
    Given the user has a collection named "Work Resources"
    When the user creates a collection with name "work resources"
    Then the response status is 409
    And the error message is "A collection with this name already exists"

  Scenario: Add bookmark to collection
    Given a collection "Work Resources" exists
    And a bookmark "TypeScript Docs" exists
    When the user adds "TypeScript Docs" to "Work Resources"
    Then the bookmark's collectionId matches "Work Resources"
    And the collection's bookmark count is 1

  Scenario: Delete collection preserves bookmarks
    Given a collection "Old Stuff" exists with 3 bookmarks
    When the user deletes the collection "Old Stuff"
    Then the collection is deleted
    And all 3 bookmarks still exist with collectionId set to null

  Scenario: List bookmarks by collection
    Given 5 bookmarks exist in collection "Work Resources"
    And 3 bookmarks exist in collection "Personal"
    And 2 bookmarks exist with no collection
    When the user lists bookmarks filtered by collection "Work Resources"
    Then exactly 5 bookmarks are returned
    And all returned bookmarks belong to "Work Resources"
```

### Writing Effective BDD Scenarios

#### Rules for Given (Preconditions)

```gherkin
# GOOD: Specific state setup
Given 10 bookmarks exist in the store
Given a user with email "jane@example.com" is registered
Given the bookmark "TypeScript Docs" has tags ["typescript", "docs"]

# BAD: Vague state
Given the system is ready
Given some data exists
Given the user is set up
```

#### Rules for When (Action)

```gherkin
# GOOD: Specific action with parameters
When the user sends POST /api/v1/bookmarks with { "url": "https://example.com", "title": "Example" }
When the user searches for "typescript"
When the user deletes bookmark with ID "bk_123"

# BAD: Vague action
When the user does something
When the feature is used
When the API is called
```

#### Rules for Then (Outcome)

```gherkin
# GOOD: Specific, verifiable outcome
Then the response status is 201
Then the response body contains { "id": "bk_*", "title": "Example" }
Then the bookmark count is 11
Then the error field "url" has message "is required"

# BAD: Vague outcome
Then it works
Then no errors occur
Then the user is happy
```

### Mapping BDD to Test Code

```typescript
// Scenario: Create a new collection
describe('POST /api/v1/collections', () => {
  it('should create a collection with valid name (201)', async () => {
    // Given: the user is authenticated (setup in beforeEach)

    // When
    const res = await request(app)
      .post('/api/v1/collections')
      .send({ name: 'Work Resources' });

    // Then
    expect(res.status).toBe(201);
    expect(res.body.name).toBe('Work Resources');
    expect(res.body.bookmarks).toEqual([]);
  });
});

// Scenario: Create collection with duplicate name
describe('POST /api/v1/collections (duplicate)', () => {
  it('should return 409 for duplicate name (case-insensitive)', async () => {
    // Given
    await request(app)
      .post('/api/v1/collections')
      .send({ name: 'Work Resources' });

    // When
    const res = await request(app)
      .post('/api/v1/collections')
      .send({ name: 'work resources' });

    // Then
    expect(res.status).toBe(409);
    expect(res.body.detail).toContain('already exists');
  });
});
```

## User Story Mapping

### Story Mapping Process

```
Step 1: Identify User Activities (top row)
────────────────────────────────────────────────────
│ Save Bookmarks │ Organize │ Find Bookmarks │ Share │
────────────────────────────────────────────────────

Step 2: Break into User Tasks (second row, left to right)
────────────────────────────────────────────────────
│ Add URL │ Edit │ Delete │ Tag │ Collect │ Search │
────────────────────────────────────────────────────

Step 3: Stack details below (priority top to bottom)
────────────────────────────────────────────────────
│ Add URL  │ Edit title │ Delete one  │ Add tag    │
│ Validate │ Edit desc  │ Bulk delete │ Remove tag │
│ Auto-    │ Edit URL   │ Undo delete │ Filter tag │
│  title   │            │             │            │
────────────────────────────────────────────────────
  ↑ MVP line (Walking Skeleton)
────────────────────────────────────────────────────
│ Import   │ Move to    │ Archive     │ Tag suggest│
│  file    │  category  │             │            │
────────────────────────────────────────────────────
  ↑ Release 2 line
```

### Walking Skeleton (MVP)

The walking skeleton is the thinnest possible end-to-end slice that proves the architecture works.

```markdown
## Walking Skeleton: LinkLoom

Minimum viable functionality:
1. Add a bookmark (URL + title) → stored in memory
2. List all bookmarks → returned as JSON array
3. View a single bookmark by ID
4. Delete a bookmark

What it proves:
- Express routing works
- In-memory store works
- JSON request/response works
- Test infrastructure works (vitest + supertest)
- Health check endpoint works

What it defers:
- Input validation (Phase 2)
- Tags and search (Phase 3)
- Frontend UI (Phase 4)
- Authentication (not in scope)
- Persistent storage (not in scope)
```

### MVP Identification Criteria

| Include in MVP | Defer |
|---------------|-------|
| Core CRUD operations | Advanced filtering |
| Basic validation | Bulk operations |
| Essential error handling | Import/export |
| Happy path + critical errors | Analytics/statistics |
| Health check | UI polish |
| Basic tests | Performance optimization |

## Requirements Traceability Matrix

Track requirements from origin through implementation and testing.

```markdown
## Traceability Matrix: Bookmark Collections

| Req ID | Requirement | Task ID | Test | Status |
|--------|------------|---------|------|--------|
| REQ-01 | Create collection | T001 | collections.test.ts:15 | Done |
| REQ-02 | List collections | T001 | collections.test.ts:32 | Done |
| REQ-03 | Update collection name | T002 | collections.test.ts:48 | In Progress |
| REQ-04 | Delete collection | T002 | collections.test.ts:65 | In Progress |
| REQ-05 | Assign bookmark to collection | T003 | collections.test.ts:82 | Todo |
| REQ-06 | Filter bookmarks by collection | T003 | bookmarks.test.ts:110 | Todo |
| REQ-07 | Unique collection names | T001 | collections.test.ts:25 | Done |
| REQ-08 | Delete collection preserves bookmarks | T002 | collections.test.ts:78 | In Progress |
```

### Traceability Rules

1. **Every requirement maps to at least one task**
2. **Every task maps to at least one test**
3. **No orphan requirements** -- if it matters, it has a task
4. **No orphan tests** -- every test verifies a requirement
5. **Update the matrix** as tasks are completed

## Impact Analysis

When a change is requested, analyze its impact across the system.

### Impact Analysis Template

```markdown
## Impact Analysis: Add collection ordering

### Requested Change
Allow users to reorder bookmarks within a collection (manual drag-and-drop ordering).

### Affected Components
| Component | Change Type | Effort |
|-----------|------------|--------|
| Collection store | Add `order` field to bookmark-collection join | M |
| Bookmark API | Add `position` param to assign/move | M |
| Collection API | Add reorder endpoint | M |
| Frontend | Add drag-and-drop UI | L |
| Tests | Add ordering tests | M |

### Data Model Impact
```json
// Current: No ordering
{ "bookmarkId": "bk_123", "collectionId": "col_456" }

// Proposed: Add position
{ "bookmarkId": "bk_123", "collectionId": "col_456", "position": 1 }
```

### API Impact
- New endpoint: `PATCH /collections/:id/reorder`
- Modified: `POST /collections/:id/bookmarks` (add `position` param)
- No breaking changes to existing endpoints

### Risk Assessment
- Gap renumbering on delete (positions: 1,2,3 → delete 2 → 1,3)
- Concurrent reorder conflicts
- Performance with large collections (100+ bookmarks)

### Recommendation
Defer to v2. Current MVP does not need ordering. Design schema to support it later
by adding a nullable `position` column with default null (unordered).
```

## Non-Functional Requirements (NFRs)

### NFR Categories and Examples

#### Performance

```markdown
- API response time p95 < 500ms for all CRUD operations
- API response time p95 < 1s for search endpoint
- Support 100 concurrent users without degradation
- Page load time < 2 seconds on 3G connection
```

#### Security

```markdown
- All API endpoints validate input using schema validation
- Passwords hashed with bcrypt (cost factor 12+)
- JWT access tokens expire within 15 minutes
- No sensitive data in error responses or logs
- HTTPS required in production
```

#### Scalability

```markdown
- System supports 10,000 bookmarks per user
- Database queries use indexes for all filtered/sorted fields
- Pagination limits prevent unbounded queries
- Stateless API enables horizontal scaling
```

#### Reliability

```markdown
- 99.9% uptime SLO (43 minutes downtime/month)
- Zero data loss on infrastructure failure
- Graceful degradation when external services are down
- Automated health checks with 30-second intervals
```

#### Usability

```markdown
- All forms provide inline validation feedback
- Error messages use plain language (no technical jargon)
- All interactive elements accessible via keyboard
- WCAG 2.1 AA compliance
```

### NFR Template

```markdown
## Non-Functional Requirement

**ID:** NFR-PERF-001
**Category:** Performance
**Requirement:** All API endpoints respond within 500ms at the 95th percentile under normal load (100 concurrent users).

**Rationale:** Slow API responses degrade user experience and can cause frontend timeouts.

**Measurement:** k6 load test with 100 virtual users over 5 minutes. 95th percentile response time extracted from results.

**Target:** p95 < 500ms
**Acceptable:** p95 < 1000ms
**Unacceptable:** p95 > 1000ms

**Verification:** Run `k6 run load-test.js` in CI pipeline.
```

## Stakeholder Identification

### RACI Matrix

| Role | Responsible | Accountable | Consulted | Informed |
|------|------------|-------------|-----------|----------|
| PM Agent | Task creation, prioritization | Feature delivery | - | - |
| BA Agent | Requirements analysis | - | Domain questions | - |
| Dev Agent | Implementation | - | Technical approach | - |
| QA Agent | Testing, verification | Quality assurance | Edge cases | - |
| Architect Agent | - | Technical direction | Design decisions | All changes |
| User/Client | - | - | Requirements | Progress, releases |

### Stakeholder Communication

```markdown
## Communication Plan

| Stakeholder | Information Need | Format | Frequency |
|------------|-----------------|--------|-----------|
| Dev Agent | Task details, ACs, technical hints | tasks.json | Per task |
| QA Agent | ACs, test scope, known risks | QA report template | Per task |
| PM Agent | Progress, blockers, scope changes | Status update | Per sprint |
| User | Feature availability, breaking changes | Release notes | Per release |
```

## Requirements Refinement Checklist

For each task in the PM's breakdown, verify:

```markdown
## Requirements Refinement

### Completeness
- [ ] All necessary steps included (no gaps in the flow)
- [ ] Error cases are covered (not just happy path)
- [ ] Edge cases identified (empty, null, boundary, concurrent)
- [ ] Data model changes specified
- [ ] API contract defined (method, URL, request/response format)

### Clarity
- [ ] Description is unambiguous (one interpretation only)
- [ ] AC uses specific values (status codes, field names, messages)
- [ ] No vague terms ("works correctly", "handles properly")
- [ ] Technical approach hinted (not mandated)

### Testability
- [ ] Each AC is independently verifiable
- [ ] Pass/fail criteria are objective
- [ ] Test data requirements are clear
- [ ] Expected error messages specified

### Consistency
- [ ] Tasks align with each other (no contradictions)
- [ ] Naming follows project conventions
- [ ] Response format matches existing API patterns
- [ ] Error format matches existing error patterns

### Feasibility
- [ ] Scope is realistic for one developer/session
- [ ] Under 200 LOC (including tests)
- [ ] Dependencies are available (predecessor tasks done)
- [ ] No blocked dependencies
```

## Best Practices

1. **Start with scope analysis** before any task creation
2. **Write BDD scenarios** for complex behavior -- they become both documentation and tests
3. **Map user stories** to identify the walking skeleton (MVP)
4. **Maintain traceability** from requirement to task to test
5. **Document NFRs** early -- they affect architecture decisions
6. **Analyze impact** before accepting change requests
7. **Explicitly state assumptions** -- unstated assumptions cause bugs
8. **Define "out of scope"** to prevent scope creep

## Anti-Patterns

- **Missing scope analysis**: Jump to implementation without understanding boundaries
- **Ambiguous ACs**: "The feature should work" is not testable
- **Gold plating**: Adding features not in the requirements
- **Undocumented assumptions**: "Everyone knows we need auth" -- write it down
- **NFRs as afterthought**: "Make it faster" after launch is 10x harder than designing for speed
- **No traceability**: Cannot tell which test covers which requirement
- **Big bang delivery**: No walking skeleton, entire feature delivered at once

## Sources & References

- Dan North - Introducing BDD: https://dannorth.net/introducing-bdd/
- Jeff Patton - User Story Mapping Book: https://www.jpattonassociates.com/user-story-mapping/
- Gherkin Reference: https://cucumber.io/docs/gherkin/reference/
- BABOK Guide (IIBA): https://www.iiba.org/business-analysis-body-of-knowledge/
- Martin Fowler - Given When Then: https://martinfowler.com/bliki/GivenWhenThen.html
- ISO 25010 - Software Quality Model: https://iso25000.com/index.php/en/iso-25000-standards/iso-25010
- Alistair Cockburn - Writing Effective Use Cases: https://www.oreilly.com/library/view/writing-effective-use/0201702258/
