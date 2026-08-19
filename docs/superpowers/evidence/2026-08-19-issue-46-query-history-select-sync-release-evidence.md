# Issue #46 Query History Filter Test Async Status Option — Release Evidence

Date: 2026-08-19

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Frontend base (`origin/main` at start) | `8bba785edafa9a987d96e22ea40937b4cd0fe02c` |
| Product source commit (cherry-picked) | `78155d4aa6708c19f828af151681736f25badcc7` — `test(query): await history status option (#46)` |
| New product candidate head | `96fe311c22b33f27c8d45e5aa197c0524db92201` (fresh cherry-pick SHA on current main) |
| Candidate branch | `issue-46-query-history-select-sync-final-20260819` (worktree `/Users/fan/JsProjects/ControlHub-wt-issue-46-final-20260819`) |
| Final pushed frontend `origin/main` | `96fe311c22b33f27c8d45e5aa197c0524db92201` |
| Push | Fast-forward `8bba785..96fe311` (1 commit) as `issue-46-query-history-select-sync-final-20260819:main`; normal push, no force |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend base (`origin/main` at start) | `4a9b7b8ba68a6403c2391b53a29823a987d50eb4` |
| Backend evidence head | the docs-only commit carrying this file on `origin/main` (SHA and its own CI run cited in the Issue #46 closing comment, written after that CI completes) |

This delivery is the Issue #46 baseline fix: the Query History status-filter test
synchronizes with the asynchronously mounted select option before clicking, so
the unit gate is deterministic. The change is test synchronization and
three-level documentation only; no production Query History component, select,
service, i18n, or runtime config changed. The backend commit is documentation
(tracked evidence) only.

### Why the old candidate was superseded

The previous #46 candidate `78155d4` sat on the pre-#44-CI-fix main and its CI
ran on a workflow without the published Playwright browser-install fix. Two
`workflow_dispatch` runs at exactly `78155d4` both showed `release-local`
success and `release-e2e` **cancelled** by the browser-install / `apt-get`
mirror stall (environment-only):

| Old run | headSha | release-local | release-e2e |
| --- | --- | --- | --- |
| `32228258972` | `78155d4aa6708c19f828af151681736f25badcc7` | SUCCESS | CANCELLED |
| `32230300582` | `78155d4aa6708c19f828af151681736f25badcc7` | SUCCESS | CANCELLED |

The published #44 CI fix is `e6bc8e7` (release-e2e timeout 45m), `bd3a1e3`
(cache Playwright browsers), `8bba785` (install chromium without `--with-deps`),
already on `origin/main` before this delivery. The new candidate is a single
re-application of the #46 product commit **only** on top of current
`origin/main` (which already contains all three fix commits); the old
`78155d4` commit, its branch `issue-46-finalize-20260819`, and its worktree
were preserved and never rebased, amended, force-pushed, or deleted. The
frontend CI workflow at the new candidate `96fe311` is the current published
workflow, so the E2E job is not gated by the browser-install stall.

> Note on the commit label: the task brief referenced the #46 product commit by
> label `4dcb003`. That label resolves to no object in either repository or on
> GitHub (HTTP 422); the only commit with subject
> `test(query): await history status option (#46)` is `78155d4`, whose diff
> (test file + component-test module README) matches the required scope exactly.
> The cherry-pick onto current main produced the new candidate `96fe311`.

## Frontend Product Diff (`8bba785..96fe311`)

`git diff origin/main..HEAD` before merge showed exactly two files:

| File | Change |
| --- | --- |
| `tests/components/query-workbench.test.tsx` | `+5 -1`: adds the L3 `input/output/pos/note` header and replaces the synchronous `getByRole("option", { name: /failed/i })` with `await findByRole(...)` before the option is clicked |
| `tests/components/README.md` | `+1 -1`: L2 module row reflects the synchronized asynchronous select interaction |

Commit message is `test(query): await history status option (#46)`; author
`Fan() <18501341937@163.com>`; no AI `Co-Authored-By` trailer; tree content
identical to the product commit `78155d4` at the same base relation. All
downstream assertions (replacement fetch with `status: "failed"`, page size)
are unchanged.

## RED → GREEN Repetition Matrix

Runtime: Node `22.22.0` / npm `10.9.4` (asdf; exact requirement from
`.tool-versions` `nodejs 22.22.0` and `engines: 22.22.x`, enforced by
`npm run check:runtime`). All runs from clean process in each worktree.

| Phase | Command | Runs | Result |
| --- | --- | --- | --- |
| RED @ base `8bba785` | `node ./node_modules/vitest/vitest.mjs run tests/components/query-workbench.test.tsx -t "Filter Apply triggers a replace fetch with filter params"` | 3/3 | **1 failed / 180 skipped** each run (missing accessible `option` /failed/i) |
| GREEN @ candidate `96fe311` | same `-t` command | 10/10 | 1 passed / 180 skipped, **0 failed** each run |
| Full file @ `96fe311` | `node ./node_modules/vitest/vitest.mjs run tests/components/query-workbench.test.tsx` | 5/5 | 181/181 passed, **0 failed, 0 skipped** each |
| Full unit @ `96fe311` | `npm run test` (`vitest run`) | 3/3 consecutive | 98 files / 1502 tests passed, **0 failed, 0 skipped** each (~135s) |

## Local Candidate Gates (`96fe311`, worktree `/Users/fan/JsProjects/ControlHub-wt-issue-46-final-20260819`)

| Command | Result |
| --- | --- |
| `npm run check:runtime` | PASS — expected Node 22.22.0, actual 22.22.0 |
| `npm run release:local` (check:runtime, check:e2e-preflight, check:e2e-governance, `tsc --noEmit`, eslint, `vitest run`, `next build`) | PASS, exit 0; TypeScript clean; ESLint 0 errors; 1502 tests passed, 0 failed; build compiled |
| `git diff --check origin/main HEAD` | PASS — no whitespace errors |
| `scripts/check_three_level_doc.sh` on the change set | PASS, exit 0 — L3 header present, L2 `tests/components/README.md` in change set; L1 root-README reminder informational |

No test was made green by retry, skip, timeout increase, reduced concurrency,
sleep, or weakened assertion; the fix is the async `findByRole` synchronization.

## Frontend CI (exact SHA)

[Candidate run 32251350922](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32251350922) — event `workflow_dispatch`, branch `issue-46-query-history-select-sync-final-20260819`, `headSha` **`96fe311c22b33f27c8d45e5aa197c0524db92201`** (exact match to candidate):

| Required job | Result |
| --- | --- |
| `release-local` | SUCCESS |
| `release-e2e` | SUCCESS — Chromium totals: smoke `7 passed` + interaction `3 passed` + full `179 passed` = **189 passed / 0 failed / 0 skipped**; `Install Playwright browsers` step completed success (browser cache hit `Linux-playwright-3d105f…` restored), E2E step ran ~22 min to completion (no skip/cancel) |

[Merged-main run 32254300836](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32254300836) — event `push`, branch `main`, `headSha` **`96fe311c22b33f27c8d45e5aa197c0524db92201`** (exact match to merged SHA):

| Required job | Result |
| --- | --- |
| `release-local` | SUCCESS |
| `release-e2e` | SUCCESS — Chromium totals: smoke `7 passed` + interaction `3 passed` + full `179 passed` = **189 passed / 0 failed / 0 skipped**; browser-install step success, E2E ran fully |

After push, `git fetch origin` verified frontend `HEAD == origin/main ==
96fe311c22b33f27c8d45e5aa197c0524db92201`; the product range `8bba785..96fe311`
still contains only the #46 test-synchronization + documentation change on top
of the published main.

## Independent Review

Code-review style parallel read-only sub-agents (fresh context, strictly
read-only) reviewed `8bba785..96fe311` (the complete final range) before push:

- **Standards sub-agent** (reviewer, fresh): P1=**0**, P2=**0**, nits=0;
  conforms to repo conventions (async select pattern matches existing use in
  other component tests), L3/L2 documentation complete, commit hygiene clean,
  scope surgical.
- **Spec sub-agent** (reviewer, fresh): P1=**0**, P2=**0**, nits=0; exact
  `findByRole` synchronization at line 3867, all replacement-fetch assertions
  unchanged, only the two test/documentation files changed, no prohibited
  stabilization mechanism.

Remaining P1/P2 after review: **0**.

## Backend Gates (evidence SHA)

The backend evidence commit is docs-only (no code/test change). The repository
required gates were executed by backend CI at the pushed evidence SHA (exact
head). Local mechanical validation per process was run in the evidence
worktree:

| Command | Result |
| --- | --- |
| `git diff --check origin/main...HEAD` | PASS |
| evidence tracked (`git ls-files --error-unmatch <path>`, `git show HEAD:<path>` matches committed content) | PASS |
| evidence scanned (policy: no unresolved-marker text, no unverified CI claims, no secrets) | none found |

The backend evidence head completed backend CI (both required jobs
`release-local-gates` and `release-docker-gates` SUCCESS) at the exact pushed
SHA; run URL and job conclusions are cited in the Issue #46 closing comment
(written after that CI completes).

## Root Worktree Preservation

Neither push used the root worktrees. Root dirty-path manifests were hashed
before and after all candidate gates, review, push, and CI; sorted SHA-256
manifests are byte-for-byte identical.

- Frontend root `/Users/fan/JsProjects/ControlHub` (6 dirty paths) — manifest SHA-256 identical before/after (`19ce4526a026eed6fd8afb1d2f0841f2a7673f11c52dd468011913d53c45266a`; same hash as the Issue #44 closure record). Local branch `main` untouched at `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3`.
- Backend root `/Users/fan/GolangProjects/ControlHub` (11 dirty paths) — manifest SHA-256 identical before/after (`f2b78db28473767d00018be601984f64ae0d791e9fb15f898807297594a76081`). Local `HEAD` unchanged.

No stash, reset, clean, relocation, overwrite, rebase, amend, force push, tag,
or deploy was used during closure.

## Cleanup

After frontend main CI green and this evidence push: the temporary
RED-reproduction worktree `/Users/fan/JsProjects/ControlHub-wt-issue-46-red-base`
(detached, at base `8bba785`) was removed. No E2E services or databases were
started for this delivery (CI runs are self-contained). Preserved intentionally:
frontend candidate worktree/branch
`issue-46-query-history-select-sync-final-20260819` (delivery worktree, per
repository convention), old #46 artifacts (commit `78155d4`, branch
`issue-46-finalize-20260819`), all user WIP in both roots, all historical
worktrees, and shared fixtures.
