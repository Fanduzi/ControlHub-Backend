# Decision: Phase 38J Uses Backend-Owned FK Navigation With Narrow Provenance

## Status

Accepted 2026-07-14. This decision implements the approved constraints in
`docs/superpowers/specs/2026-07-14-phase-38j-governed-foreign-key-record-navigation.md`.

## Context

ControlHub has two relevant but deliberately separate paths:

- `QueryExecutionService.Execute` accepts parser-guarded read-only SQL and
  persists safe metadata for each attempt.
- `QuerySchemaService` and `MySQLSchemaInspector` retrieve foreign-key metadata
  through fixed, parameterized `information_schema` queries.

Neither path identifies the source table of arbitrary result rows. Treating a
header named `customer_id`, a join, or a typed `SELECT` as proof of provenance
would be incorrect. Concatenating a selected value into a SQL statement would
also place data in editor state and statement history.

## Decision

### Provenance Is Local, Explicit, And Narrow

Only Object Explorer's implemented **Preview rows** command creates source
context. It creates an exact qualified table statement and a local context:

```text
target ID, database, table name, object kind, generated statement identity
```

The context is not persisted or sent with normal execution. On Run, the frontend
may associate it with the result only when the target and statement are still
identical. Any edit invalidates it permanently; reverting text does not restore
it. Target/database/worksheet change, new execution, close, and stale async
response also clear it.

### Navigation Has Its Own Governed Endpoint

`POST /query-targets/{id}/related-records` is a narrowly typed operation. It is
not a second public SQL executor and it does not extend `POST /execute`.

The handler obtains actor identity from existing auth middleware and passes a
typed request to the execution service. The request names only source database,
source table, kind, FK constraint, ordered local scalar values, and a bounded
row cap. The endpoint rejects NULL values, unknown source objects/constraints,
value-count mismatches, invalid row limits, and non-ready targets with controlled
errors that do not echo values.

### The Service Owns Trust And Query Construction

The service must:

1. Resolve the target through the existing governed target/credential path.
2. Retrieve current source-table FK metadata using the existing schema service
   boundary, then match database/table/kind/constraint exactly.
3. Treat the metadata's referenced schema/table/columns as the only trusted
   identifiers. Quote those identifiers in backend code only.
4. Construct a fixed shape:

   ```sql
   SELECT * FROM <trusted referenced schema>.<trusted referenced table>
   WHERE <trusted referenced column 1> = ? AND ...
   LIMIT <server-clamped integer>
   ```

5. Bind local values in FK ordinal order using `database/sql` arguments in the
   existing read-only transaction. A server-clamped limit is structural SQL, not
   a browser value.
6. Reuse existing timeout and result caps. The executor may gain a private
   typed bound-query method, but no generic `(sql, params)` capability may be
   exposed to browser-originated normal execution.

### Recordkeeping Uses Relation Metadata, Never Values

The service records each navigation attempt using fixed action metadata such as
source object and FK constraint identity. It does not persist the generated SQL,
bound values, result rows, DSN, credentials, or raw driver error. Public errors
remain fixed mappings. Existing history actor-scoping policy applies unchanged.

### Related Results Stay Separate From SQL Text

The frontend shows related results in worksheet-local transient state below the
source grid. It retains the original result and editor text. A close action
returns focus to the invoking Related records control. This prevents navigation
data from becoming a hidden editor mutation or a synthetic SQL worksheet.

## Rejected Alternatives

### Browser-generated SQL

Rejected because escaping is not a sufficient boundary: values would still enter
the editor, query preview/history, and potentially logs. The browser cannot
vouch for referenced identifiers or connection governance.

### Header-name or SQL-text inference

Rejected because joins, aliases, expressions, duplicate names, and user-edited
statements have no trustworthy single source object.

### Add Parameters To Normal Execute

Rejected because it widens the parser-guarded public execution surface and makes
the parameter-binding boundary harder to audit. Navigation needs a narrow typed
operation, not a generic API.

### Auto-navigation On Row Focus

Rejected because it hides a data-access action, produces accidental requests,
and is hostile to keyboard exploration.

## Consequences

- Initial scope supports MySQL/TiDB table FKs only, matching existing query and
  schema support.
- Object Explorer requires a real Preview rows action before result navigation
  can appear; schema detail alone is insufficient.
- The backend gains an explicit contract, OpenAPI schema, tests, and a narrow
  private parameter-binding capability.
- The frontend gains local provenance and related-result lifecycle state, but
  no persistence and no arbitrary-SQL navigation.
- Future navigation from arbitrary SQL requires a separate provenance design,
  not incremental exceptions to this decision.

## Verification Requirements

- Unit/API tests prove request validation, trusted FK lookup, composite-key
  ordinal binding, target governance, fixed public errors, and no-value
  history/audit fields.
- Integration tests use a real FK fixture and prove parameter binding without
  SQL interpolation.
- Component tests prove provenance invalidation and action suppression for
  edited/joined/aliased/ambiguous/NULL-key results.
- Real E2E proves explicit navigation, mobile and EN/ZH behavior, and no value
  leakage to editor/history/audit/error surfaces.
