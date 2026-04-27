---
name: design-review
description: Architect skill — architecture review, ADRs, C4 diagrams, database design, caching, and scalability
---

# Design Review

## Purpose

Guide the Architect agent in conducting architecture reviews, writing Architecture Decision Records (ADRs), creating C4 model documentation, designing database schemas, planning caching strategies, and evaluating scalability patterns.

## Architecture Review Template

### Review Checklist

For every significant change, conduct an architecture review using this template.

```markdown
## Architecture Review: [Feature/Change Name]

### Date: YYYY-MM-DD
### Reviewers: [names/roles]

### 1. Current State
- How does the system work today?
- What are the current limitations or pain points?
- What existing components are affected?

### 2. Proposed Changes
- What is being changed and why?
- New components being added
- Existing components being modified
- Components being removed

### 3. Impact Analysis

#### Files Affected
| File | Change Type | Risk |
|------|-----------|------|
| src/routes/bookmarks.ts | Modified | Low |
| src/services/search.ts | New | Medium |
| src/store.ts | Modified | High (shared) |

#### Dependencies
- New dependencies: [list with versions]
- Changed dependencies: [list]
- Removed dependencies: [list]

#### Data Model Changes
- New tables/collections: [list]
- Modified schemas: [list]
- Migration required: Yes/No

#### API Surface Changes
- New endpoints: [list]
- Modified endpoints: [list]
- Deprecated endpoints: [list]
- Breaking changes: Yes/No

### 4. Quality Assessment
- [ ] Follows existing architectural patterns
- [ ] No circular dependencies introduced
- [ ] Error handling covers all failure modes
- [ ] Logging/monitoring for new components
- [ ] Performance impact assessed
- [ ] Security impact assessed

### 5. Risks and Mitigations
| Risk | Probability | Impact | Mitigation |
|------|------------|--------|-----------|
| | | | |

### 6. Decision
- [ ] Approved as-is
- [ ] Approved with conditions: [conditions]
- [ ] Needs revision: [feedback]
- [ ] Rejected: [reason]
```

### Lightweight Review (for Small Changes)

```markdown
## Quick Review: [Change]

**Change**: [1-2 sentences]
**Files**: [list]
**Risk**: Low / Medium / High
**Breaking**: Yes / No
**Decision**: Approved / Needs changes

**Notes**: [Any concerns or suggestions]
```

## Architecture Decision Records (ADRs)

### ADR Template

ADRs document significant architectural decisions with context, so future developers understand why a decision was made.

```markdown
# ADR-001: Use In-Memory Store Instead of Database

## Status
Accepted

## Date
2025-01-15

## Context
LinkLoom is a learning project for Claude Code multi-agent workflows. We need
a data storage solution that:
- Works without external dependencies (no Docker, no database server)
- Is simple enough for beginners to understand
- Supports all CRUD operations
- Runs tests quickly

Options considered:
1. **PostgreSQL** - Production-grade, requires Docker or local install
2. **SQLite** - File-based, no server needed, but requires driver
3. **In-memory Map** - Zero dependencies, instant, lost on restart
4. **JSON file** - Persistent, no dependencies, but file locking issues

## Decision
Use an in-memory `Map<string, T>` store.

## Consequences

### Positive
- Zero external dependencies
- Tests run instantly (no DB setup/teardown)
- Simple to understand for learning purposes
- No migrations needed

### Negative
- Data is lost on server restart
- Cannot share state between processes/instances
- No SQL queries (must implement filtering/sorting in code)
- Not representative of production architecture

### Neutral
- Can be replaced with a database later using the Repository pattern
- Store interface abstracts the implementation
```

### ADR File Naming Convention

```
docs/adr/
├── 0001-use-in-memory-store.md
├── 0002-express-over-fastify.md
├── 0003-vitest-for-testing.md
├── 0004-cursor-based-pagination.md
└── 0005-zod-for-validation.md
```

### ADR Status Lifecycle

```
Proposed → Accepted → Superseded by ADR-XXX
                   → Deprecated
                   → Rejected
```

