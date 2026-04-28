# Frontend E2E Governance Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Document and enforce frontend E2E hygiene rules so future workers do not reintroduce broken auth setup, broad output suppression, success-path screenshots, or unguarded browser tests.

**Architecture:** Add one frontend documentation page and one lightweight Node.js policy checker. The checker scans Playwright config and E2E spec files for known anti-patterns, while the documentation explains the rules, exceptions, and required verification commands.

**Tech Stack:** Next.js frontend repo, Playwright, Vitest, Node.js script using built-in `fs`, `path`, and `process` modules only.

---

## File Structure

- Create `docs/e2e-governance.md`
  - Human-readable policy for E2E auth, guards, output handling, screenshots, and full-suite failure classification.
- Create `scripts/check-e2e-governance.mjs`
  - Static policy checker for Playwright config and `e2e/**/*.spec.ts`.
- Modify `package.json`
  - Add `check:e2e-governance`.
- Test by running the checker and the existing frontend validation commands.

Do not modify backend files. Do not modify product UI files.

---

### Task 1: Add The E2E Governance Document

**Files:**
- Create: `docs/e2e-governance.md`

- [ ] **Step 1: Create the document**

Create `docs/e2e-governance.md` with this content:

```md
# E2E Governance

This document defines the rules for frontend browser tests in ControlHub.

## Required Commands

Before claiming a frontend phase is complete, run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run check:e2e-governance
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

`npm run test:e2e` must be fully green. If it is not green, do not call the
phase complete. Classify every failure with the table in "Failure
Classification".

## Auth Rule

Use `loginViaUI(page)` for E2E tests that navigate to application pages.

Do not use `loginViaApi(page)` for SSR page tests. `loginViaApi()` seeds
client-side state, but server components fetch during SSR and cannot read that
state.

Allowed exception:

```ts
// e2e-governance-allow-loginViaApi: API-only test, no SSR page render.
```

Only use this marker when the test does not depend on server-rendered pages.

## Console And Network Guards

Application-page E2E specs must use:

```ts
collectConsoleMessages(page)
collectNetworkErrors(page)
assertClean(consoleMessages, networkErrors)
```

Allowed warnings/errors must be local to the spec and precise. Do not add broad
global suppressions.

## Process Output Rule

Do not use:

```ts
stderr: "ignore"
stdout: "ignore"
```

Do not use shell redirection that hides complete process output:

```bash
2>/dev/null
>/dev/null
```

Known runtime noise may be filtered only by exact documented pattern. Current
allowed dev-server noise:

```text
controller[kState].transformAlgorithm
```

All other stderr/stdout must pass through.

## Screenshot Rule

Do not screenshot every successful test.

Failure-only screenshots are allowed:

```ts
if (testInfo.status !== testInfo.expectedStatus) {
  await page.screenshot({ path, fullPage: true });
}
```

Screenshot filename patterns must be gitignored.

## Failure Classification

If full E2E fails, classify each failure:

| Test | URL | Failing locator/assertion | Classification | Root cause | Next action |
|---|---|---|---|---|---|

Allowed classifications:

- `obsolete-test`
- `real-regression`
- `environment-dependent`
- `covered-by-new-gate`
- `needs-product-decision`

Do not write "pre-existing" without this table.

## Interaction Gate Triggers

Run `npm run test:e2e:interaction` after touching:

- sheet/dialog code
- dropdown/multi-select code
- resource/database table row handling
- resource links
- theme/accent/provider code
- browser history or navigation code
- app layout code
```

- [ ] **Step 2: Verify the document exists**

Run:

```bash
test -f docs/e2e-governance.md && sed -n '1,220p' docs/e2e-governance.md
```

Expected: the document prints and includes the Auth Rule, Process Output Rule,
Screenshot Rule, and Failure Classification sections.

- [ ] **Step 3: Commit**

```bash
git add docs/e2e-governance.md
git commit -m "docs: add frontend e2e governance rules"
```

---

### Task 2: Add The Static E2E Governance Checker

**Files:**
- Create: `scripts/check-e2e-governance.mjs`

- [ ] **Step 1: Create the checker**

Create `scripts/check-e2e-governance.mjs` with this content:

