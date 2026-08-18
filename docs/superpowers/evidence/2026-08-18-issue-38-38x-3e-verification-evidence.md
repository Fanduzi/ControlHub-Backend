# Issue #38 (38X-3E) Independent Verification of 38X-3 Delivery — Verification Record

Date: 2026-08-18

Status: **BLOCKED with evidence** — one valid Spec-review P1 remains in the
published 38X-3 product code; ticket #38 and parent #9 stay open.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Verified head (exact published ref, `origin/main`) | `c7303ea33a2edb1ca93bad7dbb76402b1350942c` |
| Root worktree | `~/GolangProjects/ControlHub` at `3af5d29bb4f492a0d7628fea777ee90b74b30df8` (unchanged, see Root Worktree Preservation) |
| Verification worktree / branch | `~/GolangProjects/ControlHub-wt-issue-38-20260818` on `issue-38-reverify-20260818` (created from `c7303ea`) |
| Reviewed delivery range | `3af5d29..c7303ea` — issues #34 (atomic pair), #35 (cancellation durability), #36 (related-record navigation), #37 (graceful drain); parent #9 |
| Range size | 40 files, +3373/-138 (includes evidence/docs commits; production code assertions below) |

All gates below ran in the verification worktree at `HEAD=c7303ea` unless
stated. No file in the range carries AI co-author attribution.

