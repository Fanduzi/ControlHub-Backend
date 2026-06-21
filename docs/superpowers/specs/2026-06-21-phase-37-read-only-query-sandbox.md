# Phase 37 Read-only Query Sandbox Design

## Background

Phase 36 made the query platform visible without execution:

```text
existing database resources -> GET /query-targets -> locked /query workbench shell
```

Phase 37 is the first execution milestone. It must prove that ControlHub can run
a narrowly-scoped read-only query safely before expanding to more engines or
governance workflows.

## Goal

Add a backend-enforced, audited, read-only SQL sandbox for one engine family:

```text
MySQL / TiDB SELECT only
```

The frontend may enable Run only when the backend says the selected target is
executable. Backend enforcement is mandatory; frontend state is only a user
experience layer.

## Non-goals

- Do not support PostgreSQL, ClickHouse, Redis, or MongoDB execution.
- Do not support writes: `INSERT`, `UPDATE`, `DELETE`, `REPLACE`, DDL, `CALL`,
  `SET`, `USE`, `LOAD`, administrative statements, or transaction control.
- Do not support multi-statement input.
- Do not support export.
- Do not support saved queries or shared worksheets.
- Do not support approval workflows or permission requests.
- Do not add AI query assistance.
- Do not store plaintext credentials in the database or return credentials to
  the frontend.
- Do not rely on frontend disabled buttons for safety.

## Engine choice

Phase 37 uses MySQL/TiDB first.

Reasons:

- The backend already uses `github.com/go-sql-driver/mysql`.
- Integration tests already use Testcontainers MySQL.
- MySQL/TiDB `SELECT` is enough to validate the difficult platform concerns:
  credential resolution, SQL guard, timeout, max rows, audit, history, and
  frontend result rendering.
- ClickHouse, PostgreSQL, Redis, and MongoDB require different drivers and guard
  models. They belong in Phase 38 after the sandbox mechanics are proven.

## User flow

1. User opens `/query`.
2. Frontend loads `GET /query-targets`.
3. Backend marks only explicitly configured MySQL/TiDB targets as executable.
4. User selects an executable target.
5. User submits one SQL statement.
6. Backend validates the target, credential, statement, limits, and timeout.
7. Backend executes under read-only controls.
8. Backend records an audit/history row.
9. Frontend displays rows, columns, duration, row count, truncation state, and
   execution status.

## Backend contract

### Authenticated actor prerequisite

Phase 37 execution endpoints require an authenticated actor. Current auth issues
tokens through `POST /auth/login`; Phase 37 must add backend token verification
for query execution routes before any query can run.

Rules:

- reject missing or invalid `Authorization: Bearer <token>` with 401
- derive `actorUserId` from the verified token on the server
- never accept `actorUserId` from request JSON
- write query history and audit rows with the verified actor ID
- keep non-execution read endpoints unchanged unless a separate auth phase
  explicitly broadens authentication

If authenticated actor extraction is not implemented, the execute endpoint must
remain unavailable.

#### Auth must be closed across OpenAPI, handler tests, and fuzz

Bearer auth on execute/history must be expressed end-to-end, not only in the
handler:

- OpenAPI: declare a `401` response on `POST /query-targets/{id}/execute` and
  `GET /query-targets/{id}/executions`. Declare a Bearer security scheme (or
  otherwise state explicitly that these paths are protected by Bearer auth) so
  the published contract matches runtime behavior.
- handler tests: cover missing `Authorization` header -> `401` and
  invalid/malformed bearer -> `401`, in addition to the happy path.
- Schemathesis / OpenAPI fuzz: pick one explicit strategy and document it —
  either supply a valid test bearer token to the fuzz harness for protected
  endpoints, or declare the unauthenticated `401` as a documented response so a
  `401` from fuzz is a conformance pass and never an unclassified failure.
- Cross-repo E2E: the frontend must use the existing authenticated API
  client/session. Never place `actorUserId` in the request body or query string.

#### Query execution token freshness

The existing auth token embeds an `issuedAt` Unix timestamp
(`<userID>:<role>:<issuedAt>:<signature>`), so token age can be checked without
changing login. Query execution is a higher-risk surface than the existing
read/list routes, so Phase 37 must bound token freshness specifically for query
execution routes:

- The query execution middleware must reject tokens older than a bounded TTL.
- Add a `QueryExecutionTokenMaxAge` configuration value. Suggested default `8h`
  or shorter.
- This applies only to query execution/history routes. Existing non-query
  read/list route authentication behavior must not change.

