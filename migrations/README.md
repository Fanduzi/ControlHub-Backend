# Migrations Module

Forward and rollback MySQL schema/data migrations applied in numeric order.

## Files

| File | Responsibility |
|------|---------------|
| 00023_observed_effective_values.sql | Adds FK-free per-CI/source/field observations and versioned manual overrides after migration 00022 |
| 00022_resource_health_observations.sql | Makes the legacy health column a nullable manual override and stores one latest observation per resource/observer |
| 00018_inventory_audit_changes.sql | Adds JSON field-diff evidence to inventory audit events |
| 0001_initial_schema.sql–00017_auth_audit_nullable_actor.sql | Baseline schema and prior forward changes |

## Interfaces

- Goose `Up` and `Down` sections consumed by server/test migration runners.
- `resource_health_observations(resource_id, observer, health_status, observed_at)` latest-evidence table.
- Nullable `resources.health_status` manual override; rollback maps cleared values to `unknown` before restoring `NOT NULL`.
- `resource_observed_values` and `resource_manual_overrides` effective-value tables without foreign keys.

## Dependencies

- Upstream: MySQL 8.0 schema state through migration 00022.
- Downstream: `internal/repository/mysql` queries and integration tests.

## Update Rule

If schema members or contracts change, update this file with the migration.
