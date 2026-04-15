# Frontend Phase 13.6: E2E Login And Harness Cleanup

You are implementing the frontend E2E harness cleanup phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-04-14-agent-friendly-integration-testing-roadmap.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-5-playwright-api-e2e-hardening-worker.md`
- `/Users/fan/JsProjects/ControlHub/playwright.config.ts`
- `/Users/fan/JsProjects/ControlHub/e2e`
- `/Users/fan/JsProjects/ControlHub/tsconfig.json`
- `/Users/fan/JsProjects/ControlHub/vitest.config.ts`
- `/Users/fan/JsProjects/ControlHub/eslint.config.mjs`
- `/Users/fan/JsProjects/ControlHub/next.config.ts`

## Goal

Frontend Phase 13.5 hardened Playwright E2E with API setup/cleanup helpers. The remaining harness issues are:

- login E2E still uses a mocked login route in at least one path
- test-created resources remain because backend has no hard-delete resource endpoint
- local toolchain must continue respecting `.worktrees/**` isolation
- E2E output has noisy environment warnings

This phase removes avoidable E2E mocks and tightens the test harness. It is not a product UI phase.

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
- base includes frontend Phase 13.5 merge and tooling isolation fixes
- worktree is clean

Stop and report if the worktree path, branch, or base is wrong.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Replace mocked login E2E behavior with real backend login through the existing E2E API proxy/server setup.
- Keep Playwright E2E focused on visible behavior and backend contract requests.
- Do not add Prism, WireMock, Pact, k6, or frontend-only API mocks in this phase.
- Do not add product UI features.
- Do not add backend APIs.
- Preserve `.worktrees/**` excludes for TypeScript, Vitest, ESLint, Playwright, Next.js, and build/test globs.
- Use project-local worktree path under `/Users/fan/JsProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. remove login E2E route mocking where practical
2. make login E2E use real `POST /auth/login`
3. keep invalid-login coverage meaningful without hiding backend behavior
4. improve E2E test-data hygiene for resources that cannot be hard-deleted
5. reduce avoidable `NO_COLOR` / `FORCE_COLOR` warning noise if it can be done without risk
6. ensure `.worktrees/**` remains excluded from all relevant frontend tooling

Do not change product UI unless a test reveals a real bug. If a UI bug is found, add or update the failing test first and fix the smallest code path.

## Login E2E Requirements

Update `e2e/login.spec.ts` and helpers as needed.

The login flow must:

- submit seeded real credentials through the UI
- call real backend `POST /auth/login` through the same base URL/proxy setup as other E2E tests
- assert redirect to `/overview`
- assert the app shell renders after login
- assert invalid credentials are rejected

Do not use route stubs for successful login.

For invalid login:

- prefer real backend invalid credential response
- if a controlled stub remains necessary, document exactly why and keep it limited to the invalid path only

## Test Data Hygiene Requirements

Backend currently has no `DELETE /resources/{id}` endpoint. Do not invent frontend cleanup for resources.

Improve hygiene with existing backend capabilities only:

- use deterministic `e2e-` name prefix
- use unique suffixes per test/worker
- if possible, patch test-created resources to harmless final state after test completion:
  - `lifecycleStatus`: `decommissioning` or another existing safe value
  - `healthStatus`: `unknown`
  - labels include a cleanup marker such as `{"test": "e2e", "cleanup": "manual"}`
- always delete test-created relations with `DELETE /resource-relations/{id}`

Do not depend on exact seed row counts.

## Tooling Isolation Requirements

Verify and preserve:

- `tsconfig.json` excludes `.worktrees`
- `vitest.config.ts` excludes `.worktrees/**`
- `eslint.config.mjs` ignores `.worktrees/**`
- Playwright does not use `.worktrees` as source input
- Next/Turbopack root is fixed to the project directory, not a parent directory

If any relevant tool still scans `.worktrees/**`, fix it in this phase.

## E2E Noise Cleanup

The Playwright output currently shows repeated warnings like:

```text
Warning: The 'NO_COLOR' env is ignored due to the 'FORCE_COLOR' env being set.
```

If this can be reduced by adjusting Playwright webServer environment variables safely, do it.

Rules:

- do not hide test failures
- do not redirect all output to `/dev/null`
- do not make debugging harder
- if the warning remains, document why

## Required Tests / Checks

At minimum cover:

- successful login uses real backend auth
- invalid login still fails visibly
- resources/topology E2E still pass with API setup
- `.worktrees/**` is not scanned by TypeScript/Vitest/ESLint from the main checkout

If useful, add a small unit or script-level test for helper behavior. Do not overbuild.

## Verification

You must run inside the worktree:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

After the branch is ready, also verify from the main checkout if possible:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

If main-checkout verification is not possible from your session, say so explicitly.

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
- login E2E changes
- whether successful login uses real backend auth
- whether invalid login uses real backend auth or a limited stub, with reason
- test data hygiene strategy
- `.worktrees/**` tooling isolation confirmation
- whether `NO_COLOR` / `FORCE_COLOR` noise was reduced
- verification command results
- live backend verification result
- confirmation that `.worktrees`, `.next`, and `test-results` were not committed
- negative scope confirmation:
  - did not add product UI features
  - did not add frontend-only mutation mocks
  - did not add Prism/WireMock/Pact/k6
  - did not change backend APIs
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not tag, push, release, or add AI co-author
- next phase input:
  - remaining E2E brittleness
  - whether backend resource cleanup API should be prioritized
  - whether frontend is ready for backend Schemathesis results

## Constraints

- use a dedicated worktree under `/Users/fan/JsProjects/ControlHub/.worktrees`
- use TDD for changed helpers and affected E2E behavior
- do not reset the repo
- do not discard unrelated work
- do not add product features
- do not add mock servers in this phase
- do not rely on exact seed row counts
- do not let any tool scan `.worktrees/**` from the main checkout
