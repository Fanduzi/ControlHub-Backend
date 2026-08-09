// Package openapi_test verifies the embedded OpenAPI contract.
// input: embedded OpenAPI YAML, kin-openapi parser
// output: OpenAPI schema and authorization contract tests
// pos: Prevents documented API contracts from drifting from router behavior
// note: if this file changes, update header and README.md
package openapi_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/openapi"
)

func TestOpenAPIYAMLIsValid(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	if err := doc.Validate(context.Background()); err != nil {
		t.Fatalf("openapi.yaml validation failed: %v", err)
	}
}

func TestOpenAPIUsesTheOperatorAccessBoundary(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	if len(doc.Security) != 1 || len(doc.Security[0]["bearerAuth"]) != 0 {
		t.Fatalf("expected bearerAuth to protect operations by default, got %#v", doc.Security)
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
}

func TestOpenAPINumericIDsUseInt64(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	assertPathParamInt64(t, doc, "/resources/{id}", "get", "id")
	assertPathParamInt64(t, doc, "/resources/{id}", "patch", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/archive", "post", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/unarchive", "post", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/profile", "get", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/relations", "get", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/relations", "post", "id")
	assertPathParamInt64(t, doc, "/resource-relations/{id}", "delete", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/audit-events", "get", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/topology", "get", "id")
	assertQueryParamArrayItemsInt64(t, doc, "/resources", "get", "environmentId")
	assertQueryParamInt64(t, doc, "/audit-events", "get", "targetResourceId")

	assertSchemaPropertyInt64(t, doc, "Resource", "id")
	assertSchemaPropertyInt64(t, doc, "Resource", "environmentId")
	assertSchemaPropertyInt64(t, doc, "Resource", "ownerId")
	assertSchemaPropertyInt64(t, doc, "ResourceRelation", "id")
	assertSchemaPropertyInt64(t, doc, "ResourceRelation", "fromResourceId")
	assertSchemaPropertyInt64(t, doc, "ResourceRelation", "toResourceId")
	assertSchemaPropertyInt64(t, doc, "AuditEvent", "id")
	assertSchemaPropertyInt64(t, doc, "AuditEvent", "actorUserId")
	assertSchemaPropertyInt64(t, doc, "AuditEvent", "targetResourceId")
	assertSchemaPropertyInt64(t, doc, "Environment", "id")
	assertSchemaPropertyInt64(t, doc, "Owner", "id")
	assertSchemaPropertyInt64(t, doc, "Role", "id")
	assertSchemaPropertyInt64(t, doc, "ResourceProfileResponse", "resourceId")
	assertSchemaPropertyInt64(t, doc, "ResourceCreateInput", "environmentId")
	assertSchemaPropertyInt64(t, doc, "ResourceCreateInput", "ownerId")
	assertSchemaPropertyInt64(t, doc, "ResourcePatchRequest", "environmentId")
	assertSchemaPropertyInt64(t, doc, "ResourcePatchRequest", "ownerId")
	assertSchemaPropertyInt64(t, doc, "RelationCreateInput", "toResourceId")
	assertSchemaPropertyInt64(t, doc, "TopologyResponse", "rootResourceId")
	assertSchemaPropertyInt64(t, doc, "TopologyNode", "id")
	assertSchemaPropertyInt64(t, doc, "TopologyNode", "environmentId")
	assertSchemaPropertyInt64(t, doc, "TopologyNode", "ownerId")
	assertSchemaPropertyInt64(t, doc, "TopologyNode", "replicationParentId")
	assertSchemaPropertyInt64(t, doc, "TopologyEdge", "id")
	assertSchemaPropertyInt64(t, doc, "TopologyEdge", "fromResourceId")
	assertSchemaPropertyInt64(t, doc, "TopologyEdge", "toResourceId")
	assertSchemaArrayItemsInt64(t, doc, "TopologyGroup", "nodeIds")
}

func TestOpenAPITopologyPrimaryOmitsReplicationParentID(t *testing.T) {
	if strings.Contains(string(openapi.YAML), "replicationParentId: 0") {
		t.Fatal("expected topology example to omit replicationParentId for primary nodes")
	}
	if strings.Contains(string(openapi.YAML), "Zero for the primary (root of the chain).") {
		t.Fatal("expected topology schema description to describe omission, not zero sentinel")
	}
}

func TestOpenAPIStringIdentifiersRemainStrings(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	assertSchemaPropertyString(t, doc, "Resource", "externalId")
	assertSchemaPropertyString(t, doc, "TopologyNode", "groupKey")
	assertSchemaPropertyInt64(t, doc, "TopologyGroup", "id")
}

func TestOpenAPIQueryTargetsListDocumentsPaginationContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	assertQueryParamString(t, doc, "/query-targets", "get", "q")
	assertQueryParamInt64(t, doc, "/query-targets", "get", "targetId")
	assertQueryParamIntegerBounds(t, doc, "/query-targets", "get", "page", 1, 1, 1000000000)
	assertQueryParamIntegerBounds(t, doc, "/query-targets", "get", "pageSize", 50, 1, 100)
	assertSchemaPropertyRef(t, doc, "QueryTargetListResponse", "pageInfo", "#/components/schemas/PageInfo")
}

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

// TestOpenAPIExecutionsPageSizeContract proves the documented pageSize contract
// for GET /query-targets/{id}/executions: minimum 1, maximum 500, default 20.
// WHY: the prior spec omitted `maximum: 500`, so out-of-range pageSize values
// were not contractually rejected and the handler silently clamped them.
func TestOpenAPIExecutionsPageSizeContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	assertQueryParamIntegerBounds(t, doc, "/query-targets/{id}/executions", "get", "pageSize", 20, 1, 500)
}

