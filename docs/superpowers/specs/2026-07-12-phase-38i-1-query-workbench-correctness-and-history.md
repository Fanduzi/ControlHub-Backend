# Phase 38I.1 Query Workbench Correctness And History

## Status

**Implementation specification.** Phase 38I shipped its schema-intelligence
foundation, but two production-visible P1 defects remain: Object Explorer can
crash on a valid schema response, and Query History reports a false empty state
until a new query happens to refresh it. This package fixes those defects and
closes the related cross-user history-preview exposure before Phase 38J work
begins.

## Objective

Make Object Explorer and Query History trustworthy at first use:

1. A successful schema detail response never contains `null` where the public
   contract promises an array, and the frontend never crashes if an older or
   malformed response does.
2. A worksheet loads its existing execution history when the user first opens
   History, without reintroducing mount-time worksheet/history races.
3. Execution history is readable and privacy-safe: ordinary operators see
   their own records, admins see the target's shared operations history, and
   the UI identifies actors and execution outcomes clearly.

## Evidence And Root Causes

### P1-A: Object Explorer crash

`ObjectDetailResponse` declares `columns`, `indexes`, and `foreignKeys` as
required arrays in OpenAPI. `toModelObjectDetail` builds Go slices through
conditional `append`, so empty slices serialize as JSON `null`. Nested index
and foreign-key column lists have the same risk. The frontend assumes arrays
and calls `.length`, causing the observed crash for ordinary tables/views with
no matching index or foreign-key metadata.

This is a backend contract violation and a frontend resilience defect. Both
must be fixed.

### P1-B: Query History false empty

The initial worksheet deliberately avoids a mount-time history fetch to prevent
the former cross-worksheet race. History is currently refreshed only after a
query run or a navigator-driven target switch. Clicking History changes the
visible tab but does not fetch. The empty initial array is therefore rendered
as “No executions yet”; after any run, the refresh returns older records.

### P1-C: Cross-user statement-preview exposure

The history route currently requires a fresh bearer token but passes no actor
or role into its service/repository query. It filters only by target ID and
returns every execution's statement preview to any authenticated user who knows
that target ID. `query_executions.actor_user_id` and `users.display_name`
already exist, so a migration is not required.

## Product Decisions

These decisions are fixed for 38I.1; do not reopen them during implementation.

| Topic | Decision |
| --- | --- |
| Non-admin history visibility | Only records whose `actor_user_id` equals the authenticated actor. |
| Admin history visibility | All records for the requested target. |
| Actor UI identity | `actor.displayName` only. Do not display email or raw numeric user IDs in the primary table. |
| Missing/deleted user | API returns `actor.displayName: "Unknown user"`; the UI renders it as-is. |
| Target validation | A nonexistent target returns controlled `404`; do not return a misleading empty list. |
| Readiness requirement | History is not blocked merely because a formerly executable target is now locked or unresolved. It is an audit record, not query execution. |
| Detail errors | Failed object-detail loading is visible per object with Retry; it is never silently rendered as empty metadata. |

ControlHub has no per-target user ACL model. This package does not invent one.
The actor/role scope above is the smallest safe policy compatible with the
current auth model.

## Backend Contract

### Object details

For every successful `GET /query-targets/{id}/schema/object-details` response:

```text
columns         is always an array
indexes         is always an array
foreignKeys     is always an array
indexes[*].columns is always an array
foreignKeys[*].columns is always an array
foreignKeys[*].referencedColumns is always an array
```

Empty collections serialize as `[]`, never `null`. Preserve existing bounded,
governed metadata reads, cache behavior, audit behavior, error mapping, and
no-secret rules. Do not add DDL definition, SHOW CREATE, right-click actions,
new schema engines, or information-schema browser features in this hotfix.

### History list authorization and shape

The current route remains the verified route:

```text
GET /query-targets/{id}/executions
```

It continues to require a fresh bearer token. The handler must obtain actor ID
and role from the existing authenticated request context, validate that the
target exists, then pass an explicit history visibility scope to the service
and repository:

```text
admin     -> target_resource_id = ?
non-admin -> target_resource_id = ? AND actor_user_id = ?
```

The repository must use a parameterized `LEFT JOIN users` (or equivalent
bounded batched lookup) and return a stable actor projection. The public item
shape becomes:

```json
{
  "id": 1001,
  "targetResourceId": 22,
  "actor": { "displayName": "ControlHub Admin" },
  "engine": "mysql",
  "statementDigest": "select id from resources limit ?",
  "statementPreview": "select id from resources limit 20",
  "status": "success",
  "rowCount": 1,
  "durationMs": 18,
  "errorCode": "",
  "errorMessage": "",
  "createdAt": "2026-07-12T08:30:00Z"
}
```

