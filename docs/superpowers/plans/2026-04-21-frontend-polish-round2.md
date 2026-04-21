# Frontend Polish Round 2 — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix i18n hardcodes, add DbTypeIcon to 5 missing locations, complete database table columns, unify "resource" terminology, and fix detail page edge cases.

**Architecture:** Straightforward component edits — no new components, no new dependencies. Group by dependency: i18n keys first (they're consumed by everything), then icon placements, then table fixes, then detail page fixes.

**Tech Stack:** Next.js 16, React 19, TanStack Table, next-intl, Tailwind CSS

**Base path:** `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/`

---

## Phase 1: i18n Foundation

### Task 1: Add new i18n keys and fix "asset" → "resource"

**Files:**
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Add new keys to en.json**

Add to `common.fields` section:
```json
"hostname": "Hostname",
"port": "Port",
"nodes": "Nodes"
```

- [ ] **Step 2: Add new keys to zh-CN.json**

Add to `common.fields` section:
```json
"hostname": "主机名",
"port": "端口",
"nodes": "节点数"
```

- [ ] **Step 3: Replace "asset" with "resource" in en.json**

In these keys, replace "asset"/"assets" with "resource"/"resources":
- `pages.overview.posture.total`: "Total managed assets" → "Total managed resources"
- `pages.cmdb.description`: "Browse assets by" → "Browse resources by"
- `pages.settings.dictionaries.resourceType`: "Top-level asset families" → "Top-level resource families"
- `pages.settings.dictionaries.lifecycleStatus`: "Asset lifecycle state classification" → "Resource lifecycle state classification"
- `mutations.create.description`: "manually maintained asset" → "manually maintained resource"
- `mutations.relation.addDescription`: "to another asset" → "to another resource"
- `detailSheet.relationsDescription`: "linked to this asset" → "linked to this resource"
- `pages.settings.environments.description`: "asset registration" → "resource registration"
- `shell.description`: "Platform asset context" → "Platform resource context"
- `shell.subtitle`: "asset visibility" → "resource visibility"

- [ ] **Step 4: Replace "asset" equivalents in zh-CN.json**

Apply the same asset→resource replacements in Chinese for all matching keys. Use "资源" consistently instead of "资产".

- [ ] **Step 5: Commit**

```bash
git add messages/en.json messages/zh-CN.json
git commit -m "fix: add hostname/port/nodes i18n keys, unify asset→resource terminology"
```

---

## Phase 2: Hardcoded String Fixes

### Task 2: Fix 6 hardcoded column headers in resource-table.tsx

**Files:**
- Modify: `components/resources/resource-table.tsx`

- [ ] **Step 1: Replace hardcoded headers with t() calls**

At line 189, change `"Hostname"` to `t("common.fields.hostname")`

At line 203, change `"Port"` to `t("common.fields.port")`

At line 216, change `"Nodes"` to `t("common.fields.nodes")`

At line 237, change `"Subtype"` to `t("common.fields.resourceSubtype")`

At line 245, change `"External ID"` to `t("common.fields.externalId")`

At line 253, change `"Source"` to `t("common.fields.source")`

- [ ] **Step 2: Verify build**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build 2>&1 | tail -10
```

- [ ] **Step 3: Commit**

```bash
git add components/resources/resource-table.tsx
git commit -m "fix: replace 6 hardcoded column headers with i18n translations"
```

---

### Task 3: Fix hardcoded strings in resource-search-combobox.tsx

**Files:**
- Modify: `components/blocks/resource-search-combobox.tsx`

- [ ] **Step 1: Add useTranslations import and hook**

Add `import { useTranslations } from "next-intl";` if not already present. Add `const t = useTranslations();` inside the component.

- [ ] **Step 2: Replace hardcoded strings**

At line 83, replace `"Searching..."` with `t("common.loading")`

At line 86, replace `"No resources found."` with `t("common.noResults")`

- [ ] **Step 3: Verify build**

- [ ] **Step 4: Commit**

```bash
git add components/blocks/resource-search-combobox.tsx
git commit -m "fix: replace hardcoded search strings with i18n translations"
```

---

## Phase 3: DbTypeIcon Consistency

### Task 4: Add DbTypeIcon to cluster members table

**Files:**
- Modify: `components/blocks/cluster-members-table.tsx`

- [ ] **Step 1: Add import**

```typescript
import { DbTypeIcon } from "@/components/blocks/db-type-icon";
```

- [ ] **Step 2: Update subtype column cell (around line 57-58)**

Change from:
```tsx
<td className="px-4 py-2 capitalize text-muted-foreground">
  {member.resourceSubtype}
</td>
```

To:
```tsx
<td className="px-4 py-2">
  <div className="flex items-center gap-1.5">
    <DbTypeIcon subtype={member.resourceSubtype} className="size-3.5" />
    <span className="capitalize text-muted-foreground">
      {member.resourceSubtype}
    </span>
  </div>
</td>
```

- [ ] **Step 3: Verify build**

- [ ] **Step 4: Commit**

```bash
git add components/blocks/cluster-members-table.tsx
git commit -m "feat: add engine icon to cluster members subtype column"
```

---

### Task 5: Add DbTypeIcon to relation panel type pill

**Files:**
- Modify: `components/blocks/resource-relation-panel.tsx`

- [ ] **Step 1: Add import**

```typescript
import { DbTypeIcon } from "@/components/blocks/db-type-icon";
```

- [ ] **Step 2: Update type pill (around line 219-223)**

Change the type badge to conditionally include DbTypeIcon:
```tsx
{related && (
  <span className="inline-flex items-center gap-1.5 rounded-md bg-muted px-1.5 py-0.5 text-[10px] font-medium lowercase tracking-normal text-muted-foreground">
    {(related.resourceType === "database_instance" ||
      related.resourceType === "database_cluster" ||
      related.resourceType === "database_proxy") && (
      <DbTypeIcon subtype={related.resourceSubtype} className="size-3.5" />
    )}
    {formatLabel(related.resourceType)}
  </span>
)}
```

Note: The `RelatedResourceSummary` type already has `resourceSubtype` (added in previous session).

- [ ] **Step 3: Verify build**

- [ ] **Step 4: Commit**

```bash
git add components/blocks/resource-relation-panel.tsx
git commit -m "feat: add engine icon to relation panel type badges"
```

---

### Task 6: Add DbTypeIcon to detail page subtype field

**Files:**
- Modify: `app/(console)/resources/[id]/page.tsx`

- [ ] **Step 1: Add import**

```typescript
import { DbTypeIcon } from "@/components/blocks/db-type-icon";
```

- [ ] **Step 2: Update subtype display (around line 109)**

Change from:
```tsx
<dd className="mt-1 font-medium text-foreground">
  {resource.resourceSubtype}
</dd>
```

To:
```tsx
<dd className="mt-1 font-medium text-foreground">
  {resource.resourceSubtype ? (
    <span className="inline-flex items-center gap-1.5">
      {(resource.resourceType === "database_instance" ||
        resource.resourceType === "database_cluster" ||
        resource.resourceType === "database_proxy") && (
        <DbTypeIcon subtype={resource.resourceSubtype} className="size-4" />
      )}
      {formatLabel(resource.resourceSubtype)}
    </span>
  ) : (
    t("common.notSet")
  )}
</dd>
```

This also fixes M7 (empty subtype no fallback).

- [ ] **Step 3: Fix labels empty state (around line 152)**

Change from:
```tsx
{Object.entries(resource.labels).map(([key, value]) => (
  <span key={key} ...>
    {key}: {value}
  </span>
))}
```

To:
```tsx
{Object.keys(resource.labels).length === 0 ? (
  <span className="text-sm text-muted-foreground">{t("common.notSet")}</span>
) : (
  Object.entries(resource.labels).map(([key, value]) => (
    <span key={key} className="inline-flex items-center rounded-md border border-border px-2 py-0.5 text-xs font-medium text-muted-foreground">
      {key}: {value}
    </span>
  ))
)}
```

- [ ] **Step 4: Verify build**

- [ ] **Step 5: Commit**

```bash
git add "app/(console)/resources/[id]/page.tsx"
git commit -m "feat: add engine icon to detail subtype field, fix empty subtype and labels"
```

---

### Task 7: Add DbTypeIcon to topology panel nodes

**Files:**
- Modify: `components/blocks/topology-panel.tsx`

- [ ] **Step 1: Add import**

```typescript
import { DbTypeIcon } from "@/components/blocks/db-type-icon";
```

- [ ] **Step 2: Update node subtype display (around line 298-305)**

Change from:
```tsx
{data.resourceSubtype && (
  <>
    <span>·</span>
    <span>{data.resourceSubtype}</span>
  </>
)}
```

To:
```tsx
{data.resourceSubtype && (
  <>
    <span>·</span>
    {(data.resourceType === "database_instance" ||
      data.resourceType === "database_cluster" ||
      data.resourceType === "database_proxy") ? (
      <span className="inline-flex items-center gap-1">
        <DbTypeIcon subtype={data.resourceSubtype} className="size-3" />
        <span>{data.resourceSubtype}</span>
      </span>
    ) : (
      <span>{data.resourceSubtype}</span>
    )}
  </>
)}
```

Note: Check what `data` is typed as. It may be `TopologyNode` from `types/resource.ts`. Verify the field names match.

- [ ] **Step 3: Verify build**

- [ ] **Step 4: Commit**

```bash
git add components/blocks/topology-panel.tsx
git commit -m "feat: add engine icon to topology node subtype labels"
```

---

### Task 8: Add DbTypeIcon to detail sheet header

**Files:**
- Modify: `components/resources/resource-detail-sheet.tsx`

- [ ] **Step 1: Add import**

```typescript
import { DbTypeIcon } from "@/components/blocks/db-type-icon";
```

- [ ] **Step 2: Update header description (around line 87)**

Change from:
```tsx
{resource.name} · {localizeResourceType(resource.resourceType, t)}
```

To:
```tsx
<span className="inline-flex items-center gap-1.5">
  {(resource.resourceType === "database_instance" ||
    resource.resourceType === "database_cluster" ||
    resource.resourceType === "database_proxy") && (
    <DbTypeIcon subtype={resource.resourceSubtype} className="size-4" />
  )}
  {resource.name} · {localizeResourceType(resource.resourceType, t)}
</span>
```

- [ ] **Step 3: Verify build**

- [ ] **Step 4: Commit**

```bash
git add components/resources/resource-detail-sheet.tsx
git commit -m "feat: add engine icon to resource detail sheet header"
```

---

## Phase 4: Database Table Completeness

### Task 9: Add hostname, port columns and fix database-table.tsx

**Files:**
- Modify: `components/databases/database-table.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Add hostname and port columns**

After the `resourceSubtype` (Engine) column definition and before the `status` column, add:

```typescript
columnHelper.display({
  id: "hostname",
  header: t("common.fields.hostname"),
  cell: ({ row }) => {
    const rt = row.original.resourceType;
    if (rt !== "database_instance" && rt !== "host") return null;
    return (
      <span className="text-sm text-muted-foreground">
        {row.original.profileSummary?.hostname ?? "—"}
      </span>
    );
  },
}),
columnHelper.display({
  id: "port",
  header: t("common.fields.port"),
  cell: ({ row }) => {
    if (row.original.resourceType !== "database_instance") return null;
    return (
      <span className="text-sm text-muted-foreground">
        {row.original.profileSummary?.port ?? "—"}
      </span>
    );
  },
}),
```

- [ ] **Step 2: Add lifecycle badge to status column**

Change the status column (currently only health) to show both badges:

```typescript
columnHelper.display({
  id: "status",
  header: t("common.fields.status"),
  cell: ({ row }) => (
    <div className="flex flex-wrap gap-2">
      <StatusBadge status={row.original.healthStatus} tone="health" />
      <StatusBadge status={row.original.lifecycleStatus} tone="lifecycle" />
    </div>
  ),
}),
```

- [ ] **Step 3: Fix environment and owner cell renderers**

Add explicit cell renderers to match resource table styling:

```typescript
columnHelper.accessor("environmentName", {
  header: t("common.fields.environment"),
  cell: (info) => (
    <span className="text-sm text-foreground">{info.getValue()}</span>
  ),
}),
columnHelper.accessor("ownerName", {
  header: t("common.fields.owner"),
  cell: (info) => (
    <span className="text-sm text-foreground">{info.getValue()}</span>
  ),
}),
```

- [ ] **Step 4: Fix date formatting**

Import `formatRelativeDateTime` from `@/lib/format`. Change the updatedAt column from `formatDateTime` to:

```typescript
cell: (info) => (
  <span className="text-sm text-muted-foreground whitespace-nowrap" title={formatDateTime(info.getValue(), locale)}>
    {formatRelativeDateTime(info.getValue(), locale)}
  </span>
),
```

- [ ] **Step 5: Verify build**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build 2>&1 | tail -10
```

- [ ] **Step 6: Commit**

```bash
git add components/databases/database-table.tsx
git commit -m "feat: add hostname/port columns, lifecycle badge, and relative dates to database table"
```

---

## Phase 5: Micro-fixes

### Task 10: Fix deployed resources card empty description

**Files:**
- Modify: `components/blocks/deployed-resources-card.tsx`

- [ ] **Step 1: Remove empty description prop**

Change from:
```tsx
<EmptyState
  title={t("empty")}
  description=""
/>
```

To:
```tsx
<EmptyState
  title={t("empty")}
/>
```

Check if `EmptyState` requires `description` prop. If it does, change the component to make it optional instead, or provide a meaningful description.

- [ ] **Step 2: Commit**

```bash
git add components/blocks/deployed-resources-card.tsx
git commit -m "fix: remove empty description from deployed resources empty state"
```

---

## Phase 6: Verification

### Task 11: Full build + test verification

- [ ] **Step 1: Run build**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build
```

- [ ] **Step 2: Run test suite**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx vitest run
```

- [ ] **Step 3: Visual verification checklist**

- [ ] Resource table: column headers all translated in zh-CN
- [ ] Database table: hostname, port, lifecycle badge visible
- [ ] Database table: dates show relative format ("2h ago")
- [ ] Cluster members: engine icons next to subtype text
- [ ] Relation panel: engine icons in type pills
- [ ] Detail page: subtype shows engine icon + "Not set" fallback
- [ ] Detail page: empty labels show "Not set"
- [ ] Topology: database nodes show engine icons
- [ ] Detail sheet: header shows engine icon
- [ ] Search combobox: zh-CN shows translated strings
