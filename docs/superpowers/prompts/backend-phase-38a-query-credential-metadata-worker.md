# Backend Phase 38A Query Credential Metadata Worker Prompt

You are implementing the backend side of Phase 38A for ControlHub.

Backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repository is separate and must not be edited by this worker:

```text
/Users/fan/JsProjects/ControlHub
```

## Objective

Add authenticated query credential metadata management APIs for MySQL/TiDB query
targets. The backend manages metadata only: credential reference, enabled flag,
and environment policy. It must never accept, store, return, audit, or log a
plaintext DSN/password.

## Required Reading

Read first:

```text
docs/superpowers/specs/2026-06-24-phase-38a-query-credential-metadata-management-design.md
docs/superpowers/plans/2026-06-24-phase-38a-query-credential-metadata-management.md
docs/superpowers/specs/2026-06-24-phase-38a-query-credential-ui-preview.md
docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

Read relevant code:

```text
internal/model/query_execution.go
internal/model/query_target.go
internal/service/query_execution_service.go
internal/service/query_target_service.go
internal/repository/mysql/query_execution_repository.go
internal/repository/mysql/query_target_repository.go
internal/api/auth_middleware.go
internal/api/router.go
cmd/server/main.go
internal/openapi/openapi.yaml
```

## Worktree

Create a backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-38a-query-credential-metadata -b phase-38a-query-credential-metadata main
cd .worktrees/backend-phase-38a-query-credential-metadata
git status --short --branch
```

Do not edit the main worktree directly.

## Scope

Allowed:

```text
query credential metadata model/request/response types
metadata get/upsert/delete repository methods
credential runtime status service
readiness correction for GET /query-targets
authenticated credential metadata API
OpenAPI contract
backend unit/integration/fuzz tests
evidence note
```

Not allowed:

```text
frontend edits
plaintext DSN/password in request/response/storage/audit/logs
secret write API
secret manager integration
new query engines
SQL guard relaxation
export/saved query/approval features
workflow edits
push/tag/release/deploy
```

## Implementation

Follow backend Tasks B1 through B6 in:

```text
docs/superpowers/plans/2026-06-24-phase-38a-query-credential-metadata-management.md
```

Critical requirements:

- Use TDD per task block.
- Add routes:
  - `GET /query-targets/{id}/credential`
  - `PUT /query-targets/{id}/credential`
  - `DELETE /query-targets/{id}/credential`
- All credential routes require fresh bearer auth.
- PUT/DELETE require `admin` role.
- Request body must never accept `actorUserId`, `dsn`, `password`, `host`, or
  `port`.
- Metadata engine is derived from the selected target, never accepted from the
  request.
- `all_environments` requires explicit confirmation.
- `GET /query-targets` must mark a target ready only when the server can resolve
  the credential DSN and bind it to the target host/port.
- Metadata alone must never produce `availableActions.run=true`.
- If adding new `credentialState` values, update OpenAPI and document the
  frontend-facing string set.

## Verification

Run from the backend worktree:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Backend
```

If GitNexus reports a stale index, run:

```bash
npx gitnexus analyze
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Backend
```

Docker gates must run if Docker is available. Do not skip silently.

## Final Report

Include:

- worktree, branch, commit hashes
- files changed
- API paths and request/response shape
- auth/admin behavior
- readiness correction proof
- no-secret proof:
  - no DSN/password in request JSON
  - no DSN/password in response JSON
  - no DSN/password in metadata table
  - no DSN/password in audit rows
- backend verification matrix
- OpenAPI fuzz result
- GitNexus result and caveats
- final git status
- scope confirmation:
  - no frontend edits
  - no secret write API
  - no new query engines
  - no SQL guard relaxation
  - no push/tag/release/deploy
  - no AI co-author
