---
name: playwright-testing
description: Playwright E2E testing patterns, Page Object Model, cross-browser testing, and CI integration
---

# Playwright E2E Testing

## Overview

Playwright is a modern end-to-end testing framework by Microsoft that supports Chromium, Firefox, and WebKit with a single API. It provides auto-waiting, web-first assertions, and powerful tooling for reliable, fast tests.

## Installation & Setup

```bash
# Install Playwright
npm init playwright@latest

# Or add to existing project
pnpm add -D @playwright/test
npx playwright install
```

### Configuration (playwright.config.ts)

```typescript
import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: [
    ['html', { open: 'never' }],
    ['junit', { outputFile: 'results/junit.xml' }],
    process.env.CI ? ['github'] : ['list'],
  ],
  use: {
    baseURL: process.env.BASE_URL || 'http://localhost:3000',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
    actionTimeout: 10_000,
    navigationTimeout: 30_000,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'firefox',
      use: { ...devices['Desktop Firefox'] },
    },
    {
      name: 'webkit',
      use: { ...devices['Desktop Safari'] },
    },
    {
      name: 'mobile-chrome',
      use: { ...devices['Pixel 7'] },
    },
    {
      name: 'mobile-safari',
      use: { ...devices['iPhone 14'] },
    },
  ],
  webServer: {
    command: 'pnpm dev',
    url: 'http://localhost:3000',
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
});
```

## Page Object Model (POM)

The POM pattern encapsulates page interactions behind a clean API, improving test readability and maintainability.

### Base Page

```typescript
// e2e/pages/base.page.ts
import { type Page, type Locator } from '@playwright/test';

export abstract class BasePage {
  readonly page: Page;

  constructor(page: Page) {
    this.page = page;
  }

  async navigate(path: string): Promise<void> {
    await this.page.goto(path);
  }

  async waitForPageLoad(): Promise<void> {
    await this.page.waitForLoadState('networkidle');
  }

  async getToastMessage(): Promise<string> {
    const toast = this.page.locator('[role="alert"]');
    await toast.waitFor({ state: 'visible' });
    return toast.textContent() as Promise<string>;
  }
}
```

### Page Implementation

```typescript
// e2e/pages/login.page.ts
import { type Page, type Locator, expect } from '@playwright/test';
import { BasePage } from './base.page';

export class LoginPage extends BasePage {
  // Locators — defined once, reused everywhere
  readonly emailInput: Locator;
  readonly passwordInput: Locator;
  readonly submitButton: Locator;
  readonly errorMessage: Locator;
  readonly forgotPasswordLink: Locator;

  constructor(page: Page) {
    super(page);
    this.emailInput = page.getByLabel('Email');
    this.passwordInput = page.getByLabel('Password');
    this.submitButton = page.getByRole('button', { name: 'Sign in' });
    this.errorMessage = page.getByRole('alert');
    this.forgotPasswordLink = page.getByRole('link', { name: 'Forgot password?' });
  }

  async goto(): Promise<void> {
    await this.navigate('/login');
  }

  async login(email: string, password: string): Promise<void> {
    await this.emailInput.fill(email);
    await this.passwordInput.fill(password);
    await this.submitButton.click();
  }

  async expectError(message: string): Promise<void> {
    await expect(this.errorMessage).toContainText(message);
  }

  async expectLoggedIn(): Promise<void> {
    await expect(this.page).toHaveURL(/\/dashboard/);
  }
}
```

### Component Page Object (for reusable UI components)

