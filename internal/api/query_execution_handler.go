// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, strconv, strings, chi, internal/model, internal/service
// output: handleExecuteQuery, handleListQueryExecutions, writeQueryExecutionError, queryExecutionAPI interface
// pos: HTTP handlers for POST /query-targets/{id}/execute and GET /query-targets/{id}/executions (Phase 37 read-only query sandbox)
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

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
	ListHistory(ctx context.Context, actorUserID uint64, actorRole string, targetID uint64, q model.QueryExecutionListQuery) ([]model.QueryExecutionRecord, *model.PageInfo, error)
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
		items, pageInfo, err := svc.ListHistory(r.Context(), actorUserID, actorRole, targetID, model.QueryExecutionListQuery{
			TargetResourceID: targetID, Page: page, PageSize: pageSize,
		})
		if err != nil {
			writeQueryExecutionError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, model.QueryExecutionListResponse{Items: items, PageInfo: pageInfo})
	}
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
	page := 1
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
