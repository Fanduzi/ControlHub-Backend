# Backend Phase 12.5: Multi-Select Query Contracts For Console Cleanup

You are implementing a focused backend contract phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

This phase exists because the console cleanup now requires real multi-select filtering across list pages. The decision is already made. Do not debate single-select vs multi-select. Implement the backend query contract that the frontend can rely on.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-9-list-pagination-and-filtering-worker.md`
- `/Users/fan/GolangProjects/ControlHub/internal/model/pagination.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/resource_handler.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/audit_handler.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/resource_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/audit_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/repository/mysql/resource_repository.go`
- `/Users/fan/GolangProjects/ControlHub/internal/repository/mysql/audit_repository.go`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
```

Expected:

- worktree path is under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- branch is dedicated to this phase
- base includes backend pagination/filtering work on `main`
- worktree is clean

Stop and report if the path, branch, base, or cleanliness is wrong.

## Parallel Coordination Rules

Frontend and backend workers cannot talk to each other during execution. This prompt is self-contained.

- Your output is the filter-contract freeze for the frontend console cleanup.
- Frontend may build multi-select UI scaffolding in parallel, but it cannot claim final completion until your contract lands on `main`.
- Keep backward compatibility where practical, but prioritize a clean repeatable-parameter contract.
- Final completion requires clear reporting of exact query parameter behavior so the frontend worker can sync latest `main` and wire the UI without guessing.
- Recommended execution order is:
  1. backend Phase 12.5 lands first
  2. frontend Phase 14A syncs latest `main`
  3. frontend finishes request wiring, E2E, and live verification
- Treat true parallel work as limited to frontend scaffolding only, not final integration.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives.

- Multi-select filtering is required.
- Use repeated query parameters for arrays. Example:
  - `?resourceType=database_instance&resourceType=database_cluster`
  - `?environmentId=...&environmentId=...`
  - `?lifecycleStatus=running&lifecycleStatus=degraded`
  - `?healthStatus=healthy&healthStatus=warning`
  - `?eventType=resource.updated&eventType=resource.deleted`
  - `?result=success&result=failure`
- Do not switch to comma-separated query syntax.
- Keep pagination contract intact.
- Keep free-text search parameter intact.
- Do not add a new search backend, full-text index, or query language.
- Do not add sorting in this phase unless required to preserve existing behavior.
- Do not add topology work in this phase.
- You may perform a narrow audit of demo/seed truth only where it directly blocks the overview attention queue from showing meaningful summary text.

## Exact Scope

Implement multi-select query support for:

### `GET /resources`

Support repeated parameters for:

- `resourceType`
- `environmentId`
- `lifecycleStatus`
- `healthStatus`

Keep existing support for:

- `page`
- `pageSize`
- `q`

Semantics:

- within the same filter family, repeated values mean logical OR
- across different filter families, filters combine with logical AND
- empty or absent filter families mean “no restriction”
- preserve archived filtering behavior already on `main`

### `GET /audit-events`

Support repeated parameters for:

- `eventType`
- `result`
- `targetResourceId` only if the current model naturally supports it; if not, leave it single-value and state that explicitly

Keep existing support for:

- `page`
- `pageSize`

Semantics:

- same-family repeated values mean logical OR
- different families combine with AND

### Narrow supporting audit: attention-queue summary truth

The overview attention queue currently risks showing low-value filler. While this is mainly a frontend cleanup phase, you must check whether the current backend truth is the reason the frontend falls back to placeholder summary text.

Bounded responsibility:

- inspect the current demo/seed resources that drive the attention queue
- determine whether meaningful summary text can be derived from existing backend truth
- if the current truth is obviously missing and a small seed patch can fix it cleanly, add that narrow patch in this phase
- if the root cause is not appropriate for this phase, report it explicitly so the frontend worker does not fake the data

Do not turn this into a broad demo-data redesign.

## Required Contract Rules

1. Deterministic parsing

- preserve input order only if it is cheap and natural
- deduplicate repeated identical values if helpful
- do not error on repeated valid values

2. Validation

- invalid enum-like values should keep existing validation semantics
- do not silently coerce unsupported values into a valid one
- malformed pagination values must keep current behavior unless a narrow fix is required

3. SQL/repository behavior

- implement true backend filtering, not in-memory post-filtering
- use safe query building
- do not concatenate raw user strings into SQL

4. OpenAPI

- document repeated-parameter behavior clearly
- keep the spec aligned with actual handler behavior

## TDD Requirements

Use TDD. Add failing tests first.

At minimum add or update tests for:

- parsing repeated `resourceType`
- parsing repeated `environmentId`
- parsing repeated `lifecycleStatus`
- parsing repeated `healthStatus`
- AND combination across families on `/resources`
- repeated `eventType` on `/audit-events`
- repeated `result` on `/audit-events`
- pagination still working when multi-select filters are present
- OpenAPI examples reflecting repeated params

Integration tests must prove real MySQL behavior for at least:

- multiple resource types in one request
- multiple environments in one request
- mixed lifecycle + health filter combination
- multiple audit event types in one request

If you add a narrow seed/data patch for attention-queue summary truth, add regression coverage for that exact truth as well.

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

If local migration/state changes are needed for verification, use the existing non-destructive workflow. Do not reset the user's daily DB.

## Live Verification

Run direct HTTP verification against the local backend and report exact URLs plus key pageInfo/count outcomes for at least:

- one `/resources` request with 2 resource types
- one `/resources` request with 2 environments
- one `/resources` request combining multi-select plus `q`
- one `/audit-events` request with 2 event types
- one `/audit-events` request with 2 results

Also report whether the current backend truth is sufficient for the overview attention queue to show meaningful non-filler summaries.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

If GitNexus is available, run the repo-configured change-impact check before commit.

Stage explicit files only.

## Final Report

Your final report must include:

- worktree path
- branch
- commit hash
- exact repeated-parameter contract now supported on `/resources`
- exact repeated-parameter contract now supported on `/audit-events`
- whether any parameter remained single-value and why
- whether a narrow summary-truth patch was needed for the attention queue, and exactly what changed
- test files added/updated
- all verification command results
- live verification URLs and outcomes
- whether OpenAPI now documents repeated parameters clearly
- `git status --short --branch`

Do not return a vague summary. State the final contract precisely so the frontend worker can wire the UI without asking follow-up questions.
