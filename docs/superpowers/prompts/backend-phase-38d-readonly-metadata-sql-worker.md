# Backend Phase 38D Read-Only Metadata SQL Worker Prompt

You are implementing the backend side of Phase 38D for ControlHub.

Backend repo:

```text
/Users/fan/GolangProjects/ControlHub
```

Do not edit the frontend repo in this backend task.

## Objective

Expand the MySQL/TiDB read-only SQL allow-list from Phase 38C to include
database-level metadata exploration.

Newly allowed in Phase 38D:

- `SHOW DATABASES`;
- `SHOW TABLES FROM <database>`;
- `SHOW COLUMNS FROM <database>.<table>`;
- `DESCRIBE <database>.<table>` / `DESC <database>.<table>`.

Already allowed and must remain allowed:

- single `SELECT`;
- `SHOW TABLES`;
- `SHOW COLUMNS FROM <table>`;
- `DESCRIBE <table>` / `DESC <table>`;
- `EXPLAIN SELECT ...`.

Still forbidden:

- multi-statement input;
- DDL/DML;
- `CALL`, `SET`, `USE`;
- transaction control;
- locks and locking clauses;
- grants/users/session administration;
- `SHOW PROCESSLIST`;
- `SHOW GRANTS`;
- file/export paths;
- side-effect functions;
- user-variable assignment.

## Required Reading

```text
docs/decisions/2026-07-05-phase-38d-query-workbench-admin-followups.md
docs/superpowers/specs/2026-07-05-phase-38d-query-workbench-admin-followups.md
docs/superpowers/plans/2026-07-05-phase-38d-query-workbench-admin-followups.md
internal/service/query_guard.go
internal/service/query_guard_test.go
internal/integration/query_execution_test.go
internal/openapi/openapi.yaml
```

## Rules

- Do not use string-prefix matching as the final guard.
- Use Vitess AST type/field checks.
- Preserve the Phase 38C `EXPLAIN SELECT` wrapper fix.
- Preserve credential binding, timeout, row cap, history, and audit guarantees.
- Do not add new query engines.
- Do not add DSN/password storage or logging.
- Do not change credential metadata APIs.
- Do not push/tag/release/deploy.
- Do not add AI co-author.

## Tasks

1. Parser spike:
   - inspect AST representation for `SHOW DATABASES`;
   - inspect AST representation for schema-qualified `SHOW TABLES`,
     `SHOW COLUMNS`, `DESCRIBE`, and `DESC`;
   - confirm `SHOW PROCESSLIST` and `SHOW GRANTS` remain distinguishable.
2. Add failing guard tests:
   - `AllowsShowDatabases`;
   - `AllowsShowTablesFromSchema`;
   - `AllowsShowColumnsFromQualifiedTable`;
   - `AllowsDescribeQualifiedTable`;
   - `AllowsDescQualifiedTable`.
3. Update prior Phase 38C rejection tests that now conflict with the new product
   boundary.
4. Keep or add rejection tests:
   - `RejectsShowProcesslist`;
   - `RejectsShowGrants`;
   - `RejectsUseDatabase`;
   - `RejectsSetStatement`;
   - writes, DDL, multi-statement remain rejected.
5. Implement parser-backed allow-list changes.
6. Add integration tests:
   - `SHOW DATABASES` includes `query_e2e`;
   - `SHOW TABLES FROM query_e2e` includes `query_e2e_items`;
   - `SHOW COLUMNS FROM query_e2e.query_e2e_items` succeeds;
   - `DESCRIBE query_e2e.query_e2e_items` succeeds;
   - unsafe statements remain rejected.
7. Update OpenAPI copy only if needed. Request/response schemas must not change.
8. Record evidence after verification.

## Verification

Run:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Backend
```

If GitNexus requires analyze, run it. If analyze modifies generated
`CLAUDE.md` or `AGENTS.md` stats blocks, restore them unless explicitly
authorized.

## Final Report

Include:

- commit hash;
- parser-spike findings;
- final allowed/rejected matrix;
- tests added/updated;
- full verification matrix;
- integration and fuzz results;
- GitNexus result and caveats;
- final git status;
- scope confirmation:
  - no frontend edits;
  - no new query engines;
  - no credential leak;
  - no SQL guard relaxation beyond explicit read-only metadata statements;
  - no push/tag/release/deploy;
  - no AI co-author.
