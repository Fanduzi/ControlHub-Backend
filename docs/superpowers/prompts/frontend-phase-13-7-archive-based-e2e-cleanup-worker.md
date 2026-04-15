# Frontend Phase 13.7: Archive-Based E2E Cleanup

You are implementing the frontend archive-based E2E cleanup phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-04-14-agent-friendly-integration-testing-roadmap.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-6-e2e-login-and-harness-cleanup-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-1-resource-archive-worker.md`
- `/Users/fan/JsProjects/ControlHub/e2e`
- `/Users/fan/JsProjects/ControlHub/playwright.config.ts`
- `/Users/fan/JsProjects/ControlHub/services/resources.ts`
- `/Users/fan/JsProjects/ControlHub/types/resource.ts`
- `/Users/fan/JsProjects/ControlHub/tests`

## Goal

Frontend Phase 13.6 already uses real backend login and API-assisted E2E setup, but test-created resources are only decommissioned after tests. Backend Phase 12.1 now provides real archive semantics:

- `POST /resources/{id}/archive`
- default `GET /resources` excludes archived resources
- `GET /resources?includeArchived=true` includes archived resources

This phase switches frontend E2E cleanup to archive-based cleanup so repeated AI-agent runs do not keep polluting normal list views with old `e2e-` resources.

This is a test-harness and contract-alignment phase, not a product-UI feature phase.

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
```

Expected:

- worktree path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is dedicated to this phase
- base includes frontend Phase 13.6 on `main`
- backend Phase 12.1 archive contract is already available on the local backend
- worktree is clean

Stop and report if the worktree path, branch, or base is wrong.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Keep real backend login E2E from Phase 13.6.
- Replace resource cleanup by decommission-only patching with backend archive cleanup where the backend supports it.
- Keep relation cleanup with `DELETE /resource-relations/{id}`.
- Do not add new product UI, archive management UI, or archived-resource browsing UI in this phase.
- Do not add frontend-only mocks, Prism, WireMock, Pact, k6, or backend API changes.
- Preserve `.worktrees/**` excludes for TypeScript, Vitest, ESLint, Playwright, Next.js, and build/test globs.
- Use project-local worktree path under `/Users/fan/JsProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. add frontend helper support for `POST /resources/{id}/archive`
2. switch E2E cleanup from decommission-only resource patching to archive-based cleanup
3. keep relation cleanup explicit and deterministic
4. keep tests independent from exact seed row counts
5. verify that archived test resources no longer reappear in default resource list flows
6. keep existing tooling isolation and real-backend login behavior intact

Do not change product UI unless a test reveals a real bug. If a UI bug is found, add or update the failing test first and fix the smallest code path.

## Backend Contract To Use

Use the backend Phase 12.1 contract exactly:

### `POST /resources/{id}/archive`

Request:

```json
{
  "reason": "e2e cleanup"
}
```

Response:

- `200 OK`
- body is the archived `Resource`

Behavior assumptions:

- repeated archive is idempotent
- archived resources are excluded from default `GET /resources`
- archived resources remain directly fetchable by ID

Do not invent any hard-delete flow.

## Helper Changes

Update E2E helpers under `e2e/` as needed.

At minimum support:

- authenticated login/session setup
- create test resource
- create test relation
- delete test relation
- archive test resource

Preferred helper shape:

- keep existing helper names if they already fit
- replace or supersede `decommissionTestResource()` with archive-based cleanup
- if temporary patching is still needed before archive in a narrow case, document why and keep it minimal

Resource naming must continue using deterministic `e2e-` prefixes with unique suffixes per suite/worker.

## E2E Flows To Update

At minimum update:

### 1. Resources Sheet Flow

- API-create a test resource
- verify it can be found/opened in UI
- archive it in cleanup
- verify cleanup path succeeds without leaving the resource in normal list behavior

### 2. Topology Flow

- API-create root/related resources and relation as needed
- clean created relation explicitly
- archive created resources in cleanup
- keep topology assertions focused on visible behavior

### 3. Login Flow

- keep successful and invalid login paths on the real backend
- do not reintroduce route stubs

## Required Assertions

Add or update tests so the phase proves these facts:

- archive cleanup helper sends `POST /resources/{id}/archive`
- cleanup uses a stable archive reason such as `e2e cleanup`
- relation cleanup still sends `DELETE /resource-relations/{id}`
- archived `e2e-` resources do not have to be counted in default list expectations
- resource/topology E2E still pass after switching cleanup strategy

If useful, add focused unit tests for helper behavior. Do not overbuild.

## Tooling Isolation Requirements

Verify and preserve:

- `tsconfig.json` excludes `.worktrees`
- `vitest.config.ts` excludes `.worktrees/**`
- `eslint.config.mjs` ignores `.worktrees/**`
- Playwright test discovery stays local to `e2e`
- `next.config.ts` keeps Turbopack root fixed to the frontend project directory

If any relevant tool still scans `.worktrees/**`, fix it in this phase.

## Verification

You must run inside the worktree:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

After the branch is ready, also verify from the main checkout if practical:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

If main-checkout verification is not possible from your session, say so explicitly.

## Manual / Live Verification

Run against a live backend that already includes Phase 12.1.

Confirm at minimum:

- login E2E still works against the real backend
- resources E2E passes with archive cleanup
- topology E2E passes with archive cleanup
- archived `e2e-` resources do not continue polluting default `/resources` flows after cleanup

Do not rely on exact total row counts. Use prefix-based or directly created resource checks instead.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only. Do not stage `.next`, `test-results`, `.worktrees`, logs, screenshots, or temporary files.

## Final Report

Your final report must include:

- worktree path and branch
- commit hash
- changed files
- helper changes
- whether resource cleanup now uses archive
- whether any decommission-only fallback remains, and exactly why
- whether login E2E still uses real backend auth
- relation cleanup behavior
- `.worktrees/**` tooling isolation confirmation
- verification command results
- live backend verification result
- confirmation that `.worktrees`, `.next`, and `test-results` were not committed
- negative scope confirmation:
  - did not add product UI features
  - did not add archived-resource UI
  - did not add frontend-only mutation mocks
  - did not add Prism/WireMock/Pact/k6
  - did not change backend APIs
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not tag, push, release, or add AI co-author
- next phase input:
  - whether any E2E cleanup gaps remain
  - whether backend archive semantics caused any frontend edge cases
  - whether lingering Playwright env-noise still needs a tiny cleanup phase

## Constraints

- use a dedicated worktree under `/Users/fan/JsProjects/ControlHub/.worktrees`
- use TDD for changed helpers and affected E2E behavior
- do not reset the repo
- do not discard unrelated work
- do not add product features
- do not add mock servers in this phase
- do not rely on exact seed row counts
- do not let any tool scan `.worktrees/**` from the main checkout
