# Phase 37F Query Execute UI And Dev Credentials Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the existing Query Workbench run one safe MySQL/TiDB SELECT against a backend-ready local/dev target.

**Architecture:** Backend first adds explicit local/dev credential metadata seeding without storing DSNs. Frontend then consumes Phase 37 execute/history APIs and enables Run only for backend-ready targets. Cross-repo E2E proves one ready target can execute `select 1` while locked targets and unsafe statements remain blocked.

**Tech Stack:** Go, MySQL, Makefile, OpenAPI, Next.js, React, TypeScript, Vitest, Playwright cross-repo E2E.

---

## Required Reading

Backend:

```text
docs/superpowers/specs/2026-06-22-phase-37f-query-execute-ui-and-dev-credentials.md
docs/decisions/2026-06-22-phase-37f-query-execute-ui-boundary.md
docs/superpowers/specs/2026-06-21-phase-37-read-only-query-sandbox.md
internal/service/query_execution_service.go
internal/service/query_target_service.go
internal/repository/mysql/query_execution_repository.go
internal/repository/mysql/query_target_repository.go
internal/integration/query_execution_test.go
migrations/00011_query_execution.sql
Makefile
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

Backend task first:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree add .worktrees/backend-phase-37f-dev-query-credential -b phase-37f-dev-query-credential main
```

Frontend task second, after backend merge:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree add .worktrees/frontend-phase-37f-query-execute-ui -b feat/phase-37f-query-execute-ui main
```

Do not create parallel branches unless the user explicitly requests parallel
work. The frontend depends on the backend dev credential path for final E2E.

## Backend File Map

Expected backend files:

```text
cmd/querydev/main.go                         # local/dev seed command
internal/service/query_dev_seed.go           # pure validation + seed orchestration
internal/service/query_dev_seed_test.go      # validator/service tests
internal/repository/mysql/query_execution_repository.go
internal/integration/query_dev_seed_test.go  # real MySQL seed/readiness tests
Makefile                                     # seed-query-dev-credential target
docs/superpowers/notes/<date>-phase-37f-dev-credential-evidence.md
```

If the worker finds a simpler existing command pattern, follow it, but keep the
same behavior and tests.

## Frontend File Map

Expected frontend files:

```text
types/query-execution.ts
services/query-executions.ts
components/query/query-editor-shell.tsx
components/query/query-workbench.tsx
components/query/query-history-panel.tsx
tests/services/query-executions.test.ts
tests/components/query-workbench.test.tsx
e2e/query-workbench.spec.ts
messages/en.json
messages/zh-CN.json
```

## Task B1: Backend Dev Credential Seed

**Files:**
- Create: `cmd/querydev/main.go`
- Create: `internal/service/query_dev_seed.go`
- Create: `internal/service/query_dev_seed_test.go`
- Modify: `internal/repository/mysql/query_execution_repository.go`
- Modify: `Makefile`

- [ ] **Step 1: Write failing service tests**

Add tests covering:

```text
RejectsMissingTargetResourceID
RejectsInvalidCredentialRef
RejectsUnsupportedEngine
RejectsIncompleteConnection
RejectsMissingResolvedDSN
BuildsCredentialMetadataForNonProdOnly
RejectsAllEnvironmentsUnlessExplicitlyAllowed
```

Expected initial result:

```bash
go test -count=1 ./internal/service -run QueryDevSeed
```

The tests fail because the seed service does not exist.

- [ ] **Step 2: Implement seed config and validation**

Create a focused service type:

```go
type QueryDevCredentialSeedConfig struct {
    TargetResourceID uint64
    CredentialRef string
    EnvironmentPolicy model.QueryEnvironmentPolicy
    AllowAllEnvironments bool
}
```

Validation rules:

```text
TargetResourceID > 0
CredentialRef passes model.ValidateCredentialRef
EnvironmentPolicy validates
all_environments requires AllowAllEnvironments=true
target engine is mysql/tidb
target host non-empty
target port non-zero
resolved DSN is non-empty
```

Use existing Phase 37 DSN binding behavior. Do not duplicate weaker matching
logic.

- [ ] **Step 3: Add repository upsert**

Add a repository method for metadata only:

```go
UpsertCredentialMetadata(ctx context.Context, meta model.QueryCredentialMetadata) error
```

It writes:

```text
resource_id
engine
credential_ref
enabled
environment_policy
```

It never accepts or stores a DSN.

Use `insert ... on duplicate key update` against the existing unique key on
`resource_id`.

- [ ] **Step 4: Add command**

Create `cmd/querydev/main.go`. It reads:

```text
DATABASE_DSN
QUERY_DEV_TARGET_RESOURCE_ID
QUERY_DEV_CREDENTIAL_REF
QUERY_DEV_ENVIRONMENT_POLICY
QUERY_DEV_ALLOW_ALL_ENVIRONMENTS
CONTROLHUB_QUERY_CREDENTIAL_<REF>
```

It loads `.env` using the same project convention as `cmd/server/main.go`.

It prints only safe metadata:

```text
target resource id
credential ref
engine
environment policy
readiness after seed
```

It must not print the DSN.

- [ ] **Step 5: Add Makefile target**

Add:

```make
seed-query-dev-credential:
	go run ./cmd/querydev