Required middleware/service tests:

- token issued within TTL is accepted
- token older than TTL is rejected with `401`
- malformed token and bad-signature token are rejected with `401`

### Query target readiness update

Phase 36 returned `credential_required` for complete targets because no
credential metadata existed. Phase 37 adds backend-owned execution readiness.

`GET /query-targets` should mark a target executable only when all are true:

- engine is `mysql` or `tidb`
- connection metadata is complete
- target has an enabled credential reference
- backend can resolve that credential reference to a DSN
- policy allows the target's environment

When executable:

```json
{
  "readiness": "ready",
  "governance": {
    "executionEnabled": true,
    "credentialState": "configured_readonly_credential",
    "auditRequired": true,
    "safetyState": "readonly_sandbox_enabled"
  },
  "availableActions": {
    "run": true,
    "explain": false,
    "export": false,
    "saveSheet": false,
    "requestAccess": false
  }
}
```

All other targets must remain visible and non-executable.

#### New `safetyState` value requires a full enum chain

The ready example above returns
`governance.safetyState = "readonly_sandbox_enabled"`. That value does not exist
in the current `QueryTargetSafetyState` enum in `internal/model/query_target.go`,
which today only covers the Phase 36 non-executable states (`credential_missing`,
`execution_disabled`, `unsupported_engine`, `connection_incomplete`).

Phase 37 must not let the service emit an undeclared string. The new value must
be added across the whole contract before any ready target is returned:

Backend (required):

- Go enum: add
  `SafetyStateReadonlySandboxEnabled QueryTargetSafetyState = "readonly_sandbox_enabled"`
  to `internal/model/query_target.go`, with its dictionary entry and `Validate()`
  coverage, so the value is a known state rather than a free string.
- OpenAPI: add `"readonly_sandbox_enabled"` to the `QueryTargetSafetyState`
  enum/schema in `internal/openapi/openapi.yaml` and keep the `ready` example
  consistent with it.
- service tests: assert a ready target reports
  `safetyState = readonly_sandbox_enabled`, and that every other readiness state
  still reports its existing non-executable safety state.
- handler/integration tests: assert `GET /query-targets` serializes the new enum
  for a ready target and that no unknown safety strings leak.

Frontend (required):

- TypeScript: add `readonly_sandbox_enabled` to the `QueryTargetSafetyState`
  union in `types/query-target.ts`.
- i18n: add a human label for `readonly_sandbox_enabled`; components must render
  the label, never the raw enum.
- component tests: assert the workbench shows the new label for a ready target
  and never prints the raw enum string.

### Execute endpoint

Add:

```text
POST /query-targets/{id}/execute
```

Request:

```json
{
  "statement": "select id, name from resources where resource_type = 'service' limit 20",
  "maxRows": 100
}
```

Response:

```json
{
  "executionId": 1001,
  "status": "success",
  "targetResourceId": 22,
  "engine": "mysql",
  "columns": [
    { "name": "id", "databaseType": "BIGINT", "nullable": false },
    { "name": "name", "databaseType": "VARCHAR", "nullable": false }
  ],
  "rows": [
    [1, "orders-api"]
  ],
  "rowCount": 1,
  "truncated": false,
  "durationMs": 18,
  "limitApplied": 100,
  "executedAt": "2026-06-21T08:30:00Z"
}
```

Failure response uses existing JSON error style:

```json
{
  "error": "validation_failed",
  "message": "only a single SELECT statement is allowed"
}
```

### History endpoint

Add:

```text
GET /query-targets/{id}/executions?page=1&pageSize=20
```

Returns execution metadata only, not full result rows:

```json
{
  "items": [
    {
      "id": 1001,
      "targetResourceId": 22,
      "actorUserId": 1,
      "statementDigest": "select id, name from resources where resource_type = ? limit ?",
      "status": "success",
      "rowCount": 1,
      "durationMs": 18,
      "errorCode": "",
      "createdAt": "2026-06-21T08:30:00Z"
    }
  ],
  "pageInfo": {
    "page": 1,
    "pageSize": 20,
    "totalItems": 1,
    "totalPages": 1
  }
}
```

## Credential model

Phase 37 needs credential metadata but must not store plaintext secrets in
ControlHub database tables.

Add metadata table:

```text
query_target_credentials
  id
  resource_id
  engine
  credential_ref
  enabled
  environment_policy
  created_at
  updated_at
```

`credential_ref` is an opaque key. The server resolves it from process
environment or a later secret manager. Example local format:

