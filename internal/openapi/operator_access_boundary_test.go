// Package openapi_test verifies the embedded OpenAPI contract.
// input: embedded OpenAPI YAML, kin-openapi parser, internal/testsupport/operatoraccess
// output: TestOpenAPIUsesTheOperatorAccessBoundary
// pos: Proves every protected operation documents the status codes its operatoraccess class requires
// note: if this file changes, update header and README.md
package openapi_test

import (
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/fan/controlhub/internal/openapi"
	"github.com/fan/controlhub/internal/testsupport/operatoraccess"
)

// TestOpenAPIUsesTheOperatorAccessBoundary proves every protected operation in
// the shared operatoraccess policy documents the status codes its class
// requires: 401 everywhere; 403 on router/handler-admin operations; 403 and
// 404 on the conditional saved-statement mutations. Editor-readable and
// fresh-any-role operations must not document a role-only 403 (a fresh-any-role
// 403 is allowed only for non-role policy blocks like guard/disclosure denial).
func TestOpenAPIUsesTheOperatorAccessBoundary(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	if len(doc.Security) != 1 {
		t.Fatalf("expected bearerAuth to protect operations by default, got %#v", doc.Security)
	}
	if scopes, ok := doc.Security[0]["bearerAuth"]; !ok || len(scopes) != 0 || len(doc.Security[0]) != 1 {
		t.Fatalf("expected only bearerAuth with no scopes, got %#v", doc.Security)
	}
	for _, path := range []string{"/auth/login", "/health"} {
		operation := doc.Paths.Value(path).Get
		if path == "/auth/login" {
			operation = doc.Paths.Value(path).Post
		}
		if operation.Security == nil || len(*operation.Security) != 0 {
			t.Fatalf("%s must explicitly remain public, got %#v", path, operation.Security)
		}
	}

	for _, op := range operatoraccess.All() {
		operation := openapiOperation(t, doc, op)
		for _, status := range op.Class.RequiredOpenAPIResponses() {
			if operation.Responses.Value(status) == nil {
				t.Fatalf("%s %s must document %s for class %s", op.Method, op.Path, status, op.Class)
			}
		}
		switch op.Class {
		case operatoraccess.AuthenticatedRead:
			if operation.Responses.Value("403") != nil {
				t.Fatalf("%s %s must not document a role-only 403 (editor-readable)", op.Method, op.Path)
			}
		case operatoraccess.FreshAnyRole:
			if resp := operation.Responses.Value("403"); resp != nil && resp.Value != nil && resp.Value.Description != nil &&
				strings.Contains(strings.ToLower(*resp.Value.Description), "admin") {
				t.Fatalf("%s %s documents a role-only 403: %q", op.Method, op.Path, *resp.Value.Description)
			}
		}
	}
}

func openapiOperation(t *testing.T, doc *openapi3.T, op operatoraccess.Operation) *openapi3.Operation {
	t.Helper()
	pathItem := doc.Paths.Value(op.Path)
	if pathItem == nil {
		t.Fatalf("path %q not found in openapi.yaml", op.Path)
	}
	var operation *openapi3.Operation
	switch op.Method {
	case "GET":
		operation = pathItem.Get
	case "POST":
		operation = pathItem.Post
	case "PATCH":
		operation = pathItem.Patch
	case "PUT":
		operation = pathItem.Put
	case "DELETE":
		operation = pathItem.Delete
	}
	if operation == nil {
		t.Fatalf("operation %s %s not found in openapi.yaml", op.Method, op.Path)
	}
	return operation
}
