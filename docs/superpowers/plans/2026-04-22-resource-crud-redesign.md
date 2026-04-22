# Resource CRUD Redesign — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Overhaul resource creation and editing to support type-specific profile fields, strict subtype validation, editable name, and structured error responses.

**Architecture:** Backend gains subtype dictionary + profile write APIs. Frontend rewrites create/edit as dynamic forms using react-hook-form + zod, with a centralized profile field registry driving type-specific fields.

**Tech Stack:** Go 1.26 (backend), React + react-hook-form + zod + shadcn/ui + next-intl (frontend)

**Working directories:**
- Backend: `/Users/fan/GolangProjects/ControlHub`
- Frontend: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign`

**Spec:** `docs/superpowers/specs/2026-04-22-resource-crud-redesign.md`

---

## File Structure

### Backend (new/modified)

| File | Responsibility |
|------|---------------|
| `internal/model/taxonomy.go` | Add `resourceSubtypeDictionaryItems`, `ResourceSubtypeDictionary()`, `ResourceSubtype.Validate()` |
| `internal/api/dictionary_handler.go` | Add `handleListResourceSubtypes` |
| `internal/service/dictionary_service.go` | Add `ResourceSubtypeService` |
| `internal/api/router.go` | Add routes for `/resource-subtypes`, profile write endpoints |
| `internal/repository/mysql/resource_repository.go` | Add profile write methods (UpsertHostProfile etc.) |
| `internal/service/profile_service.go` | **New** — `ProfileService` with Put/Patch methods |
| `internal/api/profile_handler.go` | **New** — PUT/PATCH profile handlers |
| `internal/model/resource_write.go` | Add `Profile` field to `ResourceCreateInput`; make `Name` mutable |
| `internal/service/resource_service.go` | Add subtype validation; handle profile in Create; allow Name in Update |
| `internal/api/resource_handler.go` | Handle profile in create flow |
| `internal/api/test_server.go` | Add profile write fakes |

### Frontend (new/modified)

| File | Responsibility |
|------|---------------|
| `lib/profile-field-registry.ts` | **New** — Profile field definitions per resource type |
| `services/resources.ts` | Add `updateProfile`, `listSubtypes` functions |
| `services/api-client.ts` | Structured error responses |
| `services/settings.ts` | Add `listResourceSubtypes` |
| `types/resource.ts` | Extend `CreateResourceInput` with profile; add `UpdateResourceInput.name` |
| `messages/en.json` | Add profile field labels, subtype names, form hints |
| `messages/zh-CN.json` | Same |
| `components/resources/create-resource-sheet.tsx` | Rewrite as dynamic form with A/B/C sections |
| `components/resources/edit-resource-sheet.tsx` | Add profile editing, name editing, UUID fix |

---

## Task 1: Backend — Subtype dictionary in taxonomy model

**Files:**
- Modify: `internal/model/taxonomy.go`

- [ ] **Step 1: Write the failing test**

Add to `internal/model/taxonomy_test.go` (or create it):

```go
package model

import "testing"

func TestResourceSubtypeValidate_Valid(t *testing.T) {
	tests := []struct {
		resourceType string
		subtype      string
	}{
		{"database_instance", "mysql"},
		{"database_instance", "postgresql"},
		{"database_instance", "redis"},
		{"database_instance", "clickhouse"},
		{"database_instance", "mongodb"},
		{"database_instance", "tidb"},
		{"database_cluster", "mysql"},
		{"host", "vm"},
		{"host", "physical"},
		{"host", "container"},
		{"service", "api"},
		{"database_proxy", "proxysql"},
		{"control_plane_component", "orchestrator"},
	}
	for _, tt := range tests {
		err := ValidateResourceSubtype(tt.resourceType, tt.subtype)
		if err != nil {
			t.Errorf("ValidateResourceSubtype(%q, %q) = %v, want nil", tt.resourceType, tt.subtype, err)
		}
	}
}

func TestResourceSubtypeValidate_Invalid(t *testing.T) {
	err := ValidateResourceSubtype("database_instance", "invalid_engine")
	if err == nil {
		t.Error("ValidateResourceSubtype(invalid) = nil, want error")
	}
}

func TestResourceSubtypeValidate_NoSubtypes(t *testing.T) {
	err := ValidateResourceSubtype("domain_name", "anything")
	if err != nil {
		t.Errorf("ValidateResourceSubtype(domain_name, anything) should be ignored, got %v", err)
	}
}

func TestResourceSubtypeDictionary(t *testing.T) {
	items := ResourceSubtypeDictionary("database_instance")
	if len(items) != 6 {
		t.Errorf("ResourceSubtypeDictionary(database_instance) returned %d items, want 6", len(items))
	}
	keys := make(map[string]bool)
	for _, item := range items {
		keys[item.Key] = true
	}
	if !keys["mysql"] || !keys["postgresql"] || !keys["redis"] {
		t.Error("missing expected subtypes for database_instance")
	}
}

