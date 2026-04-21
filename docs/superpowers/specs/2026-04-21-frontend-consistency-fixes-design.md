# Frontend Consistency Fixes — Design Document

**Date:** 2026-04-21
**Source audit:** `docs/superpowers/audits/2026-04-21-frontend-ux-ui-audit.md`
**Scope:** P0 + P1 items from audit (8 CRITICAL + 10 RECOMMENDED)
**Out of scope:** Mobile navigation (C1/C7 — separate sprint), IA restructuring (C8)

---

## 1. Shared Severity Color Map

**Problem:** Severity colors (rose/amber/sky/emerald) and their shade levels (-400/-500/-600/-700) are scattered across 6+ components with inconsistent choices.

**Solution:** Extract a single `lib/severity-colors.ts` module exporting typed color maps.

```typescript
// Health status colors (fixed palette, never follows accent theme)
// Lifecycle colors for non-standard states
// Audit result colors (success/warning/error)
// Each entry: { bg, text, border } with light + dark variants
```

**Rules:**
- Health colors are palette-locked: healthy=emerald, warning=amber, critical=rose, degraded=orange, unknown=muted
- All severity border accents use -500 shade (not -400)
- Dark mode text uses -400 shade for readability on dark surfaces
- StatusBadge healthy state uses emerald explicitly, NOT `primary`

**Files changed:**
- Create `lib/severity-colors.ts`
- Modify: `status-badge.tsx`, `audit-table.tsx` (RESULT_DOT, RESULT_ROW_BORDER), `overview-content.tsx` (attention borders, posture metric colors)

---

## 2. Shared ResourceLink Component

**Problem:** Resource link styling differs across 4 tables (underline-offset, hover opacity, focus ring, transition).

**Solution:** Create `components/blocks/resource-link.tsx`.

```typescript
type ResourceLinkProps = {
  href: string;
  children: ReactNode;
  className?: string;
};
```

Canonical style: `font-medium text-foreground hover:text-primary hover:underline underline-offset-4 focus-visible:outline-2 focus-visible:outline-ring/50 transition-colors`

**Files changed:**
- Create `components/blocks/resource-link.tsx`
- Modify: `resource-table.tsx`, `database-table.tsx`, `audit-table.tsx`, `cluster-members-table.tsx`

---

## 3. StatusBadge Healthy Color Fix

**Problem:** Healthy uses `bg-primary/10 text-primary` which follows accent theme. When accent=amber, healthy looks like warning.

**Solution:** Add explicit emerald data-attributes for healthy in the health tone string. Remove the fallback-to-primary behavior.

Before:
```
bg-primary/10 text-primary data-[status=healthy]:bg-emerald-500/10 data-[status=healthy]:text-emerald-700 dark:data-[status=healthy]:text-emerald-300
```

The healthy overrides already exist but the base `bg-primary/10 text-primary` applies when data-status doesn't match any known health value. After the fix, only "unknown" uses the muted fallback, and the default (no matching data-status) also uses muted.

**Files changed:** `status-badge.tsx`

---

## 4. Audit Search Wiring

**Problem:** Search Input renders but has no value/onChange — inert decoration.

**Solution:** Add client-side filter state. The search filters the already-fetched `events` array by eventType label, targetResourceName, actorLabel, and environmentLabel. URL search params not required — this is a table-level client filter, similar to how the overview filters resources.

```typescript
const [search, setSearch] = useState("");
const filtered = useMemo(() => {
  if (!search.trim()) return events;
  const q = search.toLowerCase();
  return events.filter(e =>
    e.targetResourceName.toLowerCase().includes(q) ||
    e.actorLabel.toLowerCase().includes(q) ||
    getEventTypeLabel(e.eventType).toLowerCase().includes(q) ||
    e.environmentLabel.toLowerCase().includes(q)
  );
}, [events, search]);
```

The table renders `filtered` instead of `events`. The pagination still uses server-side `pageInfo`.

**Files changed:** `audit-table.tsx`

---

## 5. Posture Grid Dark Mode Fix

**Problem:** `gap-px` with `bg-border` gap color is nearly transparent in dark mode. Cells merge visually.

**Solution:** Replace `bg-border` gap technique with explicit border on each cell. Use `divide-x` or per-cell `border-r` with `border-border` which is properly themed.

Before:
```
grid grid-cols-2 overflow-hidden rounded-lg border border-border bg-border gap-px
```

After:
```
grid grid-cols-2 rounded-lg border border-border divide-x divide-border sm:grid-cols-4 divide-y sm:divide-y-0
```

This uses the `divide-*` utility which renders borders using `border-color` (themed) rather than gap background.

**Files changed:** `overview-content.tsx`

---

## 6. Attention Table ARIA

**Problem:** Raw `<table>` without `scope`, `caption`, or `aria-label`.

**Solution:** Add `aria-label` to `<table>`, `scope="col"` to `<th>` elements. Keep as raw table (migrating to TanStack is N2 backlog).

**Files changed:** `overview-content.tsx`

---

## 7. Table Row Hover Consistency

**Problem:** Four different hover opacity values across tables (30%, 40%, 50%).

**Solution:** Standardize to `hover:bg-muted/40` for interactive table rows (resources, databases, audits, overview attention). Update base `TableRow` in `ui/table.tsx` to `hover:bg-muted/40` and remove per-table overrides where they simply repeat the base pattern.