```js
#!/usr/bin/env node

import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const root = process.cwd();
const failures = [];

function read(file) {
  return fs.readFileSync(path.join(root, file), "utf8");
}

function exists(file) {
  return fs.existsSync(path.join(root, file));
}

function listFiles(dir, predicate) {
  const abs = path.join(root, dir);
  if (!fs.existsSync(abs)) return [];

  const result = [];
  for (const entry of fs.readdirSync(abs, { withFileTypes: true })) {
    const rel = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      result.push(...listFiles(rel, predicate));
    } else if (predicate(rel)) {
      result.push(rel);
    }
  }
  return result;
}

function fail(file, message) {
  failures.push(`${file}: ${message}`);
}

if (!exists("docs/e2e-governance.md")) {
  fail("docs/e2e-governance.md", "missing governance document");
}

if (exists("playwright.config.ts")) {
  const config = read("playwright.config.ts");
  if (/stderr\s*:\s*["']ignore["']/.test(config)) {
    fail("playwright.config.ts", 'forbidden broad suppression: stderr: "ignore"');
  }
  if (/stdout\s*:\s*["']ignore["']/.test(config)) {
    fail("playwright.config.ts", 'forbidden broad suppression: stdout: "ignore"');
  }
  if (/(^|[^&])2>\s*\/dev\/null/.test(config) || /(^|[^&])>\s*\/dev\/null/.test(config)) {
    fail("playwright.config.ts", "forbidden process output redirection to /dev/null");
  }
}

const specs = listFiles("e2e", (file) => file.endsWith(".spec.ts"));

for (const spec of specs) {
  const source = read(spec);
  const loadsApplicationPage =
    /page\.goto\(["']\/(?!login)/.test(source) ||
    /locator\(["']a\[href=/.test(source) ||
    /getByRole\(["']link/.test(source);

  if (/loginViaApi\s*\(/.test(source) && !source.includes("e2e-governance-allow-loginViaApi")) {
    fail(spec, "loginViaApi used without e2e-governance-allow-loginViaApi exception");
  }

  if (loadsApplicationPage) {
    const hasConsoleGuard =
      source.includes("collectConsoleMessages") &&
      source.includes("collectNetworkErrors") &&
      source.includes("assertClean");
    if (!hasConsoleGuard) {
      fail(spec, "application-page E2E spec must use console/network guards");
    }
  }

  const screenshotCalls = [...source.matchAll(/page\.screenshot\s*\(/g)];
  if (screenshotCalls.length > 0 && !source.includes("testInfo.status !== testInfo.expectedStatus")) {
    fail(spec, "screenshots must be failure-only");
  }
}

if (failures.length > 0) {
  console.error("E2E governance check failed:");
  for (const failure of failures) {
    console.error(`- ${failure}`);
  }
  process.exit(1);
}

console.log(`E2E governance check passed (${specs.length} spec files scanned).`);
```

- [ ] **Step 2: Run the checker directly**

Run:

```bash
node scripts/check-e2e-governance.mjs
```

Expected:

```text
E2E governance check passed (... spec files scanned).
```

If it fails, fix the reported E2E spec or config violation. Do not weaken the
checker to hide a real violation.

- [ ] **Step 3: Commit**

```bash
git add scripts/check-e2e-governance.mjs
git commit -m "test: add e2e governance checker"
```

---

### Task 3: Wire The Checker Into Package Scripts

**Files:**
- Modify: `package.json`

- [ ] **Step 1: Add the npm script**

Add this script to `package.json`:

```json
"check:e2e-governance": "node scripts/check-e2e-governance.mjs"
```

Keep existing scripts unchanged.

- [ ] **Step 2: Run the npm script**

Run:

```bash
npm run check:e2e-governance
```

Expected:

```text
E2E governance check passed (... spec files scanned).
```

- [ ] **Step 3: Run static verification**

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add package.json
git commit -m "chore: wire e2e governance check"
```

---

### Task 4: Run Browser Gate Verification

**Files:**
- No code changes expected.

- [ ] **Step 1: Verify backend health**

Run:

```bash
curl -fsS http://localhost:8080/health
```

Expected:

```json
{"status":"ok"}
```

If backend is not running, start it from:

```bash
cd /Users/fan/GolangProjects/ControlHub
go run ./cmd/server
```

- [ ] **Step 2: Run smoke E2E**

Run:

```bash
npm run test:e2e:smoke
```

Expected: all tests pass.

- [ ] **Step 3: Run interaction E2E**

Run:

```bash
npm run test:e2e:interaction
```

Expected: all tests pass.

- [ ] **Step 4: Run full E2E**

Run:

```bash
npm run test:e2e
```

Expected: all tests pass.

If full E2E fails, do not call the phase complete. Create the classification
table required by `docs/e2e-governance.md`, fix real regressions, and rerun.

- [ ] **Step 5: Final status**

Run:

```bash
git status --short --branch
git log --oneline -5
```

Expected: clean working tree on `feat/phase-18c-e2e-governance`.

---

## Final Verification Matrix

Before final report, run:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
git status --short --branch
```

All commands must pass. If any command fails, fix the cause or report the exact
blocker with evidence.

## Final Report Requirements

Report:

- worktree path, branch, commit hashes
- files created/modified
- governance rules now enforced
- `npm run check:e2e-governance` result
- smoke/interaction/full E2E results
- confirmation that no backend files changed
- confirmation that no product UI files changed
- confirmation of no broad output suppression
- confirmation of no success-path screenshots
- confirmation of no tag/push/release
- confirmation of no AI co-author

