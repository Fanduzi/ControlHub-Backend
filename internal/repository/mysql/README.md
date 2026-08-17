# MySQL Repository Module

Data access layer implementing service-layer repository interfaces with raw SQL queries. No ORM.

## Files
| File | Responsibility |
|------|---------------|
| resource_repository.go | Resource CRUD, profile queries, type-specific profile scanning, transactional create-with-profile (resource + initial profile roll back together), atomic COALESCE partial profile merge (PatchProfile) |
| relation_repository.go | Relation queries by resource ID |
| audit_repository.go | Audit event queries (global and by resource) |
| user_repository.go | User credential lookup by email/id; Authorization Version mutators (role/active/password); UpgradePasswordHash for legacy-to-Argon2id migration; CountLegacyHashUsers for operator visibility |
| dictionary_repository.go | Dictionary queries — DB-backed (environments, owners, roles) and static (resource types, relation types, lifecycle/health statuses) |
| query_execution_repository.go | Query credential metadata (get/upsert/delete with audit), atomic `InsertExecutionWithAudit` Execution Evidence Pair (Issue #34), execution history, audit events, `QueryEvidencePersistenceFailures` counter |
| query_target_repository.go | Read-only query target read model — joins database_instance resources with profiles, environments, owners, and cluster membership |
| query_disclosure_repository.go | Phase 38Q governed result-disclosure policy CRUD (insert/update/delete/list/get by scope); duplicate scope insert maps MySQL 1062 to `ErrQueryDisclosurePolicyConflict`, update/get of a missing scope returns `sql.ErrNoRows` (fail-closed), delete is idempotent |
| query_saved_statement_repository.go | Phase 38W governed saved statements CRUD with ordered parameter definitions and atomic audit (create/update/delete with audit, list with visibility, get by ID) |
| auth_audit_emitter.go | MySQL-backed AuthAuditEmitter — fail-open INSERT for auth/authz audit events; AuthAuditPersistenceFailures fixed-category counter |

## Exports
- `NewResourceRepository(db)`, `NewRelationRepository(db)`, `NewAuditRepository(db)`, `NewUserRepository(db)`, `NewDictionaryRepository(db)`, `NewAuthAuditEmitter(db)` — constructor functions
- `NewQueryExecutionRepository(db)`, `NewQueryTargetRepository(db)`, `NewQueryDisclosureRepository(db)`, `NewQuerySavedStatementRepository(db)` — constructor functions
- `QueryDisclosureReader`, `QueryDisclosureWriter` — narrow service-owned interfaces for disclosure policy access
- `QuerySavedStatementReader`, `QuerySavedStatementWriter` — narrow service-owned interfaces for saved statement access, including atomic parameter-definition replacement
- `QueryEvidencePersistenceFailures` — dimensionless expvar counter for atomic Execution Evidence Pair persistence failures (Issue #34)
- `QueryExecutionRepository.InsertExecutionWithAudit` — repository-owned atomic Execution Evidence Pair: one transaction commits the history row and its fixed audit event; on any failure both roll back, the counter increments once, and one fixed safe log line is emitted
- Repository structs satisfy service-layer interfaces

## Dependencies
- Upstream: `internal/model` (domain types), `database/sql`
- Downstream: MySQL 8.0+ (schema defined in migrations/)

## Update Rule
If queries or table structures change, update this file and the corresponding migration.
