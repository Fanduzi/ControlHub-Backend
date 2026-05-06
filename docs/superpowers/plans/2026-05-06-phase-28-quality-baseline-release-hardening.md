# Phase 28 Quality Baseline And Release Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish a documented, verified release-quality baseline across ControlHub frontend and backend without adding product features.

**Architecture:** Start with evidence: inventory current commands, tests, E2E suites, backend gates, and known risks. Then research targeted engineering-quality practices, add only small high-value guards if the evidence justifies them, and finish with a release checklist that future phases can execute.

**Tech Stack:** Go backend, MySQL/Testcontainers, OpenAPI/Schemathesis, Next.js App Router frontend, Vitest, Playwright, Node.js scripts, Make/npm quality gates.

---

## Required Documents

Read first:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-28-quality-baseline-release-hardening.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27b-e2e-query-param-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-28-frontend-e2e-governance-gate.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md
```

## Worktrees

This phase may touch both repositories. Use worktrees, not main.

Backend docs/scripts worktree:

```text
/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-28-quality-baseline-release-hardening
```

Backend branch:

```text
phase-28-quality-baseline-release-hardening
```

Frontend docs/scripts/test worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-28-quality-baseline-release-hardening
```

Frontend branch:

```text
feat/phase-28-quality-baseline-release-hardening
```

If you only change one repository, only create the required worktree for that
repository. Do not edit either main worktree directly.

## Phase Constraints

- No product UI changes.
- No backend API contract changes.
- No SQL execution.
- No write operations or work orders.
- No topology layout changes.
- No broad output suppression.
- No skipped/deleted tests.
- No timeout-only fixes.
- No tag, push, release, or merge.
- No AI co-author.

---

## File Structure

Expected backend-repo documentation files:

```text
docs/quality-baseline.md
docs/release-hardening-checklist.md
docs/superpowers/notes/2026-05-06-phase-28-quality-research.md
```

Optional backend code/script files only if justified:

```text
Makefile
scripts/check-quality-gates.sh
```

Expected frontend files if frontend work is needed:

```text
docs/quality-baseline.md
docs/release-hardening-checklist.md
scripts/check-e2e-preflight.mjs
tests/<script-or-helper-test>.test.ts
package.json
```

Follow existing repository patterns if these exact paths conflict with current
layout. Report any path changes in the final report.

---

## Task 1: Inventory Existing Quality Gates

**Files:**
- Create/modify: `docs/quality-baseline.md` in the backend docs worktree
- Read-only inspect: frontend `package.json`, backend `Makefile`, E2E specs,
  backend test directories, frontend test directories

- [ ] **Step 1: Create backend worktree**

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-28-quality-baseline-release-hardening -b phase-28-quality-baseline-release-hardening main
cd .worktrees/backend-phase-28-quality-baseline-release-hardening
git status --short --branch
```

Expected: clean branch `phase-28-quality-baseline-release-hardening`.

- [ ] **Step 2: Inventory backend commands**

Run:

```bash
grep -n "^[a-zA-Z0-9_.:-].*:" Makefile
find internal -maxdepth 3 -name '*_test.go' | sort
find . -maxdepth 3 -name '*openapi*' -o -name '*schemathesis*'
```

Record the actual command list in `docs/quality-baseline.md`.

- [ ] **Step 3: Inventory frontend commands**

Run from the frontend main repo or a frontend worktree if one already exists:

```bash
cd /Users/fan/JsProjects/ControlHub
node -e "const p=require('./package.json'); console.log(JSON.stringify(p.scripts,null,2))"
find e2e -maxdepth 2 -name '*.spec.ts' | sort
find tests -maxdepth 3 -name '*.test.ts' -o -name '*.test.tsx' | sort
find scripts -maxdepth 2 -type f | sort
```

Record the actual command list and suite list in `docs/quality-baseline.md`.

- [ ] **Step 4: Draft the quality baseline**

Create `docs/quality-baseline.md` with these sections:

```md
# ControlHub Quality Baseline

## Purpose

