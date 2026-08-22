# Issue #49 — 38X-5E Release E2E Runs The Full Suite Once Release Evidence

Date: 2026-08-22

## Summary

Issue #49 `38X-5E: Release E2E runs the full suite once` is a frontend product
delivery. CI `release:e2e` now invokes the full Playwright suite once
(`npm run test:e2e` → `env -u NO_COLOR playwright test`). It no longer chains
`test:e2e:smoke` and `test:e2e:interaction` in front of that suite, so those
files are not billed twice. Local `test:e2e:smoke` and `test:e2e:interaction`
scripts remain for developers. `scripts/check-e2e-governance.mjs` exports
`evaluateReleaseE2EGraph` and fails if the chain returns or if the local
subset scripts are removed.

The product commit was already fast-forwarded to frontend `origin/main` on
2026-08-21. This evidence record is backend documentation only. Parent
`Fanduzi/ControlHub-Backend#11` stays open. Successors #50 and #51 stay open.

## Refs

| Item | Value |
|------|-------|
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Frontend base (`origin/main` before #49) | `53c97716d56b85dd01aa64717d37b7b017432be9` (#53 enum checker) |
| Candidate / merged product SHA | `4de441ef0663a4e3e95fe1d76522e6bdb1e04303` |
| Candidate branch | `issue-49-38x-5e-e2e-once-20260821231432` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-49-38x-5e` |
| Product push | Fast-forward `53c97716..4de441e` (1 commit) as `4de441e:main`; normal push, no force |
| Frontend `origin/main` immediately after product push | `4de441ef0663a4e3e95fe1d76522e6bdb1e04303` |
| Frontend `origin/main` at evidence capture | `ae1c1347c3eac4776eed1e46734dd6bfbaffe2e4` (later #50/#51 commits; #49 remains an ancestor; none of those later commits touch the #49 files) |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend evidence base | `6c0926132be155e71fc775e37f70bc93d44d35b2` |
| Backend evidence body SHA | `71b63333edd59caa1a58fecfc8878bcb7036ed7f` |
| Backend evidence push | Fast-forward `6c09261..71b6333` as `71b6333:main`; normal push, no force |
| Backend `origin/main` after evidence body push | `71b63333edd59caa1a58fecfc8878bcb7036ed7f` |
| Backend evidence branch | `issue-49-publication-evidence-20260822` |
| Backend evidence worktree | `/tmp/controlhub-evidence-49-20260822` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/49 |
| Parent | https://github.com/Fanduzi/ControlHub-Backend/issues/11 (OPEN) |
| Successor #50 | OPEN — https://github.com/Fanduzi/ControlHub-Backend/issues/50 |
| Successor #51 | OPEN — https://github.com/Fanduzi/ControlHub-Backend/issues/51 |

## Frontend Merged Commit (fast-forward `53c97716..4de441e`)

| SHA | Message |
|-----|---------|
| `4de441ef0663a4e3e95fe1d76522e6bdb1e04303` | `ci(e2e): run the full Playwright suite once (#49)` |

Author `Fan() <18501341937@163.com>`. No AI `Co-Authored-By` trailer.

## Changed Files (`53c97716..4de441e`)

```
package.json
scripts/check-e2e-governance.mjs
tests/scripts/check-e2e-governance.test.ts
```

3 files, 300 insertions, 40 deletions. `git diff --check 53c97716...4de441e` is clean.

Production seams:

- `package.json` `release:e2e` is `npm run test:e2e` (was
  `npm run test:e2e:smoke && npm run test:e2e:interaction && npm run test:e2e`)
- `test:e2e:smoke` and `test:e2e:interaction` still exist and still target the
  two subset specs
- `.github/workflows/frontend-ci.yml` is unchanged: one step
  `run: npm run release:e2e`; no smoke or interaction suite steps
- `evaluateReleaseE2EGraph` fails chained smoke+interaction+full, missing
  local subset scripts, extra CI suite invocations, and a double full-suite
  CI graph; the checked-in `package.json` + `frontend-ci.yml` graph passes

## Local Candidate Gates

All commands ran from
`/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-49-38x-5e`
at exact `HEAD` `4de441ef0663a4e3e95fe1d76522e6bdb1e04303`.
Node `v22.22.0` via asdf (`.tool-versions` `nodejs 22.22.0`).
Candidate worktree porcelain empty. `node_modules` is a real directory, not a
symlink. `:3100` and `:8081` were free. No process was killed.

The first `npm run release:local` in this worktree failed at
`check:controlled-error-codes` because `CONTROLHUB_BACKEND_DIR` was unset
(Issue #53 added that checker to `release:local`). Recovery was local only:
rerun with `CONTROLHUB_BACKEND_DIR=/Users/fan/GolangProjects/ControlHub-wt-issue-53-38x-5d`
at backend SHA `b28cd349d295c6d82d6239d44840dd03ab4cf5a4` (same backend SHA
the exact-head frontend CI job checked out; `internal/openapi/openapi.yaml`
is unchanged from that SHA to backend `origin/main`). Tracked files were not
edited.

| Gate | Result |
|------|--------|
| `git diff --check 53c97716...HEAD` | clean |
| `npm run check:runtime` | pass (expected Node 22.22.0, actual Node 22.22.0) |
| `npm run check:e2e-preflight` | pass (`:3100` and `:8081` free) |
| `npm run check:e2e-governance` | pass (14 spec files scanned) |
| `npm run check:controlled-error-codes` | pass (37 codes) with `CONTROLHUB_BACKEND_DIR` as above |
| `npx tsc --noEmit -p tsconfig.json` | 0 errors (silent success inside `release:local`; TypeScript also finished in 5.2s during `next build`) |
| `npm run lint` | 0 errors, 6 warnings, none in the #49 diff. Warnings live in `query-editor-shell.tsx:3108`, `query-history-panel.tsx:200`, `e2e/query-workbench.spec.ts:2804`, `tests/app/proxy.test.ts:18`, `tests/lib/query-sql-format.test.ts:67` (two unused args). Same files exist on parent `53c97716`. |
| `npm run test` (`vitest run`) | **101** files passed, **1568** tests passed, 0 failed, 0 skipped (23.63s) |
| `npm run build` | success (Next.js 16.2.3, compiled in 4.0s) |
| `npm run release:local` (with `CONTROLHUB_BACKEND_DIR`) | exit 0 |

## Real Chromium (`npm run release:e2e`) at exact product SHA

Exact-head Chromium ran in GitHub Actions, not a local Playwright process.
Command on the runner: `npm run release:e2e` (`npm run test:e2e` →
`env -u NO_COLOR playwright test`). The job log contains that expansion once
and contains no `test:e2e:smoke` or `test:e2e:interaction` invocations.

| Item | Value |
|------|-------|
| CI run | [32497331327](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32497331327) |
| Event | `push` to `main` |
| Frontend serving CWD | `/home/runner/work/ControlHub-Frontend/ControlHub-Frontend` |
| Frontend serving SHA | `4de441ef0663a4e3e95fe1d76522e6bdb1e04303` (`git log -1 --format=%H` after checkout) |
| Backend serving CWD | `/home/runner/work/ControlHub-Frontend/ControlHub-Frontend/controlhub-backend` |
| Backend serving SHA | `b28cd349d295c6d82d6239d44840dd03ab4cf5a4` (backend `origin/main` at that job; includes #53 closed enum) |
| `PLAYWRIGHT_PROXY_TARGET` | `http://localhost:8080` |
| `PLAYWRIGHT_PROXY_PORT` | `8081` |
| Chromium | Playwright bundled, 1 worker |

| Command | Running | Passed | Failed | Skipped |
|---------|---------|--------|--------|---------|
| `env -u NO_COLOR playwright test` | 183 | 183 | 0 | 0 |

Playwright printed only `183 passed (19.9m)` with no failed/skipped/flaky
lines after `Running 183 tests using 1 worker`. Compared with the previous
chained graph at #52 (`7 + 3 + 183`), this is the same full-suite coverage
once, without a second billing of the smoke and interaction files. No route
mocks, forced clicks, skips, or `page.route` were added to obtain green.

Later frontend `origin/main` `ae1c134` has a separate `release-e2e` failure on
Issue #51 documentation (`run 32499385191`). That SHA is not this product
commit. #49 files are unchanged after `4de441e`. #49 closure uses the
exact-head green run above.

## Candidate CI

| CI Run | URL | headSha | release-local | release-e2e |
|--------|-----|---------|---------------|-------------|
| Main push of #49 | [32497331327](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32497331327) | `4de441ef0663a4e3e95fe1d76522e6bdb1e04303` | SUCCESS (5m34s) | SUCCESS (22m4s) |

Required frontend jobs: `release-local` and `release-e2e`. Both succeeded.
Node.js 20 deprecation annotations on the actions did not fail or skip either
job. GitHub Actions `release-local` also recorded **101** files / **1568**
tests passed. `check:e2e-governance` printed
`E2E governance check passed (14 spec files scanned).`

## Backend Evidence CI

[Backend CI run 32551581714](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32551581714)
completed successfully at exact evidence body SHA
`71b63333edd59caa1a58fecfc8878bcb7036ed7f`:

| Required job | Result |
|--------------|--------|
| `release-local-gates` | SUCCESS (1m19s) |
| `release-docker-gates` | SUCCESS (2m21s) |

The Node.js action-runtime deprecation annotations did not fail or skip either
required job. Argon2id budget ran as part of `release-local-gates` and
succeeded.

## Standards / Spec Verdict

Review tool: two independent read-only `general-purpose` subagents, one
Standards axis and one Spec axis, against `53c97716...4de441e` in the clean
candidate worktree. Neither agent edited files.

### Spec

| AC | Status |
|----|--------|
| CI `release:e2e` invokes the full Playwright suite once | PASS — `package.json` `release:e2e` is `npm run test:e2e`; workflow has one `run: npm run release:e2e`; exact-head job log expands that once to `env -u NO_COLOR playwright test` and runs 183 tests |
| CI does not also run the smoke and interaction suites as separate suite invocations that re-execute those files | PASS — workflow has no `test:e2e:smoke` / `test:e2e:interaction` steps; exact-head job log has none of those invocations |
| Local smoke and interaction commands still exist for a short subset | PASS — `test:e2e:smoke` and `test:e2e:interaction` remain; governance fails if they are removed |
| No new skips, route mocks, forced clicks, or coverage cuts to obtain green | PASS — no new `test.skip`, `page.route`, or `force: true` vs parent; full suite size matches the previous full-suite invocation (183) |

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | — |
| P2 | 0 | — |

Verdict: **APPROVE**. Remaining P1/P2: 0.

### Standards

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | No documented-standard breach |
| P2 | 0 | — |
| P3 | 1 | Test fixture object `FULL_SUITE_SCRIPTS` also holds the smoke/interaction subset scripts (Mysterious Name, judgement only) |

Verdict: **APPROVE**. Remaining P1/P2: 0. Residual P3 is not AC-blocking.

## Root WIP Preservation

Dirty-path SHA-256 manifests were taken before candidate gates, reviews, and
this evidence commit, and reconfirmed immediately before the commit. No stash,
reset, clean, relocation, overwrite, rebase, amend, force push, tag, or deploy
was used.

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
`/tmp/controlhub-evidence-49-20260822`, not from the dirty root.

## Cleanup

- Candidate frontend worktree and branch `issue-49-38x-5e-e2e-once-20260821231432` are retained until the independent verifier confirms push and backend CI
- Evidence worktree `/tmp/controlhub-evidence-49-20260822` is retained until that same confirmation
- No unrelated worktree, branch, container, service, or user file was removed
- Shared Docker query-fixture containers and root listeners were not touched
- Local Playwright was not started; `:3100` and `:8081` remained free
