// Package api provides the HTTP handler for POST /query-targets/{id}/explain.
// input: net/http, strings, internal/model, internal/service
// output: handleExplainQuery, queryExplainAPI (interface), writeQueryExplainError
// pos: Phase 38N governed Explain endpoint — fresh-actor, target-scoped, leak-free
// note: if this file changes, update header, README.md, and internal/openapi/openapi.yaml
package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

// queryExplainAPI is the thin interface the handler depends on. The concrete
// *service.QueryExplainService satisfies it. Keeping a separate interface
// (rather than extending queryExecutionAPI) keeps Explain independently
// wireable and testable, and prevents the execute handler stub from growing
// an Explain method.
type queryExplainAPI interface {
	Explain(ctx context.Context, actorUserID uint64, targetID uint64, req model.ExplainRequest) (model.ExplainResponse, error)
}

// handleExplainQuery handles POST /query-targets/{id}/explain. It decodes
// only the worksheet statement; the actor is derived from the verified token
// and the engine/governance context from target resolution. It never echoes
// the statement or a database error.
func handleExplainQuery(svc queryExplainAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		targetID, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.ExplainRequest
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
		resp, err := svc.Explain(r.Context(), actorUserID, targetID, req)
		if err != nil {
			writeQueryExplainError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// writeQueryExplainError maps a service sentinel to a controlled HTTP
// response. CRITICAL (Oracle P1.4): the handler selects a fixed,
// sentinel-specific message string and NEVER serializes err.Error() in the
// response body. The existing writeQueryExecutionError pattern calls
// err.Error() for the public message field; Explain does NOT copy that
// pattern because Vitess parse errors can contain user SQL/literals.
func writeQueryExplainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrQueryValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", "statement is not a permitted read-only SELECT")
	case errors.Is(err, service.ErrQueryNotAllowed):
		writeJSONError(w, http.StatusForbidden, "query_not_allowed", "target is not enabled for execution")
	case errors.Is(err, service.ErrQueryTargetNotFound):
		writeJSONError(w, http.StatusNotFound, "query_target_not_found", "query target not found")
	case errors.Is(err, service.ErrQueryExplainNotSupported):
		writeJSONError(w, http.StatusConflict, "query_explain_not_supported", "explain is not supported for this target engine")
	case errors.Is(err, service.ErrQueryTimeout):
		writeJSONError(w, http.StatusRequestTimeout, "query_timeout", "explain exceeded the timeout")
	case errors.Is(err, service.ErrQueryBackendFailure):
		writeJSONError(w, http.StatusBadGateway, "query_backend_error", "target database rejected the explain request")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
	}
}
