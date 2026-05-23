# Phase 29 Release Readiness Mechanism Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a repeatable release-readiness mechanism that evaluates a backend/frontend commit pair, runs named gates, performs live smoke checks, and records a go/no-go evidence bundle.

**Architecture:** Split implementation across backend/docs and frontend worktrees. Backend owns canonical release-readiness documentation, Makefile aggregate gates, and candidate evidence templates. Frontend owns npm aggregate gates and optional CDP live smoke inspired by MusicRadio. The two streams meet in a final dry-run evidence file.

**Tech Stack:** Go backend, Make, OpenAPI/Schemathesis, Testcontainers, Next.js frontend, npm scripts, Playwright, optional Chrome DevTools Protocol smoke script, Markdown evidence files.

---

## Required Documents

Read first:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-23-phase-29-release-readiness-mechanism.md
/Users/fan/GolangProjects/ControlHub/docs/quality-baseline.md
/Users/fan/GolangProjects/ControlHub/docs/release-hardening-checklist.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-05-06-phase-28-quality-research.md
```

Reference repos:

```text
DeltaScope backend gate patterns:
/Users/fan/GolangProjects/DeltaScope/Makefile
/Users/fan/GolangProjects/DeltaScope/.github/workflows/release.yml
/Users/fan/GolangProjects/DeltaScope/.github/workflows/release-smoke.yml

MusicRadio CDP smoke patterns:
/Users/fan/JsProjects/MusicRadio/scripts/cdp-helper.cjs
/Users/fan/JsProjects/MusicRadio/scripts/cdp-ui-test.cjs
/Users/fan/JsProjects/MusicRadio/scripts/cdp-playback-test.cjs
```

## Worktrees

Backend worktree:

```text
/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-29-release-readiness-mechanism
```

Backend branch:

```text
phase-29-release-readiness-mechanism
```

Frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-29-release-readiness-mechanism
```

Frontend branch:

```text
feat/phase-29-release-readiness-mechanism
```

Do not edit either main worktree directly.

## Phase Constraints

- No product UI changes.
- No backend API contract changes.
- No SQL execution.
- No write operations or work orders.
- No topology layout changes.
- No publishing, deployment, tags, or pushes.
- No broad output suppression.
- No skipped/deleted tests.
- No AI co-author.

---

## Task 1: Add Backend Release Gate Targets

**Files:**
- Modify: `Makefile`
- Modify: `docs/release-hardening-checklist.md`
- Modify: `docs/quality-baseline.md`

- [ ] **Step 1: Create backend worktree**

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-29-release-readiness-mechanism -b phase-29-release-readiness-mechanism main
cd .worktrees/backend-phase-29-release-readiness-mechanism
git status --short --branch
```

Expected: clean branch `phase-29-release-readiness-mechanism`.

- [ ] **Step 2: Inspect current Makefile**

```bash
sed -n '1,120p' Makefile
```

Expected: existing targets include `test`, `test-integration`,
`test-openapi-fuzz`, `openapi-validate`.

- [ ] **Step 3: Add release gate targets**

Add these targets to `Makefile`:

```make
.PHONY: test test-integration test-openapi-fuzz run openapi-validate migrate-up migrate-status migrate-down-one migrate-reset-dev cutover-local release-local-gates release-docker-gates release-readiness-gates

release-local-gates: ## Run local backend release-readiness gates (no Docker)
	go test -count=1 ./...
	go vet ./...
	go build ./...
	$(MAKE) openapi-validate

release-docker-gates: ## Run Docker-backed backend release-readiness gates
	$(MAKE) test-integration
	$(MAKE) test-openapi-fuzz

