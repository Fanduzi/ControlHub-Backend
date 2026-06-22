# Phase 37F Query Execute UI And Dev Credentials Design

## Background

Phase 36 made query targets visible in the frontend. Phase 37 added the backend
read-only execution API for MySQL/TiDB SELECT, but local viewing still shows the
Query Workbench locked because two pieces are missing:

- no local/dev read-only credential metadata exists for any target
- the frontend shell does not call the execute or history APIs

Phase 37F connects these pieces in the smallest safe way. The goal is not a
complete query platform. The goal is to let a user open `/query`, choose one
ready dev target, run one guarded SELECT, and see rows/history while all unsafe
targets remain locked.

## Goal

Deliver an end-to-end local/dev query execution path:

```text
ready MySQL/TiDB target -> Run SELECT in /query -> backend execute API -> result grid + history
```

The backend remains the authority. The frontend may enable Run only when
`availableActions.run=true`, and the backend must still reject anything that
violates auth, credential binding, policy, SQL guard, timeout, or row limits.

## Non-goals

- Do not add PostgreSQL, ClickHouse, Redis, or MongoDB execution.
- Do not add writes, exports, saved queries, approvals, or AI query assistance.
- Do not add a credential write API.
- Do not store plaintext DSNs or passwords in ControlHub tables.
- Do not make production targets executable by default.
- Do not loosen Phase 37 backend guards.
- Do not fake query execution in the frontend.

## Architecture

Phase 37F has two sequential parts.

1. Backend dev credential seed:
   - Add a local/dev-only way to seed `query_target_credentials`.
   - The metadata row contains only `resource_id`, `engine`, `credential_ref`,
     `enabled`, and `environment_policy`.
   - The actual DSN stays in an environment variable:
     `CONTROLHUB_QUERY_CREDENTIAL_<REF>`.
   - The DSN must bind to the selected target host/port, using the Phase 37
     validation already in the service.

2. Frontend execute wiring:
   - Add typed `POST /query-targets/{id}/execute` and
     `GET /query-targets/{id}/executions` services.
   - Enable Run only for the selected target when the backend says Run is
     available.
   - Render results, truncation, duration, limit, controlled errors, and history.
   - Keep locked UI for all non-ready targets.

## Backend Dev Credential Seed

### Required behavior

The local/dev seed must be explicit and fail closed:

- It runs only when an explicit local/dev flag or command is used.
- It never runs automatically in production.
- It never writes a DSN or password to the database.
- It validates the selected target is MySQL/TiDB and has complete host/port.
- It creates or updates one metadata row in `query_target_credentials`.
- It chooses `environment_policy = non_prod_only` by default.
- It allows `all_environments` only with an explicit override and only for
  local/dev evidence, not as a production default.

Recommended shape:

```text
make seed-query-dev-credential
```

Environment inputs:

```text
QUERY_DEV_TARGET_RESOURCE_ID=<resource id>
QUERY_DEV_CREDENTIAL_REF=LOCAL_MYSQL_RO
CONTROLHUB_QUERY_CREDENTIAL_LOCAL_MYSQL_RO=<readonly mysql dsn>
QUERY_DEV_ENVIRONMENT_POLICY=non_prod_only
```

The seed command should be idempotent: running it again updates the metadata row
for the same resource rather than creating duplicates.

### Local target expectation

For local viewing, one target can point at the existing local MySQL instance if
that target's connection profile host/port matches the DSN. If no existing
target matches the local DSN, the seed command should fail with a clear message
instead of silently creating a misleading ready target.

Creating synthetic resources or altering resource profiles is out of scope for
Phase 37F unless the worker proves the existing seed data cannot support a ready
target. If that happens, stop and report the gap before adding more fixture
behavior.

## Frontend Execute UX

### Target states

The UI must distinguish three states:

1. Locked target:
   - `availableActions.run=false`
   - Run button disabled
   - editor may be readable/editable as a draft, but no execution action is
     possible
   - governance panel explains the missing requirement

2. Ready target:
   - `availableActions.run=true`
   - Run button enabled
   - user can submit one statement
   - result panel shows execution response

3. Error state:
   - controlled API errors are rendered in the result panel, not as raw JSON
   - auth errors tell the user to log in again
   - policy/guard errors preserve the backend message but do not expose DSNs

### Execute request

Request:

```json
{
  "statement": "select 1",
  "maxRows": 100
}
```

Rules:

- Do not send `actorUserId`.
- Do not send credentials.
- Use the existing authenticated API client/session.
- Disable Run while a request is in flight.
- Do not retry automatically on query execution errors.
- Preserve the statement text after an error so the user can edit it.

### Execute response rendering

Display:

- status
- columns
- rows
- row count
- truncation state
- limit applied
- duration
- execution id
- executed at

`null` cells must render as an explicit empty/null value, not as `0`, empty
string, or `undefined`.

### History

After a successful execution or a controlled rejection, refresh the history tab
for the selected target:

```text
GET /query-targets/{id}/executions?page=1&pageSize=20
```

History displays metadata only:

- execution id
- status
- statement preview
- row count
- duration
- error code/message
- created at

History must never display DSNs or credentials.

## Security And Governance Boundary

The backend remains the enforcement layer:

- frontend disabled buttons are only UX
- backend rejects unauthorized calls
- backend rejects non-ready targets
- backend rejects unsafe SQL
- backend enforces row/time limits
- backend records history/audit
- backend binds credentials to target host/port

The frontend must not infer readiness from engine alone. It must consume
`readiness`, `governance.executionEnabled`, and `availableActions.run`.

## Testing Strategy

Backend:

- Unit tests for the dev credential seed parser/validator.
- Repository tests for idempotent metadata upsert.
- Integration test that seeds one ready target and verifies `GET /query-targets`
  reports `ready` and `availableActions.run=true`.
- Integration test that a mismatched DSN remains rejected by Phase 37 binding.
- Full backend gates: `go test`, `go vet`, `go build`, OpenAPI validate,
  integration, OpenAPI fuzz.

Frontend:

- Service tests for execute/history request shape and auth client usage.
- Component tests for locked, ready, loading, success, controlled error, and
  null-cell rendering.
- E2E against real backend:
  - locked target cannot run
  - ready dev target can run `select 1`
  - unsafe statement shows controlled rejection
  - history updates after execution

## Success Criteria

- A local developer can configure one read-only DSN and seed one ready target.
- `/query` shows that target as ready.
- Run executes `select 1` and displays a result.
- Unsafe statements remain blocked by the backend.
- Locked targets remain locked.
- Query history updates.
- No credentials or DSNs appear in UI, API responses, logs, or history rows.
- Cross-repo E2E passes.

