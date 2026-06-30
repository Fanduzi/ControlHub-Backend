# Phase 38C Query Workbench Read-Only SQL And UX Stabilization Plan

> **Scope:** cross-repo stabilization. Backend widens the MySQL/TiDB guard from
> SELECT-only to read-only SQL allow-list. Frontend fixes two bugs and tightens
> Query Workbench IA. Do not add new query engines or credential secret handling.

## Objective

Deliver a Query Workbench that can be previewed without obvious product
contradictions:

- safe read-only metadata statements work;
- query page does not hydrate incorrectly;
- credential settings detail panel does not crash;
- target selection is searchable;
- governance and target facts are compact;
- worksheet copy matches the backend boundary.

## Non-Goals

- No DSN/password input or storage.
- No secret manager UI.
- No credential secret write API.
- No new query engines.
- No export.
- No saved query feature.
- No full SQL IDE editor migration.
- No persistent worksheet tabs.
- No CI workflow changes.
- No tag, release, or deployment.

## B1. Backend Parser Spike For Read-Only Statements

**Files to inspect:**

- `internal/service/query_guard.go`
- `internal/service/query_guard_test.go`
- existing parser spike/tests, if present

### Steps

1. Confirm how the current Vitess parser represents:
   - `SHOW TABLES`;
   - `SHOW COLUMNS FROM query_e2e_items`;
   - `DESCRIBE query_e2e_items`;
   - `DESC query_e2e_items`;
   - `EXPLAIN SELECT * FROM query_e2e_items`;
   - `SHOW PROCESSLIST`;
   - `SHOW DATABASES`.
2. Document whether each statement is reachable as an AST node or requires a
   specific parser API.
3. Do not implement string-prefix allow-listing.

### RED Tests

Add or update guard tests that currently fail for allowed metadata statements:

- `AllowsShowTables`
- `AllowsShowColumns`
- `AllowsDescribeTable`
- `AllowsDescTable`
- `AllowsExplainSelect`

Add rejection tests:

- `RejectsShowProcesslist`
- `RejectsShowDatabases`
- `RejectsShowGrants`
- `RejectsUseDatabase`
- `RejectsSetStatement`

### Commit

Suggested commit:

```bash
git commit -m "test: cover readonly query metadata statements"
```

## B2. Backend Guard Widening

**Files:**

- `internal/service/query_guard.go`
- `internal/service/query_execution_service.go` if statement classification copy is
  centralized there
- tests in `internal/service`

### Design

Change the guard model from:

```text
single SELECT
```

to:

```text
single read-only SQL statement from an explicit allow-list
```

Allowed:

- `SELECT`;
- `SHOW TABLES`;
- `SHOW COLUMNS FROM <table>`;
- `DESCRIBE <table>` / `DESC <table>`;
- `EXPLAIN SELECT ...`.

Still reject:

- writes, DDL, session mutation, transactions, locks, grants/users;
- `SHOW PROCESSLIST`;
- `SHOW DATABASES`;
- file/export paths;
- side-effect functions;
- user-variable assignments;
- multi-statement input.

Keep existing row cap, timeout, history, audit, and credential binding unchanged.

### GREEN Criteria

```bash
go test -count=1 ./internal/service -run 'QueryGuard|QueryExecution'
go test -count=1 ./...
```

### Commit

Suggested commit:

```bash
git commit -m "feat: allow readonly metadata query statements"
```

## B3. Backend Contract And E2E Coverage

**Files:**

- `internal/openapi/openapi.yaml`
- `internal/integration/query_execution_test.go` or related integration tests
- evidence note

### Steps

1. Update OpenAPI descriptions from "SELECT" to "read-only SQL" where the execute
   request or errors describe the allowed statement class.
2. Add integration coverage against the dedicated query fixture:
   - `SHOW TABLES` succeeds;
   - `DESCRIBE query_e2e_items` succeeds;
   - `EXPLAIN SELECT * FROM query_e2e_items` succeeds;
   - `UPDATE` remains rejected.
3. Ensure history/audit records the accepted metadata statements and rejected
   attempts.

### Verification

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

### Commit

Suggested commit:

```bash
git commit -m "test: cover readonly metadata query execution"
```

## F1. Frontend Immediate Bug Fixes

**Files:**

