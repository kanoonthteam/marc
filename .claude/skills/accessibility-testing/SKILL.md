---
name: accessibility-testing
description: WCAG 2.1/2.2 compliance testing with axe-core, keyboard navigation, and screen reader validation
---

# Accessibility Testing

## Overview

Accessibility (a11y) testing ensures digital products are usable by everyone, including people with disabilities. WCAG (Web Content Accessibility Guidelines) is the international standard, and most legal requirements mandate at least Level AA compliance.

## WCAG 2.1/2.2 AA Compliance

### The Four Principles (POUR)

**1. Perceivable** -- Information must be presentable in ways users can perceive.

| Guideline | Requirement | How to Test |
|-----------|-------------|-------------|
| 1.1 Text Alternatives | All non-text content has alt text | Check `alt` on images, `aria-label` on icons |
| 1.2 Time-Based Media | Captions for video, transcripts for audio | Manual review |
| 1.3 Adaptable | Content structure via semantic HTML | Check heading hierarchy, landmark regions |
| 1.4 Distinguishable | 4.5:1 contrast ratio (text), 3:1 (large text) | Contrast checker tools |

**2. Operable** -- UI components must be operable by all users.

| Guideline | Requirement | How to Test |
|-----------|-------------|-------------|
| 2.1 Keyboard Accessible | All functionality via keyboard | Tab through entire page |
| 2.2 Enough Time | Adjustable time limits | Check for auto-advancing content |
| 2.3 Seizures | No content flashes >3 times/second | Visual review |
| 2.4 Navigable | Skip links, descriptive page titles, focus order | Tab order testing |
| 2.5 Input Modalities | Touch target >= 24x24 CSS px (WCAG 2.2) | Measure touch targets |

**3. Understandable** -- Content and operation must be understandable.

| Guideline | Requirement | How to Test |
|-----------|-------------|-------------|
| 3.1 Readable | Page language declared (`lang` attribute) | Check `<html lang="en">` |
| 3.2 Predictable | Consistent navigation, no unexpected context changes | Navigate through the site |
| 3.3 Input Assistance | Error identification, labels, suggestions | Submit forms with invalid data |

**4. Robust** -- Content must be compatible with assistive technologies.

| Guideline | Requirement | How to Test |
|-----------|-------------|-------------|
| 4.1 Compatible | Valid HTML, proper ARIA usage | HTML validator, axe-core |

### WCAG 2.2 New Criteria (AA)

- **2.4.11 Focus Not Obscured (Minimum)** -- Focused element not entirely hidden
- **2.4.13 Focus Appearance** -- Visible focus indicator (>= 2px, 3:1 contrast)
- **2.5.7 Dragging Movements** -- Alternative to drag-and-drop
- **2.5.8 Target Size (Minimum)** -- Touch targets >= 24x24 CSS pixels
- **3.3.7 Redundant Entry** -- Don't re-ask info already provided
- **3.3.8 Accessible Authentication** -- No cognitive function test for login

## Automated Testing with axe-core

### jest-axe (Unit/Component Testing)

```typescript
// __tests__/components/Button.test.tsx
import { render } from '@testing-library/react';
import { axe, toHaveNoViolations } from 'jest-axe';

expect.extend(toHaveNoViolations);

describe('Button component', () => {
  it('has no accessibility violations', async () => {
    const { container } = render(
      <button type="button">Click me</button>
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('icon-only button has accessible name', async () => {
    const { container } = render(
      <button type="button" aria-label="Close dialog">
        <svg aria-hidden="true">...</svg>
      </button>
    );
    const results = await axe(container);
    expect(results).toHaveNoViolations();
  });

  it('flags inaccessible patterns', async () => {
    const { container } = render(
      <div onClick={() => {}} role="button">
        {/* Missing tabIndex and keyboard handler */}
        Clickable div
      </div>
    );
    const results = await axe(container);
    // This SHOULD have violations
    expect(results.violations.length).toBeGreaterThan(0);
  });
});
```

### @axe-core/playwright (E2E Testing)

