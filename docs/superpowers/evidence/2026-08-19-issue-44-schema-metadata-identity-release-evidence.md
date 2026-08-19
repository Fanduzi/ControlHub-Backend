# Issue #44 Schema Metadata Identity Isolation — Release Evidence

Date: 2026-08-19

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Product repository | `Fanduzi/ControlHub-Frontend` |
| Base (`origin/main` at start) | `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3` |
| Product candidate head | `5d56ac308e0abd6ce7168704f825fbb4643dc673` |
| Final pushed `origin/main` | `8bba785edafa9a987d96e22ea40937b4cd0fe02c` |
| Candidate branch | `issue-44-schema-metadata-identity-20260819` (worktree `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-44-20260819-002451`) |
| Push | Fast-forward `d6bc752..8bba785` (7 commits) as `issue-44...:main`; no force push |
| Evidence repository | `Fanduzi/ControlHub-Backend` |
| Backend base (`origin/main` at start, also the E2E serving SHA) | `cc645994693632fa05bde31ff3ea5692a58e2a82` |
| Backend evidence head | `cc645994693632fa05bde31ff3ea5692a58e2a82` + the docs-only evidence commit carrying this file |

This delivery is the frontend schema-only slice of Issue #44: schema completion
metadata owned by one active Schema Metadata Identity `(targetResourceId,
database)`. Saved Sheets terminal-state behavior belongs to Issues #42/#43 and
is untouched. The backend contains no production or test change for #44; the
backend commit is documentation (tracked evidence) only.

## Frontend Merged Commits (fast-forward `d6bc752..8bba785`)

