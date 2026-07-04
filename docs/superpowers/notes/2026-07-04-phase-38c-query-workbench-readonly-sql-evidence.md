# Phase 38C Query Workbench Read-Only SQL Evidence — 2026-07-04

## Commits

| Commit | Description |
|--------|-------------|
| `5cdc813` | test: cover readonly query metadata statements |
| `fc1e426` | feat: allow readonly metadata query statements |
| `df78f99` | docs: update OpenAPI copy from SELECT to read-only SQL |
| `944670c` | test: cover readonly metadata query execution |

## Parser Spike Findings

The Vitess SQL parser (v0.24.1) represents the allowed statements as follows:

| Statement | AST Type | Internal/Details |
|-----------|----------|------------------|
| `SELECT ...` | `*sqlparser.Select` | Standard SELECT with optional LIMIT, INTO, Lock |
| `SHOW TABLES` | `*sqlparser.Show` | `*ShowBasic` with `Command: Table` |
| `SHOW COLUMNS FROM t` | `*sqlparser.Show` | `*ShowBasic` with `Command: Column` |
| `DESCRIBE t` / `DESC t` | `*sqlparser.ExplainTab` | Table name in `Table` field |
| `EXPLAIN SELECT ...` | `*sqlparser.ExplainStmt` | Inner `Statement` is `*Select` |
| `SHOW PROCESSLIST` | `*sqlparser.Show` | `*ShowBasic` with `Command: ProcessList` |
| `SHOW DATABASES` | `*sqlparser.Show` | `*ShowBasic` with `Command: Database` |
| `SHOW GRANTS` | `*sqlparser.Show` | `*ShowGrants` (not ShowBasic) |
| `USE db` | `*sqlparser.Use` | DBName field |
| `SET ...` | `*sqlparser.Set` | Variable assignments |

All statement classification is done via AST type assertion, never string-prefix matching.

## Allowed/Rejected Statement Matrix

### Allowed (Phase 38C)

| Statement | Guard Path | Limit Applied |
|-----------|------------|---------------|
| `SELECT ...` | `guardSelect` → side-effect walk + LIMIT injection | DefaultMaxRows/HardMaxRows |
| `SHOW TABLES` | `guardShow` → ShowBasic/Table | 0 (no row cap) |
| `SHOW COLUMNS FROM t` | `guardShow` → ShowBasic/Column | 0 (no row cap) |
| `DESCRIBE t` | `guardExplainTab` | 0 (no row cap) |
| `DESC t` | `guardExplainTab` | 0 (no row cap) |
| `EXPLAIN SELECT ...` | `guardExplain` → inner `guardSelect` | DefaultMaxRows/HardMaxRows |

### Rejected (unchanged from Phase 37 + new)

| Statement | Reason |
|-----------|--------|
| Multi-statement | Injection amplifier |
| INSERT/UPDATE/DELETE/REPLACE | Write operations |
| CREATE/ALTER/DROP/TRUNCATE | DDL |
| CALL/SET/USE | Session mutation |
| BEGIN/COMMIT/ROLLBACK | Transaction control |
| LOCK/UNLOCK | Lock statements |
| GRANT/REVOKE | Privilege statements |
| SHOW PROCESSLIST | Cross-session visibility |
| SHOW DATABASES | Cross-schema visibility |
| SHOW GRANTS | Privilege information |
| INTO OUTFILE/DUMPFILE | File export |
| SLEEP/BENCHMARK/LOAD_FILE | Side-effect functions |
| GET_LOCK/RELEASE_LOCK | Advisory locks |
| @var := ... | User variable assignment |

## Tests Added

### Unit Tests (internal/service/query_guard_test.go)

**Allowed metadata statements:**
- `TestQueryGuardAllowsShowTables`
- `TestQueryGuardAllowsShowColumns`
- `TestQueryGuardAllowsDescribeTable`
- `TestQueryGuardAllowsDescTable`
- `TestQueryGuardAllowsExplainSelect`

**Rejection tests:**
- `TestQueryGuardRejectsShowProcesslist`
- `TestQueryGuardRejectsShowDatabases`
- `TestQueryGuardRejectsShowGrants`
- `TestQueryGuardRejectsUseDatabase`
- `TestQueryGuardRejectsSetStatement`

### Integration Tests (internal/integration/query_execution_test.go)

- `TestQueryExecution_ShowTablesSucceeds` — verifies SHOW TABLES returns fixture table
- `TestQueryExecution_DescribeTableSucceeds` — verifies DESCRIBE returns column metadata
- `TestQueryExecution_ExplainSelectSucceeds` — verifies EXPLAIN SELECT returns execution plan
- `TestQueryExecution_UpdateRemainsRejected` — verifies writes still rejected

## Implementation Changes

### internal/service/query_guard.go

- Updated error message from "only a single SELECT statement is allowed" to "only read-only SQL statements are allowed"
- Refactored `Guard()` to dispatch on statement type: `*Select`, `*Show`, `*ExplainStmt`, `*ExplainTab`
- Added `guardSelect()` — existing SELECT logic (INTO, locking, side-effect walk, LIMIT)
- Added `guardShow()` — allows ShowBasic with Command=Table or Column; rejects all others
- Added `guardExplain()` — allows EXPLAIN SELECT by validating inner SELECT
- Added `guardExplainTab()` — allows DESCRIBE/DESC as-is

### internal/service/query_executor.go

- Fixed row limit check: `limit > 0 && result.RowCount >= limit` (was `result.RowCount >= limit`)
- When `limitApplied` is 0 (SHOW/DESCRIBE), no row cap is enforced

### internal/openapi/openapi.yaml

- Updated summary: "Execute a read-only SQL statement against a query target"
- Updated description to list allowed statements
- Updated error message example to match new guard message
- Updated `QueryExecuteRequest.statement` description to "read-only SQL statement"
- Added `acceptedShowTables` request example

## Verification Matrix

| Check | Result |
|-------|--------|
| `git diff --check` | PASS (no whitespace errors) |
| `go test -count=1 ./...` | PASS (all packages) |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `make openapi-validate` | PASS |
| `make test-integration` | PASS (all integration tests) |
| `make test-openapi-fuzz` | PASS (1274 test cases, all checks passed) |

## GitNexus Result

GitNexus index was stale. Ran `npx gitnexus analyze` to update:
- 6,233 nodes | 15,416 edges | 104 clusters | 217 flows

`detect_changes --scope compare --base-ref main` reports only CLAUDE.md stats block change (from analyze), which was restored per instructions.

## Scope Confirmation

- [x] No frontend edits
- [x] No new query engines
- [x] No credential leak (DSN/password never in logs/responses/errors)
- [x] No SQL guard relaxation beyond the explicit allow-list
- [x] No push/tag/release/deploy
- [x] No AI co-author
- [x] Existing SELECT side-effect protections preserved
- [x] Credential binding, timeout, row cap, history, and audit guarantees preserved
