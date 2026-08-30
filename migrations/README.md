# Migrations Module

Forward and rollback MySQL schema/data migrations applied in numeric order.

## Files

| File | Responsibility |
|------|---------------|
| 00028_query_workspace_and_execution_statement.sql | Adds one bounded JSON workspace row per owner and nullable private full SQL without backfill; rollback refuses while either contains data |
| 00027_collector_scan_lifecycle.sql | Adds the idempotent per-principal completed-scan ledger and capped per-principal/per-CI Missing state; rollback refuses while either contains data |
| 00026_machine_query_evidence_identity.sql | Makes query execution evidence exactly one-of user/machine actor and audit evidence at-most-one, with machine lookup indexes, no foreign keys, and guarded rollback |
| 00025_machine_principals.sql | Adds FK-free machine principals and independently expiring/revocable hash-only credentials with one to seven closed scopes |
| 00024_named_inventory_views.sql | Adds reusable personal/shared Inventory view state without result snapshots |
| 00023_observed_effective_values.sql | Adds FK-free per-CI/source/field observations and versioned manual overrides after migration 00022 |
| 00022_resource_health_observations.sql | Makes the legacy health column a nullable manual override and stores one latest observation per resource/observer |
| 00018_inventory_audit_changes.sql | Adds JSON field-diff evidence to inventory audit events |
| 0001_initial_schema.sql–00017_auth_audit_nullable_actor.sql | Baseline schema and prior forward changes |

## Interfaces

- Goose `Up` and `Down` sections consumed by server/test migration runners.
- `resource_health_observations(resource_id, observer, health_status, observed_at)` latest-evidence table.
- Nullable `resources.health_status` manual override; rollback maps cleared values to `unknown` before restoring `NOT NULL`.
- `resource_observed_values` and `resource_manual_overrides` effective-value tables without foreign keys.
- `machine_principals` and `machine_principal_credentials` with stable lookup IDs, SHA-256 hashes, one to seven closed-scope JSON entries, expiry, last-use, revoke, and rotation lineage.
- Nullable `actor_machine_principal_id` evidence columns: query executions enforce exactly one user/machine actor; audit events allow at most one; both stay FK-free and indexed for actor history.
- `collector_scan_ledger` keeps the SHA-256 payload hash and terminal result under unique `(machine_principal_id, collector_scan_id)` idempotency.
- `collector_ci_scan_states` keeps last-seen/last-completed ledger truth, caps complete-scan omissions at three, and makes `missing_since` non-null exactly at that cap; identity integrity remains application-owned without foreign keys.
- Migration 00027 downgrade fails with SQLSTATE `45000` while either collector lifecycle table contains data; operators must explicitly export or purge both data sets first.
- `query_workspaces(owner_user_id, worksheets, version, updated_at)` stores one optimistic JSON worksheet aggregate per owner without target foreign keys.
- Nullable `query_executions.full_statement` stores private SQL for later exact-owner successful-execution retrieval; migration 00028 performs no legacy backfill.
- Migration 00028 downgrade fails with SQLSTATE `45000` while any workspace row or non-null full statement exists; operators must explicitly export or purge both data sets first.

## Dependencies

- Upstream: MySQL 8.0 schema state through migration 00027.
- Downstream: `internal/repository/mysql` queries and integration tests.

## Update Rule

If schema members or contracts change, update this file with the migration.