```typescript
// e2e/accessibility.spec.ts
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

test.describe('Accessibility', () => {
  test('homepage meets WCAG AA', async ({ page }) => {
    await page.goto('/');

    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
      .analyze();

    // Log violations for debugging
    if (results.violations.length > 0) {
      console.log('Violations:', JSON.stringify(results.violations, null, 2));
    }

    expect(results.violations).toEqual([]);
  });

  test('login form is accessible', async ({ page }) => {
    await page.goto('/login');

    const results = await new AxeBuilder({ page })
      .include('#login-form')
      .withTags(['wcag2a', 'wcag2aa'])
      .analyze();

    expect(results.violations).toEqual([]);
  });

  test('navigation is accessible after interaction', async ({ page }) => {
    await page.goto('/');

    // Open mobile menu
    await page.getByRole('button', { name: /menu/i }).click();

    // Re-scan after DOM change
    const results = await new AxeBuilder({ page })
      .include('[role="navigation"]')
      .analyze();

    expect(results.violations).toEqual([]);
  });

  test('modal dialog is accessible', async ({ page }) => {
    await page.goto('/dashboard');

    // Open modal
    await page.getByRole('button', { name: 'Create project' }).click();
    await page.waitForSelector('[role="dialog"]');

    const results = await new AxeBuilder({ page })
      .include('[role="dialog"]')
      .analyze();

    expect(results.violations).toEqual([]);
  });
});
```

### Scanning Multiple Pages Automatically

```typescript
// e2e/a11y-audit.spec.ts
import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

const pages = [
  { name: 'Homepage', path: '/' },
  { name: 'Login', path: '/login' },
  { name: 'Signup', path: '/signup' },
  { name: 'Dashboard', path: '/dashboard' },
  { name: 'Settings', path: '/settings' },
];

for (const { name, path } of pages) {
  test(`${name} (${path}) has no a11y violations`, async ({ page }) => {
    await page.goto(path);
    const results = await new AxeBuilder({ page })
      .withTags(['wcag2a', 'wcag2aa'])
      .analyze();

    expect(results.violations).toEqual([]);
  });
}
```

## Keyboard Navigation Testing

```typescript
// e2e/keyboard.spec.ts
import { test, expect } from '@playwright/test';

test.describe('Keyboard Navigation', () => {
  test('skip link is first focusable element', async ({ page }) => {
    await page.goto('/');

    await page.keyboard.press('Tab');

    const skipLink = page.getByRole('link', { name: /skip to/i });
    await expect(skipLink).toBeFocused();
    await expect(skipLink).toBeVisible();
  });

  test('tab order follows visual order', async ({ page }) => {
    await page.goto('/');

    const expectedOrder = [
      'Skip to content',
      'Home',
      'Products',
      'About',
      'Sign in',
    ];

    for (const name of expectedOrder) {
      await page.keyboard.press('Tab');
      const focused = page.getByRole('link', { name });
      await expect(focused).toBeFocused();
    }
  });

  test('modal traps focus', async ({ page }) => {
    await page.goto('/dashboard');
    await page.getByRole('button', { name: 'Delete' }).click();

    // Focus should be inside the dialog
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // Tab through all focusable elements in dialog
    await page.keyboard.press('Tab');
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeFocused();

    await page.keyboard.press('Tab');
    await expect(page.getByRole('button', { name: 'Confirm' })).toBeFocused();

    // Tab wraps back to first element (focus trap)
    await page.keyboard.press('Tab');
    await expect(page.getByRole('button', { name: 'Cancel' })).toBeFocused();

    // Escape closes the dialog
    await page.keyboard.press('Escape');
    await expect(dialog).toBeHidden();

    // Focus returns to trigger element
    await expect(page.getByRole('button', { name: 'Delete' })).toBeFocused();
  });

  test('dropdown menu keyboard interaction', async ({ page }) => {
    await page.goto('/');

    const menuButton = page.getByRole('button', { name: 'Options' });
    await menuButton.focus();
    await page.keyboard.press('Enter'); // Opens menu

    // Arrow keys navigate menu items
    await page.keyboard.press('ArrowDown');
    await expect(page.getByRole('menuitem', { name: 'Edit' })).toBeFocused();

    await page.keyboard.press('ArrowDown');
    await expect(page.getByRole('menuitem', { name: 'Delete' })).toBeFocused();

    // Escape closes menu
    await page.keyboard.press('Escape');
    await expect(menuButton).toBeFocused();
  });
});
```

