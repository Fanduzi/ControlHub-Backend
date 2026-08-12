// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, internal/service, internal/model
// output: handleLogin
// pos: HTTP handler for POST /auth/login
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleLogin(authService *service.AuthService, emitter service.AuthAuditEmitter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_payload", "invalid payload")
			return
		}

		resp, err := authService.Login(req.Email, req.Password)
		if err != nil {
			if errors.Is(err, service.ErrInvalidCredentials) {
				emitter.EmitAuthAudit("auth.login", "rejected", nil, nil)
				writeJSONError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
			return
		}

		// Succeeded login: emit event. The actor user id is intentionally omitted
		// from the audit record because the Login response already returns the
		// token; operators correlate via timestamp + email from the login request
		// log. No identity or credential data is written to the audit row.
		emitter.EmitAuthAudit("auth.login", "succeeded", nil, nil)
		writeJSON(w, http.StatusOK, resp)
	}
}
