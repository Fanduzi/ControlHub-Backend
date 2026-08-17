# Issue #8 (38X-2) Resource and Profile Write Atomicity — Release Evidence

Date: 2026-08-17

This is the backend delivery record for the complete scope of issue
`Fanduzi/ControlHub-Backend#8` — "38X-2: Make resource and profile writes
strict, partial, and atomic":

- create-with-profile is a single all-or-nothing transaction (resource row
  plus initial profile row roll back together; profile errors are propagated,
  never swallowed);
- PUT `/resources/{id}/profile` is an explicit full replacement (empty body
  clears all fields);
- PATCH `/resources/{id}/profile` is a partial merge (omitted fields are
  preserved, explicit empty/zero values honored, empty body is a 200 no-op),
  implemented as one atomic COALESCE statement in the repository so concurrent
  partial updates cannot overwrite each other's fields;
- strict single-JSON-object decoding on the profile endpoints (trailing and
  multiple JSON values rejected, JSON `null` body rejected);
- per-resourceType profile field validation (unknown fields, non-string
  values, fractional/non-integer/out-of-range port 1-65535, overlong values
  bounded by the MySQL varchar column widths) all rejected with controlled
  400 `validation_failed` responses carrying field-level details — the known
  oversized-field 500 gap is closed;
- OpenAPI documents PUT full-replacement vs PATCH partial-merge semantics,
  including the PATCH empty-body 200 no-op.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main` before merge, after `git fetch origin`) | `14fa3fa8ba211c07333d32b33ddbc471138004cd` |
| Candidate worktree | `/tmp/controlhub-diag-40841/wt` |
| Candidate branch | `diag/resource-profile-consistency` |
| Product commits | `8e09b696139e04e17deb03ea95f9c022c089bbd5` `fix(resource): make create-with-profile atomic (#8)`; `cfe769b3ce0e5d7d34030908353f9bdd880b8131` `fix(resource): strict profile validation and atomic PATCH merge (#8)` |
| Merge | Fast-forward only (`git merge --ff-only cfe769b3ce0e5d7d34030908353f9bdd880b8131`), normal `git push origin main` (`14fa3fa8..cfe769b3`); no rebase, amend, force-push, tag, or deploy |
| `origin/main` at product publication | `cfe769b3ce0e5d7d34030908353f9bdd880b8131` |
| Evidence commit | this commit (docs) |

Product range (`git diff --stat 14fa3fa8..cfe769b3`) is 21 files, +1678/-92,
all under `cmd/server/`, `internal/`, plus four module READMEs. The only new
file is `internal/api/profile_handler_test.go`. No commit in the range carries
AI co-author attribution.

## Candidate Gates (exact product SHA `cfe769b3`)

All commands executed in `/tmp/controlhub-diag-40841/wt` at
`HEAD=cfe769b3ce0e5d7d34030908353f9bdd880b8131` (commit-time tree), with the
same gates re-run in the merged root at `HEAD=cfe769b3` after the fast-forward
merge.

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, 1772 passed in 14 packages |
| `go vet ./...` (+ `go vet -tags=integration ./internal/integration/`) | PASS, clean |
| `go build ./...` | PASS, clean |
| `go test ./internal/openapi -run TestOpenAPIYAMLIsValid` | PASS |
| `go test -tags=integration -count=1 -run '^Test[^O]' ./internal/integration` | PASS, 243 passed (Testcontainers mysql:8.0) |
| `go test -count=1 -tags=integration -run '^TestOpenAPIFuzz$' ./internal/integration` | PASS; Schemathesis report `failures=0, errors=0, tests=50` (SEED=42, checks include `not_a_server_error`), latest report `junit-20260817T101950Z.xml` |
| `git diff --check` | PASS, clean |
| `check_three_level_doc.sh` (three-level doc protocol) | PASS (`[three-level-doc] OK`; L1 reminder confirmed: root README architecture section is a generic module table — no update needed) |

Zero failures, zero skips. Regression tests were written first (19 RED cases
across service/handler/MySQL seams in the atomic slice; 24 tests added in the
validation/PATCH slice) and went green with the fixes.

## Review

Independent dual-axis reviews (parallel sub-agents, read-only, final-state
passes on each slice):

| Axis | Final verdict |
| --- | --- |
| Standards (AGENTS.md 12-rule, quality-baseline gates, three-level doc protocol, Fowler smell baseline) | P1=0, P2=0, P3=1 (accepted P3: per-type profile field dispatch mirrored across service/repository/test-fake with synchronization comments) |
| Spec (issue #8 scope/acceptance + approved spec `docs/superpowers/specs/2026-04-22-resource-crud-redesign.md`, tracked at base SHA) | P1=0, P2=0, P3=0 |

Intermediate review findings (JSON `null` accepted as empty object; PATCH
read-merge-write lost-update race; PATCH empty body returned 204 instead of
the spec's 200; accidental gofmt break; int64 port converted to zero) were all
fixed and re-verified in later passes. The final commit was reviewed after all
fixes; no unresolved P1/P2 remains.

## CI

| Item | Value |
| --- | --- |
| Run | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31987703218 |
| Event / head SHA | push / `cfe769b3ce0e5d7d34030908353f9bdd880b8131` |
| Job `release-local-gates` (make release-local-gates + argon2id-budget) | SUCCESS |
| Job `release-docker-gates` (test-integration + test-openapi-fuzz + Schemathesis) | SUCCESS |
| Conclusion | success |

## Tracker Ticket

Issue https://github.com/Fanduzi/ControlHub-Backend/issues/8 (38X-2) — the
full acceptance scope is delivered by this range; the issue was closed in this
delivery with a factual comment citing the merged SHA, this evidence path, and
the CI run URL. No parent/successor ticket was closed.

## Root Worktree Preservation

The root working tree at `main` was preserved byte-for-byte through the
delivery (porcelain snapshot taken before the first worktree was created
matches the post-merge state). Approved dirty-path whitelist:

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

Task worktree `/tmp/controlhub-diag-40841/wt` and branch
`diag/resource-profile-consistency` are removed after push and CI success.
Unrelated worktrees, branches, containers, and user files were not touched.
