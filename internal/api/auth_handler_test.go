package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLogin(t *testing.T) {
	server := NewTestServer()
	body := bytes.NewBufferString(`{"email":"admin@example.com","password":"secret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}
