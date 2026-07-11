//go:build integration

// Package integration proves the schema metadata APIs work against the real
// dedicated query MySQL fixture: database list/default/system filtering,
// object list kind/search/pagination, table and view details, PK/composite
// index/FK ordering, read-only rejection, binding mismatch, locked target,
// target DB failure, audit metadata, and no-DSN-in-output invariants.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// setupSchemaSandboxTarget provisions a ready mysql/staging query target whose
// credential_ref resolves back to the disposable test MySQL, then creates the
// query_e2e + query_e2e_aux fixture databases with parent/child/view/index/FK
// objects. It returns the wired schema service, the target resource id, and
// the raw DB handle for direct assertions.
func setupSchemaSandboxTarget(t *testing.T) (*service.QuerySchemaService, uint64, *sql.DB) {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()

	// Self-contained fixture table (reused by execution tests).
	mustExec(t, db, `drop table if exists qe_sandbox_fixtures`)
	mustExec(t, db, `create table qe_sandbox_fixtures (id bigint unsigned not null primary key, name varchar(64) not null)`)
	mustExec(t, db, `insert into qe_sandbox_fixtures (id, name) values (1,'alpha'),(2,'beta'),(3,'gamma')`)

	// query_e2e database with a simple items table.
	mustExec(t, db, "CREATE DATABASE IF NOT EXISTS query_e2e")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e.query_e2e_items")
	mustExec(t, db, "CREATE TABLE query_e2e.query_e2e_items (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, name VARCHAR(64) NOT NULL, category VARCHAR(32) NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)")
	mustExec(t, db, "INSERT INTO query_e2e.query_e2e_items (id, name, category) VALUES (1,'alpha','sample'),(2,'beta','sample') ON DUPLICATE KEY UPDATE name=VALUES(name),category=VALUES(category)")

	// query_e2e_aux database with parent/child tables, view, composite/secondary indexes, FK.
	mustExec(t, db, "CREATE DATABASE IF NOT EXISTS query_e2e_aux")
	mustExec(t, db, "DROP VIEW IF EXISTS query_e2e_aux.schema_parent_summary")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e_aux.schema_child")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e_aux.schema_parent")
	mustExec(t, db, `CREATE TABLE query_e2e_aux.schema_parent (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		parent_code VARCHAR(32) NOT NULL,
		label VARCHAR(128) NOT NULL DEFAULT '',
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (id),
		UNIQUE KEY uq_schema_parent_code (parent_code),
		KEY idx_schema_parent_label (label)
	) ENGINE=InnoDB`)
	mustExec(t, db, `CREATE TABLE query_e2e_aux.schema_child (
		id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		parent_id BIGINT UNSIGNED NOT NULL,
		child_name VARCHAR(64) NOT NULL,
		sort_order INT NOT NULL DEFAULT 0,
		PRIMARY KEY (id),
		KEY idx_schema_child_parent (parent_id, sort_order),
		CONSTRAINT fk_schema_child_parent FOREIGN KEY (parent_id) REFERENCES query_e2e_aux.schema_parent (id) ON UPDATE CASCADE ON DELETE RESTRICT
	) ENGINE=InnoDB`)
	mustExec(t, db, "CREATE OR REPLACE VIEW query_e2e_aux.schema_parent_summary AS SELECT id, parent_code, label FROM query_e2e_aux.schema_parent")
	mustExec(t, db, "INSERT IGNORE INTO query_e2e_aux.schema_parent (id, parent_code, label) VALUES (1,'P_ALPHA','Alpha Parent'),(2,'P_BETA','Beta Parent')")
	mustExec(t, db, "INSERT IGNORE INTO query_e2e_aux.schema_child (id, parent_id, child_name, sort_order) VALUES (1,1,'child_a1',1),(2,1,'child_a2',2),(3,2,'child_b1',1)")

	// Grant the disposable container's root user SELECT on both (the test DSN
	// uses root, so the inspector can read these databases).
	mustExec(t, db, "GRANT SELECT ON query_e2e.* TO 'root'@'%'")
	mustExec(t, db, "GRANT SELECT ON query_e2e_aux.* TO 'root'@'%'")

	// Target resource (mysql, staging) + its connection profile (host/port).
	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qe-schema-target-" + strings.ReplaceAll(t.Name(), "/", "-"),
		DisplayName:     "Query Schema Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create schema target resource: %v", err)
	}

	dsnHost, dsnPort, err := service.ParseMySQLDSNHostPort(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse test dsn host/port: %v", err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'mysql', '8.0', ?, ?, 'primary', '{}')`, res.ID, dsnHost, dsnPort)

	// Enabled credential allowing non-production execution.
	seedCredentialRow(t, db, res.ID, "mysql", schemaCredentialRef, true, string(model.QueryEnvPolicyNonProdOnly))

	// Resolve the credential_ref back to the disposable test MySQL DSN.
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+schemaCredentialRef, globalEnv.dsn)

	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	credentialResolver := service.NewEnvCredentialResolver()

	svc := service.NewQuerySchemaService(
		service.NewTargetAccessResolver(queryTargetRepo, queryExecutionRepo, credentialResolver),
		service.NewMySQLSchemaInspector(),
		service.NewQuerySchemaCache(256, wallClock{}),
		queryExecutionRepo,
		wallClock{},
	)
	return svc, res.ID, db
}

