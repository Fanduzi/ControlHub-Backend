# Backend Phase 12.7: Resource Subtype Filter Contract

You are implementing a focused backend contract fix for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

This phase exists because the frontend now sends `resourceSubtype` query parameters from the resources and databases pages, but live review showed this URL does not filter correctly:

`/databases?environment=prod&page=1&resourceSubtype=mysql`

The backend must make `resourceSubtype` a real query contract, not a frontend-only URL decoration.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-5-multi-select-query-contracts-worker.md`
- `/Users/fan/GolangProjects/ControlHub/internal/model/pagination.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/resource_handler.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/resource_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/repository/mysql/resource_repository.go`
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
- branch is dedicated to this phase, for example `phase-12.7-resource-subtype-filter`
- base includes backend Phase 12.5 and Phase 12.6 on `main`
- worktree is clean

Stop and report if the path, branch, base, or cleanliness is wrong.

## Parallel Coordination Rules

Frontend and backend workers cannot communicate during execution. This prompt is self-contained.

- This backend phase freezes the `resourceSubtype` filter contract for the frontend Phase 15A cleanup.
- Frontend may implement UI cleanup in parallel, but it cannot claim final completion until this backend contract lands on `main`.
- After this backend phase lands, frontend must rebase/sync latest `main`, then rerun E2E and live browser verification against the updated backend.

Recommended merge order:

1. backend Phase 12.7 lands first
2. frontend Phase 15A syncs latest backend truth through its normal local backend
3. frontend Phase 15A completes final E2E/live verification

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives.

- `resourceSubtype` must filter real backend results.
- Use the same repeated-parameter convention as Phase 12.5:
  - `?resourceSubtype=mysql`
  - `?resourceSubtype=mysql&resourceSubtype=clickhouse`
- Do not switch to comma-separated syntax.
- Same-family repeated values mean logical OR.
- Different filter families combine with logical AND.
- Keep `environmentId`, `resourceType`, `lifecycleStatus`, `healthStatus`, `q`, archive filters, and pagination behavior intact.
- Do not add database vendor logo/icon behavior in backend.
- Do not change topology semantics in this phase.
- Do not modify frontend code in this backend phase.

## Exact Scope

Implement `resourceSubtype` filtering for `GET /resources`.

Required behavior:

- `GET /resources?resourceSubtype=mysql` returns only resources whose `resource_subtype = 'mysql'`.
- `GET /resources?resourceSubtype=mysql&resourceSubtype=clickhouse` returns mysql OR clickhouse resources.
- `GET /resources?environmentId=<prod>&resourceSubtype=mysql` returns mysql resources within that environment only.
- `GET /resources?resourceType=database_instance&resourceSubtype=mysql` combines with AND.
- `GET /resources?q=mysql&resourceSubtype=mysql` combines search and subtype filtering.
- Empty / absent `resourceSubtype` means no subtype restriction.
- Duplicate subtype values should not change results.

Data model expectations:

- If `ResourceListQuery` already has `ResourceSubtype`, convert or extend it consistently to match Phase 12.5 multi-select style.
- Keep the public API backwards-compatible for single subtype values.
- Do not use in-memory post-filtering; filtering must happen in repository SQL.
- Build parameterized `IN (?, ?)` clauses safely.

OpenAPI expectations:

- Document `resourceSubtype` on `GET /resources` as an array query parameter:
  - `style: form`
  - `explode: true`
  - `items.type: string`
- Keep existing examples valid.

## TDD Requirements

Use TDD. Add failing tests first.

At minimum add or update tests for:

- handler parses one `resourceSubtype`
- handler parses repeated `resourceSubtype`
- duplicate subtype values are deduplicated or otherwise do not alter results
- subtype combines with `environmentId`
- subtype combines with `resourceType`
- subtype combines with `q`
- fake repository path reflects real filtering behavior
- MySQL repository integration proves subtype filtering against seed data
- OpenAPI validation still passes

If the current `NormalizePagination` / query structs need new helpers, add narrow model tests for them.

## Required Verification

Run all of:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
make test-integration
make test-openapi-fuzz
```

If Docker or Schemathesis is unavailable, do not claim final completion. Report an implementation progress update with the missing command and reason.

## Live HTTP Verification

Start the backend against local MySQL and report exact URLs plus `pageInfo.totalItems` / sampled item subtypes for:

```text
/resources?environmentId=10000000-0000-0000-0000-000000000001&resourceSubtype=mysql&page=1&pageSize=50
/resources?environmentId=10000000-0000-0000-0000-000000000001&resourceSubtype=clickhouse&page=1&pageSize=50
/resources?environmentId=10000000-0000-0000-0000-000000000001&resourceSubtype=mysql&resourceSubtype=clickhouse&page=1&pageSize=50
/resources?resourceType=database_instance&resourceSubtype=mysql&page=1&pageSize=50
/resources?q=mysql&resourceSubtype=mysql&page=1&pageSize=50
```

The sampled items must all match the expected subtype constraints. Do not rely only on HTTP status 200.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

If GitNexus is available, run the configured staged change detection before committing.

Stage only files required for this backend contract. Do not commit unrelated docs, local DB artifacts, logs, screenshots, or frontend files.

## Commit

Commit after verification passes.

Suggested message:

```bash
git commit -m "feat: support resource subtype filtering in resource list queries (Phase 12.7)"
```

Do not add AI co-author trailers.

## Final Report Requirements

Only write a final closeout report if all Closeout Gate requirements from the shared guardrails are satisfied.

The final report must include:

- commit hash
- worktree path and branch
- clean git status
- exact files changed
- exact `resourceSubtype` query semantics
- OpenAPI parameter shape
- test results for every required command
- live HTTP verification table with URLs and counts
- confirmation that frontend code was not modified
- confirmation that topology, logos/icons, SQL work orders, auth middleware, tags, pushes, and releases were not touched
- next phase input for frontend Phase 15A
