// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, encoding/json, internal/service, internal/model
// output: handleListResources, handleGetResource, handleGetResourceProfile, handleCreateResource, handlePatchResource, writeJSON, parseResourceListQuery, parseIntDefault
// pos: HTTP handlers for resource read and write operations with pagination and filtering
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func handleListResources(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := parseResourceListQuery(r)
		items, pageInfo, err := resourceService.List(r.Context(), query)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items    []model.Resource `json:"items"`
			PageInfo *model.PageInfo  `json:"pageInfo"`
		}{Items: items, PageInfo: pageInfo})
	}
}

func handleGetResource(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, err := resourceService.Get(chi.URLParam(r, "id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, item)
	}
}

func handleCreateResource(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input model.ResourceCreateInput
		if err := decodeJSONBody(r, &input); err != nil {
			writeJSONError(w, http.StatusBadRequest, "malformed_json", "request body must be valid JSON")
			return
		}

		created, err := resourceService.Create(r.Context(), input)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusCreated, created)
	}
}

func handlePatchResource(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var patch model.ResourcePatchRequest
		if err := decodeJSONBody(r, &patch); err != nil {
			writeJSONError(w, http.StatusBadRequest, "malformed_json", "request body must be valid JSON")
			return
		}

		updated, err := resourceService.Update(r.Context(), chi.URLParam(r, "id"), patch)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, updated)
	}
}

func handleGetResourceProfile(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profile, err := resourceService.GetProfile(chi.URLParam(r, "id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, profile)
	}
}

func handleArchiveResource(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.ArchiveRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "malformed_json", "request body must be valid JSON")
			return
		}

		archived, err := resourceService.Archive(r.Context(), chi.URLParam(r, "id"), req)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, archived)
	}
}

func handleUnarchiveResource(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		unarchived, err := resourceService.Unarchive(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, unarchived)
	}
}

func parseResourceListQuery(r *http.Request) model.ResourceListQuery {
	q := r.URL.Query()
	page, pageSize := model.NormalizePagination(
		parseIntDefault(q.Get("page"), model.DefaultPage),
		parseIntDefault(q.Get("pageSize"), model.DefaultPageSize),
	)
	return model.ResourceListQuery{
		ResourceTypes:   model.DedupStrings(q["resourceType"]),
		ResourceSubtypes: model.DedupStrings(q["resourceSubtype"]),
		EnvironmentIDs:  model.DedupStrings(q["environmentId"]),
		LifecycleStatus: model.DedupStrings(q["lifecycleStatus"]),
		HealthStatuses:  model.DedupStrings(q["healthStatus"]),
		Query:           q.Get("q"),
		IncludeArchived: q.Get("includeArchived") == "true",
		ArchivedOnly:    q.Get("archivedOnly") == "true",
		Page:            page,
		PageSize:        pageSize,
	}
}

func parseIntDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func decodeJSONBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if decoder.More() {
		return errors.New("multiple JSON values are not allowed")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: code, Message: message})
}

func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrValidationFailed):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrEnvironmentNotFound):
		writeJSONError(w, http.StatusNotFound, "environment_not_found", err.Error())
	case errors.Is(err, service.ErrOwnerNotFound):
		writeJSONError(w, http.StatusNotFound, "owner_not_found", err.Error())
	case errors.Is(err, service.ErrResourceNotFound):
		writeJSONError(w, http.StatusNotFound, "resource_not_found", err.Error())
	case errors.Is(err, service.ErrRelationNotFound):
		writeJSONError(w, http.StatusNotFound, "relation_not_found", err.Error())
	case errors.Is(err, service.ErrResourceConflict):
		writeJSONError(w, http.StatusConflict, "resource_conflict", err.Error())
	case errors.Is(err, service.ErrRelationConflict):
		writeJSONError(w, http.StatusConflict, "relation_conflict", err.Error())
	case errors.Is(err, service.ErrResourceArchived):
		writeJSONError(w, http.StatusConflict, "resource_archived", "resource is archived")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
	}
}
