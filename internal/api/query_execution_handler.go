// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, strconv, strings, chi, internal/model, internal/service
// output: handleExecuteQuery, handleListQueryExecutions, writeQueryExecutionError, queryExecutionAPI interface
// pos: HTTP handlers for POST /query-targets/{id}/execute and GET /query-targets/{id}/executions (Phase 37 read-only query sandbox)
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"errors"
	"fmt"
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
	Execute(ctx context.Context, actorUserID uint64, targetID uint64, req model.QueryExecuteRequest) (model.QueryExecuteResponse, error)
	ListHistory(ctx context.Context, actorUserID uint64, actorRole string, targetID uint64, q model.QueryExecutionListQuery) (*model.QueryExecutionCursorPage, error)
	NavigateRelatedRecords(ctx context.Context, actorUserID uint64, targetID uint64, req model.RelatedRecordNavigationRequest) (model.RelatedRecordNavigationResponse, error)
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
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.Execute(r.Context(), actorUserID, targetID, req)
		if err != nil {
			writeQueryExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
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
		page, pageSize := parseExecutionPagination(r)
		status, from, to, cursor, err := parseExecutionFilters(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		result, err := svc.ListHistory(r.Context(), actorUserID, actorRole, targetID, model.QueryExecutionListQuery{
			TargetResourceID: targetID, Page: page, PageSize: pageSize,
			Status: status, From: from, To: to, Cursor: cursor,
		})
		if err != nil {
			writeQueryExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
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
func writeQueryExecutionError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
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

func parseExecutionPagination(r *http.Request) (int, int) {
	var page int // 0 = not explicitly set → cursor-initial mode
	if raw := r.URL.Query().Get("page"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			page = v
		}
	}
	pageSize := 20
	if raw := r.URL.Query().Get("pageSize"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			pageSize = v
		}
	}
	return page, pageSize
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
// shares the same governance and sentinel vocabulary.
func writeNavigationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryValidationFailed),
		errors.Is(err, service.ErrNavigationSourceNotFound),
		errors.Is(err, service.ErrNavigationValueMismatch):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
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
