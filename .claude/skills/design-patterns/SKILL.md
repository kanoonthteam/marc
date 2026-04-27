---
name: design-patterns
description: Architect skill — layered architecture, CQRS, event sourcing, saga patterns, and service design
---

# Design Patterns

## Purpose

Guide the Architect agent in selecting and applying appropriate architectural patterns. Covers layered architecture, CQRS, event sourcing, saga patterns, microservice trade-offs, dependency injection, and hexagonal architecture.

## Layered Architecture

### Standard Layers

```
┌───────────────────────────────────────────────┐
│              Presentation Layer               │
│    Routes, Controllers, Request/Response      │
│    Validation, Serialization, Error Format    │
├───────────────────────────────────────────────┤
│              Business Logic Layer             │
│    Services, Domain Models, Business Rules    │
│    Orchestration, State Machines, Policies    │
├───────────────────────────────────────────────┤
│              Data Access Layer                │
│    Repositories, Query Builders, ORM          │
│    Caching, External Service Clients          │
├───────────────────────────────────────────────┤
│              Infrastructure Layer             │
│    Database, Cache, Message Queue, File System│
│    HTTP Clients, Email, SMS                   │
└───────────────────────────────────────────────┘

Rules:
  - Each layer only depends on the layer below it
  - Never skip layers (routes should not call repositories directly)
  - Business logic layer has NO infrastructure dependencies
```

### Implementation Example

```typescript
// Presentation Layer: Route handler
// Responsibility: Parse request, call service, format response
router.post('/api/v1/bookmarks', validate(CreateBookmarkSchema), async (req, res) => {
  try {
    const bookmark = await bookmarkService.create(req.validatedBody);
    res.status(201).json(bookmark);
  } catch (error) {
    if (error instanceof DuplicateError) {
      return res.status(409).json(problemDetails(409, 'Conflict', error.message));
    }
    throw error; // Let error middleware handle
  }
});

// Business Logic Layer: Service
// Responsibility: Business rules, orchestration, validation
class BookmarkService {
  constructor(
    private bookmarkRepo: BookmarkRepository,
    private urlValidator: UrlValidator,
    private eventBus: EventBus,
  ) {}

  async create(input: CreateBookmarkInput): Promise<Bookmark> {
    // Business rule: Validate URL is reachable
    await this.urlValidator.validate(input.url);

    // Business rule: No duplicate URLs
    const existing = await this.bookmarkRepo.findByUrl(input.url);
    if (existing) {
      throw new DuplicateError(`Bookmark with URL ${input.url} already exists`);
    }

    // Create and persist
    const bookmark = Bookmark.create(input);
    await this.bookmarkRepo.save(bookmark);

    // Side effect: Emit event for other systems
    this.eventBus.emit('bookmark.created', bookmark);

    return bookmark;
  }
}

// Data Access Layer: Repository
// Responsibility: Data persistence, query construction
class BookmarkRepository {
  constructor(private db: Database) {}

  async save(bookmark: Bookmark): Promise<void> {
    await this.db.query(
      'INSERT INTO bookmarks (id, url, title, created_at) VALUES ($1, $2, $3, $4)',
      [bookmark.id, bookmark.url, bookmark.title, bookmark.createdAt],
    );
  }

  async findByUrl(url: string): Promise<Bookmark | null> {
    const row = await this.db.queryOne(
      'SELECT * FROM bookmarks WHERE url = $1',
      [url],
    );
    return row ? Bookmark.fromRow(row) : null;
  }

  async findById(id: string): Promise<Bookmark | null> {
    const row = await this.db.queryOne(
      'SELECT * FROM bookmarks WHERE id = $1',
      [id],
    );
    return row ? Bookmark.fromRow(row) : null;
  }
}
```

## CQRS (Command Query Responsibility Segregation)

### Concept

Separate the write model (commands) from the read model (queries). Each can be optimized independently.

```
                    ┌──────────────┐
  Commands ───────▶ │ Write Model  │ ───▶ Event Store / Database
  (Create, Update,  │ (Validation, │
   Delete)          │  Business    │
                    │  Rules)      │
                    └──────────────┘
                           │
                     Domain Events
                           │
                    ┌──────▼───────┐
                    │ Read Model   │ ───▶ Read-optimized Store
  Queries ────────▶ │ (Projections,│      (Denormalized, Cached)
  (List, Search,    │  Views)      │
   Report)          │              │
                    └──────────────┘
```

### Simple CQRS Implementation

