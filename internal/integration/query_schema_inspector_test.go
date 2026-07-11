//go:build integration

// Package integration proves the MySQLSchemaInspector works against the real
// dedicated query MySQL fixture: database listing with system exclusion,
// object listing with kind/search, and object details with columns, composite
// indexes, and foreign keys.
package integration

import (
	"context"
	"testing"

	"github.com/fan/controlhub/internal/service"
)

// setupSchemaFixture creates the query_e2e + query_e2e_aux databases with
// parent/child tables, view, composite index, secondary index, and FK in the
// disposable test MySQL. Returns a DSN the inspector can connect with.
func setupSchemaFixture(t *testing.T) string {
	t.Helper()
	db := setupTestDB(t)

	mustExec(t, db, "CREATE DATABASE IF NOT EXISTS query_e2e")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e.query_e2e_items")
	mustExec(t, db, "CREATE TABLE query_e2e.query_e2e_items (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, name VARCHAR(64) NOT NULL, category VARCHAR(32) NOT NULL, created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP)")
	mustExec(t, db, "INSERT INTO query_e2e.query_e2e_items (id, name, category) VALUES (1,'alpha','sample'),(2,'beta','sample') ON DUPLICATE KEY UPDATE name=VALUES(name),category=VALUES(category)")

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

	return globalEnv.dsn
}

func TestInspector_ListDatabases_ExcludesSystem(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	items, pageInfo, err := insp.ListDatabases(ctx, dsn, "", false, 1, 100)
	if err != nil {
		t.Fatalf("ListDatabases: %v", err)
	}
	names := inspDBNames(items)
	for _, sys := range []string{"information_schema", "performance_schema", "mysql", "sys"} {
		if containsStr(names, sys) {
			t.Fatalf("system database %q must be excluded, got %v", sys, names)
		}
	}
	if !containsStr(names, "query_e2e") {
		t.Fatalf("query_e2e must appear, got %v", names)
	}
	if !containsStr(names, "query_e2e_aux") {
		t.Fatalf("query_e2e_aux must appear, got %v", names)
	}
	if pageInfo.TotalItems < 2 {
		t.Fatalf("totalItems = %d, want >= 2", pageInfo.TotalItems)
	}
}

func TestInspector_ListDatabases_IncludeSystem(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	items, _, err := insp.ListDatabases(ctx, dsn, "", true, 1, 100)
	if err != nil {
		t.Fatalf("ListDatabases includeSystem: %v", err)
	}
	names := inspDBNames(items)
	if !containsStr(names, "information_schema") {
		t.Fatalf("system databases must appear when includeSystem=true, got %v", names)
	}
}

func TestInspector_ListDatabases_Search(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	items, _, err := insp.ListDatabases(ctx, dsn, "aux", false, 1, 100)
	if err != nil {
		t.Fatalf("ListDatabases search: %v", err)
	}
	names := inspDBNames(items)
	if !containsStr(names, "query_e2e_aux") {
		t.Fatalf("search 'aux' must match query_e2e_aux, got %v", names)
	}
	if containsStr(names, "query_e2e") {
		t.Fatalf("search 'aux' must not match plain query_e2e, got %v", names)
	}
}

