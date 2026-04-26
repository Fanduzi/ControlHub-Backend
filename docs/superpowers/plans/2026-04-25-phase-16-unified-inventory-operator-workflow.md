# Phase 16 Unified Inventory IA + Database Operator Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stabilize the backend contract and consolidate ControlHub into a trustworthy unified inventory console with a database operator workflow.

**Architecture:** Phase 16 is cross-repo but sequenced. Backend first fixes test drift and freezes the inventory contract. Frontend then consolidates Resources/CMDB/Databases around that contract. Browser-backed QA is a required exit gate, not a separate optional polish pass.

**Tech Stack:** Go, chi, MySQL, goose, OpenAPI 3.1, Testcontainers, Schemathesis, Next.js App Router, React, TypeScript, shadcn/ui, TanStack Table, Playwright, Vitest.

---

## Required Startup Rules

- Use dedicated worktrees under each repo's `.worktrees/` directory.
- Start every task with `git status --short --branch`.
- Stop immediately if the worktree is not clean, except for explicitly identified unrelated generated artifacts.
- Follow TDD for every behavior change.
- Commit each task independently.
- Do not tag, push, release, or add AI co-author lines.
- Do not modify unrelated docs, screenshots, or generated QA artifacts.

---

## File Structure

### Backend Repo: `/Users/fan/GolangProjects/ControlHub`

| File | Responsibility | Expected Phase 16 Use |
|------|----------------|-----------------------|
| `internal/model/pagination.go` | Pagination caps and normalization | Phase 16.0 page-size cap fix |
| `internal/api/resource_handler.go` | Resource route parsing and response shape | Contract freeze for list/detail/members |
| `internal/api/resource_handler_test.go` | Handler contract tests | Page-size and response shape tests |
| `internal/repository/mysql/resource_repository.go` | Resource list/detail SQL | `profileSummary`, `clusterId`, member contract as needed |
| `internal/service/resource_service.go` | Resource service boundary | Contract orchestration as needed |
| `internal/openapi/openapi.yaml` | Public API contract | Must match frozen response shapes |
| `internal/integration/resource_test.go` | MySQL-backed resource behavior | Contract integration tests |
| `internal/integration/openapi_fuzz_test.go` | Schemathesis fuzz | Regression gate |

### Frontend Repo: `/Users/fan/JsProjects/ControlHub`

| File | Responsibility | Expected Phase 16 Use |
|------|----------------|-----------------------|
| `app/(console)/resources/page.tsx` | Canonical inventory route | IA consolidation |
| `components/resources/resource-table.tsx` | Inventory table | CMDB columns, filters, readable values |
| `app/(console)/cmdb/page.tsx` | Current CMDB route | Remove, redirect, or turn into saved inventory view |
| `app/(console)/databases/page.tsx` | Database operator route | Database tree/detail workflow |
| `components/databases/database-table.tsx` | Database tree/table | profile summary and cluster/member behavior |
| `components/resources/resource-detail-sheet.tsx` | Detail sheet | section order, members, readable relations |
| `app/(console)/resources/[id]/page.tsx` | Full resource detail | cluster member table and inventory detail model |
| `services/resources.ts` | Resource API client | Frozen backend contract consumption |
| `types/resource.ts` | Resource wire types | Frozen backend contract types |
| `e2e/*.spec.ts` | Browser QA | Required acceptance coverage |

---

## Phase 16.0: Backend Stabilization Patch

**Goal:** Re-green backend `main` before any contract or frontend milestone builds on it.

**Files:**
- Modify: `internal/model/pagination.go`
- Modify: `internal/api/resource_handler_test.go`
- Maybe modify: `internal/openapi/openapi.yaml`

### Task 16.0.1: Resolve Page-Size Cap Drift

- [ ] **Step 1: Create backend worktree**

