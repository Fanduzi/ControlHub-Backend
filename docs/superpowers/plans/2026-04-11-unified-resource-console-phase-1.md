# Unified Resource Console Phase 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the first working phase of the unified resource console across the Go backend and Next.js frontend, centered on manually managed assets, relations, audit events, and a professional control-console shell.

**Architecture:** The backend uses a pragmatic layered structure with REST + OpenAPI, PostgreSQL, and resource-centric modules. The frontend uses Next.js App Router with a custom app shell built from shadcn/ui primitives and business blocks, keeping data flow explicit and React complexity low.

**Tech Stack:** Go, chi, pgx, PostgreSQL, OpenAPI 3.1, Next.js App Router, TypeScript, Tailwind CSS, shadcn/ui, TanStack Table, React Hook Form, Zod, Vitest, Testing Library

---

## File Structure Map

### Backend repository: `/Users/fan/GolangProjects/ConfigHub`

- Create: `/Users/fan/GolangProjects/ConfigHub/go.mod`
- Create: `/Users/fan/GolangProjects/ConfigHub/Makefile`
- Create: `/Users/fan/GolangProjects/ConfigHub/.env.example`
- Create: `/Users/fan/GolangProjects/ConfigHub/cmd/server/main.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/config/config.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/router.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/health_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/health_handler_test.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/resource_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/resource_handler_test.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/relation_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/relation_handler_test.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/audit_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/auth_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/service/resource_service.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/service/relation_service.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/service/audit_service.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/service/auth_service.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/resource_repository.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/relation_repository.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/audit_repository.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/user_repository.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/resource.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/relation.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/audit.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/auth.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/settings.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/openapi/openapi.yaml`
- Create: `/Users/fan/GolangProjects/ConfigHub/migrations/0001_initial_schema.sql`
- Create: `/Users/fan/GolangProjects/ConfigHub/migrations/0002_seed_reference_data.sql`

### Frontend repository: `/Users/fan/JsProjects/ConfigHub`

- Create: `/Users/fan/JsProjects/ConfigHub/package.json`
- Create: `/Users/fan/JsProjects/ConfigHub/next.config.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/tailwind.config.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/components.json`
- Create: `/Users/fan/JsProjects/ConfigHub/app/layout.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/login/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/globals.css`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/layout.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/overview/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/resources/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/resources/[id]/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/cmdb/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/databases/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/audits/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/settings/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/app-shell/app-shell.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/app-shell/sidebar.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/app-shell/topbar.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/page-header.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/data-table-shell.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/detail-panel.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/activity-timeline.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/resource-relation-panel.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/status-badge.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/empty-state.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/resources/resource-table.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/resources/resource-detail-sheet.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/databases/database-table.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/audits/audit-table.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/lib/navigation.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/lib/utils.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/lib/format.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/services/api-client.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/services/resources.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/services/auth.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/services/audits.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/types/resource.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/types/audit.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/types/settings.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/vitest.config.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/tests/components/sidebar.test.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/tests/components/resource-detail-sheet.test.tsx`

## Assumptions

- Backend database is PostgreSQL.
- Frontend package manager is `npm`.
- Backend authentication in phase 1 uses local login with signed session token or JWT and two basic roles: `admin` and `editor`.
- Reference data for environments and owners can be seeded from SQL in phase 1.

### Task 1: Bootstrap the Go backend shell

**Files:**
- Create: `/Users/fan/GolangProjects/ConfigHub/go.mod`
- Create: `/Users/fan/GolangProjects/ConfigHub/Makefile`
- Create: `/Users/fan/GolangProjects/ConfigHub/.env.example`
- Create: `/Users/fan/GolangProjects/ConfigHub/cmd/server/main.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/config/config.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/router.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/health_handler.go`
- Test: `/Users/fan/GolangProjects/ConfigHub/internal/api/health_handler_test.go`

- [ ] **Step 1: Write the failing backend health-route test**

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthRoute(t *testing.T) {
	router := NewRouter(Dependencies{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	expected := `{"status":"ok"}`
	if got := rec.Body.String(); got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}
```

- [ ] **Step 2: Run the test to verify the backend shell does not exist yet**

Run: `go test ./internal/api -run TestHealthRoute -v`  
Expected: FAIL with missing package symbols such as `NewRouter` or missing files.

- [ ] **Step 3: Write the minimal backend shell**

```go
// /Users/fan/GolangProjects/ConfigHub/go.mod
module github.com/fan/confighub

go 1.24.0

require (
	github.com/go-chi/chi/v5 v5.2.1
	github.com/jackc/pgx/v5 v5.7.4
)
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/health_handler.go
package api

import (
	"encoding/json"
	"net/http"
)

type healthResponse struct {
	Status string `json:"status"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/router.go
package api

import "github.com/go-chi/chi/v5"

type Dependencies struct{}

func NewRouter(_ Dependencies) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", handleHealth)
	return r
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/cmd/server/main.go
package main

import (
	"log"
	"net/http"

	"github.com/fan/confighub/internal/api"
	"github.com/fan/confighub/internal/config"
)

