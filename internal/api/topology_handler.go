// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, internal/service, internal/model
// output: handleGetTopology
// pos: HTTP handler for GET /resources/{id}/topology
// note: if this file changes, update header and README.md
package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleGetTopology(topologyService *service.TopologyService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rootID := chi.URLParam(r, "id")
		if rootID == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "resource id is required")
			return
		}

		q := r.URL.Query()
		depth := parseIntDefault(q.Get("depth"), 1)
		if depth < 1 || depth > 2 {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "depth must be 1 or 2")
			return
		}

		direction := parseDirection(q.Get("direction"))
		if direction == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "direction must be both, upstream, or downstream")
			return
		}

		relationType := model.RelationType(q.Get("relationType"))

		resp, err := topologyService.BuildTopology(model.TopologyQuery{
			RootID:       rootID,
			Depth:        depth,
			Direction:    direction,
			RelationType: relationType,
		})
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func parseDirection(s string) model.TopologyDirection {
	if s == "" {
		return model.TopologyDirectionBoth
	}
	d := model.TopologyDirection(s)
	switch d {
	case model.TopologyDirectionBoth, model.TopologyDirectionUpstream, model.TopologyDirectionDownstream:
		return d
	}
	return ""
}

// parseIntParam parses a query parameter as int with a default.
func parseIntParam(s string, def int) (int, error) {
	if s == "" {
		return def, nil
	}
	return strconv.Atoi(s)
}
