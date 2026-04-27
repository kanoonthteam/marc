---
name: testing-specialized
description: Specialized testing — API testing matrix, OWASP security testing, performance testing, and accessibility
---

# Specialized Testing

## Purpose

Guide the QA agent in executing specialized testing disciplines: API endpoint testing, security testing (OWASP Top 10), performance testing (k6), and accessibility testing (WCAG 2.1 AA). Each section provides actionable checklists and practical examples.

## API Testing Checklist

### Endpoint Test Matrix

For every API endpoint, verify the following matrix.

```markdown
## Endpoint: POST /api/v1/bookmarks

| Test Case | Expected Status | Expected Body | Result |
|-----------|----------------|---------------|--------|
| Valid input | 201 | Created bookmark | |
| Missing required field (url) | 422 | Field-level error | |
| Missing required field (title) | 422 | Field-level error | |
| Invalid URL format | 422 | URL validation error | |
| Duplicate URL | 409 | Conflict error | |
| Empty body | 400 | Bad request | |
| Invalid JSON | 400 | Parse error | |
| Extra unknown fields | 201 | Ignored (or 400) | |
| Exceeds max title length | 422 | Length error | |
| Special characters in title | 201 | Properly stored | |
| Missing Content-Type header | 415 | Unsupported media type | |
```

### REST CRUD Matrix Template

```markdown
## Resource: Bookmarks

| Method | Endpoint | Auth | Test Cases |
|--------|----------|------|------------|
| GET | /bookmarks | No | List all, empty list, with pagination, with filters, with sort |
| POST | /bookmarks | No | Valid create, validation errors, duplicate, max fields |
| GET | /bookmarks/:id | No | Found, not found, invalid ID format |
| PUT | /bookmarks/:id | No | Full update, not found, validation errors |
| PATCH | /bookmarks/:id | No | Partial update, not found, no changes |
| DELETE | /bookmarks/:id | No | Found, not found, already deleted |
```

### Authentication Flow Testing

```typescript
describe('Authentication Flows', () => {
  // Token lifecycle
  it('should reject request with no token', async () => {
    const res = await request(app).get('/api/v1/protected');
    expect(res.status).toBe(401);
    expect(res.body.title).toBe('Unauthorized');
  });

  it('should reject expired token', async () => {
    const expiredToken = generateToken({ exp: Math.floor(Date.now() / 1000) - 60 });
    const res = await request(app)
      .get('/api/v1/protected')
      .set('Authorization', `Bearer ${expiredToken}`);
    expect(res.status).toBe(401);
  });

  it('should reject malformed token', async () => {
    const res = await request(app)
      .get('/api/v1/protected')
      .set('Authorization', 'Bearer not-a-valid-jwt');
    expect(res.status).toBe(401);
  });

  it('should reject token with wrong signature', async () => {
    const wrongToken = generateToken({}, 'wrong-secret');
    const res = await request(app)
      .get('/api/v1/protected')
      .set('Authorization', `Bearer ${wrongToken}`);
    expect(res.status).toBe(401);
  });

  // Authorization
  it('should reject user accessing another user resources', async () => {
    const userAToken = generateToken({ sub: 'user-a' });
    const res = await request(app)
      .get('/api/v1/users/user-b/bookmarks')
      .set('Authorization', `Bearer ${userAToken}`);
    expect(res.status).toBe(403);
  });

  it('should allow admin to access any resource', async () => {
    const adminToken = generateToken({ sub: 'admin', role: 'admin' });
    const res = await request(app)
      .get('/api/v1/users/user-b/bookmarks')
      .set('Authorization', `Bearer ${adminToken}`);
    expect(res.status).toBe(200);
  });
});
```

### Pagination Boundary Testing