```typescript
// Command: Write side
interface CreateBookmarkCommand {
  url: string;
  title: string;
  tags?: string[];
}

class BookmarkCommandHandler {
  constructor(
    private writeStore: BookmarkWriteStore,
    private eventBus: EventBus,
  ) {}

  async handleCreate(command: CreateBookmarkCommand): Promise<string> {
    // Validate
    const validUrl = await validateUrl(command.url);

    // Persist
    const bookmark = {
      id: generateId(),
      url: validUrl,
      title: command.title.trim(),
      tags: command.tags ?? [],
      createdAt: new Date(),
    };
    await this.writeStore.insert(bookmark);

    // Emit event for read model to update
    this.eventBus.emit('bookmark.created', bookmark);

    return bookmark.id;
  }
}

// Query: Read side
interface BookmarkListQuery {
  search?: string;
  tag?: string;
  page?: number;
  perPage?: number;
}

class BookmarkQueryHandler {
  constructor(private readStore: BookmarkReadStore) {}

  async handleList(query: BookmarkListQuery): Promise<PaginatedResult<BookmarkView>> {
    // Read from denormalized, optimized read store
    return this.readStore.search({
      search: query.search,
      tag: query.tag,
      page: query.page ?? 1,
      perPage: query.perPage ?? 20,
    });
  }
}

// Projection: Updates read model when events occur
class BookmarkProjection {
  constructor(private readStore: BookmarkReadStore) {}

  async handleBookmarkCreated(event: BookmarkCreatedEvent): Promise<void> {
    await this.readStore.upsert({
      id: event.bookmarkId,
      url: event.url,
      title: event.title,
      tags: event.tags,
      tagCount: event.tags.length,
      searchText: `${event.title} ${event.url} ${event.tags.join(' ')}`,
      createdAt: event.occurredAt,
    });
  }
}
```

### When to Use CQRS

| Use CQRS | Do Not Use CQRS |
|----------|----------------|
| Read and write patterns differ significantly | Simple CRUD with same read/write model |
| Need different storage for reads vs writes | Small application, single database |
| High read-to-write ratio (100:1) | Equal read/write ratio |
| Complex queries (search, analytics) | Simple list/get operations |
| Event-driven architecture | Synchronous request/response only |

## Event Sourcing

### Concept

Store the sequence of events that led to the current state, rather than the current state itself. The current state is derived by replaying events.

```
Event Store (append-only):
  1. BookmarkCreated { id: "bk_1", url: "https://ex.com", title: "Example", at: T1 }
  2. BookmarkTagged { id: "bk_1", tag: "reference", at: T2 }
  3. BookmarkUpdated { id: "bk_1", title: "Example (Updated)", at: T3 }
  4. BookmarkTagged { id: "bk_1", tag: "important", at: T4 }
  5. BookmarkUntagged { id: "bk_1", tag: "reference", at: T5 }

Current State (derived by replaying events):
  {
    id: "bk_1",
    url: "https://ex.com",
    title: "Example (Updated)",
    tags: ["important"],
    createdAt: T1,
    updatedAt: T3
  }
```

### Event Store Implementation

```typescript
interface DomainEvent {
  eventId: string;
  aggregateId: string;
  type: string;
  data: Record<string, unknown>;
  occurredAt: Date;
  version: number;  // For optimistic concurrency
}

class EventStore {
  private events: Map<string, DomainEvent[]> = new Map();

  async append(aggregateId: string, events: DomainEvent[], expectedVersion: number): Promise<void> {
    const existing = this.events.get(aggregateId) ?? [];

    // Optimistic concurrency check
    if (existing.length !== expectedVersion) {
      throw new ConcurrencyError(
        `Expected version ${expectedVersion}, but aggregate is at version ${existing.length}`,
      );
    }

    const versioned = events.map((e, i) => ({
      ...e,
      version: expectedVersion + i + 1,
    }));

    this.events.set(aggregateId, [...existing, ...versioned]);
  }

  async getEvents(aggregateId: string): Promise<DomainEvent[]> {
    return this.events.get(aggregateId) ?? [];
  }
}

// Rebuild aggregate from events
class BookmarkAggregate {
  private state: BookmarkState;
  private version: number = 0;

  static fromEvents(events: DomainEvent[]): BookmarkAggregate {
    const aggregate = new BookmarkAggregate();
    for (const event of events) {
      aggregate.apply(event);
    }
    return aggregate;
  }

  private apply(event: DomainEvent): void {
    switch (event.type) {
      case 'bookmark.created':
        this.state = {
          id: event.aggregateId,
          url: event.data.url as string,
          title: event.data.title as string,
          tags: [],
          createdAt: event.occurredAt,
        };
        break;
      case 'bookmark.tagged':
        this.state.tags.push(event.data.tag as string);
        break;
      case 'bookmark.untagged':
        this.state.tags = this.state.tags.filter(t => t !== event.data.tag);
        break;
      case 'bookmark.updated':
        Object.assign(this.state, event.data);
        break;
    }
    this.version = event.version;
  }
}
```

