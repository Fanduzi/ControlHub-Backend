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
| query_execution_repository.go | Query credential metadata (get/upsert/delete with audit), execution history, audit events |
| query_target_repository.go | Read-only query target read model — joins database_instance resources with profiles, environments, owners, and cluster membership |
| query_disclosure_repository.go | Phase 38Q governed result-disclosure policy CRUD (insert/update/delete/list/get by scope) |
| query_saved_statement_repository.go | Phase 38W governed saved statements CRUD with ordered parameter definitions and atomic audit (create/update/delete with audit, list with visibility, get by ID) |

## Exports
- `NewResourceRepository(db)`, `NewRelationRepository(db)`, `NewAuditRepository(db)`, `NewUserRepository(db)`, `NewDictionaryRepository(db)` — constructor functions
- `NewQueryExecutionRepository(db)`, `NewQueryTargetRepository(db)`, `NewQueryDisclosureRepository(db)`, `NewQuerySavedStatementRepository(db)` — constructor functions
- `QueryDisclosureReader`, `QueryDisclosureWriter` — narrow service-owned interfaces for disclosure policy access
- `QuerySavedStatementReader`, `QuerySavedStatementWriter` — narrow service-owned interfaces for saved statement access, including atomic parameter-definition replacement
- Repository structs satisfy service-layer interfaces

## Dependencies
- Upstream: `internal/model` (domain types), `database/sql`
- Downstream: MySQL 8.0+ (schema defined in migrations/)

## Update Rule
If queries or table structures change, update this file and the corresponding migration.
