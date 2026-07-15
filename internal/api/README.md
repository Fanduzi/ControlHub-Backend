# API Module

HTTP handlers, chi routing, CORS middleware, and fake-repo test infrastructure.

## Files
| File | Responsibility |
|------|---------------|
| router.go | Route registration, Dependencies struct, CORS middleware |
| health_handler.go | GET /health endpoint |
| resource_handler.go | Resource list, detail, and profile handlers |
| relation_handler.go | Resource relation list handler |
| audit_handler.go | Audit event list handlers (global and per-resource) |
| auth_handler.go | POST /auth/login handler |
| dictionary_handler.go | Dictionary list handlers (environments, owners, roles, resource-types, relation-types, lifecycle-statuses, health-statuses) |
| query_schema_handler.go | Query schema metadata handlers, including GET /query-targets/{id}/schema/table-definition |
| test_server.go | Fake repositories and NewTestServer() for handler tests |
| health_handler_test.go | Health endpoint tests |
| resource_handler_test.go | Resource and profile endpoint tests |
| relation_handler_test.go | Relation endpoint tests |
| auth_handler_test.go | Auth endpoint tests |
| dictionary_handler_test.go | Dictionary endpoint tests |

## Exports
- `Dependencies` struct — all service dependencies
- `NewRouter(deps Dependencies) *chi.Mux` — wired router

## Query schema routes

- `GET /query-targets/{id}/schema/table-definition?database={database}&name={name}` — returns the governed MySQL `SHOW CREATE TABLE` definition for a verified base table.

## Dependencies
- Upstream: `internal/service` (all services), `github.com/go-chi/chi/v5`
- Downstream: none (HTTP layer is the top)

## Update Rule
If routes, handlers, or Dependencies struct change, update this file and root README.md.
