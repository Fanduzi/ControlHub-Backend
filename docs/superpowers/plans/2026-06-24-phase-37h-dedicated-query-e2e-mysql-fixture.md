# Phase 37H Dedicated Query E2E MySQL Fixture Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move Query Workbench ready-target E2E off the ControlHub metadata database and onto a dedicated Docker MySQL query fixture.

**Architecture:** Continue from the Phase 37G dev fixture branch. Add a Docker-managed query MySQL fixture, then change `cmd/querydev` fixture mode so it derives target host and port from `CONTROLHUB_QUERY_CREDENTIAL_<REF>` rather than `DATABASE_DSN`. `DATABASE_DSN` remains only the ControlHub metadata DB.

**Tech Stack:** Go, MySQL, Docker CLI, Makefile, existing ControlHub repositories and services, existing Playwright query E2E.

---

## 0. Branch Positioning

- [ ] Start from the existing `phase-37g-dev-ready-query-target-fixture` branch if available and clean.
- [ ] Confirm the branch includes the review fix commit that prevents reuse of same-name non-fixture resources.
- [ ] Do not merge Phase 37G to `main` before applying this correction unless explicitly instructed.
- [ ] If the branch is unavailable, recreate from the approved Phase 37G commits and then apply this plan.
- [ ] Keep backend and frontend repositories separate. Backend code changes happen only in the backend repo.

Verification:

```bash
git status --short --branch
git log --oneline -8
```

Expected:

- clean backend worktree,
- branch contains the dev fixture service and querydev fixture mode,
- no push, tag, release, or deploy.

## B1. Add Dedicated Query MySQL Docker Fixture

Files:

- `scripts/query-e2e-mysql.sh` new
- `Makefile`
- `.gitignore`

Design:

- Add one script with subcommands:
  - `up`
  - `down`
  - `status`
- Defaults:
  - `QUERY_E2E_MYSQL_CONTAINER=controlhub-query-e2e-mysql`
  - `QUERY_E2E_MYSQL_PORT=13306`
  - `QUERY_E2E_MYSQL_DATABASE=query_e2e`
  - `QUERY_E2E_MYSQL_READONLY_USER=query_e2e_ro`
- The script owns the local password handoff. It may accept `QUERY_E2E_MYSQL_READONLY_PASSWORD`, otherwise it generates or reuses a local fixture password inside the gitignored env file.
- The script must write `.query-e2e-mysql.env` with `CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO=<dedicated read-only DSN>`.
- Add `.query-e2e-mysql.env` to `.gitignore`.
- The script must not print the read-only DSN or password.
- The script may print safe facts:
  - container name,
  - host,
  - port,
  - database name,
  - readiness state.
- `up` must create or start the named container, wait for MySQL readiness, create schema, create seed table, insert seed rows, create read-only user, and grant `SELECT`.
- `up` must create or refresh `.query-e2e-mysql.env` without echoing its contents.
- `down` must stop and remove only the named fixture container.
- `down` may remove `.query-e2e-mysql.env` after stopping the container.
- `status` must report whether the named fixture is running.
- Do not touch ControlHub metadata DB.

Add Makefile targets:

```make
query-e2e-mysql-up
query-e2e-mysql-down
query-e2e-mysql-status
```

TDD / verification:

- [ ] Run `bash -n scripts/query-e2e-mysql.sh`.
- [ ] Run `make -n query-e2e-mysql-up`.
- [ ] Run `make -n query-e2e-mysql-down`.
- [ ] Run `make query-e2e-mysql-up`.
- [ ] Verify `.query-e2e-mysql.env` exists and is ignored by git: `git check-ignore .query-e2e-mysql.env`.
- [ ] Verify a read-only user can run `select * from query_e2e_items`.
- [ ] Verify a write attempt using the read-only user fails.
- [ ] Run `make query-e2e-mysql-down`.
- [ ] Run `make query-e2e-mysql-up` again to prove idempotency.

Commit:

```bash
git add scripts/query-e2e-mysql.sh Makefile .gitignore
git commit -m "test: add dedicated query e2e mysql fixture"
```

## B2. Derive Fixture Target Host And Port From Credential DSN

Files:

- `cmd/querydev/main.go`
- `cmd/querydev/main_test.go`
- `internal/service/query_dev_target_fixture.go`
- `internal/service/query_dev_target_fixture_test.go`

Design:

- `DATABASE_DSN` must only open the ControlHub metadata repository.
- With `QUERY_DEV_ALLOW_TARGET_FIXTURE=true`, querydev must:
  - read `QUERY_DEV_CREDENTIAL_REF`,
  - resolve `CONTROLHUB_QUERY_CREDENTIAL_<REF>`,
  - parse the resolved credential DSN's explicit `tcp(host:port)` address,
  - pass that host and port into `QueryDevTargetFixture.EnsureLocalQueryTarget`,
  - then run the existing metadata-only credential seed.
- The command output must not print the resolved DSN or password.
- Keep the Phase 37G source boundary:
  - reuse only `source='dev-fixture'`,
  - reject same-name non-fixture resources,
  - do not profile-overwrite non-fixture resources.
- Rename or replace any helper that implies the ControlHub metadata DSN is the target DSN. A neutral helper name such as `ParseMySQLDSNHostPort` is preferred.

RED tests:

