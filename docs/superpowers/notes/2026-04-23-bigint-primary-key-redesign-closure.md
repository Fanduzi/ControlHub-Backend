# 2026-04-23 Bigint Primary-Key Redesign Closure

This note records the formal closeout state for the bigint primary-key redesign on the backend daily local database.

## Outcome

- Daily local `controlhub` now runs the bigint schema as the canonical runtime database.
- Historical/mock/demo data was preserved and migrated instead of being discarded.
- Old UUID-backed local state remains preserved in `controlhub_v1` for rollback and forensic comparison.
- Temporary validation database `controlhub_bigint_verify` was removed after the rebuilt daily database passed validation.

## What Changed

- Added a preserve-then-import cutover path in repo code:
  - preserve old `controlhub` tables into `controlhub_v1`
  - rebuild fresh bigint `controlhub`
  - run goose migrations
  - clear seeded target business tables
  - import preserved UUID-backed data into bigint tables
- Added explicit resume protection so an interrupted local cutover can continue only with `--resume`.
- Refused unsafe cases:
  - non-empty `controlhub_v1` plus still-legacy runtime DB
  - already-bigint runtime DB being preserved as if it were the legacy source
  - non-explicit resume over an existing preserved database

## Validation Evidence

### Database state

- `controlhub.resources.id` = `bigint`
- `controlhub.resource_relations.from_resource_id` = `bigint`
- `controlhub.audit_events.actor_user_id` = `bigint`
- `controlhub_v1.resources.id` remains `char`

### Row-count parity

The rebuilt bigint `controlhub` matches `controlhub_v1` on the preserved business tables:

- `roles` = 2
- `users` = 2
- `environments` = 3
- `owners` = 5
- `resources` = 348
- `resource_profiles_host` = 12
- `resource_profiles_database_instance` = 15
- `resource_profiles_database_cluster` = 8
- `resource_profiles_service` = 9
- `resource_relations` = 66
- `audit_events` = 27

### Command validation performed

- `go test ./internal/cutover ./cmd/cutover-local`
- `make migrate-status`
- `go run ./cmd/cutover-local --resume`
- HTTP smoke checks against a live server on the rebuilt daily `controlhub`:
  - `GET /health`
  - `GET /resources`
  - `GET /environments`
  - `GET /owners`
  - `GET /resources/{id}` with numeric ID

## Rollback Position

- Fast fallback: point `DATABASE_DSN` at `controlhub_v1` and restart the server.
- Full rollback: rebuild `controlhub` from `controlhub_v1` tables if the bigint daily DB proves unusable.
- `controlhub_v1` must remain intact until this redesign is considered fully retired.

## What Was Explicitly Not Done

- No permanent UUID-to-bigint mapping artifact was kept.
- `controlhub_v1` was not dropped.
- `controlhub_goose_verify` was not changed in this closure step.
- This note does not start `resource-crud-redesign` or `topology-orchestrator-upgrade`.

## Follow-on Process Change

This redesign confirmed that design/spec plus implementation plan is not enough for subagent-driven execution. Future work should start with an execution ledger artifact before implementation begins, not after drift appears.
