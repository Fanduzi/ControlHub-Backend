// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, errors, net/http, internal/model, internal/service
// output: querySavedStatementAPI interface, handleListSavedStatements, handleCreateSavedStatement, handleUpdateSavedStatement, handleDeleteSavedStatement, writeSavedStatementError
// pos: HTTP handlers for CRUD /query-targets/{id}/saved-statements (Phase 38R governed saved statements)
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// querySavedStatementAPI is the handler-level interface the saved statement
// service satisfies. Actor is taken from the verified token, never from body.
type querySavedStatementAPI interface {
	List(ctx context.Context, actor service.AuthenticatedUser, targetResourceID uint64, q string, page, pageSize int) (model.QuerySavedStatementListResponse, error)
	Create(ctx context.Context, actor service.AuthenticatedUser, req model.QuerySavedStatementCreateRequest) (model.QuerySavedStatement, error)
	Update(ctx context.Context, actor service.AuthenticatedUser, targetResourceID, statementID uint64, req model.QuerySavedStatementUpdateRequest) error
	Delete(ctx context.Context, actor service.AuthenticatedUser, targetResourceID, statementID uint64) error
}

// handleListSavedStatements handles GET /query-targets/{id}/saved-statements.
func handleListSavedStatements(svc querySavedStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}

		targetResourceID, err := parseUint64IDParam(chi.URLParam(r, "id"), "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		q := r.URL.Query().Get("q")
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))

		resp, err := svc.List(r.Context(), actor, targetResourceID, q, page, pageSize)
		if err != nil {
			writeSavedStatementError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handleCreateSavedStatement handles POST /query-targets/{id}/saved-statements.
func handleCreateSavedStatement(svc querySavedStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}

		targetResourceID, err := parseUint64IDParam(chi.URLParam(r, "id"), "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		var req model.QuerySavedStatementCreateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		req.TargetResourceID = targetResourceID

		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		result, err := svc.Create(r.Context(), actor, req)
		if err != nil {
			writeSavedStatementError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

// handleUpdateSavedStatement handles PUT /query-targets/{id}/saved-statements/{statementId}.
func handleUpdateSavedStatement(svc querySavedStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}

		targetResourceID, err := parseUint64IDParam(chi.URLParam(r, "id"), "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		statementID, err := parseUint64IDParam(chi.URLParam(r, "statementId"), "statementId")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		var req model.QuerySavedStatementUpdateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}

		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		if err := svc.Update(r.Context(), actor, targetResourceID, statementID, req); err != nil {
			writeSavedStatementError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleDeleteSavedStatement handles DELETE /query-targets/{id}/saved-statements/{statementId}.
func handleDeleteSavedStatement(svc querySavedStatementAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}

		targetResourceID, err := parseUint64IDParam(chi.URLParam(r, "id"), "id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		statementID, err := parseUint64IDParam(chi.URLParam(r, "statementId"), "statementId")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		if err := svc.Delete(r.Context(), actor, targetResourceID, statementID); err != nil {
			writeSavedStatementError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// writeSavedStatementError maps a service sentinel to a controlled HTTP response.
func writeSavedStatementError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", err.Error())
	case errors.Is(err, service.ErrQuerySavedStatementNotFound):
		writeJSONError(w, http.StatusNotFound, "saved_statement_not_found", err.Error())
	case errors.Is(err, service.ErrQueryForbidden):
		writeJSONError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, service.ErrQueryValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
