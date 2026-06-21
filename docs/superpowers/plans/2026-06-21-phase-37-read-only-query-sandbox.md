# Phase 37 Read-only Query Sandbox Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a backend-enforced MySQL/TiDB read-only SELECT sandbox and connect the existing `/query` workbench to it without enabling writes, export, or multi-engine execution.

**Architecture:** Extend Phase 36 query targets with explicit execution readiness, add a narrowly-scoped execute API, and persist query execution history. Enforcement lives in backend service code: credential resolution, SQL parsing, statement guard, timeout, row cap, audit, and result mapping. The frontend only enables Run when the backend exposes `availableActions.run=true`.

**Tech Stack:** Go, `database/sql`, `github.com/go-sql-driver/mysql`, Vitess SQL parser, MySQL migrations, OpenAPI 3.1, Next.js, React, TypeScript, Vitest, Playwright cross-repo E2E.

---

## Required Reading

Backend:

```text
docs/superpowers/specs/2026-06-21-phase-37-read-only-query-sandbox.md
docs/superpowers/specs/2026-06-20-phase-36-query-workbench-foundation.md
docs/decisions/2026-06-21-query-workbench-phase-36-boundary.md
internal/model/query_target.go
internal/service/query_target_service.go
internal/repository/mysql/query_target_repository.go
internal/api/query_target_handler.go
internal/api/router.go
cmd/server/main.go
internal/openapi/openapi.yaml
internal/integration/testenv_test.go
```

Frontend:

```text
/Users/fan/JsProjects/ControlHub/app/(console)/query/page.tsx
/Users/fan/JsProjects/ControlHub/components/query/query-workbench.tsx
/Users/fan/JsProjects/ControlHub/components/query/query-editor-shell.tsx
/Users/fan/JsProjects/ControlHub/components/query/query-governance-panel.tsx
/Users/fan/JsProjects/ControlHub/services/query-targets.ts
/Users/fan/JsProjects/ControlHub/types/query-target.ts
/Users/fan/JsProjects/ControlHub/e2e/query-workbench.spec.ts
```

## Worktree Strategy

Backend first, frontend second.

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree add .worktrees/backend-phase-37-readonly-query-sandbox -b phase-37-readonly-query-sandbox main
```

After backend merges and pushes:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree add .worktrees/frontend-phase-37-readonly-query-sandbox -b feat/phase-37-readonly-query-sandbox main
```

## File Structure

Backend create:

```text
internal/model/query_execution.go
internal/service/query_guard.go
internal/service/query_guard_test.go
internal/service/query_execution_service.go
internal/service/query_execution_service_test.go
internal/repository/mysql/query_execution_repository.go
internal/api/query_execution_handler.go
internal/api/query_execution_handler_test.go
internal/integration/query_execution_test.go
migrations/00011_query_execution.sql
```

Backend modify:

```text
go.mod
internal/service/auth_service.go
internal/model/query_target.go
internal/service/query_target_service.go
internal/repository/mysql/query_target_repository.go
internal/api/auth_middleware.go
internal/api/router.go
cmd/server/main.go
internal/api/test_server.go
internal/integration/openapi_fuzz_test.go
internal/openapi/openapi.yaml
```

Frontend create or modify:

```text
/Users/fan/JsProjects/ControlHub/services/query-executions.ts
/Users/fan/JsProjects/ControlHub/types/query-execution.ts
/Users/fan/JsProjects/ControlHub/components/query/query-editor-shell.tsx
/Users/fan/JsProjects/ControlHub/components/query/query-workbench.tsx
/Users/fan/JsProjects/ControlHub/components/query/query-history-panel.tsx
/Users/fan/JsProjects/ControlHub/tests/services/query-executions.test.ts
/Users/fan/JsProjects/ControlHub/tests/components/query-workbench.test.tsx
/Users/fan/JsProjects/ControlHub/e2e/query-workbench.spec.ts
```

