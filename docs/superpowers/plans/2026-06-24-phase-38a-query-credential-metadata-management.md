# Phase 38A Query Credential Metadata Management Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add authenticated product APIs and frontend UI for query credential metadata management without storing or displaying plaintext secrets.

**Architecture:** Reuse the existing `query_target_credentials` table and env-backed resolver. Add a backend service that manages metadata, inspects resolver/binding status, audits successful changes, and corrects query-target readiness so metadata alone never marks a target ready. Add an admin-only Settings UI for credential metadata management; the Query Workbench only displays credential readiness/status and never exposes edit controls to normal query users. The DSN remains in server env/external secret configuration.

**Tech Stack:** Go, Chi, MySQL, OpenAPI, existing ControlHub auth middleware, Next.js, TypeScript, next-intl, Playwright.

---

## File Map

Backend files:

- Create `internal/model/query_credential.go` for credential metadata API request/response and runtime status enum.
- Modify `internal/model/query_execution.go` only if shared types must move; prefer leaving existing execution types in place.
- Modify `internal/model/README.md`.
- Modify `internal/repository/mysql/query_execution_repository.go` to add product-safe metadata get/upsert/delete methods.
- Create `internal/service/query_credential_service.go` for metadata management and runtime status inspection.
- Modify `internal/service/query_target_service.go` so readiness uses runtime status, not metadata alone.
- Modify `internal/service/query_execution_service.go` only to share DSN binding helper if needed.
- Create `internal/api/query_credential_handler.go`.
- Modify `internal/api/auth_middleware.go` to add an admin-role middleware helper.
- Modify `internal/api/router.go` to mount credential routes under fresh bearer auth.
- Modify `cmd/server/main.go` and `internal/api/test_server.go` for wiring.
- Modify `internal/integration/openapi_fuzz_test.go` for wiring and declared auth behavior.
- Modify `internal/openapi/openapi.yaml`.
- Add backend tests beside changed files.

Frontend files:

- Create `/Users/fan/JsProjects/ControlHub/types/query-credential.ts`.
- Create `/Users/fan/JsProjects/ControlHub/services/query-credentials.ts`.
- Create `/Users/fan/JsProjects/ControlHub/app/(console)/settings/query-credentials/page.tsx` or the closest existing admin/settings route convention.
- Add admin credential management components under `/Users/fan/JsProjects/ControlHub/components/settings/` or an existing settings/admin component directory.
- Modify `/Users/fan/JsProjects/ControlHub/components/query/query-governance-panel.tsx` only to show read-only status and an admin/settings link where appropriate.
- Modify frontend i18n files.
- Add frontend unit/component/service tests.
- Extend `/Users/fan/JsProjects/ControlHub/e2e/query-workbench.spec.ts` or add a settings/admin E2E spec for credential management.

Docs:

- Add evidence note `docs/superpowers/notes/2026-06-24-phase-38a-query-credential-metadata-management-evidence.md`.

## B1. Backend Model Contract

**Files:**

- Create: `internal/model/query_credential.go`
- Test: `internal/model/query_credential_test.go`
- Modify: `internal/model/README.md`

- [ ] **Step 1: Add failing model tests**

Create tests for these behaviors:

```go
func TestQueryCredentialRuntimeStatusValidate(t *testing.T) {
	valid := []QueryCredentialRuntimeStatus{
		QueryCredentialRuntimeMissingMetadata,
		QueryCredentialRuntimeInvalidRef,
		QueryCredentialRuntimeDisabled,
		QueryCredentialRuntimePolicyBlocked,
		QueryCredentialRuntimeSecretMissing,
		QueryCredentialRuntimeBindingMismatch,
		QueryCredentialRuntimeSecretResolved,
		QueryCredentialRuntimeUnsupportedTarget,
		QueryCredentialRuntimeIncompleteConnection,
	}
	for _, status := range valid {
		if err := status.Validate(); err != nil {
			t.Fatalf("%s should validate: %v", status, err)
		}
	}
	if err := QueryCredentialRuntimeStatus("raw_unknown").Validate(); err == nil {
		t.Fatal("unknown runtime status must fail validation")
	}
}

func TestQueryCredentialUpsertRequestValidate(t *testing.T) {
	body := QueryCredentialUpsertRequest{
		CredentialRef:     "ORDER_MYSQL_RO",
		Enabled:           true,
		EnvironmentPolicy: QueryEnvPolicyNonProdOnly,
	}
	if err := body.Validate(); err != nil {
		t.Fatalf("valid request should pass: %v", err)
	}

	body.EnvironmentPolicy = QueryEnvPolicyAllEnvironments
	body.ConfirmAllEnvironments = false
	if err := body.Validate(); err == nil {
		t.Fatal("all_environments must require explicit confirmation")
	}
}
```

