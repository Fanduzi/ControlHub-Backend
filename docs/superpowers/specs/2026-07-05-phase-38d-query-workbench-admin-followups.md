# Phase 38D Query Workbench Admin Follow-Ups Spec

## Background

Phase 38C made the Query Workbench substantially more usable:

- `SHOW TABLES`, `DESCRIBE`, and `EXPLAIN SELECT` are accepted by the backend;
- the query target picker is searchable;
- governance display is more compact;
- the Chinese credential-copy ICU `{ref}` error is fixed;
- the Query Workbench hydration mismatch is fixed.

Manual preview on 2026-07-05 still exposed unresolved issues:

1. `SHOW DATABASES` is rejected even though the product boundary is now
   "read-only SQL".
2. Directly opening `/settings/query-credentials` can show the restricted
   "Credential configuration is managed by administrators" view for an admin
   user because the page depends on `sessionStorage["controlhub.role"]`.
3. `/settings` does not expose a Query Credential management entry.
4. The Query Workbench target area is still split across the target picker, a
   separate search/filter strip, and an always-open target-facts block.

Phase 38D is a focused follow-up. It is not a full SQL IDE phase.

## Goals

- Treat safe metadata exploration as read-only SQL, including `SHOW DATABASES`.
- Keep backend enforcement parser-backed and fail closed for unsafe statements.
- Make credential administration reachable and truthful for admins on direct
  navigation and refresh.
- Add a visible Query Credential entry from `/settings`.
- Further compress the Query Workbench target-selection area so it does not
  dominate the worksheet.

## Non-Goals

- No DSN/password browser input.
- No credential secret write API.
- No new query engines.
- No export.
- No saved queries.
- No approval workflow.
- No full Monaco/CodeMirror migration.
- No SQL formatter.
- No multiple worksheet tabs.
- No CI workflow change.
- No tag, release, or deployment.

## Backend Boundary

Phase 38D keeps the endpoint unchanged:

```text
POST /query-targets/{id}/execute
```

Request and response shapes stay unchanged.

Allowed MySQL/TiDB statements after Phase 38D:

- single `SELECT`;
- `SHOW DATABASES`;
- `SHOW TABLES`;
- `SHOW TABLES FROM <database>`;
- `SHOW COLUMNS FROM <table>`;
- `SHOW COLUMNS FROM <database>.<table>`;
- `DESCRIBE <table>` / `DESC <table>`;
- `DESCRIBE <database>.<table>` / `DESC <database>.<table>`;
- `EXPLAIN SELECT ...`.

Still forbidden:

- multi-statement input;
- `INSERT`, `UPDATE`, `DELETE`, `REPLACE`;
- `CREATE`, `ALTER`, `DROP`, `TRUNCATE`;
- `CALL`, `SET`, `USE`;
- `BEGIN`, `COMMIT`, `ROLLBACK`;
- lock statements and locking clauses;
- `SHOW PROCESSLIST`;
- `SHOW GRANTS`;
- privilege/user/session administration;
- `INTO OUTFILE`, `INTO DUMPFILE`, `LOAD_FILE`;
- `SLEEP`, `BENCHMARK`, named-lock functions;
- user-variable assignment.

The SQL guard must inspect Vitess AST nodes and fields. String-prefix matching
is not acceptable.

## Frontend Admin Entry

`/settings/query-credentials` is an administrator-only management page, but it
must not show a false non-admin state just because the role is missing from
sessionStorage.

Required behavior:

- SSR and first client render remain hydration-safe.
- If `sessionStorage["controlhub.role"]` is present, use it.
- If role is missing but the auth cookie/session exists, recover the current
  role through the existing auth model or a shared frontend helper.
- If the role cannot be recovered, show a loading/unknown state briefly and then
  a clear restricted view.
- Direct URL load, refresh, and new tab should all show admin controls for an
  authenticated admin.
- Non-admin users must still never see credential edit controls.

If the existing backend has no `/me` endpoint, the frontend task must avoid
inventing a fake role. It can either reuse an existing login/session endpoint or
surface the backend gap and add a small backend follow-up only if explicitly
approved.

## Settings Entry

`/settings` must include a visible Query Credential management entry.

Minimum behavior:

- Add a card or row titled "Query credential settings" / "查询凭据设置".
- Explain that administrators configure metadata references only; DSN/password
  stay server-side.
- Link to `/settings/query-credentials`.
- Admin users see an action such as "Open credential settings".
- Non-admin users can either see a disabled/contact-admin state or no action,
  but they must not see edit controls.

The command palette/navigation may also include the entry if that matches the
frontend's existing IA, but the `/settings` page entry is required.

## Query Workbench Target Area

The target selector should become the primary target discovery and filtering
surface. The separate search/filter strip and expanded target facts should not
compete with it.

Required behavior:

- Keep one primary target picker.
- Picker search covers display name, resource name, engine, environment, host,
  port, cluster, and readiness.
- Engine/readiness/query-kind filters should move into the picker as compact
  filter chips or an "advanced filters" popover; they should not occupy a
  separate always-visible row.
- The selected target summary should show only compact chips such as `mysql`,
  `Development`, `ready`, and `127.0.0.1:13306`.
- Owner, language, cluster, and secondary details should move behind a details
  disclosure, tooltip, or popover.
- The right governance panel must not repeat the same engine/environment/host
  facts already shown near the target picker.

## Acceptance Criteria

Backend:

- `SHOW DATABASES` succeeds against the dedicated query MySQL fixture.
- `SHOW TABLES FROM query_e2e` succeeds.
- `SHOW COLUMNS FROM query_e2e.query_e2e_items` succeeds.
- `DESCRIBE query_e2e.query_e2e_items` succeeds.
- `SHOW PROCESSLIST` remains rejected.
- `SHOW GRANTS` remains rejected.
- `USE query_e2e` remains rejected.
- `UPDATE query_e2e_items SET name='x'` remains rejected.
- Multi-statement input remains rejected.
- Full backend gate matrix passes.

Frontend:

- Direct URL load of `/settings/query-credentials` as admin shows the admin
  management UI after role recovery.
- Refreshing `/settings/query-credentials` as admin still shows admin controls.
- Non-admin users still see the restricted management-by-admin view.
- `/settings` exposes a Query Credential settings entry.
- `/query` no longer shows a separate always-visible filter strip below the
  target picker.
- `/query` selected target facts are compact and do not repeat in the governance
  panel.
- Real query E2E passes against backend plus dedicated query MySQL fixture.

## Verification

Backend:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Frontend:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
npm run test:e2e -- --grep query
npm run test:e2e -- --grep "query credential"
```

## References

- Phase 38D decision:
  `docs/decisions/2026-07-05-phase-38d-query-workbench-admin-followups.md`
- Phase 38C spec:
  `docs/superpowers/specs/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization.md`
- Backend worker prompt:
  `docs/superpowers/prompts/backend-phase-38d-readonly-metadata-sql-worker.md`
- Frontend worker prompt:
  `docs/superpowers/prompts/frontend-phase-38d-query-admin-navigation-worker.md`
