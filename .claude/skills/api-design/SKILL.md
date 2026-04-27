---
name: api-design
description: RESTful API design, OpenAPI specification, pagination strategies, and response format conventions
---

# API Design Best Practices

## Purpose

Guide agents in designing consistent, well-documented REST APIs following industry conventions. Covers URL design, HTTP semantics, pagination strategies, response formats, and OpenAPI specification.

## RESTful URL Design

### Core Patterns

```
GET    /api/v1/users              # List users (collection)
POST   /api/v1/users              # Create user
GET    /api/v1/users/:id          # Get single user (resource)
PUT    /api/v1/users/:id          # Full replace user
PATCH  /api/v1/users/:id          # Partial update user
DELETE /api/v1/users/:id          # Delete user

# Nested resources (max 2 levels deep)
GET    /api/v1/users/:id/posts           # List user's posts
POST   /api/v1/users/:id/posts           # Create post for user
GET    /api/v1/users/:id/posts/:postId   # Get specific post

# Actions that don't map to CRUD (use verbs as exception)
POST   /api/v1/users/:id/activate        # Trigger action
POST   /api/v1/orders/:id/cancel         # Trigger action
POST   /api/v1/reports/generate          # Trigger async operation
```

### URL Design Rules

1. Use **plural nouns** for resources: `/users`, not `/user`
2. Use **kebab-case** for multi-word URLs: `/user-profiles`, not `/userProfiles`
3. Use **camelCase** for JSON fields: `createdAt`, not `created_at`
4. Keep nesting **shallow** (max 2 levels)
5. Use **query parameters** for filtering, not path segments
6. Use **ISO 8601** for dates: `2025-01-15T10:30:00Z`
7. Use **UUIDs or nanoids** for resource IDs, not sequential integers (avoid information leakage)

## HTTP Methods Reference

| Method | Idempotent | Safe | Request Body | Response Body | Use Case |
|--------|-----------|------|-------------|---------------|----------|
| GET | Yes | Yes | No | Yes | Read resource(s) |
| POST | No | No | Yes | Yes | Create resource, trigger action |
| PUT | Yes | No | Yes | Yes | Full replacement |
| PATCH | No* | No | Yes | Yes | Partial update |
| DELETE | Yes | No | No | Optional | Remove resource |
| HEAD | Yes | Yes | No | No | Check existence, get headers |
| OPTIONS | Yes | Yes | No | Yes | CORS preflight, discover methods |

*PATCH can be made idempotent with JSON Merge Patch (RFC 7396).

## HTTP Status Codes (Complete Reference)

### Success (2xx)

| Code | Name | When to Use |
|------|------|-------------|
| 200 | OK | GET, PATCH, DELETE (with body), successful action |
| 201 | Created | POST that creates a resource (include Location header) |
| 202 | Accepted | Async operation accepted, not yet completed |
| 204 | No Content | DELETE with no body, PUT/PATCH with no response needed |

### Client Errors (4xx)

| Code | Name | When to Use |
|------|------|-------------|
| 400 | Bad Request | Malformed syntax, invalid JSON, missing required params |
| 401 | Unauthorized | No authentication provided or token expired |
| 403 | Forbidden | Authenticated but lacks permission |
| 404 | Not Found | Resource does not exist |
| 405 | Method Not Allowed | HTTP method not supported on this endpoint |
| 409 | Conflict | Duplicate resource, concurrent modification conflict |
| 410 | Gone | Resource was deleted and will not return |
| 413 | Payload Too Large | Request body exceeds limit |
| 415 | Unsupported Media Type | Content-Type not supported |
| 422 | Unprocessable Entity | Valid syntax but semantic validation failed |
| 429 | Too Many Requests | Rate limit exceeded (include Retry-After header) |

### Server Errors (5xx)

| Code | Name | When to Use |
|------|------|-------------|
| 500 | Internal Server Error | Unexpected server-side error |
| 502 | Bad Gateway | Upstream service returned invalid response |
| 503 | Service Unavailable | Temporarily overloaded or in maintenance |
| 504 | Gateway Timeout | Upstream service timed out |

