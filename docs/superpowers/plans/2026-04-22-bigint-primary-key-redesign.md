# BIGINT 自增主键重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace all UUID-string primary keys with `BIGINT UNSIGNED AUTO_INCREMENT`, remove database foreign keys, and align backend, OpenAPI, frontend, tests, and E2E around numeric IDs.

**Architecture:** Rebuild the schema from scratch around single-column numeric surrogate keys, then convert Go model/repository/API layers to `uint64`, and finally convert the Next.js frontend and Playwright suite to numeric IDs. The backend remains `database/sql` + repository pattern; referential integrity stays in application logic rather than MySQL `FOREIGN KEY` constraints.

**Tech Stack:** Go 1.26 + chi + `database/sql` + MySQL/goose + Schemathesis (backend), Next.js 16 + TypeScript + Vitest + Playwright (frontend)

**Working directories:**
- Backend: `/Users/fan/GolangProjects/ControlHub`
- Frontend: `/Users/fan/JsProjects/ControlHub`

**Spec:** `docs/superpowers/specs/2026-04-22-bigint-primary-key-redesign-design.md`

---

## File Structure

### Backend schema and migrations

| File | Responsibility |
|------|---------------|
| `migrations/0001_initial_schema.sql` | Rebuild all base tables with bigint PKs, no FK, comments, indexes |
| `migrations/0002_seed_reference_data.sql` | Re-seed roles/environments/owners/users without hard-coded IDs |
| `migrations/0003_expand_resource_type_constraint.sql` | Keep resource type check compatible with new base schema |
| `migrations/0004_seed_demo_data.sql` | Re-seed demo resources/profiles/relations/audits via business-key lookups |
| `migrations/0005_add_lifecycle_status_index.sql` | Keep lifecycle index aligned with rebuilt schema |
| `migrations/0006_add_resource_name_environment_unique.sql` | Keep name+environment unique index aligned with rebuilt schema |
| `migrations/0007_add_resource_archive_fields.sql` | Keep archive columns aligned with bigint IDs |
| `migrations/0008_apply_demo_seed_cleanup_patch.sql` | Neutralize/replace UUID-specific cleanup patch for fresh bigint world |

### Backend model/service/API

| File | Responsibility |
|------|---------------|
| `internal/model/resource.go` | Convert resource/read-side ID fields to `uint64` |
| `internal/model/resource_write.go` | Convert create/update request IDs to `uint64` |
| `internal/model/relation.go` | Convert relation IDs to `uint64` |
| `internal/model/relation_write.go` | Convert write-side relation IDs to `uint64` |
| `internal/model/settings.go` | Convert environment/owner/role IDs to `uint64` |
| `internal/model/auth.go` | Convert user credential ID to `uint64` |
| `internal/model/audit.go` | Convert audit IDs to `uint64` |
| `internal/model/topology.go` | Convert topology IDs and node references to `uint64` |
| `internal/api/resource_handler.go` | Parse numeric path/query/body IDs and reject invalid values |
| `internal/api/relation_handler.go` | Parse numeric relation/resource IDs |
| `internal/api/profile_handler.go` | Parse numeric resource IDs |
| `internal/api/topology_handler.go` | Parse numeric topology root ID |
| `internal/api/audit_handler.go` | Parse numeric `targetResourceId` filter |
| `internal/api/test_server.go` | Update fake repositories and fixtures to numeric IDs |
| `internal/repository/mysql/resource_repository.go` | Remove `UUID()`, use `LastInsertId()`, scan bigint IDs |
| `internal/repository/mysql/relation_repository.go` | Remove `UUID()`, use `LastInsertId()`, scan bigint IDs |
| `internal/repository/mysql/audit_repository.go` | Scan bigint IDs and filters |
| `internal/repository/mysql/dictionary_repository.go` | Scan bigint IDs |
| `internal/repository/mysql/user_repository.go` | Scan bigint user/role IDs |
| `internal/service/resource_service.go` | Use numeric IDs across validation and updates |
| `internal/service/relation_service.go` | Use numeric IDs across create/delete/list |
| `internal/service/profile_service.go` | Use numeric resource IDs |
| `internal/service/audit_service.go` | Use numeric audit/resource IDs |
| `internal/service/topology_service.go` | Use numeric graph IDs |

### Backend tests and contract files

| File | Responsibility |
|------|---------------|
| `internal/integration/mysql_test.go` | Assert bigint PKs, no FK, profile uniqueness, required indexes |
| `internal/integration/resource_test.go` | Assert resource create/read/update with numeric IDs |
| `internal/integration/relation_test.go` | Assert relation create/delete with numeric IDs |
| `internal/integration/archive_test.go` | Assert archive/unarchive with numeric IDs |
| `internal/integration/topology_test.go` | Assert topology response with numeric IDs |
| `internal/integration/openapi_fuzz_test.go` | Keep fuzzing aligned with numeric ID contract |
| `internal/api/resource_handler_test.go` | Assert numeric path/body/query parsing and 400s |
| `internal/api/relation_handler_test.go` | Assert numeric relation/resource handling |
| `internal/api/topology_handler_test.go` | Assert numeric topology route parsing |
| `internal/api/audit_handler_test.go` | Assert numeric target resource filtering |
| `internal/openapi/openapi.yaml` | Convert all ID schemas/examples/path params to integer/int64 |
| `internal/openapi/openapi_test.go` | Validate embedded OpenAPI after contract rewrite |

### Frontend wire types and UI

| File | Responsibility |
|------|---------------|
| `types/resource.ts` | Convert resource/relation/topology IDs to `number` |
| `types/audit.ts` | Convert audit IDs and target IDs to `number` |
| `types/settings.ts` | Convert environment/owner/role IDs to `number` |
| `types/view-models.ts` | Keep view models aligned with numeric IDs |
| `services/resources.ts` | Use numeric IDs in path construction and payload types |
| `services/audits.ts` | Use numeric filters/response fields |
| `services/settings.ts` | Use numeric IDs in settings resource models |
| `services/topology.ts` | Use numeric root/resource/edge IDs |
| `lib/view-models.ts` | Preserve UI mapping with numeric IDs |
| `lib/resource-summary.ts` | Avoid string assumptions when formatting IDs |
| `lib/topology-mapper.ts` | Use numeric node/edge IDs consistently |
| `lib/navigation.ts` | Keep route generation stable with numeric IDs |
| `components/resources/*.tsx` | Accept numeric IDs in resource detail/create/edit/archive components |
| `components/blocks/resource-link.tsx` | Link to numeric resource routes |
| `components/blocks/resource-relation-panel.tsx` | Use numeric resource and relation IDs |
| `components/blocks/topology-panel.tsx` | Render numeric topology IDs |
| `app/(console)/resources/[id]/page.tsx` | Parse route param as number via service layer |