## Color Contrast Testing

### Minimum Contrast Ratios (WCAG AA)

| Element Type | Minimum Ratio |
|-------------|---------------|
| Normal text (<18pt / <14pt bold) | 4.5:1 |
| Large text (>=18pt / >=14pt bold) | 3:1 |
| UI components & graphical objects | 3:1 |
| Incidental / decorative | No requirement |

### Automated Contrast Checks

```typescript
// Color contrast is checked by axe-core automatically
// To specifically test contrast:
const results = await new AxeBuilder({ page })
  .withRules(['color-contrast'])
  .analyze();

expect(results.violations).toEqual([]);
```

### CSS Best Practices for Contrast

```css
/* Use CSS custom properties for theme colors */
:root {
  --text-primary: #1a1a2e;       /* On white: 15.4:1 */
  --text-secondary: #4a4a68;     /* On white: 7.1:1 */
  --text-on-primary: #ffffff;    /* On brand blue: 8.6:1 */
  --bg-primary: #1a56db;
  --border-input: #6b7280;       /* On white: 4.6:1 */
  --focus-ring: #1a56db;         /* Visible focus indicator */
}

/* Focus styles must be visible */
:focus-visible {
  outline: 2px solid var(--focus-ring);
  outline-offset: 2px;
}

/* Do NOT remove focus outlines */
/* BAD: :focus { outline: none; } */
```

## ARIA Patterns

### Landmarks

```html
<body>
  <a href="#main" class="skip-link">Skip to main content</a>

  <header role="banner">
    <nav aria-label="Main navigation">...</nav>
  </header>

  <main id="main" role="main">
    <h1>Page Title</h1>
    <!-- Page content -->
  </main>

  <aside role="complementary" aria-label="Related links">
    <!-- Sidebar -->
  </aside>

  <footer role="contentinfo">...</footer>
</body>
```

### Live Regions

```html
<!-- Polite: announced after current speech -->
<div aria-live="polite" aria-atomic="true">
  <!-- Updated dynamically: "3 results found" -->
</div>

<!-- Assertive: interrupts current speech (use sparingly) -->
<div role="alert" aria-live="assertive">
  <!-- Error messages, critical alerts -->
</div>

<!-- Status: polite + role for status messages -->
<div role="status">
  Saving... <!-- Screen reader announces: "Saving..." -->
</div>
```

### Common ARIA Widget Patterns

```html
<!-- Tabs -->
<div role="tablist" aria-label="Settings">
  <button role="tab" aria-selected="true" aria-controls="panel-1" id="tab-1">
    General
  </button>
  <button role="tab" aria-selected="false" aria-controls="panel-2" id="tab-2"
          tabindex="-1">
    Security
  </button>
</div>
<div role="tabpanel" id="panel-1" aria-labelledby="tab-1">
  <!-- General settings content -->
</div>
<div role="tabpanel" id="panel-2" aria-labelledby="tab-2" hidden>
  <!-- Security settings content -->
</div>

<!-- Disclosure (accordion) -->
<button aria-expanded="false" aria-controls="section-1">
  FAQ Question
</button>
<div id="section-1" hidden>
  FAQ Answer...
</div>
```

## Screen Reader Testing

### VoiceOver (macOS/iOS)

```
Start:  Cmd + F5
Navigate: VO + Right/Left arrow (VO = Ctrl + Option)
Interact: VO + Space
Rotor:  VO + U (browse headings, landmarks, links)
Stop:   Cmd + F5
```

### NVDA (Windows)

```
Start:  Ctrl + Alt + N
Navigate: Tab, Arrow keys
Browse:   H (headings), D (landmarks), K (links)
Stop:   Insert + Q
```

### Testing Checklist for Screen Readers

1. Page title is announced on load
2. Headings form a logical hierarchy (h1 > h2 > h3)
3. All images have meaningful alt text (or empty alt for decorative)
4. Form fields have associated labels
5. Error messages are announced
6. Dynamic content changes are announced (live regions)
7. Modal dialogs announce their title
8. Custom widgets announce their role and state

## Automated vs Manual Testing Boundary

