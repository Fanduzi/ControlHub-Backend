// Package model provides domain entities for the resource management system.
// input: fmt package
// output: ObjectKind type + Validate, DatabaseSummary, ObjectSummary, ColumnDetail, IndexDetail, ForeignKeyDetail, TruncationFlags, DatabaseListResponse, ObjectListResponse, ObjectDetailResponse
// pos: Schema metadata response types for the governed query schema introspection API (Phase 38I)
// note: if this file changes, update header and README.md
package model

import "fmt"

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
	Name                string   `json:"name"`
	Columns             []string `json:"columns"`
	ReferencedDatabase  string   `json:"referencedDatabase"`
	ReferencedObject    string   `json:"referencedObject"`
	ReferencedColumns   []string `json:"referencedColumns"`
	OnUpdate            string   `json:"onUpdate"`
	OnDelete            string   `json:"onDelete"`
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
	TargetResourceID int64              `json:"targetResourceId"`
	DefaultDatabase  *string            `json:"defaultDatabase"`
	Items            []DatabaseSummary  `json:"items"`
	PageInfo         PageInfo           `json:"pageInfo"`
}

// ObjectListResponse is the envelope for GET /query-targets/{id}/schema/objects.
type ObjectListResponse struct {
	TargetResourceID int64            `json:"targetResourceId"`
	Database         string           `json:"database"`
	Items            []ObjectSummary  `json:"items"`
	PageInfo         PageInfo         `json:"pageInfo"`
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
