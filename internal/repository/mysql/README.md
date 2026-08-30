# MySQL Repository Module

Data access layer implementing service-layer repository interfaces with raw SQL queries. No ORM.

## Files
| File | Responsibility |
|------|---------------|
| resource_repository.go | Resource CRUD, governed identity and one-query batched typed-profile reads, latest-per-observer health evidence, effective-health derivation, per-source observed/effective values, read-only validated bulk previews, atomic audited identity/manual-override/bulk mutations, and User/collector atomic ingestion confirmation |
| bulk_resource_mutation_test.go | SQL-level bulk coverage for transaction commit/rollback, externalId persistence, locked archived-CI rejection, current resource/governed-identity lock queries, normal update validation parity, and audit-failure rollback |
| relation_repository.go | Relation queries plus atomic create/delete serialized by stable endpoint resource-row locks, effective-health relation/member projections, topology resource lookup, and environment candidate starts |
| audit_repository.go | Audit event queries (global and by resource), including nullable user/machine actor projection, target-resource environment filtering, principal-name search, pagination, and JSON field changes |
| user_repository.go | User credential lookup by email/id; Authorization Version mutators (role/active/password); UpgradePasswordHash for legacy-to-Argon2id migration; CountLegacyHashUsers for operator visibility |
| dictionary_repository.go | Dictionary queries — DB-backed (environments, owners, roles) and static (resource types, relation types, lifecycle/health statuses) |
| query_execution_repository.go | Query credential metadata plus atomic identity-aware `InsertExecutionWithAudit`; full statements persist only for successful user executions, stay absent from list/audit projections, and are readable only for an exact owner/user/success match |
| query_execution_statement_repository_test.go | Successful-user full-statement insert args, machine/non-success NULL binding, audit omission, owner-only retrieval predicates, and denial-as-no-row coverage |
| query_workspace_repository.go | One JSON aggregate row per owner with missing-as-version-zero reads and optimistic insert/update conflict handling |
| query_workspace_repository_test.go | Missing/owner-scoped reads, invalid/deleted target preservation, exact SQL args, duplicate conflict, and OCC race coverage |
| query_target_repository.go | Read-only query target read model — joins database_instance resources with profiles, environments, owners, and cluster membership |
| query_disclosure_repository.go | Phase 38Q governed result-disclosure policy CRUD (insert/update/delete/list/get by scope); duplicate scope insert maps MySQL 1062 to `ErrQueryDisclosurePolicyConflict`, update/get of a missing scope returns `sql.ErrNoRows` (fail-closed), delete is idempotent |
| query_saved_statement_repository.go | Phase 38W governed saved statements CRUD with ordered parameter definitions and atomic audit (create/update/delete with audit, list with visibility, get by ID) |
| named_inventory_view_repository.go | Personal/shared named inventory view CRUD, owner-visible listing, and an actor-free shared-only read seam for future machine principals |
| machine_principal_repository.go | Single-query safe lifecycle aggregation plus atomic machine-principal create/credential rotate/revoke with admin audit, hash lookup, and idempotent state-bounded last-used updates |
| machine_principal_repository_test.go | SQL lifecycle, hash-only arguments, safe audit payload, and rollback coverage with sqlmock |
| collector_scan_repository.go | Locked exact/conflicting retry lookup plus caller-transaction completed-scan ledger and deterministic per-principal/per-CI state application |
| collector_scan_repository_test.go | SQL ledger, rediscovery, third-omission, non-complete no-op, deterministic-order, and caller-owned rollback coverage with sqlmock |
| auth_audit_emitter.go | MySQL-backed AuthAuditEmitter — fail-open INSERT for auth/authz audit events; AuthAuditPersistenceFailures fixed-category counter |

