# Phase 38D Query Workbench Admin Follow-Ups — Backend Evidence

## Commit

Implementation verified before commit; final commit hash is reported in the completion report.

## Parser Spike Findings

Vitess sqlparser v0.24.1 AST representations confirmed:

| Statement | AST Type | Key Fields |
|---|---|---|
| `SHOW DATABASES` | `*Show` → `*ShowBasic{Command: Database}` | No qualifier |
| `SHOW TABLES FROM db` | `*Show` → `*ShowBasic{Command: Table, DbName: "db"}` | `basic.DbName` |
| `SHOW COLUMNS FROM db.tbl` | `*Show` → `*ShowBasic{Command: Column, Tbl.Qualifier: "db"}` | `basic.Tbl.Qualifier` + `basic.DbName` |
| `DESCRIBE db.tbl` | `*ExplainTab{Table.Qualifier: "db"}` | `tab.Table.Qualifier` |
| `SHOW PROCESSLIST` | `*Show` → `*ShowBasic{Command: ProcessList}` | Distinct constant |
| `SHOW GRANTS` | `*Show` → `*ShowGrants{}` | Different type, caught by `!ok` |

All new allowed statements and all forbidden statements are distinguishable via AST
type and field checks. No string-prefix matching required.

## Final Allowed/Rejected Matrix

### Allowed (read-only metadata)

| Statement | Guard Path | Notes |
|---|---|---|
| `SELECT` | `guardSelect` | Full side-effect walk + LIMIT injection |
| `SHOW DATABASES` | `guardShow`, `case Database` | New in Phase 38D |
| `SHOW TABLES` | `guardShow`, `case Table` | Unchanged |
| `SHOW TABLES FROM <db>` | `guardShow`, `case Table` | New in Phase 38D (DbName allowed) |
| `SHOW COLUMNS FROM <table>` | `guardShow`, `case Column` | Unchanged |
| `SHOW COLUMNS FROM <db>.<table>` | `guardShow`, `case Column` | New in Phase 38D (Qualifier allowed) |
| `DESCRIBE <table>` | `guardExplainTab` | Unchanged |
| `DESCRIBE <db>.<table>` | `guardExplainTab` | New in Phase 38D (Qualifier allowed) |
| `DESC <table>` / `DESC <db>.<table>` | `guardExplainTab` | Same as DESCRIBE |
| `EXPLAIN SELECT` | `guardExplain` | Unchanged |

### Rejected (forbidden)

| Statement | Why Rejected |
|---|---|
| Multi-statement | Injection amplifier |
| DML (INSERT, UPDATE, DELETE, REPLACE) | Write |
| DDL (CREATE, ALTER, DROP, TRUNCATE) | Schema mutation |
| `CALL` | Stored procedure |
| `SET` | Session mutation |
| `USE` | Session mutation |
| `BEGIN`/`COMMIT`/`ROLLBACK` | Transaction control |
| `SHOW PROCESSLIST` | Cross-session visibility |
| `SHOW GRANTS` | Privilege information |
| `SHOW VARIABLES`, etc. | Not in allow-list |
| `INTO OUTFILE`/`DUMPFILE` | Filesystem write |
| `SLEEP`/`BENCHMARK`/`LOAD_FILE` | Resource consumption |
| `GET_LOCK`/`RELEASE_LOCK` | Advisory locks |
| `@var := ...` / `INTO @var` | Variable assignment |
| Locking clauses (FOR UPDATE) | Row locks |

## Tests Added/Updated

### Unit tests (query_guard_test.go)

| Test | Action | Why |
|---|---|---|
| `TestQueryGuardAllowsShowDatabases` | Renamed from `RejectsShowDatabases` | Phase 38D product decision |
| `TestQueryGuardAllowsShowTablesFromSchema` | Renamed from `RejectsShowTablesFromSchema` | Phase 38D product decision |
| `TestQueryGuardAllowsShowColumnsFromQualifiedTable` | Renamed from `RejectsShowColumnsFromQualifiedTable` | Phase 38D product decision |
| `TestQueryGuardAllowsDescribeQualifiedTable` | Renamed from `RejectsDescribeQualifiedTable` | Phase 38D product decision |
| `TestQueryGuardAllowsDescQualifiedTable` | Renamed from `RejectsDescQualifiedTable` | Phase 38D product decision |

Existing rejection tests retained: `RejectsShowProcesslist`, `RejectsShowGrants`,
`RejectsUseDatabase`, `RejectsSetStatement`, plus all write/DDL/multi-statement
rejection tests from Phase 38C.

### Integration tests (query_execution_test.go)

| Test | Statement | Expected |
|---|---|---|
| `TestQueryExecution_ShowDatabasesSucceeds` | `SHOW DATABASES` | success, includes `query_e2e` |
| `TestQueryExecution_ShowTablesFromDatabaseSucceeds` | `SHOW TABLES FROM query_e2e` | success, includes `query_e2e_items` |
| `TestQueryExecution_ShowColumnsFromQualifiedTableSucceeds` | `SHOW COLUMNS FROM query_e2e.query_e2e_items` | success |
| `TestQueryExecution_DescribeQualifiedTableSucceeds` | `DESCRIBE query_e2e.query_e2e_items` | success |
| `TestQueryExecution_ShowProcesslistRemainsRejected` | `SHOW PROCESSLIST` | ErrQueryValidationFailed |
| `TestQueryExecution_ShowGrantsRemainsRejected` | `SHOW GRANTS` | ErrQueryValidationFailed |
| `TestQueryExecution_UseDatabaseRemainsRejected` | `USE query_e2e` | ErrQueryValidationFailed |

## Verification Matrix

| Check | Result |
|---|---|
| `git diff --check` | Clean (no whitespace errors) |
| `go test -count=1 ./...` | All pass |
| `go vet ./...` | Clean |
| `go build ./...` | Clean |
| `make openapi-validate` | PASS |
| `make test-integration` | All 77 tests pass |
| `make test-openapi-fuzz` | All 1274 fuzz cases pass (2 pre-existing warnings) |
| GitNexus detect-changes | 4 files, 9 symbols, 3 flows, risk MEDIUM |

## GitNexus Result and Caveats

4 files changed, 9 symbols affected, 3 execution flows impacted:

- `Guard` → `GuardedQuery` (3 steps) — changed: Guard
- `Guard` → `RejectForbiddenNodes` (3 steps) — changed: Guard
- `Guard` → `Digest` (3 steps) — changed: Guard

Risk: MEDIUM — expected for a security-sensitive boundary change. The guard
logic is the sole enforcement point for the read-only sandbox; the change
expands the allow-list from schema-local to cross-schema metadata exploration.

## Scope Confirmation

- [x] No frontend edits (backend task only)
- [x] No new query engines
- [x] No credential leak (DSN/password never logged or returned)
- [x] No SQL guard relaxation beyond explicit read-only metadata statements
- [x] No push/tag/release/deploy
- [x] No AI co-author
- [x] Parser-backed AST checks only (no string-prefix matching)
- [x] Phase 38C EXPLAIN SELECT wrapper fix preserved
- [x] Credential binding, timeout, row cap, history, and audit guarantees preserved
