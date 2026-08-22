# Issue #51 — 38X-5G Console Docs Stop Calling #19 a Prerequisite Release Evidence

Date: 2026-08-22

## Summary

Issue #51 `38X-5G: Console docs stop calling #19 a prerequisite` is a frontend
documentation-only delivery. Three console documents (`CLAUDE.md`,
`docs/e2e-governance.md`, `e2e/harness/README.md`) described the backend
`cmd/e2e-fixture-bootstrap` seam (backend ticket #19) as a current unmerged
frontend release prerequisite. The seam has been on backend `main` and is
required by `release:e2e`, not an open blocker. The delivered commit restates
the seam as shipped: "on backend main, shipped as ticket #19; frontend CI
already calls it". The fail-loud fixture identity resolver wording, the
retired-seed refusal / no-fallback statement, and the TEST/CI-ONLY seam gate
description are retained unchanged.

The product commit was fast-forwarded to frontend `origin/main` on 2026-08-21
as part of the 38X-5 push sequence. This evidence record is backend
documentation only. Parent `Fanduzi/ControlHub-Backend#11` stays open.
Sibling issues stay in their own states; this closure touches only #51.

## Refs

| Item | Value |
|------|-------|
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Frontend base (`origin/main` before #51) | `a9d5002bab87d50e42d0e976348533fc8f1e5d5f` (#50 original product SHA) |
| Candidate / product SHA | `ae1c1347c3eac4776eed1e46734dd6bfbaffe2e4` |
| Final frontend `origin/main` at closure | `175add77e5a0323362ccaf04db65d84ef5c295c1` |
| Merge type | Fast-forward onto `main`; normal pushes only, no force. Push range `a9d5002..ae1c134` (1 commit) |
| Ancestry proof | `git merge-base --is-ancestor ae1c134 origin/main` → ancestor (verified 2026-08-22) |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-51-38x-5g` (clean at `ae1c134`) |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend evidence base | `06c2e88320f7253d26e2b1ee27e1f9ff58ca99de` |
| Evidence branch | `issue-51-publication-evidence-20260822` |
| Evidence worktree | `/private/tmp/controlhub-evidence-51-20260822` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/51 |
| Parent | https://github.com/Fanduzi/ControlHub-Backend/issues/11 (stays OPEN) |

## Files Changed (`a9d5002..ae1c134`, exactly the three AC surfaces)

| File | Change |
|------|--------|
| `CLAUDE.md` | "main (backend ticket #19). The frontend CI depends on it; without it, release-e2e cannot run." → "`main` (shipped as ticket #19). Frontend CI already calls it." |
| `docs/e2e-governance.md` | "The seam is on backend `main` (backend ticket #19). The frontend CI workflow calls it, and release-e2e requires it." → "The seam is on backend `main` (shipped as ticket #19). The frontend CI workflow already calls it." |
| `e2e/harness/README.md` | fixtures.ts row: "(backend ticket #19, currently a frontend release prerequisite)" → "(on backend main, shipped as ticket #19; frontend CI already calls it)" |

Historical evidence files were not rewritten; no advisor-plan WIP is included.

## Acceptance Criteria Verification (read-only, against shipped `origin/main`)

Verified 2026-08-22 against `origin/main` = `175add7` content, not worktree state:

| AC | Status |
|----|--------|
| The three files no longer call #19 a current unmerged release prerequisite | PASS — `git grep -nE "#19\|ticket 19\|prereq" origin/main -- CLAUDE.md docs/e2e-governance.md e2e/harness/README.md` returns only "shipped as ticket #19 … already calls it" statements (plus an unrelated gitnexus `#1939` link reference) |
| Fail-loud fixture identities, no seed fallback still described | PASS — `e2e/harness/README.md` retains "Fail-loud fixture identity resolver … refuses the retired 0002 seed accounts — no fallback"; `docs/e2e-governance.md` retains "it refuses the retired seed identities" |
| TEST/CI-ONLY seam gates still described | PASS — `docs/e2e-governance.md:37`: "Provisioning: backend `cmd/e2e-fixture-bootstrap` (TEST/CI-ONLY seam)" |
| Historical evidence files not rewritten; advisor-plan WIP excluded | PASS — commit diff touches exactly the 3 AC files |

## Review Verdict

Docs-only change: 4 insertions, 5 deletions across three Markdown files; no
code path, spec file, harness module, or CI definition changed. Per the
delivery-closure standard this is trivial work, so no reviewer-subagent loop
was required. Verification was performed directly: acceptance criteria checked
against shipped `origin/main` content (table above), governance consistency
gate re-run locally (below), and CI outcomes inspected independently via the
GitHub API. Remaining unresolved P1/P2: **0**.

## Candidate Gates

Gates were run against the exact delivered SHAs:

| Gate | Command | Where | Result |
|------|---------|-------|--------|
| Whitespace | `git diff --check ae1c134^..ae1c134` | candidate worktree @ `ae1c134` | clean, exit 0 |
| Governance consistency | `npm run check:e2e-governance` | detached checkout of exact final SHA `175add7` (temp verify worktree, removed after) | pass — "E2E governance check passed (14 spec files scanned)", exit 0 |
| `tsc --noEmit` / `eslint` / `vitest` | n/a | — | Not applicable: pure-Markdown diff, no TypeScript or test files changed |

## Real Chromium (`npm run release:e2e`) Status for the Delivered Range

No local Playwright process was started. Exact-head Chromium ran in GitHub
Actions on each push to `main`.

### At product SHA `ae1c134`

| Item | Value |
|------|-------|
| CI run | [32499385191](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32499385191) |
| Event | `push` to `main` |
| Jobs verified via GitHub API | `release-local` SUCCESS; `release-e2e` FAILURE (step "Run frontend E2E release gates") |
| Failure detail | Unrelated flaky UI test `console-ux.spec.ts:96 › database page has engine filter` (182 passed / 1 failed), documented in the #50 evidence record; zero files touched by #51 are involved; subsequent main pushes restored full green |

### Final frontend `origin/main` SHA `175add7` — used for closure

| Item | Value |
|------|-------|
| CI run | [32570870769](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32570870769) |
| Event | `push` to `main` |
| Frontend serving CWD | `/home/runner/work/ControlHub-Frontend/ControlHub-Frontend` |
| Frontend serving SHA | `175add77e5a0323362ccaf04db65d84ef5c295c1` |
| Required jobs | `release-local` SUCCESS; `release-e2e` SUCCESS (183 passed, 0 failed, 0 skipped) |

No skips, mocks, forced clicks, route interception, or timeout widening were
used to obtain green anywhere in the range.

## Frontend CI Summary (all runs in delivered range)

| CI Run | headSha | release-local | release-e2e |
|--------|---------|---------------|-------------|
| [32498921106](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32498921106) | `a9d5002` (base) | SUCCESS | SUCCESS |
| [32499385191](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32499385191) | `ae1c134` (#51 product) | SUCCESS | FAILURE (unrelated flake above) |
| [32564512767](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32564512767) | `d968b9e` | SUCCESS | SUCCESS |
| [32570691719](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32570691719) | `91eaf6c` | SUCCESS | SUCCESS |
| [32570870769](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32570870769) | `175add7` (final) | SUCCESS (3m56s) | SUCCESS (22m51s) |

Required frontend jobs: `release-local` and `release-e2e`. Both succeeded on
the final merged HEAD used for closure.

## Backend Evidence CI

[Backend CI run 32579413326](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32579413326)
completed successfully at evidence body SHA
`664d0dbc35032b52d5cfcb96e3295fae3754693a`:

| Required job | Result |
|--------------|--------|
| `release-local-gates` | SUCCESS |
| `release-docker-gates` | SUCCESS |

Job conclusions verified independently via the GitHub API
(`actions/runs/32579413326/jobs`).

## Root WIP Preservation

Dirty-path manifests taken immediately before evidence commit and compared with
the #50 recorded whitelist. Every hash below is byte-identical to the #50
record: no user work changed during this delivery. No stash, reset, clean,
relocation, overwrite, rebase, amend, force push, tag, or deploy was used.

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
| `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md` | `e0bdcc5b8db13b68d81fa6134f9798518ee43fde9e57d144a3d2aeab54ff90fb` |
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

- Candidate frontend worktree `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-51-38x-5g`
  and branch `issue-51-38x-5g-docs-20260821234215` are retained until the
  independent verifier confirms push and backend CI.
- Evidence worktree `/private/tmp/controlhub-evidence-51-20260822` is retained
  until that same confirmation.
- Temporary final-SHA verify checkout `/private/tmp/controlhub-final-main-verify-20260822`
  was removed after its single governance-check use (task-created, not user work).
- No unrelated worktree, branch, container, service, or user file was removed.
- Local Playwright was not started; shared Docker query-fixture containers were
  not touched.
