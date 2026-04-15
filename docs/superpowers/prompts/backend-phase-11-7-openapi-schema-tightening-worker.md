# Backend Phase 11.7: OpenAPI Schema Tightening

You are implementing the backend OpenAPI schema tightening phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-04-14-agent-friendly-integration-testing-roadmap.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-11-6-schemathesis-openapi-fuzz-worker.md`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`
- `/Users/fan/GolangProjects/ControlHub/internal/model/resource_write.go`
- `/Users/fan/GolangProjects/ControlHub/internal/model/relation_write.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/auth_handler.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/resource_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/relation_service.go`

## Goal

Backend Phase 11.6 introduced Schemathesis OpenAPI fuzzing. It passed, but reported schema validation mismatch warnings because OpenAPI allows request shapes that the backend correctly rejects.

This phase tightens the OpenAPI schema and associated contract tests so generated requests better match real backend validation.

This is a contract-quality phase, not a product feature phase.

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
- base includes backend Phase 11.6 Schemathesis fuzzing
- worktree is clean
- Docker is available for fuzz verification

If Docker is unavailable, stop and report the blocker. Do not replace fuzz verification with fake repositories.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Tighten OpenAPI schema to match existing backend validation.
- Do not relax backend validation just to satisfy Schemathesis.
- Do not add product features.
- Do not add archive / soft-delete in this phase. That belongs to Phase 12.1.
- Do not add auth middleware.
- Do not add Prism, WireMock, Pact, k6, or oapi-codegen.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. inspect Schemathesis warnings from Phase 11.6
2. tighten request schemas for the warned operations
3. add or update contract-focused tests for representative invalid payloads
4. rerun OpenAPI validation and Schemathesis
5. update README/CLAUDE only if commands or documented contract behavior changed

Do not change endpoint behavior unless a test proves OpenAPI and implementation cannot otherwise be made consistent.

## Operations To Tighten

At minimum cover these Phase 11.6 warning operations:

- `POST /auth/login`
- `POST /resources`
- `PATCH /resources/{id}`
- `POST /resources/{id}/relations`

Focus on fields the backend already validates.

### `POST /auth/login`

Schema should document:

- `email` required
- `password` required
- both are strings
- both have `minLength: 1`
- `email` should use `format: email` if compatible with current validation

Do not change auth behavior in this phase.

### `POST /resources`

Schema should document:

- required fields:
  - `resourceType`
  - `name`
  - `displayName`
  - `environmentId`
  - `ownerId`
  - `lifecycleStatus`
  - `healthStatus`
  - `source`
- enum values for:
  - `resourceType`
  - `lifecycleStatus`
  - `healthStatus`
  - `source` currently only `manual`
- string `minLength` where backend requires non-empty
- `labels` as an object with string values if that matches current behavior
- `name` pattern matching the existing operations-friendly backend validation

Do not invent new allowed resource types or statuses.

### `PATCH /resources/{id}`

Schema should document:

- only mutable fields
- no `id`, `name`, `resourceType`, or `createdAt`
- enum values for mutable enum fields
- non-empty string constraints where backend enforces them
- `labels` object shape

If OpenAPI cannot forbid unknown immutable fields cleanly without breaking compatibility, document the limitation in the final report.

### `POST /resources/{id}/relations`

Schema should document:

- `toResourceId` required, string, non-empty
- `relationType` required
- enum values for relation types

Self-relation and existence checks remain runtime validation, not pure schema validation.

## Tests

Follow TDD for any behavior-touching change.

Required:

- OpenAPI validation still passes.
- Add or update API tests only where needed to lock existing validation behavior.
- Do not add broad duplicate tests if equivalent tests already exist.

Suggested focused tests:

- login malformed / missing fields returns JSON error
- create resource missing required field returns 400
- create resource unsupported enum returns 400
- patch immutable field returns 400 if current behavior detects it
- create relation missing `toResourceId` or unsupported relation type returns 400

## Schemathesis Goal

After tightening, run:

```bash
make test-openapi-fuzz
```

Target outcome:

- all checks pass
- schema mismatch warnings are reduced or eliminated for the 4 warned operations

If warnings remain:

- explain exactly why
- identify whether the remaining issue is a real OpenAPI limitation, an implementation limitation, or a future phase item

Do not skip the warned operations just to remove warnings.

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

## Merge Coordination With Phase 12.1

Backend Phase 12.1 may be developed in parallel in another worktree. This phase should be merged first.

If Phase 12.1 is already merged before you finish, sync with `main`, resolve OpenAPI conflicts carefully, and rerun the full verification list.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only. Do not stage `.idea`, logs, container artifacts, temporary files, generated Schemathesis reports, or local virtualenv files.

If GitNexus is available, run the repository's configured change-impact check before commit.

## Final Report

Your final report must include:

- worktree path and branch
- commit hash
- changed files
- exact OpenAPI schema constraints added
- tests added or updated
- Schemathesis warnings before vs after
- verification command results
- confirmation that the daily `controlhub` DB was not touched
- negative scope confirmation:
  - did not add product features
  - did not add archive / soft-delete
  - did not add auth middleware
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not add Prism/WireMock/Pact/k6/oapi-codegen
  - did not tag, push, release, or add AI co-author
- next phase input:
  - remaining OpenAPI limitations
  - whether Phase 12.1 must account for any schema pattern introduced here

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD for behavior-touching changes
- do not reset the repo
- do not discard unrelated work
- do not touch the user's daily `controlhub` database
- do not add product features
- do not weaken backend validation to match a loose schema
