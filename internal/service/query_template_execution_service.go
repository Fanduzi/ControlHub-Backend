// Package service provides the governed template-execution adapter.
// input: bytes, context, database/sql, encoding/json, errors, fmt, time, internal/model
// output: TemplateValueValidationError, QueryExecutionService.WithTemplateExecution, QueryExecutionService.ExecuteSavedStatement
// pos: Fresh-query-actor execution of saved statements — re-reads the latest authorized statement, validates typed values, compiles server-side, then runs the existing access/guard/disclosure/executor/history/audit chain per page
package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
)

// TemplateValueValidationError carries controlled per-parameter field codes
// ("missing", "unknown", "invalid", "oversized"). Keys are author-declared
// parameter names only; supplied values are never included.
type TemplateValueValidationError struct {
	Fields map[string]string
}

func (e *TemplateValueValidationError) Error() string {
	return "template parameter validation failed"
}

// WithTemplateExecution wires the saved-statement reader and server-owned
// compiler required by ExecuteSavedStatement. It is a setter so the execution
// service constructor and its existing tests stay unchanged.
func (s *QueryExecutionService) WithTemplateExecution(statements QuerySavedStatementReader, compiler *TemplateStatementCompiler) *QueryExecutionService {
	s.statements = statements
	s.compiler = compiler
	return s
}

// ExecuteSavedStatement runs a saved statement (static or parameterized
// template) through the existing governed chain. It re-reads the latest
// authorized statement for every execution/page, validates the submitted
// typed values against the stored declarations, compiles placeholders into
// positional driver bindings, and then runs access, guard, disclosure,
// executor, history, and audit unchanged. Personal statements are visible
// only to their owner — never to other users or admins. Parameter values
// never reach history, audit, errors, or logs.
func (s *QueryExecutionService) ExecuteSavedStatement(ctx context.Context, actorUserID, targetID, statementID uint64, req model.QuerySavedStatementExecuteRequest) (model.QueryExecuteResponse, error) {
	start := s.clock.Now()

	if s.statements == nil || s.compiler == nil {
		return model.QueryExecuteResponse{}, fmt.Errorf("template execution is not configured")
	}

	access, err := s.access.Resolve(ctx, actorUserID, targetID)
	if err != nil {
		if errors.Is(err, ErrQueryTargetNotFound) {
			return model.QueryExecuteResponse{}, err
		}
		var accessErr *TargetAccessError
		if errors.As(err, &accessErr) {
			return s.reject(ctx, access.Target, actorUserID, nil, "query_not_allowed", accessErr.Error(),
				fmt.Errorf("%w: %s", ErrQueryNotAllowed, accessErr.Error()), start)
		}
		return model.QueryExecuteResponse{}, err
	}
	target := access.Target

	statement, err := s.statements.GetByID(ctx, targetID, statementID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return model.QueryExecuteResponse{}, ErrQuerySavedStatementNotFound
		}
		return model.QueryExecuteResponse{}, fmt.Errorf("get saved statement: %w", err)
	}
	if statement.Scope == model.QuerySavedStatementPersonal && statement.OwnerUserID != actorUserID {
		return model.QueryExecuteResponse{}, ErrQuerySavedStatementNotFound
	}

	if err := req.Validate(); err != nil {
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}

	definitions := make([]TemplateParameterDefinition, len(statement.Parameters))
	for index, parameter := range statement.Parameters {
		definitions[index] = TemplateParameterDefinition{Name: parameter.Name, Type: TemplateParameterType(parameter.Type)}
	}
	values, fields := validateTemplateValues(definitions, req.Values)
	if len(fields) > 0 {
		valueErr := &TemplateValueValidationError{Fields: fields}
		// Record the rejected attempt with fixed metadata (no values) so the
		// template route honors the every-attempt-recorded guarantee and stays
		// consistent with access, guard, and disclosure rejections.
		if _, perr := s.persistAttempt(ctx, target, actorUserID, nil, model.QueryExecutionRejected, 0, "validation_failed", valueErr.Error(), start); perr != nil {
			return model.QueryExecuteResponse{}, errPersistAttempt
		}
		return model.QueryExecuteResponse{}, valueErr
	}

	maxRows := req.MaxRows
	if isProductionEnvironment(target.ConnectionContext.Environment) && (maxRows == 0 || maxRows > productionHardMaxRows) {
		maxRows = productionHardMaxRows
	}

	input := TemplateStatementInput{Statement: statement.Statement, Definitions: definitions, Values: values}
	var guardedTemplate GuardedTemplateStatement
	if req.Pagination != nil {
		guardedTemplate, err = s.compiler.CompileAndGuardPaginated(s.guard, input, req.Pagination.Page, req.Pagination.PageSize, maxRows)
	} else {
		guardedTemplate, err = s.compiler.CompileAndGuard(s.guard, input, maxRows)
	}
	if err != nil {
		return s.reject(ctx, target, actorUserID, &guardedTemplate.query, "validation_failed", err.Error(),
			fmt.Errorf("%w: %v", ErrQueryValidationFailed, err), start)
	}

	// The existing governed chain (disclosure preflight, executor, history,
	// audit) runs unchanged through the shared executeGuardedChain, so every
	// template page is governed exactly like an ordinary execution.
	var page, pageSize int
	if req.Pagination != nil {
		page, pageSize = req.Pagination.Page, req.Pagination.PageSize
	}
	return s.executeGuardedChain(ctx, target, actorUserID, access.dsn, &guardedTemplate.query,
		func(execCtx context.Context, dsn string) (QueryDatabaseResult, error) {
			return s.executor.QueryTemplate(execCtx, dsn, guardedTemplate)
		}, page, pageSize, start)
}

