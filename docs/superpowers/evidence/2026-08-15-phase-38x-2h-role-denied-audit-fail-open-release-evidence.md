# Phase 38X-2H Role-Denied Audit Fail-Open Release Evidence

Date: 2026-08-15
Issue: [#28, `38X-2H: Prove fail-open audit persistence preserves role-denied 403`](https://github.com/Fanduzi/ControlHub-Backend/issues/28)

This backend-only release closes the remaining gap in the fail-open audit
contract (parent #20, blocker of #25): a real-MySQL integration regression
test proves that when auth-audit persistence fails, a valid editor requesting
an admin-only protected operation still receives the controlled 403 and the
protected handler does not execute. The release is test-only; no production
authorization logic, audit taxonomy, fail-open behavior, response contract,
or audit retention changes.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base and preflight `origin/main` | `3d0b09b55278c740167b836c5d623535dc05e552` |
| Task branch | `issue-28-auth-audit-mysql-20260815-104859` |
| Task worktree | `/Users/fan/GolangProjects/ControlHub-wt-issue-28-20260815-104859` |
| Product commit | `2308da5da491e22f5a233eb34e94ae5dd0b94913` (`test: prove fail-open audit persistence preserves role-denied 403`) |
| Merge | Fast-forward only (`git merge --ff-only`), then normal `git push origin main` (`3d0b09b..2308da5`); no rebase, amend, force-push, tag, or deploy |
| Evidence commit | this commit (docs) |

Changed files (product commit):

- `internal/integration/auth_audit_emitter_test.go` (new
  `TestAuthAudit_FailOpenPreservesRoleDenied403`, header comment update)
- `internal/integration/README.md` (file table row)

No production code changed; confirmed by the independent review
(`git diff --name-only 3d0b09b..2308da5` lists exactly the two files above).

## What The New Test Proves

`TestAuthAudit_FailOpenPreservesRoleDenied403` runs against real MySQL via the
repository's Testcontainers integration harness and proves the role-denied
half of the fail-open contract (the login-success and Bearer-rejection halves
were already proven by the pre-existing fail-open test):

- **Valid editor, admin-only route, known target.** A valid editor logs in
  and sends `PATCH /resources/{id}` — an admin-only route — against a seeded
  resource with a known, non-empty `display_name` baseline.
- **Broken-DB emitter injection.** The existing `NewAuthAuditEmitter` seam is
  backed by a closed connection (identical injection to the pre-existing
  fail-open test; no second audit abstraction introduced), so every audit
  INSERT fails while the emitter stays fail-open.
- **Controlled 403, no mutation.** The externally visible response remains
  exactly the fixed controlled payload (`{"error":"forbidden","message":
  "admin role is required"}`), and the protected handler does not execute:
  a real MySQL-backed resource service is wired, so an executed handler would
  genuinely persist the request marker into `display_name`; the row is
  re-read after the denied PATCH and must be byte-identical to the baseline.
- **Safe counter shape.** The fixed-category operational counter
  `AuthAuditPersistenceFailures` increments by exactly one for the denied
  authorization emit. The snapshot is taken after login (login's own failed
  emit is excluded), so the delta isolates precisely the denied emit; zero or
  duplicated emits fail the test.
- **Safe log shape.** Captured fail-open diagnostics must each match exactly
  the fixed safe taxonomy shape — timestamp plus
  `auth_audit_emit_fail event=auth.authorization result=denied
  error_class=audit_persistence_failure` — enforced by a regexp over the
  fixed event/result taxonomy, plus a case-insensitive scan for identity,
  credential, session, DSN, and request-marker values. Failure messages echo
  neither bodies nor diagnostics; a leaking line can never re-leak into test
  output.

The test fails on every broken-contract scenario: fail-closed 5xx, handler
execution despite 403 (mutated row), leaked value in response or diagnostics,
removed or duplicated emit, dead seam, or logger swap (safe-shape regexp /
captured-count guard).

## Candidate Gates

| Command | Result |
| --- | --- |
| `git diff --check 3d0b09b..2308da5` | PASS, exit 0 |
| `go vet -tags integration ./internal/integration/` | PASS, exit 0 |
| CI run `31869357881` on merged `main` at head `2308da5` | PASS; `release-local-gates` and `release-docker-gates` both `success` |

Full CI gate totals (run
[31869357881](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31869357881),
head SHA `2308da5da491e22f5a233eb34e94ae5dd0b94913`):

- `release-local-gates` job
  [94975460729](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31869357881/job/94975460729)
  — `success`:
  - `go test -count=1 ./...` — 13 packages `ok`, 0 failures;
  - `TestOpenAPIYAMLIsValid` PASS;
  - dedicated Argon2id verification-budget gate PASS
    (`TestArgon2idVerificationBudget`), budget evidence uploaded.
- `release-docker-gates` job
  [94975460707](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31869357881/job/94975460707)
  — `success`:
  - `make test-integration` against disposable Testcontainers MySQL —
    232 integration tests PASS, 0 FAIL — including
    `TestAuthAudit_FailOpenPreservesRoleDenied403` PASS (0.11s) and the
    OpenAPI fuzz run in the same gate.

## Independent Review

A fresh-context, read-only review of the candidate range
(`3d0b09b..2308da5`) against Issue #28's acceptance criteria:

- **Verdict: PASS.** P1 `0`, P2 `0`, P3 `3` (nits only: the closed-DB handle
  ignores its open error — pre-existing pattern; the global log redirect and
  counter delta assume sequential package execution — no `t.Parallel` exists
  in `internal/integration`; the 403-body failure path reports lengths only —
  deliberate privacy choice).
- All five acceptance criteria verified with code-level evidence: valid
  editor + admin-only route with known target; failing persistence seam does
  not change the authorization decision (controlled 403, handler does not
  execute); fail-open observability stays safe (no identity, credential,
  session, DSN, request value, or failure detail in responses, logs,
  diagnostics, or test output); `auth.login` / `auth.bearer` /
  `auth.authorization` taxonomy and normal audit behavior unchanged; all
  out-of-scope items untouched.

## Root WIP Preservation

Before fetch or repository mutation, root was on `main` at
`3d0b09b55278c740167b836c5d623535dc05e552` with nine registered worktrees.
The root WIP manifest was captured to `/tmp/issue28-closure/`
(`wip-tracked.patch`, `wip-staged.patch`, `wip-status.nul`,
`wip-untracked.txt`, `manifest-sha256.txt`) before any change and is
byte-identical afterwards (re-verified after merge and push):

| Status | Path |
| --- | --- |
| modified | `CLAUDE.md` |
| modified | `advisor-plans/README.md` |
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
- Issue #28 is open at evidence time and blocks #25. After this release
  passes independent verification, #28 closes with a factual comment
  (final SHA, evidence path, CI URL); #25, #20, and #7 must remain open.
