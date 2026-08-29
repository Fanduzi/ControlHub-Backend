# OpenAPI Module

Embeds and validates the OpenAPI contract served by the API documentation route.

## Files
| File | Responsibility |
|------|---------------|
| openapi.yaml | Source OpenAPI contract for user and opaque machine security, scoped machine reads/admin lifecycle, resource-list search filters, named inventory views, admin bulk mutation preview/confirmation, governed typed-profile identity, strict multipart ingestion preview/confirm, health observations, topology workspace reads, relationship-rule discovery, audit pagination/search and field diffs, metrics, and controlled errors |
| embed.go | Embeds the YAML for serving and tests |
| openapi_test.go | Validates schema, strict ingestion multipart, bulk mutation, topology, pagination, executions, and the closed controlled-error enum |
| machine_principal_contract_test.go | Validates machine security, closed route/scope mapping, admin routes, codes, and one-time-secret schemas |
| operator_access_boundary_test.go | Proves every protected operation documents the status codes its operatoraccess class requires |
| openapi_schema_helpers_test.go | Shared schema/parameter shape assertion helpers |

## Exports
- `YAML` - embedded OpenAPI specification bytes

## Contracts
- `GET /resources`: free-text `q` across CI identity fields (excluding owner and labels), exact `ownerId`, and repeatable exact `label=key:value` filters combined with AND.
- `/inventory/views`: authenticated personal/shared named inventory view CRUD; shared mutations are admin-only and personal mutations are owner-only.
- `/resources/bulk-mutations/preview` and `/resources/bulk-mutations/confirm`: admin-only reviewed bulk mutation contract with per-CI diffs/errors and fingerprint conflict protection.
- `machineCredential`: independent opaque Bearer scheme; the documented closed route matrix grants inventory, relation/topology, query-target discovery, audit, and shared-only Named View reads without User/session fallback.
- Machine credential plaintext appears only in `MachineCredentialIssue` create/rotate responses; admin list lifecycle metadata includes only credential ID and created/expires/revoked/last-used timestamps, never lookup IDs or other auth material.
- `GET /ops/auth-audit-metrics` response schema: fixed admin-only shape with exactly `authAuditPersistenceFailures` and `authAuditSuppressedRejections` counters; carries no identity, request, or credential material
- `GET /ops/query-evidence-metrics` response schema (Issue #34): fixed admin-only shape with exactly `queryEvidencePersistenceFailures`; carries no identity, target, statement, value, credential, DSN, request, or raw error material
- `POST /admin/ingestions/preview` and `/confirm` use a strict multipart `format` (`csv` or `json`) plus one bounded `file`; confirm additionally requires the reviewed 64-hex fingerprint and returns a fresh preview in controlled 409 conflict/drift responses.
- `ErrorResponse.error`: closed Controlled Error Code enum (Issue #53). Adding a code is a contract change. The published set is backend `writeJSONError` literals plus Console BFF snake_case codes.
- `AuditEvent.changes`: optional server-owned add/update/remove field evidence with before/after values; absent on legacy and non-inventory events.
- `Resource` always exposes read-only completeness, effective status, freshness, observed time, observer, and nullable manual override; POST observations are operational and PATCH null clears the audited override.
- `GET /resources/{id}/relation-rules`: source-specific relation types, target resource types, and same-environment constraints consumed by the console; writes revalidate the same server matrix.
- `GET /resources/{id}/topology` and `GET /environments/{id}/topology`: authenticated topology reads with default depth 2, optional environment-scoped root selection, output caps, and required `truncated`.
- `GET /audit-events?q=...`: optional search over operator identity and target resource name, combined with the existing filters.
- `GET /resources?q=...&ownerId=...&label=key:value`: inventory identifier search, exact owner filtering, and repeatable exact labels combined with AND.

## Dependencies
- Upstream: `github.com/getkin/kin-openapi` for validation tests
- Downstream: `internal/api` serves the embedded contract

## Update Rule
If API route security or the OpenAPI contract changes, update this file in the same change.

The OpenAPI authorization assertions consume the test-only
`internal/testsupport/operatoraccess` policy. It distinguishes authenticated
reads and fresh-any-role query surfaces from router-admin, handler-admin, and
conditional 38R saved-statement operations.
