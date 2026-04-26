# Phase 16 Unified Inventory IA + Database Operator Workflow Design

**Date:** 2026-04-25
**Status:** Draft
**Scope:** Cross-repo milestone design for backend stabilization, inventory contract freeze, frontend IA consolidation, and browser-backed QA gates.

---

## Background

ControlHub has moved past the initial asset list, archive, topology, OpenAPI, integration, and fuzzing phases. Recent work also fixed many review findings around dictionary display, JSON error shapes, topology layout, database tree behavior, and resource detail localization.

The current product risk is no longer one missing widget. The risk is trust:

- `Resources`, `CMDB`, `Databases`, detail pages, sheets, and topology all describe the same inventory from different angles.
- The user has repeatedly found problems only through live review after workers reported completion.
- Backend contracts are mostly mature, but contract drift still appears in response envelopes, OpenAPI fields, page-size limits, `profileSummary`, and `clusterId`.
- Frontend quality is improving, but the workflow still needs a single coherent inventory mental model and proof through real browser verification.

This milestone turns ControlHub into a more credible operator console before expanding to new features.

---

## Milestone Goal

Deliver a **Unified Inventory IA + Database Operator Workflow**:

- `Resources` is the canonical asset inventory and CRUD surface.
- `CMDB` is not a separate competing product area; CMDB metadata becomes columns and detail metadata inside the inventory model.
- `Databases` is a specialized operator lens over the same resource data, optimized for clusters, instances, profile summaries, and member diagnosis.
- `Topology` is a secondary visual analysis surface that consumes the same backend truth instead of inventing its own model.
- Every completion claim must be backed by build/test evidence and live browser evidence.

---

## Current Facts That Shape The Design

### Backend

Recent backend `main` commits include:

- `4c359bc` — wraps `GET /resources/{id}` in `{ resource }`
- `8d8be36` — database tree table backend fixes from six-agent review
- `05272da` — adds `/resource-subtypes` to OpenAPI, fixes `archivedBy`, adds `profileSummary`
- JSON error shape fixes for auth, dictionaries, and audit handlers
- profile service tests and demo data gap patches

Backend already has:

- goose migrations
- OpenAPI validation
- Testcontainers integration tests
- Schemathesis OpenAPI fuzzing
- resource archive/unarchive
- multi-select list filters
- `resourceSubtype` filtering
- topology semantic metadata

Known backend blocker observed during planning:

```text
go test ./... fails:
TestListResources_PageSizeCap expected pageSize capped to 100, got 500
```

This must be fixed before any new backend work is considered stable.

### Frontend

Recent frontend `main` commits include:

- dictionary category name translation
- resource/relation type display translation
- high/medium review fixes
- 401/sign-out/404/polish fixes
- health status enum cleanup
- fallback dictionary fixes

Frontend already has:

- Next.js App Router
- shadcn/ui based console shell
- resources/databases/audits/settings pages
- URL-synced filters
- ReactFlow topology panel
- Playwright E2E and Vitest

Known process issue:

- Worker reports have repeatedly overclaimed completion.
- Live browser review caught issues not caught by unit tests alone.

---

## Design Decisions

### D1: Backend Stabilization Comes First

**Decision:** Phase 16 starts with a short backend stabilization patch before product UI work.

**Why:** A failing `go test ./...` invalidates later backend claims. The page-size cap drift is small but represents exactly the type of contract mismatch that has repeatedly hurt the project.

**Required outcome:**

- `go test ./...` passes.
- OpenAPI and tests agree on the page-size cap.
- `make openapi-validate` passes.
- Integration and fuzz gates are run before backend milestone closure.

---

### D2: Freeze Inventory Contract Before Frontend IA Work

**Decision:** Backend must freeze the inventory read contract needed by the frontend before the frontend claims the IA redesign is complete.

**Contract surfaces:**

- `GET /resources`
- `GET /resources/{id}`
- `GET /resources/{id}/relations`
- resource profile summary fields
- database cluster membership
- dictionary/resource subtype endpoints
- archive metadata shape

**Contract questions to settle:**

- Is `profileSummary` populated in list responses, or only documented?
- Is `clusterId` available consistently where the frontend needs it?
- Do relations include enough related resource display data to avoid UUID-first UI?
- Do cluster members come from `GET /resources/{id}` or a dedicated member endpoint?

---

### D3: Resources Becomes The Canonical Inventory Surface

**Decision:** `Resources` remains the canonical inventory page.

**Implications:**

