# Resource CRUD Redesign Closure Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the remaining backend-only gaps for `resource-crud-redesign` so embedded profile persistence, OpenAPI contract coverage, and closure-grade validation evidence are all real and auditable.

**Architecture:** Keep the shipped backend design intact and limit this round to closure work. The plan adds one router-level regression path that proves real dependency wiring preserves embedded profiles on create, then aligns `internal/openapi/openapi.yaml` with the already-implemented backend contract for editable PATCH `name` and profile write endpoints, then runs only the minimum validation needed to legitimately mark backend closure.

**Tech Stack:** Go 1.26, chi router, standard `go test`, kin-openapi validation via `make openapi-validate`

---

## File Structure

| File | Responsibility |
|------|---------------|
| `cmd/server/main.go` | Real application wiring via `buildDependencies`; already fixed in worktree and should only be touched if regression evidence reveals a remaining gap. |
| `cmd/server/main_test.go` | Existing DI regression test proving `ProfileService` is injected into `ResourceService`. |
| `internal/api/test_server.go` | Test-only router wiring and fake repositories; likely place to expose or inspect persisted profile state for router-level regression coverage. |
| `internal/api/resource_handler_test.go` | Best home for end-to-end handler/router tests around `POST /resources` and follow-up `GET /resources/{id}/profile`. |
| `internal/openapi/openapi.yaml` | Contract source of truth; needs closure updates for PATCH `name`, embedded create `profile`, and `PUT/PATCH/DELETE /resources/{id}/profile`. |
| `internal/service/resource_service.go` | Already supports embedded create `profile` and editable PATCH `name`; use as implementation reference, not a redesign target. |
| `internal/api/profile_handler.go` | Runtime behavior for profile write endpoints; use as source when aligning OpenAPI and deciding whether any extra test is still missing. |

---

### Task 1: Prove real router create-with-profile persistence

**Files:**
- Modify: `internal/api/test_server.go`
- Modify: `internal/api/resource_handler_test.go`
- Verify only if needed: `cmd/server/main.go`

- [ ] **Step 1: Write the failing router-level regression test**

Add a new test to `internal/api/resource_handler_test.go` near `TestCreateResource`:

```go
func TestCreateResource_PersistsEmbeddedProfileThroughRouterWiring(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"resourceSubtype":"mysql",
		"name":"order-mysql-03-prod",
		"displayName":"Order MySQL 03 Prod",
		"environmentId":"10000000-0000-0000-0000-000000000001",
		"ownerId":"20000000-0000-0000-0000-000000000002",
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"externalId":"order-mysql-03-prod",
		"labels":{"team":"order","tier":"data"},
		"profile":{
			"engine":"mysql",
			"version":"8.0.37",
			"host":"prod-db-host-03.internal",
			"port":3306,
			"role":"primary"
		}
	}`

	createReq := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	createRec := httptest.NewRecorder()
	server.Router.ServeHTTP(createRec, createReq)

	if createRec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d; body: %s", createRec.Code, createRec.Body.String())
	}

	var created model.Resource
	if err := json.NewDecoder(createRec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}

	profileReq := httptest.NewRequest(http.MethodGet, "/resources/"+created.ID+"/profile", nil)
	profileRec := httptest.NewRecorder()
	server.Router.ServeHTTP(profileRec, profileReq)

	if profileRec.Code != http.StatusOK {
		t.Fatalf("expected 200 from profile readback, got %d; body: %s", profileRec.Code, profileRec.Body.String())
	}

	var profileResp model.ResourceProfileResponse
	if err := json.NewDecoder(profileRec.Body).Decode(&profileResp); err != nil {
		t.Fatalf("decode profile response: %v", err)
	}

	checkProfileField(t, profileResp.Profile, "engine", "mysql")
	checkProfileField(t, profileResp.Profile, "version", "8.0.37")
	checkProfileField(t, profileResp.Profile, "host", "prod-db-host-03.internal")
	checkProfileField(t, profileResp.Profile, "role", "primary")
	if port := profileResp.Profile["port"]; port != float64(3306) {
		t.Fatalf("expected port 3306, got %v", port)
	}
}
```

- [ ] **Step 2: Run the test and confirm the current failure mode**

Run: `go test ./internal/api -v -run TestCreateResource_PersistsEmbeddedProfileThroughRouterWiring -count=1`
Expected: FAIL if `NewTestServer()` still wires `ResourceService` without `ProfileService`, or if the fake repo does not persist profile writes for created resources.

- [ ] **Step 3: Make the fake router wiring match production wiring**

In `internal/api/test_server.go`, wire a shared profile service into both dependencies instead of constructing `ResourceService` alone:

```go
	profileSvc := service.NewProfileService(resourceRepo, resourceRepo)

	deps := Dependencies{
		ResourceService:         service.NewResourceService(resourceRepo, profileSvc),
		RelationService:         service.NewRelationService(relationRepo),
		TopologyService:         service.NewTopologyService(topologyRepo),
		AuditService:            service.NewAuditService(fakeAuditRepo{}),
		AuthService:             service.NewAuthService(fakeUserCredentialRepo{}, "test-secret"),
		EnvironmentService:      service.NewEnvironmentService(fakeEnvironmentRepo{}),
		OwnerService:            service.NewOwnerService(fakeOwnerRepo{}),
		RoleService:             service.NewRoleService(fakeRoleRepo{}),
		ResourceTypeService:     service.NewResourceTypeService(fakeResourceTypeRepo{}),
		RelationTypeService:     service.NewRelationTypeService(fakeRelationTypeRepo{}),
		LifecycleStatusService:  service.NewLifecycleStatusService(fakeLifecycleStatusRepo{}),
		HealthStatusService:     service.NewHealthStatusService(fakeHealthStatusRepo{}),
		ResourceSubtypeService:  service.NewResourceSubtypeService(),
		ProfileService:          profileSvc,
	}