Run:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree add .worktrees/phase-16-0-backend-stabilization -b phase-16-0-backend-stabilization main
cd .worktrees/phase-16-0-backend-stabilization
git status --short --branch
```

Expected:

```text
## phase-16-0-backend-stabilization
```

- [ ] **Step 2: Reproduce the failure**

Run:

```bash
go test ./...
```

Expected current failure:

```text
TestListResources_PageSizeCap
expected pageSize capped to 100, got 500
```

- [ ] **Step 3: Decide the contract**

Use this rule:

- If frontend database tree or resource tables require page sizes above 100, keep `MaxPageSize = 500`.
- Otherwise reduce `MaxPageSize` to 100.

Recommended decision for Phase 16:

```go
const (
	DefaultPage     = 1
	DefaultPageSize = 10
	MaxPageSize     = 500
	MaxPage         = 1_000_000_000
)
```

Rationale: recent database tree backend fixes already moved toward larger bounded page sizes. Keep the higher cap, update tests and OpenAPI to match.

- [ ] **Step 4: Update handler test expectation**

In `internal/api/resource_handler_test.go`, update `TestListResources_PageSizeCap` so it expects `500`, not `100`.

The expected assertion should read:

```go
if repo.lastQuery.PageSize != model.MaxPageSize {
	t.Fatalf("expected pageSize capped to %d, got %d", model.MaxPageSize, repo.lastQuery.PageSize)
}
```

Avoid hardcoding `500` in the test.

- [ ] **Step 5: Check OpenAPI pageSize maximum**

Search:

```bash
rg -n "pageSize|maximum: 100|maximum: 500" internal/openapi/openapi.yaml internal/model internal/api
```

If the OpenAPI spec still says `maximum: 100` for resource or audit page size, update it to match `model.MaxPageSize`.

- [ ] **Step 6: Run focused tests**

Run:

```bash
go test -count=1 ./internal/model ./internal/api
make openapi-validate
```

Expected: both pass.

- [ ] **Step 7: Run backend normal verification**

Run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
```

Expected: all pass.

- [ ] **Step 8: Run GitNexus change detection**

Run from the backend worktree:

```bash
npx gitnexus detect_changes --scope all
```

If GitNexus is unavailable, record the exact error in the final report and use `git diff --stat` as fallback.

- [ ] **Step 9: Commit**

Run:

```bash
git status --short
git add internal/model/pagination.go internal/api/resource_handler_test.go internal/openapi/openapi.yaml
git commit -m "fix: align resource page size cap contract (Phase 16.0)"
```

Only add files that actually changed.

---

## Phase 16A: Inventory Contract Freeze

**Goal:** Freeze the backend contract that the unified inventory frontend will consume.

**Backend contract decisions to make and prove:**

1. `GET /resources` includes reliable `profileSummary` where documented.
2. `GET /resources` and `GET /resources/{id}` agree on `clusterId` behavior.
3. `GET /resources/{id}` returns `{ resource, members? }` consistently.
4. `GET /resources/{id}/relations` provides readable related resource data or the frontend has an approved alternative lookup path.
5. OpenAPI examples match real JSON from the server.

### Task 16A.1: Contract Audit Report

**Files:**
- Create: `docs/superpowers/notes/2026-04-25-phase-16-inventory-contract-audit.md`

- [ ] **Step 1: Create audit note**

Create the note with these sections:

```markdown
# Phase 16 Inventory Contract Audit

## Backend Commit

## Endpoints Audited

| Endpoint | Current Shape | OpenAPI Match | Frontend Need | Decision |
|----------|---------------|---------------|---------------|----------|
| GET /resources | | | | |
| GET /resources/{id} | | | | |
| GET /resources/{id}/relations | | | | |
| GET /resource-subtypes | | | | |

## Gaps

## Required Backend Fixes

## Required Frontend Assumptions
```

- [ ] **Step 2: Capture live JSON examples**

Run backend locally against the dev database:

```bash
go run ./cmd/server
```

In another shell, capture:

