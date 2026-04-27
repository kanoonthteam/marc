---
name: domain-modeling
description: BA skill — DDD bounded contexts, event storming, aggregates, and domain patterns
---

# Domain Modeling

## Purpose

Guide the BA agent in modeling complex domains using Domain-Driven Design principles. Covers bounded contexts, ubiquitous language, aggregates, value objects, event storming, and common domain patterns.

## Domain-Driven Design Overview

### When to Use DDD

| Scenario | Use DDD? | Reason |
|----------|----------|--------|
| Complex business domain | Yes | DDD manages complexity through boundaries |
| Simple CRUD application | No | Over-engineering, just use basic patterns |
| Multiple teams/services | Yes | Bounded contexts align with team boundaries |
| Rapidly changing rules | Yes | Ubiquitous language keeps code aligned with domain |
| Greenfield project | Maybe | Start simple, introduce DDD as complexity grows |
| Legacy system | Yes | Strangler fig pattern with bounded contexts |

## Bounded Contexts

### Identification Process

A bounded context is a semantic boundary where a particular model is defined and applicable. The same word can mean different things in different contexts.

```
┌────────────────────────┐  ┌─────────────────────────┐
│   Catalog Context      │  │   Order Context          │
│                        │  │                          │
│  Product:              │  │  Product:                │
│   - name               │  │   - productId            │
│   - description        │  │   - quantity             │
│   - category           │  │   - unitPrice            │
│   - specifications     │  │   - lineTotal            │
│   - images             │  │                          │
│                        │  │  "Product" means the     │
│  "Product" means the   │  │   line item in an order  │
│   full catalog entry   │  │                          │
└────────────────────────┘  └─────────────────────────┘
```

### Context Mapping Patterns

```
┌──────────┐                    ┌──────────┐
│ Context A │───────────────────│ Context B │
└──────────┘    Relationship    └──────────┘

Relationships:
  Partnership:        Both teams align, co-evolve
  Shared Kernel:      Shared model subset, both own
  Customer-Supplier:  Upstream provides, downstream consumes
  Conformist:         Downstream adopts upstream's model as-is
  Anti-Corruption Layer: Downstream translates upstream's model
  Open Host Service:  Upstream provides well-defined API
  Published Language: Shared schema (OpenAPI, Protobuf, JSON Schema)
```

### Context Map Example: E-Commerce

```
┌───────────────┐    Published    ┌───────────────┐
│   Catalog     │───Language ────▶│   Search      │
│   Context     │   (Product     │   Context     │
│               │    Schema)     │               │
└───────┬───────┘                └───────────────┘
        │
   Customer-Supplier
        │
┌───────▼───────┐                ┌───────────────┐
│   Order       │    ACL ────────│   Payment     │
│   Context     │◀──────────────│   Context     │
│               │               │  (Stripe)     │
└───────┬───────┘                └───────────────┘
        │
   Partnership
        │
┌───────▼───────┐
│   Shipping    │
│   Context     │
└───────────────┘
```

### Anti-Corruption Layer (ACL)

When integrating with external systems, create a translation layer to protect your domain model.

```typescript
// External system returns different structure
interface StripePaymentResponse {
  id: string;
  amount_cents: number;
  currency: string;
  status: 'succeeded' | 'failed' | 'pending';
  created: number; // Unix timestamp
}

// Your domain model
interface Payment {
  id: string;
  externalId: string;
  amount: Money;
  status: PaymentStatus;
  processedAt: Date;
}

// ACL: Translate between external and domain
class PaymentTranslator {
  static fromStripe(stripe: StripePaymentResponse): Payment {
    return {
      id: generateId(),
      externalId: stripe.id,
      amount: Money.fromCents(stripe.amount_cents, stripe.currency),
      status: this.mapStatus(stripe.status),
      processedAt: new Date(stripe.created * 1000),
    };
  }

  private static mapStatus(stripeStatus: string): PaymentStatus {
    const mapping: Record<string, PaymentStatus> = {
      'succeeded': PaymentStatus.COMPLETED,
      'failed': PaymentStatus.FAILED,
      'pending': PaymentStatus.PENDING,
    };
    return mapping[stripeStatus] ?? PaymentStatus.UNKNOWN;
  }
}
```

## Ubiquitous Language

### Building a Glossary

Every bounded context should have a glossary of terms that the team (including non-developers) agrees on.

```markdown
## Glossary: Bookmark Management Context

| Term | Definition | NOT to be confused with |
|------|-----------|------------------------|
| **Bookmark** | A saved reference to a web URL with title and metadata | Browser bookmark (different scope) |
| **Collection** | A user-defined group of related bookmarks | Database collection (technical term) |
| **Tag** | A label attached to a bookmark for categorization | HTML tag (different domain) |
| **Archive** | Moving a bookmark to inactive status (not deleted) | File archive (zip/tar) |
| **Import** | Bulk creation of bookmarks from external source | Module import (code) |
| **Starred** | User marking a bookmark as favorite/important | GitHub star (different context) |
```

