// Package model provides tests for query schema domain types.
// input: reflect, testing
// output: TestDatabaseListResponse_*, TestObjectListResponse_*, TestObjectDetailResponse_*, TestTableDefinitionResponse_*, TestObjectKind_*, TestTruncationFlags_*, TestNoCredentialFields_*
// pos: Unit tests for schema metadata response shape contracts and credential leak prevention
// note: if this file changes, update header and README.md
package model

import (
	"encoding/json"
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

// TestTableDefinitionResponse_JSONShape verifies the governed table-definition
// response exposes exactly the seven fields required by the wire contract.
func TestTableDefinitionResponse_JSONShape(t *testing.T) {
	t.Parallel()
	// Given
	resp := TableDefinitionResponse{
		TargetResourceID: 22,
		Database:         "sandbox",
		Name:             "plain_table",
		Kind:             ObjectKindTable,
		Dialect:          "mysql",
		Definition:       "CREATE TABLE `plain_table` (...)\n",
		Truncated:        true,
	}

	// When
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Then
	for _, name := range []string{
		"targetResourceId", "database", "name", "kind", "dialect", "definition", "truncated",
	} {
		if _, ok := body[name]; !ok {
			t.Errorf("JSON response is missing field %q: %s", name, raw)
		}
	}
}

// TestTableDefinitionResponse_NoSensitiveFields verifies JSON tags do not
// expose connection, identity, or raw/error details in the response.
func TestTableDefinitionResponse_NoSensitiveFields(t *testing.T) {
	t.Parallel()
	// Given
	typ := reflect.TypeOf(TableDefinitionResponse{})
	forbidden := []string{
		"dsn", "password", "secret", "host", "port", "credential", "actor", "user", "raw", "error",
	}

	// When / Then
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		jsonName := strings.ToLower(strings.Split(field.Tag.Get("json"), ",")[0])
		for _, bad := range forbidden {
			if strings.Contains(jsonName, bad) {
				t.Errorf("%s.%s has sensitive JSON field name %q", typ.Name(), field.Name, jsonName)
			}
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

// TestObjectDetailResponse_EmptyCollectionsSerializeAsEmptyArrays proves the
// OpenAPI required-array invariant: empty columns/indexes/foreignKeys and nested
// index/FK column lists must serialize as JSON [] never null. Frontend calls
// .length; null crashes Object Explorer for tables with no secondary indexes/FKs.
func TestObjectDetailResponse_EmptyCollectionsSerializeAsEmptyArrays(t *testing.T) {
	t.Parallel()
	resp := ObjectDetailResponse{
		TargetResourceID: 22,
		Database:         "sandbox",
		Name:             "plain_table",
		Kind:             ObjectKindTable,
		// Intentionally leave Columns/Indexes/ForeignKeys as nil zero-values —
		// successful responses must still marshal them as [].
		Indexes: []IndexDetail{
			{Name: "idx_empty_cols", Unique: false, Primary: false},
			// Columns left nil
		},
		ForeignKeys: []ForeignKeyDetail{
			{Name: "fk_empty_cols", ReferencedDatabase: "sandbox", ReferencedObject: "parent"},
			// Columns + ReferencedColumns left nil
		},
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)
	for _, forbidden := range []string{
		`"columns":null`,
		`"indexes":null`,
		`"foreignKeys":null`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("body contains %s (must be []): %s", forbidden, body)
		}
	}
	// Nested arrays on the empty-column index/FK entries
	if !strings.Contains(body, `"columns":[]`) {
		t.Fatalf("expected nested columns:[] in body: %s", body)
	}
	if !strings.Contains(body, `"referencedColumns":[]`) {
		t.Fatalf("expected referencedColumns:[] in body: %s", body)
	}
	// Top-level indexes/foreignKeys must be present as arrays (not omitted/null)
	if !strings.Contains(body, `"indexes":[`) {
		t.Fatalf("expected indexes array: %s", body)
	}
	if !strings.Contains(body, `"foreignKeys":[`) {
		t.Fatalf("expected foreignKeys array: %s", body)
	}
}

// TestRelationshipMapRoleValidation verifies that RelationshipMapRole.Validate()
// accepts only "root" and "related". This is a fail-closed enum: unknown roles
// must be rejected so the frontend never renders an unclassified node.
func TestRelationshipMapRoleValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		role RelationshipMapRole
		want bool // true = expect nil error
	}{
		{RelationshipMapRoleRoot, true},
		{RelationshipMapRoleRelated, true},
		{"", false},
		{"parent", false},
		{"child", false},
		{"ROOT", false},
		{"RELATED", false},
		{"root ", false},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			err := tc.role.Validate()
			if tc.want && err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tc.role, err)
			}
			if !tc.want && err == nil {
				t.Errorf("Validate(%q) = nil, want error (fail closed)", tc.role)
			}
		})
	}
}

