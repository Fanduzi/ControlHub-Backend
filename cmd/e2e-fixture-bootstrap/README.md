# E2E Fixture Bootstrap Module (test/CI-only)

Explicit operator-invoked command that creates or reactivates the admin AND
editor fixture identities for isolated frontend E2E runs. Never runs at
server startup and never logs passwords.

**This command is test/CI-only.** It provisions ordinary users subject to the
same server-enforced role matrix — production authorization semantics are
untouched.

## Safety Boundary (primary production guard)

The command guards against accidental misconfiguration: a production DSN
cannot create a production administrator by accident. It refuses to run
unless ALL of the following hold, **before any mutation**:

1. **Explicit test-mode capability**: `CONTROLHUB_E2E_FIXTURE_MODE=1`.
   Missing or any other value fails loudly.
2. **Dedicated E2E metadata DSN**: `E2E_FIXTURE_DATABASE_DSN`. The generic
   `DATABASE_DSN` is never read. The DSN must parse (driver errors are never
   echoed — they can carry secret-bearing parameter values), the host must be
   a literal loopback address (`127.0.0.1` / `::1`; hostnames such as
   `localhost` are refused because their resolution cannot be verified
   locally), and the database name must match the disposable naming rule
   `^controlhub_[a-z0-9_]*e2e$` (e.g. `controlhub_e2e`). The default
   `controlhub` database, empty names, and production-like names are
   rejected.
3. **Migrated disposable database**: the database must be migrated to at
   least 00016 (applied rows only) AND both retired 0002 seed accounts
   (`admin@example.com` / `editor@example.com`) must exist and be inactive;
   otherwise provisioning refuses to run.
4. **Fixture identity guards** (additional, NOT the primary boundary):
   fixture emails must end with `.invalid` (RFC 2606 reserved TLD, so a
   fixture can never collide with a real operator account), and the retired
   seed identities are refused outright.

The `.invalid` email rule is an additional guard only — production safety is
provided by the dedicated-DSN + capability gates. Provisioning is
transactional: a partial failure never leaves a usable fixture administrator
behind.

## Files
| File | Responsibility |
|------|---------------|
| main.go | Env+DSN CLI: requires the capability flag, the dedicated disposable DSN, and all four fixture credentials; verifies migration 00016 + inactive retired seeds before upserting (reactivate + rotate authorization_version); refuses the 0002 seed identities; password-safe report |
| main_test.go | Hash-compatibility (vs the published 0002 seed hash), mandatory capability/DSN/credentials, DSN isolation gate (loopback + `*_e2e` naming), migration-00016 verification, legacy-seed refusal, and no-password-leak unit tests |

## Exports
- `main()` — binary entry point (`go run ./cmd/e2e-fixture-bootstrap`)
- `resolveFixtureConfig()` — env seam (capability + DSN + credentials, all mandatory)
- `parseDisposableDSN(dsn)` — the loopback/`*_e2e` DSN isolation gate
- `verifyFixtureDatabase(ctx, db)` — migration-00016 + retired-seed verification
- `runFixtureBootstrap(ctx, db, set)` — upsert seam returning per-identity outcomes
- `hashPassword(password)` — auth-compatible SHA-256 hex (same scheme as `internal/service` Login)
- `printReport(w, set, outcomes)` — CLI seam (identities/roles/outcomes only)

## Usage
```bash
CONTROLHUB_E2E_FIXTURE_MODE=1 \
E2E_FIXTURE_DATABASE_DSN="controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub_issue15_e2e?parseTime=true&charset=utf8mb4" \
E2E_FIXTURE_ADMIN_EMAIL="e2e-admin-<run-id>@controlhub-e2e.invalid" \
E2E_FIXTURE_ADMIN_PASSWORD="<per-run-generated>" \
E2E_FIXTURE_EDITOR_EMAIL="e2e-editor-<run-id>@controlhub-e2e.invalid" \
E2E_FIXTURE_EDITOR_PASSWORD="<per-run-generated>" \
go run ./cmd/e2e-fixture-bootstrap
```
Run explicitly after migrations on the disposable E2E metadata database. All
credentials and the capability flag are mandatory; passwords are hashed,
never printed, and never included in errors. A re-run on existing identities
reactivates them and rotates `authorization_version` so prior Bearer
Credentials die.

The frontend E2E suite consumes the same identities from
`E2E_FIXTURE_ADMIN_EMAIL` / `E2E_FIXTURE_ADMIN_PASSWORD` and
`E2E_FIXTURE_EDITOR_EMAIL` / `E2E_FIXTURE_EDITOR_PASSWORD` (see frontend
`e2e/harness/fixtures.ts`).

## Dependencies
- Upstream: `internal/config` (.env loading), `github.com/go-sql-driver/mysql` (DSN parsing), stdlib
- Downstream: MySQL 8.0+ `users`/`roles` tables and `goose_db_version` (migrations)

## Update Rule
If the command interface, safety gates, upsert, or report changes, update
this file in the same change.
