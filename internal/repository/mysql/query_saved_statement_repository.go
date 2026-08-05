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
	CreateWithAudit(ctx context.Context, ownerUserID, targetResourceID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error)
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
		s.Parameters = make([]model.QuerySavedStatementParameterDefinition, 0)
		items = append(items, s)
	}
	if err := rows.Err(); err != nil {
		return model.QuerySavedStatementListResponse{}, fmt.Errorf("iterate saved statements: %w", err)
	}

	items, err = r.loadParameterDefinitions(ctx, items)
	if err != nil {
		return model.QuerySavedStatementListResponse{}, err
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
	loaded, err := r.loadParameterDefinitions(ctx, []model.QuerySavedStatement{s})
	if err != nil {
		return model.QuerySavedStatement{}, err
	}
	s = loaded[0]
	return s, nil
}

// CreateWithAudit inserts a new saved statement and an audit event atomically.
// The audit event uses fixed strings only — never statement text, name, or owner ID.
func (r *MySQLQuerySavedStatementRepository) CreateWithAudit(ctx context.Context, ownerUserID, targetResourceID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	const insertQ = `INSERT INTO query_saved_statements (target_resource_id, owner_user_id, name, statement, scope)
		VALUES (?, ?, ?, ?, ?)`
	res, err := tx.ExecContext(ctx, insertQ, targetResourceID, ownerUserID, req.Name, req.Statement, string(req.Scope))
	if err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("insert saved statement: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("saved statement last insert id: %w", err)
	}
	if err := insertParameterDefinitions(ctx, tx, uint64(id), req.Parameters); err != nil {
		return model.QuerySavedStatement{}, err
	}

	const auditQ = `INSERT INTO audit_events (actor_user_id, target_resource_id, event_type, result)
		VALUES (?, ?, 'query.saved_statement.created', 'success')`
	if _, err := tx.ExecContext(ctx, auditQ, ownerUserID, targetResourceID); err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("insert audit event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("commit transaction: %w", err)
	}

	return model.QuerySavedStatement{
		ID:               uint64(id),
		TargetResourceID: targetResourceID,
		OwnerUserID:      ownerUserID,
		Name:             req.Name,
		Statement:        req.Statement,
		Scope:            req.Scope,
		Parameters:       cloneParameterDefinitions(req.Parameters),
	}, nil
}

