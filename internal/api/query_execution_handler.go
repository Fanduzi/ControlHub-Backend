// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: bytes, context, errors, fmt, io, net/http, strconv, strings, time, chi, internal/model, internal/service
// output: user-or-machine ordinary execute identity, user-only sibling query and owner-only statement handlers, controlled error mapping, queryExecutionAPI/queryExecutionStatementAPI interfaces
// pos: HTTP handlers for governed execute/history/owner-only statement retrieval, saved-statement execution, and related-record navigation. Execute/related disclosure blocks publish query_result_disclosure_blocked (Issue #48); target-not-enabled remains query_not_allowed.
// note: if this file changes, update header and README.md
package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// queryExecutionAPI is the handler-level interface the query execution service
// satisfies. Depending on the small interface (not the concrete struct) keeps
// the handlers thin and lets handler tests substitute a stub. The actor is never
// accepted from the request body — it is read from the auth middleware context.
type queryExecutionAPI interface {
	Execute(ctx context.Context, identity model.QueryExecutionIdentity, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error)
	ExecuteSavedStatement(ctx context.Context, actorUserID, targetID, statementID uint64, req model.QuerySavedStatementExecuteRequest) (model.QueryExecuteResponse, error)
	ListHistory(ctx context.Context, actorUserID uint64, actorRole string, targetID uint64, q model.QueryExecutionListQuery) (*model.QueryExecutionCursorPage, error)
	QueryEvidencePersistenceFailures() int64
	NavigateRelatedRecords(ctx context.Context, actorUserID uint64, targetID uint64, req model.RelatedRecordNavigationRequest) (model.RelatedRecordNavigationResponse, error)
}

type queryExecutionStatementAPI interface {
	GetExecutionStatement(ctx context.Context, actorUserID, targetID, executionID uint64) (model.QueryExecutionStatementResponse, error)
}

func handleExecuteQuery(svc queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.QueryExecuteRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if strings.TrimSpace(req.Statement) == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "statement is required")
			return
		}
		if req.Pagination != nil {
			if err := model.ValidatePagination(req.Pagination.Page, req.Pagination.PageSize); err != nil {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
		}
		identity, ok := queryExecutionIdentityFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.Execute(r.Context(), identity, targetID, req)
		if err != nil {
			writeQueryExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func queryExecutionIdentityFromContext(ctx context.Context) (model.QueryExecutionIdentity, bool) {
	actorUserID, userOK := actorUserIDFromContext(ctx)
	machine, machineOK := machinePrincipalFromContext(ctx)
	if userOK == machineOK {
		return model.QueryExecutionIdentity{}, false
	}
	identity := model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: actorUserID}
	if machineOK {
		identity = model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: machine.ID}
	}
	return identity, identity.Validate() == nil
}

// decodeTemplateExecuteJSONBody strictly decodes a template-execution request:
// only values, maxRows, and pagination survive; duplicate keys and unknown
// fields (SQL text, declarations, identities, roles, credentials, DSNs, policy
// and audit payloads) are rejected. The body is bounded so the 16 KiB values
// limit is enforced by the model Validate.
func decodeTemplateExecuteJSONBody(r *http.Request, target any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(model.MaxQuerySavedStatementExecuteValuesSize+2*1024)))
	if err != nil {
		return err
	}
	if err := rejectDuplicateJSONFields(body); err != nil {
		return err
	}
	return decodeJSON(bytes.NewReader(body), target)
}

