# Frontend Interaction Stability QA Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a browser-level QA gate that catches broken interactions across resource/database tables, sheets, filters, links, browser Back, and accent restoration.

**Architecture:** Keep existing smoke tests intact and add a focused interaction-stability E2E spec. Shared browser assertions live in a small harness file so every test checks accent state, modal residue, row-click recovery, dropdown recovery, console warnings/errors, and network errors consistently.

**Tech Stack:** Next.js 16, React 19, Playwright, existing E2E harness under `e2e/harness`, real backend on `:8080`, frontend dev server through existing Playwright webServer proxy.

---

## Design Summary

The test must cover interaction chains, not isolated actions. A passing result means the page is still usable after users do realistic operations in sequence:

- select a filter
- open a sheet
- close a sheet by blank click
- navigate to a resource detail page
- use browser Back
- interact with the restored page again

The new suite must verify both `/resources` and `/databases`, because they use similar table/sheet/link patterns but different row structures and filters.

## File Structure

- Create `e2e/harness/interaction-stability.ts`
  - Shared helpers for accent setup/assertion, modal residue assertion, table row sheet open/blank-close, resource-link navigation, filter menu opening.
- Create `e2e/operator-interaction-stability.spec.ts`
  - Three E2E tests:
  - resources table link → detail → Back → interactions still work
  - resources table row sheet → full detail → Back → interactions still work
  - databases engine filter → sheet close → link → Back → interactions still work
- Modify `package.json`
  - Add `test:e2e:interaction`.
- Modify `.gitignore` only if screenshots/artifacts are generated outside existing ignored paths.
  - Do not add new ignore rules unless the new test writes new file patterns.

---

### Task 1: Add Interaction Stability Harness

**Files:**
- Create: `e2e/harness/interaction-stability.ts`

- [ ] **Step 1: Create the helper file**

Create `e2e/harness/interaction-stability.ts` with this exact content:

```ts
import { expect, type Locator, type Page } from "@playwright/test";

const DEFAULT_BLUE_PRIMARY = "lab(45.2565% -10.9423 -37.8452)";

export async function setPurpleAccent(page: Page): Promise<void> {
  await page.evaluate(() => {
    localStorage.setItem("controlhub.accent", "purple");
    document.documentElement.dataset.accent = "purple";
  });
}

export async function expectPurpleAccent(page: Page): Promise<void> {
  await expect
    .poll(async () =>
      page.evaluate(() => ({
        accent: document.documentElement.dataset.accent,
        primary: getComputedStyle(document.documentElement)
          .getPropertyValue("--primary")
          .trim(),
      })),
    )
    .toMatchObject({
      accent: "purple",
    });

  const primary = await page.evaluate(() =>
    getComputedStyle(document.documentElement).getPropertyValue("--primary").trim(),
  );
  expect(primary).not.toBe(DEFAULT_BLUE_PRIMARY);
}

export async function expectNoModalResidue(page: Page): Promise<void> {
  await expect
    .poll(async () =>
      page.evaluate(() => ({
        dialogs: document.querySelectorAll('[role="dialog"]').length,
        overlays: document.querySelectorAll('[data-slot="sheet-overlay"]').length,
        inert: document.querySelectorAll("[inert]").length,
      })),
    )
    .toEqual({
      dialogs: 0,
      overlays: 0,
      inert: 0,
    });
}

export function firstResourceTableLink(page: Page): Locator {
  return page.locator('table tbody a[href^="/resources/"]').first();
}

export function firstDetailRow(page: Page): Locator {
  return page.locator('tbody tr[aria-label^="View details"]').first();
}

export async function openFirstRowSheet(page: Page): Promise<void> {
  await firstDetailRow(page).click({ position: { x: 500, y: 10 } });
  await expect(page.getByRole("dialog")).toBeVisible();
}

export async function closeSheetByBlankClick(page: Page): Promise<void> {
  await page.mouse.click(20, 20);
  await expectNoModalResidue(page);
}

export async function expectFirstFilterMenuOpens(page: Page): Promise<void> {
  const trigger = page.locator('[data-slot="multi-select-trigger"]').first();
  await expect(trigger).toBeVisible();
  await trigger.click();
  await expect(page.getByRole("menu")).toBeVisible();
  await page.keyboard.press("Escape");
}

export async function expectRestoredListIsInteractive(page: Page): Promise<void> {
  await expectPurpleAccent(page);
  await expectNoModalResidue(page);
  await openFirstRowSheet(page);
  await closeSheetByBlankClick(page);
  await expectFirstFilterMenuOpens(page);
}
```

- [ ] **Step 2: Run TypeScript check for the new helper**

Run:

```bash
npx tsc --noEmit -p tsconfig.json
```

Expected:

```text
0 errors
```

- [ ] **Step 3: Commit**

```bash
git add e2e/harness/interaction-stability.ts
git commit -m "test: add interaction stability e2e harness"
```

---