release-readiness-gates: release-local-gates release-docker-gates ## Run all backend release-readiness gates
```

If the `.PHONY` line already exists, extend it instead of duplicating it.

Do not remove existing targets.

- [ ] **Step 4: Verify Make target listing**

```bash
grep -n "release-local-gates\\|release-docker-gates\\|release-readiness-gates" Makefile
```

Expected: all three target names are present exactly once as target
definitions, plus any `.PHONY` occurrence.

- [ ] **Step 5: Update backend docs**

In `docs/quality-baseline.md`, add rows for:

```text
make release-local-gates
make release-docker-gates
make release-readiness-gates
```

In `docs/release-hardening-checklist.md`, add a section:

```md
## Backend Release Gate Shortcuts

```bash
make release-local-gates
make release-docker-gates
make release-readiness-gates
```

`release-local-gates` is the no-Docker baseline. `release-docker-gates`
requires Docker. `release-readiness-gates` composes both and is the strongest
local backend readiness signal.
```

- [ ] **Step 6: Run backend local gate**

```bash
make release-local-gates
```

Expected: passes.

- [ ] **Step 7: Commit backend release gates**

```bash
git add Makefile docs/quality-baseline.md docs/release-hardening-checklist.md
git commit -m "build: add backend release readiness gates"
```

## Task 2: Add Release Candidate Evidence Template

**Files:**
- Create: `docs/releases/candidates/TEMPLATE.md`
- Modify: `docs/release-hardening-checklist.md`

- [ ] **Step 1: Create candidate evidence directory**

```bash
mkdir -p docs/releases/candidates
```

- [ ] **Step 2: Create template**

Create `docs/releases/candidates/TEMPLATE.md`:

```md
# ControlHub Release Candidate Evidence

## Candidate

| Field | Value |
|---|---|
| Candidate ID | YYYY-MM-DD-controlhub-rc-local |
| Date | YYYY-MM-DD |
| Backend commit | BACKEND_COMMIT_SHA |
| Frontend commit | FRONTEND_COMMIT_SHA |
| Evaluator | EVALUATOR_NAME |
| Decision | GO / NO-GO |

## Backend Gates

| Gate | Command | Result | Notes |
|---|---|---|---|
| Backend local gates | `make release-local-gates` | PASS / FAIL |  |
| Backend Docker gates | `make release-docker-gates` | PASS / FAIL / NOT RUN | Requires Docker |

## Frontend Gates

| Gate | Command | Result | Notes |
|---|---|---|---|
| Frontend local gates | `npm run release:local` | PASS / FAIL |  |
| Frontend browser gates | `npm run release:e2e` | PASS / FAIL | Requires backend |
| Frontend live smoke | `npm run release:smoke:cdp` | PASS / FAIL / NOT RUN | Requires Chrome CDP |

## Live Browser Smoke

| URL | Expected | Result | Notes |
|---|---|---|---|
| `/overview?environment=prod` | attention queue loads | PASS / FAIL |  |
| `/databases?environment=prod` | database list controls usable | PASS / FAIL |  |
| `/resources/14` | abnormal cluster detail loads | PASS / FAIL |  |
| `/resources/22` | healthy instance detail loads | PASS / FAIL |  |
| `/resources?page=1&pageSize=1` | resource pagination loads | PASS / FAIL |  |
| `/audits?page=1&pageSize=1` | audit pagination loads | PASS / FAIL |  |

## Known Gaps

- List accepted gaps here.

## Failure Classification

| Failure | Classification | Evidence | Owner / Next Action |
|---|---|---|---|

## Go / No-Go Decision

Decision:

Reason:
```

- [ ] **Step 3: Update release checklist**

Add to `docs/release-hardening-checklist.md`:

```md
## Evidence Bundle

Every release-readiness run must create a candidate evidence document from:

```text
docs/releases/candidates/TEMPLATE.md
```

Store local dry-run evidence under:

```text
docs/releases/candidates/YYYY-MM-DD-controlhub-rc-local.md
```
```

- [ ] **Step 4: Commit template**

```bash
git add docs/releases/candidates/TEMPLATE.md docs/release-hardening-checklist.md
git commit -m "docs: add release candidate evidence template"
```

## Task 3: Add Frontend Release Gate Scripts

**Files:**
- Modify: `package.json`

- [ ] **Step 1: Create frontend worktree**

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-29-release-readiness-mechanism -b feat/phase-29-release-readiness-mechanism main
cd .worktrees/frontend-phase-29-release-readiness-mechanism
git status --short --branch
```