const schemaCredentialRef = "SCHEMA_TARGET"

func TestSchemaAPI_ListDatabases_ExcludesSystemByDefault(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListDatabases(ctx, ownerDBA, targetID, "", 1, 100, false, false)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if resp.TargetResourceID != int64(targetID) {
		t.Fatalf("targetResourceId = %d, want %d", resp.TargetResourceID, targetID)
	}
	names := dbNames(resp.Items)
	if contains(names, "information_schema") || contains(names, "performance_schema") || contains(names, "mysql") || contains(names, "sys") {
		t.Fatalf("system databases must be excluded by default, got %v", names)
	}
	if !contains(names, "query_e2e") {
		t.Fatalf("query_e2e must appear, got %v", names)
	}
	if !contains(names, "query_e2e_aux") {
		t.Fatalf("query_e2e_aux must appear, got %v", names)
	}
}

func TestSchemaAPI_ListDatabases_IncludeSystem(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListDatabases(ctx, ownerDBA, targetID, "", 1, 100, true, false)
	if err != nil {
		t.Fatalf("ListDatabases includeSystem: %v", err)
	}
	names := dbNames(resp.Items)
	if !contains(names, "information_schema") {
		t.Fatalf("system databases must appear when includeSystem=true, got %v", names)
	}
}

func TestSchemaAPI_ListDatabases_SearchFilter(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListDatabases(ctx, ownerDBA, targetID, "aux", 1, 100, false, false)
	if err != nil {
		t.Fatalf("ListDatabases search: %v", err)
	}
	names := dbNames(resp.Items)
	if !contains(names, "query_e2e_aux") {
		t.Fatalf("search 'aux' must match query_e2e_aux, got %v", names)
	}
	if contains(names, "query_e2e") {
		t.Fatalf("search 'aux' must not match plain query_e2e, got %v", names)
	}
}