### When to Write an ADR

- Choosing a framework, library, or tool
- Selecting an architectural pattern (CQRS, event sourcing)
- Deciding on data storage strategy
- Choosing between build vs buy
- Changing an existing architectural decision
- Any decision that is hard to reverse later

### Real-World ADR Example

```markdown
# ADR-004: Cursor-Based Pagination for List Endpoints

## Status
Accepted

## Date
2025-03-20

## Context
Our list endpoints currently return all results. As the dataset grows,
this will cause:
- Slow response times
- High memory usage
- Poor UX (loading thousands of items)

We need pagination. Two main approaches:
1. **Offset-based** (page=2&perPage=20): Simple but inconsistent with
   concurrent writes, degrades at high offsets (OFFSET is O(n) in SQL)
2. **Cursor-based** (cursor=abc&limit=20): Consistent, efficient at any
   depth, but cannot jump to arbitrary page

## Decision
Use cursor-based pagination for all list endpoints.

Cursor format: Base64-encoded JSON with sort field values.
Default limit: 20. Maximum limit: 100.

Response format:
```json
{
  "data": [...],
  "meta": { "hasMore": true, "nextCursor": "eyJjcmVhdGVkQXQiOiIyMDI1LTAxLTAxIn0" }
}
```

## Consequences

### Positive
- Consistent results under concurrent writes
- O(1) performance regardless of page depth
- Natural fit for infinite scroll UI patterns
- Works well with our index on createdAt

### Negative
- Cannot show "Page 3 of 10" in UI
- More complex to implement than offset
- Cursor becomes invalid if sort order changes

### Mitigations
- Provide total count via separate COUNT query (cached)
- Include clear documentation for API consumers
- Validate cursor format and return 400 for invalid cursors
```

## C4 Model Documentation

### Level 1: System Context

Who uses the system and what external systems does it interact with?

```
┌─────────────────────────────────────────────────────────┐
│                  System Context                          │
│                                                          │
│   ┌──────┐         ┌──────────────┐                     │
│   │ User │────────▶│  LinkLoom    │                     │
│   │      │◀────────│  System      │                     │
│   └──────┘         └──────┬───────┘                     │
│                           │                              │
│                    ┌──────▼───────┐                      │
│                    │  URL         │                      │
│                    │  Metadata    │                      │
│                    │  Service     │                      │
│                    │  (external)  │                      │
│                    └──────────────┘                      │
└─────────────────────────────────────────────────────────┘

User → LinkLoom: Manages bookmarks via web browser
LinkLoom → URL Metadata Service: Fetches page titles and descriptions
```

### Level 2: Container Diagram

What are the major technical components?

```
┌─────────────────────────────────────────────────────────┐
│                  Container Diagram                       │
│                                                          │
│  ┌──────────────┐        ┌──────────────┐               │
│  │  Frontend     │──API──▶│  API Server  │               │
│  │  (HTML/CSS/JS)│◀──────│  (Express)   │               │
│  │  Port: 3000   │        │  Port: 3000  │               │
│  └──────────────┘        └──────┬───────┘               │
│                                  │                       │
│                          ┌───────▼──────┐                │
│                          │  In-Memory   │                │
│                          │  Store       │                │
│                          │  (Map<K,V>)  │                │
│                          └──────────────┘                │
│                                                          │
│  Technology Choices:                                     │
│  - Runtime: Node.js 22                                   │
│  - Framework: Express 4                                  │
│  - Language: TypeScript                                  │
│  - Testing: Vitest + Supertest                          │
│  - Storage: In-memory Map                               │
└─────────────────────────────────────────────────────────┘
```

### Level 3: Component Diagram

What are the major components inside each container?