```

- [ ] **Step 4: Persist profile writes in the fake repository instead of dropping them**

Extend `fakeResourceRepo` in `internal/api/test_server.go` with the profile-writer methods expected by `ProfileService`, updating `f.profiles` for supported resource types:

```go
func (f *fakeResourceRepo) UpsertHostProfile(_ context.Context, resourceID string, hostname, ipAddress, osName string) error {
	f.profiles[resourceID] = &model.ResourceProfileResponse{
		ResourceID:      resourceID,
		ResourceType:    model.ResourceTypeHost,
		ResourceSubtype: f.resources[resourceID].ResourceSubtype,
		Profile: map[string]any{
			"hostname":  hostname,
			"ipAddress": ipAddress,
			"osName":    osName,
		},
	}
	return nil
}

func (f *fakeResourceRepo) UpsertDatabaseInstanceProfile(_ context.Context, resourceID string, engine, version, host string, port int, role string) error {
	f.profiles[resourceID] = &model.ResourceProfileResponse{
		ResourceID:      resourceID,
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: f.resources[resourceID].ResourceSubtype,
		Profile: map[string]any{
			"engine":  engine,
			"version": version,
			"host":    host,
			"port":    port,
			"role":    role,
		},
	}
	return nil
}
```

Add the equivalent `UpsertDatabaseClusterProfile`, `UpsertServiceProfile`, and `DeleteProfile` methods in the same style, using the existing `f.resources` map to keep `resourceType`/`resourceSubtype` aligned.

- [ ] **Step 5: Re-run the targeted handler tests**

Run: `go test ./internal/api -v -run 'TestCreateResource|TestGetResourceProfile|TestCreateResource_PersistsEmbeddedProfileThroughRouterWiring' -count=1`
Expected: PASS

- [ ] **Step 6: Re-run the server wiring regression test**

Run: `go test ./cmd/server -v -run TestBuildDependencies_WiresProfileServiceIntoResourceService -count=1`
Expected: PASS

- [ ] **Step 7: Commit the regression coverage**

```bash
git add internal/api/test_server.go internal/api/resource_handler_test.go
git commit -m "test: cover embedded profile persistence through router wiring"
```

---

### Task 2: Close the OpenAPI contract gaps

**Files:**
- Modify: `internal/openapi/openapi.yaml`

- [ ] **Step 1: Write a focused contract checklist in the schema comments while editing**

Before changing content, use this checklist against the existing spec section:

```yaml
# Closure checklist:
# - ResourceCreateInput includes optional profile object
# - ResourcePatchRequest includes editable name
# - /resources/{id}/profile documents put, patch, and delete
# - profile write endpoints return 204 plus standard error responses
# - ErrorResponse describes optional details map for structured validation failures
```

Do not keep the checklist comment in the final file; use it only as an editing guide.

- [ ] **Step 2: Add embedded create profile to `ResourceCreateInput`**

Update `internal/openapi/openapi.yaml` under `components.schemas.ResourceCreateInput.properties`:

```yaml
        profile:
          type: object
          additionalProperties: true
          description: >-
            Optional typed profile fields to persist during resource creation.
            Supported keys depend on resourceType/resourceSubtype.
```

- [ ] **Step 3: Add editable `name` to `ResourcePatchRequest`**

Update `components.schemas.ResourcePatchRequest.properties`:

```yaml
        name:
          type: string
          minLength: 1
          pattern: "^[a-z0-9][a-z0-9._-]*$"
```

- [ ] **Step 4: Document the profile write endpoints already implemented in the router**

Expand `/resources/{id}/profile` in `internal/openapi/openapi.yaml` by adding `put`, `patch`, and `delete` alongside `get`:

```yaml
    put:
      summary: Replace typed profile fields for a resource
      operationId: putResourceProfile
      parameters:
        - in: path
          name: id
          required: true
          schema:
            type: string
      requestBody:
        required: true
        content:
          application/json:
            schema:
              type: object
              additionalProperties: true
      responses:
        "204":
          description: Profile replaced
        "400":
          description: Validation failed or malformed JSON
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
        "404":
          description: Resource not found
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
        "409":
          description: Archived resource cannot be modified
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
```

Mirror the same structure for `patch` with `operationId: patchResourceProfile`, and add:

```yaml
    delete:
      summary: Delete the typed profile row for a resource
      operationId: deleteResourceProfile
      parameters:
        - in: path
          name: id
          required: true
          schema:
            type: string
      responses:
        "204":
          description: Profile deleted
        "404":
          description: Resource not found
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
        "409":
          description: Archived resource cannot be modified
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/ErrorResponse"
```

- [ ] **Step 5: Reflect structured validation details in `ErrorResponse`**

Update `components.schemas.ErrorResponse`:

```yaml
    ErrorResponse:
      type: object
      required: [error, message]
      properties:
        error:
          type: string
          description: Machine-readable error code
        message:
          type: string
          description: Human-readable error description
        details:
          type: object
          additionalProperties:
            type: string
          description: Optional field-level validation details