Do not add temporary "unknown key capture" fields to the model. Rejection of
unknown or secret-like JSON fields (`dsn`, `password`, `host`, `port`,
`actorUserId`) belongs in the handler strict-decoding tests in B4.

- [ ] **Step 2: Run model tests to verify RED**

```bash
go test ./internal/model -run QueryCredential
```

Expected: compile failure because the types do not exist.

- [ ] **Step 3: Add model types**

Add:

```go
type QueryCredentialRuntimeStatus string

const (
	QueryCredentialRuntimeMissingMetadata     QueryCredentialRuntimeStatus = "missing_metadata"
	QueryCredentialRuntimeInvalidRef          QueryCredentialRuntimeStatus = "invalid_ref"
	QueryCredentialRuntimeDisabled            QueryCredentialRuntimeStatus = "disabled"
	QueryCredentialRuntimePolicyBlocked       QueryCredentialRuntimeStatus = "policy_blocked"
	QueryCredentialRuntimeSecretMissing       QueryCredentialRuntimeStatus = "secret_missing"
	QueryCredentialRuntimeBindingMismatch     QueryCredentialRuntimeStatus = "binding_mismatch"
	QueryCredentialRuntimeSecretResolved      QueryCredentialRuntimeStatus = "secret_resolved"
	QueryCredentialRuntimeUnsupportedTarget   QueryCredentialRuntimeStatus = "unsupported_target"
	QueryCredentialRuntimeIncompleteConnection QueryCredentialRuntimeStatus = "incomplete_connection"
)
```

Add response/request types:

```go
type QueryCredentialStatusResponse struct {
	ResourceID        uint64                       `json:"resourceId"`
	Configured        bool                         `json:"configured"`
	Engine            string                       `json:"engine"`
	CredentialRef     string                       `json:"credentialRef"`
	Enabled           bool                         `json:"enabled"`
	EnvironmentPolicy QueryEnvironmentPolicy       `json:"environmentPolicy"`
	RuntimeStatus     QueryCredentialRuntimeStatus `json:"runtimeStatus"`
	ExecutionEligible bool                         `json:"executionEligible"`
	Message           string                       `json:"message"`
}

type QueryCredentialUpsertRequest struct {
	CredentialRef           string                 `json:"credentialRef"`
	Enabled                 bool                   `json:"enabled"`
	EnvironmentPolicy       QueryEnvironmentPolicy `json:"environmentPolicy"`
	ConfirmAllEnvironments  bool                   `json:"confirmAllEnvironments,omitempty"`
}
```

Implement `Validate()` methods:

- `CredentialRef` must pass `ValidateCredentialRef`.
- `EnvironmentPolicy` must pass `Validate()`.
- `all_environments` requires `ConfirmAllEnvironments`.

The handler will reject unknown body keys; the model must not grow temporary
fields just to inspect rejected JSON keys.

- [ ] **Step 4: Update tests to match final types and run GREEN**

```bash
go test ./internal/model -run QueryCredential
```

Expected: PASS.

- [ ] **Step 5: Update `internal/model/README.md`**

Add `query_credential.go` and `query_credential_test.go`.

- [ ] **Step 6: Commit**

```bash
git add internal/model/query_credential.go internal/model/query_credential_test.go internal/model/README.md
git commit -m "feat: add query credential metadata model"
```

## B2. Repository Metadata Methods

**Files:**

- Modify: `internal/repository/mysql/query_execution_repository.go`
- Test: `internal/integration/query_credential_repository_test.go`

- [ ] **Step 1: Write failing integration tests**

Cover:

- no metadata row returns a not-found sentinel or `sql.ErrNoRows`;
- upsert inserts metadata only;
- upsert updates existing metadata;
- delete removes metadata;
- invalid stored ref fails closed on read;
- no DSN-looking value exists in any credential metadata column.

