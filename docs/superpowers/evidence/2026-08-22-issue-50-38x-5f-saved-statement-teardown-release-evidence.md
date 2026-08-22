# Issue #50 — 38X-5F Saved Statement E2E Teardown Is Guaranteed Release Evidence

Date: 2026-08-22

## Summary

Issue #50 `38X-5F: Saved Statement E2E teardown is guaranteed` is a frontend
product delivery. Query Workbench E2E tests that create a Saved Statement now
record enough identity (`{ id, targetResourceId }`) to delete it in a guaranteed
`afterEach` (per-test) or `afterAll` (shared-template fixtures created in
`beforeAll`). Teardown DELETE 404 is success (the test may already have deleted
the row). Any other teardown failure fails the test. The create URL is
authoritative for the teardown target; a disagreeing response body cannot redirect
the DELETE to the wrong target.

The original product commit was fast-forwarded to frontend `origin/main` on
2026-08-21. Two fix-forward commits were pushed during the standards/spec review
loop on 2026-08-22 to resolve reviewer-found P2s. This evidence record is backend
documentation only. Parent `Fanduzi/ControlHub-Backend#11` stays open. Sibling
issues #51 and #53 stay open.

## Refs

| Item | Value |
|------|-------|
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Frontend base (`origin/main` before #50) | `4de441ef0663a4e3e95fe1d76522e6bdb1e04303` (#49 E2E-once) |
| Original candidate / product SHA | `a9d5002bab87d50e42d0e976348533fc8f1e5d5f` |
| Fix SHA 1 (URL-authoritative target) | `d968b9e8da0206b41fffed34d5ca412d2894012e` |
| Fix SHA 2 (beforeAll tracking + afterAll teardown) | `91eaf6c8dcaa1ca10dcf394a61768cdfd34a1c58` |
| Fix SHA 3 (track before validate) | `175add77e5a0323362ccaf04db65d84ef5c295c1` |
| Final frontend `origin/main` | `175add77e5a0323362ccaf04db65d84ef5c295c1` |
| Product push | Fast-forward `4de441e..175add7` (4 commits total) as `175add7:main`; normal push, no force |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-50-38x-5f` |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend evidence base | `2b2cc153cdf822f9319f7c3bbad816134fb5ccda` |
| Evidence branch | `issue-50-publication-evidence-20260822` |
| Evidence worktree | `/private/tmp/controlhub-evidence-50-20260822` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/50 |
| Parent | https://github.com/Fanduzi/ControlHub-Backend/issues/11 (OPEN) |

## Files Changed (full delivered range `4de441e..175add7`)

| File | Change |
|------|--------|
| `e2e/harness/saved-statement-teardown.ts` | New 228-line teardown harness: identity types, URL parsing, response classification, `trackSavedStatement`, `recordSavedStatementCreateResponse`, `submitSavedStatementCreate`, `teardownSavedStatements` (aggregates + throws), `installSavedStatementTeardown` (beforeEach listener + afterEach delete). |
| `e2e/query-workbench.spec.ts` | Wired `installSavedStatementTeardown()` into all four per-test create-capable describes; `trackSavedStatement` called on UI creates; `teardownSavedStatements` used in `afterAll` for shared-template `beforeAll` fixtures; create-URL-authoritative identity derivation. |
| `e2e/README.md` | Documented teardown contract: per-test `afterEach` + shared-template `afterAll`. |
| `e2e/harness/README.md` | Listed teardown harness. |
| `tests/e2e-harness/saved-statement-teardown.test.ts` | New 265-line unit test file covering URL classification, 404 handling, HTTP errors, fetch throws, multiple-failure aggregation, and URL-authoritative target mismatch. |

## Candidate Gates

No local release:e2e was run; the exact-head Chromium suite was exercised entirely
by GitHub Actions CI on each push to `main`. The following local gates were run
on the final candidate worktree at `175add7`:

| Gate | Result |
|------|--------|
| `git diff --check 4de441e...HEAD` | clean |
| `npx tsc --noEmit -p tsconfig.json` | 0 errors |
| `npx eslint e2e/harness/saved-statement-teardown.ts e2e/query-workbench.spec.ts` | 0 errors; 1 warning (pre-existing `historyItemCount` unused-var, identical at parent SHA `d968b9e`) |
| `npm run check:e2e-governance` | pass (14 spec files scanned) |
| `npx vitest run tests/e2e-harness/saved-statement-teardown.test.ts` | **15** tests passed, 0 failed |

## Real Chromium (`npm run release:e2e`) at Final Product SHA

Exact-head Chromium ran in GitHub Actions, not a local Playwright process.
Command on the runner: `npm run release:e2e` (`npm run test:e2e` →
`env -u NO_COLOR playwright test`).

### Final SHA (`175add7`) — used for closure

| Item | Value |
|------|-------|
| CI run | [32570870769](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32570870769) |
| Event | `push` to `main` |
| Frontend serving CWD | `/home/runner/work/ControlHub-Frontend/ControlHub-Frontend` |
| Frontend serving SHA | `175add77e5a0323362ccaf04db65d84ef5c295c1` |
| Chromium | Playwright bundled, 1 worker |

| Command | Running | Passed | Failed | Skipped |
|---------|---------|--------|--------|---------|
| `env -u NO_COLOR playwright test` | 183 | 183 | 0 | 0 |

Playwright printed `183 passed (20.5m)` with no failed/skipped/flaky lines.
No route mocks, forced clicks, skips, or `page.route` were added to obtain
green.

### Original product SHA (`a9d5002`) — first push

| Item | Value |
|------|-------|
| CI run | [32498921106](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32498921106) |
| Event | `push` to `main` |

| Command | Running | Passed | Failed | Skipped |
|---------|---------|--------|--------|---------|
| `env -u NO_COLOR playwright test` | 183 | 183 | 0 | 0 |

Playwright printed `183 passed (14.7m)`.

### Intermediate SHA (`ae1c134`, docs-only commit) — unrelated flake

CI run [32499385191](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32499385191)
failed on `console-ux.spec.ts:96 › database page has engine filter`
(1 failed, 182 passed). This SHA is a docs-only commit (CLAUDE.md, docs/e2e-governance.md,
e2e/harness/README.md) between `a9d5002` and `d968b9e`. The failure is an unrelated
flaky UI test; no #50 files changed. Subsequent pushes restored full green.

## Candidate CI

| CI Run | URL | headSha | release-local | release-e2e |
|--------|-----|---------|---------------|-------------|
| Original product push | [32498921106](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32498921106) | `a9d5002` | SUCCESS (5m30s) | SUCCESS (17m31s) |
| Fix SHA 1 push | [32564512767](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32564512767) | `d968b9e` | SUCCESS | SUCCESS |
| Fix SHA 2 push | [32570691719](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32570691719) | `91eaf6c` | SUCCESS | SUCCESS |
| Fix SHA 3 / final push | [32570870769](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32570870769) | `175add7` | SUCCESS (3m56s) | SUCCESS (22m51s) |

Required frontend jobs: `release-local` and `release-e2e`. Both succeeded on
every push. Node.js runtime deprecation annotations did not fail or skip either
job.

## Standards / Spec Verdict

Review tool: two independent read-only `reviewer` subagents, one Standards axis
and one Spec axis, against the full delivered range `4de441e...175add7` in the
clean candidate worktree. Neither agent edited files.

### Standards

Reviewed in two rounds:

1. **Round 1** against `4de441e...a9d5002`: **REJECT** with 1×P2
   (`savedStatementCreateIdentityFromResponse` silently preferred
   `body.targetResourceId` over the authoritative create-URL target;
   AGENTS.md Rule 12 fail-loud). Fix `d968b9e` resolved by making the
   URL authoritative.

2. **Round 2** against `4de441e...d968b9e`: **APPROVE** — P2 confirmed
   resolved; two residual P3 notes (duplicated API-base resolution across
   `saved-statement-teardown.ts:25-32` and `e2e/api.helpers.ts:16-20`;
   unused `ok` field in `SavedStatementTeardownFetch`) — non-blocking.

| Severity | Round 2 Count | Notes |
|----------|---------------|-------|
| P1 | 0 | — |
| P2 | 0 | Prior P2 resolved in `d968b9e`; subsequent commits (spec-file-only) introduce no standards regression |
| P3 | 2 | Duplicated API-base resolution; unused `ok` field |

### Spec

Reviewed in three rounds:

1. **Round 1** against `4de441e...d968b9e`: **REJECT** with 1×P2
   (`ensureSharedTemplate` in `beforeAll` created shared-template Saved
   Statements, discarded their ids, never tore them down — contradicted
   AC1 "every path"). Fix `91eaf6c` added tracking array + `afterAll`
   teardown + README correction.

2. **Round 2** against `4de441e...91eaf6c`: **REJECT** with residual
   1×P2 (tracking occurred after full response validation; a 201 with
   valid `id` but unexpected other fields threw before push, leaking
   the row). Fix `175add7` reordered to track as soon as the id is
   deletable.

3. **Round 3** (focused re-verdict) against `4de441e...175add7`:
   **APPROVE** — both prior P2s confirmed resolved; no new findings.

| AC | Status |
|----|--------|
| Every create path records deletable identity | PASS — `installSavedStatementTeardown()` covers all four per-test describes; `trackSavedStatement` called on every UI/API create; `afterAll` covers shared-template `beforeAll` fixtures. Tracking happens before any post-create contract validation throw. |
| Teardown runs on failure (afterEach + afterAll) | PASS — `afterEach` registered via `installSavedStatementTeardown` in all four describes; `afterAll` registered for shared-template fixtures; both run even when assertions fail. |
| DELETE 404 is success | PASS — `isSavedStatementTeardownSuccessStatus()` accepts 404 and 2xx; unit-tested. |
| Other teardown failures fail the test | PASS — `teardownSavedStatements` aggregates + throws; `afterEach` and `afterAll` rethrow capture failures. |
| Real Chromium, no mocks | PASS — Playwright Chromium; no `page.route`, `page.evaluate` HTTP, request interception, or forced clicks in added lines. |

| Severity | Round 3 Count | Notes |
|----------|---------------|-------|
| P1 | 0 | — |
| P2 | 0 | Both prior P2s resolved |

## Root WIP Preservation

Dirty-path manifests taken immediately before evidence commit and compared with
the #49 recorded whitelist. No stash, reset, clean, relocation, overwrite, rebase,
amend, force push, tag, or deploy was used.

### Backend root (`/Users/fan/GolangProjects/ControlHub`)

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
| `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md` | `e0bdcc5b8db13b68d18fa6134f9798518ee43fde9e57d144a3d2aeab54ff90fb` |
| `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md` | `dd19b07ae3c71090d4665145355c69a42e277ad31a0dc24626032483b661bd21` |
| `docs/superpowers/specs/2026-08-21-phase-38x-5-controlled-error-code-and-release-graph.md` | `6419566d44fecdd13437a4901beb210405613a4e53c982552c2a53ba3b4e6aae` |

Root `HEAD` remains `44474afa8febbff49c3510bbd43cb1b30f9441a0` (behind
`origin/main`). Evidence is committed from the evidence worktree, not from
the dirty root.

### Frontend root (`/Users/fan/JsProjects/ControlHub`)

| Path | SHA-256 |
|------|---------|
| `AGENTS.md` | `537222fed176d3bc2f09f97448d856bb99c55bf51b03e17329058fdcb476af65` |
| `CLAUDE.md` | `a2a51f99b33f8b815719411c53b60b21e1e81a9b98dc6fe9a35afc422464e846` |
| `AGENTS.md.bak-pre-gitnexus-uninstall` | `93b53ae0fc7310a8c72465e19784bb0525404306ea5396aed0304bedbef5a7bc` |
| `CLAUDE.md.bak-pre-gitnexus-uninstall` | `7dd27e1ee59c7403f6e69a96c454a0b42ac74762cc5484c9899067dd0a6eb469` |
| `shared-tpl-query-workbench.spec.ts--saved-statements-shared-template-affordance-(issue-#5)--375px-en:…png` | `26ff465bef29c2b939ad0d67cd21eade86ab821fdd1a9703030dfb75da390fab` |
| `shared-tpl-query-workbench.spec.ts--saved-statements-shared-template-affordance-(issue-#5)--desktop-zh-cn:…png` | `9197233ab694b17d78aae4421eef45366e5e5fd21bf3bc134ac50f9439e6ac7d` |

Root `HEAD` remains `cae99cae21b7c8fb278c928a864d40178b7bb6d5` (behind
`origin/main`). Not touched by this delivery.

## Cleanup

- Candidate frontend worktree `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-50-38x-5f`
  and branch `issue-50-fix-target-mismatch-20260822` are retained until the
  independent verifier confirms push and backend CI.
- Evidence worktree `/private/tmp/controlhub-evidence-50-20260822` is retained
  until that same confirmation.
- No unrelated worktree, branch, container, service, or user file was removed.
- Shared Docker query-fixture containers and root listeners were not touched.
- Local Playwright was not started.