```text
CONTROLHUB_QUERY_CREDENTIAL_<credential_ref>=user:password@tcp(host:port)/database?timeout=2s&readTimeout=5s
```

Rules:

- Never return the DSN or password through any API.
- Never store plaintext DSN/password in MySQL.
- Never log the DSN.
- Fail closed when the credential reference cannot be resolved.
- Credentials must be documented as read-only database users.

### `environment_policy` semantics

`environment_policy` is not a free string. Phase 37 must define and enforce an
enum:

- `disabled` — the credential is never usable; the target stays locked.
- `non_prod_only` — execution allowed only for non-production environments. This
  is the default.
- `all_environments` — execution allowed for any environment, including
  production.

Rules:

- Production environments are not executable by default. A production target is
  only executable when its credential policy is explicitly `all_environments`.
- Unknown or empty policy values fail closed: the target is treated as
  `disabled`/locked, never as `all_environments`.

Required tests:

- non-production target + enabled credential + `non_prod_only` -> ready
- production target + `non_prod_only` -> not ready / disabled
- production target + `all_environments` -> ready
- `disabled` credential/policy -> locked
- unknown/empty policy -> fail closed (locked)

### `credential_ref` format

`<credential_ref>` in `CONTROLHUB_QUERY_CREDENTIAL_<credential_ref>` is not free
text. Phase 37 must constrain it:

- `credential_ref` matches `[A-Z0-9_]+` only.
- length is bounded (for example, max 64 characters), consistent with the
  `credential_ref VARCHAR(128)` column.
- an invalid `credential_ref` is rejected at metadata write/seed time; if a bad
  value is already stored, resolution must fail closed (never perform an env
  lookup with an unvalidated key).
- the resolved DSN/password is never returned through any API and never written
  to logs.

Even when Phase 37 exposes credential metadata only through migration/seed (no
credential write API), seed data and test fixtures must obey this rule.

## SQL guard

Use a real parser for MySQL/TiDB syntax. The implementation should add a parser
dependency rather than relying only on string checks.

Recommended parser:

```text
vitess.io/vitess/go/vt/sqlparser
```

Guard rules:

- trim whitespace and comments
- reject empty statement
- reject semicolon-separated or parser-detected multi-statements
- parse exactly one statement
- allow only `SELECT`
- reject `SELECT ... INTO OUTFILE`
- reject locking clauses such as `FOR UPDATE` and `LOCK IN SHARE MODE`
- reject comments or versioned comments that hide non-read statements
- reject any parser node that is not an allowed select tree
- enforce `LIMIT`
- cap requested max rows at backend default and hard maximum

#### SELECT can still have side effects or consume resources

"Allow only SELECT" is not sufficient. A MySQL/TiDB `SELECT` can invoke
dangerous functions, hold named locks, write files, assign variables, or burn
resources. The guard must additionally reject:

- `SLEEP(...)`
- `BENCHMARK(...)`
- named lock functions: `GET_LOCK(...)`, `RELEASE_LOCK(...)`, `IS_FREE_LOCK(...)`,
  `IS_USED_LOCK(...)`
- `LOAD_FILE(...)`
- user-variable assignment patterns the parser exposes (for example
  `@var := ...` or `SELECT ... INTO @var`)
- `SELECT ... INTO OUTFILE` and `SELECT ... INTO DUMPFILE`
- locking clauses `FOR UPDATE` and `LOCK IN SHARE MODE`

These rejections must be implemented by walking the parsed statement, not by
string matching. If the chosen parser cannot reliably enumerate function calls
and the clauses above, Phase 37 must first run a parser spike/verification that
proves each rejection is reachable through the AST before claiming the rule is
done. String blacklists are not an acceptable guard mechanism and must not be
used to pretend any of these rules are implemented.

Guard tests must cover each rejected function and clause above with a concrete
statement and assert the rejection reason.

Default limits:

```text
Default max rows: 100
Hard max rows: 500
Timeout: 5 seconds
Production timeout: 3 seconds
Production hard max rows: 100
```

If a statement has no `LIMIT`, the backend should append one to the guarded
statement it executes. If a statement has a larger `LIMIT`, the backend should
replace it with the effective max.

The response should include `limitApplied`.

## Execution controls

Use defense in depth:

- resolve only enabled target credentials
- connect using the target-specific read-only credential
- execute with `context.WithTimeout`
- use `database/sql` connection scoped to the request
- use a read-only transaction where supported:

```go
db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
```