```bash
curl -sS "http://localhost:8080/resources?page=1&pageSize=2" | jq .
curl -sS "http://localhost:8080/resources/41000000-0000-0000-0000-000000000010" | jq .
curl -sS "http://localhost:8080/resources/41000000-0000-0000-0000-000000000010/relations" | jq .
curl -sS "http://localhost:8080/resource-subtypes?resourceType=database_instance" | jq .
```

Record whether the response includes:

- `resource`
- `members`
- `profileSummary`
- `clusterId`
- readable related resource display fields

- [ ] **Step 3: Compare with OpenAPI**

Run:

```bash
make openapi-validate
rg -n "ResourceDetailResponse|profileSummary|clusterId|relatedResource|members|resource-subtypes" internal/openapi/openapi.yaml
```

Update the audit note with exact mismatches.

- [ ] **Step 4: Commit audit only**

Run:

```bash
git add docs/superpowers/notes/2026-04-25-phase-16-inventory-contract-audit.md
git commit -m "docs: audit inventory contract for Phase 16"
```

### Task 16A.2: Implement Only Blocking Backend Contract Fixes

Do not start this task until Task 16A.1 identifies a concrete blocker.

Allowed fixes:

- response envelope mismatch
- missing OpenAPI property that already exists in JSON
- JSON field type mismatch
- documented field not populated when frontend requires it
- relation/member display data required to remove UUID-first UI

Disallowed fixes:

- new storage tables
- new auth model
- topology visual changes
- frontend UI changes

Required verification:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
npx gitnexus detect_changes --scope all
```

Commit message pattern:

```bash
git commit -m "fix: freeze inventory contract for database workflow (Phase 16A)"
```

---

## Phase 16B: Frontend Unified Inventory IA

**Goal:** Make Resources, CMDB, and Databases read as one coherent inventory system.

**Dependency:** Do not claim completion until Phase 16A is merged to backend `main` or the frontend report explicitly lists which backend contract assumptions were not verified.

### Task 16B.1: Resources As Canonical Inventory

**Files:**
- Modify: `app/(console)/resources/page.tsx`
- Modify: `components/resources/resource-table.tsx`
- Modify: `components/app-shell/sidebar.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Write tests for canonical inventory labels**

Add or update tests so the sidebar and resources page prove:

- Resources is the canonical inventory entry.
- CMDB is not presented as a separate unexplained model.
- Resource table can show CMDB-style metadata through columns or detail metadata.

Suggested test names:

```typescript
it("renders resources as the canonical inventory entry")
it("does not present CMDB as a competing inventory model")
```

- [ ] **Step 2: Implement the navigation decision**

Use one of these decisions, based on product choice:

Preferred:

- remove CMDB from primary navigation
- keep `/cmdb` route as redirect or compatibility view

Acceptable temporary:

- rename CMDB to a clearly scoped saved inventory view
- make page copy explain it is metadata over the same resources

- [ ] **Step 3: Verify**

Run:

```bash
npx vitest run tests/components/sidebar.test.tsx tests/pages.list-pagination.test.tsx
npx tsc --noEmit -p tsconfig.json
npm run lint
```

Commit:

```bash
git commit -m "fix: make resources the canonical inventory entry (Phase 16B)"
```

### Task 16B.2: Database Operator Tree And Detail Workflow

**Files:**
- Modify: `app/(console)/databases/page.tsx`
- Modify: `components/databases/database-table.tsx`
- Modify: `components/resources/resource-detail-sheet.tsx`
- Modify: `app/(console)/resources/[id]/page.tsx`
- Modify: `types/resource.ts`
- Modify: `services/resources.ts`

- [ ] **Step 1: Write tests for database operator data**

Tests must prove:

- database table uses backend `profileSummary`, not local guesses
- cluster rows show member count or member summary
- instance rows show hostname/IP/port when present
- cluster detail shows member table
- UUIDs are not the primary display text for members or relations

Suggested tests:

```typescript
it("renders database profile summary columns from backend data")
it("renders cluster members in resource detail")
it("uses related resource display names before UUIDs")
```

