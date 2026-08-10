# Integration Module

MySQL-backed integration tests run against disposable Testcontainers databases.

## Files
| File | Responsibility |
|------|---------------|
| testenv_test.go | Starts MySQL, applies migrations, and provides database helpers |
| auth_authorization_version_test.go | Verifies Authorization Version credential invalidation and governed-query freshness against current database state |
| operator_access_boundary_test.go | Proves the complete Operator Access Boundary matrix against current database state using the shared operatoraccess policy |
| authz_test_support_test.go | Shared authorization constants, login/bearer/user helpers, and query handler stubs |
| seed_credential_remediation_test.go | Regression guard for the forward-only seed-disable migration: proves the published seed users are inactive and their credentials rejected |
| query_disclosure_repository_test.go | Proves MySQL disclosure policy repository behavior: CRUD lifecycle, duplicate-scope insert mapped to the conflict sentinel, not-found cases (`sql.ErrNoRows`), idempotent delete, empty list, and deterministic list ordering |
| bootstrap_admin_command_test.go | Runs the operator bootstrap-admin CLI against MySQL and verifies authentication-compatible creation, reactivation, version rotation, and cleanup |
| *_test.go | Exercises repository, API, and migration behavior against MySQL |

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
