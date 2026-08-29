// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: context, errors, net/http, strings, internal/model, internal/service
// output: machine-credential authentication, user-or-machine route guard, and identity context
// pos: Opaque non-browser credential and scoped-route boundary, independent of user sessions
// note: if this file changes, update this header and module README.md.
package api

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type machinePrincipalContextKey struct{}

type machineCredentialAPI interface {
	Authenticate(context.Context, string, model.MachineScope) (model.MachinePrincipalIdentity, error)
}

func machinePrincipalFromContext(ctx context.Context) (model.MachinePrincipalIdentity, bool) {
	identity, ok := ctx.Value(machinePrincipalContextKey{}).(model.MachinePrincipalIdentity)
	return identity, ok
}

func requireMachineCredential(svc machineCredentialAPI, scope model.MachineScope) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "Bearer "
			token := strings.TrimPrefix(r.Header.Get("Authorization"), prefix)
			if token == r.Header.Get("Authorization") {
				writeJSONError(w, http.StatusUnauthorized, "machine_credential_invalid", "machine credential is required")
				return
			}
			identity, err := svc.Authenticate(r.Context(), token, scope)
			if err != nil {
				writeMachineCredentialError(w, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), machinePrincipalContextKey{}, identity)))
		})
	}
}

func requireUserOrMachineCredential(svc machineCredentialAPI, scope model.MachineScope, requireUser func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		machine := requireMachineCredential(svc, scope)(next)
		user := requireUser(next)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer chmp_") {
				machine.ServeHTTP(w, r)
				return
			}
			user.ServeHTTP(w, r)
		})
	}
}

func writeMachineCredentialError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrMachineScopeDenied):
		writeJSONError(w, http.StatusForbidden, "machine_scope_denied", "machine credential scope is not permitted")
	case errors.Is(err, service.ErrMachineCredentialExpired):
		writeJSONError(w, http.StatusUnauthorized, "machine_credential_expired", "machine credential is not active")
	case errors.Is(err, service.ErrMachineCredentialRevoked):
		writeJSONError(w, http.StatusUnauthorized, "machine_credential_revoked", "machine credential is not active")
	default:
		writeJSONError(w, http.StatusUnauthorized, "machine_credential_invalid", "machine credential is not valid")
	}
}
