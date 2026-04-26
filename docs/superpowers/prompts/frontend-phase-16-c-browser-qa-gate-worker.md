# Frontend Phase 16C: Browser QA Gate

You are implementing the frontend browser QA gate for ControlHub Phase 16.

Repository:
`/Users/fan/JsProjects/ControlHub`

This phase exists because previous worker reports overclaimed completion while live browser review still found visible issues. The browser QA gate must make live verification repeatable and hard to fake.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-25-phase-16-unified-inventory-operator-workflow-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-25-phase-16-unified-inventory-operator-workflow.md`
- `/Users/fan/JsProjects/ControlHub/playwright.config.ts`
- `/Users/fan/JsProjects/ControlHub/package.json`
- `/Users/fan/JsProjects/ControlHub/e2e`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -10
git worktree list
```

Expected:

- worktree path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is dedicated to this phase, for example `feat/phase-16c-browser-qa-gate`
- frontend Phase 16B has landed, or the final report explicitly says which pages were not verified against 16B UI
- worktree is clean

Stop and report if path, branch, base, or cleanliness is wrong.

## Fixed Decisions

- Use real backend at `http://localhost:8080`.
- Use real frontend via Playwright `webServer`.
- Backend health check failure must fail the smoke suite.
- Do not silently skip browser verification.
- Do not use frontend route mocks for auth or API data.
- Do not call `/api/v1/*`; ControlHub backend paths have no `/api/v1` prefix.
- Do not commit screenshots unless explicitly instructed.
- Do not modify backend code.
- Do not add product UI features in this phase.

## Exact Scope

Allowed files:

- `e2e/harness/backend-health.ts`
- `e2e/harness/auth.ts`
- `e2e/harness/console-guards.ts`
- `e2e/operator-console-smoke.spec.ts`
- `package.json`
- `playwright.config.ts` only if needed

If another file is required, stop and report why before editing it.

## Required Harness

### 1. Backend Health

Create `e2e/harness/backend-health.ts`:

```typescript
import { expect, request } from "@playwright/test";

export const BACKEND_BASE_URL = process.env.CONTROLHUB_API_BASE_URL ?? "http://localhost:8080";

export async function expectBackendHealthy() {
  const api = await request.newContext();
  try {
    const response = await api.get(`${BACKEND_BASE_URL}/health`, { timeout: 5_000 });
    expect(response.ok(), `Backend health check failed at ${BACKEND_BASE_URL}/health`).toBe(true);
    await expect(response).toHaveJSON({ status: "ok" });
  } finally {
    await api.dispose();
  }
}
```

### 2. Real Login

Create `e2e/harness/auth.ts`:

```typescript
import { expect, type Page } from "@playwright/test";

export async function loginAsAdmin(page: Page) {
  await page.goto("/login");
  await page.getByLabel(/email/i).fill("admin@example.com");
  await page.getByLabel(/password/i).fill("secret123");
  await page.getByRole("button", { name: /sign in|login|登录/i }).click();
  await expect(page).toHaveURL(/\/overview/, { timeout: 15_000 });
}
```

### 3. Console And Network Guards

Create `e2e/harness/console-guards.ts`:

```typescript
import type { Page, Response } from "@playwright/test";
import { expect } from "@playwright/test";

interface GuardOptions {
  allowedStatus?: Array<{
    method?: string;
    urlIncludes: string;
    status: number;
    reason: string;
  }>;
}

interface BrowserFailure {
  kind: "console" | "pageerror" | "network";
  message: string;
}

function isBackendApiResponse(response: Response) {
  const url = response.url();
  return url.includes("localhost:8080") || url.includes("/auth/") || url.includes("/resources") || url.includes("/audit-events") || url.includes("/settings");
}

function isAllowedResponse(response: Response, allowedStatus: GuardOptions["allowedStatus"] = []) {
  const request = response.request();
  return allowedStatus.some((allowed) => {
    if (allowed.method && allowed.method !== request.method()) return false;
    if (allowed.status !== response.status()) return false;
    return response.url().includes(allowed.urlIncludes);
  });
}

export function installConsoleAndNetworkGuards(page: Page, options: GuardOptions = {}) {
  const failures: BrowserFailure[] = [];

  page.on("console", (message) => {
    if (message.type() === "error") {
      failures.push({ kind: "console", message: message.text() });
    }
  });

  page.on("pageerror", (error) => {
    failures.push({ kind: "pageerror", message: error.message });
  });

  page.on("response", (response) => {
    const status = response.status();
    if (status < 400) return;
    if (!isBackendApiResponse(response)) return;
    if (isAllowedResponse(response, options.allowedStatus)) return;

    failures.push({
      kind: "network",
      message: `${response.request().method()} ${response.url()} -> ${status}`,
    });
  });

  return {
    assertClean() {
      expect(failures, failures.map((f) => `${f.kind}: ${f.message}`).join("\n")).toEqual([]);
    },
  };
}
```

Every expected 4xx must be explicitly allowlisted with a reason. Do not broadly allow all 404 or all 401.

## Smoke Spec

Create `e2e/operator-console-smoke.spec.ts`.

It must cover:

- login
- `/overview`
- `/resources`
- `/resources/[id]`
- `/databases`
- `/settings`
- `/audits`

It must save local screenshots:

```text
qa-evidence/phase-16/overview.png
qa-evidence/phase-16/resources.png
qa-evidence/phase-16/resource-detail.png
qa-evidence/phase-16/databases.png
qa-evidence/phase-16/settings.png
qa-evidence/phase-16/audits.png
```

It must assert:

- key heading is visible
- no raw English enum leak in primary Chinese UI labels
- no raw `Database Cluster · Mysql · Running` style fallback
- topology graph renders on the chosen resource detail page
- console/network guard is clean

## Package Script

Add:

```json
{
  "test:e2e:smoke": "playwright test e2e/operator-console-smoke.spec.ts"
}
```

## Mandatory Negative Test

With backend stopped, run:

```bash
npm run test:e2e:smoke
```

Expected: fail with backend health check error.

Then start backend:

```bash
cd /Users/fan/GolangProjects/ControlHub
go run ./cmd/server
```

Run again:

```bash
cd /Users/fan/JsProjects/ControlHub
npm run test:e2e:smoke
```

Expected: pass, or fail on real issues that must be fixed before completion.

## Full Verification

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
```

Also run existing E2E if feasible:

```bash
npm run test:e2e
```

If full E2E is skipped, final report must say why.

## Commit

```bash
git add e2e/harness/backend-health.ts e2e/harness/auth.ts e2e/harness/console-guards.ts e2e/operator-console-smoke.spec.ts package.json playwright.config.ts
git diff --cached --stat
git diff --check --cached
git commit -m "test: add operator console browser smoke gate (Phase 16C)"
```

Only stage `playwright.config.ts` if changed.

## Final Report Required

Report:

- worktree path and branch
- commit hash
- files changed
- backend health preflight result
- real login evidence
- console errors result
- page errors result
- unexpected API 4xx/5xx result
- screenshot paths
- explicit allowlist table or `Explicit Allowlist: none`
- command results
- whether full E2E ran
- `git status --short --branch`
- confirmation:
  - no backend code modified
  - no product UI features added
  - no screenshots committed unless explicitly approved
  - no tag/push/release
  - no AI co-author

