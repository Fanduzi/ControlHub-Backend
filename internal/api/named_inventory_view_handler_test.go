// Package api tests named inventory view HTTP handlers.
// input: net/http, net/http/httptest, testing, internal/model, internal/service
// output: named inventory view authentication, list/create router contract tests
// pos: Covers route authentication, view response metadata, strict create decoding, and controlled errors
// note: if this file changes, update this header and README.md.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type fakeNamedInventoryViewService struct {
	items     []model.NamedInventoryView
	create    model.NamedInventoryView
	createErr error
	updateErr error
}

func (f *fakeNamedInventoryViewService) List(context.Context, service.AuthenticatedUser) ([]model.NamedInventoryView, error) {
	return f.items, nil
}

func (f *fakeNamedInventoryViewService) Create(context.Context, service.AuthenticatedUser, model.NamedInventoryViewCreateRequest) (model.NamedInventoryView, error) {
	return f.create, f.createErr
}

func (f *fakeNamedInventoryViewService) Update(context.Context, service.AuthenticatedUser, uint64, model.NamedInventoryViewUpdateRequest) error {
	return f.updateErr
}

func (f *fakeNamedInventoryViewService) Delete(context.Context, service.AuthenticatedUser, uint64) error {
	return nil
}

func namedInventoryViewRouter(svc namedInventoryViewAPI) http.Handler {
	srv := NewTestServer()
	srv.deps.NamedInventoryViewService = svc
	return NewRouter(srv.deps)
}

func TestNamedInventoryViewListIncludesCanManageShared(t *testing.T) {
	router := namedInventoryViewRouter(&fakeNamedInventoryViewService{items: []model.NamedInventoryView{{ID: 1, Name: "My view", Scope: model.NamedInventoryViewPersonal}}})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, ssRequest(http.MethodGet, "/inventory/views", "", ssAdminToken(t)))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /inventory/views = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var response struct {
		Items           []model.NamedInventoryView `json:"items"`
		CanManageShared bool                       `json:"canManageShared"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(response.Items) != 1 || !response.CanManageShared {
		t.Fatalf("response = %+v, want one item and canManageShared=true", response)
	}
}

func TestNamedInventoryViewRoutesRequireAuthentication(t *testing.T) {
	router := namedInventoryViewRouter(&fakeNamedInventoryViewService{})
	for _, request := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/inventory/views", ""},
		{http.MethodPost, "/inventory/views", namedInventoryViewRequestBody},
		{http.MethodPut, "/inventory/views/1", namedInventoryViewUpdateRequestBody},
		{http.MethodDelete, "/inventory/views/1", ""},
	} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, ssRequest(request.method, request.path, request.body, ""))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s = %d, want 401: %s", request.method, request.path, rec.Code, rec.Body.String())
		}
	}
}

func TestNamedInventoryViewCreateRejectsUnknownJSONField(t *testing.T) {
	router := namedInventoryViewRouter(&fakeNamedInventoryViewService{})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, ssRequest(http.MethodPost, "/inventory/views", `{"name":"My view","scope":"personal","state":{"filters":{},"sort":{"field":"name","direction":"asc"},"columns":["name"]},"ownerUserId":42}`, ssAdminToken(t)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("POST /inventory/views with unknown field = %d, want 400: %s", rec.Code, rec.Body.String())
	}
}

func TestNamedInventoryViewCreateMapsServiceForbidden(t *testing.T) {
	router := namedInventoryViewRouter(&fakeNamedInventoryViewService{createErr: service.ErrNamedInventoryViewForbidden})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, ssRequest(http.MethodPost, "/inventory/views", namedInventoryViewRequestBody, ssAdminToken(t)))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("POST /inventory/views forbidden = %d, want 403: %s", rec.Code, rec.Body.String())
	}
}

func TestNamedInventoryViewUpdateMapsServiceNotFound(t *testing.T) {
	router := namedInventoryViewRouter(&fakeNamedInventoryViewService{updateErr: service.ErrNamedInventoryViewNotFound})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, ssRequest(http.MethodPut, "/inventory/views/1", namedInventoryViewUpdateRequestBody, ssAdminToken(t)))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("PUT /inventory/views/1 not found = %d, want 404: %s", rec.Code, rec.Body.String())
	}
}

func TestNamedInventoryViewCreateReturnsCreated(t *testing.T) {
	router := namedInventoryViewRouter(&fakeNamedInventoryViewService{create: model.NamedInventoryView{ID: 1, Name: "My view", Scope: model.NamedInventoryViewPersonal}})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, ssRequest(http.MethodPost, "/inventory/views", namedInventoryViewRequestBody, ssAdminToken(t)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /inventory/views = %d, want 201: %s", rec.Code, rec.Body.String())
	}
}

const namedInventoryViewRequestBody = `{"name":"My view","scope":"personal","state":{"filters":{"q":"orders"},"sort":{"field":"name","direction":"asc"},"columns":["name"]}}`

const namedInventoryViewUpdateRequestBody = `{"name":"My view","state":{"filters":{"q":"orders"},"sort":{"field":"name","direction":"asc"},"columns":["name"]}}`
