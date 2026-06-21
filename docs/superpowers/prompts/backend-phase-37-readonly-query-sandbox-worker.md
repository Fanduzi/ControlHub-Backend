# Backend Phase 37 Read-only Query Sandbox Worker Prompt

You are implementing the backend side of Phase 37 for ControlHub.

Backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repository is separate and must not be edited by this worker:

```text
/Users/fan/JsProjects/ControlHub
```

## Objective

Build the first backend-enforced query execution path for the Query Workbench:

```text
MySQL / TiDB SELECT only
```

This phase turns Phase 36 query targets into executable targets only when the
backend has explicit credential metadata and policy approval. Execution must be
guarded by backend-owned auth, SQL parsing, credential resolution, timeout, row
caps, payload caps, audit, and history.

The frontend will be implemented later. Do not edit frontend files in this
worker.

## Required Reading

Read these documents first:

```text
docs/superpowers/specs/2026-06-21-phase-37-read-only-query-sandbox.md
docs/superpowers/plans/2026-06-21-phase-37-read-only-query-sandbox.md
docs/decisions/2026-06-21-phase-37-readonly-query-sandbox-boundary.md
docs/superpowers/specs/2026-06-20-phase-36-query-workbench-foundation.md
docs/decisions/2026-06-21-query-workbench-phase-36-boundary.md
docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

Read these backend files before editing related areas:

```text
internal/model/query_target.go
internal/model/taxonomy.go
internal/model/README.md
internal/service/auth_service.go
internal/service/query_target_service.go
internal/repository/mysql/query_target_repository.go
internal/api/query_target_handler.go
internal/api/router.go
cmd/server/main.go
internal/api/test_server.go
internal/integration/openapi_fuzz_test.go
internal/integration/testenv_test.go
internal/openapi/openapi.yaml
```

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-37-readonly-query-sandbox -b phase-37-readonly-query-sandbox main
cd .worktrees/backend-phase-37-readonly-query-sandbox
git status --short --branch
```

Expected: clean branch `phase-37-readonly-query-sandbox`.

Do not edit the main worktree directly.

## Scope

Allowed:

```text
backend auth verification for query routes
query execution models
MySQL/TiDB SELECT SQL guard
credential metadata and execution history migration
query execution repository/service/API
GET /query-targets readiness update
OpenAPI contract
unit / handler / integration / fuzz tests
minimal docs evidence if needed
```

Not allowed:

```text
frontend repo edits
PostgreSQL execution
ClickHouse execution
Redis command execution
MongoDB execution
write statements
multi-statement execution
export
saved queries
approval workflows
AI query assistance
plaintext credentials in DB/API/logs/browser
new credential write API
tag / release / deploy
```

## Implementation Plan

Follow the implementation plan task-by-task:

```text
docs/superpowers/plans/2026-06-21-phase-37-read-only-query-sandbox.md
```

Implement backend tasks B0 through B7 only:

```text
B0 Add Authenticated Actor Extraction For Query Routes
B1 Add Query Execution Models
B2 Add SQL Guard With TDD
B3 Add Migration For Credential Metadata And History
B4 Implement Repository Layer
B5 Implement Query Execution Service
B6 Add API Handlers And OpenAPI
B7 Add End-to-End Backend Integration Tests
```

Do not implement frontend tasks F1-F4 in this worker.

## Non-Negotiable Backend Boundaries

### Auth and actor

- Execute/history endpoints require `Authorization: Bearer <token>`.
- Derive `actorUserId` from the verified token on the server.
- Never accept `actorUserId` in request JSON or query parameters.
- `VerifyToken` verifies token structure, signature, user ID, and returns
  `IssuedAt`; it does not enforce TTL.
- Query routes use `requireFreshQueryActor`, a chi middleware factory:

```go
func requireFreshQueryActor(authService *service.AuthService, cfg QueryExecutionAuthConfig) func(http.Handler) http.Handler
```

- `QueryExecutionAuthConfig.TokenMaxAge` must be positive. Zero/unset fails
  closed.
- Suggested default: `QueryExecutionTokenMaxAge = 8h` or shorter.
- Existing non-query read/list route auth behavior must not change.

### Query target readiness

Update `GET /query-targets` so a target is executable only when all are true:

```text
engine is mysql or tidb
connection metadata is complete
enabled credential metadata exists
credential_ref resolves to a DSN
environment_policy allows the target environment
```

Ready target fields:

```json
{
  "readiness": "ready",
  "governance": {
    "executionEnabled": true,
    "credentialState": "configured_readonly_credential",
    "auditRequired": true,
    "safetyState": "readonly_sandbox_enabled"
  },
  "availableActions": {
    "run": true,
    "explain": false,
    "export": false,
    "saveSheet": false,
    "requestAccess": false
  }
}
```

All other targets remain visible and non-executable.

### `safetyState` enum chain

`readonly_sandbox_enabled` must be declared before any ready target is returned.

Update:

```text
internal/model/query_target.go
internal/model/README.md
internal/openapi/openapi.yaml
```

Add:

```text
SafetyStateReadonlySandboxEnabled
QueryTargetSafetyStateDictionary()
QueryTargetSafetyState.Validate()
model tests for all known states + unknown rejection
OpenAPI enum value
service/handler/integration tests for ready target safetyState
```

Follow the existing `taxonomy.go` dictionary/Validate pattern. Keep the change
surgical; do not relocate `QueryTargetSafetyState`.

