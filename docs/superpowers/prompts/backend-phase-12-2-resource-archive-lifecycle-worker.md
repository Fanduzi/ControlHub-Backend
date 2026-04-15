# Backend Phase 12.2: Resource Archive Lifecycle Completion

You are implementing the backend resource archive lifecycle completion phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-15-resource-archive-contract-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-1-resource-archive-worker.md`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`
- `/Users/fan/GolangProjects/ControlHub/internal/model/resource.go`
- `/Users/fan/GolangProjects/ControlHub/internal/model/resource_write.go`
- `/Users/fan/GolangProjects/ControlHub/internal/model/pagination.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/resource_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/repository/mysql/resource_repository.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/resource_handler.go`

## Goal

Backend Phase 12.1 added archive metadata and `POST /resources/{id}/archive`, but the archive lifecycle is still one-way and list filtering is not expressive enough for product UI work.

This phase completes the minimal archive lifecycle so frontend product flows can:

- keep default lists operational and archive-hidden
- explicitly browse archived resources when needed
- restore archived resources back to operational visibility

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
- base includes backend Phase 12.1 on `main`
- worktree is clean
- Docker is available for integration and fuzz verification

If Docker is unavailable, stop and report the blocker.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Keep archive as metadata-based soft delete. Do not add hard delete.
- Do not add `archived` as a new `lifecycleStatus`.
- Keep default `GET /resources` behavior: archived resources are excluded unless explicitly requested.
- Add `archivedOnly=true` to fetch only archived resources.
- Add `POST /resources/{id}/unarchive`.
- Unarchive clears `archivedAt`, `archivedBy`, and `archiveReason`.
- Archived resources remain directly fetchable by ID.
- Archived resources still reject normal patch and relation-create writes unless first unarchived.
- Do not add frontend changes in this phase.
- Do not add SQL work orders, query execution, discovery, topology editing, or auth middleware.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. extend list filtering to support `archivedOnly`
2. add `POST /resources/{id}/unarchive`
3. formalize archive/unarchive response behavior and error behavior
4. keep pagination and counts correct across archive filters
5. update OpenAPI, README/CLAUDE, unit/integration/fuzz coverage

Do not implement hard delete.

## List Filtering Contract

`GET /resources` must support:

- default: archived excluded
- `includeArchived=true`: active + archived
- `includeArchived=false`: same as default
- `archivedOnly=true`: archived only

Rules:

- `archivedOnly=true` takes precedence over `includeArchived`
- pagination totals must reflect the effective archive filter
- all existing filters (`resourceType`, `environmentId`, `lifecycleStatus`, `healthStatus`, `q`) must continue to work with archive filters

If helpful, add a clear query model field rather than overloading booleans in handler code.

## Unarchive Endpoint

### `POST /resources/{id}/unarchive`

Request body:

- empty body is acceptable
- do not require a JSON payload

Response:

- `200 OK`
- body is the unarchived `Resource`

Rules:

- unknown resource returns `404`
- already active resource is idempotent and returns `200`
- unarchive clears:
  - `archivedAt`
  - `archivedBy`
  - `archiveReason`
- after unarchive, normal patch and relation-create behavior is restored

Do not require auth in this phase.

## Archive / Unarchive Semantics

Preserve Phase 12.1 behavior:

- `POST /resources/{id}/archive` remains idempotent
- archived resource is hidden from default list
- archived resource is still readable by ID
- archived resource rejects normal `PATCH`
- archived resource rejects relation creation as source or target

Add explicit unarchive semantics:

- unarchived resource reappears in default list views
- unarchived resource is writable again
- unarchived resource can participate in relation creation again

## OpenAPI

OpenAPI must formally document:

- `POST /resources/{id}/unarchive`
- `archivedOnly` query parameter on `GET /resources`
- list-filter precedence and semantics in descriptions
- idempotent behavior for both archive and unarchive
- archive metadata fields remain nullable on `Resource`

Preserve Phase 11.7 request-schema tightening and Phase 12.1 archive contract. Do not regress them.

## Testing

Follow TDD.

At minimum add or update tests for:

- default list excludes archived
- `includeArchived=true` includes both active and archived
- `archivedOnly=true` returns archived only
- `archivedOnly=true` pagination totals/counts are correct
- archive then unarchive round-trip returns expected fields
- unarchive unknown resource returns 404
- unarchive is idempotent for active resource
- unarchived resource reappears in default list
- patch on archived returns 409, then succeeds after unarchive
- relation creation against archived returns 409, then succeeds after unarchive
- OpenAPI validation still passes
- integration tests cover real MySQL + goose behavior
- Schemathesis fuzz still passes

Use fake handler/service tests and real MySQL integration tests where appropriate.

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

Also run a live local smoke when practical:

- create resource
- archive it
- confirm default list hides it
- confirm `includeArchived=true` shows it
- confirm `archivedOnly=true` shows only archived rows
- unarchive it
- confirm default list shows it again
- confirm patch and relation create work again after unarchive

Do not use the user's daily `controlhub` DB for destructive experiments unless explicitly instructed. Prefer disposable DBs where possible.

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
- exact `GET /resources` archive-filter semantics
- exact `POST /resources/{id}/unarchive` request/response shape
- idempotency behavior for archive and unarchive
- verification command results
- live backend verification result
- confirmation that the user's daily local DB was not used destructively
- negative scope confirmation:
  - did not add hard delete
  - did not add `archived` lifecycleStatus
  - did not add frontend changes
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not add auth middleware
  - did not tag, push, release, or add AI co-author
- next phase input:
  - any remaining archive lifecycle gaps
  - whether frontend now has enough contract surface for archive/unarchive UI
  - whether Schemathesis warnings changed

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD for changed behavior
- do not reset the repo
- do not discard unrelated work
- do not add product areas outside resource archive lifecycle
- do not let any tool scan `.worktrees/**` from the main checkout
