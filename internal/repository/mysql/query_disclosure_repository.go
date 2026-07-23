// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, context, errors, fmt, internal/model
// output: NewQueryDisclosureRepository, MySQLQueryDisclosureRepository (QueryDisclosureReader, QueryDisclosureWriter)
// pos: MySQL data access for Phase 38Q governed result-disclosure policy CRUD
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// QueryDisclosureReader reads disclosure policies.
type QueryDisclosureReader interface {
	ListByTarget(ctx context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error)
	GetByScope(ctx context.Context, targetResourceID uint64, database, object, column string) (model.ResultDisclosurePolicy, error)
}

// QueryDisclosureWriter writes disclosure policies.
type QueryDisclosureWriter interface {
	Insert(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error)
	Update(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) error
	Delete(ctx context.Context, targetResourceID uint64, database, object, column string) error
}

// MySQLQueryDisclosureRepository implements QueryDisclosureReader and
// QueryDisclosureWriter against the query_result_disclosure_policies table. It
// never stores raw result values, DSNs, credentials, or actor data.
type MySQLQueryDisclosureRepository struct {
	db *sql.DB
}

// NewQueryDisclosureRepository constructs a MySQLQueryDisclosureRepository.
func NewQueryDisclosureRepository(db *sql.DB) *MySQLQueryDisclosureRepository {
	return &MySQLQueryDisclosureRepository{db: db}
}

// ListByTarget returns all disclosure policies for a target, ordered by
// database_name, object_name, column_name.
func (r *MySQLQueryDisclosureRepository) ListByTarget(ctx context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error) {
	const q = `select id, target_resource_id, database_name, object_name, column_name, mode, created_at, updated_at
	           from query_result_disclosure_policies
	           where target_resource_id = ?
	           order by database_name, object_name, column_name`
	rows, err := r.db.QueryContext(ctx, q, targetResourceID)
	if err != nil {
		return nil, fmt.Errorf("list disclosure policies: %w", err)
	}
	defer rows.Close()

	items := make([]model.ResultDisclosurePolicy, 0)
	for rows.Next() {
		var p model.ResultDisclosurePolicy
		var mode string
		if err := rows.Scan(
			&p.ID, &p.TargetResourceID, &p.DatabaseName, &p.ObjectName, &p.ColumnName,
			&mode, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan disclosure policy: %w", err)
		}
		p.Mode = model.ResultDisclosureMode(mode)
		items = append(items, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate disclosure policies: %w", err)
	}
	return items, nil
}

// GetByScope returns the disclosure policy for an exact scope
// (target_resource_id, database_name, object_name, column_name). Returns
// sql.ErrNoRows when no matching policy exists (caller treats as blocked).
func (r *MySQLQueryDisclosureRepository) GetByScope(ctx context.Context, targetResourceID uint64, database, object, column string) (model.ResultDisclosurePolicy, error) {
	const q = `select id, target_resource_id, database_name, object_name, column_name, mode, created_at, updated_at
	           from query_result_disclosure_policies
	           where target_resource_id = ? and database_name = ? and object_name = ? and column_name = ?`
	var p model.ResultDisclosurePolicy
	var mode string
	err := r.db.QueryRowContext(ctx, q, targetResourceID, database, object, column).Scan(
		&p.ID, &p.TargetResourceID, &p.DatabaseName, &p.ObjectName, &p.ColumnName,
		&mode, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.ResultDisclosurePolicy{}, sql.ErrNoRows
		}
		return model.ResultDisclosurePolicy{}, fmt.Errorf("get disclosure policy: %w", err)
	}
	p.Mode = model.ResultDisclosureMode(mode)
	return p, nil
}

// Insert creates a new disclosure policy. Returns the new row ID. Returns an
// error if a policy with the same scope already exists (UNIQUE constraint on
// target_resource_id, database_name, object_name, column_name).
func (r *MySQLQueryDisclosureRepository) Insert(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error) {
	const q = `insert into query_result_disclosure_policies
	           (target_resource_id, database_name, object_name, column_name, mode)
	           values (?, ?, ?, ?, ?)`
	res, err := r.db.ExecContext(ctx, q,
		req.TargetResourceID, req.DatabaseName, req.ObjectName, req.ColumnName, string(req.Mode),
	)
	if err != nil {
		return 0, fmt.Errorf("insert disclosure policy: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("disclosure policy last insert id: %w", err)
	}
	return uint64(id), nil
}

// Update modifies the mode of an existing disclosure policy identified by scope.
// Returns sql.ErrNoRows when no matching policy exists.
func (r *MySQLQueryDisclosureRepository) Update(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) error {
	const q = `update query_result_disclosure_policies
	           set mode = ?
	           where target_resource_id = ? and database_name = ? and object_name = ? and column_name = ?`
	res, err := r.db.ExecContext(ctx, q,
		string(req.Mode), req.TargetResourceID, req.DatabaseName, req.ObjectName, req.ColumnName,
	)
	if err != nil {
		return fmt.Errorf("update disclosure policy: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update disclosure policy rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// Delete removes a disclosure policy by scope. It is idempotent: deleting a
// scope that has no row is not an error.
func (r *MySQLQueryDisclosureRepository) Delete(ctx context.Context, targetResourceID uint64, database, object, column string) error {
	const q = `delete from query_result_disclosure_policies
	           where target_resource_id = ? and database_name = ? and object_name = ? and column_name = ?`
	if _, err := r.db.ExecContext(ctx, q, targetResourceID, database, object, column); err != nil {
		return fmt.Errorf("delete disclosure policy: %w", err)
	}
	return nil
}