```typescript
describe('Pagination', () => {
  beforeEach(async () => {
    // Seed exactly 25 bookmarks
    for (let i = 0; i < 25; i++) {
      await createBookmark({ title: `Bookmark ${i}` });
    }
  });

  it('should return first page with default perPage', async () => {
    const res = await request(app).get('/api/v1/bookmarks');
    expect(res.body.data).toHaveLength(20); // Default perPage
    expect(res.body.meta.total).toBe(25);
    expect(res.body.meta.totalPages).toBe(2);
  });

  it('should return partial last page', async () => {
    const res = await request(app).get('/api/v1/bookmarks?page=2&perPage=20');
    expect(res.body.data).toHaveLength(5); // 25 - 20 = 5 remaining
  });

  it('should return empty for page beyond total', async () => {
    const res = await request(app).get('/api/v1/bookmarks?page=10&perPage=20');
    expect(res.body.data).toHaveLength(0);
  });

  it('should handle perPage=1', async () => {
    const res = await request(app).get('/api/v1/bookmarks?perPage=1');
    expect(res.body.data).toHaveLength(1);
    expect(res.body.meta.totalPages).toBe(25);
  });

  it('should cap perPage at maximum (100)', async () => {
    const res = await request(app).get('/api/v1/bookmarks?perPage=500');
    expect(res.body.data.length).toBeLessThanOrEqual(100);
  });

  it('should reject perPage=0', async () => {
    const res = await request(app).get('/api/v1/bookmarks?perPage=0');
    expect(res.status).toBe(400);
  });

  it('should reject negative page', async () => {
    const res = await request(app).get('/api/v1/bookmarks?page=-1');
    expect(res.status).toBe(400);
  });
});
```

### Concurrent Request Testing

```typescript
describe('Concurrent Operations', () => {
  it('should handle 50 concurrent creates without errors', async () => {
    const promises = Array.from({ length: 50 }, (_, i) =>
      request(app).post('/api/v1/bookmarks').send({
        url: `https://example.com/page-${i}`,
        title: `Concurrent Bookmark ${i}`,
      }),
    );

    const results = await Promise.all(promises);

    const successes = results.filter(r => r.status === 201);
    expect(successes).toHaveLength(50);

    // Verify all were stored
    const list = await request(app).get('/api/v1/bookmarks?perPage=100');
    expect(list.body.data.length).toBeGreaterThanOrEqual(50);
  });

  it('should handle concurrent updates to same resource', async () => {
    const { body: bookmark } = await request(app)
      .post('/api/v1/bookmarks')
      .send({ url: 'https://example.com', title: 'Original' });

    const promises = Array.from({ length: 10 }, (_, i) =>
      request(app)
        .patch(`/api/v1/bookmarks/${bookmark.id}`)
        .send({ title: `Updated ${i}` }),
    );

    const results = await Promise.all(promises);
    // All should succeed (last write wins) or use optimistic locking (409)
    results.forEach(r => expect([200, 409]).toContain(r.status));
  });
});
```

## Security Testing (OWASP Top 10)

### OWASP API Security Top 10 Verification Checklist

#### API1: Broken Object Level Authorization (BOLA)

```typescript
describe('BOLA Prevention', () => {
  it('should not allow user A to access user B resources', async () => {
    // Create resource as user A
    const resA = await request(app)
      .post('/api/v1/bookmarks')
      .set('Authorization', `Bearer ${userAToken}`)
      .send({ url: 'https://private.com', title: 'Private' });

    // Try to access as user B
    const resB = await request(app)
      .get(`/api/v1/bookmarks/${resA.body.id}`)
      .set('Authorization', `Bearer ${userBToken}`);

    expect(resB.status).toBe(404); // 404 preferred over 403 (don't leak existence)
  });

  it('should not allow user to modify another user resource', async () => {
    const resA = await request(app)
      .post('/api/v1/bookmarks')
      .set('Authorization', `Bearer ${userAToken}`)
      .send({ url: 'https://example.com', title: 'Test' });

    const resB = await request(app)
      .delete(`/api/v1/bookmarks/${resA.body.id}`)
      .set('Authorization', `Bearer ${userBToken}`);

    expect(resB.status).toBe(404);
  });
});
```

#### API4: Unrestricted Resource Consumption

```typescript
describe('Resource Consumption Limits', () => {
  it('should reject oversized request body', async () => {
    const largeBody = { title: 'x'.repeat(1_000_000) };
    const res = await request(app)
      .post('/api/v1/bookmarks')
      .send(largeBody);
    expect(res.status).toBe(413); // Payload Too Large
  });

  it('should limit pagination size', async () => {
    const res = await request(app).get('/api/v1/bookmarks?perPage=10000');
    expect(res.body.data.length).toBeLessThanOrEqual(100);
  });

  it('should rate limit repeated requests', async () => {
    const requests = Array.from({ length: 200 }, () =>
      request(app).get('/api/v1/bookmarks'),
    );
    const results = await Promise.all(requests);
    const rateLimited = results.filter(r => r.status === 429);
    expect(rateLimited.length).toBeGreaterThan(0);
  });
});
```

#### Security Headers Verification

```typescript
describe('Security Headers', () => {
  it('should set security headers', async () => {
    const res = await request(app).get('/api/v1/bookmarks');

    expect(res.headers['x-content-type-options']).toBe('nosniff');
    expect(res.headers['x-frame-options']).toBeDefined();
    expect(res.headers['strict-transport-security']).toBeDefined();
    expect(res.headers['x-xss-protection']).toBeDefined();
  });

  it('should not expose server version', async () => {
    const res = await request(app).get('/api/v1/bookmarks');
    expect(res.headers['x-powered-by']).toBeUndefined();
  });

  it('should set CORS headers correctly', async () => {
    const res = await request(app)
      .options('/api/v1/bookmarks')
      .set('Origin', 'https://app.example.com');

    expect(res.headers['access-control-allow-origin']).toBe('https://app.example.com');
    expect(res.headers['access-control-allow-methods']).toBeDefined();
  });

  it('should reject CORS from unknown origins', async () => {
    const res = await request(app)
      .options('/api/v1/bookmarks')
      .set('Origin', 'https://evil.example.com');

    expect(res.headers['access-control-allow-origin']).toBeUndefined();
  });
});
```

### Security Testing Checklist

```markdown
## OWASP API Security Verification

