# Frontend Phase 27B Worker Prompt — E2E Query Param Cleanup

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 27B — E2E Query Param Cleanup**

This is a frontend-only E2E debt cleanup after Phase 27. Phase 27 surfaced three
`list-pagination` E2E failures that are already present on frontend `main`.
They must now be fixed instead of carried forward as known failures.

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27b-e2e-query-param-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-06-phase-27b-e2e-query-param-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27-database-operational-triage.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-06-phase-27-database-operational-triage.md
```

## Mandatory Worktree Requirement

Do **not** develop directly on frontend `main`.

Create and use this dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-27b-e2e-query-param-cleanup
```

Branch:

```text
feat/phase-27b-e2e-query-param-cleanup
```

Base:

```text
frontend main after Phase 27 is merged
```

If Phase 27 is not merged to frontend `main` yet, stop and report:

```text
BLOCKED — Phase 27 must be merged before Phase 27B starts.
```

Do not branch 27B from the Phase 27 feature worktree unless explicitly
instructed.

Before editing, report:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git log --oneline -8
git worktree list
```

Create the worktree:

```bash
git worktree add .worktrees/frontend-phase-27b-e2e-query-param-cleanup -b feat/phase-27b-e2e-query-param-cleanup main
cd .worktrees/frontend-phase-27b-e2e-query-param-cleanup
git status --short --branch
git log --oneline -8
```

If the worktree already exists, verify it is clean and on the correct branch.
Do not overwrite user changes.

## Known Failures To Fix

These three tests failed identically on Phase 27 and frontend `main`:

```text
[chromium] › e2e/list-pagination.spec.ts:93:7
List pagination and backend query params › resources pagination sends page and pageSize query params

[chromium] › e2e/list-pagination.spec.ts:119:7
List pagination and backend query params › resources search and filters reset to page 1 and stay in query params

[chromium] › e2e/list-pagination.spec.ts:176:7
List pagination and backend query params › audits pagination and filters send page, eventType, and result query params
```

All fail at:

```text
e2e/list-pagination.spec.ts:44
expectRequestParam() waiting for recorded backend query params
```

The previous evidence:

```text
git diff main -- e2e/list-pagination.spec.ts
```

was empty on Phase 27, so Phase 27 did not cause the failures. Phase 27B must
now fix the failures.

## Core Rule

Do not guess.

Before applying a fix, classify each failed assertion with evidence:

```text
product_bug
obsolete_test_expectation
recorded_request_harness_gap
selector_or_action_bug
data_setup_gap
```

For every original failure, collect:

```text
browser URL after action
browser network requests after action
localhost:8081 recorded requests after action
frontend code path that updates URL
frontend code path that fetches data
```

## Constraints

Do not:

- change backend code
- change API contracts
- delete or skip tests
- solve by only increasing timeout
- add broad output suppression
- use `evaluate()` to bypass user input
- modify Phase 27 database operational triage behavior unless required for test compatibility
- tag, push, release, or merge
- add `Co-Authored-By`

## Files To Inspect

Start with:

```text
e2e/list-pagination.spec.ts
e2e/api-proxy.mjs
playwright.config.ts
services/resources.ts
services/audits.ts
app/(console)/resources/page.tsx
app/(console)/audits/page.tsx
```

Follow existing code structure if names differ.

## Required Implementation Direction

### 1. Improve diagnostics

The current failure is opaque:

```text
Expected: true
Received: false
```

Update request assertion helpers so failures include:

```text
expected path
expected key/value
recorded request count
recorded request search strings
recorded request searchParams
current browser URL where practical
```

Do not make timeout longer as the fix.

### 2. Fix resources tests or behavior

For:

```text
resources pagination sends page and pageSize query params
resources search and filters reset to page 1 and stay in query params
```

Decide whether each interaction is intended to trigger backend fetches. If yes,
fix frontend navigation/fetch behavior. If no, update E2E to assert the actual
URL/client-side contract and explain why recorded backend request assertion was
obsolete.

Keep real user interactions:

```text
click
fill
keyboard
dropdown selection
```

No input bypass.

### 3. Fix audits tests or behavior

For:

```text
audits pagination and filters send page, eventType, and result query params
```

Apply the same evidence-based decision. If backend requests are intended, fix
the product path. If URL-only is intended, update assertions accordingly.

### 4. Preserve governance

Do not weaken E2E governance. Do not introduce success-path screenshots. Do not
silence stderr/stdout broadly.

## Required Commands

Run targeted first:

```bash
npm run test:e2e -- e2e/list-pagination.spec.ts
```

Then standard gates:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

Expected final result:

```text
all pass
```

If a different E2E failure appears, compare it against current frontend `main`
before calling it pre-existing.

## Commit Guidance

Use small commits, for example:

```text
test: improve list pagination request diagnostics
fix: stabilize resources query-param e2e coverage
fix: stabilize audits query-param e2e coverage
```

Do not include AI co-author trailers.

## Final Report Required Format

Report:

```text
Worktree / branch / commits
Root-cause table for the three original failures
Exact assertions changed and why
Whether product behavior changed
Targeted list-pagination result
Full E2E result
Standard verification matrix
Live browser evidence
Clean git status
Scope confirmation
```

Scope confirmation must include:

```text
No backend changes
No API contract changes
No skipped/deleted tests
No timeout-only fix
No input bypass
No broad output suppression
No tag/push/release
No AI co-author
```
