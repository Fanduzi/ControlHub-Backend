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

// Relationship map constants define the v1 caps for the relationship map response.
const (
	RelationshipMapMaxNodes = 40
	RelationshipMapMaxEdges = 80
)

// RelationshipMapRole classifies a node in the relationship map.
type RelationshipMapRole string

const (
	RelationshipMapRoleRoot    RelationshipMapRole = "root"
	RelationshipMapRoleRelated RelationshipMapRole = "related"
)

// Validate returns nil only for a known role.
func (r RelationshipMapRole) Validate() error {
	switch r {
	case RelationshipMapRoleRoot, RelationshipMapRoleRelated:
		return nil
	}
	return fmt.Errorf("invalid relationship map role: %s", r)
}

// RelationshipMapDirection classifies an edge direction in the relationship map.
type RelationshipMapDirection string

const (
	RelationshipMapDirectionInbound  RelationshipMapDirection = "inbound"
	RelationshipMapDirectionOutbound RelationshipMapDirection = "outbound"
)

// Validate returns nil only for a known direction.
func (d RelationshipMapDirection) Validate() error {
	switch d {
	case RelationshipMapDirectionInbound, RelationshipMapDirectionOutbound:
		return nil
	}
	return fmt.Errorf("invalid relationship map direction: %s", d)
}

// RelationshipMapNode describes one table node in the relationship map.
// ID is an opaque request-local token (n0, n1, ...), not parseable by clients.
type RelationshipMapNode struct {
	ID       string              `json:"id"`
	Database string              `json:"database"`
	Name     string              `json:"name"`
	Kind     ObjectKind          `json:"kind"`
	Role     RelationshipMapRole `json:"role"`
}

// RelationshipMapEdge describes one foreign-key edge in the relationship map.
// ID is an opaque request-local token (e0, e1, ...), not parseable by clients.
// SourceID and TargetID must reference existing node IDs in the same response.
type RelationshipMapEdge struct {
	ID                string                  `json:"id"`
	Direction         RelationshipMapDirection `json:"direction"`
	SourceID          string                  `json:"sourceId"`
	TargetID          string                  `json:"targetId"`
	Columns           []string                `json:"columns"`
	ReferencedColumns []string                `json:"referencedColumns"`
	OnUpdate          string                  `json:"onUpdate"`
	OnDelete          string                  `json:"onDelete"`
}

// RelationshipMapResponse is the envelope for
// GET /query-targets/{id}/schema/relationship-map. It returns the direct
// inbound and outbound foreign-key relationships for one base table.
// Nodes and edges are always JSON arrays (never null).
type RelationshipMapResponse struct {
	TargetResourceID int64                  `json:"targetResourceId"`
	Root             RelationshipMapNode    `json:"root"`
	Nodes            []RelationshipMapNode  `json:"nodes"`
	Edges            []RelationshipMapEdge  `json:"edges"`
	Truncated        bool                   `json:"truncated"`
}

// Validate checks structural invariants of the relationship map response.
// It ensures: nodes contains exactly one root matching the Root field,
// all node/edge IDs are unique, every edge endpoint references an existing node,
// inbound edges point related→root, outbound edges point root→related,
// and column arrays are non-empty and same length.
func (r RelationshipMapResponse) Validate() error {
	if len(r.Nodes) == 0 {
		return fmt.Errorf("nodes must not be empty")
	}

	rootCount := 0
	rootIdx := -1
	nodeIDs := make(map[string]bool, len(r.Nodes))
	for i, n := range r.Nodes {
		if n.Role == RelationshipMapRoleRoot {
			rootCount++
			rootIdx = i
		}
		if nodeIDs[n.ID] {
			return fmt.Errorf("duplicate node ID: %s", n.ID)
		}
		nodeIDs[n.ID] = true
	}
	if rootCount != 1 {
		return fmt.Errorf("expected exactly one root node, got %d", rootCount)
	}
	if rootIdx < 0 {
		return fmt.Errorf("root node not found")
	}

	root := r.Nodes[rootIdx]
	if root.Database != r.Root.Database || root.Name != r.Root.Name || root.Kind != r.Root.Kind {
		return fmt.Errorf("root node does not match Root field")
	}

	edgeIDs := make(map[string]bool, len(r.Edges))
	for _, e := range r.Edges {
		if edgeIDs[e.ID] {
			return fmt.Errorf("duplicate edge ID: %s", e.ID)
		}
		edgeIDs[e.ID] = true

		if !nodeIDs[e.SourceID] {
			return fmt.Errorf("edge %s references missing source node: %s", e.ID, e.SourceID)
		}
		if !nodeIDs[e.TargetID] {
			return fmt.Errorf("edge %s references missing target node: %s", e.ID, e.TargetID)
		}

		switch e.Direction {
		case RelationshipMapDirectionInbound:
			if e.TargetID != root.ID {
				return fmt.Errorf("inbound edge %s target must be root", e.ID)
			}
		case RelationshipMapDirectionOutbound:
			if e.SourceID != root.ID {
				return fmt.Errorf("outbound edge %s source must be root", e.ID)
			}
		default:
			return fmt.Errorf("edge %s has invalid direction: %s", e.ID, e.Direction)
		}

		if len(e.Columns) == 0 {
			return fmt.Errorf("edge %s columns must not be empty", e.ID)
		}
		if len(e.Columns) != len(e.ReferencedColumns) {
			return fmt.Errorf("edge %s columns/referencedColumns length mismatch", e.ID)
		}
	}
	return nil
}

// MarshalJSON preserves the OpenAPI required-array invariant: nodes, edges,
// and edge column lists are always JSON arrays (never null).
func (r RelationshipMapResponse) MarshalJSON() ([]byte, error) {
	type alias RelationshipMapResponse
	out := alias(r)
	if out.Nodes == nil {
		out.Nodes = []RelationshipMapNode{}
	}
	if out.Edges == nil {
		out.Edges = []RelationshipMapEdge{}
	} else {
		for i := range out.Edges {
			if out.Edges[i].Columns == nil {
				out.Edges[i].Columns = []string{}
			}
			if out.Edges[i].ReferencedColumns == nil {
				out.Edges[i].ReferencedColumns = []string{}
			}
		}
	}
	return json.Marshal(out)
}

// EnsureNonNilCollections mutates r so all declared collection fields are
// non-nil empty slices. Call at the service boundary before caching/returning.
func (r *RelationshipMapResponse) EnsureNonNilCollections() {
	if r.Nodes == nil {
		r.Nodes = []RelationshipMapNode{}
	}
	if r.Edges == nil {
		r.Edges = []RelationshipMapEdge{}
	}
	for i := range r.Edges {
		if r.Edges[i].Columns == nil {
			r.Edges[i].Columns = []string{}
		}
		if r.Edges[i].ReferencedColumns == nil {
			r.Edges[i].ReferencedColumns = []string{}
		}
	}
}
