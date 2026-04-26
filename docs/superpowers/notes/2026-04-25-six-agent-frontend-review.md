# Six-Agent Frontend Review Findings

> **Date:** 2026-04-25
> **Scope:** Full frontend project at /Users/fan/JsProjects/ControlHub
> **Agents:** Frontend Developer, UX Architect, UX Researcher, UI Designer, Evidence Collector, API Tester
> **Status:** Findings documented, awaiting triage

---

## Deduplicated Summary

All 6 agents reviewed independently. Findings below are deduplicated and consolidated by severity.

### CRITICAL (6)

| # | Category | Finding | Files | Discovered By |
|---|----------|---------|-------|---------------|
| C1 | Performance | `buildLookupMaps()` fetches ALL resources on every detail page load just to resolve foreign key IDs into display names. O(n) API calls per page view, degrades linearly at scale. | `lib/view-models.ts:83-97` | Frontend Dev, API Tester |
| C2 | Performance | `listDatabaseResources()`, `listAttentionResources()`, `getOverviewMetrics()` all call `listAllResources()` then filter client-side. Backend already supports filter params. | `services/resources.ts:132-169` | Frontend Dev |
| C3 | Data Integrity | Hardcoded `resourceSummaries` and `actorLabels` maps keyed by demo resource IDs (1-4). Any production data shows raw IDs. | `lib/view-models.ts:41-56`, `lib/resource-copy.ts:1-10` | Frontend Dev |
| C4 | Security | No auth middleware, no route protection. Unauthenticated users can navigate directly to `/overview`, `/resources` etc. | Missing: `middleware.ts` | UX Architect, API Tester |
| C5 | API Mismatch | Frontend `ResourceDetailResponse` expects `{ resource, members }` but backend returns flat `Resource`. Cluster members table is silently broken. | Frontend: `types/resource.ts:251-254`, `services/resources.ts:82-103`. Backend: `internal/api/resource_handler.go:46-61` | API Tester |
| C6 | Code Quality | Duplicate `appendRepeated` utility copy-pasted in two service files. | `services/resources.ts:15-25`, `services/audits.ts:8-18` | Frontend Dev |

### HIGH (12)

| # | Category | Finding | Files | Discovered By |
|---|----------|---------|-------|---------------|
| H1 | Broken Feature | Database table search and engine filters are visual-only (change URL params) but never filter displayed data. | `databases/page.tsx:24`, `database-table.tsx:151-159` | Evidence Collector |
| H2 | Broken Feature | Edit button opens empty `EditResourceSheet` when detail data hasn't loaded yet (click before fetch completes). | `resource-detail-sheet.tsx:73,109,283-287` | Evidence Collector |
| H3 | UX Flow | Resource detail "back" breadcrumb navigates to `/resources` without preserving filter/search/page state. `saveResourceListUrl` is saved but never read back. | `resources/[id]/page.tsx`, `resource-table.tsx:151` | UX Architect |
| H4 | Responsive | Sidebar completely hidden below `lg` breakpoint with no hamburger menu, drawer, or alternative navigation. Mobile users cannot navigate. | `app-shell.tsx:23` | UX Architect |
| H5 | State Mgmt | TopologyPanel manages 9 separate `useState` hooks, causing multiple re-renders per fetch cycle. Should use `useReducer`. | `topology-panel.tsx:60-68` | Frontend Dev |
| H6 | Type Safety | `getResourceById` does `response as unknown as Resource` double cast, silently masking type errors for legacy response format. | `services/resources.ts:80-104` | Frontend Dev |
| H7 | Performance | `resource-table.tsx` columns array (136 lines) not wrapped in `useMemo`, forcing TanStack Table recalculation on every render. | `resource-table.tsx:128-264` | Frontend Dev |
| H8 | Performance | `database-table.tsx` `replaceSearchParams` not wrapped in `useCallback`, causes `useDebounceCallback` to reset each render. | `database-table.tsx:360-374` | Frontend Dev |
| H9 | Fragile Code | Error status detection uses string matching (`message.includes("404")`) instead of `instanceof ApiError && err.status === 404` in 5+ locations. | `topology.ts:36`, `api-error.tsx:15-23`, `resource-archive-button.tsx:42,64`, `resource-relation-panel.tsx:82-93` | Frontend Dev |
| H10 | Security | Token stored in `sessionStorage` (XSS-vulnerable). No token refresh mechanism. Role stored but never used. | `api-client.ts:43`, `app/login/page.tsx:42-43` | API Tester |
| H11 | UX Flow | Create resource sheet has two independent state instances (topbar + resource table) that can open simultaneously. | `topbar.tsx:169`, `resource-table.tsx:396` | UX Architect |
| H12 | UX Flow | Sign out button has no `onClick` handler — clicking it does nothing. | `topbar.tsx:219` | UX Architect, UX Researcher |

