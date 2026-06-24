// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, errors, net/http, chi, internal/model, internal/service
// output: queryCredentialAPI interface, handleGetQueryCredential, handlePutQueryCredential, handleDeleteQueryCredential, writeQueryCredentialError
// pos: HTTP handlers for GET/PUT/DELETE /query-targets/{id}/credential (Phase 38A credential metadata management — metadata only, never a DSN)
// note: if this file changes, update header and README.md
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// queryCredentialAPI is the handler-level interface the credential service
// satisfies. Depending on the small interface (not the concrete struct) keeps
// handlers thin and lets tests substitute a stub. The actor is taken from the
// verified token (context), never from the request body.
type queryCredentialAPI interface {
	GetStatus(ctx context.Context, targetID uint64) (model.QueryCredentialStatusResponse, error)
	Upsert(ctx context.Context, actor service.AuthenticatedUser, targetID uint64, req model.QueryCredentialUpsertRequest) (model.QueryCredentialStatusResponse, error)
	Delete(ctx context.Context, actor service.AuthenticatedUser, targetID uint64) error
}

// adminRoleName is the canonical role permitted to write/delete credential
// metadata. It mirrors the seeded admin role (internal/api/test_server.go) and
// service.adminRole; only this role receives a non-403 on PUT/DELETE.
const adminRoleName = "admin"

// handleGetQueryCredential handles GET /query-targets/{id}/credential. It returns
// the metadata-only status response (never a DSN). Any authenticated actor with a
// fresh bearer token may read.
func handleGetQueryCredential(svc queryCredentialAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		resp, err := svc.GetStatus(r.Context(), targetID)
		if err != nil {
			writeQueryCredentialError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handlePutQueryCredential handles PUT /query-targets/{id}/credential. It decodes
// the request strictly (rejecting any unknown field — including DSN, password,
// host, port, or actor fields), enforces the admin-only boundary, validates the
// request contract, then upserts. The actor is taken from the verified token. The
// response carries metadata only.
func handlePutQueryCredential(svc queryCredentialAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.QueryCredentialUpsertRequest
		if err := decodeJSONBody(r, &req); err != nil {
			// Strict decoding rejects unknown fields, including any DSN, password,
			// host, port, or actor field — these must never be accepted.
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if actor.Role != adminRoleName {
			writeJSONError(w, http.StatusForbidden, "forbidden", "admin role required to manage credential metadata")
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		resp, err := svc.Upsert(r.Context(), actor, targetID, req)
		if err != nil {
			writeQueryCredentialError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handleDeleteQueryCredential handles DELETE /query-targets/{id}/credential. It
// enforces the admin-only boundary and returns 204 on success.
func handleDeleteQueryCredential(svc queryCredentialAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		if actor.Role != adminRoleName {
			writeJSONError(w, http.StatusForbidden, "forbidden", "admin role required to manage credential metadata")
			return
		}
		if err := svc.Delete(r.Context(), actor, targetID); err != nil {
			writeQueryCredentialError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// actorFromContext builds the authenticated actor from the id and role stored by
// the auth middleware. The role gates admin-only writes; the id is attributed to
// audit rows. Both come from the verified token.
func actorFromContext(ctx context.Context) (service.AuthenticatedUser, bool) {
	id, ok := actorUserIDFromContext(ctx)
	if !ok {
		return service.AuthenticatedUser{}, false
	}
	role, _ := actorRoleFromContext(ctx)
	return service.AuthenticatedUser{ID: id, Role: role}, true
}

// writeQueryCredentialError maps a service sentinel to a controlled HTTP response.
// No validation, authorization, or not-found issue becomes a 500; only unexpected
// persistence failures do.
func writeQueryCredentialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryCredentialForbidden):
		writeJSONError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, service.ErrQueryCredentialValidation):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
	}
}
