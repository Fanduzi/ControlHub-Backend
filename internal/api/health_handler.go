// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: net/http
// output: handleHealth
// pos: Liveness check endpoint
// note: if this file changes, update header and README.md
package api

import "net/http"

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
