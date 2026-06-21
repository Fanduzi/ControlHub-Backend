// Package service provides the MySQL/TiDB query executor for the read-only sandbox.
// input: context, database/sql, fmt, time, vitess-free, internal/model
// output: MySQLQueryExecutor, NewMySQLQueryExecutor, QueryExecutorCaps (implements QueryDatabaseExecutor)
// pos: Runs a guarded SELECT against a target DB under a read-only transaction with column/cell/payload caps
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
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

	result := QueryDatabaseResult{Columns: columns}
	limit := guarded.LimitApplied
	responseBytes := 0

	// Allocate typed scan buffers from each column's ScanType so values come back
	// in their native Go type (int64 for BIGINT, float64 for DOUBLE, time.Time for
	// DATETIME, []byte for text/blob) rather than all-as-[]byte. This preserves
	// JSON number fidelity. Numeric NULLs land as their zero value — a documented
	// Phase 37 limitation; text NULLs land as nil []byte -> JSON null.
	scanTypes := make([]reflect.Type, len(colTypes))
	for i, ct := range colTypes {
		if st := ct.ScanType(); st != nil {
			scanTypes[i] = st
		} else {
			scanTypes[i] = reflect.TypeOf([]byte(nil))
		}
	}

	for rows.Next() {
		// Scan one row past the limit so truncation can be detected.
		if result.RowCount >= limit {
			result.Truncated = true
			break
		}
		ptrs := make([]any, len(colTypes))
		for i := range ptrs {
			ptrs[i] = reflect.New(scanTypes[i]).Interface()
		}
		if err := rows.Scan(ptrs...); err != nil {
			return QueryDatabaseResult{}, err
		}
		row := make([]any, len(colTypes))
		rowBytes := 0
		for i, p := range ptrs {
			v := reflect.ValueOf(p).Elem().Interface()
			safe, n := e.toJSONSafe(v)
			row[i] = safe
			rowBytes += n
		}
		responseBytes += rowBytes
		if responseBytes > e.caps.MaxResponseBytes {
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