### Choosing Between 400 and 422

```
400: Request is syntactically wrong
  - Missing Content-Type header
  - Invalid JSON body: `{ name: "bad json" }` (unquoted key)
  - Wrong parameter type: `/users/not-a-uuid`

422: Request is syntactically valid but semantically wrong
  - Email format invalid: `{ "email": "not-an-email" }`
  - Business rule violation: `{ "age": -5 }`
  - Missing required field: `{ "name": "" }`
```

## Response Format

### Single Resource

```json
{
  "id": "usr_abc123",
  "name": "Jane Smith",
  "email": "jane@example.com",
  "role": "admin",
  "createdAt": "2025-01-15T10:30:00Z",
  "updatedAt": "2025-06-20T14:22:00Z"
}
```

### Collection with Metadata

```json
{
  "data": [
    { "id": "usr_abc123", "name": "Jane Smith", "email": "jane@example.com" },
    { "id": "usr_def456", "name": "John Doe", "email": "john@example.com" }
  ],
  "meta": {
    "total": 142,
    "page": 2,
    "perPage": 20,
    "totalPages": 8
  },
  "links": {
    "self": "/api/v1/users?page=2&perPage=20",
    "first": "/api/v1/users?page=1&perPage=20",
    "prev": "/api/v1/users?page=1&perPage=20",
    "next": "/api/v1/users?page=3&perPage=20",
    "last": "/api/v1/users?page=8&perPage=20"
  }
}
```

### Error Response (RFC 7807 Problem Details)

```json
{
  "type": "https://api.example.com/errors/validation-failed",
  "title": "Validation Failed",
  "status": 422,
  "detail": "The request body contains invalid fields.",
  "instance": "/api/v1/users",
  "errors": [
    {
      "field": "email",
      "message": "must be a valid email address",
      "code": "INVALID_FORMAT"
    },
    {
      "field": "name",
      "message": "must be between 2 and 100 characters",
      "code": "INVALID_LENGTH"
    }
  ]
}
```

## Naming Conventions

### JSON Field Naming

```json
{
  "id": "usr_abc123",
  "firstName": "Jane",
  "lastName": "Smith",
  "emailAddress": "jane@example.com",
  "isActive": true,
  "totalOrderCount": 42,
  "createdAt": "2025-01-15T10:30:00Z",
  "updatedAt": "2025-06-20T14:22:00Z",
  "deletedAt": null
}
```

**Rules:**
- Use `camelCase` for all JSON keys
- Boolean fields: prefix with `is`, `has`, `can`, `should`
- Timestamps: suffix with `At` (createdAt, updatedAt, deletedAt)
- Counts: suffix with `Count` (totalOrderCount, failedLoginCount)
- IDs: use prefixed format for clarity (usr_, ord_, prd_)

## Query Parameters

### Filtering

```
# Exact match
GET /api/v1/users?role=admin

# Bracket notation for operators
GET /api/v1/products?price[gte]=10&price[lte]=100
GET /api/v1/orders?status[in]=pending,processing
GET /api/v1/users?name[contains]=smith

# Multiple values (comma-separated)
GET /api/v1/products?category=electronics,books
```

### Sorting

```
# Single field (prefix with - for descending)
GET /api/v1/users?sort=name          # Ascending
GET /api/v1/users?sort=-createdAt    # Descending

# Multiple fields (comma-separated)
GET /api/v1/users?sort=-createdAt,name
```

### Field Selection (Sparse Fieldsets)

```
# Return only specified fields
GET /api/v1/users?fields=id,name,email

# Nested field selection
GET /api/v1/users?fields=id,name,profile.avatar
```

### Search

```
# Full-text search
GET /api/v1/users?search=jane+smith

# Scoped search
GET /api/v1/users?search[name]=jane&search[email]=example.com
```

## Pagination Strategies

### Offset-Based Pagination

```
GET /api/v1/users?page=3&perPage=20
```

**Pros:**
- Simple to implement
- Can jump to any page
- Easy to calculate total pages