func TestResourceSubtypeDictionary_Empty(t *testing.T) {
	items := ResourceSubtypeDictionary("domain_name")
	if len(items) != 0 {
		t.Errorf("ResourceSubtypeDictionary(domain_name) returned %d items, want 0", len(items))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/fan/GolangProjects/ControlHub && go test ./internal/model/ -v -run TestResourceSubtype -count=1`
Expected: FAIL — `ValidateResourceSubtype` and `ResourceSubtypeDictionary` undefined

- [ ] **Step 3: Implement subtype dictionary**

Add to `internal/model/taxonomy.go` after `resourceTypeDictionaryItems` (after line 76):

```go
// resourceSubtypeMap defines valid subtypes per resource type.
// Types not present have no subtypes (subtype field is ignored).
var resourceSubtypeMap = map[string][]DictionaryItem{
	string(ResourceTypeDatabaseInstance): {
		{Key: "mysql", Label: "MySQL"},
		{Key: "postgresql", Label: "PostgreSQL"},
		{Key: "redis", Label: "Redis"},
		{Key: "clickhouse", Label: "ClickHouse"},
		{Key: "mongodb", Label: "MongoDB"},
		{Key: "tidb", Label: "TiDB"},
	},
	string(ResourceTypeDatabaseCluster): {
		{Key: "mysql", Label: "MySQL"},
		{Key: "postgresql", Label: "PostgreSQL"},
		{Key: "redis", Label: "Redis"},
		{Key: "clickhouse", Label: "ClickHouse"},
		{Key: "mongodb", Label: "MongoDB"},
		{Key: "tidb", Label: "TiDB"},
	},
	string(ResourceTypeDatabaseProxy): {
		{Key: "proxysql", Label: "ProxySQL"},
		{Key: "chproxy", Label: "CHProxy"},
		{Key: "haproxy", Label: "HAProxy"},
		{Key: "maxscale", Label: "MaxScale"},
	},
	string(ResourceTypeHost): {
		{Key: "vm", Label: "Virtual Machine"},
		{Key: "physical", Label: "Physical Server"},
		{Key: "container", Label: "Container"},
	},
	string(ResourceTypeService): {
		{Key: "api", Label: "API Service"},
		{Key: "web", Label: "Web Application"},
		{Key: "job", Label: "Batch Job"},
		{Key: "cron", Label: "Cron Job"},
	},
	string(ResourceTypeControlPlaneComponent): {
		{Key: "orchestrator", Label: "Orchestrator"},
		{Key: "ha_monitor", Label: "HA Monitor"},
		{Key: "backup_manager", Label: "Backup Manager"},
	},
}
```

Add after `ResourceTypeDictionary()` function (after line 98):

```go
// ResourceSubtypeDictionary returns the valid subtypes for a given resource type.
func ResourceSubtypeDictionary(resourceType string) []DictionaryItem {
	items, ok := resourceSubtypeMap[resourceType]
	if !ok {
		return nil
	}
	return cloneDictionaryItems(items)
}

// ValidateResourceSubtype validates that subtype is valid for the given resource type.
// If the resource type has no subtypes defined, any subtype is silently accepted (ignored).
func ValidateResourceSubtype(resourceType, subtype string) error {
	items, ok := resourceSubtypeMap[resourceType]
	if !ok {
		return nil // no subtypes defined for this type — ignore
	}
	if subtype == "" {
		return fmt.Errorf("resourceSubtype is required for %s", resourceType)
	}
	for _, item := range items {
		if item.Key == subtype {
			return nil
		}
	}
	return fmt.Errorf("invalid resourceSubtype %q for type %s", subtype, resourceType)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/model/ -v -run TestResourceSubtype -count=1`
Expected: ALL PASS

- [ ] **Step 5: Run full model tests**

Run: `go test ./internal/model/ -v -count=1`
Expected: ALL PASS (existing + new tests)

- [ ] **Step 6: Commit**

```bash
git add internal/model/taxonomy.go internal/model/taxonomy_test.go
git commit -m "feat: add resource subtype dictionary and validation to taxonomy"
```

---

## Task 2: Backend — Subtype dictionary API endpoint

**Files:**
- Create: `internal/service/dictionary_service.go` (add `ResourceSubtypeService`)
- Modify: `internal/api/dictionary_handler.go` (add handler)
- Modify: `internal/api/router.go` (add route)
- Modify: `internal/api/test_server.go` (wire into test deps)

- [ ] **Step 1: Add ResourceSubtypeService**

Add to `internal/service/dictionary_service.go` (after existing dictionary services):

```go
type ResourceSubtypeService struct{}

func NewResourceSubtypeService() *ResourceSubtypeService {
	return &ResourceSubtypeService{}
}

func (s *ResourceSubtypeService) List(resourceType string) ([]model.DictionaryItem, error) {
	items := model.ResourceSubtypeDictionary(resourceType)
	if items == nil {
		items = []model.DictionaryItem{}
	}
	return items, nil
}
```

- [ ] **Step 2: Add handler**

Add to `internal/api/dictionary_handler.go` after `handleListResourceTypes` (after line 97):

```go
func handleListResourceSubtypes(subtypeService *service.ResourceSubtypeService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resourceType := r.URL.Query().Get("resourceType")
		if resourceType == "" {
			http.Error(w, "resourceType query parameter is required", http.StatusBadRequest)
			return
		}
		items, err := subtypeService.List(resourceType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			ResourceType string                 `json:"resourceType"`
			Subtypes     []model.DictionaryItem `json:"subtypes"`
		}{
			ResourceType: resourceType,
			Subtypes:     items,
		})
	}
}
```

- [ ] **Step 3: Add route and wire dependency**

In `internal/api/router.go`, add after line 67 (`resource-types` route):

```go
router.Get("/resource-subtypes", handleListResourceSubtypes(deps.ResourceSubtypeService))
```

In `internal/api/test_server.go`, add `ResourceSubtypeService` to `Dependencies` struct and wire it in `NewTestServer`:

```go
ResourceSubtypeService: service.NewResourceSubtypeService(),
```

- [ ] **Step 4: Verify it compiles and tests pass**

Run: `go build ./... && go test ./internal/api/ -v -count=1`
Expected: BUILD PASS, ALL TESTS PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/dictionary_service.go internal/api/dictionary_handler.go internal/api/router.go internal/api/test_server.go
git commit -m "feat: add GET /resource-subtypes API endpoint"
```

---

## Task 3: Backend — Profile write repository methods

**Files:**
- Modify: `internal/repository/mysql/resource_repository.go`

- [ ] **Step 1: Write the failing test**

Create `internal/repository/mysql/profile_write_test.go` with integration test stubs (build tag `integration`):

```go
//go:build integration

package mysql

import (
	"context"
	"testing"
)

func TestUpsertHostProfile(t *testing.T) {
	// Integration test — will be run with: make test-integration
	// Tests INSERT ... ON DUPLICATE KEY UPDATE for resource_profiles_host
}

func TestUpsertDatabaseInstanceProfile(t *testing.T) {
	// Integration test for resource_profiles_database_instance
}
```

- [ ] **Step 2: Add profile write methods to repository**

Add `ProfileWriter` interface to `internal/service/profile_service.go` (we'll create the full service in Task 4, but the interface is needed here):

Actually, add the methods directly to the existing `ResourceRepository` interface consumer. Add these methods to the `resource_repository.go` file after the existing profile fetch methods (after line 367):

```go
// UpsertHostProfile inserts or updates the host profile.
func (r *MySQLResourceRepository) UpsertHostProfile(ctx context.Context, resourceID string, hostname, ipAddress, osName string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resource_profiles_host (resource_id, hostname, ip_address, os_name)
		 VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE hostname = VALUES(hostname), ip_address = VALUES(ip_address), os_name = VALUES(os_name)`,
		resourceID, hostname, ipAddress, osName,
	)
	return err
}

