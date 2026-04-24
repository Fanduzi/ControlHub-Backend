# Bigint Local DB Cutover And Historical Data Migration Plan

## Context
This bigint redesign is already implemented in code and migration baseline, but the daily local `controlhub` database still reflects the older UUID / `char(36)` schema. The temporary `controlhub_bigint_verify` database proved that the bigint stack works, but it is not the canonical daily database and should not remain the runtime target.

The user requirement is stronger than a clean rebuild: the current local demo/mock/history data in `controlhub` must not be lost. The new daily `controlhub` must preserve the existing data footprint that keeps the frontend useful, while moving the schema to the bigint-based model. The old UUID-backed state must remain preserved in `controlhub_v1` for rollback and forensic comparison.

## Recommended Approach
Use a **preserve-then-import** cutover:
1. Stop runtime processes using `controlhub`.
2. Preserve the old UUID-backed daily DB as `controlhub_v1`.
3. Recreate a fresh empty `controlhub`.
4. Run the existing bigint migration chain to build the new schema.
5. Import the old data from `controlhub_v1` into the new bigint `controlhub`, preserving current data density and frontend-visible content.
6. Validate the imported daily DB end-to-end.
7. Drop `controlhub_bigint_verify` only after validation succeeds.
8. Write the bigint closure note and add an execution-ledger artifact for future work.

This is not a seed-only rebuild. Repo seed/demo data is fallback/reference material only; the primary target is to migrate the current local historical/mock/demo dataset into the new bigint schema.

## Hard Requirements From Current Discovery
- Current daily `controlhub` is still old UUID / `char(36)` schema.
- Current repo does **not** contain a reusable legacy-import tool.
- Repo **does** contain the bigint target schema and validation tests.
- Old data must be preserved in `controlhub_v1`.
- New `controlhub` must keep the current usable dataset, not become an empty or seed-only DB.
- No persisted UUID->bigint mapping artifact is requested; fallback relies on retaining `controlhub_v1` intact.

## Existing Repo Pieces To Reuse
- Migration application: `Makefile:31` via `make migrate-up`
- Migration status: `Makefile:35` via `make migrate-status`
- Destructive reset behavior to avoid as the main cutover path: `Makefile:43`
- Runtime DB selection through `DATABASE_DSN`: `internal/config/config.go:44`
- Local DSN default pointing at `controlhub`: `.env.example:5`
- Bigint schema baseline: `migrations/0001_initial_schema.sql:5`
- Reference seed data: `migrations/0002_seed_reference_data.sql:5`
- Demo seed data: `migrations/0004_seed_demo_data.sql:24`
- Migration/schema verification expectations: `internal/integration/mysql_test.go:88`

## Critical Files To Modify
- `README.md`
- `Makefile`
- `.env.example`
- new importer / cutover code or script under the backend repo
- bigint closure note under repo docs
- execution ledger artifact under repo docs

## Data Migration Strategy
The old database uses UUID-like `char(36)` IDs across all business tables. The new schema uses bigint auto-increment IDs. Because no persistent mapping table is desired, the import flow should use **ephemeral staging maps during migration only**, while keeping `controlhub_v1` as the permanent historical source of truth.

### Entities That Must Be Preserved
Migrate all current local historical/demo data from old `controlhub` into new `controlhub` for:
- `roles`
- `users`
- `environments`
- `owners`
- `resources`
- `resource_profiles_host`
- `resource_profiles_database_instance`
- `resource_profiles_database_cluster`
- `resource_profiles_service`
- `resource_relations`
- `audit_events`

### Import Rules
- Preserve business content first: names, emails, slugs, display labels, profile content, relations, audit event semantics, timestamps.
- Generate new bigint IDs in the target schema by inserting rows into the new tables and capturing inserted IDs.
- Use temporary mapping tables during the import process only to resolve old UUID references to new bigint IDs.
- Drop temporary mapping tables after validation succeeds.
- Do **not** additionally flood the new daily DB with full repo demo seed unless import coverage is proven insufficient. The primary target is the current existing local dataset.
- If the old dataset is missing reference rows required by the current app, add only the minimum supplemental rows needed after import.