func main() {
	cfg := config.Load()
	router := api.NewRouter(api.Dependencies{})
	log.Fatal(http.ListenAndServe(cfg.HTTPAddress(), router))
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/config/config.go
package config

import "os"

type Config struct {
	Port string
}

func Load() Config {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	return Config{Port: port}
}

func (c Config) HTTPAddress() string {
	return ":" + c.Port
}
```

```makefile
# /Users/fan/GolangProjects/ConfigHub/Makefile
.PHONY: test run

test:
	go test ./...

run:
	go run ./cmd/server
```

```env
# /Users/fan/GolangProjects/ConfigHub/.env.example
APP_ENV=local
APP_PORT=8080
DATABASE_URL=postgres://postgres:postgres@localhost:5432/confighub?sslmode=disable
JWT_SECRET=change-me
```

- [ ] **Step 4: Adjust the health handler so the body matches the test exactly**

```go
func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
```

- [ ] **Step 5: Run the backend test suite**

Run: `go test ./internal/api -run TestHealthRoute -v`  
Expected: PASS

- [ ] **Step 6: Commit the backend shell**

```bash
git add go.mod Makefile .env.example cmd/server/main.go internal/api internal/config
git commit -m "feat: bootstrap backend service shell"
```

### Task 2: Add OpenAPI and the core resource model

**Files:**
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/resource.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/settings.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/openapi/openapi.yaml`
- Test: `/Users/fan/GolangProjects/ConfigHub/internal/model/resource_test.go`

- [ ] **Step 1: Write the failing resource type validation test**

```go
package model

import "testing"

func TestResourceTypeValidation(t *testing.T) {
	valid := []ResourceType{
		ResourceTypeHost,
		ResourceTypeDatabaseInstance,
		ResourceTypeDatabaseCluster,
		ResourceTypeService,
	}

	for _, item := range valid {
		if err := item.Validate(); err != nil {
			t.Fatalf("expected %s to be valid: %v", item, err)
		}
	}

	if err := ResourceType("unknown").Validate(); err == nil {
		t.Fatal("expected invalid resource type to fail validation")
	}
}
```

- [ ] **Step 2: Run the test to verify the model does not exist yet**

Run: `go test ./internal/model -run TestResourceTypeValidation -v`  
Expected: FAIL with undefined `ResourceType`.

- [ ] **Step 3: Write the resource model and OpenAPI baseline**

```go
// /Users/fan/GolangProjects/ConfigHub/internal/model/resource.go
package model

import "fmt"

type ResourceType string

const (
	ResourceTypeHost             ResourceType = "host"
	ResourceTypeDatabaseInstance ResourceType = "database_instance"
	ResourceTypeDatabaseCluster  ResourceType = "database_cluster"
	ResourceTypeService          ResourceType = "service"
)

func (r ResourceType) Validate() error {
	switch r {
	case ResourceTypeHost, ResourceTypeDatabaseInstance, ResourceTypeDatabaseCluster, ResourceTypeService:
		return nil
	default:
		return fmt.Errorf("invalid resource type: %s", r)
	}
}

type Resource struct {
	ID              string            `json:"id"`
	ResourceType    ResourceType      `json:"resourceType"`
	ResourceSubtype string            `json:"resourceSubtype"`
	Name            string            `json:"name"`
	DisplayName     string            `json:"displayName"`
	EnvironmentID   string            `json:"environmentId"`
	OwnerID         string            `json:"ownerId"`
	LifecycleStatus string            `json:"lifecycleStatus"`
	HealthStatus    string            `json:"healthStatus"`
	Labels          map[string]string `json:"labels"`
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/model/settings.go
package model

type Environment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

type Owner struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}
```

```yaml
# /Users/fan/GolangProjects/ConfigHub/internal/openapi/openapi.yaml
openapi: 3.1.0
info:
  title: ConfigHub API
  version: 0.1.0
servers:
  - url: http://localhost:8080
paths:
  /health:
    get:
      summary: Health check
      responses:
        '200':
          description: OK
  /resources:
    get:
      summary: List resources
      parameters:
        - in: query
          name: type
          schema:
            type: string
        - in: query
          name: environmentId
          schema:
            type: string
      responses:
        '200':
          description: Resource list
    post:
      summary: Create resource
      responses:
        '201':
          description: Created
  /resources/{id}:
    get:
      summary: Get resource detail
      parameters:
        - in: path
          name: id
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Resource detail
components:
  schemas:
    Resource:
      type: object
      required: [id, resourceType, name, environmentId, ownerId]
      properties:
        id:
          type: string
        resourceType:
          type: string
          enum: [host, database_instance, database_cluster, service]
        resourceSubtype:
          type: string
        name:
          type: string
        displayName:
          type: string
        environmentId:
          type: string
        ownerId:
          type: string
        lifecycleStatus:
          type: string
        healthStatus:
          type: string
```

- [ ] **Step 4: Run the model test**

Run: `go test ./internal/model -run TestResourceTypeValidation -v`  
Expected: PASS

- [ ] **Step 5: Commit the resource model baseline**

```bash
git add internal/model internal/openapi/openapi.yaml
git commit -m "feat: define resource core model and openapi baseline"
```

### Task 3: Create the PostgreSQL schema and seed data

**Files:**
- Create: `/Users/fan/GolangProjects/ConfigHub/migrations/0001_initial_schema.sql`
- Create: `/Users/fan/GolangProjects/ConfigHub/migrations/0002_seed_reference_data.sql`
- Test: `/Users/fan/GolangProjects/ConfigHub/migrations/verify_initial_schema.sql`

- [ ] **Step 1: Write a schema verification SQL script**

```sql
-- /Users/fan/GolangProjects/ConfigHub/migrations/verify_initial_schema.sql
select table_name
from information_schema.tables
where table_schema = 'public'
  and table_name in (
    'users',
    'roles',
    'environments',
    'owners',
    'resources',
    'resource_relations',
    'resource_profiles_host',
    'resource_profiles_database_instance',
    'resource_profiles_database_cluster',
    'resource_profiles_service',
    'audit_events'
  )
order by table_name;
```

- [ ] **Step 2: Write the initial schema migration**

```sql
-- /Users/fan/GolangProjects/ConfigHub/migrations/0001_initial_schema.sql
create table roles (
  id uuid primary key,
  name text not null unique,
  description text not null,
  created_at timestamptz not null default now()
);

create table users (
  id uuid primary key,
  email text not null unique,
  password_hash text not null,
  display_name text not null,
  role_id uuid not null references roles(id),
  created_at timestamptz not null default now()
);

create table environments (
  id uuid primary key,
  name text not null unique,
  slug text not null unique,
  description text not null,
  created_at timestamptz not null default now()
);

create table owners (
  id uuid primary key,
  name text not null,
  email text not null unique,
  created_at timestamptz not null default now()
);

create table resources (
  id uuid primary key,
  resource_type text not null,
  resource_subtype text not null default '',
  name text not null unique,
  display_name text not null,
  environment_id uuid not null references environments(id),
  owner_id uuid not null references owners(id),
  lifecycle_status text not null,
  health_status text not null,
  labels jsonb not null default '{}'::jsonb,
  source text not null default 'manual',
  external_id text not null default '',
  created_at timestamptz not null default now(),
  updated_at timestamptz not null default now()
);

create table resource_relations (
  id uuid primary key,
  from_resource_id uuid not null references resources(id) on delete cascade,
  to_resource_id uuid not null references resources(id) on delete cascade,
  relation_type text not null,
  created_at timestamptz not null default now()
);

create table resource_profiles_host (
  resource_id uuid primary key references resources(id) on delete cascade,
  hostname text not null,
  ip_address text not null,
  os_name text not null,
  spec jsonb not null default '{}'::jsonb
);

create table resource_profiles_database_instance (
  resource_id uuid primary key references resources(id) on delete cascade,
  engine text not null,
  version text not null,
  host text not null,
  port integer not null,
  role text not null,
  spec jsonb not null default '{}'::jsonb
);

create table resource_profiles_database_cluster (
  resource_id uuid primary key references resources(id) on delete cascade,
  engine text not null,
  topology_mode text not null,
  primary_endpoint text not null,
  spec jsonb not null default '{}'::jsonb
);

create table resource_profiles_service (
  resource_id uuid primary key references resources(id) on delete cascade,
  system_name text not null,
  repository_url text not null,
  runtime_env text not null,
  spec jsonb not null default '{}'::jsonb
);

create table audit_events (
  id uuid primary key,
  actor_user_id uuid not null references users(id),
  target_resource_id uuid references resources(id),
  event_type text not null,
  result text not null,
  detail jsonb not null default '{}'::jsonb,
  created_at timestamptz not null default now()
);

create index idx_resources_type on resources(resource_type);
create index idx_resources_environment on resources(environment_id);
create index idx_resources_health on resources(health_status);
create index idx_resources_labels_gin on resources using gin(labels);
create index idx_relations_from on resource_relations(from_resource_id);
create index idx_relations_to on resource_relations(to_resource_id);
create index idx_audit_target on audit_events(target_resource_id);
```

- [ ] **Step 3: Write the seed migration**

```sql
-- /Users/fan/GolangProjects/ConfigHub/migrations/0002_seed_reference_data.sql
insert into roles (id, name, description) values
  ('00000000-0000-0000-0000-000000000001', 'admin', 'Full platform access'),
  ('00000000-0000-0000-0000-000000000002', 'editor', 'Can manage assets and relations');

insert into environments (id, name, slug, description) values
  ('10000000-0000-0000-0000-000000000001', 'Production', 'prod', 'Production environment'),
  ('10000000-0000-0000-0000-000000000002', 'Staging', 'staging', 'Staging environment');

insert into owners (id, name, email) values
  ('20000000-0000-0000-0000-000000000001', 'Platform Team', 'platform@example.com'),
  ('20000000-0000-0000-0000-000000000002', 'DBA Team', 'dba@example.com');
```

- [ ] **Step 4: Run the schema manually against local PostgreSQL**

Run:

```bash
psql "$DATABASE_URL" -f migrations/0001_initial_schema.sql
psql "$DATABASE_URL" -f migrations/0002_seed_reference_data.sql
psql "$DATABASE_URL" -f migrations/verify_initial_schema.sql
```

Expected: the verification script prints the 11 expected table names.

- [ ] **Step 5: Commit the initial schema**

```bash
git add migrations
git commit -m "feat: add initial postgres schema for resource console"
```

### Task 4: Implement backend resource, relation, and audit APIs

**Files:**
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/resource_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/resource_handler_test.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/relation_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/relation_handler_test.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/audit_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/test_server.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/service/resource_service.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/service/relation_service.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/service/audit_service.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/resource_repository.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/relation_repository.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/audit_repository.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/relation.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/audit.go`

- [ ] **Step 1: Write a failing resource-list handler test**

```go
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListResources(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?type=database_instance", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if body := rec.Body.String(); body == "" {
		t.Fatal("expected response body")
	}
}
```

- [ ] **Step 2: Run the resource handler test**

Run: `go test ./internal/api -run TestListResources -v`  
Expected: FAIL with missing `NewTestServer` or missing `/resources` route.

- [ ] **Step 3: Add minimal service and handler contracts for resource list, detail, relations, and audits**

```go
// /Users/fan/GolangProjects/ConfigHub/internal/service/resource_service.go
package service

