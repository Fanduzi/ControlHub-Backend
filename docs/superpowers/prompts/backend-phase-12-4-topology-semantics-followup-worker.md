# Backend Phase 12.4: Topology Semantics Follow-Up For Frontend Closeout

You are implementing a small backend follow-up phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

This phase exists because live frontend review still found topology/demo-truth gaps after backend Phase 12.3. Do not expand scope. Only improve the backend truth required to support the remaining frontend closeout work.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-3-demo-data-and-topology-semantics-worker.md`
- `/Users/fan/GolangProjects/ControlHub/migrations/0004_seed_demo_data.sql`
- `/Users/fan/GolangProjects/ControlHub/migrations/0008_apply_demo_seed_cleanup_patch.sql`
- `/Users/fan/GolangProjects/ControlHub/internal/service/topology_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/integration/topology_test.go`
- `/Users/fan/GolangProjects/ControlHub/internal/model/taxonomy.go`
- `/Users/fan/GolangProjects/ControlHub/internal/model/relation.go`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
docker info
```

Expected:

- worktree path is under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- branch is dedicated to this phase
- base includes backend Phase 12.3 on `main`
- worktree is clean
- Docker is available

If Docker is unavailable, stop and report the blocker.

## Parallel Coordination Rules

Frontend and backend workers cannot talk to each other during execution. This prompt is self-contained.

- Your job is to improve the backend graph truth and demo truth so frontend closeout work has a stable source of truth.
- Do not assume the frontend worker knows what changed unless you state it precisely in the final report.
- If you change seed truth in a way that affects current local dev databases, use a new patch migration.
- Your output is considered the graph-truth freeze for the frontend closeout phase.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives.

- Do not change the topology endpoint shape.
- Do not add topology editing.
- Do not add pixel coordinates or explicit layout hints.
- Do not introduce SQL work orders, query execution, discovery, or new product areas.
- Keep UUID primary keys.
- Preserve and stabilize environment `id`, `name`, and `slug`.
- Prefer existing resource types and relation types.
- Only extend taxonomy if absolutely necessary and clearly justified.
- If existing local dev databases need to pick up seed-truth fixes, add a new patch migration rather than relying on edits to old applied migrations.

## Exact Problems To Revisit

### 1. `Notification Delivery Service` still reads like a demo oddity

Current problem:

- the long migration-sentence name was removed in 12.3
- but the resource still reads like a strange exception sample rather than a believable platform asset

Required outcome:

- either improve its naming and/or graph placement so it reads more naturally
- or justify in the final report why it should remain as-is

Do not silently keep it strange without explanation.

### 2. Payment MySQL topology truth may still be too weak

The frontend still struggles to render a clearly layered graph from current truth.

Re-examine whether Payment MySQL should express additional truth through current relation types, such as:

- domain -> VIP
- VIP -> proxy
- active/standby proxy semantics
- proxy -> cluster
- primary -> replica
- host / control-plane relationships that help the graph read naturally
- whether service consumers should depend on cluster, proxy, or another more natural resource in the current model

Do not add speculative edges that distort the model just to satisfy layout.

### 3. Config MySQL topology truth may still be too flat

The live detail page for:

- `41000000-0000-0000-0000-000000000013`

still reads like “cluster + a bag of neighbors”.

Re-examine whether the Config MySQL seed truth still lacks enough structure for the frontend to separate:

- proxy
- cluster
- primary/replica
- control-plane
- services
- hosts

If yes, patch the seed truth using a new migration.

### 4. Environment readability support must remain trustworthy

The frontend still exposes UUID-style `environmentId` in URLs, but the backend already has:

- `id`
- `name`
- `slug`

This phase must confirm that environment `name` and `slug` truth is clean and stable for future frontend use.

If reference data needs patching, use a new migration.

## Testing

Follow TDD.

At minimum add or update tests for:

- Payment MySQL topology integration truth
- Config MySQL topology integration truth
- demo resource naming/semantics regression guards
- environment `id/name/slug` consistency if any reference data changes
- seed patch behavior if a new migration is introduced

## Verification

You must run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
make test-integration
make test-openapi-fuzz
```

If you introduce a new patch migration, also run:

```bash
make migrate-up
make migrate-status
```

Then directly query the local `controlhub` database to prove the patch landed.

## Live Verification

At minimum verify:

- one Payment MySQL topology payload
- one Config MySQL topology payload
- one renamed / adjusted demo resource by ID
- environment dictionary output including `id`, `name`, `slug`

Do not destructively reset the user's daily dev DB unless explicitly instructed.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only.

If GitNexus is available, run the repo-configured change-impact check before commit.

## Final Report

Your final report must include:

- worktree path
- branch
- commit hash
- whether a new patch migration was added, and its filename
- exactly what changed in Payment MySQL truth
- exactly what changed in Config MySQL truth
- exactly how `Notification Delivery Service` was handled
- whether environment `id/name/slug` changed
- all verification command results
- live verification results
- confirmation that the local `controlhub` database did or did not receive the patch
- `git status --short --branch`

Do not write vague summaries. Report the truth changes explicitly so the frontend worker can rely on them after syncing latest `main`.
