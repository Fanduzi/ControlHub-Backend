# Phase 38I Schema Intelligence, Object Explorer, And SQL Autocomplete Design

## Background

Phase 38F made the worksheet a credible SQL editor. Phase 38G improved its
readability and navigation. Phase 38H is adding bounded query-target search,
pagination, and a scalable two-region information architecture.

The next missing capability is not another layout pass. The Query Workbench
still has no live understanding of the selected database:

- backend `QueryTarget.SchemaPreview` is deliberately always empty;
- frontend `QuerySchemaBrowser` is a locked placeholder;
- CodeMirror provides SQL syntax support but disables autocompletion;
- users must remember database, table, and column names before writing SQL;
- there is no bounded object search or keyboard object navigation.

Tabularis demonstrates useful database-tool patterns: lazy object expansion,
table/column completion, a quick navigator, short-lived metadata caches, and
bounded concurrent column loads. ControlHub must learn those interaction
patterns without copying Tabularis's desktop connection model, write-oriented
features, eager multi-database loading, or unrestricted SQL keyword set.

ControlHub is a governed company query platform. The browser selects a managed
target and consumes sanitized metadata; it never receives connection secrets or
opens database connections itself.

## Dependency Gate

Phase 38I may be designed now, but implementation MUST NOT start until Phase
38H has completed its finishing flow in both repositories.

Before creating Phase 38I worktrees, verify:

- backend `main` contains the paged/searchable `GET /query-targets` contract;
- frontend `main` contains the final bounded target navigator and responsive
  Query Workbench explorer surface;
- both `main` worktrees are clean and synchronized with their remotes;
- the final Phase 38H real E2E evidence is available.

Do not base Phase 38I on the temporary Phase 38H feature worktrees. If the gate
is not satisfied, stop and report the dependency instead of guessing at the
unfinished contract.

## Goal

Give a user enough live schema intelligence to discover objects and write safe
read-only SQL without turning ControlHub into a browser-hosted DBeaver clone.

Phase 38I should deliver:

- governed, bounded, read-only schema metadata APIs for MySQL and TiDB targets;
- a lazy Object Explorer for databases, tables, views, columns, keys, indexes,
  and foreign-key relationships;
- server-backed object search and a keyboard Quick Navigator;
- CodeMirror completion for approved read-only SQL keywords, database objects,
  table names, qualified names, columns, and common aliases;
- one shared, bounded in-memory metadata model for the explorer, navigator, and
  editor completion;
- real integration and E2E proof against the dedicated query MySQL fixture.

## Non-Goals

- No PostgreSQL, ClickHouse, Redis, MongoDB, or new executable engine.
- No SQL guard widening or behavior change.
- No `USE <database>` execution.
- No DDL definitions, stored procedures, functions, triggers, events, or grants.
- No create, alter, drop, rename, truncate, insert, update, or delete controls.
- No direct browser database connection.
- No DSN, password, database username, or raw credential response/display/log.
- No credential edit controls inside `/query`.
- No ER diagram, Visual EXPLAIN, visual query builder, data-grid editing, saved
  query, export, approval, JIT, notebook, AI assistant, or MCP integration.
- No persistent schema snapshot or migration in the ControlHub metadata DB.
- No localStorage/sessionStorage/IndexedDB persistence of schema metadata.
- No Monaco migration; keep the existing CodeMirror 6 foundation.
- No CI workflow, release, tag, or deployment change.

## Product Principles

1. **Target first.** Every metadata request is scoped to one managed query
   target and follows the same credential, policy, and host/port binding as
   query execution.
2. **Lazy by default.** Do not load all databases, objects, and columns on page
   entry. Load one bounded level only when the user opens or searches it.
3. **Search before tree depth.** Large schemas require server search and bounded
   pages, not a DOM tree containing every object.
4. **One metadata truth.** Explorer, Quick Navigator, and autocomplete consume
   the same target/database/object cache and stale-request guards.
5. **Read-only semantics in the editor.** Do not suggest write/DDL keywords that
   the backend will reject, even though the SQL grammar knows them.
6. **No fake intelligence.** Never show placeholder tables or infer columns from
   result history. Unknown metadata is a loading, empty, or error state.
7. **Governance remains server-enforced.** Autocomplete and UI controls improve
   authoring only; the existing SQL guard remains the execution authority.

