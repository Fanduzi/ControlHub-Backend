# MySQL Repository Module

Data access layer implementing service-layer repository interfaces with raw SQL queries. No ORM.

## Files
| File | Responsibility |
|------|---------------|
| resource_repository.go | Resource CRUD, profile queries, type-specific profile scanning |
| relation_repository.go | Relation queries by resource ID |
| audit_repository.go | Audit event queries (global and by resource) |
| user_repository.go | User credential lookup by email |
| dictionary_repository.go | Dictionary queries — DB-backed (environments, owners, roles) and static (resource types, relation types, lifecycle/health statuses) |

## Exports
- `NewResourceRepository(db)`, `NewRelationRepository(db)`, `NewAuditRepository(db)`, `NewUserRepository(db)`, `NewDictionaryRepository(db)` — constructor functions
- Repository structs satisfy service-layer interfaces

## Dependencies
- Upstream: `internal/model` (domain types), `database/sql`
- Downstream: MySQL 8.0+ (schema defined in migrations/)

## Update Rule
If queries or table structures change, update this file and the corresponding migration.
