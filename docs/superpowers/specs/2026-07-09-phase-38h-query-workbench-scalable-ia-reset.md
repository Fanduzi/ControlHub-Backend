# Phase 38H Query Workbench Scalable IA Reset Design

## Background

Phase 38G improved several visible details, but manual preview and two
read-only design reviews showed a deeper information-architecture problem:
the Query Workbench still behaves like a collection of panels instead of a
database IDE.

The failing patterns are:

- `/query` uses a three-column layout: persistent connection list, editor, and
  Governance & Access. This over-weights navigation and policy metadata while
  shrinking the editor/result surface.
- The connection navigator still renders like an all-target list. That cannot
  scale to hundreds or thousands of instances.
- Governance details are important, but they do not justify a permanent right
  column for every query.
- `/settings/query-credentials` is an admin inventory page, but it currently
  depends on all-target loading and a detail surface that competes with the
  table. Small screens need a modal/full-screen drawer, not a squeezed
  side-by-side inspector.
- Frontend-only filtering is now the wrong abstraction. Without server-side
  pagination/search, UI changes will keep becoming fake optimizations.

Phase 38H is therefore a full-stack IA reset. It must first make the query
target list scalable at the API level, then simplify the frontend into a
database-tool layout.

## Goal

Make Query Workbench and Query Credential Management usable at large target
counts without pretending the frontend can render or fan out across every
target.

Phase 38H should produce:

- a paged/searchable query-target contract;
- a query page whose main surface is the editor/results, not policy chrome;
- a bounded target switcher/explorer that never dumps all connections by
  default;
- credential management with paged inventory and responsive modal/drawer
  detail editing;
- tests proving the UI does not depend on rendering all targets.

## Non-Goals

- No SQL guard behavior change.
- No new query engines.
- No DSN/password input, browser storage, response display, or logs.
- No credential edit controls inside `/query`.
- No `actorUserId` in requests.
- No saved query implementation.
- No export implementation.
- No approval/JIT implementation.
- No worksheet backend persistence.
- No production deployment, tag, or release.
- No frontend-only "pagination" that still loads every target and all
  credential statuses at initial render.

## Product Principles

1. **Search-first at fleet scale.** Empty state shows active/recent/favorite
   targets and a bounded page, not every target.
2. **The editor owns the page.** SQL editor, schema/object browser, result
   grid, and history are the persistent work surfaces.
3. **Governance is status, not layout.** Show one blocker and compact policy
   badges inline; details open on demand.
4. **Admin inventory is table-first.** Credential management starts with a
   paged, searchable table; editing opens a modal/drawer.
5. **Server contracts must match UI scale.** If the UI claims to support 1000
   targets, backend pagination/search and bounded credential lookup must exist.

## Requirements

### H1. Paged Query Target API

Extend `GET /query-targets` without breaking the existing envelope shape.

Required query parameters:

- `q`: optional text search across display name, resource name, host, engine,
  environment, cluster name, and readiness/searchable status fields where
  available.
- `page`: optional positive integer, default `1`.
- `pageSize`: optional positive integer, default `50`, max `100`.
- Existing filters such as `engine` and `environmentId` must keep working.

Required response shape:

```json
{
  "items": [],
  "pageInfo": {
    "page": 1,
    "pageSize": 50,
    "totalItems": 0,
    "totalPages": 0,
    "hasNextPage": false,
    "hasPreviousPage": false
  }
}
```

Backwards compatibility:

- Keep `items` at the top level.
- Frontend code that only reads `items` should still work.
- OpenAPI and fuzz tests must be updated.

Acceptance criteria:

- Invalid `page`, `pageSize`, or oversized `pageSize` returns `400`.
- A 1000-target fixture/search test returns a bounded page.
- Query target list tests prove `q` matches name, host, engine, environment,
  and cluster.
- No credential secret is introduced into search or response fields.

### H2. Bounded Credential Status Loading

Credential admin must stop issuing credential-status requests for every target
across the whole fleet on initial load.

Required behavior:

- On initial load, only current-page targets request credential statuses.
- Coverage/summary cards must either:
  - clearly represent the current filtered/page scope; or
  - come from a backend aggregate endpoint if implemented in this phase.
- Bulk actions must be scoped to explicitly selected visible rows unless a
  server-backed "select all matching filter" contract exists.

Acceptance criteria:

