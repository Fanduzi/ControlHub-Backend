// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: internal/api, internal/model, net/http, net/http/httptest, encoding/json
// output: TestLogin, TestLoginInvalidPassword, TestLoginRejectsMissingEmail, TestLoginRejectsMissingPassword, TestLoginRejectsMalformedJSON
// pos: Validates login success and failure flows
// note: if this file changes, update header and README.md
package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestLoginInvalidPassword(t *testing.T) {
	server := NewTestServer()
	body := bytes.NewBufferString(`{"email":"admin@example.com","password":"wrong"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusUnauthorized, "invalid_credentials")
}

func TestLoginRejectsMissingEmail(t *testing.T) {
	server := NewTestServer()
	body := bytes.NewBufferString(`{"password":"secret123"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusUnauthorized, "invalid_credentials")
}

func TestLoginRejectsMissingPassword(t *testing.T) {
	server := NewTestServer()
	body := bytes.NewBufferString(`{"email":"admin@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusUnauthorized, "invalid_credentials")
}

func TestLoginRejectsMalformedJSON(t *testing.T) {
	server := NewTestServer()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Router.ServeHTTP(rec, req)

	assertAPIError(t, rec, http.StatusBadRequest, "invalid_payload")
}
