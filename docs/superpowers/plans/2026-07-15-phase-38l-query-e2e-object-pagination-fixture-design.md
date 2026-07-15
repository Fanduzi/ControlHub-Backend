# Phase 38L-0: Query E2E Object Pagination Fixture Design

## Decision

Expand the existing dedicated Docker MySQL fixture in
`scripts/query-e2e-mysql.sh`. It is the authoritative lifecycle owner for
`query_e2e_aux`: `make query-e2e-mysql-up` creates it and `down` removes its
container. Product metadata is intentionally not used to manufacture schema
objects.

## Object Set And Ordering

The MySQL schema inspector uses deterministic `ORDER BY TABLE_TYPE, TABLE_NAME`.
Base tables appear before views, and names are alphabetical inside each type.

The added names are therefore fixed as:

```text
schema_zz_page_01 ... schema_zz_page_26
```

This placement matters. Names such as `schema_page_*` would sort before
`schema_parent`, moving existing parent-table assertions to page two and
breaking current Inspector/FK E2E before the new frontend pagination UI exists.
The `zz` names leave `schema_child` and `schema_parent` on page one, then put
the final added tables on page two. `schema_parent_summary` remains the one
existing view after all base tables.

## Creation Strategy

Add explicit, static `CREATE TABLE IF NOT EXISTS` statements to the existing
`query_e2e_aux` heredoc after the `schema_child` definition and before the view
definition. Each table has only a non-null primary-key `id` and a short
non-sensitive label column. No data insert is necessary for information-schema
listing; do not create rows merely to make a table visible.

Use explicit static SQL rather than dynamic SQL, stored procedures, or shell
interpolation. It is easier to audit, cannot accept external identifiers, and
matches the current fixture's declarative heredoc style.

## Security And Cleanup

Existing root setup remains internal to the disposable container. The existing
read-only user retains only `SELECT` on `query_e2e_aux.*`; the new tables inherit
that scope. No DSN/password variable appears in test output or documentation.

The expansion must not alter the env-file generation or grant block. The
container lifecycle already establishes clean recreation through `down + up`.

## Test Boundary

The fixture change proves real database shape and governed API paging. The
subsequent frontend delivery owns UI pagination/search behavior. Keep their
commits and worktrees separate, but run the cross-repo E2E after both are
available so page two is proved with the actual backend and MySQL fixture.
