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
