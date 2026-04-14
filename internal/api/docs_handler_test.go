package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleOpenAPIYAML(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	rec := httptest.NewRecorder()

	server := NewTestServer()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "openapi:") {
		t.Error("response should contain 'openapi:' marker")
	}
	if !strings.Contains(body, "paths:") {
		t.Error("response should contain 'paths:' marker")
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "yaml") && !strings.Contains(ct, "octet-stream") {
		t.Errorf("expected yaml content type, got %q", ct)
	}
}

func TestHandleDocs(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/docs", nil)
	rec := httptest.NewRecorder()

	server := NewTestServer()
	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "/openapi.yaml") {
		t.Error("docs page should reference /openapi.yaml")
	}
	if !strings.Contains(body, "ControlHub") {
		t.Error("docs page should mention ControlHub")
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Errorf("expected text/html content type, got %q", ct)
	}
}
