# Integration Module

MySQL-backed integration tests run against disposable Testcontainers databases.

## Files
| File | Responsibility |
|------|---------------|
| health_observation_test.go | Real-MySQL latest observation, freshness, effective filtering, no-audit, and atomic manual override contracts |
| testenv_test.go | Starts MySQL, applies migrations, and provides database helpers |
| mysql_test.go | Exact migration-27 schema/table, collector/user/machine constraints and indexes, unsigned-ID, and no-foreign-key guards after clean migration |
| resource_test.go | Resource repository CRUD, key-presence/exact-value label filtering, constant-query batched-profile reads, observation-derived cluster rollups, create-with-profile atomicity, profile validation, and PATCH partial-merge semantics against real MySQL |
| inventory_audit_test.go | Real-MySQL inventory audit atomicity, typed-profile and relationship behavior, per-CI evidence, multi-source observations, override precedence, stale-write conflicts, clear, effective provenance, and per-CI relationship evidence |
| bulk_resource_mutation_test.go | Real-MySQL reviewed bulk success/idempotent preview/conflict, structurally decoded externalId field audit, true multi-target mid-batch rollback, audit-failure rollback, archived-CI lock validation, and two-connection lock/drift enforcement |
| ingestion_confirm_test.go | Discoverable real-MySQL User and collector tests: real-router machine-authenticated empty CSV/JSON preview-to-confirm and ledger persistence, 500-state admission rollback, issue #83 behavior plus discovered/ownerless create, machine audit attribution, server fingerprint validation, complete/incomplete/failed scans, third-omission list/detail Missing projection with source/principal and stable time, recovery, exact/conflicting retry, never-archive, and whole-transaction rollback |
| collector_scan_migration_test.go | Real-MySQL migration-27 downgrade guard: completed-scan ledger and per-CI lifecycle rows survive a refused rollback, while empty storage permits downgrade |
| typed_profile_identity_test.go | Real-MySQL create/read/edit for the four core typed profiles, minimum manual identity rejection, worker subtype, labels-as-classification, and T01 profile audit |
| resource_identity_test.go | MySQL identity normalization/uniqueness, immutable ID/origin, and atomic identity-plus-field-audit rollback coverage |
| resource_identity_migration_test.go | Fail-loud v21 migration preflight for duplicate legacy external IDs before schema mutation |
| query_workspace_migration_test.go | Real-MySQL migration-28 downgrade guard: workspace rows and any non-null full SQL survive a refused rollback, while empty storage permits downgrade |
| query_workspace_statement_api_test.go | Real-MySQL + HTTP workspace OCC and owner-only successful statement access matrix, including other/admin/machine/failed/legacy denial and list/audit non-disclosure |
| topology_test.go | Topology traversal plus real-MySQL bounded multi-observer effective-health projection, deterministic cap-plus-sentinel reads, and overflow propagation |
| auth_authorization_version_test.go | Verifies Authorization Version credential invalidation and governed-query freshness against current database state |
| operator_access_boundary_test.go | Proves the complete Operator Access Boundary matrix against current database state using the shared operatoraccess policy; concrete resource and query-target example paths bind to a self-contained database_instance fixture so the matrix never depends on mutable canonical seed IDs |
| named_inventory_view_test.go | Proves personal/shared authorization and lossless filters/sort/columns JSON round-trip without result or page snapshots against real MySQL |
| machine_principal_test.go | Proves one-time/hash-only credentials, last use, all seven closed scopes, expiry, revoke, overlapping rotation, admin audit safety, database/log/history absence, and audit-failure rollback |
| auth_audit_emitter_test.go | Proves auth.login/auth.bearer/auth.authorization audit events persist against MySQL, stay fail-open on inject errors (login success, Bearer rejection, and role-denied 403 all unchanged), and never contain prohibited values; bounded untrusted-Bearer budget proved against real MySQL (missing header emits no row, 60 of 61 supplied rejections persist, suppression counter safe, role denial unbudgeted, process-shared across routers) |
| authz_test_support_test.go | Shared authorization constants, login/bearer/user and truthful execution-identity helpers, and query handler stubs (incl. the QueryEvidencePersistenceFailures stub) |
| seed_credential_remediation_test.go | Regression guard for the forward-only seed-disable migration: proves the published seed users are inactive and their credentials rejected |
| query_disclosure_repository_test.go | Proves MySQL disclosure policy repository behavior: CRUD lifecycle, duplicate-scope insert mapped to the conflict sentinel, not-found cases (`sql.ErrNoRows`), idempotent delete, empty list, and deterministic list ordering |
| bootstrap_admin_command_test.go | Runs the operator bootstrap-admin CLI against MySQL and verifies authentication-compatible creation, reactivation, version rotation, and cleanup |
| e2e_fixture_bootstrap_command_test.go | Runs the test/CI-only e2e-fixture-bootstrap CLI against a disposable `controlhub_*_e2e` database: dual-role (admin+editor) creation, retired-seed inactivity, idempotent reactivation with `authorization_version` rotation, secret-free output, and whole-transaction rollback |
| openapi_fuzz_contract_test.go | Enforces the OpenAPI Fuzz Exclusion Contract (no build tag): Schemathesis exclusions must be narrow single-operation `--exclude-operation-id` flags in openapi-fuzz.sh, within the canonical documented set, with no broad path/method/tag exclusions, no exclusion directives in schemathesis.toml, and a matching contract section in scripts/README.md |
| openapi_fuzz_test.go | Serves the production dependency graph, including named Inventory views, and fuzzes every non-excluded OpenAPI operation against disposable MySQL |
| legacy_import_test.go | Proves UUID-to-bigint cutover import against MySQL: full migration, origin/external-identity mapping, non-empty target rejection, parseTime validation, NULL audit actor preserved as NULL, and unknown non-NULL actors/sources failing loud with no partial import |
| *_test.go | Exercises repository, API, and migration behavior against MySQL |
| query_evidence_pair_test.go | Proves atomic history+audit commit/rollback and persistence telemetry plus migration-26 user/machine actor constraints against real MySQL |
| query_execution_test.go | Proves governed query execution, history, audit, paging, disclosure, and failure behavior with explicit user evidence identity against real MySQL |
| query_dev_seed_test.go / query_dev_target_fixture_test.go | Proves dev seed/fixture readiness and governed execution with explicit user evidence identity against real MySQL |
| navigate_related_records_integration_test.go | Proves governed FK related-record navigation over real MySQL, including the Issue #36 atomic-pair rollback proof: a forced `related_record_navigation` audit insert failure rolls the navigation history row back with it (no partial evidence), the controlled backend-error envelope is preserved, and the failure counter increments exactly once |

## Operator access coverage

`operator_access_boundary_test.go` consumes `internal/testsupport/operatoraccess`,
the same test-only operation table used by the API and OpenAPI tests. Saved
statement mutations remain service-authorized by scope and ownership: personal
statements are owner-only (any role may create personal, including admin;
update/delete require ownership), while shared templates are admin-only.

## Exports
- Test-only helpers guarded by the `integration` build tag

## Dependencies
- Upstream: `internal/api`, `internal/service`, `internal/repository/mysql`, Testcontainers
- Downstream: none

## Update Rule
If integration coverage gains a new behavior boundary or shared helper, update this file in the same change.
