# Backend Phase 37F Dev Query Credential Worker Prompt

You are implementing the backend side of Phase 37F for ControlHub.

Backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repository is separate and must not be edited by this worker:

```text
/Users/fan/JsProjects/ControlHub
```

## Objective

Add a local/dev-only credential metadata seed path so one MySQL/TiDB query target
can become ready for the Query Workbench. This worker does not build frontend
execution UI.

## Required Reading

Read first:

```text
docs/superpowers/specs/2026-06-22-phase-37f-query-execute-ui-and-dev-credentials.md
docs/decisions/2026-06-22-phase-37f-query-execute-ui-boundary.md
docs/superpowers/plans/2026-06-22-phase-37f-query-execute-ui-and-dev-credentials.md
docs/superpowers/specs/2026-06-21-phase-37-read-only-query-sandbox.md
docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

Read relevant code:

```text
internal/service/query_execution_service.go
internal/service/query_target_service.go
internal/repository/mysql/query_execution_repository.go
internal/repository/mysql/query_target_repository.go
internal/integration/query_execution_test.go
cmd/server/main.go
Makefile
```

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-37f-dev-query-credential -b phase-37f-dev-query-credential main
cd .worktrees/backend-phase-37f-dev-query-credential
git status --short --branch
```

Do not edit the main worktree directly.

## Scope

Allowed:

```text
local/dev seed command
query_target_credentials metadata upsert
service validation
integration tests
Makefile target
evidence note
```

Not allowed:

```text
frontend edits
credential write API
plaintext DSN/password storage
printing DSN/password
production auto-enable
new query engines
SQL execution behavior changes except tests needed for seed verification
push/tag/release/deploy
```

## Implementation

Follow backend Tasks B1 and B2 in:

```text
docs/superpowers/plans/2026-06-22-phase-37f-query-execute-ui-and-dev-credentials.md
```

Key behavior:

- Seed command is explicit, not automatic.
- Metadata row stores only `resource_id`, `engine`, `credential_ref`,
  `enabled`, and `environment_policy`.
- Actual DSN is read from `CONTROLHUB_QUERY_CREDENTIAL_<REF>`.
- DSN must bind to selected target host/port using Phase 37 logic.
- Default policy is `non_prod_only`.
- `all_environments` requires explicit override.
- Command is idempotent.

Suggested Makefile target:

```text
make seed-query-dev-credential
```

Required env:

```text
QUERY_DEV_TARGET_RESOURCE_ID
QUERY_DEV_CREDENTIAL_REF
CONTROLHUB_QUERY_CREDENTIAL_<REF>
QUERY_DEV_ENVIRONMENT_POLICY
QUERY_DEV_ALLOW_ALL_ENVIRONMENTS
```

## Verification

Run:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Docker gates must run if Docker is available. Do not skip silently.

## Final Report

Include:

- worktree, branch, commit hash
- files changed
- exact seed command and env vars
- proof no DSN/password is stored or printed
- integration test summary
- full verification matrix
- Docker gate result
- final git status
- scope confirmation:
  - no frontend changes
  - no credential write API
  - no plaintext credential storage/logging/response
  - no production auto-enable
  - no push/tag/release/deploy
  - no AI co-author
