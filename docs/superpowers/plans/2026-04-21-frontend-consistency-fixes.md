# Frontend Consistency Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Resolve all P0 and P1 items from the 2026-04-21 UX/UI audit by extracting shared utilities, fixing inconsistencies, and wiring inert controls.

**Architecture:** Extract two shared modules (`severity-colors.ts`, `resource-link.tsx`), then update consuming components. Group changes by dependency order — shared modules first, then consumers.

**Tech Stack:** Next.js 16, React 19, TanStack Table, shadcn/ui, Tailwind CSS, next-intl

**Base path:** `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/`

---

## Phase 1: Shared Foundations

### Task 1: Create severity color map

**Files:**
- Create: `lib/severity-colors.ts`

- [x] **Step 1: Write the module**

```typescript
// lib/severity-colors.ts

/** Shared severity color classes for health, lifecycle, and audit result indicators. */

export const HEALTH_COLORS: Record<string, { bg: string; text: string; textDark: string }> = {
  healthy: { bg: "bg-emerald-500/10", text: "text-emerald-700", textDark: "dark:text-emerald-300" },
  warning: { bg: "bg-amber-500/10", text: "text-amber-700", textDark: "dark:text-amber-300" },
  critical: { bg: "bg-rose-500/10", text: "text-rose-700", textDark: "dark:text-rose-300" },
  degraded: { bg: "bg-orange-500/10", text: "text-orange-700", textDark: "dark:text-orange-300" },
};

export const HEALTH_BORDER: Record<string, string> = {
  critical: "border-l-2 border-l-rose-500",
  degraded: "border-l-2 border-l-rose-500",
  warning: "border-l-2 border-l-amber-500",
  pending: "border-l-2 border-l-sky-500",
};

export const HEALTH_METRIC_TEXT: Record<string, string> = {
  degraded: "text-rose-600 dark:text-rose-400",
  warning: "text-amber-600 dark:text-amber-400",
  pending: "text-sky-600 dark:text-sky-400",
};

export const AUDIT_RESULT_DOT: Record<string, string> = {
  success: "bg-emerald-500",
  warning: "bg-amber-500",
  error: "bg-rose-500",
};

export const AUDIT_RESULT_BORDER: Record<string, string> = {
  error: "border-l-2 border-l-rose-500",
  warning: "border-l-2 border-l-amber-500",
};

export const POSTURE_BAR_COLORS: Record<string, string> = {
  degraded: "bg-rose-500",
  warning: "bg-amber-500",
  pending: "bg-sky-500",
};
```

- [x] **Step 2: Commit**

```bash
git add lib/severity-colors.ts
git commit -m "feat: add shared severity color map for consistent status indicators"
```

---

### Task 2: Create ResourceLink component

**Files:**
- Create: `components/blocks/resource-link.tsx`

- [x] **Step 1: Write the component**

```tsx
// components/blocks/resource-link.tsx

import Link from "next/link";
import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

type ResourceLinkProps = {
  href: string;
  children: ReactNode;
  className?: string;
  onClick?: React.MouseEventHandler<HTMLAnchorElement>;
};

export function ResourceLink({ href, children, className, onClick }: ResourceLinkProps) {
  return (
    <Link
      href={href}
      onClick={onClick}
      className={cn(
        "font-medium text-foreground underline-offset-4 hover:text-primary hover:underline focus-visible:outline-2 focus-visible:outline-ring/50 transition-colors",
        className,
      )}
    >
      {children}
    </Link>
  );
}
```

- [x] **Step 2: Commit**

```bash
git add components/blocks/resource-link.tsx
git commit -m "feat: add shared ResourceLink component for consistent table link styling"
```

---

## Phase 2: StatusBadge + Severity Consumers

### Task 3: Fix StatusBadge healthy color

**Files:**
- Modify: `components/blocks/status-badge.tsx`

- [x] **Step 1: Update the health tone class string**

Replace the `health` entry in `toneClasses` (line 15-16). Change the base from `bg-primary/10 text-primary` to `bg-muted text-muted-foreground`, so the default (no matching data-status) is neutral. The explicit `data-[status=healthy]` overrides already use emerald and will take precedence.

The full health string becomes:
```
"border-transparent bg-muted text-muted-foreground data-[status=healthy]:bg-emerald-500/10 data-[status=healthy]:text-emerald-700 dark:data-[status=healthy]:text-emerald-300 data-[status=warning]:bg-amber-500/10 data-[status=warning]:text-amber-700 dark:data-[status=warning]:text-amber-300 data-[status=critical]:bg-rose-500/10 data-[status=critical]:text-rose-700 dark:data-[status=critical]:text-rose-300 data-[status=degraded]:bg-orange-500/10 data-[status=degraded]:text-orange-700 dark:data-[status=degraded]:text-orange-300"
```