## Backend Tasks

### Task B0: Add Authenticated Actor Extraction For Query Routes

**Files:**
- Modify: `internal/service/auth_service.go`
- Modify: `internal/service/auth_service_test.go`
- Create: `internal/api/auth_middleware.go`
- Create: `internal/api/auth_middleware_test.go`

- [ ] **Step 1: Write auth verification tests**

Add service tests:

```text
VerifyTokenReturnsUserIDAndRoleForIssuedToken
VerifyTokenRejectsMalformedToken
VerifyTokenRejectsBadSignature
```

Add API middleware tests:

```text
AuthenticatedActorRejectsMissingBearerToken
AuthenticatedActorRejectsInvalidBearerToken
AuthenticatedActorStoresActorInContext
```

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
go test -count=1 ./internal/service -run VerifyToken
go test -count=1 ./internal/api -run AuthenticatedActor
```

Expected: FAIL because verification and middleware do not exist.

- [ ] **Step 3: Implement token verification**

Add:

```go
type AuthenticatedUser struct {
	ID   uint64
	Role string
}

var ErrInvalidToken = errors.New("invalid token")

func (s *AuthService) VerifyToken(token string) (*AuthenticatedUser, error)
```

Rules:

- decode the existing base64 token format
- require exactly `userID:role:issuedAt:signature`
- recompute HMAC with the configured signing key
- reject bad signatures
- reject non-positive user IDs
- keep token TTL out of scope unless existing login behavior already defines it

- [ ] **Step 4: Implement route middleware helper**

Create:

```go
func requireAuthenticatedActor(authService *service.AuthService, next http.Handler) http.Handler
func actorUserIDFromContext(ctx context.Context) (uint64, bool)
```

Only apply this middleware to query execution/history routes in Task B6. Do not
change the authentication behavior of existing read/list routes in this task.

- [ ] **Step 5: Run auth tests**

Run:

```bash
go test -count=1 ./internal/service -run VerifyToken
go test -count=1 ./internal/api -run AuthenticatedActor
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/auth_service.go internal/service/auth_service_test.go internal/api/auth_middleware.go internal/api/auth_middleware_test.go
git commit -m "feat: add authenticated actor extraction"
```

### Task B1: Add Query Execution Models

**Files:**
- Create: `internal/model/query_execution.go`
- Test: `go test -count=1 ./internal/model`

- [ ] **Step 1: Create model types**

Add:

```go
// Package model provides domain entities for the resource management system.
// input: time package
// output: QueryExecution*, QueryResult* types and status/error enums
// pos: Query sandbox execution requests, responses, and history records
// note: if this file changes, update header and README.md
package model

import "time"

type QueryExecutionStatus string

const (
	QueryExecutionSuccess  QueryExecutionStatus = "success"
	QueryExecutionRejected QueryExecutionStatus = "rejected"
	QueryExecutionFailed   QueryExecutionStatus = "failed"
	QueryExecutionTimeout  QueryExecutionStatus = "timeout"
)

type QueryExecuteRequest struct {
	Statement string `json:"statement"`
	MaxRows   int    `json:"maxRows,omitempty"`
}

type QueryResultColumn struct {
	Name         string `json:"name"`
	DatabaseType string `json:"databaseType"`
	Nullable     bool   `json:"nullable"`
}

type QueryExecuteResponse struct {
	ExecutionID      uint64               `json:"executionId"`
	Status           QueryExecutionStatus `json:"status"`
	TargetResourceID uint64               `json:"targetResourceId"`
	Engine           string               `json:"engine"`
	Columns          []QueryResultColumn  `json:"columns"`
	Rows             [][]any              `json:"rows"`
	RowCount         int                  `json:"rowCount"`
	Truncated        bool                 `json:"truncated"`
	DurationMs       int64                `json:"durationMs"`
	LimitApplied     int                  `json:"limitApplied"`
	ExecutedAt       time.Time            `json:"executedAt"`
}

