---
name: task-estimation
description: PM skill — dependency mapping, estimation techniques, risk assessment, and work breakdown
---

# Task Estimation

## Purpose

Guide the PM agent in mapping task dependencies, estimating effort, assessing risks, and breaking epics into manageable work items. Covers dependency DAGs, estimation techniques, spike tasks, and real-world breakdown examples.

## Dependency Mapping

### Directed Acyclic Graph (DAG)

Dependencies form a DAG. Tasks can only depend on tasks that come before them (no cycles allowed).

```
T001 (Store) ──┬──▶ T002 (Create) ──▶ T005 (Validation)
               │                              │
               ├──▶ T003 (List) ──────▶ T010 (Search)
               │                              │
               ├──▶ T004 (Get by ID) ──▶ T007 (Update)
               │                        ──▶ T008 (Delete)
               │
               └──▶ T009 (Tags) ──────▶ T010 (Search)
```

### Critical Path

The critical path is the longest chain of dependent tasks. It determines the minimum time to complete the feature.

```
Critical path: T001 → T002 → T005 → T009 → T010
Length: 5 tasks

Parallel paths:
  Path A: T001 → T003 → T010 (3 tasks)
  Path B: T001 → T004 → T007 (3 tasks)
  Path C: T001 → T004 → T008 (3 tasks)

Optimized schedule (3 parallel agents):
  Agent 1: T001 → T002 → T005 → T009 → T010
  Agent 2: T003 → T007 (starts after T001, T004)
  Agent 3: T004 → T008 (starts after T001)
  Total: 5 sequential steps (critical path length)
```

### Dependency Types

| Type | Notation | Example |
|------|----------|---------|
| **Finish-to-Start** | T002 depends on T001 | Create endpoint needs store first |
| **Shared dependency** | T003, T004 both depend on T001 | List and Get both need store |
| **Convergent** | T010 depends on T003 AND T009 | Search needs List + Tags |
| **No dependency** | T007 independent of T009 | Update and Tags can run in parallel |

### Dependency Declaration in tasks.json

```json
{
  "tasks": [
    { "id": "T001", "title": "Create bookmark store", "dependencies": [] },
    { "id": "T002", "title": "Add create endpoint", "dependencies": ["T001"] },
    { "id": "T003", "title": "Add list endpoint", "dependencies": ["T001"] },
    { "id": "T004", "title": "Add get-by-id endpoint", "dependencies": ["T001"] },
    { "id": "T005", "title": "Add input validation", "dependencies": ["T002"] },
    { "id": "T007", "title": "Add update endpoint", "dependencies": ["T004"] },
    { "id": "T008", "title": "Add delete endpoint", "dependencies": ["T004"] },
    { "id": "T009", "title": "Add tag system", "dependencies": ["T005"] },
    { "id": "T010", "title": "Add search endpoint", "dependencies": ["T003", "T009"] }
  ]
}
```

### Dependency Validation Rules

1. **No circular dependencies**: T001 -> T002 -> T001 is invalid
2. **All references must exist**: If T002 depends on T099, T099 must be defined
3. **Minimize dependency chains**: Long chains create bottlenecks
4. **Maximize parallelism**: Independent tasks should have no unnecessary dependencies
5. **Foundation first**: Store/model tasks should be the root of the DAG

## Estimation Techniques

### T-Shirt Sizing

Quick, relative estimation for initial planning. Map to approximate effort.

| Size | Effort | LOC (approx) | Complexity | Example |
|------|--------|--------------|-----------|---------|
| **XS** | < 30 min | < 30 | Trivial change | Fix typo, update config |
| **S** | 30 min - 1 hr | 30-80 | Simple, well-understood | Add simple endpoint |
| **M** | 1-3 hrs | 80-150 | Moderate, some decisions | Feature with validation |
| **L** | 3-5 hrs | 150-200 | Complex, multiple parts | Feature with multiple endpoints |
| **XL** | > 5 hrs | > 200 | Too big, must split | Multi-feature epic |

### Estimation Workflow

```
1. Read the task description and ACs
2. Identify the technical approach
3. List all files that need to change
4. Estimate LOC for each file
5. Add test LOC (typically 1-2x implementation LOC)
6. Assign T-shirt size
7. If XL → split the task

Example:
  Task: "Add bookmark search endpoint"
  Files:
    - src/routes/bookmarks.ts (+30 LOC)
    - src/store.ts (+20 LOC)
    - src/routes/bookmarks.test.ts (+60 LOC)
  Total: ~110 LOC → Size M
```