Also remove the trailing `data-[status=unknown]:bg-muted data-[status=unknown]:text-muted-foreground` since the base class now handles unknown.

- [x] **Step 2: Verify visually in browser**

Check that healthy badges show emerald (not accent color) and that unknown shows muted.

- [x] **Step 3: Commit**

```bash
git add components/blocks/status-badge.tsx
git commit -m "fix: healthy badge uses fixed emerald color instead of accent-dependent primary"
```

---

### Task 4: Update audit-table.tsx to use shared severity colors + wire search

**Files:**
- Modify: `components/audits/audit-table.tsx`

- [x] **Step 1: Replace local color maps with imports**

Remove `RESULT_DOT` (lines 47-51) and `RESULT_ROW_BORDER` (lines 53-56). Add imports:

```typescript
import { AUDIT_RESULT_DOT, AUDIT_RESULT_BORDER } from "@/lib/severity-colors";
```

Update references: `RESULT_DOT[...]` → `AUDIT_RESULT_DOT[...]`, `RESULT_ROW_BORDER[...]` → `AUDIT_RESULT_BORDER[...]`.

- [x] **Step 2: Wire the search input**

Add state and filter logic:

```typescript
import { useMemo, useState } from "react";
// ...
const [search, setSearch] = useState("");

const filteredEvents = useMemo(() => {
  if (!search.trim()) return events;
  const q = search.toLowerCase();
  return events.filter((event) =>
    event.targetResourceName.toLowerCase().includes(q) ||
    event.actorLabel.toLowerCase().includes(q) ||
    getEventTypeLabel(event.eventType).toLowerCase().includes(q) ||
    event.environmentLabel.toLowerCase().includes(q)
  );
}, [events, search]);
```

Update the Input (line 181-183) to be controlled:
```tsx
<Input
  value={search}
  onChange={(e) => setSearch(e.target.value)}
  placeholder={t("tables.audits.searchPlaceholder")}
  className="h-9 w-[240px] border-border bg-background py-2"
/>
```

Update `useReactTable` data from `events` to `filteredEvents`.

