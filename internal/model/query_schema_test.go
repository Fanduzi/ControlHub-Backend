// Package model provides tests for query schema domain types.
// input: reflect, testing
// output: TestDatabaseListResponse_*, TestObjectListResponse_*, TestObjectDetailResponse_*, TestObjectKind_*, TestTruncationFlags_*, TestNoCredentialFields_*
// pos: Unit tests for schema metadata response shape contracts and credential leak prevention
// note: if this file changes, update header and README.md
package model

import (
	"reflect"
	"strings"
	"testing"
)

// TestDatabaseListResponse_RequiresTargetResourceID verifies that the database
// list response always carries the owning target resource ID so the frontend
// can correlate responses when switching targets.
func TestDatabaseListResponse_RequiresTargetResourceID(t *testing.T) {
	t.Parallel()
	r := DatabaseListResponse{}
	if r.TargetResourceID != 0 {
		t.Error("zero-value TargetResourceID should be 0")
	}
	// The struct must have a TargetResourceID field of type int64.
	f, ok := reflect.TypeOf(DatabaseListResponse{}).FieldByName("TargetResourceID")
	if !ok {
		t.Fatal("DatabaseListResponse must have TargetResourceID field")
	}
	if f.Type.Kind() != reflect.Int64 {
		t.Errorf("TargetResourceID kind = %v, want int64", f.Type.Kind())
	}
}

// TestDatabaseListResponse_HasItemsAndPageInfo verifies the standard list
// envelope shape: Items slice and PageInfo pointer.
func TestDatabaseListResponse_HasItemsAndPageInfo(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(DatabaseListResponse{})
	for _, name := range []string{"Items", "PageInfo"} {
		f, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("DatabaseListResponse must have %s field", name)
		}
		_ = f
	}
}

// TestObjectListResponse_RequiresTargetResourceIDAndDatabase verifies the
// object list response carries both the target ID and the database name so
// the frontend can display breadcrumb context.
func TestObjectListResponse_RequiresTargetResourceIDAndDatabase(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(ObjectListResponse{})
	for _, name := range []string{"TargetResourceID", "Database", "Items", "PageInfo"} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatalf("ObjectListResponse must have %s field", name)
		}
	}
}

// TestObjectDetailResponse_RequiresAllTopLevelFields verifies the object
// detail response has all required top-level metadata fields.
func TestObjectDetailResponse_RequiresAllTopLevelFields(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(ObjectDetailResponse{})
	for _, name := range []string{
		"TargetResourceID", "Database", "Name", "Kind",
		"Columns", "Indexes", "ForeignKeys", "Truncated",
	} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatalf("ObjectDetailResponse must have %s field", name)
		}
	}
}

// TestObjectKind_OnlyAcceptsTableAndView verifies the ObjectKind type rejects
// anything other than "table" or "view". This is a safety constraint — unknown
// kinds must fail closed so the frontend never renders an unsupported object
// type as if it were valid.
func TestObjectKind_OnlyAcceptsTableAndView(t *testing.T) {
	t.Parallel()
	for _, kind := range []ObjectKind{ObjectKindTable, ObjectKindView} {
		if err := kind.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", kind, err)
		}
	}
	for _, kind := range []ObjectKind{
		"", "materialized_view", "procedure", "function", "TABLE", "VIEW", "table ",
	} {
		if err := kind.Validate(); err == nil {
			t.Errorf("Validate(%q) = nil, want error (fail closed)", kind)
		}
	}
}

// TestTruncationFlags_AreExplicitBooleans verifies that TruncationFlags has
// three explicit boolean fields. This prevents the frontend from assuming
// absence means not-truncated — each flag must be explicitly set.
func TestTruncationFlags_AreExplicitBooleans(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(TruncationFlags{})
	for _, name := range []string{"Columns", "Indexes", "ForeignKeys"} {
		f, ok := typ.FieldByName(name)
		if !ok {
			t.Fatalf("TruncationFlags must have %s field", name)
		}
		if f.Type.Kind() != reflect.Bool {
			t.Errorf("TruncationFlags.%s kind = %v, want bool", name, f.Type.Kind())
		}
	}
}

// TestNoCredentialFieldsInSchemaResponses is a security gate: it uses
// reflection to walk every exported field of all schema response types and
// rejects any field whose lowercase name contains credential, dsn, password,
// or username. Schema metadata must never leak connection secrets.
func TestNoCredentialFieldsInSchemaResponses(t *testing.T) {
	t.Parallel()
	forbidden := []string{"credential", "dsn", "password", "username", "secret"}
	types := []interface{}{
		DatabaseListResponse{},
		ObjectListResponse{},
		ObjectDetailResponse{},
		DatabaseSummary{},
		ObjectSummary{},
		ColumnDetail{},
		IndexDetail{},
		ForeignKeyDetail{},
		TruncationFlags{},
	}
	for _, v := range types {
		typ := reflect.TypeOf(v)
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			lower := strings.ToLower(field.Name)
			for _, bad := range forbidden {
				if strings.Contains(lower, bad) {
					t.Errorf("%s.%s contains forbidden term %q — schema responses must never carry credentials",
						typ.Name(), field.Name, bad)
				}
			}
		}
	}
}

// TestColumnDetail_HasOrdinalPosition verifies the column detail carries its
// ordinal position so the frontend can render columns in database-defined order
// rather than alphabetically or by arrival.
func TestColumnDetail_HasOrdinalPosition(t *testing.T) {
	t.Parallel()
	f, ok := reflect.TypeOf(ColumnDetail{}).FieldByName("OrdinalPosition")
	if !ok {
		t.Fatal("ColumnDetail must have OrdinalPosition field")
	}
	if f.Type.Kind() != reflect.Int {
		t.Errorf("OrdinalPosition kind = %v, want int", f.Type.Kind())
	}
}

// TestForeignKeyDetail_HasReferencedColumns verifies foreign key detail
// carries both source and referenced column lists so the frontend can render
// the full relationship.
func TestForeignKeyDetail_HasReferencedColumns(t *testing.T) {
	t.Parallel()
	typ := reflect.TypeOf(ForeignKeyDetail{})
	for _, name := range []string{"Columns", "ReferencedDatabase", "ReferencedObject", "ReferencedColumns"} {
		if _, ok := typ.FieldByName(name); !ok {
			t.Fatalf("ForeignKeyDetail must have %s field", name)
		}
	}
}