## Reference Decisions

### What To Learn From Tabularis

Reference these local files for behavior, not for direct code copying:

- `/Users/fan/JsProjects/tabularis/src/utils/autocomplete.ts`
- `/Users/fan/JsProjects/tabularis/src/hooks/useSqlAutocompleteRegistration.ts`
- `/Users/fan/JsProjects/tabularis/src/components/modals/QuickNavigatorModal.tsx`
- `/Users/fan/JsProjects/tabularis/src/utils/quickNavigator.ts`
- `/Users/fan/JsProjects/tabularis/src/components/layout/ExplorerSidebar.tsx`
- `/Users/fan/JsProjects/tabularis/src/components/layout/sidebar/SidebarTableItem.tsx`

Adopt:

- lazy table-detail loading;
- positive cache TTL of five minutes;
- short negative cache for empty metadata;
- at most five concurrent detail fetches;
- a 50-object-detail browser cache cap;
- alias/table-aware column suggestions;
- keyboard object navigation.

Do not adopt:

- frontend connection profiles or secrets;
- eager loading of every configured database;
- write/DDL keyword suggestions;
- table mutation context menus;
- unrestricted routines, triggers, grants, or process metadata;
- global editor providers that can leak state between worksheets/targets.

### CodeMirror, Not Monaco

The installed `@codemirror/lang-sql` version accepts an `SQLNamespace` in
`sql({ schema })` and exposes `schemaCompletionSource`. Phase 38I must extend the
existing CodeMirror integration rather than replace it.

Use a controlled completion override so the visible keyword vocabulary matches
the backend's read-only allow-list. Add `@codemirror/autocomplete` as an explicit
dependency if Phase 38I imports it directly instead of relying on a transitive
dependency.

## Backend Contract

### `schemaPreview` Compatibility

Do not populate live metadata into `QueryTarget.SchemaPreview`. Keep the field
as an empty, deprecated compatibility field in the existing query-target
contract for this phase, and stop consuming it in the frontend. Removing it is
a separate versioned-contract decision. All Phase 38I metadata comes from the
dedicated schema routes below.

### Route Boundary

Add three read-only routes under the existing `requireFreshQueryActor` group:

```text
GET /query-targets/{id}/schema/databases
GET /query-targets/{id}/schema/objects
GET /query-targets/{id}/schema/object-details
```

Query parameters:

```text
/schema/databases?q=&page=1&pageSize=50&includeSystem=false&refresh=false
/schema/objects?database=<name>&kind=all|table|view&q=&page=1&pageSize=50&refresh=false
/schema/object-details?database=<name>&name=<object>&kind=table|view&refresh=false
```

Rules:

- `page` defaults to `1`;
- `pageSize` defaults to `50` and is capped at `100`;
- `q`, `database`, and `name` have explicit length caps;
- `database` and `name` are query parameters so unusual identifiers are URL
  encoded rather than embedded in router path segments;
- `includeSystem` defaults to false and hides `information_schema`,
  `performance_schema`, `mysql`, and `sys` from the database list;
- `defaultDatabase` is null when the DSN has no database or when that database
  is excluded by the current `includeSystem` filter;
- `refresh=true` bypasses and replaces the relevant in-memory cache entry;
- all list responses use the Phase 38H `pageInfo` shape.

### Response Models

Database list:

```json
{
  "targetResourceId": 616,
  "defaultDatabase": "query_e2e",
  "items": [
    { "name": "query_e2e", "isDefault": true }
  ],
  "pageInfo": {
    "page": 1,
    "pageSize": 50,
    "totalItems": 1,
    "totalPages": 1,
    "hasNextPage": false,
    "hasPreviousPage": false
  }
}
```

Object list:

```json
{
  "targetResourceId": 616,
  "database": "query_e2e",
  "items": [
    { "database": "query_e2e", "name": "query_e2e_items", "kind": "table" }
  ],
  "pageInfo": {
    "page": 1,
    "pageSize": 50,
    "totalItems": 1,
    "totalPages": 1,
    "hasNextPage": false,
    "hasPreviousPage": false
  }
}
```

Object detail:

