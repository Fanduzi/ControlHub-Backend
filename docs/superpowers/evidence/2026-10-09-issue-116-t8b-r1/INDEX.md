# T8-B-R1 — Budget-Drop Regression Fix and Gate Evidence Index

Date: 2026-10-09 (UTC timestamps inside each log header)

## Identity

| Item | Value |
|------|-------|
| Repository | `Fanduzi/ControlHub-Backend` |
| Tracker | https://github.com/Fanduzi/ControlHub-Backend/issues/116 (parent #108) |
| Branch | `impl/pg-workbench-t8-result-contract` |
| Task base / remote at start | `4e46417cc90cb6f7a4310880594c1a88abdfc14e` |
| Reviewed production code | `b4dc07cc2be2b6d36b46dbda0da36dd03f5b60cb` |
| Accepted T8-A base | `7bab56dcff2faf31697d5e8745fa23b7f2406a26` |
| **Tested code HEAD (this round)** | `dff4cdeef31214de3e0a6dfcd75d74988bd1fdab` |
| Evidence HEAD | the commit adding this directory (not the tested HEAD) |
| Worktree | `/private/tmp/pg-t8` |

## What this round changed

One test fix only — `internal/service/query_executor_test.go`,
`TestScanBoundedRows_BudgetDroppedRowLeavesNoFlag`. The old fixture
(`"aaa"/"bbb"/"ccc"`, 9 bytes total under a 12-byte cap) never entered the
budget-drop branch, so a `CellTruncated` append moved ahead of the budget
check was undetectable. The new third row is `strings.Repeat("c", 8193)`:
it truncates to an 8192-byte delivered prefix (flag `true`) that overflows
the 12-byte response budget after the two kept 3-byte rows. Assertions:
`err == nil`, `RowCount == 2`, rows exactly `[["aaa"],["bbb"]]`,
`Truncated == true`, `CellTruncated` exactly `[[false],[false]]`, the
retained page passes `DecideResultDelivery` with no declared capability,
and the same input under `paginatedWindow=true` returns
`ErrQueryResultTooLarge` with an empty result (no half page).

No production logic was modified. `git diff b4dc07c..dff4cde` touches only
`query_executor_test.go` plus the prior evidence file; the named production
file set (`query_executor.go`, `query_disclosure_service.go`,
`query_execution_service.go`, `query_template_execution_service.go`,
`query_execution_handler.go`, `openapi.yaml`) is byte-identical to the
reviewed `b4dc07c` — see `raw/99-source-identity.txt`.

### Test-sensitivity proof (scratch mutation, not committed)

With the matrix append deliberately moved ahead of the budget check
(`result.CellTruncated = append(...)` placed before
`responseBytes > MaxResponseBytes`), the fixed test fails at
`query_executor_test.go:398` observing `[[false] [false] [true]]` — the
dropped row's flag lingers and is caught. The mutation was applied in the
worktree, the failing run captured, then the file was restored to its exact
committed content (`git diff` = 0 lines afterwards). The mutation is a
sensitivity proof for the test, not evidence of a production defect; the
shipped ordering keeps the append after the budget check. Console capture:
`raw/2026-10-09_mutant-sensitivity-proof_console-capture.log`
(the shown `rc` in that capture is the display pipeline's; the `FAIL`
lines are the test result).

## Gate runs at `dff4cde` (this round — full logs + real rc)

Each log in `raw/` carries: gate name, start/end UTC, cwd, HEAD,
worktree-status count, full argv, combined stdout+stderr, and the tested
command's own exit code (`tested-command-rc`), captured by running the
command and recording its rc directly — never a `tee`/`grep` rc.

| # | Command | rc | Log |
|---|---------|----|-----|
| 00 | `go test -count=1 -v ./internal/service -run 'TestScanBoundedRows_.*(Budget\|Payload\|Sentinel)'` | 0 | `raw/00-new-test-regex.log` — 4 tests matched and ran (non-empty match) |
| 01 | `go build ./...` | 0 | `raw/01-go-build.log` |
| 02 | `go vet ./...` | 0 | `raw/02-go-vet.log` |
| 03 | `go test -count=1 ./internal/service` | 0 | `raw/03-go-test-service.log` |
| 04 | `GOFLAGS=-count=1 make test` | 0 | `raw/04-make-test.log` |
| 05 | `go test -race -count=1 ./internal/service` | 0 | `raw/05-race-service.log` |
| 06 | `go test -race -count=1 ./internal/api` | 0 | `raw/06-race-api.log` (≈5 min under `-race`; no failures) |
| 07 | `make openapi-validate` | 0 | `raw/07-openapi-validate.log` |
| 08 | `make test-integration` | 0 | `raw/08-make-test-integration.log` — real MySQL 8.0 suite, 363 `--- PASS`, all `TestResultGateIntegration_*` visible with `-v` detail (three entries: refusal / capable / clean page, masked control, persistence-failure precedence), plus PG pool version gate |
| 99 | source identity capture | — | `raw/99-source-identity.txt` |

`-count=1` disables the Go test cache for every `go test` invocation above;
`make` targets run their own flags as defined in the Makefile. Docker
Desktop 29.7.2 was available — no environment blocking, no mock substitute.

## Attribution for inherited T8-B gates (b4dc07c)

The original T8-B evidence file
(`../2026-10-09-issue-116-t8b-result-contract-production-wiring-evidence.md`)
labeled a single gate table "at tested SHA b4dc07c" while the runs were
actually pre-commit on the working tree that became `b4dc07c`. That table
is now rewritten per gate. Archived console captures from those runs:

| File | Content | Provenance |
|------|---------|------------|
| `raw/archive-b4dc07c-console/2026-10-09_make-test-integration_console-capture.log` | complete `-v` output of `make test-integration` (3025 lines: MySQL suite + PG pool gate) | pre-commit tree → `b4dc07c` content |
| `raw/archive-b4dc07c-console/2026-10-09_make-test_console-capture.log` | **partial** tail of `make test` (12 lines; earlier packages not preserved) | pre-commit tree → `b4dc07c` content |

These are agent-console captures archived after the fact, not raw logs
written at run time; they are labeled accordingly. They are reused as
historical records only — every frozen gate was independently re-run at
`dff4cde` this round (table above), so no claim rests solely on them.

## Missing records (declared, not reconstructed)

- The T8-B red baseline (17 unit failures + 10 integration failures) ran
  in-session; its stdout was not preserved to a file. The failure counts
  and commands remain recorded in the T8-B evidence file; no original
  stdout exists to archive.
- `go build`, `go vet`, `-race` runs, `openapi-validate`, `gofmt`, and the
  three-level doc check at `b4dc07c` were console-only; no raw originals
  were kept. Their `dff4cde` re-runs above carry full logs.
- Post-commit `b4dc07c` verification was limited to `go build` + targeted
  tests (console record only) — stated as such in the revised table.

## Boundaries

- No production change, no PR, merge, tag, release, or issue action.
- No PG/frontend scope, no new CSV endpoint, no new business matrix.
- No credentials, DSNs, user data, or binaries in any log; fixture rows are
  the fixed `t8_b_payload` test data only.
- `make test-openapi-fuzz` not run (Schemathesis not required by this
  ticket; unchanged scope).
