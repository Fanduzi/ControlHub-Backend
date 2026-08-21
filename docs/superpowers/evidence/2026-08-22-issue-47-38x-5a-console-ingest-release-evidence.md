# Issue #47 — 38X-5A Console Ingest Preserves Controlled Error Code Release Evidence

Date: 2026-08-22

## Summary

Issue #47 `38X-5A: Console ingest preserves Controlled Error Code` is a
frontend product delivery. Console BFF synthesized JSON errors now publish a
snake_case Controlled Error Code on `error` next to the existing safe
`message` token. The shared API client copies that field onto `ApiError.code`
and does not invent a business code when the envelope omits `error`. Upstream
non-401 bodies stay forwarded byte-for-byte. 401 still performs Controlled
Authorization Error handling (clear session, login) and the replacement body
includes `unauthorized`.

The product commit was already fast-forwarded to frontend `origin/main` on
2026-08-21. This evidence record is backend documentation only. Parent
`Fanduzi/ControlHub-Backend#11` stays open. Query Workbench feature remaps
remain Issue #52.

## Refs

| Item | Value |
|------|-------|
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Frontend base (`origin/main` before #47) | `defda6bb732f2225e53d916cc3dc1ea610a9ac0f` |
| Candidate / merged product SHA | `f645530dff9667ad68e7880e5e0627c401591640` |
| Candidate branch | `issue-47-38x-5a-console-ingest-20260821220252` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-47-38x-5a` |
| Product push | Fast-forward `defda6bb..f645530` (1 commit) as `f645530:main`; normal push, no force |
| Frontend `origin/main` immediately after product push | `f645530dff9667ad68e7880e5e0627c401591640` |
| Frontend `origin/main` at evidence capture | `ae1c1347c3eac4776eed1e46734dd6bfbaffe2e4` (later 38X-5B–G commits; #47 remains an ancestor) |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend evidence base | `b28cd349d295c6d82d6239d44840dd03ab4cf5a4` |
| Backend evidence branch | `issue-47-publication-evidence-20260822` |
| Backend evidence worktree | `/tmp/controlhub-evidence-47-20260822` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/47 |
| Parent | https://github.com/Fanduzi/ControlHub-Backend/issues/11 (OPEN) |

## Frontend Merged Commit (fast-forward `defda6bb..f645530`)

| SHA | Message |
|-----|---------|
| `f645530dff9667ad68e7880e5e0627c401591640` | `fix(bff): preserve Controlled Error Code on console ingest (#47)` |

Author `Fan() <18501341937@163.com>`. No AI `Co-Authored-By` trailer.

## Changed Files (`defda6bb..f645530`)

```
app/api/operator-session/README.md
app/api/proxy/[...path]/README.md
lib/operator-session/README.md
lib/operator-session/response.ts
services/README.md
services/api-client.ts
tests/app/api/README.md
tests/app/api/operator-session-route.test.ts
tests/app/api/proxy-route.test.ts
tests/lib/README.md
tests/lib/operator-session-response.test.ts
tests/services/README.md
tests/services/api-client.test.ts
```

13 files, 221 insertions, 47 deletions. `git diff --check defda6bb...f645530` is clean.

Production seams:

- `bffJson` publishes `{ error: token.replaceAll("-", "_"), message: token }`
- `ApiError.code` is the JSON `error` string when present and non-empty; otherwise undefined
- Proxy still forwards upstream non-401 JSON unchanged (403 fixture keeps `query_result_disclosure_blocked`)

## Local Candidate Gates

All commands ran from
`/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-47-38x-5a`
at exact `HEAD` `f645530dff9667ad68e7880e5e0627c401591640`.
Node `v22.22.0` via asdf (`.tool-versions` `nodejs 22.22.0`).
Candidate worktree porcelain empty. `:3100` and `:8081` were free.
No process was killed.

| Gate | Result |
|------|--------|
| `git diff --check defda6bb...HEAD` | clean |
| `npm run check:runtime` | pass (expected Node 22.22.0, actual Node 22.22.0) |
| `npm run check:e2e-preflight` | pass (`:3100` and `:8081` free) |
| `npm run check:e2e-governance` | pass (14 spec files scanned) |
| `npx tsc --noEmit -p tsconfig.json` | 0 errors (via `next build` TypeScript step and `release:local`) |
| `npm run lint` | 0 errors, 6 warnings, none in the #47 diff. Warnings live in `query-editor-shell.tsx:3106`, `query-history-panel.tsx:200`, `e2e/query-workbench.spec.ts:2804`, `tests/app/proxy.test.ts:18`, `tests/lib/query-sql-format.test.ts:67` (two unused args). Same files exist on parent `defda6bb`. |
| `npm run test` (`vitest run`) | **99** files passed, **1526** tests passed, 0 failed, 0 skipped |
| `npm run build` | success (Next.js 16.2.3, compiled in 3.6s) |
| `npm run release:local` | exit 0 |

## Real Chromium (`npm run release:e2e`) at exact product SHA

Exact-head Chromium ran in GitHub Actions, not a local Playwright process.
Command on the runner: `npm run release:e2e`
(`npm run test:e2e:smoke && npm run test:e2e:interaction && npm run test:e2e`).

| Item | Value |
|------|-------|
| CI run | [32491035649](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32491035649) |
| Event | `push` to `main` |
| Frontend serving CWD | `/home/runner/work/ControlHub-Frontend/ControlHub-Frontend` |
| Frontend serving SHA | `f645530dff9667ad68e7880e5e0627c401591640` (`git log -1 --format=%H` in the job) |
| Backend serving CWD | `/home/runner/work/ControlHub-Frontend/ControlHub-Frontend/controlhub-backend` |
| Backend serving SHA | `518f79121fd2315bd46befc6eb718b9042579871` (backend `origin/main` at that job) |
| `PLAYWRIGHT_PROXY_TARGET` | `http://localhost:8080` |
| `PLAYWRIGHT_PROXY_PORT` | `8081` |
| Chromium | Playwright bundled, 1 worker |

| Command | Running | Passed | Failed | Skipped |
|---------|---------|--------|--------|---------|
| `env -u NO_COLOR playwright test e2e/operator-console-smoke.spec.ts` | 7 | 7 | 0 | 0 |
| `env -u NO_COLOR playwright test e2e/operator-interaction-stability.spec.ts` | 3 | 3 | 0 | 0 |
| `env -u NO_COLOR playwright test` | 183 | 183 | 0 | 0 |

Playwright printed only `N passed` with no failed/skipped/flaky lines after
`Running N tests using 1 worker`. Durations: smoke 1.0m, interaction 1.0m,
full suite 20.8m. No route mocks, forced clicks, skips, or `page.route` were
added to obtain green.

Later frontend `origin/main` `ae1c134` has a separate `release-e2e` failure on
Issue #51 documentation (`run 32499385191`). That SHA is not this product
commit. #47 closure uses the exact-head green run above.

## Candidate CI

| CI Run | URL | headSha | release-local | release-e2e |
|--------|-----|---------|---------------|-------------|
| Main push of #47 | [32491035649](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32491035649) | `f645530dff9667ad68e7880e5e0627c401591640` | SUCCESS (5m28s) | SUCCESS (25m16s) |

Required frontend jobs: `release-local` and `release-e2e`. Both succeeded.
Node.js 20 deprecation annotations on the actions did not fail or skip either
job.

## Standards / Spec Verdict

Review tool: two independent read-only `general-purpose` subagents, one
Standards axis and one Spec axis, against `defda6bb...f645530` in the clean
candidate worktree. Neither agent edited files.

### Standards

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | No documented-standard breach |
| P2 | 0 | — |
| P3 | 2 | Duplicated coded-envelope assertions across helper + route tests; `bffJson` parameter still named `message` while it is the hyphenated token |

Verdict: **APPROVE**.

### Spec

| AC | Status |
|----|--------|
| BFF-synthesized failures include snake_case `error` (`service_unavailable`, `not_found`, `forbidden_header`, `invalid_request`, `payload_too_large`; `unauthorized`/`forbidden` unchanged) | PASS (`bffJson` table + session/proxy route tests) |
| Upstream non-401 responses forwarded without rewriting the body | PASS (proxy 403 keeps `query_result_disclosure_blocked`) |
| Shared API client exposes envelope `error` as Controlled Error Code alongside status, message, and details | PASS (`ApiError.code`) |
| JSON missing `error` is not mapped to codes such as `query_not_allowed` via HTTP status | PASS (`code` stays undefined) |
| Non-JSON and dropped-connection failures remain transport failures | PASS (non-JSON still becomes `ApiError` without `code`; thrown `fetch` still bubbles). Spec axis noted missing dedicated tests for those two unchanged paths |
| 401 still Controlled Authorization Error handling and replacement body includes `unauthorized` | PASS |
| Tests at BFF helper/proxy and API client seams cover the above; feature mapping may still use status until 38X-5C | PASS |

| Severity | Count | Notes |
|----------|-------|-------|
| P1 | 0 | — |
| P2 | 1 | Spec axis: no new api-client test that `response.json()` throws or that `fetch` rejects. Behavior is unchanged. Highest-seam proofs required by the spec (preserve `error`, omit-`error` is not status-mapped, 401 session path) are present. Spec verdict is APPROVE and every issue AC is PASS. Follow-up tests are not landed here because later frontend `origin/main` `ae1c134` already carries unrelated 38X-5 work and a red #51 `release-e2e` |

Verdict: **APPROVE**. Remaining product P1: 0. The Spec-axis coverage note is residual and does not reopen the ingest contract.

## Root WIP Preservation

Dirty-path SHA-256 manifests were taken before candidate gates, reviews, and
this evidence commit. No stash, reset, clean, relocation, overwrite, rebase,
amend, force push, tag, or deploy was used.

### Frontend root (`/Users/fan/JsProjects/ControlHub`)

Allowed preserved dirty paths (hashes at preflight):

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

Allowed preserved dirty paths (hashes at preflight):

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
`/tmp/controlhub-evidence-47-20260822`, not from the dirty root.

## Cleanup

- Candidate frontend worktree and branch `issue-47-38x-5a-console-ingest-20260821220252` are retained until the independent verifier confirms push and backend CI
- Evidence worktree `/tmp/controlhub-evidence-47-20260822` is retained until that same confirmation
- No unrelated worktree, branch, container, service, or user file was removed
- Shared Docker query-fixture containers and root listeners were not touched
- Local Playwright was not started; `:3100` and `:8081` remained free