- Resource CRUD lives here.
- CMDB metadata such as `externalId`, `source`, labels, archive metadata, and profile summary should be available as columns or detail metadata.
- The navigation should not imply that `CMDB` is a separate data model from `Resources`.

**Acceptable implementation options:**

- Remove `/cmdb` navigation and keep the route as a redirect or lightweight compatibility page.
- Keep `/cmdb` temporarily but make it explicitly a saved/customized inventory view, not a separate product concept.

The preferred end state is a single inventory mental model.

---

### D4: Databases Is A Specialized Operator Lens

**Decision:** `Databases` should not be a second generic resource list. It should answer database operator questions quickly:

- Which clusters exist in this environment?
- Which instances belong to each cluster?
- Which instance is primary/replica/proxy/control plane?
- What hostname/IP/port should I use?
- What is unhealthy, missing, archived, or unusual?

**Required UI capabilities:**

- tree/table view with stable cluster/member rows
- server-backed filters
- visible profile summary columns such as hostname, IP, port, engine, node count
- detail page/sheet with cluster member table
- relation names and member names instead of UUID-first rendering

---

### D5: Topology Is Secondary, Not The Primary Detail Workflow

**Decision:** Do not make topology the headline of Phase 16.

**Why:** Previous topology work suffered because the product model underneath was unclear. Once `Resources` and `Databases` have stable read models, topology should consume those same semantics and become easier to validate.

**Topology work allowed in Phase 16:**

- bug fixes that block database detail understanding
- consuming frozen member/profile/relation contract
- closing known problem summary gaps if low-risk

**Topology work deferred:**

- another visual redesign round
- topology editing
- export/share features

---

### D6: Browser Evidence Is A Release Gate

**Decision:** Phase 16 cannot close on unit tests alone.

Every worker final report must include:

- exact commands run
- frontend screenshots or saved Playwright artifacts for critical pages
- console error check
- network 4xx/5xx check
- explicit "not verified" statements for skipped areas

**Required critical flows:**

- login
- resources list
- resources detail
- create/edit/archive/unarchive where applicable
- databases tree/detail
- settings dictionaries
- audits list

---

## In Scope

- Backend test drift fix for page-size cap.
- Backend inventory contract parity for `profileSummary`, `clusterId`, resource detail envelope, relations, and cluster members.
- Frontend IA consolidation of Resources/CMDB/Databases.
- Database operator workflow improvements based on backend contract.
- Browser-backed QA gate and completion evidence.
- Documentation of known limitations.

---

## Out Of Scope

- New storage model or new database tables unless a contract cannot be met without them.
- Full authentication/authorization redesign.
- Bulk actions.
- Command palette.
- Onboarding.
- Another topology visual redesign.
- SQL work orders or SQL execution.
- CSV import/export.
- Notification/webhook system.

---

## Acceptance Criteria

### Product Acceptance

- A user can log in, browse resources, filter/search resources, open a resource detail, and understand what the resource is without raw enum or fallback text leaks.
- A user can open `/databases`, identify clusters and members, see profile summary information, filter by engine/environment, and open useful details.
- `CMDB` no longer competes with `Resources` as a separate unexplained concept.
- Relations and members use readable names first; UUIDs are secondary inspect/copy details.
- Topology remains usable and consistent with the inventory/detail model.

### Backend Acceptance

- `go test ./...` passes.
- `go vet ./...` passes.
- `go build ./...` passes.
- `make openapi-validate` passes.
- `make test-integration` passes.
- `make test-openapi-fuzz` passes.
- OpenAPI examples cover the frozen inventory contract.

### Frontend Acceptance

- `npx tsc --noEmit -p tsconfig.json` passes.
- `npm run lint` passes.
- `npm run test` passes.
- `npm run build` passes.
- Playwright critical path passes.
- Browser console has no unexpected errors.
- Unexpected API 4xx/5xx requests are investigated and reported.
- Screenshots are attached or stored for the acceptance pages.

---

## Recommended Phase Split

| Phase | Name | Owner | Purpose |
|-------|------|-------|---------|
| 16.0 | Backend Stabilization Patch | Backend | Fix current test drift and re-green backend |
| 16A | Inventory Contract Freeze | Backend | Freeze resource/list/detail/relation/member/profile contract |
| 16B | Unified Inventory IA | Frontend | Consolidate Resources/CMDB/Databases user model |
| 16C | Browser QA Gate | Frontend + Process | Make live verification part of done |

---

## Non-Negotiable Reporting Rule

Workers must not report "complete" unless all required verification was run in the same worktree after the final code change.

If a check is skipped, the report must say:

```text
Not verified: <area>, reason: <specific blocker>
```