Test names:

```go
func TestQueryCredentialRepository_UpsertGetDelete(t *testing.T)
func TestQueryCredentialRepository_InvalidStoredRefFailsClosed(t *testing.T)
func TestQueryCredentialRepository_MetadataNeverStoresDSN(t *testing.T)
```

- [ ] **Step 2: Run targeted integration tests to verify RED**

```bash
go test -count=1 -tags=integration ./internal/integration -run QueryCredentialRepository
```

Expected: compile failure for missing repository methods.

- [ ] **Step 3: Add repository methods**

Add concrete methods to `QueryExecutionRepository`:

```go
func (r *QueryExecutionRepository) UpsertCredentialMetadata(ctx context.Context, meta model.QueryCredentialMetadata) error
func (r *QueryExecutionRepository) DeleteCredentialByResourceID(ctx context.Context, resourceID uint64) error
```

`UpsertCredentialMetadata` already exists for dev seed on current main. Keep it
and harden it if needed:

- validate `CredentialRef`,
- validate `EnvironmentPolicy`,
- never accept DSN/password,
- keep idempotent `on duplicate key update`.

Add delete:

```sql
delete from query_target_credentials where resource_id = ?
```

- [ ] **Step 4: Run GREEN**

```bash
go test -count=1 -tags=integration ./internal/integration -run QueryCredentialRepository
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/mysql/query_execution_repository.go internal/integration/query_credential_repository_test.go
git commit -m "feat: add query credential metadata repository operations"
```

## B3. Runtime Status Service And Readiness Correction

**Files:**

- Create: `internal/service/query_credential_service.go`
- Test: `internal/service/query_credential_service_test.go`
- Modify: `internal/service/query_target_service.go`
- Test: `internal/service/query_target_service_test.go`
- Modify: `internal/service/query_execution_service.go` only if DSN binding helper must be shared.

- [ ] **Step 1: Write failing service tests**

Create tests for:

- missing metadata -> `missing_metadata`, not eligible;
- invalid ref -> `invalid_ref`, not eligible, resolver not called;
- disabled metadata -> `disabled`;
- policy blocked production target with `non_prod_only` -> `policy_blocked`;
- missing env secret -> `secret_missing`;
- DSN host mismatch -> `binding_mismatch`;
- DSN port mismatch -> `binding_mismatch`;
- matching DSN -> `secret_resolved`, eligible;
- unsupported engine -> `unsupported_target`;
- incomplete host/port -> `incomplete_connection`;
- successful upsert writes audit event `query.credential.updated`;
- delete writes audit event `query.credential.deleted`;
- non-admin actor cannot write/delete.

- [ ] **Step 2: Run service tests to verify RED**

```bash
go test ./internal/service -run 'QueryCredential|QueryTarget'
```

Expected: compile failure or failing readiness assertions.

- [ ] **Step 3: Implement service interfaces**

Add small interfaces:

```go
type QueryCredentialMetadataStore interface {
	GetCredentialByResourceID(ctx context.Context, resourceID uint64) (model.QueryCredentialMetadata, error)
	UpsertCredentialMetadata(ctx context.Context, meta model.QueryCredentialMetadata) error
	DeleteCredentialByResourceID(ctx context.Context, resourceID uint64) error
	InsertAuditEvent(ctx context.Context, actorUserID uint64, targetResourceID uint64, eventType string, result string) error
}
```

Add `QueryCredentialService` with:

```go
GetStatus(ctx context.Context, targetID uint64) (model.QueryCredentialStatusResponse, error)
Upsert(ctx context.Context, actor AuthenticatedUser, targetID uint64, req model.QueryCredentialUpsertRequest) (model.QueryCredentialStatusResponse, error)
Delete(ctx context.Context, actor AuthenticatedUser, targetID uint64) error
```

Use target lookup through the existing `QueryTargetRepository`. Do not accept
engine from request; derive it from target.

Share a helper that evaluates credential runtime status without returning DSN:

```go
InspectCredentialRuntime(ctx, target model.QueryTarget, cred *model.QueryCredentialMetadata) model.QueryCredentialRuntimeStatus
```

It may call resolver and `validateDSNBinding`, but it must never store or return
the DSN value.

- [ ] **Step 4: Correct QueryTargetService readiness**

