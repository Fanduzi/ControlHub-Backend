# Phase 38L-0: Query E2E Object Pagination Fixture

## Status

Planned prerequisite for Phase 38L Delivery A Schema Explorer search and
pagination. This is fixture infrastructure only. It does not change product
schema, OpenAPI, query execution, SQL guard behavior, credentials, or
migrations.

## Problem

The dedicated Docker MySQL fixture creates only a small set of objects in
`query_e2e_aux`. That is enough for existing object metadata, Inspector, table
definition, and FK tests, but it cannot prove that the frontend can load schema
object page two when the governed API uses its normal page size of 25.

## Goal

After `make query-e2e-mysql-down` followed by `make query-e2e-mysql-up`, the
existing read-only fixture target exposes at least 29 deterministic objects in
`query_e2e_aux`:

- preserve `schema_child`, `schema_parent`, and `schema_parent_summary`;
- add 26 fixture-only base tables named `schema_zz_page_01` through
  `schema_zz_page_26`.

The MySQL schema inspector orders objects by type then name. The `schema_zz_*`
prefix deliberately sorts after `schema_parent`, preserving `schema_child` and
`schema_parent` on page one while making page two real and stable.

## Scope

The primary product-adjacent change is only
`scripts/query-e2e-mysql.sh`, in its existing `query_e2e_aux` SQL setup heredoc.
Required verification may add narrowly scoped fixture-script or E2E assertions.

No goose migration, product table, API route, OpenAPI field, credential model,
grant expansion, query guard, frontend feature, CI workflow, or deployment is
in scope.

## Fixture Contract

Each added table is deterministic, contains no business or sensitive data, and
uses a small fixed shape sufficient for information-schema discovery. The
existing SELECT grant on `query_e2e_aux.*` remains the only access mechanism;
no write grant is introduced.

Creation is idempotent through `CREATE TABLE IF NOT EXISTS`. The existing
`down` command removes the named Docker container and the local ignored env
file; the next `up` rebuilds all objects from the script. The script must never
print or commit credentials, passwords, DSNs, or generated env-file contents.

## Required Verification

- A clean down/up fixture cycle exposes at least 29 objects in `query_e2e_aux`
  to the existing read-only fixture account.
- Object page 1 with `pageSize=25` and page 2 are non-overlapping and have
  stable total/page metadata through the existing governed schema API.
- Page 1 still contains `schema_child` and `schema_parent`.
- Page 2 contains at least one `schema_zz_page_*` object.
- Existing `schema_child -> schema_parent` FK navigation, Inspector, table
  definition, and current query-workbench E2E paths remain valid.
- Phase 38L-A frontend E2E can use the real fixture to prove its Load more
  objects path, without route mocks, skips, client-only filtering, or injected
  browser clicks.

## Explicit Non-Goals

No synthetic ControlHub metadata, no fake API responses, no view-definition
expansion, no table rows needed for pagination, no browser DDL, no fixture
credential output, no change to `query_e2e_items`, and no changes to existing
foreign-key semantics.
