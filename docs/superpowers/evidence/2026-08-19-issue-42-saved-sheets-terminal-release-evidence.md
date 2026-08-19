# Issue #42 — Saved Sheets Terminal List Generations Release Evidence

## Summary

Issue #42 `38X-4B: Settle every Saved Sheets list generation` has been rebased
onto the latest published frontend main, re-verified, and merged. The original
product commit `201b41528d6e0a78c3dadf21888a279dc1da5020` was cherry-picked
onto `96fe311c22b33f27c8d45e5aa197c0524db92201` (which includes #44 schema
isolation and #46 query-history select sync), with one manual merge conflict
resolution in `tests/components/README.md`.

## Refs

| Item | Value |
|------|-------|
| Frontend base (origin/main) | `96fe311c22b33f27c8d45e5aa197c0524db92201` |
| Original product commit | `201b41528d6e0a78c3dadf21888a279dc1da5020` (parent `d6bc752`) |
| New candidate / merged SHA | `cae99cae21b7c8fb278c928a864d40178b7bb6d5` |
| Frontend main (post-merge) | `cae99cae21b7c8fb278c928a864d40178b7bb6d5` |
| Backend evidence base | `b635530d3efba6c52c8a99aae3a92409ed02e1fd` |
| Candidate branch | `issue-42-saved-sheets-terminal-final-20260819` |
| Candidate worktree | `/tmp/controlhub-issue-42-final-20260819` |

## Blocker Resolution

- Issue #41 (`38X-4A: Publish the Query Workbench terminal-state contract`) is
  **CLOSED** since 2026-08-18T15:22:59Z. Backend evidence `cc64599`. CI run
  `32153458480` both required jobs green.
- Issue #46 (`[Frontend baseline] Query History filter test races async status
  option`) is **CLOSED**. Frontend published at `96fe311`. Frontend CI
  `32254300836` both jobs green. Backend evidence `b635530d`. Backend CI
  `32256744920` both jobs green.
- Both blockers were published and closed before this evidence was created.
  A factual correction comment was posted on #42 at
  `#issuecomment-5343248148` updating the outdated "Blocked by #41" text.

## Cherry-Pick Conflict and Intent Merge

The cherry-pick had one conflict in `tests/components/README.md`:

- **HEAD (96fe311)** had `query-workbench.test.tsx` described as
  "full workbench integration with synchronized asynchronous select
  interactions" (from #46).
- **Incoming (#42)** wanted to change `query-saved-statements.test.tsx`
  description to "terminal list generations" and left the query-workbench
  line as plain "full workbench integration".
- **Resolution**: kept #46's updated query-workbench description AND applied
  #42's terminal list generations label for saved-statements. No other
  files conflicted. The other 5 files (query-saved-statements.tsx,
  README.md, en.json, zh-CN.json, saved-statements test) applied cleanly.

## Changed Files

| File | Change |
|------|--------|
| `components/query/README.md` | Updated saved-statements description to "terminal list generations" |
| `components/query/query-saved-statements.tsx` | Terminal generation state machine, target-scoped cleanup, ABA protection, retry/forbidden/not_found error types, disabled mutations during loading, role="alert"/"status"/aria-live |
| `messages/en.json` | Added `retry`, `error.forbidden`, `error.not_found`, `error.retryable` |
| `messages/zh-CN.json` | Added matching zh-CN translations |
| `tests/components/README.md` | Updated saved-statements description; preserved #46 query-workbench line |
| `tests/components/query-saved-statements.test.tsx` | Added deferred-promise helper, 15 new tests covering generation, 403/404/retry, same-target hide-old-rows, target reset, ABA, stale response |

## Component RED → GREEN Test Matrix

### query-saved-statements.test.tsx — 33/33 (3 consecutive greens)

Tests covering #42 terminal generation behavior:

| Test | Covers |
|------|--------|
| settles a 403 response as a non-retryable controlled error | AC #2 |
| settles a 404 response as a non-retryable controlled error | AC #3 |
| settles a transient failure with an accessible Retry action | AC #4 |
| retains same-target rows during refresh but disables mutations and hides them on failure | AC #5, #6 |
| resets target-scoped search, rows, and dialogs when the target changes | AC #7 |
| ignores a mutation completion after an A-B-A target transition | AC #7 (stale response) |
| ignores a late response from the previous target generation | AC #7 (stale response) |

### query-workbench.test.tsx — 181/181

No regression on #46 async status select fix. The test file includes
`query-workbench.test.tsx:3866` status select interaction tests that
previously failed synchronously and were fixed by #46.

### query-editor-shell.test.tsx — 81/81

No regression on #44 schema metadata identity isolation. All deferred-promise
tests for target-scoped database list, identity clearing, and mid-flight
database selection pass.

### Full unit — 98/98 files, 1508 tests (3 consecutive greens)

| Run | Files | Tests | Duration |
|-----|-------|-------|----------|
| 1 | 98/98 | 1508/1508 | 134s |
| 2 | 98/98 | 1508/1508 | 135s |
| 3 | 98/98 | 1508/1508 | 135s |

## Local Release Gates

| Gate | Result |
|------|--------|
| `git diff --check` | clean |
| `tsc --noEmit` | 0 errors |
| `eslint` | 0 errors, 6 warnings (identical to base `96fe311`) |
| `vitest run` | 98/98 files, 1508/1508 tests |
| `next build` | success (4.5s) |
| `check:e2e-preflight` | pass |
| `check:e2e-governance` | pass (14 spec files) |
| three-level docs | L3 headers present in both changed source files; L2 READMEs updated in same commit |
| `release:local` | green |

## Real Chromium Environment

| Item | Value |
|------|-------|
| Chromium | Playwright bundled Chromium (headless) |
| Backend | `go run ./cmd/server` at `localhost:8080` |
| Backend SHA | `6e9f688770236df131f2d2288ee2eea93e19dda9` |
| MySQL | Homebrew mysqld at `127.0.0.1:3306` |
| Disposable metadata DB | `controlhub_42_e2e` (migrated to v17) |
| Query fixture DB | Docker `mysql:8.0` at `127.0.0.1:13306`, database `query_e2e` |
| Fixture operators | `e2e-admin-issue42@controlhub-e2e.invalid` (admin), `e2e-editor-issue42@controlhub-e2e.invalid` (editor) |
| BFF proxy | `e2e/api-proxy.mjs` at `localhost:8081` |
| Frontend dev server | `e2e/harness/dev-server-wrapper.sh` at `localhost:3100` |

### Chromium Totals

| Scope | Passed | Failed | Skipped | Duration |
|-------|--------|--------|---------|----------|
| Saved Sheets only (pre-verification) | 19 | 0 | 0 | 1.1m |
| Full `release:e2e` (smoke + interaction + all) | 179 | 0 | 0 | 4.9m |

### Scenarios covered

- Desktop EN: saved statement create, list, load, delete flow
- 375px mobile EN: saved statements create dialog opens
- Desktop zh-CN: saved statements panel shows localized translations
- Desktop EN / 375px EN / Desktop zh-CN: parameterized template loads typed form
- Desktop EN: loading saved statement resets paging state
- Template execution uses saved-statement execute route
- Shared template affordance: authorized manager (Load/Edit/Delete)
- Shared template: 375px mobile, zh-CN, non-manager, non-admin editor
- Security: no owner/author/value leakage in rows
- Template value disposal on refresh and re-login
- Shared statement list pagination

## Candidate CI

| CI Run | URL | headSha | release-local | release-e2e |
|--------|-----|---------|---------------|-------------|
| Candidate PR (#1) | [32273413388](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32273413388) | `cae99cae21b7c8fb278c928a864d40178b7bb6d5` | SUCCESS (5m55s) | SUCCESS (23m26s) |
| Main push | [32275788979](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32275788979) | `cae99cae21b7c8fb278c928a864d40178b7bb6d5` | SUCCESS | SUCCESS |

## Standards / Spec Verdict

### Standards

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | — |
| P2 | 0 | — |
| P3 | 3 | (1) 6 lint warnings match base exactly, no new warnings; (2) L3 headers present and correct; (3) L2 READMEs updated in same commit |

### Spec (Acceptance Criteria)

| AC | Status | Evidence |
|----|--------|----------|
| 1. Success/empty settle to terminal states | ✅ | `shows loading state`, `shows empty state`, `shows saved statements` tests |
| 2. 403 → non-retryable | ✅ | `settles a 403 response as a non-retryable controlled error` |
| 3. 404 → non-retryable | ✅ | `settles a 404 response as a non-retryable controlled error` |
| 4. Network/5xx → retryable | ✅ | `settles a transient failure with an accessible Retry action` |
| 5. Same-target loading retains rows, mutations disabled, Search usable | ✅ | `retains same-target rows during refresh but disables mutations and hides them on failure` |
| 6. Failed refresh hides prior rows | ✅ | Same test: `refresh.reject(...)` → `expect(screen.queryByText("Current row")).not.toBeInTheDocument()` |
| 7. Target switch resets all state, stale ignored | ✅ | `resets target-scoped search, rows, and dialogs when the target changes` + ABA + stale tests |
| 8. Deferred-promise component tests | ✅ | `deferred<T>()` helper used in 5 tests |
| 9. EN, zh-CN, accessibility | ✅ | Both locale files updated; `role="alert"`, `role="status"`, `aria-live="polite"`, `aria-busy` present |
| 10. No backend/auth/persistence/toast change | ✅ | Changed files limited to components/query, messages, tests/components; no backend files touched |

| Severity | Count |
|----------|-------|
| P1 | 0 |
| P2 | 0 |

## Root WIP Preservation

### Frontend root (`/Users/fan/JsProjects/ControlHub`)

- `AGENTS.md`: modified (gitnexus block removed — user WIP)
- `CLAUDE.md`: modified (gitnexus block removed — user WIP)
- 4 untracked files preserved (bak files, screenshot PNGs)

### Backend root (`/Users/fan/GolangProjects/ControlHub`)

- `CLAUDE.md`: modified (gitnexus replaced with agent skills — user WIP)
- `advisor-plans/README.md`: modified (reconciliation notes — user WIP)
- 7 untracked files preserved (bak files, docs/agents, docs/decisions, docs/superpowers specs/plans)

Neither root was stash, reset, restored, cleaned, checked-out, or overwritten
during this task.

## Cleanup

- `/tmp/controlhub-issue-42-final-20260819` — candidate worktree (will be removed)
- `/tmp/controlhub-evidence-42-20260819` — evidence worktree (will be removed after push)
- `/tmp/e2e42_backend.log`, `/tmp/e2e42_backend.pid` — local E2E backend artifacts (will be removed)
- `/tmp/e2e42_admin_email`, `/tmp/e2e42_admin_pw`, `/tmp/e2e42_editor_email`, `/tmp/e2e42_editor_pw` — fixture credentials (will be removed)
- `controlhub_42_e2e` database — disposable E2E metadata DB (will be dropped)
- Old #42 worktree (`/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-42-20260818`) — **preserved**
- Old #42 branch (`issue-42-saved-sheets-terminal-20260818`) — **preserved**
- Old #42 commit (`201b41528d6e0a78c3dadf21888a279dc1da5020`) — **preserved**
- All other worktrees, branches, and user WIP — **preserved**

## Issue Status

| Issue | State | Notes |
|-------|-------|-------|
| #42 | OPEN → closing via this evidence | This evidence is the release record |
| #43 | OPEN | Unblocked by #42 closure (blocker satisfied) |
| #45 | OPEN | Independent verification, unaffected |
| #10 | OPEN | Parent epic, unaffected |
