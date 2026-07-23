// Package service provides tests for SQL AST projection provenance resolution.
package service

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

type projectionInspector struct {
	detail *ObjectDetail
	err    error
	calls  []projectionInspectCall
}

type projectionInspectCall struct {
	dsn      string
	database string
	object   string
	kind     string
}

func (f *projectionInspector) ListDatabases(context.Context, string, string, bool, int, int) ([]DatabaseSummary, model.PageInfo, error) {
	return nil, model.PageInfo{}, nil
}

func (f *projectionInspector) ListObjects(context.Context, string, string, string, string, int, int) ([]ObjectSummary, model.PageInfo, error) {
	return nil, model.PageInfo{}, nil
}

func (f *projectionInspector) GetObjectDetails(_ context.Context, dsn, database, object, kind string) (*ObjectDetail, error) {
	f.calls = append(f.calls, projectionInspectCall{dsn: dsn, database: database, object: object, kind: kind})
	if f.err != nil {
		return nil, f.err
	}
	return f.detail, nil
}

func (f *projectionInspector) GetTableDefinition(context.Context, string, string, string) (*TableDefinition, error) {
	return nil, nil
}

func (f *projectionInspector) GetRelationshipMap(context.Context, string, string, string) (*RelationshipMapResult, error) {
	return nil, nil
}

func TestDisclosureProjectionResolveExecute(t *testing.T) {
	t.Parallel()

	columns := []ColumnSummary{{Name: "id"}, {Name: "email"}, {Name: "name"}}
	tests := []struct {
		name      string
		statement string
		want      []ColumnProvenance
		wantErr   bool
	}{
		{
			name:      "unqualified column",
			statement: "SELECT email FROM customers",
			want:      []ColumnProvenance{{OutputName: "email", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "email"}},
		},
		{
			name:      "aliased table column",
			statement: "SELECT c.email FROM customers AS c",
			want:      []ColumnProvenance{{OutputName: "email", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "email"}},
		},
		{
			name:      "multiple columns",
			statement: "SELECT email, name FROM customers",
			want: []ColumnProvenance{
				{OutputName: "email", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "email"},
				{OutputName: "name", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "name"},
			},
		},
		{
			name:      "star expansion",
			statement: "SELECT * FROM customers",
			want: []ColumnProvenance{
				{OutputName: "id", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "id"},
				{OutputName: "email", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "email"},
				{OutputName: "name", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "name"},
			},
		},
		{
			name:      "qualified star expansion",
			statement: "SELECT t.* FROM customers AS t",
			want: []ColumnProvenance{
				{OutputName: "id", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "id"},
				{OutputName: "email", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "email"},
				{OutputName: "name", SourceDatabase: "app", SourceObject: "customers", SourceColumn: "name"},
			},
		},
		{
			name:      "schema qualified column",
			statement: "SELECT db.customers.email FROM db.customers",
			want:      []ColumnProvenance{{OutputName: "email", SourceDatabase: "db", SourceObject: "customers", SourceColumn: "email"}},
		},
		{name: "constant returns empty plan (no table columns)", statement: "SELECT 1", want: nil},
		{name: "aggregate", statement: "SELECT COUNT(*) FROM customers", wantErr: true},
		{name: "expression", statement: "SELECT email || name FROM customers", wantErr: true},
		{name: "join", statement: "SELECT email FROM t1 JOIN t2 ON t1.id = t2.id", wantErr: true},
		{name: "derived table", statement: "SELECT email FROM (SELECT email FROM customers) AS c", wantErr: true},
		{name: "implicit join", statement: "SELECT email FROM t1, t2", wantErr: true},
		{name: "unknown qualifier", statement: "SELECT unknown_alias.email FROM customers", wantErr: true},
		{name: "update returns empty plan (guard rejects DML)", statement: "UPDATE customers SET name = 'x'", want: nil},
		{name: "show returns empty plan (metadata, no column values)", statement: "SHOW TABLES", want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inspector := &projectionInspector{detail: &ObjectDetail{Columns: columns}}
			// Given: an already-guarded statement and canonical table metadata.
			// When: its selected columns are resolved from the SQL AST.
			got, err := resolveExecuteProjection(context.Background(), inspector, executeProjectionInput{
				dsn:      "dsn",
				database: "app",
				guarded:  GuardedQuery{OriginalStatement: tt.statement},
			})
			// Then: supported direct projections retain their metadata provenance;
			// every ambiguous or computed projection fails closed.
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveExecuteProjection(%q) error = nil, want rejection", tt.statement)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveExecuteProjection(%q): %v", tt.statement, err)
			}
			if !reflect.DeepEqual(got.Columns, tt.want) {
				t.Fatalf("Columns = %#v, want %#v", got.Columns, tt.want)
			}
		})
	}
}

func TestDisclosureProjectionResolveRelatedRecords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		columns []string
		detail  *ObjectDetail
		err     error
		want    []ColumnProvenance
		wantErr bool
	}{
		{
			name:    "verified referenced columns",
			columns: []string{"id", "email"},
			detail:  &ObjectDetail{Columns: []ColumnSummary{{Name: "id"}, {Name: "email"}}},
			want: []ColumnProvenance{
				{OutputName: "id", SourceDatabase: "ref_db", SourceObject: "accounts", SourceColumn: "id"},
				{OutputName: "email", SourceDatabase: "ref_db", SourceObject: "accounts", SourceColumn: "email"},
			},
		},
		{
			name:    "missing referenced column",
			columns: []string{"missing"},
			detail:  &ObjectDetail{Columns: []ColumnSummary{{Name: "id"}}},
			wantErr: true,
		},
		{
			name:    "inspector failure",
			columns: []string{"id"},
			err:     errors.New("schema unavailable"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			inspector := &projectionInspector{detail: tt.detail, err: tt.err}
			// Given: FK metadata containing referenced columns.
			// When: the trusted related-record projection is built.
			got, err := resolveRelatedRecordProjection(context.Background(), inspector, relatedRecordProjectionInput{
				dsn:               "dsn",
				database:          "ref_db",
				object:            "accounts",
				referencedColumns: tt.columns,
			})
			// Then: every output column is verified against canonical metadata.
			if tt.wantErr {
				if err == nil {
					t.Fatal("resolveRelatedRecordProjection error = nil, want rejection")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveRelatedRecordProjection: %v", err)
			}
			if !reflect.DeepEqual(got.Columns, tt.want) {
				t.Fatalf("Columns = %#v, want %#v", got.Columns, tt.want)
			}
		})
	}
}
