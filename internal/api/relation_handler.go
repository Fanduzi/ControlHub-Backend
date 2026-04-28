// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, internal/service
// output: handleListResourceRelations, handleCreateResourceRelation, handleDeleteResourceRelation
// pos: HTTP handlers for relation listing and maintenance
// note: if this file changes, update header and README.md
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleListResourceRelations(relationService *service.RelationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		view := r.URL.Query().Get("view")
		if view == "resolved" {
			items, err := relationService.ListRelationViewsByResourceID(id)
			if err != nil {
				writeServiceError(w, err)
				return
			}
			writeJSON(w, http.StatusOK, struct {
				Items []model.ResourceRelationView `json:"items"`
			}{Items: items})
			return
		}

		items, err := relationService.ListByResourceID(id)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.ResourceRelation `json:"items"`
		}{Items: items})
	}
}

func handleGetResourceMembers(relationService *service.RelationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		members, err := relationService.ListClusterMembers(id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Members []model.ClusterMemberView `json:"members"`
		}{Members: members})
	}
}

func handleCreateResourceRelation(relationService *service.RelationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input model.RelationCreateInput
		if err := decodeJSONBody(r, &input); err != nil {
			writeJSONError(w, http.StatusBadRequest, "malformed_json", "request body must be valid JSON")
			return
		}

		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		created, err := relationService.Create(r.Context(), id, input)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, created)
	}
}

func handleDeleteResourceRelation(relationService *service.RelationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "relation id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		if err := relationService.Delete(r.Context(), id); err != nil {
			writeServiceError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
