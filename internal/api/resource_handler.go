package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleListResources(resourceService *service.ResourceService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := resourceService.List(r.URL.Query().Get("type"), r.URL.Query().Get("environmentId"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.Resource `json:"items"`
		}{Items: items})
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

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
