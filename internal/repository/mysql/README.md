# MySQL Repository Module

Data access layer implementing service-layer repository interfaces with raw SQL queries. No ORM.

## Files
| File | Responsibility |
|------|---------------|
| resource_repository.go | Resource CRUD, governed identity and one-query batched typed-profile reads, latest-per-observer health evidence, effective-health derivation, per-source observed/effective values, atomic audited identity/manual-override mutations, and reviewed bulk mutations in one transaction |
| bulk_resource_mutation_test.go | SQL-level bulk confirmation transaction coverage using current resource and governed-identity lock queries, including commit and audit-failure rollback |
| relation_repository.go | Relation queries plus atomic create/delete, effective-health relation/member projections, and topology resource lookup |
| audit_repository.go | Audit event queries (global and by resource), including pagination, actor/resource search, and JSON field changes |
| user_repository.go | User credential lookup by email/id; Authorization Version mutators (role/active/password); UpgradePasswordHash for legacy-to-Argon2id migration; CountLegacyHashUsers for operator visibility |
| dictionary_repository.go | Dictionary queries — DB-backed (environments, owners, roles) and static (resource types, relation types, lifecycle/health statuses) |
| query_execution_repository.go | Query credential metadata (get/upsert/delete with audit), atomic `InsertExecutionWithAudit` Execution Evidence Pair (Issues #34/#36) with per-caller fixed audit event type, execution history, audit-only `InsertAuditEvent`, `QueryEvidencePersistenceFailures` counter |
| query_target_repository.go | Read-only query target read model — joins database_instance resources with profiles, environments, owners, and cluster membership |
| query_disclosure_repository.go | Phase 38Q governed result-disclosure policy CRUD (insert/update/delete/list/get by scope); duplicate scope insert maps MySQL 1062 to `ErrQueryDisclosurePolicyConflict`, update/get of a missing scope returns `sql.ErrNoRows` (fail-closed), delete is idempotent |
| query_saved_statement_repository.go | Phase 38W governed saved statements CRUD with ordered parameter definitions and atomic audit (create/update/delete with audit, list with visibility, get by ID) |
| named_inventory_view_repository.go | Personal/shared named inventory view CRUD, owner-visible listing, and an actor-free shared-only read seam for future machine principals |
| auth_audit_emitter.go | MySQL-backed AuthAuditEmitter — fail-open INSERT for auth/authz audit events; AuthAuditPersistenceFailures fixed-category counter |

## Exports
- `NewResourceRepository(db)`, `NewRelationRepository(db)`, `NewAuditRepository(db)`, `NewUserRepository(db)`, `NewDictionaryRepository(db)`, `NewAuthAuditEmitter(db)` — constructor functions
- `ResourceRepository.UpsertHealthObservation` and `SetManualHealthOverrideWithAudit` — non-audited operational evidence and audited nullable override persistence
- `NewQueryExecutionRepository(db)`, `NewQueryTargetRepository(db)`, `NewQueryDisclosureRepository(db)`, `NewQuerySavedStatementRepository(db)` — constructor functions
- `NewNamedInventoryViewRepository(db)` — named inventory view persistence constructor
- `QueryDisclosureReader`, `QueryDisclosureWriter` — narrow service-owned interfaces for disclosure policy access
- `QuerySavedStatementReader`, `QuerySavedStatementWriter` — narrow service-owned interfaces for saved statement access, including atomic parameter-definition replacement
- `QueryEvidencePersistenceFailures` — dimensionless expvar counter for atomic Execution Evidence Pair persistence failures (Issue #34), readable through the repository's `QueryEvidencePersistenceFailures()` accessor for the service layer
- `QueryExecutionRepository.InsertExecutionWithAudit` — repository-owned atomic Execution Evidence Pair: one transaction commits the execution-history row and its fixed per-caller audit event (service passes `query.executed` for execution and `related_record_navigation` for navigation, Issue #36); on any failure both roll back, the counter increments once, and one fixed safe log line is emitted
- `ResourceRepository.ConfirmBulkResourceMutation` — stable-order resource locking, in-transaction re-preview/fingerprint verification, typed field and explicit label mutation, and per-CI field audit in one commit
- Repository structs satisfy service-layer interfaces
- `ResourceRepository.PutObservedValues`, `GetEffectiveValues`, `SetManualOverrideWithAudit`, and `ClearManualOverrideWithAudit`

Inventory audit writes are fail-closed. Resource identity, typed-profile, and
relationship mutations use the shared `AuditChange` representation; audit
insert or commit failure rolls the inventory mutation back. Relationship
mutations write separate target-specific evidence for both affected CIs.
Observed refreshes preserve sibling sources. Manual overrides use optimistic
versions and win only in the effective read projection; clearing one exposes
the latest observation immediately.

`resource_health_observations` stores one current row per resource/observer.
Older evidence cannot replace a newer timestamp. Effective-health filters use
the same conservative Go calculation as list/detail reads; this path scans the
matched inventory set until scale justifies a dedicated indexed read model.

## Dependencies
- Upstream: `internal/model` (domain types), `database/sql`
- Downstream: MySQL 8.0+ (schema defined in migrations/)

## Update Rule
If queries or table structures change, update this file and the corresponding migration.