```json
{
  "targetResourceId": 616,
  "database": "query_e2e",
  "name": "query_e2e_items",
  "kind": "table",
  "columns": [
    {
      "name": "id",
      "ordinalPosition": 1,
      "databaseType": "bigint",
      "nullable": false,
      "primaryKey": true,
      "autoIncrement": false
    }
  ],
  "indexes": [
    { "name": "PRIMARY", "columns": ["id"], "unique": true, "primary": true }
  ],
  "foreignKeys": [
    {
      "name": "fk_item_category",
      "columns": ["category_id"],
      "referencedDatabase": "query_e2e",
      "referencedObject": "query_e2e_categories",
      "referencedColumns": ["id"],
      "onUpdate": "RESTRICT",
      "onDelete": "RESTRICT"
    }
  ],
  "truncated": {
    "columns": false,
    "indexes": false,
    "foreignKeys": false
  }
}
```

Object detail intentionally excludes:

- column default values and comments;
- view/DDL definitions;
- sample rows or row counts;
- grants, users, owners, routines, triggers, or events;
- connection or credential material.

### Runtime Access Boundary

Schema introspection is allowed only when all of these are true:

- target exists and engine is MySQL or TiDB;
- connection metadata is complete;
- credential metadata exists, is enabled, and permits the target environment;
- credential reference resolves on the server;
- resolved DSN host and explicit port bind to the selected target;
- request carries a verified fresh bearer actor.

The backend MUST enforce these checks independently of frontend readiness.
Extract the smallest shared target-access resolver needed so query execution and
schema introspection cannot drift on credential, environment, or binding rules.
Preserve existing execution status/error behavior with characterization tests.
The resolved DSN stays inside the service-to-inspector call and must never enter
a model, cache key, response, audit row, or log.

### Inspector Queries

Implement a MySQL/TiDB schema inspector that uses fixed, parameterized queries
against `information_schema`:

- `SCHEMATA` for visible databases;
- `TABLES` for base tables and views;
- `COLUMNS` for ordered column metadata;
- `STATISTICS` for grouped indexes;
- `KEY_COLUMN_USAGE` plus `REFERENTIAL_CONSTRAINTS` for ordered foreign keys.

Requirements:

- never interpolate `database`, `name`, or `q` into SQL text;
- escape `%`, `_`, and the escape character before a `LIKE` search;
- deterministic ordering before pagination/grouping;
- one read-only transaction and at most one open target connection per request;
- same timeout policy as query execution: five seconds by default, three in
  production;
- response caps: 512 columns, 256 index-column rows, 256 foreign-key column
  mappings, with explicit `truncated` flags;
- fixed client-safe errors; never return raw driver errors.

### Caching

Backend cache:

- in-process only; no database persistence;
- five-minute positive TTL;
- 30-second negative TTL for empty lists/details;
- bounded key count with oldest-entry eviction;
- key includes target id, non-secret credential reference, database, object
  kind/name, and query/page inputs as applicable;
- cache hit still performs authorization and audit for the requesting actor;
- `refresh=true` bypasses and replaces only the requested key scope.

Cache correctness is eventual. Multi-instance cache synchronization is not a
Phase 38I requirement.

### Audit And Error Contract

Every schema API attempt that resolves a valid target must write one audit event:

```text
query.schema.databases.listed
query.schema.objects.listed
query.schema.object.read
```

Audit rows contain actor id, target resource id, fixed event type, and fixed
result only. Do not store database/object names, query text, DSN, username, or
password. A success response must not be returned if its audit event cannot be
persisted.

Controlled error categories:

- `400 schema_validation_failed` for invalid or oversized query parameters;
- `403 schema_not_allowed` for unsupported/locked/policy/credential/binding
  states;
- `404 schema_target_not_found` or `schema_object_not_found`;
- `408 schema_timeout`;
- `502 schema_backend_error` for target DB or required-audit failure;
- `500 internal_error` only for unexpected server failures.

## Frontend Experience

### Object Explorer

Replace the placeholder `QuerySchemaBrowser` with a lazy explorer inside the
final Phase 38H on-demand explorer surface. It must not reintroduce a permanent
third column or an all-object render.

Required behavior:

- active target identity remains in the compact workbench header;
- explorer has clear `Objects` and `Connections` modes rather than mixing the
  two trees;