- [ ] **BOLA**: Users cannot access other users' resources
- [ ] **Auth**: Endpoints reject missing/expired/invalid tokens
- [ ] **Property-level auth**: Users cannot modify read-only fields (role, id)
- [ ] **Resource limits**: Max body size, max perPage, rate limiting
- [ ] **Function-level auth**: Admin endpoints reject non-admin users
- [ ] **Mass assignment**: Unknown fields in request body are ignored
- [ ] **SSRF**: User-supplied URLs are validated (no internal IPs)
- [ ] **Security headers**: X-Content-Type-Options, HSTS, no X-Powered-By
- [ ] **Error info**: 500 errors don't leak stack traces or internal paths
- [ ] **Injection**: SQL/NoSQL injection strings handled safely
```

## Performance Testing (k6)

### Basic Load Test

```javascript
// k6/load-test.js
import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';

const errorRate = new Rate('errors');
const responseTime = new Trend('response_time');

export const options = {
  stages: [
    { duration: '30s', target: 10 },   // Ramp up to 10 users
    { duration: '1m', target: 10 },    // Stay at 10 users
    { duration: '30s', target: 50 },   // Ramp up to 50 users
    { duration: '1m', target: 50 },    // Stay at 50 users
    { duration: '30s', target: 0 },    // Ramp down
  ],
  thresholds: {
    http_req_duration: ['p(95)<500', 'p(99)<1000'],  // 95th < 500ms, 99th < 1s
    errors: ['rate<0.05'],                             // Error rate < 5%
    http_req_failed: ['rate<0.01'],                    // HTTP failures < 1%
  },
};

export default function () {
  // List bookmarks
  const listRes = http.get('http://localhost:3000/api/v1/bookmarks');
  check(listRes, {
    'list status is 200': (r) => r.status === 200,
    'list has data': (r) => JSON.parse(r.body).data !== undefined,
  });
  errorRate.add(listRes.status !== 200);
  responseTime.add(listRes.timings.duration);

  sleep(1); // Think time between requests

  // Create bookmark
  const createRes = http.post(
    'http://localhost:3000/api/v1/bookmarks',
    JSON.stringify({
      url: `https://example.com/${Date.now()}`,
      title: `Load Test Bookmark ${__VU}-${__ITER}`,
    }),
    { headers: { 'Content-Type': 'application/json' } },
  );
  check(createRes, {
    'create status is 201': (r) => r.status === 201,
  });
  errorRate.add(createRes.status !== 201);

  sleep(1);
}
```

### Running k6

```bash
# Install k6
brew install k6  # macOS
# or: docker run --rm -i grafana/k6 run - < k6/load-test.js