- [ ] **Step 2: Implement against frozen backend types**

Update `types/resource.ts` only to match the Phase 16A OpenAPI truth.

Do not add frontend-only fields that are not in the backend contract unless they are clearly marked view-model fields.

- [ ] **Step 3: Implement table/detail UI**

Minimum UI behavior:

- `/databases` remains a database-specific tree/table.
- `hostname`, `ip`, `port`, `engine`, and `nodeCount` appear where available.
- cluster detail page has a visible member table above topology.
- relation/member links use display names.
- UUIDs are available as secondary copy/tooltip detail only.

- [ ] **Step 4: Verify focused tests**

Run:

```bash
npx vitest run tests/components/database-table.test.tsx tests/components/resource-detail-sheet.test.tsx tests/resource-detail-page.test.tsx
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git commit -m "feat: add database operator detail workflow (Phase 16B)"
```

### Task 16B.3: Remove Frontend Demo-ID Fallbacks

**Files:**
- Modify: `lib/view-models.ts`
- Modify: `lib/resource-copy.ts`
- Modify tests covering detail/list summaries

- [ ] **Step 1: Write failing tests**

Tests must fail if:

- `resourceSummaries` maps hardcoded demo IDs
- `actorLabels` maps hardcoded demo actor IDs
- production resources fall back to raw UUID-first display

Suggested tests:

```typescript
it("does not rely on hardcoded demo resource summaries")
it("renders resource summaries from backend fields or localized fallback")
it("does not render raw actor IDs as primary audit actor labels")
```

- [ ] **Step 2: Remove hardcoded maps**

Use backend-provided fields first. Use localized generic fallback only when backend data is absent.

- [ ] **Step 3: Verify**

Run:

```bash
npx vitest run tests/lib/view-models.test.ts tests/lib/resource-summary.test.ts
npm run test
```

Commit:

```bash
git commit -m "fix: remove demo-id copy fallbacks from view models (Phase 16B)"
```

---

## Phase 16C: Browser QA Gate

**Goal:** Make live verification repeatable and hard to overclaim.

**Harness policy:** Phase 16C uses the real backend at `http://localhost:8080` and the real frontend dev server from Playwright `webServer`. The smoke harness must fail if the backend health check is not green. Do not silently skip browser verification because the backend is unavailable.

### Task 16C.1: Add Browser Harness Utilities

**Files:**
- Create: `/Users/fan/JsProjects/ControlHub/e2e/harness/backend-health.ts`
- Create: `/Users/fan/JsProjects/ControlHub/e2e/harness/auth.ts`
- Create: `/Users/fan/JsProjects/ControlHub/e2e/harness/console-guards.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/package.json`

- [ ] **Step 1: Add backend health preflight**

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

This makes backend availability an explicit requirement. If the backend is down, the smoke suite fails before UI claims are made.

- [ ] **Step 2: Add real-login helper**

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

Rules:

- Use the real login form.
- Do not use `page.route()` to mock auth.
- Do not call `/api/v1/auth/login`; ControlHub backend paths have no `/api/v1` prefix.