### MEDIUM (20)

| # | Category | Finding | Files |
|---|----------|---------|-------|
| M1 | i18n | LabelsEditor has 4 hardcoded English strings ("Key", "Value", "Add label", "Key cannot be empty", "Duplicate key") with no i18n. | `labels-editor.tsx:73,88,103,23,25` |
| M2 | React | LabelsEditor uses array index as React key, fragile validation state on delete. | `labels-editor.tsx:68` |
| M3 | Performance | `useDebounceCallback` stores callback in dependency array, resets debounce timer on every render with inline closures. | `hooks/use-debounce.ts` |
| M4 | Performance | `ResourceSearchCombobox` fires immediate API request on every keystroke (no debounce). | `resource-search-combobox.tsx:40-57` |
| M5 | Performance | `listAllResources` fetches all pages sequentially (50+ sequential HTTP requests possible). No upper bound, no AbortController. | `services/resources.ts:63-78` |
| M6 | Performance | `listRecentAuditEvents` fetches ALL events then slices 5. Backend should support `limit` param. | `services/audits.ts:74-80` |
| M7 | Performance | `listAvailableSubtypes` fetches all pages of resources just to extract distinct subtypes on every page load. | `resources/page.tsx:25-49` |
| M8 | Consistency | `resource-table.tsx` `subtypeOptions` computed outside `useMemo`, new array every render. | `resource-table.tsx:307-313` |
| M9 | Consistency | `overview-content.tsx` `hasMoreAttention` computed outside `useMemo`. | `overview-content.tsx:130` |
| M10 | Consistency | Duplicate `updateMultiSelectParams` function in resource-table and database-table. | `resource-table.tsx:68-77`, `database-table.tsx:125-134` |
| M11 | Type Drift | Fallback lifecycle statuses in `settings.ts` (`running`, `pending`, `retired`) don't match backend enum (`provisioning`, `running`, `stopped`, `degraded`, `decommissioning`). | `services/settings.ts:100-106` |
| M12 | Type Drift | Fallback relation types in `settings.ts` only list 4 of 7 backend values, plus 2 that don't exist in backend. | `services/settings.ts:97-99` |
| M13 | API Client | No request timeout in `apiClient`. Hung backend connection leaves UI spinning indefinitely. | `services/api-client.ts:56` |
| M14 | API Client | `cache: "no-store"` on every API call, no ISR or revalidation strategy. Every navigation = full refetch. | `services/api-client.ts:63` |
| M15 | Auth | Server components send unauthenticated requests (getAuthHeaders returns empty on server side). | `services/api-client.ts:39-41` |
| M16 | UX | Database posture counts and table data fetched in separate API calls — can be inconsistent if data changes between calls. | `databases/page.tsx:21-26` |
| M17 | UX | Resource detail page shows 8-11 sections with no tab-based progressive disclosure. Excessive vertical scrolling. | `resources/[id]/page.tsx` |
| M18 | UX | Create resource form has 13+ fields in single scrolling sheet, no step-based wizard. | `create-resource-sheet.tsx` |
| M19 | UX | Topbar controls overflow on small screens — environment selector, language, theme, accent, quick action all wrap. | `topbar.tsx:142` |
| M20 | UX | No bulk actions for resource management (no row selection, no batch operations). | `resource-table.tsx` |

### Visual / UI Issues (from UI Designer)