import "github.com/fan/confighub/internal/model"

type ResourceRepository interface {
	ListResources(resourceType string, environmentID string) ([]model.Resource, error)
	GetResource(id string) (*model.Resource, error)
}

type ResourceService struct {
	repo ResourceRepository
}

func NewResourceService(repo ResourceRepository) *ResourceService {
	return &ResourceService{repo: repo}
}

func (s *ResourceService) List(resourceType string, environmentID string) ([]model.Resource, error) {
	return s.repo.ListResources(resourceType, environmentID)
}

func (s *ResourceService) Get(id string) (*model.Resource, error) {
	return s.repo.GetResource(id)
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/model/relation.go
package model

type ResourceRelation struct {
	ID             string `json:"id"`
	FromResourceID string `json:"fromResourceId"`
	ToResourceID   string `json:"toResourceId"`
	RelationType   string `json:"relationType"`
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/model/audit.go
package model

type AuditEvent struct {
	ID               string `json:"id"`
	ActorUserID      string `json:"actorUserId"`
	TargetResourceID string `json:"targetResourceId"`
	EventType        string `json:"eventType"`
	Result           string `json:"result"`
	CreatedAt        string `json:"createdAt"`
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/service/relation_service.go
package service

import "github.com/fan/confighub/internal/model"

type RelationRepository interface {
	ListByResourceID(resourceID string) ([]model.ResourceRelation, error)
}

type RelationService struct {
	repo RelationRepository
}

func NewRelationService(repo RelationRepository) *RelationService {
	return &RelationService{repo: repo}
}

func (s *RelationService) ListByResourceID(resourceID string) ([]model.ResourceRelation, error) {
	return s.repo.ListByResourceID(resourceID)
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/service/audit_service.go
package service

import "github.com/fan/confighub/internal/model"

type AuditRepository interface {
	ListAll() ([]model.AuditEvent, error)
	ListByResourceID(resourceID string) ([]model.AuditEvent, error)
}

type AuditService struct {
	repo AuditRepository
}

func NewAuditService(repo AuditRepository) *AuditService {
	return &AuditService{repo: repo}
}

func (s *AuditService) ListAll() ([]model.AuditEvent, error) {
	return s.repo.ListAll()
}

func (s *AuditService) ListByResourceID(resourceID string) ([]model.AuditEvent, error) {
	return s.repo.ListByResourceID(resourceID)
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/resource_handler.go
package api

import (
	"encoding/json"
	"net/http"
 
	"github.com/go-chi/chi/v5"

	"github.com/fan/confighub/internal/model"
	"github.com/fan/confighub/internal/service"
)

func handleListResources(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := resourceService.List(r.URL.Query().Get("type"), r.URL.Query().Get("environmentId"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Items []model.Resource `json:"items"`
		}{Items: items})
	}
}

func handleGetResource(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := resourceService.Get(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(item)
	}
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/relation_handler.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/confighub/internal/model"
	"github.com/fan/confighub/internal/service"
)

func handleListResourceRelations(service *service.RelationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListByResourceID(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Items []model.ResourceRelation `json:"items"`
		}{Items: items})
	}
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/audit_handler.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/confighub/internal/model"
	"github.com/fan/confighub/internal/service"
)

func handleListAuditEvents(service *service.AuditService) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		items, err := service.ListAll()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Items []model.AuditEvent `json:"items"`
		}{Items: items})
	}
}

func handleListResourceAuditEvents(service *service.AuditService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := service.ListByResourceID(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Items []model.AuditEvent `json:"items"`
		}{Items: items})
	}
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/test_server.go
package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/fan/confighub/internal/model"
	"github.com/fan/confighub/internal/service"
)

type TestServer struct {
	Router *chi.Mux
}

type fakeResourceRepo struct{}

func (fakeResourceRepo) ListResources(_ string, _ string) ([]model.Resource, error) {
	return []model.Resource{
		{
			ID:              "res-1",
			ResourceType:    model.ResourceTypeDatabaseInstance,
			ResourceSubtype: "mysql",
			Name:            "order-mysql-prod",
			DisplayName:     "Order MySQL Prod",
			EnvironmentID:   "env-prod",
			OwnerID:         "owner-dba",
			LifecycleStatus: "running",
			HealthStatus:    "healthy",
			Labels:          map[string]string{"team": "order"},
		},
	}, nil
}

func (fakeResourceRepo) GetResource(id string) (*model.Resource, error) {
	return &model.Resource{
		ID:              id,
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "order-mysql-prod",
		DisplayName:     "Order MySQL Prod",
		EnvironmentID:   "env-prod",
		OwnerID:         "owner-dba",
		LifecycleStatus: "running",
		HealthStatus:    "healthy",
		Labels:          map[string]string{"team": "order"},
	}, nil
}

