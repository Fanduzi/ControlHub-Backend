# Phase 27B E2E Query Param Cleanup Design

## Background

Phase 27 adds database operational triage. During Phase 27 full E2E verification,
three tests in `e2e/list-pagination.spec.ts` failed. The same three failures
were reproduced on frontend `main` at:

```text
f6334ec fix: clarify database instance signals and cluster detail order
```

The failing tests are:

```text
e2e/list-pagination.spec.ts:93
resources pagination sends page and pageSize query params

e2e/list-pagination.spec.ts:119
resources search and filters reset to page 1 and stay in query params

e2e/list-pagination.spec.ts:176
audits pagination and filters send page, eventType, and result query params
```

All fail at the same helper:

```text
expectRequestParam() -> expect.poll() timed out after 10s
```

The browser URL updates, but the E2E recorded-request harness does not observe
the expected backend request query parameter.

Phase 27B exists because this is real E2E debt. It should not be ignored as
"pre-existing" indefinitely, and it should not be mixed into Phase 27's product
triage scope.

## Goal

Make `e2e/list-pagination.spec.ts` accurate, diagnostic, and green.

After Phase 27B:

1. The failing resource pagination/query-param tests pass.
2. The failing audits pagination/query-param tests pass.
3. Full E2E no longer carries these three known failures.
4. The test harness clearly distinguishes:
   - URL state assertions.
   - actual browser network requests.
   - API-proxy recorded backend requests.
5. If product behavior is wrong, the frontend behavior is fixed. If the test is
   obsolete, the test is corrected with evidence.

## Non-Goals

- Do not change backend code.
- Do not change API contracts.
- Do not delete `list-pagination.spec.ts`.
- Do not skip the failing tests.
- Do not solve the issue by increasing the timeout.
- Do not add broad output suppression.
- Do not use `evaluate()` as an input bypass.
- Do not change database operational triage behavior from Phase 27.
- Do not merge or clean up the Phase 27 branch as part of this phase.

## Current Test Model

`e2e/list-pagination.spec.ts` currently checks two things:

```text
URL params in the browser
backend query params via localhost:8081 recorded-request endpoints
```

Recorded-request helper endpoints:

```text
GET http://localhost:8081/__reset-recorded-requests?path=/resources
GET http://localhost:8081/__recorded-requests?path=/resources
GET http://localhost:8081/__reset-recorded-requests?path=/audit-events
GET http://localhost:8081/__recorded-requests?path=/audit-events
```

The failing helper:

```ts
requests.some((request) => request.searchParams[key] === value)
```

The failure does not by itself prove a product bug. It only proves that the
test harness did not record the expected request.

## Required Investigation

Before changing code, capture evidence for each failing assertion:

1. Browser URL after the user action.
2. Browser network requests seen by Playwright after the user action.
3. API-proxy recorded requests after the user action.
4. Current frontend code path that updates the URL.
5. Current frontend code path that fetches list data.

Classify each failed assertion as one of:

```text
product_bug
obsolete_test_expectation
recorded_request_harness_gap
selector_or_action_bug
data_setup_gap
```

Do not apply a fix before this classification is written down in the final
report.

## Likely Root Causes To Check

### URL-only navigation

Some list controls may update URL state with browser history APIs or client-side
state. If no server navigation or data fetch is intended, a backend request
assertion is obsolete.

### App Router navigation without proxy recording

The page may perform a Next.js App Router transition and fetch data server-side,
but the `localhost:8081` recorded-request harness may not observe the request
path or query as expected.

### Helper path mismatch

The test records:

```text
/resources
/audit-events
```

Actual requests may target a different path, include a prefix, or route through
`/__api`.

### Query shape mismatch

Multi-select params may be represented differently from the legacy test
assumption. The test must match the actual URL/API contract, not an old control
implementation.

### Debounced search expectation drift

If a search box is intentionally client-side, expecting a backend `q` request is
wrong. If a search box is intended to be server-side, lack of a backend request
is a product bug.

## Product Contract

The intended list behavior must be explicit after Phase 27B:

- Resource list pagination and page-size changes should be shareable through
  URL params.
- Audit list pagination and filters should be shareable through URL params.
- Backend request assertions should only exist for interactions that are
  intended to trigger backend data fetching.
- URL-only assertions are acceptable only when the list is intentionally
  client-side for that interaction.
- Tests must report useful diagnostics when recorded requests do not match.

## Required Test Harness Improvements

`expectRequestParam()` and related helpers should produce actionable timeout
messages. On failure, the output should include:

```text
expected path
expected key/value
recorded request count
recorded request path/search values
current browser URL if available
```

This prevents future "Expected true / Received false" failures without context.

## Acceptance Criteria

### Targeted E2E

```bash
npm run test:e2e -- e2e/list-pagination.spec.ts
```

Expected:

```text
all tests pass
```

### Full E2E

```bash
npm run test:e2e
```

Expected:

```text
full suite passes
```

If any non-27B failure remains, prove it also fails on current frontend `main`
and include exact comparison evidence. The target remains full green.

### Standard Gates

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Expected:

```text
all pass
```

## Scope Confirmation

Phase 27B is frontend E2E cleanup only unless investigation proves a frontend
product bug in list query-param behavior.

Do not change backend, API contract, SQL, topology, database operational signal
semantics, or product IA beyond the minimal fix required by evidence.
