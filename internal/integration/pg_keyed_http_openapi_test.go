//go:build integration

// Package integration — PG-HTTP-T12-R2 contract proof: real responses from the enabled PG assembly validated against the repository's current internal/openapi/openapi.yaml via kin-openapi, plus load-bearing negative controls on tampered copies.
// input: the enabled fixture's real router, disposable PG16, embedded openapi.YAML parsed by kin-openapi loaders and the legacy router
// output: TestPGKeyedHTTP_OpenAPIResponseContract — per-route 200 + one reachable controlled error validated per operation/status/content-type, and tampered copies of a real success body rejected by the same validator
// pos: R2-B — replaces subset-struct decoding as the public-envelope proof; cross-field semantics OpenAPI cannot express stay in the adjacent tests
// note: if this file changes, update this header and module README.md.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/openapi"
)

// pgOpenAPIRouter resolves real requests to their OpenAPI operations — the
// same contract the fuzz gate serves.
func pgOpenAPIRouter(t *testing.T) routers.Router {
	t.Helper()
	doc, err := openapi3.NewLoader().LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml invalid: %v", err)
	}
	rtr, err := legacyrouter.NewRouter(doc)
	if err != nil {
		t.Fatalf("build openapi router: %v", err)
	}
	return rtr
}

// validateResp runs one real response through openapi3filter
// ValidateResponse — resolved by the request's own operation/method/status/
// content-type against the full schema, never a partial Go struct.
func validateResp(rtr routers.Router, method, rawPath string, rec *httptest.ResponseRecorder) error {
	req, err := http.NewRequest(method, "http://localhost:8080"+rawPath, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	route, pathParams, err := rtr.FindRoute(req)
	if err != nil {
		return fmt.Errorf("find route %s %s: %w", method, rawPath, err)
	}
	rvi := &openapi3filter.RequestValidationInput{
		Request:    req,
		PathParams: pathParams,
		Route:      route,
	}
	rvi2 := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: rvi,
		Status:                 rec.Code,
		Header:                 rec.Header(),
		Body:                   io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
	}
	return openapi3filter.ValidateResponse(context.Background(), rvi2)
}

// expectValid asserts a real response satisfies its documented contract.
func expectValid(t *testing.T, rtr routers.Router, method, path string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if err := validateResp(rtr, method, path, rec); err != nil {
		t.Fatalf("%s %s status %d failed OpenAPI validation: %v\nbody=%s", method, path, rec.Code, err, rec.Body.String())
	}
}

// expectInvalid asserts a tampered copy is rejected — the validator is
// load-bearing, not a rubber stamp.
func expectInvalid(t *testing.T, rtr routers.Router, method, path string, status int, body []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	rec.Code = status
	rec.Header().Set("Content-Type", "application/json")
	_, _ = rec.Body.Write(body)
	if err := validateResp(rtr, method, path, rec); err == nil {
		t.Fatalf("tampered response passed OpenAPI validation — validator not load-bearing\nbody=%s", body)
	}
}

// mutateJSON copies a real response body and applies one corruption.
func mutateJSON(t *testing.T, src []byte, fn func(map[string]any)) []byte {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(src, &m); err != nil {
		t.Fatalf("copy response body: %v", err)
	}
	fn(m)
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("re-encode tampered body: %v", err)
	}
	return out
}

