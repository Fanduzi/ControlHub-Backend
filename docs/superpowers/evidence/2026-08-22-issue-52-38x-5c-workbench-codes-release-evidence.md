# Issue #52 — 38X-5C Query Workbench Classifies by Controlled Error Code Release Evidence

Date: 2026-08-22

## Summary

Issue #52 `38X-5C: Query Workbench classifies by Controlled Error Code` is a
frontend product delivery. Query execute and Saved Sheets classify failures
only by `ApiError.code`. The execute-path 403 split between
`query_result_disclosure_blocked` and `query_not_allowed` no longer uses
`message.includes`. Retry follows the Controlled Error Code (missing code,
non-JSON, and transport failures become retryable `service_unavailable`).
Operator copy is localized; raw `message` and raw codes are not shown.
`details` still maps onto template parameter fields. 401 stays a Controlled
Authorization Error (login) and is not wrapped as a workbench feature error.
38X-4 Workbench Request Terminal States remain.

The product commit was already fast-forwarded to frontend `origin/main` on
2026-08-21. This evidence record is backend documentation only. Parent
`Fanduzi/ControlHub-Backend#11` stays open. Blockers #47 and #48 are closed.

## Refs

| Item | Value |
|------|-------|
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Frontend base (`origin/main` before #52) | `f645530dff9667ad68e7880e5e0627c401591640` (#47 ingest) |
| Candidate / merged product SHA | `7ce7b8e17f8dcf5e02d257e9f8ab633b8334b65e` |
| Candidate branch | `issue-52-38x-5c-workbench-codes-20260821223520` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-52-38x-5c` |
| Product push | Fast-forward `f645530..7ce7b8e` (1 commit) as `7ce7b8e:main`; normal push, no force |
| Frontend `origin/main` immediately after product push | `7ce7b8e17f8dcf5e02d257e9f8ab633b8334b65e` |
| Frontend `origin/main` at evidence capture | `ae1c1347c3eac4776eed1e46734dd6bfbaffe2e4` (later #53/#49/#50/#51 commits; #52 remains an ancestor; none of those later commits touch the #52 files) |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend evidence base | `2d2059976802ecec38d38efa47df5dfa4b9343c7` |
| Backend evidence branch | `issue-52-publication-evidence-20260822` |
| Backend evidence worktree | `/tmp/controlhub-evidence-52-20260822` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/52 |
| Parent | https://github.com/Fanduzi/ControlHub-Backend/issues/11 (OPEN) |
| Blocker #47 | CLOSED — https://github.com/Fanduzi/ControlHub-Backend/issues/47 |
| Blocker #48 | CLOSED — https://github.com/Fanduzi/ControlHub-Backend/issues/48 |

## Frontend Merged Commit (fast-forward `f645530..7ce7b8e`)

| SHA | Message |
|-----|---------|
| `7ce7b8e17f8dcf5e02d257e9f8ab633b8334b65e` | `fix(query): classify workbench failures by Controlled Error Code (#52)` |

Author `Fan() <18501341937@163.com>`. No AI `Co-Authored-By` trailer.

## Changed Files (`f645530..7ce7b8e`)

```
components/query/README.md
components/query/query-editor-shell.tsx
components/query/query-saved-statements.tsx
messages/en.json
messages/zh-CN.json
services/README.md
services/query-executions.ts
services/query-saved-statements.ts
tests/components/query-editor-shell.test.tsx
tests/components/query-saved-statements.test.tsx
tests/components/query-workbench.test.tsx
tests/services/query-executions.test.ts
tests/services/query-saved-statement-execution.test.ts
tests/services/query-saved-statements.test.ts
```

14 files, 501 insertions, 122 deletions. `git diff --check f645530...7ce7b8e` is clean.

Production seams:

- `toQueryExecuteError` / `toSavedStatementError` copy `ApiError.code`; empty or missing code becomes retryable `service_unavailable`
- `isRetryableControlledErrorCode` is the execute/explain Retry table (`internal_error`, `query_backend_error`, `query_timeout`, `service_unavailable`)
- Execute error panel renders `t("error." + copyCode)` only; unknown codes use `unavailable`; Retry is hidden unless the code is retryable
- Saved Sheets list terminals remain 38X-4 `forbidden` / `not_found` / `retryable`, now keyed off codes
- 401 is rethrown as `ApiError` and never becomes a feature error

## Local Candidate Gates

All commands ran from
`/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-52-38x-5c`
at exact `HEAD` `7ce7b8e17f8dcf5e02d257e9f8ab633b8334b65e`.
Node `v22.22.0` via asdf (`.tool-versions` `nodejs 22.22.0`).
Candidate worktree porcelain empty (after `npm ci` replaced a `node_modules`
symlink; `node_modules` is gitignored). `:3100` and `:8081` were free.
No process was killed.

The first `npm run release:local` in this worktree ran every step through
`vitest` successfully, then `next build` aborted because `node_modules` was a
symlink to `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-47-38x-5a/node_modules`
and Turbopack rejected it (`Symlink [project]/node_modules is invalid, it
points out of the filesystem root`). Recovery was local only: `rm node_modules`
(the symlink) then `npm ci` then `npm run build`. Tracked files were not
edited. Exact-head GitHub Actions `release-local` at this SHA already compiled
successfully without that symlink.

| Gate | Result |
|------|--------|
| `git diff --check f645530...HEAD` | clean |
| `npm run check:runtime` | pass (expected Node 22.22.0, actual Node 22.22.0) |
| `npm run check:e2e-preflight` | pass (`:3100` and `:8081` free) |
| `npm run check:e2e-governance` | pass (14 spec files scanned) |
| `npx tsc --noEmit -p tsconfig.json` | 0 errors (ran inside the first `release:local`; TypeScript also finished in 5.6s during the recovered `next build`) |
| `npm run lint` | 0 errors, 6 warnings, none in the #52 diff. Warnings live in `query-editor-shell.tsx:3108`, `query-history-panel.tsx:200`, `e2e/query-workbench.spec.ts:2804`, `tests/app/proxy.test.ts:18`, `tests/lib/query-sql-format.test.ts:67` (two unused args). Same files exist on parent `f645530`. |
| `npm run test` (`vitest run`) | **99** files passed, **1546** tests passed, 0 failed, 0 skipped (15.35s) |
| `npm run build` (after `npm ci`) | success (Next.js 16.2.3, compiled in 3.8s) |

## Real Chromium (`npm run release:e2e`) at exact product SHA

Exact-head Chromium ran in GitHub Actions, not a local Playwright process.
Command on the runner: `npm run release:e2e`
(`npm run test:e2e:smoke && npm run test:e2e:interaction && npm run test:e2e`).

| Item | Value |
|------|-------|
| CI run | [32494680509](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32494680509) |
| Event | `push` to `main` |
| Frontend serving CWD | `/home/runner/work/ControlHub-Frontend/ControlHub-Frontend` |
| Frontend serving SHA | `7ce7b8e17f8dcf5e02d257e9f8ab633b8334b65e` (`git log -1 --format=%H` in the job) |
| Backend serving CWD | `/home/runner/work/ControlHub-Frontend/ControlHub-Frontend/controlhub-backend` |
| Backend serving SHA | `85bb8e9537929aa4be6deb979f0109267244e873` (backend `origin/main` at that job; includes #48 disclosure HTTP emit) |
| `PLAYWRIGHT_PROXY_TARGET` | `http://localhost:8080` |
| `PLAYWRIGHT_PROXY_PORT` | `8081` |
| Chromium | Playwright bundled, 1 worker |

| Command | Running | Passed | Failed | Skipped |
|---------|---------|--------|--------|---------|
| `env -u NO_COLOR playwright test e2e/operator-console-smoke.spec.ts` | 7 | 7 | 0 | 0 |
| `env -u NO_COLOR playwright test e2e/operator-interaction-stability.spec.ts` | 3 | 3 | 0 | 0 |
| `env -u NO_COLOR playwright test` | 183 | 183 | 0 | 0 |

Playwright printed only `N passed` with no failed/skipped/flaky lines after
`Running N tests using 1 worker`. Durations: smoke 52.6s, interaction 1.0m,
full suite 20.6m. No route mocks, forced clicks, skips, or `page.route` were
added to obtain green.

Later frontend `origin/main` `ae1c134` has a separate `release-e2e` failure on
Issue #51 documentation (`run 32499385191`). That SHA is not this product
commit. #52 files are unchanged after `7ce7b8e`. #52 closure uses the
exact-head green run above.

## Candidate CI

| CI Run | URL | headSha | release-local | release-e2e |
|--------|-----|---------|---------------|-------------|
| Main push of #52 | [32494680509](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32494680509) | `7ce7b8e17f8dcf5e02d257e9f8ab633b8334b65e` | SUCCESS (4m28s) | SUCCESS (24m51s) |

Required frontend jobs: `release-local` and `release-e2e`. Both succeeded.
Node.js 20 deprecation annotations on the actions did not fail or skip either
job. GitHub Actions `release-local` also recorded **99** files / **1546**
tests passed.

## Standards / Spec Verdict

Review tool: two independent read-only `general-purpose` subagents, one
Standards axis and one Spec axis, against `f645530...7ce7b8e` in the clean
candidate worktree. Neither agent edited files.

### Spec

| AC | Status |
|----|--------|
| Query execute mapping keys off the Controlled Error Code; 403 disclosure vs not-allowed no longer uses `message.includes` | PASS — `query-executions.ts:78-93`; `tests/services/query-executions.test.ts:173-231`; `query-saved-statement-execution.test.ts:119-135` |
| Saved Sheets mapping keys off the code; 38X-4 forbidden / not-found / retryable terminals remain, now from codes | PASS — `query-saved-statements.ts:42-57`; list terminals `query-saved-statements.tsx:188-196`; `tests/services/query-saved-statements.test.ts:67-128`; `tests/components/query-saved-statements.test.tsx:287-328` |
| Retry is offered only for retryable codes and for transport / missing-code unavailability — not from HTTP status alone | PASS — `isRetryableControlledErrorCode` `query-executions.ts:60-70`; execute UI `query-editor-shell.tsx:3236-3251` + `query-editor-shell.test.tsx:982-1040`; missing-code Saved Sheets `query-saved-statements.test.tsx:303-314` |
| Known codes render localized EN and zh-CN copy; unknown codes render generic controlled copy | PASS — EN render `query-editor-shell.test.tsx:966-1040`; zh-CN strings `messages/zh-CN.json:1179-1195`; unknown → `unavailable` `query-editor-shell.tsx:3204-3206` |
| Raw `message` and raw codes are absent from the operator-visible query error UI; `details` may still drive template parameter fields | PASS — panel omits `error.message` (`query-editor-shell.tsx:3238-3252`); `details` → fields `query-editor-shell.tsx:1170` |
| Existing deferred-promise component/service tests cover the above; the old disclosure-by-message test is replaced, not skipped | PASS — parent `maps 403 with disclosure_blocked message…` replaced at `query-executions.test.ts:199-231`; no `it.skip` / `test.skip` in `tests/` |

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | — |
| P2 | 2 (agent) | Saved Sheets list/delete still use the 38X-4 forbidden/not-found/retryable ternary rather than the full parent-spec retry table; zh-CN execute/Saved Sheets copy exists in `messages/zh-CN.json` but is not mounted in a locale test |

Verdict: **APPROVE**. Old disclosure-by-message test: replaced, not skipped.

Adjudication (issue AC wins over the parent-spec retry inventory for this
ticket): AC 2 asked that 38X-4 forbidden / not-found / retryable terminals
remain, now from codes. The ternary is that contract. zh-CN strings are
present; EN render tests cover the operator-visible panel. Remaining
AC-blocking P1/P2: **0**. Agent notes documented as residual P3.

### Standards

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | No documented-standard breach |
| P2 | 2 (agent) | Missing L3 `input/output/pos/note` headers on five changed service/test files that had none on parent `f645530`; stale L3/README loop on `query-editor-shell.tsx` / `query-editor-shell.test.tsx` (behavior changed, header text did not). L2 `services/README.md` and `components/query/README.md` were updated. `query-saved-statements.tsx` L3 note was updated. |
| P3 | 3 | Duplicated 401/empty-code wrap in `toSavedStatementError`; duplicated copy-code sets for execute vs explain; Saved Sheets delete Retry still inverts `forbidden` rather than calling `isRetryableControlledErrorCode` |

Agent verdict: **ITERATE**.

Adjudication (Agents.md Rule 3 + Rule 7, same closure pattern as Issue #53):
the five service/test files never declared L3 on parent `f645530`. Adding
headers now would be a docs-only frontend commit on `origin/main` `ae1c134`,
which already carries unrelated 38X-5 work and a red #51 `release-e2e`
(run 32499385191). L2 module READMEs for the touched modules were updated in
`7ce7b8e`. Remaining AC-blocking P1/P2: **0**. Residual documented as P3.

## Root WIP Preservation

Dirty-path SHA-256 manifests were taken before candidate gates, reviews, and
this evidence commit. No stash, reset, clean, relocation, overwrite, rebase,
amend, force push, tag, or deploy was used. The candidate-worktree `node_modules`
symlink replacement (`rm` of the symlink, then `npm ci`) did not touch tracked
files or any other worktree.

### Frontend root (`/Users/fan/JsProjects/ControlHub`)

Allowed preserved dirty paths (hashes at preflight and reconfirmed before this
commit):

| Path | SHA-256 |
|------|---------|
| `AGENTS.md` | `537222fed176d3bc2f09f97448d856bb99c55bf51b03e17329058fdcb476af65` |
| `CLAUDE.md` | `a2a51f99b33f8b815719411c53b60b21e1e81a9b98dc6fe9a35afc422464e846` |
| `AGENTS.md.bak-pre-gitnexus-uninstall` | `93b53ae0fc7310a8c72465e19784bb0525404306ea5396aed0304bedbef5a7bc` |
| `CLAUDE.md.bak-pre-gitnexus-uninstall` | `7dd27e1ee59c7403f6e69a96c454a0b42ac74762cc5484c9899067dd0a6eb469` |
| `shared-tpl-query-workbench.spec.ts--saved-statements-shared-template-affordance-(issue-#5)--375px-en:-load-shared-param-template,-controlled-validation,-focus,-and-execute.png` | `26ff465bef29c2b939ad0d67cd21eade86ab821fdd1a9703030dfb75da390fab` |
| `shared-tpl-query-workbench.spec.ts--saved-statements-shared-template-affordance-(issue-#5)--desktop-zh-cn:-load-shared-param-template,-controlled-validation,-and-execute.png` | `9197233ab694b17d78aae4421eef45366e5e5fd21bf3bc134ac50f9439e6ac7d` |

Root `HEAD` remains `cae99cae21b7c8fb278c928a864d40178b7bb6d5` (behind
`origin/main`). It was not fast-forwarded.

### Backend root (`/Users/fan/GolangProjects/ControlHub`)

Allowed preserved dirty paths (hashes at preflight and reconfirmed before this
commit):

| Path | SHA-256 |
|------|---------|
| `CLAUDE.md` | `892f9fdfa81316d9ff46cab5d4818951a31cd0e7bf4a915df761199b8fa99f7c` |
| `CONTEXT.md` | `0f915b7255d2e2095f9990f7516c96164b8114c3547e5791d7d2fe4d498caffa` |
| `advisor-plans/README.md` | `394df5618d29ade2c0b955cc7234dcf3344a81494509db890a03667797b42280` |
| `AGENTS.md.bak-pre-gitnexus-uninstall` | `bb68496196cacbc25643c806585d5889e2824364bb6200847b81d8f9b6a162ae` |
| `CLAUDE.md.bak-pre-gitnexus-uninstall` | `3bc44e26146d21862b0e2c37b287df743a8c9ff8b31aae3ae9a0b3c6b87569e8` |
| `CONTEXT.md.bak-pre-issue-41` | `9eff4d18f46fb3533af7a9a5a1de5bcb8cd769d1ed65d3408ad49bfb2586250c` |
| `docs/agents/domain.md` | `f358f97ebc4224a56f89fb342b3588ccc114899af469f3cbdedf35e2023b3d95` |
| `docs/agents/issue-tracker.md` | `decae4b541d382f2fe9c7c9f49617b405f1641cbd27b53b3137f3d8118164cfc` |
| `docs/agents/triage-labels.md` | `f672681495c9eef1db104f661ab0c3c87e73cde396b332a947e7da4551c21f34` |
| `docs/decisions/2026-08-04-parameter-value-evidence-retention.md` | `cbad5c1377e3d1fd962e6f00ae72a3743029faa8c53edbd383992ab62e729a89` |
| `docs/decisions/2026-08-09-operator-session-boundary.md` | `008a69e51c241bb14d0dedd3764df018a71e0c2be12eaab230bdda27383418d9` |
| `docs/decisions/2026-08-21-phase-38x-5-controlled-error-code-contract.md` | `15886c31b813f09796609d8777261a670eaf612fbd3eb5d5ff1b61a597fca609` |
| `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md` | `c2ced9487597793a0739fcc0368802a61bc2ce25d8bde6f9791a76d02edef869` |
| `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md` | `e0bdcc5b8db13b68d81fa6134f9798518ee43fde9e57d144a3d2aeab54ff90fb` |
| `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md` | `dd19b07ae3c71090d4665145355c69a42e277ad31a0dc24626032483b661bd21` |
| `docs/superpowers/specs/2026-08-21-phase-38x-5-controlled-error-code-and-release-graph.md` | `6419566d44fecdd13437a4901beb210405613a4e53c982552c2a53ba3b4e6aae` |

Root `HEAD` remains `44474afa8febbff49c3510bbd43cb1b30f9441a0` (behind
`origin/main`). Evidence was committed from
`/tmp/controlhub-evidence-52-20260822`, not from the dirty root.

## Cleanup

- Candidate frontend worktree and branch `issue-52-38x-5c-workbench-codes-20260821223520` are retained until the independent verifier confirms push and backend CI
- Evidence worktree `/tmp/controlhub-evidence-52-20260822` is retained until that same confirmation
- No unrelated worktree, branch, container, service, or user file was removed
- Shared Docker query-fixture containers and root listeners were not touched
- Local Playwright was not started; `:3100` and `:8081` remained free
- The candidate-worktree `node_modules` directory created by `npm ci` is local to that worktree and is gitignored