This document defines the current quality baseline for ControlHub. It records
which commands protect which behavior, where coverage is missing, and which
checks block future phase completion.

## Backend Gates

| Command | What It Protects | Required Before Merge | Notes |
|---|---|---|---|
| `go test -count=1 ./...` | Unit and package-level behavior | Yes | Runs without Docker |
| `go vet ./...` | Static correctness checks | Yes | Runs without Docker |
| `go build ./...` | Compilation across packages | Yes | Runs without Docker |
| `make openapi-validate` | OpenAPI YAML validity | Yes | Runs without Docker |
| `make test-integration` | MySQL/Testcontainers integration flows | Merge gate when Docker is available | Requires Docker |
| `make test-openapi-fuzz` | OpenAPI/runtime contract fuzzing | Release/nightly gate or merge gate for API changes | Requires Docker and Schemathesis |

## Frontend Gates

| Command | What It Protects | Required Before Merge | Notes |
|---|---|---|---|
| `npx tsc --noEmit -p tsconfig.json` | TypeScript contract and type safety | Yes | Runs without backend |
| `npm run lint` | ESLint rules and unused code | Yes | Runs without backend |
| `npm run test` | Unit/component behavior | Yes | Runs without backend |
| `npm run build` | Production build and SSR compatibility | Yes | Runs without backend |
| `npm run check:e2e-governance` | Browser-test policy compliance | Yes | Runs without backend |
| `npm run test:e2e:smoke` | Core console reachability | Yes when backend available | Requires backend |
| `npm run test:e2e:interaction` | Sheets/dropdowns/back navigation stability | Yes when frontend interaction code changes | Requires backend |
| `npm run test:e2e` | Full browser regression | Merge gate before phase close | Requires backend |

## Coverage Matrix

| Capability | Backend Unit | Backend Integration | OpenAPI/Fuzz | Frontend Unit/Component | E2E Smoke | E2E Interaction | E2E Workflow | Manual Browser | Gap / Next Action |
|---|---|---|---|---|---|---|---|---|---|
| Login and auth session | Partial | No | No | No | Yes | No | Yes | Yes | Keep UI login E2E |
| Console shell navigation | No | No | No | Partial | Yes | Yes | Yes | Yes | Covered |
| Environment context | No | No | No | Partial | Partial | Partial | Yes | Yes | Add cases only if regressions recur |
| Resource list pagination/query params | Partial | Partial | Partial | Partial | No | No | Yes | Yes | Covered by list-pagination E2E after Phase 27B |
| Database list search/filter/sort/signal | No | Backend rollup only | Partial | Yes | No | Yes | Yes | Yes | Covered |
| Database detail cluster abnormal member workflow | Backend read model | Yes | Partial | Yes | No | No | Yes | Yes | Covered |
| Database detail healthy instance workflow | Backend read model | Yes | Partial | Yes | No | No | Yes | Yes | Covered |
| Overview attention queue | No | No | No | Yes | Partial | No | Yes | Yes | Covered by database workflow |
| Topology load and same-origin API proxy | Backend read model | Yes | Partial | Yes | Yes | Yes | Yes | Yes | Covered |
| Audit list pagination/filtering | Partial | Partial | Partial | Partial | No | No | Yes | Yes | Covered by list-pagination E2E |
| Settings dictionaries | Backend dictionaries | Partial | Partial | Partial | Yes | No | No | Yes | Smoke only is acceptable |
| Backend database operational summary | Yes | Yes | OpenAPI schema | Frontend type coverage | No | No | Yes | Yes | Covered |

## Merge Blocking Rules