| # | Priority | Finding | Files |
|---|----------|---------|-------|
| V1 | High | LabelsEditor uses `border-red-500` / `text-red-500` instead of `destructive` token — breaks in dark mode. | `labels-editor.tsx:78,83` |
| V2 | High | Relation panel delete button uses `x` text character instead of `<X>` icon. | `resource-relation-panel.tsx:242` |
| V3 | Medium | "Archived" indicator uses inline classes instead of Badge component. | `resource-table.tsx:183`, `resource-detail-sheet.tsx:114` |
| V4 | Medium | Database table expander button bypasses Button component, missing focus ring. | `database-table.tsx:226` |
| V5 | Medium | Audit table has double-wrapped overflow container. | `audit-table.tsx:208-209` |
| V6 | Medium | Success banner uses hardcoded emerald instead of semantic token. | `create-resource-sheet.tsx:698` |
| V7 | Medium | Cluster badge uses `rounded` (4px) while StatusBadge uses `rounded-md` (6px). | `database-table.tsx:254` |
| V8 | Medium | Form action buttons not sticky at bottom of long sheets. | `create-resource-sheet.tsx:727`, `edit-resource-sheet.tsx:735` |
| V9 | Medium | Topbar eyebrow (xs/0.14em) vs sidebar brand (11px/0.16em) — inconsistent sizing. | `topbar.tsx:134`, `sidebar.tsx:44` |
| V10 | Low | `tracking-[0.14em]` repeated 20+ times — should be extracted to shared utility. | Multiple files |
| V11 | Low | Topology popup repositioning has no CSS transition — jumps between nodes. | `topology-node-popup.tsx:61` |
| V12 | Low | Archived rows have same hover behavior as active rows despite `opacity-60`. | `resource-table.tsx:531` |

### UX Researcher — Major Usability Findings

| # | Finding | Heuristic |
|---|---------|-----------|
| U1 | Notifications bell is a dead button — no onClick, no tooltip, no empty state. | Visibility of system status |
| U2 | No global loading indicator for navigation transitions. | Visibility of system status |
| U3 | No onboarding or first-run experience for new users. | Help and documentation |
| U4 | No keyboard navigation within tables (arrow keys, shortcuts). | Flexibility and efficiency |
| U5 | Create sheet has no unsaved-changes guard (unlike edit sheet). | User control and freedom |
| U6 | Login page hardcodes `text-rose-700` for validation errors — breaks dark mode. | Consistency and standards |

### Positive Patterns Noted by Multiple Agents

1. **URL-synced filter state** — all table filters/pagination in URL params, shareable/bookmarkable
2. **Unsaved changes guard** — edit sheet AlertDialog prevents accidental data loss
3. **Server-side field error mapping** — backend validation errors mapped to individual form fields
4. **i18n completeness** — perfect key parity between en.json and zh-CN.json (656 lines each)
5. **Skeleton loading** — all async data surfaces show skeleton placeholders
6. **Topology visualization** — sophisticated ReactFlow graph with layer bands, problem highlighting, fullscreen mode
7. **Accessibility foundations** — table rows with `role`, `tabIndex`, `aria-label`, `aria-expanded`
8. **Preference persistence** — environment/owner saved to localStorage for repeated creates
9. **Design token system** — oklch colors with light/dark/accent theme variants
10. **Resizable sheets** — sheet width persisted to localStorage

---

## Recommended Fix Priority

### Phase 1 — Broken Features & Security (must fix)
1. C4: Add auth middleware (Next.js middleware.ts)
2. C5: Fix ResourceDetailResponse mismatch (backend or frontend)
3. H1: Make database search/engine filters actually work
4. H2: Disable edit button while detail is loading
5. H12: Wire up sign out handler

### Phase 2 — Performance & Scaling
1. C1: Cache dictionary/lookup data instead of fetching all resources
2. C2: Push filtering to backend API instead of client-side
3. C3: Remove hardcoded demo data maps
4. H7/H8: Wrap columns in useMemo, memoize replaceSearchParams
5. M5/M6/M7: Optimize sequential API calls

### Phase 3 — Code Quality & Consistency
1. H9: Replace all string-based error detection with ApiError status checks
2. M1/M2: Fix LabelsEditor i18n and React key
3. M3: Fix debounce hook stale closure
4. V1/V2: Fix design token violations (red-500 → destructive, x → X icon)

### Phase 4 — UX Polish
1. H3: Preserve filter state on breadcrumb back navigation
2. H4: Add mobile navigation pattern
3. V8: Sticky form action buttons
4. M17/M18: Consider tab-based detail page and wizard-style create form

---

## Agent Stats

| Agent | Tool Calls | Duration | Findings |
|-------|-----------|----------|----------|
| Frontend Developer | 56 | ~3.5 min | 4 CRITICAL + 9 HIGH + 12 MEDIUM + 7 LOW |
| UX Architect | 42 | ~3.5 min | 3 HIGH + 11 MEDIUM + 6 LOW |
| UX Researcher | 45 | ~5 min | 5 Major + 14 Minor + 4 Cosmetic |
| UI Designer | 47 | ~5.5 min | 2 High + 8 Medium + 9 Low |
| Evidence Collector | 27 | ~2.5 min | 2 HIGH + 3 MEDIUM |
| API Tester | 53 | ~4.5 min | 1 CRITICAL + 4 HIGH + 9 MEDIUM + 8 LOW |