### Credential metadata

Add metadata table:

```text
query_target_credentials
```

Fields:

```text
id
resource_id
engine
credential_ref
enabled
environment_policy
created_at
updated_at
```

Phase 37 has no credential write API. Credential rows come from migration/seed
or test fixtures only.

`credential_ref` rules:

```text
matches [A-Z0-9_]+
max length 64
invalid ref fails closed on read/resolve
never perform env lookup with an unvalidated ref
never return or log DSN/password
```

Environment policy enum:

```text
disabled
non_prod_only
all_environments
```

Rules:

```text
production target + non_prod_only -> locked
production target + all_environments -> ready
non-production target + non_prod_only -> ready
disabled -> locked
unknown/empty -> locked
```

### SQL guard

Use a parser. Recommended:

```text
vitess.io/vitess/go/vt/sqlparser
```

Guard must reject:

```text
empty input
multi-statement input
non-SELECT statements
INSERT / UPDATE / DELETE / REPLACE
DDL
CALL
SET
USE
LOAD
transaction control
SELECT ... INTO OUTFILE
SELECT ... INTO DUMPFILE
FOR UPDATE
LOCK IN SHARE MODE
SLEEP(...)
BENCHMARK(...)
GET_LOCK(...)
RELEASE_LOCK(...)
IS_FREE_LOCK(...)
IS_USED_LOCK(...)
LOAD_FILE(...)
user-variable assignment such as @var := ... or SELECT ... INTO @var
```

Function/clause rejection must be implemented by AST traversal, not substring
matching. If Vitess cannot reliably expose the needed nodes, stop and run a
parser spike/verification before claiming the guard is complete.

Limits:

```text
Default max rows: 100
Hard max rows: 500
Timeout: 5 seconds
Production timeout: 3 seconds
Production hard max rows: 100
Max columns: 100
Max cell bytes: 8192
Max response bytes: 1 MiB
```

### Execute and history API

Add:

```text
POST /query-targets/{id}/execute
GET /query-targets/{id}/executions?page=1&pageSize=20
```

Execution request:

```json
{
  "statement": "select 1 as value",
  "maxRows": 100
}
```

Do not accept actor IDs from client input.

Error mapping:

```text
ErrQueryValidationFailed -> 400 validation_failed
ErrQueryNotAllowed -> 403 query_not_allowed
ErrQueryTargetNotFound -> 404 query_target_not_found
ErrQueryTimeout -> 408 query_timeout
ErrQueryBackendFailure -> 502 query_backend_error
unknown -> 500 internal_error
```

No guard, policy, credential, or target-database validation issue should become
a 500.

### OpenAPI and fuzz

Update OpenAPI for:

```text
QueryExecuteRequest
QueryExecuteResponse
QueryResultColumn
QueryExecutionRecord
QueryExecutionListResponse
POST /query-targets/{id}/execute
GET /query-targets/{id}/executions
Bearer security scheme
401 responses
```

Schemathesis/fuzz must not produce an unclassified auth failure. Pick and
document one strategy:

```text
supply a valid test bearer token for protected operations
or document unauthenticated 401 as a conformance pass
```

Fuzz must not find 5xx on blocked or malformed statements.

## TDD Requirements

Use test-first discipline for meaningful behavior:

```text
auth verification and fresh-query middleware
query execution model validators
SQL guard
repository credential metadata/history
service policy and execution behavior
handler status/error mapping
integration execution flow
OpenAPI/fuzz contract
```

Minimum required tests are listed in the Phase 37 implementation plan. Do not
skip or delete existing tests.

## Required Verification

Backend local gates:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Before committing, also run:

```bash
git diff --check
```

If Docker is unavailable, say so explicitly and do not claim Docker-backed gates
passed.

If `make test-openapi-fuzz` produces warnings, classify them. Do not hide,
suppress, or broaden-ignore warnings without evidence.

## GitNexus / Impact Analysis

This repo uses GitNexus.

Before editing any non-trivial symbol, run impact analysis according to
`AGENTS.md`.

Before committing, run change detection if available:

```bash
npx gitnexus detect-changes --scope all
```

If GitNexus reports a stale index, run:

```bash
npx gitnexus analyze
```

If GitNexus is unavailable in the worktree, say so explicitly and rely on normal
diff/test evidence.

## Commit Strategy

Prefer small commits matching the plan tasks:

```text
feat: add authenticated query actor extraction
feat: add query execution models
feat: add read-only query guard
feat: add query execution persistence
feat: add query execution service
feat: add query execution API
test: cover read-only query sandbox integration
```

Use the repo's existing commit style. Do not add AI co-author.

Do not push unless explicitly instructed.

## Final Report Required

Report:

```text
worktree and branch
commit list
files changed
API paths added
migration name
credential model summary
SQL guard summary
auth/TTL behavior
query target readiness behavior
OpenAPI/fuzz status
unit/handler/integration test counts
verification matrix with exact commands/results
GitNexus impact/detect summary
final git status
```

Scope confirmation must include:

```text
only MySQL/TiDB SELECT execution
no writes
no multi-statement execution
no plaintext credentials in DB/API/logs/browser
no credential write API
audit/history written for every attempt
OpenAPI updated
integration and fuzz gates passed or explicitly not run with reason
no frontend repo changes
no export
no saved queries
no tag/release/deploy
no push unless explicitly instructed
no AI co-author
```
