# Service Module

Business logic layer with interface-based repository dependencies. Each service defines its own repository interface where it's used.

## Files
| File | Responsibility |
|------|---------------|
| resource_service.go | Resource listing, detail, profile projection |
| relation_service.go | Relation listing by resource |
| audit_service.go | Audit event listing (global and per-resource) |
| auth_service.go | Login, token generation, credential validation |
| environment_service.go | Environment listing |
| owner_service.go | Owner listing |
| role_service.go | Role listing |
| resource_type_service.go | Resource type dictionary listing |
| relation_type_service.go | Relation type dictionary listing |
| lifecycle_status_service.go | Lifecycle status dictionary listing |
| health_status_service.go | Health status dictionary listing |
| auth_service_test.go | Auth service tests |
| dictionary_service_test.go | Dictionary service tests |

## Exports
- `NewXxxService(repo) *XxxService` constructors for all services
- `ErrResourceNotFound`, `ErrInvalidCredentials` sentinel errors

## Dependencies
- Upstream: `internal/model` (domain types)
- Downstream: `internal/repository/mysql` (implements the interfaces)

## Update Rule
If services or repository interfaces change, update this file.
