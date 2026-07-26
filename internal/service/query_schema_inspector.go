// Package service provides a parameterized MySQL/TiDB schema inspector that
// queries information_schema with fixed SQL and bind parameters.
// input: context, database/sql, strings, unicode/utf8, go-sql-driver/mysql, internal/model
// output: QuerySchemaInspector interface, TableDefinition, MySQLSchemaInspector, EscapeSchemaSearch, GetTableDefinition
// pos: Introspects databases, objects (tables/views), columns, indexes, and
// foreign keys from information_schema using parameterized queries; never
// interpolates external values into SQL text
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/model"
)

// Response caps for schema metadata queries.
const (
	schemaMaxColumns       = 512
	schemaMaxIndexColumns  = 256
	schemaMaxFKColumnPairs = 256
)

// System databases excluded by default when listing schemas.
var systemDatabases = []string{
	"information_schema",
	"performance_schema",
	"mysql",
	"sys",
}

// DatabaseSummary represents one row from information_schema.SCHEMATA.
type DatabaseSummary struct {
	Name string `json:"name"`
}

// ObjectSummary represents one table or view from information_schema.TABLES.
type ObjectSummary struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // "table" or "view"
}

// ObjectDetail holds the full column, index, and foreign-key metadata for a
// single table or view.
type ObjectDetail struct {
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Columns     []ColumnSummary `json:"columns"`
	Indexes     []IndexSummary  `json:"indexes"`
	ForeignKeys []FKSummary     `json:"foreignKeys"`
	Truncated   bool            `json:"truncated,omitempty"`
}

// TableDefinition holds the bounded SHOW CREATE TABLE output for a single
// verified base table. The definition is request-ephemeral and must not be
// cached, persisted, or logged.
type TableDefinition struct {
	Definition string
	Truncated  bool
}

// ColumnSummary is one row from information_schema.COLUMNS.
type ColumnSummary struct {
	Name     string `json:"name"`
	Position int    `json:"position"`
	Type     string `json:"type"`
	Nullable string `json:"nullable"`
	Key      string `json:"key"`
	Extra    string `json:"extra"`
}

// IndexSummary groups one logical index (potentially composite) with its
// constituent columns in SEQ_IN_INDEX order.
type IndexSummary struct {
	Name      string        `json:"name"`
	NonUnique bool          `json:"nonUnique"`
	Columns   []IndexColumn `json:"columns"`
}

// IndexColumn is one column within an index.
type IndexColumn struct {
	Name       string `json:"name"`
	SeqInIndex int    `json:"seqInIndex"`
}

// FKSummary groups one foreign key constraint with its column mappings in
// ORDINAL_POSITION order and its update/delete rules.
type FKSummary struct {
	Name       string     `json:"name"`
	Columns    []FKColumn `json:"columns"`
	UpdateRule string     `json:"updateRule"`
	DeleteRule string     `json:"deleteRule"`
}

// FKColumn is one column mapping within a foreign key.
type FKColumn struct {
	Column           string `json:"column"`
	ReferencedSchema string `json:"referencedSchema"`
	ReferencedTable  string `json:"referencedTable"`
	ReferencedColumn string `json:"referencedColumn"`
}

// RelationshipMapResult holds the inbound and outbound foreign-key
// relationships for one base table.
type RelationshipMapResult struct {
	Nodes     []RelationshipMapNodeResult
	Edges     []RelationshipMapEdgeResult
	Truncated bool
}

// RelationshipMapNodeResult is one table node in the relationship map.
type RelationshipMapNodeResult struct {
	ID       string
	Database string
	Name     string
	Kind     string
	Role     string
}

// RelationshipMapEdgeResult is one foreign-key edge in the relationship map.
type RelationshipMapEdgeResult struct {
	ID                string
	Direction         string
	SourceID          string
	TargetID          string
	Columns           []string
	ReferencedColumns []string
	OnUpdate          string
	OnDelete          string
}

