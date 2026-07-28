// Package service manages governed saved statements with authorization.
// input: context, errors, fmt, internal/model
// output: QuerySavedStatementService, NewQuerySavedStatementService, QuerySavedStatementReader, QuerySavedStatementWriter
// pos: Phase 38R saved-statement service — authorization, guard validation, metadata-only target checks
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// Sentinel errors for the saved statement service.
var (
	ErrQuerySavedStatementNotFound = errors.New("saved statement not found")
	ErrQueryForbidden              = errors.New("forbidden")
)

// QuerySavedStatementReader reads saved statements.
type QuerySavedStatementReader interface {
	ListVisible(ctx context.Context, query model.QuerySavedStatementListQuery) (model.QuerySavedStatementListResponse, error)
	GetByID(ctx context.Context, targetResourceID, id uint64) (model.QuerySavedStatement, error)
}

// QuerySavedStatementWriter writes saved statements with atomic audit.
type QuerySavedStatementWriter interface {
	CreateWithAudit(ctx context.Context, ownerUserID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error)
	UpdateWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest, isAdmin bool) error
	DeleteWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64, isAdmin bool) error
}

// SavedStatementGuard validates SQL statements for saving.
type SavedStatementGuard interface {
	GuardSavedStatement(statement string) (string, error)
}

// QuerySavedStatementService manages saved statements with authorization.
type QuerySavedStatementService struct {
	reader  QuerySavedStatementReader
	writer  QuerySavedStatementWriter
	targets QueryTargetRepository
	guard   SavedStatementGuard
}

// NewQuerySavedStatementService constructs a QuerySavedStatementService.
func NewQuerySavedStatementService(
	reader QuerySavedStatementReader,
	writer QuerySavedStatementWriter,
	targets QueryTargetRepository,
	guard SavedStatementGuard,
) *QuerySavedStatementService {
	return &QuerySavedStatementService{
		reader:  reader,
		writer:  writer,
		targets: targets,
		guard:   guard,
	}
}

// List returns saved statements visible to the actor for a target.
// Personal statements are only visible to the owner. Shared templates
// are visible to all authenticated actors for the target.
func (s *QuerySavedStatementService) List(ctx context.Context, actor AuthenticatedUser, targetResourceID uint64, q string, page, pageSize int) (model.QuerySavedStatementListResponse, error) {
	if err := s.validateTargetExists(ctx, targetResourceID); err != nil {
		return model.QuerySavedStatementListResponse{}, err
	}

	query := model.QuerySavedStatementListQuery{
		TargetResourceID: targetResourceID,
		OwnerUserID:      actor.ID,
		Page:             page,
		PageSize:         pageSize,
		Search:           q,
	}

	resp, err := s.reader.ListVisible(ctx, query)
	if err != nil {
		return model.QuerySavedStatementListResponse{}, fmt.Errorf("list saved statements: %w", err)
	}

	resp.CanManageSharedTemplates = isAdmin(actor)
	return resp, nil
}

// Create creates a new saved statement. Personal statements can be created
// by any authenticated actor. Shared templates can only be created by admins.
func (s *QuerySavedStatementService) Create(ctx context.Context, actor AuthenticatedUser, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error) {
	if err := req.Validate(); err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	// Admin check for shared templates.
	if req.Scope == model.QuerySavedStatementSharedTemplate && !isAdmin(actor) {
		return model.QuerySavedStatement{}, ErrQueryForbidden
	}

	if err := s.validateTargetExists(ctx, req.TargetResourceID); err != nil {
		return model.QuerySavedStatement{}, err
	}

	// Validate SQL statement.
	if _, err := s.guard.GuardSavedStatement(req.Statement); err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	return s.writer.CreateWithAudit(ctx, actor.ID, req)
}

// Update updates a saved statement. Scope is immutable.
// Personal statements: owner only. Shared templates: admin only.
func (s *QuerySavedStatementService) Update(ctx context.Context, actor AuthenticatedUser, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest) error {
	if err := req.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	if err := s.validateTargetExists(ctx, targetResourceID); err != nil {
		return err
	}

	// Fetch statement to check scope for authorization.
	stmt, err := s.reader.GetByID(ctx, targetResourceID, statementID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQuerySavedStatementNotFound
		}
		return fmt.Errorf("get saved statement: %w", err)
	}

	// Authorization: personal = owner only, shared_template = admin only.
	if stmt.Scope == model.QuerySavedStatementPersonal && stmt.OwnerUserID != actor.ID {
		return ErrQuerySavedStatementNotFound
	}
	if stmt.Scope == model.QuerySavedStatementSharedTemplate && !isAdmin(actor) {
		return ErrQueryForbidden
	}

	// Validate SQL statement.
	if _, err := s.guard.GuardSavedStatement(req.Statement); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	isAdminUser := isAdmin(actor)
	err = s.writer.UpdateWithAudit(ctx, actor.ID, targetResourceID, statementID, req, isAdminUser)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQuerySavedStatementNotFound
		}
		return fmt.Errorf("update saved statement: %w", err)
	}
	return nil
}

// Delete deletes a saved statement.
// Personal statements: owner only. Shared templates: admin only.
func (s *QuerySavedStatementService) Delete(ctx context.Context, actor AuthenticatedUser, targetResourceID, statementID uint64) error {
	if err := s.validateTargetExists(ctx, targetResourceID); err != nil {
		return err
	}

	// Fetch statement to check scope for authorization.
	stmt, err := s.reader.GetByID(ctx, targetResourceID, statementID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQuerySavedStatementNotFound
		}
		return fmt.Errorf("get saved statement: %w", err)
	}

	// Authorization: personal = owner only, shared_template = admin only.
	if stmt.Scope == model.QuerySavedStatementPersonal && stmt.OwnerUserID != actor.ID {
		return ErrQuerySavedStatementNotFound
	}
	if stmt.Scope == model.QuerySavedStatementSharedTemplate && !isAdmin(actor) {
		return ErrQueryForbidden
	}

	isAdminUser := isAdmin(actor)
	err = s.writer.DeleteWithAudit(ctx, actor.ID, targetResourceID, statementID, isAdminUser)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrQuerySavedStatementNotFound
		}
		return fmt.Errorf("delete saved statement: %w", err)
	}
	return nil
}

// validateTargetExists checks that the target exists without resolving credentials.
func (s *QuerySavedStatementService) validateTargetExists(ctx context.Context, targetResourceID uint64) error {
	targets, _, err := s.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{TargetID: targetResourceID})
	if err != nil {
		return ErrQueryTargetNotFound
	}
	for _, t := range targets {
		if t.ResourceID == targetResourceID {
			return nil
		}
	}
	return ErrQueryTargetNotFound
}
