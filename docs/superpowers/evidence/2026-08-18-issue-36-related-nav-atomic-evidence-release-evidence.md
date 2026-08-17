# Issue #36 (38X-3C) Related-Record Navigation Atomic Evidence — Release Evidence

Date: 2026-08-18

This is the backend delivery record for `Fanduzi/ControlHub-Backend#36`
("38X-3C: Migrate related-record navigation to atomic evidence", parent #9).
It is the contract step of the 38X-3 expand-contract migration: governed
related-record navigation now records its history row and fixed
`related_record_navigation` audit event through the same repository-owned
atomic Execution Evidence Pair primitive used by ordinary, paged, and template
execution, and the obsolete standalone execution-history write seam
(`InsertExecution`) is deleted.

## Exact Refs And Scope

| Item | Value |
| --- | --- |
| Repository | `Fanduzi/ControlHub-Backend` |
| Base (`origin/main`, after #35 closure at `7327de3`) | `7327de395219b3af09d5fb2145e61af0d86fd406` |
| Branch / worktree | `issue-36-related-nav-atomic-evidence-20260818` at `~/GolangProjects/ControlHub-wt-issue-36-20260818` |
| Product SHA (all candidate gates run here) | `e3b77df50b7ab8e3fb3a46898db59de8472dfd05` |
| Merged / pushed `origin/main` | `0508477c73425f697cc6c87b5695831e9b77afd8` (fast-forward push `7327de3..0508477`; product tree identical to `e3b77df` plus the evidence commits) |
| Delivery commits | `7354a37` `feat(evidence): migrate related-record navigation to the atomic Execution Evidence Pair (issue #36)`; `e3b77df` `fix(evidence): address code-review findings on Issue #36 range`; `3e04c28` `docs(evidence): record #36 related-record navigation atomic evidence release evidence`; `17192a4` `docs(evidence): finalize #36 evidence with merged-root re-run and CI result at shipped SHA`; `0508477` `docs(evidence): add final pushed-SHA CI run to #36 evidence` |

Delivery range (`git diff --stat origin/main...HEAD`): 17 files, +540/-115 —
`internal/repository/mysql/query_execution_repository.go` (InsertExecutionWithAudit
eventType + standalone InsertExecution deleted), `internal/service/query_execution_service.go`
(+ navigation migrate + navigation terminal-outcome helper + interface),
`internal/service/query_disclosure_service.go` (navigation preflight
inspector-failure sentinel wrap), the corresponding service/repository/api/
integration test files, and docs (`CONTEXT.md`, `internal/service/README.md`,
`internal/repository/mysql/README.md`, `internal/api/README.md`,
`internal/integration/README.md`).

## What Was Built

1. **Navigation joins the shared atomic primitive (AC 1).** `persistNavigationAttempt`
   now writes the navigation history row and its fixed
   `related_record_navigation` audit event through
   `InsertExecutionWithAudit` — the same repository-owned transaction used by
   core execution — instead of the old split `InsertExecution` +
   `InsertAuditEvent` sequence. The atomic primitive now takes the fixed
   per-caller audit event type (`query.executed` for execution,
   `related_record_navigation` for navigation); the type is a service-side
   constant, never request-controlled, and the single method keeps the parent
   #9 "one atomic persistence interface" invariant.
2. **Standalone seam deleted (AC 6, AC 7).** The `InsertExecution` method is
   removed from both the concrete repository and the service interface; its
   only production caller was navigation. No split-write sequence remains in
   governed query execution — the no-split invariant is now structural. The
   audit-only `InsertAuditEvent` remains on the interface for explain/schema
   operations, which intentionally write no history row (parent #9 decision).
   Integration history-list fixtures that previously seeded rows through the
   repository API now use a test-local `seedExecutionHistoryRow` SQL helper.
3. **Cancellation durability (AC 3).** Navigation evidence is persisted in the
   same fixed two-second Evidence Persistence Window
   (`context.WithTimeout(context.WithoutCancel(ctx), 2s)`) as core execution, so
   a client disconnect can never drop terminal navigation evidence. A client
   cancellation during navigation executor/disclosure work is classified
   `failed` / `query_canceled` / fixed safe message "query canceled".
4. **Disclosure terminal outcomes (AC 4/5, parent #9 story 17).** Navigation
   disclosure-preflight terminal failures now reach the shared atomic evidence
   path: a genuine policy rejection stays `rejected`/403; a canceled,
   deadline-expired, or machinery failure is recorded as fixed safe failed or
   timeout evidence and surfaces the existing controlled backend error. The
   navigation preflight inspector-error wrap now uses
   `ErrQueryDisclosureBackendFailure` (matching core execution) so machinery
   failures classify as `query_disclosure_backend_error`. Note: a
   disclosure-machinery failure that previously returned a raw unrecorded 500
   now returns the controlled 502 with evidence — this is the parent-mandated
   outcome (user story 17), not a public contract regression.
5. **Success before disconnect stays success (AC 4).** A navigation whose
   backend work completed before the client disconnect is recorded as `success`
   through the detached window; evidence describes backend work, not response
   delivery.

## Candidate Gates (exact product SHA `e3b77df`)

All executed in the issue-36 worktree at `HEAD=e3b77df`.

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, exit 0, 1809 tests, 14 packages |
| `go test -race -count=1 ./...` | PASS, exit 0, 1809 tests, no data races |
| `go vet ./...` | PASS, clean |
| `go vet -tags=integration ./internal/integration/` | PASS, clean |
| `go build ./...` | PASS, clean |
| `go test ./internal/openapi` | PASS (OpenAPI YAML validity; no OpenAPI change needed — no new endpoint/table/status enum) |
| `make test-integration` | PASS, exit 0, 389 passed / 0 failed / 0 skipped (Testcontainers mysql:8.0; includes the new `TestNavigateRelatedRecords_Integration_AuditFailureRollsBackBothRows`) |
| `make test-openapi-fuzz` | PASS, exit 0, exactly 1 test (`TestOpenAPIFuzz`), Schemathesis checks clean |
| `check_three_level_doc.sh` | PASS on the change set (L3 headers + four module READMEs synchronized; root README reviewed — no new module/contract row needed) |
| `gofmt -l` (changed files) | Clean (pre-existing origin/main baseline files remain unformatted and were not touched) |

## Merged-root Re-run (pushed SHA `3e04c28`) and CI

All required gates were re-run at the pushed root `3e04c28` from the issue
worktree (proven `HEAD == origin/main`).

| Command | Result |
| --- | --- |
| `go test -count=1 ./...` | PASS, 1809 tests, 14 packages |
| `go test -race -count=1 ./...` | PASS, 1809 tests, no data races |
| `go vet ./...`, `go build ./...` | PASS, clean |
| `go test ./internal/openapi` | PASS |
| `go test -tags=integration -count=1 -run '^Test' -skip '^TestOpenAPIFuzz$' ./internal/integration` | PASS, 389 passed / 0 failed / 0 skipped |
| `make test-openapi-fuzz` | PASS, exactly 1 test (`TestOpenAPIFuzz`), clean |
| `check_three_level_doc.sh` | PASS on the merged root |

CI at the product-equivalent pushed root: <https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32081284820>
— Backend CI at head `3e04c28`: `release-local-gates` success,
`release-docker-gates` success, conclusion `success`. Follow-up docs-only pushes
ran green as well: `17192a4`
(<https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32081728126>)
and the final head `0508477`
(<https://github.com/Fanduzi/ControlHub-Backend/actions/runs/32081936804>), both
jobs success in each.

Root worktree preservation: the ROOT worktree (`~/GolangProjects/ControlHub`,
still at `3af5d29`) was not touched by this delivery; its pre-existing dirty
paths (modified `CLAUDE.md`, `advisor-plans/README.md`; untracked user WIP
docs incl. a local `CONTEXT.md`, `AGENTS.md.bak*`, `CLAUDE.md.bak*`,
`docs/agents/`, two decision records, two superpowers plans/specs) remain
untouched in place.

## Acceptance Criteria Status

All eight #36 ACs are met; the two-axis code review (Standards + Spec, run on
the committed range) found no blockers. Deliberate judgement calls recorded
for traceability:

- The `eventType` parameter on `InsertExecutionWithAudit` reverses the #34
  review de-parameterization (`e956266`). It is required by the contract step:
  one shared primitive must carry navigation's different fixed audit type.
  Mitigations: fixed service-side literals only, "never request-controlled"
  documented in the repository comment, the service interface comment, and the
  CONTEXT.md glossary entry; tests assert the exact literals.
- The 500→502 shift for navigation disclosure-machinery failures is the
  parent-spec-mandated terminal-outcome recording (see item 4 above).