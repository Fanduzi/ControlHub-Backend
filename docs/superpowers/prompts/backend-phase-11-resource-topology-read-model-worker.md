# Backend Phase 11: Resource Topology Read Model

You are implementing the backend topology read-model phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`
- `/Users/fan/GolangProjects/ControlHub/README.md`
- `/Users/fan/GolangProjects/ControlHub/CLAUDE.md`

## Goal

ControlHub now has asset CRUD, relation maintenance, goose migrations, and OpenAPI docs. The next CMDB foundation is a topology read model built from existing resources and relations.

This phase adds a backend projection endpoint that frontend graph views can consume.

Do not build frontend graph UI in this phase.
Do not add topology editing.
Do not add layout persistence.
Do not change the relation model unless a bug blocks the read model.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Add `GET /resources/{id}/topology`.
- Build topology from `resources` and `resource_relations`.
- Return nodes and edges, not coordinates.
- Support `depth=1` and `depth=2`; default `depth=1`.
- Support optional `direction=both|upstream|downstream`; default `both`.
- Support optional relation type filtering with `relationType`.
- Use resource and relation ids from the existing schema.
- Keep this endpoint read-only.
- Update OpenAPI formally; comments are not contract.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Endpoint

### `GET /resources/{id}/topology`

Query parameters:

- `depth`: `1` or `2`, default `1`
- `direction`: `both`, `upstream`, or `downstream`, default `both`
- `relationType`: optional, may be repeated or comma-separated; pick one and document it clearly

Definitions:

- `downstream`: edges where the root or current frontier node is `fromResourceId`
- `upstream`: edges where the root or current frontier node is `toResourceId`
- `both`: include both upstream and downstream expansion
- `depth=1`: root plus directly related resources
- `depth=2`: root plus direct neighbors plus one additional hop from those neighbors

Response shape:

```json
{
  "rootResourceId": "40000000-0000-0000-0000-000000000001",
  "depth": 1,
  "direction": "both",
  "nodes": [
    {
      "id": "40000000-0000-0000-0000-000000000001",
      "resourceType": "database_cluster",
      "resourceSubtype": "mysql",
      "name": "order-mysql-cluster-prod",
      "displayName": "Order MySQL Cluster Prod",
      "environmentId": "10000000-0000-0000-0000-000000000001",
      "ownerId": "20000000-0000-0000-0000-000000000002",
      "lifecycleStatus": "running",
      "healthStatus": "healthy",
      "isRoot": true,
      "distance": 0
    }
  ],
  "edges": [
    {
      "id": "50000000-0000-0000-0000-000000000001",
      "fromResourceId": "40000000-0000-0000-0000-000000000002",
      "toResourceId": "40000000-0000-0000-0000-000000000001",
      "relationType": "member_of"
    }
  ],
  "groups": [
    {
      "id": "group-database",
      "label": "Database",
      "resourceType": "database_cluster",
      "nodeIds": ["40000000-0000-0000-0000-000000000001"]
    }
  ]
}
```

Group requirements:

- Groups are optional but should be returned if implementation stays simple.
- Recommended grouping: by `resourceType`.
- Do not make groups a blocker for the main endpoint if node/edge projection is clean.

## Data Rules

- Include the root node even if it has no relations.
- Return `404` if the root resource does not exist.
- De-duplicate nodes and edges.
- Prevent infinite loops in cyclic graphs.
- Keep deterministic ordering for tests:
  - root node first
  - then by distance, resource type, name, id
  - edges by relation type, from id, to id, id
- Do not include resource profiles in topology response.
- Do not include audit events in topology response.

## Implementation Guidance

Follow the existing backend layering:

- `internal/api`
- `internal/service`
- `internal/repository/mysql`
- `internal/model`

Recommended shape:

- model types for topology response
- repository method to fetch root and relation neighborhood
- service method to validate depth/direction/filter and build projection
- handler method to parse params and write JSON

Keep SQL readable. This can be implemented with iterative queries per hop; do not over-engineer recursive SQL unless it stays clearer.

## OpenAPI

Update `internal/openapi/openapi.yaml` formally with:

- path
- query parameters
- schemas
- examples
- error responses

Run `make openapi-validate`.

## Tests

Follow TDD.

At minimum add/update tests covering:

- root with no relations returns one node and no edges
- `depth=1` returns direct neighbors only
- `depth=2` returns second-hop neighbors
- `direction=upstream`
- `direction=downstream`
- `direction=both`
- `relationType` filter
- cyclic graph does not loop forever
- missing root returns `404`
- invalid `depth` returns `400`
- invalid `direction` returns `400`
- OpenAPI validation remains green

Use fake repositories for service/API tests as project conventions require. Add repository tests only if the project already has a clear pattern for MySQL repository behavior.

## Live Verification

With local MySQL and seeded demo data, verify:

- `GET /resources/{database_cluster_id}/topology`
- `GET /resources/{database_cluster_id}/topology?depth=2`
- `GET /resources/{database_cluster_id}/topology?direction=upstream`
- `GET /resources/{database_cluster_id}/topology?direction=downstream`
- `GET /resources/{database_cluster_id}/topology?relationType=member_of`

Use a resource that demonstrates database cluster, instances, proxy, VIP/domain/service, or control plane relations where available.

## Verification

You must run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
```

## Final Report

Your final report must include:

- changed files
- final endpoint contract
- query parameter behavior
- node/edge/group response examples
- test/vet/build/openapi validation results
- live MySQL verification results
- commit hash
- remaining risks

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD
- do not reset the repo
- do not discard unrelated work
- do not add topology editing
- do not add frontend graph UI
- do not add layout persistence
- do not add SQL work orders or query execution