- [x] **Step 3: Verify build**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build 2>&1 | tail -10
```

- [x] **Step 4: Commit**

```bash
git add components/audits/audit-table.tsx
git commit -m "fix: wire audit search input and use shared severity colors"
```

---

### Task 5: Update overview-content.tsx — dark mode grid, ARIA, severity colors

**Files:**
- Modify: `components/overview/overview-content.tsx`

- [x] **Step 1: Replace posture grid gap-px with divide approach**

Change line 149 from:
```
grid grid-cols-2 overflow-hidden rounded-lg border border-border bg-border gap-px sm:grid-cols-4
```
to:
```
grid grid-cols-2 rounded-lg border border-border divide-x divide-border overflow-hidden sm:grid-cols-4 divide-y sm:divide-y-0
```

- [x] **Step 2: Import and use shared severity colors**

Add import:
```typescript
import { HEALTH_BORDER, HEALTH_METRIC_TEXT, POSTURE_BAR_COLORS } from "@/lib/severity-colors";
```

Replace the hardcoded color classes in posture metric values and attention row borders with the imported maps.

Replace `attentionRowColor` function body to use `HEALTH_BORDER`:
```typescript
function attentionRowColor(resource: ResourceListViewModel): string {
  return HEALTH_BORDER[resource.healthStatus] ?? HEALTH_BORDER[resource.lifecycleStatus] ?? "";
}
```

Replace bar segment classes to use `POSTURE_BAR_COLORS`:
```typescript
className={POSTURE_BAR_COLORS.degraded}
className={POSTURE_BAR_COLORS.warning}
className={POSTURE_BAR_COLORS.pending}
```

Replace posture metric text classes to use `HEALTH_METRIC_TEXT`:
```typescript
className={`mt-1 text-2xl font-semibold ${HEALTH_METRIC_TEXT.degraded}`}
className={`mt-1 text-2xl font-semibold ${HEALTH_METRIC_TEXT.warning}`}
className={`mt-1 text-2xl font-semibold ${HEALTH_METRIC_TEXT.pending}`}
```

- [x] **Step 3: Add ARIA to attention table**

Add `aria-label` to the `<table>` element (line 219):
```tsx
<table className="w-full text-sm" aria-label={t("pages.overview.attention.title")}>
```

Add `scope="col"` to all `<th>` elements (lines 222-235):
```tsx
<th scope="col" className="px-3 py-2 ...">
```

- [x] **Step 4: Verify build**

- [x] **Step 5: Commit**

```bash
git add components/overview/overview-content.tsx
git commit -m "fix: overview posture grid dark mode, attention table ARIA, shared severity colors"
```

---

## Phase 3: Table Consistency

### Task 6: Standardize table row hover

**Files:**
- Modify: `components/ui/table.tsx`
- Modify: `components/resources/resource-table.tsx`
- Modify: `components/databases/database-table.tsx`

- [x] **Step 1: Update base TableRow hover**

In `ui/table.tsx` line 60, change `hover:bg-muted/50` to `hover:bg-muted/40`.

- [x] **Step 2: Remove redundant hover overrides in resource-table.tsx**

Line 523: remove `hover:bg-muted/40` (now matches base). Keep only the additional classes:
```tsx
className={`cursor-pointer transition-colors${row.original.isArchived ? " opacity-60" : ""}`}
```

- [x] **Step 3: Remove redundant hover overrides in database-table.tsx**

Line 242: remove `hover:bg-muted/40` from the row className:
```tsx
className="cursor-pointer transition-colors"
```

- [x] **Step 4: Keep audit table hover at muted/30**

Audit table rows (line 241) keep `hover:bg-muted/30` intentionally — severity borders provide primary signal.

- [x] **Step 5: Commit**

```bash
git add components/ui/table.tsx components/resources/resource-table.tsx components/databases/database-table.tsx
git commit -m "fix: standardize table row hover to muted/40 across all tables"
```

---

### Task 7: Adopt ResourceLink in all tables

**Files:**
- Modify: `components/resources/resource-table.tsx`
- Modify: `components/databases/database-table.tsx`
- Modify: `components/audits/audit-table.tsx`
- Modify: `components/blocks/cluster-members-table.tsx`

- [x] **Step 1: resource-table.tsx**

Add import:
```typescript
import { ResourceLink } from "@/components/blocks/resource-link";
```

Replace the `<Link>` in the resource name cell (around line 146) with `<ResourceLink>`. Remove the old className from the Link. Keep the `onClick={(e) => e.stopPropagation()}` on ResourceLink.

- [x] **Step 2: database-table.tsx**

Same pattern — replace `<Link>` with `<ResourceLink>`.

- [x] **Step 3: audit-table.tsx**

Replace the `<Link>` in targetResourceName cell (around line 132-137) with `<ResourceLink>`.

- [x] **Step 4: cluster-members-table.tsx**

Replace `<Link>` with `<ResourceLink>`. The different underline-offset and hover opacity will now match the canonical style.

- [x] **Step 5: Verify build**

- [x] **Step 6: Commit**

```bash
git add components/resources/resource-table.tsx components/databases/database-table.tsx components/audits/audit-table.tsx components/blocks/cluster-members-table.tsx
git commit -m "refactor: use shared ResourceLink component across all tables"
```

---

### Task 8: Add row accessibility attributes

**Files:**
- Modify: `components/resources/resource-table.tsx`
- Modify: `components/databases/database-table.tsx`

- [x] **Step 1: resource-table.tsx — add role and aria-label to clickable rows**

Find the `<TableRow>` with `tabIndex={0}` (around line 521). Add:
```tsx
role="button"
aria-label={`View details for ${row.original.displayName}`}
```

- [x] **Step 2: database-table.tsx — same pattern**

Add to the clickable `<tr>` (around line 241):
```tsx
role="button"
aria-label={`View details for ${row.original.displayName}`}
```

- [x] **Step 3: Commit**

```bash
git add components/resources/resource-table.tsx components/databases/database-table.tsx
git commit -m "fix: add role=button and aria-label to clickable table rows"
```

---

## Phase 4: Visual Consistency Fixes

### Task 9: Fix DetailPanel header padding

**Files:**
- Modify: `components/blocks/detail-panel.tsx`

- [x] **Step 1: Change py-3 to py-4**

Line 22: change `px-4 py-3` to `px-4 py-4`.

- [x] **Step 2: Commit**

```bash
git add components/blocks/detail-panel.tsx
git commit -m "fix: align DetailPanel header padding to py-4 matching DataTableShell"
```

---

### Task 10: Fix multi-select filter trigger styling

**Files:**
- Modify: `components/blocks/multi-select-filter.tsx`

- [x] **Step 1: Update trigger classes**

Line 68, change:
```
rounded-md border border-border bg-background px-3 text-sm outline-none focus:ring-2 focus:ring-ring/20
```
to:
```
rounded-lg border border-border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50
```

Changes: `rounded-md` → `rounded-lg`, `focus:` → `focus-visible:`, `ring-ring/20` → `ring-ring/50`.

- [x] **Step 2: Commit**

```bash
git add components/blocks/multi-select-filter.tsx
git commit -m "fix: align multi-select trigger radius and focus ring with Button pattern"
```

---

### Task 11: Fix eyebrow tracking + archived badge + other small fixes

**Files:**
- Modify: `components/app-shell/sidebar.tsx`
- Modify: `app/(console)/resources/[id]/page.tsx`

- [x] **Step 1: Fix sidebar eyebrow tracking**

In `sidebar.tsx` line 44, change `tracking-[0.18em]` to `tracking-[0.16em]`.

- [x] **Step 2: Fix archived badge padding**

In `resources/[id]/page.tsx`, find the archived badge span (around line 79) with `px-2` and change to `px-1.5`.

- [x] **Step 3: Commit**

```bash
git add components/app-shell/sidebar.tsx "app/(console)/resources/[id]/page.tsx"
git commit -m "fix: standardize eyebrow tracking and archived badge padding"
```

---

## Phase 5: i18n + Interaction

### Task 12: Fix hardcoded strings to i18n

**Files:**
- Modify: `components/resources/resource-table.tsx`
- Modify: `components/blocks/resource-search-combobox.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [x] **Step 1: Add translation keys**

