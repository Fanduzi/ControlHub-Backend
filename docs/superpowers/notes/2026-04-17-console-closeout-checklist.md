# 2026-04-17 Console Closeout Checklist

This checklist records the 11 console issues raised during live review and classifies each item against the **currently merged main branches** only.

Scope used for this checklist:

- frontend main: `/Users/fan/JsProjects/ControlHub` at `18a9906`
- backend main: `/Users/fan/GolangProjects/ControlHub` at `0d793bb`
- included: already merged code on `main`
- excluded: unmerged worktrees, prompt intent, worker claims without merged code evidence

Status meanings:

- `Resolved` = merged main behavior matches the requested outcome closely enough
- `Partially resolved` = some improvement landed, but the original requirement is still not fully satisfied
- `Unresolved` = the core problem is still present on merged main

## Checklist

### 1. Rename awkward `Notification Delivery Service Production`

- Status: `Resolved`
- Current fact:
  - seed truth now uses `Notification Delivery Service`
- Evidence:
  - [0008_apply_demo_seed_cleanup_patch.sql](/Users/fan/GolangProjects/ControlHub/migrations/0008_apply_demo_seed_cleanup_patch.sql:24)
  - [0004_seed_demo_data.sql](/Users/fan/GolangProjects/ControlHub/migrations/0004_seed_demo_data.sql:211)

### 2. Sidebar / dock should be collapsible

- Status: `Resolved`
- Current fact:
  - desktop sidebar collapse exists
  - sticky collapse control exists at the bottom edge
- Evidence:
  - [app-shell.tsx](/Users/fan/JsProjects/ControlHub/components/app-shell/app-shell.tsx:20)
  - [sidebar.tsx](/Users/fan/JsProjects/ControlHub/components/app-shell/sidebar.tsx:149)

### 3. “Open full detail” should be at the top of the detail sheet

- Status: `Resolved`
- Current fact:
  - the detail sheet header action cluster includes the full-detail link
- Evidence:
  - [resource-detail-sheet.tsx](/Users/fan/JsProjects/ControlHub/components/resources/resource-detail-sheet.tsx:69)

### 4. `degraded` / topology status labels should be localized in Chinese

- Status: `Resolved`
- Current fact:
  - status badges translate `degraded`, `unknown`, `stopped`
  - topology nodes render status through `StatusBadge`
- Evidence:
  - [status-badge.tsx](/Users/fan/JsProjects/ControlHub/components/blocks/status-badge.tsx:27)
  - [zh-CN.json](/Users/fan/JsProjects/ControlHub/messages/zh-CN.json:1)
  - [topology-panel.tsx](/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx:147)

### 5. Database replication topology still has poor layout and edge-through-node problems

- Status: `Resolved`
- Current fact:
  - frontend main now consumes backend semantic topology fields and uses a semantic banded layout plus replication-depth columns
  - expanded topology inspection exists
  - React Flow handle warnings were fixed by removing mismatched explicit handle IDs from generated edges
  - NaN geometry warnings were fixed with defensive topology position guards
  - edge clearance was improved with wider spacing and differentiated backbone / non-backbone routing
- Evidence:
  - semantic layer ordering + replication-depth columns: [topology-mapper.ts](/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts:11)
  - semantic node rendering + URL-synced expanded mode: [topology-panel.tsx](/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx:62)
  - backend semantic fields: [topology.go](/Users/fan/GolangProjects/ControlHub/internal/model/topology.go:77)

### 6. VIP / ProxySQL / Orchestrator / MySQL should be separated into semantic bands, not mixed into one generic graph

- Status: `Resolved`
- Current fact:
  - frontend main now implements a banded operator view using backend semantic metadata
  - layer order is application → entry → cluster → replication → control_plane → host
- Evidence:
  - backend semantics: [topology.go](/Users/fan/GolangProjects/ControlHub/internal/model/topology.go:77)
  - frontend semantic layer order: [topology-mapper.ts](/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts:13)

### 7. Topology canvas is still too small and lacks fullscreen / expanded analysis mode

- Status: `Resolved`
- Current fact:
  - merged main now includes a URL-synced expanded / fullscreen-style topology overlay
  - `topologyDepth`, `topologyDirection`, and `topologyExpanded` are synced in the detail page URL
  - `Esc` closes the expanded view
