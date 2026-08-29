# Migrations Module

Forward and rollback MySQL schema/data migrations applied in numeric order.

## Files

| File | Responsibility |
|------|---------------|
| 00027_collector_scan_lifecycle.sql | Adds the idempotent per-principal completed-scan ledger and capped per-principal/per-CI Missing state |
| 00026_machine_query_evidence_identity.sql | Makes query execution evidence exactly one-of user/machine actor and audit evidence at-most-one, with machine lookup indexes, no foreign keys, and guarded rollback |
| 00025_machine_principals.sql | Adds FK-free machine principals and independently expiring/revocable hash-only scoped credentials |
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
- `machine_principals` and `machine_principal_credentials` with stable lookup IDs, SHA-256 hashes, closed-scope JSON, expiry, last-use, revoke, and rotation lineage.
- Nullable `actor_machine_principal_id` evidence columns: query executions enforce exactly one user/machine actor; audit events allow at most one; both stay FK-free and indexed for actor history.
- `collector_scan_ledger` keeps the SHA-256 payload hash and terminal result under unique `(machine_principal_id, collector_scan_id)` idempotency.
- `collector_ci_scan_states` keeps last-seen/last-completed ledger truth, caps complete-scan omissions at three, and makes `missing_since` non-null exactly at that cap; identity integrity remains application-owned without foreign keys.

## Dependencies

- Upstream: MySQL 8.0 schema state through migration 00026.
- Downstream: `internal/repository/mysql` queries and integration tests.

## Update Rule

If schema members or contracts change, update this file with the migration.
