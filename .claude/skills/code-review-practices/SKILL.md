---
name: code-review-practices
description: Code review best practices including SOLID checks, security review, AI-generated code quality, and PR automation
---

# Code Review Practices

## Overview

Code review is a quality gate where peers examine code changes before merging. Effective reviews catch bugs, improve design, share knowledge, and maintain code quality. This skill covers what to look for, how to give feedback, PR templates, automation, and handling AI-generated code.

## What to Review

### SOLID Principles Check

#### Single Responsibility Principle (SRP)

```typescript
// VIOLATION: Class does too many things
class OrderService {
  async createOrder(data: OrderInput) { /* order logic */ }
  async sendEmail(to: string, body: string) { /* email logic */ }
  async generatePDF(order: Order) { /* PDF logic */ }
  async calculateTax(items: Item[]) { /* tax logic */ }
}

// BETTER: Each class has one reason to change
class OrderService {
  constructor(
    private taxCalculator: TaxCalculator,
    private notifier: OrderNotifier,
    private invoiceGenerator: InvoiceGenerator,
  ) {}

  async createOrder(data: OrderInput) {
    const tax = this.taxCalculator.calculate(data.items);
    const order = await this.saveOrder(data, tax);
    await this.notifier.notifyOrderCreated(order);
    return order;
  }
}
```

**Review question**: "Does this class/function have more than one reason to change?"

#### Open-Closed Principle (OCP)

```typescript
// VIOLATION: Adding a new payment type requires modifying existing code
function processPayment(type: string, amount: number) {
  if (type === 'credit') { /* ... */ }
  else if (type === 'debit') { /* ... */ }
  else if (type === 'crypto') { /* ... */ }  // <-- Must modify existing function
}

// BETTER: Open for extension, closed for modification
interface PaymentProcessor {
  process(amount: number): Promise<PaymentResult>;
}

class CreditCardProcessor implements PaymentProcessor {
  async process(amount: number) { /* ... */ }
}

class CryptoProcessor implements PaymentProcessor {
  async process(amount: number) { /* ... */ }
}

// New payment types added without modifying existing code
const processors: Record<string, PaymentProcessor> = {
  credit: new CreditCardProcessor(),
  crypto: new CryptoProcessor(),
};
```

**Review question**: "Can we add new behavior without changing existing code?"

#### Interface Segregation Principle (ISP)

```typescript
// VIOLATION: Fat interface forces unnecessary implementations
interface Repository {
  findById(id: string): Promise<Entity>;
  findAll(): Promise<Entity[]>;
  save(entity: Entity): Promise<void>;
  delete(id: string): Promise<void>;
  bulkInsert(entities: Entity[]): Promise<void>;
  runMigration(): Promise<void>;  // Not all repos need this
  exportToCSV(): Promise<string>; // Not all repos need this
}

// BETTER: Smaller, focused interfaces
interface Readable<T> {
  findById(id: string): Promise<T>;
  findAll(): Promise<T[]>;
}

interface Writable<T> {
  save(entity: T): Promise<void>;
  delete(id: string): Promise<void>;
}

interface BulkWritable<T> {
  bulkInsert(entities: T[]): Promise<void>;
}

// Compose what you need
class OrderRepository implements Readable<Order>, Writable<Order> {
  // Only implement what's needed
}
```

**Review question**: "Are consumers forced to depend on methods they don't use?"

### Security Checklist

```markdown
## Security Review Checklist

### Input Validation
- [ ] All user input is validated (type, length, format, range)
- [ ] SQL queries use parameterized statements (no string concatenation)
- [ ] HTML output is escaped to prevent XSS
- [ ] File uploads are validated (type, size, content scanning)
- [ ] URL parameters and headers are validated

### Authentication & Authorization
- [ ] Authentication is required for protected endpoints
- [ ] Authorization checks on every request (not just UI hiding)
- [ ] No hardcoded credentials or API keys
- [ ] Passwords hashed with bcrypt/argon2 (never MD5/SHA)
- [ ] JWT tokens have appropriate expiration

### Sensitive Data
- [ ] No secrets in code, logs, or error messages
- [ ] PII is not logged or stored unnecessarily
- [ ] Sensitive data encrypted at rest
- [ ] API responses do not leak internal details (stack traces, DB errors)

### Dependencies
- [ ] New dependencies are from trusted sources
- [ ] No known vulnerabilities (check `npm audit`, Snyk)
- [ ] Dependency versions are pinned

### Configuration
- [ ] No debug/dev settings in production code
- [ ] CORS is properly configured (not `*` in production)
- [ ] Rate limiting on public endpoints
```