- opening `Objects` fetches only the first bounded database page;
- expanding a database fetches only its first object page;
- expanding a table/view fetches only that object's details;
- show Tables and Views as distinct groups;
- object detail shows Columns, Keys, Indexes, and Foreign Keys as compact
  read-only groups;
- search is debounced, server-backed, and scoped to the active database;
- `Load more` appends bounded pages up to 500 loaded objects, then directs the
  user to search instead of growing the DOM indefinitely;
- every level has explicit loading, empty, error, retry, and refresh states;
- target/database changes abort or ignore stale responses;
- mobile uses the Phase 38H drawer, not a squeezed permanent sidebar.

Read-only object actions:

- copy identifier;
- insert quoted identifier at the active editor selection;
- refresh metadata;
- reveal an object selected from Quick Navigator.

No mutation or auto-execution action is allowed. Selecting an object must never
run a query.

### Per-Worksheet Database Context

Extend local worksheet state with `activeDatabase`.

- initialize from `defaultDatabase` when metadata first loads;
- keep it isolated per worksheet;
- worksheet switching restores its database context;
- changing database changes explorer scope and completion context only;
- never send `USE <database>` automatically;
- when inserting an object from a different database, insert a fully qualified
  MySQL/TiDB identifier such as `` `database`.`table` ``.

### Quick Navigator

Add `Cmd/Ctrl+P` on `/query`.

- opens an accessible command dialog for the active target;
- searches databases, tables, and views through bounded server requests;
- includes columns only for object details already loaded in the shared cache;
- keyboard Up/Down, Enter, Escape, focus trap, and visible focus are required;
- selecting a database changes the worksheet database context;
- selecting an object reveals it in Object Explorer and inserts its quoted
  identifier only when the user chooses the explicit Insert action;
- no routines, triggers, grants, query history, or cross-target search in 38I.

### Shared Browser Metadata Store

Explorer, Quick Navigator, and autocomplete must share one in-memory store:

- key by target id, database, kind, and object name;
- five-minute positive TTL;
- 30-second negative TTL for genuinely empty metadata;
- maximum 50 object-detail entries;
- maximum five concurrent object-detail requests;
- deduplicate identical in-flight requests;
- use AbortController/request generations to reject stale writes;
- clear on logout/auth failure and when the page unmounts;
- never persist to browser storage.

### SQL Autocomplete

Enable CodeMirror autocompletion with a controlled override.

Completion sources, in priority order:

1. columns for an explicitly qualified `table.` or `alias.` reference;
2. columns from tables referenced in the active statement when their metadata
   is loaded or can be fetched within the concurrency cap;
3. tables/views in the active database;
4. database-qualified objects;
5. approved read-only SQL keywords and common read-only functions.

Approved statement/keyword vocabulary must align with the existing guard:

- `SELECT`, `SHOW`, `DESCRIBE`, `DESC`, `EXPLAIN`;
- read-only clauses such as `FROM`, `JOIN`, `ON`, `WHERE`, `GROUP BY`,
  `HAVING`, `ORDER BY`, `LIMIT`, `OFFSET`, `WITH`, `AS`, and set operators;
- safe built-in functions already accepted by the guard.

Do not suggest write/DDL/session keywords such as `INSERT`, `UPDATE`, `DELETE`,
`CREATE`, `ALTER`, `DROP`, `TRUNCATE`, `CALL`, `SET`, `USE`, `GRANT`,
`BEGIN`, `COMMIT`, `ROLLBACK`, or locking clauses.

Autocomplete requirements:

- `Ctrl+Space` explicitly opens suggestions;
- normal typing and `.` can trigger suggestions;
- MySQL/TiDB identifiers are inserted with correct backtick quoting;
- aliases are resolved within the current statement, not from another
  worksheet;
- completion state is scoped by worksheet id, target id, and active database;
- a network failure degrades to keywords/loaded metadata and never blocks
  editing or execution;
- accepting a suggestion only edits text; backend validation still decides
  whether a statement can execute;
- no completion source may return DSN, username, password, credential ref, or
  host metadata.

## Scale And Accessibility Requirements

- No initial all-database/all-object/all-column fan-out.
- No more than one database-page request on opening Object Explorer.
- No more than one object-page request on expanding a database.
- No object-detail request until expand, explicit completion, or explicit
  Quick Navigator need.
