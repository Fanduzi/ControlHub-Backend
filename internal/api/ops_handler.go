// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: encoding/json, net/http, internal/repository/mysql
// output: handleAuthAuditMetrics
// pos: Admin-only operational metrics endpoint for auth audit persistence failures
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"

	"github.com/fan/controlhub/internal/repository/mysql"
)

// handleAuthAuditMetrics returns the auth audit persistence-failure counter.
// Admin-only; returns a fixed JSON shape with no identity, request values,
// or internal details.
func handleAuthAuditMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			AuthAuditPersistenceFailures int64 `json:"authAuditPersistenceFailures"`
		}{
			AuthAuditPersistenceFailures: mysql.AuthAuditPersistenceFailures.Value(),
		})
	}
}
