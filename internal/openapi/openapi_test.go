// Package openapi_test verifies the embedded OpenAPI contract.
// input: embedded OpenAPI YAML, kin-openapi parser, internal/model
// output: OpenAPI schema, resource completeness, health observation, effective-value override, bulk mutation, topology, pagination, execution, and closed error-enum tests
// pos: Prevents documented API contracts from drifting from router behavior
// note: if this file changes, update this header and module README.md.
package openapi_test

import (
	"context"
	"slices"
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

func TestOpenAPIResourceHealthObservationContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	path := doc.Paths.Value("/resources/{id}/health-observations")
	if path == nil || path.Post == nil {
		t.Fatal("POST /resources/{id}/health-observations must be documented")
	}
	resource := doc.Components.Schemas["Resource"].Value
	for _, field := range []string{"healthStatus", "healthFreshness", "healthObservedAt", "healthObserver", "manualHealthOverride"} {
		if resource.Properties[field] == nil {
			t.Fatalf("Resource.%s must be documented", field)
		}
		if !slices.Contains(resource.Required, field) {
			t.Fatalf("Resource.%s must be required", field)
		}
	}
	patch := doc.Components.Schemas["ResourcePatchRequest"].Value.Properties["healthStatus"]
	if patch == nil || patch.Value == nil || !patch.Value.Nullable {
		t.Fatal("ResourcePatchRequest.healthStatus must allow null to clear the manual override")
	}
}

func TestOpenAPIBulkResourceMutationContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	preview := doc.Paths.Value("/resources/bulk-mutations/preview")
	confirm := doc.Paths.Value("/resources/bulk-mutations/confirm")
	if preview == nil || preview.Post == nil || confirm == nil || confirm.Post == nil {
		t.Fatal("bulk preview and confirm POST operations must be documented")
	}
	if preview.Post.RequestBody == nil || preview.Post.RequestBody.Value == nil || preview.Post.RequestBody.Value.Content.Get("application/json").Schema.Ref != "#/components/schemas/BulkResourceMutationRequest" {
		t.Fatal("bulk preview must use BulkResourceMutationRequest")
	}
	if confirm.Post.RequestBody == nil || confirm.Post.RequestBody.Value == nil || confirm.Post.RequestBody.Value.Content.Get("application/json").Schema.Ref != "#/components/schemas/BulkResourceMutationConfirmRequest" {
		t.Fatal("bulk confirm must use BulkResourceMutationConfirmRequest")
	}
	for name, operation := range map[string]*openapi3.Operation{"preview": preview.Post, "confirm": confirm.Post} {
		for _, status := range []string{"200", "400", "401", "403"} {
			if operation.Responses.Value(status) == nil {
				t.Fatalf("bulk %s must document response %s", name, status)
			}
		}
		if operation.Responses.Value("200").Value.Content.Get("application/json").Schema.Ref != "#/components/schemas/BulkResourcePreview" {
			t.Fatalf("bulk %s must return BulkResourcePreview", name)
		}
	}
	if confirm.Post.Responses.Value("409") == nil {
		t.Fatal("bulk confirm must document reviewed-state conflicts as 409")
	}

	request := doc.Components.Schemas["BulkResourceMutationRequest"].Value
	if request == nil || !slices.Contains(request.Required, "targets") {
		t.Fatal("bulk request must require targets")
	}
	targets := request.Properties["targets"].Value
	if targets == nil || targets.Items == nil || targets.Items.Value == nil || targets.Items.Ref != "#/components/schemas/BulkResourceMutationTarget" {
		t.Fatal("bulk request targets must use BulkResourceMutationTarget items")
	}
	labels := request.Properties["labels"].Value
	if labels == nil || request.Properties["labels"].Ref != "#/components/schemas/LabelOperations" {
		t.Fatal("bulk request labels must use the explicit label operations schema")
	}
	for _, operation := range []string{"add", "update", "remove"} {
		if labels.Properties[operation] == nil {
			t.Fatalf("bulk labels missing %s operation", operation)
		}
	}

	confirmRequest := doc.Components.Schemas["BulkResourceMutationConfirmRequest"].Value
	if confirmRequest == nil || !slices.Contains(confirmRequest.Required, "request") || !slices.Contains(confirmRequest.Required, "reviewedFingerprint") {
		t.Fatal("bulk confirm must require request and reviewedFingerprint")
	}
	if doc.Components.Schemas["BulkResourcePreview"].Value.Properties["items"] == nil || doc.Components.Schemas["BulkResourcePreviewItem"].Value.Properties["fieldDiffs"] == nil || doc.Components.Schemas["BulkResourcePreviewItem"].Value.Properties["labelDiffs"] == nil {
		t.Fatal("bulk preview must expose per-resource items and field/label diffs")
	}

	errorSchema := doc.Components.Schemas["ErrorResponse"].Value.Properties["error"].Value
	if errorSchema == nil || !slices.Contains(errorSchema.Enum, "bulk_resource_mutation_conflict") {
		t.Fatal("ErrorResponse.error must include bulk_resource_mutation_conflict")
	}
}

