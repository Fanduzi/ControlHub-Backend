# Backend Phase 36A Query Targets Worker Prompt

You are implementing the backend side of Phase 36 for ControlHub.

Backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repository is separate and must not be edited by this worker:

```text
/Users/fan/JsProjects/ControlHub
```

## Objective

Add the backend read model and API contract that turn existing database
resources into query-capable targets for a future Query Workbench.

This phase does **not** execute queries. It only exposes target capability,
connection completeness, missing configuration, and safety state.

## Required Reading

```text
docs/superpowers/specs/2026-06-20-query-workbench-roadmap.md
docs/superpowers/specs/2026-06-20-phase-36-query-workbench-foundation.md
docs/superpowers/plans/2026-06-20-phase-36-query-workbench-foundation.md
docs/superpowers/notes/2026-06-20-phase-36-bytebase-ui-research.md
```

Code context:

```text
internal/model/resource.go
internal/model/resource_write.go
internal/repository/mysql/resource_repository.go
internal/service/resource_service.go
internal/api/resource_handler.go
internal/api/server.go
internal/openapi/openapi.yaml
internal/integration/resource_test.go
```

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-36a-query-targets -b phase-36a-query-targets main
cd .worktrees/backend-phase-36a-query-targets
git status --short --branch
```

Expected: clean branch `phase-36a-query-targets`.

Do not edit the main worktree directly.

## Scope

Allowed:

```text
backend read model
backend service/repository read path
GET /query-targets API
OpenAPI contract
unit / handler / integration tests
docs evidence if needed
```

Not allowed:

```text
query execution
SQL execution
Redis command execution
Mongo query execution
credential storage
connection pooling
new write APIs
SQL migrations unless explicitly approved
frontend repo edits
tag / release / deploy
```

## Required Product Contract

Add:

```text
GET /query-targets
```

Initial response shape:

```json
{
  "items": [
    {
      "resourceId": 22,
      "resourceName": "analytics-ch-node-01-prod",
      "displayName": "Analytics ClickHouse Node 01 Production",
      "resourceType": "database_instance",
      "environment": "Production",
      "owner": "DBA Team",
      "engine": "clickhouse",
      "queryKind": "sql",
      "host": "prod-ch-host-01.internal",
      "port": 8123,
      "clusterId": 14,
      "clusterName": "Analytics ClickHouse Cluster Production",
      "readiness": "credential_required",
      "missingFields": ["readonlyCredential"],
      "safetyState": "credential_missing",
      "safetyNote": "Query execution is not enabled in this phase."
    }
  ]
}
```

If existing project conventions use a different envelope, follow the local API
style and document the exact shape in OpenAPI and final report.

## Derivation Rules

### Query kind

```text
mysql      -> sql
tidb       -> sql
postgresql -> sql
clickhouse -> sql
redis      -> redis
mongodb    -> mongo
unknown    -> unsupported
```

Unknown engines must remain visible as unsupported targets.

### Readiness

Use explicit values:

```text
ready
missing_connection
credential_required
unsupported_engine
disabled
```

Phase 36 should normally return `credential_required`, not `ready`, because
read-only credential metadata does not exist yet. Only return `ready` if there
is concrete existing backend evidence of a read-only credential.

### Missing fields

Examples:

```text
host
port
engine
readonlyCredential
```

### Safety state

Use explicit values:

```text
credential_missing
execution_disabled
unsupported_engine
connection_incomplete
```

The safety note must make the boundary clear:

```text
Query execution is not enabled in this phase.
```

## Implementation Requirements

1. Add model types:

```text
QueryTarget
QueryKind
QueryTargetReadiness
QueryTargetSafetyState
QueryTargetListResponse
```

2. Add a pure derivation helper:

```text
engine + host + port + credential evidence -> query kind + readiness + safety
```

3. Source targets from existing database instance resources and database
profiles. Include cluster relation if available through existing `member_of`
relations.

4. Do not add credentials. Represent missing credentials as
`credential_required`.

5. Add handler route and OpenAPI schema.

6. Keep the endpoint read-only. It must not mutate resources or topology.

## GitNexus Requirements

Before editing any Go symbol, run impact analysis for that symbol and report the
blast radius in the final report.

Before committing, run:

```bash
npx gitnexus detect-changes --scope all
```

If the GitNexus index is stale, run:

```bash
npx gitnexus analyze
```

## Tests

Add tests for:

```text
engine -> query kind mapping
readiness derivation
missing host
missing port
unknown engine
complete connection but no readonly credential -> credential_required
GET /query-targets handler response
OpenAPI validation
integration coverage if repository joins MySQL data
```

Expected target examples from seed data should include at least:

```text
ClickHouse database instance
MySQL/TiDB/PostgreSQL-like SQL target if present
Redis target if present
MongoDB target if present
unsupported/unknown path if feasible through unit test
```

## Verification

Run:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
npx gitnexus detect-changes --scope all
```

If Docker is unavailable for integration/fuzz gates, state that explicitly and
do not claim those gates passed.

## Commit

Commit only after verification is complete.

Commit message suggestion:

```text
feat: add query target read model
```

Do not include `Co-Authored-By`.

Do not push, tag, release, or deploy unless explicitly instructed.

## Final Report Requirements

Report:

```text
worktree / branch / commit
files changed
API shape
query kind and readiness rules
seed targets observed
verification matrix
GitNexus detect_changes summary
scope confirmation
```

Scope confirmation must include:

```text
no query execution
no credentials
no SQL execution
no Redis/Mongo execution
no SQL migrations unless explicitly approved
no write operations
OpenAPI updated
tests added
no tag / release / deploy / push
no AI co-author
```