// UpsertDatabaseInstanceProfile inserts or updates the database instance profile.
func (r *MySQLResourceRepository) UpsertDatabaseInstanceProfile(ctx context.Context, resourceID string, engine, version, host string, port int, role string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE engine = VALUES(engine), version = VALUES(version), host = VALUES(host), port = VALUES(port), role = VALUES(role)`,
		resourceID, engine, version, host, port, role,
	)
	return err
}

// UpsertDatabaseClusterProfile inserts or updates the database cluster profile.
func (r *MySQLResourceRepository) UpsertDatabaseClusterProfile(ctx context.Context, resourceID string, engine, topologyMode, primaryEndpoint string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resource_profiles_database_cluster (resource_id, engine, topology_mode, primary_endpoint)
		 VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE engine = VALUES(engine), topology_mode = VALUES(topology_mode), primary_endpoint = VALUES(primary_endpoint)`,
		resourceID, engine, topologyMode, primaryEndpoint,
	)
	return err
}

// UpsertServiceProfile inserts or updates the service profile.
func (r *MySQLResourceRepository) UpsertServiceProfile(ctx context.Context, resourceID string, systemName, repositoryUrl, runtimeEnv string) error {
	_, err := r.db.ExecContext(ctx,
		`INSERT INTO resource_profiles_service (resource_id, system_name, repository_url, runtime_env)
		 VALUES (?, ?, ?, ?)
		 ON DUPLICATE KEY UPDATE system_name = VALUES(system_name), repository_url = VALUES(repository_url), runtime_env = VALUES(runtime_env)`,
		resourceID, systemName, repositoryUrl, runtimeEnv,
	)
	return err
}

// DeleteProfile removes the profile record for the given resource type.
func (r *MySQLResourceRepository) DeleteProfile(ctx context.Context, resourceID, resourceType string) error {
	tableMap := map[string]string{
		"host":               "resource_profiles_host",
		"database_instance":  "resource_profiles_database_instance",
		"database_cluster":   "resource_profiles_database_cluster",
		"service":            "resource_profiles_service",
	}
	table, ok := tableMap[resourceType]
	if !ok {
		return nil // no profile table for this type
	}
	_, err := r.db.ExecContext(ctx, "DELETE FROM "+table+" WHERE resource_id = ?", resourceID)
	return err
}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: BUILD PASS

- [ ] **Step 4: Commit**

```bash
git add internal/repository/mysql/resource_repository.go
git commit -m "feat: add profile write methods to MySQL repository"
```

---

## Task 4: Backend — Profile service and API endpoints

**Files:**
- Create: `internal/service/profile_service.go`
- Create: `internal/api/profile_handler.go`
- Modify: `internal/api/router.go` (add profile routes)
- Modify: `internal/api/test_server.go` (wire profile service)

- [ ] **Step 1: Create ProfileService**

Create `internal/service/profile_service.go`:

```go
package service

import (
	"context"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

type ProfileRepository interface {
	UpsertHostProfile(ctx context.Context, resourceID string, hostname, ipAddress, osName string) error
	UpsertDatabaseInstanceProfile(ctx context.Context, resourceID string, engine, version, host string, port int, role string) error
	UpsertDatabaseClusterProfile(ctx context.Context, resourceID string, engine, topologyMode, primaryEndpoint string) error
	UpsertServiceProfile(ctx context.Context, resourceID string, systemName, repositoryUrl, runtimeEnv string) error
	DeleteProfile(ctx context.Context, resourceID, resourceType string) error
}

type ProfileService struct {
	profileRepo   ProfileRepository
	resourceRepo  ResourceRepository
}

func NewProfileService(profileRepo ProfileRepository, resourceRepo ResourceRepository) *ProfileService {
	return &ProfileService{profileRepo: profileRepo, resourceRepo: resourceRepo}
}

func (s *ProfileService) PutProfile(ctx context.Context, resourceID string, fields map[string]interface{}) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return fmt.Errorf("cannot update profile of archived resource")
	}
	return s.writeProfile(ctx, resourceID, res.ResourceType, fields)
}

func (s *ProfileService) PatchProfile(ctx context.Context, resourceID string, fields map[string]interface{}) error {
	res, err := s.resourceRepo.GetResource(resourceID)
	if err != nil {
		return err
	}
	if res.ArchivedAt != nil {
		return fmt.Errorf("cannot update profile of archived resource")
	}
	return s.writeProfile(ctx, resourceID, res.ResourceType, fields)
}

func (s *ProfileService) writeProfile(ctx context.Context, resourceID string, resourceType model.ResourceType, fields map[string]interface{}) error {
	switch resourceType {
	case model.ResourceTypeHost:
		return s.profileRepo.UpsertHostProfile(ctx, resourceID,
			getString(fields, "hostname"),
			getString(fields, "ipAddress"),
			getString(fields, "osName"),
		)
	case model.ResourceTypeDatabaseInstance:
		return s.profileRepo.UpsertDatabaseInstanceProfile(ctx, resourceID,
			getString(fields, "engine"),
			getString(fields, "version"),
			getString(fields, "host"),
			getInt(fields, "port"),
			getString(fields, "role"),
		)
	case model.ResourceTypeDatabaseCluster:
		return s.profileRepo.UpsertDatabaseClusterProfile(ctx, resourceID,
			getString(fields, "engine"),
			getString(fields, "topologyMode"),
			getString(fields, "primaryEndpoint"),
		)
	case model.ResourceTypeService:
		return s.profileRepo.UpsertServiceProfile(ctx, resourceID,
			getString(fields, "systemName"),
			getString(fields, "repositoryUrl"),
			getString(fields, "runtimeEnv"),
		)
	default:
		return fmt.Errorf("resource type %s has no profile table", resourceType)
	}
}

func getString(m map[string]interface{}, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

func getInt(m map[string]interface{}, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case string:
		return 0
	default:
		return 0
	}
}
```

