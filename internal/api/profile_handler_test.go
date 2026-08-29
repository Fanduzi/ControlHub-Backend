// Package api provides HTTP tests for profile write endpoints.
// input: internal/api test server, internal/model, net/http, net/http/httptest, encoding/json
// output: TestPutResourceProfile_*, TestPatchResourceProfile_*, TestPutAndPatchDomainNameProfileNormalizeAndRejectResolutionTarget
// pos: Validates PUT/PATCH profile writes including Domain Name FQDN normalize and resolution-target rejection
// output: TestPutResourceProfile_*, TestPatchResourceProfile_*
// pos: Validates PUT full-replacement, PATCH partial-merge, strict JSON decoding, and field validation at the HTTP seam including Database Proxy identity
// note: if this file changes, update header and README.md
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func resourceIDPath(id uint64) string {
	return "/resources/" + strconv.FormatUint(id, 10) + "/profile"
}

func getProfile(t *testing.T, server *TestServer, id uint64) model.ResourceProfileResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, resourceIDPath(id), nil)
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get profile status = %d body=%s", rec.Code, rec.Body.String())
	}
	var profile model.ResourceProfileResponse
	if err := json.NewDecoder(rec.Body).Decode(&profile); err != nil {
		t.Fatalf("decode profile: %v", err)
	}
	return profile
}

func putProfile(t *testing.T, server *TestServer, id uint64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, resourceIDPath(id), strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	return rec
}

func patchProfile(t *testing.T, server *TestServer, id uint64, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, resourceIDPath(id), strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	return rec
}

// ---------- PUT: explicit full replacement ----------

