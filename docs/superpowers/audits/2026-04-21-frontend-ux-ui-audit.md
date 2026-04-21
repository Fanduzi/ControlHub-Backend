# Frontend UX/UI Comprehensive Audit

**Date:** 2026-04-21
**Scope:** ControlHub console frontend (`cmdb-redesign` worktree)
**Auditors:** UX Architect + UI Designer agents
**Codebase:** Next.js 16 + TanStack Table + shadcn/ui + Tailwind CSS

---

## CRITICAL

### C1. No mobile navigation

`components/app-shell/app-shell.tsx:23` — Sidebar uses `hidden lg:block`, completely invisible below 1024px. No hamburger menu, drawer, or bottom tab bar exists. The app is unusable for navigation on mobile/tablet.

### C2. Audit search input is non-functional

`components/audits/audit-table.tsx:181-183` — The `<Input>` has no `value`, `onChange`, or `onKeyDown`. It renders as a visual placeholder; typing does nothing. Users will expect to filter audits by text but the control is inert.

### C3. Healthy badge color follows accent theme

`components/blocks/status-badge.tsx:15-16` — The "healthy" state uses `text-primary` + `bg-primary/10` which shifts with the accent color. When accent is amber, healthy badges look identical to warning badges. When accent is emerald, they lose distinction against the theme. Health colors should be fixed (emerald for healthy) to maintain universal "green = good" semantics.

### C4. Audit row severity borders use -400 shades; overview uses -500

- `components/audits/audit-table.tsx:53-56` — `border-l-rose-400`, `border-l-amber-400`
- `components/overview/overview-content.tsx:85-95` — `border-l-rose-500`, `border-l-amber-500`

Same semantic concept (severity indicator), different visual intensity. Audit rows appear washed out compared to overview attention queue.

### C5. Posture grid separators nearly invisible in dark mode

`components/overview/overview-content.tsx:149` — Uses `gap-px` with `bg-border` as gap color. In dark mode `--border` becomes `oklch(1 0 0 / 15%)` which is nearly transparent. The four posture cells visually merge into one block.

### C6. Status column mixes two orthogonal dimensions

`components/resources/resource-table.tsx:176-186` — The "Status" column renders health badge + lifecycle badge + optional archived indicator. Three distinct dimensions in one column header labeled "Status" — impossible to sort or scan by a specific dimension.

### C7. Overview attention table lacks ARIA

`components/overview/overview-content.tsx:219-279` — Uses raw `<table>`, `<th>` without `scope` attributes, no `role="table"`, no caption or `aria-label`. TanStack-based tables get this automatically.

### C8. Databases and Resources share data model but are peer nav items

`lib/navigation.ts:17-22` — Databases is a filtered view of Resources (`/resources?type=database_*`) but appears as an equal sidebar entry. Users cannot understand the relationship. The databases posture card (`databases/page.tsx:36-50`) duplicates overview concerns.

---

## RECOMMENDED

### R1. Table row hover opacity differs across pages

| Page | Opacity | File |
|------|---------|------|
| Resources | 40% | resource-table.tsx:523 |
| Databases | 40% | database-table.tsx:242 |
| Audits | 30% | audit-table.tsx:241 |
| Overview attention | 30% | overview-content.tsx:243 |
| Base TableRow | 50% (dead code) | ui/table.tsx:60 |

Four different values for the same interaction pattern.

### R2. Row click has dual interaction model

`components/resources/resource-table.tsx:519-527` — Clicking a row opens the detail sheet, but the resource name is also a `<Link>` to `/resources/{id}` with `stopPropagation()`. Some clicks open a sheet; others navigate. Users cannot predict which behavior they get.

### R3. Column visibility dropdown shows raw column IDs

`components/resources/resource-table.tsx:404-417` — Uses `column.id` with `className="capitalize"`. IDs like `resourceSubtype` render as "Resourcesubtype" instead of localized labels.

### R4. Relation delete button has no confirmation

`components/blocks/resource-relation-panel.tsx:216-225` — A bare `x` button with no tooltip and no confirmation dialog. One misclick permanently deletes a relation.

### R5. Eyebrow tracking inconsistent

| Location | Size | Tracking |
|----------|------|----------|
| page-header.tsx:20 | 11px | 0.16em |
| sidebar.tsx:44 | 11px | 0.18em |
| topbar.tsx:123 | 12px | 0.14em |

Three different letter-spacing values for the same typography pattern.

### R6. Resource link styles differ across four tables

| Table | Underline offset | Hover color | Focus ring | Transition |
|-------|-----------------|-------------|------------|------------|
| resource-table.tsx:146 | 4 | primary | yes | yes |
| database-table.tsx:107 | 4 | primary | yes | yes |
| audit-table.tsx:134 | default | primary | no | no |
| cluster-members-table.tsx:55 | 2 | primary/80 | no | no |

### R7. Archived badge padding inconsistency

- `resource-table.tsx:179` — `px-1.5`
- `resources/[id]/page.tsx:79` — `px-2`

Same visual element, different horizontal padding.

### R8. DetailPanel header padding differs from DataTableShell

- `components/blocks/detail-panel.tsx:22` — `py-3`
- `components/blocks/data-table-shell.tsx:24` — `py-4`

Both are card-like container headers but at different density.

### R9. Overview fetches all resources for client-side filtering

`components/overview/overview-content.tsx:106-111` — Receives the full resource list and filters with `useMemo` by `currentEnvironmentId`. Server component (`overview/page.tsx:14`) calls `listResourceViewModels()` with no parameters. Should pass environment filter server-side.

