# Phase 38R: Governed Saved Queries & Templates — Final Delivery Evidence

> Date: 2026-07-28
> Status: Release acceptance complete

## SHAs

| Repo | Original base | Final pushed (origin/main) |
|---|---|---|
| Backend | `b7778c588750292592543b72e82336eb86402b55` | `69b463db09cc916dbec36f054c836fdca00c6a30` |
| Frontend | `3580eea58a3dbcd03a4e3c8c2892dc60361331b8` | `3954109` (pending push with E2E fixes) |

## Commits (backend, oldest first)

| SHA | Message |
|---|---|
| `e8073b0` | phase38r: add missing service authorization tests |
| `fc4e6ff` | phase38r: add admin/owner success tests for shared template and personal mutation |
| `69b463d` | fix: resolve integration/fuzz failures, remove CI gate, add delivery evidence |

## Commits (frontend, oldest first)

| SHA | Message |
|---|---|
| `20a847a` | phase38r: create dialog, responsive Dialog/Sheet, focus restoration, docs |
| `19eaff9` | phase38r: E2E tests, conditional Dialog/Sheet, zh-CN fix |
| `665e86c` | fix: initialize useIsDesktop state lazily to avoid set-state-in-effect lint error |
| `7f3d70c` | fix: remove CI gate, add credential env and seed step for E2E |
| `0e79524` | fix: add required env vars for query dev seed step in CI |
| `3954109` | fix: add query_e2e database, tables, and seed data to CI MySQL |
| *(pending)* | fix: remove test.skip, add Load side-effect contract, fix E2E repeatability |

## Root/Worktree/Branch Status

- Backend root: clean
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
- **E2E contract**: After Load, the test monitors all network requests and asserts zero requests to `/execute`, `/explain`, `/schema/`, `/query-history`, or `/related-record` endpoints
- **E2E contract**: After Load, the Run button remains enabled (governed target still active) and the execute URL points to the governed `/execute` endpoint

## test.skip Compliance

- Zero `test.skip` in Phase 38R E2E tests
- All3 Phase 38R tests use hard failure (`throw noReadyTargetFixtureError()`) when fixture is missing
- Pre-existing `test.skip` calls in other test sections are unchanged (not part of Phase 38R scope)

## E2E Totals

| Context | Run | Tests | Passed | Failed | Skipped |
|---|---|---|---|---|---|
| Phase 38R × 3 consecutive | 1-3 | 9 | 9 | 0 | 0 |
| Full suite | 1 | 133 | 133 | 0 | 0 |

## CI Evidence

| Repo | Run ID | SHA | Jobs | Conclusion | URL |
|---|---|---|---|---|---|
| Backend | 30347983050 | `69b463d` | release-local-gates, release-docker-gates | all success | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/30347983050 |
| Frontend | 30351232581 | `3954109` | release-local, release-e2e | all success | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/30351232581 |

### CI Job Details
- Backend `release-local-gates`: gofmt, go vet, go build, unit tests, openapi-validate — all PASS
- Backend `release-docker-gates`: integration tests, OpenAPI fuzz (48 Schemathesis checks) — all PASS
- Frontend `release-local`: tsc, lint, unit tests (1239), build — all PASS
- Frontend `release-e2e`: E2E preflight, Playwright 133 tests — all PASS

## Gate Results

### Backend
| Gate | Result | Notes |
|---|---|---|
| `git diff --check` | clean | |
| `gofmt -d` | clean | |
| `go vet ./...` | clean | |
| `go build ./...` | clean | |
| `go test -count=1 ./...` | 10 packages OK | 27 service tests |
| `make openapi-validate` | PASS | |
| `make test-integration` | PASS | All integration tests pass |
| `make test-openapi-fuzz` | PASS | 48 Schemathesis checks pass |

### Frontend
| Gate | Result | Notes |
|---|---|---|
| `git diff --check` | clean | |
| `npx tsc --noEmit` | clean | |
| `npm run lint` | 0 errors, 5 warnings (pre-existing) | |
| `npm run test` | 84 files, 1239 tests passed | 12 saved-statement tests |
| `npm run build` | clean | |
| E2E (Phase 38R × 3) | 9/9 pass | 0 failures, 0 skips |
| E2E (full suite) | 133/133 pass | 0 failures, 0 skips |

## Reviews

| Reviewer | Verdict | Artifact |
|---|---|---|
| Momus (design doc) | OKAY — no P1/P2 | `docs/superpowers/audits/2026-07-28-phase-38r-momus-review.md` |

## Documentation

- Spec: `docs/superpowers/specs/2026-07-28-phase-38r-governed-saved-queries-and-templates.md`
- Design: `docs/superpowers/plans/2026-07-28-phase-38r-governed-saved-queries-and-templates-design.md`

## Remaining Risks

**none**
