# Phase 38K Governed MySQL Table Definition Specification

## Status

Proposed 2026-07-15. This specification defines Phase 38K Delivery B. It
depends on the merged Delivery A Object Inspector metadata UI.

## Background

Object Inspector now renders normalized columns, indexes, and foreign keys.
Operators also need the MySQL table definition when diagnosing an index,
default, engine option, or foreign-key declaration. The current schema API
intentionally has no definition-text field, and the browser must not construct
or execute database metadata SQL.

## Goal

Expose a governed, read-only MySQL table-definition endpoint for a single
existing base table. It returns a bounded `SHOW CREATE TABLE` result after the
backend resolves target access and verifies the requested object is a table.

## Scope

### In Scope

- MySQL and compatible MySQL targets supported by the existing schema
  inspector.
- One authenticated endpoint:

  ```text
  GET /query-targets/{id}/schema/table-definition?database={database}&name={name}
  ```

- A typed response containing target identity, database, table name, dialect,
  bounded definition text, and an explicit truncation flag.
- Server-side target access, table-kind verification, MySQL identifier quoting,
  fixed audit events, controlled public errors, and unit/handler/integration
  coverage.

### Out Of Scope

- Views, `SHOW CREATE VIEW`, routines, triggers, events, grants, users, and
  arbitrary schema SQL.
- Browser database access, browser SQL construction, generic SQL or identifier
  execution APIs, data samples, export, persistence, or history entries.
- Changes to the existing object-detail response, SQL guard, migrations,
  credentials, target policy, result grid, or Object Inspector UI.
- A frontend definition panel. That is a separate delivery after this endpoint
  and its contract are accepted.

## Request And Response Contract

The route requires the existing fresh bearer-query actor middleware. The actor
is taken only from verified middleware context; request parameters never carry
an actor, credential reference, DSN, or engine connection detail.

`database` and `name` are required and use the existing schema identifier
length limits. The initial response shape is:

```json
{
  "targetResourceId": 42,
  "database": "orders",
  "name": "order_items",
  "kind": "table",
  "dialect": "mysql",
  "definition": "CREATE TABLE ...",
  "truncated": false
}
```

`definition` is an engine-produced `SHOW CREATE TABLE` string. It is capped at
64 KiB of UTF-8 output. If the cap is reached, the server returns a valid
UTF-8 prefix and `truncated: true`; consumers must label it incomplete and
must not represent it as a complete, executable definition. The endpoint does
not return a raw database error, DSN, user name, password, result rows, or
request values other than the requested object identity.

## Trust, Disclosure, And Audit Boundary

1. The service first resolves governed target access through the existing
   `TargetAccessResolver`.
2. The MySQL inspector verifies the exact database/name pair with a fixed,
   parameterized `information_schema.TABLES` query and requires `BASE TABLE`.
3. Only after that verification may the inspector issue `SHOW CREATE TABLE`
   using server-side backtick quoting with embedded backticks doubled. MySQL
   does not allow identifier placeholders, so parameter binding alone cannot
   express this statement.
4. The endpoint returns the server-provided table definition verbatim apart
   from the documented size cap. It is controlled schema metadata: table
   comments and default literals may be present, so it is never logged,
   persisted, placed in query history, or cached. A partial regex redactor is
   forbidden because it would be neither complete nor faithful.
5. Every attempt writes only the fixed audit event type
   `query.schema.table_definition.read` and a `success` or `failed` result.
   Definition text, object identifiers, raw driver failures, and connection
   data are not audit payloads.

## Cache Policy

Table definitions are deliberately **not cached** in Delivery B. A user action
causes one governed read so schema changes are not hidden behind the five-minute
metadata cache. This also avoids retaining potentially sensitive table comments
or default literals in process memory beyond the request. Existing database,
object-list, and object-detail cache behavior is unchanged.

## Error Contract

- Invalid or missing identifier input: `400 schema_validation_failed`.
- Target, credential, policy, engine, or readiness denial: existing controlled
  schema authorization responses; no connection detail is exposed.
- Missing table: `404 schema_object_not_found`.
- Existing view or other non-table object: `400 schema_definition_not_supported`.
- Context cancellation or timeout: `408 schema_timeout`.
- Driver/inspection/audit failures: `502 schema_backend_error` or existing
  controlled internal failure; never return driver text.

## Acceptance Criteria

- A fresh authorized actor can retrieve a known MySQL base-table definition.
- A view never reaches `SHOW CREATE VIEW` and receives the documented
  unsupported-kind response.
- Table identity is verified with bound metadata parameters before quoted DDL
  is issued; a pure identifier-quoting test covers embedded backticks.
- Definition output is valid UTF-8, capped at 64 KiB, and declares truncation.
- The response, audit events, query-execution history, logs, and cache never
  contain definition text, credentials, or raw driver errors.
- Existing schema detail/list endpoints and their cache behavior remain
  unchanged.
- Model, service, handler, OpenAPI, real MySQL integration, format, vet,
  build, OpenAPI validation, full tests, integration tests, and fuzz tests pass
  on the exact final candidate commit with no new skips.

## Deferred

A later frontend delivery may add a read-only Definition section to Object
Inspector only by calling this typed endpoint. It must show truncation, avoid
persistence and copy/export by default, and retain Delivery A focus/mobile/i18n
requirements. Supporting views requires a separate policy for `DEFINER` and
must not be inferred from this table-only contract.
