// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, internal/service, internal/model
// output: handleListAuditEvents, handleListResourceAuditEvents
// pos: HTTP handlers for audit event listing with pagination and filtering
// note: if this file changes, update header and README.md
package api

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleListAuditEvents(auditService *service.AuditService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, err := parseAuditListQuery(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
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
		id, err := parseUint64IDParam(chi.URLParam(r, "id"), "resource id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		items, err := auditService.ListByResourceID(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		writeJSON(w, http.StatusOK, struct {
			Items []model.AuditEvent `json:"items"`
		}{Items: items})
	}
}

func parseAuditListQuery(r *http.Request) (model.AuditListQuery, error) {
	q := r.URL.Query()
	page, pageSize := model.NormalizePagination(
		parseIntDefault(q.Get("page"), model.DefaultPage),
		parseIntDefault(q.Get("pageSize"), model.DefaultPageSize),
	)
	query := model.AuditListQuery{
		EventTypes: model.DedupStrings(q["eventType"]),
		Results:    model.DedupStrings(q["result"]),
		Page:       page,
		PageSize:   pageSize,
	}
	if values, ok := q["targetResourceId"]; ok {
		if len(values) == 0 {
			return model.AuditListQuery{}, fmt.Errorf("targetResourceId must be a positive integer")
		}
		var err error
		query.TargetResourceID, err = parseOptionalUint64QueryValue(values[0])
		if err != nil {
			return model.AuditListQuery{}, err
		}
	}
	return query, nil
}

func parseOptionalUint64QueryValue(raw string) (*uint64, error) {
	if raw == "" {
		return nil, fmt.Errorf("targetResourceId must be a positive integer")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || id == 0 {
		return nil, fmt.Errorf("targetResourceId must be a positive integer")
	}
	return &id, nil
}