### Frontend tests and E2E

| File | Responsibility |
|------|---------------|
| `tests/services/resources.test.ts` | Assert numeric ID request/response wiring |
| `tests/services/audits.test.ts` | Assert numeric audit IDs |
| `tests/services/settings.test.ts` | Assert numeric settings IDs |
| `tests/components/resource-table.test.tsx` | Assert rows and links with numeric IDs |
| `tests/components/resource-detail-sheet.test.tsx` | Assert detail sheet data with numeric IDs |
| `tests/components/resource-relation-panel.test.tsx` | Assert relation panel actions with numeric IDs |
| `tests/resource-detail-page.test.tsx` | Assert detail page loading with numeric route params |
| `e2e/api.helpers.ts` | Create/archive resources using numeric IDs |
| `e2e/resources-sheet.spec.ts` | Cover create/detail/edit flow with numeric IDs |
| `e2e/topology.spec.ts` | Cover topology flow with numeric IDs |
| `e2e/resource-archive.spec.ts` | Cover archive/unarchive flow with numeric IDs |

---

## Task 1: Rebuild schema and seed data for bigint IDs

**Files:**
- Modify: `migrations/0001_initial_schema.sql`
- Modify: `migrations/0002_seed_reference_data.sql`
- Modify: `migrations/0003_expand_resource_type_constraint.sql`
- Modify: `migrations/0004_seed_demo_data.sql`
- Modify: `migrations/0005_add_lifecycle_status_index.sql`
- Modify: `migrations/0006_add_resource_name_environment_unique.sql`
- Modify: `migrations/0007_add_resource_archive_fields.sql`
- Modify: `migrations/0008_apply_demo_seed_cleanup_patch.sql`
- Test: `internal/integration/mysql_test.go`

- [x] **Step 1: Write the failing schema assertions**

Add/replace bigint/FK assertions in `internal/integration/mysql_test.go`:

```go
func TestSchemaUsesBigintPrimaryKeysWithoutForeignKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	env := newTestEnv(t)
	db := env.DB

	assertColumnType := func(table, column, want string) {
		t.Helper()
		var got string
		err := db.QueryRowContext(ctx, `
			SELECT COLUMN_TYPE
			FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?
		`, table, column).Scan(&got)
		if err != nil {
			t.Fatalf("query column type %s.%s: %v", table, column, err)
		}
		if got != want {
			t.Fatalf("column %s.%s = %s, want %s", table, column, got, want)
		}
	}

	assertColumnType("resources", "id", "bigint unsigned")
	assertColumnType("resource_relations", "id", "bigint unsigned")
	assertColumnType("resource_relations", "from_resource_id", "bigint unsigned")
	assertColumnType("resource_profiles_host", "id", "bigint unsigned")
	assertColumnType("resource_profiles_host", "resource_id", "bigint unsigned")

	var fkCount int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.REFERENTIAL_CONSTRAINTS
		WHERE CONSTRAINT_SCHEMA = DATABASE()
	`).Scan(&fkCount)
	if err != nil {
		t.Fatalf("count foreign keys: %v", err)
	}
	if fkCount != 0 {
		t.Fatalf("expected 0 foreign keys, got %d", fkCount)
	}
}

