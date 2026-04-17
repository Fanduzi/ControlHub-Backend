// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, internal/service, internal/model
// output: handleListAuditEvents, handleListResourceAuditEvents
// pos: HTTP handlers for audit event listing with pagination and filtering
// note: if this file changes, update header and README.md
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleListAuditEvents(auditService *service.AuditService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query := parseAuditListQuery(r)
		items, pageInfo, err := auditService.List(r.Context(), query)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items    []model.AuditEvent `json:"items"`
			PageInfo *model.PageInfo    `json:"pageInfo"`
		}{Items: items, PageInfo: pageInfo})
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

func parseAuditListQuery(r *http.Request) model.AuditListQuery {
	q := r.URL.Query()
	page, pageSize := model.NormalizePagination(
		parseIntDefault(q.Get("page"), model.DefaultPage),
		parseIntDefault(q.Get("pageSize"), model.DefaultPageSize),
	)
	return model.AuditListQuery{
		TargetResourceID: q.Get("targetResourceId"),
		EventTypes:       model.DedupStrings(q["eventType"]),
		Results:          model.DedupStrings(q["result"]),
		Page:             page,
		PageSize:         pageSize,
	}
}
