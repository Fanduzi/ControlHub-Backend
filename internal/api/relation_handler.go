package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleListResourceRelations(relationService *service.RelationService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := relationService.ListByResourceID(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.ResourceRelation `json:"items"`
		}{Items: items})
	}
}
