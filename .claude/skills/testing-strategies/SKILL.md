---
name: testing-strategies
description: Advanced testing — property-based, contract, mutation, E2E patterns, and testcontainers
---

# Advanced Testing Strategies

## Purpose

Guide agents in applying advanced testing techniques including property-based testing, contract testing, mutation testing, testcontainers for integration, and E2E patterns. Covers when to use each strategy and practical implementation.

## Property-Based Testing

Instead of testing specific examples, define properties that should hold for all valid inputs. The framework generates hundreds of random test cases automatically.

### fast-check (JavaScript/TypeScript)

```typescript
import { fc } from '@fast-check/vitest';
import { describe, it, expect } from 'vitest';

describe('slugify', () => {
  // Traditional example-based test
  it('should convert "Hello World" to "hello-world"', () => {
    expect(slugify('Hello World')).toBe('hello-world');
  });

  // Property-based: Test properties that hold for ALL inputs
  it.prop([fc.string({ minLength: 1 })])(
    'should always return lowercase',
    (input) => {
      const result = slugify(input);
      expect(result).toBe(result.toLowerCase());
    },
  );

  it.prop([fc.string()])(
    'should never contain spaces',
    (input) => {
      const result = slugify(input);
      expect(result).not.toContain(' ');
    },
  );

  it.prop([fc.string({ minLength: 1, maxLength: 200 })])(
    'should be idempotent (slugifying a slug returns same result)',
    (input) => {
      const once = slugify(input);
      const twice = slugify(once);
      expect(twice).toBe(once);
    },
  );
});

// Testing mathematical properties
describe('sort', () => {
  it.prop([fc.array(fc.integer())])(
    'should preserve array length',
    (arr) => {
      expect(sort(arr)).toHaveLength(arr.length);
    },
  );

  it.prop([fc.array(fc.integer())])(
    'should be ordered (each element <= next)',
    (arr) => {
      const sorted = sort(arr);
      for (let i = 0; i < sorted.length - 1; i++) {
        expect(sorted[i]).toBeLessThanOrEqual(sorted[i + 1]);
      }
    },
  );

  it.prop([fc.array(fc.integer())])(
    'should contain same elements as input',
    (arr) => {
      const sorted = sort(arr);
      expect(sorted.sort()).toEqual([...arr].sort());
    },
  );
});
```

### Common Property Patterns

| Property | Description | Example |
|----------|-------------|---------|
| Roundtrip | encode then decode returns original | `decode(encode(x)) === x` |
| Idempotent | Applying twice gives same result | `sort(sort(x)) === sort(x)` |
| Invariant | A condition always holds | `result.length <= input.length` |
| Commutativity | Order doesn't matter | `merge(a, b) === merge(b, a)` |
| Oracle | Compare with known-good implementation | `fastSort(x) === Array.sort(x)` |

### When to Use Property-Based Testing

- Serialization/deserialization (roundtrip property)
- Sorting, filtering, mapping (invariant properties)
- Parsers and formatters
- Mathematical functions
- State machines (valid state transitions)
- API input validation (no valid input should crash)

## Testcontainers

Run real databases and services in Docker containers for integration tests. No more mocking database queries.

### Setup with Vitest

```typescript
import { PostgreSqlContainer, StartedPostgreSqlContainer } from '@testcontainers/postgresql';
import { Client } from 'pg';
import { describe, it, expect, beforeAll, afterAll } from 'vitest';

describe('UserRepository (integration)', () => {
  let container: StartedPostgreSqlContainer;
  let client: Client;

  beforeAll(async () => {
    // Start a real PostgreSQL container
    container = await new PostgreSqlContainer('postgres:17-alpine')
      .withDatabase('testdb')
      .withUsername('test')
      .withPassword('test')
      .start();

    client = new Client({
      connectionString: container.getConnectionUri(),
    });
    await client.connect();

    // Run migrations
    await client.query(`
      CREATE TABLE users (
        id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
        name VARCHAR(100) NOT NULL,
        email VARCHAR(255) UNIQUE NOT NULL,
        created_at TIMESTAMPTZ DEFAULT NOW()
      )
    `);
  }, 60_000); // 60s timeout for container startup

  afterAll(async () => {
    await client.end();
    await container.stop();
  });

  it('should insert and retrieve a user', async () => {
    const repo = new UserRepository(client);

    const user = await repo.create({ name: 'Jane', email: 'jane@test.com' });

    expect(user.id).toBeDefined();
    expect(user.name).toBe('Jane');

    const found = await repo.findById(user.id);
    expect(found).toEqual(user);
  });

  it('should throw on duplicate email', async () => {
    const repo = new UserRepository(client);
    await repo.create({ name: 'User 1', email: 'dup@test.com' });

    await expect(repo.create({ name: 'User 2', email: 'dup@test.com' }))
      .rejects.toThrow(/unique/i);
  });
});
```

