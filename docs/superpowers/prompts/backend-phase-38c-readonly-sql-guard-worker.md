# Backend Phase 38C Read-Only SQL Guard Worker Prompt

You are implementing the backend side of Phase 38C for ControlHub.

Backend repo:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repo is separate. Do not edit it in this backend task.

## Objective

Widen the MySQL/TiDB query execution guard from SELECT-only to an explicit
read-only SQL allow-list.

Allowed in this phase:

- single `SELECT`;
- `SHOW TABLES`;
- `SHOW COLUMNS FROM <table>`;
- `DESCRIBE <table>` / `DESC <table>`;
- `EXPLAIN SELECT ...`.

Still forbidden:

- multi-statement input;
- writes and DDL;
- `CALL`, `SET`, `USE`;
- transaction control;
- locks and locking clauses;
- grants/users/session administration;
- `SHOW PROCESSLIST`;
- `SHOW DATABASES`;
- file/export paths;
- side-effect functions already blocked by Phase 37;
- user-variable assignment.

## Required Reading

```text
docs/decisions/2026-06-30-phase-38c-query-workbench-readonly-sql-boundary.md
docs/superpowers/specs/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization.md
docs/superpowers/plans/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization.md
docs/superpowers/notes/2026-06-30-query-workbench-preview-findings.md
internal/service/query_guard.go
internal/service/query_guard_test.go
internal/service/query_execution_service.go
internal/openapi/openapi.yaml
internal/integration/query_execution_test.go
```

## Rules

- Do not use string-prefix matching as the final guard.
- Run a parser spike first and record how allowed/disallowed statements are
  represented.
- Preserve all existing SELECT side-effect protections.
- Preserve credential binding, timeout, row cap, history, and audit guarantees.
- Do not add new query engines.
- Do not add DSN/password storage or logging.
- Do not change credential metadata APIs.
- Do not push/tag/release/deploy.
- Do not add AI co-author.

## Implementation Tasks

1. Add failing tests for allowed read-only metadata statements:
   - `SHOW TABLES`;
   - `SHOW COLUMNS FROM query_e2e_items`;
   - `DESCRIBE query_e2e_items`;
   - `DESC query_e2e_items`;
   - `EXPLAIN SELECT * FROM query_e2e_items`.
2. Add failing rejection tests:
   - `SHOW PROCESSLIST`;
   - `SHOW DATABASES`;
   - `SHOW GRANTS`;
   - `USE mysql`;
   - `SET sql_safe_updates=1`;
   - writes/DDL remain rejected.
3. Implement parser-backed guard widening.
4. Update OpenAPI copy from "SELECT" to "read-only SQL" where applicable.
5. Add integration tests against the dedicated query fixture.
6. Record evidence in the Phase 38C evidence note.

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

If GitNexus reports stale index and requires analyze, run:

```bash
npx gitnexus analyze
```

If analyze edits generated `CLAUDE.md` or `AGENTS.md` stats blocks, do not commit
those changes unless explicitly authorized.

## Final Report

Include:

- commits;
- parser-spike findings;
- allowed/rejected statement matrix;
- tests added;
- full verification matrix;
- integration and fuzz results;
- GitNexus result and caveats;
- final git status;
- scope confirmation:
  - no frontend edits;
  - no new query engines;
  - no credential leak;
  - no SQL guard relaxation beyond the explicit allow-list;
  - no push/tag/release/deploy;
  - no AI co-author.
