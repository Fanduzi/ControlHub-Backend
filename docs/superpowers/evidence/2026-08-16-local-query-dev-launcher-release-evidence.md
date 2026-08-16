# Local Query Workbench Dev Launcher Release Evidence

Date: 2026-08-16

This is the backend delivery record for the local Query Workbench launcher
work: `make run-query-dev`, user-level mode-0600 fixture state, one-time
legacy `.query-e2e-mysql.env` migration, state isolation by
container/port/database/readonly-user identity, cross-worktree reuse, and
fail-fast when state is missing. It does not change frontend code, production
authentication logic, or any prior tracked evidence or specification.

No tracker issue is attached to this delivery. Open issues were inspected
and none matched this work; none were closed.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main` before merge, after `git fetch origin`) | `3c32d19b8886e0832ac8e8c76e07e3806d49da61` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-wt-local-query-dev-20260816` |
| Candidate branch | `chore/local-query-dev-launcher-20260816` |
| Product commit | `92689f270b025ee862fcc9854c25ad23376e2fca` (`fix(scripts): isolate query fixture state per fixture identity`) |
| Merge | Fast-forward only (`git merge --ff-only chore/local-query-dev-launcher-20260816`), normal `git push origin main` (`3c32d19b8886e0832ac8e8c76e07e3806d49da61..92689f270b025ee862fcc9854c25ad23376e2fca`); no rebase, amend, force-push, tag, or deploy |
| `origin/main` at product publication | `92689f270b025ee862fcc9854c25ad23376e2fca` |
| Evidence commit | this commit (docs) |

Product range (`git diff --stat 3c32d19b8886e0832ac8e8c76e07e3806d49da61..92689f270b025ee862fcc9854c25ad23376e2fca`) is exactly six files: `Makefile`, `README.md`, `scripts/README.md`, `scripts/query-e2e-mysql-state.test.sh`, `scripts/query-e2e-mysql.sh`, `scripts/run-query-dev.sh`. No Go production handler, authentication, OpenAPI, or frontend file changed.

Product commits in order:

1. `8680659fecd81c8e8269f28804a7aa48a8427bf8` `chore(dev): add make run-query-dev local query acceptance launcher`
2. `d470f627fe7ae48994e6684a46b05bf2dc1d4ed2` `docs(scripts): add run-query-dev L3 header and scripts README entry`
3. `5c72aea52f0dc3363211e45f2bc191ce509b0d6b` `fix(dev): share query fixture state across worktrees`
4. `92689f270b025ee862fcc9854c25ad23376e2fca` `fix(scripts): isolate query fixture state per fixture identity`

No commit in this range carries AI co-author attribution.

## Candidate Gates (exact product SHA)

All commands below were executed in
`/Users/fan/GolangProjects/ControlHub-wt-local-query-dev-20260816` at
`HEAD=92689f270b025ee862fcc9854c25ad23376e2fca`.

| Command | Result |
| --- | --- |
| `bash -n scripts/query-e2e-mysql.sh scripts/run-query-dev.sh scripts/query-e2e-mysql-state.test.sh` | PASS, exit 0 |
| `bash scripts/query-e2e-mysql-state.test.sh` | PASS, exit 0 |
| `bash scripts/check-docs.sh` | exit 0; printed `No changed files to check` because the candidate worktree was clean and the script reads the staged index (empty) rather than `origin/main...HEAD` |
| `check_three_level_doc.sh --staged` | exit 0; printed `[three-level-doc] no changed files` on the clean candidate worktree |
| Committed-tree three-level-doc equivalent on `origin/main...HEAD` using the same L3/L2 rules | PASS: changed source files `scripts/query-e2e-mysql-state.test.sh`, `scripts/query-e2e-mysql.sh`, `scripts/run-query-dev.sh` all have `input`/`output`/`pos`/`note`; module `scripts/README.md` is in the same range |
| `git diff --check` (worktree and `origin/main...HEAD`) | PASS, exit 0 |
| `go build ./...` | PASS, exit 0 |
| `make test` (`go test ./...`) | PASS, exit 0; 13 packages `ok` (most cached), `internal/testsupport/operatoraccess` has no test files; 0 FAIL |
| `make openapi-validate` (`go test ./internal/openapi -v -run TestOpenAPIYAMLIsValid`) | PASS |

Zero failures. The clean-worktree `check-docs.sh` / `--staged` no-op is not treated as coverage of the committed range; the committed-tree three-level check is the range proof.

## Identity-Isolation Regression

`scripts/query-e2e-mysql-state.test.sh` is Docker-free. It uses a temporary
`XDG_STATE_HOME` and a stub `docker` executable. It does not read, copy, or
print the backend root legacy fixture file. The passing run covered:

- one-time legacy `.query-e2e-mysql.env` migration into user-level state
- migrated state mode `0600`
- two fixture identities (container/port/database/readonly-user) write
  distinct state paths and do not overwrite each other
- the same identity reused from a fresh directory keeps the stored state
  (no regeneration)
- `down` removes only the current identity's state
- a running fixture with no state, and a mismatched legacy handoff, fail
  fast without guessing credentials; the mismatched legacy file is left
  untouched

The real backend-root legacy fixture was not opened, copied, or printed
during candidate gates or this closure.

## Published Product CI

CI run
[31950458524](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31950458524)
on `origin/main` at head `92689f270b025ee862fcc9854c25ad23376e2fca`:
`event=push`, `status=completed`, `conclusion=success`.

- `release-local-gates` job `95173017227` — `success`
- `release-docker-gates` job `95173017219` — `success`

`headSha` of the run equals the product commit. Both required jobs
succeeded. The Node.js 20 deprecation annotation on the runner is
pre-existing Actions-runtime noise and is not a job failure.

## Root WIP Preservation

Backend root stayed on `main`. The root WIP manifest was captured with
NUL-safe `git status --porcelain=v1 -z`, tracked unstaged patch, staged
patch (empty), untracked name-only manifest, and SHA-256 hashes of the
untracked files. The same snapshots were byte-identical after the
fast-forward merge and after the product push:

- tracked modified: `CLAUDE.md`, `advisor-plans/README.md`
- staged: empty
- untracked: `AGENTS.md.bak-pre-gitnexus-uninstall`,
  `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md`,
  `docs/agents/domain.md`, `docs/agents/issue-tracker.md`,
  `docs/agents/triage-labels.md`,
  `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`,
  `docs/decisions/2026-08-09-operator-session-boundary.md`,
  `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`,
  `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`,
  `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md`

None of those paths overlap the product range. No stash, restore, reset,
clean, or amend occurred.

## Intentionally Not Executed

`APP_PORT=8084 make run-query-dev` was not run from the backend root. That
command is a stateful smoke: if a root ignored legacy
`.query-e2e-mysql.env` is present, the launcher migrates it into user-level
state. That migration is left as a human acceptance step after this
publication. After the human run, confirm `/health` returns 200 on port
8084 and re-login to the frontend for Query Workbench acceptance. This
closure did not create users, copy credentials, or stop existing services.

## Cleanup And Safety

- Existing `controlhub-query-e2e-mysql` was left running.
- Existing listener on `:8082` was left running. `:8080` had no listener
  before or after this closure; none was started or stopped.
- Candidate worktree
  `/Users/fan/GolangProjects/ControlHub-wt-local-query-dev-20260816` and
  branch `chore/local-query-dev-launcher-20260816` were retained.
- No other worktree or branch was created, moved, or deleted.
- No issue was closed.
