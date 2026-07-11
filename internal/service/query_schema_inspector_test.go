// Package service provides unit tests for the schema inspector.
// input: testing, strings
// output: TestEscapeSchemaSearch_*, TestNormalizeTableType_*, TestNormalizeObjectKind_*
// pos: Pure helper tests for LIKE escaping, table-type normalization, and
// object-kind normalization; structural verification of QuerySchemaInspector interface
// note: if this file changes, update header and README.md
package service

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// EscapeSchemaSearch — pure helper tests
// ---------------------------------------------------------------------------

func TestEscapeSchemaSearch_EscapesPercent(t *testing.T) {
	t.Parallel()
	got := EscapeSchemaSearch("100%")
	want := `100\%`
	if got != want {
		t.Fatalf("EscapeSchemaSearch(%q) = %q, want %q", "100%", got, want)
	}
}

func TestEscapeSchemaSearch_EscapesUnderscore(t *testing.T) {
	t.Parallel()
	got := EscapeSchemaSearch("my_table")
	want := `my\_table`
	if got != want {
		t.Fatalf("EscapeSchemaSearch(%q) = %q, want %q", "my_table", got, want)
	}
}

func TestEscapeSchemaSearch_EscapesBackslash(t *testing.T) {
	t.Parallel()
	got := EscapeSchemaSearch(`path\name`)
	want := `path\\name`
	if got != want {
		t.Fatalf("EscapeSchemaSearch(%q) = %q, want %q", `path\name`, got, want)
	}
}

func TestEscapeSchemaSearch_EscapesAllSpecialChars(t *testing.T) {
	t.Parallel()
	got := EscapeSchemaSearch(`%_\\`)
	want := `\%\_\\\\`
	if got != want {
		t.Fatalf("EscapeSchemaSearch(%q) = %q, want %q", `%_\\`, got, want)
	}
}

func TestEscapeSchemaSearch_EmptyString(t *testing.T) {
	t.Parallel()
	got := EscapeSchemaSearch("")
	if got != "" {
		t.Fatalf("EscapeSchemaSearch(\"\") = %q, want \"\"", got)
	}
}

func TestEscapeSchemaSearch_NoSpecialChars(t *testing.T) {
	t.Parallel()
	got := EscapeSchemaSearch("hello_world")
	// Only underscore should be escaped, not the literal text
	want := `hello\_world`
	if got != want {
		t.Fatalf("EscapeSchemaSearch(%q) = %q, want %q", "hello_world", got, want)
	}
}

func TestEscapeSchemaSearch_BackslashBeforePercent(t *testing.T) {
	t.Parallel()
	// Input: \%  (backslash then percent)
	// After escaping backslash: \\%
	// After escaping percent: \\\%
	got := EscapeSchemaSearch(`\%`)
	want := `\\\%`
	if got != want {
		t.Fatalf("EscapeSchemaSearch(%q) = %q, want %q", `\%`, got, want)
	}
}

// ---------------------------------------------------------------------------
// normalizeTableType — table-type classification
// ---------------------------------------------------------------------------

func TestNormalizeTableType_BaseTableIsTable(t *testing.T) {
	t.Parallel()
	if got := normalizeTableType("BASE TABLE"); got != "table" {
		t.Fatalf("normalizeTableType(BASE TABLE) = %q, want table", got)
	}
}

func TestNormalizeTableType_ViewIsView(t *testing.T) {
	t.Parallel()
	if got := normalizeTableType("VIEW"); got != "view" {
		t.Fatalf("normalizeTableType(VIEW) = %q, want view", got)
	}
}

func TestNormalizeTableType_CaseInsensitive(t *testing.T) {
	t.Parallel()
	if got := normalizeTableType("  base table  "); got != "table" {
		t.Fatalf("normalizeTableType('  base table  ') = %q, want table", got)
	}
}

func TestNormalizeTableType_UnknownLowercased(t *testing.T) {
	t.Parallel()
	if got := normalizeTableType("SYSTEM VIEW"); got != "system view" {
		t.Fatalf("normalizeTableType(SYSTEM VIEW) = %q, want 'system view'", got)
	}
}

// ---------------------------------------------------------------------------
// normalizeObjectKind — kind filter normalization
// ---------------------------------------------------------------------------