Do not expose email, DSN, password, database username, raw driver errors,
result rows, or an `actorUserId` request parameter. If `actorUserId` exists in
the response today, remove it from the public frontend type and UI rather than
making it a primary identity. Update OpenAPI and all backend/frontend wire
contracts together.

The list remains paginated with the shared `pageInfo` contract. 38I.1 must use
the first page correctly; it must not fabricate client-side totals or fetch all
history records.

## Frontend Behavior

### Object Explorer

Normalize object detail responses at the service/store boundary so every
top-level and nested collection is an empty array when the wire value is
missing or `null`. Rendering must still use safe collection handling.

For each object detail request, distinguish `idle`, `loading`, `ready`, and
`error`. On error show a compact, localized error state and an accessible Retry
action for that object. A valid response with empty arrays is a ready state
showing zero metadata, not an error.

### Worksheet history state machine

History is per worksheet and per target. Replace the ambiguous
`historyLoading + []` state with explicit state equivalent to:

```text
idle -> loading -> ready
                  -> error -> loading (Retry or next History open)
```

Required behavior:

1. Initial page mount performs no history request.
2. On the first History-tab activation for an executable worksheet in `idle`,
   fetch page 1 for that worksheet's bound target.
3. Show loading while pending, empty only after a successful empty response,
   and a localized error with Retry on failure.
4. After a successful query run, refresh the originating worksheet's history.
5. A new worksheet or target retarget invalidates that worksheet's history to
   `idle`; it must never show another target's rows.
6. Stale responses must be rejected using a history-specific generation/token
   plus current worksheet ID and target ID. Do not couple an independent history
   request to the execution request ID.
7. Switching worksheets preserves each worksheet's own history state and rows.

### History presentation

The History table must present, in a responsive and localized layout:

| Field | Presentation |
| --- | --- |
| Executed at | Relative time with an absolute-time tooltip or accessible label. |
| Actor | `actor.displayName`; no raw numeric ID. |
| Status | Existing semantic status treatment. |
| Statement | Monospace, safely truncated preview. |
| Rows | Formatted count. |
| Duration | Formatted duration with units, not an unexplained raw number. |
| Error | Safe code/message only when present; no raw database/driver secret content. |

The execution ID may remain in an accessible detail/tooltip if useful but is
not a primary table column. Preserve mobile readability without horizontal
overflow or hiding the state/error explanation.

## Scope Boundaries

- No SQL guard behavior change.
- No new query engine or browser database connection.
- No DSN, password, database username, secret value, or raw driver error in
  browser state, request, response, display, cache keys, audit, errors, or logs.
- No `actorUserId` request field.
- No credential edit controls in `/query`.
- No schema persistence migration or browser persistence.
- No DDL definition, SHOW CREATE, arbitrary information-schema browser,
  context menu, right-click action, ER diagram, Visual Explain, saved queries,
  export, approval/JIT, notebook, AI, MCP, visual builder, or editable grid.
- No global credential aggregate work, CI workflow changes, tag, release, or
  deployment.

## Required Tests And Acceptance Criteria

### Backend

1. JSON serialization tests prove empty top-level and nested object-detail
   collections are `[]`, never `null`.
2. Integration/API test uses an object with no secondary index and no foreign
   key and validates the OpenAPI-compatible response shape.
3. History API tests prove admin receives multiple actors for a target;
   non-admin receives only its own records; unknown target is `404`; stale or
   absent bearer remains `401`.
4. Tests prove actor projection returns display name and deleted-user fallback
   without email or secrets.
5. Pagination and deterministic ordering remain intact.

### Frontend

1. A wire detail containing `null` top-level/nested arrays renders zero counts
   and never throws.
2. A failed object-detail request shows error + Retry and reaches ready state
   after retry.
3. Initial worksheet mount makes no history request.
4. First History click fetches the initial target's records and displays prior
   rows without executing a new query.
5. Empty, loading, error/retry, target change, worksheet switching, concurrent
   history fetches, and post-run refresh preserve per-worksheet isolation.
6. History displays actor, formatted duration, row count, status, statement,
   and safe error content; raw actor IDs are not primary UI text.
7. Locked/non-executable worksheet behavior remains truthful and does not fetch
   schema/history through a forbidden path.

### Real end-to-end

Run against the real backend and dedicated query MySQL fixture. Seed at least
two execution records before the browser opens History, including records from
different actors when the existing test harness can authenticate both roles.
Prove the initial History click shows preexisting records, the schema object
with empty metadata does not crash, and existing query/credential flows remain
green. Report exact pass/fail/skip counts.

## Completion Gate

38I.1 is not complete until backend and frontend contract changes are committed
in separate focused commits, both worktrees are clean, all required gates pass,
and real E2E has passed against the final committed heads. A blocked live E2E
must be reported as blocked, not converted into a completion claim.
