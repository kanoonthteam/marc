---
name: testing-verification
description: QA verification methodology — AC verification, edge case testing, regression strategy, and bug reporting
---

# Testing Verification

## Purpose

Guide the QA agent in systematically verifying implementations against acceptance criteria, identifying edge cases, executing regression testing, and reporting bugs with clear, actionable information.

## AC Verification Process

### Systematic Verification Workflow

```
1. Read AC from tasks.json
        │
2. Identify verification method
   (test output / API call / code inspection)
        │
3. Set up test environment
   (install deps, run migrations, seed data)
        │
4. Execute verification per AC
        │
5. Test edge cases
        │
6. Run regression tests
        │
7. Perform code quality checks
        │
8. Write QA report
```

### Step 1: Pre-Verification Setup

```bash
# Pull latest changes
git pull origin feature-branch

# Install dependencies
pnpm install

# Run database migrations (if applicable)
pnpm db:migrate

# Ensure clean test environment
pnpm test -- --run  # Verify existing tests pass first
```

### Step 2: AC Verification Matrix

For each acceptance criterion, document the verification method and result.

```markdown
| # | Acceptance Criterion | Method | Result | Evidence |
|---|---------------------|--------|--------|----------|
| 1 | POST /bookmarks returns 201 | API test | PASS | test: bookmarks.test.ts:15 |
| 2 | URL validation rejects invalid URLs | Unit test | PASS | test: validation.test.ts:42 |
| 3 | Duplicate URLs return 409 | API test | FAIL | Returns 500 instead of 409 |
| 4 | Tags are stored as array | Code review | PASS | store.ts:28 uses string[] |
```

### Step 3: Verification Methods

**Test Output Verification:**
```bash
# Run specific test file
pnpm test -- src/routes/bookmarks.test.ts

# Run tests matching a pattern
pnpm test -- --grep "should create bookmark"

# Run with verbose output
pnpm test -- --reporter=verbose
```

**API Verification (curl/httpie):**
```bash
# Test endpoint directly
curl -s -X POST http://localhost:3000/api/v1/bookmarks \
  -H "Content-Type: application/json" \
  -d '{"url": "https://example.com", "title": "Test"}' | jq .

# Check status code
curl -s -o /dev/null -w "%{http_code}" \
  -X POST http://localhost:3000/api/v1/bookmarks \
  -H "Content-Type: application/json" \
  -d '{"url": "invalid", "title": "Test"}'
```

**Code Inspection:**
```bash
# Verify implementation details
# - Check that password is hashed (grep for bcrypt/argon2)
# - Check that validation is applied before DB write
# - Check that error messages match AC specifications
```

## Edge Case Testing

### Input Validation Edge Cases

#### String Inputs

```typescript
describe('Edge Cases: String Inputs', () => {
  // Empty values
  it('should reject empty string', () => {
    expect(() => validate({ title: '' })).toThrow();
  });

  it('should reject whitespace-only string', () => {
    expect(() => validate({ title: '   ' })).toThrow();
  });

  // Length boundaries
  it('should accept title at maximum length (200 chars)', () => {
    const title = 'a'.repeat(200);
    expect(() => validate({ title })).not.toThrow();
  });

  it('should reject title exceeding maximum length', () => {
    const title = 'a'.repeat(201);
    expect(() => validate({ title })).toThrow();
  });

  // Special characters
  it('should handle Unicode characters', () => {
    const result = validate({ title: 'Bookmarks' });
    expect(result.title).toContain('');
  });

  it('should handle emoji in title', () => {
    const result = validate({ title: 'My Awesome Bookmark' });
    expect(result.title).toBe('My Awesome Bookmark');
  });

  it('should strip HTML tags from title', () => {
    const result = validate({ title: '<script>alert("xss")</script>Hello' });
    expect(result.title).not.toContain('<script>');
  });

  // SQL injection attempt
  it('should handle SQL injection strings safely', () => {
    const result = validate({ title: "'; DROP TABLE bookmarks; --" });
    expect(result.title).toBe("'; DROP TABLE bookmarks; --");
    // The DB layer should use parameterized queries
  });
});
```

#### Numeric Inputs

```typescript
describe('Edge Cases: Numeric Inputs', () => {
  it('should handle zero', () => { /* ... */ });
  it('should reject negative numbers', () => { /* ... */ });
  it('should handle maximum safe integer', () => {
    expect(() => validate({ page: Number.MAX_SAFE_INTEGER })).not.toThrow();
  });
  it('should reject NaN', () => {
    expect(() => validate({ page: NaN })).toThrow();
  });
  it('should reject Infinity', () => {
    expect(() => validate({ page: Infinity })).toThrow();
  });
  it('should reject floating point for integer fields', () => {
    expect(() => validate({ page: 1.5 })).toThrow();
  });
});
```