### Redis Testcontainer

```typescript
import { GenericContainer } from 'testcontainers';
import Redis from 'ioredis';

let container;
let redis;

beforeAll(async () => {
  container = await new GenericContainer('redis:7-alpine')
    .withExposedPorts(6379)
    .start();

  redis = new Redis({
    host: container.getHost(),
    port: container.getMappedPort(6379),
  });
}, 30_000);

afterAll(async () => {
  await redis.quit();
  await container.stop();
});
```

### When to Use Testcontainers

- Database query testing (SQL correctness, constraints, migrations)
- Cache behavior testing (expiry, eviction)
- Message queue integration (Kafka, RabbitMQ)
- Search engine queries (Elasticsearch, OpenSearch)
- Any external service with complex query semantics

## Contract Testing (Pact)

Verify that API consumers and providers agree on the contract without running both services together.

### Consumer-Driven Contract (CDC) Flow

```
Consumer Test                   Provider Test
─────────────                   ─────────────
1. Define expected              4. Replay interactions
   interactions (Pact)             against real provider
2. Generate Pact file           5. Verify all contracts
3. Publish to Pact Broker          are satisfied
```

### Consumer Side (JavaScript)

```typescript
import { PactV4 } from '@pact-foundation/pact';

const provider = new PactV4({
  consumer: 'BookmarkUI',
  provider: 'BookmarkAPI',
});

describe('Bookmark API Contract', () => {
  it('should return a bookmark by ID', async () => {
    // Define the expected interaction
    await provider
      .addInteraction()
      .given('a bookmark with ID bk_123 exists')
      .uponReceiving('a request for bookmark bk_123')
      .withRequest('GET', '/api/v1/bookmarks/bk_123')
      .willRespondWith(200, (builder) => {
        builder
          .header('Content-Type', 'application/json')
          .jsonBody({
            id: 'bk_123',
            url: 'https://example.com',
            title: 'Example',
          });
      })
      .executeTest(async (mockServer) => {
        // Test your client code against the mock
        const client = new BookmarkClient(mockServer.url);
        const bookmark = await client.getById('bk_123');

        expect(bookmark.id).toBe('bk_123');
        expect(bookmark.url).toBe('https://example.com');
      });
  });
});
```

### Provider Side Verification

```typescript
import { Verifier } from '@pact-foundation/pact';

describe('Pact Verification', () => {
  it('should satisfy consumer contracts', async () => {
    const verifier = new Verifier({
      providerBaseUrl: 'http://localhost:3000',
      pactBrokerUrl: 'https://pact-broker.example.com',
      provider: 'BookmarkAPI',
      providerVersion: process.env.GIT_SHA,
      publishVerificationResult: process.env.CI === 'true',
      stateHandlers: {
        'a bookmark with ID bk_123 exists': async () => {
          await db.bookmarks.create({ id: 'bk_123', url: 'https://example.com', title: 'Example' });
        },
      },
    });

    await verifier.verifyProvider();
  });
});
```

### When to Use Contract Testing

- Microservice architectures (API boundaries between teams)
- Third-party API integrations
- Frontend/backend split teams
- When E2E tests are too slow or flaky
- Preventing breaking changes in APIs

## Mutation Testing

Mutation testing measures test quality by introducing small changes (mutations) to your code and checking if tests catch them. If a mutation survives (tests still pass), your tests have a gap.

### Stryker Mutator (JavaScript/TypeScript)

```bash
# Install
pnpm add -D @stryker-mutator/core @stryker-mutator/vitest-runner

# Initialize config
pnpm stryker init
```

```json
// stryker.config.json
{
  "$schema": "./node_modules/@stryker-mutator/core/schema/stryker-schema.json",
  "testRunner": "vitest",
  "mutate": [
    "src/**/*.ts",
    "!src/**/*.test.ts",
    "!src/**/*.d.ts"
  ],
  "reporters": ["html", "progress"],
  "thresholds": {
    "high": 80,
    "low": 60,
    "break": 50
  }
}
```

