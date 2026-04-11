package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleListAuditEvents(auditService *service.AuditService) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		items, err := auditService.ListAll()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.AuditEvent `json:"items"`
		}{Items: items})
	}
}

func handleListResourceAuditEvents(auditService *service.AuditService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := auditService.ListByResourceID(chi.URLParam(r, "id"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.AuditEvent `json:"items"`
		}{Items: items})
	}
}