### Ubiquitous Language Rules

1. **Code uses domain terms**: Class names, method names, and variables reflect the glossary
2. **Developers and domain experts speak the same language**
3. **Ambiguous terms are disambiguated** by adding context
4. **The glossary evolves** as understanding deepens
5. **Every bounded context has its own language** -- same term can mean different things

### Language in Code

```typescript
// GOOD: Uses ubiquitous language
class BookmarkService {
  archive(bookmarkId: string): Bookmark { /* ... */ }
  star(bookmarkId: string): Bookmark { /* ... */ }
  addToCollection(bookmarkId: string, collectionId: string): void { /* ... */ }
}

// BAD: Technical jargon instead of domain language
class BookmarkService {
  softDelete(bookmarkId: string): Bookmark { /* ... */ }  // "archive" in domain
  setFlag(bookmarkId: string, flag: string): Bookmark { /* ... */ }  // "star" in domain
  createJunction(bookmarkId: string, groupId: string): void { /* ... */ }  // technical
}
```

## Aggregates and Value Objects

### Aggregate Design

An aggregate is a cluster of domain objects treated as a single unit for data changes. Only the aggregate root is referenced externally.

```typescript
// Aggregate Root: Order
class Order {
  private id: OrderId;
  private customerId: CustomerId;
  private items: OrderItem[];  // Entities within the aggregate
  private status: OrderStatus; // Value object
  private shippingAddress: Address; // Value object

  // All modifications go through the aggregate root
  addItem(product: ProductRef, quantity: number, unitPrice: Money): void {
    if (this.status !== OrderStatus.DRAFT) {
      throw new DomainError('Cannot add items to a non-draft order');
    }
    const item = new OrderItem(product, quantity, unitPrice);
    this.items.push(item);
  }

  removeItem(productId: string): void {
    if (this.status !== OrderStatus.DRAFT) {
      throw new DomainError('Cannot remove items from a non-draft order');
    }
    this.items = this.items.filter(i => i.productId !== productId);
  }

  submit(): void {
    if (this.items.length === 0) {
      throw new DomainError('Cannot submit an empty order');
    }
    this.status = OrderStatus.SUBMITTED;
    // Emit domain event
  }

  get total(): Money {
    return this.items.reduce(
      (sum, item) => sum.add(item.lineTotal),
      Money.zero('USD'),
    );
  }
}

// Value Object: Immutable, compared by value
class Money {
  constructor(
    readonly amount: number,
    readonly currency: string,
  ) {
    if (amount < 0) throw new DomainError('Amount cannot be negative');
  }

  add(other: Money): Money {
    if (this.currency !== other.currency) {
      throw new DomainError('Cannot add different currencies');
    }
    return new Money(this.amount + other.amount, this.currency);
  }

  equals(other: Money): boolean {
    return this.amount === other.amount && this.currency === other.currency;
  }

  static zero(currency: string): Money {
    return new Money(0, currency);
  }

  static fromCents(cents: number, currency: string): Money {
    return new Money(cents / 100, currency);
  }
}

// Value Object: Address
class Address {
  constructor(
    readonly street: string,
    readonly city: string,
    readonly state: string,
    readonly zip: string,
    readonly country: string,
  ) {
    if (!zip.match(/^\d{5}(-\d{4})?$/)) {
      throw new DomainError('Invalid ZIP code format');
    }
  }

  equals(other: Address): boolean {
    return this.street === other.street
      && this.city === other.city
      && this.state === other.state
      && this.zip === other.zip
      && this.country === other.country;
  }
}
```

### Aggregate Design Rules

1. **Reference aggregates by ID only** -- never hold a direct reference to another aggregate
2. **Keep aggregates small** -- fewer entities per aggregate means less contention
3. **One aggregate = one transaction** -- do not modify multiple aggregates in one transaction
4. **Use domain events** to coordinate between aggregates
5. **Eventual consistency** between aggregates is preferred over distributed transactions

### Entity vs Value Object

| Aspect | Entity | Value Object |
|--------|--------|-------------|
| Identity | Has unique ID | No identity, compared by value |
| Mutability | Can change over time | Immutable (create new instead) |
| Lifecycle | Created, modified, deleted | Created, replaced |
| Example | User, Order, Bookmark | Email, Money, Address, DateRange |
| Storage | Own table/collection | Embedded in entity or own table |

## Event Storming

### Facilitation Process

Event storming is a collaborative workshop technique to discover domain events, commands, and read models.