### Performance Checklist

```markdown
## Performance Review Checklist

### Database
- [ ] No N+1 query patterns (loading related data in a loop)
- [ ] Queries use appropriate indexes
- [ ] Large result sets are paginated
- [ ] No unnecessary `SELECT *` (select only needed columns)
- [ ] Bulk operations instead of per-record operations

### Memory
- [ ] No unbounded collections (arrays that grow without limit)
- [ ] Large objects are released/garbage collected when done
- [ ] Streams used for large file processing (not loading into memory)
- [ ] No memory leaks from event listeners or intervals not cleaned up

### Frontend (if applicable)
- [ ] No unnecessary re-renders (React: proper deps in useEffect, memo where needed)
- [ ] Images are properly sized and lazy-loaded
- [ ] Bundle size impact is reasonable
- [ ] No blocking operations on the main thread
- [ ] Virtualization for long lists (>100 items)

### API
- [ ] Appropriate caching headers set
- [ ] Response payloads are not excessively large
- [ ] Async operations are properly awaited (no fire-and-forget)
- [ ] Timeouts set for external service calls
```

### N+1 Query Example

```typescript
// BAD: N+1 queries (1 query for orders + N queries for customers)
const orders = await db.query('SELECT * FROM orders');
for (const order of orders) {
  order.customer = await db.query('SELECT * FROM customers WHERE id = ?', [order.customerId]);
  // Each iteration executes a separate query!
}

// GOOD: 2 queries total
const orders = await db.query(`
  SELECT o.*, c.name as customer_name, c.email as customer_email
  FROM orders o
  JOIN customers c ON o.customer_id = c.id
`);

// ALSO GOOD: Batch loading
const orders = await db.query('SELECT * FROM orders');
const customerIds = [...new Set(orders.map(o => o.customerId))];
const customers = await db.query('SELECT * FROM customers WHERE id IN (?)', [customerIds]);
const customerMap = new Map(customers.map(c => [c.id, c]));
orders.forEach(o => { o.customer = customerMap.get(o.customerId); });
```

## AI-Generated Code Quality Control

### What to Watch For

```markdown
## AI Code Review Checklist

### Correctness
- [ ] Does it actually solve the stated problem?
- [ ] Are there off-by-one errors or boundary condition bugs?
- [ ] Are error cases handled (not just happy path)?
- [ ] Are types correct and complete (no unnecessary `any`)?

### Hallucination Detection
- [ ] API calls use real, existing endpoints (not made-up APIs)
- [ ] Library imports exist and have correct method signatures
- [ ] Configuration options are valid for the actual version used
- [ ] File paths and environment variables are correct for this project
- [ ] Does the imported package actually exist in the registry?

### Project Consistency
- [ ] Follows project conventions (naming, file structure, patterns)
- [ ] Uses project's existing utilities instead of reimplementing
- [ ] Consistent error handling with the rest of the codebase
- [ ] Uses the same libraries the project already depends on (not alternatives)

### Testing
- [ ] Tests actually test meaningful behavior (not just calling the function)
- [ ] Edge cases and error paths are tested
- [ ] Test assertions are correct (not just checking that something is truthy)
- [ ] Mock/stub setup is realistic (not masking bugs)
- [ ] Tests would fail if the implementation was broken

### Security
- [ ] No hardcoded secrets or API keys (AI models sometimes generate these)
- [ ] Input validation is not bypassed
- [ ] No debug/logging code left in production paths
```

### Common AI Code Issues

```typescript
// ISSUE 1: Non-existent API
import { createRouter } from 'express-magic-router'; // This package doesn't exist!

// ISSUE 2: Incorrect API usage
const result = await fetch(url, { method: 'GET', body: JSON.stringify(data) });
// GET requests should not have a body

// ISSUE 3: Subtly wrong logic
function isLeapYear(year: number): boolean {
  return year % 4 === 0; // Missing: && (year % 100 !== 0 || year % 400 === 0)
}

// ISSUE 4: Overly verbose when project has utilities
// AI generates manual date formatting when project uses date-fns
const formatted = `${date.getFullYear()}-${String(date.getMonth()+1).padStart(2,'0')}-...`;
// Project already has: import { format } from 'date-fns';

// ISSUE 5: Tests that always pass
test('handles error', async () => {
  try {
    await riskyFunction();
  } catch (error) {
    expect(error).toBeDefined(); // This test passes even if no error is thrown!
  }
});
// CORRECT:
test('handles error', async () => {
  await expect(riskyFunction()).rejects.toThrow('expected message');
});
```

