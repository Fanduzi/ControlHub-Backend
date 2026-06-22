# Phase 37F — Local Dev Query Credential Seed Evidence

## Scope

Backend-only dev credential metadata seed path (Phase 37F Task B1 + B2).
Adds an explicit, idempotent, dev-only command that makes one MySQL/TiDB
query target ready for the Query Workbench by writing credential METADATA
only. The DSN is never stored, logged, or printed.

This note records the implementation commit, the seed behavior, the
no-DSN-persistence confirmation, and the verification matrix.

## Implementation commit

- Branch: `phase-37f-dev-query-credential`
- Worktree: `.worktrees/backend-phase-37f-dev-query-credential`
- Seed feature commit: `e3a5cbc` (`feat: add local query credential seed`)
- Integration + evidence commit: this commit (Task B2)

## Files

- `cmd/querydev/main.go` — dev-only seed command (env-driven; prints safe metadata + derived readiness only).
- `internal/service/query_dev_seed.go` — `QueryDevCredentialSeeder`: pure validation + target/DSN binding + metadata upsert orchestration.
- `internal/service/query_dev_seed_test.go` — unit tests for validation, binding, and metadata shape.
- `internal/repository/mysql/query_execution_repository.go` — `UpsertCredentialMetadata` (metadata only, idempotent `ON DUPLICATE KEY UPDATE`).
- `internal/integration/query_dev_seed_test.go` — real-MySQL readiness/execution/no-DSN-stored + mismatch regression.
- `Makefile` — `seed-query-dev-credential` target (dev-only; not in release gates).
- This note.

## Seed command

```bash
make seed-query-dev-credential
# equivalent to: go run ./cmd/querydev
```

Required environment (read via `config.LoadDotEnv()` from `.env`, same convention
as `cmd/server`):

| Variable | Purpose |
|----------|---------|
| `DATABASE_DSN` | ControlHub's own DB connection (passed to the driver; never stored/printed). |
| `QUERY_DEV_TARGET_RESOURCE_ID` | The database_instance resource id to seed. |
| `QUERY_DEV_CREDENTIAL_REF` | Opaque `[A-Z0-9_]+` ref stored in metadata. |
| `CONTROLHUB_QUERY_CREDENTIAL_<REF>` | The real DSN, read ONLY by the env resolver for binding validation. |
| `QUERY_DEV_ENVIRONMENT_POLICY` | Optional; defaults to `non_prod_only`. |
| `QUERY_DEV_ALLOW_ALL_ENVIRONMENTS` | Optional bool; required to use `all_environments`. |

The command is explicit (never auto-enabled), idempotent (re-running refreshes
the same row), and re-derives readiness through the real read model before
printing it.

## No DSN / password persistence or disclosure

- `query_target_credentials` stores only `resource_id, engine,
  credential_ref, enabled, environment_policy`. There is no DSN column and the
  upsert accepts only `model.QueryCredentialMetadata` (which has no DSN field).
- The DSN is read from `CONTROLHUB_QUERY_CREDENTIAL_<REF>` by
  `EnvCredentialResolver`, validated to bind to the target's host/port via the
  existing Phase 37 `validateDSNBinding`, and then discarded. It is never
  assigned to a field that is persisted, logged, or returned.
- The command prints only: target resource id, credential ref, engine,
  environment policy, enabled, derived readiness, and an explicit
  `stored dsn: none` line.
- Binding-failure errors are fixed strings; the parsed DSN (which contains the
  password) is discarded in favor of the seed sentinel. Unit test
  `TestQueryDevSeed_RejectsUnboundDSNWithoutLeakingIt` asserts the failure
  message contains neither the DSN password marker nor `tcp(`.
- Integration test `assertCredentialRowStoresNoDSN` scans every stored column
  for the seeded target and asserts none equals or resembles the DSN
  (no `tcp(`, `://`, or `@`; not equal to the real DSN string).

## Verification matrix

All run on the worktree branch tip. Docker was available (29.2.1) and the
Docker gates ran.

| Gate | Command | Result |
|------|---------|--------|
| Whitespace | `git diff --check` | PASS (clean) |
| Unit tests | `go test -count=1 ./...` | PASS (10 pkgs; `cmd/querydev` has no tests by design) |
| Static analysis | `go vet ./...` | PASS (clean) |
| Compile | `go build ./...` | PASS |
| OpenAPI | `make openapi-validate` | PASS |
| Integration (Docker) | `make test-integration` | PASS (9.7s) |
| OpenAPI fuzz (Docker) | `make test-openapi-fuzz` | PASS — all checks passed (1121/1121 cases; 2 advisory warnings are pre-existing auth/schema notes, unrelated to this change) |
| GitNexus scope | `detect_changes` (worktree) | LOW risk, 0 affected processes — purely additive |

Focused Phase 37F integration results:

- `TestQueryDevSeed_MakesTargetReadyAndExecutesSelectOne` — PASS: seed makes
  target `ready` with `availableActions.run=true`,
  `governance.executionEnabled=true`,
  `governance.safetyState=readonly_sandbox_enabled`; `select 1` executes
  successfully; re-seed is idempotent (still 1 row); no DSN-looking value stored.
- `TestQueryDevSeed_RejectsMismatchedCredentialAndStaysLocked` — PASS: a
  credential whose host/port does not match the target is rejected, no metadata
  row is written, the target is not runnable, and execution is rejected.

## Known local setup

1. Ensure migrations are applied to the local `controlhub` DB:
   `make migrate-up`.
2. Identify a `database_instance` resource id (e.g. from `GET /query-targets`
   or the demo seed data).
3. Set the env vars above (the credential DSN must point at the SAME host/port
   recorded in that target's `resource_profiles_database_instance` row).
4. Run `make seed-query-dev-credential`.
5. The target's readiness becomes `ready` via `GET /query-targets`; the Query
   Workbench can then run a guarded `select 1`.

## Negative scope

- No frontend changes (frontend is a separate repo; out of scope).
- No credential write API (the seed is a dev-only command, not an HTTP endpoint).
- No plaintext DSN/password storage, logging, or response disclosure.
- No production auto-enable (the command is explicit and dev-only).
- No new query engines (mysql/tidb only, reusing Phase 37 gates).
- No push, tag, release, or deploy.
- No SQL execution behavior changes (the seed path only writes metadata;
  execution reuse is unchanged).

## Next phase input

- The frontend Phase 37F tasks (F1–F3) can now wire the execute/history UI
  against the existing Phase 37 APIs; one backend target can be made ready
  locally with the command above for cross-repo E2E.
- GitNexus index is stale at `29f07d9` (pre-B1); re-index when convenient so
  the new `QueryDevCredentialSeeder` / `UpsertCredentialMetadata` symbols are
  tracked for downstream impact analysis.