- [ ] Querydev fixture mode uses credential DSN host and port, not `DATABASE_DSN` host and port.
- [ ] Querydev fixture mode errors when `QUERY_DEV_CREDENTIAL_REF` is missing.
- [ ] Querydev fixture mode errors when `CONTROLHUB_QUERY_CREDENTIAL_<REF>` is missing.
- [ ] Querydev fixture mode errors when the credential DSN omits an explicit port.
- [ ] Querydev fixture mode output contains no `tcp(`, no `@`, no `://`, and no password marker.
- [ ] Existing same-name non-fixture rejection tests still pass.

GREEN criteria:

```bash
go test ./cmd/querydev -run QueryDev
go test ./internal/service -run 'QueryDevTargetFixture|Parse.*DSNHostPort'
```

Commit:

```bash
git add cmd/querydev internal/service
git commit -m "fix: derive dev query target from credential dsn"
```

## B3. Add Local End-To-End Backend Verification Against Dedicated Query DB

Files:

- `internal/integration/query_dev_target_fixture_test.go`
- `docs/superpowers/notes/2026-06-24-phase-37h-dedicated-query-e2e-mysql-fixture-evidence.md` new or appended after D1

Design:

- Keep existing Testcontainers integration tests.
- Add or adjust tests so at least one path proves metadata DB and query target DB are conceptually distinct.
- If a full second Testcontainers MySQL inside Go tests is too heavy, cover the separation in command-level local verification and document it explicitly in the evidence note. Do not weaken the existing integration coverage.

Required checks:

- [ ] Fixture target profile host and port match the credential DSN host and port.
- [ ] Fixture target becomes `ready`.
- [ ] `select 1` or a deterministic fixture table query succeeds.
- [ ] Unsafe statement is rejected.
- [ ] Query history records attempts.
- [ ] No DSN is stored in `query_target_credentials`.
- [ ] Same-name non-fixture resource remains protected.

Commands:

```bash
go test -count=1 ./internal/integration -run 'QueryDev|QueryExecution'
make test-integration
```

Commit:

```bash
git add internal/integration docs/superpowers/notes
git commit -m "test: verify dedicated query target fixture"
```

## B4. Local Manual Verification With Backend And Frontend

Backend commands from the backend repo:

```bash
make query-e2e-mysql-up

set -a
. ./.env
. ./.query-e2e-mysql.env
set +a

QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO \
make seed-query-dev-target

go run ./cmd/server
```

The `.query-e2e-mysql.env` file is the only supported DSN handoff in this phase. It must be gitignored, must not be committed, and must not be printed. Reports may state only that the credential DSN was sourced from `.query-e2e-mysql.env`.

Verify:

```bash
curl -fsS http://localhost:8080/health
curl -fsS http://localhost:8080/query-targets
```

Expected:

- one local fixture target is `ready`,
- `availableActions.run` is `true`,
- `governance.safetyState` is `readonly_sandbox_enabled`,
- no DSN appears in output.
- `git status --short` does not show `.query-e2e-mysql.env`.

Frontend commands from `/Users/fan/JsProjects/ControlHub`:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npm run test:e2e -- --grep query
```

Expected:

- query E2E has zero failures,
- ready-target tests are executed,
- ready-target skips are zero.

After verification:

```bash
make query-e2e-mysql-down
```

Stop the backend server unless the user explicitly asks to keep it running.

## B5. Full Backend Verification

Run from the backend worktree:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Expected:

- all pass,
- no reduced checks,
- no skipped tests hidden in the report,
- fuzz warnings, if any, are classified explicitly.

Run GitNexus checks per repository rules:

```bash
npx gitnexus detect-changes --scope compare --base-ref main
```

If the index is stale, run:

```bash
npx gitnexus analyze
```

Then rerun detect changes. Report any HIGH or CRITICAL risk honestly.

## D1. Evidence Note

Files:

- `docs/superpowers/notes/2026-06-24-phase-37h-dedicated-query-e2e-mysql-fixture-evidence.md`

Record:

- branch and commit list,
- Docker fixture container name and port,
- statement that the query target DSN was supplied via `.query-e2e-mysql.env` and not printed,
- proof that `DATABASE_DSN` remained the ControlHub metadata database only,
- `/query-targets` ready-target count,
- backend verification matrix,
- frontend query E2E result with ready-target skip count,
- cleanup status,
- scope confirmations.

Do not include:

- DSN value,
- password,
- JWT secret,
- access token,
- screenshots with credentials.

Commit:

```bash
git add docs/superpowers/notes
git commit -m "docs: record dedicated query e2e mysql fixture evidence"
```

## Rollback And Cleanup

Local Docker cleanup:

```bash
make query-e2e-mysql-down
```

Local ControlHub metadata cleanup may use the existing Phase 37G rollback SQL, constrained by:

- `name='local-mysql-query-dev'`,
- `resource_type='database_instance'`,
- `source='dev-fixture'`,
- environment slug `dev`,
- exact resolved fixture resource id.

Do not use name-only deletes.

## Final Report Checklist

Report:

- branch,
- commit hashes,
- changed files,
- exact verification commands and results,
- Docker fixture status,
- whether frontend query E2E ran with zero ready-target skips,
- whether backend server was stopped,
- whether Docker query fixture was stopped,
- GitNexus result and caveats,
- final git status for backend and frontend,
- scope confirmation.

Scope confirmation must include:

- no credential leak,
- no credential UI,
- no production auto-enable,
- no migration,
- no new repository method,
- no credential-binding relaxation,
- no frontend product edits unless explicitly authorized,
- no push, tag, release, or deploy,
- no AI co-author.