```
Step 1: Domain Events (orange sticky notes)
  What happened in the system?
  ┌─────────────────┐ ┌─────────────────┐ ┌─────────────────┐
  │ Bookmark Created │ │ Bookmark Tagged │ │ Bookmark Deleted │
  └─────────────────┘ └─────────────────┘ └─────────────────┘

Step 2: Commands (blue sticky notes)
  What triggered the event?
  ┌─────────────────┐     ┌─────────────────┐
  │ Create Bookmark │ ──▶ │ Bookmark Created │
  └─────────────────┘     └─────────────────┘

Step 3: Actors (yellow sticky notes)
  Who triggers the command?
  ┌───────┐   ┌─────────────────┐     ┌─────────────────┐
  │ User  │──▶│ Create Bookmark │ ──▶ │ Bookmark Created │
  └───────┘   └─────────────────┘     └─────────────────┘

Step 4: Read Models (green sticky notes)
  What information does the actor need to make a decision?
  ┌─────────────────┐   ┌───────┐   ┌─────────────────┐
  │ Bookmark List   │──▶│ User  │──▶│ Archive Bookmark│
  └─────────────────┘   └───────┘   └─────────────────┘

Step 5: Policies (lilac sticky notes)
  What automated rules react to events?
  ┌─────────────────┐     ┌──────────────────┐     ┌──────────────────┐
  │ Bookmark Created │ ──▶ │ Fetch Page Title │ ──▶ │ Title Updated    │
  └─────────────────┘     │ (Policy)         │     └──────────────────┘
                          └──────────────────┘

Step 6: External Systems (pink sticky notes)
  ┌─────────────────┐     ┌──────────────────┐
  │ Bookmark Created │ ──▶ │ URL Metadata     │
  └─────────────────┘     │ Service (ext)    │
                          └──────────────────┘
```

### Event Storming Output: Bookmark Management

```
Timeline (left to right):

[User]─▶[Create Bookmark]─▶{Bookmark Created}─▶[Fetch Metadata (policy)]─▶{Metadata Fetched}
                                  │
                                  └─▶[Update Stats (policy)]─▶{Stats Updated}

[User]─▶[Add Tag]─▶{Bookmark Tagged}
[User]─▶[Remove Tag]─▶{Bookmark Untagged}
[User]─▶[Archive Bookmark]─▶{Bookmark Archived}
[User]─▶[Delete Bookmark]─▶{Bookmark Deleted}─▶[Update Stats (policy)]─▶{Stats Updated}

[User]─▶[Search Bookmarks]◁─[Search Results (read model)]
[User]─▶[View Stats]◁─[Bookmark Stats (read model)]
[System]─▶[Import Bookmarks]─▶{Bookmarks Imported}─▶[Validate URLs (policy)]
```

### From Event Storming to Code

```typescript
// Domain Events (derived from orange stickies)
interface BookmarkCreated {
  type: 'bookmark.created';
  bookmarkId: string;
  url: string;
  title: string;
  createdBy: string;
  occurredAt: Date;
}

interface BookmarkTagged {
  type: 'bookmark.tagged';
  bookmarkId: string;
  tag: string;
  occurredAt: Date;
}

interface BookmarkArchived {
  type: 'bookmark.archived';
  bookmarkId: string;
  occurredAt: Date;
}

// Commands (derived from blue stickies)
interface CreateBookmarkCommand {
  url: string;
  title: string;
  description?: string;
  tags?: string[];
}

// Policies (derived from lilac stickies)
class MetadataFetchPolicy {
  async handle(event: BookmarkCreated): Promise<void> {
    const metadata = await this.urlMetadataService.fetch(event.url);
    await this.bookmarkStore.updateMetadata(event.bookmarkId, metadata);
  }
}

class StatsUpdatePolicy {
  async handle(event: BookmarkCreated | BookmarkDeleted): Promise<void> {
    await this.statsStore.recalculate();
  }
}
```

## Entity-Relationship Modeling

### ER Diagram (Text-Based)

```
┌──────────────┐        ┌──────────────┐
│   Bookmark   │        │   Category   │
│──────────────│        │──────────────│
│ id (PK)      │───┐    │ id (PK)      │
│ url          │   │    │ name         │
│ title        │   │    │ color        │
│ description  │   │    │ createdAt    │
│ categoryId(FK│◀──┘    └──────────────┘
│ createdAt    │
│ updatedAt    │        ┌──────────────┐
│ archivedAt   │        │     Tag      │
└──────┬───────┘        │──────────────│
       │                │ id (PK)      │
       │    N:M         │ name         │
       ├────────────────│ createdAt    │
       │                └──────────────┘
       │
┌──────▼───────┐
│ BookmarkTag  │
│──────────────│
│ bookmarkId(FK│
│ tagId (FK)   │
│ addedAt      │
└──────────────┘
```

### Relationship Types