type QueryExecutionRecord struct {
	ID               uint64               `json:"id"`
	TargetResourceID uint64               `json:"targetResourceId"`
	ActorUserID      uint64               `json:"actorUserId"`
	Engine           string               `json:"engine"`
	StatementDigest  string               `json:"statementDigest"`
	StatementPreview string               `json:"statementPreview"`
	Status           QueryExecutionStatus `json:"status"`
	RowCount         int                  `json:"rowCount"`
	DurationMs       int64                `json:"durationMs"`
	ErrorCode        string               `json:"errorCode"`
	ErrorMessage     string               `json:"errorMessage"`
	CreatedAt        time.Time            `json:"createdAt"`
}

type QueryExecutionListQuery struct {
	TargetResourceID uint64
	Page             int
	PageSize         int
}

type QueryExecutionListResponse struct {
	Items    []QueryExecutionRecord `json:"items"`
	PageInfo *PageInfo              `json:"pageInfo"`
}

type QueryCredentialMetadata struct {
	ID                uint64 `json:"id"`
	ResourceID        uint64 `json:"resourceId"`
	Engine            string `json:"engine"`
	CredentialRef     string `json:"credentialRef"`
	Enabled           bool   `json:"enabled"`
	EnvironmentPolicy string `json:"environmentPolicy"`
}
```

- [ ] **Step 2: Run compile check**

Run:

```bash
go test -count=1 ./internal/model
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/model/query_execution.go
git commit -m "feat: add query execution models"
```

### Task B2: Add SQL Guard With TDD

**Files:**
- Create: `internal/service/query_guard.go`
- Create: `internal/service/query_guard_test.go`
- Modify: `go.mod`

- [ ] **Step 1: Add parser dependency**

Run:

```bash
go get vitess.io/vitess/go/vt/sqlparser
```

If this pulls a large dependency tree, report the diff before continuing.

- [ ] **Step 2: Write guard tests**

Create `internal/service/query_guard_test.go` with tests named:

```text
TestQueryGuardAllowsSimpleSelect
TestQueryGuardRejectsWriteStatements
TestQueryGuardRejectsDDLAndAdminStatements
TestQueryGuardRejectsMultiStatements
TestQueryGuardAppliesDefaultLimit
TestQueryGuardCapsLargeLimit
TestQueryGuardRejectsSelectIntoOutfile
TestQueryGuardRejectsLockingSelect
```

Each test should assert the reason, not only the output string. Example:

```go
func TestQueryGuardRejectsWriteStatements(t *testing.T) {
	guard := NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	for _, stmt := range []string{
		"insert into users(id) values (1)",
		"update users set name = 'x'",
		"delete from users",
		"replace into users(id) values (1)",
	} {
		t.Run(stmt, func(t *testing.T) {
			_, err := guard.Guard(stmt, 100)
			if !errors.Is(err, ErrQueryStatementNotAllowed) {
				t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementNotAllowed", stmt, err)
			}
		})
	}
}
```

- [ ] **Step 3: Run tests to verify RED**

Run:

```bash
go test -count=1 ./internal/service -run QueryGuard
```

Expected: FAIL because guard does not exist.

- [ ] **Step 4: Implement guard**

Create `internal/service/query_guard.go` with:

```go
type QueryGuardConfig struct {
	DefaultMaxRows int
	HardMaxRows    int
}

type GuardedQuery struct {
	OriginalStatement string
	ExecutableSQL     string
	LimitApplied      int
	StatementDigest   string
	StatementPreview  string
}

var (
	ErrQueryStatementEmpty      = errors.New("query statement is empty")
	ErrQueryStatementNotAllowed = errors.New("only a single SELECT statement is allowed")
	ErrQueryLimitInvalid        = errors.New("query maxRows must be positive")
)

type QueryGuard struct {
	config QueryGuardConfig
	parser *sqlparser.Parser
}