In `en.json`, add to `tables.resources`:
```json
"columnVisibility": "Columns"
```

In `zh-CN.json`, add to `tables.resources`:
```json
"columnVisibility": "列"
```

Add to `en.json` in `relations`:
```json
"searchPlaceholder": "Search resources..."
```

Add to `zh-CN.json` in `relations`:
```json
"searchPlaceholder": "搜索资源..."
```

- [x] **Step 2: resource-table.tsx — column visibility label**

Around line 403, replace `"Columns"` with `{t("tables.resources.columnVisibility")}`.

Fix column IDs in the visibility dropdown. Instead of `column.id` with `capitalize`, use the column's header translation. For each column, the header is already translated. Use a label map or read from `column.columnDef.header`.

Replace the dropdown item label (around line 410-413):
```tsx
<DropdownMenuCheckboxItem
  key={column.id}
  checked={column.getIsVisible()}
  onCheckedChange={(value) => column.toggleVisibility(!!value)}
>
  {typeof column.columnDef.header === "string"
    ? column.columnDef.header
    : column.id.replace(/([A-Z])/g, " $1").trim()}
</DropdownMenuCheckboxItem>
```

- [x] **Step 3: resource-search-combobox.tsx — replace hardcoded strings**

Around line 62, replace `"Search resources..."` with `t("relations.searchPlaceholder")`.

Add `const t = useTranslations();` if not already present (check first — it may already have translations).

- [x] **Step 4: Verify build**

- [x] **Step 5: Commit**

```bash
git add components/resources/resource-table.tsx components/blocks/resource-search-combobox.tsx messages/en.json messages/zh-CN.json
git commit -m "fix: replace hardcoded strings with i18n translations"
```

---

### Task 13: Add relation delete confirmation

**Files:**
- Modify: `components/blocks/resource-relation-panel.tsx`

- [x] **Step 1: Add confirmation dialog**

Import AlertDialog components:
```typescript
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
```

Add state:
```typescript
const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null);
```

Replace the direct `handleDeleteRelation` call on the button with `setPendingDeleteId(relation.id)`.

Add AlertDialog component after the panel content:
```tsx
<AlertDialog open={!!pendingDeleteId} onOpenChange={(open) => !open && setPendingDeleteId(null)}>
  <AlertDialogContent>
    <AlertDialogHeader>
      <AlertDialogTitle>{mt("relation.confirmDelete")}</AlertDialogTitle>
      <AlertDialogDescription>
        {mt("relation.deleteSuccess")}
      </AlertDialogDescription>
    </AlertDialogHeader>
    <AlertDialogFooter>
      <AlertDialogCancel>{t("common.actions.cancel")}</AlertDialogCancel>
      <AlertDialogAction
        onClick={() => {
          if (pendingDeleteId) handleDeleteRelation(pendingDeleteId);
          setPendingDeleteId(null);
        }}
      >
        {t("common.actions.confirm")}
      </AlertDialogAction>
    </AlertDialogFooter>
  </AlertDialogContent>
</AlertDialog>
```

Note: Check that `@/components/ui/alert-dialog` exists. If not, add via shadcn CLI first: `npx shadcn@latest add alert-dialog`.

- [x] **Step 2: Verify build**

- [x] **Step 3: Commit**

```bash
git add components/blocks/resource-relation-panel.tsx
git commit -m "fix: add confirmation dialog before deleting resource relations"
```

---

## Phase 6: Verification

### Task 14: Full build verification + visual check

- [x] **Step 1: Run build**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build
```

- [x] **Step 2: Run existing tests**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx vitest run
```

- [x] **Step 3: Visual verification checklist**

- [x] Overview: posture grid visible in both light and dark mode
- [x] Overview: attention table has aria-label
- [x] Resources: healthy badges show emerald (not accent color)
- [x] Resources: row hover consistent
- [x] Audits: search input filters events when typing
- [x] Audits: severity borders use -500 shade
- [x] Relations: delete shows confirmation dialog
- [x] Detail panel: header padding matches data table shell
- [x] Filter triggers: rounded-lg with focus-visible ring
- [x] zh-CN mode: "Columns" shows "列"