### Task 2: Add Resources Interaction Stability E2E Coverage

**Files:**
- Create: `e2e/operator-interaction-stability.spec.ts`

- [ ] **Step 1: Add the first two resources tests**

Create `e2e/operator-interaction-stability.spec.ts` with this exact content:

```ts
import { expect, test } from "@playwright/test";
import { checkBackendHealth } from "./harness/backend-health";
import { loginViaUI } from "./harness/auth";
import {
  assertClean,
  collectConsoleMessages,
  collectNetworkErrors,
} from "./harness/console-guards";
import {
  closeSheetByBlankClick,
  expectNoModalResidue,
  expectPurpleAccent,
  expectRestoredListIsInteractive,
  firstResourceTableLink,
  openFirstRowSheet,
  setPurpleAccent,
} from "./harness/interaction-stability";

test.describe("Operator interaction stability", () => {
  let consoleMessages: ReturnType<typeof collectConsoleMessages>;
  let networkErrors: string[];

  test.beforeAll(async () => {
    await checkBackendHealth();
  });

  test.beforeEach(async ({ page }) => {
    consoleMessages = collectConsoleMessages(page, {
      allowedErrors: [
        /Fast Refresh/,
        /HMR/,
        /Download the React DevTools/,
      ],
    });
    networkErrors = collectNetworkErrors(page);

    await page.context().addCookies([
      {
        name: "controlhub.locale",
        value: "zh-CN",
        domain: "localhost",
        path: "/",
      },
    ]);
  });

  test.afterEach(async () => {
    assertClean(consoleMessages, networkErrors);
  });

  test("resources table remains interactive after resource link navigation and browser back", async ({
    page,
  }) => {
    await loginViaUI(page);
    await setPurpleAccent(page);

    await page.goto("/resources?environment=prod&page=1");
    await expect(page.locator("table")).toBeVisible();
    await expectPurpleAccent(page);

    await firstResourceTableLink(page).click();
    await expect(page).toHaveURL(/\/resources\/\d+/, { timeout: 10_000 });
    await expect(page.locator("h1")).toBeVisible();

    await page.goBack({ waitUntil: "domcontentloaded" });
    await page.waitForLoadState("networkidle");

    await expect(page).toHaveURL(/\/resources\?environment=prod&page=1/);
    await expectRestoredListIsInteractive(page);
  });

  test("resources table remains interactive after sheet full-detail navigation and browser back", async ({
    page,
  }) => {
    await loginViaUI(page);
    await setPurpleAccent(page);

    await page.goto("/resources?environment=prod&page=1");
    await expect(page.locator("table")).toBeVisible();

    await openFirstRowSheet(page);
    await page.getByRole("link", { name: "打开完整详情" }).click();
    await expect(page).toHaveURL(/\/resources\/\d+/, { timeout: 10_000 });
    await expect(page.locator("h1")).toBeVisible();

    await page.goBack({ waitUntil: "domcontentloaded" });
    await page.waitForLoadState("networkidle");

    await expect(page).toHaveURL(/\/resources\?environment=prod&page=1/);
    await expectRestoredListIsInteractive(page);
  });
});
```

- [ ] **Step 2: Run the new spec and verify it fails before any missing implementation fixes**

Run:

```bash
env -u NO_COLOR playwright test e2e/operator-interaction-stability.spec.ts
```

Expected if the current fix is not present:

```text
FAIL ... accent is blue/default or row/dropdown interaction does not work after browser back
```

Expected if the current fix is already present:

```text
2 passed
```

- [ ] **Step 3: Commit**

```bash
git add e2e/operator-interaction-stability.spec.ts
git commit -m "test: cover resources table interaction recovery"
```

---

### Task 3: Add Databases Interaction Stability E2E Coverage

**Files:**
- Modify: `e2e/operator-interaction-stability.spec.ts`

- [ ] **Step 1: Append the databases test**

Add this test inside the existing `test.describe("Operator interaction stability", () => { ... })` block in `e2e/operator-interaction-stability.spec.ts`:

```ts
  test("databases table remains interactive after engine filter, sheet close, link navigation, and browser back", async ({
    page,
  }) => {
    await loginViaUI(page);
    await setPurpleAccent(page);

    await page.goto("/databases?environment=prod&page=1");
    await expect(page.locator("table")).toBeVisible();

    const engineFilter = page.locator('[data-slot="multi-select-trigger"]').first();
    await expect(engineFilter).toBeVisible();
    await engineFilter.click();
    await page.getByRole("menuitemcheckbox", { name: /mysql/i }).click();

    await expect(page).toHaveURL(/resourceSubtype=mysql/);
    await expect(page.getByRole("menu")).toHaveCount(0);

    await openFirstRowSheet(page);
    await closeSheetByBlankClick(page);

    await firstResourceTableLink(page).click();
    await expect(page).toHaveURL(/\/resources\/\d+/, { timeout: 10_000 });
    await expect(page.locator("h1")).toBeVisible();

    await page.goBack({ waitUntil: "domcontentloaded" });
    await page.waitForLoadState("networkidle");

    await expect(page).toHaveURL(/\/databases\?environment=prod&page=1&resourceSubtype=mysql/);
    await expectRestoredListIsInteractive(page);
  });
```