```
┌─────────────────────────────────────────────────────────┐
│                  API Server Components                    │
│                                                          │
│  ┌────────────────────────────────┐                      │
│  │         Routes Layer           │                      │
│  │  bookmarks.ts  categories.ts   │                      │
│  │  labels.ts     activity.ts     │                      │
│  └────────────┬───────────────────┘                      │
│               │                                          │
│  ┌────────────▼───────────────────┐                      │
│  │       Middleware Layer          │                      │
│  │  validation  error-handler     │                      │
│  │  cors        static-files      │                      │
│  └────────────┬───────────────────┘                      │
│               │                                          │
│  ┌────────────▼───────────────────┐                      │
│  │         Store Layer            │                      │
│  │  bookmarkStore  categoryStore  │                      │
│  │  labelStore     activityStore  │                      │
│  └────────────────────────────────┘                      │
└─────────────────────────────────────────────────────────┘
```

### Level 4: Code (for critical components only)

Show class/module structure for the most important components.

```typescript
// Level 4: BookmarkStore interface and implementation
interface BookmarkStore {
  create(input: CreateBookmarkInput): Bookmark;
  findById(id: string): Bookmark | undefined;
  findAll(filter?: BookmarkFilter): Bookmark[];
  update(id: string, input: UpdateBookmarkInput): Bookmark | undefined;
  delete(id: string): boolean;
  search(query: string): Bookmark[];
}

class InMemoryBookmarkStore implements BookmarkStore {
  private bookmarks: Map<string, Bookmark> = new Map();
  // ... implementations
}
```

## Database Design

### Schema Design Principles

1. **Normalize for write-heavy** workloads (reduce duplication)
2. **Denormalize for read-heavy** workloads (reduce JOINs)
3. **Use appropriate data types** (do not store dates as strings)
4. **Foreign keys for referential integrity**
5. **Indexes on WHERE, JOIN, ORDER BY columns**

### Indexing Strategy

```sql
-- Primary key (auto-indexed)
CREATE TABLE bookmarks (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  url TEXT NOT NULL,
  title VARCHAR(200) NOT NULL,
  description TEXT,
  category_id UUID REFERENCES categories(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Unique constraint (creates unique index)
ALTER TABLE bookmarks ADD CONSTRAINT uq_bookmarks_url UNIQUE (url);

-- Foreign key lookup index
CREATE INDEX idx_bookmarks_category_id ON bookmarks(category_id);

-- Sort/range queries
CREATE INDEX idx_bookmarks_created_at ON bookmarks(created_at DESC);

-- Full-text search
CREATE INDEX idx_bookmarks_search ON bookmarks
  USING GIN (to_tsvector('english', title || ' ' || COALESCE(description, '')));

-- Composite index for filtered + sorted queries
CREATE INDEX idx_bookmarks_category_created ON bookmarks(category_id, created_at DESC);
```

### Index Guidelines

| Query Pattern | Index Type | Example |
|---------------|-----------|---------|
| Exact match (WHERE x = ?) | B-tree (default) | `INDEX ON bookmarks(url)` |
| Range (WHERE x > ?) | B-tree | `INDEX ON bookmarks(created_at)` |
| Full-text search | GIN | `INDEX USING GIN (to_tsvector(...))` |
| Array contains | GIN | `INDEX USING GIN (tags)` |
| JSON field | GIN | `INDEX USING GIN (metadata jsonb_path_ops)` |
| Sort | B-tree with direction | `INDEX ON bookmarks(created_at DESC)` |

### Partitioning

```sql
-- Partition by range (time-based)
CREATE TABLE events (
  id UUID NOT NULL,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL
) PARTITION BY RANGE (created_at);

-- Monthly partitions
CREATE TABLE events_2025_01 PARTITION OF events
  FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');
CREATE TABLE events_2025_02 PARTITION OF events
  FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');
```

### Migration Patterns

```typescript
// Migration: Forward and reverse
// migrations/001_create_bookmarks.ts
export async function up(db: Database): Promise<void> {
  await db.query(`
    CREATE TABLE bookmarks (
      id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
      url TEXT NOT NULL UNIQUE,
      title VARCHAR(200) NOT NULL,
      created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
    )
  `);
}

export async function down(db: Database): Promise<void> {
  await db.query('DROP TABLE IF EXISTS bookmarks');
}
```

