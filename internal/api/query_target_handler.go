// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, strconv, strings, internal/model, internal/service
// output: handleListQueryTargets, parseQueryTargetListQuery
// pos: Read-only HTTP handler for GET /query-targets (Query Workbench target context)
// note: if this file changes, update header and README.md
package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleListQueryTargets(queryTargetService *service.QueryTargetService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, err := parseQueryTargetListQuery(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		items, err := queryTargetService.List(r.Context(), query)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
			return
		}
		writeJSON(w, http.StatusOK, model.QueryTargetListResponse{Items: items})
	}
}

// parseQueryTargetListQuery parses the cheap, conventional filters for
// /query-targets. Only engine and environmentId are filtered server-side;
// readiness and query kind are derived by the client from the response.
func parseQueryTargetListQuery(r *http.Request) (model.QueryTargetListQuery, error) {
	values := r.URL.Query()
	engine := strings.ToLower(strings.TrimSpace(values.Get("engine")))

	var environmentID uint64
	if raw := values.Get("environmentId"); raw != "" {
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			return model.QueryTargetListQuery{}, fmt.Errorf("environmentId must be a positive integer")
		}
		environmentID = id
	}

	return model.QueryTargetListQuery{Engine: engine, EnvironmentID: environmentID}, nil
}
