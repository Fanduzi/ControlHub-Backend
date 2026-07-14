# Phase 38K Governed Object Inspector Metadata Specification

## Status

Accepted 2026-07-14. This specification defines Phase 38K Delivery A.

## Background

Object Explorer already retrieves governed object detail through
`GET /query-targets/{id}/schema/object-details`. The response has ordered
columns, indexes, foreign keys, and independent truncation flags, but the
workbench currently renders only counts. Users therefore cannot understand a
table's shape, key membership, index composition, or foreign-key targets while
working with a schema preview or related-record navigation result.

## Goal

Provide a localized, accessible Object Inspector for already-loaded table and
view metadata. It must make columns, indexes, and foreign keys readable while
preserving existing schema, query, credential, and result-data boundaries.

## User Flow

1. A user opens Objects for a ready target and expands a table or view.
2. Object Explorer loads the existing governed object detail.
3. Ready detail exposes an explicit **Inspect** action.
4. Inspect opens a read-only panel or Sheet for that exact object.
5. The panel shows Columns, Indexes, and Foreign keys, including empty and
   truncation states.
6. Close or Escape returns focus to Inspect.

Inspector opening must not run SQL, issue a second object-detail request,
create a worksheet, mutate a result, or initiate FK navigation.

## Scope

### In Scope

- Existing normalized metadata for `table` and `view` objects.
- Columns: ordinal, name, database type, nullable, primary-key, and
  auto-increment state.
- Indexes: name, ordered columns, unique state, and primary state.
- Foreign keys: constraint, ordered local and referenced columns, referenced
  database/object, and update/delete rules.
- Per-section empty and truncation notices, EN/ZH, keyboard/focus, desktop,
  mobile Sheet, dark theme, component tests, and real E2E.

### Out Of Scope

- New backend route, OpenAPI, schema-cache change, migration, or SQL guard
  change.
- `SHOW CREATE`, raw DDL, definition text, view definitions, copy/export, or
  data samples.
- Context menus, ER diagrams, reverse FK navigation, Visual Explain, saved
  queries, AI/MCP, persistence, or a third persistent workbench column.
- Browser database access, credentials, DSNs, usernames, actor IDs, raw driver
  errors, or result values in Inspector state or display.

## Data Contract And Trust Boundary

Delivery A uses only the authenticated existing endpoint:

```text
GET /query-targets/{id}/schema/object-details?database={database}&object={object}
```

The backend remains responsible for target access, fixed parameterized
`information_schema` queries in a read-only transaction, response caps, cache,
audit, and controlled failures. Inspector consumes the detail already loaded by
Object Explorer. It must not derive identity from SQL/results, refetch detail,
or persist metadata in the URL, localStorage, worksheet state, or global cache.

## UX And Accessibility Requirements

- Inspect is an explicit native localized button, never hover-only or
  context-menu-only.
- The transient Inspector has a localized semantic title, native close, Escape,
  focus trap, and deterministic component-local focus restoration.
- Desktop must not gain a third fixed pane. Mobile uses a labelled Sheet.
- API ordering is preserved; composite index/FK columns are never re-sorted.
- State badges have text labels in addition to color. Wide keys remain usable
  on small screens.
- Target switch, source collapse/reload, or stale detail state closes Inspector;
  it never shows data from a previous target.

## Empty And Truncation States

`truncated.columns`, `truncated.indexes`, and `truncated.foreignKeys` are
independent. The matching section must show localized incomplete-data copy and
must never infer completeness from a non-empty list. Empty valid arrays show a
localized empty state, not an error or a hidden section.

## Acceptance Criteria

- Ready tables/views open Inspector from already-loaded detail.
- All normalized column/index/FK fields render in response order with correct
  empty and independent truncation states.
- Inspector open triggers no execute, preview, related-record, or extra detail
  request; existing Preview rows and FK navigation remain unchanged.
- Stale target/object state cannot remain visible after target switch, collapse,
  reload, or late response.
- EN/ZH, keyboard, desktop, 375px mobile, focus, dark theme, unit tests,
  typecheck, lint, build, governance, and real E2E pass with no new skips.
- No backend/API/SQL guard change, definition SQL, context menu, secret,
  credential, result value, or browser persistence is introduced.

## Deferred: Phase 38K Delivery B

Governed table-definition text is a separate backend-first decision. It must
specify table-only initial scope, normalization/redaction policy, payload cap,
cache/audit behavior, public errors, and integration coverage. `SHOW CREATE
VIEW` is excluded until a dedicated policy handles the MySQL `DEFINER`
disclosure surface.
