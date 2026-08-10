# Bootstrap Admin Module

Explicit operator-invoked command that creates or reactivates the ControlHub
administrator. Never runs at server startup and never logs passwords.

## Files
| File | Responsibility |
|------|---------------|
| main.go | Env+DSN CLI: requires BOOTSTRAP_ADMIN_EMAIL + BOOTSTRAP_ADMIN_PASSWORD, hashes with the auth-compatible SHA-256 scheme, idempotent upsert on the unique users.email (reactivate + rotate authorization_version), password-safe report |
| main_test.go | Hash-compatibility (vs the published 0002 seed hash), required-credentials, and no-password-leak unit tests |

## Exports
- `main()` — binary entry point (`go run ./cmd/bootstrap-admin`)
- `runBootstrap(ctx, db, cfg)` — upsert seam returning `outcomeCreated` / `outcomeReactivated`
- `hashPassword(password)` — auth-compatible SHA-256 hex (same scheme as `internal/service` Login)
- `resolveBootstrapConfig()`, `printReport(w, email, outcome)` — CLI seams

## Usage
```bash
DATABASE_DSN="controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4" \
BOOTSTRAP_ADMIN_EMAIL="ops@example.com" \
BOOTSTRAP_ADMIN_PASSWORD="<operator-supplied>" \
go run ./cmd/bootstrap-admin
```
Run explicitly after migrations. Both credentials are mandatory; the password
is hashed, never printed, and never included in errors. A re-run on an existing
account reactivates it and rotates `authorization_version` so prior Bearer
Credentials die.

## Dependencies
- Upstream: `internal/config` (.env loading), `github.com/go-sql-driver/mysql`, stdlib
- Downstream: MySQL 8.0+ `users`/`roles` tables (migrations)

## Update Rule
If the command interface, upsert, or report changes, update this file in the same change.
