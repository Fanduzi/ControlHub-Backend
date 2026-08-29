// Package api exposes named inventory view HTTP handlers.
// input: context, errors, net/http, chi/v5, internal/model, internal/service
// output: authenticated list/create/update/delete handlers and controlled errors
// pos: HTTP boundary for personal and shared named inventory views
// note: if this file changes, update this header and module README.md.
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type namedInventoryViewAPI interface {
	List(context.Context, service.AuthenticatedUser) ([]model.NamedInventoryView, error)
	Create(context.Context, service.AuthenticatedUser, model.NamedInventoryViewCreateRequest) (model.NamedInventoryView, error)
	Update(context.Context, service.AuthenticatedUser, uint64, model.NamedInventoryViewUpdateRequest) error
	Delete(context.Context, service.AuthenticatedUser, uint64) error
}

func handleListNamedInventoryViews(svc namedInventoryViewAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		items, err := svc.List(r.Context(), actor)
		if err != nil {
			writeNamedInventoryViewError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Items           []model.NamedInventoryView `json:"items"`
			CanManageShared bool                       `json:"canManageShared"`
		}{items, actor.Role == "admin"})
	}
}

func handleCreateNamedInventoryView(svc namedInventoryViewAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		var req model.NamedInventoryViewCreateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		view, err := svc.Create(r.Context(), actor, req)
		if err != nil {
			writeNamedInventoryViewError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, view)
	}
}

func handleUpdateNamedInventoryView(svc namedInventoryViewAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		id, err := parseUint64IDParam(chi.URLParam(r, "viewId"), "view id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.NamedInventoryViewUpdateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		if err := svc.Update(r.Context(), actor, id, req); err != nil {
			writeNamedInventoryViewError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleDeleteNamedInventoryView(svc namedInventoryViewAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		id, err := parseUint64IDParam(chi.URLParam(r, "viewId"), "view id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		if err := svc.Delete(r.Context(), actor, id); err != nil {
			writeNamedInventoryViewError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeNamedInventoryViewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrNamedInventoryViewValidation):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrNamedInventoryViewForbidden):
		writeJSONError(w, http.StatusForbidden, "forbidden", "admin role required")
	case errors.Is(err, service.ErrNamedInventoryViewNotFound):
		writeJSONError(w, http.StatusNotFound, "not_found", "named inventory view not found")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