```

- [ ] **Step 6: Run OpenAPI validation**

Run: `make openapi-validate`
Expected: PASS

- [ ] **Step 7: Commit the contract alignment**

```bash
git add internal/openapi/openapi.yaml
git commit -m "docs: close resource CRUD OpenAPI contract gaps"
```

---

### Task 3: Add only the minimum extra closure evidence

**Files:**
- Modify only if needed: `internal/api/resource_handler_test.go`
- Modify only if needed: `internal/api/profile_handler.go`

- [ ] **Step 1: Check whether PATCH `name` already has direct handler coverage**

Search `internal/api/resource_handler_test.go` for a test that patches only `name` and verifies success.
Expected: if no direct success-path test exists, add one; if one already exists, skip to Step 3.

- [ ] **Step 2: Add the missing PATCH `name` success test only if absent**

If needed, add this targeted test:

```go
func TestPatchResource_AllowsNameUpdate(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-1", strings.NewReader(`{"name":"order-mysql-primary-prod"}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var resp model.Resource
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Name != "order-mysql-primary-prod" {
		t.Fatalf("expected updated name, got %s", resp.Name)
	}
}
```

- [ ] **Step 3: Check whether profile write endpoints already have any route-level tests**

Search `internal/api/resource_handler_test.go` and adjacent test files for `PUT /resources/{id}/profile`, `PATCH /resources/{id}/profile`, and `DELETE /resources/{id}/profile` coverage.
Expected: if none exist, add one concise happy-path test and one malformed-JSON/error-shape test; do not redesign the handlers in this closure pass unless tests expose a real contract mismatch.

- [ ] **Step 4: Add the minimal profile write endpoint coverage only if absent**

If needed, add targeted tests like:

```go
func TestPatchResourceProfile(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-db-instance/profile", strings.NewReader(`{"version":"8.0.38"}`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d; body: %s", rec.Code, rec.Body.String())
	}
}
```

and:

```go
func TestPatchResourceProfileRejectsMalformedJSON(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPatch, "/resources/res-db-instance/profile", strings.NewReader(`{"version":`))
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d; body: %s", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 5: Run the narrow API test slice**

Run: `go test ./internal/api -v -run 'TestCreateResource|TestPatchResource|Test.*ResourceProfile' -count=1`
Expected: PASS

- [ ] **Step 6: Run the closure validation set**

Run: `go test ./cmd/server ./internal/api -count=1 && make openapi-validate`
Expected: PASS

- [ ] **Step 7: Commit the closure evidence**

```bash
git add internal/api/resource_handler_test.go internal/api/profile_handler.go
git commit -m "test: add resource CRUD closure coverage"
```

---

### Task 4: Final backend closure verification

**Files:**
- No intended code changes
- Evidence target: `docs/superpowers/notes/2026-04-23-resource-crud-redesign-closure.md` only if the user later asks to refresh the closure note after implementation

- [ ] **Step 1: Run the unit/package verification set**

Run: `go test ./cmd/server ./internal/api ./internal/service -count=1`
Expected: PASS

- [ ] **Step 2: Run integration verification only if Docker is available and the user wants closure-grade extra confidence**

Run: `make test-integration`
Expected: PASS

- [ ] **Step 3: Record the exact closure evidence for handoff**

Capture these outputs in the implementation handoff message:

```text
- Router regression: embedded profile persisted on POST /resources
- Contract validation: make openapi-validate passed
- PATCH name: handler success path covered
- Profile write endpoints: route coverage present
```

- [ ] **Step 4: Commit remaining changes**

```bash
git status --short
git add cmd/server/main.go cmd/server/main_test.go internal/api/test_server.go internal/api/resource_handler_test.go internal/openapi/openapi.yaml
git commit -m "fix: close remaining resource CRUD backend gaps"
```

---

## Notes

- Keep this pass backend-only. Do not reopen frontend redesign work here.
- Do not redesign `ResourceService.Create`; the current closure goal is to prove wiring and contract alignment, not invent a new write flow.
- If tests reveal that embedded profile write failures are still swallowed in a way that breaks the expected API contract, stop and decide explicitly whether that belongs in this closure pass before broadening scope.
- If `internal/api/profile_handler.go` malformed JSON behavior diverges from the intended OpenAPI error contract, prefer the smallest handler/test adjustment that matches existing API conventions.
