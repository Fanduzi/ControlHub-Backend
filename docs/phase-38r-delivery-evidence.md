# Phase 38R: Governed Saved Queries & Templates — Final Delivery Evidence

> Date: 2026-07-28
> Status: Release acceptance complete

## SHAs

| Repo | Original base | Final pushed (origin/main) |
|---|---|---|
| Backend | `b7778c588750292592543b72e82336eb86402b55` | `fc4e6ff2ebd4285ee8348181dab50659c8f18fd9` |
| Frontend | `3580eea58a3dbcd03a4e3c8c2892dc60361331b8` | `665e86c984bd21750e37de47ab773b622771df29` |

## Commits (backend, oldest first)

| SHA | Message |
|---|---|
| `e8073b0` | phase38r: add missing service authorization tests |
| `fc4e6ff` | phase38r: add admin/owner success tests for shared template and personal mutation |

## Commits (frontend, oldest first)

| SHA | Message |
|---|---|
| `20a847a` | phase38r: create dialog, responsive Dialog/Sheet, focus restoration, docs |
| `19eaff9` | phase38r: E2E tests, conditional Dialog/Sheet, zh-CN fix |
| `665e86c` | fix: initialize useIsDesktop state lazily to avoid set-state-in-effect lint error |

## Root/Worktree/Branch Status

- Backend root: clean (untracked `Check` file is unrelated)
- Frontend root: clean; `wip/query-runtime-fixes-2026-07-20` branch preserved
- HEAD == origin/main in both repos
- All worktrees and branches cleaned up

## Authorization Matrix Proof

| Scenario | Test | Result |
|---|---|---|
| Admin creates shared template | `TestQuerySavedStatementServiceCreate/creates_shared_template_for_admin` | PASS |
| Non-admin rejected from creating shared | `TestQuerySavedStatementServiceCreate/rejects_shared_template_for_non-admin` | PASS |
| Admin updates shared template | `TestQuerySavedStatementServiceUpdate/admin_successfully_updates_shared_template` | PASS |
| Non-admin rejected from updating shared | `TestQuerySavedStatementServiceUpdate/rejects_non-admin_updating_shared_template` | PASS |
| Admin deletes shared template | `TestQuerySavedStatementServiceDelete/admin_successfully_deletes_shared_template` | PASS |
| Non-admin rejected from deleting shared | `TestQuerySavedStatementServiceDelete/rejects_non-admin_deleting_shared_template` | PASS |
| Owner updates own personal | `TestQuerySavedStatementServiceUpdate/owner_successfully_updates_own_personal_statement` | PASS |
| Non-owner rejected from updating personal | `TestQuerySavedStatementServiceUpdate/rejects_non-owner_updating_another_user's_personal_statement` | PASS |
| Owner deletes own personal | `TestQuerySavedStatementServiceDelete/owner_successfully_deletes_own_personal_statement` | PASS |
| Non-owner rejected from deleting personal | `TestQuerySavedStatementServiceDelete/rejects_non-owner_deleting_another_user's_personal_statement` | PASS |

## Save/Load No-Execution Proof

- `GuardSavedStatement` returns trimmed text without LIMIT injection, executor, or `query_executions`
- `validateTargetExists` uses `ListQueryTargets` — no credential/DSN resolution
- No handler calls `executor.Query`, `disclosure.Preflight`, or `history.Record`
- Frontend `onStatementLoad` only calls `updateActiveWorksheet({ statement })`
- E2E test verifies load changes editor content without execute/explain/schema calls

## E2E Totals

| Context | Run | Tests | Passed | Failed | Skipped |
|---|---|---|---|---|---|
| Candidate | 1 | 3 | 3 | 0 | 0 |
| Candidate | 2 | 3 | 3 | 0 | 0 |
| Candidate | 3 | 3 | 3 | 0 | 0 |
| Full suite (candidate) | 1 | 64 | 64 | 0 | 0 |
| Merged root | 1 | 3 | 3 | 0 | 0 |
| Merged root | 2 | 3 | 3 | 0 | 0 |
| Merged root | 3 | 3 | 3 | 0 | 0 |

## CI Evidence

| Repo | Run ID | SHA | Job | Conclusion | URL |
|---|---|---|---|---|---|
| Backend | 30345571742 | `fc4e6ff` | release-local-gates | success | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/30345571742 |
| Frontend | 30345865872 | `665e86c` | release-local | success | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/30345865872 |

## Gate Results

### Backend
| Gate | Result | Notes |
|---|---|---|
| `git diff --check` | clean | |
| `gofmt -d` | clean | |
| `go vet ./...` | clean | |
| `go build ./...` | clean | |
| `go test -count=1 ./...` | 10 packages OK | 27 service tests (was 21) |
| `make openapi-validate` | PASS | |
| `make test-integration` | 7 pre-existing failures | Show/Describe/Explain tests from Phase 38Q; not caused by 38R |
| `make test-openapi-fuzz` | 4 pre-existing failures | Same Phase 38Q contract violations |

### Frontend
| Gate | Result | Notes |
|---|---|---|
| `git diff --check` | clean | |
| `npx tsc --noEmit` | clean | |
| `npm run lint` | 0 errors, 5 warnings (pre-existing) | |
| `npm run test` | 84 files, 1239 tests passed | 12 saved-statement tests (was 7) |
| `npm run build` | clean | |
| E2E (saved statements) | 3/3 × 3 runs | 0 failures, 0 skips |
| E2E (full suite) | 64/64 | 0 failures, 0 skips |

## Reviews

| Reviewer | Verdict | Artifact |
|---|---|---|
| Momus (design doc) | OKAY — no P1/P2 | `docs/superpowers/audits/2026-07-28-phase-38r-momus-review.md` |

## Documentation

- Spec: `docs/superpowers/specs/2026-07-28-phase-38r-governed-saved-queries-and-templates.md`
- Design: `docs/superpowers/plans/2026-07-28-phase-38r-governed-saved-queries-and-templates-design.md`

## Remaining Risks

**none P1/P2**

The 7 integration test failures and 4 OpenAPI fuzz failures are pre-existing from Phase 38Q (confirmed identical on base commit `b7778c5`). They are not caused by Phase 38R changes.