#### Null and Undefined

```typescript
describe('Edge Cases: Null/Undefined', () => {
  it('should reject null for required fields', () => {
    expect(() => validate({ title: null })).toThrow();
  });

  it('should reject undefined for required fields', () => {
    expect(() => validate({ title: undefined })).toThrow();
  });

  it('should accept null for optional fields', () => {
    const result = validate({ title: 'Test', description: null });
    expect(result.description).toBeNull();
  });

  it('should handle missing body', () => {
    const res = await request(app).post('/api/v1/bookmarks').send();
    expect(res.status).toBe(400);
  });

  it('should handle empty object body', () => {
    const res = await request(app).post('/api/v1/bookmarks').send({});
    expect(res.status).toBe(422);
  });
});
```

### State Edge Cases

```typescript
describe('Edge Cases: State', () => {
  // Empty state
  it('should return empty array when no bookmarks exist', async () => {
    const res = await request(app).get('/api/v1/bookmarks');
    expect(res.body.data).toEqual([]);
    expect(res.body.meta.total).toBe(0);
  });

  // Single item
  it('should handle list with exactly one item', async () => {
    await createBookmark({ title: 'Only One' });
    const res = await request(app).get('/api/v1/bookmarks');
    expect(res.body.data).toHaveLength(1);
  });

  // Pagination boundary
  it('should handle page beyond total pages', async () => {
    // 5 items, requesting page 100
    const res = await request(app).get('/api/v1/bookmarks?page=100&perPage=20');
    expect(res.body.data).toEqual([]);
    expect(res.body.meta.page).toBe(100);
  });

  // Concurrent modifications
  it('should handle concurrent creates without data loss', async () => {
    const promises = Array.from({ length: 10 }, (_, i) =>
      request(app).post('/api/v1/bookmarks').send({
        url: `https://example.com/${i}`,
        title: `Bookmark ${i}`,
      }),
    );

    const results = await Promise.all(promises);
    results.forEach(res => expect(res.status).toBe(201));

    const list = await request(app).get('/api/v1/bookmarks');
    expect(list.body.data.length).toBeGreaterThanOrEqual(10);
  });

  // Not found
  it('should return 404 for non-existent ID', async () => {
    const res = await request(app).get('/api/v1/bookmarks/nonexistent-id');
    expect(res.status).toBe(404);
  });

  // Already deleted
  it('should return 404 when deleting already-deleted resource', async () => {
    const created = await request(app).post('/api/v1/bookmarks').send(validInput);
    await request(app).delete(`/api/v1/bookmarks/${created.body.id}`);

    const res = await request(app).delete(`/api/v1/bookmarks/${created.body.id}`);
    expect(res.status).toBe(404);
  });
});
```

### Edge Case Checklist

```markdown
## Input Validation
- [ ] Empty/null/undefined values for required fields
- [ ] Whitespace-only strings
- [ ] Maximum length strings (at boundary and beyond)
- [ ] Minimum length strings (at boundary)
- [ ] Special characters: Unicode, emoji, RTL text, zero-width chars
- [ ] HTML/script injection attempts
- [ ] SQL injection strings
- [ ] Very long strings (10,000+ characters)
- [ ] Boundary numbers: 0, -1, MAX_SAFE_INTEGER, NaN, Infinity
- [ ] Invalid types (string where number expected)
- [ ] Extra/unknown fields in request body

## State Handling
- [ ] Empty state (no data in store)
- [ ] Single item
- [ ] Exactly at pagination boundary (perPage items)
- [ ] Page beyond total pages
- [ ] Concurrent modifications
- [ ] Resource not found (invalid ID)
- [ ] Resource already deleted
- [ ] Duplicate resource creation

## Error Handling
- [ ] Missing Content-Type header
- [ ] Invalid JSON body
- [ ] Missing Authorization header (if applicable)
- [ ] Expired token (if applicable)
- [ ] Validation errors return field-level messages
- [ ] Internal errors do not leak stack traces
```

## Flutter E2E Verification (Patrol)

### Running Patrol Tests

```bash
# Run all Patrol E2E journey tests
cd app && ./scripts/patrol_test.sh

