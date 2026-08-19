# Issue #34 (38X-3A) Atomic Execution Evidence Pair — Release Evidence

Date: 2026-08-17

This is the backend delivery record for `Fanduzi/ControlHub-Backend#34`
("38X-3A: Make core query execution evidence atomic", parent #9). The
implementation was authored earlier on the local-only branch
`issue-34-query-evidence-atomic-20260817`; this delivery re-verified it
against the latest `origin/main` (which by then included the Issue #39
operator-access matrix fix), fixed two review findings, and closed the issue.

## Scope

Ordinary, paged, and saved-template query executions persist history plus the
fixed `query.executed` audit event as one repository-owned atomic Execution
Evidence Pair (`internal/repository/mysql/query_execution_repository.go`
`InsertExecutionWithAudit`): one transaction, no partial commit, committed id
returned, controlled error on failure, dimensionless
`queryEvidencePersistenceFailures` counter incremented exactly once per failed
pair with one fixed safe log line. A new admin-only
`GET /ops/query-evidence-metrics` operation returns exactly
`queryEvidencePersistenceFailures` (anonymous 401, editor 403), the glossary
records the three evidence terms (Evidence-Bearing Query Attempt, Execution
Evidence Pair, Query Evidence Persistence Failure), and affected module
documentation is synchronized. The standalone history/audit seam remains only
for #36 related-record navigation, per the expand step.

Issue #39 was delivered first (same day) and is fully outside this range.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main` before this delivery, after #39 closure) | `3af5d29bb4f492a0d7628fea777ee90b74b30df8` |
| Pre-rebase branch tip (backup ref `issue-34-pre-rebase-backup-20260817`) | `85a938b8f405bbe53e00a5ce2839df533d972acc` |
| Rebase | `git rebase origin/main` onto `3af5d29`; no conflicts (the only overlapping file with #39, `internal/integration/README.md`, merged cleanly across different rows). New commit SHAs: `7753a1f9170b8b3e6a20cd5f9185e2d478f54bb8` `feat(evidence): atomic Execution Evidence Pair for query execution (issue #34)`, `17701ce0f88129e9797dc54a123f5fbb9d3eaa8a` `docs(domain): merge query evidence terms into glossary`. No content changes other than the rebase. |
| Review-fix commit | `e9562663088a776ac92c114d7c53e67d491114a3` `fix(evidence): route metrics counter through service layer and fix audit event type (#34)` |
| Merged / pushed `origin/main` | `e9562663088a776ac92c114d7c53e67d491114a3` |
| Evidence commit | this commit (docs) |

Delivery range (`git diff --stat 3af5d29..e956266`): 25 files, +1081/-64.
The merge was fast-forward only, executed in a temporary worktree
(`/tmp/wt34-merge`, branch `delivery-issue-34-merge`) because the range adds
`CONTEXT.md` while the root working tree holds an untracked `CONTEXT.md` with
different content; pushing was a normal fast-forward push
(`3af5d29..17701ce` then `17701ce..e956266`). No rebase beyond the initial
rebase above, no amend, no force-push, no tag, no deploy. No commit in the
range carries AI co-author attribution.

## CONTEXT.md Handling (owner-approved)

The range adds `CONTEXT.md` (the domain glossary, previously only an untracked
workspace file) including the three Issue-#34 terms. The repository owner
explicitly approved "merge glossary as-is" (the merged file is the superset:
root WIP content + the 3 new terms). The root untracked `CONTEXT.md` (SHA-256
`9eff4d18f46fb3533af7a9a5a1de5bcb8cd769d1ed65d3408ad49bfb2586250c`) was
preserved byte-for-byte and is untouched; after this delivery it is a
superseded viewer-side copy, and the root local `main` ref was therefore left
at `3af5d29` (one delivery behind `origin/main`) so the root working tree and
all WIP stay byte-identical. Root can catch up once the owner reconciles
`CONTEXT.md` (e.g. adopts the merged glossary).

## Candidate Gates (exact final SHA `e956266`)

All commands executed in `/tmp/wt34-merge` at `HEAD=e956266` (the merged tree)
and, for the pre-fix product, in the issue-34 worktree at `HEAD=17701ce`.

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, exit 0 (13 packages `ok`, 1 no-test package) |
| `go test -race -count=1 ./...` | PASS, exit 0, 13 packages `ok`, no data races |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./internal/integration/` | PASS, clean |
| `go build ./...` | PASS, clean |
| `go test ./internal/openapi -run TestOpenAPIYAMLIsValid` | PASS |
| `go test -tags=integration -count=1 ./internal/integration` | PASS, exit 0, 389 passed / 0 failed / 0 skipped (Testcontainers mysql:8.0; 382 at the #39 baseline + 4 new `TestQueryEvidencePair*` tests + 3 new `/ops/query-evidence-metrics` boundary subtests) |
| `make test-integration` | PASS, exit 0, 381 tests executed, 0 failed, 0 skipped; `TestOperatorAccessBoundary` executed (139 subtests incl. the 3 new `/ops/query-evidence-metrics` anonymous/editor/admin cases) and the 4 `TestQueryEvidencePair*` real-MySQL evidence tests passed |
| `make test-openapi-fuzz` | PASS, exit 0, exactly 1 test executed (`TestOpenAPIFuzz`), Schemathesis checks clean |
| `git diff --check origin/main...HEAD` | PASS, clean |
| `check_three_level_doc.sh` (three-level doc protocol) | PASS at end state; L2 satisfied across the delivery scope (all four module READMEs present and changed) |
| `gofmt -l` | Changed files clean; no new files beyond the pre-existing 26-file `origin/main` baseline |

Zero failures, zero skips. Re-verification from latest `origin/main` is the
point of this delivery: the issue-34 range was rebased onto `3af5d29` (which
includes the #39 Makefile/test changes) and the complete combined-state gate
set was run at `17701ce` and again at the final `e956266`.

## Review

Independent dual-axis reviews (parallel read-only sub-agents), initial pass on
the rebased range at `17701ce` and a re-review of the fix commit:

| Axis | Initial verdict | After fix (`e956266`) |
| --- | --- | --- |
| Standards (AGENTS.md 12-rule, CLAUDE.md layering, quality-baseline, three-level doc protocol, Fowler smells) | P1=1 (api read repository counter directly, violating api->service layering), P2=1 (unparameterized fixed audit event exposed as an `eventType` argument) | P1=0, P2=0; P3=1 (accepted, documented: the service-test fake now asserts its own hardcoded `query.executed` constant — tautological, but the real-MySQL integration test asserts `event_type='query.executed'` against the committed `audit_events` row, so production behavior is proven) |
| Spec (issue #34 body, 9 acceptance criteria) | P1=0, P2=0, P3=0 | P1=0, P2=0, P3=0 (no regressions; metrics still exactly-one-field and admin-gated; pair still atomic; counter still exposed) |

## CI

| Item | Value |
| --- | --- |
| Run (product SHA `17701ce`) | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32039839410 — conclusion `success` |
| Run (final SHA `e956266`) | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32042596619 — conclusion `success` |
| Workflow | Backend CI (`.github/workflows/backend-ci.yml`), push event; job `release-local-gates` (make release-local-gates + argon2id-budget) is the only push-triggered job per the workflow definition; `release-docker-gates` runs on PR/manual dispatch and its equivalents were executed locally at the exact SHA above |
| Conclusion | success on the final head SHA |

## Tracker Ticket

Issue https://github.com/Fanduzi/ControlHub-Backend/issues/34 — delivered by
this range and closed with a factual comment citing the merged SHA, this
evidence path, and the CI run URL. Parent issue #9 was not closed; the
successor is unaffected.

## Root Worktree Preservation

The root working tree at `main` was preserved byte-for-byte through both #39
and #34 deliveries: sorted `git status --porcelain` snapshots taken before the
first merge and after the last push are identical; tracked-content hashes for
`CONTEXT.md`, `CLAUDE.md`, and `advisor-plans/README.md` are unchanged.
Approved dirty-path whitelist (unchanged across the deliveries):

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

Temp merge worktree `/tmp/wt34-merge` and its branch `delivery-issue-34-merge`
are removed after push and CI success, as is the temporary gofmt baseline
worktree `/tmp/gofmt-check`. The issue-34 task worktree
`~/GolangProjects/ControlHub-wt-issue-34-20260817` and branch
`issue-34-query-evidence-atomic-20260817` are intentionally preserved, as is
the pre-rebase backup ref and the issue-39 task worktree/branch. Unrelated
worktrees, branches, containers, and user files were not touched.