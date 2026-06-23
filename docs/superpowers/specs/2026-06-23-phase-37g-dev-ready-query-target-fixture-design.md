# Phase 37G Dev Ready Query Target Fixture Design

## Background

Phase 37 added the backend read-only query execution API (MySQL/TiDB SELECT only).
Phase 37F connected the frontend `/query` workbench to it and added the dev-only
credential metadata seed (`cmd/querydev` / `make seed-query-dev-credential`). The
credential seed binds an environment-resolved DSN to an **already-existing** query
target — it never creates resources.

Phase 37F verification proved the gap the spec anticipated: **no existing query
target's host/port matches the local ControlHub MySQL**. The 33 seeded
`database_instance` targets all use synthetic `.internal` hostnames; none points at
`127.0.0.1:3306`. Because the credential DSN is bound to the target's profile
host/port (Phase 37 binding check), there is no target the dev seed can make ready.
Consequently the 3 frontend ready-target E2E tests (ready SELECT, unsafe reject,
history) skip with `no ready query target seeded`.

The Phase 37F spec sanctioned the fix as a follow-on: *"Creating synthetic resources
or altering resource profiles is out of scope for Phase 37F unless the worker proves
the existing seed data cannot support a ready target. If that happens, stop and
report the gap before adding more fixture behavior."* Phase 37G is that follow-on.

## Goal

Provide an explicit, dev/test-only, idempotent path that **ensures** one local
`database_instance` MySQL query target whose profile host/port matches the
ControlHub own DB (`DATABASE_DSN`), so the existing `cmd/querydev` credential seed
produces a `ready` target and the 3 frontend ready-target E2E tests run for real.

```text
QUERY_DEV_ALLOW_TARGET_FIXTURE=true
  -> ensure database_instance resource + profile (host/port from DATABASE_DSN)
  -> existing credential seed binds CONTROLHUB_QUERY_CREDENTIAL_<REF>
  -> target becomes readiness=ready, availableActions.run=true
  -> ready-target E2E (SELECT / unsafe reject / history) runs and passes
```

## Non-goals

- No credential write UI, export, saved query, approval, or AI assistance.
- No production credential management. This is a dev/test fixture only.
- No new SQL migration. Reuse `resources`, `resource_profiles_database_instance`,
  and `query_target_credentials`.
- No automatic enablement in production or in release gates. The fixture runs only
  behind an explicit env switch.
- No DSN/password storage, logging, or printing — only opaque metadata.
- No loosening of the Phase 37 read-only guard (MySQL/TiDB SELECT only; unsafe SQL
  rejected; history/audit recorded).
- No frontend product code changes (the frontend already handles ready targets from
  Phase 37F). If a real frontend gap surfaces, stop and report.
- No cross-repo CI wiring in this phase (deferred follow-up; the command is
  CI-portable by design).
- No tag/release/deploy.

## Architecture

Phase 37G adds one dev-only capability and wires it into the existing querydev
command as an optional, explicit mode. The credential-binding path is untouched.

### Components

1. **Fixture service** (`internal/service/query_dev_target_fixture.go`, new):
   `QueryDevTargetFixture.EnsureLocalQueryTarget(ctx, cfg) (resourceID uint64, err error)`.
   Pure orchestration over existing repository methods:

   - Resolve `environment_id` from `DictionaryRepository.ListEnvironments()` by slug.
   - Resolve `owner_id` from `DictionaryRepository.ListOwners()` by email.
   - **Find or create** the `database_instance` resource: list via
     `ResourceRepository.ListResources` filtered by name + environment + type
     `database_instance`, exact-match the name; reuse its id if present, else
     `ResourceRepository.CreateResource`. (Name+environment has a unique index, so
     this is the idempotency key.)
   - **Upsert** the profile via the existing
     `ResourceRepository.UpsertDatabaseInstanceProfile(id, engine, version, host,
     port, role)` (already idempotent on `resource_id`).
   - Return the resource id. Host/port arrive pre-parsed; the service never sees the
     DSN.

   Dependencies are expressed as a small interface defined where used
   (`DevTargetFixtureStore`), satisfied by the concrete repositories — mirroring the
   existing `DevCredentialWriter` pattern. No production repository contract is
   widened; no new repository method is required.