Extend `QueryTargetService` with an optional runtime inspector. `GET
/query-targets` should mark ready only when runtime status is
`secret_resolved`. Metadata alone is not enough.

Expected behavior:

- metadata exists, env secret missing -> locked, `credentialState=secret_missing`;
- metadata exists, binding mismatch -> locked, `credentialState=binding_mismatch`;
- metadata exists and resolved/bound -> ready.

Because `credentialState` is surfaced to the frontend, update the whole contract
when adding these values:

- backend constants/tests or documented string set for `credentialState`;
- OpenAPI schema descriptions/examples for `QueryTargetGovernance`;
- frontend `KNOWN_CREDENTIAL_STATES`;
- English and Chinese i18n labels;
- component tests proving raw enum values are not rendered.

- [ ] **Step 5: Run GREEN**

```bash
go test ./internal/service -run 'QueryCredential|QueryTarget'
```

Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/query_credential_service.go internal/service/query_credential_service_test.go internal/service/query_target_service.go internal/service/query_target_service_test.go internal/service/query_execution_service.go
git commit -m "feat: add query credential metadata service"
```

## B4. Backend API And Auth

**Files:**

- Create: `internal/api/query_credential_handler.go`
- Test: `internal/api/query_credential_handler_test.go`
- Modify: `internal/api/auth_middleware.go`
- Test: `internal/api/auth_middleware_test.go`
- Modify: `internal/api/router.go`
- Modify: `cmd/server/main.go`
- Modify: `internal/api/test_server.go`
- Modify: `internal/integration/openapi_fuzz_test.go`

- [ ] **Step 1: Write failing handler/auth tests**

Cover:

- GET credential requires bearer;
- PUT credential requires bearer;
- DELETE credential requires bearer;
- PUT/DELETE require admin role;
- request body with `actorUserId`, `dsn`, `password`, `host`, or `port` is 400;
- `all_environments` without confirmation is 400;
- invalid target id is 400;
- target not found is 404;
- successful PUT returns status response;
- successful DELETE returns 204.

- [ ] **Step 2: Run handler tests to verify RED**

```bash
go test ./internal/api -run 'QueryCredential|AuthenticatedActor|FreshQueryActor'
```

Expected: compile failure for missing handler/routes or failing auth behavior.

- [ ] **Step 3: Add admin helper**

In `auth_middleware.go`, add a helper that checks context actor role. The current
context stores only user id, so either:

- store the full `AuthenticatedUser` in context, or
- add a second role context value.

Keep `actorUserIDFromContext` working for execution handlers.

Add:

```go
func actorRoleFromContext(ctx context.Context) (string, bool)
```

Write admin checks in credential handler. Do not alter query execution behavior.

- [ ] **Step 4: Add handler**

Routes:

```text
GET    /query-targets/{id}/credential
PUT    /query-targets/{id}/credential
DELETE /query-targets/{id}/credential
```

Use strict JSON decoding for PUT:

- reject unknown fields,
- reject DSN/password/host/port/actor fields,
- map service errors to controlled JSON.

- [ ] **Step 5: Wire routes**

Mount under:

```go
r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth))
```

Then role check in handler or middleware.

- [ ] **Step 6: Run GREEN**

```bash
go test ./internal/api -run 'QueryCredential|AuthenticatedActor|FreshQueryActor'
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/api cmd/server/main.go internal/integration/openapi_fuzz_test.go
git commit -m "feat: add query credential metadata API"
```

## B5. OpenAPI Contract

**Files:**

- Modify: `internal/openapi/openapi.yaml`
- Test: `internal/openapi/openapi_test.go` if schema assertions are needed.

- [ ] **Step 1: Add OpenAPI paths and schemas**

Add:

```text
/query-targets/{id}/credential
```

Methods:

- GET
- PUT
- DELETE

Declare:

- Bearer security,
- 400, 401, 403, 404, 409, 500 responses,
- `QueryCredentialStatusResponse`,
- `QueryCredentialUpsertRequest`,
- `QueryCredentialRuntimeStatus`.

Make clear that schemas contain no DSN/password fields.
Also update `QueryTargetGovernance.credentialState` descriptions/examples for
the new values introduced by readiness correction:

- `secret_missing`,
- `binding_mismatch`,
- any additional runtime-backed credential state emitted by
  `QueryTargetService`.

- [ ] **Step 2: Validate OpenAPI**

```bash
make openapi-validate
```

Expected: PASS.

- [ ] **Step 3: Run fuzz**

```bash
make test-openapi-fuzz
```

Expected: PASS. Unauthenticated 401/403 on protected credential endpoints must
be declared and treated as conformance, not failure.

- [ ] **Step 4: Commit**

```bash
git add internal/openapi/openapi.yaml internal/openapi/openapi_test.go
git commit -m "docs: document query credential metadata API"
```

## B6. Backend Integration

**Files:**

- Create: `internal/integration/query_credential_api_test.go`

- [ ] **Step 1: Write integration tests**

Cover:

- admin PUT creates metadata and GET returns configured response;
- server env missing -> status `secret_missing`, target not ready;
- matching env secret -> status `secret_resolved`, target ready;
- binding mismatch -> status `binding_mismatch`, target locked;
- DELETE removes metadata and target returns credential required;
- audit rows written for PUT and DELETE;
- DSN never appears in metadata tables, audit rows, or responses.

- [ ] **Step 2: Run targeted integration**

```bash
go test -count=1 -tags=integration ./internal/integration -run QueryCredential
```

Expected: PASS after implementation.

- [ ] **Step 3: Run backend full matrix**

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Expected: all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/integration/query_credential_api_test.go
git commit -m "test: cover query credential metadata API"
```

