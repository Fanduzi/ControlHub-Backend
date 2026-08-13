// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http, internal/service
// output: handleGetLegacyHashCount
// pos: Admin-only handler for the non-identity-bearing legacy password hash count
// note: if this file changes, update header and README.md
package api

import (
	"net/http"

	"github.com/fan/controlhub/internal/service"
)

// handleGetLegacyHashCount returns the number of users whose stored password
// hash is not Argon2id. The response carries only an integer count — no
// identity-bearing information. Admin-only.
func handleGetLegacyHashCount(authService *service.AuthService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		count, err := authService.LegacyHashCount()
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "internal_error", "unexpected server failure")
			return
		}
		writeJSON(w, http.StatusOK, struct {
			Count int64 `json:"count"`
		}{Count: count})
	}
}