```

Do not make this target part of release gates.

- [ ] **Step 6: Run backend unit tests**

Run:

```bash
go test -count=1 ./internal/service -run 'QueryDevSeed|Credential'
go test -count=1 ./internal/repository/mysql -run Query
```

Expected: pass.

- [ ] **Step 7: Commit backend seed unit slice**

Commit:

```bash
git add cmd/querydev internal/service internal/repository/mysql Makefile
git commit -m "feat: add local query credential seed"
```

## Task B2: Backend Integration And Evidence

**Files:**
- Create: `internal/integration/query_dev_seed_test.go`
- Create: `docs/superpowers/notes/2026-06-22-phase-37f-dev-credential-evidence.md`

- [ ] **Step 1: Write integration test**

Create a disposable MySQL target whose profile host/port matches the
Testcontainers DSN. Seed credential metadata with a safe ref such as
`LOCAL_QUERY_RO`.

Assert:

```text
GET /query-targets equivalent service path reports readiness=ready
availableActions.run=true
governance.executionEnabled=true
governance.safetyState=readonly_sandbox_enabled
execute select 1 succeeds through QueryExecutionService
query_target_credentials contains no DSN-looking value
```

- [ ] **Step 2: Add mismatch regression**

Add an integration test where the credential resolves to a mismatched host or
port. Assert execution is rejected and target is not treated as runnable.

- [ ] **Step 3: Run backend gates**

Run:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Docker gates must run if Docker is available.

- [ ] **Step 4: Record evidence note**

Write a short note with:

```text
commit hash
seed command behavior
no DSN persistence confirmation
integration result
fuzz result
known local setup command
```

- [ ] **Step 5: Commit backend integration slice**

Commit:

```bash
git add internal/integration docs/superpowers/notes
git commit -m "test: cover local query credential readiness"
```

## Task F1: Frontend Execute Service And Types

**Files:**
- Create: `/Users/fan/JsProjects/ControlHub/types/query-execution.ts`
- Create: `/Users/fan/JsProjects/ControlHub/services/query-executions.ts`
- Create: `/Users/fan/JsProjects/ControlHub/tests/services/query-executions.test.ts`

- [ ] **Step 1: Write service tests**

Tests must assert:

```text
executeQueryTarget posts to /query-targets/:id/execute
request body contains statement and maxRows
request body does not contain actorUserId
listQueryExecutions gets /query-targets/:id/executions?page=1&pageSize=20
controlled API errors are surfaced without raw Response leakage
```

- [ ] **Step 2: Add types**

Define:

```text
QueryExecuteRequest
QueryExecuteResponse
QueryResultColumn
QueryExecutionRecord
QueryExecutionListResponse
```

Match the backend OpenAPI names and field casing exactly.

- [ ] **Step 3: Implement service**

Use the existing authenticated API helper pattern in the frontend. Do not add a
new auth scheme. Do not accept actor id as an argument.

- [ ] **Step 4: Run frontend service tests**

Run:

```bash
npm run test -- tests/services/query-executions.test.ts
npx tsc --noEmit
```

- [ ] **Step 5: Commit**

Commit:

```bash
git add types/query-execution.ts services/query-executions.ts tests/services/query-executions.test.ts
git commit -m "feat: add query execution frontend service"
```

## Task F2: Frontend Workbench Execution UI

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/components/query/query-workbench.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/query/query-editor-shell.tsx`
- Create or modify: `/Users/fan/JsProjects/ControlHub/components/query/query-history-panel.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/messages/en.json`
- Modify: `/Users/fan/JsProjects/ControlHub/messages/zh-CN.json`
- Modify: `/Users/fan/JsProjects/ControlHub/tests/components/query-workbench.test.tsx`