### Snapshots

For aggregates with many events, periodically save a snapshot to avoid replaying the entire history.

```typescript
class SnapshotStore {
  async save(aggregateId: string, state: BookmarkState, version: number): Promise<void> {
    // Save snapshot every 100 events
    await this.db.upsert('snapshots', {
      aggregateId,
      state: JSON.stringify(state),
      version,
    });
  }

  async load(aggregateId: string): Promise<{ state: BookmarkState; version: number } | null> {
    return this.db.findOne('snapshots', { aggregateId });
  }
}

// Rebuild: Load snapshot + replay events after snapshot
async function loadAggregate(aggregateId: string): Promise<BookmarkAggregate> {
  const snapshot = await snapshotStore.load(aggregateId);
  const events = snapshot
    ? await eventStore.getEventsSince(aggregateId, snapshot.version)
    : await eventStore.getEvents(aggregateId);

  return snapshot
    ? BookmarkAggregate.fromSnapshot(snapshot.state, events)
    : BookmarkAggregate.fromEvents(events);
}
```

### When to Use Event Sourcing

| Good Fit | Bad Fit |
|----------|---------|
| Audit trail required (finance, healthcare) | Simple CRUD applications |
| Need to reconstruct past states | Small datasets with simple logic |
| Complex domain with temporal queries | Team unfamiliar with event-driven patterns |
| Collaborative editing (multiple writers) | Strong consistency required everywhere |

## Saga Pattern

### Choreography (Event-Driven)

Each service listens for events and reacts. No central coordinator.

```
Order Service          Payment Service       Inventory Service
     │                      │                      │
     │ OrderCreated ───────▶│                      │
     │                      │ PaymentProcessed ───▶│
     │                      │                      │ InventoryReserved ──▶ Done
     │                      │                      │
     │                      │ PaymentFailed ──────▶│
     │◀── OrderCancelled ───│                      │ InventoryReleased
```

```typescript
// Choreography: Each service reacts to events
class PaymentService {
  constructor(private eventBus: EventBus) {
    eventBus.on('order.created', this.handleOrderCreated.bind(this));
  }

  async handleOrderCreated(event: OrderCreatedEvent): Promise<void> {
    try {
      const payment = await this.processPayment(event.orderId, event.total);
      this.eventBus.emit('payment.processed', {
        orderId: event.orderId,
        paymentId: payment.id,
      });
    } catch (error) {
      this.eventBus.emit('payment.failed', {
        orderId: event.orderId,
        reason: error.message,
      });
    }
  }
}
```

### Orchestration (Central Coordinator)

A saga orchestrator coordinates the steps and handles compensation.

```typescript
// Orchestration: Central saga coordinator
class OrderSaga {
  private steps: SagaStep[] = [
    {
      name: 'reserve-inventory',
      execute: (ctx) => this.inventoryService.reserve(ctx.orderId, ctx.items),
      compensate: (ctx) => this.inventoryService.release(ctx.orderId),
    },
    {
      name: 'process-payment',
      execute: (ctx) => this.paymentService.charge(ctx.orderId, ctx.total),
      compensate: (ctx) => this.paymentService.refund(ctx.paymentId),
    },
    {
      name: 'confirm-order',
      execute: (ctx) => this.orderService.confirm(ctx.orderId),
      compensate: (ctx) => this.orderService.cancel(ctx.orderId),
    },
  ];

  async execute(context: SagaContext): Promise<void> {
    const completedSteps: SagaStep[] = [];

    for (const step of this.steps) {
      try {
        await step.execute(context);
        completedSteps.push(step);
      } catch (error) {
        // Compensate in reverse order
        for (const completed of completedSteps.reverse()) {
          try {
            await completed.compensate(context);
          } catch (compensateError) {
            // Log and alert: compensation failed, manual intervention needed
            console.error(`Compensation failed for ${completed.name}`, compensateError);
          }
        }
        throw new SagaFailedError(step.name, error);
      }
    }
  }
}
```

### Choreography vs Orchestration

| Aspect | Choreography | Orchestration |
|--------|-------------|--------------|
| Coupling | Low (event-driven) | Medium (coordinator knows all steps) |
| Visibility | Hard to trace full flow | Easy to see full flow in one place |
| Complexity | Distributed logic | Centralized logic |
| Failure handling | Each service handles own | Coordinator handles all |
| Best for | Simple flows (2-3 steps) | Complex flows (4+ steps) |
| Debugging | Harder (follow events) | Easier (read orchestrator) |