For audit table rows with severity borders, keep `hover:bg-muted/30` to avoid overpowering the border accent. The lower hover on severity-accented rows is intentional — the border provides the primary visual signal.

**Files changed:** `ui/table.tsx`, `resource-table.tsx`, `database-table.tsx`, `overview-content.tsx`

---

## 8. Eyebrow/Typography Alignment

**Problem:** Three different tracking values (0.14em, 0.16em, 0.18em) and two sizes (11px, 12px) for eyebrow text.

**Solution:** Standardize to two categories:

| Category | Classes | Usage |
|----------|---------|-------|
| Eyebrow brand | `font-mono text-[11px] font-semibold uppercase tracking-[0.16em]` | PageHeader eyebrow, sidebar logo |
| Eyebrow label | `text-xs uppercase tracking-[0.14em]` | Field labels, table headers, section headers |

The 0.18em in sidebar becomes 0.16em. The 12px topbar eyebrow stays at `text-xs` (it's a different context — topbar vs page body).

**Files changed:** `sidebar.tsx`

---

## 9. Archived Badge Consistency

**Problem:** `px-1.5` vs `px-2` for the same badge.

**Solution:** Use `px-1.5` everywhere (the tighter value). Update `resources/[id]/page.tsx`.

**Files changed:** `app/(console)/resources/[id]/page.tsx`

---

## 10. DetailPanel Header Padding Alignment

**Problem:** DetailPanel uses `py-3`, DataTableShell uses `py-4`.

**Solution:** Align DetailPanel to `py-4`. DetailPanel is a card-like container just like DataTableShell.

**Files changed:** `detail-panel.tsx`

---

## 11. Multi-Select Filter Alignment

**Problem:** Trigger uses `rounded-md` and `focus:ring-2 ring-ring/20` while Button uses `rounded-lg` and `focus-visible:ring-3 ring-ring/50`.

**Solution:** Update multi-select trigger to use `rounded-lg` (matching Button) and `focus-visible:ring-2 focus-visible:ring-ring/50` (use focus-visible, not focus; match ring opacity). Keep ring-2 width (ring-3 is for primary actions, filter triggers are secondary).

**Files changed:** `multi-select-filter.tsx`

---

## 12. Hardcoded Strings → i18n

**Problem:** "Columns", "Search resources...", "Search by name..." not wrapped in translation function.

**Solution:** Add keys to en.json and zh-CN.json, wrap with `t()`.

| Key | en | zh-CN |
|-----|----|----|
| `tables.resources.columnVisibility` | "Columns" | "列" |
| `mutations.relation.targetPlaceholder` (already exists) | — | — |
| `relations.searchPlaceholder` | "Search resources..." | "搜索资源..." |

Also fix column IDs in visibility dropdown: use i18n header labels instead of `column.id`.

**Files changed:** `resource-table.tsx`, `resource-search-combobox.tsx`, `messages/en.json`, `messages/zh-CN.json`

---

## 13. Relation Delete Confirmation

**Problem:** `×` button with no confirmation. One click deletes.

**Solution:** Add a confirmation dialog using shadcn AlertDialog. On click, show dialog with "Remove this relation?" message and Cancel/Confirm buttons.

**Files changed:** `resource-relation-panel.tsx`

---

## 14. Row tabIndex Accessibility

**Problem:** `tabIndex={0}` on table rows without `role` or `aria-label`.

**Solution:** Add `role="button"` and `aria-label` describing the row action to clickable table rows in resource-table and database-table.

```tsx
<tr
  role="button"
  tabIndex={0}
  aria-label={`View details for ${row.original.displayName}`}
  ...
/>
```

**Files changed:** `resource-table.tsx`, `database-table.tsx`

---

## Files Summary

### New files
| File | Purpose |
|------|---------|
| `lib/severity-colors.ts` | Shared severity color maps |
| `components/blocks/resource-link.tsx` | Unified resource link component |

### Modified files
| File | Changes |
|------|---------|
| `components/blocks/status-badge.tsx` | Fix healthy to emerald, use severity-colors |
| `components/audits/audit-table.tsx` | Wire search, use severity-colors for dots/borders |
| `components/overview/overview-content.tsx` | Fix grid dark mode, ARIA, use severity-colors |
| `components/resources/resource-table.tsx` | ResourceLink, hover, ARIA, column label i18n |
| `components/databases/database-table.tsx` | ResourceLink, hover, ARIA |
| `components/blocks/cluster-members-table.tsx` | ResourceLink |
| `components/blocks/detail-panel.tsx` | py-3 → py-4 |
| `components/blocks/multi-select-filter.tsx` | rounded-md → rounded-lg, focus ring |
| `components/blocks/resource-relation-panel.tsx` | Delete confirmation dialog |
| `components/ui/table.tsx` | Standardize hover to muted/40 |
| `components/app-shell/sidebar.tsx` | tracking 0.18em → 0.16em |
| `app/(console)/resources/[id]/page.tsx` | Archived badge px-2 → px-1.5 |
| `components/blocks/resource-search-combobox.tsx` | i18n hardcoded strings |
| `messages/en.json` | New keys |
| `messages/zh-CN.json` | New keys |

### Not changed (backlog)
- C1 mobile nav — separate sprint
- C8 IA restructuring — separate design
- R9 overview server-side filtering — perf optimization
- R15 sticky detail header — UX enhancement
- N2 attention table to TanStack — refactor
- N3-N9 nice-to-have polish items
