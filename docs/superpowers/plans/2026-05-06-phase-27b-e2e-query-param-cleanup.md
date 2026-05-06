# Phase 27B E2E Query Param Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> superpowers:systematic-debugging first, then superpowers:executing-plans or
> superpowers:test-driven-development. This phase starts with evidence, not
> speculative fixes.

**Goal:** Fix the three pre-existing `e2e/list-pagination.spec.ts` query-param
failures so targeted and full E2E are green after Phase 27.

**Architecture:** Frontend-only E2E cleanup. The implementation may update E2E
helpers/tests or frontend list navigation behavior, but only after proving which
side is wrong.

**Tech Stack:** Next.js App Router, Playwright, existing E2E API proxy on
`localhost:8081`, Vitest, TypeScript.

---

## Required Documents

Read first:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27b-e2e-query-param-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27-database-operational-triage.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-06-phase-27-database-operational-triage.md
```

## Worktree

Use a dedicated frontend worktree:

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

If Phase 27 is not merged yet, stop and report that Phase 27B is blocked until
Phase 27 is on frontend `main`. Do not branch 27B from the Phase 27 feature
worktree unless explicitly instructed.

## Constraints

- No backend changes.
- No API contract changes.
- No deleting or skipping failing tests.
- No timeout-only fix.
- No broad output suppression.
- No `evaluate()` input bypass.
- No database operational triage behavior changes unless needed to keep tests
  compiling after Phase 27.
- No tag, push, release, or merge.
- No AI co-author.

---

## Task 1: Reproduce And Capture Evidence

**Files to inspect:**

```text
e2e/list-pagination.spec.ts
e2e/api-proxy.mjs
playwright.config.ts
services/resources.ts
services/audits.ts
app/(console)/resources/page.tsx
app/(console)/audits/page.tsx
```

- [ ] Start from a clean Phase 27B worktree.
- [ ] Ensure backend is running and healthy on `localhost:8080`.
- [ ] Run the targeted failing spec:

```bash
npm run test:e2e -- e2e/list-pagination.spec.ts
```

- [ ] For each failing assertion, capture:

```text
test name
user action
browser URL after action
browser network requests after action
recorded requests from localhost:8081 after action
actual frontend code path that updates URL
actual frontend code path that fetches data
```

- [ ] Produce a root-cause table with one classification per assertion:

```text
product_bug
obsolete_test_expectation
recorded_request_harness_gap
selector_or_action_bug
data_setup_gap
```

Do not modify source before this table is complete.

## Task 2: Improve Failure Diagnostics

**Likely file:**

```text
e2e/list-pagination.spec.ts
```

- [ ] Replace opaque `expect.poll(...).toBe(true)` request assertions with a
  helper that emits useful failure context.
- [ ] On timeout, include:

```text
expected path
expected key/value or expected key
recorded request count
recorded request search strings
recorded request searchParams
current browser URL where practical
```

- [ ] Keep helper local to this spec unless another E2E file already has a
  shared request-recording helper.
- [ ] Do not increase timeout as the primary fix.

Run:

```bash
npm run test:e2e -- e2e/list-pagination.spec.ts
```

Expected: failures, if still present, are now diagnostic.

Commit if this is a standalone improvement:

```bash
git add e2e/list-pagination.spec.ts
git commit -m "test: improve list pagination request diagnostics"
```

## Task 3: Fix Resource List Query Assertions Or Behavior

**Failing coverage:**

```text
resources pagination sends page and pageSize query params
resources search and filters reset to page 1 and stay in query params
```

- [ ] Decide per interaction whether backend request is intended.
- [ ] If intended, fix frontend navigation/fetch behavior so a real request is
  made with the expected params.
- [ ] If not intended, change the E2E assertion to match the actual product
  contract and document why URL-only assertion is correct.
- [ ] Keep assertions realistic:

```text
real clicks
real fills
real dropdown selections
no evaluate input bypass
no skipped tests
```

- [ ] Preserve existing assertions that URL params reset `page` to `1`.
- [ ] Preserve checks that legacy params such as `type` do not appear.

Run:

```bash
npm run test:e2e -- e2e/list-pagination.spec.ts --grep "resources"
```

Expected: resource list pagination/search/filter tests pass.

Commit:

```bash
git add <changed-files>
git commit -m "fix: stabilize resources query-param e2e coverage"
```

## Task 4: Fix Audit List Query Assertions Or Behavior

**Failing coverage:**

```text
audits pagination and filters send page, eventType, and result query params
```

- [ ] Repeat the same evidence-based decision for `/audits`.
- [ ] If backend request is intended, fix frontend navigation/fetch behavior.
- [ ] If URL-only behavior is the product contract, update the E2E assertion
  and explain why recorded backend request is obsolete.
- [ ] Keep filter controls realistic and selector-stable.

Run:

```bash
npm run test:e2e -- e2e/list-pagination.spec.ts --grep "audits"
```

Expected: audit list pagination/filter tests pass.

Commit:

```bash
git add <changed-files>
git commit -m "fix: stabilize audits query-param e2e coverage"
```

## Task 5: Targeted And Full Verification

Run:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e -- e2e/list-pagination.spec.ts
npm run test:e2e
```

Expected:

```text
all pass
```

If full E2E has a different failure:

- [ ] Re-run the failing spec in isolation.
- [ ] Compare against current frontend `main`.
- [ ] Do not label it pre-existing without identical main evidence.

## Task 6: Live Browser Sanity Check

With frontend and backend running, verify:

```text
/resources?page=1&pageSize=1
/resources?page=1&pageSize=20
/audits?page=1&pageSize=1
```

Check:

```text
pagination controls remain interactive
page/pageSize URL params update
resource search updates expected URL state
resource filters update expected URL state
audit eventType/result filters update expected URL state
no console errors
no 4xx/5xx network errors
```

## Final Report Requirements

Include:

```text
worktree / branch / commits
root-cause table for all three original failures
which assertions changed and why
whether any product behavior changed
targeted list-pagination result
full E2E result
standard verification matrix
live browser evidence
scope confirmation
clean git status
```

Explicitly confirm:

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
