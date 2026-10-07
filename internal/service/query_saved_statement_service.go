// Package service manages governed saved statements with authorization.
// input: context, database/sql, errors, fmt, math, internal/model, internal/pgsql
// output: QuerySavedStatementService, NewQuerySavedStatementService, WithPGContext, RestoreContext, QuerySavedStatementReader, QuerySavedStatementWriter
// pos: Phase 38R saved-statement service — authorization, dialect-aware template validation, (target,database,schema) context persistence and G12 restore revalidation
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
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
	CreateWithAudit(ctx context.Context, ownerUserID, targetResourceID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error)
	UpdateWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest, isAdmin bool) error
	DeleteWithAudit(ctx context.Context, actorUserID, targetResourceID, statementID uint64, isAdmin bool) error
}

// SavedStatementGuard validates SQL statements for saving.
type SavedStatementGuard interface {
	GuardSavedStatement(statement string) (string, error)
}

// QuerySavedStatementService manages saved statements with authorization.
// credentials and access are required for PostgreSQL context validation and
// restore checks; they are wired by WithPGContext so the constructor and its
// existing callers stay unchanged. pgCatalog is nil in production, selecting
// the live catalog; tests substitute a fake.
type QuerySavedStatementService struct {
	reader      QuerySavedStatementReader
	writer      QuerySavedStatementWriter
	targets     QueryTargetRepository
	guard       SavedStatementGuard
	compiler    *TemplateStatementCompiler
	credentials QueryCredentialReader
	access      *TargetAccessResolver
	pgCatalog   pgSchemaCatalog
}

// NewQuerySavedStatementService constructs a QuerySavedStatementService.
func NewQuerySavedStatementService(
	reader QuerySavedStatementReader,
	writer QuerySavedStatementWriter,
	targets QueryTargetRepository,
	guard SavedStatementGuard,
) *QuerySavedStatementService {
	return &QuerySavedStatementService{
		reader:   reader,
		writer:   writer,
		targets:  targets,
		guard:    guard,
		compiler: NewTemplateStatementCompiler(),
	}
}

// WithPGContext wires the connection-metadata reader and the shared governed
// access resolver required by PostgreSQL statement context validation and
// restore. It is a setter so the constructor and its existing tests stay
// unchanged; the production wiring must call it.
func (s *QuerySavedStatementService) WithPGContext(credentials QueryCredentialReader, access *TargetAccessResolver) *QuerySavedStatementService {
	s.credentials = credentials
	s.access = access
	return s
}

// pgRestoreCatalog returns the configured catalog or the live implementation.
func (s *QuerySavedStatementService) pgRestoreCatalog() pgSchemaCatalog {
	if s.pgCatalog != nil {
		return s.pgCatalog
	}
	return livePGSchemaCatalog{}
}