- close rows promptly
- scan at most `limitApplied + 1` rows so truncation can be detected
- return at most `limitApplied` rows
- convert database values to JSON-safe values
- reject result sets with too many columns
- never execute additional statements before or after user SQL except backend
  owned connection/transaction setup

Column and payload caps:

```text
Max columns: 100
Max cell bytes: 8192
Max response bytes: 1 MiB
```

If caps are hit, return a controlled validation or truncation response, not a
500.

## Audit and history

Every execution attempt must be recorded.

Add table:

```text
query_executions
  id
  target_resource_id
  actor_user_id
  engine
  statement_digest
  statement_preview
  status
  row_count
  duration_ms
  error_code
  error_message
  created_at
```

Store:

- statement digest or normalized statement
- short preview with length cap
- status: `success`, `rejected`, `failed`, `timeout`
- duration
- row count
- error code/message without leaking secrets

Do not store full result rows in Phase 37.

Also write an audit event:

```text
event_type = query.executed
result = success | validation_failed | failed | timeout
target_resource_id = query target resource id
```

If existing `audit_events` cannot hold enough details, keep the detailed query
metadata in `query_executions` and use `audit_events` for the general event
stream.

## Frontend behavior

Frontend Phase 37 should stay thin:

- enable Run only when `availableActions.run` is true
- submit to `POST /query-targets/{id}/execute`
- render rows/columns/duration/truncation
- render validation errors from backend
- render query history from `GET /query-targets/{id}/executions`
- keep Export disabled
- keep Save Sheet disabled
- do not parse SQL client-side for enforcement

Client-side syntax hints are allowed, but all enforcement must remain backend
owned.

### Frontend auth and session

- Execution and history services must use the existing authenticated API
  client/session. No query service accepts `actorUserId`.
- Tests must assert the request body and query string never include
  `actorUserId`.
- If the auth token is missing or expired, the UI must show a controlled auth
  error and must not retry endlessly or silently resubmit.

## Error handling

Use controlled error categories:

| HTTP | Error | Meaning |
|---|---|---|
| 400 | `validation_failed` | bad request, blocked statement, bad maxRows |
| 403 | `query_not_allowed` | target not enabled or environment disallows execution |
| 404 | `query_target_not_found` | resource does not exist or is not a query target |
| 408 | `query_timeout` | execution exceeded timeout |
| 502 | `query_backend_error` | target database rejected or connection failed |
| 500 | `internal_error` | unexpected ControlHub failure only |

No guard or target-database validation issue should become a 500.

## Testing strategy

Backend unit tests:

- SQL guard allows simple SELECT.
- SQL guard rejects writes, DDL, CALL, SET, USE, multi-statement, empty input.
- SQL guard applies LIMIT when missing.
- SQL guard caps LIMIT when too high.
- target policy rejects unsupported engines and missing credentials.
- result mapper handles strings, numbers, bools, nulls, times, bytes.

Backend integration tests:

- executable MySQL target with read-only test credential can run SELECT.
- INSERT/UPDATE/DELETE/DDL return 400 and do not mutate data.
- multi-statement returns 400.
- timeout returns controlled timeout response.
- row limit and truncation are enforced.
- query_executions history row is written for success and rejection.
- audit event is written for success and rejection.

OpenAPI/fuzz:

- add execute and history schemas.
- fuzz must not find 5xx on blocked statements.
- examples should include one accepted SELECT and one rejected write.

Frontend tests:

- Run remains disabled for non-ready targets.
- Run becomes enabled for backend-ready targets.
- execution success renders table rows and metadata.
- validation failure renders backend message.
- query history tab loads metadata only.
- Export and Save remain disabled.

Cross-repo E2E:

- start backend with one seeded executable MySQL/TiDB sandbox target.
- visit `/query`.
- select executable target.
- run `select 1 as value`.
- assert result table shows `1`.
- run or type an UPDATE and assert it is blocked.
- assert query history shows both attempts.
- assert no export control is enabled.

## Success criteria

Phase 37 is complete when:

- only MySQL/TiDB SELECT execution is available
- execution requires configured backend credential reference
- backend guard rejects unsafe statements
- timeout, max rows, max columns, and payload caps are enforced
- every attempt writes query history and audit event
- frontend displays results and failures from backend
- cross-repo E2E proves one successful SELECT and one blocked write
- no credentials or result exports are exposed

## Deferred work

- ClickHouse support
- PostgreSQL support
- Redis read command support
- MongoDB read query support
- saved queries
- export
- approval workflow
- masking
- kill query
- connection pool management
- secret manager integration beyond environment-backed credential refs