- `/Users/fan/JsProjects/ControlHub/messages/en.json`
- `/Users/fan/JsProjects/ControlHub/messages/zh-CN.json`
- `/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx`
- `/Users/fan/JsProjects/ControlHub/components/query/query-governance-panel.tsx`
- component tests

### Steps

1. Fix `queryCredentialSettings.detail.credentialRefHint` and
   `queryCredentialSettings.detail.boundaryNote` so they do not require `{ref}`
   unless the component passes a safe literal.
2. Prefer static copy with `CONTROLHUB_QUERY_CREDENTIAL_your-ref`.
3. Fix query governance admin-link hydration:
   - no render-time `window` / `sessionStorage` access;
   - initial admin state `null`;
   - stable SSR and first-client markup;
   - `useEffect` sets admin state;
   - admin link appears only after role confirmation.

### Tests

- settings detail target selection does not log `FORMATTING_ERROR`;
- zh-CN and en messages render without ICU variable errors;
- query governance panel first render is hydration-safe;
- admin link appears after role is read;
- viewer/non-admin sees contact-admin copy.

### Commit

Suggested commit:

```bash
git commit -m "fix: stabilize query credential settings and governance hydration"
```

## F2. Frontend Query Target Picker

**Files:**

- `components/query/query-workbench.tsx`
- new subcomponent if useful, e.g. `components/query/query-target-picker.tsx`
- tests and i18n messages

### Design

Replace the current target dropdown with a searchable picker:

- search display name, resource name, engine, environment, host, cluster;
- show readable rows with name, engine, env, host:port, readiness/run status;
- visually mark ready targets and preferably sort them first;
- keep keyboard and screen-reader accessibility;
- avoid raw enum leakage.

The picker may be implemented with existing project UI primitives. Do not add a
heavy dependency unless the repo already uses it.

### Tests

- ready target is visible and easy to select by search;
- long target names remain readable;
- search matches host and engine;
- selecting a target updates editor/governance/history context;
- no `:0` host/port regression.

### Commit

Suggested commit:

```bash
git commit -m "feat: add searchable query target picker"
```

## F3. Frontend Governance And Facts Compression

**Files:**

- `components/query/query-governance-panel.tsx`
- `components/query/query-workbench.tsx`
- messages and component tests

### Design

Convert large governance explanations into compact badges:

- executable/locked;
- credential status;
- audit required;
- sandbox/safety state.

Move long text to tooltip/popover/details. Deduplicate target facts:

- compact target summary near the picker;
- secondary target facts collapsible;
- ready target view prioritizes editor, results, and history.

Do not add Workbench credential edit controls.

### Tests

- locked target shows one primary blocking reason;
- ready target shows compact executable/read-only/audited badges;
- details are available through tooltip/details;
- duplicate large fact blocks are absent;
- no credential inputs or save/delete buttons appear in `/query`.

### Commit

Suggested commit:

```bash
git commit -m "feat: compact query workbench governance details"
```

## F4. Frontend Copy And E2E For Read-Only SQL

**Files:**

- `components/query/query-editor-shell.tsx`
- `e2e/query-workbench.spec.ts`
- messages

### Steps

1. Update copy from "SELECT only" to "read-only SQL".
2. Add examples or hints for:
   - `select * from query_e2e_items`;
   - `show tables`;
3. Add real-backend E2E coverage:
   - ready target runs `SHOW TABLES`;
   - ready target runs `DESCRIBE query_e2e_items`;
   - unsafe `UPDATE` remains rejected;
   - history records the attempts.

### Commit

Suggested commit:

```bash
git commit -m "test: cover readonly query workbench statements"
```

## Cross-Repo Verification

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
```

Real E2E:

```bash
cd /Users/fan/GolangProjects/ControlHub
make query-e2e-mysql-up
# start backend with .query-e2e-mysql.env sourced and seed ready target

cd /Users/fan/JsProjects/ControlHub
npm run test:e2e -- --grep query
```

Expected:

- no ready-target skips;
- `SELECT` succeeds;
- `SHOW TABLES` succeeds;
- `DESCRIBE` succeeds;
- `UPDATE` is rejected;
- no fake backend;
- no DSN/password printed.

## Evidence

Record final evidence in a new note:

```text
docs/superpowers/notes/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization-evidence.md
```

Include:

- backend commits and verification;
- frontend commits and verification;
- real E2E environment and results;
- no-secret and no-credential-UI boundaries;
- any deferred worksheet IDE work.