- Evidence:
  - [topology-panel.tsx](/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx:81)
  - [resources/[id]/page.tsx](/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx:1)

### 8. Databases page: layout, search, page density, and filtering

- Status: `Resolved`
- Current fact:
  - right-side posture squeeze is gone; the posture summary is now a top strip
  - search exists
  - default page size is now `15`
  - engine filtering exists and is now multi-select
- Evidence:
  - top strip: [databases/page.tsx](/Users/fan/JsProjects/ControlHub/app/(console)/databases/page.tsx:25)
  - search + multi-select engine filter: [database-table.tsx](/Users/fan/JsProjects/ControlHub/components/databases/database-table.tsx:74)
  - default page size: [list-page-search-params.ts](/Users/fan/JsProjects/ControlHub/lib/list-page-search-params.ts:7)

### 9. Environment URL should be readable instead of exposing UUIDs

- Status: `Resolved`
- Current fact:
  - user-facing console URLs now use readable `environment=<slug>` in supported flows
  - pages resolve slug to backend `environmentId` internally before requests
  - backend/service-layer still uses `environmentId` internally, but the browser address bar no longer needs to expose UUIDs for normal console filtering/navigation
- Evidence:
  - slug resolution: [environment-params.ts](/Users/fan/JsProjects/ControlHub/lib/environment-params.ts:7)
  - topbar writes readable `environment`: [topbar.tsx](/Users/fan/JsProjects/ControlHub/components/app-shell/topbar.tsx:77)
  - sidebar links use environment slug: [sidebar.tsx](/Users/fan/JsProjects/ControlHub/components/app-shell/sidebar.tsx:24)
  - CMDB cleanup removes legacy `environmentId` from URL: [cmdb-table.tsx](/Users/fan/JsProjects/ControlHub/components/cmdb/cmdb-table.tsx:157)
  - internal request mapping still uses `environmentId`: [services/resources.ts](/Users/fan/JsProjects/ControlHub/services/resources.ts:39)

### 10. Resources page still has filter and action problems

- Status: `Resolved`
- Current fact:
  - default page size is reduced to `15`
  - subtype filtering exists
  - lifecycle and health are now multi-select
  - resource type and resource subtype are now multi-select
  - multi-value filters use repeated URL params, for example `?resourceType=database_instance&resourceType=host`
  - create action duplication is gone; page header button was removed and table toolbar keeps the single create entry
  - archive filter intentionally remains single-select because it is a mutually exclusive view-mode toggle, not an OR filter family
- Evidence:
  - page header no longer renders create action: [resources/page.tsx](/Users/fan/JsProjects/ControlHub/app/(console)/resources/page.tsx:67)
  - table-local single create action: [resource-table.tsx](/Users/fan/JsProjects/ControlHub/components/resources/resource-table.tsx:261)
  - resource type / subtype / lifecycle / health multi-select filters: [resource-table.tsx](/Users/fan/JsProjects/ControlHub/components/resources/resource-table.tsx:284)
  - repeated resource query params: [services/resources.ts](/Users/fan/JsProjects/ControlHub/services/resources.ts:39)
  - default page size: [list-page-search-params.ts](/Users/fan/JsProjects/ControlHub/lib/list-page-search-params.ts:7)

### 11. Audits page “recent changes” should not be a cramped side rail

- Status: `Resolved`
- Current fact:
  - audits page now uses a stacked composition
  - table first, timeline below
- Evidence:
  - [audits/page.tsx](/Users/fan/JsProjects/ControlHub/app/(console)/audits/page.tsx:24)

## Rollup

### Resolved

- 1. Notification Delivery Service naming
- 2. Sidebar collapse
- 3. Open full detail placement
- 4. Chinese status localization in topology/status badges
- 5. Replication topology layout and edge routing
- 6. Semantic banded operator view for database topology
- 7. Expanded/fullscreen topology analysis mode
- 8. Databases page cleanup
- 9. Environment URL readability
- 10. Resources page filters and action cleanup
- 11. Audits stacked layout

### Partially Resolved

- none

### Unresolved
- none from the original 11 remain fully unresolved on merged main

## What This Means For The Next Phase

All 11 originally tracked console-review issues are now closed on merged main. Future work should be planned as new product phases rather than as leftovers from this checklist.

This checklist should be used during future frontend closeout review instead of relying on memory or prompt prose.
