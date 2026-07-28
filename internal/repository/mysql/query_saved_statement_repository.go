// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, context, errors, fmt, strings, internal/model
// output: NewQuerySavedStatementRepository, MySQLQuerySavedStatementRepository (QuerySavedStatementReader, QuerySavedStatementWriter)
// pos: MySQL data access for Phase 38R governed saved statements CRUD with atomic audit
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/fan/controlhub/internal/model"
)

// QuerySavedStatementReader reads saved statements.
type QuerySavedStatementReader interface {
	ListVisible(ctx context.Context, query model.QuerySavedStatementListQuery) (model.QuerySavedStatementListResponse, error)
	GetByID(ctx context.Context, targetResourceID, id uint64) (model.QuerySavedStatement, error)
}

// QuerySavedStatementWriter writes saved statements with atomic audit.
type QuerySavedStatementWriter interface {
	CreateWithAudit(ctx context.Context, ownerUserID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error)
	UpdateWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest) error
	DeleteWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64) error
}

// MySQLQuerySavedStatementRepository implements QuerySavedStatementReader and
// QuerySavedStatementWriter against the query_saved_statements table.
type MySQLQuerySavedStatementRepository struct {
	db *sql.DB
}

// NewQuerySavedStatementRepository constructs a MySQLQuerySavedStatementRepository.
func NewQuerySavedStatementRepository(db *sql.DB) *MySQLQuerySavedStatementRepository {
	return &MySQLQuerySavedStatementRepository{db: db}
}

// ListVisible returns saved statements visible to the actor:
// - All shared_template statements for the target
// - Only the actor's personal statements for the target
// Ordered by updated_at DESC, id DESC. Name-only search.
func (r *MySQLQuerySavedStatementRepository) ListVisible(ctx context.Context, query model.QuerySavedStatementListQuery) (model.QuerySavedStatementListResponse, error) {
	// Build WHERE clause
	where := []string{"target_resource_id = ?"}
	args := []any{query.TargetResourceID}

	// Visibility: shared OR (personal AND owned by actor)
	where = append(where, "(scope = 'shared_template' OR (scope = 'personal' AND owner_user_id = ?))")
	args = append(args, query.OwnerUserID)

	// Name-only search
	if query.Search != "" {
		where = append(where, "name LIKE ?")
		args = append(args, "%"+escapeLike(query.Search)+"%")
	}

	whereClause := strings.Join(where, " AND ")

	// Count total
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM query_saved_statements WHERE %s", whereClause)
	var total int
	if err := r.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return model.QuerySavedStatementListResponse{}, fmt.Errorf("count saved statements: %w", err)
	}

	// Apply pagination
	page, pageSize := model.NormalizePagination(query.Page, query.PageSize)
	offset := (page - 1) * pageSize

	selectQ := fmt.Sprintf(`SELECT id, target_resource_id, owner_user_id, name, statement, scope, created_at, updated_at
		FROM query_saved_statements
		WHERE %s
		ORDER BY updated_at DESC, id DESC
		LIMIT ? OFFSET ?`, whereClause)
	args = append(args, pageSize, offset)

	rows, err := r.db.QueryContext(ctx, selectQ, args...)
	if err != nil {
		return model.QuerySavedStatementListResponse{}, fmt.Errorf("list saved statements: %w", err)
	}
	defer rows.Close()

	items := make([]model.QuerySavedStatement, 0)
	for rows.Next() {
		var s model.QuerySavedStatement
		var scope string
		if err := rows.Scan(&s.ID, &s.TargetResourceID, &s.OwnerUserID, &s.Name, &s.Statement, &scope, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return model.QuerySavedStatementListResponse{}, fmt.Errorf("scan saved statement: %w", err)
		}
		s.Scope = model.QuerySavedStatementScope(scope)
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return model.QuerySavedStatementListResponse{}, fmt.Errorf("iterate saved statements: %w", err)
	}

	return model.QuerySavedStatementListResponse{
		Items:    items,
		PageInfo: model.NewPageInfo(page, pageSize, total),
	}, nil
}

