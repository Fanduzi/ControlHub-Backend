# Phase 38X-2I-C Bounded Audit And Cutover Re-verification Evidence

Date: 2026-08-16
Issue: [#32, `38X-2I-C: Re-verify bounded authentication audit and cutover compatibility`](https://github.com/Fanduzi/ControlHub-Backend/issues/32)

This is the independent re-verification of the published bounded
authentication-audit and nullable-cutover boundary delivered by #30 and #31,
executed on the latest published backend ref (which now includes the #33
docs-only OpenAPI README follow-up). Every acceptance criterion was re-proven
with fresh runs, all release gates re-ran with zero failures and zero skipped
tests, and the #30/#31/#33 publication claims (exact SHAs, CI runs, job
outcomes, tracked evidence, issue states, ADR byte identity) all match the
published refs. This document supersedes the earlier #32 evidence candidate
that was based on the pre-#33 head; the old candidate commit is retained only
as a historical record and was neither merged nor used for any claim here.

## Candidate Distinction And Exact Refs

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Pre-verification `origin/main` (after `git fetch origin`) | `2f5fdbda651a6d09512aea45e193efabaee04ab2` |
| Task branch (new, unique) | `issue-32-r3-reverify-20260816-20260816-101945` |
| Task worktree (new, unique) | `/Users/fan/GolangProjects/ControlHub-wt-issue-32-r3-20260816-101945` |
| Published head verified | `2f5fdbda651a6d09512aea45e193efabaee04ab2` |
| Old #32 candidate (historical only, superseded) | branch `issue-32-reverify-20260816-20260816-003741` at `ca13519`, based on the pre-#33 head `c72506f`; never rebased, amended, merged, or published |
| Frontend repository | `Fanduzi/ControlHub-Frontend`, `origin/main` = `d6bc7520000a14841bb4d2cd117c4f0bacc8fbf3` |
| Frontend reference worktree (read-only) | `/Users/fan/GolangProjects/ControlHub-Frontend-wt-issue-32-r3-reference-20260816-101945` (detached at `d6bc752`) |

## #30/#31/#33 Publication Chain Verification

