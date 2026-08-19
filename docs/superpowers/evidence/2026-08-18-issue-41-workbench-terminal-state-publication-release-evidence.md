# Issue #41 Query Workbench Terminal-State Contract — Release Evidence

Date: 2026-08-18

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main` at start) | `1382c73a061cca807fc18d26147708765514b1c0` |
| Candidate / merged product SHA | `b8c09648bacc7eee37f358e45eeac596ee52af77` |
| Candidate branch | `design/issue-10-workbench-terminal-state-20260818` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-wt-design-issue-10-20260818` |
| Push | Fast-forward `1382c73a061cca807fc18d26147708765514b1c0..b8c09648bacc7eee37f358e45eeac596ee52af77` to `origin/main`; no force push |
| `origin/main` after product push | `b8c09648bacc7eee37f358e45eeac596ee52af77` |

The published product is exactly the reviewed two-commit candidate: `3c6db06e1560bafd8a5514d2ed39c529a0b91e5e` followed by `b8c09648bacc7eee37f358e45eeac596ee52af77`. The delivery modifies only `CONTEXT.md`, the accepted decision record, and the approved delivery specification. No production or test file changed.

## Candidate Gates

All gates ran in the clean candidate worktree at exact `HEAD=b8c09648bacc7eee37f358e45eeac596ee52af77`.

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, 1819 tests in 14 packages |
| `go test -race -count=1 ./...` | PASS, 1819 tests, no data races |
| `go vet ./...` | PASS |
| `go vet -tags=integration ./...` | PASS |
| `go build ./...` | PASS |
| `make openapi-validate` | PASS |
| `make test-integration` | PASS, 239 top-level integration tests, 0 failed, 0 skipped |
| `make test-openapi-fuzz` | PASS, Schemathesis 4.15.2, 2041 generated cases passed, 0 failed; 35 non-generated OpenAPI example cases were skipped by Schemathesis and the fuzz phase tested all 51 selected operations |
| `make argon2id-budget` | PASS, 20 samples, median 101.1ms and p95 106.4ms within budget |
| `bash scripts/check-docs.sh` | PASS (`scripts/check-docs.sh` is not executable, so direct invocation returned permission denied before the successful shell invocation) |
| `git diff --check origin/main...HEAD` | PASS |

Frontend/browser E2E is not present in this backend documentation repository and no runtime behavior changed. The repository's required real-MySQL integration and OpenAPI fuzz gates are recorded above with exact totals.

## Independent Review

Parallel read-only closure reviews examined `origin/main...b8c09648bacc7eee37f358e45eeac596ee52af77`:

- **Standards:** APPROVE; 0 remaining P1, 0 remaining P2.
- **Issue #41 / parent #10 specification:** APPROVE; 0 remaining P1, 0 remaining P2.
- Both reviews confirmed the two accepted commits are published without squash, amend, cherry-pick, or re-authoring and that no runtime/test behavior changed.

## CI

[Backend CI run 32152948129](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32152948129) completed successfully at exact product SHA `b8c09648bacc7eee37f358e45eeac596ee52af77`:

| Required job | Result |
| --- | --- |
| `release-local-gates` | SUCCESS |
| `release-docker-gates` | SUCCESS |

The Node.js action-runtime deprecation annotation and the empty optional Schemathesis artifact upload annotation did not fail or skip either required job.

## Root-WIP Preservation

The root worktree `/Users/fan/GolangProjects/ControlHub` was not used for the fast-forward push. Its pre-existing modified and untracked paths were hashed before and after candidate gates, review, push, and CI; the sorted SHA-256 manifests are byte-for-byte identical.

Allowed preserved dirty paths:

- `CLAUDE.md`
- `advisor-plans/README.md`
- `AGENTS.md.bak-pre-gitnexus-uninstall`
- `CLAUDE.md.bak-pre-gitnexus-uninstall`
- `CONTEXT.md.bak-pre-issue-41`
- `docs/agents/`
- `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`
- `docs/decisions/2026-08-09-operator-session-boundary.md`
- `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`
- `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`
- `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md`

No stash, reset, clean, relocation, overwrite, rebase, amend, force push, tag, or deploy was used during closure.

## Cleanup

The design branch and worktree are intentionally preserved through evidence push, final CI, and independent verification. No unrelated worktree, branch, container, service, or user file was removed.