```typescript
// e2e/pages/components/navbar.component.ts
import { type Page, type Locator } from '@playwright/test';

export class NavbarComponent {
  readonly page: Page;
  readonly userMenu: Locator;
  readonly logoutButton: Locator;
  readonly searchInput: Locator;
  readonly notificationBell: Locator;

  constructor(page: Page) {
    this.page = page;
    const navbar = page.getByRole('navigation', { name: 'Main' });
    this.userMenu = navbar.getByRole('button', { name: /user menu/i });
    this.logoutButton = navbar.getByRole('menuitem', { name: 'Log out' });
    this.searchInput = navbar.getByPlaceholder('Search...');
    this.notificationBell = navbar.getByRole('button', { name: /notifications/i });
  }

  async logout(): Promise<void> {
    await this.userMenu.click();
    await this.logoutButton.click();
  }

  async search(query: string): Promise<void> {
    await this.searchInput.fill(query);
    await this.searchInput.press('Enter');
  }
}
```

### Dashboard Page with Components

```typescript
// e2e/pages/dashboard.page.ts
import { type Page, type Locator, expect } from '@playwright/test';
import { BasePage } from './base.page';
import { NavbarComponent } from './components/navbar.component';

export class DashboardPage extends BasePage {
  readonly navbar: NavbarComponent;
  readonly heading: Locator;
  readonly projectCards: Locator;
  readonly createProjectButton: Locator;

  constructor(page: Page) {
    super(page);
    this.navbar = new NavbarComponent(page);
    this.heading = page.getByRole('heading', { name: 'Dashboard' });
    this.projectCards = page.getByTestId('project-card');
    this.createProjectButton = page.getByRole('button', { name: 'New project' });
  }

  async goto(): Promise<void> {
    await this.navigate('/dashboard');
  }

  async expectProjectCount(count: number): Promise<void> {
    await expect(this.projectCards).toHaveCount(count);
  }

  async openProject(name: string): Promise<void> {
    await this.page.getByRole('link', { name }).click();
  }
}
```

## Auto-Waiting & Web-First Assertions

Playwright automatically waits for elements to be actionable before performing actions. Web-first assertions auto-retry until the condition is met or timeout.

### Auto-Waiting in Action

```typescript
import { test, expect } from '@playwright/test';

test('auto-waiting example', async ({ page }) => {
  await page.goto('/products');

  // Playwright automatically waits for:
  //  - element to be visible
  //  - element to be enabled
  //  - element to be stable (no animation)
  //  - element to receive events
  await page.getByRole('button', { name: 'Add to cart' }).click();

  // Web-first assertion — retries until true or timeout
  await expect(page.getByTestId('cart-count')).toHaveText('1');

  // Wait for specific network response
  const responsePromise = page.waitForResponse('**/api/cart');
  await page.getByRole('button', { name: 'Checkout' }).click();
  const response = await responsePromise;
  expect(response.status()).toBe(200);
});
```

### Locator Best Practices

```typescript
// PREFER: User-facing locators (resilient to implementation changes)
page.getByRole('button', { name: 'Submit' });
page.getByLabel('Email address');
page.getByPlaceholder('Search...');
page.getByText('Welcome back');
page.getByAltText('Company logo');

// OK: Test IDs (for complex elements without clear roles)
page.getByTestId('chart-container');

// AVOID: CSS/XPath selectors (brittle, implementation-coupled)
page.locator('.btn-primary');          // fragile
page.locator('#submit-form-btn');      // fragile
page.locator('//div[@class="card"]');  // fragile
```

## Fixtures & Test Hooks

### Custom Fixtures

```typescript
// e2e/fixtures.ts
import { test as base, expect } from '@playwright/test';
import { LoginPage } from './pages/login.page';
import { DashboardPage } from './pages/dashboard.page';

type Fixtures = {
  loginPage: LoginPage;
  dashboardPage: DashboardPage;
  authenticatedPage: DashboardPage;
};

export const test = base.extend<Fixtures>({
  loginPage: async ({ page }, use) => {
    const loginPage = new LoginPage(page);
    await use(loginPage);
  },

  dashboardPage: async ({ page }, use) => {
    const dashboardPage = new DashboardPage(page);
    await use(dashboardPage);
  },

  // Fixture that provides an already-authenticated page
  authenticatedPage: async ({ page, dashboardPage }, use) => {
    // Use storageState for faster auth (skip login UI)
    await page.goto('/login');
    const loginPage = new LoginPage(page);
    await loginPage.login('test@example.com', 'password123');
    await expect(page).toHaveURL(/\/dashboard/);
    await use(dashboardPage);
  },
});

export { expect };
```