**Migration Rules:**
1. **Migrations must be reversible** -- always implement `down`
2. **Never modify released migrations** -- create new ones
3. **Separate schema from data** migrations
4. **Test against production-like data** volume
5. **Backward-compatible changes** first, then data migration, then cleanup

## API Evolution and Versioning

### Non-Breaking Changes (Safe)

```
- Adding a new endpoint
- Adding optional fields to request body
- Adding fields to response body
- Adding optional query parameters
- Adding new enum values (if client ignores unknown)
```

### Breaking Changes (Require Version Bump)

```
- Removing an endpoint
- Removing a field from response
- Making an optional field required
- Changing field type (string → number)
- Changing field name
- Changing URL path structure
- Changing authentication method
```

### Versioning Strategy

```typescript
// URL path versioning
app.use('/api/v1', v1Routes);
app.use('/api/v2', v2Routes);

// Sunset old version
app.use('/api/v1', (req, res, next) => {
  res.set('Sunset', 'Sat, 01 Mar 2026 00:00:00 GMT');
  res.set('Deprecation', 'true');
  res.set('Link', '</api/v2>; rel="successor-version"');
  next();
}, v1Routes);
```

## Caching Architecture

### Cache Patterns

#### Cache-Aside (Lazy Loading)

Application checks cache first, loads from DB on miss.

```typescript
async function getBookmark(id: string): Promise<Bookmark | null> {
  // 1. Check cache
  const cached = await cache.get(`bookmark:${id}`);
  if (cached) return cached;

  // 2. Cache miss: Load from DB
  const bookmark = await db.bookmarks.findById(id);
  if (bookmark) {
    // 3. Populate cache
    await cache.set(`bookmark:${id}`, bookmark, { ttl: 300 }); // 5 min
  }

  return bookmark;
}

// Invalidate on write
async function updateBookmark(id: string, data: UpdateInput): Promise<Bookmark> {
  const updated = await db.bookmarks.update(id, data);
  await cache.delete(`bookmark:${id}`);
  await cache.delete('bookmark-list:*'); // Invalidate list caches
  return updated;
}
```

#### Write-Through

Write to cache and DB simultaneously. Cache is always up to date.

```typescript
async function createBookmark(data: CreateInput): Promise<Bookmark> {
  const bookmark = await db.bookmarks.create(data);
  // Write to cache immediately
  await cache.set(`bookmark:${bookmark.id}`, bookmark, { ttl: 300 });
  return bookmark;
}
```

#### Write-Behind (Write-Back)

Write to cache first, asynchronously persist to DB. Higher risk of data loss.

```typescript
// Only for high-throughput scenarios where slight data loss is acceptable
async function recordPageView(bookmarkId: string): Promise<void> {
  await cache.increment(`views:${bookmarkId}`);
  // Flush to DB every 60 seconds (background job)
}
```

### Cache Invalidation Strategies

| Strategy | When | Example |
|----------|------|---------|
| Time-based (TTL) | Data can be slightly stale | 5 min TTL on list queries |
| Event-based | Must be fresh after writes | Invalidate on create/update/delete |
| Version-based | Cache busting | `bookmark:v3:${id}` |
| Tag-based | Invalidate groups | Tag: "bookmarks" → invalidate all |

### Cache Key Design

```typescript
// Good cache key patterns
`bookmark:${id}`                    // Single resource
`bookmarks:page:${page}:per:${per}` // Paginated list
`bookmarks:user:${userId}:tag:${tag}` // Filtered list
`bookmarks:search:${hashOfQuery}`    // Search results
`bookmarks:count:${userId}`          // Computed value
```

## Scalability Patterns

### Horizontal Scaling

```
                    ┌──────────────┐
   Load Balancer ──▶│ Instance 1   │──▶ Database
                ──▶│ Instance 2   │──▶ (Primary)
                ──▶│ Instance 3   │     │
                    └──────────────┘     ├── Read Replica 1
                                        └── Read Replica 2
```

