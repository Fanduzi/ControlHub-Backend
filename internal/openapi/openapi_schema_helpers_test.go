// Package openapi_test verifies the embedded OpenAPI contract.
// input: kin-openapi parser, parsed OpenAPI document
// output: shared schema/parameter shape assertion helpers
// pos: Single source of shape assertions for the OpenAPI contract tests
// note: if this file changes, update header and README.md
package openapi_test

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func assertPathParamInt64(t *testing.T, doc *openapi3.T, path string, method string, paramName string) {
	t.Helper()

	schema := findOperationParamSchema(t, doc, path, method, paramName)
	assertIntegerInt64(t, path+" "+method+" parameter "+paramName, schema)
}

func assertSchemaPropertyInt64(t *testing.T, doc *openapi3.T, schemaName string, propertyName string) {
	t.Helper()

	property := findSchemaProperty(t, doc, schemaName, propertyName)
	assertIntegerInt64(t, schemaName+"."+propertyName, property)
}

func assertSchemaPropertyString(t *testing.T, doc *openapi3.T, schemaName string, propertyName string) {
	t.Helper()

	property := findSchemaProperty(t, doc, schemaName, propertyName)
	if property.Type == nil || !property.Type.Is("string") {
		t.Fatalf("expected %s.%s type string, got %#v", schemaName, propertyName, property.Type)
	}
}

func assertQueryParamString(t *testing.T, doc *openapi3.T, path string, method string, paramName string) {
	t.Helper()

	schema := findOperationParamSchema(t, doc, path, method, paramName)
	if schema.Type == nil || !schema.Type.Is("string") {
		t.Fatalf("expected %s %s query parameter %s type string, got %#v", path, method, paramName, schema.Type)
	}
}

func assertQueryParamInt64(t *testing.T, doc *openapi3.T, path string, method string, paramName string) {
	t.Helper()

	schema := findOperationParamSchema(t, doc, path, method, paramName)
	assertIntegerInt64(t, path+" "+method+" query parameter "+paramName, schema)
}

func assertQueryParamIntegerBounds(t *testing.T, doc *openapi3.T, path string, method string, paramName string, defaultValue int, minimum int, maximum int) {
	t.Helper()

	schema := findOperationParamSchema(t, doc, path, method, paramName)
	if schema.Type == nil || !schema.Type.Is("integer") {
		t.Fatalf("expected %s %s query parameter %s type integer, got %#v", path, method, paramName, schema.Type)
	}
	defaultNumber, ok := schema.Default.(float64)
	if !ok || defaultNumber != float64(defaultValue) {
		t.Fatalf("expected %s %s query parameter %s default %d, got %#v", path, method, paramName, defaultValue, schema.Default)
	}
	if schema.Min == nil || *schema.Min != float64(minimum) {
		t.Fatalf("expected %s %s query parameter %s minimum %d, got %#v", path, method, paramName, minimum, schema.Min)
	}
	if maximum > 0 && (schema.Max == nil || *schema.Max != float64(maximum)) {
		t.Fatalf("expected %s %s query parameter %s maximum %d, got %#v", path, method, paramName, maximum, schema.Max)
	}
}

func assertSchemaPropertyRef(t *testing.T, doc *openapi3.T, schemaName string, propertyName string, ref string) {
	t.Helper()

	schemaRef := doc.Components.Schemas[schemaName]
	if schemaRef == nil || schemaRef.Value == nil {
		t.Fatalf("schema %q not found", schemaName)
	}

	propertyRef := schemaRef.Value.Properties[propertyName]
	if propertyRef == nil {
		t.Fatalf("property %q not found in schema %q", propertyName, schemaName)
	}
	if propertyRef.Ref != ref {
		t.Fatalf("expected %s.%s ref %q, got %q", schemaName, propertyName, ref, propertyRef.Ref)
	}
}

func assertQueryParamArrayItemsInt64(t *testing.T, doc *openapi3.T, path string, method string, paramName string) {
	t.Helper()

	schema := findOperationParamSchema(t, doc, path, method, paramName)
	if schema.Items == nil || schema.Items.Value == nil {
		t.Fatalf("array items not found for %s %s query parameter %s", path, method, paramName)
	}

	assertIntegerInt64(t, path+" "+method+" query parameter "+paramName+"[]", schema.Items.Value)
}

func assertSchemaArrayItemsInt64(t *testing.T, doc *openapi3.T, schemaName string, propertyName string) {
	t.Helper()

	schemaRef := doc.Components.Schemas[schemaName]
	if schemaRef == nil || schemaRef.Value == nil {
		t.Fatalf("schema %q not found", schemaName)
	}

	propertyRef := schemaRef.Value.Properties[propertyName]
	if propertyRef == nil || propertyRef.Value == nil {
		t.Fatalf("property %q not found in schema %q", propertyName, schemaName)
	}
	if propertyRef.Value.Items == nil || propertyRef.Value.Items.Value == nil {
		t.Fatalf("array items not found for %s.%s", schemaName, propertyName)
	}

	assertIntegerInt64(t, schemaName+"."+propertyName+"[]", propertyRef.Value.Items.Value)
}

func findOperationParamSchema(t *testing.T, doc *openapi3.T, path string, method string, paramName string) *openapi3.Schema {
	t.Helper()

	pathItem := doc.Paths.Value(path)
	if pathItem == nil {
		t.Fatalf("path %q not found", path)
	}

	var operation *openapi3.Operation
	switch method {
	case "get":
		operation = pathItem.Get
	case "patch":
		operation = pathItem.Patch
	case "post":
		operation = pathItem.Post
	case "delete":
		operation = pathItem.Delete
	default:
		t.Fatalf("unsupported method %q", method)
	}
	if operation == nil {
		t.Fatalf("operation %s %s not found", method, path)
	}

	for _, paramRef := range operation.Parameters {
		if paramRef.Value != nil && paramRef.Value.Name == paramName {
			if paramRef.Value.Schema == nil || paramRef.Value.Schema.Value == nil {
				t.Fatalf("schema not found for %s %s parameter %s", method, path, paramName)
			}
			return paramRef.Value.Schema.Value
		}
	}

	t.Fatalf("parameter %q not found for %s %s", paramName, method, path)
	return nil
}

func findSchemaProperty(t *testing.T, doc *openapi3.T, schemaName string, propertyName string) *openapi3.Schema {
	t.Helper()

	schemaRef := doc.Components.Schemas[schemaName]
	if schemaRef == nil || schemaRef.Value == nil {
		t.Fatalf("schema %q not found", schemaName)
	}

	propertyRef := schemaRef.Value.Properties[propertyName]
	if propertyRef == nil || propertyRef.Value == nil {
		t.Fatalf("property %q not found in schema %q", propertyName, schemaName)
	}

	return propertyRef.Value
}

func assertIntegerInt64(t *testing.T, field string, schema *openapi3.Schema) {
	t.Helper()

	if schema.Type == nil || !schema.Type.Is("integer") {
		t.Fatalf("expected %s type integer, got %#v", field, schema.Type)
	}
	if schema.Format != "int64" {
		t.Fatalf("expected %s format int64, got %q", field, schema.Format)
	}
}
