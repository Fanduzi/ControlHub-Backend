// Package api provides HTTP handlers and routing for the ControlHub REST API.
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthRoute(t *testing.T) {
	router := NewRouter(Dependencies{})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	expected := "{\"status\":\"ok\"}"
	if got := rec.Body.String(); got != expected {
		t.Fatalf("expected %s, got %s", expected, got)
	}
}