| Relationship | Cardinality | Example |
|-------------|-------------|---------|
| One-to-One | 1:1 | User → Profile |
| One-to-Many | 1:N | Category → Bookmarks |
| Many-to-Many | N:M | Bookmarks ↔ Tags (via junction table) |
| Self-referential | 1:N | Category → SubCategories |

## Common Domain Patterns

### RBAC (Role-Based Access Control)

```typescript
// Roles and permissions
enum Permission {
  BOOKMARK_CREATE = 'bookmark:create',
  BOOKMARK_READ = 'bookmark:read',
  BOOKMARK_UPDATE = 'bookmark:update',
  BOOKMARK_DELETE = 'bookmark:delete',
  BOOKMARK_ADMIN = 'bookmark:admin',  // All bookmark permissions
  USER_MANAGE = 'user:manage',
}

const ROLE_PERMISSIONS: Record<string, Permission[]> = {
  viewer: [Permission.BOOKMARK_READ],
  editor: [Permission.BOOKMARK_CREATE, Permission.BOOKMARK_READ, Permission.BOOKMARK_UPDATE],
  admin: Object.values(Permission),
};

function authorize(requiredPermission: Permission) {
  return (req, res, next) => {
    const userPermissions = ROLE_PERMISSIONS[req.user.role] || [];
    if (!userPermissions.includes(requiredPermission)) {
      return res.status(403).json({ error: 'Insufficient permissions' });
    }
    next();
  };
}
```

### Multi-Tenancy

```typescript
// Row-level isolation (shared database)
class TenantAwareRepository {
  constructor(private tenantId: string, private db: Database) {}

  async findAll(): Promise<Bookmark[]> {
    // Always filter by tenant
    return this.db.query('SELECT * FROM bookmarks WHERE tenant_id = $1', [this.tenantId]);
  }

  async create(data: CreateBookmarkInput): Promise<Bookmark> {
    return this.db.query(
      'INSERT INTO bookmarks (url, title, tenant_id) VALUES ($1, $2, $3) RETURNING *',
      [data.url, data.title, this.tenantId],
    );
  }
}

// Middleware to set tenant context
function tenantMiddleware(req, res, next) {
  const tenantId = req.headers['x-tenant-id'];
  if (!tenantId) {
    return res.status(400).json({ error: 'X-Tenant-Id header required' });
  }
  req.tenantId = tenantId;
  next();
}
```

### State Machine (Lifecycle)

```typescript
// Bookmark lifecycle states
enum BookmarkStatus {
  ACTIVE = 'active',
  ARCHIVED = 'archived',
  DELETED = 'deleted',
}

// Valid state transitions
const TRANSITIONS: Record<BookmarkStatus, BookmarkStatus[]> = {
  [BookmarkStatus.ACTIVE]: [BookmarkStatus.ARCHIVED, BookmarkStatus.DELETED],
  [BookmarkStatus.ARCHIVED]: [BookmarkStatus.ACTIVE, BookmarkStatus.DELETED],
  [BookmarkStatus.DELETED]: [], // Terminal state
};

function transition(current: BookmarkStatus, target: BookmarkStatus): BookmarkStatus {
  const validTargets = TRANSITIONS[current];
  if (!validTargets.includes(target)) {
    throw new DomainError(
      `Cannot transition from ${current} to ${target}. Valid transitions: ${validTargets.join(', ')}`,
    );
  }
  return target;
}
```

## Best Practices

1. **Start with event storming** to discover the domain before writing code
2. **Define ubiquitous language** early and enforce it in code
3. **Keep bounded contexts independent** -- they should be deployable separately
4. **Use anti-corruption layers** when integrating with external systems
5. **Favor value objects** over primitives (Email, Money, URL instead of string)
6. **Model the domain, not the database** -- let the domain drive the schema
7. **Small aggregates** -- prefer referencing by ID over embedding

## Anti-Patterns

- **Anemic domain model**: Entities with only getters/setters, logic in services
- **Big ball of mud**: No bounded context boundaries, everything coupled
- **Shared database across contexts**: Leads to tight coupling, prevents independent evolution
- **Premature DDD**: Applying full DDD to a simple CRUD app
- **Missing ubiquitous language**: Developers use different terms than domain experts
- **God aggregate**: One massive aggregate that owns everything

## Sources & References

- Eric Evans - Domain-Driven Design: https://www.domainlanguage.com/ddd/
- Vaughn Vernon - Implementing Domain-Driven Design: https://www.oreilly.com/library/view/implementing-domain-driven-design/9780133039900/
- Alberto Brandolini - Event Storming: https://www.eventstorming.com/
- Martin Fowler - Bounded Context: https://martinfowler.com/bliki/BoundedContext.html
- Martin Fowler - Value Object: https://martinfowler.com/bliki/ValueObject.html
- Context Mapping Patterns: https://github.com/ddd-crew/context-mapping
- DDD Crew Starter Modelling Process: https://github.com/ddd-crew/ddd-starter-modelling-process
