# Cutover Module

One-shot legacy data migration that preserves the current UUID-schema database,
rebuilds the bigint runtime database, and imports preserved data inside a single
target transaction.

## Files
| File | Responsibility |
|------|---------------|
| local.go | Preserve-then-import orchestration: detect legacy schema, preserve tables as `controlhub_v1`, rebuild target, optional resume |
| local_test.go | Unit coverage of preserve/rebuild/import orchestration decisions via a fake admin store |
| import.go | Imports roles, users, environments, owners, resources, profiles, relations, and audit events; maps legacy source to immutable origin and non-empty externalId to the globally unique `legacy` identity; unknown sources/actors fail loud with no partial import |

## Interfaces
- `ImportLegacyData(ctx, ImportConfig)` — full transactional import from a
  legacy source DSN into an empty migrated target DSN.
- `LocalCutoverConfig` / `runLocalPreserveThenImport` — local cutover command
  orchestration (see `cmd/cutover-local`).

## Dependencies
- Upstream: `cmd/cutover-local`
- Downstream: none

## Update Rule
If cutover gains a new behavior boundary or shared helper, update this file in the same change.