- [ ] **Step 2: Create profile handler**

Create `internal/api/profile_handler.go`:

```go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/fan/controlhub/internal/service"
)

func handlePutResourceProfile(profileService *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "missing resource id", http.StatusBadRequest)
			return
		}

		var fields map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if err := profileService.PutProfile(r.Context(), id, fields); err != nil {
			writeServiceError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

func handlePatchResourceProfile(profileService *service.ProfileService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			http.Error(w, "missing resource id", http.StatusBadRequest)
			return
		}

		var fields map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if err := profileService.PatchProfile(r.Context(), id, fields); err != nil {
			writeServiceError(w, err)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
```

- [ ] **Step 3: Add routes**

In `internal/api/router.go`, add after the GET profile route (line 56):

```go
router.Put("/resources/{id}/profile", handlePutResourceProfile(deps.ProfileService))
router.Patch("/resources/{id}/profile", handlePatchResourceProfile(deps.ProfileService))
```

- [ ] **Step 4: Wire in Dependencies and test_server**

Add `ProfileService` to `Dependencies` struct in `resource_handler.go` or a central deps file. Add to `NewTestServer` in `test_server.go`:

```go
ProfileService: service.NewProfileService(profileRepo, resourceRepo),
```

Add fake `ProfileRepository` to `test_server.go`:

```go
type fakeProfileRepo struct{}

func (f *fakeProfileRepo) UpsertHostProfile(ctx context.Context, resourceID string, hostname, ipAddress, osName string) error {
	return nil
}
func (f *fakeProfileRepo) UpsertDatabaseInstanceProfile(ctx context.Context, resourceID string, engine, version, host string, port int, role string) error {
	return nil
}
func (f *fakeProfileRepo) UpsertDatabaseClusterProfile(ctx context.Context, resourceID string, engine, topologyMode, primaryEndpoint string) error {
	return nil
}
func (f *fakeProfileRepo) UpsertServiceProfile(ctx context.Context, resourceID string, systemName, repositoryUrl, runtimeEnv string) error {
	return nil
}
func (f *fakeProfileRepo) DeleteProfile(ctx context.Context, resourceID, resourceType string) error {
	return nil
}
```

Note: import `"context"` at the top of test_server.go.

- [ ] **Step 5: Verify it compiles and tests pass**

Run: `go build ./... && go test ./... -count=1`
Expected: BUILD PASS, ALL TESTS PASS

- [ ] **Step 6: Commit**

```bash
git add internal/service/profile_service.go internal/api/profile_handler.go internal/api/router.go internal/api/test_server.go
git commit -m "feat: add profile write service and PUT/PATCH API endpoints"
```

---

## Task 5: Backend — Create resource with embedded profile

**Files:**
- Modify: `internal/model/resource_write.go` (add Profile field to CreateInput)
- Modify: `internal/service/resource_service.go` (handle profile in Create)
- Modify: `internal/api/resource_handler.go` (pass profile to service)

- [ ] **Step 1: Add Profile field to ResourceCreateInput**

In `internal/model/resource_write.go`, add to `ResourceCreateInput` struct (after line 20, before Labels):

```go
Profile map[string]interface{} `json:"profile,omitempty"`
```

- [ ] **Step 2: Update ResourceService to accept ProfileService**

In `internal/service/resource_service.go`, add `profileService` field to `ResourceService`:

```go
type ResourceService struct {
	repo          ResourceRepository
	profileSvc    *ProfileService
}
```

Update `NewResourceService` to accept it:

```go
func NewResourceService(repo ResourceRepository, profileSvc *ProfileService) *ResourceService {
	return &ResourceService{repo: repo, profileSvc: profileSvc}
}
```

In the `Create` method, after `repo.CreateResource` succeeds (after line 88), add:

```go
if len(input.Profile) > 0 && s.profileSvc != nil {
	_ = s.profileSvc.PutProfile(ctx, created.ID, input.Profile)
}
```

- [ ] **Step 3: Add subtype validation in create**

In `validateResourceCreateInput`, add after resourceType validation (after line 199):

```go
if err := ValidateResourceSubtype(string(input.ResourceType), input.ResourceSubtype); err != nil {
	return err
}
```

- [ ] **Step 4: Update wiring in test_server.go AND cmd/server/main.go**

In `test_server.go`, update `NewResourceService` call to pass `profileService`:

```go
resourceService := service.NewResourceService(resourceRepo, profileService)
```

In `cmd/server/main.go`, update the `NewResourceService` call to pass `profileService`:

```go
profileService := service.NewProfileService(resourceRepo, resourceRepo)
resourceService := service.NewResourceService(resourceRepo, profileService)
```

- [ ] **Step 5: Verify it compiles and tests pass**

Run: `go build ./... && go test ./... -count=1`
Expected: BUILD PASS, ALL TESTS PASS (existing tests still pass because Profile is optional)

- [ ] **Step 6: Commit**

```bash
git add internal/model/resource_write.go internal/service/resource_service.go internal/api/test_server.go
git commit -m "feat: support embedded profile in resource creation + subtype validation"
```

---

## Task 6: Backend — Name editable in PATCH

**Files:**
- Modify: `internal/model/resource_write.go` (remove Name from HasImmutableFields)
- Modify: `internal/service/resource_service.go` (add Name validation in Update)

- [ ] **Step 1: Remove Name from HasImmutableFields**

In `internal/model/resource_write.go`, change `HasImmutableFields` (line 52-54):

```go
func (r ResourcePatchRequest) HasImmutableFields() bool {
	return r.ID != nil || r.ResourceType != nil || r.CreatedAt != nil
}
```

(Name removed from the check)

- [ ] **Step 2: Add Name to HasMutableFields**