// UpdateWithAudit updates a saved statement and inserts an audit event atomically.
// When isAdmin is true, the update applies to any statement (for shared templates).
// When isAdmin is false, the update only applies to statements owned by actorUserID.
// Returns sql.ErrNoRows when the statement doesn't exist or isn't accessible.
func (r *MySQLQuerySavedStatementRepository) UpdateWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest, isAdmin bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Update statement - admins can update any statement, non-owners only their own
	var updateQ string
	var args []any
	if isAdmin {
		updateQ = `UPDATE query_saved_statements
			SET name = ?, statement = ?, updated_at = CURRENT_TIMESTAMP(6)
			WHERE target_resource_id = ? AND id = ?
			  AND (scope = 'shared_template' OR owner_user_id = ?)`
		args = []any{req.Name, req.Statement, targetResourceID, statementID, actorUserID}
	} else {
		updateQ = `UPDATE query_saved_statements
			SET name = ?, statement = ?, updated_at = CURRENT_TIMESTAMP(6)
			WHERE target_resource_id = ? AND id = ? AND owner_user_id = ?`
		args = []any{req.Name, req.Statement, targetResourceID, statementID, actorUserID}
	}
	res, err := tx.ExecContext(ctx, updateQ, args...)
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM query_saved_statement_parameters WHERE statement_id = ?`, statementID); err != nil {
		return fmt.Errorf("delete saved statement parameters: %w", err)
	}
	if err := insertParameterDefinitions(ctx, tx, statementID, req.Parameters); err != nil {
		return err
	}

	// Insert audit event
	const auditQ = `INSERT INTO audit_events (actor_user_id, target_resource_id, event_type, result)
		VALUES (?, ?, 'query.saved_statement.updated', 'success')`
	if _, err := tx.ExecContext(ctx, auditQ, actorUserID, targetResourceID); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return tx.Commit()
}

// DeleteWithAudit deletes a saved statement and inserts an audit event atomically.
// When isAdmin is true, the delete applies to any statement (for shared templates).
// When isAdmin is false, the delete only applies to statements owned by actorUserID.
// Returns sql.ErrNoRows when the statement doesn't exist or isn't accessible.
func (r *MySQLQuerySavedStatementRepository) DeleteWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64, isAdmin bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete statement - admins can delete any statement, non-owners only their own
	var deleteQ string
	var args []any
	if isAdmin {
		deleteQ = `DELETE FROM query_saved_statements
			WHERE target_resource_id = ? AND id = ?
			  AND (scope = 'shared_template' OR owner_user_id = ?)`
		args = []any{targetResourceID, statementID, actorUserID}
	} else {
		deleteQ = `DELETE FROM query_saved_statements
			WHERE target_resource_id = ? AND id = ? AND owner_user_id = ?`
		args = []any{targetResourceID, statementID, actorUserID}
	}
	res, err := tx.ExecContext(ctx, deleteQ, args...)
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
	if _, err := tx.ExecContext(ctx, `DELETE FROM query_saved_statement_parameters WHERE statement_id = ?`, statementID); err != nil {
		return fmt.Errorf("delete saved statement parameters: %w", err)
	}

	// Insert audit event
	const auditQ = `INSERT INTO audit_events (actor_user_id, target_resource_id, event_type, result)
		VALUES (?, ?, 'query.saved_statement.deleted', 'success')`
	if _, err := tx.ExecContext(ctx, auditQ, actorUserID, targetResourceID); err != nil {
		return fmt.Errorf("insert audit event: %w", err)
	}

	return tx.Commit()
}

func (r *MySQLQuerySavedStatementRepository) loadParameterDefinitions(ctx context.Context, statements []model.QuerySavedStatement) ([]model.QuerySavedStatement, error) {
	if len(statements) == 0 {
		return statements, nil
	}
	loadedStatements := make([]model.QuerySavedStatement, len(statements))
	copy(loadedStatements, statements)
	for index := range loadedStatements {
		loadedStatements[index].Parameters = cloneParameterDefinitions(statements[index].Parameters)
	}
	statements = loadedStatements
	placeholders := make([]string, len(statements))
	args := make([]any, len(statements))
	byID := make(map[uint64]int, len(statements))
	for index, statement := range statements {
		placeholders[index] = "?"
		args[index] = statement.ID
		byID[statement.ID] = index
		if statements[index].Parameters == nil {
			statements[index].Parameters = make([]model.QuerySavedStatementParameterDefinition, 0)
		}
	}

	query := fmt.Sprintf(`SELECT statement_id, name, type, ordinal
		FROM query_saved_statement_parameters
		WHERE statement_id IN (%s)
		ORDER BY statement_id, ordinal`, strings.Join(placeholders, ", "))
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list saved statement parameters: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var statementID uint64
		var definition model.QuerySavedStatementParameterDefinition
		var ordinal int
		if err := rows.Scan(&statementID, &definition.Name, &definition.Type, &ordinal); err != nil {
			return nil, fmt.Errorf("scan saved statement parameter: %w", err)
		}
		index, ok := byID[statementID]
		if !ok {
			return nil, fmt.Errorf("saved statement parameter references unknown statement")
		}
		if ordinal != len(statements[index].Parameters) {
			return nil, fmt.Errorf("saved statement parameter order is invalid")
		}
		statements[index].Parameters = append(statements[index].Parameters, definition)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate saved statement parameters: %w", err)
	}
	return statements, nil
}

func insertParameterDefinitions(ctx context.Context, tx *sql.Tx, statementID uint64, parameters []model.QuerySavedStatementParameterDefinition) error {
	const query = `INSERT INTO query_saved_statement_parameters (statement_id, name, type, ordinal)
		VALUES (?, ?, ?, ?)`
	for ordinal, parameter := range parameters {
		if _, err := tx.ExecContext(ctx, query, statementID, parameter.Name, string(parameter.Type), ordinal); err != nil {
			return fmt.Errorf("insert saved statement parameter: %w", err)
		}
	}
	return nil
}

func cloneParameterDefinitions(parameters []model.QuerySavedStatementParameterDefinition) []model.QuerySavedStatementParameterDefinition {
	if len(parameters) == 0 {
		return make([]model.QuerySavedStatementParameterDefinition, 0)
	}
	return append([]model.QuerySavedStatementParameterDefinition(nil), parameters...)
}