**Cons:**
- Inconsistent results when data changes (items shift between pages)
- Performance degrades on large datasets (OFFSET is O(n) in SQL)
- Not suitable for real-time feeds

**Implementation:**

```typescript
app.get('/api/v1/users', async (req, res) => {
  const page = Math.max(1, parseInt(req.query.page) || 1);
  const perPage = Math.min(100, Math.max(1, parseInt(req.query.perPage) || 20));
  const offset = (page - 1) * perPage;

  const [users, total] = await Promise.all([
    db.users.findMany({ skip: offset, take: perPage, orderBy: { createdAt: 'desc' } }),
    db.users.count(),
  ]);

  res.json({
    data: users,
    meta: { total, page, perPage, totalPages: Math.ceil(total / perPage) },
  });
});
```

### Cursor-Based Pagination

```
GET /api/v1/users?cursor=eyJpZCI6MTAwfQ&limit=20
```

**Pros:**
- Consistent results even when data changes
- Efficient for large datasets (uses indexed WHERE instead of OFFSET)
- Natural for infinite scroll and real-time feeds

**Cons:**
- Cannot jump to arbitrary page
- More complex to implement
- Cursor must encode sort order

**Implementation:**

```typescript
app.get('/api/v1/users', async (req, res) => {
  const limit = Math.min(100, Math.max(1, parseInt(req.query.limit) || 20));
  const cursor = req.query.cursor
    ? JSON.parse(Buffer.from(req.query.cursor, 'base64url').toString())
    : null;

  const where = cursor ? { createdAt: { lt: cursor.createdAt } } : {};

  const users = await db.users.findMany({
    where,
    take: limit + 1, // Fetch one extra to determine if there's a next page
    orderBy: { createdAt: 'desc' },
  });

  const hasMore = users.length > limit;
  const data = hasMore ? users.slice(0, limit) : users;
  const nextCursor = hasMore
    ? Buffer.from(JSON.stringify({ createdAt: data.at(-1).createdAt })).toString('base64url')
    : null;

  res.json({
    data,
    meta: { hasMore, nextCursor },
  });
});
```

### When to Use Which

| Scenario | Recommendation |
|----------|----------------|
| Admin dashboard with page numbers | Offset-based |
| Infinite scroll feed | Cursor-based |
| Real-time data (chat, notifications) | Cursor-based |
| Small datasets (< 10,000 rows) | Either works |
| Large datasets (100K+ rows) | Cursor-based |
| API consumed by third parties | Cursor-based (more stable) |

## OpenAPI 3.1 Specification

### Design-First Approach

Write the OpenAPI spec before implementing. This enables parallel frontend/backend development, auto-generated client SDKs, and contract testing.

```yaml
# openapi.yml
openapi: 3.1.0
info:
  title: LinkLoom API
  version: 1.0.0
  description: Bookmark management API

servers:
  - url: http://localhost:3000/api/v1
    description: Development
  - url: https://api.linkloom.example.com/v1
    description: Production

paths:
  /bookmarks:
    get:
      operationId: listBookmarks
      summary: List bookmarks
      tags: [Bookmarks]
      parameters:
        - $ref: '#/components/parameters/PageParam'
        - $ref: '#/components/parameters/PerPageParam'
        - name: category
          in: query
          schema:
            type: string
      responses:
        '200':
          description: Paginated list of bookmarks
          content:
            application/json:
              schema:
                type: object
                properties:
                  data:
                    type: array
                    items:
                      $ref: '#/components/schemas/Bookmark'
                  meta:
                    $ref: '#/components/schemas/PaginationMeta'

    post:
      operationId: createBookmark
      summary: Create a bookmark
      tags: [Bookmarks]
      requestBody:
        required: true
        content:
          application/json:
            schema:
              $ref: '#/components/schemas/CreateBookmark'
      responses:
        '201':
          description: Bookmark created
          content:
            application/json:
              schema:
                $ref: '#/components/schemas/Bookmark'
        '422':
          $ref: '#/components/responses/ValidationError'

components:
  schemas:
    Bookmark:
      type: object
      required: [id, url, title, createdAt]
      properties:
        id:
          type: string
          example: "bk_abc123"
        url:
          type: string
          format: uri
        title:
          type: string
          maxLength: 200
        description:
          type: string
          maxLength: 1000
        tags:
          type: array
          items:
            type: string
        createdAt:
          type: string
          format: date-time

    CreateBookmark:
      type: object
      required: [url, title]
      properties:
        url:
          type: string
          format: uri
        title:
          type: string
          minLength: 1
          maxLength: 200
        description:
          type: string
          maxLength: 1000
        tags:
          type: array
          items:
            type: string

    PaginationMeta:
      type: object
      properties:
        total:
          type: integer
        page:
          type: integer
        perPage:
          type: integer
        totalPages:
          type: integer

  parameters:
    PageParam:
      name: page
      in: query
      schema:
        type: integer
        minimum: 1
        default: 1
    PerPageParam:
      name: perPage
      in: query
      schema:
        type: integer
        minimum: 1
        maximum: 100
        default: 20

  responses:
    ValidationError:
      description: Validation failed
      content:
        application/json:
          schema:
            type: object
            properties:
              type:
                type: string
              title:
                type: string
              status:
                type: integer
              errors:
                type: array
                items:
                  type: object
                  properties:
                    field:
                      type: string
                    message:
                      type: string
```