## Strangler Fig Migration

Incrementally migrate from legacy to new system by wrapping the old system.

```
Phase 1: Proxy all traffic through the new system
┌─────────┐     ┌──────────────┐     ┌────────────┐
│ Client  │────▶│ New System   │────▶│ Legacy     │
│         │     │ (proxy only) │     │ System     │
└─────────┘     └──────────────┘     └────────────┘

Phase 2: Migrate features one at a time
┌─────────┐     ┌──────────────┐     ┌────────────┐
│ Client  │────▶│ New System   │  ┌─▶│ Legacy     │
│         │     │ /users (new) │  │  │ /orders    │
│         │     │ /orders ─────│──┘  │ /products  │
│         │     │ /products ───│─────│            │
└─────────┘     └──────────────┘     └────────────┘

Phase 3: All features migrated, remove legacy
┌─────────┐     ┌──────────────┐
│ Client  │────▶│ New System   │
│         │     │ (all routes) │
└─────────┘     └──────────────┘
```

## Microservices vs Monolith

### Decision Framework

| Factor | Monolith | Microservices |
|--------|---------|--------------|
| Team size | < 10 developers | 10+ developers, multiple teams |
| Deploy frequency | Weekly-monthly | Daily per service |
| Domain complexity | Single domain | Multiple bounded contexts |
| Scaling needs | Uniform | Independent per service |
| Operational maturity | Limited DevOps | Strong DevOps/platform team |
| Data consistency | Transactions needed | Eventual consistency acceptable |

### Modular Monolith (Recommended Starting Point)

```
┌──────────────────────────────────────────────┐
│                  Monolith                     │
│                                              │
│  ┌──────────────┐  ┌──────────────┐          │
│  │ Bookmarks    │  │ Collections  │          │
│  │ Module       │  │ Module       │          │
│  │  - routes    │  │  - routes    │          │
│  │  - service   │  │  - service   │          │
│  │  - store     │  │  - store     │          │
│  └──────┬───────┘  └──────┬───────┘          │
│         │  Public API      │                  │
│         │  (interface)     │                  │
│         └──────────────────┘                  │
│                                              │
│  Shared: Database, Auth middleware, Logger    │
└──────────────────────────────────────────────┘

Rules:
  - Modules communicate through public interfaces only
  - No cross-module database queries
  - Each module owns its own tables
  - Extract to microservice when needed (not before)
```

## Common Design Patterns

### Service Pattern

```typescript
// Service: Orchestrates business operations
class BookmarkService {
  constructor(
    private repo: BookmarkRepository,
    private validator: BookmarkValidator,
    private events: EventEmitter,
  ) {}

  async create(input: CreateInput): Promise<Result<Bookmark>> {
    const validation = this.validator.validate(input);
    if (!validation.success) {
      return Result.fail(validation.errors);
    }

    const bookmark = Bookmark.create(validation.data);
    await this.repo.save(bookmark);
    this.events.emit('bookmark.created', bookmark);

    return Result.ok(bookmark);
  }
}
```

### Repository Pattern

```typescript
// Abstract repository interface
interface BookmarkRepository {
  save(bookmark: Bookmark): Promise<void>;
  findById(id: string): Promise<Bookmark | null>;
  findByUrl(url: string): Promise<Bookmark | null>;
  findAll(filter: BookmarkFilter): Promise<PaginatedResult<Bookmark>>;
  delete(id: string): Promise<boolean>;
}

// Concrete implementation
class PostgresBookmarkRepository implements BookmarkRepository {
  constructor(private db: Pool) {}

  async save(bookmark: Bookmark): Promise<void> {
    await this.db.query(
      'INSERT INTO bookmarks (id, url, title) VALUES ($1, $2, $3) ON CONFLICT (id) DO UPDATE SET url=$2, title=$3',
      [bookmark.id, bookmark.url, bookmark.title],
    );
  }

  // ... other methods
}

// In-memory for testing
class InMemoryBookmarkRepository implements BookmarkRepository {
  private bookmarks = new Map<string, Bookmark>();

  async save(bookmark: Bookmark): Promise<void> {
    this.bookmarks.set(bookmark.id, bookmark);
  }

  // ... other methods
}
```

### Decorator Pattern

