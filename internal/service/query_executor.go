// Package service provides the MySQL/TiDB query executor for the read-only sandbox.
// input: context, database/sql, fmt, strings, time, go-sql-driver/mysql, internal/model
// output: MySQLQueryExecutor, NewMySQLQueryExecutor, QueryExecutorCaps, newScanPointer, normalizeScanned (implements QueryDatabaseExecutor)
// pos: Runs guarded ordinary, compiler-owned template, and navigation SELECTs against a target DB under read-only transactions with column/cell/payload caps; emits the aligned cellTruncated matrix from the same scan pass that retains values; paginated windows reject on payload-cap overflow, non-paged results truncate; preserves SQL NULL as JSON null
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
)

// Default result caps (spec): 100 columns, 8192 bytes per cell, 1 MiB total.
const (
	defaultMaxColumns       = 100
	defaultMaxCellBytes     = 8192
	defaultMaxResponseBytes = 1 << 20
)

// QueryExecutorCaps bounds a result set so a sandboxed query can never return an
// unbounded payload. Zero values fall back to the spec defaults.
type QueryExecutorCaps struct {
	MaxColumns       int
	MaxCellBytes     int
	MaxResponseBytes int
}

// MySQLQueryExecutor runs a guarded SELECT against a MySQL/TiDB target using a
// per-request connection and a read-only transaction. It enforces column, cell,
// and total-response caps and returns ErrQueryResultTooLarge when the column cap
// is exceeded (a controlled failure, not a 500).
type MySQLQueryExecutor struct {
	caps   QueryExecutorCaps
	openDB func(string) (*sql.DB, error)
}

// NewMySQLQueryExecutor builds an executor, filling zero caps with defaults.
func NewMySQLQueryExecutor(caps QueryExecutorCaps) *MySQLQueryExecutor {
	if caps.MaxColumns == 0 {
		caps.MaxColumns = defaultMaxColumns
	}
	if caps.MaxCellBytes == 0 {
		caps.MaxCellBytes = defaultMaxCellBytes
	}
	if caps.MaxResponseBytes == 0 {
		caps.MaxResponseBytes = defaultMaxResponseBytes
	}
	return &MySQLQueryExecutor{
		caps:   caps,
		openDB: func(dsn string) (*sql.DB, error) { return sql.Open("mysql", dsn) },
	}
}

// Query executes the guarded SELECT and returns the bounded result. The DSN is
// used only to open the connection and never leaves this method.
func (e *MySQLQueryExecutor) Query(ctx context.Context, dsn string, guarded GuardedQuery) (QueryDatabaseResult, error) {
	return e.queryGuarded(ctx, dsn, guarded)
}

// QueryTemplate executes only the compiler-produced guarded statement and its
// positional arguments. It is not a generic parameterized-query API.
func (e *MySQLQueryExecutor) QueryTemplate(ctx context.Context, dsn string, statement GuardedTemplateStatement) (QueryDatabaseResult, error) {
	return e.queryGuarded(ctx, dsn, statement.query, statement.args...)
}

func (e *MySQLQueryExecutor) queryGuarded(ctx context.Context, dsn string, guarded GuardedQuery, args ...any) (QueryDatabaseResult, error) {
	db, err := e.openTarget(dsn)
	if err != nil {
		return QueryDatabaseResult{}, fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	// Read-only transaction as defense in depth behind the SQL guard.
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return QueryDatabaseResult{}, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, guarded.ExecutableSQL, args...)
	if err != nil {
		return QueryDatabaseResult{}, err
	}
	defer rows.Close()

	scanLimit := guarded.LimitApplied
	if guarded.ResultLimit > 0 {
		scanLimit = guarded.ResultLimit
	}
	return e.scanBoundedRows(rows, scanLimit, guarded.ResultLimit > 0)
}

// QueryRelatedRecords executes a parameterized SELECT built by the service for
// related-record navigation. It runs under a read-only transaction, binds only
// the service-supplied values, and enforces the same column/cell/payload caps as
// Query. This method is not a generic parameterized-query API.
func (e *MySQLQueryExecutor) QueryRelatedRecords(ctx context.Context, dsn string, input RelatedRecordsQueryInput) (QueryDatabaseResult, error) {
	db, err := e.openTarget(dsn)
	if err != nil {
		return QueryDatabaseResult{}, fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return QueryDatabaseResult{}, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, input.Statement, input.Values...)
	if err != nil {
		return QueryDatabaseResult{}, err
	}
	defer rows.Close()

	return e.scanBoundedRows(rows, input.Limit, false)
}

func (e *MySQLQueryExecutor) openTarget(dsn string) (*sql.DB, error) {
	if e.openDB != nil {
		return e.openDB(dsn)
	}
	return sql.Open("mysql", dsn)
}

