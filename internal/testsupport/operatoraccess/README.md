# Operator Access Policy (test support)

Shared, test-only metadata describing every protected API operation and its
authorization class.

## Files
| File | Responsibility |
|------|---------------|
| policy.go | Protected-operation table with authorization classes (`AuthenticatedRead`, `RouterAdmin`, `HandlerAdmin`, `FreshAnyRole`, `ConditionalSavedStatementMutation`), canonical OpenAPI paths, and concrete request paths |

## Exports
- `operatoraccess.All()` — returns a fresh protected-operation slice
- `operatoraccess.Operation`, `operatoraccess.Class` — operation and authorization-class types
- `operatoraccess.Class.RequiredOpenAPIResponses()` — derives mandatory OpenAPI response statuses from the class

## Consumers
- `internal/api/operator_access_boundary_test.go` — router-level 401/403/2xx matrix
- `internal/openapi/operator_access_boundary_test.go` — OpenAPI status-code contract
- `internal/integration/operator_access_boundary_test.go` — MySQL-backed boundary matrix

## Update Rule
If a protected route, its authorization class, or its OpenAPI status contract
changes, update `policy.go` and re-run the consumer tests.