## F1. Frontend Types And Service

**Repo:** `/Users/fan/JsProjects/ControlHub`

**Files:**

- Create: `types/query-credential.ts`
- Create: `services/query-credentials.ts`
- Test: `tests/services/query-credentials.test.ts`

- [ ] **Step 1: Add failing service tests**

Assert:

- GET calls `/query-targets/:id/credential`;
- PUT body contains only `credentialRef`, `enabled`,
  `environmentPolicy`, `confirmAllEnvironments`;
- DELETE calls correct path;
- service exports no DSN/password helper;
- request body never contains `actorUserId`, `dsn`, `password`, `host`, or
  `port`.

- [ ] **Step 2: Run RED**

```bash
cd /Users/fan/JsProjects/ControlHub
npm run test -- tests/services/query-credentials.test.ts
```

Expected: fail because service does not exist.

- [ ] **Step 3: Implement types and service**

Types mirror OpenAPI:

```ts
export type QueryCredentialRuntimeStatus =
  | "missing_metadata"
  | "invalid_ref"
  | "disabled"
  | "policy_blocked"
  | "secret_missing"
  | "binding_mismatch"
  | "secret_resolved"
  | "unsupported_target"
  | "incomplete_connection";
```

Service functions:

```ts
getQueryCredential(targetResourceId: number)
saveQueryCredential(targetResourceId: number, input: QueryCredentialUpsertRequest)
deleteQueryCredential(targetResourceId: number)
```

- [ ] **Step 4: Run GREEN**

```bash
npm run test -- tests/services/query-credentials.test.ts
```

Expected: PASS.

- [ ] **Step 5: Commit frontend**

```bash
git add types/query-credential.ts services/query-credentials.ts tests/services/query-credentials.test.ts
git commit -m "feat: add query credential metadata service"
```

## F2. Frontend Admin Credential Settings UI

**Repo:** `/Users/fan/JsProjects/ControlHub`

**Files:**

