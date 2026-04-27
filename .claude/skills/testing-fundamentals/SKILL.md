---
name: testing-fundamentals
description: Testing pyramid, AAA pattern, TDD workflow, mocking guidelines, and test organization
---

# Testing Fundamentals

## Purpose

Guide agents in writing well-structured, maintainable tests following the testing pyramid, TDD workflow, and established patterns. Covers unit testing, integration testing, mocking, coverage strategy, and test organization.

## Testing Pyramid

```
              ┌──────────┐
             /    E2E     \          Few (5-10%)
            /  Tests       \         Slow, expensive, high confidence
           /────────────────\
          / Integration      \       Some (15-25%)
         /  Tests             \      Medium speed, good confidence
        /──────────────────────\
       /    Unit Tests          \    Many (65-80%)
      /  (Business Logic)       \   Fast, cheap, focused, isolated
     /____________________________\
```

### Layer Responsibilities

| Layer | What to Test | Speed | Example |
|-------|-------------|-------|---------|
| Unit | Pure functions, business logic, data transforms | < 10ms | `calculateTotal(items)` returns correct sum |
| Integration | API endpoints, DB queries, service interactions | 100ms-2s | `POST /api/users` creates a record |
| E2E | Critical user flows across the full stack | 5-30s | User signs up, verifies email, logs in |

### What to Test at Each Layer

**Unit Tests (many):**
- Business logic functions
- Data transformations and mappings
- Validation rules
- State machines and workflows
- Utility functions
- Edge cases and boundary conditions

**Integration Tests (some):**
- API endpoint request/response contracts
- Database queries and transactions
- Cache interactions
- Message queue publishing/consuming
- External service client wrappers

**E2E Tests (few):**
- Critical user journeys (signup, checkout, payment)
- Cross-service flows
- Smoke tests for deployment verification

## AAA Pattern (Arrange, Act, Assert)

Every test should follow this structure for clarity and consistency.

```typescript
describe('BookmarkService', () => {
  describe('create', () => {
    it('should create a bookmark with valid URL and title', () => {
      // Arrange: Set up test data and dependencies
      const store = new InMemoryStore();
      const service = new BookmarkService(store);
      const input = {
        url: 'https://example.com',
        title: 'Example Site',
        tags: ['reference'],
      };

      // Act: Execute the behavior under test
      const bookmark = service.create(input);

      // Assert: Verify the expected outcome
      expect(bookmark.id).toBeDefined();
      expect(bookmark.url).toBe('https://example.com');
      expect(bookmark.title).toBe('Example Site');
      expect(bookmark.tags).toEqual(['reference']);
      expect(bookmark.createdAt).toBeInstanceOf(Date);
    });

    it('should throw ValidationError when URL is missing', () => {
      // Arrange
      const store = new InMemoryStore();
      const service = new BookmarkService(store);

      // Act & Assert (combined for thrown errors)
      expect(() => service.create({ title: 'No URL' }))
        .toThrow(ValidationError);
    });

    it('should generate unique IDs for each bookmark', () => {
      // Arrange
      const store = new InMemoryStore();
      const service = new BookmarkService(store);
      const input = { url: 'https://example.com', title: 'Example' };

      // Act
      const bookmark1 = service.create(input);
      const bookmark2 = service.create(input);

      // Assert
      expect(bookmark1.id).not.toBe(bookmark2.id);
    });
  });
});
```

### AAA Rules

1. **One Act per test** -- a single behavior being tested
2. **Arrange can be shared** via `beforeEach` if common across tests in a `describe`
3. **Assert specifically** -- avoid `toMatchObject` when you can check exact values
4. **No logic in tests** -- no conditionals, loops, or try/catch in test bodies
5. **Keep Arrange minimal** -- only set up what this specific test needs

## Test Naming Conventions

### Pattern: "should [behavior] when [condition]"

