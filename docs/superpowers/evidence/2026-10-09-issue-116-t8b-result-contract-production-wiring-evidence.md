# Issue #116 — T8-B Result-Contract Production Wiring Evidence

Date: 2026-10-09

## Summary

T8-B wires the accepted T8-A per-cell truncation contract into the three
governed MySQL/TiDB production result paths: ordinary execute,
saved-statements/execute, and related-record navigation. The scanner now emits
an aligned `cellTruncated` matrix for every retained row; disclosure `Apply`
validates and carries the matrix (clearing flags on masked replacements); the
post-finalize `DecideResultDelivery` gate runs after the committed success
evidence pair on all three paths; HTTP maps the refusal to 400
`result_contract_upgrade_required`. This slice does not claim #116 complete —
PostgreSQL path enablement and frontend capability declaration remain with
their owning tickets.

## Refs

| Item | Value |
|------|-------|
| Repository | `Fanduzi/ControlHub-Backend` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/116 (parent #108) |
| Accepted base (T8-A + R1) | `7bab56dcff2faf31697d5e8745fa23b7f2406a26` |
| Tested code HEAD | `b4dc07cc2be2b6d36b46dbda0da36dd03f5b60cb` |
| Evidence HEAD | appended on top of the tested SHA (this commit) |
| Branch | `impl/pg-workbench-t8-result-contract` (existing ticket branch, not from main) |
| Worktree | `/private/tmp/pg-t8` |
| Decision record | `docs/decisions/2026-10-09-t8b-result-contract-production-wiring.md` |

## Product Commits

| SHA | Message |
|-----|---------|
| `b4dc07cc2be2b6d36b46dbda0da36dd03f5b60cb` | `feat(query): T8-B — wire cellTruncated gate across governed result paths (#116)` |

No AI `Co-Authored-By` trailer; real Git author preserved.

## Changed Product Files

```text
internal/service/query_executor.go                    scanner matrix emission; toJSONSafe converts only
internal/service/query_disclosure_service.go          Apply takes/returns matrix; validates shape; clears masked flags
internal/service/query_execution_service.go           capabilities param; post-finalize gate on execute + navigation
internal/service/query_template_execution_service.go  saved-execute capabilities into the shared chain
internal/api/query_execution_handler.go               ErrResultContractUpgradeRequired → 400 on all three mappers
internal/openapi/openapi.yaml                         active-gate descriptions + 400 examples on three endpoints
internal/{service,api,integration}/README.md          L2 sync
docs/decisions/2026-10-09-t8b-result-contract-production-wiring.md
```

Tests: `internal/service/query_result_gate_test.go`,
`internal/api/query_result_contract_handler_test.go`,
`internal/integration/query_result_gate_integration_test.go`, plus scanner /
disclosure / existing service and handler test updates and the shared fixture
wiring `WithTemplateExecution` + `QuerySavedStatementService`.

## Red Baseline (before implementation)

| Gate | Result |
|------|--------|
| `go test ./internal/service ./internal/api` | FAIL — 17 cases: no matrix from scanner, no gate on any path, refusal mapped to 500 |
| `go test -tags=integration ./internal/integration -run 'TestResultGateIntegration'` | FAIL — 10 cases: real-MySQL paths returned 200 with rows and no matrix on a truncated page |

## Final Gates — per-gate run attribution (revised in T8-B-R1)

The table below was previously headlined "at tested SHA `b4dc07c`" without
per-gate attribution. Corrected attribution, per T8-B-R1:

| Gate | Result | When it ran | Raw record |
|------|--------|-------------|------------|
| `go build ./...` | PASS | pre-commit, on the working tree that became `b4dc07c` (content-identical) | console record only — original stdout not preserved |
| `go vet ./...` | PASS | same pre-commit tree | console record only |
| `GOFLAGS=-count=1 make test` | PASS (all packages) | same pre-commit tree | partial console capture (tail only) archived under `2026-10-09-issue-116-t8b-r1/raw/archive-b4dc07c-console/` |
| `go test -race -count=1 ./internal/service` | PASS (28.8s) | same pre-commit tree | console record only |
| `go test -race -count=1 ./internal/api` | PASS (305.1s) | same pre-commit tree | console record only |
| `make openapi-validate` | PASS | same pre-commit tree (via `go test ./internal/openapi`) | console record only |
| `make test-integration` | PASS — real MySQL 8.0 suite (70.1s, 11 `TestResultGateIntegration_*`) + PG pool gate (3.8s) | same pre-commit tree | complete `-v` console capture archived under `2026-10-09-issue-116-t8b-r1/raw/archive-b4dc07c-console/` |
| `gofmt -l` on changed files | clean | pre-commit | console record only |
| three-level doc check | OK | pre-commit | console record only |
| `go build ./...` + targeted service/api tests | PASS | **post-commit** at `b4dc07c`, clean tree | console record only |

No gate output was re-timestamped: "pre-commit tree" means exactly that —
the runs happened before `git commit`, on the same file content that was
committed. T8-B-R1 re-ran every frozen gate at `dff4cde` (production files
byte-identical to `b4dc07c`; only `query_executor_test.go` changed) with
full stdout/stderr logs and real exit codes archived; see
`docs/superpowers/evidence/2026-10-09-issue-116-t8b-r1/`.

## Real-MySQL Integration Evidence

`internal/integration/query_result_gate_integration_test.go` provisions a
dedicated `t8_b_payload(id INT PRIMARY KEY, payload TEXT)` fixture
(`payload = REPEAT('a', 8193)`, raw_copy_allowed + copyAllowed policy) inside
the disposable Testcontainers MySQL and drives the real HTTP router:

- **Execute, no capability** → HTTP 400 `result_contract_upgrade_required`,
  no `rows`/`cellTruncated` in the envelope; real repository counts show
  exactly 1 `success` history row + 1 audit event.
- **Execute, `capabilities:["cellTruncated"]`** → HTTP 200, 8192-byte prefix,
  aligned `[[true]]` matrix; 1 success + 1 audit.
- **Execute, clean 8192-byte page** → 200 without capability, `[[false]]`
  matrix present (not omitted).
- **Saved-statement execute** → same refusal/delivery pair via the real
  `/saved-statements/{id}/execute` route.
- **Related-records navigation** → same refusal/delivery via the real
  `/related-records` route with trusted fixture FK.
- **Masked oversized cell** → deliverable without capability (flag cleared);
  CSV/export still denied via `CopyAllowed=false`.
- **Evidence-persistence failure injection** (duplicate-audit trigger) → 502
  wins over the refusal; history and audit roll back together (0 rows each).
- Unknown capability tokens do not satisfy the gate
  (`capabilities:["celltruncated"]` case-sensitive negative at unit level).

## Boundaries Honored

- `internal/service/query_result_contract.go` untouched (accepted T8-A frozen).
- No PR, no merge, no tag, no issue comments/closures on #116/#108.
- No PG executor/HTTP enablement, no `clientExecutionId`/claim consumption
  changes, no server CSV endpoint, no matrix persistence, no new error codes
  or config surface.
- Not run: `make test-openapi-fuzz` (requires Schemathesis CLI; out of scope
  per ticket), frontend tests (none exist for this backend slice).
