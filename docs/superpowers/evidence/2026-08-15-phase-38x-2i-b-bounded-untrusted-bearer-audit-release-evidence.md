# Phase 38X-2I-B Bounded Untrusted Bearer Audit Release Evidence

Date: 2026-08-15
Issue: [#31, `38X-2I-B: Bound untrusted Bearer audit persistence`](https://github.com/Fanduzi/ControlHub-Backend/issues/31)

This backend-only release bounds persistence of untrusted Bearer rejection
audit events while keeping Controlled Authorization Errors unchanged. A
request with no `Authorization` header returns the existing generic 401 with
no audit event (absence of a credential, not a rejected supplied credential).
A supplied but untrusted Backend Bearer Credential may persist
`auth.bearer/rejected` at most 60 events per minute per server process via a
fixed, race-safe, process-shared budget; after exhaustion the response stays
the same 401, the handler still does not execute, no per-attempt row or
detailed log is written, and only a safe non-identity-bearing suppression
counter increments on the existing administrator-only auth-audit metrics
surface. Verified-actor role denials keep their unbounded
`auth.authorization/denied` audit behavior. No frontend, cutover, successful-
read auditing, role-denial taxonomy, governed-query, password-migration, or
retention changes; no new dependency.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base and preflight `origin/main` | `c96c53913219b0d1097a74f0165f1928e7858f59` |
| Task branch | `issue-31-bounded-bearer-audit-20260815-210053` |
| Task worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-31-20260815-210053` |
| Product commits | `7f8d7fcc80fa841c0fefdcf14d852a4600fb0572` (`feat(auth): bound untrusted Bearer audit persistence to 60/min per process`) and `0ccdbe632873b250bf396d524d67138cf7f3339e` (`fix(auth): make untrusted-Bearer audit budget per server process, not per router`) |
| Merged/pushed `main` | `0ccdbe632873b250bf396d524d67138cf7f3339e` (product SHA; this evidence commit is the docs-only follow-up and does not self-reference) |
| Merge | Fast-forward only (`git merge --ff-only`), then normal `git push origin main` (`c96c539..0ccdbe6`); no rebase, amend, force-push, tag, or deploy |
| Evidence commit | this commit (docs) |

Changed files (product range, `git diff c96c539..0ccdbe6 --stat`: 15 files,
+893/−60):

- `internal/service/auth_audit_emitter.go` — `BearerRejectBudget`
  (race-safe, fixed one-minute window anchored at the first event in the
  window, `Reset` test seam) and the `ProcessBearerRejectBudget` singleton
  shared by every router in the process; `BoundedAuthAuditEmitter` decorator
  budgets only `auth.bearer`/`rejected` events with no verified actor
- `internal/api/auth_middleware.go` — missing `Authorization` returns the
  generic 401 with no audit event
- `internal/api/router.go` — `NewRouter` wires
  `service.ProcessBearerRejectBudget` (single production call site)
- `internal/api/ops_handler.go` — metrics surface adds
  `authAuditSuppressedRejections` beside the existing persistence-failure
  counter
- `internal/openapi/openapi.yaml` — metrics response schema documents the new
  field
- Tests: `internal/api/auth_middleware_test.go`,
  `internal/api/bounded_bearer_audit_test.go`,
  `internal/service/auth_audit_emitter_budget_test.go`,
  `internal/integration/auth_audit_emitter_test.go`,
  `internal/repository/mysql/auth_audit_emitter_test.go` (L3 header added)
- Docs: `docs/decisions/2026-08-15-phase-38x-2i-b-bounded-untrusted-bearer-audit.md`
  (new ADR), L2 READMEs for api/service/integration/mysql

## Behavior Proved

- **Missing Authorization**: generic 401, handler does not run, zero
  `auth.bearer/rejected` events (router seam and real MySQL: row delta 0).
- **Supplied untrusted Bearer within budget**: controlled 401 plus the fixed
  `auth.bearer/rejected` event with no actor.
- **Budget exhaustion**: the 61st rejection in a minute keeps the same 401
  and handler non-execution, writes no row and no per-request log, and
  increments only the suppression counter.
- **Per server process, not per router**: `TestBoundedBearerAudit_TwoRoutersShareOneProcessBudget`
  proves 40 + 40 forged requests across two routers in one process persist
  exactly 60 events; the pre-fix per-router implementation admitted 80 (this
  was the single review P2, fixed in `0ccdbe6`).
- **Concurrency**: `TestBoundedAuthAuditEmitter_ConcurrentConsumptionNeverExceedsLimit`
  (8 goroutines, 160 attempts → exactly 60 admitted) and full `-race` suite
  green.
- **Window reset**: budget rolls forward on the fixed one-minute window
  (`TestBoundedAuthAuditEmitter_WindowResetProvesTheNextMinuteAllowsEventsAgain`).
- **Unbounded role denial**: verified-actor role denials keep persisting
  `auth.authorization/denied` after budget exhaustion (router seam and real
  MySQL: editor PUT stays 403, denied row delta 1).
- **Metrics safety**: `/ops/auth-audit-metrics` (admin-only) returns exactly
  `authAuditPersistenceFailures` and `authAuditSuppressedRejections` — no
  identity, request, or credential material (`TestAuthAuditMetricsNoLeak`,
  `TestOpsAuthAuditMetricsOperatorBoundary`: anonymous 401 / editor 403 /
  admin 200).
- **Real MySQL budget proof**: 61 forged-token requests against
  Testcontainers MySQL 8.0 → exactly 60 persisted untrusted rejection rows,
  suppression counter delta 1.

## Real-MySQL Integration Evidence

`internal/integration/auth_audit_emitter_test.go` (existing harness, no new
test abstraction):

- **RED (baseline `c96c539`, before implementation):**
  `TestAuthAudit_MissingHeaderEmitsNoRow` failed (row delta 1, want 0) and
  `TestAuthAudit_BoundedUntrustedBearerPersistence` failed (row delta 61,
  want 60).
- **GREEN (candidate `0ccdbe6`):** all 11 `TestAuthAudit_*` tests PASS,
  including the 61-request budget test, missing-header zero-row proof,
  role-denial-after-exhaustion persistence, fail-open on DB error (401/403
  unchanged), freshness rejection with verified actor, and the
  no-prohibited-values scan over all `auth.%` rows.

## Candidate Gates

| Command | Result |
| --- | --- |
| `git diff --check` (candidate worktree, exact SHA) | PASS, exit 0 |
| `gofmt -l` on changed Go files | clean |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | 1742 PASS, 0 FAIL (14 packages) |
| `go test -race -count=1 ./...` | 1742 PASS, 0 FAIL (14 packages) |
| `go test ./internal/openapi -run TestOpenAPIYAMLIsValid` | PASS |
| `go test -tags=integration -count=1 -run '^Test[^O]' ./internal/integration` | 234 PASS, 0 FAIL |
| `go test -tags=integration -count=1 -run TestOpenAPIFuzz ./internal/integration` | PASS (Schemathesis; all checks passed) |
| Three-level docs (L3 headers + L2 READMEs in change set + `check_three_level_doc.sh`) | PASS, 0 errors |
| Sensitive-value scan of the change set | clean (no credentials, DSNs, request values, or unresolved markers) |

Merged-root re-run (after `git merge --ff-only` + push, from the root
worktree at the merged SHA): `go test -count=1 ./...` and
`go test -race -count=1 ./...` both 1742 PASS / 0 FAIL; `git diff --check`
PASS.

## CI On Product SHA

CI run
[31894885740](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31894885740)
on merged `main` at head `0ccdbe632873b250bf396d524d67138cf7f3339e`:
`status=completed`, `conclusion=success`.

- `release-local-gates` job
  [95036561699](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31894885740/job/95036561699)
  — `success`.
- `release-docker-gates` job
  [95036561697](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31894885740/job/95036561697)
  — `success` (Testcontainers MySQL integration + Schemathesis fuzz).

## Independent Review

Two-axis `/code-review` of the candidate range (`c96c539..0ccdbe6`) with
fresh-context read-only reviewers, plus a dedicated re-review of the P2 fix
delta:

- **Standards axis (initial):** no hard violations; mild judgment calls
  (limit/window constructor params consistent with the repo's injected-clock
  precedent; the repeated 61-iteration loop was extracted into a shared
  `exhaustUntrustedBearerBudget` helper; taxonomy strings match the existing
  inline style).
- **Spec axis (initial):** no missing requirements, no scope creep.
  `TestAuthAudit_AuthorizationDenied` was actor-scoped because the new shared
  coverage legitimately adds role-denial rows to the shared test database.
- **P2 fix re-review (delta `7f8d7fc..0ccdbe6`):** verdict P1=0, P2=0 — the
  budget is now genuinely per server process (one shared
  `ProcessBearerRejectBudget`, single production wiring point), the
  two-router regression test would have failed on the pre-fix code (80 vs 60
  events), the global suppression counter maps 1:1 to the shared budget, and
  the ADR wording matches the implementation.
- Final: **APPROVE, P1=0, P2=0**. Accepted P3s: `BearerRejectBudget.Reset`
  is an exported test seam on a production type (required cross-package by
  the router tests; comment labels it as such); the two-router test's
  40-iteration loop duplicates the 61-iteration helper shape by design;
  multi-process deployments each carry their own 60/min budget, which is the
  specified per-process contract with no distributed coordination.

## Root WIP Preservation

Before fetch or repository mutation, root was on `main` at
`c96c53913219b0d1097a74f0165f1928e7858f59`. The root WIP manifest was
captured (tracked-modified, staged, NUL-safe `git status --porcelain -z`,
untracked) before any change and re-verified identical after merge, push, and
evidence commit:

| Status | Path |
| --- | --- |
| modified | `CLAUDE.md`, `advisor-plans/README.md` |
| untracked | `AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md` |
| untracked | `docs/agents/domain.md`, `docs/agents/issue-tracker.md`, `docs/agents/triage-labels.md` |
| untracked | `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`, `docs/decisions/2026-08-09-operator-session-boundary.md` |
| untracked | `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`, `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`, `docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md` |

None of these paths overlaps the product diff. The root worktree was
observe-only: no stash, restore, reset, clean, relocate, or overwrite
occurred. Existing root services, Docker containers, fixtures, historical
worktrees, and branches were not started, stopped, or modified.

## Cleanup And Issue Safety

- At the time this evidence was committed, no task resource had been deleted:
  the task worktree and the local candidate branch are retained for the
  mandated post-evidence independent verification. Per the authorized closure
  protocol, they are deleted only after the final CI run and the independent
  verification both pass; the deletion receipt is recorded in the final
  report. All other worktrees, branches, services, fixtures, and root WIP are
  preserved.
- Issue #31 closes after the final CI run and the independent verification
  pass, with a factual comment (final SHA, evidence path, CI URL). #32, #29,
  #20, and #7 must remain open.
