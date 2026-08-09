// Package operatoraccess is test-support metadata describing every protected
// API operation and its authorization class. It is imported only by tests.
// input: standard library only
// output: Class, Operation, All
// pos: Single source of truth for protected-operation coverage, shared by the
// router boundary test, the OpenAPI contract test, and integration tests
// note: if this file changes, update header and README.md
package operatoraccess

// Class is the authorization class of a protected operation. Consumers derive
// the required OpenAPI response statuses from the class.
type Class string

const (
	// AuthenticatedRead: anonymous 401; any authenticated role 2xx. No 403.
	AuthenticatedRead Class = "authenticated-read"
	// RouterAdmin: anonymous 401; editor 403; admin 2xx (router-level gate).
	RouterAdmin Class = "router-admin"
	// HandlerAdmin: anonymous 401; editor 403; admin 2xx (handler-level gate).
	HandlerAdmin Class = "handler-admin"
	// FreshAnyRole: anonymous 401; any authenticated role with a fresh token 2xx.
	// No role-only 403 (policy 403s like guard/disclosure blocks are not role 403s).
	FreshAnyRole Class = "fresh-any-role"
	// ConditionalSavedStatementMutation: anonymous 401; editor 2xx only for own
	// personal statements, 403 for shared templates, 404 for others' personal
	// statements; admin 2xx for shared templates and own personal statements,
	// 404 for others' personal statements. Not a uniform admin-only gate.
	ConditionalSavedStatementMutation Class = "conditional-saved-statement-mutation"
)

// Operation describes one protected operation.
type Operation struct {
	Class       Class  // authorization class
	Method      string // HTTP method (GET/POST/PUT/PATCH/DELETE)
	Path        string // canonical OpenAPI path, e.g. /resources/{id}
	RequestPath string // concrete request path for boundary tests, e.g. /resources/1
}

// RequiredOpenAPIResponses returns the status codes every consumer must
// document for an operation of the given class: 401 for every protected
// operation, 403 for the admin-gated classes, and 403+404 for the conditional
// saved-statement mutations. The result is a fresh slice.
func (c Class) RequiredOpenAPIResponses() []string {
	switch c {
	case RouterAdmin, HandlerAdmin:
		return []string{"401", "403"}
	case ConditionalSavedStatementMutation:
		return []string{"401", "403", "404"}
	default:
		return []string{"401"}
	}
}