Expected: clean branch `feat/phase-29-release-readiness-mechanism`.

- [ ] **Step 2: Inspect package scripts**

```bash
node -e "const p=require('./package.json'); console.log(JSON.stringify(p.scripts,null,2))"
```

Expected: scripts include `check:e2e-preflight`, `check:e2e-governance`,
`test:e2e:smoke`, `test:e2e:interaction`, `test:e2e`.

- [ ] **Step 3: Add aggregate scripts**

Modify `package.json` scripts:

```json
{
  "release:local": "npm run check:e2e-preflight && npm run check:e2e-governance && npx tsc --noEmit -p tsconfig.json && npm run lint && npm run test && npm run build",
  "release:e2e": "npm run test:e2e:smoke && npm run test:e2e:interaction && npm run test:e2e",
  "release:check": "npm run release:local && npm run release:e2e"
}
```

Preserve existing scripts.

- [ ] **Step 4: Verify script listing**

```bash
node -e "const s=require('./package.json').scripts; console.log(s['release:local']); console.log(s['release:e2e']); console.log(s['release:check'])"
```

Expected: all three scripts print.

- [ ] **Step 5: Run local frontend release gate**

```bash
npm run release:local
```

Expected: passes.

- [ ] **Step 6: Commit frontend release scripts**

```bash
git add package.json
git commit -m "build: add frontend release readiness scripts"
```

## Task 4: Add Optional Frontend CDP Live Smoke

**Files:**
- Create: `scripts/cdp-release-smoke.mjs`
- Create: `tests/scripts/cdp-release-smoke.test.ts`
- Modify: `package.json`

- [ ] **Step 1: Write tests for pure helpers**

Create `tests/scripts/cdp-release-smoke.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  hasForbiddenRawEnum,
  summarizeSmokeResult,
} from "../../scripts/cdp-release-smoke.mjs";

describe("cdp release smoke helpers", () => {
  it("detects raw enum leaks in visible text", () => {
    expect(hasForbiddenRawEnum("排序 abnormal_first")).toBe(true);
    expect(hasForbiddenRawEnum("运维信号 needs_attention")).toBe(true);
    expect(hasForbiddenRawEnum("排序 异常优先")).toBe(false);
  });

  it("summarizes failed page checks", () => {
    expect(
      summarizeSmokeResult([
        { url: "/overview?environment=prod", ok: true, checks: [] },
        { url: "/databases?environment=prod", ok: false, checks: ["raw enum"] },
      ]),
    ).toContain("/databases?environment=prod");
  });
});
```

- [ ] **Step 2: Run tests and confirm failure**

```bash
npm run test -- tests/scripts/cdp-release-smoke.test.ts
```

Expected: fails because script does not exist.

- [ ] **Step 3: Create CDP smoke script**

Create `scripts/cdp-release-smoke.mjs` with:

```js
#!/usr/bin/env node

import http from "node:http";
import process from "node:process";

const CDP_PORT = Number(process.env.CDP_PORT || "9222");
const BASE_URL = process.env.CONTROLHUB_FRONTEND_URL || "http://localhost:3000";

const pages = [
  { url: "/overview?environment=prod", expect: ["概览"] },
  { url: "/databases?environment=prod", expect: ["数据库集群与实例"] },
  { url: "/resources/14", expect: ["资源拓扑"] },
  { url: "/resources/22", expect: ["资源拓扑"] },
  { url: "/resources?page=1&pageSize=1", expect: ["资源"] },
  { url: "/audits?page=1&pageSize=1", expect: ["审计"] },
];

const forbiddenRawEnums = [
  "abnormal_first",
  "needs_attention",
  "databaseSignal",
  "databaseSort",
];

export function hasForbiddenRawEnum(text) {
  return forbiddenRawEnums.some((value) => text.includes(value));
}

export function summarizeSmokeResult(results) {
  const failed = results.filter((result) => !result.ok);
  if (failed.length === 0) return "CDP release smoke passed";
  return failed
    .map((result) => `${result.url}: ${result.checks.join(", ")}`)
    .join("\n");
}

function getJson(path) {
  return new Promise((resolve, reject) => {
    http
      .get(`http://127.0.0.1:${CDP_PORT}${path}`, (response) => {
        let body = "";
        response.on("data", (chunk) => {
          body += chunk;
        });
        response.on("end", () => {
          try {
            resolve(JSON.parse(body));
          } catch (error) {
            reject(error);
          }
        });
      })
      .on("error", reject);
  });
}

