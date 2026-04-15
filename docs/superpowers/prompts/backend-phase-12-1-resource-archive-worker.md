# Backend Phase 12.1: Resource Archive / Soft Delete

You are implementing the backend resource archive / soft-delete phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-15-resource-archive-contract-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-10-asset-write-and-relation-maintenance-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-11-7-openapi-schema-tightening-worker.md`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`
- `/Users/fan/GolangProjects/ControlHub/internal/model/resource.go`
- `/Users/fan/GolangProjects/ControlHub/internal/model/resource_write.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/resource_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/relation_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/repository/mysql/resource_repository.go`
- `/Users/fan/GolangProjects/ControlHub/internal/repository/mysql/relation_repository.go`

## Goal

ControlHub now supports manual asset creation and E2E tests can create resources. Because there is no hard-delete resource API, test-created and retired resources accumulate in normal list views.

This phase adds resource archive / soft-delete semantics so resources can be removed from default operational lists without physically deleting identity or history.

This is a product-contract phase. Keep it small, explicit, and testable.

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
docker info
```

Expected:

- worktree path is under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- branch is dedicated to this phase
- base includes backend Phase 11.6 Schemathesis fuzzing
- worktree is clean
- Docker is available for integration/fuzz verification

If Docker is unavailable, stop and report the blocker for integration verification.

## Parallel Development / Merge Order

Backend Phase 11.7 may be developed in parallel in another worktree.

Rules:

- You may implement this phase in parallel with Phase 11.7.
- Do not merge this phase before Phase 11.7 unless explicitly told.
- Before final merge, sync/rebase/merge the latest `main` after Phase 11.7 has landed.
- After syncing Phase 11.7, rerun the full verification list, including `make test-openapi-fuzz`.
- Resolve OpenAPI conflicts deliberately. Do not overwrite Phase 11.7 schema tightening.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Implement archive / soft-delete, not hard delete.
- Use `POST /resources/{id}/archive` as the archive endpoint.
- `GET /resources` excludes archived resources by default.
- `GET /resources?includeArchived=true` includes archived resources.
- `GET /resources/{id}` still returns archived resources by ID.
- Archived resources reject normal metadata patch and new relation creation.
- Existing relations are not cascade-deleted during archive in this phase.
- Do not add frontend changes in this phase.
- Do not add SQL work orders, query execution, discovery, or topology editing.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. add archive fields to the resource model and MySQL schema
2. add `POST /resources/{id}/archive`
3. add `includeArchived` filtering to `GET /resources`
4. reject normal writes involving archived resources
5. update OpenAPI, README/CLAUDE, tests, integration tests, and Schemathesis compatibility

Do not implement hard delete.

## Data Model

Add a goose migration:

```text
migrations/0007_add_resource_archive_fields.sql
```

Recommended columns:

- `archived_at DATETIME(6) NULL`
- `archived_by VARCHAR(128) NULL`
- `archive_reason VARCHAR(512) NULL`

Add indexes only if needed for default list filtering. A simple index on `archived_at` is acceptable if it helps the default `archived_at IS NULL` query.

Update the resource wire shape with nullable fields:

- `archivedAt`
- `archivedBy`
- `archiveReason`

Do not remove existing fields.

## Endpoint

### `POST /resources/{id}/archive`

Request:

```json
{
  "reason": "e2e cleanup"
}
```

Response:

- `200 OK`
- body is the archived `Resource`

Rules:

- unknown resource returns `404`
- already archived resource is idempotent and returns `200`
- `reason` is optional
- if `reason` is present, it must be non-empty after trimming
- `archivedBy` may be empty/null until production auth exists

Do not require auth in this phase.

## List Behavior

Update `GET /resources`:

- default excludes archived resources
- `includeArchived=true` includes archived resources
- `includeArchived=false` behaves like default
- pagination totals must respect the archive filter
- all existing filters must continue to work

Update `ResourceListQuery` or equivalent model to carry `includeArchived`.

## Detail Behavior

`GET /resources/{id}` returns archived resources by ID.

Rationale:

- direct links remain inspectable
- E2E cleanup can verify archive state
- history/debug workflows can still inspect retired assets

## Write Behavior For Archived Resources

Add explicit behavior and tests:

- `PATCH /resources/{id}` returns `409 resource_archived` if the resource is archived
- `POST /resources/{id}/relations` returns `409 resource_archived` if source resource is archived
- relation creation returns `409 resource_archived` if target resource is archived
- `GET /resources/{id}/relations` remains readable
- `GET /resources/{id}/topology` remains readable

Use existing JSON error shape:

```json
{
  "error": "resource_archived",
  "message": "resource is archived"
}
```

## OpenAPI

OpenAPI must formally document:

- `POST /resources/{id}/archive`
- archive request schema
- archive fields on `Resource`
- `includeArchived` query parameter on `GET /resources`
- `409 resource_archived` for:
  - `PATCH /resources/{id}`
  - `POST /resources/{id}/relations`

If Phase 11.7 schema tightening is already on main, preserve those constraints.

## Testing

Follow TDD.

At minimum add/update tests for:

- migration adds archive fields
- archive unknown resource returns 404
- archive valid resource returns 200 with archive fields
- archive is idempotent
- invalid blank reason returns 400
- default list excludes archived resources
- `includeArchived=true` includes archived resources
- pagination counts exclude/include archived resources correctly
- `GET /resources/{id}` returns archived resource
- `PATCH /resources/{id}` rejects archived resource with 409
- relation creation from archived source returns 409
- relation creation to archived target returns 409
- relation list/topology reads remain available as intended

Use fake API/service tests and real MySQL integration tests where appropriate.

## Verification

You must run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Also run a live local smoke against disposable or local dev DB if practical:

- create resource
- archive it
- verify default `/resources` excludes it
- verify `/resources?includeArchived=true` includes it
- verify direct `GET /resources/{id}` returns it
- verify patch rejects it

Do not use the user's daily `controlhub` DB for destructive experiments unless explicitly instructed. Archive tests should use disposable DBs where possible.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only. Do not stage `.idea`, logs, container artifacts, temporary files, generated Schemathesis reports, or local virtualenv files.

If GitNexus is available, run the repository's configured change-impact check before commit.

## Final Report

Your final report must include:

- worktree path and branch
- commit hash
- changed files
- migration name and columns added
- final archive endpoint JSON examples
- final `GET /resources` includeArchived behavior
- archived resource mutation behavior
- relation behavior with archived resources
- tests added or updated
- verification command results
- live smoke result, if run
- whether Phase 11.7 was already merged before final verification
- confirmation that the daily `controlhub` DB was not touched for destructive testing
- negative scope confirmation:
  - did not implement hard delete
  - did not add frontend changes
  - did not add auth middleware
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not tag, push, release, or add AI co-author
- next phase input:
  - final contract for frontend Phase 13.7 cleanup integration
  - any remaining archive/list semantics questions
  - whether hard delete should remain out of scope

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD
- do not reset the repo
- do not discard unrelated work
- do not physically delete resources in this phase
- do not add frontend changes
- do not weaken OpenAPI validation or Schemathesis coverage
