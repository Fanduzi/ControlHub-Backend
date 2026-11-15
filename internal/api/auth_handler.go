// Package api provides HTTP handlers and routing for the ControlHub REST API.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

func handleLogin(authService *service.AuthService, emitter service.AuthAuditEmitter, limiter *loginRateLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, loginBodyMaxBytes)
		var req model.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_payload", "invalid payload")
			return
		}

		if !limiter.allow(r.RemoteAddr, req.Email) {
			writeLoginRateLimited(w)
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

		// Succeeded login: emit event with the verified actor ID. The user was
		// authenticated by the service layer; this is a trusted, verified identity.
		emitter.EmitAuthAudit("auth.login", "succeeded", &resp.UserID, nil)
		writeJSON(w, http.StatusOK, resp)
	}
}
