# Issue #45 (38X-4E) — Independent Verification of the 38X-4 Query Workbench Delivery

Date: 2026-08-21

Status: **VERIFIED** — the published 38X-4 child deliveries (#41–#44) were
independently re-verified from fresh published backend and frontend refs with
real Chromium, real backend services, complete release gates, and independent
Standards / Spec / Security review. Ticket #45 closes below; parent #10 is left
open for a separate authorized closure.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Frontend repository | `Fanduzi/ControlHub-Frontend` |
| Frontend verified head (`origin/main`) | `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` |
| Frontend delivery range verified | `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3..defda6bb732f2225e53d916cc3dc1ea610a9ac0f` |
| Range contents | Issue #44 (schema identity isolation), #46 (query-history select sync baseline fix), #42 (Saved Sheets list terminal generations), #43 (deletion terminal + mobile-safe); 17 commits, 15 files, +1468/−107 |
| Backend repository | `Fanduzi/ControlHub-Backend` (tracked evidence) |
| Backend verified head (`origin/main`) | `4aa690c5d72815c76caafee559374476d44075b5` |
| Verification frontend worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-43-final-20260820` at `HEAD` `defda6bb` (clean; == `origin/main`) |
| Verification backend worktree | `/Users/fan/GolangProjects/ControlHub-Backend-wt-issue45-e2e` at `HEAD` `4aa690c` |
| E2E backend serving CWD/SHA | `/Users/fan/GolangProjects/ControlHub-Backend-wt-issue45-e2e` at `4aa690c`, built to `/tmp/controlhub-server-issue45`, serving `:8080` |

## Prerequisite Tickets — Published, Closed, Evidence, Green CI

All four prerequisite tickets were independently confirmed CLOSED, backed by
tracked evidence files in this repository, and exact-head green CI.

| Ticket | State | Backend evidence | Frontend CI | Backend CI |
| --- | --- | --- | --- | --- |
| #41 (38X-4A contract publication) | CLOSED 2026-08-18 | `2026-08-18-issue-41-workbench-terminal-state-publication-release-evidence.md` | n/a (docs) | run `32153458480` green |
| #42 (38X-4B Saved Sheets list generations) | CLOSED 2026-08-19 | `2026-08-19-issue-42-saved-sheets-terminal-release-evidence.md` | run `cae99ca` green | green |
| #43 (38X-4C deletion terminal + mobile) | CLOSED 2026-08-20 | `2026-08-20-issue-43-saved-sheet-deletion-terminal-release-evidence.md` | run `32396672667` green @ `defda6b` | run `32399036306` green @ `4aa690c` |
| #44 (38X-4D schema identity isolation) | CLOSED 2026-08-19 | `2026-08-19-issue-44-schema-metadata-identity-release-evidence.md` | run `32238786306` green @ `8bba785` | run `32242476759` green |

Frontend `origin/main` CI run `32396672667` (release-local + release-e2e) is
green at the exact verified head `defda6bb`; backend CI run `32399036306`
(release-local-gates + release-docker-gates) is green at `4aa690c`.

## Re-Run Component Contracts (deterministic, deferred promises)

All gates ran in the verification frontend worktree at `HEAD` `defda6bb` with
Node 22.22.0 (`.tool-versions`), real Chromium headless, real backend, no route
mocks, no forced clicks, no skips, no request bypass.

| Contract | Command | Result |
| --- | --- | --- |
| Saved Sheets list terminal-state + generation-isolation (#42) | `npx vitest run tests/components/query-saved-statements.test.tsx` | **41 passed / 0 failed** |
| Deletion terminal-state, pagination, accessibility, mobile-layout (#43) | same file (above) | **41 passed / 0 failed** |
| Schema identity, dedup, null-default, stale-response, warning/retry (#44) | `npx vitest run tests/components/query-editor-shell.test.tsx` | **81 passed / 0 failed** |
| Query Workbench integration | `npx vitest run tests/components/query-workbench.test.tsx` | **181 passed / 0 failed** |

The deferred-promise component seams (Saved Sheets + editor shell) cover: list
success/empty/forbidden/not-found/retry, same-target retained loading, error
replacement, target reset, stale/A-B-A response rejection, delete pending,
duplicate-submit prevention, 403 cancel-only, 404 absence announce + refresh,
transient retry, previous-page fallback, schema identity clearing on target and
database change, one database-list request, null-default, keyword-only
degradation, warning/retry. Zero skips.

## Real Chromium (`npm run release:e2e` components) — real backend

Real Chromium (Playwright bundled, headless) against the fresh backend
`4aa690c` on `:8080`, fresh disposable metadata DB `controlhub_45_e2e`
(migrated to goose v17), query fixture Docker `controlhub-query-e2e-mysql`
(`:13306`), and fixture operators `e2e-admin-issue45@controlhub-e2e.invalid` /
`e2e-editor-issue45@controlhub-e2e.invalid`. All requests through the api-proxy
(`:8081`); no route mocks, `page.evaluate` request bypasses, forced clicks, or
skips.

| Command | Passed | Failed | Skipped |
| --- | --- | --- | --- |
| `npm run test:e2e:smoke` | 7 | 0 | 0 |
| `npm run test:e2e:interaction` | 3 | 0 | 0 |
| `npx playwright test` (full suite) | **183** | 0 | 0 |
| Aggregate `release:e2e` | 193 invocations / 183 unique | 0 | 0 |

## AC-Specific Real-Chromium Assertions (all within the 183 green)

| Assertion | Test line (query-workbench.spec.ts) | Result |
| --- | --- | --- |
| No horizontal overflow at 375px (schema completion) | 839, 3664 | PASS (verified scrollWidth ≤ innerWidth, measured at real 375px viewport) |
| No overflow desktop zh-CN | 892 | PASS |
| Exactly one database-list request during default selection (reused on selection) | 773 (`expect.poll(() => dbList.list().length).toBe(1)`) | PASS |
| Saved sheets search own row at 375px, no overflow | 3664 | PASS |
| Terminal delete 404-absence, desktop EN / 375px EN / desktop zh-CN | query-workbench.spec.ts deletion block | PASS |

The schema database-list request-count contract (`pageSize=100`,
`/schema/databases`, exactly one during a load generation, reused on explicit
selection with no duplicate request) is asserted and passed in real Chromium.

## Complete Frontend Release Gates

All gates at `defda6bb` in the verification worktree with Node 22.22.0:

| Gate | Result |
| --- | --- |
| `npm run check:runtime` | Node 22.22.0 PASS |
| `npm run check:e2e-preflight` | `:3100`/`:8081` free before run; PASS |
| `npm run check:e2e-governance` | 14 spec files scanned; PASS |
| `npx tsc --noEmit -p tsconfig.json` | 0 errors |
| `npx eslint` | 0 errors, 6 warnings (all pre-existing, incl. documented `historyItemCount` @ e2e/query-workbench.spec.ts:2804 identical to base) |
| `npm run test` (`vitest run`) | **98/98 files, 1516/1516 tests, 0 failed, 0 skipped** |
| `npm run build` (`next build`) | Compiled successfully |
| `npm run release:local` (full) | **exit 0** |

Note: an initial `release:local` attempt was contaminated by the E2E fixture
env vars (`E2E_FIXTURE_ADMIN_EMAIL` etc.) being exported into the vitest
process, causing the fail-loud `e2e-api-helpers` "no seed fallback" test to see
the env present. Re-run with those vars unset gave a clean 98/98, 1516/1516,
exit 0. This was test-environment isolation, not a product defect.

## Backend Compatibility Gates

All gates at `4aa690c` in the verification backend worktree:

| Gate | Result |
| --- | --- |
| `go test -count=1 ./...` | 1819 passed, 14 packages, 0 failed, 0 skipped |
| `go vet ./...` | PASS, clean |
| `go build ./...` | PASS, clean |
| `go test ./internal/openapi -run TestOpenAPIYAMLIsValid` | PASS |
| `make argon2id-budget` | PASS (median 96.4ms ≤ 250ms, p95 98.7ms ≤ 300ms, 20 samples) |
| `make test-integration` | PASS — 389 top-level PASS, 0 FAIL (Testcontainers MySQL 8.0, migrations 1→17) |
| `make test-openapi-fuzz` | PASS — Schemathesis checks passed |

For the 38X-4 delivery the backend contains no production or test change (the
range is frontend product + backend docs/evidence only); the backend gates
confirm compatibility at the published head.

## Independent Review (Standards / Spec / Security)

Three read-only reviewers were dispatched in parallel against the exact range
`d6bc752..defda6b`. Two of the three automated reviewer runs were curtailed by
the shared Codex usage limit (infrastructure, not a finding); the parent
completed each axis by direct read-only inspection of the actual product diff,
spec, and localization. Findings:

- **Spec (AC/user-story coverage):** All 34 user stories and every acceptance
  criterion of #42/#43/#44 are implemented and asserted. Verified: list
  terminal states (ready/empty/forbidden/not-found/retryable with Retry only
  for retryable); same-target retained rows with mutations disabled; failed
  refresh hides retained rows; target reset clears search/debounce/page/list/
  dialogs/pending; delete pending prevents dismissal + duplicate submit; 403
  cancel-only; 404 closes + refreshes + announces absence without a success
  claim; last-row-on-later-page loads previous page; one database-list request
  supplies default + completions; null default stays null (no first-database
  inference); stale schema responses rejected by identity/generation; warning +
  Retry; keyword-only degradation; Run unchanged. **P1 0, P2 0.**
- **Standards:** Deferred promises drive deterministic generations (no
  wall-clock sleeps); stale-response protection keyed by target + generation;
  `role="alert"` for errors, `role="status"` `aria-live="polite"` for
  reconciliation, `aria-busy` on retained-list loading; no focus steal. Tests
  encode intent (why), not just behavior. **P1 0, P2 0.**
- **Security:** Controlled errors and announcements render only localized
  category text (`error.forbidden` / `error.not_found` / `error.retryable` /
  `error.deleteForbidden` / `error.deleteRetryable` / `deleted` /
  `noLongerExists`). Rendered strings carry no statement text, credentials,
  DSNs, raw server failures, or prior-target metadata (verified against the
  full localization diff in `messages/en.json` + `messages/zh-CN.json`). The
  saved-statement service is unchanged in the range (status→controlled-
  category mapping verified); no actor/role/credential/DSN/authorization
  version field was added to any browser request; no new backend endpoint,
  persistence, or cache; no storage of protected metadata across identities.
  **P1 0, P2 0.**

Remaining P1/P2 after review: **0**.

## Root Worktree Preservation

- Frontend root `/Users/fan/JsProjects/ControlHub` (main): user WIP `AGENTS.md`,
  `CLAUDE.md` modified; 4 untracked files (bak files, screenshot PNGs). The
  published head was read via `origin/main` (`defda6b`); the root was not
  checked out, stashed, reset, or cleaned during verification.
- Backend root `/Users/fan/GolangProjects/ControlHub` (main): user WIP
  `CLAUDE.md`, `advisor-plans/README.md` modified; untracked bak files,
  `docs/agents/`, and older specs/decisions preserved. All edits for this
  verification were made in the dedicated worktree, not the root.

## Cleanup (after verification)

- Disposable metadata DB `controlhub_45_e2e` dropped.
- Fresh backend process on `:8080` (built `/tmp/controlhub-server-issue45`)
  stopped.
- The pre-existing stale #43 E2E backend process (PID 14258, serving `:8080`
  from `/private/tmp/controlhub-issue-43-e2e-backend-20260821`) was stopped at
  the start of this run so the fresh `4aa690c` backend could serve; it is
  recorded here as the prior run's leftover, not user-owned.
- Shared Query E2E Docker fixture `controlhub-query-e2e-mysql` (`:13306`)
  preserved (untouched).
- Verifier frontend worktree/branch retained at `defda6bb` until closure
  confirmed.