### Story Points (Fibonacci Scale)

Use Fibonacci numbers to reflect increasing uncertainty at larger sizes.

| Points | Meaning | Equivalent |
|--------|---------|------------|
| 1 | Trivial, done before | Config change, typo fix |
| 2 | Small, well-understood | Simple endpoint |
| 3 | Small-medium, minor unknowns | Endpoint + validation |
| 5 | Medium, some decisions needed | Feature with tests |
| 8 | Large, significant decisions | Multi-file feature |
| 13 | Very large, consider splitting | Feature spanning multiple modules |
| 21+ | Must split | Epic-level work |

### Velocity-Based Estimation

Track completed points over recent sprints to forecast capacity.

```
Sprint History:
  Sprint 1: 18 points completed
  Sprint 2: 22 points completed
  Sprint 3: 20 points completed
  Sprint 4: 16 points completed

Average velocity: 19 points/sprint
Planned sprint: 21 points → Slightly overcommitted, consider deferring 1 M-size task
```

### Three-Point Estimation

For uncertain tasks, estimate optimistic, most likely, and pessimistic scenarios.

```
Task: "Integrate third-party payment API"

Optimistic (O):   2 hours (API is straightforward, good docs)
Most Likely (M):  5 hours (Some quirks, need error handling)
Pessimistic (P): 12 hours (API has bugs, poor docs, auth issues)

PERT estimate = (O + 4M + P) / 6 = (2 + 20 + 12) / 6 = 5.7 hours
Standard deviation = (P - O) / 6 = (12 - 2) / 6 = 1.7 hours
```

## Risk Assessment

### Risk Identification

For each task or feature, identify technical and schedule risks.

```json
{
  "task_id": "T010",
  "title": "Add search endpoint",
  "risks": [
    {
      "description": "Search performance may degrade with large datasets",
      "likelihood": "medium",
      "impact": "high",
      "mitigation": "Implement pagination early, consider indexing strategy",
      "contingency": "Fall back to simple substring match if full-text search is too slow"
    },
    {
      "description": "Tag-based search may have edge cases with special characters",
      "likelihood": "low",
      "impact": "medium",
      "mitigation": "Add edge case tests for Unicode, emoji, special chars",
      "contingency": "Sanitize search input before matching"
    }
  ]
}
```

### Risk Matrix

```
              Impact
              Low    Medium   High
Likelihood
  High      │  M   │   H    │  H   │
  Medium    │  L   │   M    │  H   │
  Low       │  L   │   L    │  M   │

L = Accept (monitor)
M = Mitigate (plan ahead)
H = Avoid or heavily mitigate (spike task, prototype, fallback plan)
```

### Common Technical Risks

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|-----------|
| Third-party API changes | Medium | High | Pin API version, write integration tests |
| Performance bottleneck | Medium | High | Load test early, set SLAs |
| Complex state management | High | Medium | Clear state diagram, edge case tests |
| Database migration failure | Low | Critical | Test migration on prod-like data |
| Dependency vulnerability | Medium | Medium | Automated scanning (Dependabot, Snyk) |
| Scope creep | High | High | Clear ACs, explicit out-of-scope list |

### Risk Mitigation Actions

1. **Spike task**: Timeboxed exploration before committing to approach
2. **Prototype**: Build minimal proof of concept
3. **Fallback plan**: Define what to do if primary approach fails
4. **Early integration**: Test integration points before building full feature
5. **Feature flag**: Deploy behind flag, roll back if problems

## Spike Tasks

Timeboxed exploration tasks to reduce uncertainty before committing to an approach.

### Spike Template

```json
{
  "id": "S001",
  "title": "SPIKE: Evaluate full-text search approaches",
  "description": "Timebox: 2 hours. Evaluate full-text search options for bookmark search: (1) Simple regex/includes match on in-memory data, (2) Fuse.js fuzzy search, (3) MiniSearch library. Produce a recommendation with trade-offs.",
  "acceptance_criteria": [
    "Document evaluated at least 2 approaches with pros/cons",
    "Include code snippet demonstrating recommended approach",
    "Performance benchmark with 1000 bookmarks",
    "Recommendation with justification"
  ],
  "tags": ["backend", "node", "research"],
  "dependencies": [],
  "status": "todo",
  "timebox": "2h",
  "output": "Decision document (ADR or comment in task)"
}
```

