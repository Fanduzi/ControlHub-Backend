# Integration Module

MySQL-backed integration tests run against disposable Testcontainers databases.

## Files
| File | Responsibility |
|------|---------------|
| testenv_test.go | Starts MySQL, applies migrations, and provides database helpers |
| resource_test.go | Resource repository CRUD/filtering, create-with-profile atomicity, profile validation, and PATCH partial-merge semantics against real MySQL |
| auth_authorization_version_test.go | Verifies Authorization Version credential invalidation and governed-query freshness against current database state |
| operator_access_boundary_test.go | Proves the complete Operator Access Boundary matrix against current database state using the shared operatoraccess policy; concrete resource and query-target example paths bind to a self-contained database_instance fixture so the matrix never depends on mutable canonical seed IDs |
| auth_audit_emitter_test.go | Proves auth.login/auth.bearer/auth.authorization audit events persist against MySQL, stay fail-open on inject errors (login success, Bearer rejection, and role-denied 403 all unchanged), and never contain prohibited values; bounded untrusted-Bearer budget proved against real MySQL (missing header emits no row, 60 of 61 supplied rejections persist, suppression counter safe, role denial unbudgeted, process-shared across routers) |
| authz_test_support_test.go | Shared authorization constants, login/bearer/user helpers, and query handler stubs |
| seed_credential_remediation_test.go | Regression guard for the forward-only seed-disable migration: proves the published seed users are inactive and their credentials rejected |
| query_disclosure_repository_test.go | Proves MySQL disclosure policy repository behavior: CRUD lifecycle, duplicate-scope insert mapped to the conflict sentinel, not-found cases (`sql.ErrNoRows`), idempotent delete, empty list, and deterministic list ordering |
| bootstrap_admin_command_test.go | Runs the operator bootstrap-admin CLI against MySQL and verifies authentication-compatible creation, reactivation, version rotation, and cleanup |
| e2e_fixture_bootstrap_command_test.go | Runs the test/CI-only e2e-fixture-bootstrap CLI against a disposable `controlhub_*_e2e` database: dual-role (admin+editor) creation, retired-seed inactivity, idempotent reactivation with `authorization_version` rotation, secret-free output, and whole-transaction rollback |
| openapi_fuzz_contract_test.go | Enforces the OpenAPI Fuzz Exclusion Contract (no build tag): Schemathesis exclusions must be narrow single-operation `--exclude-operation-id` flags in openapi-fuzz.sh, within the canonical documented set, with no broad path/method/tag exclusions, no exclusion directives in schemathesis.toml, and a matching contract section in scripts/README.md |
| legacy_import_test.go | Proves UUID-to-bigint cutover import against MySQL: full migration, non-empty target rejection, parseTime validation, NULL audit actor preserved as NULL with fixed event/result/target/created-at metadata, and unknown non-NULL audit actor failing loud with no partial import |
| *_test.go | Exercises repository, API, and migration behavior against MySQL |
| query_evidence_pair_test.go | Proves the atomic Execution Evidence Pair (Issue #34) against real MySQL: history + audit commit together and both roll back on audit/history insert failure; persistence-failure counter increments exactly once per failed pair with a fixed safe log |

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
