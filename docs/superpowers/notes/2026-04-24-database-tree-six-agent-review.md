# Database Tree Table — Six-Agent Review Findings

**Date:** 2026-04-24
**Scope:** `/databases` page tree-style database resource table
**Reviewers:** Frontend Developer, UX Architect, UX Researcher, UI Designer, Evidence Collector, API Tester

## CRITICAL

### C1. Expansion state leaks across paginated pages
- **Who found it:** Frontend Dev, UX Architect, Evidence Collector
- **Root cause:** `useReactTable` uses array index as row ID (no `getRowId`). When `data` changes on page navigation, expanded state keyed by index "0" applies to a different cluster on the new page.
- **Code:** `database-table.tsx:332-340` — `initialState: { expanded: {} }` with no stable row IDs
- **Fix:** Add `getRowId: (row) => String(row.original.id)` to use stable resource IDs, and reset expansion on page change.

### C2. Backend MaxPageSize=100, frontend requests 200
- **Who found it:** API Tester
- **Root cause:** `pagination.go:11` caps `MaxPageSize = 100`. Frontend requests `pageSize: 200` in `databases/page.tsx:24`. The backend silently returns only 100 items.
- **Code:** `internal/model/pagination.go:70` — `NormalizePagination` caps pageSize
- **Fix:** Raise `MaxPageSize` to 500, or implement proper server-side tree pagination.

### C3. Orphan instances with non-null clusterId silently dropped
- **Who found it:** Frontend Dev
- **Root cause:** `buildTree()` marks instances as orphans when their `clusterId` references a cluster not in the response. These orphans are appended to the tree. But `paginateTree()` filters top-level rows by `!row.clusterId` — orphans still have `clusterId` set, so they're excluded from the page count and never rendered.
- **Code:** `database-table.tsx:86-87` (orphan push) vs `database-table.tsx:110` (filter `!row.clusterId`)
- **Fix:** In `paginateTree`, filter top-level rows by checking if the row is in the top-level array returned by `buildTree` (no `clusterId` or `clusterId` not found), not just `!row.clusterId`.

### C4. Expand button 20x20px violates WCAG touch target (44px minimum)
- **Who found it:** UI Designer, Evidence Collector
- **Code:** `database-table.tsx:212` — `className="flex size-5"` (20x20px)
- **Fix:** Increase visual button to at least 44x44px with `h-11 w-11`, keep icon at `size-4`.

### C5. Cluster/instance rows distinguished only by 30% opacity background — not colorblind-safe
- **Who found it:** UI Designer
- **Code:** `database-table.tsx:444` — `bg-muted/30 hover:bg-muted/40`
- **Fix:** Add non-color visual markers: left border accent on cluster rows, or slightly different font weight.

### C6. pageSize selector disconnected from tree table, loses filters on change
- **Who found it:** UX Architect
- **Root cause:** `PaginationControls` writes `pageSize` to URL, but `DatabaseTable` hardcodes `CLUSTERS_PER_PAGE = 10` and never reads the URL param. Changing the dropdown triggers server navigation that can lose `resourceSubtype` filter state.
- **Code:** `database-table.tsx:50` vs `pagination-controls.tsx:66`
- **Fix:** Either read pageSize from URL in DatabaseTable, or hide the pageSize selector for the tree table.

## HIGH

### H1. Enter key on expand button opens detail sheet simultaneously
- **Who found it:** Evidence Collector
- **Root cause:** Expand button's `onClick` calls `e.stopPropagation()`, but row's `onKeyDown` fires on Enter key events that bubble up. `stopPropagation()` on click doesn't prevent keyboard event bubbling.
- **Code:** `database-table.tsx:206-209` (button onClick) vs `database-table.tsx:443-446` (row onKeyDown)
- **Fix:** Add `onKeyDown` handler on expand button that stops propagation for Enter/Space.

### H2. Search empty state message misleading
- **Who found it:** UX Researcher
- **Root cause:** Empty state always shows "暂无数据库资源 / 尚未登记任何数据库实例或集群" regardless of whether filters/search are active.
- **Code:** `database-table.tsx:420-427`
- **Fix:** Show different message when search or filters are active: "未找到匹配的数据库资源" / "尝试不同的搜索词或清除筛选条件".

### H3. profileSummary never populated in list API — hostname/port columns always show "—"
- **Who found it:** API Tester, Evidence Collector
- **Root cause:** `ListResources` repository method never joins profile data. The `profileSummary` field exists on the model but is never scanned/set.
- **Code:** `resource_repository.go` ListResources method; `database-table.tsx:304,316`
- **Fix:** Join profile summary data in the list query, or hide these columns when no data is available.

### H4. Expand button missing aria-label and aria-expanded
- **Who found it:** Frontend Dev, Evidence Collector
- **Code:** `database-table.tsx:203-218`
- **Fix:** Add `aria-label={row.getIsExpanded() ? "Collapse ..." : "Expand ..."}` and `aria-expanded={row.getIsExpanded()}`.

### H5. `<tr role="button">` breaks table ARIA semantics
- **Who found it:** Frontend Dev
- **Code:** `database-table.tsx:440` — `role="button"` on `<tr>`
- **Fix:** Keep `role="row"`, handle keyboard activation via tabIndex and onKeyDown without overriding the role.

## IMPORTANT

### I1. columns array not memoized — recreated every render
- **Who found it:** Frontend Dev
- **Fix:** Wrap in `useMemo` keyed on `locale`.

### I2. Search triggers server re-render, loses tree expansion
- **Who found it:** UX Architect
- **Fix:** Auto-expand parent clusters when search results include their instances.

### I3. clusterId missing from OpenAPI spec
- **Who found it:** API Tester
- **Fix:** Add `clusterId` to the Resource schema in `openapi.yaml`.

### I4. clusterId not returned in single-resource GET
- **Who found it:** API Tester
- **Fix:** Add the subquery to `GetResource` repository method, or document the limitation.

### I5. Page param NaN not handled
- **Who found it:** Frontend Dev
- **Code:** `database-table.tsx:153` — `Number(searchParams.get("page"))`
- **Fix:** Use `parseInt(...) || 1`.

### I6. Child row indentation inconsistent — name column 40px, others 24px
- **Who found it:** UI Designer
- **Fix:** Apply indentation only to expander + name columns, remove `pl-6` from other child cells.

### I7. DbTypeIcon renders at 20x17.78px due to non-square SVG aspect ratio
- **Who found it:** UI Designer
- **Fix:** Add `object-contain` or wrap in a fixed-size container.

## MINOR

- M1: `buildTree` mutates arrays in-place (Frontend Dev)
- M2: `totalTopLevels` computed outside useMemo (Frontend Dev)
- M3: searchDraft/search sync via useEffect creates extra render (Frontend Dev)
- M4: Badge font size inconsistency between cluster badge (10px) and status badges (11px) (UI Designer)
- M5: Posture counts don't reflect current page scope (UX Architect)
- M6: ProxySQL/CHProxy mixed with database engines in filter (UX Architect)
- M7: Console warnings for SVG aspect ratio on redis/clickhouse icons (Evidence Collector)

## SUGGESTIONS

- Extract `buildTree`/`paginateTree` to utility module for unit testing
- Increase test coverage beyond 1 test
- Add sticky table header for scrolling context
- Consider dedicated `/resources/database-tree` API endpoint
- Add environment filter alongside engine filter
- Show engine filter counts (e.g., "MySQL (8)")
