# OpenAPI Module

Embeds and validates the OpenAPI contract served by the API documentation route.

## Files
| File | Responsibility |
|------|---------------|
| openapi.yaml | Source contract for governed typed-profile identity, health observations, relationship-rule discovery, access security, audit pagination/search and field diffs, metrics, and controlled errors |
| embed.go | Embeds the YAML for serving and tests |
| openapi_test.go | Validates schema, topology, pagination, and executions contract tests |
| operator_access_boundary_test.go | Proves every protected operation documents the status codes its operatoraccess class requires |
| openapi_schema_helpers_test.go | Shared schema/parameter shape assertion helpers |

## Exports
- `YAML` - embedded OpenAPI specification bytes

## Contracts
- `GET /ops/auth-audit-metrics` response schema: fixed admin-only shape with exactly `authAuditPersistenceFailures` and `authAuditSuppressedRejections` counters; carries no identity, request, or credential material
- `GET /ops/query-evidence-metrics` response schema (Issue #34): fixed admin-only shape with exactly `queryEvidencePersistenceFailures`; carries no identity, target, statement, value, credential, DSN, request, or raw error material
- `ErrorResponse.error`: closed Controlled Error Code enum (Issue #53). Adding a code is a contract change. The published set is backend `writeJSONError` literals plus Console BFF snake_case codes.
- `AuditEvent.changes`: optional server-owned add/update/remove field evidence with before/after values; absent on legacy and non-inventory events.
- `Resource` always exposes effective status, freshness, observed time, observer, and nullable manual override; POST observations are operational and PATCH null clears the audited override.
- `GET /resources/{id}/relation-rules`: source-specific relation types, target resource types, and same-environment constraints consumed by the console; writes revalidate the same server matrix.
- `GET /audit-events?q=...`: optional search over operator identity and target resource name, combined with the existing filters.

## Dependencies
- Upstream: `github.com/getkin/kin-openapi` for validation tests
- Downstream: `internal/api` serves the embedded contract

## Update Rule
If API route security or the OpenAPI contract changes, update this file in the same change.

The OpenAPI authorization assertions consume the test-only
`internal/testsupport/operatoraccess` policy. It distinguishes authenticated
reads and fresh-any-role query surfaces from router-admin, handler-admin, and
conditional 38R saved-statement operations.