// TestRelationshipMapDirectionValidation verifies that
// RelationshipMapDirection.Validate() accepts only "inbound" and "outbound".
// This is a fail-closed enum: unknown directions must be rejected so the
// frontend never renders an edge with ambiguous directionality.
func TestRelationshipMapDirectionValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		dir  RelationshipMapDirection
		want bool
	}{
		{RelationshipMapDirectionInbound, true},
		{RelationshipMapDirectionOutbound, true},
		{"", false},
		{"both", false},
		{"bidirectional", false},
		{"INBOUND", false},
		{"OUTBOUND", false},
		{"inbound ", false},
	} {
		t.Run(string(tc.dir), func(t *testing.T) {
			err := tc.dir.Validate()
			if tc.want && err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tc.dir, err)
			}
			if !tc.want && err == nil {
				t.Errorf("Validate(%q) = nil, want error (fail closed)", tc.dir)
			}
		})
	}
}

// TestRelationshipMapResponseValidate verifies that Validate() enforces all
// structural invariants: non-empty nodes, exactly one root, unique IDs, valid
// edge endpoints, correct direction targeting, non-empty columns, and matching
// columns/referencedColumns lengths.
func TestRelationshipMapResponseValidate(t *testing.T) {
	t.Parallel()

	// validNode is a reusable related node for building test cases.
	validNode := RelationshipMapNode{ID: "n1", Database: "db", Name: "other", Kind: ObjectKindTable, Role: RelationshipMapRoleRelated}
	// validRoot is the standard root node.
	validRoot := RelationshipMapNode{ID: "n0", Database: "db", Name: "t1", Kind: ObjectKindTable, Role: RelationshipMapRoleRoot}
	// validEdge is an inbound edge from related to root.
	validEdge := RelationshipMapEdge{ID: "e0", Direction: RelationshipMapDirectionInbound, SourceID: "n1", TargetID: "n0", Columns: []string{"fk_col"}, ReferencedColumns: []string{"id"}}

	// buildValid returns a valid response; callers can mutate for negative cases.
	buildValid := func() RelationshipMapResponse {
		return RelationshipMapResponse{
			TargetResourceID: 22,
			Root:             validRoot,
			Nodes:            []RelationshipMapNode{validRoot, validNode},
			Edges:            []RelationshipMapEdge{validEdge},
		}
	}

	t.Run("valid response passes", func(t *testing.T) {
		if err := buildValid().Validate(); err != nil {
			t.Errorf("Validate() = %v, want nil", err)
		}
	})

	t.Run("empty nodes", func(t *testing.T) {
		r := buildValid()
		r.Nodes = nil
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for empty nodes")
		}
	})

	t.Run("no root node", func(t *testing.T) {
		r := buildValid()
		r.Nodes = []RelationshipMapNode{
			{ID: "n0", Database: "db", Name: "t1", Kind: ObjectKindTable, Role: RelationshipMapRoleRelated},
			validNode,
		}
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for no root node")
		}
	})

	t.Run("multiple root nodes", func(t *testing.T) {
		r := buildValid()
		r.Nodes = []RelationshipMapNode{
			validRoot,
			{ID: "n1", Database: "db", Name: "other", Kind: ObjectKindTable, Role: RelationshipMapRoleRoot},
		}
		r.Edges = nil
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for multiple root nodes")
		}
	})

	t.Run("duplicate node IDs", func(t *testing.T) {
		r := buildValid()
		r.Nodes = []RelationshipMapNode{
			validRoot,
			{ID: "n0", Database: "db", Name: "dup", Kind: ObjectKindTable, Role: RelationshipMapRoleRelated},
		}
		r.Edges = nil
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for duplicate node IDs")
		}
	})

	t.Run("duplicate edge IDs", func(t *testing.T) {
		r := buildValid()
		r.Edges = []RelationshipMapEdge{validEdge, validEdge}
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for duplicate edge IDs")
		}
	})

	t.Run("edge referencing missing source node", func(t *testing.T) {
		r := buildValid()
		r.Edges = []RelationshipMapEdge{
			{ID: "e0", Direction: RelationshipMapDirectionInbound, SourceID: "missing", TargetID: "n0", Columns: []string{"c"}, ReferencedColumns: []string{"id"}},
		}
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for missing source node")
		}
	})

	t.Run("edge referencing missing target node", func(t *testing.T) {
		r := buildValid()
		r.Edges = []RelationshipMapEdge{
			{ID: "e0", Direction: RelationshipMapDirectionInbound, SourceID: "n1", TargetID: "missing", Columns: []string{"c"}, ReferencedColumns: []string{"id"}},
		}
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for missing target node")
		}
	})

	t.Run("inbound edge with wrong target", func(t *testing.T) {
		r := buildValid()
		// Inbound edge should target root, but targets n1 instead.
		r.Edges = []RelationshipMapEdge{
			{ID: "e0", Direction: RelationshipMapDirectionInbound, SourceID: "n0", TargetID: "n1", Columns: []string{"c"}, ReferencedColumns: []string{"id"}},
		}
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for inbound edge not targeting root")
		}
	})

	t.Run("outbound edge with wrong source", func(t *testing.T) {
		r := buildValid()
		// Outbound edge should source from root, but sources from n1 instead.
		r.Edges = []RelationshipMapEdge{
			{ID: "e0", Direction: RelationshipMapDirectionOutbound, SourceID: "n1", TargetID: "n0", Columns: []string{"c"}, ReferencedColumns: []string{"id"}},
		}
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for outbound edge not sourcing from root")
		}
	})

	t.Run("empty columns", func(t *testing.T) {
		r := buildValid()
		r.Edges = []RelationshipMapEdge{
			{ID: "e0", Direction: RelationshipMapDirectionInbound, SourceID: "n1", TargetID: "n0", Columns: nil, ReferencedColumns: []string{"id"}},
		}
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for empty columns")
		}
	})

	t.Run("mismatched columns/referencedColumns length", func(t *testing.T) {
		r := buildValid()
		r.Edges = []RelationshipMapEdge{
			{ID: "e0", Direction: RelationshipMapDirectionInbound, SourceID: "n1", TargetID: "n0", Columns: []string{"a", "b"}, ReferencedColumns: []string{"id"}},
		}
		if err := r.Validate(); err == nil {
			t.Error("Validate() = nil, want error for mismatched columns/referencedColumns length")
		}
	})
}