- Do not complete a phase with failing unit, lint, typecheck, or build gates.
- Do not call full E2E failures pre-existing without identical main comparison evidence.
- Do not merge browser-facing changes without smoke E2E.
- Do not merge interaction changes without interaction E2E.
- Do not merge backend API/read-model changes without OpenAPI validation and targeted backend tests.
```

- [ ] **Step 5: Commit baseline doc**

```bash
git add docs/quality-baseline.md
git commit -m "docs: document controlhub quality baseline"
```

## Task 2: Research Engineering-Quality Options

**Files:**
- Create: `docs/superpowers/notes/2026-05-06-phase-28-quality-research.md`

- [ ] **Step 1: Research only targeted questions**

Research these topics using official documentation or primary project docs where
possible:

```text
Playwright webServer reuseExistingServer and environment handling
Playwright traces/reporters for flaky failure diagnosis
Schemathesis/OpenAPI fuzzing role in API contract checks
CI gate layering: PR vs merge vs nightly
Visual regression tradeoffs for a data-heavy internal console
```

Do not write a generic testing essay. Every recommendation must map to a
ControlHub risk.

- [ ] **Step 2: Write research notes**

Create `docs/superpowers/notes/2026-05-06-phase-28-quality-research.md`.
Use this exact structure, but fill each row with a concrete source and a
specific ControlHub decision:

```md
# Phase 28 Quality Research Notes

## Research Questions

1. How should Playwright prevent stale dev server reuse when proxy env vars matter?
2. How should ControlHub split PR, merge, and nightly gates?
3. How should OpenAPI validation, fuzzing, and frontend types work together?
4. Should ControlHub add visual regression now?
5. How should flaky E2E failures be classified?

## Findings

| Topic | Source | Finding | ControlHub Decision |
|---|---|---|---|
| Playwright webServer reuse | Playwright official docs URL | State the exact relevant behavior | Adopt / defer / reject with reason |
| E2E traces | Playwright official docs URL | State the exact relevant behavior | Adopt / defer / reject with reason |
| OpenAPI fuzz | Schemathesis or OpenAPI source URL | State the exact relevant behavior | Adopt / defer / reject with reason |
| Gate layering | Source URL or existing local project policy | State the exact relevant behavior | Adopt / defer / reject with reason |
| Visual regression | Playwright official docs URL or local decision source | State the exact relevant behavior | Adopt / defer / reject with reason |

## Adopted Recommendations

- Add or preserve an E2E proxy/preflight check so stale server reuse fails fast.
- Keep full E2E as a phase-close gate.
- Keep OpenAPI validation as a merge gate for backend API changes.
- Keep OpenAPI fuzz as release/nightly or API-change gate because it is heavier.
- Defer visual regression unless layout churn continues or screenshot review becomes cheap in CI.

## Deferred Recommendations

- Browser matrix expansion: defer until cross-browser issues appear.
- Full visual regression: defer; current pain is semantic/interactivity rather than pixel drift.

## Rejected Recommendations

- Broad retries as flake management: reject because they hide root causes.
- Skipping known failures: reject unless there is an issue link and owner.
```

Do not commit generic statements such as "best practice says." Every row must
name a concrete source and state the decision for ControlHub.

- [ ] **Step 3: Commit research notes**

```bash
git add docs/superpowers/notes/2026-05-06-phase-28-quality-research.md
git commit -m "docs: record phase 28 quality research"
```

## Task 3: Add Release Hardening Checklist

**Files:**
- Create: `docs/release-hardening-checklist.md`

- [ ] **Step 1: Create checklist**

Create `docs/release-hardening-checklist.md`:

```md
# Release Hardening Checklist

## Purpose

Use this checklist before merging a phase or preparing a release. It separates
fast local gates, backend contract gates, browser gates, and manual checks.

## Frontend Preflight

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
lsof -nP -iTCP:3000 -sTCP:LISTEN || true
lsof -nP -iTCP:3100 -sTCP:LISTEN || true
lsof -nP -iTCP:8081 -sTCP:LISTEN || true
```

If `:3100` or `:8081` is occupied before E2E, confirm whether it is the current
Playwright webServer. Kill stale frontend/proxy processes before full E2E.

## Backend Preflight

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
curl -fsS http://localhost:8080/health
```

If backend is not running, start it explicitly and record the PID in the final
report.