| Automated (axe-core catches) | Manual (human testing needed) |
|------------------------------|-------------------------------|
| Missing alt text | Alt text quality/accuracy |
| Missing form labels | Label clarity |
| Color contrast ratios | Color as sole indicator |
| Missing landmark roles | Logical content order |
| Invalid ARIA attributes | Meaningful ARIA usage |
| Missing document language | Content readability |
| Duplicate IDs | Tab order makes sense |

Automated tools catch approximately **30-40%** of WCAG violations. Manual testing with assistive technology is essential for full compliance.

## Semantic HTML Checklist

```html
<!-- Use semantic elements, not divs with roles -->

<!-- BAD -->
<div class="header">...</div>
<div class="nav">...</div>
<div onclick="submit()">Submit</div>

<!-- GOOD -->
<header>...</header>
<nav aria-label="Main">...</nav>
<button type="submit">Submit</button>

<!-- Heading hierarchy -->
<h1>Page Title</h1>        <!-- One per page -->
  <h2>Section</h2>
    <h3>Subsection</h3>
  <h2>Another Section</h2>  <!-- Don't skip levels -->

<!-- Lists for list content -->
<ul>
  <li>Item one</li>
  <li>Item two</li>
</ul>

<!-- Tables for tabular data -->
<table>
  <caption>Monthly Sales</caption>
  <thead>
    <tr><th scope="col">Month</th><th scope="col">Revenue</th></tr>
  </thead>
  <tbody>
    <tr><td>January</td><td>$10,000</td></tr>
  </tbody>
</table>
```

## Compliance Standards

| Standard | Region | Requirement |
|----------|--------|-------------|
| ADA (Americans with Disabilities Act) | USA | WCAG 2.1 AA for web (DOJ ruling 2024) |
| Section 508 | USA (federal) | WCAG 2.0 AA minimum |
| EAA (European Accessibility Act) | EU | EN 301 549 / WCAG 2.1 AA (June 2025) |
| AODA | Ontario, Canada | WCAG 2.0 AA |
| EN 301 549 | EU | WCAG 2.1 AA + mobile/documents |

## Best Practices

1. **Test early and often** -- integrate axe-core in unit tests, not just before launch
2. **Use semantic HTML first** -- ARIA is a supplement, not a replacement for native semantics
3. **Test with real assistive technology** -- automated tools miss 60%+ of issues
4. **Include users with disabilities** in usability testing
5. **Document known issues** with remediation timelines (VPAT/ACR)
6. **Focus management** -- always manage focus after dynamic content changes
7. **Provide multiple ways** to accomplish tasks (mouse, keyboard, touch, voice)
8. **Test responsive design** -- accessibility at all viewport sizes
9. **Test with browser zoom** at 200% and 400%
10. **Maintain an accessibility statement** on your website

## Anti-Patterns

1. **`outline: none` without replacement** -- removes visible focus for keyboard users
2. **Using color alone** to convey information (errors, status, required fields)
3. **Auto-playing media** without user control
4. **CAPTCHAs** without accessible alternatives
5. **Placeholder as label** -- disappears on input, insufficient contrast
6. **Mouse-only interactions** (hover menus, drag-only operations)
7. **Empty links/buttons** -- `<a href="#"><img src="icon.svg"></a>` with no alt text
8. **Hiding content with `display:none`** when it should be screen-reader accessible
9. **Non-dismissible modals** that cannot be closed with Escape key

## Sources & References

- https://www.w3.org/WAI/WCAG21/quickref/ -- WCAG 2.1 Quick Reference
- https://www.w3.org/TR/WCAG22/ -- WCAG 2.2 Specification
- https://www.deque.com/axe/ -- axe-core accessibility engine
- https://github.com/dequelabs/axe-core -- axe-core GitHub
- https://www.w3.org/WAI/ARIA/apg/ -- ARIA Authoring Practices Guide
- https://playwright.dev/docs/accessibility-testing -- Playwright accessibility testing
- https://webaim.org/resources/contrastchecker/ -- WebAIM Contrast Checker
- https://www.a11yproject.com/checklist/ -- A11y Project Checklist
- https://www.ada.gov/resources/web-guidance/ -- ADA Web Accessibility Guidance
- https://www.etsi.org/deliver/etsi_en/301500_301599/301549/ -- EN 301 549 Standard
