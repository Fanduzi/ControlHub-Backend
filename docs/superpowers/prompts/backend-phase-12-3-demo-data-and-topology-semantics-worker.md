# Backend Phase 12.3: Demo Data And Topology Semantics Cleanup

You are implementing the backend demo-data and topology-semantics cleanup phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-11-resource-topology-read-model-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-2-resource-archive-lifecycle-worker.md`
- `/Users/fan/GolangProjects/ControlHub/migrations/0004_seed_demo_data.sql`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`
- `/Users/fan/GolangProjects/ControlHub/internal/model/taxonomy.go`
- `/Users/fan/GolangProjects/ControlHub/internal/model/relation.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/topology_service.go`

## Goal

ControlHub now has a working asset model, archive lifecycle, and topology read API. The current user-visible issues are no longer about missing endpoints, but about semantics and demo realism:

- some demo resource names read like internal migration leftovers instead of believable platform resources
- the current seed topology does not express the Proxy/VIP/MySQL layering clearly enough
- environment identity is stable but too opaque in user-visible flows unless slug-friendly data is consistently available

This phase improves backend demo truth and topology semantics so the frontend can present more realistic control-console views without inventing relationships client-side.

This is a backend data-contract and seed-quality phase. Keep it explicit and testable.

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
- base includes backend Phase 12.2 on `main`
- worktree is clean
- Docker is available for integration and fuzz verification

If Docker is unavailable, stop and report the blocker.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Do not change the topology endpoint shape in a breaking way.
- Do not add topology editing.
- Do not add new product areas such as SQL work orders, query execution, or discovery.
- Keep UUID-style internal IDs as backend primary keys.
- Preserve environment `id` fields, but make sure slug/name data remains available and consistent for frontend-readable use.
- Improve demo realism in seed data rather than adding frontend-only presentation hacks.
- Strengthen the MySQL + ProxySQL + VIP/HA topology story in seed data.
- Use existing resource types and relation types where possible; only extend taxonomy if absolutely necessary and justified by the prompt scope.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. clean up demo resource names/descriptions that currently read like temporary migration notes
2. improve seed topology around database_proxy / virtual_ip / database_cluster / database_instance / control_plane_component
3. ensure the seeded graph expresses realistic upstream layering for MySQL-style deployments
4. preserve or improve frontend-readable environment metadata support (`id`, `name`, `slug`)
5. update tests, integration verification, and docs only where needed to reflect the new seed truth

Do not redesign the whole domain model.

## Required Seed Improvements

At minimum address these semantics in `0004_seed_demo_data.sql`:

- database middleware should sit above MySQL cluster resources
- VIP / domain / proxy relationships should read naturally in topology
- HA/control-plane components should not visually collapse into the same role as application services
- at least one MySQL production example should clearly express:
  - VIP or domain entry
  - two proxy nodes or an explicit active/standby-style proxy representation if the current model supports it cleanly
  - one cluster
  - one primary instance
  - one or more replica instances

You do not need to perfectly simulate every database product. You do need to stop the current graph from looking like a flat bag of neighbors.

## Naming Cleanup

Replace seed names/descriptions that are obviously temporary or confusing.

Example category:

- `Notification Delivery Service Production - Currently Disabled for Migration`

The replacement should still preserve the operational meaning (disabled, stopped, migration-related if relevant), but it should read like a believable platform asset name rather than a sentence accidentally exposed to users.

Do not mass-rename everything. Fix the clearly bad demo/UI-facing names.

## Environment Metadata

Preserve the current UUID primary-key model.

Do not switch APIs to slug-only identifiers.

But ensure the backend remains frontend-friendly by keeping these usable and stable:

- environment `id`
- environment `name`
- environment `slug`

If the current seeded names/slugs are inconsistent with the user-facing environment experience, fix the seed/reference data accordingly.

## Topology Semantics Rules

The topology API remains a graph API, not a layout API. Do not add pixel coordinates or frontend layout hints in this phase.

But the returned neighborhood should become semantically more useful through better graph data:

- proxy resources should relate to clusters with meaningful relation types
- database instances should remain clearly tied to their cluster
- host/control-plane relationships should stay readable but not dominate the core MySQL path
- application services that depend on the database should remain visible as consumers, not be confused with proxy/middleware layers

Prefer improving graph truth over adding frontend-only documentation comments.

## Testing

Follow TDD.

At minimum add or update tests for:

- changed seed/resource counts if they materially change
- topology integration cases that prove the improved graph semantics
- demo name cleanup assertions if there are tests or fixtures that reference those names
- environment dictionary behavior if names/slugs change
- OpenAPI validation still passing
- integration tests still passing on disposable MySQL
- Schemathesis fuzz still passing

If a test currently asserts brittle old seed names, update it to assert the new intended truth, not the old mistake.

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

Also run live local verification when practical:

- fetch at least one renamed resource by ID
- inspect one database-cluster topology payload after the seed improvements
- verify the topology now clearly includes proxy / cluster / primary / replica style neighbors

Do not use the user's daily `controlhub` DB destructively unless explicitly instructed. Prefer disposable DBs or carefully replayable local verification.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only. Do not stage local scratch files, container artifacts, generated reports, or unrelated temp files.

If GitNexus is available, run the repository's configured change-impact check before commit.

## Final Report

Your final report must include:

- worktree path and branch
- commit hash
- changed files
- exact demo-name changes made
- exact topology/seed semantics improved
- whether any taxonomy/relation changes were needed
- verification command results
- live backend verification result
- negative scope confirmation:
  - did not add frontend changes
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not replace UUID primary keys with slug IDs
  - did not tag, push, release, or add AI co-author
- next phase input:
  - any remaining demo-data weirdness
  - any remaining topology semantics gaps
  - whether frontend now has enough truthful data for layout/UX cleanup

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD for changed behavior and seed-backed expectations
- do not reset the repo
- do not discard unrelated work
- do not redesign the whole schema
- do not let any tool scan `.worktrees/**` from the main checkout