// handleExecuteSavedStatement handles
// POST /query-targets/{id}/saved-statements/{statementId}/execute. The actor
// comes from the verified token; the statement ID and values only from the
// body/path. The server re-reads and authorizes the latest saved statement.
func handleExecuteSavedStatement(svc queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		statementID, err := parseUint64IDParam(chi.URLParam(r, "statementId"), "statement id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.QuerySavedStatementExecuteRequest
		if err := decodeTemplateExecuteJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.ExecuteSavedStatement(r.Context(), actorUserID, targetID, statementID, req)
		if err != nil {
			writeTemplateExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// writeJSONFieldError writes the error envelope with controlled per-field
// details. Field keys are author-declared parameter names and values are fixed
// codes; supplied parameter values are never included.
func writeJSONFieldError(w http.ResponseWriter, status int, code, message string, fields map[string]string) {
	writeJSON(w, status, map[string]any{
		"error":   code,
		"message": message,
		"details": fields,
	})
}

// writeTemplateExecutionError maps a template-execution service sentinel to a
// controlled HTTP response. The common execute sentinels reuse the ordinary
// execute mapper; only the template-specific cases (field codes and the
// saved-statement 404) are handled here. Parameter-name field codes carry no
// values.
func writeTemplateExecutionError(w http.ResponseWriter, err error) {
	var valueErr *service.TemplateValueValidationError
	if errors.As(err, &valueErr) {
		writeJSONFieldError(w, http.StatusBadRequest, "validation_failed", valueErr.Error(), valueErr.Fields)
		return
	}
	if errors.Is(err, service.ErrQuerySavedStatementNotFound) {
		writeJSONError(w, http.StatusNotFound, "saved_statement_not_found", err.Error())
		return
	}
	writeQueryExecutionError(w, err)
}

func handleListQueryExecutions(svc queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		actorRole, _ := actorRoleFromContext(r.Context())
		status, from, to, cursor, err := parseExecutionFilters(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		// WHY: parseExecutionFilters is parsed first so the page+cursor conflict
		// message takes precedence over page-value validation — the conflict is a
		// more specific diagnosis than "page is not a positive integer" and keeps
		// the existing 400 contract stable for clients that already special-case it.
		pg, err := parseExecutionPagination(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		result, err := svc.ListHistory(r.Context(), actorUserID, actorRole, targetID, model.QueryExecutionListQuery{
			TargetResourceID: targetID, Page: pg.Page, PageSize: pg.PageSize,
			Status: status, From: from, To: to, Cursor: cursor,
		})
		if err != nil {
			writeQueryExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleGetQueryExecutionStatement(svc queryExecutionStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		executionID, err := parseUint64IDParam(chi.URLParam(r, "executionId"), "execution id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		response, err := svc.GetExecutionStatement(r.Context(), actorUserID, targetID, executionID)
		if errors.Is(err, service.ErrQueryExecutionNotFound) {
			writeJSONError(w, http.StatusNotFound, "query_execution_not_found", err.Error())
			return
		}
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
			return
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func parseExecutionFilters(r *http.Request) (status *model.QueryExecutionStatus, from, to *time.Time, cursor *string, err error) {
	q := r.URL.Query()

	if q.Has("page") && q.Has("cursor") {
		return nil, nil, nil, nil, fmt.Errorf("cannot use both page and cursor parameters")
	}

	if q.Has("status") {
		raw := q.Get("status")
		if raw == "" {
			return nil, nil, nil, nil, fmt.Errorf("status parameter must not be empty")
		}
		if err := model.ValidateStatus(raw); err != nil {
			return nil, nil, nil, nil, err
		}
		s := model.QueryExecutionStatus(raw)
		status = &s
	}

	if q.Has("from") {
		raw := q.Get("from")
		if raw == "" {
			return nil, nil, nil, nil, fmt.Errorf("from parameter must not be empty")
		}
		t, err := model.ParseTimestamp(raw)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		from = &t
	}

	if q.Has("to") {
		raw := q.Get("to")
		if raw == "" {
			return nil, nil, nil, nil, fmt.Errorf("to parameter must not be empty")
		}
		t, err := model.ParseTimestamp(raw)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		to = &t
	}

	if err := model.ValidateTimeWindow(from, to); err != nil {
		return nil, nil, nil, nil, err
	}

	if q.Has("cursor") {
		raw := q.Get("cursor")
		if raw == "" {
			return nil, nil, nil, nil, fmt.Errorf("cursor parameter must not be empty")
		}
		cursor = &raw
	}

	return status, from, to, cursor, nil
}

// writeQueryExecutionError maps a service sentinel to a controlled HTTP response.
// No guard, policy, credential, or target-database validation issue becomes a 500.
// ErrQueryDisclosureBlocked is matched before ErrQueryNotAllowed because execute
// rejections wrap both; matching the other way published query_not_allowed.
func writeQueryExecutionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrQueryDisclosureBlocked):
		writeJSONError(w, http.StatusForbidden, "query_result_disclosure_blocked", err.Error())
	case errors.Is(err, service.ErrQueryNotAllowed):
		writeJSONError(w, http.StatusForbidden, "query_not_allowed", err.Error())
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", err.Error())
	case errors.Is(err, service.ErrQueryTimeout):
		writeJSONError(w, http.StatusRequestTimeout, "query_timeout", err.Error())
	case errors.Is(err, service.ErrQueryBackendFailure):
		writeJSONError(w, http.StatusBadGateway, "query_backend_error", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
	}
}

// executionPagination is the validated pagination boundary for the history
// list endpoint. Page == 0 means "page parameter absent" → cursor-initial mode.
// PageSize is always in [1, MaxPageSize] when this struct is returned without
// error; an absent pageSize parameter resolves to DefaultPageSize.
type executionPagination struct {
	Page     int
	PageSize int
}

// parseExecutionPagination is the single HTTP contract parser for the page and
// pageSize query parameters on GET /query-targets/{id}/executions. It
// distinguishes "parameter absent" from "parameter supplied but invalid":
//
//   - absent page        → cursor-initial mode (Page == 0)
//   - valid page (>=1)   → legacy offset mode (Page == N)
//   - invalid page       → 400 validation_failed (empty, zero, negative,
//     non-integer, >1 value, or out of int range)
//   - absent pageSize    → DefaultPageSize (20)
//   - valid pageSize     → clamped to [1, MaxPageSize]
//   - invalid pageSize   → 400 validation_failed (empty, zero, negative,
//     non-integer, >1 value, >MaxPageSize, or out of int
//     range)
//
// Repeated query parameters (?page=1&page=2) are rejected because the choice
// between two valid values is ambiguous and not part of the public contract.
// The service-layer NormalizePagination remains as defense-in-depth for any
// internal caller that bypasses this handler, but it is no longer the HTTP
// contract parser.
func parseExecutionPagination(r *http.Request) (executionPagination, error) {
	q := r.URL.Query()
	var pg executionPagination
	pg.PageSize = model.DefaultPageSize

	if raw, present := q["page"]; present {
		// q["page"] returns []string{} for "?page=" and a single-element slice
		// for "?page=N". Get() collapses both to "", which would silently treat
		// an explicit empty value as absent — that is the regression being fixed.
		if len(raw) != 1 || raw[0] == "" {
			return executionPagination{}, fmt.Errorf("page parameter must be a positive integer")
		}
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 {
			return executionPagination{}, fmt.Errorf("page parameter must be a positive integer")
		}
		pg.Page = n
	}

	if raw, present := q["pageSize"]; present {
		if len(raw) != 1 || raw[0] == "" {
			return executionPagination{}, fmt.Errorf("pageSize parameter must be an integer in 1..500")
		}
		n, err := strconv.Atoi(raw[0])
		if err != nil || n < 1 || n > model.MaxPageSize {
			return executionPagination{}, fmt.Errorf("pageSize parameter must be an integer in 1..500")
		}
		pg.PageSize = n
	}

	return pg, nil
}

// handleNavigateRelatedRecords handles POST /query-targets/{id}/related-records.
// It validates the request body, extracts the actor from the auth context, and
// delegates to the service. The browser never supplies referenced identifiers,
// SQL, credentials, or actor identity.
func handleNavigateRelatedRecords(svc queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.RelatedRecordNavigationRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.NavigateRelatedRecords(r.Context(), actorUserID, targetID, req)
		if err != nil {
			writeNavigationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// writeNavigationError maps a service sentinel to a controlled HTTP response.
// It reuses the same error mapping as writeQueryExecutionError since navigation
// shares the same governance and sentinel vocabulary. Disclosure is matched
// before not-allowed for the same wrap-order reason as writeQueryExecutionError.
func writeNavigationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryValidationFailed),
		errors.Is(err, service.ErrNavigationSourceNotFound),
		errors.Is(err, service.ErrNavigationValueMismatch):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrQueryDisclosureBlocked):
		writeJSONError(w, http.StatusForbidden, "query_result_disclosure_blocked", err.Error())
	case errors.Is(err, service.ErrQueryNotAllowed):
		writeJSONError(w, http.StatusForbidden, "query_not_allowed", err.Error())
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", err.Error())
	case errors.Is(err, service.ErrQueryTimeout):
		writeJSONError(w, http.StatusRequestTimeout, "query_timeout", err.Error())
	case errors.Is(err, service.ErrQueryBackendFailure):
		writeJSONError(w, http.StatusBadGateway, "query_backend_error", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
	}
}