## Backend Gates

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
```

For backend API/read-model changes, also run when Docker is available:

```bash
make test-integration
make test-openapi-fuzz
```

## Frontend Gates

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

## Manual Browser Checks

Minimum pages:

```text
/overview?environment=prod
/databases?environment=prod
/resources/14
/resources/22
/resources?page=1&pageSize=1
/audits?page=1&pageSize=1
```

Check:

```text
console errors = 0
network 4xx/5xx = 0 unless expected and documented
all API calls use /__api in browser
database list search/filter/sort remain interactive
sheet/dialog opens and closes
topology loads
overview attention reason matches database detail reason
```

## Failure Classification

Do not write "pre-existing" without this table:

| Test | Branch | Main comparison | Error | Classification | Owner / next action |
|---|---|---|---|---|---|

Allowed classifications:

```text
real_regression
environment_gap
obsolete_test
needs_product_decision
main_preexisting_with_identical_evidence
```

## Merge Blockers

- Dirty worktree.
- Untracked artifacts that are not intentionally ignored.
- Failing typecheck, lint, unit, or build.
- Failing E2E without classification and owner.
- Broad output suppression.
- Skipped/deleted tests without explicit approval.
- Backend API change without OpenAPI validation.
- Docker-dependent backend gate skipped without stating why.
```

- [ ] **Step 2: Commit checklist**

```bash
git add docs/release-hardening-checklist.md
git commit -m "docs: add release hardening checklist"
```

## Task 4: Decide Whether To Add Small Automation

**Files if adopted:**
- Frontend: `scripts/check-e2e-preflight.mjs`, `package.json`,
  `tests/scripts/check-e2e-preflight.test.ts`
- Backend: `Makefile` for a quality aggregate target

- [ ] **Step 1: Review evidence**

Use Tasks 1-3 to decide whether automation is needed now.

Adopt only if it protects a known recurring risk:

```text
stale :3100 frontend server
stale :8081 E2E proxy
forgotten frontend quality command sequence
forgotten backend quality command sequence
```

- [ ] **Step 2: If adding frontend preflight, create frontend worktree**

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree add .worktrees/frontend-phase-28-quality-baseline-release-hardening -b feat/phase-28-quality-baseline-release-hardening main
cd .worktrees/frontend-phase-28-quality-baseline-release-hardening
git status --short --branch
```

- [ ] **Step 3: Add a minimal preflight script only if adopted**

If adopted, implement a script that checks `:3100` and `:8081` and prints
actionable output. It must not kill processes automatically.

Required behavior:

```text
port free -> pass
port occupied -> print PID, command, and reminder to verify whether it is stale
```

Do not auto-kill user processes.

- [ ] **Step 4: Add tests for script parsing where practical**

If the script has pure parsing helpers, test them with Vitest.

- [ ] **Step 5: Commit automation**

```bash
git add scripts/check-e2e-preflight.mjs package.json tests/scripts/check-e2e-preflight.test.ts
git commit -m "test: add e2e preflight diagnostics"
```

If no automation is added, write the decision in the final report:

```text
No automation added because the existing Phase 27B proxy guard covers stale proxy recording, and the checklist covers manual process cleanup.
```

## Task 5: Verification

Run backend gates from the backend worktree:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
```

If Docker is running:

```bash
make test-integration
make test-openapi-fuzz
```

If Docker is not running, state that explicitly.

Run frontend gates if frontend files changed:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

If frontend files did not change, at least document the latest known frontend
gate status from Phase 27B:

```text
main @ 72bcb27, full E2E 50/50 passed
```

## Final Report Requirements

Include:

```text
worktree / branch / commits per repo
documents created
coverage matrix summary
research sources and adopted/deferred/rejected recommendations
automation added or explicitly deferred
backend verification matrix
frontend verification matrix if frontend changed
known remaining gaps
release-blocking rules
clean git status
scope confirmation
```

Scope confirmation:

```text
No product UI changes
No backend API contract changes
No SQL
No write operations
No topology layout changes
No broad output suppression
No skipped/deleted tests
No tag/push/release
No AI co-author
```
