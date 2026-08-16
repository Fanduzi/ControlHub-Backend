# Phase 38X-1 Operator Access Boundary Parent Release Evidence

Date: 2026-08-16
Issue: [#7, `38X-1: Define and enforce the operator authentication boundary`](https://github.com/Fanduzi/ControlHub-Backend/issues/7)

This is the final parent closure evidence for the Operator Access Boundary.
The original 2026-08-12 parent verification (historical branch, `1f3f5e1`)
was blocked with P1/P2 findings and never published on main; it spawned the
38X-2 hardening phase (#20). All child deliveries from both phases are now
published, independently verified, and closed on the tracker, and this record
documents the boundary contract on the current published refs with fresh
runs and fresh read-only reviews. Issue #7 is the final phase closure.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Backend repository | `Fanduzi/ControlHub-Backend` |
| Backend published head verified (`origin/main`) | `795d38c958c68df9193c26317d1829d149d3a31f` |
| Task branch (new, unique) | `issue-7-parent-closure-20260816-20260816-134859` |
| Task worktree (new, unique) | `/Users/fan/GolangProjects/ControlHub-wt-issue-7-closure-final-20260816-134859` |
| Frontend repository | `Fanduzi/ControlHub-Frontend`, `origin/main` = `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3` |
| Historical parent verification | `2026-08-12-phase-38x-1-operator-access-boundary-parent-release-evidence.md` (branch `1f3f5e1`, blocked P1/P2; superseded by the 38X-2 hardening and this record) |
| Evidence commit | this commit (docs) |

## Child Publication Chain (all closed)

| Child | Issue | State | Tracked evidence |
| --- | --- | --- | --- |
| 38X-1A authorization version | #12 | CLOSED | `2026-08-09-phase-38x-1a-authorization-version-release-evidence.md` |
| 38X-1B operator boundary | #13 | CLOSED | `2026-08-11-phase-38x-1b-operator-boundary-release-evidence.md` |
| 38X-1C/1D BFF sessions | #14/#15 | CLOSED | frontend evidence + `operator-session.spec.ts` (BFF Chromium) |
| 38X-1E immediate invalidation | #16 | CLOSED | invalidation tests + real-MySQL authorization-version integration + BFF Chromium |
| 38X-1F release verification | #17 | CLOSED | 2e/2i-c/#20 evidence chain |
| 38X-1B closure evidence / 1D fixtures | #18/#19 | CLOSED | `2026-08-12-phase-38x-1d-e2e-fixture-provisioning-release-evidence.md` |
| 38X-2 hardening (parent) | #20 | CLOSED | `2026-08-16-phase-38x-2-operator-boundary-hardening-parent-release-evidence.md` |

Issue states verified on the tracker: #12-#20 all CLOSED.

## Boundary Contract On The Current Published Refs

The Operator Access Boundary was re-verified fresh on the current head as part
of the #20 parent re-verification (2026-08-16) and is corroborated here:

- **Access matrix**: anonymous limited to health/login/docs; authenticated
  editors read Inventory and use granted governed-query capabilities; only
  admins mutate Inventory or read audit events. Proved by
  `operator_access_boundary_test.go` and the router suite (fresh, 0 FAIL).
- **Controlled outcomes**: one generic 401 for missing/invalid/expired/
  revoked/disabled credentials; distinct 403 for valid actors lacking a role;
  no internals disclosed (`TestGeneric401EquivalenceAcrossAuthFailures`,
  `TestOpsAuthAuditMetricsOperatorBoundary`).
- **Authorization Version**: current server-owned active state and role,
  never a token-embedded role; disablement/role change/password reset
  invalidate immediately (`TestRoleChangeInvalidatesPriorCredential`,
  `TestDisablementInvalidatesPriorCredential`,
  `TestPasswordResetInvalidatesPriorCredential`,
  `TestVerifyTokenUsesCurrentRoleNotEmbeddedClaim`; real-MySQL
  `auth_authorization_version_test.go`).
- **Fixed freshness**: 8-hour exclusive bound for credentials and Operator
  Sessions; obsolete freshness setting rejected at startup (contract 1 of
  #20).
- **BFF-only browser boundary**: HttpOnly sealed Operator Session, no Backend
  Bearer in browser storage/DOM/readable cookies, client `Authorization`
  rejected, unsafe Origins rejected, logout clears the session (13/13 BFF
  Chromium, 176/176 full E2E, fresh).
- **Audit taxonomy and fail-open**: minimal fixed events, fail-open
  persistence with safe metrics/logs, bounded untrusted-Bearer persistence
  (contract 2 of #20; real-MySQL proofs).
- **Production BFF configuration**: fail-closed keys and single HTTPS Origin
  (contract 3 of #20; 57/57 config tests).
- **Password hardening**: Argon2id migration with verified budget (contract 4
  of #20; budget median ~100 ms).
- **OpenAPI**: public/protected declarations validated
  (`TestOpenAPIYAMLIsValid` PASS, Schemathesis fuzz PASS).

## Fresh Gates (backend at `795d38c`, zero failure / zero skip)

`git diff --check` PASS · `gofmt -l` clean · `go vet ./...` PASS · `go build
./...` PASS · `go test -count=1 ./...` 1742 PASS / 0 FAIL (14 packages) ·
`go test -race -count=1 ./...` 1742 PASS / 0 FAIL · OpenAPI validation PASS ·
integration 234 PASS / 0 FAIL (real MySQL) · Schemathesis fuzz PASS · Argon2id
budget PASS · three-level docs PASS · forbidden-value scan clean. Frontend:
BFF Chromium 13/13 and full `release:e2e` 176/176 (isolated fixture
environment, frontend `d6bc752`), frontend CI run 136 green.

## Independent Review

Fresh read-only Standards/Spec/Security reviews of the current published refs
(reported in the #20 parent evidence) conclude P1=0, P2=0 across the four
hardening contracts, with residual P3s documented there (maintainability nits
and two adjudicated hardening suggestions). The historical 2026-08-12 parent
blocking findings are resolved by the published 38X-2 deliveries (eight-hour
freshness, minimal privacy-safe audit, production BFF configuration, Argon2id
migration), each with its own tracked evidence and independent verification.

## Root WIP Preservation

Backend root was on `main` at `795d38c` before any operation; the root WIP
manifest (tracked-modified, staged, NUL-safe status, untracked) is unchanged
from the phase records: modified `CLAUDE.md`, `advisor-plans/README.md`;
untracked bak files, `CONTEXT.md`, `docs/agents/`, two decision docs, three
superpowers plan/spec docs; staged empty. Frontend root is clean at
`d6bc752`. No stash, restore, reset, clean, or amend occurred.

## Cleanup And Issue Safety

- Disposable verification resources from the re-verification runs were
  removed after use; per-run fixture passwords were never printed.
- This evidence commit is docs-only, carries no AI co-author, and is merged
  fast-forward and pushed only after the independent verifier passes; the
  final SHA and CI are recorded in the closing comment.
- Issue #7 closes with a factual comment (final SHA, evidence path, CI URL).
  No other issue is touched by this closure.