| Claim | Verification result |
| --- | --- |
| Commit chain `877381f..2f5fdbd` | exactly the 7 expected commits: `ffaeead` (#30 product), `c96c539` (#30 evidence), `7f8d7fc` + `0ccdbe6` (#31 product), `394afda` (#31 evidence), `c72506f` (#33 product), `2f5fdbd` (#33 evidence) |
| #33 product commit `c72506f` scope | exactly `internal/openapi/README.md` (+4/−1), docs-only, no AI co-author |
| #33 evidence `2f5fdbd` tracked and readable | `git show 2f5fdbd:docs/superpowers/evidence/2026-08-16-phase-38x-2i-follow-up-openapi-readme-release-evidence.md` succeeds; present in the tracked evidence directory on main |
| #33 OpenAPI README alignment fixed | `internal/openapi/README.md` at `2f5fdbd` documents the admin-only auth-audit metrics responses and the exact two counter fields; matches `openapi.yaml` (required list) and the handler response shape |
| CI runs | 31883490781 (head `ffaeead`), 31883942596 (head `c96c539`), 31894885740 (head `0ccdbe6`), 31895130369 (head `394afda`), 31913058167 (head `c72506f`), 31921244166 (head `2f5fdbd`) — all `status=completed`, `conclusion=success`, head SHAs match; `release-local-gates` and `release-docker-gates` success on the #33 product and final runs |
| ADR byte identity | `docs/decisions/2026-08-12-phase-38x-authentication-hardening-decisions.md` at the published head is byte-identical to the historical blob |
| Issue states | #33 CLOSED (with factual closure comment), #32/#29/#20/#7 OPEN (unchanged) |
| #32 evidence absent from main | the 2i-c evidence path is not on `origin/main` (this task's evidence commit is the docs-only candidate, not published, per the #32 protocol) |

## Independent Acceptance Matrix (fresh runs at `2f5fdbd`)

| Acceptance item | Proof (new executions) |
| --- | --- |
| Missing `Authorization` → generic 401, handler not executed, zero `auth.bearer/rejected` rows | router seam `TestMissingAuthorizationEmitsNoAuditEvent` (both middleware factories: 401, handler never runs, 0 events); real MySQL `TestAuthAudit_MissingHeaderEmitsNoRow` (row delta 0) |
| Supplied untrusted Bearer within budget → controlled 401 + fixed event | `TestBoundedBearerAudit_InvalidTokenEmitsWhileBudgetRemains`; real MySQL `TestAuthAudit_SuppliedInvalidBearerEmitsRejectedRow` |
| Fixed 60/min per server process; multiple routers in one process sum ≤ 60 | `TestBoundedAuthAuditEmitter_CapsUntrustedRejectionsPerWindow`; `TestBoundedBearerAudit_TwoRoutersShareOneProcessBudget` (40+40 across two routers → exactly 60); real MySQL `TestAuthAudit_BoundedUntrustedBearerPersistence` (61 forged requests → exactly 60 rows) |
| Budget exhaustion → same 401, handler non-execution, no per-attempt row/log, only safe suppression metric | `TestBoundedBearerAudit_SixtyFirstRejectionSuppressed` (61st suppressed, suppression counter delta 1); `TestBoundedBearerAudit_HandlerNeverExecutes`; live server log for the full E2E run is a single startup line |
| Concurrency: budget never exceeded under race | `TestBoundedAuthAuditEmitter_ConcurrentConsumptionNeverExceedsLimit` (8 goroutines, 160 attempts → exactly 60) plus full `-race` suite |
| Verified role denial keeps `auth.authorization/denied`, unbounded by the budget | `TestBoundedBearerAudit_RoleDenialUnaffectedByBudget` (exactly 60 rejections + 1 unbudgeted denial with actor); real MySQL: after exhaustion, editor PUT stays 403 and the denial row delta is 1 |
| Real-MySQL broken audit emitter: editor admin-only request stays controlled 403, no mutation, fail-open metric and safe log | `TestAuthAudit_FailOpenOnDBError`; `TestAuthAudit_FailOpenPreservesRoleDenied403` (real resource service wired: exact 403 payload, resource row unchanged, persistence-failure counter delta 1, captured diagnostics match the fixed taxonomy/error-class shape with zero prohibited values) |
| Cutover: NULL actor preserved as NULL with fixed metadata; non-NULL unknown actor fails loud with full-transaction rollback | `TestImportLegacyData_PreservesNullAuditActor` (NULL actor + NULL target, fixed event/result/created-at preserved, mapped row imports alongside); `TestImportLegacyData_UnknownAuditActorFailsLoudWithoutPartialImport` (loud mapping failure; all eleven business tables at count 0 after rollback) |
| Admin-only metrics surface exposes only the two safe fixed fields; anonymous 401 / editor 403 / admin 200 | `TestAuthAuditMetricsNoLeak` (exactly two keys), `TestOpsAuthAuditMetricsOperatorBoundary`; live server check: admin receives exactly the two counters, anonymous receives 401 |

## Release Gates (backend, fresh runs at `2f5fdbd`)

| Command | Result |
| --- | --- |
| `git diff --check` (worktree + full range `877381f..2f5fdbd`) | PASS, exit 0 |
| `gofmt -l` on changed Go files | clean |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | 1742 PASS, 0 FAIL (14 packages) |
| `go test -race -count=1 ./...` | 1742 PASS, 0 FAIL (14 packages) |
| `go test ./internal/openapi -run TestOpenAPIYAMLIsValid` | PASS |
| `go test -tags=integration -count=1 -run '^Test[^O]' ./internal/integration` | 234 PASS, 0 FAIL (Testcontainers MySQL 8.0) |
| `go test -tags=integration -count=1 -run TestOpenAPIFuzz ./internal/integration` | PASS (Schemathesis) |
| Argon2id verification-budget gate | PASS |
| Three-level docs (`check_three_level_doc.sh` + manual range verification: all 11 changed Go files carry complete `input/output/pos/note` headers, all six changed modules have in-range L2 README updates including openapi) | PASS, 0 errors |
| Focused acceptance suite (`TestAuthAudit_*`, `TestImportLegacyData_*`) | 16 PASS, 0 FAIL (11 auth-audit + 5 legacy-import) |

Zero skipped tests and zero failures across every gate. Two environment
transients were recorded with exact commands and re-run green: the first full
frontend E2E attempt in the prior verification pass hit an incomplete isolated
MySQL fixture seed (re-seeded to CI parity, same suite green), and one fuzz
attempt hit a transient Testcontainers reaper container-start failure (stray
container removed, same test green). Neither required any code change.

## Frontend BFF/Release Evidence (isolated environment, fresh runs)

Isolated fixture environment rebuilt for this verification: a dedicated
disposable MySQL 8.0 container seeded exactly per the frontend CI workflow
(metadata database, query fixture databases, pagination fixtures, read-only
query user), backend built from the published head `2f5fdbd` with goose
migrations applied, query-dev target seeded, per-run admin/editor fixture
operators provisioned by the backend CI-only bootstrap seam (random per-run
passwords, never printed), backend served on the loopback, and the frontend
reference checkout at `d6bc752` with the pinned Node runtime (22.22.0) and
installed Chromium.

| Check | Result |
| --- | --- |
| BFF focused Chromium (`e2e/operator-session.spec.ts`) | 13 PASS, 0 FAIL |
| Full `release:e2e` (smoke + interaction + all specs) | 176 PASS, 0 FAIL |

One environment transient: the first full `release:e2e` attempt of this
verification timed out waiting for the Playwright webServer because the host
machine was under extreme load (load average ~130 from unrelated processes);
the identical command re-ran green (176/176, 5.1m). No code or fixture change
was involved.

## Independent Review (fresh read-only reviewers on the published range)

- **Standards: P1=0, P2=0, P3=1.** The #31-era OpenAPI README P2 is resolved
  (README documents the suppression-counter contract matching the schema;
  the #33 follow-up is docs-only). One residual P3: `NewBearerRejectBudget`
  exposes fixed limit/window constructor parameters while production uses
  constants; this was the #31 delivery's accepted injected-clock test-seam
  design (the window-reset tests pass custom values) — recorded for a future
  simplification, not a defect of the published boundary.
- **Spec: P1=0, P2=0 (adjudicated), P3=2.** One finding was adjudicated from
  P2 to P3 with justification: the #33 evidence document on main references
  this 2i-c evidence path as the record of the #32 re-verification, and that
  file is absent from `origin/main` because the #32 evidence is, per this
  task's protocol, a docs-only candidate that is not pushed or merged. The
  claim it points to is verified true by this task's own fresh runs, and the
  reference closes when the #32 evidence lands in an authorized future step —
  the same pattern the #30 evidence records as accepted for the published ADR
  (references closing when documents land on main). The other P3 is the
  accepted dangling ADR references. No missing requirement or scope creep.
- **Security: P1=0, P2=0, P3=0.** The boundary is structurally sound: 401
  invariance across missing/untrusted/exhausted paths, exact budget predicate
  (auth.bearer/rejected/nil actor only), process-shared race-safe budget,
  dimensionless suppression telemetry, two-field admin-only metrics surface,
  transactional NULL/unknown cutover, and forbidden-value hygiene.

## Forbidden-Value Scan

Responses, audit rows, metrics, logs, test output, artifacts, and this
evidence were scanned for emails, passwords, hashes, Bearer credential
values, session/cookie material, keys, DSNs, request values, IP addresses,
User-Agents, and internal failure reasons: all clean. Live audit rows carry
only fixed event/result taxonomy with numeric or absent actor/target ids; the
metrics response exposes exactly the two fixed counters; the live server log
for the full E2E run is a single startup line; all gate logs, the Schemathesis
JUnit artifact, and Playwright artifacts are clean. Test-source fixtures
(synthetic token values, test-only secrets, `.example.com` addresses) are
inputs, not outputs, and never appear in responses, rows, metrics, logs, or
evidence.

## Root WIP Preservation

Backend root was on `main` at `2f5fdbd` (== `origin/main`) before any
operation. The root WIP manifest was captured (tracked-modified, staged,
NUL-safe `git status --porcelain -z`, untracked) before fetch and re-verified
byte-identical throughout: modified `CLAUDE.md`, `advisor-plans/README.md`;
untracked `AGENTS.md.bak-pre-gitnexus-uninstall`,
`CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md`, `docs/agents/` (three
files), two untracked decision docs, and three untracked superpowers
plan/spec docs; staged empty. None overlaps the published diff. Frontend root
is clean at `d6bc752`. No stash, restore, reset, clean, or amend occurred in
either root.

## Cleanup And Issue Safety

- Disposable verification resources (isolated MySQL container, backend server
  process, per-run fixture credential files) are removed after the runs;
  per-run fixture passwords were never printed.
- This evidence commit is docs-only, carries no AI co-author, and per the #32
  protocol is not merged, pushed, or used to close anything.
- The new task worktree/branch and the frontend reference worktree are
  retained (not deleted). The superseded old #32 candidate is retained
  untouched as a historical record.
- Issue states unchanged: #32, #29, #20, and #7 remain open.