- [ ] **Step 2: Run only the new interaction spec**

Run:

```bash
env -u NO_COLOR playwright test e2e/operator-interaction-stability.spec.ts
```

Expected:

```text
3 passed
```

- [ ] **Step 3: Commit**

```bash
git add e2e/operator-interaction-stability.spec.ts
git commit -m "test: cover database table interaction recovery"
```

---

### Task 4: Add Package Script

**Files:**
- Modify: `package.json`

- [ ] **Step 1: Add the script**

Modify `package.json` scripts so the block includes `test:e2e:interaction`:

```json
{
  "scripts": {
    "dev": "next dev",
    "build": "next build",
    "start": "next start",
    "lint": "eslint",
    "test": "vitest run",
    "test:e2e": "env -u NO_COLOR playwright test",
    "test:e2e:smoke": "env -u NO_COLOR playwright test e2e/operator-console-smoke.spec.ts",
    "test:e2e:interaction": "env -u NO_COLOR playwright test e2e/operator-interaction-stability.spec.ts"
  }
}
```

- [ ] **Step 2: Run the new script**

Run:

```bash
npm run test:e2e:interaction
```

Expected:

```text
3 passed
```

- [ ] **Step 3: Commit**

```bash
git add package.json
git commit -m "test: add interaction stability e2e script"
```

---

### Task 5: Add Documentation for When to Run the Gate

**Files:**
- Modify: `docs/superpowers/specs/2026-04-28-frontend-interaction-stability-qa-gate.md`

- [ ] **Step 1: Add run policy**

Append this section to `docs/superpowers/specs/2026-04-28-frontend-interaction-stability-qa-gate.md`:

```markdown
## Run Policy

Run `npm run test:e2e:interaction` before claiming completion for any frontend change touching:

- `components/ui/sheet.tsx`
- `components/ui/dropdown-menu.tsx`
- `components/ui/select.tsx`
- `components/blocks/multi-select-filter.tsx`
- `components/blocks/resource-link.tsx`
- `components/resources/resource-table.tsx`
- `components/databases/database-table.tsx`
- `components/providers/accent-provider.tsx`
- `components/providers/app-providers.tsx`
- `app/layout.tsx`
- `proxy.ts`
- any navigation, routing, locale, theme, accent, table, sheet, dropdown, or modal behavior

Do not replace this gate with unit tests only. This gate exists because the failure mode depends on real browser history restoration and real pointer interactions.
```

- [ ] **Step 2: Commit**

```bash
git add docs/superpowers/specs/2026-04-28-frontend-interaction-stability-qa-gate.md
git commit -m "docs: define interaction stability e2e run policy"
```

---

### Task 6: Full Verification and Final Commit Hygiene

**Files:**
- Verify all changed files.

- [ ] **Step 1: Run frontend verification**

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
```

Expected:

```text
TypeScript: 0 errors
ESLint: 0 errors, 0 warnings
Vitest: all tests pass
Build: success
Smoke E2E: all tests pass
Interaction E2E: 3 passed
```

- [ ] **Step 2: Run full E2E if backend is available**

Run:

```bash
npm run test:e2e
```

Expected:

```text
All Playwright tests pass
```

If backend is not running on `:8080`, start it from `/Users/fan/GolangProjects/ControlHub`:

```bash
go run ./cmd/server
```

Then rerun:

```bash
npm run test:e2e
```

- [ ] **Step 3: Check git status**

Run:

```bash
git status --short --branch
```

Expected:

```text
## <branch-name>
```

No unstaged or untracked files except ignored build artifacts.

- [ ] **Step 4: Final commit if needed**

If there are remaining staged-worthy changes:

```bash
git add e2e/harness/interaction-stability.ts e2e/operator-interaction-stability.spec.ts package.json docs/superpowers/specs/2026-04-28-frontend-interaction-stability-qa-gate.md
git commit -m "test: add frontend interaction stability qa gate"
```

Commit message must not include `Co-Authored-By`.

---

## Self-Review Checklist

- [ ] Covers resources table link → detail → Back.
- [ ] Covers resources sheet → full detail → Back.
- [ ] Covers databases engine filter → sheet close → resource link → Back.
- [ ] Asserts accent restoration.
- [ ] Asserts no dialog, overlay, or inert residue.
- [ ] Asserts row click works after Back.
- [ ] Asserts dropdown works after Back.
- [ ] Uses real backend auth through existing `loginViaUI`.
- [ ] Uses existing console and network guards.
- [ ] Adds a dedicated npm script.
- [ ] Does not suppress warnings.
- [ ] Does not alter product behavior.

