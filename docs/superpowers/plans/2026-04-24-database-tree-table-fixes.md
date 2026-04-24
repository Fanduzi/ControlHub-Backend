# Database Tree Table Review Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix all CRITICAL and HIGH issues found by six-agent review of the database tree table, plus IMPORTANT items that are low-risk.

**Architecture:** Targeted fixes to existing files — no new components, no refactoring. Nine focused tasks touching 5 files total.

**Tech Stack:** React, TanStack Table, Tailwind CSS, Next.js, Go (chi), MySQL

---

### Task 1: Stable Row IDs & Controlled Expansion State

**Files:**
- Modify: `frontend/components/databases/database-table.tsx:332-340`

This fixes C1 (expansion state leak), H4 (aria attributes), and H5 (role="button").

- [ ] **Step 1: Add controlled expansion state**

Replace `initialState: { expanded: {} }` with controlled state:

```tsx
const [expanded, setExpanded] = useState<Record<string, boolean>>({});
```

Add `useEffect` to reset expansion when page changes:

```tsx
useEffect(() => {
  setExpanded({});
}, [page]);
```

- [ ] **Step 2: Update useReactTable config**

```tsx
const table = useReactTable({
  data: pagedTree,
  columns,
  getCoreRowModel: getCoreRowModel(),
  getExpandedRowModel: getExpandedRowModel(),
  getSubRows: (row) => row.subRows,
  getRowCanExpand: (row) => (row.original.subRows?.length ?? 0) > 0,
  getRowId: (row) => String(row.original.id),
  state: { expanded },
  onExpandedChange: setExpanded,
});
```

- [ ] **Step 3: Add aria attributes to expand button**

In the expander column cell (lines 203-218), update the button:

```tsx
<button
  type="button"
  onClick={(e) => {
    e.stopPropagation();
    row.toggleExpanded();
  }}
  onKeyDown={(e) => {
    if (e.key === "Enter" || e.key === " ") {
      e.stopPropagation();
    }
  }}
  aria-label={row.getIsExpanded() ? `Collapse ${row.original.displayName}` : `Expand ${row.original.displayName}`}
  aria-expanded={row.getIsExpanded()}
  className="flex size-11 items-center justify-center rounded text-muted-foreground hover:text-foreground"
>
```

Note: `size-11` = 44px — meets WCAG touch target.

- [ ] **Step 4: Fix row role from "button" to "row"**

Change line ~440:

```tsx
<TableRow
  key={row.id}
  role="row"
  tabIndex={0}
  aria-label={`View details for ${row.original.displayName}`}
```

- [ ] **Step 5: Verify no TypeScript errors**

Run: `cd /Users/fan/JsProjects/ControlHub && npx tsc --noEmit 2>&1 | grep database-table`
Expected: No errors

---

### Task 2: Fix Backend MaxPageSize

**Files:**
- Modify: `backend/internal/model/pagination.go:11`

- [ ] **Step 1: Raise MaxPageSize from 100 to 500**

In `internal/model/pagination.go`, change:

```go
MaxPageSize = 500
```

- [ ] **Step 2: Verify backend compiles**

Run: `cd /Users/fan/GolangProjects/ControlHub && go build ./...`
Expected: No errors

---

### Task 3: Fix Orphan Instance Pagination

**Files:**
- Modify: `frontend/components/databases/database-table.tsx:108-124`

- [ ] **Step 1: Fix paginateTree to use buildTree's top-level ordering**

Replace the current `paginateTree` function:

```tsx
function paginateTree(tree: TreeRow[], page: number, perPage: number) {
  const topLevels = tree.filter((row) => !row.subRows && !row.clusterId || (row.clusterId && row.subRows === undefined));
  const totalPages = Math.max(1, Math.ceil(topLevels.length / perPage));
  const safePage = Math.min(page, totalPages);
  const offset = (safePage - 1) * perPage;
  const slice = topLevels.slice(offset, offset + perPage);

  const pageIds = new Set(slice.map((r) => r.id));
  const pagedTree: TreeRow[] = [];
  for (const node of tree) {
    if (pageIds.has(node.id)) {
      pagedTree.push(node);
    }
  }

  return { pagedTree, totalPages, safePage };
}
```

Wait — that's wrong. The issue is that `buildTree` returns orphans as top-level nodes in the flat array, but `paginateTree` uses `!row.clusterId` to find top-level rows. Orphans from `buildTree` DO have `clusterId` set (the cluster just wasn't in the data). The fix: `paginateTree` should track top-level rows the same way `buildTree` does — by checking if the row was placed as a top-level item (clusters + orphans), not by `clusterId`.

The correct fix is simpler. `buildTree` returns a flat array where top-level nodes (clusters + orphans) appear first, followed by nothing (children are in `subRows`). So the top-level nodes are exactly the ones that are NOT children of another node. We can identify them by checking if they have `subRows` defined (clusters) or if they are in the orphan list.

Actually, the simplest fix: make `buildTree` return a separate list of top-level row IDs, and pass that to `paginateTree`. Or: mark orphan rows by clearing their `clusterId` in `buildTree`.

Best approach: In `buildTree`, clear `clusterId` on orphan instances so they match `!row.clusterId`:

```tsx
} else {
  orphans.push({ ...r, clusterId: undefined });
}
```

This way `paginateTree`'s `!row.clusterId` filter works correctly for both clusters and orphans.

- [ ] **Step 2: Verify TypeScript compiles**

Run: `cd /Users/fan/JsProjects/ControlHub && npx tsc --noEmit 2>&1 | grep database-table`
Expected: No errors