### Authentication Setup (Global)

```typescript
// e2e/auth.setup.ts
import { test as setup, expect } from '@playwright/test';
import path from 'node:path';

const authFile = path.join(__dirname, '.auth/user.json');

setup('authenticate', async ({ page }) => {
  await page.goto('/login');
  await page.getByLabel('Email').fill('test@example.com');
  await page.getByLabel('Password').fill('password123');
  await page.getByRole('button', { name: 'Sign in' }).click();

  await expect(page).toHaveURL('/dashboard');

  // Save signed-in state for reuse
  await page.context().storageState({ path: authFile });
});
```

```typescript
// In playwright.config.ts — use auth state in projects
projects: [
  { name: 'setup', testMatch: /.*\.setup\.ts/ },
  {
    name: 'chromium',
    use: {
      ...devices['Desktop Chrome'],
      storageState: 'e2e/.auth/user.json',
    },
    dependencies: ['setup'],
  },
],
```

### Test Hooks

```typescript
import { test, expect } from './fixtures';

test.describe('Project management', () => {
  test.beforeEach(async ({ page }) => {
    // Seed test data via API
    await page.request.post('/api/test/seed', {
      data: { scenario: 'projects' },
    });
  });

  test.afterEach(async ({ page }) => {
    // Clean up
    await page.request.delete('/api/test/cleanup');
  });

  test('can create a new project', async ({ authenticatedPage }) => {
    await authenticatedPage.createProjectButton.click();
    // ...
  });
});
```

## API Testing with Playwright

```typescript
import { test, expect } from '@playwright/test';

test.describe('API: Users', () => {
  let apiContext;

  test.beforeAll(async ({ playwright }) => {
    apiContext = await playwright.request.newContext({
      baseURL: 'http://localhost:3000/api',
      extraHTTPHeaders: {
        Authorization: `Bearer ${process.env.TEST_TOKEN}`,
        'Content-Type': 'application/json',
      },
    });
  });

  test.afterAll(async () => {
    await apiContext.dispose();
  });

  test('GET /users returns user list', async () => {
    const response = await apiContext.get('/users');
    expect(response.ok()).toBeTruthy();

    const body = await response.json();
    expect(body.data).toBeInstanceOf(Array);
    expect(body.data.length).toBeGreaterThan(0);
    expect(body.data[0]).toHaveProperty('email');
  });

  test('POST /users creates user', async () => {
    const response = await apiContext.post('/users', {
      data: {
        name: 'Test User',
        email: `test-${Date.now()}@example.com`,
      },
    });

    expect(response.status()).toBe(201);
    const user = await response.json();
    expect(user).toMatchObject({
      name: 'Test User',
    });
  });

  test('POST /users validates input', async () => {
    const response = await apiContext.post('/users', {
      data: { name: '' },
    });

    expect(response.status()).toBe(400);
    const body = await response.json();
    expect(body.error).toBeDefined();
  });
});
```

## Visual Regression Testing (Screenshot Comparison)

```typescript
import { test, expect } from '@playwright/test';

test('homepage visual snapshot', async ({ page }) => {
  await page.goto('/');

  // Full page screenshot comparison
  await expect(page).toHaveScreenshot('homepage.png', {
    fullPage: true,
    maxDiffPixelRatio: 0.01, // Allow 1% pixel difference
  });
});

test('component visual regression', async ({ page }) => {
  await page.goto('/components');

  // Element-level screenshot
  const card = page.getByTestId('feature-card').first();
  await expect(card).toHaveScreenshot('feature-card.png', {
    animations: 'disabled',        // Freeze animations
    mask: [page.locator('.timestamp')], // Mask dynamic content
  });
});

test('responsive design check', async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 }); // iPhone
  await page.goto('/');
  await expect(page).toHaveScreenshot('homepage-mobile.png');

  await page.setViewportSize({ width: 1440, height: 900 }); // Desktop
  await page.goto('/');
  await expect(page).toHaveScreenshot('homepage-desktop.png');
});
```