func NewQueryGuard(config QueryGuardConfig) *QueryGuard
func (g *QueryGuard) Guard(statement string, requestedMaxRows int) (GuardedQuery, error)
```

Implementation rules:

- trim whitespace
- reject empty input
- reject semicolon-separated multi-statements
- parse exactly one statement
- allow only `*sqlparser.Select`
- reject `SELECT ... INTO OUTFILE`
- reject locking clauses
- apply/cap `LIMIT`
- produce a digest and 512-character preview

- [ ] **Step 5: Run guard tests**

Run:

```bash
go test -count=1 ./internal/service -run QueryGuard
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/service/query_guard.go internal/service/query_guard_test.go
git commit -m "feat: add read-only query guard"
```

### Task B3: Add Migration For Credential Metadata And History

**Files:**
- Create: `migrations/00011_query_execution.sql`
- Test: `make test-integration`

- [ ] **Step 1: Add migration**

Create:

```sql
-- +goose Up
CREATE TABLE query_target_credentials (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  resource_id BIGINT UNSIGNED NOT NULL,
  engine VARCHAR(32) NOT NULL,
  credential_ref VARCHAR(128) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT FALSE,
  environment_policy VARCHAR(32) NOT NULL DEFAULT 'non_prod_only',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  UNIQUE KEY uq_query_target_credentials_resource (resource_id),
  CONSTRAINT fk_query_target_credentials_resource
    FOREIGN KEY (resource_id) REFERENCES resources(id)
    ON DELETE CASCADE
);

CREATE TABLE query_executions (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
  target_resource_id BIGINT UNSIGNED NOT NULL,
  actor_user_id BIGINT UNSIGNED NOT NULL,
  engine VARCHAR(32) NOT NULL,
  statement_digest VARCHAR(512) NOT NULL,
  statement_preview VARCHAR(512) NOT NULL,
  status VARCHAR(32) NOT NULL,
  row_count INT NOT NULL DEFAULT 0,
  duration_ms INT NOT NULL DEFAULT 0,
  error_code VARCHAR(64) NOT NULL DEFAULT '',
  error_message VARCHAR(512) NOT NULL DEFAULT '',
  created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
  PRIMARY KEY (id),
  KEY idx_query_executions_target_created (target_resource_id, created_at),
  KEY idx_query_executions_actor_created (actor_user_id, created_at),
  CONSTRAINT fk_query_executions_target_resource
    FOREIGN KEY (target_resource_id) REFERENCES resources(id)
    ON DELETE CASCADE,
  CONSTRAINT fk_query_executions_actor_user
    FOREIGN KEY (actor_user_id) REFERENCES users(id)
    ON DELETE RESTRICT
);

-- +goose Down
DROP TABLE IF EXISTS query_executions;
DROP TABLE IF EXISTS query_target_credentials;
```

- [ ] **Step 2: Run integration migration smoke**

Run:

```bash
make test-integration
```

Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add migrations/00011_query_execution.sql
git commit -m "feat: add query execution persistence"
```

### Task B4: Implement Repository Layer

**Files:**
- Create: `internal/repository/mysql/query_execution_repository.go`
- Create/modify tests in `internal/integration/query_execution_test.go`

- [ ] **Step 1: Write integration tests first**

Add tests for:

```text
InsertQueryExecutionAndListByTarget
CredentialMetadataMarksTargetReady
DisabledCredentialKeepsTargetLocked
```

The tests should create resources and credential metadata in the disposable
MySQL database. Do not rely on fuzz-mutated seed state.

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
go test -count=1 -tags=integration ./internal/integration -run QueryExecution
```

Expected: FAIL because repository does not exist.

- [ ] **Step 3: Implement repository**

Create methods:

```go
type QueryExecutionRepository struct { db *sql.DB }

