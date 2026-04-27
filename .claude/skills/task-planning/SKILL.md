---
name: task-planning
description: PM skill — task templates, AC writing, phase planning, tag routing, and story slicing
---

# Task Planning

## Purpose

Guide the PM agent in creating well-structured task breakdowns from feature requests. Covers task template format, writing effective titles and acceptance criteria, phase planning, tag-based routing, sizing rules, and vertical slicing techniques.

## Task Template

Every task must follow this JSON structure for machine-readable consumption by the pipeline.

```json
{
  "id": "T001",
  "title": "Add bookmark creation endpoint",
  "description": "Implement POST /api/v1/bookmarks endpoint that accepts url and title, validates input, stores in the bookmark store, and returns the created bookmark with 201 status.",
  "acceptance_criteria": [
    "POST /api/v1/bookmarks with valid {url, title} returns 201 with created bookmark",
    "Response includes id, url, title, createdAt fields",
    "Returns 422 with field-level errors when url is missing",
    "Returns 422 with field-level errors when title is missing",
    "Returns 422 when url is not a valid URL format",
    "Title is trimmed of leading/trailing whitespace"
  ],
  "tags": ["backend", "node"],
  "dependencies": [],
  "status": "todo",
  "assignee": null,
  "phase": 1,
  "estimated_loc": 80
}
```

### Field Definitions

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique task ID (T001, T002, ...) |
| `title` | string | Yes | Actionable title (verb + noun), < 60 chars |
| `description` | string | Yes | Context, approach hints, technical details |
| `acceptance_criteria` | string[] | Yes | Testable conditions (3-8 per task) |
| `tags` | string[] | Yes | Routing tags for agent assignment |
| `dependencies` | string[] | Yes | IDs of tasks that must complete first |
| `status` | string | Yes | todo, in_progress, review, done |
| `assignee` | string | No | Agent assigned to this task |
| `phase` | number | No | Phase number (1-4) |
| `estimated_loc` | number | No | Estimated lines of code |

## Title Writing Guidelines

### Pattern: Verb + Specific Noun + Context

```
Good Titles:
  "Add bookmark creation endpoint"
  "Implement tag filtering for bookmarks"
  "Add cursor-based pagination to list endpoints"
  "Configure CORS for production domains"
  "Add input validation with Zod schemas"

Bad Titles:
  "Bookmarks"              → Not actionable
  "Fix stuff"              → Too vague
  "Work on the API"        → Too broad
  "Validate things"        → Unspecific
  "Task 3"                 → Not descriptive
```

### Title Rules

1. **Start with a verb**: Add, Implement, Create, Configure, Update, Fix, Refactor, Remove
2. **Be specific**: Include the resource/feature name
3. **Keep under 60 characters**
4. **Distinguish between**: Create (new), Update (change existing), Fix (bug)
5. **Include scope**: "Add email validation **to signup form**" not just "Add validation"

## Acceptance Criteria Guidelines

### INVEST Criteria for Each AC

Each acceptance criterion should be:
- **I**ndependent: Verifiable on its own
- **N**egotiable: Open to implementation approach
- **V**aluable: Delivers observable value
- **E**stimable: Scope is clear enough to estimate
- **S**mall: One testable statement
- **T**estable: Objectively verifiable (pass/fail)

### Writing Good ACs

```markdown
# Pattern 1: Direct assertion
"POST /api/v1/bookmarks with valid {url, title} returns 201"
"GET /api/v1/bookmarks returns array sorted by createdAt descending"
"Search is case-insensitive (searching 'React' matches 'react')"

# Pattern 2: Given/When/Then (for complex behavior)
"Given a bookmark exists, when DELETE /bookmarks/:id is called, then it returns 204"
"Given 50 bookmarks exist, when GET /bookmarks?page=3&perPage=20 is called, then 10 bookmarks are returned"

# Pattern 3: Negative cases
"Returns 422 with {field: 'url', message: 'is required'} when url is missing"
"Returns 404 when bookmark ID does not exist"
"Returns 409 when creating a bookmark with a duplicate URL"
```

### AC Writing Rules

1. **Include specific values**: Status codes, field names, messages
2. **Cover happy path AND error cases** in every task
3. **No vague terms**: Avoid "works correctly", "handles properly", "is fast"
4. **Be implementation-agnostic**: Describe what, not how
5. **3-8 ACs per task**: Too few means undertested; too many means task is too big
6. **Each AC maps to at least one test case**

### Good vs Bad ACs

```markdown
# GOOD (specific, testable)
- "POST /api/v1/bookmarks returns 201 with the created bookmark object"
- "Returns 400 with field-level errors when email is missing"
- "Password is hashed with bcrypt before storing"
- "List endpoint returns at most 100 items per page"

# BAD (vague, untestable)
- "User creation works"
- "Errors are handled"
- "Tests pass"
- "Performance is good"
- "API is RESTful"
```

## Phase Planning

Break large features into deployable phases. Each phase should be independently deployable and testable.

### Four-Phase Model

