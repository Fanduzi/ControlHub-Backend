// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: TestListResourceRelations
// pos: Validates relation listing per resource
// note: if this file changes, update header and README.md
package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListResourceRelations(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources/res-1/relations", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if body := rec.Body.String(); body == "" {
		t.Fatal("expected response body")
	}
}
