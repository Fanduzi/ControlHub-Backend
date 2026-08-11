# E2E Fixture Bootstrap Module (test/CI-only)

Explicit operator-invoked command that creates or reactivates the admin AND
editor fixture identities for isolated frontend E2E runs. Never runs at
server startup and never logs passwords.

**This command is test/CI-only.** It refuses to recreate the published 0002
seed accounts (`admin@example.com` / `editor@example.com` / `secret123`) that
migration 00016 disabled, so E2E can never silently fall back to published
credentials. It provisions ordinary users subject to the same server-enforced
role matrix — production authorization semantics are untouched.

## Files
| File | Responsibility |
|------|---------------|
| main.go | Env+DSN CLI: requires `E2E_FIXTURE_ADMIN_EMAIL`, `E2E_FIXTURE_ADMIN_PASSWORD`, `E2E_FIXTURE_EDITOR_EMAIL`, `E2E_FIXTURE_EDITOR_PASSWORD`; hashes with the auth-compatible SHA-256 scheme; idempotent upserts (reactivate + rotate authorization_version); refuses the 0002 seed identities; password-safe report |
| main_test.go | Hash-compatibility (vs the published 0002 seed hash), mandatory-credentials, legacy-seed refusal, and no-password-leak unit tests |

## Exports
- `main()` — binary entry point (`go run ./cmd/e2e-fixture-bootstrap`)
- `runFixtureBootstrap(ctx, db, set)` — upsert seam returning per-identity outcomes
- `resolveFixtureConfig()` — env seam (admin + editor, all mandatory)
- `hashPassword(password)` — auth-compatible SHA-256 hex (same scheme as `internal/service` Login)
- `printReport(w, set, outcomes)` — CLI seam (identities/roles/outcomes only)

## Usage
```bash
DATABASE_DSN="controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4" \
E2E_FIXTURE_ADMIN_EMAIL="e2e-admin@controlhub-e2e.invalid" \
E2E_FIXTURE_ADMIN_PASSWORD="<per-run-generated>" \
E2E_FIXTURE_EDITOR_EMAIL="e2e-editor@controlhub-e2e.invalid" \
E2E_FIXTURE_EDITOR_PASSWORD="<per-run-generated>" \
go run ./cmd/e2e-fixture-bootstrap
```
Run explicitly after migrations on the E2E metadata database. All four
credentials are mandatory; passwords are hashed, never printed, and never
included in errors. A re-run on existing identities reactivates them and
rotates `authorization_version` so prior Bearer Credentials die.

The frontend E2E suite consumes the same identities from
`E2E_FIXTURE_ADMIN_EMAIL` / `E2E_FIXTURE_ADMIN_PASSWORD` and
`E2E_FIXTURE_EDITOR_EMAIL` / `E2E_FIXTURE_EDITOR_PASSWORD` (see frontend `e2e/harness/fixtures.ts`).

## Safety Gate

Both fixture emails MUST end with `.invalid` (RFC 2606 reserved TLD, cannot
resolve or belong to a real operator). This makes an accidental production
`DATABASE_DSN` harmless: the command can never upsert or reactivate a real
operator account. The published 0002 seed identities are refused separately.

## Dependencies
- Upstream: `internal/config` (.env loading), `github.com/go-sql-driver/mysql`, stdlib
- Downstream: MySQL 8.0+ `users`/`roles` tables (migrations)

## Update Rule
If the command interface, upsert, or report changes, update this file in the same change.
