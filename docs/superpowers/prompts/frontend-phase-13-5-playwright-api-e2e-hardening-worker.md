# Frontend Phase 13.5: Playwright API Setup And E2E Hardening

You are implementing the frontend E2E hardening phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-04-14-agent-friendly-integration-testing-roadmap.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-resource-topology-view-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-11-resource-topology-read-model-worker.md`
- `/Users/fan/JsProjects/ControlHub/playwright.config.ts`
- `/Users/fan/JsProjects/ControlHub/e2e`

## Goal

ControlHub frontend now has E2E coverage for login, lists, asset maintenance, and topology. Some E2E tests still depend on seed data and UI-driven setup. This phase hardens Playwright tests by using API setup/cleanup for mutable data and by making topology/resource flows less brittle.

This is a test-harness phase, not a product feature phase.

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
- base includes frontend Phase 13 topology view
- worktree is clean

Stop and report if the worktree path, branch, or base is wrong.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Use Playwright API requests for E2E data setup and cleanup.
- Keep UI tests focused on user-visible behavior.
- Do not rely on exact seed row counts.
- Do not leave uncontrolled test data in the daily local database.
- Do not add Prism, WireMock, Pact, k6, or frontend-only mocks in this phase.
- Use project-local worktree path under `/Users/fan/JsProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. add reusable Playwright API helpers for authenticated backend requests
2. create deterministic test resource and relation setup helpers
3. add cleanup helpers for test-created relations/resources where backend supports cleanup
4. refactor the most fragile E2E flows to use API setup
5. keep existing UI coverage meaningful

Do not change product UI unless a test reveals a real bug. If a UI bug is found, add/adjust the failing test first and fix the smallest code path.

## Required E2E Helper Behavior

Add or update helpers under `e2e/`.

Helpers should support:

- login/session setup
- `POST /resources` to create a test resource
- `PATCH /resources/{id}` where needed
- `POST /resources/{id}/relations` to create a test relation
- `DELETE /resource-relations/{id}` to clean relation data
- search/list helper only when needed

Resource test names must use a deterministic prefix:

```text
e2e-<suite>-<timestamp-or-worker-suffix>
```

Cleanup:

- Always clean relations created by tests.
- If backend does not support hard delete resources, do not invent cleanup. Instead:
  - use unique names
  - set lifecycle/status fields to harmless values if needed
  - document the remaining test data strategy

Do not use frontend-only mutation mocks as final E2E setup.

## E2E Flows To Harden

At minimum harden:

### 1. Asset Maintenance Flow

Use API setup where it makes the test more stable.

Assert:

- create/edit UI still works for user-visible behavior
- request payloads or resulting backend state are correct where practical
- relation add/delete behavior is stable

### 2. Topology Flow

Use API setup to create a small deterministic topology when practical:

- root resource
- related resource
- relation between them

Assert:

- topology graph renders nodes and edge
- depth/direction controls trigger expected backend requests or visible changes
- node click navigates or opens expected detail behavior

If backend cannot delete resources, use existing seeded resources for read-only topology and API-created relation only when cleanup is safe.

### 3. List/Pagination Flow

Avoid exact seed counts.

Assert:

- query params sent to backend are correct
- UI updates after pagination/filter/search
- no reliance on fixed total count except where test-created data controls it

## API Request Verification

Where useful, record or inspect network requests to assert:

- `page`
- `pageSize`
- `resourceType`
- `environmentId`
- `depth`
- `direction`
- `relationType`

Do not over-test implementation details. Focus on request params that enforce frontend/backend contract.

## Test Quality Rules

- Tests must be deterministic enough for repeated AI-agent runs.
- Tests must not depend on row ordering unless explicitly sorted or controlled.
- Tests must not require manual browser interaction.
- Tests should fail clearly when backend is unavailable.
- Avoid sleeps; use locators, network waits, or visible state.
- Keep test data isolated by prefix.

## Verification

You must run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

Also manually verify with a live backend:

- asset maintenance flow still works
- topology graph still renders
- E2E test data does not create uncontrolled clutter

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
- E2E helper changes
- which E2E flows now use API setup/cleanup
- test data naming and cleanup strategy
- verification command results
- live backend verification result
- confirmation that `.worktrees`, `.next`, and `test-results` were not committed
- negative scope confirmation:
  - did not add frontend-only mutation mocks
  - did not add Prism/WireMock/Pact/k6
  - did not change backend APIs
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not tag, push, release, or add AI co-author
- next phase input:
  - remaining E2E brittleness
  - backend cleanup API gaps, if any
  - whether frontend is ready for Schemathesis-backed backend contract fuzzing

## Constraints

- use a dedicated worktree under `/Users/fan/JsProjects/ControlHub/.worktrees`
- use TDD for changed helpers and affected components
- do not reset the repo
- do not discard unrelated work
- do not add product features
- do not add mock servers in this phase
- do not rely on exact seed row counts
