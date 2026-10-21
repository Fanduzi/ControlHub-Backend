// Package service provides the governed template-execution adapter.
// input: bytes, context, database/sql, encoding/json, errors, fmt, internal/model
// output: TemplateValueValidationError, QueryExecutionService.WithTemplateExecution, QueryExecutionService.ExecuteSavedStatement
// pos: User-only fresh-query-actor saved-statement execution that passes a validated user identity into the shared evidence chain without template identity/values; the saved entry kind withholds all statement-derived history (digest, preview, full statement); the read error classifies cancellation/deadline before sql.ErrNoRows and the returned snapshot is re-verified for exact id/target/known scope as the same invisible not-found (a reader-contract defense — the production WHERE already scopes by target/id); a non-empty clientExecutionId admits the shared MySQL/TiDB claim before compile/guard and finalizes exactly once (keyless pair or keyed finalize)
// note: if this file changes, update this header and module README.md.
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
	identity := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: actorUserID}
	if actorUserID == 0 || targetID == 0 || statementID == 0 || identity.Validate() != nil {
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: invalid execution identity", ErrQueryValidationFailed)
	}
	if err := req.Validate(); err != nil {
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
	}
	if s.statements == nil || s.compiler == nil {
		return model.QueryExecuteResponse{}, ErrQueryBackendFailure
	}

	// Engine dispatch: only when the construction-time PG HTTP capability is
	// wired may a postgresql target leave the legacy path. The ?database=
	// scope is engine-bound: PG requires an explicit single non-empty value
	// (matched to the authorized statement's own snapshot inside the
	// orchestrator); every other engine keeps the legacy empty-scope
	// contract and must not let a PG-style selector slide in.
	if s.pgHTTPEntries {
		target, terr := s.pgDispatchTarget(ctx, targetID)
		if terr != nil {
			return model.QueryExecuteResponse{}, terr
		}
		if isPGEngine(target.ConnectionContext.Engine) {
			if err := model.ValidateIdentifierShape("database", req.ScopeDatabase, true); err != nil {
				return model.QueryExecuteResponse{}, fmt.Errorf("%w: %v", ErrQueryValidationFailed, err)
			}
			return s.executeSavedPGHTTP(ctx, identity, target, statementID, req)
		}
	}
	if req.ScopeDatabase != "" {
		return model.QueryExecuteResponse{}, fmt.Errorf("%w: database is not accepted for this target", ErrQueryValidationFailed)
	}

	// The request belongs to this attempt: clone every caller-owned mutable
	// field — the raw values map and each RawMessage byte slice, the
	// pagination struct, and the capability list — before any external call
	// so nothing the caller mutates can reach the claim digest, the typed
	// values, the guard, or the delivery gate.
	ownedValues := make(map[string]json.RawMessage, len(req.Values))
	for name, raw := range req.Values {
		ownedValues[name] = append(json.RawMessage(nil), raw...)
	}
	req.Values = ownedValues
	req.Capabilities = append([]string(nil), req.Capabilities...)
	var page, pageSize int
	if req.Pagination != nil {
		pagination := *req.Pagination
		req.Pagination = &pagination
		page, pageSize = pagination.Page, pagination.PageSize
	}
	start := s.clock.Now()

	access, err := s.access.Resolve(ctx, actorUserID, targetID)
	if err != nil {
		if errors.Is(err, ErrQueryTargetNotFound) {
			return model.QueryExecuteResponse{}, err
		}
		var accessErr *TargetAccessError
		if errors.As(err, &accessErr) {
			return s.reject(ctx, access.Target, identity, nil, "query_not_allowed", accessErr.Error(),
				fmt.Errorf("%w: %s", ErrQueryNotAllowed, accessErr.Error()), start)
		}
		return model.QueryExecuteResponse{}, err
	}
	target := access.Target

	attempt := newMySQLExecutionAttempt(s, target, identity, mysqlEntrySaved, start)

	// One joined snapshot read per request/page: the latest statement row and
	// its parameter declarations are used once for digest, compile, and
	// execution. A missing row and a non-owner personal statement are the
	// same invisible not-found; a canceled or deadline read classifies
	// through the shared taxonomy; any other read fault is a backend
	// failure. Raw repository errors never reach the response.
	statement, err := s.statements.GetByID(ctx, targetID, statementID)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
			// Cancellation/deadline classify first so a canceled or timed-out
			// read joined with sql.ErrNoRows can never masquerade as a missing
			// statement.
			status, sentinel, code, safeMsg := classifyExecutorError(err)
			rec := attempt.buildAttemptRecord(nil, status, 0, code, safeMsg)
			if _, perr := attempt.persistRecord(ctx, rec); perr != nil {
				return model.QueryExecuteResponse{}, errPersistAttempt
			}
			return model.QueryExecuteResponse{}, sentinel
		case errors.Is(err, sql.ErrNoRows):
			return attempt.reject(ctx, nil, "saved_statement_not_found", "saved statement not found",
				ErrQuerySavedStatementNotFound)
		default:
			rec := attempt.buildAttemptRecord(nil, model.QueryExecutionFailed, 0, "query_backend_error", "saved statement read failed")
			if _, perr := attempt.persistRecord(ctx, rec); perr != nil {
				return model.QueryExecuteResponse{}, errPersistAttempt
			}
			return model.QueryExecuteResponse{}, ErrQueryBackendFailure
		}
	}
	// The returned snapshot must be exactly the requested statement on this
	// target carrying a known scope — defense against a reader violating its
	// contract, nothing more. The production repository's WHERE already
	// scopes by (target, id), so a mismatch can only come from a broken
	// reader, never from stored rows; the unit proofs pin that controlled
	// boundary and do not claim a production cross-target leak. Every
	// violation — foreign id, foreign target, unknown scope, or a non-owner
	// personal statement — is the same invisible not-found so existence and
	// ownership never leak.
	if statement.ID != statementID || statement.TargetResourceID != targetID ||
		statement.Scope.Validate() != nil ||
		(statement.Scope == model.QuerySavedStatementPersonal && statement.OwnerUserID != actorUserID) {
		return attempt.reject(ctx, nil, "saved_statement_not_found", "saved statement not found",
			ErrQuerySavedStatementNotFound)
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
		rec := attempt.buildAttemptRecord(nil, model.QueryExecutionRejected, 0, "validation_failed", valueErr.Error())
		if _, perr := attempt.persistRecord(ctx, rec); perr != nil {
			return model.QueryExecuteResponse{}, errPersistAttempt
		}
		return model.QueryExecuteResponse{}, valueErr
	}

	maxRows := s.guard.effectiveMaxRows(clampProductionMaxRows(target.ConnectionContext.Environment, req.MaxRows))

	// Claim admission runs before compile and guard: a non-empty client key
	// occupies the (target, key) slot once with empty MySQL/TiDB connection
	// scope. Static statements admit exactly like parameterized ones.
	timeout := defaultQueryTimeout
	if isProductionEnvironment(target.ConnectionContext.Environment) {
		timeout = productionQueryTimeout
	}
	digest := s.mysqlSavedDigest(statementID, definitions, values, page, pageSize, maxRows)
	if err := attempt.admit(ctx, req.ClientExecutionID, digest, timeout); err != nil {
		return model.QueryExecuteResponse{}, err
	}

	input := TemplateStatementInput{Statement: statement.Statement, Definitions: definitions, Values: values}
	var guardedTemplate GuardedTemplateStatement
	if req.Pagination != nil {
		guardedTemplate, err = s.compiler.CompileAndGuardPaginated(s.guard, input, req.Pagination.Page, req.Pagination.PageSize, maxRows)
	} else {
		guardedTemplate, err = s.compiler.CompileAndGuard(s.guard, input, maxRows)
	}
	if err != nil {
		return attempt.reject(ctx, nil, "validation_failed", "saved statement validation failed",
			fmt.Errorf("%w: %v", ErrQueryValidationFailed, err))
	}

	// The existing governed chain (disclosure preflight, executor, history,
	// audit) runs unchanged through the shared executeGuardedChain, so every
	// template page is governed exactly like an ordinary execution. The saved
	// entry kind forces digest, preview, and full statement empty — compiled
	// SQL never reaches history regardless of guard metadata.
	return s.executeGuardedChain(ctx, attempt, access.dsn, &guardedTemplate.query,
		func(execCtx context.Context, dsn string) (QueryDatabaseResult, error) {
			return s.executor.QueryTemplate(execCtx, dsn, guardedTemplate)
		}, req.Capabilities, page, pageSize)
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
