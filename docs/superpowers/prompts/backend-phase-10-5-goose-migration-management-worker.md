# Backend Phase 10.5: Goose Migration Management

You are implementing the migration-management cleanup phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-10-asset-write-and-relation-maintenance-worker.md`
- `/Users/fan/GolangProjects/ControlHub/README.md`
- `/Users/fan/GolangProjects/ControlHub/CLAUDE.md`

## Goal

ControlHub currently applies SQL migrations by hand:

```bash
mysql < migrations/0001_initial_schema.sql
mysql < migrations/0002_seed_reference_data.sql
...
```

This has already caused uncertainty about whether local MySQL is at `0004`, `0005`, or `0006`.

This phase introduces `goose` as the standard migration manager so future schema state is explicit and repeatable.

Do not widen into new product features, write endpoints, frontend work, topology, auth middleware, or SQL work orders.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Use `github.com/pressly/goose/v3`.
- Use SQL migrations, not Go migrations.
- Do not implement a custom `schema_migrations` table.
- Do not run migrations automatically on server startup.
- Add explicit Makefile targets for migration operations.
- Preserve existing migration file numbering and intent.
- Support both clean DB setup and adopting the user's existing local `controlhub` database.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. convert existing SQL migration files to goose-compatible format
2. add goose-based migration commands
3. document clean DB and existing DB workflows
4. verify clean DB migration from zero to latest
5. verify adopting the current local `controlhub` DB without re-running already-applied SQL
6. keep Phase 10 write endpoints and tests working

## Migration Files

Convert all current migration files to goose SQL format:

- `migrations/0001_initial_schema.sql`
- `migrations/0002_seed_reference_data.sql`
- `migrations/0003_expand_resource_type_constraint.sql`
- `migrations/0004_seed_demo_data.sql`
- `migrations/0005_add_lifecycle_status_index.sql`
- `migrations/0006_add_resource_name_environment_unique.sql`

Each file must include:

```sql
-- +goose Up
-- +goose StatementBegin
...
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
...
-- +goose StatementEnd
```

Down migrations:

- must be valid SQL
- may be best-effort for dev seed files
- must not pretend to be production-safe if they delete seed/demo rows
- must be documented clearly

For seed migrations:

- `0002_seed_reference_data.sql` is baseline reference data
- `0004_seed_demo_data.sql` is development/demo data
- If `Down` deletes fixed seed/demo ids, document that this is dev-oriented and not a production rollback strategy

## Makefile Targets

Add or update Makefile targets:

```makefile
migrate-up
migrate-status
migrate-down-one
```

Optional but useful:

```makefile
migrate-reset-dev
```

Rules:

- commands must use `DATABASE_DSN` from `.env` or exported env
- do not require the user to paste DSNs manually
- print clear errors when `DATABASE_DSN` is missing
- do not auto-create or drop the primary `controlhub` database unless the target name explicitly says dev reset and asks for an explicit env var/confirmation

## Existing Local DB Adoption

The user's current local `controlhub` DB has already had migrations applied manually.

Do not blindly run `goose up` against it from version zero.

You must implement and document a safe adoption workflow:

1. inspect current DB structure
2. confirm it matches latest expected schema (`0006`)
3. initialize goose's version tracking to the latest version without re-running `0001-0006`
4. run `goose status` and show all versions as applied

Use goose's supported versioning/status commands if possible.

If goose cannot safely baseline an existing DB with one command, implement a small explicit helper target or documented command that inserts the correct goose version rows only after structural verification.

Do not modify data destructively in the user's current `controlhub` DB.

## Required Verification

### 1. Clean DB Verification

Create a disposable database, for example:

```text
controlhub_goose_verify
```

Run goose migrations from zero to latest.

Verify:

- `goose_db_version` exists
- latest version is `0006`
- all expected tables exist
- `resources` has `idx_resources_lifecycle`
- `resources` has composite unique `uq_resource_name_env(name, environment_id)`
- `resources` no longer has global unique index on only `name`
- `resource_relations` still has unique `from_resource_id + to_resource_id + relation_type`
- expected seed/demo counts are present

### 2. Existing Local DB Adoption Verification

Against the user's existing `controlhub` DB:

1. inspect current status before adoption
2. apply any missing SQL migration only if the DB is missing it
3. baseline goose version tracking to latest only after structure matches latest schema
4. run `migrate-status`
5. prove goose sees the DB as current

If the current `controlhub` DB is missing `0005` or `0006`, apply those migrations first using the final goose workflow, not ad hoc `mysql < file` commands.

### 3. Backend Regression Verification

Run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
```

If Phase 10 write endpoints exist in the branch, also run the Phase 10 live smoke tests after goose migration.

## Documentation

Update:

- `README.md`
- `CLAUDE.md`
- any migration-related internal README that exists

Replace manual `mysql < migrations/000X...` instructions with goose commands.

Document:

- first-time clean setup
- status check
- applying pending migrations
- adopting an existing manually-migrated local DB
- how seed/demo migrations are treated
- warning that migrations are not automatically run on server startup

## Final Report

Your final report must include:

- changed files
- final Makefile commands
- clean DB goose verification result
- existing local `controlhub` adoption result
- final `goose status` output or concise equivalent
- whether current local DB is now at version `0006`
- test/vet/build results
- whether Phase 10 live smoke still passes
- commit hash
- remaining risks

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD where code behavior changes
- do not reset the repo
- do not drop the user's current `controlhub` database
- do not discard unrelated work
- do not add product features
- do not implement app-start auto migration
