# Backend Phase 11.6: Schemathesis OpenAPI Fuzzing

You are implementing the backend OpenAPI fuzz-testing phase for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-04-14-agent-friendly-integration-testing-roadmap.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-11-5-testcontainers-integration-harness-worker.md`
- `/Users/fan/GolangProjects/ControlHub/README.md`
- `/Users/fan/GolangProjects/ControlHub/CLAUDE.md`
- `/Users/fan/GolangProjects/ControlHub/Makefile`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`

## Goal

ControlHub now has:

- OpenAPI validation
- goose-managed migrations
- real MySQL integration tests through Testcontainers
- resource writes, relation writes, and topology reads

This phase adds Schemathesis-based OpenAPI fuzzing against a disposable real backend. The goal is to find contract violations, unexpected 5xx responses, and schema drift before frontend or future agents depend on broken API behavior.

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
- base includes backend Phase 11.5 Testcontainers integration harness
- worktree is clean
- Docker is available

If Docker is unavailable, stop and report the blocker. Do not replace this phase with fake repositories or the daily local MySQL database.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Use Schemathesis to exercise `internal/openapi/openapi.yaml`.
- Run fuzzing against a real HTTP server backed by disposable Testcontainers MySQL.
- Do not touch the user's daily `controlhub` database.
- Add a repeatable command, preferably `make test-openapi-fuzz`.
- Keep `make test` fast and non-Docker.
- Keep `make test-integration` focused on Go integration tests.
- Do not add Prism, WireMock, Pact, k6, oapi-codegen, or frontend changes in this phase.
- Do not add product features.
- Use project-local worktree path under `/Users/fan/GolangProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. add a Schemathesis fuzz-test harness
2. start a real local backend server against disposable MySQL for fuzzing
3. run goose migrations before fuzzing
4. execute bounded Schemathesis checks against `/openapi.yaml`
5. add `make test-openapi-fuzz`
6. document the workflow in README/CLAUDE
7. fix only contract or server bugs directly exposed by fuzzing

Do not change product API behavior unless Schemathesis exposes a real bug. If a bug is found, add a focused regression test first, then make the smallest fix.

## Preferred Implementation Shape

Use the existing Phase 11.5 Testcontainers pattern.

Preferred files:

```text
internal/integration/
  openapi_fuzz_test.go
scripts/
  openapi-fuzz.sh      # only if a script is cleaner than embedding command setup in Go
```

Preferred command:

```bash
make test-openapi-fuzz
```

Preferred behavior:

- start disposable MySQL 8.0 container
- run goose migrations to latest
- start ControlHub HTTP server on `127.0.0.1:<random-port>`
- serve `/openapi.yaml` from that same server
- run Schemathesis against that server
- stop server and container after completion

If invoking the Schemathesis CLI from Go is awkward, a small shell script is acceptable. The script must still use disposable DB/server setup or be called by an integration test that does.

## Schemathesis Requirements

Use a bounded local run suitable for AI agents and local development.

The fuzz run must check at least:

- no unexpected 5xx responses
- responses match declared OpenAPI schemas
- declared status codes and content types are respected
- query/path parameter edge cases do not crash handlers

Keep the run bounded so it is usable locally. If you set case limits, deadline, or seed options, document the exact values.

Do not run Schemathesis against the user's existing `http://localhost:8080` unless explicitly doing a manual extra smoke. The repeatable test command must be self-contained.

## Handling Writes During Fuzzing

The fuzz target may call write endpoints such as:

- `POST /resources`
- `PATCH /resources/{id}`
- `POST /resources/{id}/relations`
- `DELETE /resource-relations/{id}`

That is acceptable because the database is disposable.

Rules:

- do not point fuzzing at the daily `controlhub` database
- do not disable write endpoints just to make fuzzing easier
- if fuzzing exposes validation gaps, fix the handler/service contract or OpenAPI schema intentionally
- if a case is invalid but OpenAPI says it is valid, fix OpenAPI or validation so they match

## Auth

Current backend APIs are not protected by auth middleware. Do not add auth middleware in this phase.

If Schemathesis reaches `POST /auth/login`, it should not require a pre-auth token. If auth-related contract bugs appear, document them and fix only schema/handler issues that are in scope.

## Expected Bug-Fix Pattern

If Schemathesis finds a failure:

1. capture the failing operation, method, path, parameters/body, and response
2. add a focused Go test reproducing the bug
3. fix the smallest handler/service/OpenAPI mismatch
4. rerun the focused test
5. rerun `make test-openapi-fuzz`

Do not patch around Schemathesis by skipping broad endpoint groups unless there is a clearly documented product reason.

## Makefile

Add:

```makefile
test-openapi-fuzz
```

Rules:

- `make test` must remain non-Docker unit tests.
- `make test-integration` must remain the Testcontainers Go integration suite.
- `make test-openapi-fuzz` may require Docker and Schemathesis tooling.
- If required local tooling is missing, fail with a clear message explaining what to install or which command wrapper is expected.

## Documentation

Update:

- `README.md`
- `CLAUDE.md`

Document:

- purpose of Schemathesis fuzzing
- Docker requirement
- Schemathesis tooling requirement
- `make test-openapi-fuzz`
- that the daily `controlhub` DB is not touched
- when workers should run unit tests, integration tests, and OpenAPI fuzzing
- any known endpoint exclusions, if unavoidable

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

If `make test-openapi-fuzz` finds real API bugs, fix them and rerun the full verification list.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only. Do not stage `.idea`, logs, container artifacts, temporary files, generated Schemathesis reports, or local virtualenv files unless explicitly intended.

If GitNexus is available, run the repository's configured change-impact check before commit.

## Final Report

Your final report must include:

- worktree path and branch
- commit hash
- changed files
- Schemathesis version and invocation method
- whether the fuzz target used disposable MySQL
- exact `make test-openapi-fuzz` behavior
- endpoints covered
- endpoints skipped, if any, with reason
- failures found and fixes made
- verification command results
- confirmation that the daily `controlhub` DB was not touched
- negative scope confirmation:
  - did not add product features
  - did not add auth middleware
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not add Prism/WireMock/Pact/k6/oapi-codegen
  - did not tag, push, release, or add AI co-author
- next phase input:
  - remaining contract gaps
  - endpoints that need stronger examples or schemas
  - whether frontend E2E should consume any newly clarified contract behavior

## Constraints

- use a dedicated worktree under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- use TDD for any bug revealed by Schemathesis
- do not reset the repo
- do not discard unrelated work
- do not touch the user's daily `controlhub` database
- do not add product features
- do not make server startup auto-run migrations
- do not weaken OpenAPI validation to hide failures
