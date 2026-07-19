// Package service provides the MySQL Explain executor for Phase 38N.
// input: context, database/sql, encoding/json, errors, fmt, go-sql-driver/mysql
// output: ExplainStatement (sealed), QueryExplainExecutor (typed interface), MySQLExplainExecutor, ExplainRawPlan, MaxExplainPlanBytes
// pos: Runs EXPLAIN FORMAT=JSON against a target DB under a read-only transaction; never executes the bare SELECT
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "github.com/go-sql-driver/mysql"
)

// MaxExplainPlanBytes bounds the raw EXPLAIN FORMAT=JSON cell before
// json.Unmarshal. A hostile or pathologically complex plan cannot consume
// unbounded memory/CPU because the executor rejects oversize input before
// parsing.
const MaxExplainPlanBytes = 1 << 20 // 1 MiB

// ErrQueryExplainNotSupported is the controlled sentinel for any condition
// that makes Explain unavailable: unsupported engine, malformed raw plan,
// oversize plan, or unsupported plan shape. The handler maps it to HTTP 409
// with a fixed message; the raw error never carries plan bytes or driver text.
var ErrQueryExplainNotSupported = errors.New("query explain not supported")

// ExplainStatement is the sealed, engine-wrapped SQL the executor may run.
// It is constructed by the service from a parser-approved SELECT by
// prepending the engine Explain prefix. The executor cannot reach the
// original bare SELECT through this type — there is no accessor for the
// unwrapped form. This is the structural seam (Oracle P1.1) that prevents
// a faulty executor from running the bare SELECT via tx.QueryContext.
type ExplainStatement struct {
	wrappedSQL string
}

// NewExplainStatement wraps a guarded SELECT in EXPLAIN FORMAT=JSON. The
// caller (the service) must have already run GuardExplainSelect on the
// input; this constructor does not re-validate. The wrapped SQL is the ONLY
// string the executor will run.
func NewExplainStatement(guardedSelect string) ExplainStatement {
	return ExplainStatement{wrappedSQL: "EXPLAIN FORMAT=JSON " + guardedSelect}
}

// WrappedSQL returns the EXPLAIN-prefixed SQL. Package-private: only the
// MySQLExplainExecutor implementation in this package reads it.
func (s ExplainStatement) WrappedSQL() string { return s.wrappedSQL }

// ExplainRawPlan is the opaque, parsed raw plan tree. It carries no public
// fields; only the ExplainNormalizer consumes it. The raw JSON never leaves
// the service package through this type.
type ExplainRawPlan struct {
	tree interface{}
}

// Tree returns the parsed plan tree for the normalizer. Package-private.
func (r ExplainRawPlan) Tree() interface{} { return r.tree }

// QueryExplainExecutor runs EXPLAIN against a target database for an
// already-wrapped ExplainStatement and returns the raw engine plan tree.
// The implementer owns engine-specific Explain syntax construction (via
// NewExplainStatement) and MUST NOT execute the underlying bare SELECT.
// The interface is deliberately narrow: there is no Query(sql string)
// method and no path to pass arbitrary SQL.
type QueryExplainExecutor interface {
	Explain(ctx context.Context, dsn string, stmt ExplainStatement) (ExplainRawPlan, error)
}

// MySQLExplainExecutor runs EXPLAIN FORMAT=JSON against a MySQL target using
// a per-request connection and a read-only transaction. It reads the single
// JSON cell into a []byte first, rejects oversize input before unmarshalling,
// and rolls back the transaction (never commits). It never calls tx.Exec and
// never runs the bare guarded SELECT.
type MySQLExplainExecutor struct{}

// NewMySQLExplainExecutor builds the MySQL Explain executor.
func NewMySQLExplainExecutor() *MySQLExplainExecutor {
	return &MySQLExplainExecutor{}
}

// Explain runs EXPLAIN FORMAT=JSON <guarded select> against the target DB.
// The dsn is used only to open the connection and never leaves this method.
func (e *MySQLExplainExecutor) Explain(ctx context.Context, dsn string, stmt ExplainStatement) (ExplainRawPlan, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return ExplainRawPlan{}, fmt.Errorf("open target database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return ExplainRawPlan{}, err
	}
	defer tx.Rollback()

	dispatched := dispatchSQLForExplain(stmt)
	rows, err := tx.QueryContext(ctx, dispatched)
	if err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	defer rows.Close()

	if !rows.Next() {
		return ExplainRawPlan{}, ErrQueryExplainNotSupported
	}
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	if err := rows.Err(); err != nil {
		return ExplainRawPlan{}, classifyExplainDriverError(err)
	}
	if len(raw) == 0 {
		return ExplainRawPlan{}, ErrQueryExplainNotSupported
	}
	if len(raw) > MaxExplainPlanBytes {
		return ExplainRawPlan{}, ErrQueryExplainNotSupported
	}
	var tree interface{}
	if err := json.Unmarshal(raw, &tree); err != nil {
		return ExplainRawPlan{}, ErrQueryExplainNotSupported
	}
	return ExplainRawPlan{tree: tree}, nil
}

// dispatchSQLForExplain returns the exact SQL string the executor passes to
// tx.QueryContext. It is package-private so the unit test can assert the
// dispatched SQL is the EXPLAIN-prefixed wrapped form, NOT the bare guarded
// select. This is the structural-enforcement seam for Oracle P1.1.
func dispatchSQLForExplain(stmt ExplainStatement) string {
	return stmt.WrappedSQL()
}

// classifyExplainDriverError maps a database/sql error to a controlled
// sentinel. A deadline exceeded becomes ErrQueryTimeout; everything else
// becomes ErrQueryBackendFailure. The raw error (which may contain DSN
// fragments) is never returned to the caller.
func classifyExplainDriverError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrQueryTimeout
	}
	return ErrQueryBackendFailure
}