```typescript
// Add cross-cutting concerns without modifying the original
class CachedBookmarkRepository implements BookmarkRepository {
  constructor(
    private inner: BookmarkRepository,
    private cache: Cache,
  ) {}

  async findById(id: string): Promise<Bookmark | null> {
    const cached = await this.cache.get(`bookmark:${id}`);
    if (cached) return cached;

    const bookmark = await this.inner.findById(id);
    if (bookmark) {
      await this.cache.set(`bookmark:${id}`, bookmark, { ttl: 300 });
    }
    return bookmark;
  }

  async save(bookmark: Bookmark): Promise<void> {
    await this.inner.save(bookmark);
    await this.cache.invalidate(`bookmark:${bookmark.id}`);
  }
}

// Usage: Compose decorators
const repo = new CachedBookmarkRepository(
  new LoggingBookmarkRepository(
    new PostgresBookmarkRepository(db),
    logger,
  ),
  cache,
);
```

### Dependency Injection

```typescript
// Composition root: Wire all dependencies in one place
function createApp(config: AppConfig): Express {
  // Infrastructure
  const db = new Pool(config.database);
  const cache = new RedisCache(config.redis);
  const eventBus = new EventBus();

  // Repositories
  const bookmarkRepo = new CachedBookmarkRepository(
    new PostgresBookmarkRepository(db),
    cache,
  );
  const categoryRepo = new PostgresCategoryRepository(db);

  // Services
  const bookmarkService = new BookmarkService(bookmarkRepo, new UrlValidator(), eventBus);
  const categoryService = new CategoryService(categoryRepo);

  // Routes
  const app = express();
  app.use('/api/v1/bookmarks', createBookmarkRoutes(bookmarkService));
  app.use('/api/v1/categories', createCategoryRoutes(categoryService));

  return app;
}
```

## Hexagonal Architecture (Ports and Adapters)

```
                    ┌─────────────────────────────────┐
                    │         Application Core         │
    Driving        │                                   │        Driven
    Adapters       │  ┌───────────────────────────┐   │        Adapters
                    │  │    Domain Model           │   │
  ┌──────────┐     │  │    (Entities, Value Objects│   │     ┌──────────┐
  │ REST API │────▶│──│    Business Rules)         │──│────▶│ Database │
  └──────────┘     │  └───────────────────────────┘   │     └──────────┘
                    │                                   │
  ┌──────────┐     │  Ports (Interfaces):              │     ┌──────────┐
  │ CLI      │────▶│──  - BookmarkRepository (out)     │────▶│ Cache    │
  └──────────┘     │    - CreateBookmarkUseCase (in)   │     └──────────┘
                    │    - EventPublisher (out)         │
  ┌──────────┐     │    - UrlValidator (out)           │     ┌──────────┐
  │ GraphQL  │────▶│──                                 │────▶│ Email    │
  └──────────┘     │                                   │     └──────────┘
                    └─────────────────────────────────┘

Driving (Primary) Ports: How external actors use the application
Driven (Secondary) Ports: How the application uses external services
```

### Key Principle

The domain core has ZERO dependencies on infrastructure. All infrastructure is accessed through interfaces (ports) that are implemented by adapters.

## Best Practices

1. **Start with a modular monolith** -- extract services when there is a proven need
2. **Choose patterns based on complexity** -- simple apps do not need CQRS or event sourcing
3. **Favor composition over inheritance** -- use decorators and dependency injection
4. **One service = one responsibility** -- services should be focused
5. **Repository per aggregate** -- not per table
6. **Separate commands from queries** even without full CQRS
7. **Use the strangler fig pattern** for legacy migrations
8. **Define clear module boundaries** even within a monolith

## Anti-Patterns

- **Premature microservices**: Splitting before understanding the domain
- **Distributed monolith**: Microservices that are tightly coupled and must deploy together
- **Anemic domain model**: All logic in services, entities are just data holders
- **God service**: One service class with hundreds of methods
- **Leaky abstractions**: Repository returning database-specific objects
- **Big ball of mud**: No architectural boundaries, everything depends on everything

## Sources & References

- Martin Fowler - CQRS: https://martinfowler.com/bliki/CQRS.html
- Martin Fowler - Event Sourcing: https://martinfowler.com/eaaDev/EventSourcing.html
- Martin Fowler - Strangler Fig Application: https://martinfowler.com/bliki/StranglerFigApplication.html
- Alistair Cockburn - Hexagonal Architecture: https://alistair.cockburn.us/hexagonal-architecture/
- Chris Richardson - Microservices Patterns: https://microservices.io/patterns/
- Sam Newman - Building Microservices: https://samnewman.io/books/building_microservices_2nd_edition/
- Microsoft Architecture Guides: https://learn.microsoft.com/en-us/azure/architecture/patterns/