# Run load test
k6 run k6/load-test.js

# Run with custom options
k6 run --vus 100 --duration 2m k6/load-test.js

# Output to JSON for analysis
k6 run --out json=results.json k6/load-test.js
```

### Response Time SLAs

| Endpoint Type | p50 Target | p95 Target | p99 Target |
|---------------|-----------|-----------|-----------|
| Health check | < 10ms | < 50ms | < 100ms |
| List (paginated) | < 100ms | < 300ms | < 500ms |
| Get by ID | < 50ms | < 200ms | < 500ms |
| Create/Update | < 100ms | < 500ms | < 1000ms |
| Search | < 200ms | < 500ms | < 1000ms |
| Report/Analytics | < 500ms | < 2000ms | < 5000ms |

### Performance Testing Checklist

```markdown
## Performance Verification

- [ ] Response time p95 < 500ms for CRUD operations
- [ ] Response time p95 < 1s for search/analytics
- [ ] Error rate < 1% under expected load
- [ ] No memory leaks (monitor RSS over time)
- [ ] No connection pool exhaustion under load
- [ ] Graceful degradation under 2x expected load
- [ ] Pagination prevents unbounded queries
```

## Accessibility Testing (WCAG 2.1 AA)

### Automated Testing Tools

```bash
# axe-core (most popular automated accessibility scanner)
pnpm add -D @axe-core/playwright

# pa11y (command-line accessibility testing)
npx pa11y http://localhost:3000

# Lighthouse CI
npx lighthouse http://localhost:3000 --output=json --only-categories=accessibility
```

### Playwright + axe-core Integration

```typescript
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.describe('Accessibility', () => {
  test('homepage should have no critical violations', async ({ page }) => {
    await page.goto('/');

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])  // WCAG 2.1 AA
      .analyze();

    expect(results.violations).toEqual([]);
  });

  test('bookmark form should be accessible', async ({ page }) => {
    await page.goto('/bookmarks/new');

    const results = await new AxeBuilder({ page })
      .include('#bookmark-form')       // Scope to form
      .withTags(['wcag2a', 'wcag2aa'])
      .analyze();

    expect(results.violations).toEqual([]);
  });
});
```

### WCAG 2.1 AA Manual Checklist

```markdown
## Perceivable
- [ ] All images have alt text (or alt="" for decorative)
- [ ] Color is not the only way to convey information
- [ ] Text contrast ratio >= 4.5:1 (normal text) or >= 3:1 (large text)
- [ ] Page is readable at 200% zoom
- [ ] Audio/video has captions (if applicable)

## Operable
- [ ] All functionality available via keyboard
- [ ] No keyboard traps (Tab moves through all elements)
- [ ] Focus order is logical (matches visual order)
- [ ] Focus indicator is visible on all interactive elements
- [ ] Skip navigation link present
- [ ] Page title is descriptive and unique

## Understandable
- [ ] Language attribute set on html element
- [ ] Form labels are associated with inputs (for/id or wrapping)
- [ ] Error messages identify the field and describe the error
- [ ] Form instructions appear before the form
- [ ] Consistent navigation across pages

## Robust
- [ ] Valid HTML (no duplicate IDs, proper nesting)
- [ ] ARIA roles used correctly (if applicable)
- [ ] Custom components expose proper roles and states
- [ ] Works with screen readers (VoiceOver, NVDA)
```

### Common Accessibility Fixes

```html
<!-- BAD: No label association -->
<input type="text" placeholder="Search...">

<!-- GOOD: Proper label -->
<label for="search-input">Search bookmarks</label>
<input type="text" id="search-input" placeholder="Search...">