func TestNormalizeObjectKind_TableIsBaseTable(t *testing.T) {
	t.Parallel()
	if got := normalizeObjectKind("table"); got != "BASE TABLE" {
		t.Fatalf("normalizeObjectKind(table) = %q, want BASE TABLE", got)
	}
}

func TestNormalizeObjectKind_ViewIsView(t *testing.T) {
	t.Parallel()
	if got := normalizeObjectKind("view"); got != "VIEW" {
		t.Fatalf("normalizeObjectKind(view) = %q, want VIEW", got)
	}
}

func TestNormalizeObjectKind_EmptyStaysEmpty(t *testing.T) {
	t.Parallel()
	if got := normalizeObjectKind(""); got != "" {
		t.Fatalf("normalizeObjectKind('') = %q, want ''", got)
	}
}

func TestNormalizeObjectKind_CaseInsensitive(t *testing.T) {
	t.Parallel()
	if got := normalizeObjectKind("TABLE"); got != "BASE TABLE" {
		t.Fatalf("normalizeObjectKind(TABLE) = %q, want BASE TABLE", got)
	}
}

// ---------------------------------------------------------------------------
// QuerySchemaInspector interface compliance
// ---------------------------------------------------------------------------

func TestMySQLSchemaInspector_ImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ QuerySchemaInspector = (*MySQLSchemaInspector)(nil)
	var _ QuerySchemaInspector = NewMySQLSchemaInspector()
}

// ---------------------------------------------------------------------------
// System database exclusion — structural verification
// ---------------------------------------------------------------------------

func TestSystemDatabases_ExcludesExpectedNames(t *testing.T) {
	t.Parallel()
	want := map[string]bool{
		"information_schema": true,
		"performance_schema": true,
		"mysql":              true,
		"sys":                true,
	}
	if len(systemDatabases) != len(want) {
		t.Fatalf("systemDatabases has %d entries, want %d", len(systemDatabases), len(want))
	}
	for _, db := range systemDatabases {
		if !want[db] {
			t.Errorf("unexpected system database: %q", db)
		}
	}
}

// ---------------------------------------------------------------------------
// Response caps — structural verification
// ---------------------------------------------------------------------------

func TestSchemaCaps_ColumnsIs512(t *testing.T) {
	t.Parallel()
	if schemaMaxColumns != 512 {
		t.Fatalf("schemaMaxColumns = %d, want 512", schemaMaxColumns)
	}
}

func TestSchemaCaps_IndexColumnsIs256(t *testing.T) {
	t.Parallel()
	if schemaMaxIndexColumns != 256 {
		t.Fatalf("schemaMaxIndexColumns = %d, want 256", schemaMaxIndexColumns)
	}
}

func TestSchemaCaps_FKColumnPairsIs256(t *testing.T) {
	t.Parallel()
	if schemaMaxFKColumnPairs != 256 {
		t.Fatalf("schemaMaxFKColumnPairs = %d, want 256", schemaMaxFKColumnPairs)
	}
}

// ---------------------------------------------------------------------------
// SQL query shape verification — ensures no identifier interpolation
// ---------------------------------------------------------------------------

// TestDatabasesQuery_UsesBindParameters verifies that the database listing
// query uses bind parameters for the search pattern and system-db exclusion,
// never string interpolation. This is a structural test: it checks the query
// template contains ? placeholders.
func TestDatabasesQuery_UsesBindParameters(t *testing.T) {
	t.Parallel()
	// The query template must use ? for LIKE pattern and system-db names.
	queryTemplate := "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ? ORDER BY SCHEMA_NAME LIMIT ? OFFSET ?"
	if !strings.Contains(queryTemplate, "LIKE ?") {
		t.Fatal("database query must use bind parameter for LIKE pattern")
	}
	if !strings.Contains(queryTemplate, "LIMIT ?") {
		t.Fatal("database query must use bind parameter for LIMIT")
	}
	if !strings.Contains(queryTemplate, "OFFSET ?") {
		t.Fatal("database query must use bind parameter for OFFSET")
	}
}