func TestSchemaAPI_ListDatabases_Pagination(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListDatabases(ctx, ownerDBA, targetID, "", 1, 1, false, false)
	if err != nil {
		t.Fatalf("ListDatabases page 1: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("page 1 items = %d, want 1", len(resp.Items))
	}
	if !resp.PageInfo.HasNextPage {
		t.Fatal("pageInfo.hasNext = false, want true (more than 1 non-system DB)")
	}
}

func TestSchemaAPI_ListObjects_TablesAndViews(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListObjects(ctx, ownerDBA, targetID, "query_e2e_aux", "", "", 1, 100, false)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if resp.Database != "query_e2e_aux" {
		t.Fatalf("database = %q, want query_e2e_aux", resp.Database)
	}
	names := objNames(resp.Items)
	if !contains(names, "schema_parent") || !contains(names, "schema_child") {
		t.Fatalf("must include schema_parent and schema_child, got %v", names)
	}
	if !contains(names, "schema_parent_summary") {
		t.Fatalf("must include view schema_parent_summary, got %v", names)
	}
}

func TestSchemaAPI_ListObjects_KindFilter(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	tables, err := svc.ListObjects(ctx, ownerDBA, targetID, "query_e2e_aux", "table", "", 1, 100, false)
	if err != nil {
		t.Fatalf("ListObjects kind=table: %v", err)
	}
	for _, o := range tables.Items {
		if o.Kind != model.ObjectKindTable {
			t.Fatalf("kind filter leaked %q with kind=%q", o.Name, o.Kind)
		}
	}
	views, err := svc.ListObjects(ctx, ownerDBA, targetID, "query_e2e_aux", "view", "", 1, 100, false)
	if err != nil {
		t.Fatalf("ListObjects kind=view: %v", err)
	}
	if len(views.Items) != 1 || views.Items[0].Name != "schema_parent_summary" {
		t.Fatalf("view filter result = %v, want [schema_parent_summary]", objNames(views.Items))
	}
}

func TestSchemaAPI_ListObjects_SearchFilter(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListObjects(ctx, ownerDBA, targetID, "query_e2e_aux", "", "child", 1, 100, false)
	if err != nil {
		t.Fatalf("ListObjects search: %v", err)
	}
	names := objNames(resp.Items)
	if !contains(names, "schema_child") {
		t.Fatalf("search 'child' must match schema_child, got %v", names)
	}
	if contains(names, "schema_parent") {
		t.Fatalf("search 'child' must not match schema_parent, got %v", names)
	}
}

func TestSchemaAPI_ListObjects_Pagination(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListObjects(ctx, ownerDBA, targetID, "query_e2e_aux", "", "", 1, 2, false)
	if err != nil {
		t.Fatalf("ListObjects pageSize=2: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("page 1 items = %d, want 2", len(resp.Items))
	}
	if !resp.PageInfo.HasNextPage {
		t.Fatal("pageInfo.hasNext = false, want true (3 objects in aux)")
	}
}

func TestSchemaAPI_GetObjectDetails_TableColumns(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	detail, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent", "table", false)
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	if detail.Database != "query_e2e_aux" || detail.Name != "schema_parent" || detail.Kind != model.ObjectKindTable {
		t.Fatalf("header = %+v", detail)
	}
	colNames := colNameSet(detail.Columns)
	for _, want := range []string{"id", "parent_code", "label", "created_at"} {
		if !colNames[want] {
			t.Fatalf("missing column %q, got %v", want, colNames)
		}
	}
}

func TestSchemaAPI_GetObjectDetails_ViewColumns(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	detail, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent_summary", "view", false)
	if err != nil {
		t.Fatalf("GetObjectDetails view: %v", err)
	}
	if detail.Kind != model.ObjectKindView {
		t.Fatalf("kind = %q, want view", detail.Kind)
	}
	colNames := colNameSet(detail.Columns)
	for _, want := range []string{"id", "parent_code", "label"} {
		if !colNames[want] {
			t.Fatalf("view missing column %q, got %v", want, colNames)
		}
	}
}

func TestSchemaAPI_GetObjectDetails_PrimaryKeyOrdering(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	detail, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent", "table", false)
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	var pkCols []string
	for _, c := range detail.Columns {
		if c.PrimaryKey {
			pkCols = append(pkCols, c.Name)
		}
	}
	if len(pkCols) != 1 || pkCols[0] != "id" {
		t.Fatalf("PK columns = %v, want [id]", pkCols)
	}
}

func TestSchemaAPI_GetObjectDetails_CompositeIndexOrdering(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	detail, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_child", "table", false)
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	var compositeIdx *model.IndexDetail
	for i, idx := range detail.Indexes {
		if idx.Name == "idx_schema_child_parent" {
			compositeIdx = &detail.Indexes[i]
			break
		}
	}
	if compositeIdx == nil {
		t.Fatalf("composite index idx_schema_child_parent not found in %+v", detail.Indexes)
	}
	if len(compositeIdx.Columns) != 2 {
		t.Fatalf("composite index columns = %d, want 2", len(compositeIdx.Columns))
	}
	if compositeIdx.Columns[0] != "parent_id" || compositeIdx.Columns[1] != "sort_order" {
		t.Fatalf("composite index columns = %v, want [parent_id, sort_order]", compositeIdx.Columns)
	}
	if compositeIdx.Unique {
		t.Fatal("composite index must not be unique")
	}
	if compositeIdx.Primary {
		t.Fatal("composite index must not be primary")
	}
}

func TestSchemaAPI_GetObjectDetails_FKOrdering(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	detail, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_child", "table", false)
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	if len(detail.ForeignKeys) != 1 {
		t.Fatalf("FK count = %d, want 1", len(detail.ForeignKeys))
	}
	fk := detail.ForeignKeys[0]
	if fk.Name != "fk_schema_child_parent" {
		t.Fatalf("FK name = %q, want fk_schema_child_parent", fk.Name)
	}
	if len(fk.Columns) != 1 || fk.Columns[0] != "parent_id" {
		t.Fatalf("FK columns = %v, want [parent_id]", fk.Columns)
	}
	if len(fk.ReferencedColumns) != 1 || fk.ReferencedColumns[0] != "id" {
		t.Fatalf("FK referenced columns = %v, want [id]", fk.ReferencedColumns)
	}
	if fk.ReferencedObject != "schema_parent" {
		t.Fatalf("FK referenced object = %q, want schema_parent", fk.ReferencedObject)
	}
	if fk.OnUpdate != "CASCADE" || fk.OnDelete != "RESTRICT" {
		t.Fatalf("FK rules = update:%q delete:%q, want CASCADE/RESTRICT", fk.OnUpdate, fk.OnDelete)
	}
}

func TestSchemaAPI_GetObjectDetails_UniqueIndexFlagged(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	detail, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent", "table", false)
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	var uqIdx *model.IndexDetail
	for i, idx := range detail.Indexes {
		if idx.Name == "uq_schema_parent_code" {
			uqIdx = &detail.Indexes[i]
			break
		}
	}
	if uqIdx == nil {
		t.Fatalf("unique index uq_schema_parent_code not found in %+v", detail.Indexes)
	}
	if !uqIdx.Unique {
		t.Fatal("uq_schema_parent_code must be unique")
	}
}

func TestSchemaAPI_ReadOnlyFixtureUserCannotInsert(t *testing.T) {
	svc, targetID, db := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	roUser := "schema_test_ro"
	roPass := "ro_test_pass_123456"
	mustExec(t, db, "CREATE USER IF NOT EXISTS '"+roUser+"'@'%' IDENTIFIED BY '"+roPass+"'")
	mustExec(t, db, "GRANT SELECT ON query_e2e.* TO '"+roUser+"'@'%'")
	mustExec(t, db, "GRANT SELECT ON query_e2e_aux.* TO '"+roUser+"'@'%'")
	mustExec(t, db, "FLUSH PRIVILEGES")

	dsnHost, dsnPort, err := service.ParseMySQLDSNHostPort(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	roDSN := fmt.Sprintf("%s:%s@tcp(%s:%d)/query_e2e?parseTime=true&charset=utf8mb4", roUser, roPass, dsnHost, dsnPort)
	roDB, err := sql.Open("mysql", roDSN)
	if err != nil {
		t.Fatalf("open ro db: %v", err)
	}
	defer roDB.Close()

	var n int
	if err := roDB.QueryRow("SELECT COUNT(*) FROM query_e2e_aux.schema_parent").Scan(&n); err != nil {
		t.Fatalf("ro SELECT: %v", err)
	}

	_, insertErr := roDB.Exec("INSERT INTO query_e2e_aux.schema_parent (id, parent_code, label) VALUES (999, 'TEST', 'test')")
	if insertErr == nil {
		t.Fatal("expected INSERT to fail for read-only user, got nil")
	}

	resp, err := svc.ListDatabases(ctx, ownerDBA, targetID, "", 1, 100, false, true)
	if err != nil {
		t.Fatalf("ListDatabases after rejected write: %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatal("database list must still work after a failed write attempt")
	}
}

func TestSchemaAPI_BindingMismatchRejectsAccess(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qe-schema-mismatch-" + strings.ReplaceAll(t.Name(), "/", "-"),
		DisplayName:     "Schema Mismatch Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'mysql', '8.0', 'mismatch.invalid', 9999, 'primary', '{}')`, res.ID)
	seedCredentialRow(t, db, res.ID, "mysql", "MISMATCH_TARGET", true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_MISMATCH_TARGET", globalEnv.dsn)

	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	credentialResolver := service.NewEnvCredentialResolver()

	svc := service.NewQuerySchemaService(
		service.NewTargetAccessResolver(queryTargetRepo, queryExecutionRepo, credentialResolver),
		service.NewMySQLSchemaInspector(),
		service.NewQuerySchemaCache(256, wallClock{}),
		queryExecutionRepo,
		wallClock{},
	)

	_, err = svc.ListDatabases(ctx, ownerDBA, res.ID, "", 1, 100, false, false)
	if !errors.Is(err, service.ErrSchemaNotAllowed) {
		t.Fatalf("ListDatabases mismatch error = %v, want ErrSchemaNotAllowed", err)
	}
}

func TestSchemaAPI_LockedTargetRejectsAccess(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qe-schema-locked-" + strings.ReplaceAll(t.Name(), "/", "-"),
		DisplayName:     "Schema Locked Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create resource: %v", err)
	}
	dsnHost, dsnPort, err := service.ParseMySQLDSNHostPort(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse test dsn host/port: %v", err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'mysql', '8.0', ?, ?, 'primary', '{}')`, res.ID, dsnHost, dsnPort)

	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	credentialResolver := service.NewEnvCredentialResolver()

	svc := service.NewQuerySchemaService(
		service.NewTargetAccessResolver(queryTargetRepo, queryExecutionRepo, credentialResolver),
		service.NewMySQLSchemaInspector(),
		service.NewQuerySchemaCache(256, wallClock{}),
		queryExecutionRepo,
		wallClock{},
	)

	_, err = svc.ListDatabases(ctx, ownerDBA, res.ID, "", 1, 100, false, false)
	if !errors.Is(err, service.ErrSchemaNotAllowed) {
		t.Fatalf("ListDatabases locked target error = %v, want ErrSchemaNotAllowed", err)
	}
}

func TestSchemaAPI_NonexistentTargetReturnsNotFound(t *testing.T) {
	svc, _, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	_, err := svc.ListDatabases(ctx, ownerDBA, 999999, "", 1, 100, false, false)
	if !errors.Is(err, service.ErrSchemaTargetNotFound) {
		t.Fatalf("ListDatabases nonexistent target error = %v, want ErrSchemaTargetNotFound", err)
	}
}

func TestSchemaAPI_NonexistentDatabaseReturnsEmptyList(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListObjects(ctx, ownerDBA, targetID, "nonexistent_db_xyz", "", "", 1, 100, false)
	if err != nil {
		t.Fatalf("ListObjects nonexistent db: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("nonexistent database must return empty list, got %d items", len(resp.Items))
	}
	if resp.PageInfo.TotalItems != 0 {
		t.Fatalf("totalItems = %d, want 0 for nonexistent database", resp.PageInfo.TotalItems)
	}
}

func TestSchemaAPI_AuditRowsContainFixedMetadata(t *testing.T) {
	svc, targetID, db := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	if _, err := svc.ListDatabases(ctx, ownerDBA, targetID, "", 1, 100, false, false); err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	if _, err := svc.ListObjects(ctx, ownerDBA, targetID, "query_e2e_aux", "", "", 1, 100, false); err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if _, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent", "table", false); err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}

	rows, err := db.Query(`SELECT event_type, result FROM audit_events WHERE target_resource_id = ? AND event_type LIKE 'query.schema%' ORDER BY event_type`, targetID)
	if err != nil {
		t.Fatalf("query audit: %v", err)
	}
	defer rows.Close()

	seen := map[string]bool{}
	for rows.Next() {
		var eventType, result string
		if err := rows.Scan(&eventType, &result); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		seen[eventType] = true
		for _, val := range []string{eventType, result} {
			if strings.Contains(val, "tcp(") || strings.Contains(val, "://") || strings.Contains(val, "@") {
				t.Fatalf("audit column %q looks like a DSN fragment", val)
			}
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit: %v", err)
	}
	for _, want := range []string{"query.schema.databases.listed", "query.schema.objects.listed", "query.schema.object.read"} {
		if !seen[want] {
			t.Fatalf("missing audit event type %q, got %v", want, seen)
		}
	}
}

func TestSchemaAPI_NoDSNInResponseBody(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.ListDatabases(ctx, ownerDBA, targetID, "", 1, 100, false, false)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	assertSchemaResponseNoDSN(t, resp, globalEnv.dsn)
}

func TestSchemaAPI_AutoIncrementColumnFlagged(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	detail, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_parent", "table", false)
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	for _, c := range detail.Columns {
		if c.Name == "id" {
			if !c.AutoIncrement {
				t.Fatal("id column must have autoIncrement=true")
			}
			return
		}
	}
	t.Fatal("id column not found")
}

func TestSchemaAPI_NullableColumnFlagged(t *testing.T) {
	svc, targetID, _ := setupSchemaSandboxTarget(t)
	ctx := context.Background()

	detail, err := svc.GetObjectDetails(ctx, ownerDBA, targetID, "query_e2e_aux", "schema_child", "table", false)
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	for _, c := range detail.Columns {
		if c.Name == "sort_order" {
			if c.Nullable {
				t.Fatal("sort_order column must NOT be nullable (INT NOT NULL DEFAULT 0)")
			}
			return
		}
	}
	t.Fatal("sort_order column not found")
}

// --- helpers ---

func dbNames(items []model.DatabaseSummary) []string {
	names := make([]string, len(items))
	for i, d := range items {
		names[i] = d.Name
	}
	return names
}

func objNames(items []model.ObjectSummary) []string {
	names := make([]string, len(items))
	for i, o := range items {
		names[i] = o.Name
	}
	return names
}

func colNameSet(cols []model.ColumnDetail) map[string]bool {
	s := make(map[string]bool, len(cols))
	for _, c := range cols {
		s[c.Name] = true
	}
	return s
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func assertSchemaResponseNoDSN(t *testing.T, resp model.DatabaseListResponse, dsn string) {
	t.Helper()
	raw := fmt.Sprintf("%+v", resp)
	if strings.Contains(raw, dsn) {
		t.Fatalf("response contains the DSN: %s", raw)
	}
	for _, marker := range []string{"tcp(", "://", "root:test"} {
		if strings.Contains(raw, marker) {
			t.Fatalf("response contains DSN marker %q: %s", marker, raw)
		}
	}
}
