// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, errors, net/http, strconv, chi, internal/model, internal/service
// output: querySchemaAPI interface, handleListSchemaDatabases, handleListSchemaObjects, handleGetObjectDetails, writeQuerySchemaError
// pos: HTTP handlers for GET /query-targets/{id}/schema/databases, /schema/objects, /schema/object-details (Phase 38I schema metadata)
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

// querySchemaAPI is the handler-level interface the schema service satisfies.
// Depending on the small interface (not the concrete struct) keeps handlers thin
// and lets handler tests substitute a stub. The actor is never accepted from
// query params — it is read from the auth middleware context.
type querySchemaAPI interface {
	ListDatabases(ctx context.Context, actorID, targetID uint64, q string, page, pageSize int, includeSystem, refresh bool) (model.DatabaseListResponse, error)
	ListObjects(ctx context.Context, actorID, targetID uint64, database, kind, q string, page, pageSize int, refresh bool) (model.ObjectListResponse, error)
	GetObjectDetails(ctx context.Context, actorID, targetID uint64, database, name, kind string, refresh bool) (model.ObjectDetailResponse, error)
}

// Schema query parameter length caps. Exceeding these is a 400.
const (
	schemaQMaxLen      = 200
	schemaDatabaseMax  = 128
	schemaNameMaxLen   = 128
	schemaPageSizeMax  = 100
	schemaPageSizeDef  = 50
	schemaPageDef      = 1
)

// handleListSchemaDatabases handles GET /query-targets/{id}/schema/databases.
// It parses query params (q, page, pageSize, includeSystem, refresh), extracts
// the actor from the verified token, and delegates to the service.
func handleListSchemaDatabases(svc querySchemaAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		q := r.URL.Query().Get("q")
		if len(q) > schemaQMaxLen {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "q exceeds maximum length")
			return
		}
		page, pageSize, err := parseSchemaPagination(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		includeSystem, err := parseBoolParam(r, "includeSystem")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		refresh, err := parseBoolParam(r, "refresh")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.ListDatabases(r.Context(), actorUserID, targetID, q, page, pageSize, includeSystem, refresh)
		if err != nil {
			writeQuerySchemaError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handleListSchemaObjects handles GET /query-targets/{id}/schema/objects.
// It parses query params (database, kind, q, page, pageSize, refresh), extracts
// the actor from the verified token, and delegates to the service.
func handleListSchemaObjects(svc querySchemaAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		database := r.URL.Query().Get("database")
		if database == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "database is required")
			return
		}
		if len(database) > schemaDatabaseMax {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "database exceeds maximum length")
			return
		}
		kind := r.URL.Query().Get("kind")
		if kind != "" {
			if err := model.ObjectKind(kind).Validate(); err != nil {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", "kind must be table or view")
				return
			}
		}
		q := r.URL.Query().Get("q")
		if len(q) > schemaQMaxLen {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "q exceeds maximum length")
			return
		}
		page, pageSize, err := parseSchemaPagination(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		refresh, err := parseBoolParam(r, "refresh")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.ListObjects(r.Context(), actorUserID, targetID, database, kind, q, page, pageSize, refresh)
		if err != nil {
			writeQuerySchemaError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// handleGetObjectDetails handles GET /query-targets/{id}/schema/object-details.
// It parses query params (database, name, kind, refresh), extracts the actor
// from the verified token, and delegates to the service.
func handleGetObjectDetails(svc querySchemaAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		database := r.URL.Query().Get("database")
		if database == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "database is required")
			return
		}
		if len(database) > schemaDatabaseMax {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "database exceeds maximum length")
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "name is required")
			return
		}
		if len(name) > schemaNameMaxLen {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "name exceeds maximum length")
			return
		}
		kind := r.URL.Query().Get("kind")
		if kind == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "kind is required")
			return
		}
		if err := model.ObjectKind(kind).Validate(); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "kind must be table or view")
			return
		}
		refresh, err := parseBoolParam(r, "refresh")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		actorUserID, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		resp, err := svc.GetObjectDetails(r.Context(), actorUserID, targetID, database, name, kind, refresh)
		if err != nil {
			writeQuerySchemaError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// writeQuerySchemaError maps a service sentinel to a controlled HTTP response.
func writeQuerySchemaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrSchemaValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "schema_validation_failed", err.Error())
	case errors.Is(err, service.ErrSchemaNotAllowed):
		writeJSONError(w, http.StatusForbidden, "schema_not_allowed", err.Error())
	case errors.Is(err, service.ErrSchemaTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "schema_target_not_found", err.Error())
	case errors.Is(err, service.ErrSchemaObjectNotFound):
		writeJSONError(w, http.StatusNotFound, "schema_object_not_found", err.Error())
	case errors.Is(err, service.ErrSchemaTimeout):
		writeJSONError(w, http.StatusRequestTimeout, "schema_timeout", err.Error())
	case errors.Is(err, service.ErrSchemaBackendError):
		writeJSONError(w, http.StatusBadGateway, "schema_backend_error", err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
	}
}

// parseSchemaPagination extracts page and pageSize from query params with
// defaults (page=1, pageSize=50) and caps (pageSize max 100).
func parseSchemaPagination(r *http.Request) (int, int, error) {
	page := schemaPageDef
	if raw := r.URL.Query().Get("page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return 0, 0, errors.New("page must be a positive integer")
		}
		page = v
	}
	pageSize := schemaPageSizeDef
	if raw := r.URL.Query().Get("pageSize"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			return 0, 0, errors.New("pageSize must be a positive integer")
		}
		if v > schemaPageSizeMax {
			return 0, 0, errors.New("pageSize exceeds maximum of 100")
		}
		pageSize = v
	}
	return page, pageSize, nil
}

// parseBoolParam parses a query parameter as a boolean. Accepts "true",
// "false", "1", "0", or absence (returns false). Any other value is a 400.
func parseBoolParam(r *http.Request, name string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(name))
	if raw == "" {
		return false, nil
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New(name + " must be a boolean")
	}
	return v, nil
}