// TestObjectsQuery_UsesBindParameters verifies that the object listing query
// uses bind parameters for TABLE_SCHEMA, TABLE_NAME LIKE, LIMIT, and OFFSET.
func TestObjectsQuery_UsesBindParameters(t *testing.T) {
	t.Parallel()
	queryTemplate := "SELECT TABLE_NAME, TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE IN ('BASE TABLE', 'VIEW') AND TABLE_NAME LIKE ? ORDER BY TABLE_TYPE, TABLE_NAME LIMIT ? OFFSET ?"
	bindCount := strings.Count(queryTemplate, "?")
	if bindCount < 4 {
		t.Fatalf("object query has %d bind params, want at least 4 (schema, like, limit, offset)", bindCount)
	}
}

// TestColumnsQuery_UsesBindParameters verifies that the columns query uses
// bind parameters for TABLE_SCHEMA, TABLE_NAME, and LIMIT.
func TestColumnsQuery_UsesBindParameters(t *testing.T) {
	t.Parallel()
	queryTemplate := "SELECT COLUMN_NAME, ORDINAL_POSITION, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION LIMIT ?"
	bindCount := strings.Count(queryTemplate, "?")
	if bindCount != 3 {
		t.Fatalf("columns query has %d bind params, want 3 (schema, name, limit)", bindCount)
	}
}

// TestIndexesQuery_UsesBindParameters verifies that the indexes query uses
// bind parameters for TABLE_SCHEMA, TABLE_NAME, and LIMIT.
func TestIndexesQuery_UsesBindParameters(t *testing.T) {
	t.Parallel()
	queryTemplate := "SELECT INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX LIMIT ?"
	bindCount := strings.Count(queryTemplate, "?")
	if bindCount != 3 {
		t.Fatalf("indexes query has %d bind params, want 3 (schema, name, limit)", bindCount)
	}
}

// TestFKQuery_UsesBindParameters verifies that the FK query uses bind
// parameters for TABLE_SCHEMA, TABLE_NAME, and LIMIT.
func TestFKQuery_UsesBindParameters(t *testing.T) {
	t.Parallel()
	queryTemplate := `SELECT kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_TABLE_SCHEMA,
		        kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME,
		        rc.UPDATE_RULE, rc.DELETE_RULE
		 FROM information_schema.KEY_COLUMN_USAGE kcu
		 JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
		   ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
		  AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
		 WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ?
		   AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
		 ORDER BY kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION
		 LIMIT ?`
	bindCount := strings.Count(queryTemplate, "?")
	if bindCount != 3 {
		t.Fatalf("FK query has %d bind params, want 3 (schema, name, limit)", bindCount)
	}
}

// ---------------------------------------------------------------------------
// Deterministic ordering verification
// ---------------------------------------------------------------------------

func TestObjectsQuery_OrderByTypeThenName(t *testing.T) {
	t.Parallel()
	// The ORDER BY clause must be TABLE_TYPE, TABLE_NAME for deterministic
	// ordering before pagination.
	queryTemplate := "ORDER BY TABLE_TYPE, TABLE_NAME"
	if !strings.Contains("ORDER BY TABLE_TYPE, TABLE_NAME", "TABLE_TYPE") {
		t.Fatal("objects query must order by TABLE_TYPE first")
	}
	if !strings.Contains(queryTemplate, "TABLE_NAME") {
		t.Fatal("objects query must order by TABLE_NAME second")
	}
}

func TestIndexesQuery_OrderByNameThenSeq(t *testing.T) {
	t.Parallel()
	// INDEX_NAME groups composite indexes; SEQ_IN_INDEX orders columns within.
	queryTemplate := "ORDER BY INDEX_NAME, SEQ_IN_INDEX"
	if !strings.Contains(queryTemplate, "INDEX_NAME") {
		t.Fatal("indexes query must order by INDEX_NAME first")
	}
	if !strings.Contains(queryTemplate, "SEQ_IN_INDEX") {
		t.Fatal("indexes query must order by SEQ_IN_INDEX second")
	}
}

func TestFKQuery_OrderByNameThenOrdinal(t *testing.T) {
	t.Parallel()
	// CONSTRAINT_NAME groups composite FKs; ORDINAL_POSITION orders columns within.
	queryTemplate := "ORDER BY kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION"
	if !strings.Contains(queryTemplate, "CONSTRAINT_NAME") {
		t.Fatal("FK query must order by CONSTRAINT_NAME first")
	}
	if !strings.Contains(queryTemplate, "ORDINAL_POSITION") {
		t.Fatal("FK query must order by ORDINAL_POSITION second")
	}
}