func TestPGKeyedHTTP_OpenAPIResponseContract(t *testing.T) {
	fx := setupPGHTTPFixture(t)
	rtr := pgOpenAPIRouter(t)

	saved := createSavedPG(t, fx.db, fx.userID, fx.targetID, model.QuerySavedStatementCreateRequest{
		Name: "openapi-saved", Statement: `SELECT id FROM app.orders WHERE id = 1`,
		Scope: model.QuerySavedStatementPersonal,
	})

	// --- Ordinary execute: real success + reachable controlled error.
	execOK := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id, qty FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"r2-oa-x-ok"}`)
	if execOK.Code != http.StatusOK {
		t.Fatalf("execute success: %d; body=%s", execOK.Code, execOK.Body.String())
	}
	expectValid(t, rtr, http.MethodPost, fmt.Sprintf("/query-targets/%d/execute", fx.targetID), execOK)

	execErr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id FROM app.type_probe WHERE id = 1","database":"discdb","clientExecutionId":"r2-oa-x-err"}`)
	if execErr.Code != http.StatusForbidden {
		t.Fatalf("execute disclosure error: %d; body=%s", execErr.Code, execErr.Body.String())
	}
	expectValid(t, rtr, http.MethodPost, fmt.Sprintf("/query-targets/%d/execute", fx.targetID), execErr)

	// --- Saved-statement execute: real success + controlled error (404
	// saved_statement_not_found on a missing id — a reachable envelope).
	savedOK := doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, saved.ID), fx.token,
		`{"clientExecutionId":"r2-oa-s-ok"}`)
	if savedOK.Code != http.StatusOK {
		t.Fatalf("saved success: %d; body=%s", savedOK.Code, savedOK.Body.String())
	}
	expectValid(t, rtr, http.MethodPost,
		fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", fx.targetID, saved.ID), savedOK)

	savedErr := doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, 99999999), fx.token,
		`{"clientExecutionId":"r2-oa-s-err"}`)
	if savedErr.Code != http.StatusNotFound {
		t.Fatalf("saved not-found: %d; body=%s", savedErr.Code, savedErr.Body.String())
	}
	expectValid(t, rtr, http.MethodPost,
		fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", fx.targetID, 99999999), savedErr)

	// --- Related-records navigation: its own response schema carries the
	// source/referenced scope fields.
	navOK := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.nav, fx.token,
		`{"source":{"database":"discdb","schema":"app","object":"order_items","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":"r2-oa-n-ok"}`)
	if navOK.Code != http.StatusOK {
		t.Fatalf("nav success: %d; body=%s", navOK.Code, navOK.Body.String())
	}
	expectValid(t, rtr, http.MethodPost, fmt.Sprintf("/query-targets/%d/related-records", fx.targetID), navOK)

	navErr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.nav, fx.token,
		`{"source":{"database":"discdb","schema":"app","object":"no_such_object","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":"r2-oa-n-err"}`)
	if navErr.Code != http.StatusBadRequest {
		t.Fatalf("nav unknown object: %d; body=%s", navErr.Code, navErr.Body.String())
	}
	expectValid(t, rtr, http.MethodPost, fmt.Sprintf("/query-targets/%d/related-records", fx.targetID), navErr)

	// --- Load-bearing reverse controls on copies of the real success body:
	// delete a required field, break durationMs's type, break limitApplied's
	// type — the same validator must reject each while the original passed.
	okBody := execOK.Body.Bytes()
	expectInvalid(t, rtr, http.MethodPost, fmt.Sprintf("/query-targets/%d/execute", fx.targetID), http.StatusOK,
		mutateJSON(t, okBody, func(m map[string]any) { delete(m, "executionId") }))
	expectInvalid(t, rtr, http.MethodPost, fmt.Sprintf("/query-targets/%d/execute", fx.targetID), http.StatusOK,
		mutateJSON(t, okBody, func(m map[string]any) { m["durationMs"] = "18" }))
	expectInvalid(t, rtr, http.MethodPost, fmt.Sprintf("/query-targets/%d/execute", fx.targetID), http.StatusOK,
		mutateJSON(t, okBody, func(m map[string]any) { m["limitApplied"] = "51" }))
	// Error envelope representative: ErrorResponse requires `error`.
	expectInvalid(t, rtr, http.MethodPost, fmt.Sprintf("/query-targets/%d/execute", fx.targetID), http.StatusForbidden,
		mutateJSON(t, execErr.Body.Bytes(), func(m map[string]any) { delete(m, "error") }))
}