func NewQueryExecutionRepository(db *sql.DB) *QueryExecutionRepository
func (r *QueryExecutionRepository) GetCredentialByResourceID(ctx context.Context, resourceID uint64) (QueryCredentialMetadata, error)
func (r *QueryExecutionRepository) InsertExecution(ctx context.Context, rec model.QueryExecutionRecord) (uint64, error)
func (r *QueryExecutionRepository) ListExecutions(ctx context.Context, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, int, error)
func (r *QueryExecutionRepository) InsertAuditEvent(ctx context.Context, actorUserID uint64, targetResourceID uint64, eventType string, result string) error
```

Return domain-level not-found errors through the service layer; do not expose
SQL errors to handlers.

- [ ] **Step 4: Run integration tests**

Run:

```bash
go test -count=1 -tags=integration ./internal/integration -run QueryExecution
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/mysql/query_execution_repository.go internal/integration/query_execution_test.go
git commit -m "feat: persist query execution history"
```

### Task B5: Implement Query Execution Service

**Files:**
- Create: `internal/service/query_execution_service.go`
- Create: `internal/service/query_execution_service_test.go`
- Modify: `internal/service/query_target_service.go`

- [ ] **Step 1: Write service tests**

Cover:

```text
RejectsUnsupportedTarget
RejectsMissingCredential
RejectsUnresolvableCredentialRef
RejectsUnsafeStatement
ExecutesSelectWithLimit
RecordsRejectedAttempt
RecordsSuccessfulAttempt
MapsTimeoutToQueryTimeout
```

Use fakes for repository and DB executor. Do not connect to real databases in
unit tests.

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
go test -count=1 ./internal/service -run QueryExecution
```

Expected: FAIL.

- [ ] **Step 3: Implement service**

Add:

```go
type QueryExecutionService struct {
	targets     QueryTargetRepository
	executions  QueryExecutionRepository
	credentials QueryCredentialResolver
	executor    QueryDatabaseExecutor
	guard       *QueryGuard
	clock       Clock
}

type QueryExecutionRepository interface {
	GetCredentialByResourceID(ctx context.Context, resourceID uint64) (model.QueryCredentialMetadata, error)
	InsertExecution(ctx context.Context, rec model.QueryExecutionRecord) (uint64, error)
	ListExecutions(ctx context.Context, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, int, error)
	InsertAuditEvent(ctx context.Context, actorUserID uint64, targetResourceID uint64, eventType string, result string) error
}

type QueryCredentialResolver interface {
	Resolve(ctx context.Context, credentialRef string) (dsn string, err error)
}

type QueryDatabaseExecutor interface {
	Query(ctx context.Context, dsn string, guarded GuardedQuery) (QueryDatabaseResult, error)
}

type QueryDatabaseResult struct {
	Columns   []model.QueryResultColumn
	Rows      [][]any
	RowCount  int
	Truncated bool
}

type Clock interface {
	Now() time.Time
}

func NewQueryExecutionService(
	targets QueryTargetRepository,
	executions QueryExecutionRepository,
	credentials QueryCredentialResolver,
	executor QueryDatabaseExecutor,
	guard *QueryGuard,
	clock Clock,
) *QueryExecutionService
func (s *QueryExecutionService) Execute(ctx context.Context, actorUserID uint64, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error)
func (s *QueryExecutionService) ListHistory(ctx context.Context, targetID uint64, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, *model.PageInfo, error)
```

Use sentinel errors:

```go
ErrQueryTargetNotFound
ErrQueryNotAllowed
ErrQueryValidationFailed
ErrQueryTimeout
ErrQueryBackendFailure
```

- [ ] **Step 4: Run service tests**

Run:

```bash
go test -count=1 ./internal/service -run 'QueryExecution|QueryGuard|QueryTarget'
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/query_execution_service.go internal/service/query_execution_service_test.go internal/service/query_target_service.go
git commit -m "feat: add query execution service"
```

### Task B6: Add API Handlers And OpenAPI