func TestPutResourceProfile_ReplacesAllFields(t *testing.T) {
	server := NewTestServer()
	rec := putProfile(t, server, 6, `{"hostname":"renamed-host","ipAddress":"10.9.9.9","osName":"Rocky 9"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	profile := getProfile(t, server, 6)
	if profile.Profile["hostname"] != "renamed-host" || profile.Profile["ipAddress"] != "10.9.9.9" || profile.Profile["osName"] != "Rocky 9" {
		t.Fatalf("expected full replacement, got %#v", profile.Profile)
	}
}

func TestPutResourceProfile_EmptyBodyClearsFields(t *testing.T) {
	server := NewTestServer()
	rec := putProfile(t, server, 6, `{}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	profile := getProfile(t, server, 6)
	if profile.Profile["hostname"] != "" || profile.Profile["ipAddress"] != "" || profile.Profile["osName"] != "" {
		t.Fatalf("expected empty body to clear all fields, got %#v", profile.Profile)
	}
}

func TestPutResourceProfile_RejectsUnknownField(t *testing.T) {
	server := NewTestServer()
	rec := putProfile(t, server, 6, `{"hostname":"x","bogus":"y"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp apiErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if resp.Error != "validation_failed" {
		t.Fatalf("expected validation_failed error code, got %q", resp.Error)
	}
}

func TestPutResourceProfile_RejectsOverlongValue(t *testing.T) {
	server := NewTestServer()
	body := `{"hostname":"` + strings.Repeat("h", 256) + `"}`
	rec := putProfile(t, server, 6, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for overlong value, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutResourceProfile_RejectsTrailingJSON(t *testing.T) {
	server := NewTestServer()
	rec := putProfile(t, server, 6, `{"hostname":"x"} trailing`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for trailing JSON, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutResourceProfile_RejectsMultipleJSONValues(t *testing.T) {
	server := NewTestServer()
	rec := putProfile(t, server, 6, `{"hostname":"x"} {"hostname":"y"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for multiple JSON values, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// ---------- PATCH: partial merge ----------

func TestPatchResourceProfile_PreservesOmittedFields(t *testing.T) {
	server := NewTestServer()
	rec := patchProfile(t, server, 6, `{"hostname":"patched-host"}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d body=%s", rec.Code, rec.Body.String())
	}
	profile := getProfile(t, server, 6)
	if profile.Profile["hostname"] != "patched-host" {
		t.Fatalf("expected patched hostname, got %#v", profile.Profile)
	}
	if profile.Profile["ipAddress"] != "10.0.10.21" {
		t.Fatalf("expected omitted ipAddress preserved as 10.0.10.21, got %#v", profile.Profile)
	}
	if profile.Profile["osName"] != "Ubuntu 24.04" {
		t.Fatalf("expected omitted osName preserved as Ubuntu 24.04, got %#v", profile.Profile)
	}
}

func TestPatchResourceProfile_EmptyBodyIsNoOp(t *testing.T) {
	server := NewTestServer()
	before := getProfile(t, server, 6)
	rec := patchProfile(t, server, 6, `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 no-op for empty PATCH per the approved design spec, got %d body=%s", rec.Code, rec.Body.String())
	}
	after := getProfile(t, server, 6)
	if after.Profile["hostname"] != before.Profile["hostname"] || after.Profile["ipAddress"] != before.Profile["ipAddress"] {
		t.Fatalf("expected empty PATCH to be a no-op, before=%#v after=%#v", before.Profile, after.Profile)
	}
}

func TestPatchResourceProfile_RejectsUnknownField(t *testing.T) {
	server := NewTestServer()
	rec := patchProfile(t, server, 6, `{"hostname":"x","bogus":"y"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutResourceProfile_RejectsNullBody(t *testing.T) {
	server := NewTestServer()
	rec := putProfile(t, server, 6, `null`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for null body, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPatchResourceProfile_RejectsNullBody(t *testing.T) {
	server := NewTestServer()
	rec := patchProfile(t, server, 6, `null`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for null body, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func createDomainName(t *testing.T, server *TestServer) uint64 {
	body := `{
		"resourceType":"domain_name",
		"resourceSubtype":"dns",
		"name":"profile-domain-01",
		"displayName":"Profile Domain",
		"environmentId":1,
		"ownerId":2,
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"profile":{"fqdn":"initial.example.com"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create domain name: expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp model.Resource
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created domain name: %v", err)
	}
	return resp.ID
}

func createDatabaseProxy(t *testing.T, server *TestServer) uint64 {
	t.Helper()
	body := `{
		"resourceType":"database_proxy",
		"resourceSubtype":"proxysql",
		"name":"profile-proxy-01",
		"displayName":"Profile Proxy",
		"environmentId":1,
		"ownerId":2,
		"lifecycleStatus":"running",
		"healthStatus":"healthy",
		"source":"manual",
		"profile":{"technologySubtype":"proxysql","host":"proxy-initial","port":6033,"role":"active"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/resources", strings.NewReader(body))
	rec := httptest.NewRecorder()
	server.Router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create database proxy: expected 201, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp model.Resource
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode created database proxy: %v", err)
	}
	return resp.ID
}

func TestPutAndPatchDomainNameProfileNormalizeAndRejectResolutionTarget(t *testing.T) {
	server := NewTestServer()
	id := createDomainName(t, server)

	putRec := putProfile(t, server, id, `{"fqdn":"Orders.Example.COM."}`)
	if putRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for domain name PUT, got %d body=%s", putRec.Code, putRec.Body.String())
	}
	profile := getProfile(t, server, id)
	if profile.Profile["fqdn"] != "orders.example.com" {
		t.Fatalf("expected PUT to persist normalized fqdn, got %#v", profile.Profile)
	}

	patchRec := patchProfile(t, server, id, `{"fqdn":"API.Example.COM."}`)
	if patchRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for domain name PATCH, got %d body=%s", patchRec.Code, patchRec.Body.String())
	}
	profile = getProfile(t, server, id)
	if profile.Profile["fqdn"] != "api.example.com" {
		t.Fatalf("expected PATCH to persist normalized fqdn, got %#v", profile.Profile)
	}

	reject := putProfile(t, server, id, `{"fqdn":"orders.example.com","resolutionTarget":"10.0.0.10"}`)
	if reject.Code == http.StatusNoContent {
		t.Fatal("resolution target must not be accepted as profile text")
	}
}

func TestPutAndPatchDatabaseProxyProfileRejectsPrimaryRole(t *testing.T) {
	server := NewTestServer()
	id := createDatabaseProxy(t, server)

	putRec := putProfile(t, server, id, `{"technologySubtype":"proxysql","host":"proxy-prod-01","port":6033,"role":"standby","version":"2.5.5"}`)
	if putRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for database_proxy PUT, got %d body=%s", putRec.Code, putRec.Body.String())
	}
	profile := getProfile(t, server, id)
	if profile.Profile["role"] != "standby" || profile.Profile["host"] != "proxy-prod-01" {
		t.Fatalf("expected PUT to persist proxy identity, got %#v", profile.Profile)
	}

	reject := putProfile(t, server, id, `{"technologySubtype":"proxysql","host":"proxy-prod-01","port":6033,"role":"primary"}`)
	if reject.Code == http.StatusNoContent {
		t.Fatal("primary role must not be accepted for database_proxy")
	}
}