// scanBoundedRows reads rows into a bounded QueryDatabaseResult, enforcing
// column, cell, and payload caps. It is shared by Query and QueryRelatedRecords.
// A paginated window must stay contiguous with its fixed offset, so a
// response-byte overflow rejects the whole window with ErrQueryResultTooLarge
// instead of returning a partial page the next offset would silently skip.
// Non-paginated callers keep the bounded truncated-success contract.
func (e *MySQLQueryExecutor) scanBoundedRows(rows *sql.Rows, limit int, paginatedWindow bool) (QueryDatabaseResult, error) {
	colTypes, err := rows.ColumnTypes()
	if err != nil {
		return QueryDatabaseResult{}, err
	}
	if len(colTypes) > e.caps.MaxColumns {
		return QueryDatabaseResult{}, ErrQueryResultTooLarge
	}

	columns := make([]model.QueryResultColumn, len(colTypes))
	for i, ct := range colTypes {
		nullable, _ := ct.Nullable()
		columns[i] = model.QueryResultColumn{
			Name:         ct.Name(),
			DatabaseType: normalizeDatabaseType(ct.DatabaseTypeName()),
			Nullable:     nullable,
		}
	}

	result := QueryDatabaseResult{Columns: columns, Rows: make([][]any, 0)}
	cellTruncated := make([][]bool, 0)
	responseBytes := 0

	for rows.Next() {
		if limit > 0 && result.RowCount >= limit {
			result.Truncated = true
			break
		}
		ptrs := make([]any, len(colTypes))
		for i, ct := range colTypes {
			ptrs[i] = newScanPointer(ct.DatabaseTypeName())
		}
		if err := rows.Scan(ptrs...); err != nil {
			return QueryDatabaseResult{}, err
		}
		row := make([]any, len(colTypes))
		rowTruncated := make([]bool, len(colTypes))
		rowBytes := 0
		for i, p := range ptrs {
			safe, n, truncated, err := e.toJSONSafe(normalizeScanned(p))
			if err != nil {
				return QueryDatabaseResult{}, err
			}
			row[i] = safe
			rowTruncated[i] = truncated
			rowBytes += n
		}
		responseBytes += rowBytes
		if responseBytes > e.caps.MaxResponseBytes {
			if paginatedWindow {
				return QueryDatabaseResult{}, ErrQueryResultTooLarge
			}
			result.Truncated = true
			break
		}
		result.Rows = append(result.Rows, row)
		// The matrix row appends in lockstep with the delivered row: a row
		// dropped by the response-byte budget above leaves no dangling
		// truncation evidence behind (T8-B).
		cellTruncated = append(cellTruncated, rowTruncated)
		result.RowCount++
	}
	if err := rows.Err(); err != nil {
		return QueryDatabaseResult{}, err
	}
	result.CellTruncated = cellTruncated
	return result, nil
}

// newScanPointer returns a pointer to a NULL-safe scan destination chosen by the
// column's database type name. SQL NULL lands as a zero-valued sql.Null*
// (Valid=false); non-null values keep their native type (int64, float64,
// time.Time, bool, string) so JSON preserves numbers as numbers. Text, blob,
// JSON, and unknown types scan into NullString (NULL -> nil, non-null -> string).
func newScanPointer(dbType string) any {
	switch strings.ToUpper(strings.TrimSpace(dbType)) {
	case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "YEAR":
		return new(sql.NullInt64)
	case "FLOAT", "DOUBLE", "REAL":
		return new(sql.NullFloat64)
	case "DATE", "DATETIME", "TIMESTAMP", "TIME":
		return new(sql.NullTime)
	case "BOOL", "BOOLEAN":
		return new(sql.NullBool)
	default:
		// DECIMAL/NUMERIC (preserve precision as text), CHAR/VARCHAR/TEXT/BLOB,
		// JSON/ENUM/SET, and unknown types all scan into NullString.
		return new(sql.NullString)
	}
}

// normalizeScanned unwraps a NULL-safe scan pointer into a plain JSON value or
// nil. It is the NULL/validity boundary between the driver and the response.
func normalizeScanned(p any) any {
	switch v := p.(type) {
	case *sql.NullInt64:
		if v.Valid {
			return v.Int64
		}
		return nil
	case *sql.NullFloat64:
		if v.Valid {
			return v.Float64
		}
		return nil
	case *sql.NullString:
		if v.Valid {
			return v.String
		}
		return nil
	case *sql.NullTime:
		if v.Valid {
			return v.Time
		}
		return nil
	case *sql.NullBool:
		if v.Valid {
			return v.Bool
		}
		return nil
	default:
		return nil
	}
}

// toJSONSafe converts one materialized scan value into its retained JSON-safe
// value, its approximate serialized byte weight for response-cap accounting,
// and the cellTruncated flag — produced in the same pass through the approved
// result-contract truncation primitive so the flag always describes the
// original value, never a post-hoc guess (T8-B). Truncation is rune-safe and
// never splits UTF-8. Invalid UTF-8 text and undeclared materialized shapes
// surface the fixed internal error through the existing failure path rather
// than being masked by byte replacement or reformatting.
func (e *MySQLQueryExecutor) toJSONSafe(v any) (any, int, bool, error) {
	safe, truncated, err := TruncateCellValue(v, e.caps.MaxCellBytes)
	if err != nil {
		return nil, 0, false, err
	}
	return safe, jsonSafeWeight(safe), truncated, nil
}

// jsonSafeWeight approximates a materialized value's serialized byte weight
// for the total response cap. It only sees the declared shapes TruncateCellValue
// already accepted; anything else is unreachable and weighs zero.
func jsonSafeWeight(v any) int {
	switch x := v.(type) {
	case nil:
		return 0
	case string:
		return len(x)
	case time.Time:
		return len(x.Format(time.RFC3339Nano))
	case int64, float64:
		return 8
	case bool:
		return 1
	default:
		return 0
	}
}

// normalizeDatabaseType returns a stable, non-empty database type label.
func normalizeDatabaseType(name string) string {
	if name == "" {
		return "UNKNOWN"
	}
	return name
}
