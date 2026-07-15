// Package model provides domain entities for the resource management system.
// input: encoding/json, fmt packages
// output: ObjectKind type + Validate, DatabaseSummary, ObjectSummary, ColumnDetail, IndexDetail, ForeignKeyDetail, TruncationFlags, DatabaseListResponse, ObjectListResponse, ObjectDetailResponse, TableDefinitionResponse
// pos: Schema metadata response types for the governed query schema introspection API (Phase 38I)
// note: if this file changes, update header and README.md
package model

import (
	"encoding/json"
	"fmt"
)

// ObjectKind classifies a database object as either a table or a view.
// Unknown kinds fail closed so the frontend never renders an unsupported
// object type as if it were valid.
type ObjectKind string

const (
	ObjectKindTable ObjectKind = "table"
	ObjectKindView  ObjectKind = "view"
)

// Validate returns nil only for a known object kind.
func (k ObjectKind) Validate() error {
	switch k {
	case ObjectKindTable, ObjectKindView:
		return nil
	}
	return fmt.Errorf("invalid object kind: %s", k)
}

// DatabaseSummary is a lightweight entry in the database list response.
// It carries only the database name and whether it is the default for
// the target — no credentials, hosts, or connection details.
type DatabaseSummary struct {
	Name      string `json:"name"`
	IsDefault bool   `json:"isDefault"`
}

// ObjectSummary is a lightweight entry in the object (table/view) list
// response. It carries the database, object name, and kind — no column
// or index details.
type ObjectSummary struct {
	Database string     `json:"database"`
	Name     string     `json:"name"`
	Kind     ObjectKind `json:"kind"`
}

// ColumnDetail describes one column in an object detail response. It carries
// metadata only — no sample data, no DSN, no credentials.
type ColumnDetail struct {
	Name            string `json:"name"`
	DatabaseType    string `json:"databaseType"`
	OrdinalPosition int    `json:"ordinalPosition"`
	Nullable        bool   `json:"nullable"`
	PrimaryKey      bool   `json:"primaryKey"`
	AutoIncrement   bool   `json:"autoIncrement"`
}

// IndexDetail describes one index on a database object.
type IndexDetail struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
	Primary bool     `json:"primary"`
}

// ForeignKeyDetail describes one foreign key relationship from a database
// object to another object (possibly in a different database).
type ForeignKeyDetail struct {
	Name               string   `json:"name"`
	Columns            []string `json:"columns"`
	ReferencedDatabase string   `json:"referencedDatabase"`
	ReferencedObject   string   `json:"referencedObject"`
	ReferencedColumns  []string `json:"referencedColumns"`
	OnUpdate           string   `json:"onUpdate"`
	OnDelete           string   `json:"onDelete"`
}

// TruncationFlags explicitly reports whether any section of the object detail
// response was truncated due to size limits. Each flag is an explicit boolean
// so the frontend never has to guess from absence.
type TruncationFlags struct {
	Columns     bool `json:"columns"`
	Indexes     bool `json:"indexes"`
	ForeignKeys bool `json:"foreignKeys"`
}

// DatabaseListResponse is the envelope for GET /query-targets/{id}/schema/databases.
// It always carries the owning TargetResourceID so the frontend can correlate
// responses when switching targets.
type DatabaseListResponse struct {
	TargetResourceID int64             `json:"targetResourceId"`
	DefaultDatabase  *string           `json:"defaultDatabase"`
	Items            []DatabaseSummary `json:"items"`
	PageInfo         PageInfo          `json:"pageInfo"`
}

// ObjectListResponse is the envelope for GET /query-targets/{id}/schema/objects.
type ObjectListResponse struct {
	TargetResourceID int64           `json:"targetResourceId"`
	Database         string          `json:"database"`
	Items            []ObjectSummary `json:"items"`
	PageInfo         PageInfo        `json:"pageInfo"`
}

// ObjectDetailResponse is the full detail for one database object
// (GET /query-targets/{id}/schema/object-details). It carries columns, indexes,
// and foreign keys — never credentials or DSNs.
type ObjectDetailResponse struct {
	TargetResourceID int64              `json:"targetResourceId"`
	Database         string             `json:"database"`
	Name             string             `json:"name"`
	Kind             ObjectKind         `json:"kind"`
	Columns          []ColumnDetail     `json:"columns"`
	Indexes          []IndexDetail      `json:"indexes"`
	ForeignKeys      []ForeignKeyDetail `json:"foreignKeys"`
	Truncated        TruncationFlags    `json:"truncated"`
}

// TableDefinitionResponse is the governed response for
// GET /query-targets/{id}/schema/table-definition. It returns a bounded
// MySQL SHOW CREATE TABLE result. Definition text is request-ephemeral:
// never cached, persisted, logged, or placed in query history.
type TableDefinitionResponse struct {
	TargetResourceID int64      `json:"targetResourceId"`
	Database         string     `json:"database"`
	Name             string     `json:"name"`
	Kind             ObjectKind `json:"kind"`
	Dialect          string     `json:"dialect"`
	Definition       string     `json:"definition"`
	Truncated        bool       `json:"truncated"`
}

// MarshalJSON preserves the OpenAPI required-array invariant: columns, indexes,
// and foreignKeys are always JSON arrays (never null), including nested index
// and foreign-key column lists. Empty metadata is a valid ready state.
func (r ObjectDetailResponse) MarshalJSON() ([]byte, error) {
	type alias ObjectDetailResponse
	out := alias(r)
	if out.Columns == nil {
		out.Columns = []ColumnDetail{}
	}
	if out.Indexes == nil {
		out.Indexes = []IndexDetail{}
	} else {
		for i := range out.Indexes {
			if out.Indexes[i].Columns == nil {
				out.Indexes[i].Columns = []string{}
			}
		}
	}
	if out.ForeignKeys == nil {
		out.ForeignKeys = []ForeignKeyDetail{}
	} else {
		for i := range out.ForeignKeys {
			if out.ForeignKeys[i].Columns == nil {
				out.ForeignKeys[i].Columns = []string{}
			}
			if out.ForeignKeys[i].ReferencedColumns == nil {
				out.ForeignKeys[i].ReferencedColumns = []string{}
			}
		}
	}
	return json.Marshal(out)
}

// EnsureNonNilCollections mutates r so all declared collection fields are
// non-nil empty slices. Call at the service boundary before caching/returning.
func (r *ObjectDetailResponse) EnsureNonNilCollections() {
	if r.Columns == nil {
		r.Columns = []ColumnDetail{}
	}
	if r.Indexes == nil {
		r.Indexes = []IndexDetail{}
	}
	for i := range r.Indexes {
		if r.Indexes[i].Columns == nil {
			r.Indexes[i].Columns = []string{}
		}
	}
	if r.ForeignKeys == nil {
		r.ForeignKeys = []ForeignKeyDetail{}
	}
	for i := range r.ForeignKeys {
		if r.ForeignKeys[i].Columns == nil {
			r.ForeignKeys[i].Columns = []string{}
		}
		if r.ForeignKeys[i].ReferencedColumns == nil {
			r.ForeignKeys[i].ReferencedColumns = []string{}
		}
	}
}