```typescript
describe('UserService', () => {
  describe('create', () => {
    // Happy path
    it('should create user with valid data', () => { /* ... */ });
    it('should hash password before storing', () => { /* ... */ });
    it('should send welcome email after creation', () => { /* ... */ });

    // Error cases
    it('should throw ValidationError when email is invalid', () => { /* ... */ });
    it('should return 409 Conflict when email already exists', () => { /* ... */ });
    it('should throw when name exceeds 100 characters', () => { /* ... */ });

    // Edge cases
    it('should trim whitespace from name and email', () => { /* ... */ });
    it('should handle Unicode characters in name', () => { /* ... */ });
  });

  describe('findById', () => {
    it('should return user when ID exists', () => { /* ... */ });
    it('should return null when ID does not exist', () => { /* ... */ });
    it('should not return soft-deleted users', () => { /* ... */ });
  });
});
```

### Alternative: BDD-style with `context`

```typescript
describe('BookmarkService.search', () => {
  context('when query matches title', () => {
    it('returns matching bookmarks sorted by relevance', () => { /* ... */ });
  });

  context('when query matches tags', () => {
    it('returns bookmarks with matching tags', () => { /* ... */ });
  });

  context('when no results found', () => {
    it('returns empty array', () => { /* ... */ });
  });
});
```

### Naming Rules

- Start with `should` for clarity
- Describe the **behavior**, not the implementation
- Include the **condition** that triggers the behavior
- Be specific enough that a failing test name tells you what broke
- Avoid vague names: "should work", "handles errors", "basic test"

## TDD Workflow (Red-Green-Refactor)

### The Cycle

```
  ┌─────────┐     ┌─────────┐     ┌──────────┐
  │  RED     │────▶│  GREEN  │────▶│ REFACTOR │
  │ Write a  │     │ Make it │     │ Clean up │
  │ failing  │     │ pass    │     │ the code │
  │ test     │     │ (minimal│     │ (tests   │
  │          │     │  code)  │     │  stay    │
  └──────────┘     └─────────┘     │  green)  │
       ▲                           └─────┬────┘
       │                                 │
       └─────────────────────────────────┘
```

### TDD Example: Building a Slug Generator

```typescript
// Step 1: RED -- Write a failing test
describe('slugify', () => {
  it('should convert title to lowercase kebab-case', () => {
    expect(slugify('Hello World')).toBe('hello-world');
  });
});
// Run: FAIL (slugify is not defined)

// Step 2: GREEN -- Minimal code to pass
function slugify(title: string): string {
  return title.toLowerCase().replace(/\s+/g, '-');
}
// Run: PASS

// Step 3: Add next test (RED)
it('should remove special characters', () => {
  expect(slugify('Hello, World!')).toBe('hello-world');
});
// Run: FAIL

// Step 4: GREEN
function slugify(title: string): string {
  return title
    .toLowerCase()
    .replace(/[^a-z0-9\s-]/g, '')
    .replace(/\s+/g, '-');
}
// Run: PASS

// Step 5: Add edge case tests (RED)
it('should handle multiple spaces', () => {
  expect(slugify('hello   world')).toBe('hello-world');
});

it('should trim leading and trailing dashes', () => {
  expect(slugify(' Hello World ')).toBe('hello-world');
});

// Step 6: GREEN + REFACTOR
function slugify(title: string): string {
  return title
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9\s-]/g, '')
    .replace(/\s+/g, '-')
    .replace(/^-+|-+$/g, '');
}
```

### When TDD Works Best

- Business logic with clear input/output
- Algorithms and data transformations
- Validation rules
- Bug fixes (write test that reproduces bug first)

### When TDD Is Less Practical

- Exploratory/prototype code (write tests after settling on approach)
- UI layout and styling
- Integration with external systems (test after with mocks/stubs)

## Mocking Guidelines

### When to Mock

| Mock | Do Not Mock |
|------|------------|
| External APIs (HTTP calls) | The module under test |
| Database (in unit tests) | Simple data structures |
| File system | Pure functions |
| Time/Date (use fake timers) | Your own business logic |
| Email/SMS services | Standard library functions |
| Random number generation | Framework code |

### Dependency Injection (Preferred Over Module Mocking)

