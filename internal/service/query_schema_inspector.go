// Package service provides a parameterized MySQL/TiDB schema inspector that
// queries information_schema with fixed SQL and bind parameters.
// input: context, database/sql, strings, go-sql-driver/mysql, internal/model
// output: QuerySchemaInspector interface, MySQLSchemaInspector, EscapeSchemaSearch
// pos: Introspects databases, objects (tables/views), columns, indexes, and
// foreign keys from information_schema using parameterized queries; never
// interpolates external values into SQL text
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// Response caps for schema metadata queries.
const (
	schemaMaxColumns        = 512
	schemaMaxIndexColumns   = 256
	schemaMaxFKColumnPairs  = 256
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
	Name       string           `json:"name"`
	Kind       string           `json:"kind"`
	Columns    []ColumnSummary  `json:"columns"`
	Indexes    []IndexSummary   `json:"indexes"`
	ForeignKeys []FKSummary     `json:"foreignKeys"`
	Truncated  bool             `json:"truncated,omitempty"`
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
	Name       string           `json:"name"`
	NonUnique  bool             `json:"nonUnique"`
	Columns    []IndexColumn    `json:"columns"`
}

// IndexColumn is one column within an index.
type IndexColumn struct {
	Name    string `json:"name"`
	SeqInIndex int `json:"seqInIndex"`
}

// FKSummary groups one foreign key constraint with its column mappings in
// ORDINAL_POSITION order and its update/delete rules.
type FKSummary struct {
	Name       string       `json:"name"`
	Columns    []FKColumn   `json:"columns"`
	UpdateRule string       `json:"updateRule"`
	DeleteRule string       `json:"deleteRule"`
}

// FKColumn is one column mapping within a foreign key.
type FKColumn struct {
	Column              string `json:"column"`
	ReferencedSchema    string `json:"referencedSchema"`
	ReferencedTable     string `json:"referencedTable"`
	ReferencedColumn    string `json:"referencedColumn"`
}

// QuerySchemaInspector inspects MySQL/TiDB schema metadata using parameterized
// information_schema queries.
type QuerySchemaInspector interface {
	ListDatabases(ctx context.Context, dsn string, q string, includeSystem bool, page, pageSize int) ([]DatabaseSummary, model.PageInfo, error)
	ListObjects(ctx context.Context, dsn string, database, kind, q string, page, pageSize int) ([]ObjectSummary, model.PageInfo, error)
	GetObjectDetails(ctx context.Context, dsn string, database, name, kind string) (*ObjectDetail, error)
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