**Requirements for horizontal scaling:**
- Stateless application (no in-memory sessions)
- Shared state in external store (Redis, DB)
- Health check endpoint for load balancer
- Graceful shutdown handling

### Read Replicas

```typescript
// Route queries to read replicas, writes to primary
class BookmarkRepository {
  constructor(
    private primary: Pool,    // Write operations
    private replica: Pool,    // Read operations
  ) {}

  async create(data: CreateInput): Promise<Bookmark> {
    return this.primary.query(
      'INSERT INTO bookmarks (...) VALUES (...) RETURNING *',
      [...],
    );
  }

  async findAll(filter: Filter): Promise<Bookmark[]> {
    return this.replica.query(
      'SELECT * FROM bookmarks WHERE ...',
      [...],
    );
  }
}
```

### Scaling Decision Matrix

| Bottleneck | Solution |
|-----------|---------|
| CPU-bound (computation) | Horizontal scaling (more instances) |
| Memory-bound (large datasets) | Vertical scaling + caching (Redis) |
| I/O-bound (database) | Read replicas, connection pooling, caching |
| Storage-bound | Object storage (S3), database partitioning |
| Network-bound | CDN, edge caching, compression |

## Technology Selection Framework

### Evaluation Criteria

```markdown
## Technology Evaluation: [Technology Name]

### Criteria Matrix

| Criterion | Weight | Score (1-5) | Weighted |
|-----------|--------|-------------|----------|
| Fits use case | 30% | | |
| Team expertise | 20% | | |
| Community/ecosystem | 15% | | |
| Maintenance burden | 15% | | |
| Performance | 10% | | |
| Cost | 10% | | |
| **Total** | 100% | | |

### Evaluation
- **Strengths**: [list]
- **Weaknesses**: [list]
- **Risks**: [list]
- **Alternatives considered**: [list with reason for rejection]
- **Recommendation**: [chosen option with justification]
```

### Technology Radar Categories

| Category | Description | Action |
|----------|------------|--------|
| Adopt | Proven, recommended for production use | Use for new projects |
| Trial | Worth trying on non-critical projects | Prototype, evaluate |
| Assess | Interesting, monitor development | Research, stay informed |
| Hold | Not recommended for new projects | Migrate away over time |

## Best Practices

1. **Write ADRs for significant decisions** -- they save hours of "why did we do this?"
2. **Review architecture before implementation** -- catching issues early is 10x cheaper
3. **Use C4 diagrams** for communication -- different levels for different audiences
4. **Design database indexes for your queries** -- not for your tables
5. **Cache at the right level** -- application cache, CDN, or database query cache
6. **Plan for horizontal scaling** from day one -- stateless services, external state stores
7. **Version your API** from the start -- adding versioning later is a breaking change
8. **Prefer non-breaking changes** -- add fields, do not remove or rename

## Anti-Patterns

- **No ADRs**: "We chose MongoDB because... someone said so?"
- **Big design up front**: Spending weeks on architecture without building anything
- **Premature optimization**: Caching everything before measuring actual bottlenecks
- **God table**: One table with 50+ columns instead of proper normalization
- **Missing indexes**: Full table scans on every query
- **Cache without invalidation strategy**: Serving stale data indefinitely
- **Breaking API changes without versioning**: Surprise breaking changes for all consumers
- **Architecture astronaut**: Designing for scale of Google when you have 100 users

## Sources & References

- Michael Nygard - ADR Process: https://cognitect.com/blog/2011/11/15/documenting-architecture-decisions
- ADR GitHub Organization: https://adr.github.io/
- Simon Brown - C4 Model: https://c4model.com/
- Martin Fowler - Database Refactoring: https://martinfowler.com/books/refactoringDatabases.html
- AWS Caching Best Practices: https://aws.amazon.com/caching/best-practices/
- Google Cloud Architecture Framework: https://cloud.google.com/architecture/framework
- Use The Index Luke (SQL Indexing): https://use-the-index-luke.com/
- PostgreSQL Documentation - Partitioning: https://www.postgresql.org/docs/current/ddl-partitioning.html