```typescript
// Production code: Accept dependencies as parameters
class BookmarkService {
  constructor(
    private store: BookmarkStore,
    private urlValidator: UrlValidator,
    private eventBus: EventBus,
  ) {}

  async create(input: CreateBookmarkInput): Promise<Bookmark> {
    await this.urlValidator.validate(input.url);
    const bookmark = await this.store.save(input);
    this.eventBus.emit('bookmark.created', bookmark);
    return bookmark;
  }
}

// Test: Inject test doubles
describe('BookmarkService.create', () => {
  it('should validate URL before saving', async () => {
    // Arrange: Create test doubles
    const store = { save: vi.fn().mockResolvedValue({ id: '1', ...input }) };
    const urlValidator = { validate: vi.fn().mockResolvedValue(true) };
    const eventBus = { emit: vi.fn() };
    const service = new BookmarkService(store, urlValidator, eventBus);
    const input = { url: 'https://example.com', title: 'Test' };

    // Act
    await service.create(input);

    // Assert
    expect(urlValidator.validate).toHaveBeenCalledWith('https://example.com');
    expect(store.save).toHaveBeenCalledWith(input);
    expect(eventBus.emit).toHaveBeenCalledWith('bookmark.created', expect.any(Object));
  });
});
```

### Vitest Mocking Patterns

```typescript
import { vi, describe, it, expect, beforeEach, afterEach } from 'vitest';

// Spy on a method
const spy = vi.spyOn(service, 'sendEmail');
expect(spy).toHaveBeenCalledTimes(1);

// Mock return value
const mock = vi.fn().mockReturnValue(42);
const asyncMock = vi.fn().mockResolvedValue({ id: '1' });

// Mock implementation
const mock = vi.fn().mockImplementation((a, b) => a + b);

// Reset between tests
beforeEach(() => {
  vi.clearAllMocks();   // Clear call history but keep implementation
});

afterEach(() => {
  vi.restoreAllMocks(); // Restore original implementations
});

// Fake timers
it('should expire after 30 minutes', () => {
  vi.useFakeTimers();
  const token = createToken();

  vi.advanceTimersByTime(30 * 60 * 1000); // 30 minutes

  expect(token.isExpired()).toBe(true);
  vi.useRealTimers();
});
```

## Coverage Guidelines

### Target Coverage by Layer

| Layer | Target | Rationale |
|-------|--------|-----------|
| Business logic | 90%+ | Core value, must be reliable |
| API routes | 80%+ | Contract testing, error paths |
| Utilities | 80%+ | Shared code, high reuse |
| Config/setup | No target | Framework code, not worth testing |
| Generated code | No target | Tested by generator |

### Coverage Rules

1. **Aim for 80%+ overall** on business logic
2. **Do not chase 100%** -- test value, not metrics
3. **Cover all error paths** -- errors are where bugs hide
4. **Cover boundary conditions** -- 0, 1, max, empty, null
5. **Every bug fix includes a regression test** -- prove the bug existed, prove it is fixed
6. **Measure branch coverage**, not just line coverage

### Vitest Coverage Configuration

```typescript
// vitest.config.ts
import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    coverage: {
      provider: 'v8',
      reporter: ['text', 'html', 'lcov'],
      include: ['src/**/*.ts'],
      exclude: ['src/**/*.test.ts', 'src/**/*.d.ts', 'src/index.ts'],
      thresholds: {
        branches: 80,
        functions: 80,
        lines: 80,
        statements: 80,
      },
    },
  },
});
```

## Test Organization

### File Structure

```
src/
├── services/
│   ├── bookmark.service.ts
│   └── bookmark.service.test.ts     # Co-located test
├── routes/
│   ├── bookmarks.ts
│   └── bookmarks.test.ts            # Integration test
└── utils/
    ├── slugify.ts
    └── slugify.test.ts
```

### Test Structure Pattern

```typescript
describe('ModuleName', () => {
  // Shared setup (if needed)
  let service: BookmarkService;

  beforeEach(() => {
    service = createTestService();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('methodName', () => {
    // Group: Happy path
    it('should [expected behavior] with valid input', () => { /* ... */ });
    it('should [another expected behavior]', () => { /* ... */ });

    // Group: Error cases
    it('should throw when [error condition]', () => { /* ... */ });
    it('should return 404 when [not found condition]', () => { /* ... */ });

    // Group: Edge cases
    it('should handle empty input', () => { /* ... */ });
    it('should handle maximum length input', () => { /* ... */ });
  });
});
```

