# Migrations Module

Forward and rollback MySQL schema/data migrations applied in numeric order.

## Files

| File | Responsibility |
|------|---------------|
| 00022_resource_health_observations.sql | Makes the legacy health column a nullable manual override and stores one latest observation per resource/observer |
| 00018_inventory_audit_changes.sql | Adds JSON field-diff evidence to inventory audit events |
| 0001_initial_schema.sql–00017_auth_audit_nullable_actor.sql | Baseline schema and prior forward changes |

## Interfaces

- Goose `Up` and `Down` sections consumed by server/test migration runners.
- `resource_health_observations(resource_id, observer, health_status, observed_at)` latest-evidence table.
- Nullable `resources.health_status` manual override; rollback maps cleared values to `unknown` before restoring `NOT NULL`.

## Dependencies

- Upstream: MySQL 8.0 schema state from the preceding migration.
- Downstream: `internal/repository/mysql` queries and integration tests.

## Update Rule

If schema members or contracts change, update this file with the migration.
