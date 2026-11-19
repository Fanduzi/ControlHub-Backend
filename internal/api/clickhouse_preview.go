package api

import (
	"encoding/json"
	"github.com/fan/controlhub/internal/service"
	"github.com/go-chi/chi/v5"
	"net/http"
)

func handleCHPreview(s *service.CHPreview) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		user, ok := actorUserIDFromContext(r.Context())
		if !ok {
			writeJSONError(w, 401, "unauthorized", "user required")
			return
		}
		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "target id")
		if err != nil {
			writeJSONError(w, 400, "validation_failed", "invalid target")
			return
		}
		if s == nil {
			writeQueryExecutionError(w, service.ErrQueryTargetNotFound)
			return
		}
		if r.Method == http.MethodGet {
			catalog, err := s.Catalog(r.Context(), user, id)
			if err != nil {
				writeQueryExecutionError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, catalog)
			return
		}
		var req service.CHPreviewRequest
		body, err := readStrictBody(r, 4096)
		if err == nil {
			_, err = strictJSONObjectFields(body, map[string][]string{"generation": nil, "table": nil, "columns": nil, "limit": nil})
		}
		if err == nil {
			err = json.Unmarshal(body, &req)
		}
		if err != nil {
			writeJSONError(w, 400, "validation_failed", "invalid preview request")
			return
		}
		result, err := s.Preview(r.Context(), user, id, req)
		if err != nil {
			writeQueryExecutionError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(result)
	}
}