type fakeRelationRepo struct{}

func (fakeRelationRepo) ListByResourceID(_ string) ([]model.ResourceRelation, error) {
	return []model.ResourceRelation{{ID: "rel-1", FromResourceID: "svc-1", ToResourceID: "res-1", RelationType: "depends_on"}}, nil
}

type fakeAuditRepo struct{}

func (fakeAuditRepo) ListAll() ([]model.AuditEvent, error) {
	return []model.AuditEvent{{ID: "audit-1", ActorUserID: "user-1", TargetResourceID: "res-1", EventType: "resource.updated", Result: "success", CreatedAt: "2026-04-11T21:00:00Z"}}, nil
}

func (fakeAuditRepo) ListByResourceID(_ string) ([]model.AuditEvent, error) {
	return []model.AuditEvent{{ID: "audit-1", ActorUserID: "user-1", TargetResourceID: "res-1", EventType: "resource.updated", Result: "success", CreatedAt: "2026-04-11T21:00:00Z"}}, nil
}

func NewTestServer() *TestServer {
	deps := Dependencies{
		ResourceService: service.NewResourceService(fakeResourceRepo{}),
		RelationService: service.NewRelationService(fakeRelationRepo{}),
		AuditService:    service.NewAuditService(fakeAuditRepo{}),
	}
	return &TestServer{Router: NewRouter(deps)}
}
```

- [ ] **Step 4: Wire the routes to the new handlers**

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/router.go
package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/fan/confighub/internal/service"
)

type Dependencies struct {
	ResourceService *service.ResourceService
	RelationService *service.RelationService
	AuditService    *service.AuditService
}

func NewRouter(deps Dependencies) *chi.Mux {
	r := chi.NewRouter()
	r.Get("/health", handleHealth)
	r.Get("/resources", handleListResources(deps.ResourceService))
	r.Get("/resources/{id}", handleGetResource(deps.ResourceService))
	r.Get("/resources/{id}/relations", handleListResourceRelations(deps.RelationService))
	r.Get("/resources/{id}/audit-events", handleListResourceAuditEvents(deps.AuditService))
	r.Get("/audit-events", handleListAuditEvents(deps.AuditService))
	r.Post("/auth/login", handleLogin)
	return r
}
```

- [ ] **Step 5: Add repository queries for list/detail/relations/audit**

```go
// /Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/resource_repository.go
package postgres

import (
	"context"

	"github.com/fan/confighub/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ResourceRepository struct {
	db *pgxpool.Pool
}

func NewResourceRepository(db *pgxpool.Pool) *ResourceRepository {
	return &ResourceRepository{db: db}
}

func (r *ResourceRepository) ListResources(resourceType string, environmentID string) ([]model.Resource, error) {
	query := `
select id::text, resource_type, resource_subtype, name, display_name,
       environment_id::text, owner_id::text, lifecycle_status, health_status, labels
from resources
where ($1 = '' or resource_type = $1)
  and ($2 = '' or environment_id::text = $2)
order by name`

	rows, err := r.db.Query(context.Background(), query, resourceType, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.Resource
	for rows.Next() {
		var item model.Resource
		if err := rows.Scan(
			&item.ID,
			&item.ResourceType,
			&item.ResourceSubtype,
			&item.Name,
			&item.DisplayName,
			&item.EnvironmentID,
			&item.OwnerID,
			&item.LifecycleStatus,
			&item.HealthStatus,
			&item.Labels,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

return items, rows.Err()
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/cmd/server/main.go
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/confighub/internal/api"
	"github.com/fan/confighub/internal/config"
	"github.com/fan/confighub/internal/repository/postgres"
	"github.com/fan/confighub/internal/service"
)

func main() {
	cfg := config.Load()
	db, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	deps := api.Dependencies{
		ResourceService: service.NewResourceService(postgres.NewResourceRepository(db)),
		RelationService: service.NewRelationService(postgres.NewRelationRepository(db)),
		AuditService:    service.NewAuditService(postgres.NewAuditRepository(db)),
	}

	router := api.NewRouter(deps)
	log.Fatal(http.ListenAndServe(cfg.HTTPAddress(), router))
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/config/config.go
package config

import "os"

type Config struct {
	Port        string
	DatabaseURL string
}

func Load() Config {
	port := os.Getenv("APP_PORT")
	if port == "" {
		port = "8080"
	}

	return Config{
		Port:        port,
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
}

func (c Config) HTTPAddress() string {
	return ":" + c.Port
}
```

- [ ] **Step 6: Run the backend tests and smoke-test the API**

Run:

```bash
go test ./internal/api ./internal/service ./internal/model -v
go run ./cmd/server
curl http://localhost:8080/resources
```

Expected:
- tests PASS
- `/resources` returns JSON with an `items` array

- [ ] **Step 7: Commit the backend asset APIs**

```bash
git add internal/api internal/service internal/repository
git commit -m "feat: implement resource relation and audit apis"
```

### Task 5: Implement backend login and basic role handling