// TestOpenAPIExecutionsCursorExampleIsVersion1 decodes the documented `cursor`
// response example nextCursor value and asserts it is a structurally valid
// version-1 CursorPayload (v=1, RFC3339 t, decimal-string id, 64-hex q).
// WHY: the prior example `eyJpZCI6MTAwMn0=` decoded to `{"id":1002}` — a legacy
// non-versioned payload that does not match the versioned CursorPayload the
// server actually emits. Consumers decoding by the spec sample would hit a
// version mismatch.
func TestOpenAPIExecutionsCursorExampleIsVersion1(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	pathItem := doc.Paths.Value("/query-targets/{id}/executions")
	if pathItem == nil || pathItem.Get == nil {
		t.Fatal("executions endpoint not found")
	}
	resp := pathItem.Get.Responses.Value("200")
	if resp == nil {
		t.Fatal("200 response not found")
	}
	appJSON := resp.Value.Content.Get("application/json")
	if appJSON == nil {
		t.Fatal("application/json content not found")
	}
	cursorExampleRef := appJSON.Examples["cursor"]
	if cursorExampleRef == nil || cursorExampleRef.Value == nil {
		t.Fatal("cursor example not found")
	}
	// The example value is a map[string]interface{} with a nextCursor field.
	exampleMap, ok := cursorExampleRef.Value.Value.(map[string]interface{})
	if !ok {
		t.Fatalf("cursor example value type = %T, want map", cursorExampleRef.Value.Value)
	}
	nextCursor, ok := exampleMap["nextCursor"].(string)
	if !ok {
		t.Fatalf("cursor example nextCursor type = %T, want string", exampleMap["nextCursor"])
	}

	// Decode the cursor using the model.DecodeCursor contract.
	payload, err := model.DecodeCursor(nextCursor)
	if err != nil {
		t.Fatalf("documented cursor example must decode as version-1 CursorPayload; got error: %v; cursor=%q", err, nextCursor)
	}
	if payload.Version != 1 {
		t.Fatalf("cursor example version = %d, want 1", payload.Version)
	}
	if payload.CreatedAt.IsZero() {
		t.Fatal("cursor example missing createdAt")
	}
	// WHY: RFC3339 parseability is the wire-format contract. The example must
	// round-trip through time.Parse(time.RFC3339, ...).
	if _, perr := time.Parse(time.RFC3339, payload.CreatedAt.Format(time.RFC3339)); perr != nil {
		t.Fatalf("cursor example createdAt is not RFC3339: %v", perr)
	}
	if _, perr := strconv.ParseUint(payload.ID, 10, 64); perr != nil {
		t.Fatalf("cursor example id %q is not a canonical positive uint64 string: %v", payload.ID, perr)
	}
	// WHY: q must be a 64-char lowercase hex SHA256 digest.
	if len(payload.QueryHash) != 64 {
		t.Fatalf("cursor example q length = %d, want 64", len(payload.QueryHash))
	}
	for _, r := range payload.QueryHash {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f':
		default:
			t.Fatalf("cursor example q %q must be lowercase hex [0-9a-f]", payload.QueryHash)
		}
	}
}

// TestOpenAPIExecutions400DocumentsValidationExamples proves the 400 response
// block documents all execution-history validation examples with the exact
// handler error strings. WHY: this keeps the spec and handler in lockstep — a
// future change to any validation message must update the spec and vice versa.
func TestOpenAPIExecutions400DocumentsValidationExamples(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	pathItem := doc.Paths.Value("/query-targets/{id}/executions")
	if pathItem == nil || pathItem.Get == nil {
		t.Fatal("executions endpoint not found")
	}
	resp := pathItem.Get.Responses.Value("400")
	if resp == nil {
		t.Fatal("400 response not found")
	}
	appJSON := resp.Value.Content.Get("application/json")
	if appJSON == nil {
		t.Fatal("application/json content not found")
	}

	type exampleAssertion struct {
		name    string
		message string
	}
	assertions := []exampleAssertion{
		{"invalidStatus", "invalid status: INVALID"},
		{"invalidTimestamp", `invalid timestamp "not-a-date": must be RFC3339 with timezone`},
		{"invalidPage", "page parameter must be a positive integer"},
		{"invalidPageSize", "pageSize parameter must be an integer in 1..500"},
	}
	for _, a := range assertions {
		exRef := appJSON.Examples[a.name]
		if exRef == nil || exRef.Value == nil {
			t.Fatalf("400 response missing example %q", a.name)
		}
		exMap, ok := exRef.Value.Value.(map[string]interface{})
		if !ok {
			t.Fatalf("example %q value type = %T, want map", a.name, exRef.Value.Value)
		}
		msg, ok := exMap["message"].(string)
		if !ok {
			t.Fatalf("example %q message type = %T, want string", a.name, exMap["message"])
		}
		if msg != a.message {
			t.Fatalf("example %q message = %q, want %q", a.name, msg, a.message)
		}
	}
}
