# Phase 36 Query Workbench Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a governed Query Workbench shell across backend and frontend without executing queries. The backend exposes query target context data; the frontend renders a locked IDE-style workspace around that data.

**Architecture:** Implement the backend query target context read model first, then build a frontend `/query` workbench shell against the contract. Keep query execution disabled and explicit in both API and UI. The target inventory is support data inside the workbench, not the entire product surface.

**Tech Stack:** Go, OpenAPI 3.1, MySQL read models, Next.js, React, TypeScript, Vitest, Playwright.

---

## Required Reading

Backend:

```text
docs/superpowers/specs/2026-06-20-query-workbench-roadmap.md
docs/superpowers/specs/2026-06-20-phase-36-query-workbench-foundation.md
docs/superpowers/notes/2026-06-20-phase-36-bytebase-ui-research.md
internal/model/resource.go
internal/model/resource_write.go
internal/repository/mysql/resource_repository.go
internal/service/resource_service.go
internal/api/resource_handler.go
internal/openapi/openapi.yaml
```

Frontend:

```text
/Users/fan/JsProjects/ControlHub/app
/Users/fan/JsProjects/ControlHub/components
/Users/fan/JsProjects/ControlHub/services
/Users/fan/JsProjects/ControlHub/types
/Users/fan/JsProjects/ControlHub/messages/en.json
/Users/fan/JsProjects/ControlHub/messages/zh-CN.json
```

## Worktree Strategy

Use separate worktrees.

Backend:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree add .worktrees/backend-phase-36-query-workbench-foundation -b phase-36-query-workbench-foundation main
```

Frontend:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree add .worktrees/frontend-phase-36-query-workbench-foundation -b feat/phase-36-query-workbench-foundation main
```

Recommended execution order:

```text
1. Backend contract/read model
2. Frontend page against contract
3. Cross-repo E2E smoke
```

Frontend can start with local TypeScript types after the backend contract is
drafted, but final merge should follow backend merge so the real API exists.

## Backend Tasks

### Task B1: Define Query Target Model

Create or modify backend model files to add:

```text
QueryTarget
QueryTargetConnectionContext
QueryTargetCapability
QueryTargetGovernance
QueryTargetAvailableActions
QueryTargetSchemaPreviewNode
QueryKind
QueryTargetReadiness
QueryTargetSafetyState
```

Expected enum-like values:

```text
query kind: sql, redis, mongo, unsupported
readiness: ready, missing_connection, credential_required, unsupported_engine, disabled
safety state: credential_missing, execution_disabled, unsupported_engine, connection_incomplete
```

Rules:

- No credentials are stored or returned.
- No query execution fields are added.
- Unknown engines remain visible as unsupported targets.
- `governance.executionEnabled` is always false in Phase 36.
- `availableActions.run`, `explain`, `export`, `saveSheet`, and
  `requestAccess` are all false in Phase 36.
- `schemaPreview` is derived only from existing ControlHub metadata or empty.
  No live database introspection.

Before editing any Go symbol, run GitNexus impact analysis for the target symbol.

### Task B2: Implement Derivation Helper

Add a pure helper that derives query capability from existing resource data:

```text
engine -> queryKind/editorMode/languageLabel
host/port presence -> connection completeness
credential absence -> credential_required
unsupported engine -> unsupported_engine
executionEnabled -> false
availableActions -> all false
```

Test cases:

- MySQL -> SQL
- TiDB -> SQL
- PostgreSQL -> SQL
- ClickHouse -> SQL
- Redis -> Redis
- MongoDB -> Mongo
- unknown engine -> unsupported
- missing host -> missing_connection
- missing port -> missing_connection
- complete connection but no credential -> credential_required
- capability.editorMode matches queryKind
- governance.auditRequired is true
- governance.executionEnabled is false
- all availableActions are false
- schemaPreview empty when no metadata exists

