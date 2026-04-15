# Backend Phase 11.5: Testcontainers Integration Harness

You are implementing the backend integration-test harness phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-04-14-agent-friendly-integration-testing-roadmap.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-11-resource-topology-read-model-worker.md`
- `/Users/fan/GolangProjects/ControlHub/README.md`
- `/Users/fan/GolangProjects/ControlHub/CLAUDE.md`
- `/Users/fan/GolangProjects/ControlHub/Makefile`

## Goal

ControlHub now has asset writes, relation maintenance, topology reads, goose migrations, and OpenAPI validation. Most backend tests still use fake repositories, which cannot catch real MySQL behavior.

This phase adds a focused Testcontainers-based MySQL integration harness.

Do not add Schemathesis in this phase. Schemathesis is a later backend phase after the MySQL integration harness is stable.

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
- base includes backend Phase 11 topology merge
- worktree is clean except unrelated local files in the main worktree
- Docker is available

If Docker is unavailable, stop and report the blocker. Do not replace Testcontainers with fake repositories.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Use Testcontainers Go with MySQL 8.0.
- Add `make test-integration`.
- Keep regular `make test` fast and not dependent on Docker.
- Integration tests must run against a disposable MySQL container.
- Use goose to migrate the disposable DB from zero to latest.
- Do not use the user's daily `controlhub` database.
- Do not add Schemathesis, Pact, Prism, WireMock, k6, or oapi-codegen in this phase.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. add Testcontainers MySQL integration-test support
2. add helper code for disposable DB setup and goose migration
3. add integration tests for high-value MySQL behavior
4. add `make test-integration`
5. update README/CLAUDE with integration-test workflow

Do not change product API behavior unless an integration test reveals a real bug. If a bug is found, add the failing integration test first, then fix the smallest code path.

## Suggested Structure

Use a clear integration-test boundary. Acceptable options:

- `internal/integration/...`
- or package-local `*_integration_test.go` files with a build tag

Preferred:

```text
internal/integration/
  mysql_test.go
  testenv_test.go
```

Use a build tag so normal unit tests remain fast:

```go
//go:build integration
```

Then `make test-integration` can run:

```bash
go test -tags=integration -count=1 ./internal/integration
```

If you choose a different structure, document why in the final report.

## Testcontainers Requirements

Use:

- `github.com/testcontainers/testcontainers-go`
- MySQL 8.0 image

The integration harness must:

- start a disposable MySQL container
- create/use a test database
- build a valid `DATABASE_DSN`
- run goose migrations to latest
- expose helpers for opening `*sql.DB`
- terminate the container after tests

Tests must not require manually running MySQL.

## Integration Tests Required

At minimum cover these cases.

### 1. Goose Clean Migration

Assert:

- all migrations apply successfully from zero
- `goose_db_version` exists
- latest version is current
- expected tables exist
- `resources` has `idx_resources_lifecycle`
- `resources` has `uq_resource_name_env(name, environment_id)`
- `resources` does not have global unique index only on `name`
- `resource_relations` has unique `(from_resource_id, to_resource_id, relation_type)`

### 2. Resource Repository Writes

Using real repository/service/API path where practical, assert:

- create resource succeeds
- fetch created resource succeeds
- patch mutable fields succeeds
- same `name + environmentId` returns conflict
- same `name` in different environment succeeds
- MySQL duplicate key `1062` maps to `409`-equivalent service error

### 3. Relation Repository Writes

Assert:

- create relation succeeds
- duplicate relation returns conflict
- delete relation succeeds
- deleting unknown relation returns not found

### 4. Topology SQL Neighborhood

Assert with seeded/demo data or created fixtures:

- topology relation neighborhood query returns expected direct relations
- depth-relevant relation batches include both incoming and outgoing edges
- relation type filtering works through the service/API path if practical

Do not overfit exact demo row counts. Prefer deterministic test-created fixtures where possible.

## Makefile

Add:

```makefile
test-integration
```

Rules:

- `make test` must remain non-Docker unit tests.
- `make test-integration` may require Docker.
- If Docker is unavailable, it should fail clearly.

Do not make `make test` call `make test-integration` in this phase.

## Documentation

Update:

- `README.md`
- `CLAUDE.md`
- any relevant backend testing README if present

Document:

- purpose of integration tests
- Docker requirement
- `make test-integration`
- that tests use disposable MySQL containers
- that daily local `controlhub` DB is not touched
- when workers should run unit tests vs integration tests

## Verification

You must run:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
make test-integration
```

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only. Do not stage unrelated `.idea`, logs, container artifacts, or temporary files.

## Final Report

Your final report must include:

- worktree path and branch
- commit hash
- changed files
- Testcontainers image/version used
- integration-test structure
- exact `make test-integration` command behavior
- integration cases covered
- verification command results
- whether Docker was required and available
- confirmation that the daily `controlhub` DB was not touched
- negative scope confirmation:
  - did not add Schemathesis
  - did not change product API behavior unless explicitly listed
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not tag, push, release, or add AI co-author
- next phase input:
  - whether backend is ready for Schemathesis
  - any MySQL behavior gaps found
  - any performance risks found

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD
- do not reset the repo
- do not discard unrelated work
- do not touch the user's daily `controlhub` database
- do not add Schemathesis in this phase
- do not add product features
- do not make server startup auto-run migrations