Update snapshots with:
```bash
npx playwright test --update-snapshots
```

## Trace Viewer Debugging

```typescript
// Enable traces in config
use: {
  trace: 'on-first-retry',   // Capture trace only on retry
  // trace: 'on',             // Always capture (debug mode)
  // trace: 'retain-on-failure', // Keep only failed traces
},
```

```bash
# View trace after test failure
npx playwright show-trace test-results/my-test/trace.zip

# Generate trace programmatically
test('with manual trace', async ({ page, context }) => {
  await context.tracing.start({ screenshots: true, snapshots: true });

  await page.goto('/');
  // ... actions ...

  await context.tracing.stop({ path: 'trace.zip' });
});
```

The trace viewer shows:
- **Timeline** of all actions
- **DOM snapshots** at each step
- **Network requests** with bodies
- **Console logs** and errors
- **Source code** linked to each action

## Cross-Browser Testing

```typescript
// playwright.config.ts — per-project browser settings
projects: [
  {
    name: 'chromium',
    use: {
      ...devices['Desktop Chrome'],
      channel: 'chrome',  // Use installed Chrome instead of Chromium
    },
  },
  {
    name: 'firefox',
    use: { ...devices['Desktop Firefox'] },
  },
  {
    name: 'webkit',
    use: { ...devices['Desktop Safari'] },
  },
  {
    name: 'edge',
    use: {
      ...devices['Desktop Edge'],
      channel: 'msedge',
    },
  },
],
```

Run specific browser:
```bash
npx playwright test --project=firefox
npx playwright test --project=chromium --project=webkit
```

## Parallel Execution

```typescript
// playwright.config.ts
export default defineConfig({
  fullyParallel: true,           // Run tests in files in parallel
  workers: process.env.CI ? 1 : undefined,  // Use all CPUs locally
  // workers: '50%',             // Use half of available CPUs
});

// Per-file parallelism control
test.describe.configure({ mode: 'serial' }); // Run tests in this file serially
test.describe.configure({ mode: 'parallel' }); // Run tests in this file in parallel
```

### Sharding for CI

```yaml
# GitHub Actions — shard across multiple machines
jobs:
  test:
    strategy:
      matrix:
        shard: [1, 2, 3, 4]
    steps:
      - run: npx playwright test --shard=${{ matrix.shard }}/4
```

## Video Recording

```typescript
use: {
  video: 'retain-on-failure', // Record but only save on failure
  // video: 'on',             // Always record
  // video: 'on-first-retry', // Record on retry
  video: {
    mode: 'retain-on-failure',
    size: { width: 1280, height: 720 },
  },
},
```

## Accessibility Testing with Playwright

```typescript
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test('homepage has no accessibility violations', async ({ page }) => {
  await page.goto('/');

  const results = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .analyze();

  expect(results.violations).toEqual([]);
});

test('form accessibility', async ({ page }) => {
  await page.goto('/signup');

  const results = await new AxeBuilder({ page })
    .include('#signup-form')
    .disableRules(['color-contrast']) // Temporarily skip known issue
    .analyze();

  expect(results.violations).toEqual([]);
});

test('keyboard navigation works', async ({ page }) => {
  await page.goto('/');

  // Tab through interactive elements
  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: 'Skip to content' })).toBeFocused();

  await page.keyboard.press('Tab');
  await expect(page.getByRole('link', { name: 'Home' })).toBeFocused();

  // Enter activates the focused element
  await page.keyboard.press('Enter');
  await expect(page).toHaveURL('/');
});
```