```
Phase 1: Foundation          Phase 2: Business Logic
┌──────────────────────┐    ┌──────────────────────┐
│ Data model            │    │ Validation rules      │
│ Basic CRUD endpoints  │    │ Business workflows    │
│ Store/repository      │    │ State transitions     │
│ Basic tests           │    │ Error handling        │
└──────────────────────┘    └──────────────────────┘
           ↓                            ↓
Phase 3: Integration         Phase 4: Polish
┌──────────────────────┐    ┌──────────────────────┐
│ Authentication        │    │ UI refinements        │
│ External services     │    │ Performance tuning    │
│ Notifications         │    │ Edge case handling    │
│ Webhooks              │    │ Documentation         │
└──────────────────────┘    └──────────────────────┘
```

### Phase Planning Example: Bookmark Feature

```json
{
  "feature": "Bookmark Management",
  "phases": [
    {
      "phase": 1,
      "name": "Foundation",
      "tasks": [
        { "id": "T001", "title": "Create bookmark store with Map-based storage" },
        { "id": "T002", "title": "Add POST /bookmarks endpoint", "dependencies": ["T001"] },
        { "id": "T003", "title": "Add GET /bookmarks list endpoint", "dependencies": ["T001"] },
        { "id": "T004", "title": "Add GET /bookmarks/:id endpoint", "dependencies": ["T001"] }
      ]
    },
    {
      "phase": 2,
      "name": "Business Logic",
      "tasks": [
        { "id": "T005", "title": "Add URL validation with Zod", "dependencies": ["T002"] },
        { "id": "T006", "title": "Add duplicate URL detection", "dependencies": ["T002"] },
        { "id": "T007", "title": "Add PUT/PATCH /bookmarks/:id", "dependencies": ["T004"] },
        { "id": "T008", "title": "Add DELETE /bookmarks/:id", "dependencies": ["T004"] }
      ]
    },
    {
      "phase": 3,
      "name": "Integration",
      "tasks": [
        { "id": "T009", "title": "Add tag system to bookmarks", "dependencies": ["T005"] },
        { "id": "T010", "title": "Add search endpoint", "dependencies": ["T003", "T009"] },
        { "id": "T011", "title": "Add pagination to list endpoint", "dependencies": ["T003"] }
      ]
    },
    {
      "phase": 4,
      "name": "Polish",
      "tasks": [
        { "id": "T012", "title": "Add bulk operations", "dependencies": ["T008"] },
        { "id": "T013", "title": "Add export/import functionality", "dependencies": ["T003"] },
        { "id": "T014", "title": "Add bookmark statistics endpoint", "dependencies": ["T003", "T009"] }
      ]
    }
  ]
}
```

## Tag Routing Matrix

Tags determine which agent receives the task. Use at least one stack tag and one area tag per task.

### Stack Tags (Primary Routing)

| Tag | Routes to Agent | When to Use |
|-----|----------------|-------------|
| `backend`, `node` | dev-node | Node.js/Express/Fastify backend |
| `backend`, `rails` | dev-rails | Ruby on Rails backend |
| `backend`, `odoo` | dev-odoo | Odoo module development |
| `backend`, `salesforce` | dev-salesforce | Salesforce Apex/Lightning |
| `frontend`, `react` | dev-react | React frontend |
| `mobile`, `flutter` | dev-flutter | Flutter mobile app |
| `devops`, `flyio` | devop-flyio | Fly.io deployment |
| `devops`, `aws` | devop-aws | AWS infrastructure |
| `devops`, `azure` | devop-azure | Azure infrastructure |
| `devops`, `gcloud` | devop-gcloud | Google Cloud infrastructure |
| `devops`, `firebase` | devop-firebase | Firebase services |

### Area Tags (Secondary)

| Tag | Description |
|-----|-------------|
| `api` | API endpoint work |
| `auth` | Authentication/authorization |
| `database` | Schema, migrations, queries |
| `testing` | Test infrastructure |
| `config` | Configuration and setup |
| `ui` | User interface |
| `docs` | Documentation |

### Tagging Examples

```json
// Backend API task
{ "tags": ["backend", "node", "api"] }

// Full-stack task (split into subtasks)
{ "tags": ["frontend", "react", "ui"] }
{ "tags": ["backend", "node", "api"] }

// Infrastructure task
{ "tags": ["devops", "aws", "config"] }
```

## Sizing Rules

### Task Size Guidelines

| Size | LOC | Duration | Characteristics |
|------|-----|----------|----------------|
| Small (S) | < 50 | < 1 hour | Single function, simple endpoint |
| Medium (M) | 50-150 | 1-3 hours | Feature with validation, tests |
| Large (L) | 150-200 | 3-5 hours | Complex feature, multiple files |
| Too Large | > 200 | > 5 hours | Must be split |

### Splitting Rules

1. **Under 200 LOC changes** per task (including tests)
2. **Completable in one session** by one developer
3. **Testable in isolation** -- no half-implemented features
4. **One concern per task** -- do not mix CRUD with validation with search
5. **If you need "and" in the title, split it**: "Add create AND search" becomes two tasks

