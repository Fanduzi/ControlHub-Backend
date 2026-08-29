# API Module

HTTP handlers, chi routing, CORS middleware, and fake-repo test infrastructure.

## Files
| File | Responsibility |
|------|---------------|
| router.go | Route registration, Operator Access Boundary middleware (authenticated Inventory/dictionary reads; fresh-token query surfaces; admin-gated ops metrics), Dependencies struct, CORS middleware, process-shared bounded untrusted-Bearer audit persistence wiring |
| health_handler.go | GET /health endpoint |
| resource_handler.go | Resource list/detail handlers; authenticated PATCH routes identity and status changes through atomic inventory audit |
| profile_handler.go | PUT/PATCH/DELETE /resources/{id}/profile handlers with strict decoding and token-derived atomic inventory audit |
| relation_handler.go | Resource relation reads and token-derived atomic audited create/delete handlers |
| audit_handler.go | Audit event list handlers (global and per-resource), including inventory field changes |
| auth_handler.go | POST /auth/login handler |
| auth_middleware.go | Bearer, role, Authorization Version, and query-freshness middleware; missing Authorization emits no audit event; supplied untrusted Bearer rejection emits the fixed event within the 60/min per-process budget |
| dictionary_handler.go | Dictionary list handlers (environments, owners, roles, resource-types, relation-types, lifecycle-statuses, health-statuses) |
| query_schema_handler.go | handleGetTableDefinition for MySQL table-definition requests |
| query_execution_handler.go | POST execute, POST saved-statement template execute, POST related-records, and GET execution-history handlers, including optional governed result paging; execute/related disclosure blocks publish `query_result_disclosure_blocked` |
| query_credential_handler.go | Phase 38A credential metadata handlers (GET/PUT/DELETE) |
| query_disclosure_handler.go | Phase 38Q disclosure policy CRUD/list handlers (handler-admin) |
| query_saved_statement_handler.go | Phase 38W saved statement CRUD handlers with strict typed parameter declaration decoding |
| legacy_hash_handler.go | Admin-only GET /admin/legacy-hash-count — non-identity-bearing legacy password hash count |
| json_body.go | Shared strict JSON body decoding with unknown-field and multiple-value rejection |
| test_server.go | Fake repositories and NewTestServer() with a default admin actor for handler tests |
| health_handler_test.go | Health endpoint tests |
| resource_handler_test.go | Resource and profile endpoint tests, including create-with-profile atomicity, minimum manual identity, service worker subtype, and Domain Name/Virtual IP identity at the HTTP seam |
| profile_handler_test.go | PUT full-replacement and PATCH partial-merge tests: strict JSON decoding, field validation, no-op empty PATCH, Domain Name FQDN normalize |
| relation_handler_test.go | Relation endpoint tests |
| audit_handler_test.go | Audit list contract tests, including field-level before/after changes |
| auth_handler_test.go | Auth endpoint tests |
| dictionary_handler_test.go | Dictionary endpoint tests |
| query_credential_handler_test.go | Credential metadata handler tests |
| query_disclosure_handler_test.go | Disclosure policy handler tests, including list `query_result_disclosure_blocked` |
| query_execution_handler_test.go | Query execution handler tests, including Preflight and Apply-path `query_result_disclosure_blocked` vs `query_not_allowed` |
| navigate_related_records_handler_test.go | Related-record navigation handler tests, including Preflight and Apply-path disclosure vs not-allowed Controlled Error Codes |
| query_saved_statement_handler_test.go | Saved statement handler tests |
| query_saved_statement_execution_handler_test.go | Template-execution handler tests (strict request decoding, controlled field errors, `query_result_disclosure_blocked`) |
| operator_access_boundary_test.go | Anonymous, editor, and admin router authorization matrix driven by the shared operatoraccess policy, including 38R conditional saved statements (personal by owner — editor or admin — and shared templates admin-only) |
| ops_handler.go | Admin-only operational metrics handlers: `handleAuthAuditMetrics` (auth audit persistence failures + untrusted-Bearer suppression) and `handleQueryEvidenceMetrics` (Issue #34 — exactly `queryEvidencePersistenceFailures`, read through the service layer) |
| query_evidence_metrics_test.go | Query-evidence metrics endpoint tests: anonymous/editor/admin 401/403/200 matrix, exactly-one-field response, published counter |

## Routes
| Method | Path | Description |
|--------|------|-------------|
| GET | /query-targets/{id}/schema/table-definition | Get MySQL table definition (base tables only) |
| POST | /query-targets/{id}/execute | Execute a governed read-only statement, with optional page-number result paging for SELECT |
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
