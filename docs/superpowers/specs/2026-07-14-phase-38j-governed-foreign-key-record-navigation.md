# Phase 38J Governed Foreign-Key Record Navigation Specification

## Status

Accepted 2026-07-14. This specification and its paired technical design are
the source of truth for Phase 38J Delivery B.

## Background

Phase 38J Delivery A added a local-only, single-value Copy action to the query
result grid. Users can now inspect schema foreign keys but cannot follow a
relationship from a bounded result row to the referenced record. That is useful
for real debugging, but the result row must not be turned into browser-built
SQL: doing so would put data values in worksheet text and execution history.

## Goal

Let an authorized user explicitly open bounded referenced records for an
eligible foreign key, while preserving ControlHub's read-only, credential,
audit, history, result-cap, and no-secret boundaries.

## Non-Goals

- Navigation from arbitrary SQL, joins, aliases, expressions, duplicated
  headers, or manually typed statements.
- Browser SQL construction, generic query parameters, or changes to normal
  `POST /query-targets/{id}/execute`.
- DDL, `SHOW CREATE`, definitions, export, saved queries, editable grids,
  ER diagrams, Visual Explain, AI/MCP, new engines, migrations, or persistence.
- Credential controls in `/query`, browser database connections, DSNs,
  passwords, usernames, or an `actorUserId` request field.

## User Flow

1. In Object Explorer, a user selects a ready table and chooses **Preview
   rows**. This opens a new local worksheet containing a fixed qualified
   `SELECT * FROM <trusted table>` preview statement, but does not run it.
2. Running that unchanged generated statement creates a result with trusted
   local source context. Editing the statement, changing target/database,
   switching to a different result, or closing the worksheet invalidates that
   context permanently for that result.
3. The user selects a data cell in the existing result grid. If that row has a
   non-null value for every local column of a trusted FK, a **Related records**
   action is enabled. Headers and ineligible rows do not expose the action.
4. Selecting a relation is explicit. It never runs on hover, focus, object-tree
   expansion, row selection alone, or editor load.
5. The related result is rendered in a transient, labelled panel below the
   source result in the same worksheet. The source grid and editor remain
   unchanged. Closing the panel restores focus to the action that opened it.

## Eligibility And Provenance

An eligible result must satisfy every condition:

- It came from an Object Explorer-generated preview of one `table`, on the
  current target and database.
- The generated statement has not been edited since provenance was attached.
- The result belongs to that worksheet's current execution, not history or a
  prior result.
- The table's current schema detail contains the selected FK.
- Every FK local column appears exactly once in the result columns and every
  corresponding selected-row value is non-null.

The UI must fail closed. It must not infer a source table from SQL text, a
column name, an alias, a result header, or a user-provided referenced table.

## API Contract

The proposed authenticated route is:

```text
POST /query-targets/{id}/related-records
```

The browser request contains only:

```json
{
  "source": {
    "database": "orders",
    "object": "order_items",
    "kind": "table",
    "foreignKey": "fk_order_items_order"
  },
  "localValues": ["42"],
  "maxRows": 100
}
```

`localValues` is an ordered array of rendered non-null scalar values. It is not
SQL and is never included in a URL, worksheet statement, history preview,
audit display, API error, or product log. The backend validates length/order
against the FK metadata. The caller never supplies a referenced database,
referenced object, referenced columns, a DSN, credentials, or an actor ID.

The response uses the existing bounded result shape (`columns`, `rows`,
`rowCount`, `truncated`, `durationMs`, `limitApplied`) plus a relation label
safe to display. It never contains a query string, DSN, credential metadata,
or raw driver error.

## Authorization, Governance, And Recordkeeping

- The route uses the same fresh authenticated actor requirement as query
  execution and schema access. Actor identity comes only from the verified
  token.
- The service applies the same target lookup, readiness, credential binding,
  environment policy, read-only transaction, timeout, and result caps as a
  normal governed query.
- Every attempt is recorded. History/audit use the fixed action
  `related_record_navigation` plus source object and FK identity. They never
  store `localValues`, result rows, SQL text containing values, credentials, or
  raw database errors.
- Existing history visibility remains unchanged: non-admin users see only their
  own entries; admins may see the target's entries.

## User Experience Requirements

- The action is localized in English and Chinese, keyboard reachable, visible
  in dark/high-contrast themes, and usable on small screens.
- The relation menu names the trusted constraint and referenced table/columns,
  but does not repeat selected values.
- Loading, empty, denied, validation, timeout, and truncated states are
  controlled and clearly distinguished.
- The source result remains visible while related records are shown; no editor
  statement is replaced or silently executed.
- A new run, source-context invalidation, worksheet switch, target switch, or
  worksheet close clears related-result state and ignores late responses.

## Security Requirements

- Backend resolves FK identity from current governed schema metadata, not from
  browser-provided referenced identifiers.
- Backend quotes identifiers only after trusted resolution and binds every local
  value as a database parameter. No selected value is interpolated into SQL.
- Normal query execution remains parser-guarded and never accepts this endpoint's
  parameters.
- Errors use fixed public messages. Product code must not log request bodies or
  raw driver errors.
- The feature is unavailable for locked, non-ready, unknown, unauthorized, or
  schema-ineligible targets/objects/rows.

## Acceptance Criteria

- A generated table preview can navigate one eligible FK row to real referenced
  records through the governed backend.
- Arbitrary, joined, aliased, edited, duplicate-column, missing-column, and
  NULL-key results show no related-record action.
- Composite keys preserve schema column order and require all values.
- Target policy, timeout, response caps, audit/history visibility, and existing
  query/schema/history flows remain enforced.
- Selected row values appear in neither editor SQL, history previews, audit UI,
  browser-visible errors, nor product logs.
- Component, API, integration, OpenAPI, and real E2E suites pass with zero
  failures and zero skips in the dedicated fixture environment.

## Deferred

- Reverse foreign-key navigation, arbitrary-query provenance, a full object
  inspector, definition SQL, context menus, ER diagrams, and export remain
  separate product decisions.
