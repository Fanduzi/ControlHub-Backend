# Backend Phase 9: List Pagination And Filtering

You are implementing the next backend phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`

## Goal

ControlHub now has richer demo data. The list APIs are starting to show their limits:

- no pagination
- no scalable server-side filtering
- frontend is still forced to fetch too much data and filter locally

This phase adds scalable list query support without widening into write APIs or upper-layer features.

## Scope

Do exactly this:

1. add pagination to list endpoints
2. add practical server-side filtering for resource and audit lists
3. keep current contracts backward-compatible where reasonable
4. update OpenAPI and tests

Do not implement asset write APIs, topology endpoints, SQL work orders, or query execution.

## Endpoints In Scope

### 1. `GET /resources`

Add support for:

- `page`
- `pageSize`
- `environmentId`
- `resourceType`
- `lifecycleStatus`
- `healthStatus`
- `q` (free-text search over useful identifier/display fields)

You may support either:

- repeated `resourceType` params
- or a comma-separated `resourceType`

Pick one and document it clearly in OpenAPI.

### 2. `GET /audit-events`

Add support for:

- `page`
- `pageSize`
- `targetResourceId`
- `eventType`
- `result`
- optional `q` only if it stays small and coherent

### 3. Optional

If it stays clean, you may also support `page` + `pageSize` on:

- `/environments`
- `/owners`
- `/roles`

But this is not required. Prioritize `/resources` and `/audit-events`.

## Response Shape

Keep the existing `items` array contract.

Add a pagination block without breaking current consumers, for example:

```json
{
  "items": [],
  "pageInfo": {
    "page": 1,
    "pageSize": 20,
    "totalItems": 64,
    "totalPages": 4
  }
}
```

Requirements:

- existing frontend that only reads `items` should continue working
- use camelCase
- use one consistent `pageInfo` schema across list endpoints

## Implementation Guidance

- Prefer small, explicit repository methods over magical query builders
- Use MySQL-friendly filtering with predictable SQL
- Keep query logic understandable
- Add sane defaults, for example:
  - `page=1`
  - `pageSize=20`
- Add a hard upper bound for page size, for example 100

For search:

- keep it pragmatic
- matching `name`, `display_name`, `external_id` is enough for phase 1.5
- do not build full-text search infrastructure

## OpenAPI

You must update OpenAPI with:

- query parameters
- `pageInfo` schema
- updated list response schemas
- concrete examples

Comments in `openapi.yaml` are not enough. This must be formal contract.

## Testing

Follow TDD.

At minimum add/update tests covering:

- default pagination
- custom `page` / `pageSize`
- resource filtering by `environmentId`
- resource filtering by `resourceType`
- resource filtering by status
- resource search by `q`
- audit filtering by `targetResourceId`
- audit filtering by `eventType` / `result`
- response `pageInfo`

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

If local MySQL is available, also do live verification with the richer seed data:

- `/resources?page=1&pageSize=20`
- `/resources?environmentId=...`
- `/resources?resourceType=database_instance`
- `/resources?q=mysql`
- `/audit-events?page=1&pageSize=10`
- `/audit-events?result=success`

## Final Report

Your final report must include:

- changed files
- exact supported query params
- final `pageInfo` JSON shape
- whether existing `items`-only consumers remain compatible
- test results
- live verification results
- commit hash
- remaining risks

## Constraints

- use a dedicated worktree unless blocked
- do not reset the repo
- do not discard unrelated work
- do not widen scope beyond list pagination and filtering
