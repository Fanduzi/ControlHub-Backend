# Issue #35 (38X-3B) Cancellation-Durable Terminal Evidence — Release Evidence

Date: 2026-08-18

This is the backend delivery record for `Fanduzi/ControlHub-Backend#35`
("38X-3B: Preserve terminal query evidence after client cancellation", parent
#9). It extends the Issue #34 atomic Execution Evidence Pair with
cancellation durability: every Evidence-Bearing Query Attempt on the
ordinary, paged, and template atomic execution paths persists its terminal
evidence in a fixed two-second Evidence Persistence Window detached from
request cancellation and deadline, and disclosure and executor terminal
outcomes all reach the shared atomic evidence path.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main`, after #34 closure at `813d296`) | `813d296cc6e8c4a6ec2a2e2d59c0f1baf0f0cf1a` (see below) |
| Branch / worktree | `issue-35-evidence-cancellation-20260818` at `~/GolangProjects/ControlHub-wt-issue-35-20260818` |
| Product SHA (all candidate gates run here) | `02287de5154b760dc82e30bc99d115f77ed1b8ae` |
| Candidate / merged / pushed `origin/main` | `905135e43c1575d46be73ef97b5591cf96cd29a3` (docs-only evidence commit; product tree identical to `02287de`) |
| Delivery commits | `501a647` `feat(evidence): cancellation-durable terminal evidence via Evidence Persistence Window (issue #35)`; `c555ee4` `fix(evidence): address code-review findings on Issue #35 range`; `02287de` `fix(evidence): classify disclosure machinery failures as failed, not rejected (issue #35 AC 4)` |

Delivery range (`git diff --stat origin/main...HEAD`): 8 files, +672/-37 —
`internal/service/query_execution_service.go` (+79), the two service test
files (+357/+48), `internal/service/query_disclosure_service.go`,
`internal/service/README.md`, `CONTEXT.md`, and
`docs/decisions/2026-08-18-phase-38x-3b-evidence-persistence-window.md`.

## What Was Built

1. **Evidence Persistence Window.** `persistAttempt` (the single service choke
   point for the atomic pair write) now runs the
   `InsertExecutionWithAudit` write in
   `context.WithTimeout(context.WithoutCancel(requestCtx), 2*time.Second)` —
   detached from client cancellation and deadline, one synchronous bounded
   attempt, no retry, queue, worker, or disk buffer. A window expiry or any
   persistence failure keeps the Issue #34 semantics: pair rollback, exactly
   one `queryEvidencePersistenceFailures` increment and fixed safe log
   category, controlled backend-error response.
2. **Client-cancellation classification.** `classifyExecutorError` maps
   `context.Canceled` to `failed` / `query_canceled` / fixed safe message
   "query canceled"; the raw driver error is never persisted, returned, or
   logged. `context.DeadlineExceeded` keeps the existing `timeout` outcome.
3. **Disclosure terminal outcomes.** The disclosure service now separates
   governance refusals (stay `rejected`: missing policy, unsupported
   projection shape, invalid stored mode, no-governable columns, `Apply`
   result-safety refusals) from machinery failures (new
   `ErrQueryDisclosureBackendFailure` sentinel → `failed` with fixed
   `query_disclosure_backend_error` evidence, or `timeout`/`query_canceled`
   when the cause is deadline/cancellation). The inner cause is
   `%w`-wrapped so `errors.Is` sees it. The repeated
   classify+persist+controlled-sentinel sequence was extracted to a shared
   `recordTerminalOutcome` helper.
4. **Success before cancellation stays success.** A query whose result was
   produced before the client disconnected is persisted as `success` through
   the detached window.
5. Scope boundary: unknown targets and pre-resolution failures remain outside
   evidence; #36 related-record navigation keeps its standalone
   history/audit seam untouched.

## Candidate Gates (exact product SHA `02287de`)

All executed in the issue-35 worktree at `HEAD=02287de`.

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, exit 0, 1801 tests, 14 packages |
| `go test -race -count=1 ./...` | PASS, exit 0, 1801 tests, no data races |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./internal/integration/` | PASS, clean |
| `go build ./...` | PASS, clean |
| `go test ./internal/openapi` | PASS, 12 tests (OpenAPI YAML validity; no OpenAPI change needed — no new endpoint/table/status enum, error codes are free strings) |
| `make test-integration` | PASS, exit 0, 389 passed / 0 failed / 0 skipped (Testcontainers mysql:8.0; `TestQueryEvidencePair*` and operator-access boundary included) |
| `make test-openapi-fuzz` | PASS, exit 0, exactly 1 test (`TestOpenAPIFuzz`), Schemathesis checks clean |
| `check_three_level_doc.sh` | PASS on the change set (L3 headers + internal/service README synchronized; root README reviewed — no new endpoint/contract row needed) |
| `gofmt -l` (changed files) | Clean (10 pre-existing origin/main baseline files remain unformatted and were not touched) |

The identical local + Docker gate set was re-run at the merged candidate
HEAD `905135e43c1575d46be73ef97b5591cf96cd29a3` in the delivery worktree
(`/tmp/wt35-merge`, branch `delivery-issue-35-merge`) before push: all PASS
(unit 1801 / 14 packages, race 1801 no races, vet + build clean, openapi 12,
integration 389 passed / 0 failed / 0 skipped, Schemathesis fuzz clean).

Zero failures, zero skips. New tests: 7 execution-service tests (canceled
executor, paged canceled, canceled-disclosure table-driven over backend- and
blocked-blends, disclosure timeout, disclosure machinery failure, success
before cancel, persistence failure on a canceled request) + 2
disclosure-service tests (inspector infra failure not blocked; cancellation
stays unwrapable — both fail if the wrap degrades), + 1 template-execution
cancellation test.

## Cross-check with Issue #35 acceptance criteria

- AC1 (two-second bounded detached window, no retry/queue/worker/disk) — the
  service test asserts the window context seen by the pair write is detached
  (`Err()==nil` while the request ctx is canceled) and deadline-bounded to
  (0s, 2s].
- AC2 (cancellation during query/disclosure records failed/query_canceled,
  fixed safe message) — executor cancel, disclosure cancel (both wrap
  shapes), template cancel tests.
- AC3 (deadline stays timeout) — existing + disclosure-deadline test.
- AC4 (policy rejection stays rejected; other post-target disclosure
  failures record fixed safe failed or timeout) — blocked-rejection test
  unchanged; disclosure machinery failure → failed; disclosure
  deadline/cancel → timeout/query_canceled.
- AC5 (success before cancellation remains success) — completed-before-cancel
  test.
- AC6 (unknown targets / pre-resolution failures outside evidence) —
  unchanged behavior, existing tests.
- AC7 (persistence failure rolls back pair, fixed telemetry, controlled
  backend-error response) — existing #34 tests + new canceled-request
  persistence-failure test (rollback + errPersistAttempt mapping; the
  exact-once counter increment is proven on the repository/integration
  seam).
- AC8 (ordinary/paged/template cancellation paths, no values/credentials/
  DSNs/raw errors) — covered by the three path tests; no-value/no-DSN leak
  assertions.
- AC9 (glossary + decision docs) — CONTEXT.md terms (Evidence Persistence
  Window, Client-Cancellation Evidence; Evidence-Bearing Query Attempt
  refined) + decision doc.
- AC10 (unit, race, real-MySQL integration, OpenAPI, fuzz, no-leak,
  documentation gates) — table above.

## CI

| Item | Value |
| --- | --- |
| Run | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32076941875 |
| Head SHA | `905135e43c1575d46be73ef97b5591cf96cd29a3` |
| Workflow | Backend CI (`.github/workflows/backend-ci.yml`) |
| Required jobs | `release-local-gates` — conclusion `success`; `release-docker-gates` — conclusion `success` |
| Conclusion | `success` |

## Review

Independent dual-axis review (parallel read-only sub-agents, fresh contexts):

| Axis | Verdict | Action |
| --- | --- | --- |
| Standards | P2=1 (fake-built wrapped error would not fail if `%w` reverted), P3=2 (misleading telemetry comment; `fmt` missing from L3 input header), P3 judgement (duplicated classify+persist+wrap sequence) | All fixed in `c555ee4`: real `QueryDisclosureService.Preflight` test proves unwrapability and fails on `%v` (verified by revert-then-test); comment corrected; header fixed; shared `recordTerminalOutcome` helper extracted |
| Spec | Blocker=1 (AC 4: disclosure machinery failures recorded as rejected, not failed; test bypassed the real wrapper) | Fixed in `02287de`: new `ErrQueryDisclosureBackendFailure` sentinel; machinery failures record failed/timeout/canceled; refusals stay rejected; tests prove the real wrap (verified by revert-then-test) |
| Spec notes | No cancellation-window real-MySQL integration test added | Judgement call, documented: the window is a service-seam behavior proven at the unit seam; the repository pair primitive is unchanged and already proven against real MySQL by the Issue #34 `TestQueryEvidencePair*` suite. Apply result-safety refusals stay `rejected` (they are governance refusals in the codebase's blocked taxonomy — surfaced, not blended) |

## Cleanup And Boundaries

The issue-35 task worktree/branch is retained for inspection. No schema or
migration change; no OpenAPI change; no new dependency. Unrelated files and
the pre-existing unformatted-file baseline were not touched.