### When to Spike

- Unknown technology or library
- Multiple viable approaches (need data to decide)
- Performance-critical decisions
- Integration with unfamiliar third-party APIs
- Architecture decisions with long-term impact

### Spike Rules

1. **Always timebox** -- 1-4 hours maximum
2. **Define clear output** -- recommendation document, not just "investigate"
3. **Spike produces knowledge, not code** -- code is throwaway
4. **Create follow-up tasks** based on spike findings
5. **Never skip the recommendation** -- the spike must end with a decision

## Epic-to-Task Hierarchy

### Three-Level Hierarchy

```
Epic (Feature)
  └── Story (User-visible behavior)
        └── Task (Developer work unit)

Example:
  Epic: Bookmark Management
    Story: As a user, I can save bookmarks
      Task: T001 - Create bookmark store
      Task: T002 - Add POST /bookmarks endpoint
      Task: T005 - Add input validation
    Story: As a user, I can organize bookmarks with tags
      Task: T009 - Add tag model and store
      Task: T010 - Add tag CRUD endpoints
      Task: T011 - Add tag filtering to list endpoint
    Story: As a user, I can search my bookmarks
      Task: T012 - Add search endpoint
      Task: T013 - Add search by title, URL, and tags
```

### Breakdown Process

```
1. Start with the Epic (the whole feature)
        │
2. Identify user-facing Stories (what users can do)
        │
3. For each Story, identify Tasks (what developers build)
        │
4. For each Task:
   a. Write title (verb + noun)
   b. Write 3-8 acceptance criteria
   c. Assign tags
   d. Map dependencies
   e. Estimate size
        │
5. Validate:
   - All tasks under 200 LOC?
   - Dependencies form valid DAG?
   - Every Story has error case tasks?
   - Foundation tasks exist for shared infrastructure?
```

## Real-World Breakdown Examples

### Example 1: Authentication Feature

```json
{
  "epic": "User Authentication",
  "tasks": [
    {
      "id": "T001",
      "title": "Create user store with password hashing",
      "tags": ["backend", "node", "auth"],
      "dependencies": [],
      "estimated_loc": 80,
      "acceptance_criteria": [
        "User store supports create, findByEmail, findById",
        "Passwords are hashed with bcrypt (cost factor 12)",
        "Plaintext password is never stored or returned"
      ]
    },
    {
      "id": "T002",
      "title": "Add user registration endpoint",
      "tags": ["backend", "node", "api", "auth"],
      "dependencies": ["T001"],
      "estimated_loc": 100,
      "acceptance_criteria": [
        "POST /api/v1/auth/register with {name, email, password} returns 201",
        "Returns 422 when email format is invalid",
        "Returns 422 when password is under 8 characters",
        "Returns 409 when email already exists",
        "Response does not include password or hash"
      ]
    },
    {
      "id": "T003",
      "title": "Add login endpoint with JWT tokens",
      "tags": ["backend", "node", "api", "auth"],
      "dependencies": ["T001"],
      "estimated_loc": 120,
      "acceptance_criteria": [
        "POST /api/v1/auth/login with {email, password} returns 200 with access and refresh tokens",
        "Access token expires in 15 minutes",
        "Refresh token expires in 7 days",
        "Returns 401 for invalid credentials (generic message, no email/password hint)",
        "Returns 422 when email or password is missing"
      ]
    },
    {
      "id": "T004",
      "title": "Add authentication middleware",
      "tags": ["backend", "node", "auth"],
      "dependencies": ["T003"],
      "estimated_loc": 60,
      "acceptance_criteria": [
        "Protected routes return 401 without Authorization header",
        "Protected routes return 401 with expired token",
        "Protected routes set req.user with token payload",
        "Token validation checks issuer and audience claims"
      ]
    },
    {
      "id": "T005",
      "title": "Add token refresh endpoint",
      "tags": ["backend", "node", "api", "auth"],
      "dependencies": ["T003"],
      "estimated_loc": 80,
      "acceptance_criteria": [
        "POST /api/v1/auth/refresh with {refreshToken} returns new token pair",
        "Old refresh token is invalidated after use",
        "Returns 401 for invalid or expired refresh token",
        "Returns 401 for already-used refresh token"
      ]
    }
  ]
}
```

### Example 2: CRUD Feature Breakdown