### Understanding Mutation Results

```
Mutation Score: 85% (170/200 mutants killed)

Survived Mutants (gaps in your tests):
  src/services/pricing.ts:15
    - Changed `price * quantity` to `price / quantity`
    - No test caught this! Add a test for total calculation.

  src/utils/validate.ts:8
    - Changed `>=` to `>`
    - Boundary condition not tested. Add test for value = minimum.
```

### Common Mutations

| Mutation Type | Original | Mutated |
|---------------|----------|---------|
| Arithmetic | `a + b` | `a - b` |
| Conditional | `a > b` | `a >= b`, `a < b` |
| Boolean | `true` | `false` |
| String | `"hello"` | `""` |
| Remove statement | `validate(input)` | (removed) |
| Negate conditional | `if (valid)` | `if (!valid)` |
| Return value | `return value` | `return null` |

### When to Use Mutation Testing

- Critical business logic (pricing, permissions, validation)
- After achieving high code coverage to verify test quality
- Libraries and shared packages
- Not suitable for: UI tests, integration tests, large codebases (run on changed files only)

## Snapshot and Golden Testing

### When Snapshot Testing Is Appropriate

- Component render output (React components)
- API response structure validation
- Configuration file generation
- Serialized data format verification
- **Not for**: Frequently changing output, large objects, anything with timestamps

### Vitest Snapshot Testing

```typescript
import { describe, it, expect } from 'vitest';

describe('ErrorResponse', () => {
  it('should format validation error correctly', () => {
    const error = formatValidationError({
      email: ['is required'],
      name: ['must be at least 2 characters'],
    });

    // First run: creates snapshot file
    // Subsequent runs: compares against snapshot
    expect(error).toMatchInlineSnapshot(`
      {
        "errors": [
          {
            "field": "email",
            "message": "is required",
          },
          {
            "field": "name",
            "message": "must be at least 2 characters",
          },
        ],
        "status": 422,
        "title": "Validation Failed",
      }
    `);
  });
});
```

### Snapshot Rules

1. **Review snapshot changes** carefully in PRs -- they are part of the test
2. **Use `toMatchInlineSnapshot`** for small outputs (visible in test file)
3. **Use `toMatchSnapshot`** for large outputs (stored in `__snapshots__/`)
4. **Never auto-update without reviewing** (`--update` flag)
5. **Remove stale snapshots** when deleting tests

## E2E Testing Patterns

### Page Object Model

```typescript
// tests/pages/login.page.ts
export class LoginPage {
  constructor(private page: Page) {}

  async navigate() {
    await this.page.goto('/login');
  }

  async fillEmail(email: string) {
    await this.page.getByLabel('Email').fill(email);
  }

  async fillPassword(password: string) {
    await this.page.getByLabel('Password').fill(password);
  }

  async submit() {
    await this.page.getByRole('button', { name: 'Sign In' }).click();
  }

  async login(email: string, password: string) {
    await this.fillEmail(email);
    await this.fillPassword(password);
    await this.submit();
  }

  async getErrorMessage() {
    return this.page.getByRole('alert').textContent();
  }
}

// tests/auth.spec.ts
import { test, expect } from '@playwright/test';
import { LoginPage } from './pages/login.page';

test.describe('Authentication', () => {
  test('should login with valid credentials', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.navigate();
    await loginPage.login('user@example.com', 'password123');

    await expect(page).toHaveURL('/dashboard');
    await expect(page.getByText('Welcome back')).toBeVisible();
  });

  test('should show error for invalid credentials', async ({ page }) => {
    const loginPage = new LoginPage(page);
    await loginPage.navigate();
    await loginPage.login('user@example.com', 'wrong');

    const error = await loginPage.getErrorMessage();
    expect(error).toContain('Invalid credentials');
  });
});
```

### data-testid for Stable Selectors

```html
<!-- Use data-testid for test-specific selectors -->
<button data-testid="submit-bookmark">Save Bookmark</button>
<div data-testid="bookmark-list">...</div>
<span data-testid="bookmark-count">42</span>
```