- [ ] **Step 1: Write component tests**

Tests must cover:

```text
locked target keeps Run disabled
ready target enables Run
click Run calls execute service once
Run disabled while request is pending
successful response renders columns/rows/rowCount/duration/limit/truncated
null cell renders as explicit null/empty marker, not 0 or undefined
403 query_not_allowed renders governance/policy error
400 validation_failed renders SQL guard error
408 query_timeout renders timeout state
502 query_backend_error renders backend failure state
history refresh is requested after execute settles
```

- [ ] **Step 2: Implement local state**

Add state for:

```text
statement
maxRows
isExecuting
executeResult
executeError
history
historyLoading
```

Default statement may be:

```sql
select 1
```

Only use it as editable text. Do not auto-run.

- [ ] **Step 3: Wire Run button**

Run is enabled only when:

```text
selectedTarget.availableActions.run === true
not currently executing
statement.trim() is not empty
```

Every other target stays locked.

- [ ] **Step 4: Render results**

Render result table using backend columns and rows. For null cells, render a
localized subtle marker:

```text
NULL
```

Do not coerce null to empty string or zero.

- [ ] **Step 5: Render history**

History tab displays metadata only. It must not show result rows or credentials.

- [ ] **Step 6: Run component tests**

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
npx tsc --noEmit
npm run lint
```

- [ ] **Step 7: Commit**

Commit:

```bash
git add components/query messages tests/components
git commit -m "feat: wire query workbench execution UI"
```

## Task F3: Cross-repo E2E

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/e2e/query-workbench.spec.ts`

- [ ] **Step 1: Ensure backend setup**

Use the backend Phase 37F seed command in CI/local setup so one target is ready.
Do not fake backend responses.

- [ ] **Step 2: Add E2E tests**

Tests:

```text
locked target cannot run
ready target runs select 1 and displays result
unsafe statement is rejected with validation message
history shows the recent attempt
```

- [ ] **Step 3: Run local frontend gates**

Run:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

- [ ] **Step 4: Run cross-repo E2E**

Run the existing manual cross-repo E2E workflow or local equivalent with the real
backend on `:8080`. Required:

```text
smoke passes
interaction passes
full E2E passes
new query workbench execute test passes
```

- [ ] **Step 5: Commit**

Commit:

```bash
git add e2e/query-workbench.spec.ts
git commit -m "test: cover query workbench execution e2e"
```

## Final Verification

Backend:

```bash
git diff --check
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
npm run test:e2e
```

Remote:

```text
backend fast CI passes after backend push
frontend fast CI passes after frontend push
frontend manual cross-repo E2E passes
```

## Handoff Requirements

Each worker final report must include:

- worktree, branch, commits
- files changed
- exact seed command or UI behavior delivered
- verification matrix
- whether Docker gates ran
- whether cross-repo E2E ran
- explicit statement that no DSNs/passwords were printed or persisted
- final git status
