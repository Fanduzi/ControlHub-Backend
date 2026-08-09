# OpenAPI Module

Embeds and validates the OpenAPI contract served by the API documentation route.

## Files
| File | Responsibility |
|------|---------------|
| openapi.yaml | Source OpenAPI contract, including Operator Access Boundary security and schema-policy responses |
| embed.go | Embeds the YAML for serving and tests |
| openapi_test.go | Validates schema, topology, pagination, and executions contract tests |
| operator_access_boundary_test.go | Proves every protected operation documents the status codes its operatoraccess class requires |
| openapi_schema_helpers_test.go | Shared schema/parameter shape assertion helpers |

## Exports
- `YAML` - embedded OpenAPI specification bytes

## Dependencies
- Upstream: `github.com/getkin/kin-openapi` for validation tests
- Downstream: `internal/api` serves the embedded contract

## Update Rule
If API route security or the OpenAPI contract changes, update this file in the same change.

The OpenAPI authorization assertions consume the test-only
`internal/testsupport/operatoraccess` policy. It distinguishes authenticated
reads and fresh-any-role query surfaces from router-admin, handler-admin, and
conditional 38R saved-statement operations.