# Run a single journey test
cd app && patrol test -t patrol_test/invoices_test.dart
```

### Reading Patrol Output

- `✅` = test passed, `❌` = test failed
- Stack traces include file:line numbers — jump directly to the failing assertion
- Summary at the end shows total passed/failed count

### Interpreting Common Failures

| Error | Meaning | Fix |
|-------|---------|-----|
| `pumpAndSettle timed out` | Screen has active timer/animation preventing settle | Use `$.pump(Duration(...))` instead of `pumpAndSettle()` |
| `Expected: true / Actual: <false>` + line | Widget not found by key/finder | Check `TestKeys` constants match widget code |
| `xcodebuild exited with code 65` | Known Patrol CLI bug | If output contains "All tests passed!" — tests DID pass |
| `No matching widget found` | Element doesn't exist at check time | Add polling loop (500ms × 20 attempts) before assertion |
| `Timeout waiting for widget` | Data loading took too long | Check API server is running; increase `visibleTimeout` |

### Verifying Journey Coverage

Each journey test should cover its domain's acceptance criteria:
- **Auth journey**: login errors, OTP flow, successful login, logout
- **Home journey**: dashboard data, navigation to sub-screens
- **Invoices journey**: list, filters, detail, payment flow
- **Maintenance journey**: list, detail, form submission, categories
- **Parcels journey**: pending/collected tabs, refresh
- **Vehicles journey**: unauthenticated redirect, vehicle list, edge cases

### Flaky Test Triage

1. **Retry once** — transient failures happen (network, simulator lag)
2. **Check API server** — is `rails s` running and seeded?
3. **Check simulator state** — is it booted? Is the app in a clean state?
4. **Check for timer/animation issues** — look for `pumpAndSettle timed out`
5. **If consistently failing** — the test or the feature has a real bug; investigate

## Regression Testing Strategy

### Risk-Based Regression

Not everything needs retesting. Focus on areas most likely to be affected by changes.

```
┌─────────────────────────────────────────────┐
│ Changed: src/routes/bookmarks.ts            │
│                                             │
│ Must retest:                                │
│  - All bookmark CRUD operations             │
│  - Bookmark validation                      │
│  - Bookmark search (depends on bookmarks)   │
│                                             │
│ Can skip:                                   │
│  - Health check endpoint                    │
│  - Category CRUD (independent module)       │
│  - Static file serving                      │
│                                             │
│ Watch for:                                  │
│  - Import changes (shared modules)          │
│  - Store changes (affects all modules)      │
│  - Middleware changes (affects all routes)   │
└─────────────────────────────────────────────┘
```

### Regression Test Selection

| Change Type | Regression Scope |
|-------------|-----------------|
| Route handler | All tests for that resource |
| Shared utility | All consumers of that utility |
| Middleware | All route tests |
| Store/Database | All integration tests |
| Package update | Full test suite |
| Configuration | Full test suite |
| Single function | Unit tests for that function + callers |

### Running Focused Regression Tests

```bash
# Run tests for changed files only
pnpm test -- --changed

# Run tests related to specific files
pnpm test -- src/routes/bookmarks.test.ts src/routes/search.test.ts

# Run full suite (for store/middleware/config changes)
pnpm test
```

## Code Quality Checks

### Pre-QA Quality Checklist

```markdown
## Code Quality
- [ ] New code has tests (not just coverage padding)
- [ ] Tests follow AAA pattern (Arrange/Act/Assert)
- [ ] Test names describe behavior: "should X when Y"
- [ ] Error messages are user-friendly (not technical jargon)
- [ ] No console.log/print statements left in production code
- [ ] No commented-out code
- [ ] No hardcoded test data in production code
- [ ] No TODO comments without ticket references
- [ ] TypeScript: no `any` types (use `unknown` or specific types)
- [ ] Lint passes: `pnpm lint`
- [ ] Type check passes: `pnpm typecheck` (if configured)

## Security Quick Check
- [ ] No secrets or API keys in code
- [ ] Input validation on all user-facing endpoints
- [ ] Error responses do not leak internal details
- [ ] SQL queries use parameterized statements
```

## Bug Report Format

### Template

```markdown
## Bug Report

**Title:** [Concise description starting with verb: "Returns 500...", "Fails to validate..."]

**Severity:** Critical | Major | Minor
**Priority:** P0 (fix immediately) | P1 (fix this sprint) | P2 (fix next sprint) | P3 (backlog)

**Environment:**
- Branch: feature/bookmarks
- Commit: abc1234
- Node: v22.x
- OS: macOS / Linux

**Steps to Reproduce:**
1. Start the server: `pnpm dev`
2. Send POST request:
   ```bash
   curl -X POST http://localhost:3000/api/v1/bookmarks \
     -H "Content-Type: application/json" \
     -d '{"url": "", "title": "Test"}'
   ```
3. Observe the response

**Expected Result:**
- Status: 422 Unprocessable Entity
- Body: `{ "error": "Validation failed", "details": { "url": ["is required"] } }`

**Actual Result:**
- Status: 500 Internal Server Error
- Body: `{ "error": "Cannot read properties of undefined (reading 'match')" }`

**Evidence:**
- Screenshot/terminal output: [attach or paste]
- Relevant log output: [paste]
- Test file that reproduces: `bookmarks.test.ts:42`

**Root Cause (if identified):**
URL validation in `bookmarks.ts:15` does not check for empty string before regex match.