```typescript
// Prefer role-based selectors, fall back to data-testid
await page.getByRole('button', { name: 'Save Bookmark' }); // Best
await page.getByTestId('submit-bookmark');                   // Fallback
await page.locator('.btn-primary');                          // Avoid (fragile)
await page.locator('#submit');                               // Avoid (fragile)
```

### Selector Priority (Most to Least Preferred)

1. `getByRole` -- accessible, user-facing
2. `getByLabel` -- form elements
3. `getByText` -- visible text content
4. `getByTestId` -- stable test ID
5. CSS/XPath selectors -- last resort

## Flaky Test Management

### Identifying Flaky Tests

```typescript
// Playwright retry configuration
import { defineConfig } from '@playwright/test';

export default defineConfig({
  retries: process.env.CI ? 2 : 0,  // Retry in CI only
  reporter: [
    ['html'],
    ['json', { outputFile: 'test-results.json' }],
  ],
});
```

### Quarantine Strategy

```typescript
// Tag flaky tests for tracking
test.describe('Payment flow', () => {
  // Skip flaky test but track it
  test.fixme('should process payment with 3DS', async ({ page }) => {
    // Known flaky: depends on third-party 3DS simulator timing
    // Ticket: JIRA-1234
  });
});
```

### Common Causes of Flakiness

| Cause | Fix |
|-------|-----|
| Timing/race conditions | Use proper waits (`waitForResponse`, `toBeVisible`) |
| Shared test state | Isolate tests, reset state in `beforeEach` |
| Network dependencies | Mock external services |
| Time-dependent tests | Use fake timers |
| Random data without seed | Use seeded random or fixed test data |
| Port conflicts | Use dynamic port allocation |
| File system state | Use temp directories, clean up in `afterEach` |

## Test Parallelization

### Vitest Parallel Execution

```typescript
// vitest.config.ts
export default defineConfig({
  test: {
    pool: 'forks',           // Use child processes for isolation
    poolOptions: {
      forks: {
        maxForks: 4,         // Limit parallel processes
      },
    },
    sequence: {
      shuffle: true,         // Randomize order to catch hidden dependencies
    },
  },
});
```

### Playwright Parallel Workers

```typescript
export default defineConfig({
  workers: process.env.CI ? 4 : undefined,  // 4 parallel workers in CI
  fullyParallel: true,                       // Run tests within files in parallel
});
```

## Visual Regression Testing

```typescript
// Playwright visual comparison
test('homepage should match screenshot', async ({ page }) => {
  await page.goto('/');
  await expect(page).toHaveScreenshot('homepage.png', {
    maxDiffPixels: 100,      // Allow minor rendering differences
    threshold: 0.2,          // Color difference threshold
  });
});

// Component visual test
test('bookmark card should render correctly', async ({ page }) => {
  await page.goto('/bookmarks');
  const card = page.getByTestId('bookmark-card').first();
  await expect(card).toHaveScreenshot('bookmark-card.png');
});
```

## Best Practices

1. **Use property-based testing** for functions with clear mathematical properties
2. **Use testcontainers** for database integration tests (not mocks)
3. **Run mutation testing** on critical business logic to verify test quality
4. **Use contract testing** between independently deployed services
5. **Keep E2E tests minimal** -- test critical flows only
6. **Use Page Object Model** for E2E test maintainability
7. **Quarantine flaky tests** immediately -- do not ignore them
8. **Parallelize tests** but ensure isolation between test cases

## Anti-Patterns

- **E2E testing everything** -- slow, flaky, expensive to maintain
- **Skipping integration tests** and relying only on unit + E2E
- **Mocking the database** when you could use testcontainers
- **Snapshot testing everything** -- becomes noise that developers blindly update
- **Ignoring mutation score** -- high coverage with low mutation score means weak tests
- **Flaky tests left running** -- they erode team trust in the test suite

## Sources & References

- fast-check Documentation: https://fast-check.dev/
- Testcontainers for Node.js: https://testcontainers.com/guides/getting-started-with-testcontainers-for-nodejs/
- Pact Contract Testing: https://docs.pact.io/
- Stryker Mutator: https://stryker-mutator.io/
- Playwright Testing Library: https://playwright.dev/docs/intro
- Martin Fowler - Contract Testing: https://martinfowler.com/bliki/ContractTest.html
- Google Testing Blog - Test Flakiness: https://testing.googleblog.com/2016/05/flaky-tests-at-google-and-how-we.html
