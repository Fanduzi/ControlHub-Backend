// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, internal/service, internal/model
// output: handleGetTopology, handleGetEnvironmentTopology
// pos: HTTP handlers for resource-rooted and environment-scoped topology workspace reads
// note: if this file changes, update this header and module README.md.
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
		rootID, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		q := r.URL.Query()
		depth, err := parseTopologyDepth(q.Get("depth"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		direction := parseDirection(q.Get("direction"))
		if direction == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "direction must be both, upstream, or downstream")
			return
		}

		var relationType model.RelationType
		if rt := q.Get("relationType"); rt != "" {
			relationType = model.RelationType(rt)
			if err := relationType.Validate(); err != nil {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", "relationType is not supported")
				return
			}
		}

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

func handleGetEnvironmentTopology(topologyService *service.TopologyService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		environmentID, err := parseUint64IDParam(chi.URLParam(r, "id"), "environment id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		q := r.URL.Query()
		depth, err := parseTopologyDepth(q.Get("depth"))
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		rootID := uint64(0)
		if rawRootID := q.Get("rootResourceId"); rawRootID != "" {
			rootID, err = parseUint64IDParam(rawRootID, "rootResourceId")
			if err != nil {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
				return
			}
		}

		direction := parseDirection(q.Get("direction"))
		if direction == "" {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "direction must be both, upstream, or downstream")
			return
		}

		var relationType model.RelationType
		if rt := q.Get("relationType"); rt != "" {
			relationType = model.RelationType(rt)
			if err := relationType.Validate(); err != nil {
				writeJSONError(w, http.StatusBadRequest, "validation_failed", "relationType is not supported")
				return
			}
		}

		resp, err := topologyService.BuildTopology(model.TopologyQuery{
			EnvironmentID: environmentID,
			RootID:        rootID,
			Depth:         depth,
			Direction:     direction,
			RelationType:  relationType,
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

func parseTopologyDepth(s string) (int, error) {
	if s == "" {
		return 0, nil
	}
	depth, err := strconv.Atoi(s)
	if err != nil || depth < 1 {
		return 0, service.ErrInvalidDepth
	}
	return depth, nil
}
