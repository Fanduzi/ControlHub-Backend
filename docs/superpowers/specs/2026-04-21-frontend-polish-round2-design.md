# Frontend Polish Round 2 — Design Document

**Date:** 2026-04-21
**Source:** 4-agent parallel audit (UX consistency, i18n text quality, table columns, detail page completeness)
**Scope:** 13 actionable items across i18n, icon consistency, table data completeness, and detail page polish
**Out of scope:** Sorting (significant feature), orphaned key cleanup (no user impact), `formatRelativeDateTime` i18n refactor (needs architecture decision)

---

## Problem Statement

After shipping the CMDB redesign baseline (archive, profile summaries, deployed resources, i18n natural-language rewrite), a multi-agent audit found three categories of remaining quality gaps:

1. **i18n hardcodes** — Column headers and relative time strings bypass the translation system, breaking zh-CN users
2. **Icon gaps** — DbTypeIcon exists but is missing from 5+ locations where database types appear
3. **Table completeness** — The database-specific table lacks columns that the general resource table already shows (hostname, port, lifecycle badge)

---

## Design Decisions

### D1: DbTypeIcon everywhere database types appear

**Decision:** Every component that renders a database resource type must include `DbTypeIcon` when `resourceType` is `database_instance`, `database_cluster`, or `database_proxy`.

**Locations to fix:**
- Cluster members table subtype column (`cluster-members-table.tsx:57`)
- Resource relation panel type pill (`resource-relation-panel.tsx:219`)
- Resource detail page subtype field (`resources/[id]/page.tsx:105`)
- Topology panel node subtype label (`topology-panel.tsx:298`)
- Resource detail sheet header (`resource-detail-sheet.tsx:87`)

**Implementation pattern** (consistent across all locations):
```tsx
{(related.resourceType === "database_instance" ||
  related.resourceType === "database_cluster" ||
  related.resourceType === "database_proxy") && (
  <DbTypeIcon subtype={related.resourceSubtype} className="size-3.5" />
)}
```

### D2: Database table column alignment

**Decision:** The database table (`database-table.tsx`) must match the resource table's column richness for database-specific fields. Add hostname, port columns (visible by default for database types) and lifecycle badge.

**New columns:**
| Column | Source | Default visible |
|--------|--------|----------------|
| Hostname | `profileSummary.hostname` | Yes |
| Port | `profileSummary.port` | Yes |
| Status (lifecycle) | `lifecycleStatus` | Yes (alongside existing health) |

**Also fix:** environment/owner columns need explicit `cell` renderers matching resource table pattern. Date column must switch from `formatDateTime` to `formatRelativeDateTime` with absolute tooltip.

### D3: i18n hardcode elimination

**Decision:** Replace all hardcoded English strings in TSX files with `t()` calls.

**Affected files and strings:**

| File | Hardcoded strings | Action |
|------|-------------------|--------|
| `resource-table.tsx` | "Hostname", "Port", "Nodes", "Subtype", "External ID", "Source" | Use existing keys or add new keys |
| `resource-search-combobox.tsx` | "Searching...", "No resources found." | Use `t("common.loading")` and `t("common.noResults")` |

**New i18n keys needed:**
| Key | EN | ZH-CN |
|-----|----|----|
| `common.fields.hostname` | "Hostname" | "主机名" |
| `common.fields.port` | "Port" | "端口" |
| `common.fields.nodes` | "Nodes" | "节点数" |

**Existing keys to reuse:**
- `common.fields.resourceSubtype` ("Resource subtype" / "资源子类型") for "Subtype"
- `common.fields.externalId` ("External ID" / "外部 ID") for "External ID"
- `common.fields.source` ("Source" / "来源") for "Source"
- `common.loading` ("Loading..." / "加载中...") for "Searching..."
- `common.noResults` ("No results found." / "未找到结果。") for "No resources found."

### D4: Terminology unification — "resource" not "asset"

**Decision:** Replace all instances of "asset" in user-facing i18n strings with "resource".

**Affected keys (both en.json and zh-CN.json):**
- `pages.overview.posture.total`: "Total managed assets" → "Total managed resources"
- `pages.cmdb.description`: "Browse assets by" → "Browse resources by"
- `pages.settings.dictionaries.resourceType`: "Top-level asset families" → "Top-level resource families"
- `pages.settings.dictionaries.lifecycleStatus`: "Asset lifecycle" → "Resource lifecycle"
- `mutations.create.description`: "manually maintained asset" → "manually maintained resource"
- `mutations.relation.addDescription`: "to another asset" → "to another resource"
- `detailSheet.relationsDescription`: "linked to this asset" → "linked to this resource"
- `pages.settings.environments.description`: "asset registration" → "resource registration"
- `shell.description`: "asset context" → "resource context"
- `shell.subtitle`: "asset visibility" → "resource visibility"

### D5: Detail page micro-fixes

**Decision:** Fix two detail page edge cases.

1. **resourceSubtype empty fallback** (`page.tsx:109`): Change `{resource.resourceSubtype}` to `{resource.resourceSubtype || t("common.notSet")}`
2. **Labels empty state** (`page.tsx:152`): Add conditional — when `Object.keys(resource.labels).length === 0`, show `{t("common.notSet")}` instead of rendering nothing

---

## Files Summary

### Modified files

| File | Changes |
|------|---------|
| `components/blocks/cluster-members-table.tsx` | Add DbTypeIcon to subtype column |
| `components/blocks/resource-relation-panel.tsx` | Add DbTypeIcon to type pill, add status badges |
| `app/(console)/resources/[id]/page.tsx` | Add DbTypeIcon to subtype field, empty fallbacks |
| `components/blocks/topology-panel.tsx` | Add DbTypeIcon to node subtype |
| `components/resources/resource-detail-sheet.tsx` | Add DbTypeIcon to header |
| `components/databases/database-table.tsx` | Add hostname/port columns, lifecycle badge, cell renderers, relative date |
| `components/resources/resource-table.tsx` | Replace 6 hardcoded column headers with t() |
| `components/blocks/resource-search-combobox.tsx` | Replace hardcoded strings with t() |
| `components/blocks/deployed-resources-card.tsx` | Fix empty description="" |
| `messages/en.json` | Add 3 keys, fix 10 asset→resource |
| `messages/zh-CN.json` | Add 3 keys, fix 10 asset→resource |

### Not changed (deferred)
- Table sorting (feature-level change, separate sprint)
- `formatRelativeDateTime` i18n (needs architecture decision on how to pass t() into lib)
- Relation panel status badges (design needs UX input on density)
- Orphaned key cleanup (no user impact)
- Profile `spec` column (backend change needed)
