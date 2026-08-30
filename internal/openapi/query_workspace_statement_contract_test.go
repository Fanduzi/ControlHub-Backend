// Package openapi_test verifies the query workspace and private statement OpenAPI slice.
// input: context, testing, kin-openapi parser, embedded OpenAPI YAML
// output: singular workspace GET/PUT, owner statement GET, schema, error-code, and list non-disclosure assertions
// pos: Contract regression coverage for frontend issues 39 and 41
// note: if this file changes, update this header and module README.md.
package openapi_test

import (
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/fan/controlhub/internal/openapi"
)

func TestOpenAPIQueryWorkspaceAndStatementContract(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("load OpenAPI: %v", err)
	}
	workspacePath := doc.Paths.Value("/query-workspace")
	if workspacePath == nil || workspacePath.Get == nil || workspacePath.Put == nil {
		t.Fatal("/query-workspace must expose GET and PUT")
	}
	statementPath := doc.Paths.Value("/query-targets/{id}/executions/{executionId}/statement")
	if statementPath == nil || statementPath.Get == nil {
		t.Fatal("execution statement GET path missing")
	}
	for _, schemaName := range []string{"QueryWorkspace", "QueryWorkspacePutRequest", "QueryWorkspaceWorksheet", "QueryExecutionStatementResponse"} {
		if doc.Components.Schemas[schemaName] == nil || doc.Components.Schemas[schemaName].Value == nil {
			t.Fatalf("schema %s missing", schemaName)
		}
	}
	worksheet := doc.Components.Schemas["QueryWorkspaceWorksheet"].Value
	for _, field := range []string{"id", "name", "targetResourceId", "statement", "activeDatabase"} {
		if worksheet.Properties[field] == nil || !containsString(worksheet.Required, field) {
			t.Fatalf("worksheet field %s must be present and required", field)
		}
	}
	record := doc.Components.Schemas["QueryExecutionRecord"].Value
	if record.Properties["statement"] != nil || record.Properties["fullStatement"] != nil {
		t.Fatal("execution list schema must not expose full statement")
	}
	errorCodes := doc.Components.Schemas["ErrorResponse"].Value.Properties["error"].Value.Enum
	for _, code := range []string{"query_workspace_conflict", "query_execution_not_found"} {
		if !containsEnum(errorCodes, code) {
			t.Fatalf("ErrorResponse.error missing %s", code)
		}
	}
}
