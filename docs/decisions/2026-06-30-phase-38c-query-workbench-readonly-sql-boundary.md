# Decision: Phase 38C Uses Read-Only SQL, Not SELECT-Only

## Status

Accepted for Phase 38C planning.

## Context

Phase 37 intentionally started with the smallest safe execution boundary:
MySQL/TiDB, single statement, `SELECT` only, backend-enforced SQL guard,
credential binding, timeout, row cap, history, and audit.

Manual preview after Phase 38B showed that the execution path works, but the
product model is too narrow for a database query workbench. Users expect safe
metadata exploration commands such as `SHOW TABLES` and `DESCRIBE table`.
Rejecting `SHOW TABLES` as "only SELECT is allowed" makes the product feel broken
even though the backend is enforcing the original Phase 37 rule correctly.

The desired boundary is:

```text
read-only SQL only
```

not:

```text
SELECT only
```

## Decision

Phase 38C widens the MySQL/TiDB guard from single `SELECT` to a small,
parser-backed read-only allow-list.

The initial allow-list is:

- single `SELECT`;
- `SHOW TABLES`;
- `SHOW COLUMNS FROM <table>`;
- `DESCRIBE <table>` / `DESC <table>`;
- `EXPLAIN SELECT ...`.

The following remain forbidden:

- multi-statement input;
- DDL and DML;
- `CALL`, `SET`, `USE`, transaction control, and lock statements;
- privilege/user/session administration;
- file/export paths such as `INTO OUTFILE`, `DUMPFILE`, and `LOAD_FILE`;
- side-effect or reliability-risk functions already rejected by Phase 37,
  including `SLEEP`, `BENCHMARK`, and named-lock functions;
- broad visibility commands such as `SHOW PROCESSLIST`;
- `SHOW DATABASES`, unless a later design constrains it to the current schema or
  explicitly approves the exposure.

The backend remains the enforcement layer. Frontend copy may explain the
boundary, but frontend controls must not be treated as enforcement.

## Consequences

- Backend SQL guard tests must cover every allowed statement and every forbidden
  statement class.
- Query history and audit still record every attempt.
- OpenAPI and frontend copy must say "read-only SQL", not "SELECT only".
- Query results can keep the existing tabular response shape.
- A richer Schema Browser remains useful, but it is not required to unblock
  basic `SHOW` / `DESCRIBE` workflows.
- ClickHouse, PostgreSQL, Redis, and MongoDB execution remain out of scope.

## References

- Preview findings:
  `docs/superpowers/notes/2026-06-30-query-workbench-preview-findings.md`
- Phase 37 boundary:
  `docs/decisions/2026-06-21-phase-37-readonly-query-sandbox-boundary.md`
- Phase 38C spec:
  `docs/superpowers/specs/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization.md`
