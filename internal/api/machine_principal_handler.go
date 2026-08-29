// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, errors, net/http, chi/v5, internal/model, internal/service
// output: safe machine-principal lifecycle list, administration handlers, and controlled errors
// pos: HTTP boundary for one-time machine credential issuance and lifecycle
// note: if this file changes, update this header and module README.md.
package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
	"github.com/go-chi/chi/v5"
)

type machinePrincipalAPI interface {
	List(context.Context, service.AuthenticatedUser) ([]model.MachinePrincipalListItem, error)
	Create(context.Context, service.AuthenticatedUser, model.MachinePrincipalCreateRequest) (model.MachineCredentialIssue, error)
	Rotate(context.Context, service.AuthenticatedUser, uint64, model.MachineCredentialRotateRequest) (model.MachineCredentialIssue, error)
	Revoke(context.Context, service.AuthenticatedUser, uint64) error
}

func handleListMachinePrincipals(svc machinePrincipalAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		items, err := svc.List(r.Context(), actor)
		if err != nil {
			writeMachinePrincipalError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Items []model.MachinePrincipalListItem `json:"items"`
		}{items})
	}
}

func handleCreateMachinePrincipal(svc machinePrincipalAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		var req model.MachinePrincipalCreateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		issue, err := svc.Create(r.Context(), actor, req)
		if err != nil {
			writeMachinePrincipalError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, issue)
	}
}

func handleRotateMachineCredential(svc machinePrincipalAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		id, err := parseUint64IDParam(chi.URLParam(r, "credentialId"), "credential id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		var req model.MachineCredentialRotateRequest
		if err := decodeJSONBody(r, &req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", "invalid request payload")
			return
		}
		issue, err := svc.Rotate(r.Context(), actor, id, req)
		if err != nil {
			writeMachinePrincipalError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, issue)
	}
}

func handleRevokeMachineCredential(svc machinePrincipalAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFromContext(r.Context())
		if !ok {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "authenticated actor missing")
			return
		}
		id, err := parseUint64IDParam(chi.URLParam(r, "credentialId"), "credential id")
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		if err := svc.Revoke(r.Context(), actor, id); err != nil {
			writeMachinePrincipalError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func writeMachinePrincipalError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrMachinePrincipalForbidden):
		writeJSONError(w, http.StatusForbidden, "forbidden", "admin role is required")
	case errors.Is(err, service.ErrMachinePrincipalValidation):
		writeJSONError(w, http.StatusBadRequest, "validation_failed", err.Error())
	case errors.Is(err, service.ErrMachineCredentialNotFound):
		writeJSONError(w, http.StatusNotFound, "not_found", "machine credential not found")
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal_error", "internal server error")
	}
}
