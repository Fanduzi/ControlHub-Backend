// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, encoding/json, internal/service, internal/model
// output: handleListResources, handleGetResource, handleGetResourceProfile, writeJSON, parseResourceListQuery, parseIntDefault
// pos: HTTP handlers for resource read operations with pagination and filtering
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

func handleListResources(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := parseResourceListQuery(r)
		items, pageInfo, err := resourceService.List(r.Context(), query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
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
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrResourceNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}

		writeJSON(w, http.StatusOK, item)
	}
}

func handleGetResourceProfile(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		profile, err := resourceService.GetProfile(chi.URLParam(r, "id"))
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, service.ErrResourceNotFound) {
				status = http.StatusNotFound
			}
			http.Error(w, err.Error(), status)
			return
		}

		writeJSON(w, http.StatusOK, profile)
	}
}

func parseResourceListQuery(r *http.Request) model.ResourceListQuery {
	q := r.URL.Query()
	page, pageSize := model.NormalizePagination(
		parseIntDefault(q.Get("page"), model.DefaultPage),
		parseIntDefault(q.Get("pageSize"), model.DefaultPageSize),
	)
	return model.ResourceListQuery{
		ResourceType:    q.Get("resourceType"),
		EnvironmentID:   q.Get("environmentId"),
		LifecycleStatus: q.Get("lifecycleStatus"),
		HealthStatus:    q.Get("healthStatus"),
		Query:           q.Get("q"),
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
