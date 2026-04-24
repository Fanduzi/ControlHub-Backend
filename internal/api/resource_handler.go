// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, encoding/json, internal/service, internal/model
// output: handleListResources, handleGetResource, handleGetResourceProfile, handleCreateResource, handlePatchResource, writeJSON, parseResourceListQuery, parseIntDefault
// pos: HTTP handlers for resource read and write operations with pagination and filtering
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"errors"
	"fmt"
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
		query, err := parseResourceListQuery(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
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
		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		item, err := resourceService.Get(id)
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

		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}

		updated, err := resourceService.Update(r.Context(), id, patch)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, updated)
	}
}

func handleGetResourceProfile(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		profile, err := resourceService.GetProfile(id)
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

		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		archived, err := resourceService.Archive(r.Context(), id, req)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, archived)
	}
}

func handleUnarchiveResource(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		unarchived, err := resourceService.Unarchive(r.Context(), id)
		if err != nil {
			writeServiceError(w, err)
			return
		}

		writeJSON(w, http.StatusOK, unarchived)
	}
}

func parseResourceListQuery(r *http.Request) (model.ResourceListQuery, error) {
	q := r.URL.Query()
	page, pageSize := model.NormalizePagination(
		parseIntDefault(q.Get("page"), model.DefaultPage),
		parseIntDefault(q.Get("pageSize"), model.DefaultPageSize),
	)
	environmentIDs, err := parseUint64QueryValues(q["environmentId"], "environmentId")
	if err != nil {
		return model.ResourceListQuery{}, err
	}
	return model.ResourceListQuery{
		ResourceTypes:    model.DedupStrings(q["resourceType"]),
		ResourceSubtypes: model.DedupStrings(q["resourceSubtype"]),
		EnvironmentIDs:   environmentIDs,
		LifecycleStatus:  model.DedupStrings(q["lifecycleStatus"]),
		HealthStatuses:   model.DedupStrings(q["healthStatus"]),
		Query:            q.Get("q"),
		IncludeArchived:  q.Get("includeArchived") == "true",
		ArchivedOnly:     q.Get("archivedOnly") == "true",
		Page:             page,
		PageSize:         pageSize,
	}, nil
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

func parseUint64IDParam(raw, name string) (uint64, error) {
	if raw == "" {
		return 0, fmt.Errorf("%s is required", name)
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return id, nil
}

func parseUint64QueryValues(values []string, name string) ([]uint64, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[uint64]bool, len(values))
	result := make([]uint64, 0, len(values))
	for _, raw := range values {
		if raw == "" {
			return nil, fmt.Errorf("%s must be a positive integer", name)
		}
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			return nil, fmt.Errorf("%s must be a positive integer", name)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, id)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
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
	case errors.As(err, new(*service.ValidationError)):
		var ve *service.ValidationError
		errors.As(err, &ve)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":   "validation_failed",
			"message": ve.Message,
			"details": ve.Fields,
		})
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