## Constructive Feedback Patterns

### Language Guide

| Instead of | Use |
|-----------|-----|
| "This is wrong" | "I think this might not handle the case where..." |
| "Why did you do this?" | "What was the reasoning behind this approach?" |
| "You should..." | "Consider..." or "What about..." |
| "This is confusing" | "I had trouble understanding this part. Could we add a comment or rename to clarify?" |
| "This is bad practice" | "In my experience, [alternative] tends to be more maintainable because..." |

### Comment Categories

```markdown
## Comment Prefixes

**[blocking]**: Must be addressed before merge
  "This SQL query is vulnerable to injection. Please use parameterized queries."

**[suggestion]**: Recommended but not required
  "Consider extracting this into a utility function since it's used in 3 places."

**[question]**: Seeking understanding
  "Could you explain why we're catching and silencing this error?"

**[nit]**: Minor style/formatting issue
  "Nit: This variable name could be more descriptive (e.g., `userEmailAddress` vs `email`)."

**[praise]**: Positive feedback (important!)
  "Great use of the builder pattern here. This makes the test setup much more readable."

**[thought]**: Sharing a consideration, not necessarily actionable
  "Thought: We might want to add rate limiting to this endpoint in the future."
```

### Effective Review Comments

```markdown
## Bad Review Comment
"This function is too long."

## Good Review Comment
"[suggestion] This function is ~80 lines and handles validation, transformation,
and persistence. Consider splitting into three functions:
- `validateInput()` - lines 5-25
- `transformData()` - lines 26-55
- `persistOrder()` - lines 56-80

This would make each function independently testable and the overall flow
easier to follow."
```

## PR Templates

### Feature PR Template

```markdown
## Description
<!-- What does this PR do? Why? -->

## Changes
- [ ] Added/Modified code
- [ ] Added/Updated tests
- [ ] Updated documentation

## Type of Change
- [ ] New feature
- [ ] Bug fix
- [ ] Refactoring (no functional change)
- [ ] Documentation
- [ ] CI/CD
- [ ] Dependencies

## Related Issues
<!-- Link to Jira/GitHub issues -->
Closes #123

## Test Plan
<!-- How was this tested? -->
- [ ] Unit tests added/updated
- [ ] Integration tests passing
- [ ] Manual testing performed

### Manual Testing Steps
1. Step one
2. Step two
3. Expected result

## Screenshots
<!-- If UI changes, add before/after screenshots -->

## Checklist
- [ ] Code follows project conventions
- [ ] Self-reviewed the diff
- [ ] No console.log or debug code left
- [ ] No sensitive data exposed
- [ ] Tests cover happy path and error cases
- [ ] Documentation updated (if applicable)

## Deployment Notes
<!-- Any special deployment considerations? -->
```

### Bug Fix PR Template

```markdown
## Bug Description
<!-- What was happening? Include error messages, screenshots -->

## Root Cause
<!-- Why was it happening? -->

## Fix
<!-- What was changed and why this fixes it -->

## Regression Test
<!-- What test was added to prevent this from happening again? -->

## Affected Areas
<!-- What other areas might be impacted by this change? -->
```

## Review Automation

### CODEOWNERS

```
# .github/CODEOWNERS
# Automatically assign reviewers based on files changed

# Backend
/src/api/          @myorg/backend-team
/src/services/     @myorg/backend-team

# Frontend
/src/components/   @myorg/frontend-team
/src/pages/        @myorg/frontend-team

# Security-sensitive
/src/auth/         @myorg/security-team
```

### Auto-Assign Reviewers (GitHub Actions)

```yaml
# .github/workflows/auto-assign.yml
name: Auto Assign
on:
  pull_request:
    types: [opened, ready_for_review]

jobs:
  assign:
    runs-on: ubuntu-latest
    steps:
      - uses: kentaro-m/auto-assign-action@v2
        with:
          configuration-path: '.github/auto-assign.yml'
```

```yaml
# .github/auto-assign.yml
addReviewers: true
addAssignees: true
numberOfReviewers: 2
reviewers:
  - alice
  - bob
  - carol
  - dave
skipKeywords:
  - wip
  - draft
```

### PR Size Labels (GitHub Actions)

```yaml
# .github/workflows/pr-size.yml
name: PR Size Label
on:
  pull_request:
    types: [opened, synchronize]

jobs:
  size:
    runs-on: ubuntu-latest
    steps:
      - uses: CodelyTV/pr-size-labeler@v1
        with:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          xs_label: 'size/XS'
          xs_max_size: 10
          s_label: 'size/S'
          s_max_size: 50
          m_label: 'size/M'
          m_max_size: 200
          l_label: 'size/L'
          l_max_size: 500
          xl_label: 'size/XL'
          fail_if_xl: true    # Block very large PRs
          message_if_xl: |
            This PR has over 500 lines changed. Please consider
            splitting it into smaller, focused PRs for easier review.
```

