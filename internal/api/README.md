# API Module

HTTP handlers, chi routing, CORS middleware, and fake-repo test infrastructure.

## Files
| File | Responsibility |
|------|---------------|
| router.go | Route registration, Operator Access Boundary middleware, Dependencies struct, CORS middleware |
| health_handler.go | GET /health endpoint |
| resource_handler.go | Resource list, detail, and profile handlers |
| relation_handler.go | Resource relation list handler |
| audit_handler.go | Audit event list handlers (global and per-resource) |
| auth_handler.go | POST /auth/login handler |
| auth_middleware.go | Bearer, role, Authorization Version, and query-freshness middleware |
| dictionary_handler.go | Dictionary list handlers (environments, owners, roles, resource-types, relation-types, lifecycle-statuses, health-statuses) |
| query_schema_handler.go | handleGetTableDefinition for MySQL table-definition requests |
| query_execution_handler.go | POST execute, POST saved-statement template execute, and GET execution-history handlers, including optional governed result paging |
| query_credential_handler.go | Phase 38A credential metadata handlers (GET/PUT/DELETE) |
| query_disclosure_handler.go | Phase 38Q disclosure policy CRUD handlers (admin-only writes) |
| query_saved_statement_handler.go | Phase 38W saved statement CRUD handlers with strict typed parameter declaration decoding |
| json_body.go | Shared strict JSON body decoding with unknown-field and multiple-value rejection |
| test_server.go | Fake repositories and NewTestServer() with a default admin actor for handler tests |
| health_handler_test.go | Health endpoint tests |
| resource_handler_test.go | Resource and profile endpoint tests |
| relation_handler_test.go | Relation endpoint tests |
| auth_handler_test.go | Auth endpoint tests |
| dictionary_handler_test.go | Dictionary endpoint tests |
| query_credential_handler_test.go | Credential metadata handler tests |
| query_disclosure_handler_test.go | Disclosure policy handler tests |
| query_saved_statement_handler_test.go | Saved statement handler tests |
| query_saved_statement_execution_handler_test.go | Template-execution handler tests (strict request decoding, controlled field errors) |
| operator_access_boundary_test.go | Anonymous, editor, and admin router authorization matrix |

## Routes
| Method | Path | Description |
|--------|------|-------------|
| GET | /query-targets/{id}/schema/table-definition | Get MySQL table definition (base tables only) |
| POST | /query-targets/{id}/execute | Execute a governed read-only statement, with optional page-number result paging for SELECT |
| GET | /query-disclosure-policies | List disclosure policies for a query target |
| POST | /query-disclosure-policies | Create a disclosure policy (admin-only) |
| PUT | /query-disclosure-policies | Update a disclosure policy (admin-only) |
| DELETE | /query-disclosure-policies | Delete a disclosure policy (admin-only) |
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

## Update Rule
If routes, handlers, or Dependencies struct change, update this file and root README.md.