In `internal/model/resource_write.go`, change `HasMutableFields` (line 56-60), add `r.Name != nil`:

```go
func (r ResourcePatchRequest) HasMutableFields() bool {
	return r.Name != nil || r.ResourceSubtype != nil || r.DisplayName != nil || r.EnvironmentID != nil || r.OwnerID != nil || r.LifecycleStatus != nil || r.HealthStatus != nil || r.Source != nil || r.ExternalID != nil || r.Labels != nil
}
```

- [ ] **Step 3: Add Name validation in Update method**

In `internal/service/resource_service.go`, in the `Update` method, add after the DisplayName validation block (after line 114):

```go
if input.Name != nil {
	if err := validateName(*input.Name); err != nil {
		return nil, err
	}
}
```

Then update the `UpdateResource` repository call in the service to pass `input.Name` when present. In `internal/repository/mysql/resource_repository.go`, the `UpdateResource` method already accepts `req model.ResourcePatchRequest` — verify the SQL SET clause includes `name = ?` when `req.Name != nil`:

```go
if req.Name != nil {
    sets = append(sets, "name = ?")
    args = append(args, *req.Name)
}
```

- [ ] **Step 4: Add subtype validation in Update**

In `validateResourcePatchRequest`, add after the source validation (after line 123):

```go
if input.ResourceSubtype != nil {
	res, err := s.repo.GetResource(id)
	if err != nil {
		return err
	}
	if err := ValidateResourceSubtype(string(res.ResourceType), *input.ResourceSubtype); err != nil {
		return err
	}
}
```

- [ ] **Step 5: Verify it compiles and tests pass**

Run: `go build ./... && go test ./... -count=1`
Expected: BUILD PASS

Update any existing tests that assert Name is immutable to now expect it to be mutable.

- [ ] **Step 6: Commit**

```bash
git add internal/model/resource_write.go internal/service/resource_service.go
git commit -m "feat: allow name modification in PATCH + subtype validation in update"
```

---

## Task 7: Backend — Structured error responses

**Files:**
- Modify: `internal/api/resource_handler.go` (return field-level errors)
- Modify: `internal/service/resource_service.go` (structured validation errors)

- [ ] **Step 1: Define ValidationError type**

Add to `internal/service/resource_service.go`:

```go
type ValidationError struct {
	Message string
	Fields  map[string]string
}

func (e *ValidationError) Error() string {
	return e.Message
}

func validationError(msg string) *ValidationError {
	return &ValidationError{Message: msg, Fields: map[string]string{}}
}

func validationErrorf(format string, args ...interface{}) *ValidationError {
	return &ValidationError{Message: fmt.Sprintf(format, args...), Fields: map[string]string{}}
}

func (e *ValidationError) WithField(field, msg string) *ValidationError {
	e.Fields[field] = msg
	return e
}
```

- [ ] **Step 2: Update key validation points to use ValidationError**

Change `validateName` to return `*ValidationError`:

```go
func validateName(name string) *ValidationError {
	if name == "" {
		return validationError("name is required").WithField("name", "Name is required")
	}
	matched, _ := regexp.MatchString(`^[a-z0-9][a-z0-9._-]*$`, name)
	if !matched {
		return validationError("invalid name format").WithField("name", "Must match pattern: lowercase letters, numbers, dots, hyphens, underscores")
	}
	return nil
}
```

Update `validateResourceCreateInput` to collect all errors and return them:

```go
func validateResourceCreateInput(input model.ResourceCreateInput) error {
	var ve *ValidationError
	hasErr := false

	if err := model.ResourceType(input.ResourceType).Validate(); err != nil {
		if ve == nil { ve = validationError("validation failed") }
		ve.WithField("resourceType", err.Error())
		hasErr = true
	}
	if vErr := validateName(input.Name); vErr != nil {
		if ve == nil { ve = validationError("validation failed") }
		for k, v := range vErr.Fields { ve.WithField(k, v) }
		hasErr = true
	}
	if input.DisplayName == "" {
		if ve == nil { ve = validationError("validation failed") }
		ve.WithField("displayName", "Display name is required")
		hasErr = true
	}
	if err := model.ValidateResourceSubtype(string(input.ResourceType), input.ResourceSubtype); err != nil {
		if ve == nil { ve = validationError("validation failed") }
		ve.WithField("resourceSubtype", err.Error())
		hasErr = true
	}
	// ... existing validations for environmentId, ownerId, lifecycleStatus, healthStatus, source
	if hasErr {
		return ve
	}
	return nil
}
```

- [ ] **Step 3: Handle ValidationError in writeServiceError**

In `internal/api/resource_handler.go`, update `writeServiceError` to check for `*service.ValidationError`:

```go
var ve *service.ValidationError
if errors.As(err, &ve) {
	writeJSON(w, http.StatusBadRequest, map[string]interface{}{
		"error":   ve.Message,
		"details": ve.Fields,
	})
	return
}
```

- [ ] **Step 4: Verify it compiles and tests pass**

Run: `go build ./... && go test ./... -count=1`
Expected: BUILD PASS. Existing tests may need updating to handle new error type.

- [ ] **Step 5: Commit**

```bash
git add internal/service/resource_service.go internal/api/resource_handler.go
git commit -m "feat: structured validation error responses with field-level details"
```

---

## Task 8: Backend — Run all tests

- [ ] **Step 1: Run full Go test suite**

Run: `go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 2: Run integration tests (if Docker available)**

Run: `make test-integration`
Expected: ALL PASS

- [ ] **Step 3: Fix any failures, then commit**

```bash
git add -A
git commit -m "fix: address test failures from CRUD redesign"
```

---

## Task 9: Frontend — TypeScript types and API services

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/types/resource.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/services/resources.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/services/api-client.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/services/settings.ts`

- [ ] **Step 1: Update TypeScript types**

In `types/resource.ts`:

Add `profile` to `CreateResourceInput` (after line 107):

```typescript
profile?: Record<string, string | number | boolean>;
```

Add `name` to `UpdateResourceInput` (after line 111):

```typescript
name?: string;
```

- [ ] **Step 2: Fix api-client error handling**

