# Resource Archive Contract Design

Date: 2026-04-15

## Purpose

ControlHub needs a safe way to remove manually created or test-created assets from normal operational views without physically deleting historical identity immediately.

This is required for:

- keeping E2E-created `e2e-` resources from polluting default resource lists
- supporting human asset retirement workflows
- preserving relation and audit context while avoiding hard-delete hazards

## Decision

Use **archive / soft-delete semantics** for resources.

Implement:

- `POST /resources/{id}/archive`
- `GET /resources` excludes archived resources by default
- `GET /resources?includeArchived=true` includes archived resources
- `GET /resources/{id}` still returns archived resources by ID
- archived resources cannot be updated through normal `PATCH /resources/{id}`
- relation creation from/to archived resources is rejected
- existing relations are not physically deleted during archive in the first implementation

Do not implement hard delete in this phase.

## Data Model

Add nullable archive metadata to `resources`:

- `archived_at`
- `archived_by`
- `archive_reason`

Recommended wire fields:

- `archivedAt`
- `archivedBy`
- `archiveReason`

`archivedBy` may be nullable until production auth is introduced.

## Archive Request

`POST /resources/{id}/archive`

Request:

```json
{
  "reason": "e2e cleanup"
}
```

Rules:

- `reason` is optional but, if provided, must be a non-empty string after trimming.
- archiving an already archived resource is idempotent and returns `200` with the resource.
- unknown resource returns `404`.
- response is the archived `Resource` using the existing camelCase shape plus archive fields.

## List Behavior

`GET /resources`

Default:

- `includeArchived` omitted or `false` excludes archived resources.

Explicit:

- `includeArchived=true` includes archived resources.

Pagination counts must follow the same filter:

- default `pageInfo.totalItems` excludes archived resources
- `includeArchived=true` counts both active and archived resources

## Detail Behavior

`GET /resources/{id}` returns the resource by ID even if archived.

Rationale:

- direct links should remain inspectable
- audit/debug workflows need to inspect archived assets
- frontend E2E can confirm cleanup state without list pollution

## Mutation Behavior

For archived resources:

- `PATCH /resources/{id}` returns `409 resource_archived`
- `POST /resources/{id}/relations` returns `409 resource_archived` if the source resource is archived
- creating a relation to an archived target returns `409 resource_archived`
- `GET /resources/{id}/relations` remains readable
- `GET /resources/{id}/topology` remains readable but may show archived nodes if relations still exist

Do not cascade delete relations in the first implementation.

## Error Shape

Use the existing JSON error shape:

```json
{
  "error": "resource_archived",
  "message": "resource is archived"
}
```

## OpenAPI

OpenAPI must document:

- `POST /resources/{id}/archive`
- `includeArchived` query param on `GET /resources`
- archive fields on `Resource`
- 409 `resource_archived` responses for write paths that reject archived resources

## Frontend Follow-Up

After backend Phase 12.1 merges, frontend Phase 13.7 should:

- use `POST /resources/{id}/archive` for E2E cleanup
- stop relying on decommission-only cleanup
- optionally expose archived state only where useful

Frontend Phase 13.7 should not start until backend Phase 12.1 freezes the final JSON contract.