- Create: `app/(console)/settings/query-credentials/page.tsx` or the closest existing admin/settings route convention
- Create: `components/settings/query-credential-settings.tsx` or an existing settings/admin component path
- Modify: `components/query/query-governance-panel.tsx`
- Modify: `components/query/query-workbench.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: `tests/components/query-workbench.test.tsx`
- Test: add a settings/admin page or component test for credential management

- [ ] **Step 1: Add failing component tests**

Cover:

- settings/admin page lists query targets and credential states;
- missing metadata shows "Configure credential" on the admin page only;
- configured metadata shows ref and policy, not DSN;
- secret missing shows a locked warning;
- binding mismatch shows a locked warning;
- ready status keeps Run controlled by backend target `availableActions.run`;
- form rejects all-environments save until confirmation checked;
- save request body has no DSN/password/actor fields;
- delete refreshes credential status and target list;
- Query Workbench does not render credential edit/remove/configure form controls;
- Query Workbench may render an admin/settings link for admin users and a
  contact-administrator state for non-admin users.

- [ ] **Step 2: Run RED**

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: failing tests.

- [ ] **Step 3: Implement component**

UI rules:

- Put credential editing in settings/admin, not inside `/query`.
- Query Workbench governance panel is read-only for credential metadata.
- Admin page fields:
  - credential reference,
  - enabled checkbox,
  - environment policy select,
  - all-environments confirmation checkbox only when needed.
- No DSN/password field.
- After save/delete, refresh credential status and query targets.
- Keep all labels localized.
- Support DBA standard-account workflows by making ref reuse/naming visible:
  show copy that a standardized read-only account is provisioned server-side,
  then bound by opaque ref. Do not imply the browser stores the account.
- Support cluster-specific overrides by allowing per target/cluster ref binding.
- Extend `KNOWN_CREDENTIAL_STATES`, English copy, and Chinese copy for
  `secret_missing`, `binding_mismatch`, and any other backend-emitted
  credential state. Component tests must assert these render as localized
  labels, not raw enum strings.

- [ ] **Step 4: Run GREEN**

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit frontend**

```bash
git add app components messages tests
git commit -m "feat: add query credential settings UI"
```

## F3. Frontend E2E

**Repo:** `/Users/fan/JsProjects/ControlHub`

**Files:**

- Modify: `e2e/query-workbench.spec.ts`
- Add or modify a settings/admin E2E spec for query credential settings.

- [ ] **Step 1: Extend E2E**

Use the Phase 37H dedicated query fixture.

Cover:

- open the settings/admin query credential page as an admin;
- configure `LOCAL_QUERY_RO` for the dedicated target from settings/admin;
- target becomes ready when server has env secret and binding matches;
- Query Workbench shows ready state but no credential configuration form;
- safe query runs;
- unsafe query rejects;
- history shows attempt;
- UI never shows DSN/password.

- [ ] **Step 2: Run frontend gates**

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Expected: PASS.

- [ ] **Step 3: Run query E2E**

With backend and dedicated query MySQL running:

```bash
npm run test:e2e -- --grep query
```

Expected: query/settings E2E passes with zero ready-target skips.

- [ ] **Step 4: Commit frontend**

```bash
git add e2e/query-workbench.spec.ts
git commit -m "test: cover query credential settings e2e"
```

## D1. Evidence And Release Docs

**Backend files:**

- Create: `docs/superpowers/notes/2026-06-24-phase-38a-query-credential-metadata-management-evidence.md`
- Modify release docs only if the user asks for release-evidence sync.

- [ ] **Step 1: Write evidence note**

Include:

- backend commits,
- frontend commits,
- API paths,
- security boundaries,
- no-secret proof,
- backend verification matrix,
- frontend verification matrix,
- query E2E result,
- GitNexus result,
- final git status.

- [ ] **Step 2: Validate docs**

```bash
git diff --check
python3 - <<'PY'
from pathlib import Path

path = Path("docs/superpowers/notes/2026-06-24-phase-38a-query-credential-metadata-management-evidence.md")
needles = ["TO" + "DO", "TB" + "D", "PLACE" + "HOLDER", "<" * 7, ">" * 7, "=" * 7]
text = path.read_text()
hits = [needle for needle in needles if needle in text]
if hits:
    raise SystemExit(f"blocked markers found: {hits}")
PY
```

Expected: no matches.

- [ ] **Step 3: Commit backend docs**

```bash
git add docs/superpowers/notes/2026-06-24-phase-38a-query-credential-metadata-management-evidence.md
git commit -m "docs: record query credential metadata management evidence"
```

## Required Final Backend Verification

Run from backend repo:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Backend
```

If GitNexus index is stale:

```bash
npx gitnexus analyze
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Backend
```

## Required Final Frontend Verification

Run from frontend repo:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
npm run test:e2e -- --grep query
```

## Scope Confirmation For Final Report

The final report must state:

- no DSN/password in request bodies,
- no DSN/password in responses,
- no DSN/password in browser state,
- no secret write API,
- no migration unless the implementation proves the existing table is
  insufficient and the user approves,
- no new query engines,
- no SQL guard relaxation,
- no export or saved-query feature,
- no workflow changes,
- no tag, release, or deployment,
- no AI co-author.