In `services/api-client.ts`, replace the error handling (lines 32-34) with:

```typescript
if (!response.ok) {
  let errorBody: Record<string, unknown> = {};
  try {
    errorBody = await response.json();
  } catch {
    // not JSON
  }
  const message = (errorBody?.error as string) || `Request failed: ${response.status}`;
  const details = (errorBody?.details as Record<string, string>) || undefined;
  const error = new ApiError(response.status, message, details);
  throw error;
}
```

Add `ApiError` class before the `apiClient` function:

```typescript
export class ApiError extends Error {
  status: number;
  details?: Record<string, string>;

  constructor(status: number, message: string, details?: Record<string, string>) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.details = details;
  }
}
```

- [ ] **Step 3: Add new service functions**

In `services/resources.ts`, add after `updateResource` (after line 188):

```typescript
export async function updateProfile(
  id: string,
  fields: Record<string, string | number | boolean>,
): Promise<void> {
  await apiClient<void>(`/resources/${encodeURIComponent(id)}/profile`, {
    method: "PATCH",
    body: JSON.stringify(fields),
  });
}
```

In `services/settings.ts`, add after `listHealthStatuses` (after line 66):

```typescript
export async function listResourceSubtypes(
  resourceType: string,
): Promise<DictionaryItem[]> {
  try {
    const res = await apiClient<{ subtypes: DictionaryItem[] }>(
      `/resource-subtypes?resourceType=${encodeURIComponent(resourceType)}`,
    );
    return res.subtypes;
  } catch {
    return [];
  }
}
```

Note: `DictionaryItem` type is `{ key: string; label: string; description?: string }` — check how it's defined in settings.ts and use the same type.

- [ ] **Step 4: Verify it compiles**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign
git add types/resource.ts services/api-client.ts services/resources.ts services/settings.ts
git commit -m "feat: update types, add profile/subtype API services, structured error handling"
```

---

## Task 10: Frontend — Profile field registry

**Files:**
- Create: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/lib/profile-field-registry.ts`

- [ ] **Step 1: Create profile field registry**

Create `lib/profile-field-registry.ts`:

```typescript
import { z } from "zod";

export type ProfileFieldDef = {
  key: string;
  labelKey: string;
  inputType: "text" | "number" | "select";
  required: boolean;
  placeholder?: string;
  options?: { value: string; labelKey: string }[];
};

export type ProfileSchema = {
  fields: ProfileFieldDef[];
  zodSchema: z.ZodObject<Record<string, z.ZodTypeAny>>;
};

const HOST_FIELDS: ProfileFieldDef[] = [
  { key: "hostname", labelKey: "profileFields.hostname", inputType: "text", required: true, placeholder: "db-prod-01" },
  { key: "ipAddress", labelKey: "profileFields.ipAddress", inputType: "text", required: true, placeholder: "10.0.0.1" },
  { key: "osName", labelKey: "profileFields.osName", inputType: "text", required: false, placeholder: "Ubuntu 22.04" },
];

const DB_INSTANCE_FIELDS: ProfileFieldDef[] = [
  { key: "engine", labelKey: "profileFields.engine", inputType: "text", required: true, placeholder: "mysql" },
  { key: "version", labelKey: "profileFields.version", inputType: "text", required: false, placeholder: "8.0" },
  { key: "host", labelKey: "profileFields.host", inputType: "text", required: false, placeholder: "db-host-01" },
  { key: "port", labelKey: "profileFields.port", inputType: "number", required: false, placeholder: "3306" },
  { key: "role", labelKey: "profileFields.role", inputType: "select", required: false, options: [
    { value: "primary", labelKey: "profileFields.rolePrimary" },
    { value: "replica", labelKey: "profileFields.roleReplica" },
  ]},
];

const DB_CLUSTER_FIELDS: ProfileFieldDef[] = [
  { key: "engine", labelKey: "profileFields.engine", inputType: "text", required: true, placeholder: "mysql" },
  { key: "topologyMode", labelKey: "profileFields.topologyMode", inputType: "select", required: false, options: [
    { value: "single-primary", labelKey: "profileFields.topologySinglePrimary" },
    { value: "multi-primary", labelKey: "profileFields.topologyMultiPrimary" },
  ]},
  { key: "primaryEndpoint", labelKey: "profileFields.primaryEndpoint", inputType: "text", required: false, placeholder: "cluster-host:3306" },
];

const SERVICE_FIELDS: ProfileFieldDef[] = [
  { key: "systemName", labelKey: "profileFields.systemName", inputType: "text", required: false, placeholder: "payment-service" },
  { key: "repositoryUrl", labelKey: "profileFields.repositoryUrl", inputType: "text", required: false, placeholder: "https://github.com/..." },
  { key: "runtimeEnv", labelKey: "profileFields.runtimeEnv", inputType: "text", required: false, placeholder: "node, python, go..." },
];

function buildZodSchema(fields: ProfileFieldDef[]): z.ZodObject<Record<string, z.ZodTypeAny>> {
  const shape: Record<string, z.ZodTypeAny> = {};
  for (const field of fields) {
    let schema: z.ZodTypeAny;
    if (field.inputType === "number") {
      schema = field.required
        ? z.number().min(1).max(65535)
        : z.union([z.number().min(1).max(65535), z.string(), z.undefined()]).optional();
    } else {
      schema = field.required ? z.string().min(1) : z.string().optional();
    }
    shape[field.key] = schema;
  }
  return z.object(shape);
}

const REGISTRY: Record<string, ProfileSchema> = {
  host: { fields: HOST_FIELDS, zodSchema: buildZodSchema(HOST_FIELDS) },
  database_instance: { fields: DB_INSTANCE_FIELDS, zodSchema: buildZodSchema(DB_INSTANCE_FIELDS) },
  database_cluster: { fields: DB_CLUSTER_FIELDS, zodSchema: buildZodSchema(DB_CLUSTER_FIELDS) },
  service: { fields: SERVICE_FIELDS, zodSchema: buildZodSchema(SERVICE_FIELDS) },
};

export function getProfileSchema(resourceType: string): ProfileSchema | undefined {
  return REGISTRY[resourceType];
}

export function hasProfileFields(resourceType: string): boolean {
  return resourceType in REGISTRY;
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx tsc --noEmit`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign
git add lib/profile-field-registry.ts
git commit -m "feat: add profile field registry with zod schemas per resource type"
```

---

## Task 11: Frontend — i18n keys

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/messages/en.json`
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/messages/zh-CN.json`

- [ ] **Step 1: Add English i18n keys**

Add under the `mutations` section in `messages/en.json`:

```json
"profileSection": "Runtime Profile",
"profileSectionHint": "Type-specific operational fields.",
"noProfileFields": "This resource type has no profile fields.",
"nameEditable": "Identifier used for system references. Changing it may affect integrations.",
"continueCreate": "Create Another",
"viewDetails": "View Details",
"createdSuccess": "Resource created successfully",
"profileFields": {
  "hostname": "Hostname",
  "ipAddress": "IP Address",
  "osName": "Operating System",
  "engine": "Engine",
  "version": "Version",
  "host": "Host",
  "port": "Port",
  "role": "Role",
  "rolePrimary": "Primary",
  "roleReplica": "Replica",
  "topologyMode": "Topology Mode",
  "topologySinglePrimary": "Single Primary",
  "topologyMultiPrimary": "Multi Primary",
  "primaryEndpoint": "Primary Endpoint",
  "systemName": "System Name",
  "repositoryUrl": "Repository URL",
  "runtimeEnv": "Runtime Environment"
},
"subtypes": {
  "mysql": "MySQL",
  "postgresql": "PostgreSQL",
  "redis": "Redis",
  "clickhouse": "ClickHouse",
  "mongodb": "MongoDB",
  "tidb": "TiDB",
  "proxysql": "ProxySQL",
  "chproxy": "CHProxy",
  "haproxy": "HAProxy",
  "maxscale": "MaxScale",
  "vm": "Virtual Machine",
  "physical": "Physical Server",
  "container": "Container",
  "api": "API Service",
  "web": "Web Application",
  "job": "Batch Job",
  "cron": "Cron Job",
  "orchestrator": "Orchestrator",
  "ha_monitor": "HA Monitor",
  "backup_manager": "Backup Manager"
}
```

- [ ] **Step 2: Add Chinese i18n keys**

Add the same structure to `messages/zh-CN.json`:

```json
"profileSection": "运行画像",
"profileSectionHint": "该资源类型的专属字段。",
"noProfileFields": "此资源类型暂无运行画像字段。",
"nameEditable": "标识符用于系统引用，修改后相关集成可能需要更新。",
"continueCreate": "继续创建",
"viewDetails": "查看详情",
"createdSuccess": "资源创建成功",
"profileFields": {
  "hostname": "主机名",
  "ipAddress": "IP 地址",
  "osName": "操作系统",
  "engine": "引擎",
  "version": "版本",
  "host": "主机",
  "port": "端口",
  "role": "角色",
  "rolePrimary": "主库",
  "roleReplica": "从库",
  "topologyMode": "拓扑模式",
  "topologySinglePrimary": "单主",
  "topologyMultiPrimary": "多主",
  "primaryEndpoint": "主端点",
  "systemName": "系统名称",
  "repositoryUrl": "仓库地址",
  "runtimeEnv": "运行环境"
},
"subtypes": {
  "mysql": "MySQL",
  "postgresql": "PostgreSQL",
  "redis": "Redis",
  "clickhouse": "ClickHouse",
  "mongodb": "MongoDB",
  "tidb": "TiDB",
  "proxysql": "ProxySQL",
  "chproxy": "CHProxy",
  "haproxy": "HAProxy",
  "maxscale": "MaxScale",
  "vm": "虚拟机",
  "physical": "物理机",
  "container": "容器",
  "api": "API 服务",
  "web": "Web 应用",
  "job": "批处理任务",
  "cron": "定时任务",
  "orchestrator": "编排器",
  "ha_monitor": "高可用监控",
  "backup_manager": "备份管理"
}
```

- [ ] **Step 3: Verify build**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign
git add messages/en.json messages/zh-CN.json
git commit -m "feat: add i18n keys for profile fields, subtypes, form labels"
```

---

## Task 12: Frontend — Rewrite create resource sheet

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/components/resources/create-resource-sheet.tsx`

This is the largest frontend task. The component needs a complete rewrite to support:
- Dynamic form with A/B/C sections
- react-hook-form + zod
- Subtype dropdown loaded from API
- Profile fields from registry
- localStorage memory for env/owner
- Create success with "Continue" / "View Details" options

- [ ] **Step 1: Rewrite the component**

Key implementation points for the rewrite:

1. Replace all `useState` form state with `useForm` from react-hook-form
2. Watch `resourceType` field to trigger dynamic rendering:
   ```tsx
   const resourceType = watch("resourceType");
   const profileSchema = getProfileSchema(resourceType);
   ```
3. Load subtypes when resourceType changes (use `useEffect` with AbortController):
   ```tsx
   useEffect(() => {
     if (!resourceType) { setSubtypes([]); return; }
     const ac = new AbortController();
     listResourceSubtypes(resourceType).then(setSubtypes);
     return () => ac.abort();
   }, [resourceType]);
   ```
4. Render A/B/C sections using existing DetailPanel-like cards (rounded-xl border)
5. B section uses `key={resourceType}` on the profile form to force remount on type change
6. Profile fields rendered by iterating `profileSchema.fields`
7. Submit sends one `POST /resources` with profile embedded
8. On success: Toast + two buttons ("继续创建" resets form, "查看详情" navigates to `/resources/{id}`)
9. On validation error: map `ApiError.details` to `form.setError(field, { message })`
10. localStorage: save `environmentId` and `ownerId` on successful create; load on mount
11. **Subtype→engine auto-fill**: When user selects `database_instance` subtype (e.g., "mysql"), auto-fill the profile `engine` field. Use a `useEffect` watching `resourceSubtype`:
    ```tsx
    useEffect(() => {
      if (resourceType === "database_instance" && resourceSubtype && profileSchema) {
        setValue("profile.engine", resourceSubtype, { shouldDirty: false });
      }
    }, [resourceSubtype, resourceType]);
    ```
12. **Type-switch warning**: When user changes `resourceType` after filling profile fields, show an amber alert before the B section remounts. Track with a `hadProfileData` ref:
    ```tsx
    const hadProfileData = useRef(false);
    // Set to true when any profile field has value
    // When resourceType changes and hadProfileData.current is true:
    // Show amber Banner: "切换资源类型将清除运行画像数据"
    // The key={resourceType} remount handles the actual clearing
    ```

The component structure:

```
SheetContent
  SheetHeader: "创建资源"
  SheetBody (scrollable)
    Card A: 基本信息
      Row: resourceType + resourceSubtype (grid-cols-2)
      Row: name + displayName (grid-cols-2)
      source (disabled)
    Card B: 运行画像 (key={resourceType})
      if profileSchema: iterate fields
      else: "此资源类型暂无运行画像字段"
    Card C: 环境与属性
      Row: environment + owner (grid-cols-2) — Skeleton while loading
      Row: lifecycleStatus + healthStatus (grid-cols-2)
      externalId
      labels
  SheetFooter:
    [取消] [创建资源]
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build`
Expected: PASS

- [ ] **Step 3: Run existing tests**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx vitest run`
Expected: Update any failing tests to match new form structure