## Exports
- `NewResourceRepository(db)`, `NewRelationRepository(db)`, `NewAuditRepository(db)`, `NewUserRepository(db)`, `NewDictionaryRepository(db)`, `NewAuthAuditEmitter(db)` — constructor functions
- `ResourceRepository.UpsertHealthObservation` and `SetManualHealthOverrideWithAudit` — non-audited operational evidence and audited nullable override persistence
- `NewQueryExecutionRepository(db)`, `NewQueryTargetRepository(db)`, `NewQueryDisclosureRepository(db)`, `NewQuerySavedStatementRepository(db)`, `NewQueryWorkspaceRepository(db)` — constructor functions
- `NewNamedInventoryViewRepository(db)` — named inventory view persistence constructor
- `NewMachinePrincipalRepository(db)` — machine-principal lifecycle and credential-authentication persistence constructor
- `ClaimCollectorScan(ctx, tx, entry)` and `ApplyCollectorScanStates(ctx, tx, entry, seenResourceIDs)` — two-phase caller-owned primitive that serializes retry identity before ingestion, then applies per-principal CI states after ingestion; `ApplyCollectorScan` composes both for callers without intervening writes
- `QueryDisclosureReader`, `QueryDisclosureWriter` — narrow service-owned interfaces for disclosure policy access
- `QuerySavedStatementReader`, `QuerySavedStatementWriter` — narrow service-owned interfaces for saved statement access, including atomic parameter-definition replacement
- `QueryEvidencePersistenceFailures` — dimensionless expvar counter for atomic Execution Evidence Pair persistence failures (Issue #34), readable through the repository's `QueryEvidencePersistenceFailures()` accessor for the service layer
- `QueryExecutionRepository.InsertExecutionWithAudit` — repository-owned atomic Execution Evidence Pair: one transaction commits the execution-history row and its fixed per-caller audit event (service passes `query.executed` for execution and `related_record_navigation` for navigation, Issue #36); on any failure both roll back, the counter increments once, and one fixed safe log line is emitted
- `QueryExecutionRepository.GetSuccessfulExecutionStatement` — exact execution/target/user lookup restricted to successful user-attributed rows with non-null full SQL; every mismatch is `sql.ErrNoRows`
- `QueryWorkspaceRepository.Get` / `Put` and `ErrQueryWorkspaceConflict` — missing-as-version-zero aggregate reads plus single-statement optimistic concurrency writes
- `ResourceRepository.ConfirmBulkResourceMutation` — stable-order resource locking, normal update revalidation, in-transaction re-preview/fingerprint verification, typed field and explicit label mutation, and per-CI field audit in one commit
- Repository structs satisfy service-layer interfaces
- `ResourceRepository.PutObservedValues`, `GetEffectiveValues`, `SetManualOverrideWithAudit`, `ClearManualOverrideWithAudit`, `PreviewIngestion`, `ConfirmIngestion`, and `ConfirmCollectorIngestion`
- `RelationRepository.ListTopologyCandidates` reuses resource reads to return environment-scoped Service, Database Cluster, Database Proxy, and abnormal CI starts for the topology workspace

Inventory audit writes are fail-closed. Resource identity, typed-profile, and
relationship mutations use the shared `AuditChange` representation; audit
insert or commit failure rolls the inventory mutation back. Relationship
mutations write separate target-specific evidence for both affected CIs.
Observed refreshes preserve sibling sources. Manual overrides use optimistic
versions and win only in the effective read projection; clearing one exposes
the latest observation immediately.

`ConfirmIngestion` receives already-parsed rows plus the reviewed preview
fingerprint and actor ID, locks matched and submitted relation-endpoint resource
rows in ID order, re-runs the service preview inside one transaction, rejects
conflicts or fingerprint drift, and commits resource, identity, typed-profile,
submitted observed-value, relation, and field-level audit writes together.
Ordinary relation create/delete uses the same stable endpoint locks, so relation
drift cannot interleave with the confirmation snapshot. Omitted observations
and sibling sources are preserved. No-op reconfirmation emits no audit row.

`ConfirmCollectorIngestion` reuses that confirmation transaction. New CIs use
`origin=discovered` with unknown owner `0`; audit rows set only
`actor_machine_principal_id`. The same transaction applies the collector scan
ledger/state after all ingestion and audit writes, so any failure rolls back
the entire confirmation. Exact receipt retries are read-only; changed payload,
fingerprint, or result reuse returns the controlled collector conflict.

`resource_health_observations` stores one current row per resource/observer.
Older evidence cannot replace a newer timestamp. Effective-health filters use
the same conservative Go calculation as list/detail reads; this path scans the
matched inventory set until scale justifies a dedicated indexed read model.

## Dependencies
- Upstream: `internal/model` (domain types), `internal/service` (repository contracts), `database/sql`
- Downstream: MySQL 8.0+ (schema defined in migrations/)

## Update Rule
If queries or table structures change, update this file and the corresponding migration.