// GetByID returns a saved statement by target and ID. Returns sql.ErrNoRows
// when not found.
func (r *MySQLQuerySavedStatementRepository) GetByID(ctx context.Context, targetResourceID, id uint64) (model.QuerySavedStatement, error) {
	const q = `SELECT id, target_resource_id, owner_user_id, name, statement, scope, created_at, updated_at
		FROM query_saved_statements
		WHERE target_resource_id = ? AND id = ?`
	var s model.QuerySavedStatement
	var scope string
	err := r.db.QueryRowContext(ctx, q, targetResourceID, id).Scan(
		&s.ID, &s.TargetResourceID, &s.OwnerUserID, &s.Name, &s.Statement, &scope, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.QuerySavedStatement{}, sql.ErrNoRows
		}
		return model.QuerySavedStatement{}, fmt.Errorf("get saved statement: %w", err)
	}
	s.Scope = model.QuerySavedStatementScope(scope)
	return s, nil
}

// CreateWithAudit inserts a new saved statement and an audit event atomically.
// The audit event uses fixed strings only — never statement text, name, or owner ID.
func (r *MySQLQuerySavedStatementRepository) CreateWithAudit(ctx context.Context, ownerUserID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Insert saved statement
	const insertQ = `INSERT INTO query_saved_statements (target_resource_id, owner_user_id, name, statement, scope)
		VALUES (?, ?, ?, ?, ?)`
	res, err := tx.ExecContext(ctx, insertQ, req.TargetResourceID, ownerUserID, req.Name, req.Statement, string(req.Scope))
	if err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("insert saved statement: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("saved statement last insert id: %w", err)
	}

	// Insert audit event (fixed strings only, no statement/name/owner)
	const auditQ = `INSERT INTO audit_events (target_resource_id, event_type, result)
		VALUES (?, 'query.saved_statement.created', 'success')`
	if _, err := tx.ExecContext(ctx, auditQ, req.TargetResourceID); err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("insert audit event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("commit transaction: %w", err)
	}

	return model.QuerySavedStatement{
		ID:               uint64(id),
		TargetResourceID: req.TargetResourceID,
		OwnerUserID:      ownerUserID,
		Name:             req.Name,
		Statement:        req.Statement,
		Scope:            req.Scope,
	}, nil
}

// UpdateWithAudit updates a saved statement and inserts an audit event atomically.
// Returns sql.ErrNoRows when the statement doesn't exist or isn't owned by the actor.
func (r *MySQLQuerySavedStatementRepository) UpdateWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update statement (only if owned by actor)
	const updateQ = `UPDATE query_saved_statements
		SET name = ?, statement = ?, updated_at = CURRENT_TIMESTAMP(6)
		WHERE target_resource_id = ? AND id = ? AND owner_user_id = ?`
	res, err := tx.ExecContext(ctx, updateQ, req.Name, req.Statement, targetResourceID, statementID, actorUserID)
	if err != nil {
		return fmt.Errorf("update saved statement: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("update saved statement rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}

	// Insert audit event
	const auditQ = `INSERT INTO audit_events (target_resource_id, event_type, result)
		VALUES (?, 'query.saved_statement.updated', 'success')`
	if _, err := tx.ExecContext(ctx, auditQ, targetResourceID); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return tx.Commit()
}

// DeleteWithAudit deletes a saved statement and inserts an audit event atomically.
// Returns sql.ErrNoRows when the statement doesn't exist or isn't owned by the actor.
func (r *MySQLQuerySavedStatementRepository) DeleteWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete statement (only if owned by actor)
	const deleteQ = `DELETE FROM query_saved_statements
		WHERE target_resource_id = ? AND id = ? AND owner_user_id = ?`
	res, err := tx.ExecContext(ctx, deleteQ, targetResourceID, statementID, actorUserID)
	if err != nil {
		return fmt.Errorf("delete saved statement: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("delete saved statement rows affected: %w", err)
	}
	if n == 0 {
		return sql.ErrNoRows
	}

	// Insert audit event
	const auditQ = `INSERT INTO audit_events (target_resource_id, event_type, result)
		VALUES (?, 'query.saved_statement.deleted', 'success')`
	if _, err := tx.ExecContext(ctx, auditQ, targetResourceID); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return tx.Commit()
}
