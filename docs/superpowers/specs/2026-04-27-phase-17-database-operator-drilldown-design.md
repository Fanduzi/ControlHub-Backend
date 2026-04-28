# Phase 17 Database Operator Drilldown Design

**Date:** 2026-04-27
**Status:** Draft
**Scope:** Cross-repo milestone design for completing read-only database operator workflows after Phase 16 unified inventory and browser QA gate.

---

## Background

Phase 16 made `Resources` the canonical inventory entry, redirected `CMDB` into that model, stabilized the frontend test baseline, and added browser smoke verification. The next product risk is no longer basic navigation. The risk is that a database operator can open a cluster or instance and still not know what action to take next.

Current facts:

- `Resources` is the canonical inventory and CRUD surface.
- `Databases` is a specialized lens over the same resource data.
- `GET /resources/{id}` now includes `clusterId` for database instances.
- `profileSummary` is documented but not populated in list/detail responses.
- `GET /resources/{id}/relations` returns bare IDs, so the frontend must do separate lookups for display names.
- Phase 16C smoke tests verify that the shell and major pages load without console/network errors.

Phase 17 turns the read-only database workflow into a credible operator drilldown.

---

## Milestone Goal

Deliver a **read-only database operator drilldown**:

- A database cluster detail page shows cluster identity, profile summary, member instances, relation context, topology, and recent audit activity.
- A database instance detail page shows parent cluster, hostname/port, role, profile summary, related resources, topology, and recent audit activity.
- Relations and member rows use human-readable display data, not bare IDs.
- The frontend no longer has to infer critical database read-model facts through multiple ad hoc lookups.
- Browser QA proves a real operator can navigate from list to cluster to instance to audit without runtime warnings or network errors.

---

## Non-Goals

- No SQL execution.
- No SQL work orders.
- No topology editing.
- No write operations beyond existing archive/unarchive/resource CRUD.
- No new permission model.
- No CMDB navigation restoration.
- No demo `resourceSummaries` restoration.

---

## Design Decisions

### D1: Backend Read Model Comes First

Frontend UX should not invent database truth. Backend Phase 17A must provide stable read-model fields before frontend Phase 17B claims completion.

Required backend read-model improvements:

- Populate `profileSummary` where possible for resource list and detail responses.
- Expose readable relation endpoint rows or an equivalent batch resource summary resolver.
- Provide a stable cluster member view for database clusters.
- Keep OpenAPI, Go models, integration tests, and Schemathesis in sync.

### D2: Keep The Workflow Read-Only

Phase 17 improves operator understanding, not operation execution. If a UI design implies "repair", "failover", "promote", or "run SQL", it is out of scope.

### D3: Cluster And Instance Detail Pages Need Different Information Hierarchy

Cluster detail should prioritize:

- cluster status and ownership
- aggregate profile summary such as node count
- member instances
- topology and relation context
- audit activity

Instance detail should prioritize:

- parent cluster
- hostname/port
- database role where available
- profile details
- placement and relations
- audit activity

### D4: Relations Must Be Human-Readable

Bare relation IDs are acceptable as a storage contract, but not as an operator UI contract. Phase 17 should either:

- add resolved relation display fields to `GET /resources/{id}/relations`, or
- add a batch resolver endpoint and make the frontend use it consistently.

The preferred backend contract is resolved relation rows because it avoids repeated lookup behavior in every frontend panel.

### D5: QA Gate Must Follow A Real Operator Path

Smoke tests prove pages load. Phase 17C must prove a workflow:

1. Login.
2. Open resources.
3. Find a database cluster.
4. Open cluster detail.
5. Inspect member instances.
6. Open one instance.
7. Navigate back to cluster or topology/audit context.
8. Assert no unexpected browser console warnings/errors and no 4xx/5xx responses.

---

## Backend Contract Shape

### ResourceProfileSummary

`profileSummary` remains nullable, but when backend can derive values it should populate:

- `hostname`
- `ip`
- `port`
- `engine`
- `version`
- `nodeCount`
- `role`

Only include fields backed by current data. Do not invent values.

### ResourceRelationView

For `GET /resources/{id}/relations`, add display metadata while preserving existing IDs:

- `id`
- `fromResourceId`
- `toResourceId`
- `relationType`
- `direction`
- `relatedResourceId`
- `relatedResourceName`
- `relatedResourceDisplayName`
- `relatedResourceType`
- `relatedResourceSubtype`
- `relatedResourceHealthStatus`
- `relatedResourceLifecycleStatus`

### ClusterMemberView

For database clusters, provide member rows either embedded in detail or through a dedicated endpoint. Preferred endpoint:

`GET /resources/{id}/members`

Response:

```json
{
  "members": [
    {
      "resourceId": 22,
      "name": "payment-mysql-primary-prod",
      "displayName": "Payment MySQL Primary Production",
      "resourceType": "database_instance",
      "resourceSubtype": "mysql",
      "lifecycleStatus": "running",
      "healthStatus": "healthy",
      "profileSummary": {
        "hostname": "payment-mysql-01.prod",
        "port": 3306,
        "role": "primary"
      }
    }
  ]
}
```

If backend chooses not to add a new endpoint, it must document exactly where the frontend should get equivalent member rows.

---

## Frontend UX Shape

### Database Cluster Detail

Required sections:

- Header: display name, type/subtype, environment, owner, health/lifecycle.
- Operator summary: node count, engine, version if available, source, archive state.
- Member instances: name, role, hostname, port, health, lifecycle, link to instance detail.
- Relations: readable related resource names and relation types.
- Topology: existing topology panel, not redesigned in this phase.
- Audit: recent events for this resource.

### Database Instance Detail

Required sections:

- Header: display name, type/subtype, environment, owner, health/lifecycle.
- Parent cluster card: link to cluster, cluster status.
- Connection/profile card: hostname, ip, port, engine, version, role.
- Relations: readable related resource names and relation types.
- Topology: existing topology panel.
- Audit: recent events for this resource.

---

## Completion Criteria

Backend Phase 17A is complete when:

- OpenAPI documents the final read contract.
- Unit tests cover profile summary, relation view, and cluster member view.
- Integration tests verify real MySQL seed data.
- `make test-openapi-fuzz` passes.
- Daily `controlhub` DB is not modified except through explicit migration flow if a migration is required.

Frontend Phase 17B is complete when:

- Cluster and instance detail pages use the new read model.
- No empty/operator-useless panels are shown when data is absent; use concise "not provided" states.
- Relations and member rows are human-readable.
- TypeScript, lint, tests, build pass.

Frontend Phase 17C is complete when:

- Operator workflow E2E passes against real backend.
- Browser output has no unexpected warnings/errors.
- Generated artifacts are cleaned.

