# Database Tree Table — Review Fixes Design

**Date:** 2026-04-24
**Status:** Draft
**Scope:** Fixes for six-agent review findings on `/databases` tree table

---

## Design Principles

1. **Minimal diff** — Targeted fixes only, no refactoring of working code
2. **Accessibility first** — All CRITICAL a11y issues must be resolved
3. **No new dependencies** — Use existing components and patterns
4. **Preserve existing behavior** — Only change what's broken

---

## Fix 1: Stable Row IDs & Expansion Reset (C1, H4, H5)

**Problem:** TanStack Table uses array index as row ID. Page changes cause expansion state to leak to different rows. Expand button lacks ARIA attributes. Row `role="button"` breaks table semantics.

**Solution:**
- Add `getRowId: (row) => String(row.original.id)` to `useReactTable` config
- Switch from `initialState` to controlled `state` + `onExpandedChange` so we can reset on page change
- Reset expansion state when `page` changes (use `useEffect` on `page`)
- Add `aria-label` and `aria-expanded` to expand button
- Change `<tr role="button">` to `<tr role="row" tabIndex={0}>` — keep table semantics, still keyboard accessible

**Files:** `components/databases/database-table.tsx`

---

## Fix 2: Backend MaxPageSize + Orphan Pagination (C2, C3)

**Problem:** Backend caps pageSize at 100. Frontend requests 200. Also, orphan instances with non-null `clusterId` are excluded from pagination.

**Solution:**
- Raise `MaxPageSize` from 100 to 500 in `internal/model/pagination.go`
- Fix `paginateTree` to use the tree's top-level ordering from `buildTree` instead of filtering by `!row.clusterId`. `buildTree` already returns orphans as top-level nodes — `paginateTree` should iterate the same top-level list.

**Files:** `internal/model/pagination.go`, `components/databases/database-table.tsx`

---

## Fix 3: Expand Button Accessibility (C4, C5, H1)

**Problem:** Button too small (20px), no keyboard event isolation, cluster/instance rows only distinguished by color.

**Solution:**
- Increase expand button to `h-11 w-11` (44px) with icon `size-4` inside, centered via flexbox
- Add `onKeyDown` to expand button that stops propagation for Enter and Space keys
- Add left border accent on cluster rows: `border-l-2 border-l-primary/40` for non-color distinction
- Cluster rows: add `font-medium` to the row content area (already partially present)

**Files:** `components/databases/database-table.tsx`

---

## Fix 4: Search Empty State (H2)

**Problem:** When search/filter returns no results, message says "no databases registered" instead of "no matches".

**Solution:**
- Pass `hasActiveFilters` prop (computed from `search || selectedEngines.length > 0`) to `EmptyState`
- Add two new i18n keys: `tables.databases.emptyFilterTitle` / `tables.databases.emptyFilterDescription`
- Conditionally render different empty state text based on filter state

**Files:** `components/databases/database-table.tsx`, `messages/en.json`, `messages/zh-CN.json`

---

## Fix 5: Hide Disconnected PageSize Selector (C6)

**Problem:** The pageSize dropdown in `PaginationControls` writes to URL but `DatabaseTable` ignores it. Changing it can lose filter state.

**Solution:**
- `DatabaseTable` reads `pageSize` from URL search params instead of hardcoding `CLUSTERS_PER_PAGE`
- Falls back to default of 10 if not specified
- Remove the `CLUSTERS_PER_PAGE` constant

**Files:** `components/databases/database-table.tsx`

---

## Fix 6: Columns Memoization + Page NaN Guard (I1, I5)

**Problem:** `columns` array recreated every render. `page` param parsed without NaN protection.

**Solution:**
- Wrap `columns` in `useMemo` keyed on `locale`
- Parse page with `parseInt(...) || 1`

**Files:** `components/databases/database-table.tsx`

---

## Fix 7: Child Row Indentation Consistency (I6)

**Problem:** Name column has 40px indent (pl-6 on cell + pl-4 on inner span), other columns only 24px (pl-6 on cell).

**Solution:**
- Remove `pl-6` from all child `<TableCell>` elements
- Apply indentation only in the name column via the inner span's `pl-4`
- This makes all columns align consistently — only the name column content is indented

**Files:** `components/databases/database-table.tsx`

---

## Fix 8: OpenAPI Spec — Add clusterId (I3)

**Problem:** `clusterId` field exists in API response but not documented in OpenAPI spec.

**Solution:**
- Add `clusterId` property to Resource schema in `openapi.yaml`

**Files:** `internal/openapi/openapi.yaml`

---

## Fix 9: DbTypeIcon Aspect Ratio (I7)

**Problem:** SVG icons render at 20x17.78px due to non-square aspect ratios, causing vertical misalignment.

**Solution:**
- Add `object-contain` class to the `<Image>` in `DbTypeIcon` component

**Files:** `components/blocks/db-type-icon.tsx`

---

## Out of Scope (deferred)

- **profileSummary population** (H3) — requires backend schema change to join profile tables in list query; significant work, separate task
- **clusterId in single-resource GET** (I4) — low priority, detail page works without it
- **Search auto-expand** (I2) — nice-to-have, complex to implement
- **Dedicated database-tree API** — architecture change, future consideration
- **Sticky table header** — CSS-only enhancement, can be done anytime
- **Environment filter** — feature addition, not a fix

---

## Impact Assessment

| Fix | User-Visible? | Risk | Files Changed |
|-----|--------------|------|----------------|
| Fix 1: Stable row IDs + expansion | Yes — stops state leak | Medium — table state management | database-table.tsx |
| Fix 2: MaxPageSize + orphan fix | Yes — prevents data loss | Low — config change + logic fix | pagination.go, database-table.tsx |
| Fix 3: Button a11y + visual distinction | Yes — better interaction | Low — styling changes | database-table.tsx |
| Fix 4: Empty state message | Yes — correct messaging | Low — conditional render | database-table.tsx, en.json, zh-CN.json |
| Fix 5: PageSize from URL | Yes — functional selector | Low — read existing param | database-table.tsx |
| Fix 6: Memo + NaN guard | No — perf/robustness | Low — useMemo + parseInt | database-table.tsx |
| Fix 7: Indentation | Yes — visual consistency | Low — CSS changes | database-table.tsx |
| Fix 8: OpenAPI clusterId | No — documentation | Minimal — add property | openapi.yaml |
| Fix 9: Icon aspect ratio | Yes — visual alignment | Minimal — one class | db-type-icon.tsx |