### Task B3: Add Repository/Service Read Path

Expose query targets from existing database resources.

Preferred source:

```text
database_instance resources with profile host/port/engine
cluster relation via member_of where available
environment and owner labels from existing resource joins
```

Do not add new tables.

### Task B4: Add API Endpoint

Add:

```text
GET /query-targets
```

Response:

```json
{
  "items": []
}
```

Support initial filters only if cheap and already conventional:

```text
environment
engine
readiness
```

If filters would add complexity, return all targets and let frontend filter in
Phase 36.

### Task B5: OpenAPI And Tests

Update OpenAPI:

```text
GET /query-targets
QueryTarget schema
QueryTargetConnectionContext schema
QueryTargetCapability schema
QueryTargetGovernance schema
QueryTargetAvailableActions schema
QueryTargetSchemaPreviewNode schema
QueryTargetListResponse schema
enum values for queryKind/readiness/safetyState
```

Run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
```

Run `make test-openapi-fuzz` if endpoint is added to OpenAPI and Docker is
available.

## Frontend Tasks

### Task F1: Add Types And Service

Add TypeScript types matching OpenAPI:

```text
QueryTarget
QueryKind
QueryTargetReadiness
QueryTargetSafetyState
```

Add service:

```text
getQueryTargets()
```

Do not include execute query methods.

### Task F2: Add `/query` Workbench Route

Create a page:

```text
/query
```

Required UI direction:

- title: Query Workbench
- use the preview as the UX reference:

```text
docs/superpowers/previews/phase-36-query-workbench-ide/index.html
```

Required surfaces:

- safety banner: query execution is not enabled
- target switcher / connection context
- schema/object browser placeholder
- worksheet tab placeholder
- search input and filters:
  - environment
  - engine
  - query kind
  - readiness
- target list/table or drawer
- disabled editor placeholder
- locked action bar:
  - Run locked
  - Explain locked
  - Save sheet placeholder
  - Export unavailable
- locked result area:
  - Result grid
  - JSON
  - Explain
  - Logs
  - Masking
- query-history placeholder
- access/governance panel:
  - credential state
  - execution disabled state
  - audit requirement
  - JIT/access future state
  - production safety notes

Do not render an enabled Run/Execute button.

Do not collapse this into a simple table page. Bytebase research showed that
connection, schema, worksheet, result, history, and access surfaces need to be
visible from the beginning so later execution work has the right product shape.

### Task F3: i18n

Add English and Chinese copy.

Required concepts:

```text
Query Workbench
Query execution is not enabled
Read-only credentials are required
Connection metadata is incomplete
Unsupported engine
SQL target
Redis command target
Mongo query target
Run locked
Explain locked
Result grid locked
Access request planned
Query history unavailable
```

### Task F4: Tests

Add/modify tests:

```text
service test for getQueryTargets
component/page test for /query
filter tests
disabled execution boundary test
locked action bar test
schema/history/access placeholder tests
```

E2E:

```text
add /query smoke to existing workflow only after backend endpoint exists
assert page loads with real backend data
assert no enabled Run/Execute action exists
```

### Task F5: Verification

Run:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

After backend and frontend are merged, run manual cross-repo E2E:

```bash
gh workflow run "Frontend CI" --ref main -f run_e2e=true
```

## Merge Strategy

Backend first:

```text
merge backend Phase 36
push backend main
Backend CI must pass
```

Frontend second:

```text
merge frontend Phase 36
push frontend main
Frontend fast CI must pass
manual cross-repo E2E should pass
```

## Scope Confirmation Required In Final Reports

Backend:

```text
no query execution
no credentials
no SQL execution
no Redis/Mongo execution
no SQL migrations unless explicitly approved
no write operations
OpenAPI updated
tests added
```

Frontend:

```text
no query execution UI
no enabled Run button
no credentials in browser
no mocked backend in final E2E
disabled boundary visible
tests added
```
