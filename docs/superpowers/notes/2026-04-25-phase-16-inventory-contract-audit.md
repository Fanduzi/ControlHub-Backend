# Phase 16 Inventory Contract Audit

## Backend Commit

cdcff76 (Phase 16.0 stabilization)

## Endpoints Audited

| Endpoint | Current Shape | OpenAPI Match | Frontend Need | Decision |
|----------|---------------|---------------|---------------|----------|
| GET /resources | `{ items, pageInfo }` — `clusterId` present for instances, `profileSummary` always null | Spec documents `profileSummary` as conditional on non-existent `includeProfile` param; `clusterId` in Resource schema | Need `clusterId` for database tree | **GAP**: `profileSummary` dead code; `clusterId` works in list |
| GET /resources/{id} | `{ resource: {...} }` envelope — `clusterId` ABSENT for instances, `profileSummary` always null | Schema includes `clusterId` and `profileSummary` | Need `clusterId` for database detail/tree | **BLOCKING GAP**: `clusterId` missing from detail response |
| GET /resources/{id}/relations | `{ items: [{id, fromResourceId, toResourceId, relationType, createdAt}] }` | Matches OpenAPI | Frontend needs readable names | **ACCEPTED**: Separate lookup path for related resource names |
| GET /resource-subtypes | `{ resourceType, subtypes: [{key, label, description}] }` | Matches OpenAPI | Good | **OK**: Contract matches |

## Live JSON Evidence

### GET /resources (list)
- `clusterId` present for database instances (e.g., `"clusterId": 14`)
- `profileSummary` never appears (always `nil`, omitempty)
- Envelope: `{ "items": [...], "pageInfo": {...} }`

### GET /resources/22 (database instance detail)
- Response wrapped in `{ "resource": {...} }` envelope
- **No `clusterId`** despite instance belonging to cluster 14
- No `profileSummary`
- Root cause: `GetResource` uses `scanResource()` with basic `resourceColumns` — no cluster_id subquery

### GET /resources/14 (database cluster detail)
- Same envelope, no `clusterId` (correct — clusters aren't members)
- No `profileSummary`

### GET /resources/22/relations
- Returns bare IDs: `{ id, fromResourceId, toResourceId, relationType, createdAt }`
- No resource names, display names, or types included
- Example: `{ "fromResourceId": 22, "toResourceId": 14, "relationType": "member_of" }`

### GET /resource-subtypes?resourceType=database_instance
- Returns 6 subtypes: mysql, postgresql, redis, clickhouse, mongodb, tidb
- Shape: `{ "resourceType": "database_instance", "subtypes": [{ "key": "mysql", "label": "MySQL", "description": "" }] }`

## Gaps

### GAP-1 (BLOCKING): clusterId missing from GET /resources/{id}

`GetResource` in `resource_repository.go:188` uses `scanResource()` with `resourceColumns` constant. This constant does not include the `cluster_id` subquery that `ListResources` has. Result: database instance detail never shows cluster membership.

Frontend impact: Database tree/detail page cannot determine which cluster an instance belongs to from the detail response alone.

**Fix**: Add cluster_id subquery to `GetResource`.

### GAP-2 (OpenAPI accuracy): profileSummary documented but not implemented

- OpenAPI `Resource.profileSummary` description says "present when includeProfile=true query param is set"
- No `includeProfile` query param exists in the spec or handler
- Repository never populates `ProfileSummary` — it is always `nil`
- Go model `ProfileSummary` struct exists but is only used in `topology_service.go`
- OpenAPI profileSummary properties (`engine, version, host, port, role`) don't match Go model fields (`hostname, ip, port, nodeCount, engine`)

**Decision**: Update OpenAPI description to accurately reflect current state — profileSummary is not yet populated in list/detail responses.

### GAP-3 (ACCEPTED): Relations return bare IDs

Relations endpoint returns `{ fromResourceId, toResourceId }` with no readable names. Frontend must batch-fetch resource details for related IDs. This is an explicit architectural choice — not a bug.

## Required Backend Fixes

1. **GAP-1**: Add `clusterId` to `GetResource` SQL query in `resource_repository.go`
2. **GAP-2**: Update OpenAPI `profileSummary` description to remove misleading `includeProfile` reference

## Required Frontend Assumptions

1. `GET /resources/{id}` will include `clusterId` after GAP-1 fix
2. `profileSummary` is NOT populated — frontend should use `GET /resources/{id}/profile` for full profile data
3. Relations return bare resource IDs — frontend must resolve names via batch lookup or resource list endpoint
4. `GET /resource-subtypes` requires `resourceType` query param (returns 400 without it)