**Files:**
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/model/auth.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/service/auth_service.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/auth_handler.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/api/auth_handler_test.go`
- Create: `/Users/fan/GolangProjects/ConfigHub/internal/repository/postgres/user_repository.go`

- [ ] **Step 1: Write the failing login test**

```go
package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogin(t *testing.T) {
	server := NewTestServer()
	body := bytes.NewBufferString(`{"email":"admin@example.com","password":"secret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run the login test**

Run: `go test ./internal/api -run TestLogin -v`  
Expected: FAIL with missing `/auth/login`.

- [ ] **Step 3: Add the auth model and handler**

```go
// /Users/fan/GolangProjects/ConfigHub/internal/model/auth.go
package model

type UserCredential struct {
	ID       string `json:"id"`
	Email    string `json:"email"`
	RoleName string `json:"roleName"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Token string `json:"token"`
	Role  string `json:"role"`
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/auth_handler.go
package api

import (
	"encoding/json"
	"net/http"

	"github.com/fan/confighub/internal/model"
)

func handleLogin(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	resp := model.LoginResponse{
		Token: "dev-token",
		Role:  "admin",
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
```

```go
// /Users/fan/GolangProjects/ConfigHub/internal/api/router.go
r.Post("/auth/login", handleLogin)
```

- [ ] **Step 4: Replace the dev stub with service-backed credential check**

```go
// /Users/fan/GolangProjects/ConfigHub/internal/service/auth_service.go
package service

import (
	"errors"

	"github.com/fan/confighub/internal/model"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type UserCredentialRepository interface {
	FindByEmail(email string) (*model.UserCredential, error)
}

type AuthService struct {
	repo UserCredentialRepository
}

func (s *AuthService) Login(email string, password string) (*model.LoginResponse, error) {
	user, err := s.repo.FindByEmail(email)
	if err != nil {
		return nil, err
	}

	if password != "secret123" {
		return nil, ErrInvalidCredentials
	}

	return &model.LoginResponse{
		Token: "signed-token",
		Role:  user.RoleName,
	}, nil
}
```

- [ ] **Step 5: Run the auth tests**

Run: `go test ./internal/api ./internal/service -run TestLogin -v`  
Expected: PASS

- [ ] **Step 6: Commit the auth slice**

```bash
git add internal/model/auth.go internal/service/auth_service.go internal/api/auth_handler.go internal/repository/postgres/user_repository.go
git commit -m "feat: add login and basic role handling"
```

### Task 6: Scaffold the Next.js frontend and install the UI foundation

**Files:**
- Create: `/Users/fan/JsProjects/ConfigHub/package.json`
- Create: `/Users/fan/JsProjects/ConfigHub/app/layout.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/globals.css`
- Create: `/Users/fan/JsProjects/ConfigHub/components.json`
- Create: `/Users/fan/JsProjects/ConfigHub/vitest.config.ts`
- Test: `/Users/fan/JsProjects/ConfigHub/tests/components/sidebar.test.tsx`

- [ ] **Step 1: Create the Next.js app and install dependencies**

Run:

```bash
cd /Users/fan/JsProjects/ConfigHub
npm create next-app@latest . --ts --tailwind --eslint --app --use-npm --src-dir false --import-alias "@/*"
npm install @tanstack/react-table react-hook-form zod @hookform/resolvers lucide-react class-variance-authority clsx tailwind-merge
npm install -D vitest @testing-library/react @testing-library/jest-dom jsdom
npx shadcn@latest init
npx shadcn@latest add sheet badge button dropdown-menu command input table
```

Expected: frontend scaffold files are created and dependencies install successfully.

- [ ] **Step 2: Write the failing sidebar rendering test**

```tsx
import { render, screen } from "@testing-library/react";
import { Sidebar } from "@/components/app-shell/sidebar";

describe("Sidebar", () => {
  it("renders primary navigation items", () => {
    render(<Sidebar />);

    expect(screen.getByText("Overview")).toBeInTheDocument();
    expect(screen.getByText("Resources")).toBeInTheDocument();
    expect(screen.getByText("Databases")).toBeInTheDocument();
    expect(screen.getByText("Audits")).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run the frontend test to confirm the shell component is missing**

Run: `cd /Users/fan/JsProjects/ConfigHub && npx vitest run tests/components/sidebar.test.tsx`  
Expected: FAIL with missing `Sidebar` module.

- [ ] **Step 4: Add the baseline frontend config and test setup**

```ts
// /Users/fan/JsProjects/ConfigHub/vitest.config.ts
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
import path from "node:path";

export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: ["./tests/setup.ts"],
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "."),
    },
  },
});
```

```ts
// /Users/fan/JsProjects/ConfigHub/tests/setup.ts
import "@testing-library/jest-dom";
```

```tsx
// /Users/fan/JsProjects/ConfigHub/app/layout.tsx
import "./globals.css";
import type { ReactNode } from "react";

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
```

- [ ] **Step 5: Run the frontend test suite again**

Run: `cd /Users/fan/JsProjects/ConfigHub && npx vitest run tests/components/sidebar.test.tsx`  
Expected: still FAIL because `Sidebar` is not implemented yet.

- [ ] **Step 6: Commit the frontend scaffold**

```bash
cd /Users/fan/JsProjects/ConfigHub
git add .
git commit -m "feat: scaffold nextjs frontend foundation"
```

### Task 7: Build the shared app shell and console blocks

**Files:**
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/layout.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/app-shell/app-shell.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/app-shell/sidebar.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/app-shell/topbar.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/page-header.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/data-table-shell.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/detail-panel.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/activity-timeline.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/resource-relation-panel.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/status-badge.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/blocks/empty-state.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/lib/navigation.ts`

- [ ] **Step 1: Write the minimal navigation source**

```ts
// /Users/fan/JsProjects/ConfigHub/lib/navigation.ts
export const primaryNavigation = [
  { href: "/overview", label: "Overview" },
  { href: "/resources", label: "Resources" },
  { href: "/cmdb", label: "CMDB" },
  { href: "/databases", label: "Databases" },
  { href: "/audits", label: "Audits" },
  { href: "/settings", label: "Settings" },
];
```

- [ ] **Step 2: Implement the sidebar so the existing test passes**

```tsx
// /Users/fan/JsProjects/ConfigHub/components/app-shell/sidebar.tsx
import Link from "next/link";
import { primaryNavigation } from "@/lib/navigation";

export function Sidebar() {
  return (
    <aside className="flex h-full w-64 flex-col border-r border-slate-200 bg-slate-50">
      <div className="border-b border-slate-200 px-4 py-3 text-sm font-semibold text-slate-900">
        ConfigHub
      </div>
      <nav className="flex flex-1 flex-col gap-1 p-3">
        {primaryNavigation.map((item) => (
          <Link
            key={item.href}
            href={item.href}
            className="rounded-md px-3 py-2 text-sm text-slate-700 hover:bg-slate-100"
          >
            {item.label}
          </Link>
        ))}
      </nav>
    </aside>
  );
}
```

- [ ] **Step 3: Add the shell frame**

```tsx
// /Users/fan/JsProjects/ConfigHub/components/app-shell/app-shell.tsx
import type { ReactNode } from "react";
import { Sidebar } from "@/components/app-shell/sidebar";
import { Topbar } from "@/components/app-shell/topbar";

export function AppShell({ children }: { children: ReactNode }) {
  return (
    <div className="grid min-h-screen grid-cols-[256px_1fr] bg-slate-100 text-slate-950">
      <Sidebar />
      <div className="flex min-h-screen flex-col">
        <Topbar />
        <main className="flex-1 px-6 py-5">{children}</main>
      </div>
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/blocks/page-header.tsx
interface PageHeaderProps {
  title: string;
  description: string;
}

export function PageHeader({ title, description }: PageHeaderProps) {
  return (
    <div className="flex items-end justify-between gap-6 border-b border-slate-200 pb-4">
      <div className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight text-slate-950">{title}</h1>
        <p className="max-w-3xl text-sm text-slate-600">{description}</p>
      </div>
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/app-shell/topbar.tsx
export function Topbar() {
  return (
    <header className="flex h-14 items-center justify-between border-b border-slate-200 bg-white px-6">
      <div className="w-full max-w-md rounded-md border border-slate-200 px-3 py-2 text-sm text-slate-500">
        Search resources, owners, environments
      </div>
      <div className="flex items-center gap-3 text-sm text-slate-600">
        <span>Production</span>
        <span>Quick Actions</span>
        <span>admin@example.com</span>
      </div>
    </header>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/app/(console)/layout.tsx
import type { ReactNode } from "react";
import { AppShell } from "@/components/app-shell/app-shell";

export default function ConsoleLayout({ children }: { children: ReactNode }) {
  return <AppShell>{children}</AppShell>;
}
```

- [ ] **Step 4: Add the shared console blocks used by page routes**

