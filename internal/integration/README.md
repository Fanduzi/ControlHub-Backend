# Integration Module

MySQL-backed integration tests run against disposable Testcontainers databases.

## Files
| File | Responsibility |
|------|---------------|
| testenv_test.go | Starts MySQL, applies migrations, and provides database helpers |
| auth_authorization_version_test.go | Verifies credential invalidation and the Operator Access Boundary against current database state |
| *_test.go | Exercises repository, API, and migration behavior against MySQL |

## Exports
- Test-only helpers guarded by the `integration` build tag

## Dependencies
- Upstream: `internal/api`, `internal/service`, `internal/repository/mysql`, Testcontainers
- Downstream: none

## Update Rule
If integration coverage gains a new behavior boundary or shared helper, update this file in the same change.
