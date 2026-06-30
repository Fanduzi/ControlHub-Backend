# Phase 38C Query Workbench Read-Only SQL And UX Stabilization Design

## Background

Phase 37 delivered backend-enforced MySQL/TiDB query execution with a conservative
single-`SELECT` SQL guard. Phase 37H added a dedicated Docker MySQL fixture so
the frontend can exercise a real ready target. Phase 38A/38B added credential
metadata administration and a real admin operations page.

Manual preview on 2026-06-30 proved the local execution path:

- backend on `:8080`;
- frontend on `:3100`;
- dedicated query database on `127.0.0.1:13306`;
- ready target `616 / Local MySQL Query Dev`;
- query execution available through the UI.

The same preview exposed two immediate bugs and several Query Workbench UX
problems:

- credential settings detail panel crashes on ICU `{ref}` placeholders;
- query governance panel has a hydration mismatch around the admin settings link;
- target selection is hard to search and read;
- the page looks locked even when a ready target exists;
- governance and target facts consume too much page real estate;
- `SHOW TABLES` is rejected because the backend says "SELECT only";
- worksheet editing lacks IDE affordances.

Phase 38C fixes the immediate bugs, corrects the read-only SQL boundary, and
tightens the Query Workbench information architecture enough for review. It does
not attempt a full database IDE rebuild.

## Goal

Make the current Query Workbench usable and truthful:

- administrators can open credential settings without console errors;
- the query page hydrates without mismatch;
- users can find the ready target through a searchable selector;
- governance and target facts are compact and do not dominate the worksheet;
- the product boundary is communicated as "read-only SQL";
- backend execution accepts a small, safe set of read-only metadata statements;
- writes, DDL, session mutation, locks, file export, and side-effect statements
  remain blocked by the backend.

## Non-Goals

- No DSN/password browser input.
- No secret manager UI.
- No credential write secret API.
- No new query engines.
- No ClickHouse, PostgreSQL, Redis, or MongoDB execution.
- No export.
- No saved queries.
- No approval workflow.
- No persistent worksheet storage.
- No full Monaco/CodeMirror migration in Phase 38C.
- No GitHub Actions workflow change.
- No tag, release, or deployment.

## Product Boundary

Phase 38C changes the user-facing execution boundary from:

```text
Only SELECT statements are allowed.
```

to:

```text
Only read-only SQL statements are allowed.
```

The backend allow-list is intentionally small.

Allowed in Phase 38C:

- single `SELECT`;
- `SHOW TABLES`;
- `SHOW COLUMNS FROM <table>`;
- `DESCRIBE <table>` / `DESC <table>`;
- `EXPLAIN SELECT ...`.

Still forbidden:

- multi-statement input;
- `INSERT`, `UPDATE`, `DELETE`, `REPLACE`;
- `CREATE`, `ALTER`, `DROP`, `TRUNCATE`;
- `CALL`, `SET`, `USE`;
- `BEGIN`, `COMMIT`, `ROLLBACK`;
- lock statements and locking clauses;
- privilege/user statements;
- `SHOW PROCESSLIST`;
- `SHOW DATABASES` unless a future phase explicitly approves it;
- file/export paths: `INTO OUTFILE`, `INTO DUMPFILE`, `LOAD_FILE`;
- side-effect or reliability-risk functions such as `SLEEP`, `BENCHMARK`,
  `GET_LOCK`, `RELEASE_LOCK`, `IS_FREE_LOCK`, and `IS_USED_LOCK`;
- user-variable assignment.

The SQL guard must remain parser-backed. String prefix matching is not enough.
If the current parser cannot represent a statement class, the backend task must
run a parser spike and document the result before implementation.

## Backend Contract Changes

The query execution endpoint stays the same:

```text
POST /query-targets/{id}/execute
```

Request/response shapes stay unchanged. The accepted statement set widens.

OpenAPI and error copy must change from "SELECT statement" to "read-only SQL
statement" where appropriate.

History and audit semantics do not change:

- every accepted statement records a successful attempt;
- every rejected statement records a rejected attempt;
- every backend failure records a failed attempt;
- no unaudited success is allowed.

## Frontend Stabilization

### Immediate Bugs

Credential settings detail panel:

- remove ICU `{ref}` placeholders from static copy, or pass a safe literal value;
- preferred copy uses a literal example:
  `CONTROLHUB_QUERY_CREDENTIAL_your-ref`;
- no DSN/password or real credential ref is rendered.

Query governance panel:

- do not read `window` or `sessionStorage` during render;
- initialize admin state as `null`;
- render stable SSR/first-client markup;
- read `controlhub.role` in `useEffect`;
- show the admin settings link only after the client confirms admin role.

### Query Target Selector

The target selector should become a searchable target picker:

- search by display name, resource name, engine, environment, host, and cluster;
- show readable rows with name, engine, environment, host:port, and status;
- highlight or group ready targets;
- make `Local MySQL Query Dev` easy to find in local preview;
- avoid rendering truncated-only labels as the primary target identity.

The existing filter controls may be kept as advanced filters, but target picking
must not feel split across unrelated boxes.

### Governance And Target Facts

Governance and access details should be compressed:

- show status badges for executable/locked, credential status, audit required,
  and sandbox state;
- move long explanations to tooltip/popover/details;
- emphasize the single blocking reason for locked targets;
- do not let future-state copy such as "JIT access planned" dominate the page.

Target facts should be deduplicated:

- keep a compact target summary near the selector;
- collapse secondary facts;
- avoid repeating engine/environment/host/cluster in multiple large panels;
- prioritize worksheet, results, and history.

### Worksheet Copy

Phase 38C does not need a full editor rewrite, but the worksheet should make the
current boundary clear:

- say "read-only SQL";
- do not say "SELECT only";
- examples should include a safe `SELECT` and a safe metadata statement such as
  `SHOW TABLES`.

## Deferred Work

The following belong in later phases:

- Monaco/CodeMirror SQL editor;
- SQL formatting;
- multiple worksheet tabs;
- worksheet persistence;
- schema browser with backend metadata APIs;
- broader read-only statement support;
- backend batch credential APIs.

## Acceptance Criteria

Backend:

- `SELECT 1` succeeds.
- `SHOW TABLES` succeeds.
- `SHOW COLUMNS FROM query_e2e_items` succeeds.
- `DESCRIBE query_e2e_items` succeeds.
- `EXPLAIN SELECT * FROM query_e2e_items` succeeds.
- `UPDATE query_e2e_items SET name='x'` is rejected.
- `SHOW PROCESSLIST` is rejected.
- `SHOW DATABASES` is rejected in Phase 38C.
- Multi-statement input is rejected.
- Existing side-effect guard tests still pass.
- OpenAPI validates.
- Integration and fuzz gates pass.

Frontend:

- `/settings/query-credentials` selecting a target has no ICU formatting errors.
- `/query` has no hydration mismatch from the governance admin link.
- target selector is searchable and shows readable target metadata.
- ready target is easy to find and can still execute.
- governance and facts display is compact.
- Query Workbench has no credential edit controls.
- real E2E passes against backend plus dedicated query MySQL fixture.

## References

- Decision:
  `docs/decisions/2026-06-30-phase-38c-query-workbench-readonly-sql-boundary.md`
- Preview findings:
  `docs/superpowers/notes/2026-06-30-query-workbench-preview-findings.md`
- Phase 37H fixture:
  `docs/superpowers/specs/2026-06-24-phase-37h-dedicated-query-e2e-mysql-fixture.md`
