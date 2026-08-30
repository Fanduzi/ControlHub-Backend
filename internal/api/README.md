# API Module

HTTP handlers, chi routing, CORS middleware, and fake-repo test infrastructure.

## Files
| File | Responsibility |
|------|---------------|
| router.go | Route registration, including user-or-machine scoped inventory/dictionary/named-view/topology reads, reserved collector-scoped ingestion/health routes with an admin User alternative, user-only ordinary mutations, Operator Access Boundary middleware, Dependencies, CORS, and bounded auth-audit wiring; `chmp_` credentials never fall back to User auth |
| ingestion_handler.go | Bounded strict multipart CSV/JSON ingestion preview plus User confirmation or verified collector confirmation with required scan metadata and controlled retry conflict mapping |
| health_handler.go | GET /health endpoint |
| resource_handler.go | Resource list/detail identity, health, and server-derived completeness fields; explicit identity conflicts, machine observations attributed to the verified stable principal name, non-audited observation ingestion, and PATCH changes through atomic inventory audit |
| profile_handler.go | PUT/PATCH/DELETE /resources/{id}/profile handlers with strict decoding and token-derived atomic inventory audit |
| relation_handler.go | Resource relation reads, source-specific rule discovery, and token-derived atomic audited create/delete handlers |
| topology_handler.go | Resource-rooted and environment-scoped topology workspace read handlers |
| audit_handler.go | Audit event list handlers (global and per-resource), including pagination, target-resource/environment filters, search, and inventory field changes |
| auth_handler.go | POST /auth/login handler |
| auth_middleware.go | User Bearer, role, Authorization Version, and query-freshness middleware; machine-prefixed credentials are rejected without User/session fallback |
| machine_credential_middleware.go | Independent opaque machine authentication and the shared user-or-machine scope guard |
| machine_principal_handler.go | Admin-only machine principal create/list and credential rotate/revoke handlers |
| dictionary_handler.go | Dictionary list handlers (environments, owners, roles, resource-types, relation-types, lifecycle-statuses, health-statuses) |
| query_schema_handler.go | handleGetTableDefinition for MySQL table-definition requests |
| query_execution_handler.go | User-or-machine POST ordinary execute plus user-only saved-statement execution, related-record navigation, and execution-history handlers; machine identity is passed without synthesizing a User; execute/related disclosure blocks publish `query_result_disclosure_blocked` |
| query_credential_handler.go | Phase 38A credential metadata handlers (GET/PUT/DELETE) |
| query_disclosure_handler.go | Phase 38Q disclosure policy CRUD/list handlers (handler-admin) |
| query_saved_statement_handler.go | Phase 38W saved statement CRUD handlers with strict typed parameter declaration decoding |
| named_inventory_view_handler.go | User personal/shared named-view CRUD plus machine-only `ListShared` reads |
| legacy_hash_handler.go | Admin-only GET /admin/legacy-hash-count — non-identity-bearing legacy password hash count |
| json_body.go | Shared strict JSON body decoding with unknown-field and multiple-value rejection |
| test_server.go | Fake repositories, including injectable collector-ingestion results and propagation capture, and NewTestServer() with a default admin actor for handler tests |
| health_handler_test.go | Health endpoint tests |
| resource_handler_test.go | Resource and profile endpoint tests, including list/detail completeness projection, strict rejection of client completeness, governed identity, explicit conflicts, immutable origin, create-with-profile atomicity, minimum manual identity, and all typed-profile identities at the HTTP seam |
| profile_handler_test.go | PUT full-replacement and PATCH partial-merge tests: strict JSON decoding, field validation, no-op empty PATCH, Domain Name normalization, and Database Proxy role contract |
| relation_handler_test.go | Relation endpoint tests |
| topology_handler_test.go | Topology endpoint tests, including default depth, deeper traversals, and environment workspace starts |
| audit_handler_test.go | Audit list contract tests, including field-level before/after changes |
| auth_handler_test.go | Auth endpoint tests |
| dictionary_handler_test.go | Dictionary endpoint tests |
| query_credential_handler_test.go | Credential metadata handler tests |
| query_disclosure_handler_test.go | Disclosure policy handler tests, including list `query_result_disclosure_blocked` |
| query_execution_handler_test.go | Query execution handler tests, including authenticated execution identity and Preflight/Apply-path `query_result_disclosure_blocked` vs `query_not_allowed` |
| navigate_related_records_handler_test.go | Related-record navigation handler tests, including Preflight and Apply-path disclosure vs not-allowed Controlled Error Codes |
| query_saved_statement_handler_test.go | Saved statement handler tests |
| query_saved_statement_execution_handler_test.go | Template-execution handler tests (strict request decoding, controlled field errors, `query_result_disclosure_blocked`) |
| named_inventory_view_handler_test.go | Named inventory view router tests, including authentication for every CRUD route, shared-management metadata, strict JSON, and controlled errors |
| ingestion_handler_test.go | Admin multipart ingestion preview/User-confirm compatibility plus collector scan-metadata validation, propagation, and 409 conflict tests |
| machine_route_scope_test.go | Closed table-driven machine read/collector route-scope matrix, verified machine health observer attribution, collector denial of ordinary patch/archive, truthful machine execute identity, user-only sibling query routes, and secret-safe controlled error tests |
| operator_access_boundary_test.go | Anonymous, editor, and admin router authorization matrix driven by the shared operatoraccess policy, including 38R conditional saved statements (personal by owner — editor or admin — and shared templates admin-only) |
| ops_handler.go | Admin-only operational metrics handlers: `handleAuthAuditMetrics` (auth audit persistence failures + untrusted-Bearer suppression) and `handleQueryEvidenceMetrics` (Issue #34 — exactly `queryEvidencePersistenceFailures`, read through the service layer) |
| query_evidence_metrics_test.go | Query-evidence metrics endpoint tests: anonymous/editor/admin 401/403/200 matrix, exactly-one-field response, published counter |

## Routes
| Method | Path | Description |
|--------|------|-------------|
| GET | /resources and inventory dictionaries | `inventory:read` machine scope or authenticated user |
| GET | /resources/{id}/relations, relation-rules, members, topology | `relations:read` machine scope or authenticated user |
| GET | /query-targets | `governed-select` machine scope or authenticated user |
| POST | /query-targets/{id}/execute | `governed-select` machine scope or fresh authenticated user; records principal identity, never credential identity/material |
| GET | /audit-events and /resources/{id}/audit-events | `audit:read` machine scope or admin user |
| GET | /inventory/views | `named-views:read` returns shared views only for machines; users retain personal/shared behavior |
| GET/POST | /admin/machine-principals | Admin machine-principal administration; GET includes only credential IDs and lifecycle timestamps for reload-safe rotate/revoke |
| POST | /admin/machine-credentials/{credentialId}/rotate or /revoke | Admin user credential lifecycle administration |
| POST | /resources/{id}/health-observations | `health:write` machine scope or admin User; machine observations replace the request observer with `machine:<verified principal name>`, while User observations retain their submitted observer |
| POST | /admin/ingestions/preview | `inventory:ingest` machine scope or admin User; bounded CSV/JSON upload preview with no writes and exact-match create/update/conflict fingerprint |
| POST | /admin/ingestions/confirm | `inventory:ingest` machine scope or admin User; User keeps `format`, `file`, and `fingerprint`, while verified collectors additionally require bounded `collectorScanId` and `collectorScanResult` (`complete`, `incomplete`, or `failed`); changed reuse returns 409 `collector_scan_conflict` |

`GET /resources` supports inventory `q` search, exact `ownerId`, and repeatable exact `label=key:value` filters; repeated labels combine with AND.
| GET | /resources/{id}/relation-rules | Discover server-owned outgoing relation and target constraints |
| GET | /resources | List resources with stable pagination and existing filters plus free-text `q`, exact `ownerId`, and repeatable AND-combined `label=key:value` filters |
| GET | /inventory/views | List the actor's personal views and all shared views |
| POST | /inventory/views | Create a personal view, or an admin-only shared view |
| PUT | /inventory/views/{viewId} | Rename/replace an owned personal view or admin-managed shared view |
| DELETE | /inventory/views/{viewId} | Delete an owned personal view or admin-managed shared view |
| POST | /resources/bulk-mutations/preview | Admin-only side-effect-free bulk mutation preview with ordered per-CI diffs/errors and a review fingerprint |
| POST | /resources/bulk-mutations/confirm | Admin-only reviewed bulk mutation confirmation; current-state or fingerprint conflicts return 409 |
| GET | /resources/{id}/topology | Get a rooted topology graph; depth defaults to 2 and larger depths are bounded by output caps |
| GET | /environments/{id}/topology | Get an environment-scoped topology workspace; `rootResourceId` is optional |
| GET | /query-targets/{id}/schema/table-definition | Get MySQL table definition (base tables only) |
| POST | /query-targets/{id}/execute | Execute a governed read-only statement as a fresh user or `governed-select` machine principal, with optional page-number result paging for SELECT |
| POST | /query-targets/{id}/related-records | Governed FK related-record navigation (Issue #36: records through the same atomic Execution Evidence Pair as execution) |
| GET | /query-disclosure-policies | List disclosure policies (handler-admin) |
| POST | /query-disclosure-policies | Create a disclosure policy (handler-admin) |
| PUT | /query-disclosure-policies | Update a disclosure policy (handler-admin) |
| DELETE | /query-disclosure-policies | Delete a disclosure policy (handler-admin) |
| GET | /ops/query-evidence-metrics | Admin-only query-evidence persistence-failure counter (Issue #34) |

Disclosure policy error mapping: a duplicate-scope POST answers `409` with the
`disclosure_policy_conflict` code, and updating a scope with no existing policy
answers `404` with `disclosure_policy_not_found`. Execute and related-record
disclosure blocks — including Apply after a successful executor run — publish
`403` with `query_result_disclosure_blocked`; target-not-enabled refusals
remain `query_not_allowed`.
| GET | /query-targets/{id}/saved-statements | List saved statements for a query target |
| POST | /query-targets/{id}/saved-statements | Create a saved statement |
| PUT | /query-targets/{id}/saved-statements/{statementId} | Update a saved statement |
| DELETE | /query-targets/{id}/saved-statements/{statementId} | Delete a saved statement |
| POST | /query-targets/{id}/saved-statements/{statementId}/execute | Execute a saved statement (governed template execution) |

## Exports
- `Dependencies` struct — all service dependencies
- `NewRouter(deps Dependencies) *chi.Mux` — wired router

## Execute result paging

`POST /query-targets/{id}/execute` accepts the existing statement request with an
optional `pagination` object containing a 1-based `page` and a `pageSize` of
10, 25, 50, or 100. The response includes `pagination` only for paged bare
`SELECT` statements. It reports the page, page size, and adjacent-page flags,
not totals or snapshot identifiers.

The server owns the page window and effective row cap. It validates the page,
applies the window through the SQL AST, and never relies on browser SQL
rewriting. Each page is a fresh governed request with target access, credential,
statement guard, disclosure policy, timeout, cap, history, and audit checks.
Result rows are not persisted and no snapshot is retained between pages.

`SHOW`, `DESCRIBE`, and typed `EXPLAIN` remain single-response metadata
statements. A supplied pagination object does not split or navigate their
responses.

## Dependencies
- Upstream: `internal/service` (all services), `github.com/go-chi/chi/v5`
- Downstream: none (HTTP layer is the top)

## Credential Freshness

All protected routes and governed-query routes reject a bearer credential at
an age of eight hours or greater. The bound is fixed and cannot be extended by
deployment configuration.

## Update Rule
If routes, handlers, or Dependencies struct change, update this file and root README.md.