### Automated Checks in PR

```yaml
# .github/workflows/pr-checks.yml
name: PR Checks
on:
  pull_request:
    branches: [main]

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: pnpm install --frozen-lockfile
      - run: pnpm lint

  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: pnpm install --frozen-lockfile
      - run: pnpm test -- --coverage
      - uses: davelosert/vitest-coverage-report-action@v2

  security:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: pnpm audit --audit-level moderate
      - uses: aquasecurity/trivy-action@master
        with:
          scan-type: 'fs'
          severity: 'HIGH,CRITICAL'
```

## Review Turnaround Time Targets

| Metric | Target | Why |
|--------|--------|-----|
| Time to first review | < 4 hours | Keeps author productive |
| Time to approval | < 24 hours | Prevents context switching |
| PR size | < 300 lines changed | Larger PRs get worse reviews |
| Review comments addressed | < 4 hours | Momentum |
| Number of review rounds | < 3 | Diminishing returns after 3 |

### Strategies to Meet Targets

1. **Small PRs** -- 100-300 lines ideal; easier to review and faster turnaround
2. **Review-first culture** -- reviews take priority over new feature work
3. **Time-box reviews** -- 30-60 minutes max per review session
4. **Draft PRs** -- share early for directional feedback
5. **Pair programming as review** -- for complex changes, pair instead of async review

## Pair Programming as Review

### When to Pair vs Async Review

| Pair Programming | Async Code Review |
|-----------------|-------------------|
| Complex algorithms | Straightforward features |
| Security-critical code | Routine changes |
| Onboarding new team members | Well-understood patterns |
| Design decisions needed | Implementation of agreed design |
| Time-sensitive fixes | Standard development flow |

### Pair Programming Styles

- **Driver/Navigator**: One types, one thinks ahead
- **Ping-Pong**: Switch roles with each test/implementation
- **Strong-Style**: "For an idea to go from your head to the computer, it must go through someone else's hands"

## Best Practices

1. **Review the design first, then the details** -- architecture mistakes are more costly than typos
2. **Be kind and constructive** -- code is not the author; review the code, not the person
3. **Explain the "why"** behind suggestions -- "This could cause X" is better than "Don't do this"
4. **Praise good code** -- positive feedback is as important as catching bugs
5. **Use checklists** -- consistent quality without relying on memory
6. **Keep PRs small** -- 100-300 lines changed; break larger features into stacked PRs
7. **Review AI-generated code more carefully** -- verify imports, APIs, and edge cases exist
8. **Automate what can be automated** -- linting, formatting, security scanning, size labels
9. **Prioritize reviews** -- blocking a teammate is more expensive than pausing your own work
10. **Learn from reviews** -- track common patterns; convert repeated feedback into linting rules

## Anti-Patterns

1. **Rubber-stamping** -- approving without reading; defeats the purpose
2. **Gatekeeping** -- blocking PRs for stylistic preferences, not real issues
3. **Review after deploy** -- reviews must happen before merge, not after
4. **Bikeshedding** -- spending 30 minutes debating variable names while missing a SQL injection
5. **Only negative feedback** -- never acknowledging good work erodes team morale
6. **Reviewing 1000+ line PRs** -- split them; large reviews miss critical issues
7. **"Looks good to me" on every PR** -- add substance to your review
8. **Personal attacks in reviews** -- "This is terrible" has no place in professional review
9. **Letting PRs sit for days** -- stale PRs cause merge conflicts and context loss
10. **Not testing locally** -- for non-trivial changes, pull the branch and verify

## Sources & References

- https://google.github.io/eng-practices/review/ -- Google's Code Review Guidelines
- https://github.blog/developer-skills/github/how-to-write-the-perfect-pull-request/ -- GitHub PR guide
- https://www.conventionalcomments.org/ -- Conventional Comments
- https://docs.github.com/en/repositories/managing-your-repositorys-settings-and-features/customizing-your-repository/about-code-owners -- CODEOWNERS documentation
- https://smartbear.com/learn/code-review/best-practices-for-peer-code-review/ -- SmartBear code review practices
- https://www.atlassian.com/agile/software-development/code-reviews -- Atlassian code review guide
- https://docs.github.com/en/communities/using-templates-to-encourage-useful-issues-and-pull-requests -- PR templates