### R10. Topbar subtitle is static across all pages

`components/app-shell/topbar.tsx:127-128` — Generic subtitle identical on every page. Should reflect current page context or be removed.

### R11. Empty states lack actionable next steps

`components/blocks/empty-state.tsx` accepts an `action` prop, but Resources, Databases, and Audits empty states pass no action. A new user on a fresh install has no guidance on what to do next.

### R12. Hardcoded English strings not through i18n

| String | File | Line |
|--------|------|------|
| "Columns" | resource-table.tsx | 403 |
| "Search resources..." | resource-search-combobox.tsx | 62 |
| "Search by name..." | resource-search-combobox.tsx | 76 |

### R13. Multi-select filter trigger styling differs from Button

`components/blocks/multi-select-filter.tsx:67-69` — Uses `rounded-md` while Button uses `rounded-lg`. Focus ring is `ring-2 ring-ring/20` vs Button's `ring-3 ring-ring/50`. Different border radius and focus behavior for adjacent controls.

### R14. Database table rows clickable but no visual affordance

`components/databases/database-table.tsx:239-248` — Rows have `tabIndex={0}` and `cursor-pointer` but no tooltip, underline, or indicator that clicking opens a detail sheet. Only the cursor change signals interactivity.

### R15. Resource detail page has no sticky header or back-to-top

`app/(console)/resources/[id]/page.tsx` — 7+ DetailPanel sections in vertical succession. Breadcrumb scrolls away immediately. No way to see the resource name while scrolled down.

### R16. LabelsEditor uses array index as React key

`components/blocks/labels-editor.tsx:37` — `<div key={index}>` causes stale values in wrong fields when removing a label from the middle.

### R17. Topology search silently swallows errors

`components/blocks/resource-search-combobox.tsx:49-50` — `catch { setResults([]) }` discards all fetch errors. User sees "No resources found" indistinguishable from a legitimate empty result.

### R18. Row-level tabIndex without accessible role

`resource-table.tsx:521`, `database-table.tsx:241` — `tabIndex={0}` makes rows focusable, `onKeyDown` handles Enter, but no `role="button"` or `aria-label` describing what happens when activated.

---

## NICE-TO-HAVE

### N1. Posture bar segments too bright in dark mode

`overview-content.tsx:187-203` — `bg-rose-500`, `bg-amber-500`, `bg-sky-500` become harsh against dark `bg-muted` track. Consider muted variants.

### N2. Attention queue uses raw `<table>` instead of shared Table component

`overview-content.tsx:219-280` — Lacks sorting, column visibility, and consistent styling that DataTableShell + TanStack Table provide.

### N3. Attention empty state differs from shared EmptyState

`overview-content.tsx:210-213` — Inline dashed box with `py-3`, while shared `<EmptyState>` uses `py-8` with `bg-muted/30` and icon/title/description structure.

### N4. Detail sheet overrides bg-popover with bg-background

`resource-detail-sheet.tsx:81` — Sheet defaults to `bg-popover`; detail sheet overrides to `bg-background`. In dark mode these diverge (`oklch(0.23)` vs `oklch(0.19)`).

### N5. DbTypeIcon uses size-5 (20px) while other icons use size-4 (16px)

`db-type-icon.tsx:27` — Database rows get subtly more visual weight than non-database rows.

### N6. Dictionary tags lack bg-background

`settings/page.tsx:172` vs `resources/[id]/page.tsx:154` — Resource detail labels include `bg-background`; settings dictionary tags have transparent background.

### N7. Pagination select h-7 vs filter triggers h-9

`pagination-controls.tsx:114` — Page size selector is 28px while multi-select filters are 36px. Visual mismatch when both appear in the same shell.

### N8. Progress bar lacks dark mode softening

`overview-content.tsx:187-203` — Bar segments use full-strength colors in dark mode.

### N9. Topology role borders need stronger dark mode opacity

`topology-panel.tsx:34-46` — 50% opacity borders hard to perceive on dark card backgrounds.

---

## Architectural Recommendations

1. **Extract shared severity color map** (`status-colors.ts`) — Single source of truth for health/lifecycle/result colors (light + dark). All components import from this. Eliminates C4, R1 partial, N1.

2. **Fix "healthy" to emerald** — Remove `primary` color dependency from healthy state. Create semantic token `--color-healthy` or just use `emerald` directly. Addresses C3.

3. **Create shared `ResourceLink` component** — Wraps the link pattern used in four tables with unified styles. Addresses R6.

4. **Create shared `FieldGroup` component** — For the repeated dt/dd label pattern (~30 instances). Addresses R5 partial, ensures consistency.

5. **Align multi-select trigger with Button** — Either compose from Button/SelectTrigger or align border-radius and focus ring classes. Addresses R13.

6. **Mobile nav as separate sprint** — C1/C7 require a drawer component and responsive sidebar refactor. Scope this independently.

---

## Suggested Fix Priority

| Priority | Items | Effort |
|----------|-------|--------|
| **P0 — This sprint** | C2 (wire audit search), C3 (fix healthy badge), C4+C5 (unify severity colors + fix dark mode grid), C7 (attention table ARIA) | ~4h |
| **P1 — Next sprint** | R1 (hover consistency), R2 (pick one row interaction model), R6 (ResourceLink component), R3 (column labels i18n), R4 (delete confirmation), R12 (hardcoded strings) | ~1 day |
| **P2 — Backlog** | C1 (mobile nav), C8 (IA rethink), R9 (server-side overview filtering), R15 (sticky detail header), N2 (attention table to TanStack) | ~3 days |