**Files:**
- Create: `internal/api/query_execution_handler.go`
- Create: `internal/api/query_execution_handler_test.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/server/main.go`
- Modify: `internal/api/test_server.go`
- Modify: `internal/integration/openapi_fuzz_test.go`
- Modify: `internal/openapi/openapi.yaml`

- [ ] **Step 1: Write handler tests**

Cover:

```text
POST /query-targets/{id}/execute success
POST invalid id returns 400
POST unsafe statement returns 400
POST disabled target returns 403
GET /query-targets/{id}/executions returns paginated history
```

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
go test -count=1 ./internal/api -run QueryExecution
```

Expected: FAIL.

- [ ] **Step 3: Implement handlers and wiring**

Routes:

```go
router.Group(func(r chi.Router) {
	r.Use(func(next http.Handler) http.Handler {
		return requireAuthenticatedActor(deps.AuthService, next)
	})
	r.Post("/query-targets/{id}/execute", handleExecuteQuery(deps.QueryExecutionService))
	r.Get("/query-targets/{id}/executions", handleListQueryExecutions(deps.QueryExecutionService))
})
```

Handlers must read the actor with `actorUserIDFromContext(r.Context())`; they
must not accept actor IDs in request JSON or query parameters.

Error mapping:

```text
ErrQueryValidationFailed -> 400 validation_failed
ErrQueryNotAllowed -> 403 query_not_allowed
ErrQueryTargetNotFound -> 404 query_target_not_found
ErrQueryTimeout -> 408 query_timeout
ErrQueryBackendFailure -> 502 query_backend_error
unknown -> 500 internal_error
```

- [ ] **Step 4: Update OpenAPI**

Add schemas:

```text
QueryExecuteRequest
QueryExecuteResponse
QueryResultColumn
QueryExecutionRecord
QueryExecutionListResponse
```

Add paths:

```text
POST /query-targets/{id}/execute
GET /query-targets/{id}/executions
```

Examples:

```text
accepted SELECT
rejected UPDATE
history response
```

- [ ] **Step 5: Run handler and OpenAPI validation**

Run:

```bash
go test -count=1 ./internal/api -run QueryExecution
make openapi-validate
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/api/query_execution_handler.go internal/api/query_execution_handler_test.go internal/api/router.go cmd/server/main.go internal/api/test_server.go internal/integration/openapi_fuzz_test.go internal/openapi/openapi.yaml
git commit -m "feat: add query execution API"
```

### Task B7: Add End-to-End Backend Integration Tests

**Files:**
- Modify: `internal/integration/query_execution_test.go`

- [ ] **Step 1: Add execution tests against disposable MySQL**

Add tests:

```text
TestQueryExecution_SelectOneReturnsRows
TestQueryExecution_BlockedWriteDoesNotMutate
TestQueryExecution_MultiStatementRejected
TestQueryExecution_LimitCapsRows
TestQueryExecution_HistoryWrittenForSuccessAndRejection
TestQueryExecution_AuditEventWrittenForSuccessAndRejection
```

Use a self-contained fixture table inside the disposable DB. Do not query
ControlHub production-like seed tables as the target database unless the test
owns the data.

- [ ] **Step 2: Run targeted integration tests**

Run:

```bash
go test -count=1 -tags=integration ./internal/integration -run QueryExecution
```

Expected: PASS.

- [ ] **Step 3: Run backend gates**

Run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Expected: PASS. Fuzz may show accepted Phase 34C validation mismatch warnings,
but must not show 5xx or configured check failures.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/query_execution_test.go
git commit -m "test: cover read-only query sandbox integration"
```

## Frontend Tasks

### Task F1: Add Types And Service

**Files:**
- Create: `/Users/fan/JsProjects/ControlHub/types/query-execution.ts`
- Create: `/Users/fan/JsProjects/ControlHub/services/query-executions.ts`
- Test: `/Users/fan/JsProjects/ControlHub/tests/services/query-executions.test.ts`

- [ ] **Step 1: Add service tests first**