// All returns every protected operation. The slice and its elements are fresh;
// callers may not mutate shared state.
func All() []Operation {
	return []Operation{
		// Authenticated inventory reads.
		{AuthenticatedRead, "GET", "/resources", "/resources"},
		{AuthenticatedRead, "GET", "/resources/{id}", "/resources/1"},
		{AuthenticatedRead, "GET", "/resources/{id}/profile", "/resources/1/profile"},
		{AuthenticatedRead, "GET", "/resources/{id}/relations", "/resources/1/relations"},
		{AuthenticatedRead, "GET", "/resources/{id}/members", "/resources/1/members"},
		{AuthenticatedRead, "GET", "/resources/{id}/topology", "/resources/1/topology"},
		// Dictionary reads.
		{AuthenticatedRead, "GET", "/environments", "/environments"},
		{AuthenticatedRead, "GET", "/owners", "/owners"},
		{AuthenticatedRead, "GET", "/roles", "/roles"},
		{AuthenticatedRead, "GET", "/resource-types", "/resource-types"},
		{AuthenticatedRead, "GET", "/relation-types", "/relation-types"},
		{AuthenticatedRead, "GET", "/lifecycle-statuses", "/lifecycle-statuses"},
		{AuthenticatedRead, "GET", "/health-statuses", "/health-statuses"},
		{AuthenticatedRead, "GET", "/resource-subtypes", "/resource-subtypes?resourceType=host"},
		// Query-targets list.
		{AuthenticatedRead, "GET", "/query-targets", "/query-targets"},
		// Router-admin inventory mutations.
		{RouterAdmin, "POST", "/resources", "/resources"},
		{RouterAdmin, "PATCH", "/resources/{id}", "/resources/1"},
		{RouterAdmin, "POST", "/resources/{id}/archive", "/resources/1/archive"},
		{RouterAdmin, "POST", "/resources/{id}/unarchive", "/resources/1/unarchive"},
		{RouterAdmin, "PUT", "/resources/{id}/profile", "/resources/1/profile"},
		{RouterAdmin, "PATCH", "/resources/{id}/profile", "/resources/1/profile"},
		{RouterAdmin, "DELETE", "/resources/{id}/profile", "/resources/1/profile"},
		{RouterAdmin, "POST", "/resources/{id}/relations", "/resources/1/relations"},
		{RouterAdmin, "DELETE", "/resource-relations/{id}", "/resource-relations/1"},
		// Router-admin audit reads.
		{RouterAdmin, "GET", "/audit-events", "/audit-events"},
		{RouterAdmin, "GET", "/resources/{id}/audit-events", "/resources/1/audit-events"},
		// Fresh-any-role query surfaces.
		{FreshAnyRole, "POST", "/query-targets/{id}/execute", "/query-targets/22/execute"},
		{FreshAnyRole, "POST", "/query-targets/{id}/explain", "/query-targets/22/explain"},
		{FreshAnyRole, "POST", "/query-targets/{id}/related-records", "/query-targets/22/related-records"},
		{FreshAnyRole, "GET", "/query-targets/{id}/executions", "/query-targets/22/executions"},
		{FreshAnyRole, "GET", "/query-targets/{id}/schema/databases", "/query-targets/22/schema/databases"},
		{FreshAnyRole, "GET", "/query-targets/{id}/schema/objects", "/query-targets/22/schema/objects?database=orders"},
		{FreshAnyRole, "GET", "/query-targets/{id}/schema/object-details", "/query-targets/22/schema/object-details?database=orders&name=users&kind=table"},
		{FreshAnyRole, "GET", "/query-targets/{id}/schema/table-definition", "/query-targets/22/schema/table-definition?database=orders&name=users"},
		{FreshAnyRole, "GET", "/query-targets/{id}/schema/relationship-map", "/query-targets/22/schema/relationship-map?database=orders&name=users"},
		{FreshAnyRole, "GET", "/query-targets/{id}/credential", "/query-targets/22/credential"},
		{FreshAnyRole, "GET", "/query-targets/{id}/saved-statements", "/query-targets/22/saved-statements"},
		{FreshAnyRole, "POST", "/query-targets/{id}/saved-statements/{statementId}/execute", "/query-targets/22/saved-statements/1/execute"},
		// Handler-admin credential writes.
		{HandlerAdmin, "PUT", "/query-targets/{id}/credential", "/query-targets/22/credential"},
		{HandlerAdmin, "DELETE", "/query-targets/{id}/credential", "/query-targets/22/credential"},
		// Handler-admin disclosure operations, including GET.
		{HandlerAdmin, "GET", "/query-disclosure-policies", "/query-disclosure-policies?targetResourceId=22"},
		{HandlerAdmin, "POST", "/query-disclosure-policies", "/query-disclosure-policies"},
		{HandlerAdmin, "PUT", "/query-disclosure-policies", "/query-disclosure-policies"},
		{HandlerAdmin, "DELETE", "/query-disclosure-policies", "/query-disclosure-policies?targetResourceId=22&databaseName=orders&objectName=users&columnName=email"},
		// Conditional saved-statement mutations (38R, service-level scope/owner policy).
		{ConditionalSavedStatementMutation, "POST", "/query-targets/{id}/saved-statements", "/query-targets/22/saved-statements"},
		{ConditionalSavedStatementMutation, "PUT", "/query-targets/{id}/saved-statements/{statementId}", "/query-targets/22/saved-statements/1"},
		{ConditionalSavedStatementMutation, "DELETE", "/query-targets/{id}/saved-statements/{statementId}", "/query-targets/22/saved-statements/1"},
	}
}