## Recommended Import Order
### 1. Preserve Old Daily DB As `controlhub_v1`
- Create empty database `controlhub_v1` with the same charset/collation used by repo docs.
- Grant the local `controlhub` user privileges on `controlhub_v1`.
- Move all existing tables from old `controlhub` into `controlhub_v1` via MySQL `RENAME TABLE` statements.
- This preserves the old exact state, including `goose_db_version`, while freeing the name `controlhub`.

### 2. Recreate Fresh Bigint `controlhub`
- Recreate empty `controlhub` with utf8mb4 / `utf8mb4_0900_ai_ci`.
- Ensure the local `controlhub` user has privileges on `controlhub`.
- Keep `.env` / shell config pointed at `controlhub`.
- Run `make migrate-up` to build the bigint schema.

### 3. Create Temporary Import Mapping Tables In New `controlhub`
Create temporary or explicitly disposable tables for old UUID -> new bigint resolution during import, for at least:
- environments
- owners
- roles
- users
- resources
Potentially relation/audit staging if needed.

These are operational staging tables only, not permanent product schema.

### 4. Import Reference Entities
Import in dependency order:
1. roles
2. environments
3. owners
4. users

For each entity type:
- read rows from `controlhub_v1`
- insert business columns into new `controlhub`
- capture newly assigned bigint IDs
- store old UUID -> new bigint correspondence in temporary mapping tables

### 5. Import Resources
- Insert resources from `controlhub_v1.resources` into new `controlhub.resources`
- translate `environment_id`, `owner_id`, and `archived_by` references using temporary mapping tables
- preserve business fields like `resource_type`, `resource_subtype`, `name`, `display_name`, `labels`, `source`, `external_id`, timestamps, lifecycle/health, archive fields
- capture old resource UUID -> new bigint resource ID in the temporary resource map

### 6. Import Profile Tables
Import each profile table by resolving `resource_id` through the temporary resource map:
- `resource_profiles_host`
- `resource_profiles_database_instance`
- `resource_profiles_database_cluster`
- `resource_profiles_service`

### 7. Import Relations
- Import `resource_relations` by translating `from_resource_id` and `to_resource_id` through the temporary resource map
- preserve `relation_type` and `created_at`

### 8. Import Audit Events
- Import `audit_events` by translating:
  - `actor_user_id` through the user map
  - `target_resource_id` through the resource map
- preserve `event_type`, `result`, `created_at`

### 9. Drop Temporary Mapping Tables
- Only after all validation passes
- If validation fails, keep them until diagnosis finishes

## Implementation Shape
Because the repo has no reusable importer today, the recommended implementation is to add a **one-off local migration/import command** or script in the backend repo that:
- connects to source DB `controlhub_v1`
- connects to target DB `controlhub`
- runs the import transactionally in ordered phases where feasible
- records counts per table migrated
- fails loudly on unresolved references
- can run idempotently only if explicitly designed for rerun, otherwise clearly document one-shot semantics

This is preferable to a long manual SQL notebook because the translation from UUID references to bigint references spans many tables and needs repeatable verification.

## Preflight And Inventory
- Verify no app process is actively using the daily DB.
- Confirm current `DATABASE_DSN` targets `controlhub`, not `controlhub_bigint_verify`.
- Capture current inventory:
  - databases matching `controlhub%`
  - tables in `controlhub`
  - row counts for all business tables in old `controlhub`
  - whether `controlhub_v1` already exists
  - whether `controlhub_bigint_verify` still exists
- Abort if `controlhub_v1` already exists and is non-empty unless reconciled first.

## Validation Plan
### Schema Validation
- Run `make migrate-status` and confirm all current migrations are applied.
- Verify key ID columns in `information_schema.columns` are `bigint unsigned`, especially:
  - `resources.id`
  - `resources.environment_id`
  - `resources.owner_id`
  - `resource_relations.id`
  - `resource_relations.from_resource_id`
  - `resource_relations.to_resource_id`
  - profile table PKs and `resource_id`
  - `audit_events.id`, `actor_user_id`, `target_resource_id`