| SHA | Message |
| --- | --- |
| `2abc795c7d02e9853b696ec74f102dac871d1de8` | feat(query): isolate schema completion by Schema Metadata Identity (#44) |
| `76f008458ecc01701451efebb838d40c37c0f139` | fix(query): reload identity metadata on clear and mid-flight db change (#44) |
| `6545bed6c53d14fe2ed0184d4e02a36a4c27d5f5` | test(query): real-Chromium schema identity coverage + backend-valid page size (#44) |
| `5d56ac308e0abd6ce7168704f825fbb4643dc673` | fix(query): decouple target-scoped db-list fetch and clear completion on object failure (#44) |
| `e6bc8e7744b0849e92c88790226683676db834f3` | ci(frontend): raise release-e2e job timeout to 45m |
| `bd3a1e3307aa1dc67bc156b0ba1e1c175113b797` | ci(frontend): cache Playwright browsers to fix flaky E2E install |
| `8bba785edafa9a987d96e22ea40937b4cd0fe02c` | ci(frontend): install chromium without --with-deps to avoid apt mirror hang |

The first four commits carry all product and test code; the last three are CI
workflow configuration only (they fix the release-e2e job's flaky run timeout /
browser-install path and do not change any product behavior).

## Candidate And Merged-Root Gates (frontend, exact SHAs)

All gates ran from `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-44-20260819-002451` at exact `HEAD`.

| SHA | Command | Result |
| --- | --- | --- |
| `5d56ac3` | `npm run release:local` (check:runtime, check:e2e-preflight, check:e2e-governance, tsc, eslint, `vitest run`, build) | PASS, exit 0; TypeScript clean; ESLint 0 errors; 1502 tests passed, 0 failed |
| `5d56ac3` | `npm run release:e2e` against live isolated backend at `cc645994693632fa05bde31ff3ea5692a58e2a82` | PASS, exit 0; smoke 7 passed + interaction 3 passed + full suite 179 passed = 189, 0 failed, 0 skipped |
| `8bba785` (merged root) | `npm run release:local` | PASS, exit 0; 1502 tests passed, 0 failed |
| `8bba785` (merged root) | `npm run release:e2e` | PASS, exit 0; smoke 7 + interaction 3 + full 179 = 189, 0 failed, 0 skipped |

E2E serving processes: frontend dev server `:3100` and api-proxy `:8081` from the
frontend worktree (HEAD above), backend `:8080` from
`/Users/fan/GolangProjects/ControlHub-Backend-wt-issue44-e2e` at exact
`cc645994693632fa05bde31ff3ea5692a58e2a82`, disposable MySQL metadata database
`controlhub_issue44_e2e` (migrations v17) + shared Query E2E Docker fixture
`controlhub-query-e2e-mysql` (`:13306`, untouched by cleanup).

## Frontend CI

[Frontend CI run 32238786306](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/32238786306) completed **success** at exact pushed SHA `8bba785edafa9a987d96e22ea40937b4cd0fe02c` (event push, branch main):

| Required job | Result |
| --- | --- |
| `release-local` | SUCCESS |
| `release-e2e` | SUCCESS (`7 passed`, `3 passed`, `179 passed`; 0 failed, 0 skipped) |

Two earlier runs (`32225809671`, `32230508882`) were cancelled by the release-e2e
job timeout due to the browser-install/`apt-get` stall on shared runners; these
were environment-only, not test failures (one attempt's `Run frontend E2E
release gates` step completed with all tests passing before the job-level
timeout). Resolved by raising the job timeout to 45m and caching Playwright
browsers, and by dropping `--with-deps` whose `apt-get update` hung on Azure
Ubuntu mirrors. The final run 32238786306 is green with those changes.

## Backend Gates (evidence SHA, local + CI)

The backend evidence commit is docs-only (no code/test change). Local gates ran
from `/Users/fan/GolangProjects/ControlHub-Backend-wt-issue44-e2e` at the
evidence SHA; the repository's full required gates were additionally executed by
CI at the pushed SHA.

| Command | Result |
| --- | --- |
| `make release-local-gates` | PASS |
| `make argon2id-budget` | PASS |
| `make openapi-validate` | PASS |
| `git diff --check origin/main...HEAD` | PASS |

The pushed evidence head runs the repository's required CI; the backend CI run completed success at the exact head SHA `929f1f233e3e0e8a8bf0718530687e3dfad5559d`:

[Backend CI run 32242118897](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32242118897)

| Required job | Result |
| --- | --- |
| `release-local-gates` | SUCCESS |
| `release-docker-gates` | SUCCESS |

## Independent Review

Code-review skill (parallel read-only sub-agents) reviewed
`origin/main...5d56ac3` after implementation and again the complete range
before closure:

- **Standards sub-agent:** found 2 blockers (mid-flight database-change dropped
  target-scoped database-list completion; object-list failure left database
  completion populated despite the keyword-only warning), 1 hard violation
  (wall-clock sleep in a deferred test), 1 judgement (duplicated overflow poll).
- **Spec sub-agent:** all #44 acceptance criteria covered; 0 missing, 0 scope
  creep; 1 residual (mid-flight db-list recovery) noted as non-blocking.
- Resolution commit `5d56ac3` reworked the metadata loader into a decoupled
  target-scoped database-list controller, clears schema-derived completion on
  object failure, replaces the sleep with a deterministic `act` flush, and adds
  a deferred mid-flight component test. Post-resolution re-verification (full
  unit suite, full query-workbench Chromium, release:local, release:e2e) passes.
- Remaining P1/P2 after resolution: **0**.

## Root Worktree Preservation

Neither fast-forward push used the root worktrees. Root dirty-path manifests
were hashed before and after candidate gates, review, push, and CI; sorted
SHA-256 manifests are byte-for-byte identical.

- Frontend root `/Users/fan/JsProjects/ControlHub` (6 dirty paths) — manifest SHA-256 identical before/after (`19ce4526a026eed6fd8afb1d2f0841f2a7673f11c52dd468011913d53c45266a`).
- Backend root `/Users/fan/GolangProjects/ControlHub` (11 dirty paths) — manifest SHA-256 identical before/after.

No stash, reset, clean, relocation, overwrite, rebase, amend, force push, tag,
or deploy was used during closure.

## Cleanup

After frontend CI green and this evidence push: temporary E2E services `:8080`
(backend), `:3100` (dev server), `:8081` (api-proxy) were stopped; disposable
metadata database `controlhub_issue44_e2e` was dropped; the temporary credential
file `/tmp/issue44-e2e-creds.env` was deleted. The shared Query E2E Docker
fixture (`controlhub-query-e2e-mysql`, `:13306`) was left untouched. Task
worktrees and branches (frontend `issue-44-schema-metadata-identity-20260819`,
backend `issue44-e2e-20260819-114239`) are intentionally preserved.