func TestInspector_ListDatabases_Pagination(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	items, pageInfo, err := insp.ListDatabases(ctx, dsn, "", false, 1, 1)
	if err != nil {
		t.Fatalf("ListDatabases pageSize=1: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("page 1 items = %d, want 1", len(items))
	}
	if !pageInfo.HasNextPage {
		t.Fatal("pageInfo.hasNext = false, want true")
	}
}

func TestInspector_ListObjects_AllKinds(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	items, _, err := insp.ListObjects(ctx, dsn, "query_e2e_aux", "", "", 1, 100)
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	names := inspObjNames(items)
	if !containsStr(names, "schema_parent") || !containsStr(names, "schema_child") {
		t.Fatalf("must include tables, got %v", names)
	}
	if !containsStr(names, "schema_parent_summary") {
		t.Fatalf("must include view, got %v", names)
	}
}

func TestInspector_ListObjects_KindFilter(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	tables, _, err := insp.ListObjects(ctx, dsn, "query_e2e_aux", "table", "", 1, 100)
	if err != nil {
		t.Fatalf("ListObjects kind=table: %v", err)
	}
	for _, o := range tables {
		if o.Kind != "table" {
			t.Fatalf("kind filter leaked %q with kind=%q", o.Name, o.Kind)
		}
	}
	views, _, err := insp.ListObjects(ctx, dsn, "query_e2e_aux", "view", "", 1, 100)
	if err != nil {
		t.Fatalf("ListObjects kind=view: %v", err)
	}
	if len(views) != 1 || views[0].Name != "schema_parent_summary" {
		t.Fatalf("view filter result = %v, want [schema_parent_summary]", inspObjNames(views))
	}
}

func TestInspector_ListObjects_Search(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	items, _, err := insp.ListObjects(ctx, dsn, "query_e2e_aux", "", "child", 1, 100)
	if err != nil {
		t.Fatalf("ListObjects search: %v", err)
	}
	names := inspObjNames(items)
	if !containsStr(names, "schema_child") {
		t.Fatalf("search 'child' must match schema_child, got %v", names)
	}
	if containsStr(names, "schema_parent") {
		t.Fatalf("search 'child' must not match schema_parent, got %v", names)
	}
}

func TestInspector_GetObjectDetails_TableColumns(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	detail, err := insp.GetObjectDetails(ctx, dsn, "query_e2e_aux", "schema_parent", "table")
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	if detail.Name != "schema_parent" || detail.Kind != "table" {
		t.Fatalf("header = %+v", detail)
	}
	colNames := inspColNames(detail.Columns)
	for _, want := range []string{"id", "parent_code", "label", "created_at"} {
		if !containsStr(colNames, want) {
			t.Fatalf("missing column %q, got %v", want, colNames)
		}
	}
}

func TestInspector_GetObjectDetails_ViewColumns(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	detail, err := insp.GetObjectDetails(ctx, dsn, "query_e2e_aux", "schema_parent_summary", "view")
	if err != nil {
		t.Fatalf("GetObjectDetails view: %v", err)
	}
	if detail.Kind != "view" {
		t.Fatalf("kind = %q, want view", detail.Kind)
	}
	colNames := inspColNames(detail.Columns)
	for _, want := range []string{"id", "parent_code", "label"} {
		if !containsStr(colNames, want) {
			t.Fatalf("view missing column %q, got %v", want, colNames)
		}
	}
}

func TestInspector_GetObjectDetails_CompositeIndexOrdering(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	detail, err := insp.GetObjectDetails(ctx, dsn, "query_e2e_aux", "schema_child", "table")
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	var compositeIdx *service.IndexSummary
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
	if compositeIdx.Columns[0].Name != "parent_id" || compositeIdx.Columns[1].Name != "sort_order" {
		t.Fatalf("composite index columns = [%s,%s], want [parent_id,sort_order]",
			compositeIdx.Columns[0].Name, compositeIdx.Columns[1].Name)
	}
	if compositeIdx.Columns[0].SeqInIndex != 1 || compositeIdx.Columns[1].SeqInIndex != 2 {
		t.Fatalf("composite index seq = [%d,%d], want [1,2]",
			compositeIdx.Columns[0].SeqInIndex, compositeIdx.Columns[1].SeqInIndex)
	}
}

func TestInspector_GetObjectDetails_FKOrdering(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	detail, err := insp.GetObjectDetails(ctx, dsn, "query_e2e_aux", "schema_child", "table")
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
	if len(fk.Columns) != 1 {
		t.Fatalf("FK column count = %d, want 1", len(fk.Columns))
	}
	if fk.Columns[0].Column != "parent_id" {
		t.Fatalf("FK column = %q, want parent_id", fk.Columns[0].Column)
	}
	if fk.Columns[0].ReferencedTable != "schema_parent" {
		t.Fatalf("FK referenced table = %q, want schema_parent", fk.Columns[0].ReferencedTable)
	}
	if fk.Columns[0].ReferencedColumn != "id" {
		t.Fatalf("FK referenced column = %q, want id", fk.Columns[0].ReferencedColumn)
	}
	if fk.UpdateRule != "CASCADE" || fk.DeleteRule != "RESTRICT" {
		t.Fatalf("FK rules = update:%q delete:%q, want CASCADE/RESTRICT", fk.UpdateRule, fk.DeleteRule)
	}
}

func TestInspector_GetObjectDetails_PrimaryKeyColumns(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	detail, err := insp.GetObjectDetails(ctx, dsn, "query_e2e_aux", "schema_parent", "table")
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	var pkCols []string
	for _, c := range detail.Columns {
		if c.Key == "PRI" {
			pkCols = append(pkCols, c.Name)
		}
	}
	if len(pkCols) != 1 || pkCols[0] != "id" {
		t.Fatalf("PK columns = %v, want [id]", pkCols)
	}
}

func TestInspector_GetObjectDetails_UniqueIndexFlagged(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	detail, err := insp.GetObjectDetails(ctx, dsn, "query_e2e_aux", "schema_parent", "table")
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	var uqIdx *service.IndexSummary
	for i, idx := range detail.Indexes {
		if idx.Name == "uq_schema_parent_code" {
			uqIdx = &detail.Indexes[i]
			break
		}
	}
	if uqIdx == nil {
		t.Fatalf("unique index uq_schema_parent_code not found in %+v", detail.Indexes)
	}
	if uqIdx.NonUnique {
		t.Fatal("uq_schema_parent_code must be unique (NonUnique=false)")
	}
}

func TestInspector_GetObjectDetails_AutoIncrementExtra(t *testing.T) {
	dsn := setupSchemaFixture(t)
	insp := service.NewMySQLSchemaInspector()
	ctx := context.Background()

	detail, err := insp.GetObjectDetails(ctx, dsn, "query_e2e_aux", "schema_parent", "table")
	if err != nil {
		t.Fatalf("GetObjectDetails: %v", err)
	}
	for _, c := range detail.Columns {
		if c.Name == "id" {
			if c.Extra != "auto_increment" {
				t.Fatalf("id extra = %q, want auto_increment", c.Extra)
			}
			return
		}
	}
	t.Fatal("id column not found")
}

func TestInspector_EscapeSearch_LiteralPercentAndUnderscore(t *testing.T) {
	escaped := service.EscapeSchemaSearch("%test_value%")
	if escaped != `\%test\_value\%` {
		t.Fatalf("EscapeSchemaSearch = %q, want \\%%s", escaped)
	}
}

// --- helpers ---

func inspDBNames(items []service.DatabaseSummary) []string {
	names := make([]string, len(items))
	for i, d := range items {
		names[i] = d.Name
	}
	return names
}

func inspObjNames(items []service.ObjectSummary) []string {
	names := make([]string, len(items))
	for i, o := range items {
		names[i] = o.Name
	}
	return names
}

func inspColNames(cols []service.ColumnSummary) []string {
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names
}

func containsStr(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
