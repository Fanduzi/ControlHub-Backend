# Phase 38X-2 Operator Boundary Hardening Parent Release Evidence

Date: 2026-08-16
Issue: [#20, `38X-2: Complete operator authentication boundary hardening`](https://github.com/Fanduzi/ControlHub-Backend/issues/20)

This is the independent parent re-verification of Issue #20 after every child
delivery published. It verifies the four hardening contracts on the current
published refs with fresh runs and fresh read-only reviews, records the child
publication chain, and documents the parent-level verdict. Per the parent
specification's delivery order, this verification is the precondition for the
Issue #7 closure step; Issue #7 remains open.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Backend repository | `Fanduzi/ControlHub-Backend` |
| Backend published head verified (`origin/main`) | `cb8f9d9121fc754a7a7b26c5f38c679e89454030` |
| Task branch (new, unique) | `issue-20-parent-reverify-20260816-20260816-120523` |
| Task worktree (new, unique) | `/Users/fan/GolangProjects/ControlHub-wt-issue-20-rverify-20260816-120523` |
| Frontend repository | `Fanduzi/ControlHub-Frontend`, `origin/main` = `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3` |
| Frontend reference worktree (read-only) | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-32-r3-reference-20260816-101945` (detached at `d6bc752`) |
| Evidence commit | this commit (docs) |

## Child Publication Chain

All children of #20 are CLOSED with tracked evidence on main:

| Child | Issue | Tracked evidence |
| --- | --- | --- |
| 38X-2A freshness + 38X-2B audit + 38X-2C BFF config + 38X-2D Argon2id | #21-#24 | `2026-08-13-phase-38x-2-auth-hardening-release-evidence.md` |
| 38X-2E re-verification | #25 | `2026-08-15-phase-38x-2e-reverify-operator-access-boundary-release-evidence.md` |
| 38X-2F exact 8h expiry | #26 | `2026-08-14-phase-38x-2f-eight-hour-expiry-boundary-release-evidence.md` |
| 38X-2G Argon2id budget | #27 | `2026-08-14-phase-38x-2g-argon2id-verification-budget-release-evidence.md` |
| 38X-2H fail-open role-denied 403 | #28 | `2026-08-15-phase-38x-2h-role-denied-audit-fail-open-release-evidence.md` |
| 38X-2I bounded audit + cutover (a/b/c + follow-up) | #29, #30-#33 | `2026-08-15-phase-38x-2i-a-…`, `2i-b-…`, `2026-08-16-phase-38x-2i-c-…`, `2026-08-16-phase-38x-2i-follow-up-…` |

Issue states verified: #21-#33 all CLOSED; #20 OPEN (this delivery); #7 OPEN.

## Contract Verification On The Current Published Refs

### Contract 1 — Fixed eight-hour freshness (backend + Operator Session)

- `internal/api/auth_middleware.go`: `MaxQueryTokenAge = 8 * time.Hour` fixed
  constant; both middleware factories reject at `>= MaxQueryTokenAge`
  (exclusive bound, #26); no configuration path exists.
- `QUERY_EXECUTION_TOKEN_MAX_AGE` is removed as a functional setting: the only
  remaining references are the startup rejection (`internal/config/config.go`
  returns `ErrQueryExecutionTokenMaxAgeRejected` when the variable is set,
  empty or not) and its tests.
- Operator Sessions share the bound: frontend
  `lib/operator-session/constants.ts` `SESSION_MAX_AGE_SECONDS = 8 * 60 * 60`.
- Fresh proofs: `TestEightHourOldBearerIsExpired`,
  `TestAuthenticatedActorRejectsTokenOlderThanEightHours`,
  `TestFreshQueryActorEnforcesFixedEightHourConstant`,
  `TestAuthenticatedActorStaleRejectionEmitsAuthBearerRejected`,
  `TestGeneric401EquivalenceAcrossAuthFailures`,
  `TestLoadRejectsQueryExecutionTokenMaxAgeEnvVar`,
  `TestLoadRejectsExplicitlyEmptyQueryExecutionTokenMaxAgeEnvVar` — all PASS
  (fresh runs, 8/8 in the focused set).

### Contract 2 — Minimal privacy-safe audit taxonomy, fail-open

- Emit sites use exactly `auth.login`/`succeeded|rejected`, `auth.bearer`/
  `rejected`, `auth.authorization`/`denied` with fixed actor/target rules;
  successful ordinary protected reads are not audited.
- Fail-open persistence: the MySQL emitter logs only the fixed taxonomy label
  and a fixed error class (never the error value, DSN, or driver internals),
  increments the fixed persistence-failure counter, and never changes a
  decision. The bounded-audit delivery (#29/#30-#33) caps untrusted Bearer
  rejection persistence at 60/min per process with a dimensionless
  suppression counter.
- The administrator-only metrics surface exposes exactly
  `authAuditPersistenceFailures` and `authAuditSuppressedRejections`.
- Fresh proofs: `TestMissingAuthorizationEmitsNoAuditEvent`,
  `TestAuthAuditMetricsNoLeak`, `TestOpsAuthAuditMetricsOperatorBoundary`,
  `TestAuthenticatedActorStaleRejectionEmitsAuthBearerRejected` (unit); the
  real-MySQL focused suite (11 auth-audit tests incl.
  `TestAuthAudit_FailOpenPreservesRoleDenied403`,
  `TestAuthAudit_BoundedUntrustedBearerPersistence`,
  `TestAuthAudit_NoProhibitedValues`) and 5 cutover import tests — all PASS
  (fresh runs, 16/16).

### Contract 3 — Production Console BFF configuration

- `lib/operator-session/config.ts` (frontend `d6bc752`): fail-closed loader.
  Sealing keys must match the base64 shape and decode to exactly 32 bytes,
  with a structural repeating-pattern guard; the Console Origin must be a
  single origin without path/credentials/query/hash, and production
  (`NODE_ENV=production`) requires exactly one HTTPS origin and Secure
  cookies. The explicit local-development exception is implemented as the
  documented non-production path (test-named "accepts HTTP origin only in
  non-production (local development)"); production remains fail-closed.
- Fresh proofs: `tests/lib/operator-session-config.test.ts`,
  `tests/lib/operator-session-seal.test.ts`, `tests/app/proxy.test.ts`,
  `tests/app/api/operator-session-route.test.ts` — 57 PASS / 0 FAIL (fresh
  vitest run). Chromium BFF evidence: `operator-session.spec.ts` 13 PASS /
  0 FAIL (fresh run on the same refs during the #32 re-verification);
  frontend CI run 136 on `d6bc752` green (release-local + release-e2e).

### Contract 4 — Argon2id gradual migration

- `internal/service/password_hasher.go`: Argon2id with 64 MiB memory, time
  cost 3, parallelism 1; legacy SHA-256 accepted only for successful-login
  migration (atomic CAS upgrade, no new legacy writes, upgrade failure
  rejects login); resets write Argon2id only.
- Non-identity-bearing legacy-hash count: admin-only
  `GET /admin/legacy-hash-count` returns a count only.
- Fresh proofs: `TestLoginLegacySHA256UpgradesToArgon2id`,
  `TestLoginFailedDoesNotUpgradeHash`, `TestLoginCASRejectsStaleUpgrade`,
  `TestLoginUpgradeFailureRejectsLogin`, `TestLoginArgon2idPasswordWorks`,
  `TestLegacyHashCountReturnsNonIdentityCount` (6/6); legacy-count handler
  matrix (5/5: admin count, auth matrix, no hash leak, unauth/viewer
  rejected); Argon2id verification-budget gate PASS (fresh run: median
  100.1 ms, p95 101.8 ms vs the 250/300 ms budget on this host).

## Fresh Gates (backend at `cb8f9d9`, zero failure / zero skip)

`git diff --check` PASS · `gofmt -l` clean · `go vet ./...` PASS · `go build
./...` PASS · `go test -count=1 ./...` 1742 PASS / 0 FAIL (14 packages) ·
`go test -race -count=1 ./...` 1742 PASS / 0 FAIL · OpenAPI validation PASS ·
`go test -tags=integration -count=1 -run '^Test[^O]' ./internal/integration`
234 PASS / 0 FAIL (Testcontainers MySQL) · Schemathesis fuzz PASS · Argon2id
budget gate PASS · three-level docs PASS (all published ranges carry complete
L3 headers and in-range L2 README updates; the #33 OpenAPI README fix holds) ·
forbidden-value scan clean (responses, audit rows, metrics, logs, test
output, artifacts, evidence).

## Independent Review (fresh read-only reviewers, current published refs)

- **Standards: P1=0, P2=0, P3=5.** Localized maintainability/documentation
  nits only: duplicated middleware factories (accepted follow-up), exported
  `Reset` test seam (labeled), configurable-looking `NewBearerRejectBudget`
  constructor (test seam), a stale dependency comment on `QueryExecutionAuth`,
  and the unused `time` entry in the `router.go` L3 header.
- **Spec: P1=0, P2=0, P3=0.** All four contracts and their tests are present
  and match the published spec; every child evidence record is tracked on
  main.
- **Security: P1=0, P2=0 (adjudicated), P3=2.** Two hardening suggestions
  were adjudicated against the documented, tested, and previously
  independently re-verified contract: (a) the synchronous fail-open audit
  write can delay a response if the store stalls — the accepted 2026-08-12
  ADR frames fail-open as the availability tradeoff, failures never change
  decisions, and #28 proves the broken-store 403 outcome; a bounded write
  deadline is a future hardening option; (b) the local-development HTTP
  exception keys on non-production rather than a literal loopback — this is
  the explicitly tested design ("accepts HTTP origin only in non-production"),
  production stays fail-closed (HTTPS + Secure cookies + validated keys), and
  #25's fresh three-axis re-verification passed it; tightening to explicit
  loopback detection is a future hardening option. No P1/P2 remains against
  the published contract.

## Root WIP Preservation

Backend root was on `main` at `cb8f9d9` before any operation; the root WIP
manifest (tracked-modified, staged, NUL-safe status, untracked) is unchanged
from the prior phase records: modified `CLAUDE.md`, `advisor-plans/README.md`;
untracked bak files, `CONTEXT.md`, `docs/agents/`, two decision docs, three
superpowers plan/spec docs; staged empty. Frontend root is clean at
`d6bc752`. No stash, restore, reset, clean, or amend occurred.

## Cleanup And Issue Safety

- Disposable verification resources from the re-verification runs (isolated
  MySQL container, backend server process, per-run fixture credentials) were
  removed after use; fixture passwords were never printed.
- This evidence commit is docs-only, carries no AI co-author, and per the
  parent closure protocol is merged fast-forward and pushed only after the
  independent verifier passes; the final SHA and CI are recorded in the
  closing comment.
- Issue #20 closes with a factual comment (final SHA, evidence path, CI URL).
  Issue #7 remains open for its own authorized closure step, which this
  verification unblocks. #8-#11 and #16-#17 are unrelated open 38X issues and
  are untouched.