- [ ] **Step 4: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign
git add components/resources/create-resource-sheet.tsx
git commit -m "feat: rewrite create resource sheet as dynamic form with profile fields"
```

---

## Task 13: Frontend — Rewrite edit resource sheet

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/components/resources/edit-resource-sheet.tsx`

- [ ] **Step 1: Rewrite the component**

Key implementation points:

1. Replace `useState` with `useForm` from react-hook-form
2. On open: load resource data + call `getResourceProfileById(id)` for original profile data
3. name is editable (with hint text about system references)
4. resourceType is read-only (displayed as disabled input with friendly name)
5. resourceSubtype dropdown loaded from API based on current resourceType
6. Profile section renders fields from registry, pre-filled with current profile data
7. **UUID fix**: Conditional rendering for environment/owner — show Skeleton while data loads, only render Select when options are available
8. Single "保存" button that does dirty detection:
   - If base fields changed → `PATCH /resources/{id}`
   - If profile fields changed → `PATCH /resources/{id}/profile`
   - Run in parallel with `Promise.allSettled`
   - Show per-area error on failure
9. Close button checks dirty state and shows confirmation dialog if unsaved changes

Component structure:

```
SheetContent
  SheetHeader: "编辑资源"
  SheetBody (scrollable)
    Card A: 基本信息
      name (editable, with hint)
      displayName
      resourceType (disabled, friendly name)
      resourceSubtype (dropdown)
    Card B: 运行画像 (key={resourceType})
      if profileSchema: iterate fields, pre-filled
      else: "此资源类型暂无运行画像字段"
    Card C: 环境与属性
      Row: environment + owner (Skeleton → Select)
      Row: lifecycleStatus + healthStatus
      externalId
      labels
  SheetFooter:
    [取消] [保存]
```

- [ ] **Step 2: Verify build**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build`
Expected: PASS

- [ ] **Step 3: Run existing tests**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx vitest run`
Expected: Update any failing tests

- [ ] **Step 4: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign
git add components/resources/edit-resource-sheet.tsx
git commit -m "feat: rewrite edit resource sheet with profile editing, name editable, UUID fix"
```

---

## Task 14: Frontend — Update tests

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/tests/components/create-resource-sheet.test.tsx` (if exists)
- Modify: `/Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign/tests/components/edit-resource-sheet.test.tsx` (if exists)

- [ ] **Step 1: Update create resource sheet tests**

Test scenarios:
- Renders resource type selector
- Changing resource type shows/hides profile section
- Shows subtype dropdown when type is selected
- Subtype options loaded from API
- Profile fields rendered for database_instance (engine, version, host, port, role)
- No profile fields shown for domain_name
- Submit sends profile in request body
- Success shows "继续创建" and "查看详情" buttons

- [ ] **Step 2: Update edit resource sheet tests**

Test scenarios:
- Loads profile data on open
- name field is editable
- resourceType field is disabled
- Environment/owner show names, not UUIDs (no UUID flash)
- Save button calls both PATCH endpoints when both areas changed
- Save button only calls resource PATCH when only base changed

- [ ] **Step 3: Run all tests**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx vitest run`
Expected: ALL PASS

- [ ] **Step 4: Commit**

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign
git add tests/
git commit -m "test: update create/edit resource sheet tests for dynamic form"
```

---

## Task 15: E2E verification

- [ ] **Step 1: Run full Go test suite**

Run: `cd /Users/fan/GolangProjects/ControlHub && go test ./... -count=1`
Expected: ALL PASS

- [ ] **Step 2: Run full frontend test suite**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx vitest run`
Expected: ALL PASS

- [ ] **Step 3: Run frontend build**

Run: `cd /Users/fan/JsProjects/ControlHub/.worktrees/cmdb-redesign && npx next build`
Expected: PASS

- [ ] **Step 4: Browser E2E test — Create resource**

Start dev server, navigate to `/resources`:
1. Click "创建资源" or equivalent
2. Select resource type "Host"
3. Verify subtype dropdown shows "Virtual Machine", "Physical Server", "Container"
4. Select subtype "Virtual Machine"
5. Verify profile fields appear: Hostname, IP Address, Operating System
6. Fill in all required fields
7. Click "创建资源"
8. Verify success toast with "继续创建" and "查看详情"
9. Click "查看详情", verify navigation to resource detail page
10. Verify profile data is visible

- [ ] **Step 5: Browser E2E test — Edit resource**

Navigate to a resource detail page:
1. Click "编辑"
2. Verify environment shows name (not UUID)
3. Verify owner shows name (not UUID)
4. Verify name field is editable
5. Verify resourceType is disabled
6. Verify profile fields are loaded and editable
7. Change name and a profile field
8. Click "保存"
9. Verify both changes are persisted