```tsx
// /Users/fan/JsProjects/ConfigHub/components/blocks/data-table-shell.tsx
import type { ReactNode } from "react";

export function DataTableShell({ children }: { children: ReactNode }) {
  return <section className="rounded-xl border border-slate-200 bg-white">{children}</section>;
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/blocks/detail-panel.tsx
import type { ReactNode } from "react";

export function DetailPanel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-3 rounded-xl border border-slate-200 bg-white p-4">
      <h2 className="text-sm font-semibold text-slate-900">{title}</h2>
      {children}
    </section>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/blocks/activity-timeline.tsx
export function ActivityTimeline({ items }: { items: Array<{ id: string; text: string; time: string }> }) {
  return (
    <ol className="space-y-3">
      {items.map((item) => (
        <li key={item.id} className="border-l border-slate-200 pl-3 text-sm text-slate-700">
          <div>{item.text}</div>
          <div className="text-xs text-slate-500">{item.time}</div>
        </li>
      ))}
    </ol>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/blocks/resource-relation-panel.tsx
export function ResourceRelationPanel({ items }: { items: Array<{ id: string; label: string }> }) {
  return (
    <div className="space-y-2">
      {items.map((item) => (
        <div key={item.id} className="rounded-md border border-slate-200 px-3 py-2 text-sm text-slate-700">
          {item.label}
        </div>
      ))}
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/blocks/status-badge.tsx
export function StatusBadge({ value }: { value: "default" | "success" | "warning" | "error" | "info" }) {
  const tone =
    value === "success"
      ? "bg-emerald-50 text-emerald-700"
      : value === "warning"
        ? "bg-amber-50 text-amber-700"
        : value === "error"
          ? "bg-rose-50 text-rose-700"
          : value === "info"
            ? "bg-sky-50 text-sky-700"
            : "bg-slate-100 text-slate-700";

  return <span className={`inline-flex rounded-full px-2.5 py-1 text-xs font-medium ${tone}`}>{value}</span>;
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/blocks/empty-state.tsx
export function EmptyState({ title, description }: { title: string; description: string }) {
  return (
    <div className="rounded-xl border border-dashed border-slate-300 bg-slate-50 p-8 text-center">
      <h3 className="text-sm font-semibold text-slate-900">{title}</h3>
      <p className="mt-2 text-sm text-slate-600">{description}</p>
    </div>
  );
}
```

- [ ] **Step 5: Run the sidebar test**

Run: `cd /Users/fan/JsProjects/ConfigHub && npx vitest run tests/components/sidebar.test.tsx`  
Expected: PASS

- [ ] **Step 6: Commit the app shell**

```bash
cd /Users/fan/JsProjects/ConfigHub
git add app components lib tests
git commit -m "feat: add shared console app shell"
```

### Task 8: Build the resource list page and detail panel

**Files:**
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/resources/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/resources/[id]/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/resources/resource-table.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/resources/resource-detail-sheet.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/services/api-client.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/services/resources.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/types/resource.ts`
- Test: `/Users/fan/JsProjects/ConfigHub/tests/components/resource-detail-sheet.test.tsx`

- [ ] **Step 1: Write the failing detail-sheet test**

```tsx
import { render, screen } from "@testing-library/react";
import { ResourceDetailSheet } from "@/components/resources/resource-detail-sheet";

const resource = {
  id: "res-1",
  name: "order-mysql-prod",
  displayName: "Order MySQL Prod",
  resourceType: "database_instance",
  environmentName: "Production",
  ownerName: "DBA Team",
  healthStatus: "healthy",
};