Assert:

```text
executeQueryTarget posts to /query-targets/{id}/execute
listQueryExecutions gets /query-targets/{id}/executions?page=1&pageSize=20
no export/save service exists
```

- [ ] **Step 2: Implement service**

Exports only:

```ts
executeQueryTarget(targetId: number, payload: QueryExecuteRequest): Promise<QueryExecuteResponse>
listQueryExecutions(targetId: number, params?: QueryExecutionListParams): Promise<QueryExecutionListResponse>
```

- [ ] **Step 3: Run tests**

```bash
npm run test -- tests/services/query-executions.test.ts
```

- [ ] **Step 4: Commit**

```bash
git add types/query-execution.ts services/query-executions.ts tests/services/query-executions.test.ts
git commit -m "feat: add query execution client service"
```

### Task F2: Enable Run Only For Ready Targets

**Files:**
- Modify: query workbench components and tests

- [ ] **Step 1: Add component tests**

Cover:

```text
Run disabled for credential_required target
Run enabled for ready target with availableActions.run=true
Export remains disabled even for ready target
```

- [ ] **Step 2: Implement minimal UI state**

Use backend flags:

```ts
const canRun = target.availableActions.run;
```

Do not infer readiness from engine or host/port.

- [ ] **Step 3: Run component tests**

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

- [ ] **Step 4: Commit**

```bash
git add components/query tests/components/query-workbench.test.tsx
git commit -m "feat: enable query run action from backend policy"
```

### Task F3: Render Results And History

**Files:**
- Modify: query components
- Create/modify: tests for result rendering and history

- [ ] **Step 1: Add tests**

Cover:

```text
successful response renders columns and rows
truncated response shows truncation notice
validation error shows backend message
history tab renders metadata and never renders full result rows
```

- [ ] **Step 2: Implement UI**

Keep:

```text
Export disabled
Save disabled
No client-side SQL enforcement beyond optional hints
```

- [ ] **Step 3: Run tests**

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

- [ ] **Step 4: Commit**

```bash
git add components/query tests/components/query-workbench.test.tsx
git commit -m "feat: render query results and history"
```

### Task F4: Add Cross-repo E2E

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/e2e/query-workbench.spec.ts`

- [ ] **Step 1: Add E2E scenarios**

Add:

```text
ready target can run select 1
write statement is blocked
history shows success and rejection
export remains disabled
```

- [ ] **Step 2: Run frontend gates**

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

- [ ] **Step 3: Run cross-repo E2E after backend and frontend merge**

```bash
gh workflow run "Frontend CI" --ref main -f run_e2e=true
```

Expected:

```text
release-local PASS
release-e2e PASS
query workbench SELECT and blocked-write tests PASS
```

- [ ] **Step 4: Commit**

```bash
git add e2e/query-workbench.spec.ts
git commit -m "test: cover query sandbox workflow"
```

## Final Verification Matrix

Backend:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Frontend:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Cross-repo:

```bash
gh workflow run "Frontend CI" --ref main -f run_e2e=true
```

## Merge Order

1. Merge backend Phase 37.
2. Push backend main and wait for fast CI.
3. Trigger backend heavy CI if API/fuzz changes need remote proof.
4. Merge frontend Phase 37.
5. Push frontend main and wait for fast CI.
6. Trigger frontend manual cross-repo E2E.
7. Record evidence in backend release docs.

## Scope Confirmation Required In Final Reports

Backend:

```text
only MySQL/TiDB SELECT execution
no writes
no multi-statement execution
no plaintext credentials in DB/API/logs
audit/history written for every attempt
OpenAPI updated
integration and fuzz gates passed
no export
no saved queries
no tag/release/deploy
```

Frontend:

```text
Run enabled only from backend availableActions.run
Export remains disabled
no credentials in browser
backend validation errors displayed
query history metadata only
cross-repo E2E passed or explicitly not run with reason
no tag/release/deploy
```