- No more than five object-detail requests in flight.
- Object lists are bounded by server pagination and the 500-loaded-object UI
  cap.
- Tree/disclosure semantics expose `aria-expanded`, selected state, and counts.
- All actions are keyboard reachable; focus order follows visual order.
- Async errors use `role=alert`; loading/status messages use non-disruptive live
  regions.
- Light, dark, and high-contrast editor completion menus remain readable.
- Desktop, tablet, and 375px mobile layouts have no horizontal page overflow.

## Test Requirements

Backend unit/API tests:

- query parsing, defaults, bounds, boolean parameters, and identifier lengths;
- fresh auth and controlled status/error mapping;
- target/credential/policy/binding fail-closed matrix;
- parameterized search including literal `%`, `_`, quotes, and Unicode names;
- stable pagination and table/view classification;
- index/FK grouping and deterministic ordering;
- cache TTL, refresh, eviction, and no-DSN cache keys;
- audit on success/failure/cache hit and fail-loud audit errors;
- response/error/log leak scans.

Backend integration tests with dedicated MySQL:

- visible database list and default database;
- hidden system schemas by default and optional inclusion;
- paged/searchable tables and views;
- columns, composite index, primary key, and foreign key detail;
- object not found and DDL race behavior;
- read-only user cannot write;
- timeout and payload-cap behavior;
- OpenAPI validation and fuzz coverage for every new operation.

Frontend tests:

- service paths and exact query parameters;
- no forbidden request fields;
- lazy request counts and bounded page append;
- loading/empty/error/retry/refresh states;
- stale target/database/object response rejection;
- shared cache TTL, negative cache, eviction, in-flight dedupe, concurrency cap;
- per-worksheet active database isolation;
- accessible explorer and Quick Navigator keyboard behavior;
- quoted identifier insertion at the current selection;
- keyword/table/view/qualified table/column/alias completion;
- forbidden write keywords are absent;
- metadata failure preserves editor use and backend execution;
- no DSN/password/username/credential ref rendering or storage.

Real E2E:

- start the dedicated query MySQL and backend; do not use a fake backend;
- fixture contains at least two application databases, two tables, one view,
  one primary key, one secondary/composite index, and one foreign key;
- select the ready target and open Object Explorer;
- search a database object and expand its columns/indexes/foreign key;
- `Cmd/Ctrl+P` finds and reveals an object;
- table and alias-column autocomplete insert visible text in CodeMirror;
- execute a completed read-only SELECT successfully;
- existing SELECT/SHOW/DESCRIBE, worksheet isolation, locked target, credential
  admin, and unsafe-SQL rejection E2E remain green;
- no tests are silently skipped.

## Documentation Requirements

On completion, add a Phase 38I evidence note and update:

- `docs/quality-baseline.md`;
- the release-readiness summary if the phase is pushed;
- module README/file headers required by the repository protocol;
- the query roadmap/status note to mark the schema placeholder as replaced and
  ER/Visual Explain as future work.

## Success Criteria

Phase 38I is complete only when:

- Phase 38H is merged first and both repos were implemented from that baseline;
- all three schema APIs are authenticated, bounded, audited, OpenAPI-documented,
  and backed by parameterized `information_schema` queries;
- schema access reuses the same credential/policy/binding boundary as execution;
- Object Explorer loads real metadata lazily without unbounded fan-out;
- Quick Navigator and CodeMirror consume the same bounded metadata store;
- autocomplete suggests schema-aware read-only SQL and omits write/DDL keywords;
- metadata failures never leak secrets or disable ordinary editing/execution;
- backend integration and final real frontend E2E pass against the dedicated
  MySQL fixture;
- adversarial architecture, security, UX, accessibility, and diff reviews have
  no remaining P1/P2 findings;
- implementation is committed in focused backend/frontend commits with clean
  worktrees, but is not pushed unless separately authorized.

## Deferred Follow-Ups

After Phase 38I evidence is stable, evaluate separately:

- Phase 38J: result-grid affordances and foreign-key record navigation;
- Phase 38K: Visual EXPLAIN using backend-normalized plan data;
- later: ER diagram generated from the same schema API;
- later: saved queries/notebooks and governed collaboration;
- later: additional database engines through explicit inspector adapters.