// List returns saved statements visible to the actor for a target.
// Personal statements are only visible to the owner. Shared templates
// are visible to all authenticated actors for the target. database is the
// composite connection scope (G1): empty reads the legacy empty-database identity that
// every MySQL/TiDB statement carries; a non-empty value selects exactly one
// (target, database) connection's statements. The filter never consults the
// connection's runtime state — saved statements stay readable when a
// connection is disabled.
func (s *QuerySavedStatementService) List(ctx context.Context, actor AuthenticatedUser, targetResourceID uint64, q, database string, page, pageSize int) (model.QuerySavedStatementListResponse, error) {
	if _, err := s.lookupTarget(ctx, targetResourceID); err != nil {
		return model.QuerySavedStatementListResponse{}, err
	}

	query := model.QuerySavedStatementListQuery{
		TargetResourceID: targetResourceID,
		OwnerUserID:      actor.ID,
		Database:         database,
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
func (s *QuerySavedStatementService) Create(ctx context.Context, actor AuthenticatedUser, targetResourceID uint64, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error) {
	if err := req.Validate(); err != nil {
		return model.QuerySavedStatement{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	if req.Scope == model.QuerySavedStatementSharedTemplate && !isAdmin(actor) {
		return model.QuerySavedStatement{}, ErrQueryForbidden
	}

	target, err := s.lookupTarget(ctx, targetResourceID)
	if err != nil {
		return model.QuerySavedStatement{}, err
	}

	if err := s.validateStatementContext(ctx, target, req.Database, req.Schema); err != nil {
		return model.QuerySavedStatement{}, err
	}

	if err := s.validateStatement(target.ConnectionContext.Engine, req.Statement, req.Parameters); err != nil {
		// Keep the inner error chain visible: PostgreSQL guard/pagination
		// RejectErrors carry the precise code callers map from.
		return model.QuerySavedStatement{}, fmt.Errorf("%w: %w", ErrQueryValidationFailed, err)
	}

	return s.writer.CreateWithAudit(ctx, actor.ID, targetResourceID, req)
}

// Update updates a saved statement. Scope is immutable.
// Personal statements: owner only. Shared templates: admin only.
func (s *QuerySavedStatementService) Update(ctx context.Context, actor AuthenticatedUser, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest) error {
	if err := req.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	target, err := s.lookupTarget(ctx, targetResourceID)
	if err != nil {
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

	if err := s.validateStatementContext(ctx, target, req.Database, req.Schema); err != nil {
		return err
	}

	// Validate SQL statement.
	if err := s.validateStatement(target.ConnectionContext.Engine, req.Statement, req.Parameters); err != nil {
		return fmt.Errorf("%w: %w", ErrQueryValidationFailed, err)
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
	if _, err := s.lookupTarget(ctx, targetResourceID); err != nil {
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

// RestoreContext re-reads an authorized saved statement and revalidates its
// persisted (target, database, schema) context against current state — the
// G12 restore check. For PostgreSQL targets it follows the G1 connection
// order through ResolveMetadata (connection row exists → enabled → policy →
// credential ref → DSN binding, with the DSN database forced equal to the
// persisted one) and then probes the pinned schema for existence and USAGE on
// the live server. Every failure is a controlled rejection; nothing falls
// back to public, another database, or a same-named object elsewhere.
// MySQL/TiDB statements keep legacy semantics: the single engine-default
// connection carries no pin, so a found target and an authorized read are
// the whole check.
func (s *QuerySavedStatementService) RestoreContext(ctx context.Context, actor AuthenticatedUser, targetResourceID, statementID uint64) (model.QuerySavedStatement, error) {
	stmt, err := s.reader.GetByID(ctx, targetResourceID, statementID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.QuerySavedStatement{}, ErrQuerySavedStatementNotFound
		}
		return model.QuerySavedStatement{}, fmt.Errorf("get saved statement: %w", err)
	}
	// Read authorization mirrors List visibility: personal statements are
	// owner-only, shared templates visible to every authenticated actor.
	if stmt.Scope == model.QuerySavedStatementPersonal && stmt.OwnerUserID != actor.ID {
		return model.QuerySavedStatement{}, ErrQuerySavedStatementNotFound
	}

	target, err := s.lookupTarget(ctx, targetResourceID)
	if err != nil {
		return model.QuerySavedStatement{}, err
	}
	if !isPGEngine(target.ConnectionContext.Engine) {
		return stmt, nil
	}
	if s.access == nil {
		return model.QuerySavedStatement{}, fmt.Errorf("%w: saved statement context validation is not wired", ErrQueryBackendFailure)
	}
	bound, err := s.access.ResolveMetadata(ctx, actor.ID, targetResourceID, stmt.DatabaseName)
	if err != nil {
		var accessErr *TargetAccessError
		if errors.As(err, &accessErr) {
			return model.QuerySavedStatement{}, ErrQueryNotAllowed
		}
		return model.QuerySavedStatement{}, err
	}
	if err := s.pgRestoreCatalog().ProbeDefinition(ctx, bound.pgConfig, bound.Credential.DatabaseName, stmt.SchemaName, bound.Credential.DefaultSchema); err != nil {
		return model.QuerySavedStatement{}, err
	}
	return stmt, nil
}

// lookupTarget fetches one query target or reports ErrQueryTargetNotFound.
func (s *QuerySavedStatementService) lookupTarget(ctx context.Context, targetResourceID uint64) (model.QueryTarget, error) {
	targets, _, err := s.targets.ListQueryTargets(ctx, model.QueryTargetListQuery{TargetID: targetResourceID})
	if err != nil {
		return model.QueryTarget{}, ErrQueryTargetNotFound
	}
	for _, t := range targets {
		if t.ResourceID == targetResourceID {
			return t, nil
		}
	}
	return model.QueryTarget{}, ErrQueryTargetNotFound
}

// validateStatementContext checks the (database, schema) context a statement
// will be persisted against. PostgreSQL requires a database segment that
// names an existing composite connection row; a schema may be empty and then
// resolves to that connection's default_schema at restore time. Legacy engines
// accept only the empty context — the single engine-default connection.
func (s *QuerySavedStatementService) validateStatementContext(ctx context.Context, target model.QueryTarget, database, schema string) error {
	if !isPGEngine(target.ConnectionContext.Engine) {
		if database != "" || schema != "" {
			return fmt.Errorf("%w: database/schema context applies only to postgresql targets", ErrQueryValidationFailed)
		}
		return nil
	}
	if err := model.ValidateConnectionName("database", database); err != nil {
		return fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if s.credentials == nil {
		return fmt.Errorf("%w: saved statement context validation is not wired", ErrQueryBackendFailure)
	}
	if _, err := s.credentials.GetCredential(ctx, target.ResourceID, database); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSchemaConnectionNotFound
		}
		return fmt.Errorf("check query connection: %w", err)
	}
	return nil
}

func (s *QuerySavedStatementService) validateStatement(engine, statement string, parameters []model.QuerySavedStatementParameterDefinition) error {
	definitions := make([]TemplateParameterDefinition, len(parameters))
	for index, parameter := range parameters {
		definitions[index] = TemplateParameterDefinition{
			Name: parameter.Name,
			Type: TemplateParameterType(parameter.Type),
		}
	}
	if isPGEngine(engine) {
		return s.validateStatementPG(statement, definitions)
	}
	compiled, err := s.compiler.validateDeclarations(statement, definitions)
	if err != nil {
		return err
	}
	_, err = s.guard.GuardSavedStatement(compiled.Statement)
	return err
}

// validateStatementPG validates a PostgreSQL template under the same contract
// execution applies: declaration checking produces the compiled $k statement,
// which then must pass GuardPG and the real G6 pagination gate — so a
// LIMIT :n template that compiles fine is still rejected here as
// unsupported_limit_offset_form, never stored to bypass pagination later.
func (s *QuerySavedStatementService) validateStatementPG(statement string, definitions []TemplateParameterDefinition) error {
	compiled, err := s.compiler.validatePGDeclarations(statement, definitions)
	if err != nil {
		return err
	}
	guarded, err := pgsql.GuardPG(compiled.Statement)
	if err != nil {
		return err
	}
	_, err = pgsql.PaginatePG(guarded.Tree, pgsql.PageOptions{Page: 1, PageSize: model.AllowedPageSizes[0], MaxRows: math.MaxInt32})
	return err
}