**Suggested Fix:**
Add empty string check before URL regex validation.
```

### Severity vs Priority Matrix

| | P0 - Now | P1 - Sprint | P2 - Next Sprint | P3 - Backlog |
|---|---------|-------------|-------------------|--------------|
| **Critical** | Data loss, security hole, complete feature failure | Auth bypass on non-critical endpoint | Edge case causing crash | -- |
| **Major** | -- | Feature partially broken, wrong status codes | Missing validation on optional field | UI inconsistency |
| **Minor** | -- | -- | Typo in user-facing message | Code style issue |

### Severity Classification Guide

| Severity | Definition | Examples |
|----------|-----------|---------|
| **Critical** | Feature is broken, data loss possible, security vulnerability | Auth bypass, data not saved, unhandled exception crashes server |
| **Major** | Feature partially works, workaround exists, wrong behavior | Wrong status code returned, filter doesn't work, missing validation |
| **Minor** | Cosmetic, non-functional, improvement opportunity | Typo in error message, inconsistent response format, missing header |

## QA Report Template

```markdown
## QA Report

**Task:** T003 - Add bookmark search
**Branch:** feature/bookmark-search
**Tested by:** QA Agent
**Date:** 2025-01-15

### Summary
5/6 acceptance criteria passed. 1 major bug found.

### AC Verification

| # | Criterion | Status | Notes |
|---|-----------|--------|-------|
| 1 | GET /bookmarks?search=term returns matching bookmarks | PASS | Tested with multiple terms |
| 2 | Search matches title and URL | PASS | Case-insensitive match verified |
| 3 | Search matches tags | FAIL | BUG-001: Tag search not implemented |
| 4 | Empty search returns all bookmarks | PASS | Returns full list |
| 5 | Search is case-insensitive | PASS | "React" and "react" return same results |
| 6 | Returns empty array when no matches | PASS | Tested with nonsense query |

### Edge Cases Tested
- [x] Empty search query (returns all)
- [x] Special characters in search query
- [x] Very long search query (500+ chars)
- [x] Unicode search terms
- [x] Multiple word search

### Bugs Found

**BUG-001: Tag search not implemented**
- Severity: Major
- Search only checks title and URL, not tags array
- AC #3 requires tag matching
- See bug report for details

### Regression
- [x] All existing tests pass (43/43)
- [x] Bookmark CRUD unaffected
- [x] Category endpoints unaffected

### Recommendation
**CONDITIONAL PASS** - Fix BUG-001 (tag search) before merge.
```

## Risk-Based Testing Prioritization

### Risk Assessment Matrix

| Factor | Low Risk | Medium Risk | High Risk |
|--------|----------|-------------|-----------|
| User impact | Internal tool | Some users affected | All users affected |
| Data impact | Read-only | Temporary data | Permanent data changes |
| Complexity | Simple CRUD | Business logic | Complex workflows |
| Change size | < 50 LOC | 50-200 LOC | > 200 LOC |
| Dependencies | None | Internal only | External services |

### Testing Depth by Risk

```
High Risk   → Full verification: All AC + all edge cases + security + performance
Medium Risk → Standard verification: All AC + key edge cases
Low Risk    → Smoke test: Happy path + basic error cases
```

## Best Practices

1. **Verify every AC individually** -- do not assume passing tests mean ACs are met
2. **Test edge cases that developers often miss** -- empty, null, boundaries, Unicode
3. **Run the full test suite** before signing off (catch regressions)
4. **Write reproducible bug reports** -- include exact steps and curl commands
5. **Focus testing effort on high-risk areas** -- new code, complex logic, data mutations
6. **Verify error messages are user-friendly** -- not stack traces or technical jargon
7. **Check that new code has meaningful tests** -- not just coverage padding
8. **Report blockers immediately** -- do not wait for the full QA cycle

## Anti-Patterns

- **Only running automated tests** -- manual verification catches things tests miss
- **Testing only the happy path** -- most bugs live in error paths and edge cases
- **Vague bug reports** ("it doesn't work") -- always include steps, expected, actual
- **Skipping regression** -- "I only changed one file" can still break unrelated features
- **Severity inflation** -- not every bug is critical; accurate severity helps prioritization
- **QA as gatekeeper** -- QA should enable quality, not block releases

## Sources & References

- ISTQB Testing Fundamentals: https://www.istqb.org/certifications/certified-tester-foundation-level
- Google Testing Blog - Risk-Based Testing: https://testing.googleblog.com/
- Microsoft Test Case Design: https://learn.microsoft.com/en-us/previous-versions/visualstudio/visual-studio-2013/dd286731(v=vs.120)
- OWASP Testing Guide: https://owasp.org/www-project-web-security-testing-guide/
- James Bach - Exploratory Testing: https://www.satisfice.com/exploratory-testing
- Ministry of Testing: https://www.ministryoftesting.com/