### Splitting Strategies

```
Too big: "Implement bookmark management system"

Split by CRUD operation:
  T001: "Create bookmark store"
  T002: "Add create bookmark endpoint"
  T003: "Add list bookmarks endpoint"
  T004: "Add get bookmark by ID endpoint"
  T005: "Add update bookmark endpoint"
  T006: "Add delete bookmark endpoint"

Split by layer:
  T001: "Define bookmark data model and store"
  T002: "Implement bookmark API routes"
  T003: "Add bookmark input validation"

Split by feature slice (vertical):
  T001: "Add basic bookmark CRUD (create + list)"
  T002: "Add bookmark update and delete"
  T003: "Add bookmark search"
  T004: "Add bookmark tags"
```

## Vertical Slicing

### Thin Vertical Slices (Walking Skeleton)

Each slice delivers a complete feature path from API to storage. Start with the thinnest possible slice and expand.

```
Horizontal (BAD):                Vertical (GOOD):
┌───────────────────┐           ┌──┐ ┌──┐ ┌──┐ ┌──┐
│    All Routes     │           │R │ │R │ │R │ │R │
├───────────────────┤           │o │ │o │ │o │ │o │
│   All Validation  │           │u │ │u │ │u │ │u │
├───────────────────┤           │t │ │t │ │t │ │t │
│   All Services    │           │e │ │e │ │e │ │e │
├───────────────────┤           │  │ │  │ │  │ │  │
│    All Store      │           │+ │ │+ │ │+ │ │+ │
└───────────────────┘           │  │ │  │ │  │ │  │
                                │S │ │V │ │S │ │T │
                                │t │ │a │ │e │ │a │
                                │o │ │l │ │r │ │g │
                                │r │ │i │ │v │ │s │
                                │e │ │d │ │i │ │  │
                                │  │ │  │ │c │ │  │
                                └──┘ └──┘ └──┘ └──┘
                                Create List Search Tags
```

### Slice Example: User Registration Feature

```
Slice 1 (Walking Skeleton):
  - POST /users with name and email → saves to store → returns 201
  - Minimal validation (required fields only)
  - One happy path test

Slice 2 (Validation):
  - Email format validation
  - Password strength rules
  - Duplicate email detection
  - Error response with field-level details

Slice 3 (Integration):
  - Password hashing (bcrypt)
  - Welcome email trigger
  - Account activation flow

Slice 4 (Polish):
  - Rate limiting on signup
  - CAPTCHA integration
  - Profile picture upload
```

## Cross-Cutting Concern Identification

When planning tasks, identify concerns that span multiple tasks.

```markdown
## Cross-Cutting Concerns

| Concern | Tasks Affected | Strategy |
|---------|---------------|----------|
| Input validation | T002, T005, T007 | Shared validation middleware (T005) |
| Error response format | All API tasks | Shared error handler (Foundation) |
| Authentication | T003, T007, T008 | Auth middleware (Phase 3) |
| Logging | All tasks | Logger middleware (Foundation) |
| Pagination | T003, T010 | Shared pagination helper (T003) |
```

### Handling Cross-Cutting Concerns

1. **Create a foundation task** for shared infrastructure (middleware, helpers)
2. **Make it a dependency** for tasks that need it
3. **Do not duplicate** -- extract to shared modules
4. **Document in description** which shared modules to use

## Best Practices

1. **Start with the walking skeleton** -- minimal end-to-end slice first
2. **Each task is independently deployable** -- no half-finished features
3. **Write ACs before implementation** -- they define the contract
4. **Include error cases in every task** -- not just happy path
5. **Tag tasks for routing** -- minimum one stack tag per task
6. **Keep task descriptions concise** but include enough context
7. **Identify dependencies early** -- they determine task ordering
8. **Review tasks as a set** -- check for gaps, overlaps, and missing error handling

## Anti-Patterns

- **Horizontal slicing** -- "Do all models, then all routes, then all tests"
- **Tasks without ACs** -- "Implement bookmarks" (what does done mean?)
- **Vague descriptions** -- "Make it work like the wireframe shows"
- **God tasks** -- one task that implements an entire feature (> 500 LOC)
- **Missing error case ACs** -- only covering the happy path
- **Circular dependencies** -- T001 depends on T002 which depends on T001
- **Tagging everything as `backend`** -- use specific stack tags for routing

## Sources & References

- INVEST in Good Stories: https://xp123.com/articles/invest-in-good-stories-and-smart-tasks/
- Jeff Patton - User Story Mapping: https://www.jpattonassociates.com/user-story-mapping/
- Agile Alliance - Story Splitting: https://www.agilealliance.org/glossary/split/
- Mike Cohn - User Stories Applied: https://www.mountaingoatsoftware.com/books/user-stories-applied
- Basecamp Shape Up - Scoping: https://basecamp.com/shapeup/3.3-chapter-10
- Martin Fowler - User Story: https://martinfowler.com/bliki/UserStory.html