- [ ] **Step 3: Add console/network guards**

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
      failures.push({
        kind: "console",
        message: message.text(),
      });
    }
  });

  page.on("pageerror", (error) => {
    failures.push({
      kind: "pageerror",
      message: error.message,
    });
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

Rules:

- Every expected 4xx must be explicitly allowlisted with a reason.
- Do not broadly allow all 404 or all 401.
- A smoke test with unexpected console/network failures is a failed smoke test.

- [ ] **Step 4: Add package script**

Modify `/Users/fan/JsProjects/ControlHub/package.json` scripts:

```json
{
  "test:e2e:smoke": "playwright test e2e/operator-console-smoke.spec.ts"
}
```

- [ ] **Step 5: Verify utilities compile**

Run:

```bash
cd /Users/fan/JsProjects/ControlHub
npx tsc --noEmit -p tsconfig.json
```

Expected: pass.

Commit:

```bash
git add e2e/harness/backend-health.ts e2e/harness/auth.ts e2e/harness/console-guards.ts package.json
git commit -m "test: add operator console browser harness (Phase 16C)"
```

### Task 16C.2: Add Critical Path Browser Smoke

**Files:**
- Create: `/Users/fan/JsProjects/ControlHub/e2e/operator-console-smoke.spec.ts`
- Maybe modify: `/Users/fan/JsProjects/ControlHub/playwright.config.ts`

- [ ] **Step 1: Write the smoke spec skeleton**

Create `e2e/operator-console-smoke.spec.ts`:

```typescript
import { expect, test } from "@playwright/test";
import { expectBackendHealthy } from "./harness/backend-health";
import { loginAsAdmin } from "./harness/auth";
import { installConsoleAndNetworkGuards } from "./harness/console-guards";

test.describe("Operator console smoke", () => {
  test.beforeAll(async () => {
    await expectBackendHealthy();
  });

  test.beforeEach(async ({ page }) => {
    const guards = installConsoleAndNetworkGuards(page);
    await loginAsAdmin(page);
    (page as unknown as { __guards: typeof guards }).__guards = guards;
  });

  test.afterEach(async ({ page }) => {
    (page as unknown as { __guards: ReturnType<typeof installConsoleAndNetworkGuards> }).__guards.assertClean();
  });

  test("overview renders trusted posture and attention data", async ({ page }) => {
    await page.goto("/overview");
    await expect(page.getByRole("heading", { name: /overview|概览/i })).toBeVisible();
    await expect(page.locator("body")).not.toContainText(/\bDegraded\b|\bRunning\b/);
    await page.screenshot({ path: "qa-evidence/phase-16/overview.png", fullPage: true });
  });

  test("resources inventory renders filters and detail navigation", async ({ page }) => {
    await page.goto("/resources");
    await expect(page.getByRole("heading", { name: /resources|资源/i })).toBeVisible();
    await expect(page.locator("body")).not.toContainText(/\bDatabase Cluster\b|\bRunning\b|\bDegraded\b/);
    await page.screenshot({ path: "qa-evidence/phase-16/resources.png", fullPage: true });
  });

  test("resource detail renders localized summary and topology surface", async ({ page }) => {
    await page.goto("/resources/41000000-0000-0000-0000-000000000010?topologyDepth=2&topologyExpanded=1");
    await expect(page.locator("body")).not.toContainText(/\bDatabase Cluster\b|\bRunning\b|\bDegraded\b/);
    await expect(page.getByTestId("topology-graph").first()).toBeVisible({ timeout: 15_000 });
    await page.screenshot({ path: "qa-evidence/phase-16/resource-detail.png", fullPage: true });
  });

  test("databases renders operator tree without raw fallback labels", async ({ page }) => {
    await page.goto("/databases?environment=prod&page=1&resourceSubtype=mysql");
    await expect(page.getByRole("heading", { name: /databases|数据库/i })).toBeVisible();
    await expect(page.locator("body")).not.toContainText(/\bDegraded\b|\bRunning\b/);
    await page.screenshot({ path: "qa-evidence/phase-16/databases.png", fullPage: true });
  });

  test("settings dictionaries render localized names", async ({ page }) => {
    await page.goto("/settings");
    await expect(page.getByRole("heading", { name: /settings|设置/i })).toBeVisible();
    await expect(page.locator("body")).not.toContainText("Top-level asset families from backend taxonomy");
    await page.screenshot({ path: "qa-evidence/phase-16/settings.png", fullPage: true });
  });

  test("audits render without timestamp overflow regressions", async ({ page }) => {
    await page.goto("/audits");
    await expect(page.getByRole("heading", { name: /audits|审计/i })).toBeVisible();
    await page.screenshot({ path: "qa-evidence/phase-16/audits.png", fullPage: true });
  });
});
```

Adjust heading regexes only if the current UI uses different localized text. Do not weaken checks to generic `body is visible`.

- [ ] **Step 2: Ensure screenshot directory exists during tests**

Add a `beforeAll` block if needed:

```typescript
import { mkdir } from "node:fs/promises";

test.beforeAll(async () => {
  await mkdir("qa-evidence/phase-16", { recursive: true });
  await expectBackendHealthy();
});
```

- [ ] **Step 3: Verify backend is mandatory**

Run with backend stopped:

```bash
cd /Users/fan/JsProjects/ControlHub
npm run test:e2e:smoke
```

Expected: fail with backend health check error. This proves the smoke harness does not silently skip.

Start backend:

```bash
cd /Users/fan/GolangProjects/ControlHub
go run ./cmd/server
```

Run again:

```bash
cd /Users/fan/JsProjects/ControlHub
npm run test:e2e:smoke
```

Expected: pass, or fail on real UI/network issues that must be fixed before Phase 16C closes.

- [ ] **Step 4: Verify screenshots**

Run:

```bash
find qa-evidence/phase-16 -type f -name "*.png" -maxdepth 1 -print
```

Expected:

```text
qa-evidence/phase-16/overview.png
qa-evidence/phase-16/resources.png
qa-evidence/phase-16/resource-detail.png
qa-evidence/phase-16/databases.png
qa-evidence/phase-16/settings.png
qa-evidence/phase-16/audits.png
```

Do not commit screenshots unless the repository policy explicitly changes. They are local evidence paths for final reports.

- [ ] **Step 5: Run full frontend verification**

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
```

Expected: all pass.

- [ ] **Step 6: Commit**

Run:

```bash
git add e2e/operator-console-smoke.spec.ts playwright.config.ts package.json
git commit -m "test: add operator console smoke coverage (Phase 16C)"
```

Only add `playwright.config.ts` if it changed.

### Task 16C.3: Add Final Report Evidence Requirements

**Files:**
- Modify: `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-25-phase-16-unified-inventory-operator-workflow.md`

- [ ] **Step 1: Ensure worker final report includes harness evidence**

Every Phase 16C final report must include:

```markdown
### Browser Harness Evidence

| Check | Result |
|-------|--------|
| Backend health preflight | |
| Real login used | |
| Console errors | |
| Page errors | |
| Unexpected API 4xx/5xx | |
| Screenshot paths | |

### Explicit Allowlist

| Method | URL contains | Status | Reason |
|--------|--------------|--------|--------|

### Not Verified

Not verified: <area>
Reason: <specific reason>
Risk: <what could still be broken>
```

If there are no allowlisted failures, write:

```text
Explicit Allowlist: none
```

---

## Final Cross-Repo Verification

Before Phase 16 closes, run these from clean main branches after merging all phase work.

### Backend

```bash
cd /Users/fan/GolangProjects/ControlHub
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
git status --short --branch
```

### Frontend

```bash
cd /Users/fan/JsProjects/ControlHub
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
git status --short --branch
```

### Live Browser Acceptance

Start backend and frontend from current main:

```bash
cd /Users/fan/GolangProjects/ControlHub
go run ./cmd/server
```

```bash
cd /Users/fan/JsProjects/ControlHub
npm run dev
```

Verify:

- `http://localhost:8080/health` returns `{"status":"ok"}`
- `http://localhost:3000/login` loads
- core pages render after login
- browser console has no unexpected errors
- screenshots are produced or explicitly marked as not captured

---

## Final Report Template

Every worker must report:

```markdown
## Phase 16 Report

### Worktree And Branch

### Commits

### Files Changed

### Contract Decisions

### Verification Results

| Check | Result |
|-------|--------|

### Browser Evidence

### Known Limitations

### Negative Scope Confirmation

- Did not tag/push/release
- Did not add AI co-author
- Did not modify unrelated generated artifacts
- Did not skip required verification without saying so
```

If any required verification was skipped, use this exact wording:

```text
Not verified: <area>
Reason: <specific reason>
Risk: <what could still be broken>
```