## Test Data Management

### Factory Functions (Preferred)

```typescript
// test/factories.ts
function createBookmark(overrides: Partial<Bookmark> = {}): Bookmark {
  return {
    id: `bk_${Math.random().toString(36).slice(2)}`,
    url: 'https://example.com',
    title: 'Test Bookmark',
    description: null,
    tags: [],
    createdAt: new Date('2025-01-01T00:00:00Z'),
    updatedAt: new Date('2025-01-01T00:00:00Z'),
    ...overrides,
  };
}

// Usage in tests
it('should filter by tag', () => {
  const bookmarks = [
    createBookmark({ tags: ['javascript'] }),
    createBookmark({ tags: ['python'] }),
    createBookmark({ tags: ['javascript', 'react'] }),
  ];

  const result = filterByTag(bookmarks, 'javascript');
  expect(result).toHaveLength(2);
});
```

### Deterministic Tests

```typescript
// BAD: Non-deterministic (depends on current time)
it('should mark as expired', () => {
  const item = createItem({ expiresAt: new Date() });
  expect(item.isExpired()).toBe(true); // Might fail depending on timing
});

// GOOD: Use fake timers or fixed dates
it('should mark as expired when past expiry date', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2025-06-15T12:00:00Z'));

  const item = createItem({
    expiresAt: new Date('2025-06-14T12:00:00Z'), // Yesterday
  });

  expect(item.isExpired()).toBe(true);
  vi.useRealTimers();
});

// BAD: Depends on random values
it('should generate unique slug', () => {
  const slug = generateSlug('Hello');
  expect(slug).toMatch(/hello-[a-z0-9]+/); // Fragile pattern match
});

// GOOD: Inject randomness
it('should append random suffix to slug', () => {
  const randomFn = () => 'abc123';
  const slug = generateSlug('Hello', { randomFn });
  expect(slug).toBe('hello-abc123');
});
```

## Test Isolation

1. **Each test is independent** -- no test depends on another test's output
2. **Clean state between tests** -- use `beforeEach` to reset
3. **No shared mutable state** -- create fresh instances per test
4. **No test ordering dependencies** -- tests should run in any order
5. **No external side effects** -- tests should not write to disk, send emails, etc.

## Best Practices

1. **Write tests first** for bug fixes (reproduce, then fix)
2. **One assertion concept per test** -- test one behavior, assert related expectations
3. **Use descriptive test names** that explain the scenario
4. **Prefer real implementations** over mocks when possible
5. **Keep tests fast** -- unit tests under 10ms, integration under 2s
6. **Delete flaky tests** rather than skip them -- they erode trust
7. **Test behavior, not implementation** -- refactoring should not break tests
8. **Use factory functions** for test data, not raw object literals

## Anti-Patterns

- **Testing implementation details**: Checking internal state instead of observable behavior
- **Excessive mocking**: Mocking everything results in tests that always pass
- **Copy-paste test code**: Extract helpers and factory functions
- **Testing framework behavior**: Testing that Express routes work, not your handler logic
- **Giant test setup**: If Arrange is 50 lines, the unit under test is too big
- **Assert-less tests**: Tests that run code but never check results
- **Conditional test logic**: `if/else` in tests means you need multiple tests

## Sources & References

- Martin Fowler - Testing Pyramid: https://martinfowler.com/articles/practical-test-pyramid.html
- Kent Beck - Test-Driven Development by Example: https://www.oreilly.com/library/view/test-driven-development/0321146530/
- Vitest Documentation: https://vitest.dev/guide/
- Testing Library Guiding Principles: https://testing-library.com/docs/guiding-principles
- Google Testing Blog: https://testing.googleblog.com/
- Vladimir Khorikov - Unit Testing Principles: https://www.manning.com/books/unit-testing
- xUnit Test Patterns: http://xunitpatterns.com/