<!-- GOOD: Visually hidden label (for icon-only inputs) -->
<label for="search-input" class="sr-only">Search bookmarks</label>
<input type="text" id="search-input" placeholder="Search...">

<!-- BAD: Click handler on div -->
<div onclick="handleClick()">Click me</div>

<!-- GOOD: Use semantic button -->
<button type="button" onclick="handleClick()">Click me</button>

<!-- BAD: Color-only indication -->
<span style="color: red">Error</span>

<!-- GOOD: Icon + color + text -->
<span role="alert" style="color: red">Error: Title is required</span>
```

## Bug Triage Process

### Severity vs Priority Decision Tree

```
Is it a security vulnerability?
  → Yes: Critical severity, P0 priority

Does it cause data loss or corruption?
  → Yes: Critical severity, P0 priority

Is the feature completely unusable?
  → Yes: Critical severity, P1 priority

Does the feature partially work (workaround exists)?
  → Yes: Major severity, P1/P2 priority

Is it cosmetic or a minor improvement?
  → Yes: Minor severity, P2/P3 priority
```

### Triage Meeting Agenda

```markdown
## Bug Triage - [Date]

### New Bugs (unclassified)
| Bug | Reported By | Severity | Priority | Assignee | Decision |
|-----|------------|----------|----------|----------|----------|
| BUG-042 | QA Agent | Major | P1 | TBD | Fix this sprint |
| BUG-043 | QA Agent | Minor | P3 | TBD | Backlog |

### Open Bugs (status update)
| Bug | Priority | Status | ETA |
|-----|----------|--------|-----|
| BUG-038 | P1 | In progress | Today |
| BUG-039 | P2 | Blocked | Needs API change |
```

## Test Environment Management

### Environment Checklist

```markdown
## Environment Setup
- [ ] Dependencies installed (`pnpm install`)
- [ ] Environment variables configured (.env or equivalent)
- [ ] Database migrated (if applicable)
- [ ] Seed data loaded (if applicable)
- [ ] External services mocked or available
- [ ] Test user accounts created
- [ ] API documentation accessible

## Environment Teardown
- [ ] Test data cleaned up
- [ ] Temporary files removed
- [ ] Database reset to known state
- [ ] No leftover processes running
```

### Test Data Isolation

```typescript
// Each test suite gets its own isolated data
describe('Bookmark API', () => {
  let testStore: InMemoryStore;

  beforeEach(() => {
    testStore = new InMemoryStore(); // Fresh store per test
    app = createApp({ store: testStore });
  });

  afterEach(() => {
    testStore.clear(); // Clean up
  });
});
```

## Best Practices

1. **Use the endpoint test matrix** for systematic API coverage
2. **Run OWASP checks** on every API that handles user data
3. **Set performance thresholds** early and test against them in CI
4. **Automate accessibility testing** with axe-core in your test suite
5. **Triage bugs immediately** -- classify severity and priority, then assign
6. **Test auth flows thoroughly** -- they are the most common attack vector
7. **Keep test environments reproducible** -- use containers or clean state

## Anti-Patterns

- **Skipping auth testing** because "it's handled by middleware"
- **Manual-only performance testing** -- automate with k6 and set thresholds
- **Ignoring accessibility** until launch -- test continuously
- **Testing only happy paths** in the endpoint matrix
- **No test data isolation** -- tests depend on data from other tests
- **Security testing as an afterthought** -- integrate into every sprint

## Sources & References

- OWASP API Security Top 10 (2023): https://owasp.org/API-Security/editions/2023/en/0x11-t10/
- OWASP Testing Guide v4.2: https://owasp.org/www-project-web-security-testing-guide/
- k6 Load Testing Documentation: https://grafana.com/docs/k6/latest/
- WCAG 2.1 Quick Reference: https://www.w3.org/WAI/WCAG21/quickref/
- axe-core for Playwright: https://github.com/dequelabs/axe-core-npm/tree/develop/packages/playwright
- Deque University (Accessibility): https://dequeuniversity.com/
- OWASP Cheat Sheet Series: https://cheatsheetseries.owasp.org/