// TestRelationshipMapResponseMarshalJSON verifies that MarshalJSON produces
// non-nil JSON arrays for nodes, edges, and edge column lists. The frontend
// calls .length on these; null crashes the relationship map renderer.
func TestRelationshipMapResponseMarshalJSON(t *testing.T) {
	t.Parallel()
	resp := RelationshipMapResponse{
		TargetResourceID: 22,
		Root: RelationshipMapNode{ID: "n0", Database: "db", Name: "t1", Kind: ObjectKindTable, Role: RelationshipMapRoleRoot},
		// Intentionally leave Nodes/Edges as nil to test MarshalJSON defaults.
	}
	raw, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)

	for _, forbidden := range []string{
		`"nodes":null`,
		`"edges":null`,
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("body contains %s (must be []): %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"nodes":[]`) {
		t.Fatalf("expected nodes:[], got: %s", body)
	}
	if !strings.Contains(body, `"edges":[]`) {
		t.Fatalf("expected edges:[], got: %s", body)
	}

	// Now test with edges that have nil column arrays.
	resp.Edges = []RelationshipMapEdge{
		{ID: "e0", Direction: RelationshipMapDirectionInbound, SourceID: "n0", TargetID: "n0", Columns: nil, ReferencedColumns: nil},
	}
	raw2, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal with edges: %v", err)
	}
	body2 := string(raw2)
	if strings.Contains(body2, `"columns":null`) {
		t.Fatalf("edge columns must be [], got: %s", body2)
	}
	if strings.Contains(body2, `"referencedColumns":null`) {
		t.Fatalf("edge referencedColumns must be [], got: %s", body2)
	}
	if !strings.Contains(body2, `"columns":[]`) {
		t.Fatalf("expected columns:[], got: %s", body2)
	}
	if !strings.Contains(body2, `"referencedColumns":[]`) {
		t.Fatalf("expected referencedColumns:[], got: %s", body2)
	}
}

// TestRelationshipMapResponseEnsureNonNilCollections verifies that
// EnsureNonNilCollections makes all collection fields non-nil empty slices.
// This is the service-boundary normalization so cached/shared responses never
// carry nil arrays.
func TestRelationshipMapResponseEnsureNonNilCollections(t *testing.T) {
	t.Parallel()
	resp := RelationshipMapResponse{
		TargetResourceID: 22,
		Root:             RelationshipMapNode{ID: "n0", Database: "db", Name: "t1", Kind: ObjectKindTable, Role: RelationshipMapRoleRoot},
		Edges: []RelationshipMapEdge{
			{ID: "e0", Direction: RelationshipMapDirectionInbound, SourceID: "n0", TargetID: "n0"},
			// Columns and ReferencedColumns left nil
		},
		// Nodes left nil
	}

	resp.EnsureNonNilCollections()

	if resp.Nodes == nil {
		t.Error("Nodes is nil after EnsureNonNilCollections")
	}
	if len(resp.Nodes) != 0 {
		t.Errorf("Nodes length = %d, want 0", len(resp.Nodes))
	}
	if resp.Edges == nil {
		t.Error("Edges is nil after EnsureNonNilCollections")
	}
	if resp.Edges[0].Columns == nil {
		t.Error("edge Columns is nil after EnsureNonNilCollections")
	}
	if resp.Edges[0].ReferencedColumns == nil {
		t.Error("edge ReferencedColumns is nil after EnsureNonNilCollections")
	}
}
