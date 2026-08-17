# Issue #39 Operator-Access Matrix Fixture Binding — Release Evidence

Date: 2026-08-17

This is the backend delivery record for `Fanduzi/ControlHub-Backend#39`
("TestOperatorAccessBoundary admin PUT/PATCH profile fails on canonical seed
(resource 1 is database_cluster)").

## Scope

The real-MySQL operator-access matrix sent database-instance profile bodies to
the canonical seed example path `/resources/1/profile`; resource 1 is a seeded
`database_cluster`, whose profile schema rejects `version`, `host`, `port`,
`role` with a controlled 400. The canonical `make test-integration` target
filtered tests with `-run '^Test[^O]'` to exclude `TestOpenAPIFuzz`, which also
excluded `TestOperatorAccessBoundary` and hid the two failures.

The delivery:

- `internal/integration/operator_access_boundary_test.go` — `boundaryRequestPath`
  now binds resource example paths (`/resources/1` -> test-created fixture id)
  exactly like the existing query-target binding, so profile bodies match the
  self-contained `database_instance` fixture's schema and the matrix never
  depends on mutable canonical seed IDs; L3 header `pos` updated to record the
  fixture-bound matrix. Strict per-resource-type profile validation is
  untouched; no seed data was renumbered or modified; no production code
  changed.
- `Makefile` — `test-integration` now runs every integration test except
  exactly `TestOpenAPIFuzz` (`-run '^Test' -skip '^TestOpenAPIFuzz$'`);
  `test-openapi-fuzz` selects exactly `TestOpenAPIFuzz`
  (`-run '^TestOpenAPIFuzz$'`).
- `internal/integration/README.md` — L2 module documentation updated.

Issue #34 (query-evidence atomicity) is explicitly out of scope for this range
and was not touched.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main` before merge, after `git fetch origin`) | `87caf961248810dfc9338ede66fc3dc6cb1eb27c` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-39-20260817` |
| Candidate branch | `issue-39-operator-matrix-fixture-bind-20260817` |
| Product commits | `3df1aa013a8c822c28658738741d6b5f6ce3036c` `fix(integration): bind boundary matrix resource paths to self-contained fixture (#39)`; `45d2297552211c19f1782f25d7978206f7c4b16a` `docs(integration): reflect fixture-bound matrix in test header (#39)` |
| Merge | Fast-forward only (`git merge --ff-only issue-39-operator-matrix-fixture-bind-20260817`), normal `git push origin main` (`87caf96..45d2297`); no rebase, amend, force-push, tag, or deploy |
| Merged / pushed `origin/main` | `45d2297552211c19f1782f25d7978206f7c4b16a` |
| Evidence commit | this commit (docs) |

Product range (`git diff --stat 87caf96..45d2297`) is 3 files, +13/-9:
`Makefile`, `internal/integration/README.md`,
`internal/integration/operator_access_boundary_test.go`. No commit in the range
carries AI co-author attribution.

## Candidate Gates (exact product SHA `45d2297`)

All commands executed in the candidate worktree at `HEAD=45d2297` (clean tree,
verified after a stray blank line inserted into the untouched
`internal/integration/query_execution_test.go` by the local environment during
the gate run was reverted; the full gate set was re-run at the clean exact-SHA
tree) and re-run in the merged root at `HEAD=45d2297` after the fast-forward.

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, exit 0 (13 packages `ok`, 1 package without test files) |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./internal/integration/` | PASS, clean |
| `go build ./...` | PASS, clean |
| `go test ./internal/openapi -run TestOpenAPIYAMLIsValid` | PASS |
| `go test -race -count=1 ./...` | PASS, exit 0, 13 packages `ok`, no data races |
| `go test -tags=integration -count=1 ./internal/integration` | PASS, exit 0, 382 passed / 0 failed / 0 skipped (Testcontainers mysql:8.0) |
| `make test-integration` | PASS, exit 0, 381 tests executed, 0 failed, 0 skipped; `TestOperatorAccessBoundary` executed (top-level + 136 subtests PASS, including `admin_PUT_/resources/{id}/profile` and `admin_PATCH_/resources/{id}/profile`); exactly `TestOpenAPIFuzz` excluded (`TestOpenAPIFuzzExclusionContract` still runs) |
| `make test-openapi-fuzz` | PASS, exit 0, exactly 1 test executed (`TestOpenAPIFuzz`), Schemathesis checks clean |
| `git diff --check origin/main...HEAD` | PASS, clean |
| `check_three_level_doc.sh` (three-level doc protocol) | PASS at end state (`[three-level-doc] no changed files`); L2 rule satisfied across the delivery scope: `internal/integration/README.md` present and changed in `origin/main...HEAD`; L1 reminder confirmed: root README references the Make targets generically, no update needed |
| `gofmt -l` | Changed files clean; repo-wide list identical to `origin/main` baseline (26 pre-existing unformatted files, none introduced by this range) |

Zero failures, zero skips. Red-to-green was demonstrated first: the focused
real-MySQL command
`go test -tags=integration -count=1 -run 'TestOperatorAccessBoundary/admin_(PUT|PATCH)_/resources/\{id\}/profile' ./internal/integration`
failed at base (`status = 400, want 2xx` for both subtests) and passed (4/4)
at the candidate.

## Review

Independent dual-axis reviews (parallel read-only sub-agents on the fixed base
`87caf96`):

| Axis | Final verdict |
| --- | --- |
| Standards (AGENTS.md 12-rule, quality-baseline gates, three-level doc protocol, Fowler smell baseline) | P1=0, P2=0 after fix; one minor finding (L3 header not synced with behavior change) fixed in `45d2297` and re-verified; no Fowler baseline smells |
| Spec (issue #39 Authoritative Agent Brief + issue body) | P1=0, P2=0, P3=0 |

## CI

| Item | Value |
| --- | --- |
| Run | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32037570789 |
| Workflow | Backend CI (`.github/workflows/backend-ci.yml`) |
| Event / head SHA | push / `45d2297552211c19f1782f25d7978206f7c4b16a` |
| Job `release-local-gates` (make release-local-gates + argon2id-budget; the only job triggered by a push event per the workflow definition) | SUCCESS (run conclusion `success`, 14:01:42Z -> 14:04:11Z) |
| Job `release-docker-gates` | Not triggered by push (workflow runs it on pull_request or manual `workflow_dispatch` only); the equivalent gates were executed locally at the exact SHA above (`make test-integration`, `make test-openapi-fuzz`) and passed |
| Conclusion | success |

Note: with the available token, the Actions per-job and check-run list
endpoints returned 404; the run object itself (run 132) reports
`status=completed, conclusion=success` for the exact head SHA, and the job set
for push events is defined by the checked-in workflow file.

## Tracker Ticket

Issue https://github.com/Fanduzi/ControlHub-Backend/issues/39 — delivered by
this range; closed with a factual comment citing the merged SHA, this evidence
path, and the CI run URL. No parent/successor ticket was closed; issue #34
remains open for its own delivery.

## Root Worktree Preservation

The root working tree at `main` was preserved byte-for-byte through the
delivery (sorted `git status --porcelain` snapshot taken before the merge is
identical to the post-merge state; tracked content checksums for `CONTEXT.md`,
`CLAUDE.md`, `advisor-plans/README.md` unchanged). Approved dirty-path
whitelist:

- tracked modifications: `CLAUDE.md`, `advisor-plans/README.md`
- untracked: `AGENTS.md.bak-pre-gitnexus-uninstall`,
  `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md`, `docs/agents/`,
  `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`,
  `docs/decisions/2026-08-09-operator-session-boundary.md`,
  `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`,
  `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`,
  `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md`

None of these paths overlap the delivered range; all were left untouched.

## Cleanup

The task worktree and branch are intentionally preserved (implementation-phase
instruction), now identical to `origin/main` at `45d2297`. Unrelated worktrees,
branches, containers, and user files were not touched.