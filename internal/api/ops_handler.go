// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: encoding/json, net/http, internal/repository/mysql, internal/service
// output: handleAuthAuditMetrics, handleQueryEvidenceMetrics (query-evidence counter read through the service layer)
// pos: Admin-only operational metrics endpoints for auth audit persistence failures, untrusted-Bearer suppression, and query-evidence persistence failures (Issue #34)
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"

	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// handleAuthAuditMetrics returns the auth audit persistence-failure and
// untrusted-Bearer suppression counters. Admin-only; returns a fixed JSON
// shape with no identity, request values, or internal details.
func handleAuthAuditMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			AuthAuditPersistenceFailures  int64 `json:"authAuditPersistenceFailures"`
			AuthAuditSuppressedRejections int64 `json:"authAuditSuppressedRejections"`
		}{
			AuthAuditPersistenceFailures:  mysql.AuthAuditPersistenceFailures.Value(),
			AuthAuditSuppressedRejections: service.AuthAuditSuppressedRejections.Value(),
		})
	}
}

// handleQueryEvidenceMetrics returns the query-evidence persistence-failure
// counter (Issue #34). Admin-only; the response contains exactly the one fixed
// field queryEvidencePersistenceFailures — no identity, target, statement,
// value, credential, DSN, request, or raw error data. The counter is read
// through the service layer; the api package never touches repository state.
func handleQueryEvidenceMetrics(q queryExecutionAPI) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(struct {
			QueryEvidencePersistenceFailures int64 `json:"queryEvidencePersistenceFailures"`
		}{
			QueryEvidencePersistenceFailures: q.QueryEvidencePersistenceFailures(),
		})
	}
}