---

### Task 4: Visual Cluster/Instance Distinction (Non-Color)

**Files:**
- Modify: `frontend/components/databases/database-table.tsx:444`

- [ ] **Step 1: Add left border accent to cluster rows**

Change the cluster row className:

```tsx
className={`cursor-pointer transition-colors border-l-2 ${
  isCluster
    ? "border-l-primary/40 bg-muted/30 hover:bg-muted/40"
    : "border-l-transparent"
}`}
```

This adds a colored left border on clusters (visible regardless of color vision) and keeps instance rows with a transparent border (same height, no visual change).

- [ ] **Step 2: Verify visually**

Navigate to `http://localhost:3000/databases`, confirm cluster rows have a visible left accent bar.

---

### Task 5: Search Empty State Messages

**Files:**
- Modify: `frontend/components/databases/database-table.tsx:420-427`
- Modify: `frontend/messages/en.json`
- Modify: `frontend/messages/zh-CN.json`

- [ ] **Step 1: Add i18n keys for filtered empty state**

In `messages/en.json`, under `tables.databases`:

```json
"emptyFilterTitle": "No matching databases",
"emptyFilterDescription": "No databases match your current search or filter. Try different keywords or clear filters."
```

In `messages/zh-CN.json`, under `tables.databases`:

```json
"emptyFilterTitle": "未找到匹配的数据库",
"emptyFilterDescription": "当前搜索或筛选条件下没有匹配的数据库。请尝试不同的关键词或清除筛选条件。"
```

- [ ] **Step 2: Update empty state rendering in component**

Compute active filter state:

```tsx
const hasActiveFilters = search.trim().length > 0 || selectedEngines.length > 0;
```

Replace the empty state block (around line 424):

```tsx
<EmptyState
  title={hasActiveFilters ? t("tables.databases.emptyFilterTitle") : t("tables.databases.emptyTitle")}
  description={hasActiveFilters ? t("tables.databases.emptyFilterDescription") : t("tables.databases.emptyDescription")}
/>
```

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/fan/JsProjects/ControlHub && npx tsc --noEmit 2>&1 | grep database-table`
Expected: No errors

---

### Task 6: Read PageSize from URL

**Files:**
- Modify: `frontend/components/databases/database-table.tsx:50,153`

- [ ] **Step 1: Remove CLUSTERS_PER_PAGE constant, read from URL**

Remove line 50:

```tsx
const CLUSTERS_PER_PAGE = 10;
```

Add page size reading after `page`:

```tsx
const page = parseInt(searchParams.get("page") ?? "1", 10) || 1;
const clustersPerPage = parseInt(searchParams.get("pageSize") ?? "10", 10) || 10;
```

Update all references from `CLUSTERS_PER_PAGE` to `clustersPerPage`.

- [ ] **Step 2: Verify pagination still works**

Navigate to `http://localhost:3000/databases`, confirm 10 clusters per page. Change pageSize dropdown to 20, confirm table updates.

---

### Task 7: Columns Memoization + Page NaN Guard

**Files:**
- Modify: `frontend/components/databases/database-table.tsx`

- [ ] **Step 1: Memoize columns array**

Wrap the `columns` array (lines 194-330) in `useMemo`:

```tsx
const columns = useMemo(() => [
  columnHelper.display({ ... }),
  columnHelper.accessor("displayName", { ... }),
  // ... all columns
], [locale, t]);
```

Note: move the columns definition AFTER `handleRowClick` and other hooks to avoid hook ordering issues.

- [ ] **Step 2: Page NaN guard was already done in Task 6**

The `parseInt(...) || 1` pattern handles NaN.

- [ ] **Step 3: Verify TypeScript compiles**

Run: `cd /Users/fan/JsProjects/ControlHub && npx tsc --noEmit 2>&1 | grep database-table`
Expected: No errors

---

### Task 8: Fix Child Row Indentation

**Files:**
- Modify: `frontend/components/databases/database-table.tsx:451`

- [ ] **Step 1: Remove pl-6 from child cells, keep indentation only in name column**

Change the child cell className:

```tsx
<TableCell key={cell.id}>
```

(Remove the `className={isChild ? "pl-6" : ""}` from TableCell)

The indentation is already handled by the inner span `className="pl-4"` in the displayName column. Other columns should align with their parent row.

- [ ] **Step 2: Verify visually**

Navigate to `http://localhost:3000/databases`, expand a cluster. Confirm child row cells in Environment, Owner, Engine, Status columns align with parent row cells. Only the Name column content is indented.

---

### Task 9: OpenAPI Spec + DbTypeIcon Fix

**Files:**
- Modify: `backend/internal/openapi/openapi.yaml`
- Modify: `frontend/components/blocks/db-type-icon.tsx`

- [ ] **Step 1: Add clusterId to OpenAPI Resource schema**

In `openapi.yaml`, find the Resource properties section and add:

```yaml
clusterId:
  type: integer
  nullable: true
  description: "For database instances, the ID of the parent cluster. Null for clusters and other resource types."
```

- [ ] **Step 2: Fix DbTypeIcon aspect ratio**

Read the current `db-type-icon.tsx`, add `object-contain` to the Image element's className or use a wrapper approach.

- [ ] **Step 3: Verify backend OpenAPI validates**

Run: `cd /Users/fan/GolangProjects/ControlHub && make openapi-validate`
Expected: PASS

- [ ] **Step 4: Commit all changes**

```bash
cd /Users/fan/GolangProjects/ControlHub && git add -A && git status
```

Review the diff, then commit both frontend and backend changes.