## CI Integration (GitHub Actions)

```yaml
name: Playwright Tests
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  playwright:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-node@v4
        with:
          node-version: 20

      - name: Install dependencies
        run: |
          corepack enable
          pnpm install --frozen-lockfile

      - name: Install Playwright browsers
        run: npx playwright install --with-deps

      - name: Run Playwright tests
        run: npx playwright test
        env:
          BASE_URL: http://localhost:3000

      - name: Upload test report
        uses: actions/upload-artifact@v4
        if: ${{ !cancelled() }}
        with:
          name: playwright-report
          path: playwright-report/
          retention-days: 14

      - name: Upload test results
        uses: actions/upload-artifact@v4
        if: ${{ !cancelled() }}
        with:
          name: test-results
          path: test-results/
          retention-days: 7
```

## Best Practices

1. **Use user-facing locators** (`getByRole`, `getByLabel`, `getByText`) over CSS selectors
2. **Avoid hardcoded waits** (`page.waitForTimeout`) -- rely on auto-waiting
3. **Isolate tests** -- each test should be independent; use API calls to seed/clean data
4. **Use POM** for pages with more than 3 tests interacting with them
5. **Keep tests focused** -- one behavior per test, clear test name
6. **Use `test.describe`** to group related tests and share setup
7. **Leverage fixtures** for shared setup (authentication, seeding, page objects)
8. **Run headless in CI** -- use headed mode locally for debugging
9. **Enable trace on first retry** -- captures failure context without slowing all runs
10. **Shard in CI** for large test suites to keep CI under 10 minutes
11. **Use `expect.soft`** for non-critical assertions to continue test after failure
12. **Tag tests** for selective execution (`@smoke`, `@regression`)

## Anti-Patterns

1. **Testing implementation details** -- don't assert on CSS classes or DOM structure
2. **Flaky selectors** -- avoid `nth-child`, class-based, or auto-generated selectors
3. **Shared mutable state** -- tests modifying data other tests depend on
4. **Long test files** -- split into focused spec files per feature
5. **Sleeping instead of waiting** -- `page.waitForTimeout(5000)` is always a code smell
6. **Ignoring test failures** -- fix flaky tests immediately, don't skip them permanently
7. **Over-mocking in E2E** -- the point is to test the real system; mock only external services
8. **Screenshot tests for everything** -- visual tests are slow; reserve for critical UI components

## Directory Structure

```
e2e/
├── fixtures.ts                  # Custom test fixtures
├── auth.setup.ts                # Authentication setup
├── .auth/                       # Stored auth state (gitignored)
├── pages/                       # Page Object Models
│   ├── base.page.ts
│   ├── login.page.ts
│   ├── dashboard.page.ts
│   └── components/
│       └── navbar.component.ts
├── specs/                       # Test specifications
│   ├── auth.spec.ts
│   ├── dashboard.spec.ts
│   └── projects.spec.ts
└── helpers/                     # Test utilities
    ├── api.helper.ts
    └── data.helper.ts
```

## Sources & References

- https://playwright.dev/docs/intro -- Playwright official documentation
- https://playwright.dev/docs/pom -- Page Object Model guide
- https://playwright.dev/docs/best-practices -- Official best practices
- https://playwright.dev/docs/trace-viewer -- Trace viewer documentation
- https://playwright.dev/docs/test-configuration -- Configuration reference
- https://playwright.dev/docs/api-testing -- API testing guide
- https://playwright.dev/docs/accessibility-testing -- Accessibility testing
- https://playwright.dev/docs/ci-intro -- CI integration guide
- https://github.com/axe-core/axe-core -- axe-core accessibility engine
- https://www.browserstack.com/guide/playwright-test-best-practices -- BrowserStack Playwright guide
