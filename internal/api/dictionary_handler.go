// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, internal/service, internal/model
// output: handleListEnvironments/Owners/Roles/ResourceTypes/RelationTypes/LifecycleStatuses/HealthStatuses
// pos: HTTP handlers for all dictionary endpoints
// note: if this file changes, update header and README.md
package api

import (
	"net/http"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleListEnvironments(envService *service.EnvironmentService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := envService.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.Environment `json:"items"`
		}{Items: items})
	}
}

func handleListOwners(ownerService *service.OwnerService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := ownerService.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.Owner `json:"items"`
		}{Items: items})
	}
}

func handleListRoles(roleService *service.RoleService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := roleService.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.Role `json:"items"`
		}{Items: items})
	}
}

func handleListLifecycleStatuses(lifecycleStatusService *service.LifecycleStatusService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := lifecycleStatusService.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.DictionaryItem `json:"items"`
		}{Items: items})
	}
}

func handleListHealthStatuses(healthStatusService *service.HealthStatusService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := healthStatusService.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.DictionaryItem `json:"items"`
		}{Items: items})
	}
}

func handleListResourceTypes(resourceTypeService *service.ResourceTypeService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := resourceTypeService.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.DictionaryItem `json:"items"`
		}{Items: items})
	}
}

func handleListRelationTypes(relationTypeService *service.RelationTypeService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := relationTypeService.List()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.DictionaryItem `json:"items"`
		}{Items: items})
	}
}

func handleListResourceSubtypes(subtypeService *service.ResourceSubtypeService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resourceType := r.URL.Query().Get("resourceType")
		if resourceType == "" {
			http.Error(w, "resourceType query parameter is required", http.StatusBadRequest)
			return
		}

		items, err := subtypeService.List(resourceType)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			ResourceType string                 `json:"resourceType"`
			Subtypes     []model.DictionaryItem `json:"subtypes"`
		}{
			ResourceType: resourceType,
			Subtypes:     items,
		})
	}
}