func TestOpenAPIResourceCompletenessContract(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	resource := doc.Components.Schemas["Resource"].Value
	completeness := resource.Properties["completeness"]
	if completeness == nil || completeness.Value == nil || len(completeness.Value.AllOf) != 1 || completeness.Value.AllOf[0].Ref != "#/components/schemas/Completeness" {
		t.Fatalf("Resource.completeness ref = %#v, want #/components/schemas/Completeness", completeness)
	}
	if !completeness.Value.ReadOnly {
		t.Fatal("Resource.completeness must be read-only")
	}
	schema := doc.Components.Schemas["Completeness"].Value
	if !schema.ReadOnly {
		t.Fatal("Completeness must be read-only")
	}
	for _, field := range []string{"score", "status", "missingRequirements"} {
		if schema.Properties[field] == nil || !slices.Contains(schema.Required, field) {
			t.Fatalf("Completeness.%s must be documented and required", field)
		}
	}
}

// TestOpenAPIQueryEvidenceMetricsExactOneField proves the Issue #34 metrics
// operation documents an exact one-field response: the schema declares
// additionalProperties: false so the published contract cannot gain identity,
// target, statement, value, credential, DSN, request, or raw error fields.
func TestOpenAPIQueryEvidenceMetricsExactOneField(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	operation := doc.Paths.Value("/ops/query-evidence-metrics").Get
	if operation == nil {
		t.Fatal(`path /ops/query-evidence-metrics GET not found in openapi.yaml`)
	}
	resp := operation.Responses.Value("200")
	if resp == nil || resp.Value == nil {
		t.Fatal(`/ops/query-evidence-metrics must document a 200 response`)
	}
	schema := resp.Value.Content["application/json"].Schema.Value
	if schema == nil {
		t.Fatal("200 response must declare an application/json schema")
	}
	props := schema.Properties
	if len(props) != 1 {
		t.Fatalf("response schema properties = %d, want exactly 1; got %v", len(props), keys(props))
	}
	if _, ok := props["queryEvidencePersistenceFailures"]; !ok {
		t.Fatalf("response schema must contain exactly queryEvidencePersistenceFailures, got %v", keys(props))
	}
	if schema.AdditionalProperties.Has == nil || *schema.AdditionalProperties.Has {
		t.Fatal("response schema must declare additionalProperties: false to enforce the exact one-field contract")
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
	assertPathParamInt64(t, doc, "/resources/{id}/effective-values", "get", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/overrides/{field}", "put", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/overrides/{field}", "delete", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/relations", "get", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/relations", "post", "id")
	assertPathParamInt64(t, doc, "/resource-relations/{id}", "delete", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/audit-events", "get", "id")
	assertPathParamInt64(t, doc, "/resources/{id}/topology", "get", "id")
	assertPathParamInt64(t, doc, "/environments/{id}/topology", "get", "id")
	assertQueryParamInt64(t, doc, "/environments/{id}/topology", "get", "rootResourceId")
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
	assertSchemaPropertyInt64(t, doc, "ManualOverrideRequest", "expectedVersion")
	assertSchemaPropertyInt64(t, doc, "ClearManualOverrideRequest", "expectedVersion")
	assertSchemaPropertyInt64(t, doc, "OverrideVersionResponse", "version")
	assertSchemaPropertyInt64(t, doc, "RelationCreateInput", "toResourceId")
	assertSchemaPropertyInt64(t, doc, "TopologyResponse", "rootResourceId")
	if !slices.Contains(doc.Components.Schemas["TopologyResponse"].Value.Required, "truncated") {
		t.Fatal("TopologyResponse.truncated must be required")
	}
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

// TestOpenAPIAuditEventActorUserIdNullable proves the AuditEvent schema allows
// null for actorUserId. Unauthenticated auth audit events (failed login,
// rejected Bearer) persist with no actor; the schema must not require it.
func TestOpenAPIAuditEventActorUserIdNullable(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	schemaRef := doc.Components.Schemas["AuditEvent"]
	if schemaRef == nil || schemaRef.Value == nil {
		t.Fatal("AuditEvent schema not found")
	}
	schema := schemaRef.Value

	// actorUserId must NOT be in the required list.
	for _, field := range schema.Required {
		if field == "actorUserId" {
			t.Fatalf("actorUserId must not be required in AuditEvent schema; required = %v", schema.Required)
		}
	}

	// The property must be nullable (OpenAPI 3.1).
	prop := schema.Properties["actorUserId"]
	if prop == nil || prop.Value == nil {
		t.Fatal("actorUserId property not found in AuditEvent schema")
	}
	if !prop.Value.Nullable {
		t.Fatal("actorUserId must be nullable in AuditEvent schema")
	}
}

// TestOpenAPILoginResponseHasUserId proves the LoginResponse schema includes
// the userId field (always returned by the handler) as required.
func TestOpenAPILoginResponseHasUserId(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	schemaRef := doc.Components.Schemas["LoginResponse"]
	if schemaRef == nil || schemaRef.Value == nil {
		t.Fatal("LoginResponse schema not found")
	}
	schema := schemaRef.Value

	// userId must be present as a property.
	if _, ok := schema.Properties["userId"]; !ok {
		t.Fatalf("LoginResponse schema missing userId property; properties = %v", keys(schema.Properties))
	}

	// userId must be in the required list (always returned).
	found := false
	for _, field := range schema.Required {
		if field == "userId" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("userId must be required in LoginResponse schema; required = %v", schema.Required)
	}
}

func keys(m map[string]*openapi3.SchemaRef) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestOpenAPIErrorResponseErrorIsClosedControlledErrorCodeEnum proves
// ErrorResponse.error is a closed enum of every inventoried Controlled Error
// Code (writeJSONError literals plus Console BFF snake_case codes). Adding a
// code is an OpenAPI contract change. query_result_disclosure_blocked must be
// present so execute-path disclosure is not published as an unconstrained string.
func TestOpenAPIErrorResponseErrorIsClosedControlledErrorCodeEnum(t *testing.T) {
	loader := openapi3.NewLoader()
	doc, err := loader.LoadFromData(openapi.YAML)
	if err != nil {
		t.Fatalf("failed to parse openapi.yaml: %v", err)
	}

	property := findSchemaProperty(t, doc, "ErrorResponse", "error")
	if property.Type == nil || !property.Type.Is("string") {
		t.Fatalf("expected ErrorResponse.error type string, got %#v", property.Type)
	}
	if len(property.Enum) == 0 {
		t.Fatal("ErrorResponse.error must be a closed enum of Controlled Error Codes")
	}

	got := make([]string, 0, len(property.Enum))
	seen := make(map[string]struct{}, len(property.Enum))
	for i, raw := range property.Enum {
		code, ok := raw.(string)
		if !ok || code == "" {
			t.Fatalf("ErrorResponse.error enum[%d] = %#v, want non-empty string", i, raw)
		}
		if _, dup := seen[code]; dup {
			t.Fatalf("ErrorResponse.error enum contains duplicate %q", code)
		}
		seen[code] = struct{}{}
		got = append(got, code)
	}
	slices.Sort(got)

	want := []string{
		"bulk_resource_mutation_conflict",
		"disclosure_policy_conflict",
		"disclosure_policy_not_found",
		"environment_not_found",
		"forbidden",
		"forbidden_header",
		"internal_error",
		"invalid_credentials",
		"invalid_payload",
		"invalid_request",
		"malformed_json",
		"not_found",
		"owner_not_found",
		"payload_too_large",
		"profile_not_supported",
		"query_backend_error",
		"query_explain_not_supported",
		"query_not_allowed",
		"query_result_disclosure_blocked",
		"query_target_not_found",
		"query_timeout",
		"relation_conflict",
		"relation_not_found",
		"relationship_map_not_supported",
		"resource_alias_conflict",
		"resource_archived",
		"resource_conflict",
		"resource_external_identifier_conflict",
		"resource_name_conflict",
		"resource_not_found",
		"saved_statement_not_found",
		"schema_backend_error",
		"schema_definition_not_supported",
		"schema_not_allowed",
		"schema_object_not_found",
		"schema_target_not_found",
		"schema_timeout",
		"schema_validation_failed",
		"service_unavailable",
		"unauthorized",
		"validation_failed",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("ErrorResponse.error enum = %v\nwant closed Controlled Error Code set %v", got, want)
	}
}