func TestProfileTablesUseUniqueResourceIDInsteadOfPrimaryKeyResourceID(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	db := env.DB
	ctx := context.Background()

	var count int
	err := db.QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE()
		  AND TABLE_NAME = 'resource_profiles_host'
		  AND INDEX_NAME = 'uniq_resource_id'
	`).Scan(&count)
	if err != nil {
		t.Fatalf("query profile unique index: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected uniq_resource_id on resource_profiles_host, got count=%d", count)
	}
}
```

- [x] **Step 2: Run the integration schema test to verify it fails**

Run:

```bash
go test -tags=integration -count=1 -run 'TestSchemaUsesBigintPrimaryKeysWithoutForeignKeys|TestProfileTablesUseUniqueResourceIDInsteadOfPrimaryKeyResourceID' ./internal/integration
```

Expected: FAIL because current migrations still create `char(36)` IDs and FK constraints.

- [x] **Step 3: Rewrite base and follow-up migrations**

Replace the core parts of `migrations/0001_initial_schema.sql` with bigint/no-FK DDL like:

```sql
CREATE TABLE resources (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT 'Primary key ID',
  resource_type VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'Resource type',
  resource_subtype VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'Resource subtype',
  name VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Unique resource name within environment',
  display_name VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Display name',
  environment_id BIGINT UNSIGNED NOT NULL COMMENT 'Environment ID',
  owner_id BIGINT UNSIGNED NOT NULL COMMENT 'Owner ID',
  lifecycle_status VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'Lifecycle status',
  health_status VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'Health status',
  labels JSON NOT NULL COMMENT 'Resource labels JSON',
  source VARCHAR(64) NOT NULL DEFAULT 'manual' COMMENT 'Source',
  external_id VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'External system ID',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Created time',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT 'Updated time',
  PRIMARY KEY (id),
  KEY idx_environment_id (environment_id),
  KEY idx_owner_id (owner_id),
  KEY idx_resource_type (resource_type),
  KEY idx_health_status (health_status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Resources';

CREATE TABLE resource_profiles_host (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT 'Primary key ID',
  resource_id BIGINT UNSIGNED NOT NULL COMMENT 'Resource ID',
  hostname VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Hostname',
  ip_address VARCHAR(64) NOT NULL DEFAULT '' COMMENT 'IP address',
  os_name VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'Operating system name',
  spec JSON NOT NULL COMMENT 'Extended profile spec JSON',
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Created time',
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT 'Updated time',
  PRIMARY KEY (id),
  UNIQUE KEY uniq_resource_id (resource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Host resource profiles';
```

Rewrite `migrations/0002_seed_reference_data.sql` to insert reference data without hard-coded IDs, for example:

```sql
INSERT INTO roles (name, description) VALUES
  ('admin', 'Full platform access'),
  ('editor', 'Can manage assets and relations');

INSERT INTO environments (name, slug, description) VALUES
  ('Production', 'prod', 'Production environment'),
  ('Staging', 'staging', 'Staging environment');

INSERT INTO owners (name, email) VALUES
  ('Platform Team', 'platform@example.com'),
  ('DBA Team', 'dba@example.com');

INSERT INTO users (email, password_hash, display_name, role_id)
SELECT 'admin@example.com', 'fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4', 'ControlHub Admin', roles.id
FROM roles
WHERE roles.name = 'admin';
```

Rewrite `migrations/0004_seed_demo_data.sql` so every dependent insert resolves IDs by business key:

```sql
INSERT INTO resources (
  resource_type, resource_subtype, name, display_name,
  environment_id, owner_id, lifecycle_status, health_status,
  labels, source, external_id
)
SELECT
  'database_instance', 'mysql', 'orders-mysql-primary-prod', 'Orders MySQL Primary',
  e.id, o.id, 'running', 'healthy',
  JSON_OBJECT('team', 'orders', 'tier', 'db'), 'manual', 'mysql:prod:orders-primary'
FROM environments e
JOIN owners o ON o.email = 'dba@example.com'
WHERE e.slug = 'prod';

INSERT INTO resource_relations (from_resource_id, to_resource_id, relation_type)
SELECT src.id, dst.id, 'depends_on'
FROM resources src
JOIN resources dst ON dst.name = 'orders-mysql-primary-prod'
WHERE src.name = 'orders-api-prod';
```

For `migrations/0008_apply_demo_seed_cleanup_patch.sql`, neutralize the UUID-specific patch:

```sql
-- +goose Up
SELECT 1;

-- +goose Down
SELECT 1;
```

- [x] **Step 4: Re-run the schema integration tests**

Run:

```bash
go test -tags=integration -count=1 -run 'TestSchemaUsesBigintPrimaryKeysWithoutForeignKeys|TestProfileTablesUseUniqueResourceIDInsteadOfPrimaryKeyResourceID' ./internal/integration
```

Expected: PASS.

- [x] **Step 5: Commit the schema rewrite**

```bash
git add migrations/0001_initial_schema.sql migrations/0002_seed_reference_data.sql migrations/0003_expand_resource_type_constraint.sql migrations/0004_seed_demo_data.sql migrations/0005_add_lifecycle_status_index.sql migrations/0006_add_resource_name_environment_unique.sql migrations/0007_add_resource_archive_fields.sql migrations/0008_apply_demo_seed_cleanup_patch.sql internal/integration/mysql_test.go
git commit -m "refactor: rebuild schema around bigint primary keys"
```

### Task 2: Convert Go model contracts from string IDs to `uint64`

**Files:**
- Modify: `internal/model/resource.go`
- Modify: `internal/model/resource_write.go`
- Modify: `internal/model/relation.go`
- Modify: `internal/model/relation_write.go`
- Modify: `internal/model/settings.go`
- Modify: `internal/model/auth.go`
- Modify: `internal/model/audit.go`
- Modify: `internal/model/topology.go`
- Test: `internal/model/resource_test.go`
- Test: `internal/model/pagination_test.go`

- [x] **Step 1: Write failing model JSON tests**

Add a focused JSON contract test to `internal/model/resource_test.go`:

```go
func TestResourceJSONUsesNumericIDs(t *testing.T) {
	payload := `{
	  "id": 42,
	  "resourceType": "database_instance",
	  "resourceSubtype": "mysql",
	  "name": "orders-db",
	  "displayName": "Orders DB",
	  "environmentId": 7,
	  "ownerId": 9,
	  "lifecycleStatus": "running",
	  "healthStatus": "healthy",
	  "source": "manual",
	  "externalId": "mysql:orders",
	  "labels": {"team": "orders"},
	  "createdAt": "2026-04-22T00:00:00Z",
	  "updatedAt": "2026-04-22T00:00:00Z"
	}`

	var resource Resource
	if err := json.Unmarshal([]byte(payload), &resource); err != nil {
		t.Fatalf("unmarshal resource: %v", err)
	}
	if resource.ID != 42 || resource.EnvironmentID != 7 || resource.OwnerID != 9 {
		t.Fatalf("unexpected numeric IDs: %#v", resource)
	}
}

func TestRelationCreateInputJSONUsesNumericIDs(t *testing.T) {
	payload := `{"toResourceId":88,"relationType":"depends_on"}`
	var input RelationCreateInput
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		t.Fatalf("unmarshal relation input: %v", err)
	}
	if input.ToResourceID != 88 {
		t.Fatalf("ToResourceID = %d, want 88", input.ToResourceID)
	}
}
```

- [x] **Step 2: Run the model tests to verify they fail**

Run:

```bash
go test ./internal/model -run 'TestResourceJSONUsesNumericIDs|TestRelationCreateInputJSONUsesNumericIDs'
```

Expected: FAIL because current structs still declare string IDs.

- [x] **Step 3: Change every model ID field to `uint64`**

Apply concrete type changes like:

```go
type Resource struct {
	ID            uint64            `json:"id"`
	ResourceType  ResourceType      `json:"resourceType"`
	ResourceSubtype string          `json:"resourceSubtype"`
	Name          string            `json:"name"`
	DisplayName   string            `json:"displayName"`
	EnvironmentID uint64            `json:"environmentId"`
	OwnerID       uint64            `json:"ownerId"`
	LifecycleStatus string          `json:"lifecycleStatus"`
	HealthStatus  string            `json:"healthStatus"`
	Source        string            `json:"source"`
	ExternalID    string            `json:"externalId"`
	Labels        map[string]string `json:"labels"`
	ProfileSummary *ProfileSummary  `json:"profileSummary,omitempty"`
	CreatedAt     time.Time         `json:"createdAt"`
	UpdatedAt     time.Time         `json:"updatedAt"`
	ArchivedAt    *time.Time        `json:"archivedAt,omitempty"`
	ArchivedBy    *uint64           `json:"archivedBy,omitempty"`
	ArchiveReason *string           `json:"archiveReason,omitempty"`
}

type ResourceCreateInput struct {
	ResourceType    ResourceType           `json:"resourceType"`
	ResourceSubtype string                 `json:"resourceSubtype"`
	Name            string                 `json:"name"`
	DisplayName     string                 `json:"displayName"`
	EnvironmentID   uint64                 `json:"environmentId"`
	OwnerID         uint64                 `json:"ownerId"`
	LifecycleStatus LifecycleStatus        `json:"lifecycleStatus"`
	HealthStatus    HealthStatus           `json:"healthStatus"`
	Source          string                 `json:"source"`
	ExternalID      string                 `json:"externalId"`
	Labels          map[string]string      `json:"labels"`
	Profile         map[string]interface{} `json:"profile,omitempty"`
}

type RelationCreateInput struct {
	FromResourceID uint64       `json:"-"`
	ToResourceID   uint64       `json:"toResourceId"`
	RelationType   RelationType `json:"relationType"`
}
```

Also convert in the other files:

```go
type Environment struct { ID uint64 `json:"id"` }
type Owner struct { ID uint64 `json:"id"` }
type Role struct { ID uint64 `json:"id"` }
type UserCredential struct { ID uint64 `json:"id"` }
type TopologyNode struct { ID uint64 `json:"id"`; EnvironmentID uint64 `json:"environmentId"`; OwnerID uint64 `json:"ownerId"`; ReplicationParentID uint64 `json:"replicationParentId,omitempty"` }
type TopologyEdge struct { ID uint64 `json:"id"`; FromResourceID uint64 `json:"fromResourceId"`; ToResourceID uint64 `json:"toResourceId"` }
```

- [x] **Step 4: Re-run the model tests**

Run:

```bash
go test ./internal/model -run 'TestResourceJSONUsesNumericIDs|TestRelationCreateInputJSONUsesNumericIDs'
```

Expected: PASS.

- [x] **Step 5: Commit the model contract conversion**

```bash
git add internal/model/resource.go internal/model/resource_write.go internal/model/relation.go internal/model/relation_write.go internal/model/settings.go internal/model/auth.go internal/model/audit.go internal/model/topology.go internal/model/resource_test.go
git commit -m "refactor: convert backend model ids to uint64"
```

### Task 3: Rewrite repositories and services around numeric IDs

**Files:**
- Modify: `internal/repository/mysql/resource_repository.go`
- Modify: `internal/repository/mysql/relation_repository.go`
- Modify: `internal/repository/mysql/audit_repository.go`
- Modify: `internal/repository/mysql/dictionary_repository.go`
- Modify: `internal/repository/mysql/user_repository.go`
- Modify: `internal/service/resource_service.go`
- Modify: `internal/service/relation_service.go`
- Modify: `internal/service/profile_service.go`
- Modify: `internal/service/audit_service.go`
- Modify: `internal/service/topology_service.go`
- Test: `internal/service/resource_write_service_test.go`
- Test: `internal/integration/resource_test.go`
- Test: `internal/integration/relation_test.go`

- [x] **Step 1: Write failing service/integration tests for numeric create paths**

Add or update a backend test like this in `internal/service/resource_write_service_test.go`:

```go
func TestResourceServiceCreate_ReturnsNumericID(t *testing.T) {
	repo := &fakeResourceWriteRepo{
		createFunc: func(_ context.Context, input model.ResourceCreateInput) (*model.Resource, error) {
			return &model.Resource{
				ID:            101,
				ResourceType:  input.ResourceType,
				ResourceSubtype: input.ResourceSubtype,
				Name:          input.Name,
				DisplayName:   input.DisplayName,
				EnvironmentID: input.EnvironmentID,
				OwnerID:       input.OwnerID,
			}, nil
		},
	}
	service := NewResourceService(repo, fakeDictionaryRepo{}, fakeOwnerRepo{}, fakeEnvironmentRepo{})

	created, err := service.Create(context.Background(), model.ResourceCreateInput{
		ResourceType:    "database_instance",
		ResourceSubtype: "mysql",
		Name:            "orders-db",
		DisplayName:     "Orders DB",
		EnvironmentID:   1,
		OwnerID:         2,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.ID != 101 {
		t.Fatalf("created.ID = %d, want 101", created.ID)
	}
}
```

And add an integration assertion in `internal/integration/resource_test.go`:

```go
func TestResourceRepositoryCreate_UsesAutoIncrementID(t *testing.T) {
	t.Parallel()

	env := newTestEnv(t)
	repo := mysqlrepository.NewResourceRepository(env.DB)

	created, err := repo.CreateResource(context.Background(), model.ResourceCreateInput{
		ResourceType:    "database_instance",
		ResourceSubtype: "mysql",
		Name:            uniqueName("orders-db"),
		DisplayName:     "Orders DB",
		EnvironmentID:   lookupEnvironmentID(t, env.DB, "prod"),
		OwnerID:         lookupOwnerID(t, env.DB, "dba@example.com"),
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "manual",
		Labels:          map[string]string{"team": "orders"},
	})
	if err != nil {
		t.Fatalf("CreateResource() error = %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected auto-increment ID > 0")
	}
}
```

- [x] **Step 2: Run the targeted tests to verify they fail**

Run:

```bash
go test ./internal/service -run TestResourceServiceCreate_ReturnsNumericID
go test -tags=integration -count=1 -run TestResourceRepositoryCreate_UsesAutoIncrementID ./internal/integration
```

Expected: FAIL because repositories/services still use string IDs and `UUID()` generation.

- [x] **Step 3: Remove `UUID()` and use `LastInsertId()` throughout repositories**

Change repository create methods to this pattern:

```go
func (r *ResourceRepository) CreateResource(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error) {
	labelsJSON, err := json.Marshal(input.Labels)
	if err != nil {
		return nil, fmt.Errorf("marshal labels: %w", err)
	}

	result, err := r.db.ExecContext(ctx, `
		INSERT INTO resources (
			resource_type, resource_subtype, name, display_name,
			environment_id, owner_id, lifecycle_status, health_status,
			source, external_id, labels, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NOW(), NOW())
	`, input.ResourceType, input.ResourceSubtype, input.Name, input.DisplayName,
		input.EnvironmentID, input.OwnerID, string(input.LifecycleStatus), string(input.HealthStatus),
		input.Source, input.ExternalID, string(labelsJSON),
	)
	if err != nil {
		var mysqlErr *mysql.MySQLError
		if errors.As(err, &mysqlErr) && mysqlErr.Number == 1062 {
			return nil, service.ErrResourceConflict
		}
		return nil, fmt.Errorf("insert resource: %w", err)
	}

	insertID, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get inserted resource id: %w", err)
	}

	return r.GetResource(uint64(insertID))
}
```

Use the same pattern in `internal/repository/mysql/relation_repository.go`.

Update repository interfaces and service signatures from `string` to `uint64`, for example:

```go
type ResourceRepository interface {
	GetResource(id uint64) (*model.Resource, error)
	CreateResource(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error)
	UpdateResource(ctx context.Context, id uint64, input model.ResourceUpdateInput) (*model.Resource, error)
	DeleteProfile(ctx context.Context, resourceID uint64, resourceType string) error
}
```

- [x] **Step 4: Re-run the repository/service tests**

Run:

```bash
go test ./internal/service -run TestResourceServiceCreate_ReturnsNumericID
go test -tags=integration -count=1 -run 'TestResourceRepositoryCreate_UsesAutoIncrementID|TestRelationRepository' ./internal/integration
```

Expected: PASS.

- [x] **Step 5: Commit the repository/service rewrite**

```bash
git add internal/repository/mysql/resource_repository.go internal/repository/mysql/relation_repository.go internal/repository/mysql/audit_repository.go internal/repository/mysql/dictionary_repository.go internal/repository/mysql/user_repository.go internal/service/resource_service.go internal/service/relation_service.go internal/service/profile_service.go internal/service/audit_service.go internal/service/topology_service.go internal/service/resource_write_service_test.go internal/integration/resource_test.go internal/integration/relation_test.go
git commit -m "refactor: switch repositories and services to bigint ids"
```

### Task 4: Convert HTTP handlers, fake repos, and routing to numeric IDs

**Files:**
- Modify: `internal/api/resource_handler.go`
- Modify: `internal/api/relation_handler.go`
- Modify: `internal/api/profile_handler.go`
- Modify: `internal/api/topology_handler.go`
- Modify: `internal/api/audit_handler.go`
- Modify: `internal/api/test_server.go`
- Test: `internal/api/resource_handler_test.go`
- Test: `internal/api/relation_handler_test.go`
- Test: `internal/api/topology_handler_test.go`
- Test: `internal/api/audit_handler_test.go`

- [x] **Step 1: Write failing handler tests for numeric parsing and 400s**

Add tests like this to `internal/api/resource_handler_test.go`:

```go
func TestHandleGetResource_InvalidNumericIDReturns400(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/not-a-number", nil)
	resp := httptest.NewRecorder()

	server.Router.ServeHTTP(resp, req)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.Code)
	}
}

func TestHandleCreateResource_AcceptsNumericReferenceIDs(t *testing.T) {
	server := NewTestServer()
	body := `{
		"resourceType":"database_instance",
		"resourceSubtype":"mysql",
		"name":"orders-db-api-test",
		"displayName":"Orders DB",
		"environmentId":1,
		"ownerId":2,
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"labels":{}
	}`

	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()

	server.Router.ServeHTTP(resp, req)

	if resp.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 body=%s", resp.Code, resp.Body.String())
	}
}
```

- [x] **Step 2: Run the handler tests to verify they fail**

Run:

```bash
go test ./internal/api -run 'TestHandleGetResource_InvalidNumericIDReturns400|TestHandleCreateResource_AcceptsNumericReferenceIDs'
```

Expected: FAIL because handlers and test fakes still assume string IDs.

- [x] **Step 3: Add shared numeric parsing helpers and update handler signatures**

Implement concrete helpers in `internal/api/resource_handler.go` (or a small helper file if you split it during execution):

```go
func parseUint64PathParam(r *http.Request, key string) (uint64, error) {
	value := chi.URLParam(r, key)
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return id, nil
}

func parseOptionalUint64Query(q url.Values, key string) (*uint64, error) {
	value := q.Get(key)
	if value == "" {
		return nil, nil
	}
	id, err := strconv.ParseUint(value, 10, 64)
	if err != nil || id == 0 {
		return nil, fmt.Errorf("%s must be a positive integer", key)
	}
	return &id, nil
}
```

Then switch handlers to call services with numeric IDs:

```go
func handleGetResource(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64PathParam(r, "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_id", err.Error())
			return
		}
		item, err := resourceService.Get(id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, item)
	}
}
```

Update `internal/api/test_server.go` fake repositories to store IDs as `uint64`:

```go	type fakeResourceRepo struct {
	items map[uint64]model.Resource
}
```

- [x] **Step 4: Re-run handler tests**

Run:

```bash
go test ./internal/api -run 'TestHandleGetResource_InvalidNumericIDReturns400|TestHandleCreateResource_AcceptsNumericReferenceIDs|TestHandleCreateRelation|TestHandleTopology'
```

Expected: PASS.

- [x] **Step 5: Commit the API conversion**

```bash
git add internal/api/resource_handler.go internal/api/relation_handler.go internal/api/profile_handler.go internal/api/topology_handler.go internal/api/audit_handler.go internal/api/test_server.go internal/api/resource_handler_test.go internal/api/relation_handler_test.go internal/api/topology_handler_test.go internal/api/audit_handler_test.go
git commit -m "refactor: parse numeric ids across HTTP handlers"
```

### Task 5: Rewrite OpenAPI to numeric ID contract and validate it

**Files:**
- Modify: `internal/openapi/openapi.yaml`
- Modify: `internal/openapi/openapi_test.go`

- [x] **Step 1: Write a failing OpenAPI assertion for integer IDs**

Add a targeted assertion to `internal/openapi/openapi_test.go`:

```go
func TestOpenAPIUsesIntegerIDs(t *testing.T) {
	spec := string(mustReadOpenAPI(t))
	if strings.Contains(spec, "format: uuid") {
		t.Fatal("OpenAPI still contains uuid-formatted IDs")
	}
	if !strings.Contains(spec, "type: integer") {
		t.Fatal("OpenAPI does not declare integer IDs")
	}
	if !strings.Contains(spec, "format: int64") {
		t.Fatal("OpenAPI does not declare int64 IDs")
	}
}
```

- [x] **Step 2: Run the OpenAPI test to verify it fails**

Run:

```bash
go test ./internal/openapi -run TestOpenAPIUsesIntegerIDs
```

Expected: FAIL because the spec still uses string/UUID examples and schemas.

- [x] **Step 3: Convert all ID schemas, parameters, and examples in `openapi.yaml`**

Use concrete schema shapes like:

```yaml
components:
  schemas:
    Resource:
      type: object
      required: [id, resourceType, resourceSubtype, name, displayName, environmentId, ownerId, lifecycleStatus, healthStatus, source, externalId, labels, createdAt, updatedAt]
      properties:
        id:
          type: integer
          format: int64
          example: 42
        environmentId:
          type: integer
          format: int64
          example: 1
        ownerId:
          type: integer
          format: int64
          example: 2

    ResourceRelation:
      type: object
      required: [id, fromResourceId, toResourceId, relationType, createdAt]
      properties:
        id:
          type: integer
          format: int64
          example: 88
        fromResourceId:
          type: integer
          format: int64
          example: 42
        toResourceId:
          type: integer
          format: int64
          example: 43
```

And path parameters like:

```yaml
parameters:
  ResourceId:
    name: id
    in: path
    required: true
    schema:
      type: integer
      format: int64
      minimum: 1
```

- [x] **Step 4: Re-run OpenAPI validation**

Run:

```bash
go test ./internal/openapi -run TestOpenAPIUsesIntegerIDs
make openapi-validate
```

Expected: PASS.

- [x] **Step 5: Commit the OpenAPI rewrite**

```bash
git add internal/openapi/openapi.yaml internal/openapi/openapi_test.go
git commit -m "docs: switch OpenAPI id contract to integers"
```

### Task 6: Convert frontend wire types and service clients to numeric IDs

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/types/resource.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/types/audit.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/types/settings.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/types/view-models.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/services/resources.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/services/audits.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/services/settings.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/services/topology.ts`
- Test: `/Users/fan/JsProjects/ControlHub/tests/services/resources.test.ts`
- Test: `/Users/fan/JsProjects/ControlHub/tests/services/audits.test.ts`
- Test: `/Users/fan/JsProjects/ControlHub/tests/services/settings.test.ts`

- [x] **Step 1: Write failing frontend service tests for numeric IDs**

Update/add tests in `/Users/fan/JsProjects/ControlHub/tests/services/resources.test.ts` like:

```ts
it("uses numeric IDs in resource responses", async () => {
  apiClientMock.mockResolvedValue({
    items: [
      {
        id: 42,
        resourceType: "database_instance",
        resourceSubtype: "mysql",
        name: "orders-db-primary",
        displayName: "Orders DB Primary",
        environmentId: 1,
        ownerId: 2,
        lifecycleStatus: "running",
        healthStatus: "healthy",
        source: "manual",
        externalId: "mysql:prod:orders-primary",
        labels: {},
        createdAt: "2026-04-14T00:00:00Z",
        updatedAt: "2026-04-14T00:00:00Z",
        archivedAt: null,
        archivedBy: null,
        archiveReason: null,
      },
    ],
    pageInfo: { page: 1, pageSize: 20, totalItems: 1, totalPages: 1 },
  });

  const result = await listResources();
  expect(result.items[0].id).toBe(42);
});

it("builds resource detail paths from numeric ids", async () => {
  apiClientMock.mockResolvedValue({
    resource: {
      id: 42,
      resourceType: "host",
      resourceSubtype: "vm",
      name: "vm-01",
      displayName: "VM 01",
      environmentId: 1,
      ownerId: 2,
      lifecycleStatus: "running",
      healthStatus: "healthy",
      source: "manual",
      externalId: "",
      labels: {},
      createdAt: "2026-04-14T00:00:00Z",
      updatedAt: "2026-04-14T00:00:00Z",
      archivedAt: null,
      archivedBy: null,
      archiveReason: null,
    },
  });

  await getResourceById(42);
  expect(apiClientMock).toHaveBeenCalledWith("/resources/42");
});
```

- [x] **Step 2: Run the frontend service tests to verify they fail**

Run:

```bash
cd /Users/fan/JsProjects/ControlHub && npm run test -- tests/services/resources.test.ts tests/services/audits.test.ts tests/services/settings.test.ts
```

Expected: FAIL because type declarations and service signatures still use `string` IDs.

- [x] **Step 3: Convert frontend types and services to `number` IDs**

Apply concrete changes in `/Users/fan/JsProjects/ControlHub/types/resource.ts`:

```ts
export type Resource = {
  id: number;
  resourceType: ResourceType;
  resourceSubtype: string;
  name: string;
  displayName: string;
  environmentId: number;
  ownerId: number;
  lifecycleStatus: string;
  healthStatus: string;
  source: string;
  externalId: string;
  labels: Record<string, string>;
  profileSummary?: ProfileSummary | null;
  createdAt: string;
  updatedAt: string;
  archivedAt: string | null;
  archivedBy: number | null;
  archiveReason: string | null;
};

export type ResourceRelation = {
  id: number;
  fromResourceId: number;
  toResourceId: number;
  relationType: string;
  createdAt: string;
  relatedResource?: RelatedResourceSummary | null;
};

export type CreateResourceRelationInput = {
  toResourceId: number;
  relationType: string;
};
```

Update service signatures in `/Users/fan/JsProjects/ControlHub/services/resources.ts`:

```ts
export async function getResourceById(id: number): Promise<ResourceDetailResponse | null> {
  try {
    const response = await apiClient<ResourceDetailResponse>(`/resources/${id}`);
    if ("resource" in response && response.resource) {
      return response;
    }
    return { resource: response as unknown as Resource };
  } catch (error) {
    if (error instanceof ApiError && error.status === 404) {
      return null;
    }
    throw error;
  }
}

export async function updateResource(id: number, input: UpdateResourceInput): Promise<Resource> {
  return apiClient<Resource>(`/resources/${id}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
}
```

- [x] **Step 4: Re-run the frontend service tests**

Run:

```bash
cd /Users/fan/JsProjects/ControlHub && npm run test -- tests/services/resources.test.ts tests/services/audits.test.ts tests/services/settings.test.ts
```

Expected: PASS.

- [x] **Step 5: Commit the frontend wire-type conversion**

```bash
git -C /Users/fan/JsProjects/ControlHub add types/resource.ts types/audit.ts types/settings.ts types/view-models.ts services/resources.ts services/audits.ts services/settings.ts services/topology.ts tests/services/resources.test.ts tests/services/audits.test.ts tests/services/settings.test.ts
git -C /Users/fan/JsProjects/ControlHub commit -m "refactor: convert frontend api ids to numbers"
```

### Task 7: Update frontend pages/components for numeric ID flows

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/resources/resource-table.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/resources/resource-detail-sheet.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/resources/edit-resource-sheet.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/resources/create-resource-sheet.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/resources/resource-archive-button.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/blocks/resource-link.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/blocks/resource-relation-panel.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx`
- Modify: `/Users/fan/JsProjects/ControlHub/lib/view-models.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts`
- Test: `/Users/fan/JsProjects/ControlHub/tests/components/resource-table.test.tsx`
- Test: `/Users/fan/JsProjects/ControlHub/tests/components/resource-detail-sheet.test.tsx`
- Test: `/Users/fan/JsProjects/ControlHub/tests/components/resource-relation-panel.test.tsx`
- Test: `/Users/fan/JsProjects/ControlHub/tests/resource-detail-page.test.tsx`

- [x] **Step 1: Write failing UI tests for numeric IDs**

Update tests to use numeric fixtures, for example in `/Users/fan/JsProjects/ControlHub/tests/components/resource-table.test.tsx`:

```ts
const resource = {
  id: 42,
  resourceType: "database_instance",
  resourceSubtype: "mysql",
  name: "orders-db-primary",
  displayName: "Orders DB Primary",
  environmentId: 1,
  ownerId: 2,
  lifecycleStatus: "running",
  healthStatus: "healthy",
  source: "manual",
  externalId: "mysql:prod:orders-primary",
  labels: {},
  createdAt: "2026-04-14T00:00:00Z",
  updatedAt: "2026-04-14T00:00:00Z",
  archivedAt: null,
  archivedBy: null,
  archiveReason: null,
};

it("renders numeric resource links", () => {
  render(<ResourceTable resources={[resource]} />);
  expect(screen.getByRole("link", { name: /Orders DB Primary/i })).toHaveAttribute("href", "/resources/42");
});
```

And in `/Users/fan/JsProjects/ControlHub/tests/resource-detail-page.test.tsx`:

```ts
it("loads detail page with numeric route params", async () => {
  mockedGetResourceById.mockResolvedValue({ resource: resourceFixture(42) });
  const page = await ResourceDetailPage({ params: Promise.resolve({ id: "42" }) });
  expect(page).toBeTruthy();
  expect(mockedGetResourceById).toHaveBeenCalledWith(42);
});
```

- [x] **Step 2: Run the focused frontend UI tests to verify they fail**

Run:

```bash
cd /Users/fan/JsProjects/ControlHub && npm run test -- tests/components/resource-table.test.tsx tests/components/resource-detail-sheet.test.tsx tests/components/resource-relation-panel.test.tsx tests/resource-detail-page.test.tsx
```

Expected: FAIL because components and page loaders still assume string IDs.

- [x] **Step 3: Convert pages/components to numeric IDs end-to-end**

Apply concrete changes such as numeric param parsing in `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`:

```ts
export default async function ResourceDetailPage({
  params,
}: {
  params: Promise<{ id: string }>;
}) {
  const { id } = await params;
  const resourceId = Number(id);
  if (!Number.isInteger(resourceId) || resourceId <= 0) {
    notFound();
  }

  const detail = await getResourceById(resourceId);
  if (!detail) {
    notFound();
  }

  return <ResourceDetailSheet resource={detail.resource} />;
}
```

And numeric prop signatures in UI components:

```ts
type ResourceArchiveButtonProps = {
  resourceId: number;
  isArchived: boolean;
  archiveReason?: string | null;
};

export function ResourceLink({ resourceId, label }: { resourceId: number; label: string }) {
  return <Link href={`/resources/${resourceId}`}>{label}</Link>;
}
```

- [x] **Step 4: Re-run the focused frontend UI tests**

Run:

```bash
cd /Users/fan/JsProjects/ControlHub && npm run test -- tests/components/resource-table.test.tsx tests/components/resource-detail-sheet.test.tsx tests/components/resource-relation-panel.test.tsx tests/resource-detail-page.test.tsx
```

Expected: PASS.

- [x] **Step 5: Commit the UI conversion**

```bash
git -C /Users/fan/JsProjects/ControlHub add app/(console)/resources/[id]/page.tsx components/resources/resource-table.tsx components/resources/resource-detail-sheet.tsx components/resources/edit-resource-sheet.tsx components/resources/create-resource-sheet.tsx components/resources/resource-archive-button.tsx components/blocks/resource-link.tsx components/blocks/resource-relation-panel.tsx components/blocks/topology-panel.tsx lib/view-models.ts lib/topology-mapper.ts tests/components/resource-table.test.tsx tests/components/resource-detail-sheet.test.tsx tests/components/resource-relation-panel.test.tsx tests/resource-detail-page.test.tsx
git -C /Users/fan/JsProjects/ControlHub commit -m "refactor: update resource UI flows for numeric ids"
```

### Task 8: Update Playwright E2E for numeric ID paths and real backend flow

**Files:**
- Modify: `/Users/fan/JsProjects/ControlHub/e2e/api.helpers.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/e2e/resources-sheet.spec.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/e2e/resource-archive.spec.ts`
- Modify: `/Users/fan/JsProjects/ControlHub/e2e/topology.spec.ts`

- [x] **Step 1: Write failing E2E helper assertions for numeric IDs**

Update `/Users/fan/JsProjects/ControlHub/e2e/api.helpers.ts` to expect numeric IDs in helper types:

```ts
export type E2EResource = {
  id: number;
  name: string;
  displayName: string;
  resourceType: string;
  resourceSubtype: string;
  environmentId: number;
  ownerId: number;
};

export async function createTestResource(token: string, input: CreateResourceInput): Promise<E2EResource> {
  const response = await fetch(`${API_BASE_URL}/resources`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify(input),
  });
  expect(response.ok()).toBeTruthy();
  return (await response.json()) as E2EResource;
}
```

Update `/Users/fan/JsProjects/ControlHub/e2e/resources-sheet.spec.ts` assertions to explicitly check numeric route usage:

```ts
test("detail route uses numeric resource id", async ({ page }) => {
  await page.goto("/resources");
  await page.getByPlaceholder("Search resource, owner, or ID").fill(resourceName);
  await page.locator("table tbody tr").first().click();
  await expect(page.locator('[data-slot="sheet-content"]')).toBeVisible();
  await expect(page.locator(`a[href="/resources/${resourceId}"]`).first()).toBeVisible();
});
```

- [x] **Step 2: Run the E2E slice to verify it fails**

Run:

```bash
cd /Users/fan/JsProjects/ControlHub && npm run test:e2e -- e2e/resources-sheet.spec.ts e2e/resource-archive.spec.ts e2e/topology.spec.ts
```

Expected: FAIL because helper types and UI flows still assume string IDs.

- [x] **Step 3: Convert E2E helpers and scenarios to numeric ID semantics**

Use numeric helper signatures:

```ts
export async function archiveTestResource(token: string, resourceId: number): Promise<void> {
  const response = await fetch(`${API_BASE_URL}/resources/${resourceId}/archive`, {
    method: "POST",
    headers: {
      Authorization: `Bearer ${token}`,
      "Content-Type": "application/json",
    },
    body: JSON.stringify({ reason: "e2e cleanup" }),
  });
  expect(response.ok()).toBeTruthy();
}
```

And make the scenario cover the full required path:

```ts
test("create, view, edit, relate, and inspect topology with numeric ids", async ({ page }) => {
  await page.goto("/resources");
  await page.getByRole("button", { name: /create resource/i }).click();
  await page.getByLabel(/name/i).fill(resourceName);
  await page.getByRole("button", { name: /save/i }).click();
  await expect(page.locator('[data-slot="sheet-content"]')).toContainText(resourceName);
  await expect(page.locator(`text=/ID\s*${resourceId}/`)).toBeVisible();
});
```

- [x] **Step 4: Re-run the E2E slice**

Run:

```bash
cd /Users/fan/JsProjects/ControlHub && npm run test:e2e -- e2e/resources-sheet.spec.ts e2e/resource-archive.spec.ts e2e/topology.spec.ts
```

Expected: PASS.

- [x] **Step 5: Commit the E2E rewrite**

```bash
git -C /Users/fan/JsProjects/ControlHub add e2e/api.helpers.ts e2e/resources-sheet.spec.ts e2e/resource-archive.spec.ts e2e/topology.spec.ts
git -C /Users/fan/JsProjects/ControlHub commit -m "test: cover numeric id flows in e2e"
```

### Task 9: Run full verification and parallel multi-role review

**Files:**
- Modify: `docs/superpowers/specs/2026-04-22-bigint-primary-key-redesign-design.md` (only if review feedback changes the design)
- Test: backend and frontend suites listed below

- [x] **Step 1: Run backend verification suite**

Run:

```bash
cd /Users/fan/GolangProjects/ControlHub && make test
cd /Users/fan/GolangProjects/ControlHub && make test-integration
cd /Users/fan/GolangProjects/ControlHub && make openapi-validate
```

Expected: all PASS.

- [x] **Step 2: Run frontend verification suite**

Run:

```bash
cd /Users/fan/JsProjects/ControlHub && npm run test
cd /Users/fan/JsProjects/ControlHub && npm run build
cd /Users/fan/JsProjects/ControlHub && npm run test:e2e
```

Expected: all PASS.

- [x] **Step 3: Dispatch the required parallel review agents**

Run these review tracks in parallel after tests are green:

```text
Frontend review agents:
- Frontend Developer
- UX Architect
- UX Researcher
- UI Designer
- Evidence Collector
- API Tester

Backend review agents:
- Backend Architect
- Code Reviewer
- Security Engineer
- API Tester
- Database Optimizer
```

Each prompt must include:

```text
- What changed: bigint PK redesign with numeric IDs across backend/frontend/OpenAPI/E2E
- Scope: database schema, Go models/repos/api, Next.js types/services/components, Playwright flows
- Evidence to check: test output, OpenAPI contract, screenshots or page snapshots, SQL/index design
- Acceptance bar: no UUID assumptions, no foreign keys, no string/number dual-track, no critical UX/API/security regressions
```

- [x] **Step 4: Fix review findings and re-run the smallest failing validation**

Use the review output to patch only confirmed issues, then re-run the minimal necessary checks, e.g.:

```bash
cd /Users/fan/GolangProjects/ControlHub && go test ./internal/api -run TestHandleCreateResource_AcceptsNumericReferenceIDs
cd /Users/fan/JsProjects/ControlHub && npm run test -- tests/components/resource-detail-sheet.test.tsx
cd /Users/fan/JsProjects/ControlHub && npm run test:e2e -- e2e/resources-sheet.spec.ts
```

Expected: PASS after fixes.

- [x] **Step 5: Commit the verified final state**

```bash
git add docs/superpowers/specs/2026-04-22-bigint-primary-key-redesign-design.md
git commit -m "refactor: finalize bigint primary key redesign"
```

---

## Self-Review

### Spec coverage

- Schema rebuild/no-FK/profile unique-key model → Task 1
- Go `uint64` ID model conversion → Task 2
- Repository/service `LastInsertId()` and numeric IDs → Task 3
- API path/query/body numeric parsing → Task 4
- OpenAPI integer contract → Task 5
- Frontend number-based wire contract → Task 6
- Frontend pages/components numeric ID flow → Task 7
- Required E2E coverage → Task 8
- Parallel multi-role agent review → Task 9

No spec gap remains.

### Placeholder scan

- No `TODO` / `TBD`
- No “similar to Task N” references
- Every task includes explicit files, code, commands, and expected outcomes

### Type consistency

- Backend ID type is consistently `uint64`
- Frontend ID type is consistently `number`
- OpenAPI ID type is consistently `integer` + `int64`
- No task reintroduces string UUID compatibility

---

Plan complete and saved to `docs/superpowers/plans/2026-04-22-bigint-primary-key-redesign.md`. Two execution options:

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**