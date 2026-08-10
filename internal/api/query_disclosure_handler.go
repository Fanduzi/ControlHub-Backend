// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, errors, net/http, internal/model, internal/service
// output: queryDisclosureAPI interface, handleListPolicies, handleCreatePolicy, handleUpdatePolicy, handleDeletePolicy, writeDisclosureError
// pos: HTTP handlers for CRUD /query-disclosure-policies (Phase 38Q result-disclosure policy management)
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// queryDisclosureAPI is the handler-level interface the disclosure service
// satisfies. Depending on the small interface (not the concrete struct) keeps
// handlers thin and lets tests substitute a stub. The actor is taken from the
// verified token (context), never from the request body.
type queryDisclosureAPI interface {
	ListPolicies(ctx context.Context, targetResourceID uint64) ([]model.ResultDisclosurePolicy, error)
	CreatePolicy(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) (uint64, error)
	UpdatePolicy(ctx context.Context, req model.ResultDisclosurePolicyUpsertRequest) error
	DeletePolicy(ctx context.Context, targetResourceID uint64, database, object, column string) error
}

// handleListPolicies handles GET /query-disclosure-policies. It returns all
// disclosure policies for a given query target. Admin-only — policy scope
// and mode metadata is governance-sensitive.
func handleListPolicies(svc queryDisclosureAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if actor.Role != adminRoleName {
			writeJSONError(w, http.StatusForbidden, "forbidden", "admin role required to manage disclosure policies")
			return
		}
		targetResourceID, err := parseUint64QueryParam(r, "targetResourceId")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		items, err := svc.ListPolicies(r.Context(), targetResourceID)
		if err != nil {
			writeDisclosureError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Items []model.ResultDisclosurePolicy `json:"items"`
		}{Items: items})
	}
}

// handleCreatePolicy handles POST /query-disclosure-policies. It decodes the
// request strictly (rejecting any unknown field), enforces the admin-only
// boundary, validates the request contract, then creates. The actor is taken
// from the verified token.
func handleCreatePolicy(svc queryDisclosureAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if actor.Role != adminRoleName {
			writeJSONError(w, http.StatusForbidden, "forbidden", "admin role required to manage disclosure policies")
			return
		}
		var req model.ResultDisclosurePolicyUpsertRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		id, err := svc.CreatePolicy(r.Context(), req)
		if err != nil {
			writeDisclosureError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, model.ResultDisclosurePolicy{
			ID:               id,
			TargetResourceID: req.TargetResourceID,
			DatabaseName:     req.DatabaseName,
			ObjectName:       req.ObjectName,
			ColumnName:       req.ColumnName,
			Mode:             req.Mode,
		})
	}
}

// handleUpdatePolicy handles PUT /query-disclosure-policies. It decodes the
// request strictly, enforces the admin-only boundary, validates the request
// contract, then updates. The actor is taken from the verified token.
func handleUpdatePolicy(svc queryDisclosureAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if actor.Role != adminRoleName {
			writeJSONError(w, http.StatusForbidden, "forbidden", "admin role required to manage disclosure policies")
			return
		}
		var req model.ResultDisclosurePolicyUpsertRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		if err := svc.UpdatePolicy(r.Context(), req); err != nil {
			writeDisclosureError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleDeletePolicy handles DELETE /query-disclosure-policies. It enforces the
// admin-only boundary and returns 204 on success. The scope (target, database,
// object, column) is extracted from query parameters.
func handleDeletePolicy(svc queryDisclosureAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if actor.Role != adminRoleName {
			writeJSONError(w, http.StatusForbidden, "forbidden", "admin role required to manage disclosure policies")
			return
		}
		targetResourceID, err := parseUint64QueryParam(r, "targetResourceId")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		database := r.URL.Query().Get("databaseName")
		object := r.URL.Query().Get("objectName")
		column := r.URL.Query().Get("columnName")
		if database == "" || object == "" || column == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "databaseName, objectName, and columnName are required")
			return
		}
		if err := svc.DeletePolicy(r.Context(), targetResourceID, database, object, column); err != nil {
			writeDisclosureError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// parseUint64QueryParam extracts a required uint64 query parameter.
func parseUint64QueryParam(r *http.Request, name string) (uint64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return 0, &fieldError{field: name, message: "is required"}
	}
	return parseUint64IDParam(raw, name)
}

// fieldError is a simple validation error for a single field.
type fieldError struct {
	field   string
	message string
}

func (e *fieldError) Error() string { return e.field + " " + e.message }

// writeDisclosureError maps a service sentinel to a controlled HTTP response.
// No validation, authorization, or not-found issue becomes a 500; only unexpected
// persistence failures do.
func writeDisclosureError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", err.Error())
	case errors.Is(err, service.ErrQueryDisclosureBlocked):
		writeJSONError(w, http.StatusForbidden, "query_result_disclosure_blocked", err.Error())
	case errors.Is(err, service.ErrQueryValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrQueryDisclosurePolicyConflict):
		writeJSONError(w, http.StatusConflict, "disclosure_policy_conflict", err.Error())
	case errors.Is(err, service.ErrQueryDisclosurePolicyNotFound):
		writeJSONError(w, http.StatusNotFound, "disclosure_policy_not_found", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
