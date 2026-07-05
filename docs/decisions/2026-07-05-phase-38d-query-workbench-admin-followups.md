# Decision: Phase 38D Expands Read-Only Metadata SQL And Hardens Admin Entry

## Status

Accepted for Phase 38D planning.

## Context

Phase 38C changed the Query Workbench boundary from `SELECT` only to a small
read-only SQL allow-list. That fixed `SHOW TABLES`, `DESCRIBE`, and
`EXPLAIN SELECT`, but manual preview after the Phase 38C push exposed three
remaining product gaps:

- users still expect `SHOW DATABASES` to work because it is a read-only
  metadata command;
- `/settings/query-credentials` can render the non-admin restricted view after a
  direct URL load even when the user is logged in as an administrator;
- `/settings` has no visible entry into Query Credential settings;
- the Query Workbench target selection area still feels split between the main
  picker, the global search/filter row, and a large target-facts block.

Phase 38C deliberately rejected `SHOW DATABASES` because broad schema visibility
was not yet approved. The product direction has changed: a query workbench should
support read-only metadata exploration. The backend must still stay parser-backed
and fail closed for write/session/privilege operations.

## Decision

Phase 38D expands the MySQL/TiDB read-only metadata allow-list to include:

- `SHOW DATABASES`;
- `SHOW TABLES FROM <database>`;
- `SHOW COLUMNS FROM <database>.<table>`;
- `DESCRIBE <database>.<table>` / `DESC <database>.<table>`.

This is an explicit visibility decision. The read-only query credential controls
what schemas can actually be listed or inspected. The SQL guard's job is to
reject writes, session mutation, privileged/admin commands, unsafe functions, and
multi-statement input; it should not reject safe metadata exploration solely
because it names a schema.

The following remain forbidden:

- multi-statement input;
- DDL and DML;
- `CALL`, `SET`, `USE`, transaction control, and lock statements;
- privilege/user/session administration;
- `SHOW PROCESSLIST`;
- `SHOW GRANTS`;
- file/export paths such as `INTO OUTFILE`, `DUMPFILE`, and `LOAD_FILE`;
- side-effect or reliability-risk functions, including `SLEEP`, `BENCHMARK`,
  and named-lock functions;
- user-variable assignment.

On the frontend, administrator access must not depend solely on
`sessionStorage["controlhub.role"]`. Direct URL loads and refreshed tabs must be
able to recover the role from the authenticated session without showing a false
non-admin state.

The settings information architecture must expose Query Credential management
from `/settings`. The Query Workbench target selector should become the single
primary target-selection surface; target facts should be compact chips or
details, not a large always-open block.

## Consequences

- Backend guard tests must be updated to allow the newly approved metadata
  statements and keep existing rejection tests for unsafe operations.
- OpenAPI and user-facing copy can keep saying "read-only SQL"; no request or
  response schema change is required.
- Integration tests must prove `SHOW DATABASES` succeeds against the dedicated
  query MySQL fixture without weakening write rejection.
- Frontend auth gating must use a shared role/session recovery helper or
  equivalent pattern instead of ad hoc render-time `sessionStorage` reads.
- `/settings` needs a Query Credential entry point with admin-oriented copy.
- Query Workbench should remove the separated filter row or fold it into the
  target picker/advanced filters.

## References

- Phase 38C decision:
  `docs/decisions/2026-06-30-phase-38c-query-workbench-readonly-sql-boundary.md`
- Phase 38C spec:
  `docs/superpowers/specs/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization.md`
- Phase 38D spec:
  `docs/superpowers/specs/2026-07-05-phase-38d-query-workbench-admin-followups.md`
