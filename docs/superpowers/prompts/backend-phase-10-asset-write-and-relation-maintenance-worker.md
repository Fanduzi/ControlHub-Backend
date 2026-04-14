# Backend Phase 10: Asset Write APIs And Relation Maintenance

You are implementing the next backend phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`

## Goal

ControlHub has a solid read-only asset foundation:

- resources can be listed with pagination and filters
- resource details, relations, profiles, dictionaries, and audit events can be read
- richer demo data exposes real list behavior

The next foundation step is to make assets maintainable by humans. This phase turns the read-only CMDB baseline into a minimal write-capable asset system.

Do not widen into SQL work orders, topology graph rendering, discovery integrations, or production auth.

## Scope

Do exactly this:

1. add resource create and update APIs
2. add relation create and delete APIs
3. add validation against existing dictionaries and reference tables
4. keep write behavior small, explicit, and testable
5. update OpenAPI, README, and tests

This phase is about manual asset maintenance only.

## Endpoints In Scope

### 1. `POST /resources`

Create a resource.

Request body:

```json
{
  "resourceType": "database_instance",
  "resourceSubtype": "mysql",
  "name": "order-mysql-02-prod",
  "displayName": "Order MySQL 02 Prod",
  "environmentId": "10000000-0000-0000-0000-000000000001",
  "ownerId": "20000000-0000-0000-0000-000000000002",
  "lifecycleStatus": "running",
  "healthStatus": "healthy",
  "source": "manual",
  "externalId": "order-mysql-02-prod",
  "labels": {
    "team": "order",
    "tier": "data"
  }
}
```

Response:

- `201 Created`
- response body is the created `Resource` using the existing camelCase wire shape

### 2. `PATCH /resources/{id}`

Update resource base metadata.

Allow updating only these fields:

- `resourceSubtype`
- `displayName`
- `environmentId`
- `ownerId`
- `lifecycleStatus`
- `healthStatus`
- `source`
- `externalId`
- `labels`

Do not allow changing:

- `id`
- `resourceType`
- `name`
- `createdAt`

Request body may be partial:

```json
{
  "displayName": "Order MySQL Primary Prod",
  "healthStatus": "warning",
  "labels": {
    "team": "order",
    "tier": "data",
    "pci": "false"
  }
}
```

Response:

- `200 OK`
- response body is the updated `Resource`

### 3. `POST /resources/{id}/relations`

Create a relation from `{id}` to another resource.

Request body:

```json
{
  "toResourceId": "40000000-0000-0000-0000-000000000002",
  "relationType": "depends_on"
}
```

Response:

- `201 Created`
- response body is the created `DependencyRelation`

Rules:

- `fromResourceId` is always the path resource id
- `toResourceId` must exist
- `relationType` must be one of `GET /relation-types`
- reject self-relations unless there is a documented reason to allow them
- prevent exact duplicate relation triples: `fromResourceId + toResourceId + relationType`

### 4. `DELETE /resource-relations/{id}`

Delete a relation by relation id.

Response:

- `204 No Content`

If this route is awkward for the current router, `DELETE /resources/{resourceId}/relations/{relationId}` is acceptable. Pick one and document it formally in OpenAPI.

## Validation Requirements

Validate all write inputs server-side.

Required fields for create:

- `resourceType`
- `name`
- `displayName`
- `environmentId`
- `ownerId`
- `lifecycleStatus`
- `healthStatus`
- `source`

Validation rules:

- `resourceType` must be one of `GET /resource-types`
- `lifecycleStatus` must be one of `GET /lifecycle-statuses`
- `healthStatus` must be one of `GET /health-statuses`
- `environmentId` must exist
- `ownerId` must exist
- `source` must be `manual` for this phase unless current model already supports more values
- `labels` must be a JSON object, not an array or scalar
- `name` must be non-empty and URL/identifier friendly enough for operations use
- `displayName` must be non-empty

Uniqueness:

- Add a pragmatic uniqueness rule for resources.
- Recommended: unique `name` within `environmentId`.
- If you choose a different rule, explain it in OpenAPI/README and tests.

Do not implement hard delete for resources in this phase.

## Database Guidance

- Use MySQL migrations.
- Add only the indexes or constraints needed for the write APIs.
- If adding a unique constraint, make sure existing seed data satisfies it.
- Do not add EAV tables.
- Do not change `resource_profiles_*` in this phase unless required for compilation.
- Do not write audit events for every mutation yet unless the current architecture makes it trivial. Audit storage is intentionally bootstrap-only and long-term ClickHouse-oriented.

## Error Shape

If the project already has a consistent error response, reuse it.

If not, keep it simple but consistent:

```json
{
  "error": "validation_failed",
  "message": "resourceType is not supported"
}
```

Use appropriate HTTP status codes:

- `400` for malformed input
- `404` for missing resource/environment/owner
- `409` for duplicate resource or duplicate relation
- `500` only for unexpected server failures

Document the chosen error shape in OpenAPI.

## OpenAPI

OpenAPI must be formal contract, not comments.

Update `internal/openapi/openapi.yaml` with:

- new paths
- request schemas
- response schemas
- error response schema
- examples
- status codes

Frontend will bind to this contract, so do not leave fields ambiguous.

## Testing

Follow TDD.

At minimum add/update tests covering:

- creating a valid resource
- rejecting unsupported `resourceType`
- rejecting unsupported `lifecycleStatus`
- rejecting unsupported `healthStatus`
- rejecting missing `environmentId`
- rejecting missing `ownerId`
- rejecting duplicate resource identity
- patching valid mutable fields
- rejecting patch attempts to immutable fields if your decoder permits detecting them
- creating a valid relation
- rejecting relation to missing resource
- rejecting unsupported `relationType`
- rejecting duplicate relation
- deleting a relation
- listing relations after create/delete

Repository/service/API layers should all have meaningful coverage where the project pattern expects it.

## Live Verification

If local MySQL is available:

1. apply migrations from a clean database
2. start the server with `.env`
3. create a manual resource
4. fetch it through `GET /resources/{id}`
5. confirm it appears in `GET /resources?q=...`
6. create a relation from an existing resource to the new resource
7. confirm it appears in `GET /resources/{id}/relations`
8. delete the relation
9. confirm it no longer appears

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

Run live MySQL smoke tests if available.

## Final Report

Your final report must include:

- changed files
- final endpoint list and request/response examples
- validation and uniqueness rules chosen
- migration summary
- test results
- live verification results
- commit hash
- remaining risks

## Constraints

- use a dedicated worktree unless blocked
- use TDD
- do not reset the repo
- do not discard unrelated work
- do not widen scope beyond asset write APIs and relation maintenance
- do not implement SQL work orders, SQL query, topology graph UI, or discovery source ingestion
