# Issue #40 (38X-3F) Inspector-Phase Cancellation/Deadline Terminal Evidence — Release Evidence

Date: 2026-08-18

This is the backend delivery record for `Fanduzi/ControlHub-Backend#40`
("38X-3F: Classify navigation inspector-phase cancellation/deadline as
terminal evidence", parent #38, grandparent #9). It closes the one valid P1
found by the #38 verification at the published head `c7303ea`: a client
disconnect or deadline expiry during the post-target FK-metadata read in
`NavigateRelatedRecords` was recorded as a governance-style rejection instead
of the spec-mandated terminal evidence outcome.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main` at start) | `c7303ea33a2edb1ca93bad7dbb76402b1350942c` |
| Branch / worktree | `issue-40-inspector-cancel-timeout-evidence-20260818` at `~/GolangProjects/ControlHub-wt-issue-40-20260818` |
| Product SHA (all candidate gates run here) | `b461a1d9470a1a89d0487ec79ea5e133957cca4f` |
| Delivery commits | `58e453c` `fix(evidence): classify navigation inspector-phase cancellation/deadline as terminal evidence (issue #40)`; `b461a1d` `test(evidence): strengthen inspector-phase cancellation/deadline coverage per review (issue #40)` |

Delivery range (`git diff --stat origin/main...HEAD`): 3 files, +178/-3 —
`internal/service/query_execution_service.go` (+11/-1), `internal/service/navigate_related_records_test.go`
(+165/-1), `internal/service/README.md` (+2/-1). No API, model, OpenAPI,
migration, or configuration change.

## What Was Built

`NavigateRelatedRecords` step 2 (post-target FK-metadata read) recorded any
`inspector.GetObjectDetails` error unconditionally via `rejectNavigation` as
`rejected` / `navigation_source_error` / `ErrNavigationSourceNotFound`. A
client disconnect or deadline expiry during that read is a terminal
Evidence-Bearing Query Attempt outcome, not a governance refusal. The fix is
one guard at the inspector-failure branch:

- `errors.Is(err, context.Canceled)` routes through the existing navigation
  terminal-outcome classifier `recordNavigationTerminalOutcome` →
  `classifyExecutorError` → status `failed`, code `query_canceled`, fixed
  message `query canceled`; the atomic Execution Evidence Pair
  (`InsertExecutionWithAudit`, event `related_record_navigation`, audit result
  `failed`) is persisted inside the detached fixed two-second Evidence
  Persistence Window (`context.WithoutCancel` + `WithTimeout(2s)`), so the
  canceled request context can never drop the terminal evidence.
- `errors.Is(err, context.DeadlineExceeded)` routes through the same
  classifier → status `timeout`, code `query_timeout`, fixed message `query
  exceeded the time limit`, audit result `timeout`, same atomic pair in the
  same detached window.
- Every other inspector error keeps the exact prior behaviour: `rejected` /
  `navigation_source_error` / fixed message / `ErrNavigationSourceNotFound`
  public contract unchanged.
- Pre-resolution safe metadata is unchanged: fixed generic preview
  `related:unresolved` / digest `nav:unresolved` (no request-controlled source
  identity is persisted before trusted FK resolution).
- The raw inspector error is reserved from the response, evidence rows, audit
  events, metrics, and logs, exactly as before.
- No new public status enum, HTTP code, retry, queue, worker, disk buffer,
  configuration option, or abstraction was added. `classifyExecutorError`,
  `recordNavigationTerminalOutcome`, `persistNavigationAttempt`, and the
  two-second window are the existing Issue #35/#36 seams; the fix only routes
  the inspector phase through them.

## RED → GREEN (test seam: `internal/service/navigate_related_records_test.go`)

- RED at `58e453c` before the fix (test-only commit): `go test
  ./internal/service -run TestNavigateRelatedRecords -count=1` →
  `36 passed, 2 failed`. `TestNavigateRelatedRecords_InspectorCanceled_RecordsFailedCanceled`
  and `TestNavigateRelatedRecords_InspectorDeadline_RecordsTimeoutEvidence`
  failed with `navigation source object or foreign key not found` — the wrong
  governance rejection recorded by the buggy path. The strengthened
  `TestNavigateRelatedRecords_InspectorError` passed (ordinary failure
  behaviour retained).
- GREEN at the product SHA: all 38 navigation tests pass. The canceled test
  proves exactly one atomic pair call with `failed`/`query_canceled`/`query
  canceled`, audit `related_record_navigation`/`failed`, fixed pre-resolution
  preview/digest, resolved target/actor/engine metadata, no raw-error or DSN
  leak, and a live bounded (0s, 2s] persistence context captured at call time
  after the request context was canceled (`assertDetachedEvidenceWindow`).
  The deadline test proves the same for `timeout`/`query_timeout` with audit
  result `timeout`. The pair-failure test proves a persistence failure on the
  canceled inspector path still surfaces the controlled `errPersistAttempt`
  backend failure with exactly one pair attempt and nothing recorded; the
  fixed value-free telemetry contract (one `queryEvidencePersistenceFailures`
  increment, one safe log category) remains repository-owned and is proven at
  the repository seam, unchanged.

## Candidate Gates (exact product SHA `b461a1d`)

All executed in the issue-40 worktree at `HEAD=b461a1d9470a1a89d0487ec79ea5e133957cca4f`.

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, exit 0, 1819 tests, 14 packages, 0 fail, 0 skip |
| `go test -race -count=1 ./...` | PASS, exit 0, 1819 tests, no data races |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./...` | PASS, clean |
| `go build ./...` | PASS, clean |
| `make openapi-validate` | PASS (OpenAPI YAML validity; no OpenAPI change — no new endpoint/status/enum) |
| `make test-integration` | PASS, exit 0, 238 top-level passed (389 runs incl. subtests) / 0 failed / 0 skipped (Testcontainers mysql:8.0, migrations 1→17) |
| `make test-openapi-fuzz` | PASS, exit 0, exactly 1 test (`TestOpenAPIFuzz`), Schemathesis 4.15.2, all checks passed |
| `make argon2id-budget` | PASS (median 99.1ms ≤ 250ms, p95 100.7ms ≤ 300ms, 20 samples) |
| `scripts/check-docs.sh` | PASSED on both delivery commits (L3 headers intact, `internal/service/README.md` L2 synchronized; L1 root README needs no change — no endpoint/contract surface changed) |
| `gofmt -l` (changed files) | Clean (pre-existing unformatted files in the module are untouched by this delivery) |
| `git diff --check` | Clean |

The existing no-leak suites (statement previews, template values, credentials,
DSNs, raw driver errors — service, handler, and repository seams) run inside
the above unit and real-MySQL integration gates and are unchanged by this
delivery, which adds no new persistence surface, log sink, or telemetry.

## Three-Axis Code Review (committed range `c7303ea...b461a1d`)

Independent Standards, Spec, and Security reviews ran as parallel read-only
sub-agents on the committed range, then against the worktree base.

- Standards: no P1. One P2 (judgement call — duplicated test construction in
  the three new tests despite the existing `navTestScaffold`), fixed in
  `b461a1d` (tests now reuse the scaffold).
- Spec: one P1 (partial acceptance coverage — the tests did not assert the
  atomic pair's safe metadata, exactly-once call, and pair-failure telemetry
  contract), fixed in `b461a1d` (preview/digest/actor/target/engine metadata,
  audit result, exactly-one pair attempt, zero rows/audit on failure, no raw
  persistence error surfaced).
- Security: no P1/P2/P3 findings. Raw inspector errors cannot reach the HTTP
  envelope, history fields, audit result, counter, or logs; the detached
  window bound is intact; cancellation reclassification does not change
  evidence count or record any additional request-controlled values.

Final verdicts: **Standards APPROVE, Spec APPROVE, Security APPROVE**; no
unresolved P1/P2. Affected gates were re-run after the review-fix commit at
the exact product SHA (table above).

## Acceptance Criteria Status

All four #40 ACs are met:

- Canceled navigation FK-metadata read records `failed` / `query_canceled`
  through the atomic pair in the detached two-second window — proved by
  `TestNavigateRelatedRecords_InspectorCanceled_RecordsFailedCanceled`
  (including the live bounded persistence context after request cancellation).
- Deadline-expired navigation FK-metadata read records `timeout` — proved by
  `TestNavigateRelatedRecords_InspectorDeadline_RecordsTimeoutEvidence`.
- Other inspector errors keep `rejected` / `navigation_source_error` /
  `ErrNavigationSourceNotFound` unchanged — proved by the strengthened
  `TestNavigateRelatedRecords_InspectorError`.
- No new public status enum or HTTP code; no value leaks — no model/API/OpenAPI
  change; raw-error and DSN leak assertions in the new tests; value-free
  telemetry contract unchanged at the repository seam.
- Unit, race, real-MySQL integration, OpenAPI, Schemathesis, no-leak, and
  documentation gates pass — candidate gate table above.
- Independent Standards, Spec, and Security reviews pass with no remaining
  P1/P2 — review section above.
- Re-verification evidence on the parent (#38) is recorded by the successor
  #38 verification rerun; parent #9 remains open until closure is authorized.

## CI

Exact-head CI on the pushed `main` refs is recorded in the final evidence
follow-up commit (same pattern as the #35/#37 evidence records); the final
conclusion is also cited in the #40 ticket closing comment.

## Root Worktree Preservation

The ROOT worktree (`~/GolangProjects/ControlHub`, still at local `3af5d29`)
was not touched by this delivery; its pre-existing dirty paths remain
untouched in place. All 13 WIP files (modified `CLAUDE.md`,
`advisor-plans/README.md`; untracked user WIP: local `CONTEXT.md`,
`AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`,
`docs/agents/`, two decision records, three superpowers plans/specs) were
verified byte-for-byte identical (sha256) before and after the delivery. The
candidate diff (`internal/service/*` only) does not intersect any dirty path.
Local `main` remains behind `origin/main` exactly as at start; the merge to
`origin/main` is a fast-forward push from the candidate branch.

## Cleanup And Provenance

- The issue-40 task worktree and branch are intentionally preserved until push
  and CI both succeed, per delivery-closure policy.
- The #38 verification worktree/branch/evidence (`issue-38-reverify-20260818`,
  worktree `~/GolangProjects/ControlHub-wt-issue-38-20260818`, HEAD
  `1a44ab6`) was not modified; the #38 rerun uses a fresh worktree from the
  post-#40 `origin/main`.
- No force push, no rebase, no amend, no tag, no deploy.