- With a 1000-target fixture, initial credential admin render does not issue
  1000 `GET /query-targets/{id}/credential` calls.
- UI copy states whether counts are current-page/current-filter or global.
- Tests cover page change and stale status responses.

### H3. Query Workbench Two-Region Layout

Remove the permanent three-column layout.

Required desktop structure:

- left region: collapsible explorer for schema and bounded connection search;
- main region: active target header, worksheet tabs, SQL editor, result/history;
- no permanent right Governance & Access column.

Required mobile/tablet structure:

- explorer opens as a drawer;
- governance details open as a drawer/popover;
- editor/results remain the primary flow.

Acceptance criteria:

- `/query` no longer renders editor plus permanent governance as a three-column
  desktop layout.
- Active target facts appear once in the compact header.
- Left explorer does not render more than the current bounded target page or
  search results.
- Current target stays visible in the header even when search/filter results do
  not include it.

### H4. Scalable Target Switcher

Connection selection must be command/search-first.

Required behavior:

- Empty switcher state shows recent/favorite targets and a bounded page, not
  all targets.
- Search is debounced and server-backed via `q`.
- Search matches display name, resource name, engine, environment, host, port,
  cluster, and readiness.
- Active target id is URL-addressable, for example `/query?targetId=616`.
- Ready targets are visually prioritized when shown in a result page.

Acceptance criteria:

- A user can jump to a known host/name without scanning a list.
- URL target id selects the target after load.
- If target id is not in the current page, the UI fetches or preserves enough
  context to show the active target header.
- E2E covers search, URL target selection, and switching.

### H5. Governance Inline Status And Details

Governance becomes compact and contextual.

Required behavior:

- Show one primary blocker inline near the editor/run controls when execution
  is locked.
- Show compact badges for read-only boundary, credential status, audit, and
  policy.
- Detailed explanations open from a "Details" control.
- Admin credential link appears only when credential state/action makes it
  relevant.

Acceptance criteria:

- No permanent right governance column.
- Locked target exposes the reason without requiring a separate panel scan.
- Ready target does not waste vertical space on safety education.
- Existing no-credential-edit-controls-on-`/query` tests remain.

### H6. Credential Management Pagination And Responsive Detail

Credential settings must be a scalable operations table.

Required behavior:

- Paged table with page size options `25`, `50`, `100`.
- Search/filter controls use server-backed query-target pagination where
  possible.
- Row click selects/highlights the row.
- "Manage" opens an edit surface:
  - desktop wide: side drawer or modal;
  - tablet/mobile: full-screen modal/drawer;
  - no inline table expansion and no bottom-of-page form.
- Focus trap, Esc close, save/delete success, and stale-target guard remain.

Acceptance criteria:

- Table shows page info and page navigation.
- Editing a target is visible immediately after row action.
- Small-screen test proves the edit UI is not a squeezed right panel.
- Delete success feedback remains visible in the edit surface.

## Test Requirements

Backend:

- unit tests for query parsing and page bounds;
- repository/service tests for `q`, `page`, `pageSize`, and total counts;
- integration tests with enough rows to prove pagination;
- OpenAPI validation and fuzz tests updated for `pageInfo`.

Frontend:

- service tests for paged query-target response;
- Query Workbench tests for bounded switcher, URL target id, no governance
  right column, and active-target preservation;
- credential settings tests for paged status loading, modal/drawer behavior,
  focus/close behavior, and stale-response guards;
- E2E with enough fixture/mock targets to prove the UI does not render all
  targets or fan out all statuses.

Real E2E:

- existing ready-target SELECT/SHOW/DESCRIBE flows still pass;
- query credential settings still allows admin metadata management;
- no fake backend in final E2E.

## Documentation Requirements

Update:

- Phase 38G evidence or preview issue status to state that Phase 38G improved
  visual details but did not solve scalable IA.
- quality baseline with Phase 38H gates and E2E evidence.
- release readiness summary if this phase is pushed.

## Success Criteria

Phase 38H is complete only when:

- query target API supports pagination/search with OpenAPI and tests;
- `/query` no longer has the left-list/editor/right-governance three-column
  IA;
- target switching is bounded, searchable, and URL-addressable;
- credential admin is paged and uses modal/drawer detail;
- final real E2E passes against backend plus dedicated query MySQL fixture;
- no P1/P2 findings remain after adversarial UI/UX review.
