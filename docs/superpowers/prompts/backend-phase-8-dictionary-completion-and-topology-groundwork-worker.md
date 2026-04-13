# Backend Phase 8: Dictionary Completion and Topology Groundwork

You are implementing the next backend phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

## Goal

Continue strengthening the asset foundation by:

1. completing the backend dictionary surface for core resource metadata
2. preparing topology projection groundwork at the contract/design level

Do not implement SQL work orders, SQL query execution, topology UI, asset write flows, or ClickHouse audit work in this phase.

## Current Backend State

Already implemented:

- resource core APIs
- resource profile projection
- environments / owners / roles
- resource-types / relation-types dictionaries
- MySQL metadata store
- OpenAPI contract

Current notable gap:

- frontend still hardcodes some lifecycle / health / dictionary assumptions
- topology is still only available implicitly through raw resources + relations

## Scope

Do exactly these two things:

1. complete dictionary endpoints for core operator-facing enums
2. produce topology projection groundwork without overbuilding

## Part 1: Dictionary Completion

Add backend dictionary support for:

- lifecycle statuses
- health statuses

Preferred endpoints:

- `GET /lifecycle-statuses`
- `GET /health-statuses`

Keep response shape aligned with existing dictionary style:

```json
{
  "items": [
    {
      "key": "running",
      "label": "Running",
      "description": "Resource is active and serving expected workload."
    }
  ]
}
```

Requirements:

- use camelCase
- follow the same dictionary item schema pattern already used by:
  - `/resource-types`
  - `/relation-types`
- update OpenAPI
- wire through model / service / repository / api consistently

Static backend dictionaries are acceptable for this phase. Do not introduce an admin-managed taxonomy system.

## Part 2: Topology Groundwork

Do **not** build a full topology API yet unless it stays very small and clean.

Instead, deliver one of these two acceptable outcomes:

### Preferred outcome

Add a small, explicit topology contract draft to the backend docs/OpenAPI notes:

- proposed endpoint path
  - e.g. `GET /resources/{id}/topology`
- proposed response shape
  - `nodes`
  - `edges`
  - optional `groups` or `layoutHints`
- scope definition
  - first version should be local topology around a resource, not a global graph

This should be documented clearly enough that the frontend can later consume a stable projection instead of reconstructing graph shape from raw entities.

### Optional implementation outcome

If the projection can be kept small and clean, you may additionally implement a minimal read-only endpoint:

- `GET /resources/{id}/topology`

But only if:

- it is clearly local in scope
- it does not mutate existing contracts
- it does not require broad schema changes
- it does not explode into graph-engine work

If you implement it, keep it narrow:

- 1-hop or 2-hop local graph around a resource
- built from existing resources + relations
- no layout engine
- no topology persistence

If this starts widening scope, stop at the documented contract draft only.

## Suggested Files To Inspect

- `internal/openapi/openapi.yaml`
- `internal/model/dictionary.go`
- `internal/model/taxonomy.go`
- `internal/model/relation.go`
- `internal/repository/mysql/dictionary_repository.go`
- `internal/api/dictionary_handler.go`
- `internal/api/router.go`
- `README.md`
- `docs/superpowers/specs/2026-04-11-unified-resource-console-design.md`

## Verification

You must run:

```bash
go test ./internal/api -v
go test ./internal/model -v
go test ./internal/service -v
make test
go vet ./...
go build ./...
```

If you implement new endpoints, also do live verification if local MySQL is available.

## Final Report

Your final report must include:

- changed files
- whether you implemented only dictionary completion, or dictionary completion + topology endpoint
- new endpoint paths
- example JSON for lifecycle and health dictionaries
- if topology work was added:
  - endpoint path
  - response shape summary
  - whether it is contract-only or fully implemented
- test results
- live verification results, if any
- commit hash
- remaining risks

## Constraints

- do not reset the repo
- do not discard unrelated work
- do not change existing core resource contracts
- do not widen into topology UI
- do not implement SQL work orders, query execution, or asset mutation APIs