let messageId = 0;

function cdpSend(ws, method, params = {}) {
  const id = ++messageId;
  return new Promise((resolve, reject) => {
    const timeout = setTimeout(() => reject(new Error(`Timeout: ${method}`)), 15000);
    const handler = (event) => {
      const message = JSON.parse(event.data.toString());
      if (message.id !== id) return;
      clearTimeout(timeout);
      ws.removeEventListener("message", handler);
      if (message.error) reject(new Error(JSON.stringify(message.error)));
      else resolve(message.result);
    };
    ws.addEventListener("message", handler);
    ws.send(JSON.stringify({ id, method, params }));
  });
}

async function evaluate(ws, expression) {
  const result = await cdpSend(ws, "Runtime.evaluate", {
    expression,
    returnByValue: true,
    awaitPromise: true,
  });
  return result.result?.value;
}

async function connect() {
  const targets = await getJson("/json/list");
  const page = targets.find((target) => target.type === "page") ?? targets[0];
  if (!page?.webSocketDebuggerUrl) {
    throw new Error(`No CDP page target found on port ${CDP_PORT}`);
  }
  const ws = new WebSocket(page.webSocketDebuggerUrl);
  await new Promise((resolve, reject) => {
    ws.addEventListener("open", resolve, { once: true });
    ws.addEventListener("error", reject, { once: true });
  });
  await cdpSend(ws, "Runtime.enable");
  await cdpSend(ws, "Page.enable");
  await cdpSend(ws, "Network.enable");
  await cdpSend(ws, "Log.enable");
  return ws;
}

async function runSmoke() {
  const ws = await connect();
  const results = [];
  const runtimeErrors = [];
  const networkErrors = [];
  ws.addEventListener("message", (event) => {
    const message = JSON.parse(event.data.toString());
    if (message.method === "Runtime.exceptionThrown") {
      runtimeErrors.push(message.params?.exceptionDetails?.text ?? "runtime exception");
    }
    if (message.method === "Log.entryAdded" && message.params?.entry?.level === "error") {
      runtimeErrors.push(message.params.entry.text ?? "console error");
    }
    if (message.method === "Network.responseReceived") {
      const status = message.params?.response?.status;
      const url = message.params?.response?.url ?? "";
      if (status >= 400) networkErrors.push(`${status} ${url}`);
    }
  });
  try {
    for (const page of pages) {
      const url = `${BASE_URL}${page.url}`;
      runtimeErrors.length = 0;
      networkErrors.length = 0;
      await cdpSend(ws, "Page.navigate", { url });
      await new Promise((resolve) => setTimeout(resolve, 1200));
      const text = (await evaluate(ws, "document.body.innerText")) ?? "";
      const checks = [];
      for (const expected of page.expect) {
        if (!text.includes(expected)) checks.push(`missing text: ${expected}`);
      }
      if (hasForbiddenRawEnum(text)) checks.push("raw enum leak");
      if (runtimeErrors.length > 0) checks.push(`runtime errors: ${runtimeErrors.join("; ")}`);
      if (networkErrors.length > 0) checks.push(`network errors: ${networkErrors.join("; ")}`);
      results.push({ url: page.url, ok: checks.length === 0, checks });
    }
  } finally {
    ws.close();
  }
  return results;
}

