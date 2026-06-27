# Phase 38B Query Credential Admin Experience Hardening — Frontend Evidence

Scope: frontend-only hardening of the Phase 38A credential metadata management UI.
No backend product code changed. No backend API changed. No migration.

Frontend repository: `/Users/fan/JsProjects/ControlHub`
Frontend branch: `feat/phase-38b-query-credential-admin-experience` (merged to `main`)
Frontend base: `main` @ `0974505`
Frontend new HEAD: `d6a40e9`
Merge type: fast-forward

## Frontend Commits

| Commit | Subject |
|--------|---------|
| `69324a8` | feat: derive query credential coverage model |
| `1555af1` | feat: add query credential operations overview |
| `e14801e` | feat: add bulk query credential metadata apply |
| `61362b6` | test: cover query credential operations experience |
| `d6a40e9` | fix: harden query credential bulk operations and labels |

## Changed Files Summary

| File | Change |
|------|--------|
| `components/settings/query-credential-settings.tsx` | +1515/-183 — operations table, coverage summary, bulk apply/remove, filtering/grouping, runtime status labels, invalid_ref card, cross-target warning |
| `lib/query-credential-operations.ts` | +406 new — pure derivation helpers for coverage model, operation rows, grouping, filtering |
| `lib/query-target-display.ts` | +27 — display helpers for target labels |
| `messages/en.json` | +110 — EN i18n labels for all credential states, operations UI |
| `messages/zh-CN.json` | +110 — zh-CN i18n labels for all credential states, operations UI |
| `e2e/query-credential-settings.spec.ts` | +91 — E2E spec updates for operations experience |
| `tests/components/query-credential-settings.test.tsx` | +845 — component tests for coverage, grouping, filtering, bulk ops |
| `tests/lib/query-credential-operations.test.ts` | +591 new — unit tests for pure derivation helpers |

Total: 8 files changed, 3512 insertions(+), 183 deletions(-)

## Pre-Merge Verification (feature worktree, HEAD `d6a40e9`)

| Gate | Result |
|------|--------|
| `git diff --check main...HEAD` | clean |
| `npm run check:e2e-preflight` | pass (`:3100` free, `:8081` free) |
| `npm run check:e2e-governance` | pass (13 spec files scanned) |
| `npx tsc --noEmit` | pass |
| `npm run lint` | pass |
| `npm run test` | pass (740 tests, 63 files) |
| `npm run build` | pass (Next.js 16.2.3, 13 routes) |

## Post-Merge Verification (main, HEAD `d6a40e9`)

| Gate | Result |
|------|--------|
| `git diff --check 0974505..HEAD` | clean |
| `npx tsc --noEmit` | pass |
| `npm run lint` | pass |
| `npm run test` | pass (740 tests, 63 files) |
| `npm run build` | pass (Next.js 16.2.3, 13 routes) |
| `npm run check:e2e-preflight` | pass |
| `npm run check:e2e-governance` | pass (13 spec files scanned) |

## Real E2E Evidence

Real E2E was run against a live backend with the Phase 37H dedicated query E2E MySQL fixture.

### E2E Environment

| Component | Value |
|-----------|-------|
| Backend | `http://localhost:8080` (live, `go run ./cmd/server/`) |
| Dedicated query DB | `controlhub-query-e2e-mysql`, host `127.0.0.1`, port `13306` |
| Credential ref | `LOCAL_QUERY_RO` |
| Ready target | id `616`, name `Local MySQL Query Dev`, engine `mysql`, host `127.0.0.1:13306`, safety `readonly_sandbox_enabled`, `availableActions.run = true` |
| Total targets | 34 |
| Ready targets | 1 |
| Backend env | `DATABASE_DSN` (metadata DB), `JWT_SECRET`, `QUERY_DEV_ALLOW_TARGET_FIXTURE=true`, `QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO`, `CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO` (dedicated query DB DSN from `.query-e2e-mysql.env`) |
| Commit tested | `d6a40e9` (same as merged main HEAD) |

### E2E Results

**query credential settings spec** (`e2e/query-credential-settings.spec.ts`): **13/13 passed**

| # | Test | Duration |
|---|------|----------|
| 1 | opens the settings query-credentials page as admin | 1.4s |
| 2 | shows coverage summary cards | 678ms |
| 3 | shows the operations table with credential data | 624ms |
| 4 | shows filter controls | 657ms |
| 5 | selecting a target in the table shows the credential detail panel | 716ms |
| 6 | credential detail panel shows runtime status and form fields | 754ms |
| 7 | credential detail panel never shows DSN or password fields | 721ms |
| 8 | DBA operating model guidance is visible | 763ms |
| 9 | all-environments policy shows confirmation checkbox | 814ms |
| 10 | Query Workbench shows credential status but no edit controls | 611ms |
| 11 | Query Workbench shows admin link for admin users | 568ms |
| 12 | search filters the operations table | 652ms |
| 13 | boundary note explains server-side credential storage | 702ms |