2. **DSN host:port parser** (pure helper, `internal/service` or `internal/config`):
   parse `DATABASE_DSN` with `mysql.ParseDSN` + `net.SplitHostPort` (the same
   approach the integration test uses) and return `(host, port)`. Pure and
   unit-testable; never returns or logs the DSN.

3. **`cmd/querydev/main.go` extension**: when
   `QUERY_DEV_ALLOW_TARGET_FIXTURE=true`:
   - Parse host:port from `DATABASE_DSN`.
   - Resolve environment/owner/name from defaults (see Env inputs) and overrides.
   - Ensure the target via the fixture service → resource id.
   - Use that id as the seed target (it is an error to also pass a conflicting
     `QUERY_DEV_TARGET_RESOURCE_ID`).
   - Run the **existing** `QueryDevCredentialSeeder.Seed(cfg)` unchanged.
   - When the flag is unset, behavior is **identical to today** (bind-only; requires
     `QUERY_DEV_TARGET_RESOURCE_ID`). This is the production-safety guarantee.

4. **`make seed-query-dev-target`**: thin wrapper =
   `QUERY_DEV_ALLOW_TARGET_FIXTURE=true go run ./cmd/querydev`, with a help comment
   listing the required env vars. Sits next to `seed-query-dev-credential`.

### Defaults (overridable via env)

| Concern | Default | Override env |
|---|---|---|
| Environment slug | `dev` (non-prod) | `QUERY_DEV_TARGET_ENV_SLUG` |
| Owner email | `dba@example.com` (seeded) | `QUERY_DEV_TARGET_OWNER_EMAIL` |
| Resource name | `local-mysql-query-dev` | `QUERY_DEV_TARGET_NAME` |
| Display name | `Local MySQL Query Dev` | `QUERY_DEV_TARGET_DISPLAY_NAME` |
| Engine / version / role | `mysql` / `8.0` / `primary` | — |

### Env inputs (ready-target flow)

```text
QUERY_DEV_ALLOW_TARGET_FIXTURE=true          # required gate to allow target creation
DATABASE_DSN=<controlhub own db dsn>         # already required by querydev; parsed for host:port only
QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO      # existing
CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO=<readonly dsn, same MySQL>  # existing
QUERY_DEV_ENVIRONMENT_POLICY=non_prod_only   # existing default
# optional overrides: QUERY_DEV_TARGET_ENV_SLUG / _OWNER_EMAIL / _NAME / _DISPLAY_NAME
```

The credential DSN must point at the **same MySQL instance** as `DATABASE_DSN` so it
binds to the fixture target's host:port. For local verification it can be
`DATABASE_DSN` itself — the Phase 37 read-only sandbox (`QueryGuard`) enforces
SELECT-only regardless of the underlying DB user's privileges. It must be supplied
**via env only and never printed**: set `QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO` and
`CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO` to a DSN with the same host:port as
`DATABASE_DSN`. The command/report may only state "credential DSN supplied via env
and matched `DATABASE_DSN` host:port" — never the value. (Concrete sourcing steps
live in the implementation plan's B5.)

## Behavior And Invariants

- **Explicit only:** no resource/profile is ever created unless
  `QUERY_DEV_ALLOW_TARGET_FIXTURE=true`. Without it, querydev is byte-for-byte
  unchanged.
- **Idempotent:** re-running finds the existing (name, environment) resource and
  upserts its profile + credential metadata. Exactly one resource, one profile row,
  one credential row result.
- **Fail closed:** missing environment slug, missing owner email, empty DSN,
  unparseable host:port, or a non-executable engine aborts before any write.
- **No DSN storage:** only opaque metadata is persisted — resource fields, the
  profile's host/port/engine/version/role (host/port is connection context, not a
  credential), and `query_target_credentials` (resource_id, engine, credential_ref,
  enabled, environment_policy). The credential DSN stays in
  `CONTROLHUB_QUERY_CREDENTIAL_<REF>`.
- **No DSN printing:** the command report prints only safe metadata — target
  resource id, name, credential ref, engine, environment policy, derived readiness,
  and the run flag. No host:port, no DSN, no password, no token.
- **Binding enforced:** the profile host:port is derived from `DATABASE_DSN`, so the
  readonly credential DSN (same MySQL) binds via the existing Phase 37 check. A
  mismatched credential is still rejected (existing behavior).