// validateTemplateValues decodes the raw JSON values into typed values and
// returns controlled per-parameter field codes. It never echoes supplied
// values; the compiler re-validates the decoded values as the hard boundary.
func validateTemplateValues(definitions []TemplateParameterDefinition, rawValues map[string]json.RawMessage) (map[string]any, map[string]string) {
	fields := make(map[string]string)
	values := make(map[string]any, len(definitions))
	declared := make(map[string]TemplateParameterDefinition, len(definitions))
	for _, definition := range definitions {
		declared[definition.Name] = definition
	}
	for name := range rawValues {
		if _, ok := declared[name]; !ok {
			fields[name] = "unknown"
		}
	}
	for _, definition := range definitions {
		raw, ok := rawValues[definition.Name]
		if !ok {
			fields[definition.Name] = "missing"
			continue
		}
		value, code := decodeTemplateValue(definition, raw)
		if code != "" {
			fields[definition.Name] = code
			continue
		}
		values[definition.Name] = value
	}
	return values, fields
}

func decodeTemplateValue(definition TemplateParameterDefinition, raw json.RawMessage) (any, string) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, "invalid"
	}
	switch definition.Type {
	case TemplateParameterString:
		text, ok := value.(string)
		if !ok {
			return nil, "invalid"
		}
		if len(text) > templateMaxValueBytes {
			return nil, "oversized"
		}
		return text, ""
	case TemplateParameterDecimal:
		text, ok := value.(string)
		if !ok {
			return nil, "invalid"
		}
		if len(text) > templateMaxValueBytes {
			return nil, "oversized"
		}
		if !templateDecimalPattern.MatchString(text) {
			return nil, "invalid"
		}
		return text, ""
	case TemplateParameterInteger:
		number, ok := value.(json.Number)
		if !ok {
			return nil, "invalid"
		}
		parsed, err := number.Int64()
		if err != nil {
			return nil, "invalid"
		}
		return parsed, ""
	case TemplateParameterBoolean:
		boolean, ok := value.(bool)
		if !ok {
			return nil, "invalid"
		}
		return boolean, ""
	}
	return nil, "invalid"
}