### Data Validation
- Compare row counts between `controlhub_v1` and rebuilt `controlhub` for all migrated business tables.
- Compare key uniqueness anchors such as:
  - role names
  - user emails
  - environment slugs
  - owner emails
  - resource `(name, environment)` combinations
- Verify no profile row references missing resource IDs.
- Verify no relation row references missing resource IDs.
- Verify no audit row references missing user/resource IDs except intentional nullable targets.

### App Validation
- Start backend against rebuilt `controlhub`.
- Run smoke checks:
  - `GET /health`
  - login with migrated users or seeded fallback users
  - list endpoints (`/resources`, `/environments`, `/owners`, `/roles`)
  - resource detail/profile endpoints using migrated numeric IDs
- Validate frontend against the new daily `controlhub`, not the temporary verify DB.

### Repo Validation
- `make test`
- `make openapi-validate`
- `make test-integration`
- optionally `go test -race ./...`

## Removal Of `controlhub_bigint_verify`
Only after the new daily `controlhub` passes validation:
- verify the temp verification DB is no longer needed
- drop `controlhub_bigint_verify`
- keep `controlhub_v1` intact for rollback and historical comparison

## Rollback Plan
### Fast Fallback
- temporarily point `DATABASE_DSN` to `controlhub_v1`
- restart the app

### Full Rollback
- stop app
- drop failed rebuilt `controlhub`
- recreate empty `controlhub`
- rename all preserved tables back from `controlhub_v1` to `controlhub`

Do not drop `controlhub_v1` during this work.

## Documentation Changes
- `README.md`
  - replace the old “Existing Local DB” section with a bigint cutover + historical import runbook
  - document `controlhub` -> `controlhub_v1` preservation
  - document importer usage and validation expectations
  - clarify that `controlhub_bigint_verify` is temporary and should be removed after success
  - update outdated UUID-style smoke examples to numeric-ID examples
- `.env.example`
  - keep `controlhub` as default DSN target
  - add a short note that `controlhub_v1` is fallback-only, not the normal runtime DB
- `Makefile`
  - clarify that `migrate-reset-dev` is destructive and not the safe historical-import cutover path
  - optionally add a dedicated helper target for the importer workflow

## Formal Bigint Closure
Add a closeout note that records:
- old UUID-backed local DB preserved in `controlhub_v1`
- new daily `controlhub` now runs bigint schema with migrated historical/demo data
- `controlhub_bigint_verify` removed
- validation commands and outcomes
- what was explicitly not done
- rollback location and procedure

## Future Process Fix
For future subagent-driven work, design/spec + implementation plan is not enough. Add a required execution ledger artifact for each initiative.

Recommended minimum structure:
- scope / source docs
- task-by-task execution status
- per-task gate result:
  - implemented
  - spec reviewed
  - code reviewed
  - marked done
- validation evidence
- closure decision
- deferred items

For this bigint round, add a lightweight closure note now. For future initiatives, create the execution ledger **before implementation starts**.

## Verification Commands
- `make migrate-status`
- `make migrate-up`
- `make run`
- `curl http://localhost:8080/health`
- `curl -X POST http://localhost:8080/auth/login -H "Content-Type: application/json" -d '{"email":"admin@example.com","password":"secret123"}'`
- `curl "http://localhost:8080/resources"`
- `curl "http://localhost:8080/environments"`
- `curl "http://localhost:8080/owners"`
- `curl "http://localhost:8080/roles"`
- `make test`
- `make openapi-validate`
- `make test-integration`
- `go test -race ./...`

## Expected Outcome
After execution:
- old local UUID-backed state is preserved in `controlhub_v1`
- new daily `controlhub` is rebuilt from bigint schema and populated with migrated historical/demo/mock data from the old local DB
- runtime and tests validate against the real daily DB
- `controlhub_bigint_verify` is gone
- the bigint workstream has a formal closure record
- future work has an execution-ledger pattern instead of relying only on plan docs
