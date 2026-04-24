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

func handleLogin(authService *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req model.LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_payload", "invalid payload")
			return
		}

		resp, err := authService.Login(req.Email, req.Password)
		if err != nil {
			if errors.Is(err, service.ErrInvalidCredentials) {
				writeJSONError(w, http.StatusUnauthorized, "invalid_credentials", err.Error())
				return
			}
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
			return
		}

		writeJSON(w, http.StatusOK, resp)
	}
}