### Code-First vs Design-First

| Aspect | Design-First | Code-First |
|--------|-------------|------------|
| Workflow | Write spec, then implement | Write code, generate spec |
| Best for | Public APIs, multi-team projects | Internal APIs, rapid prototyping |
| Tools | Stoplight Studio, Swagger Editor | tsoa, NestJS Swagger, FastAPI |
| Contract testing | Easy (spec is the contract) | Harder (spec drifts from code) |
| Parallel development | Yes (frontend uses spec) | No (must wait for backend) |

## API Versioning

### URL Path Versioning (Recommended)

```
GET /api/v1/users
GET /api/v2/users
```

**Pros:** Explicit, easy to understand, cacheable, easy to route
**Cons:** URL pollution, harder to sunset

### Header Versioning

```
GET /api/users
Accept: application/vnd.example.v2+json
```

**Pros:** Clean URLs, follows HTTP content negotiation
**Cons:** Harder to test (need headers), less discoverable

### Recommendation

Use **URL path versioning** for simplicity. Only increment the major version for breaking changes. Use additive (non-breaking) changes within a version: adding fields, adding endpoints, adding optional parameters.

## Best Practices

1. **Be consistent** -- pick conventions and follow them everywhere
2. **Use nouns for resources**, verbs only for actions (POST /orders/:id/cancel)
3. **Return the created/updated resource** in response body
4. **Include Location header** for 201 Created responses
5. **Support `Accept` header** for content negotiation
6. **Document every endpoint** with OpenAPI
7. **Use ETags** for conditional requests and caching
8. **Version your API** from day one
9. **Return helpful error messages** with field-level details

## Anti-Patterns

- **Verb-based URLs**: `/getUsers`, `/createUser` -- use HTTP methods instead
- **Deeply nested URLs**: `/users/:id/orders/:id/items/:id/reviews` -- flatten with query params
- **Inconsistent naming**: Mixing `snake_case` and `camelCase` in the same API
- **Returning 200 for errors**: Always use appropriate HTTP status codes
- **Breaking changes in the same version**: Adding required fields, removing fields, changing types
- **Exposing internal IDs**: Sequential integers leak information about your data

## Sources & References

- Microsoft REST API Guidelines: https://github.com/microsoft/api-guidelines/blob/vNext/azure/Guidelines.md
- Google API Design Guide: https://cloud.google.com/apis/design
- JSON:API Specification: https://jsonapi.org/
- OpenAPI 3.1 Specification: https://spec.openapis.org/oas/v3.1.0.html
- RFC 7807 Problem Details: https://datatracker.ietf.org/doc/html/rfc7807
- Zalando RESTful API Guidelines: https://opensource.zalando.com/restful-api-guidelines/
- Stripe API Design (reference implementation): https://docs.stripe.com/api
