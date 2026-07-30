// Package service provides the MySQL/TiDB query executor for the read-only sandbox.
// input: context, database/sql, fmt, strings, time, go-sql-driver/mysql, internal/model
// output: MySQLQueryExecutor, NewMySQLQueryExecutor, QueryExecutorCaps, newScanPointer, normalizeScanned (implements QueryDatabaseExecutor)
// pos: Runs a guarded SELECT against a target DB under a read-only transaction with column/cell/payload caps; paginated windows reject on payload-cap overflow, non-paged results truncate; preserves SQL NULL as JSON null
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
	caps QueryExecutorCaps
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
	return &MySQLQueryExecutor{caps: caps}
}

// Query executes the guarded SELECT and returns the bounded result. The DSN is
// used only to open the connection and never leaves this method.
func (e *MySQLQueryExecutor) Query(ctx context.Context, dsn string, guarded GuardedQuery) (QueryDatabaseResult, error) {
	db, err := sql.Open("mysql", dsn)
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

	rows, err := tx.QueryContext(ctx, guarded.ExecutableSQL)
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
	db, err := sql.Open("mysql", dsn)
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
		rowBytes := 0
		for i, p := range ptrs {
			safe, n := e.toJSONSafe(normalizeScanned(p))
			row[i] = safe
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
		result.RowCount++
	}
	if err := rows.Err(); err != nil {
		return QueryDatabaseResult{}, err
	}
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

// toJSONSafe converts a scanned database value into a JSON-safe value and returns
// its approximate serialized byte weight for response-cap accounting. []byte and
// over-long strings are truncated to the cell cap rather than failing the query.
func (e *MySQLQueryExecutor) toJSONSafe(v any) (any, int) {
	switch x := v.(type) {
	case nil:
		return nil, 0
	case []byte:
		if len(x) > e.caps.MaxCellBytes {
			x = x[:e.caps.MaxCellBytes]
		}
		s := string(x)
		return s, len(s)
	case string:
		if len(x) > e.caps.MaxCellBytes {
			x = x[:e.caps.MaxCellBytes]
		}
		return x, len(x)
	case time.Time:
		return x, len(x.Format(time.RFC3339Nano))
	case int64:
		return x, 8
	case float64:
		return x, 8
	case bool:
		return x, 1
	default:
		s := fmt.Sprintf("%v", v)
		return s, len(s)
	}
}

// normalizeDatabaseType returns a stable, non-empty database type label.
func normalizeDatabaseType(name string) string {
	if name == "" {
		return "UNKNOWN"
	}
	return name
}