- **Guards preserved:** MySQL/TiDB SELECT only; unsafe SQL rejected; row/time limits
  enforced; history + audit recorded. The fixture changes none of this.
- **Policy default:** `non_prod_only` by default; `all_environments` still requires
  the explicit override (existing).

## Testing

**Unit (pure, no DB):**
- Fixture config validation (non-empty name/slug/email; port > 0; engine executable).
- DSN host:port parser (valid DSN → correct host:port; malformed → error; never
  echoes the DSN).

**Integration (Testcontainers, alongside `query_dev_seed_test.go`, reusing
`setupTestDB` / `globalEnv`):**
- Ensure creates the target; `QueryTargetService.List` (the
  `/query-targets` read model) returns it.
- After the seed: `readiness == ready`, `availableActions.run == true`,
  `governance.safetyState == readonly_sandbox_enabled`.
- `QueryExecutionService.Execute` of `select 1` succeeds (rowCount 1).
- Unsafe statement is rejected; the attempt is still recorded in history.
- History (`ListExecutions`) shows the recent attempt (metadata only, no rows).
- Credential/profile rows store **no DSN-looking value** (extend
  `assertCredentialRowStoresNoDSN`; also assert the profile row carries no
  credential).
- **Idempotency:** ensure twice → exactly one resource, one profile row, one
  credential row.
- **Fail closed:** without `QUERY_DEV_ALLOW_TARGET_FIXTURE`, no resource is created
  and the existing bind-only path (requiring `QUERY_DEV_TARGET_RESOURCE_ID`) is
  unchanged.
- Profile host:port equals the parsed `DATABASE_DSN` host:port.

**Backend gates (all must pass; the dev-only fixture must not perturb them):**
`go test -count=1 ./...`, `go vet ./...`, `go build ./...`, `make openapi-validate`,
`make test-integration`, `make test-openapi-fuzz`.

## Local Ready-Target Verification (this phase)

1. Start backend on `:8080` (existing `.env` load; logs/PID under
   `/tmp/controlhub-local/`).
2. `make seed-query-dev-target` (or `QUERY_DEV_ALLOW_TARGET_FIXTURE=true go run
   ./cmd/querydev`). The credential DSN is supplied **via env only and never
   printed**; for local verification, reuse `.env`'s `DATABASE_DSN` (same
   host:port). Report prints safe metadata only.
3. Confirm `GET /query-targets` shows ≥1 target with `readiness=ready` and
   `availableActions.run=true`.
4. Confirm a guarded `select 1` executes and unsafe SQL is rejected (via API or E2E).

## Frontend (this phase)

Re-run the frontend gates (`check:e2e-preflight`, `check:e2e-governance`,
`tsc --noEmit`, `lint`, `test`, `build`) and `npm run test:e2e -- --grep query`. The
locked-target tests still pass; **the 3 ready-target tests must now PASS, not skip.**
No frontend code change is expected. If one is needed, stop and report before
editing.

## CI (deferred)

The command derives host/port from `DATABASE_DSN`, so it is CI-portable by design
(same shape works against a Testcontainers/CI MySQL). Cross-repo frontend-CI wiring
(start backend → run fixture → run ready-target E2E in CI) is an explicit follow-up,
**out of scope for this phase**, to be documented in the final report.

## Success Criteria

- `QUERY_DEV_ALLOW_TARGET_FIXTURE=true` run of querydev creates/ensures one local
  MySQL target whose profile host/port matches `DATABASE_DSN`, then seeds it ready.
- `/query-targets` reports that target as `ready` with `availableActions.run=true`.
- `select 1` executes; unsafe SQL is rejected; history updates.
- The 3 frontend ready-target E2E tests pass locally (no longer skipped).
- Re-running is idempotent (no duplicate resource/profile/credential rows).
- No credentials or DSNs appear in stored rows, command output, logs, or history.
- Without the fixture flag, querydev is unchanged; release gates and fuzz unaffected.
- Backend gates all green. No tag/release/deploy.

## Scope Confirmation

No tag/release/deploy · no credential leak (DSN/password/token never stored, logged,
or printed) · no credential UI · no production auto-enable · no new migration · no
frontend product edits unless a real gap is reported first · no AI co-author · no
push without explicit authorization.