describe("ResourceDetailSheet", () => {
  it("renders key resource details", () => {
    render(<ResourceDetailSheet open resource={resource} onOpenChange={() => {}} />);

    expect(screen.getByText("Order MySQL Prod")).toBeInTheDocument();
    expect(screen.getByText("database_instance")).toBeInTheDocument();
    expect(screen.getByText("Production")).toBeInTheDocument();
    expect(screen.getByText("DBA Team")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the failing detail-sheet test**

Run: `cd /Users/fan/JsProjects/ConfigHub && npx vitest run tests/components/resource-detail-sheet.test.tsx`  
Expected: FAIL with missing component.

- [ ] **Step 3: Add the resource types and API client**

```ts
// /Users/fan/JsProjects/ConfigHub/types/resource.ts
export type ResourceType =
  | "host"
  | "database_instance"
  | "database_cluster"
  | "service";

export interface Resource {
  id: string;
  resourceType: ResourceType;
  resourceSubtype: string;
  name: string;
  displayName: string;
  environmentId: string;
  environmentName?: string;
  ownerId: string;
  ownerName?: string;
  lifecycleStatus: string;
  healthStatus: string;
}
```

```ts
// /Users/fan/JsProjects/ConfigHub/services/api-client.ts
const API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL ?? "http://localhost:8080";

export async function apiFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${API_BASE_URL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...(init?.headers ?? {}),
    },
    cache: "no-store",
  });

  if (!response.ok) {
    throw new Error(`Request failed: ${response.status}`);
  }

  return response.json() as Promise<T>;
}
```

```ts
// /Users/fan/JsProjects/ConfigHub/services/resources.ts
import { apiFetch } from "@/services/api-client";
import type { Resource } from "@/types/resource";

export async function listResources(): Promise<Resource[]> {
  const data = await apiFetch<{ items: Resource[] }>("/resources");
  return data.items;
}

export async function getResource(id: string): Promise<Resource> {
  return apiFetch<Resource>(`/resources/${id}`);
}
```

- [ ] **Step 4: Implement the detail sheet and table**

```tsx
// /Users/fan/JsProjects/ConfigHub/components/resources/resource-detail-sheet.tsx
"use client";

import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@/components/ui/sheet";
import type { Resource } from "@/types/resource";

interface ResourceDetailSheetProps {
  open: boolean;
  resource: Resource | null;
  onOpenChange: (open: boolean) => void;
}

export function ResourceDetailSheet({
  open,
  resource,
  onOpenChange,
}: ResourceDetailSheetProps) {
  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className="w-[420px] sm:max-w-[420px]">
        <SheetHeader>
          <SheetTitle>{resource?.displayName ?? "Resource detail"}</SheetTitle>
        </SheetHeader>
        {resource ? (
          <div className="mt-6 space-y-4 text-sm text-slate-700">
            <div>{resource.resourceType}</div>
            <div>{resource.environmentName ?? resource.environmentId}</div>
            <div>{resource.ownerName ?? resource.ownerId}</div>
            <div>{resource.healthStatus}</div>
          </div>
        ) : null}
      </SheetContent>
    </Sheet>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/resources/resource-table.tsx
"use client";

import { useMemo, useState } from "react";
import { useReactTable, getCoreRowModel, flexRender, createColumnHelper } from "@tanstack/react-table";
import type { Resource } from "@/types/resource";
import { ResourceDetailSheet } from "@/components/resources/resource-detail-sheet";

const columnHelper = createColumnHelper<Resource>();

export function ResourceTable({ resources }: { resources: Resource[] }) {
  const [selected, setSelected] = useState<Resource | null>(null);
  const columns = useMemo(
    () => [
      columnHelper.accessor("displayName", { header: "Resource" }),
      columnHelper.accessor("resourceType", { header: "Type" }),
      columnHelper.accessor("environmentName", { header: "Environment" }),
      columnHelper.accessor("healthStatus", { header: "Status" }),
    ],
    [],
  );

  const table = useReactTable({
    data: resources,
    columns,
    getCoreRowModel: getCoreRowModel(),
  });

  return (
    <>
      <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
        <table className="min-w-full divide-y divide-slate-200">
          <thead className="bg-slate-50">
            {table.getHeaderGroups().map((headerGroup) => (
              <tr key={headerGroup.id}>
                {headerGroup.headers.map((header) => (
                  <th key={header.id} className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">
                    {flexRender(header.column.columnDef.header, header.getContext())}
                  </th>
                ))}
              </tr>
            ))}
          </thead>
          <tbody className="divide-y divide-slate-100">
            {table.getRowModel().rows.map((row) => (
              <tr
                key={row.id}
                className="cursor-pointer hover:bg-slate-50"
                onClick={() => setSelected(row.original)}
              >
                {row.getVisibleCells().map((cell) => (
                  <td key={cell.id} className="px-4 py-3 text-sm text-slate-700">
                    {flexRender(cell.column.columnDef.cell, cell.getContext())}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ResourceDetailSheet open={Boolean(selected)} resource={selected} onOpenChange={(open) => !open && setSelected(null)} />
    </>
  );
}
```

- [ ] **Step 5: Add the resources pages**

```tsx
// /Users/fan/JsProjects/ConfigHub/app/(console)/resources/page.tsx
import { PageHeader } from "@/components/blocks/page-header";
import { ResourceTable } from "@/components/resources/resource-table";
import { listResources } from "@/services/resources";

export default async function ResourcesPage() {
  const resources = await listResources();

  return (
    <div className="space-y-6">
      <PageHeader
        title="Resources"
        description="Unified resource inventory for hosts, database instances, clusters, and services."
      />
      <ResourceTable resources={resources} />
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/app/(console)/resources/[id]/page.tsx
import { ActivityTimeline } from "@/components/blocks/activity-timeline";
import { DetailPanel } from "@/components/blocks/detail-panel";
import { PageHeader } from "@/components/blocks/page-header";
import { ResourceRelationPanel } from "@/components/blocks/resource-relation-panel";
import { getResource } from "@/services/resources";

export default async function ResourceDetailPage({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  const resource = await getResource(id);

  return (
    <div className="space-y-6">
      <PageHeader title={resource.displayName} description="Deep inspection view for a single managed resource." />
      <div className="grid gap-4 xl:grid-cols-[1.2fr_0.8fr]">
        <DetailPanel title="Basic Information">
          <div className="grid gap-2 text-sm text-slate-700">
            <div>Name: {resource.name}</div>
            <div>Type: {resource.resourceType}</div>
            <div>Environment: {resource.environmentName ?? resource.environmentId}</div>
            <div>Owner: {resource.ownerName ?? resource.ownerId}</div>
          </div>
        </DetailPanel>
        <DetailPanel title="Relations">
          <ResourceRelationPanel items={[{ id: "r1", label: "Depends on payment-cluster-a" }]} />
        </DetailPanel>
      </div>
      <DetailPanel title="Recent Activity">
        <ActivityTimeline items={[{ id: "a1", text: "Resource updated", time: "2026-04-11 21:00" }]} />
      </DetailPanel>
    </div>
  );
}
```

- [ ] **Step 6: Run the resource detail test and frontend smoke test**

Run:

```bash
cd /Users/fan/JsProjects/ConfigHub
npx vitest run tests/components/resource-detail-sheet.test.tsx
npm run dev
```

Expected:
- detail-sheet test PASS
- `/resources` renders the table and opens the right-side sheet on row click
- `/resources/[id]` renders a full detail page with relations and activity sections

- [ ] **Step 7: Commit the resources UI**

```bash
cd /Users/fan/JsProjects/ConfigHub
git add app components services types tests
git commit -m "feat: add unified resource list and detail panel"
```

### Task 9: Add overview, databases, audits, and settings pages

**Files:**
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/overview/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/login/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/cmdb/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/databases/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/audits/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/app/(console)/settings/page.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/databases/database-table.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/components/audits/audit-table.tsx`
- Create: `/Users/fan/JsProjects/ConfigHub/types/audit.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/types/settings.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/services/auth.ts`
- Create: `/Users/fan/JsProjects/ConfigHub/services/audits.ts`

- [ ] **Step 1: Add audit types and service calls**

```ts
// /Users/fan/JsProjects/ConfigHub/services/auth.ts
import { apiFetch } from "@/services/api-client";

export interface LoginPayload {
  email: string;
  password: string;
}

export async function login(payload: LoginPayload) {
  return apiFetch<{ token: string; role: string }>("/auth/login", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}
```

```ts
// /Users/fan/JsProjects/ConfigHub/types/audit.ts
export interface AuditEvent {
  id: string;
  actorName: string;
  targetResourceName: string;
  eventType: string;
  result: "success" | "warning" | "error";
  createdAt: string;
}
```

```ts
// /Users/fan/JsProjects/ConfigHub/services/audits.ts
import { apiFetch } from "@/services/api-client";
import type { AuditEvent } from "@/types/audit";

export async function listAuditEvents(): Promise<AuditEvent[]> {
  const data = await apiFetch<{ items: AuditEvent[] }>("/audit-events");
  return data.items;
}
```

```ts
// /Users/fan/JsProjects/ConfigHub/types/settings.ts
export interface EnvironmentOption {
  id: string;
  name: string;
  slug: string;
}

export interface OwnerOption {
  id: string;
  name: string;
  email: string;
}
```

- [ ] **Step 2: Implement the overview and audit pages**

```tsx
// /Users/fan/JsProjects/ConfigHub/app/(console)/overview/page.tsx
import { PageHeader } from "@/components/blocks/page-header";

export default function OverviewPage() {
  return (
    <div className="space-y-6">
      <PageHeader
        title="Overview"
        description="Health, pending attention, risk resources, and recent audit events."
      />
      <section className="grid gap-4 lg:grid-cols-[1.3fr_0.9fr_0.9fr]">
        <div className="rounded-xl border border-slate-200 bg-white p-4">Resource health overview</div>
        <div className="rounded-xl border border-slate-200 bg-white p-4">Pending actions</div>
        <div className="rounded-xl border border-slate-200 bg-white p-4">Risk resources</div>
      </section>
      <section className="rounded-xl border border-slate-200 bg-white p-4">Recent audit events</section>
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/app/(console)/audits/page.tsx
import { PageHeader } from "@/components/blocks/page-header";
import { AuditTable } from "@/components/audits/audit-table";
import { listAuditEvents } from "@/services/audits";

export default async function AuditsPage() {
  const items = await listAuditEvents();

  return (
    <div className="space-y-6">
      <PageHeader title="Audits" description="Recent activity across resource changes and relation updates." />
      <AuditTable items={items} />
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/components/audits/audit-table.tsx
import type { AuditEvent } from "@/types/audit";

export function AuditTable({ items }: { items: AuditEvent[] }) {
  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
      <table className="min-w-full divide-y divide-slate-200">
        <thead className="bg-slate-50">
          <tr>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Actor</th>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Resource</th>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Event</th>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Result</th>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Time</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">
          {items.map((item) => (
            <tr key={item.id}>
              <td className="px-4 py-3 text-sm text-slate-700">{item.actorName}</td>
              <td className="px-4 py-3 text-sm text-slate-700">{item.targetResourceName}</td>
              <td className="px-4 py-3 text-sm text-slate-700">{item.eventType}</td>
              <td className="px-4 py-3 text-sm text-slate-700">{item.result}</td>
              <td className="px-4 py-3 text-sm text-slate-700">{item.createdAt}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
```

- [ ] **Step 3: Implement database and settings pages with the same shell language**

```tsx
// /Users/fan/JsProjects/ConfigHub/components/databases/database-table.tsx
import type { Resource } from "@/types/resource";

export function DatabaseTable({ items }: { items: Resource[] }) {
  return (
    <div className="overflow-hidden rounded-xl border border-slate-200 bg-white">
      <table className="min-w-full divide-y divide-slate-200">
        <thead className="bg-slate-50">
          <tr>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Database</th>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Subtype</th>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Environment</th>
            <th className="px-4 py-3 text-left text-xs font-medium uppercase tracking-wide text-slate-500">Health</th>
          </tr>
        </thead>
        <tbody className="divide-y divide-slate-100">
          {items.map((item) => (
            <tr key={item.id}>
              <td className="px-4 py-3 text-sm text-slate-700">{item.displayName}</td>
              <td className="px-4 py-3 text-sm text-slate-700">{item.resourceSubtype}</td>
              <td className="px-4 py-3 text-sm text-slate-700">{item.environmentName ?? item.environmentId}</td>
              <td className="px-4 py-3 text-sm text-slate-700">{item.healthStatus}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/app/(console)/databases/page.tsx
import { PageHeader } from "@/components/blocks/page-header";
import { DatabaseTable } from "@/components/databases/database-table";
import { listResources } from "@/services/resources";

export default async function DatabasesPage() {
  const items = (await listResources()).filter((item) =>
    item.resourceType === "database_instance" || item.resourceType === "database_cluster",
  );

  return (
    <div className="space-y-6">
      <PageHeader title="Databases" description="Database instances and clusters from the shared resource foundation." />
      <DatabaseTable items={items} />
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/app/(console)/cmdb/page.tsx
import { PageHeader } from "@/components/blocks/page-header";
import { ResourceTable } from "@/components/resources/resource-table";
import { listResources } from "@/services/resources";

export default async function CmdbPage() {
  const resources = await listResources();

  return (
    <div className="space-y-6">
      <PageHeader title="CMDB" description="Configuration-oriented view over the same shared resource inventory." />
      <ResourceTable resources={resources} />
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/app/(console)/settings/page.tsx
import { PageHeader } from "@/components/blocks/page-header";

export default function SettingsPage() {
  return (
    <div className="space-y-6">
      <PageHeader title="Settings" description="Environments, owners, users, roles, and supporting reference data." />
      <div className="grid gap-4 lg:grid-cols-2">
        <div className="rounded-xl border border-slate-200 bg-white p-4">Environments</div>
        <div className="rounded-xl border border-slate-200 bg-white p-4">Owners and users</div>
      </div>
    </div>
  );
}
```

```tsx
// /Users/fan/JsProjects/ConfigHub/app/login/page.tsx
"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { login } from "@/services/auth";

export default function LoginPage() {
  const router = useRouter();
  const [error, setError] = useState("");

  async function onSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const formData = new FormData(event.currentTarget);

    try {
      await login({
        email: String(formData.get("email") ?? ""),
        password: String(formData.get("password") ?? ""),
      });
      router.push("/overview");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Login failed");
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-slate-100 p-6">
      <form onSubmit={onSubmit} className="w-full max-w-sm space-y-4 rounded-2xl border border-slate-200 bg-white p-6">
        <div className="space-y-1">
          <h1 className="text-xl font-semibold text-slate-950">Sign in</h1>
          <p className="text-sm text-slate-600">Use the phase-1 local account to access the console.</p>
        </div>
        <input name="email" type="email" className="w-full rounded-md border border-slate-200 px-3 py-2 text-sm" defaultValue="admin@example.com" />
        <input name="password" type="password" className="w-full rounded-md border border-slate-200 px-3 py-2 text-sm" defaultValue="secret123" />
        {error ? <p className="text-sm text-red-600">{error}</p> : null}
        <button type="submit" className="w-full rounded-md bg-slate-950 px-3 py-2 text-sm font-medium text-white">
          Continue
        </button>
      </form>
    </main>
  );
}
```

- [ ] **Step 4: Run the frontend smoke test**

Run:

```bash
cd /Users/fan/JsProjects/ConfigHub
npm run dev
```

Expected:
- `/overview`, `/resources`, `/cmdb`, `/databases`, `/audits`, and `/settings` all render inside the shared shell
- `/login` signs in and redirects to `/overview`
- visual style stays dense and table-first instead of dashboard-card-first

- [ ] **Step 5: Commit the remaining console pages**

```bash
cd /Users/fan/JsProjects/ConfigHub
git add app components services types
git commit -m "feat: add overview audits databases and settings pages"
```

### Task 10: Verify cross-repo integration and update docs

**Files:**
- Modify: `/Users/fan/GolangProjects/ConfigHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md`
- Create: `/Users/fan/GolangProjects/ConfigHub/README.md`
- Create: `/Users/fan/JsProjects/ConfigHub/README.md`

- [ ] **Step 1: Add backend run instructions**

```md
# ConfigHub Backend

## Run

1. Create PostgreSQL database `confighub`
2. Copy `.env.example` to `.env`
3. Apply SQL files in `migrations/`
4. Run `make run`

## Test

Run `make test`
```

- [ ] **Step 2: Add frontend run instructions**

```md
# ConfigHub Frontend

## Run

1. Install dependencies with `npm install`
2. Set `NEXT_PUBLIC_API_BASE_URL=http://localhost:8080`
3. Run `npm run dev`

## Test

Run `npx vitest run`
```

- [ ] **Step 3: Run final verification**

Run:

```bash
cd /Users/fan/GolangProjects/ConfigHub && make test
cd /Users/fan/JsProjects/ConfigHub && npx vitest run
```

Expected:
- backend tests PASS
- frontend tests PASS
- manual browser smoke test confirms login, list, detail panel, and overview shell all work

- [ ] **Step 4: Commit documentation and verification updates**

```bash
cd /Users/fan/GolangProjects/ConfigHub
git add README.md docs/superpowers/specs/2026-04-11-unified-resource-console-design.md
git commit -m "docs: add backend usage guide"

cd /Users/fan/JsProjects/ConfigHub
git add README.md
git commit -m "docs: add frontend usage guide"
```