// QuerySchemaInspector inspects MySQL/TiDB schema metadata using parameterized
// information_schema queries.
type QuerySchemaInspector interface {
	ListDatabases(ctx context.Context, dsn string, q string, includeSystem bool, page, pageSize int) ([]DatabaseSummary, model.PageInfo, error)
	ListObjects(ctx context.Context, dsn string, database, kind, q string, page, pageSize int) ([]ObjectSummary, model.PageInfo, error)
	GetObjectDetails(ctx context.Context, dsn string, database, name, kind string) (*ObjectDetail, error)
	GetTableDefinition(ctx context.Context, dsn string, database, name string) (*TableDefinition, error)
	GetRelationshipMap(ctx context.Context, dsn string, database, name string) (*RelationshipMapResult, error)
}

// MySQLSchemaInspector implements QuerySchemaInspector using parameterized
// information_schema queries. It opens a per-request connection with
// MaxOpenConns=1 and uses a read-only transaction.
type MySQLSchemaInspector struct{}

// NewMySQLSchemaInspector constructs a MySQLSchemaInspector.
func NewMySQLSchemaInspector() *MySQLSchemaInspector {
	return &MySQLSchemaInspector{}
}

// EscapeSchemaSearch escapes LIKE-special characters (%, _, \) in a user search
// string so they are matched literally. The escape character is backslash.
func EscapeSchemaSearch(q string) string {
	q = strings.ReplaceAll(q, `\`, `\\`)
	q = strings.ReplaceAll(q, `%`, `\%`)
	q = strings.ReplaceAll(q, `_`, `\_`)
	return q
}

// ListDatabases returns databases matching q (LIKE search), optionally
// excluding system databases.
func (s *MySQLSchemaInspector) ListDatabases(ctx context.Context, dsn string, q string, includeSystem bool, page, pageSize int) ([]DatabaseSummary, model.PageInfo, error) {
	page, pageSize = model.NormalizePagination(page, pageSize)
	offset := (page - 1) * pageSize

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, model.PageInfo{}, fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, model.PageInfo{}, err
	}
	defer tx.Rollback()

	escaped := EscapeSchemaSearch(q)
	likePattern := "%" + escaped + "%"

	// Count total matching rows.
	countSQL := "SELECT COUNT(*) FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ?"
	countArgs := []any{likePattern}
	if !includeSystem {
		countSQL += " AND SCHEMA_NAME NOT IN (?, ?, ?, ?)"
		countArgs = append(countArgs, systemDatabases[0], systemDatabases[1], systemDatabases[2], systemDatabases[3])
	}

	var total int
	if err := tx.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, model.PageInfo{}, err
	}

	// Fetch page.
	querySQL := "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME LIKE ?"
	queryArgs := []any{likePattern}
	if !includeSystem {
		querySQL += " AND SCHEMA_NAME NOT IN (?, ?, ?, ?)"
		queryArgs = append(queryArgs, systemDatabases[0], systemDatabases[1], systemDatabases[2], systemDatabases[3])
	}
	querySQL += " ORDER BY SCHEMA_NAME LIMIT ? OFFSET ?"
	queryArgs = append(queryArgs, pageSize, offset)

	rows, err := tx.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		return nil, model.PageInfo{}, err
	}
	defer rows.Close()

	var items []DatabaseSummary
	for rows.Next() {
		var d DatabaseSummary
		if err := rows.Scan(&d.Name); err != nil {
			return nil, model.PageInfo{}, err
		}
		items = append(items, d)
	}
	if err := rows.Err(); err != nil {
		return nil, model.PageInfo{}, err
	}

	pageInfo := model.NewPageInfo(page, pageSize, total)
	return items, pageInfo, nil
}

// ListObjects returns tables and views in the given database matching q (LIKE
// search), ordered deterministically by TABLE_TYPE then TABLE_NAME.
func (s *MySQLSchemaInspector) ListObjects(ctx context.Context, dsn string, database, kind, q string, page, pageSize int) ([]ObjectSummary, model.PageInfo, error) {
	page, pageSize = model.NormalizePagination(page, pageSize)
	offset := (page - 1) * pageSize

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, model.PageInfo{}, fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, model.PageInfo{}, err
	}
	defer tx.Rollback()

	escaped := EscapeSchemaSearch(q)
	likePattern := "%" + escaped + "%"

	// Build optional kind filter.
	kindFilter := ""
	var kindArgs []any
	if kind != "" {
		normalizedKind := normalizeObjectKind(kind)
		kindFilter = " AND TABLE_TYPE = ?"
		kindArgs = append(kindArgs, normalizedKind)
	}

	// Count total matching rows.
	countSQL := "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE IN ('BASE TABLE', 'VIEW') AND TABLE_NAME LIKE ?" + kindFilter
	countArgs := append([]any{database, likePattern}, kindArgs...)

	var total int
	if err := tx.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		return nil, model.PageInfo{}, err
	}

	// Fetch page.
	querySQL := "SELECT TABLE_NAME, TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_TYPE IN ('BASE TABLE', 'VIEW') AND TABLE_NAME LIKE ?" + kindFilter + " ORDER BY TABLE_TYPE, TABLE_NAME LIMIT ? OFFSET ?"
	queryArgs := append([]any{database, likePattern}, kindArgs...)
	queryArgs = append(queryArgs, pageSize, offset)

	rows, err := tx.QueryContext(ctx, querySQL, queryArgs...)
	if err != nil {
		return nil, model.PageInfo{}, err
	}
	defer rows.Close()

	var items []ObjectSummary
	for rows.Next() {
		var o ObjectSummary
		var tableType string
		if err := rows.Scan(&o.Name, &tableType); err != nil {
			return nil, model.PageInfo{}, err
		}
		o.Kind = normalizeTableType(tableType)
		items = append(items, o)
	}
	if err := rows.Err(); err != nil {
		return nil, model.PageInfo{}, err
	}

	pageInfo := model.NewPageInfo(page, pageSize, total)
	return items, pageInfo, nil
}

// GetObjectDetails returns full column, index, and foreign-key metadata for a
// single table or view. Response caps set truncated=true instead of silently
// growing payloads.
func (s *MySQLSchemaInspector) GetObjectDetails(ctx context.Context, dsn string, database, name, kind string) (*ObjectDetail, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	detail := &ObjectDetail{
		Name: name,
		Kind: normalizeTableType(kind),
	}

	// Columns (capped at 512).
	if err := s.loadColumns(ctx, tx, database, name, detail); err != nil {
		return nil, err
	}

	// Indexes (capped at 256 index-column rows).
	if err := s.loadIndexes(ctx, tx, database, name, detail); err != nil {
		return nil, err
	}

	// Foreign keys (capped at 256 FK column mappings).
	if err := s.loadForeignKeys(ctx, tx, database, name, detail); err != nil {
		return nil, err
	}

	return detail, nil
}

// GetTableDefinition returns the bounded SHOW CREATE TABLE output for a
// verified base table.
func (s *MySQLSchemaInspector) GetTableDefinition(ctx context.Context, dsn string, database, name string) (*TableDefinition, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var tableType string
	err = tx.QueryRowContext(ctx,
		"SELECT TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? LIMIT 1",
		database, name,
	).Scan(&tableType)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrSchemaObjectNotFound
		}
		return nil, err
	}

	if strings.ToUpper(strings.TrimSpace(tableType)) != "BASE TABLE" {
		return nil, ErrSchemaDefinitionNotSupported
	}

	quotedDB := quoteMySQLIdentifier(database)
	quotedTable := quoteMySQLIdentifier(name)
	showSQL := "SHOW CREATE TABLE " + quotedDB + "." + quotedTable

	var tableName, definition string
	if err := tx.QueryRowContext(ctx, showSQL).Scan(&tableName, &definition); err != nil {
		return nil, err
	}

	const maxDefBytes = 64 * 1024
	truncated := false
	if len(definition) > maxDefBytes {
		cut := maxDefBytes
		for cut > 0 && !utf8.RuneStart(definition[cut]) {
			cut--
		}
		definition = definition[:cut]
		truncated = true
	}

	return &TableDefinition{
		Definition: definition,
		Truncated:  truncated,
	}, nil
}

// constraintIdentity holds the key fields that identify one FK constraint
// (without column-level detail).
type constraintIdentity struct {
	constraintName string
	srcSchema      string
	srcTable       string
	refSchema      string
	refTable       string
}

// constraintColumn holds one column mapping fetched for a selected constraint.
type constraintColumn struct {
	constraintName string
	column         string
	refColumn      string
	ordinal        int
	updateRule     string
	deleteRule     string
}

// GetRelationshipMap returns inbound and outbound foreign-key relationships
// for one base table. Nodes and edges are capped at model.RelationshipMapMaxNodes
// and model.RelationshipMapMaxEdges respectively; Truncated=true when any
// candidate is omitted.
//
// The algorithm caps on distinct constraints (not KCU rows) so that composite
// FKs are never split across the limit boundary.
func (s *MySQLSchemaInspector) GetRelationshipMap(ctx context.Context, dsn string, database, name string) (*RelationshipMapResult, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var tableType string
	err = tx.QueryRowContext(ctx,
		"SELECT TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?",
		database, name,
	).Scan(&tableType)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrSchemaObjectNotFound
		}
		return nil, err
	}
	if strings.ToUpper(strings.TrimSpace(tableType)) != "BASE TABLE" {
		return nil, ErrSchemaDefinitionNotSupported
	}

	// maxConstraints caps the number of distinct FK constraints fetched per
	// direction. Using MaxEdges+1 lets us detect overflow without truncating
	// a composite FK mid-constraint.
	maxConstraints := model.RelationshipMapMaxEdges + 1

	// Step 1: Query distinct outbound constraint identities.
	outConstraints, outOverflow, err := queryDistinctConstraints(ctx, tx, database, name, "outbound", maxConstraints)
	if err != nil {
		return nil, err
	}

	// Step 2: Query distinct inbound constraint identities.
	inConstraints, inOverflow, err := queryDistinctConstraints(ctx, tx, database, name, "inbound", maxConstraints)
	if err != nil {
		return nil, err
	}

	// Step 3: Fetch all columns for the selected constraints.
	outCols, err := fetchConstraintColumns(ctx, tx, database, name, "outbound", outConstraints)
	if err != nil {
		return nil, err
	}

	inCols, err := fetchConstraintColumns(ctx, tx, database, name, "inbound", inConstraints)
	if err != nil {
		return nil, err
	}

	// Step 4: Group columns by constraint identity, producing one fkGroup per FK.
	type fkGroup struct {
		direction  string
		conName    string
		srcSchema  string
		srcTable   string
		refSchema  string
		refTable   string
		updateRule string
		deleteRule string
		columns    []string
		refColumns []string
	}

	outByName := make(map[string]*fkGroup, len(outConstraints))
	for _, ci := range outConstraints {
		g := &fkGroup{
			direction: "outbound",
			conName:   ci.constraintName,
			srcSchema: ci.srcSchema,
			srcTable:  ci.srcTable,
			refSchema: ci.refSchema,
			refTable:  ci.refTable,
		}
		outByName[ci.constraintName] = g
	}
	for _, c := range outCols {
		if g, ok := outByName[c.constraintName]; ok {
			g.columns = append(g.columns, c.column)
			g.refColumns = append(g.refColumns, c.refColumn)
			if g.updateRule == "" {
				g.updateRule = c.updateRule
				g.deleteRule = c.deleteRule
			}
		}
	}

	inByName := make(map[string]*fkGroup, len(inConstraints))
	for _, ci := range inConstraints {
		g := &fkGroup{
			direction: "inbound",
			conName:   ci.constraintName,
			srcSchema: ci.srcSchema,
			srcTable:  ci.srcTable,
			refSchema: ci.refSchema,
			refTable:  ci.refTable,
		}
		inByName[ci.constraintName] = g
	}
	for _, c := range inCols {
		if g, ok := inByName[c.constraintName]; ok {
			g.columns = append(g.columns, c.column)
			g.refColumns = append(g.refColumns, c.refColumn)
			if g.updateRule == "" {
				g.updateRule = c.updateRule
				g.deleteRule = c.deleteRule
			}
		}
	}

	type sortedGroup struct {
		direction  string
		relSchema  string
		relTable   string
		conName    string
		srcSchema  string
		srcTable   string
		refSchema  string
		refTable   string
		updateRule string
		deleteRule string
		columns    []string
		refColumns []string
	}

	var allGroups []sortedGroup
	for _, g := range outByName {
		allGroups = append(allGroups, sortedGroup{
			direction:  g.direction,
			relSchema:  g.refSchema,
			relTable:   g.refTable,
			conName:    g.conName,
			srcSchema:  g.srcSchema,
			srcTable:   g.srcTable,
			refSchema:  g.refSchema,
			refTable:   g.refTable,
			updateRule: g.updateRule,
			deleteRule: g.deleteRule,
			columns:    g.columns,
			refColumns: g.refColumns,
		})
	}
	for _, g := range inByName {
		allGroups = append(allGroups, sortedGroup{
			direction:  g.direction,
			relSchema:  g.srcSchema,
			relTable:   g.srcTable,
			conName:    g.conName,
			srcSchema:  g.srcSchema,
			srcTable:   g.srcTable,
			refSchema:  g.refSchema,
			refTable:   g.refTable,
			updateRule: g.updateRule,
			deleteRule: g.deleteRule,
			columns:    g.columns,
			refColumns: g.refColumns,
		})
	}

	sort.Slice(allGroups, func(i, j int) bool {
		if allGroups[i].direction != allGroups[j].direction {
			return allGroups[i].direction < allGroups[j].direction
		}
		if allGroups[i].relSchema != allGroups[j].relSchema {
			return allGroups[i].relSchema < allGroups[j].relSchema
		}
		if allGroups[i].relTable != allGroups[j].relTable {
			return allGroups[i].relTable < allGroups[j].relTable
		}
		return allGroups[i].conName < allGroups[j].conName
	})

	nodeIDMap := map[string]string{
		database + "." + name: "n0",
	}
	nodes := []RelationshipMapNodeResult{{
		ID:       "n0",
		Database: database,
		Name:     name,
		Kind:     "table",
		Role:     "root",
	}}
	nextNodeID := 1
	truncated := outOverflow || inOverflow

	var edges []RelationshipMapEdgeResult
	nextEdgeID := 0

	for _, g := range allGroups {
		var relatedSchema, relatedTable string
		if g.direction == "outbound" {
			relatedSchema = g.refSchema
			relatedTable = g.refTable
		} else {
			relatedSchema = g.srcSchema
			relatedTable = g.srcTable
		}

		relKey := relatedSchema + "." + relatedTable
		relatedNodeID, exists := nodeIDMap[relKey]
		if !exists {
			if len(nodes) >= model.RelationshipMapMaxNodes {
				truncated = true
				continue
			}
			relatedNodeID = fmt.Sprintf("n%d", nextNodeID)
			nextNodeID++
			nodeIDMap[relKey] = relatedNodeID
			nodes = append(nodes, RelationshipMapNodeResult{
				ID:       relatedNodeID,
				Database: relatedSchema,
				Name:     relatedTable,
				Kind:     "table",
				Role:     "related",
			})
		}

		if len(edges) >= model.RelationshipMapMaxEdges {
			truncated = true
			continue
		}

		var sourceID, targetID string
		if g.direction == "outbound" {
			sourceID = "n0"
			targetID = relatedNodeID
		} else {
			sourceID = relatedNodeID
			targetID = "n0"
		}

		edgeID := fmt.Sprintf("e%d", nextEdgeID)
		nextEdgeID++
		edges = append(edges, RelationshipMapEdgeResult{
			ID:                edgeID,
			Direction:         g.direction,
			SourceID:          sourceID,
			TargetID:          targetID,
			Columns:           g.columns,
			ReferencedColumns: g.refColumns,
			OnUpdate:          g.updateRule,
			OnDelete:          g.deleteRule,
		})
	}

	return &RelationshipMapResult{
		Nodes:     nodes,
		Edges:     edges,
		Truncated: truncated,
	}, nil
}

// queryDistinctConstraints fetches distinct FK constraint identities for one
// direction. It returns the constraint list and whether more constraints exist
// beyond the limit (overflow).
func queryDistinctConstraints(ctx context.Context, tx *sql.Tx, database, tableName, direction string, limit int) ([]constraintIdentity, bool, error) {
	var rows *sql.Rows
	var err error

	switch direction {
	case "outbound":
		rows, err = tx.QueryContext(ctx,
			`SELECT DISTINCT kcu.CONSTRAINT_NAME, kcu.TABLE_SCHEMA, kcu.TABLE_NAME,
			        kcu.REFERENCED_TABLE_SCHEMA, kcu.REFERENCED_TABLE_NAME
			 FROM information_schema.KEY_COLUMN_USAGE kcu
			 WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ?
			   AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
			 ORDER BY kcu.REFERENCED_TABLE_SCHEMA, kcu.REFERENCED_TABLE_NAME,
			          kcu.CONSTRAINT_NAME
			 LIMIT ?`,
			database, tableName, limit,
		)
	case "inbound":
		rows, err = tx.QueryContext(ctx,
			`SELECT DISTINCT kcu.CONSTRAINT_NAME, kcu.TABLE_SCHEMA, kcu.TABLE_NAME,
			        kcu.REFERENCED_TABLE_SCHEMA, kcu.REFERENCED_TABLE_NAME
			 FROM information_schema.KEY_COLUMN_USAGE kcu
			 WHERE kcu.REFERENCED_TABLE_SCHEMA = ? AND kcu.REFERENCED_TABLE_NAME = ?
			 ORDER BY kcu.TABLE_SCHEMA, kcu.TABLE_NAME,
			          kcu.CONSTRAINT_NAME
			 LIMIT ?`,
			database, tableName, limit,
		)
	default:
		return nil, false, fmt.Errorf("unknown direction: %s", direction)
	}
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var constraints []constraintIdentity
	for rows.Next() {
		var ci constraintIdentity
		if err := rows.Scan(&ci.constraintName, &ci.srcSchema, &ci.srcTable, &ci.refSchema, &ci.refTable); err != nil {
			return nil, false, err
		}
		constraints = append(constraints, ci)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}

	// If we fetched exactly `limit` rows, there may be more — signal overflow.
	overflow := len(constraints) >= limit
	if overflow {
		// Drop the sentinel row; we only needed it for overflow detection.
		constraints = constraints[:len(constraints)-1]
	}

	return constraints, overflow, nil
}

// fetchConstraintColumns fetches all column mappings for the given constraint
// identities, joined with REFERENTIAL_CONSTRAINTS for update/delete rules.
func fetchConstraintColumns(ctx context.Context, tx *sql.Tx, database, tableName, direction string, constraints []constraintIdentity) ([]constraintColumn, error) {
	if len(constraints) == 0 {
		return nil, nil
	}

	names := make([]any, 0, len(constraints))
	placeholders := make([]string, 0, len(constraints))
	for _, ci := range constraints {
		names = append(names, ci.constraintName)
		placeholders = append(placeholders, "?")
	}
	inClause := strings.Join(placeholders, ",")

	var query string
	var args []any

	switch direction {
	case "outbound":
		query = fmt.Sprintf(
			`SELECT kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_COLUMN_NAME,
			        kcu.ORDINAL_POSITION, rc.UPDATE_RULE, rc.DELETE_RULE
			 FROM information_schema.KEY_COLUMN_USAGE kcu
			 JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
			   ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
			  AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
			 WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ?
			   AND kcu.CONSTRAINT_NAME IN (%s)
			 ORDER BY kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION`, inClause,
		)
		args = append([]any{database, tableName}, names...)

	case "inbound":
		query = fmt.Sprintf(
			`SELECT kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_COLUMN_NAME,
			        kcu.ORDINAL_POSITION, rc.UPDATE_RULE, rc.DELETE_RULE
			 FROM information_schema.KEY_COLUMN_USAGE kcu
			 JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
			   ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
			  AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
			 WHERE kcu.REFERENCED_TABLE_SCHEMA = ? AND kcu.REFERENCED_TABLE_NAME = ?
			   AND kcu.CONSTRAINT_NAME IN (%s)
			 ORDER BY kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION`, inClause,
		)
		args = append([]any{database, tableName}, names...)
	}

	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []constraintColumn
	for rows.Next() {
		var c constraintColumn
		if err := rows.Scan(&c.constraintName, &c.column, &c.refColumn, &c.ordinal, &c.updateRule, &c.deleteRule); err != nil {
			return nil, err
		}
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return cols, nil
}

// loadColumns queries COLUMNS for the given table, capped at schemaMaxColumns.
func (s *MySQLSchemaInspector) loadColumns(ctx context.Context, tx *sql.Tx, database, name string, detail *ObjectDetail) error {
	rows, err := tx.QueryContext(ctx,
		"SELECT COLUMN_NAME, ORDINAL_POSITION, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION LIMIT ?",
		database, name, schemaMaxColumns+1,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		if len(detail.Columns) >= schemaMaxColumns {
			detail.Truncated = true
			break
		}
		var c ColumnSummary
		if err := rows.Scan(&c.Name, &c.Position, &c.Type, &c.Nullable, &c.Key, &c.Extra); err != nil {
			return err
		}
		detail.Columns = append(detail.Columns, c)
	}
	return rows.Err()
}

// loadIndexes queries STATISTICS for the given table, groups by INDEX_NAME,
// and caps total index-column rows at schemaMaxIndexColumns.
func (s *MySQLSchemaInspector) loadIndexes(ctx context.Context, tx *sql.Tx, database, name string, detail *ObjectDetail) error {
	rows, err := tx.QueryContext(ctx,
		"SELECT INDEX_NAME, SEQ_IN_INDEX, COLUMN_NAME, NON_UNIQUE FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX LIMIT ?",
		database, name, schemaMaxIndexColumns+1,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rawIndexRow struct {
		indexName  string
		seqInIndex int
		columnName string
		nonUnique  int
	}
	var rawRows []rawIndexRow
	for rows.Next() {
		if len(rawRows) >= schemaMaxIndexColumns {
			detail.Truncated = true
			break
		}
		var r rawIndexRow
		if err := rows.Scan(&r.indexName, &r.seqInIndex, &r.columnName, &r.nonUnique); err != nil {
			return err
		}
		rawRows = append(rawRows, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Group by index name (rows are already ordered by INDEX_NAME, SEQ_IN_INDEX).
	var currentIdx *IndexSummary
	for _, r := range rawRows {
		if currentIdx == nil || currentIdx.Name != r.indexName {
			if currentIdx != nil {
				detail.Indexes = append(detail.Indexes, *currentIdx)
			}
			currentIdx = &IndexSummary{
				Name:      r.indexName,
				NonUnique: r.nonUnique != 0,
			}
		}
		currentIdx.Columns = append(currentIdx.Columns, IndexColumn{
			Name:       r.columnName,
			SeqInIndex: r.seqInIndex,
		})
	}
	if currentIdx != nil {
		detail.Indexes = append(detail.Indexes, *currentIdx)
	}

	return nil
}

// loadForeignKeys queries KEY_COLUMN_USAGE joined with REFERENTIAL_CONSTRAINTS
// for the given table, groups by CONSTRAINT_NAME, and caps total FK column
// mappings at schemaMaxFKColumnPairs.
func (s *MySQLSchemaInspector) loadForeignKeys(ctx context.Context, tx *sql.Tx, database, name string, detail *ObjectDetail) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT kcu.CONSTRAINT_NAME, kcu.COLUMN_NAME, kcu.REFERENCED_TABLE_SCHEMA,
		        kcu.REFERENCED_TABLE_NAME, kcu.REFERENCED_COLUMN_NAME,
		        rc.UPDATE_RULE, rc.DELETE_RULE
		 FROM information_schema.KEY_COLUMN_USAGE kcu
		 JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
		   ON kcu.CONSTRAINT_NAME = rc.CONSTRAINT_NAME
		  AND kcu.TABLE_SCHEMA = rc.CONSTRAINT_SCHEMA
		 WHERE kcu.TABLE_SCHEMA = ? AND kcu.TABLE_NAME = ?
		   AND kcu.REFERENCED_TABLE_NAME IS NOT NULL
		 ORDER BY kcu.CONSTRAINT_NAME, kcu.ORDINAL_POSITION
		 LIMIT ?`,
		database, name, schemaMaxFKColumnPairs+1,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	type rawFKRow struct {
		constraintName string
		column         string
		refSchema      string
		refTable       string
		refColumn      string
		updateRule     string
		deleteRule     string
	}
	var rawRows []rawFKRow
	for rows.Next() {
		if len(rawRows) >= schemaMaxFKColumnPairs {
			detail.Truncated = true
			break
		}
		var r rawFKRow
		if err := rows.Scan(&r.constraintName, &r.column, &r.refSchema, &r.refTable, &r.refColumn, &r.updateRule, &r.deleteRule); err != nil {
			return err
		}
		rawRows = append(rawRows, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Group by constraint name (rows are already ordered by CONSTRAINT_NAME, ORDINAL_POSITION).
	var currentFK *FKSummary
	for _, r := range rawRows {
		if currentFK == nil || currentFK.Name != r.constraintName {
			if currentFK != nil {
				detail.ForeignKeys = append(detail.ForeignKeys, *currentFK)
			}
			currentFK = &FKSummary{
				Name:       r.constraintName,
				UpdateRule: r.updateRule,
				DeleteRule: r.deleteRule,
			}
		}
		currentFK.Columns = append(currentFK.Columns, FKColumn{
			Column:           r.column,
			ReferencedSchema: r.refSchema,
			ReferencedTable:  r.refTable,
			ReferencedColumn: r.refColumn,
		})
	}
	if currentFK != nil {
		detail.ForeignKeys = append(detail.ForeignKeys, *currentFK)
	}

	return nil
}

// normalizeTableType maps information_schema TABLE_TYPE to a stable kind label.
func normalizeTableType(tableType string) string {
	switch strings.ToUpper(strings.TrimSpace(tableType)) {
	case "BASE TABLE":
		return "table"
	case "VIEW":
		return "view"
	default:
		return strings.ToLower(strings.TrimSpace(tableType))
	}
}

// normalizeObjectKind normalizes a user-supplied kind filter to the
// information_schema TABLE_TYPE value.
func normalizeObjectKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "table":
		return "BASE TABLE"
	case "view":
		return "VIEW"
	default:
		return strings.ToUpper(strings.TrimSpace(kind))
	}
}

// unused import guard
var _ = time.Now
