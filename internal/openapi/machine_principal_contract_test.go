// Package openapi_test verifies the embedded OpenAPI contract.
// input: context, fmt, testing, kin-openapi parser, internal/openapi
// output: machine credential scheme, scoped read/collector routes, governed execution evidence, controlled-code, and response-only secret-boundary tests
// pos: Prevents machine HTTP authorization, collector capability, evidence identity, and one-time secret documentation drift
// note: if this file changes, update this header and module README.md.
package openapi_test

import (
	"fmt"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/fan/controlhub/internal/openapi"
)

func TestOpenAPIMachineCredentialContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("load OpenAPI: %v", err)
	}

	scheme := doc.Components.SecuritySchemes["machineCredential"]
	if scheme == nil || scheme.Value == nil || scheme.Value.Type != "http" || scheme.Value.Scheme != "bearer" {
		t.Fatalf("machineCredential scheme = %#v, want HTTP bearer", scheme)
	}

	routes := []struct {
		path, method, scope string
	}{
		{"/resources", "GET", "inventory:read"},
		{"/resources/{id}", "GET", "inventory:read"},
		{"/resources/{id}/profile", "GET", "inventory:read"},
		{"/resources/{id}/effective-values", "GET", "inventory:read"},
		{"/environments", "GET", "inventory:read"},
		{"/owners", "GET", "inventory:read"},
		{"/roles", "GET", "inventory:read"},
		{"/resource-types", "GET", "inventory:read"},
		{"/relation-types", "GET", "inventory:read"},
		{"/lifecycle-statuses", "GET", "inventory:read"},
		{"/health-statuses", "GET", "inventory:read"},
		{"/resource-subtypes", "GET", "inventory:read"},
		{"/resources/{id}/relations", "GET", "relations:read"},
		{"/resources/{id}/relation-rules", "GET", "relations:read"},
		{"/resources/{id}/members", "GET", "relations:read"},
		{"/resources/{id}/topology", "GET", "relations:read"},
		{"/environments/{id}/topology", "GET", "relations:read"},
		{"/query-targets", "GET", "governed-select"},
		{"/query-targets/{id}/execute", "POST", "governed-select"},
		{"/audit-events", "GET", "audit:read"},
		{"/resources/{id}/audit-events", "GET", "audit:read"},
		{"/inventory/views", "GET", "named-views:read"},
		{"/admin/ingestions/preview", "POST", "inventory:ingest"},
		{"/admin/ingestions/confirm", "POST", "inventory:ingest"},
		{"/resources/{id}/health-observations", "POST", "health:write"},
	}
	for _, route := range routes {
		op := operationAt(t, doc, route.path, route.method)
		if op.Security == nil || !hasSecurity(*op.Security, "machineCredential") {
			t.Fatalf("%s %s does not document machineCredential", route.method, route.path)
		}
		if got := fmt.Sprint(op.Extensions["x-machine-scope"]); got != route.scope {
			t.Fatalf("%s %s x-machine-scope = %q, want %q", route.method, route.path, got, route.scope)
		}
	}

	for _, route := range []struct{ path, method string }{
		{"/admin/machine-principals", "GET"},
		{"/admin/machine-principals", "POST"},
		{"/admin/machine-credentials/{credentialId}/rotate", "POST"},
		{"/admin/machine-credentials/{credentialId}/revoke", "POST"},
	} {
		operationAt(t, doc, route.path, route.method)
	}

	errorCodes := doc.Components.Schemas["ErrorResponse"].Value.Properties["error"].Value.Enum
	for _, want := range []string{"machine_credential_invalid", "machine_credential_expired", "machine_credential_revoked", "machine_scope_denied"} {
		if !containsEnum(errorCodes, want) {
			t.Fatalf("ErrorResponse.error enum missing %q", want)
		}
	}

	for _, schemaName := range []string{"MachinePrincipal", "MachineCredential"} {
		schema := doc.Components.Schemas[schemaName]
		if schema == nil || schema.Value == nil {
			t.Fatalf("schema %s missing", schemaName)
		}
		if schema.Value.Properties["secret"] != nil {
			t.Fatalf("schema %s must not expose secret", schemaName)
		}
	}
	issue := doc.Components.Schemas["MachineCredentialIssue"]
	if issue == nil || issue.Value == nil || issue.Value.Properties["secret"] == nil {
		t.Fatal("MachineCredentialIssue must document the one-time secret")
	}
	if secret := issue.Value.Properties["secret"].Value; secret == nil || !secret.ReadOnly || secret.WriteOnly {
		t.Fatal("MachineCredentialIssue.secret must be response-only")
	}

	audit := doc.Components.Schemas["AuditEvent"].Value
	if audit == nil || audit.Properties["actorMachinePrincipalId"] == nil {
		t.Fatal("AuditEvent must project actorMachinePrincipalId")
	}
	actor := doc.Components.Schemas["QueryExecutionActor"].Value
	if actor == nil || actor.Properties["kind"] == nil || !containsEnum(actor.Properties["kind"].Value.Enum, "machine") {
		t.Fatal("QueryExecutionActor.kind must distinguish user and machine evidence")
	}
}

func TestOpenAPIMachinePrincipalListLifecycleContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("load OpenAPI: %v", err)
	}
	list := doc.Components.Schemas["MachinePrincipalListItem"]
	if list == nil || list.Value == nil || list.Value.Properties["credentials"] == nil {
		t.Fatal("MachinePrincipalListItem must document credential lifecycle metadata")
	}
	credential := doc.Components.Schemas["MachineCredentialLifecycle"]
	if credential == nil || credential.Value == nil {
		t.Fatal("MachineCredentialLifecycle schema missing")
	}
	for _, required := range []string{"id", "createdAt", "expiresAt", "lastUsedAt", "revokedAt"} {
		if credential.Value.Properties[required] == nil || !containsString(credential.Value.Required, required) {
			t.Fatalf("MachineCredentialLifecycle.%s must be required", required)
		}
	}
	for _, forbidden := range []string{"secret", "secretHash", "lookupId", "scopes", "machinePrincipalId", "rotatedFromCredentialId"} {
		if credential.Value.Properties[forbidden] != nil {
			t.Fatalf("MachineCredentialLifecycle must not expose %s", forbidden)
		}
	}
	for _, forbidden := range []string{"secret", "secretHash", "lookupId"} {
		if doc.Components.Schemas["MachineCredential"].Value.Properties[forbidden] != nil {
			t.Fatalf("MachineCredential must not expose %s", forbidden)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func operationAt(t *testing.T, doc *openapi3.T, path, method string) *openapi3.Operation {
	t.Helper()
	item := doc.Paths.Value(path)
	if item == nil {
		t.Fatalf("path %s missing", path)
	}
	switch method {
	case "GET":
		return item.Get
	case "POST":
		return item.Post
	default:
		t.Fatalf("unsupported method %s", method)
		return nil
	}
}

func hasSecurity(requirements openapi3.SecurityRequirements, name string) bool {
	for _, requirement := range requirements {
		if _, ok := requirement[name]; ok {
			return true
		}
	}
	return false
}

func containsEnum(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