```json
{
  "epic": "Category Management for Bookmarks",
  "tasks": [
    {
      "id": "T001",
      "title": "Create category store",
      "tags": ["backend", "node"],
      "dependencies": [],
      "estimated_loc": 50,
      "acceptance_criteria": [
        "Store supports CRUD operations (create, findAll, findById, update, delete)",
        "Categories have id, name, color, createdAt fields",
        "Name must be unique (case-insensitive)"
      ]
    },
    {
      "id": "T002",
      "title": "Add category CRUD endpoints",
      "tags": ["backend", "node", "api"],
      "dependencies": ["T001"],
      "estimated_loc": 120,
      "acceptance_criteria": [
        "POST /api/v1/categories returns 201 with created category",
        "GET /api/v1/categories returns all categories",
        "GET /api/v1/categories/:id returns single category or 404",
        "PATCH /api/v1/categories/:id updates and returns category",
        "DELETE /api/v1/categories/:id returns 204",
        "Returns 422 for missing name",
        "Returns 409 for duplicate name"
      ]
    },
    {
      "id": "T003",
      "title": "Add category assignment to bookmarks",
      "tags": ["backend", "node", "api"],
      "dependencies": ["T001", "T002"],
      "estimated_loc": 80,
      "acceptance_criteria": [
        "POST /bookmarks accepts optional categoryId field",
        "PATCH /bookmarks/:id can update categoryId",
        "Returns 422 when categoryId references non-existent category",
        "GET /bookmarks includes category data in response",
        "GET /bookmarks?categoryId=xxx filters by category"
      ]
    }
  ]
}
```

## Work Breakdown Structure (WBS)

### WBS Template

```
1. Feature: [Epic Name]
   1.1 Phase 1: Foundation
       1.1.1 Data model / store
       1.1.2 Basic endpoints
       1.1.3 Foundation tests
   1.2 Phase 2: Business Logic
       1.2.1 Input validation
       1.2.2 Business rules
       1.2.3 Error handling
       1.2.4 Business logic tests
   1.3 Phase 3: Integration
       1.3.1 Authentication
       1.3.2 Related features
       1.3.3 Integration tests
   1.4 Phase 4: Polish
       1.4.1 Edge cases
       1.4.2 Performance
       1.4.3 Documentation
```

### WBS Rules

1. **100% Rule**: WBS must capture 100% of the work
2. **Mutually Exclusive**: No work item should appear in two places
3. **Outcome-oriented**: Focus on deliverables, not activities
4. **Appropriate detail**: Stop decomposing when items are estimable (S or M size)
5. **8/80 Rule**: Each work item should take between 8 hours (1 day) and 80 hours (2 weeks)

## Best Practices

1. **Estimate relative to known tasks** -- "this is similar to T002 which took 2 hours"
2. **Include test writing time** in estimates (often 50-100% of implementation time)
3. **Add buffer for unknowns** -- first-time integrations take 2x expected time
4. **Re-estimate after spikes** -- updated knowledge should improve accuracy
5. **Track actuals vs estimates** -- calibrate over time
6. **Identify the critical path early** -- focus parallelization there
7. **Make dependencies explicit** -- implicit dependencies cause blocked agents

## Anti-Patterns

- **Anchoring on first estimate** -- always consider multiple perspectives
- **Planning fallacy** -- tasks almost always take longer than estimated
- **Ignoring test time** -- "it's just 50 LOC" but tests add 100 more
- **Hidden dependencies** -- two tasks that modify the same file
- **Estimation without context** -- "add an endpoint" varies wildly by complexity
- **No spike for unknowns** -- guessing instead of investigating
- **Scope creep in estimates** -- estimating more than what the AC describes

## Sources & References

- Mike Cohn - Agile Estimating and Planning: https://www.mountaingoatsoftware.com/books/agile-estimating-and-planning
- PERT Estimation Technique: https://en.wikipedia.org/wiki/Program_evaluation_and_review_technique
- Martin Fowler - Estimation: https://martinfowler.com/bliki/PurposeOfEstimation.html
- Basecamp Shape Up - Betting Table: https://basecamp.com/shapeup/2.2-chapter-07
- PMI Work Breakdown Structure: https://www.pmi.org/learning/library/applying-work-breakdown-structure-project-lifecycle-6979
- No Estimates Movement: https://ronjeffries.com/xprog/articles/the-noestimates-movement/