## Gate Results (exact verified head `c7303ea`)

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, exit 0 — 1829 tests passed, 0 failed, 0 test skips (1 no-test package, `internal/testsupport/operatoraccess`) |
| `go test -race -count=1 ./...` | PASS, exit 0 — 13 packages ok, 0 data races |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./internal/integration/` | PASS, clean |
| `go build ./...` | PASS, clean |
| `go test ./internal/openapi -run TestOpenAPIYAMLIsValid` | PASS (OpenAPI YAML valid) |
| `make test-integration` | PASS, exit 0 — 389 tests, 0 failed, 0 skipped (Testcontainers `mysql:8.0`, 238 top-level + subtests) |
| `make test-openapi-fuzz` | PASS, exit 0 — exactly 1 test (`TestOpenAPIFuzz`), Schemathesis 4.15.2, `not_a_server_error,status_code_conformance,content_type_conformance,response_schema_conformance` all checks passed |
| `gofmt -l` on changed `*.go` in range | Clean (0 files) |
| `git diff --check 3af5d29..c7303ea` | PASS, clean |
| L3 header / README doc check on range | PASS — all changed source files carry `input:/output:/pos:` + package comments and module READMEs are synchronized; the only file without an `input:` header block (`internal/integration/query_execution_test.go`) already lacked it at base `3af5d29` (pre-existing, unchanged by this range) |

No-leak suites (statement previews, template values, credentials, DSNs, raw
driver errors) run inside the above gates and pass unchanged. No leak-asserting
surface introduced by the range was found to be value-bearing.

## CI On The Exact Published Head

| Run | Head | Required jobs | Result |
| --- | --- | --- | --- |
| [32089925499](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32089925499) | `c7303ea` (= `origin/main` at verification time) | `release-local-gates`, `release-docker-gates` | Both success, conclusion `success` |

## Behavioral Acceptance Criteria Verification (all at `c7303ea`)

1. **Atomic Execution Evidence Pairs for every outcome class** — VERIFIED.
   Repository-owned single transaction `InsertExecutionWithAudit`
   (`internal/repository/mysql/query_execution_repository.go`); service tests
   cover ordinary, paged, template, disclosure rejection/failure,
   cancellation, and related-record navigation; production split-write seam
   `InsertExecution` deleted (Service interface + repository + callers).
2. **Real-MySQL audit-write failure leaves neither history nor audit**
   — VERIFIED. `TestQueryEvidencePairAuditFailureRollsBackBothRows`
   (integration, real MySQL): audit INSERT forced to fail via a trigger,
   then both `query_executions` and `audit_events` counted 0 rows; the raw
   database error is not returned to the caller.
3. **Client cancellation cannot erase a post-target terminal attempt;
   successful backend work stays success despite response cancellation**
   — VERIFIED with one exception (see Blocked On). Service tests prove
   cancellation during execution/paging/disclosure records
   `failed`/`query_canceled`/fixed "query canceled" through the detached
   window, and a completed-before-cancel query records `success`.
4. **Two-second persistence window and ten-second drain, bounded timeout
   failure** — VERIFIED. `evidencePersistenceWindow = 2 * time.Second`
   (`context.WithoutCancel` + `WithTimeout`, synchronous, no retry/queue/
   disk); `shutdownDrainTimeout = 10 * time.Second` fixed constant, `main`
   passes 0; `TestRunServer_*` prove traffic stop, in-flight completion,
   deadline-exhaustion exit 1 with a single fixed log, second-signal exit,
   and real SIGTERM/SIGINT delivery.
5. **Admin-only query-evidence metric with exactly one fixed field; failure
   telemetry value-free** — VERIFIED. `GET /ops/query-evidence-metrics`
   returns 401 anonymous / 403 editor / 200 admin; response body has exactly
   one field `queryEvidencePersistenceFailures`; the counter increments
   exactly once per failed pair and the log is one fixed constant line with a
   forbidden-value scan (no identity/target/statement/value/credential/DSN/
   request/raw-error data).
6. **Explain and schema-read audit-only behavior unchanged** — VERIFIED.
   `TestQueryExplain_NoQueryExecutionsRow`,
   `TestQueryExplain_AuditEventWrittenWithNoSecrets`, and the schema API /
   inspector / relationship-map integration tests pass unchanged; no new
   history row is created on those paths.
7. **Gates** — all pass with zero unaccounted failures or skips (table
   above). No real-client test is required by this delivery: the 38X-3
   acceptance criteria name unit, race, real-MySQL integration, OpenAPI
   validation, Schemathesis, no-leak, and documentation gates; the dedicated
   local query-dev real-client acceptance was delivered and evidenced in an
   earlier phase and is outside this range.
8. **Reviews** — see Review Outcomes. NOT satisfied (one P1).
9. **Exact merged SHAs, tracked evidence, exact-head CI, root-WIP
   preservation, service provenance, cleanup** — recorded in this file.
10. **#9 kept open** — yes, parent #9 remains open.

## Review Outcomes (independent, all against `3af5d29..c7303ea`, read-only)

| Axis | Verdict | Remaining P1/P2 |
| --- | --- | --- |
| Standards | APPROVE | 0 / 0 (one P3 note: the detached-context setup is duplicated at `query_execution_service.go:742,870`; shared constant limits risk) |
| Spec | ITERATE | **1 / 0** (blocking — see below) |
| Security | APPROVE | 0 / 0 (finding re-adjudicated: `context.WithoutCancel` value inheritance is spec-mandated — "Request-scoped values remain available, but persistence cannot exceed the new deadline" — and telemetry/public errors are value-free; signal-channel capacity 1 vs test capacity 2 downgraded to P3 since POSIX signals can coalesce regardless and forced second signal is outside the durability guarantee) |

### Blocking Spec finding (P1)

**Navigation FK-metadata inspector phase misclassifies cancellation and
deadline.** In `NavigateRelatedRecords`
(`internal/service/query_execution_service.go`, step 2), any
`inspector.GetObjectDetails` error is recorded unconditionally via
`rejectNavigation` as `rejected` / `navigation_source_error` / fixed "could
not retrieve source table metadata". The range already classifies the
disclosure-preflight, executor, and apply phases through
`recordNavigationTerminalOutcome` (context.Canceled → `failed` /
`query_canceled`; DeadlineExceeded → `timeout`), and the disclosure-preflight
path carries the explicit in-code rule that a canceled or deadline-expired
read "is NOT a policy rejection — it is a terminal failed/timeout
client-cancellation or deadline outcome" (Issue #35 precedent, commit
`02287de`). The inspector path omits the same treatment.

The parent spec (Issue #9) states: "Every terminal outcome after that point
must use the paired-evidence persistence path, including ... timeout, and
client cancellation" (point = after query target resolution, which has
already succeeded at step 1 for the navigation attempt), and Issue #36 AC 3
requires "Navigation cancellation uses the fixed Evidence Persistence Window
and controlled query_canceled metadata" — unqualified. A client disconnect or
deadline expiry during the post-target FK-metadata read therefore records
`rejected/navigation_source_error` instead of `failed/query_canceled` or
`timeout`. No test covers cancellation/deadline during the inspector phase,
and no ADR or module documentation records this as an accepted deviation.

Suggested fix (for the follow-up delivery): route `context.Canceled` /
`context.DeadlineExceeded` from `GetObjectDetails` through
`recordNavigationTerminalOutcome` while preserving every other inspector
error's existing `navigation_source_error` / `ErrNavigationSourceNotFound`
public contract, add service tests for canceled and deadline-expired
inspector reads, and re-run the full gates.

## Root Worktree Preservation

The ROOT worktree (`~/GolangProjects/ControlHub`, at `3af5d29`) was not
touched by this verification. Its pre-existing dirty paths (modified
`CLAUDE.md`, `advisor-plans/README.md`; untracked `AGENTS.md.bak*`,
`CLAUDE.md.bak*`, `CONTEXT.md`, `docs/agents/`, two `docs/decisions/*` WIP
records, two `docs/superpowers/plans|specs` WIP files) remain untouched in
place. No stash, reset, clean, or relocation was performed.

## Service Provenance And Cleanup

- Verification services: Go 1.26.2 (darwin/arm64), Docker 29.2.1,
  Testcontainers disposable `mysql:8.0` containers (created and reaped by the
  integration suites, Ryuk), Schemathesis 4.15.2 (CLI from
  `scripts/schemathesis.toml`), Goose migrations applied inside disposable
  containers only. No persistent database, DSN, or secret was created,
  altered, or stored by this verification.
- Cleanup receipts: no containers left running by the verification gates; no
  worktree or branch deleted; the verification worktree
  `~/GolangProjects/ControlHub-wt-issue-38-20260818` and branch
  `issue-38-reverify-20260818` are intentionally preserved for the follow-up
  delivery. `.schemathesis-reports/` and `.argon2id-budget/` artifacts are
  gitignored.

## Status

`blocked with evidence`. First unmet requirement: Issue #38 acceptance
criterion "Run independent Standards, Spec, and Security reviews with no
remaining P1/P2" — Spec review holds one valid P1 (inspector-phase
cancellation/deadline classification, exact SHA `c7303ea`, file
`internal/service/query_execution_service.go` step 2). Safe next action:
implement the suggested fix as a separate delivery, re-run the full gate set
and reviews at the new SHA, then re-verify and close #38; keep #9 open until
then. Tracking: follow-up issue referenced on #38.