package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListResources(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodGet, "/resources?type=database_instance", nil)
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if body := rec.Body.String(); body == "" {
		t.Fatal("expected response body")
	}
}
