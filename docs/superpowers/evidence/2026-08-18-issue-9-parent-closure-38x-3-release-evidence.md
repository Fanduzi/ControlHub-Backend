# Issue #9 (38X-3) Parent Delivery Closure — Governed Query Execution Evidence Atomicity and Cancellation Durability

Date: 2026-08-18

This is the parent-level closure record for `Fanduzi/ControlHub-Backend#9`
("38X-3: Make governed query execution evidence atomic and
cancellation-durable"). It independently re-verifies the complete child
delivery chain (#34, #35, #36, #37, #39, #40) from committed objects,
tracked evidence, current code, fresh gates, exact-head CI, and tracker state,
and records the parent closure of #9.

## Status

**merged/pushed/CI green** — see the Final Delivery Facts section for the
merged SHA, push range, evidence path, and exact CI run.

## Exact Refs

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` (root worktree `~/GolangProjects/ControlHub`) |
| `origin/main` after `git fetch origin` (verification base) | `91780336c710329163a14c69e9219a8a250f3730` |
| Root local `main` HEAD (intentionally behind, untouched) | `3af5d29bb4f492a0d7628fea777ee90b74b30df8` (29 commits behind) |
| Task branch / worktree | `dc-issue-9-parent-closure-20260818-160042` at `~/GolangProjects/ControlHub-wt-dc-issue-9-20260818-160042`, created from exact `91780336c710329163a14c69e9219a8a250f3730` |
| Verified delivery range | `3af5d29..9178033` (28 commits; issues #34, #35, #36, #37, #39, #40) |
| Parent evidence commit | the docs-only commit carrying this file on the task branch (no AI co-author); its full SHA and the final pushed `origin/main` are cited in the #9 closing comment after CI |

## Child Delivery Chain Matrix

All child tickets are CLOSED on the tracker; every claimed product/evidence
SHA is an ancestor of `origin/main` (verified with `git merge-base
--is-ancestor` for 31 SHAs); every cited CI run was re-verified via the
GitHub API with the claimed exact `headSha`, both required job names
(`release-local-gates`, `release-docker-gates`), and `conclusion=success`.

| Issue | State | Product SHA(s) | Evidence file (tracked on `origin/main`) | CI runs re-verified (run @ head, both jobs success) |
| --- | --- | --- | --- | --- |
| #34 38X-3A atomic pair | CLOSED 2026-08-17 | `7753a1f50f1feebb8771489b0c69be5a31375eaa` (feat), `17701ce` (glossary), `e956266` (merged head) | `docs/superpowers/evidence/2026-08-17-issue-34-query-evidence-atomic-release-evidence.md` | 32039839410 @ `17701ce0f88129e9797dc54a123f5fbb9d3eaa8a`; 32042596619 @ `e9562663088a776ac92c114d7c53e67d491114a3` |
| #35 38X-3B cancellation durability | CLOSED 2026-08-17 | `02287de5154b760dc82e30bc99d115f77ed1b8ae` (candidate), shipped `905135e` | `docs/superpowers/evidence/2026-08-18-issue-35-evidence-cancellation-release-evidence.md` | 32076941875 @ `905135e43c1575d46be73ef97b5591cf96cd29a3` |
| #36 38X-3C related-record navigation | CLOSED 2026-08-17 | `7354a37` (feat), `e3b77df` (candidate), `0c1116b` (merged) | `docs/superpowers/evidence/2026-08-18-issue-36-related-nav-atomic-evidence-release-evidence.md` | 32081284820 @ `3e04c28`; 32081728126 @ `17192a4`; 32081936804 @ `0508477`; 32082156284 @ `0c1116b99ff3b5cbfa3de0b49844f32bc4154a53` |
| #37 38X-3D graceful drain | CLOSED 2026-08-18 | `11f3237197f67b426659790f498359f0e3d4f097` (feat), final evidence head `c7303ea` | `docs/superpowers/evidence/2026-08-18-issue-37-graceful-drain-shutdown-release-evidence.md` | 32088870770 @ `92b20d8`; 32089163522 @ `0d8d56c`; 32089580226 @ `f3f1dff`; 32089925499 @ `c7303ea33a2edb1ca93bad7dbb76402b1350942c` (cited in the #37 closing comment) |
| #39 operator matrix fixture | CLOSED 2026-08-17 | `3df1aa013a8c822c28658738741d6b5f6ce3036c` (fix), `45d2297` (merged) | `docs/superpowers/evidence/2026-08-17-issue-39-operator-access-matrix-fixture-release-evidence.md` | 32037570789 @ `45d2297552211c19f1782f25d7978206f7c4b16a` (both jobs success; the evidence's "docker gates not triggered by push" note is stale — the API shows both jobs ran and succeeded) |
| #40 38X-3F inspector-phase classification | CLOSED 2026-08-18 | `58e453c` (fix), `b461a1d9470a1a89d0487ec79ea5e133957cca4f` (candidate), `a781da5` (merged product), `a6df769` (final evidence head) | `docs/superpowers/evidence/2026-08-18-issue-40-inspector-cancel-timeout-release-evidence.md` | 32105672610 @ `2b79c20`; 32105908430 @ `a781da5dc753e491607b95f88d5698b1907af676`; 32106649202 @ `a6df769b55bc020d6121f519d1d55be46e8013db` |
| #38 38X-3E independent re-verification | CLOSED 2026-08-18 | verified head `a6df769`; evidence head `9178033` (docs-only) | `docs/superpowers/evidence/2026-08-18-issue-38-reverify-38x-3e-verification-evidence.md` | 32106649202 @ `a6df769` (exact verified head; both jobs success) |

Every child closing comment was inspected: each cites the merged SHA, the
tracked evidence path, and the exact CI run at the final head. #9 remained
OPEN throughout the child chain; it is closed only by this record.

## Fresh Gate Results (exact head `9178033`, task worktree, all commands run fresh)

| Command | Result |
| --- | --- |
| `git diff --check 3af5d29..HEAD` | PASS, clean |
| `gofmt -l` on changed `*.go` in range | 0 files; repo-wide baseline unchanged (26 pre-existing unformatted files, identical to the `3af5d29` baseline) |
| `go test -count=1 ./...` | PASS, exit 0 — 1819 tests passed, 14 packages, 0 failed |
| `go test -race -count=1 ./...` | PASS, exit 0 — 1819 tests, no data races |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./internal/integration/` | PASS, clean |
| `go build ./...` | PASS, clean |
| `make openapi-validate` (`TestOpenAPIYAMLIsValid`) | PASS |
| `go test -count=1 ./internal/openapi` | PASS — 12 tests |
| `make test-integration` (real MySQL, Testcontainers `mysql:8.0`, goose migrations 1→17) | PASS, exit 0 — 389 runs (238 top-level + 151 subtests), 0 failed, 0 skipped |
| `make test-openapi-fuzz` (`TestOpenAPIFuzz`) | PASS, exit 0 — Schemathesis 4.15.2, checks `not_a_server_error,status_code_conformance,content_type_conformance,response_schema_conformance` all passed |
| `make argon2id-budget` | PASS — median 99.8ms ≤ 250ms, p95 100.9ms ≤ 300ms, 20 samples |
| `scripts/check-docs.sh` (three-level doc protocol) | PASS on the closure commit; range-wide L3 scan: all 24 changed `*.go` files carry `input:/output:/pos:` + package comment; the only head-6-window misses are build-tag artifacts (complete headers at lines 7–8) and the pre-existing header-less `internal/integration/query_execution_test.go` (see Residual Risks R4) |
| Sensitive-value / no-leak scans | PASS — the no-leak suites (statement previews, template values, credentials, DSNs, raw driver errors, fixed telemetry shapes) run inside the unit and integration gates above; a repo-wide scan of the tracked tree found no committed secrets (DSN matches are `.env.example` dev placeholders and test fixtures) |

Zero-skip accounting: `grep` across all `*_test.go` in `internal/` and `cmd/`
finds 0 occurrences of `t.Skip`, `.Skipf`, or `SkipNow`; the integration run
log contains 0 `--- SKIP` lines; `TestOpenAPIFuzz` is the single intentionally
separated fuzz test, run via its dedicated target. No test, retry, flake,
setup failure, or disabled Schemathesis phase is unaccounted.

## Real Lifecycle Verification (full-stack, real signals, real MySQL)

In-process: `go test ./cmd/server` — 8 tests PASS, including
`TestRunServer_TerminationSignalsBeginGracefulDrain` (real SIGTERM/SIGINT
delivered to the test process), traffic-stop, drain-bound exhaustion (exit 1),
second-signal, server-failure, and the single-fixed-log contract on every
outcome.

Full-stack (disposable fixtures provisioned for this closure — see Service
Provenance): the real `cmd/server` binary (built from `9178033`) ran against a
disposable metadata MySQL (migrations 1→17, active admin/editor operators) and
a disposable query-target MySQL (governed target `102`, credential
`DC9QRO`, disclosure policies). Two runs:

| Run | Signal | In-flight queries | New traffic after signal | Evidence | Server exit | Drain log |
| --- | --- | --- | --- | --- | --- | --- |
| 1 | SIGTERM | 12 concurrent governed `select` executions, mid-flight at signal | refused (connection failed) | 12 history rows `success` + 12 `query.executed/success` audit rows | 0 | exactly 1 fixed line `ControlHub shutdown drain complete; exiting` |
| 2 | SIGINT | 12 concurrent, mid-flight | refused | 12 + 12 (24 history rows total across runs, all `success`) | 0 | exactly 1 fixed line |

Both runs prove: in-flight governed work completes during the drain, the
atomic Execution Evidence Pair commits for every attempt, new traffic stops,
clean drain exits 0 with exactly one fixed safe log. The documented local
fixture path (`scripts/query-e2e-mysql.sh` + `cmd/querydev`) was used for the
fixture provisioning pattern; the shared `controlhub-query-e2e-mysql` fixture
container was left untouched.

## Contract Verification (#9 acceptance criteria on the current published tree)

1. Every terminal governed-query outcome after target resolution persists one
   atomic history/audit pair — VERIFIED: `persistAttempt` /
   `persistNavigationAttempt` route every terminal class (success, rejected,
   failed, timeout, canceled) through repository-owned
   `InsertExecutionWithAudit` (`internal/service/query_execution_service.go`,
   `internal/repository/mysql/query_execution_repository.go`); tests cover
   ordinary, paged, template, disclosure rejection/failure, timeout,
   cancellation, and navigation (fresh runs above).
2. Audit insertion failure rolls back history; history insertion failure
   leaves no audit — VERIFIED: single transaction with deferred rollback;
   `TestQueryEvidencePairAuditFailureRollsBackBothRows`,
   `TestQueryEvidencePairHistoryInsertFailure`,
   `TestNavigateRelatedRecords_Integration_AuditFailureRollsBackBothRows`
   (real MySQL, forced audit/history failure via trigger; both tables counted
   0 rows after rollback) — PASS in fresh runs.
3. Unknown targets and pre-resolution failures remain outside execution
   evidence — VERIFIED: `ErrQueryTargetNotFound` returns before persistence in
   `Execute` and `NavigateRelatedRecords`.
4. Ordinary, paged, static/parameterized template, disclosure, and
   related-record navigation use the shared atomic primitive — VERIFIED:
   `executeGuardedChain` is shared by `Execute` and `ExecuteSavedStatement`;
   navigation uses `persistNavigationAttempt`; the standalone `InsertExecution`
   seam has no callers (removed).
5. Explain and schema-read operations retain audit-only behavior — VERIFIED:
   `query.explain` and schema audit events go through `InsertAuditEvent` only;
   `TestQueryExplain_NoQueryExecutionsRow` passes.
6. Client cancellation records `failed/query_canceled`; deadline records
   `timeout` — VERIFIED in `classifyExecutorError` and fresh tests
   (`TestExecute_ClientCanceledDuringExecution_RecordsFailedCanceled`,
   `TestExecute_ClientCanceledDuringDisclosure_RecordsFailedCanceled`,
   `TestExecute_DisclosurePreflightTimeout_RecordsTimeoutEvidence`).
7. Inspector-phase navigation cancellation/deadline follows the same terminal
   classifier — VERIFIED (#40): `TestNavigateRelatedRecords_InspectorCanceled_
   RecordsFailedCanceled`, `TestNavigateRelatedRecords_InspectorDeadline_
   RecordsTimeoutEvidence` PASS.
8. Successful backend work remains success if response delivery is later
   canceled — VERIFIED: `TestExecute_CompletedQueryBeforeClientCancel_
   RemainsSuccess`, `TestNavigateRelatedRecords_SuccessBeforeClientCancel`.
9. Evidence persistence uses a request-value-preserving,
   cancellation-detached, fixed two-second context with no retry/queue/worker/
   disk buffer — VERIFIED: `context.WithTimeout(context.WithoutCancel(ctx),
   evidencePersistenceWindow)` with `evidencePersistenceWindow = 2 * time.Second`,
   synchronous single attempt.
10. Evidence-pair failure produces the existing controlled backend error,
    exactly one dimensionless counter increment, and one fixed value-free log
    category — VERIFIED: `errPersistAttempt` → `ErrQueryBackendFailure` (502);
    counter increments exactly once per failed pair
    (`TestQueryEvidencePairPersistenceFailureCounterIncrementsOncePerPair`,
    `TestQueryEvidencePersistenceFailuresCounterIncrementsOnce`); log is one
    fixed line `query_evidence_persistence_failed
    error_class=query_evidence_pair_persistence_failure` with a
    prohibited-value scan (`TestQueryEvidencePersistenceFailureLogIsFixedAndSafe`).
11. The administrator-only query-evidence metrics response contains exactly
    `queryEvidencePersistenceFailures`; anonymous returns 401 and editor
    returns 403 — VERIFIED: route inside `requireAdminActor`; OpenAPI
    `additionalProperties: false`; `TestQueryEvidenceMetricsOperatorBoundary`
    and the real-MySQL `TestOperatorAccessBoundary` anonymous/editor/admin
    subtests for `/ops/query-evidence-metrics` PASS in fresh runs.
12. SIGTERM/SIGINT stops new traffic and drains active handlers for at most
    ten seconds; clean drain exits 0, timeout/server failure/second signal
    exits non-zero with exactly one fixed safe log — VERIFIED: constant
    `shutdownDrainTimeout = 10 * time.Second`; `drainAndExit` exit-code/log
    contract; in-process tests and the full-stack runs above.
13. Hard crash, host loss, power loss, second forced termination, and
    `kill -9` remain explicitly outside the guarantee — VERIFIED: ADR
    `2026-08-18-phase-38x-3d-graceful-drain-shutdown.md` Decision 5 and
    `cmd/server/README.md`.
14. Public HTTP envelopes/status enums remain unchanged — VERIFIED at byte
    level: `git diff 3af5d29..9178033 -- internal/openapi/openapi.yaml` is a
    single +38-line addition (the new admin-only metrics path); the execution
    status enum stays `[success, rejected, failed, timeout]`; no new error
    code or response shape.
15. `CONTEXT.md`, ADRs, OpenAPI, L1/L2/L3 documentation, and implementation
    use consistent domain terms and boundaries — VERIFIED: glossary terms
    (Evidence-Bearing Query Attempt, Execution Evidence Pair, Evidence
    Persistence Window) present in the committed `CONTEXT.md` (lines 182+),
    ADRs 38X-3B/3D, module READMEs, L3 headers, and code constants.

## Independent Reviews (fresh, read-only) and Adjudication

Three independent read-only reviews ran across the full delivery range and
current tree (reviewer runtimes had read-only inspection tools; git/go/gh
attestation was unavailable to them and was performed by the parent instead).

| Axis | Verdict | Findings | Parent adjudication |
| --- | --- | --- | --- |
| Standards | ITERATE (P1=0, P2=2, P3=1) | R1 P2: #34 evidence cites nonexistent SHA `7753a1f9170b8b3e6a20cd5f9185e2d478f54bb8` (real commit: `7753a1f50f1feebb8771489b0c69be5a31375eaa`). R2 P2: `internal/integration/query_execution_test.go` changed in range (7354a37) without L3 header, and the #38 reverify evidence's "unchanged in the range" claim is false. R3 P3: 5 changed test files with incomplete L3 `input:` inventories. | R1/R2 are genuine documentation defects in child evidence records (see Residual Risks R2/R4). They do not block closure: every material fact they touch (product SHA ancestry, gates, CI, range diff) was re-verified independently and positively by this parent, and correcting them requires rewriting published history or child remediation, which is outside the authorized closure scope. R3 recorded. |
| Spec | REJECT (P1=2, P2=1, P3=1) | S1 P1: cancellation/deadline during credential resolution (after target lookup) is collapsed into `TargetAccessError` and persisted `rejected/query_not_allowed` (`internal/service/query_target_access.go:108-120`). S2 P1: saved-template terminal outcomes after target resolution bypass evidence (statement lookup failure/not-found, foreign-personal denial, request-shape validation; `internal/service/query_template_execution_service.go:67-79`). S3 P2: #37/#40 evidence tables omit final-head CI runs. S4 P3: attestation unavailable in reviewer runtime. | S1: real code fact, adjudicated P3 residual (R5): the attempt IS recorded with fixed safe metadata, no leak, no deadline is reachable pre-execution (the 5s deadline applies to the executor only), the window is a single fast local read, and the parent contract's cancellation wording covers "query or disclosure work" — resolve is neither. S2: real code fact, adjudicated P3 residual (R6): the file is UNTOUCHED in the delivery range (no commit in `3af5d29..9178033` modifies it — the reviewer had no git access to establish this), the parent's enumerated terminal classes exclude statement-lookup outcomes, the accepted ADR 38X-3B boundary and the prior independent #38 verification (CLOSED) recorded this exact item as P3 "out of range, file untouched", and a pre-existing test (`TestExecuteSavedStatementRejectsForeignPersonalTemplate`) codifies the behavior. S3: resolved — the final-head runs exist and succeed (32089925499 @ `c7303ea`, 32106649202 @ `a6df769`, API-verified) and are cited in the #37/#40 closing comments; the omission from the evidence tables is a P3 doc nit (R3). S4: satisfied by this parent's fresh gates/ancestry/CI verification. |
| Security | ITERATE (P1=0, P2=1, P3=2) | C1 P2: actor-supplied SQL literals persist via statement preview in history. C2 P3: disclosure rejection responses can echo actor SQL (self-echo to the authenticated requester). C3 P3: false #38 changed-file claim (no security impact). | C1: adjudicated out-of-contract as stated — history statement previews are the pre-existing Phase 37 product contract explicitly preserved by the parent spec ("Existing no-leak tests remain authoritative for statement previews…"); the parent's value-free criterion governs failure TELEMETRY (counter + log), which is value-free and leak-scanned. Recorded as a pre-existing product-contract consideration, not a #9 defect. C2: pre-existing, self-echo only, recorded (R7). C3: recorded (R4), no security impact. |

Net: zero genuine P1/P2 against the #9 contract as scoped; two
documentation-severity defects in child evidence records and three behavioral
P3 residuals are recorded below with full disclosure and follow-up
recommendations. No finding was downgraded to close; each adjudication cites
the facts that decide scope (git ancestry, parent's enumerated classes,
accepted ADRs, prior independent verification, pre-existing codified tests).

## CI Facts (all re-verified via the GitHub API on 2026-08-18)

- Workflow `.github/workflows/backend-ci.yml`; push to `main` triggers both
  required jobs `release-local-gates` (unit/vet/build/openapi + argon2id
  budget) and `release-docker-gates` (real-MySQL integration + OpenAPI fuzz).
- 14 child-chain runs verified: every run `status=completed`,
  `conclusion=success`, exact claimed `headSha`, both jobs success (table in
  the Child Delivery Chain Matrix).
- Current head `9178033` has a green push run (success, push event) —
  verified before the parent evidence commit.
- Final merged-head CI: see Final Delivery Facts.

## Root Worktree Preservation (byte-level)

The root worktree (`~/GolangProjects/ControlHub`, local `main` at
`3af5d29`, intentionally behind) was NOT touched: no pull, checkout, stash,
reset, clean, restore, edit, stage, move, or delete. Pre-existing dirty
paths recorded before verification, SHA-256 + byte size; re-checked after —
all 13 files byte-identical:

| Path | SHA-256 | Bytes |
| --- | --- | --- |
| CLAUDE.md | 892f9fdfa81316d9ff46cab5d4818951a31cd0e7bf4a915df761199b8fa99f7c | 10491 |
| advisor-plans/README.md | 394df5618d29ade2c0b955cc7234dcf3344a81494509db890a03667797b42280 | 10016 |
| AGENTS.md.bak-pre-gitnexus-uninstall | bb68496196cacbc25643c806585d5889e2824364bb6200847b81d8f9b6a162ae | 5997 |
| CLAUDE.md.bak-pre-gitnexus-uninstall | 3bc44e26146d21862b0e2c37b287df743a8c9ff8b31aae3ae9a0b3c6b87569e8 | 13448 |
| CONTEXT.md | 9eff4d18f46fb3533af7a9a5a1de5bcb8cd769d1ed65d3408ad49bfb2586250c | 8421 |
| docs/agents/issue-tracker.md | decae4b541d382f2fe9c7c9f49617b405f1641cbd27b53b3137f3d8118164cfc | 621 |
| docs/agents/triage-labels.md | f672681495c9eef1db104f661ab0c3c87e73cde396b332a947e7da4551c21f34 | 347 |
| docs/agents/domain.md | f358f97ebc4224a56f89fb342b3588ccc114899af469f3cbdedf35e2023b3d95 | 573 |
| docs/decisions/2026-08-04-parameter-value-evidence-retention.md | cbad5c1377e3d1fd962e6f00ae72a3743029faa8c53edbd383992ab62e729a89 | 1616 |
| docs/decisions/2026-08-09-operator-session-boundary.md | 008a69e51c241bb14d0dedd3764df018a71e0c2be12eaab230bdda27383418d9 | 2634 |
| docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md | c2ced9487597793a0739fcc0368802a61bc2ce25d8bde6f9791a76d02edef869 | 5416 |
| docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md | e0bdcc5b8db13b68d81fa6134f9798518ee43fde9e57d144a3d2aeab54ff90fb | 13645 |
| docs/superpowers/specs/2026-08-09-phase-38x-operator-authentication-boundary.md | dd19b07ae3c71090d4665145355c69a42e277ad31a0dc24626032483b661bd21 | 12183 |

Note: the root's untracked `CONTEXT.md` is the older pre-`17701ce` draft
(without the 54 glossary lines the committed version added on `origin/main`);
it is unrelated WIP preserved as-is. All historical issue worktrees/branches
(#34–#40, #7/#20/#25/#32, rescue branches) are untouched and preserved.

## Service / Fixture Provenance

- Verification services: Go 1.26.2 (darwin/arm64), Docker 29.2.1,
  Schemathesis 4.15.2, goose v3.27.0.
- Disposable fixtures created and destroyed by this closure (task-owned
  only): `dc9-meta` (metadata MySQL on 127.0.0.1:13317, migrations 1→17,
  active admin/editor operators inserted for the lifecycle run),
  `dc9-q` (query-target MySQL on 127.0.0.1:13318, `dc9q.q` fixture table),
  plus the three disclosure-policy rows and the `DC9QRO` target metadata
  seeded inside them. Server binary `/tmp/dc9-server` (built from `9178033`).
  All removed after final verification (cleanup receipt below).
- The shared `controlhub-query-e2e-mysql` fixture (port 13306, up 3 weeks)
  was inspected only; it was not modified, restarted, or stopped.
- Testcontainers `mysql:8.0` containers from the integration/fuzz gates are
  created and reaped by the suites (Ryuk); none left running.

## Cleanup Receipt

- Removed: `dc9-meta`, `dc9-q` containers, `/tmp/dc9-server`,
  `/tmp/dc9-lifecycle-*.json/code/log` artifacts, `/tmp/dc9-*.log` gate logs.
- Preserved (task-owned, per delivery-closure practice until CI confirms):
  task worktree `ControlHub-wt-dc-issue-9-20260818-160042` and branch
  `dc-issue-9-parent-closure-20260818-160042` (kept until CI on the merged
  SHA is verified, then removed).
- Preserved (unrelated): root WIP (manifest above), all historical issue
  worktrees/branches, the shared `controlhub-query-e2e-mysql` fixture, and
  the pre-existing `rescue/phase-38q-backend-0c00213` branch.

## Residual Risks (P3, none blocks closure)

- R1: #34 child evidence cites a nonexistent full SHA for its product commit
  (`7753a1f9170b8b3e6a20cd5f9185e2d478f54bb8`; real:
  `7753a1f50f1feebb8771489b0c69be5a31375eaa`). The abbreviated prefix,
  subject, chain position, and all gate/CI facts match the real commit;
  ancestry and CI were re-verified from the real object. Follow-up: a
  docs-only correction commit on `main` fixing the citation.
- R2: #38 reverify evidence states `internal/integration/query_execution_test.go`
  was "unchanged in the range"; commit `7354a37` (in range) modified it
  (+40/-11, test-only `seedExecutionHistoryRow` fixture helper). The
  statement is false; no behavioral impact. Follow-up: correct the sentence
  in the same correction commit.
- R3: #37/#40 evidence tables omit the final-head CI rows; those rows are
  cited in the tickets' closing comments and verified by this parent
  (32089925499, 32106649202). Follow-up: add the rows in the correction
  commit.
- R4: `internal/integration/query_execution_test.go` carries no L3 header
  (`input:/output:/pos:`); it is a test file changed in the range. The
  repo's `scripts/check-docs.sh` cannot flag it historically (it checks only
  staged/HEAD~1 changes and is not CI-wired). Follow-up: add the header in
  the correction commit.
- R5: a client cancellation landing inside `TargetAccessResolver.Resolve`'s
  credential read (after target lookup) is classified `rejected/query_not_allowed`
  rather than `failed/query_canceled`. Evidence is still recorded (fixed safe
  metadata, no leak); the window is one sub-millisecond local read; no
  deadline is reachable before execution. Pre-existing classification
  boundary, not introduced by this delivery. Follow-up: classify
  `context.Canceled`/`DeadlineExceeded` wrapped in the credential read as
  terminal failed/timeout in a future hardening ticket.
- R6: saved-template statement-lookup failures (statement not found 404,
  statement-read failure 500, foreign-personal denial 404, request-shape
  validation 400) return without an Execution Evidence Pair.
  `internal/service/query_template_execution_service.go` is untouched in the
  delivery range; template VALUE validation, compile/guard rejection, and
  every execution/disclosure outcome do record evidence through the shared
  atomic pair. Recorded by the #38 reverify as P3 "out of range, file
  untouched"; codified by the pre-existing
  `TestExecuteSavedStatementRejectsForeignPersonalTemplate`. Follow-up: a
  scoped template-route hardening ticket to route these outcomes through the
  pair, if the product boundary is expanded.
- R7: disclosure rejection responses can echo unsupported-projection
  expressions back to the requesting actor (self-echo only; evidence,
  telemetry, and logs remain value-free). Pre-existing; follow-up outside #9
  scope.
- R8: five changed test files have L3 `input:` inventories that omit some
  direct imports. Minor header-accuracy drift; follow-up in the correction
  commit.

## Final Delivery Facts

- Candidate branch: `dc-issue-9-parent-closure-20260818-160042`; the
  docs-only parent evidence commit carries this file and has no AI co-author
  (verified by commit-trailer scan of the whole delivery range — zero
  `Co-authored-by` trailers).
- Fast-forward check before push: `git fetch origin` re-run; `origin/main`
  unchanged at `91780336c710329163a14c69e9219a8a250f3730`; candidate is a
  fast-forward descendant (its parent chain starts at `9178033`).
- Merge type: fast-forward only; pushed normally (non-force `git push origin
  <branch>:main`) from the clean task-owned worktree. No rebase, amend,
  force-push, tag, deploy, or PR.
- The final merged/pushed `origin/main` SHA and the exact CI run on it
  (workflow `backend-ci.yml`, jobs `release-local-gates` +
  `release-docker-gates`, both success) are cited in the #9 closing comment,
  following the established child pattern (#40 evidence records its final
  head the same way and cites the run in the ticket). The push range is
  `9178033` up to the final SHA, recorded in the closing comment with the evidence path.
- Merged-head checks after CI (performed by the closing agent): re-fetch;
  `HEAD == origin/main` in the task worktree at the exact final SHA;
  `git show HEAD:docs/superpowers/evidence/2026-08-18-issue-9-parent-closure-38x-3-release-evidence.md`
  equals the committed evidence (content from final HEAD, not the worktree);
  `git diff --check origin/main...HEAD` clean; the shown evidence contains no
  `pending`/`TBD`/placeholder strings.
- Independent final verifier: every delivery-closure checklist item proved
  (exact refs, ancestry, evidence tracked at HEAD, gate totals, CI facts,
  root-WIP byte preservation, cleanup, tracker state).
- Tracker: #9 closed with one factual closing comment (final merged SHA,
  evidence path, CI URL); only #9 was closed; #34–#40 remain closed as
  delivered; no other issue was created, closed, relabeled, or modified.