if (import.meta.url === `file://${process.argv[1]}`) {
  runSmoke()
    .then((results) => {
      const summary = summarizeSmokeResult(results);
      console.log(summary);
      if (results.some((result) => !result.ok)) process.exit(1);
    })
    .catch((error) => {
      console.error(
        `CDP release smoke failed to connect. Start Chrome with --remote-debugging-port=${CDP_PORT} and ensure ${BASE_URL} is reachable.`,
      );
      console.error(error.message);
      process.exit(1);
    });
}
```

- [ ] **Step 4: Add npm script**

Add to `package.json`:

```json
"release:smoke:cdp": "node scripts/cdp-release-smoke.mjs"
```

Do not include this in `release:check` because it requires a manually-started
CDP browser.

- [ ] **Step 5: Verify tests**

```bash
npm run test -- tests/scripts/cdp-release-smoke.test.ts
```

Expected: passes.

- [ ] **Step 6: Commit CDP smoke**

```bash
git add scripts/cdp-release-smoke.mjs tests/scripts/cdp-release-smoke.test.ts package.json
git commit -m "test: add cdp release smoke diagnostics"
```

## Task 5: Run Release Readiness Dry Run

**Files:**
- Create: `docs/releases/candidates/2026-05-23-controlhub-rc-local.md`

- [ ] **Step 1: Get current commits**

Backend:

```bash
cd /Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-29-release-readiness-mechanism
git rev-parse --short HEAD
```

Frontend:

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-29-release-readiness-mechanism
git rev-parse --short HEAD
```

- [ ] **Step 2: Run backend gates**

```bash
cd /Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-29-release-readiness-mechanism
make release-local-gates
```

If Docker is available:

```bash
make release-docker-gates
```

- [ ] **Step 3: Run frontend gates**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-29-release-readiness-mechanism
npm run release:local
npm run release:e2e
```

- [ ] **Step 4: Optionally run CDP smoke**

Only run if Chrome is available with remote debugging:

```bash
CDP_PORT=9222 CONTROLHUB_FRONTEND_URL=http://localhost:3000 npm run release:smoke:cdp
```

If no CDP browser is running, mark this as `NOT RUN` and explain.

- [ ] **Step 5: Create dry-run evidence**

In backend worktree, create:

```text
docs/releases/candidates/2026-05-23-controlhub-rc-local.md
```

Use `docs/releases/candidates/TEMPLATE.md` and fill actual results. Do not leave
`PASS / FAIL` placeholders.

- [ ] **Step 6: Commit dry-run evidence**

```bash
git add docs/releases/candidates/2026-05-23-controlhub-rc-local.md
git commit -m "docs: record phase 29 release readiness dry run"
```

## Task 6: Final Verification

Backend:

```bash
cd /Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-29-release-readiness-mechanism
git diff --check
make release-local-gates
```

Also scan `docs/releases`, `docs/quality-baseline.md`, and
`docs/release-hardening-checklist.md` for unfilled template markers, conflict
markers, and unresolved candidate result placeholders. The scan must return no
matches for the final dry-run evidence file.

If Docker is available:

```bash
make release-docker-gates
```

Frontend:

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-29-release-readiness-mechanism
npm run release:local
npm run release:e2e
npm run test -- tests/scripts/cdp-release-smoke.test.ts
```

## Final Report Requirements

Report:

```text
backend worktree / branch / commits
frontend worktree / branch / commits
backend gate targets added
frontend npm scripts added
CDP smoke implemented or deferred
release candidate evidence path
dry-run go/no-go decision
backend verification matrix
frontend verification matrix
Docker-dependent gate result
CDP live smoke result or explicit not-run reason
clean git status for both repos
scope confirmation
```

Scope confirmation:

```text
No product UI changes
No backend API contract changes
No SQL
No write operations
No topology layout changes
No publish/deploy/tag/push
No broad output suppression
No skipped/deleted tests
No AI co-author
```
