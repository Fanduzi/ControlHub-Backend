# OpenAPI Module

Embeds and validates the OpenAPI contract served by the API documentation route.

## Files
| File | Responsibility |
|------|---------------|
| openapi.yaml | Source OpenAPI contract for user and opaque machine security, query workspace GET/PUT, server-computed history restore eligibility, owner-only successful execution statements, scoped machine reads/reserved collector ingestion and health routes/ordinary governed execution, truthful evidence actors and read-only per-principal collector presence, admin lifecycle, resource-list search filters, named inventory views, bulk mutation and strict ingestion preview/confirmation, governed typed-profile identity, health observations, topology workspace reads, relationship-rule discovery, audit pagination/search/environment filtering and field diffs, metrics, and controlled errors |
| audit_environment_contract_test.go | Audit environment-filter parameter contract regression test |
| embed.go | Embeds the YAML for serving and tests |
| openapi_test.go | Validates schema, read-only collector presence, strict ingestion multipart, bulk mutation, topology, pagination, executions, and the closed controlled-error enum |
| machine_principal_contract_test.go | Validates machine security, closed read/query/collector scopes, truthful execution/audit actors, sibling-route mapping, admin routes, codes, and one-time-secret schemas |
| operator_access_boundary_test.go | Proves every protected operation documents the status codes its operatoraccess class requires |
| openapi_schema_helpers_test.go | Shared schema/parameter shape assertion helpers |
| query_workspace_statement_contract_test.go | Workspace/statement routes and schemas, controlled codes, execution-list non-disclosure, and required boolean restore eligibility contract |

## Exports
- `YAML` - embedded OpenAPI specification bytes

## Contracts
- `GET /resources`: free-text `q` across CI identity fields (excluding owner and labels), exact `ownerId`, and repeatable exact `label=key:value` filters combined with AND.
- `/inventory/views`: authenticated personal/shared named inventory view CRUD; shared mutations are admin-only and personal mutations are owner-only.
- `/resources/bulk-mutations/preview` and `/resources/bulk-mutations/confirm`: admin-only reviewed bulk mutation contract with per-CI diffs/errors and fingerprint conflict protection.
- `machineCredential`: independent opaque Bearer scheme; the closed route matrix grants inventory, relation/topology, query-target discovery, audit, shared-only Named View reads, reserved `inventory:ingest`/`health:write` collector routes, and only ordinary `POST /query-targets/{id}/execute` for `governed-select`; sibling query routes and ordinary mutations remain user-only.
- Machine credential plaintext appears only in `MachineCredentialIssue` create/rotate responses; admin list lifecycle metadata includes only credential ID and created/expires/revoked/last-used timestamps, never lookup IDs or other auth material.
- Query execution history and audit schemas expose the machine principal identity independently from user identity; they never expose the authenticating credential ID or secret.
- `GET/PUT /query-workspace` is the User-only singular aggregate contract; history `canRestore` is server-computed for the authenticated User's own successful row with available private SQL; `GET /query-targets/{id}/executions/{executionId}/statement` remains fresh-User owner-only and never widens execution-list or audit schemas.
- `GET /ops/auth-audit-metrics` response schema: fixed admin-only shape with exactly `authAuditPersistenceFailures` and `authAuditSuppressedRejections` counters; carries no identity, request, or credential material
- `GET /ops/query-evidence-metrics` response schema (Issue #34): fixed admin-only shape with exactly `queryEvidencePersistenceFailures`; carries no identity, target, statement, value, credential, DSN, request, or raw error material
- `POST /admin/ingestions/preview` and `/confirm` use a strict multipart `format` (`csv` or `json`) plus one bounded `file`; confirm additionally requires the reviewed 64-hex fingerprint and returns a fresh preview in controlled 409 conflict/drift responses.
- `ErrorResponse.error`: closed Controlled Error Code enum (Issue #53). Adding a code is a contract change. The published set is backend `writeJSONError` literals plus Console BFF snake_case codes.
- `AuditEvent.changes`: optional server-owned add/update/remove field evidence with before/after values; absent on legacy and non-inventory events.
- `Resource` exposes read-only completeness and optional sorted per-principal `collectorPresence` with present/Missing status, collector source, principal ID/name, and stable nullable `missingSince`; effective status, freshness, observed time, observer, and nullable manual override remain server-owned.
- `GET /resources/{id}/relation-rules`: source-specific relation types, target resource types, and same-environment constraints consumed by the console; writes revalidate the same server matrix.
- `GET /resources/{id}/topology` and `GET /environments/{id}/topology`: authenticated topology reads with default depth 2, optional environment-scoped root selection, output caps, and required `truncated`.
- `GET /audit-events?q=...&environmentId=...`: optional search and positive target-resource environment filtering, combined with the existing filters; targetless events do not match an environment.
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