**query workbench spec** (`e2e/query-workbench.spec.ts`, `--grep query`): **5/5 passed**

| # | Test | Duration |
|---|------|----------|
| 1 | loads with real backend data and an execution-disabled banner | 1.8s |
| 2 | target switcher surfaces at least one database target | 557ms |
| 3 | a locked query target keeps Run disabled | 1.2s |
| 4 | switching the target updates the governance panel facts | 1.6s |
| 5 | a ready target runs a guarded SELECT and shows the result | 3.8s |
| 6 | an unsafe statement is rejected with a controlled validation message | 3.9s |
| 7 | query history shows the recent attempt after a run | 3.6s |

**All query-related E2E** (`--grep query`, includes list-pagination): **23/23 passed** (19.6s total)

No fake backend was used. No tests were skipped. The "ready target runs a guarded SELECT"
test confirms the real backend + dedicated query DB + credential resolver pipeline works
end-to-end.

## Frontend CI Evidence

| Field | Value |
|---|---|
| CI run ID | `28282054309` |
| CI URL | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/28282054309 |
| `release-local` | succeeded (3m31s) |
| `release-e2e` | skipped — no E2E runner in CI context (expected) |

## Push Details

| Field | Value |
|---|---|
| Pushed range | `0974505..d6a40e9` |
| Push target | `origin main` |
| Push method | SSH (port 22) |

## Admin / Settings Boundary

Credential metadata operations live in **settings/admin** (`/settings/query-credentials`),
not Query Workbench. The Query Workbench remains read-only for credential status and has
no edit controls. Admin users see the full operations surface (coverage summary, filtering,
grouping, bulk apply/remove). Non-admin users see a restricted view with "Contact
administrator" guidance.

## No-Secret Proof

- No DSN/password fields in any UI component or request body.
- Request body whitelist for PUT: `credentialRef`, `enabled`, `environmentPolicy`,
  optional `confirmAllEnvironments`. Strict decoding on backend rejects unknown fields.
- No `actorUserId` sent from frontend; derived from verified bearer token server-side.
- No DSN/password in browser state, localStorage, sessionStorage, or network requests.
- `selectedOperationTargets` is assembled from operation rows and cast as `QueryTarget`;
  current dialogs only use `resourceId`, `displayName`, `connectionContext` — no secret fields.

## Bulk Operation Behavior

- Bulk apply: sequential fan-out to `PUT /query-targets/{id}/credential` per selected target.
- Bulk remove: sequential fan-out to `DELETE /query-targets/{id}/credential` per selected target.
- Partial failure is visible per-target; no silent swallowing of errors.
- `all_environments` policy requires explicit confirmation checkbox.
- Unsupported/incomplete targets are not selectable for bulk operations.

## Known Non-Blocking Residual Risk

`selectedOperationTargets` is currently assembled from operation rows and cast as
`QueryTarget`. This is safe because bulk dialogs only access `resourceId`, `displayName`,
and `connectionContext`. If future dialogs need additional `QueryTarget` fields, the
approach should be changed to pass `CredentialOperationRow[]` or use `targetById` to
fetch complete objects.

## Cleanup Results

| Action | Result |
|--------|--------|
| Feature worktree removed | `.worktrees/frontend-phase-38b-query-credential-admin-experience` deleted |
| Merged branch deleted | `feat/phase-38b-query-credential-admin-experience` (was `d6a40e9`) |
| `git worktree prune` | done |
| Final worktree list | only `/Users/fan/JsProjects/ControlHub` @ `d6a40e9 [main]` |
| Final branch list | only `main` |
| Backend stopped | `:8080` released, PID killed |
| Dedicated query DB stopped | `make query-e2e-mysql-down`, container `controlhub-query-e2e-mysql` removed |
| `.query-e2e-mysql.env` | removed by script (gitignored, never committed) |

## Scope Confirmation

- No backend product code changed
- No backend API changed
- No migration
- No DSN/password in any UI, request, response, state, or log
- No `actorUserId` sent from frontend
- No Workbench edit controls added
- Real E2E run with live backend + dedicated query DB (no fake backend)
- No tag/release/deploy
- No AI co-author in any commit
