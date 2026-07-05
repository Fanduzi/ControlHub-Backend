# Phase 38D Query Workbench Admin Follow-Ups Plan

> **Scope:** cross-repo follow-up. Backend expands read-only metadata SQL to
> include database-level metadata. Frontend fixes admin direct navigation,
> settings IA, and target-selector density. No credential secrets, no new query
> engines, no full SQL editor.

## Objective

Resolve the remaining manual-preview issues after Phase 38C:

- `SHOW DATABASES` should work as a read-only metadata statement;
- admin direct navigation to `/settings/query-credentials` should not show the
  false non-admin view;
- `/settings` should expose Query Credential settings;
- `/query` should have a single compact target-selection area instead of a
  picker plus separate filters plus large facts.

## Non-Goals

- No DSN/password input or storage.
- No secret manager UI.
- No credential secret write API.
- No new query engines.
- No export.
- No saved queries.
- No full SQL IDE editor migration.
- No SQL formatting.
- No multiple worksheet tabs.
- No CI workflow changes.
- No tag, release, or deployment.

## B1. Backend Parser And Guard Tests

**Files:**

- `internal/service/query_guard.go`
- `internal/service/query_guard_test.go`
- `internal/integration/query_execution_test.go`
- `internal/openapi/openapi.yaml` if copy needs clarification

### Steps

1. Confirm Vitess AST fields for:
   - `SHOW DATABASES`;
   - `SHOW TABLES FROM query_e2e`;
   - `SHOW COLUMNS FROM query_e2e.query_e2e_items`;
   - `DESCRIBE query_e2e.query_e2e_items`;
   - `SHOW PROCESSLIST`;
   - `SHOW GRANTS`.
2. Add RED tests that reflect the new product decision:
   - `AllowsShowDatabases`;
   - `AllowsShowTablesFromSchema`;
   - `AllowsShowColumnsFromQualifiedTable`;
   - `AllowsDescribeQualifiedTable`;
   - `AllowsDescQualifiedTable`.
3. Keep rejection tests:
   - `RejectsShowProcesslist`;
   - `RejectsShowGrants`;
   - `RejectsUseDatabase`;
   - `RejectsSetStatement`;
   - writes/DDL/multi-statement.

### GREEN Design

- Remove the Phase 38C cross-schema rejection for safe metadata statements.
- Continue to dispatch by AST type and command enum.
- Do not string-prefix match.
- Preserve `EXPLAIN SELECT` wrapper behavior fixed in Phase 38C.

### Commit

```bash
git commit -m "feat: allow readonly database metadata statements"
```

## B2. Backend Integration And Contract Verification

**Files:**

- `internal/integration/query_execution_test.go`
- `internal/openapi/openapi.yaml`
- evidence note

### Tests

Against the dedicated query MySQL fixture:

- `SHOW DATABASES` succeeds and includes `query_e2e`;
- `SHOW TABLES FROM query_e2e` succeeds and includes `query_e2e_items`;
- `SHOW COLUMNS FROM query_e2e.query_e2e_items` succeeds;
- `DESCRIBE query_e2e.query_e2e_items` succeeds;
- `SHOW PROCESSLIST` remains rejected;
- `SHOW GRANTS` remains rejected;
- `UPDATE` remains rejected.

### Verification

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

### Commit

```bash
git commit -m "test: cover readonly database metadata execution"
```

## F1. Frontend Auth Role Recovery For Admin Pages

**Files:**

- `/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx`
- auth/session helper files, if present
- tests for query credential settings

### Problem

`QueryCredentialSettings` uses `sessionStorage["controlhub.role"] === "admin"`.
That fails on direct URL entry, refreshed tabs, or restored cookie sessions where
the auth cookie exists but sessionStorage role is missing.

### Design

1. Create or reuse a small frontend auth role helper:
   - reads sessionStorage role if present;
   - if missing, attempts role recovery from the current authenticated session;
   - returns `admin`, non-admin role, or unauthenticated/unknown.
2. Keep SSR/first-client render hydration-safe.
3. Do not show admin controls until admin is confirmed.
4. Do not show a final non-admin restricted state while role recovery is still
   pending.

If no backend endpoint exists to recover the role, stop and report the backend
gap instead of inventing an admin role in the browser.

### Tests

- direct render with only cookie/session available recovers admin and shows admin
  UI;
- missing role while recovery is pending shows loading/unknown state, not false
  restricted state;
- viewer/non-admin still sees restricted view;
- non-admin never triggers save/delete controls;
- hydration-safe first render remains stable.

### Commit

```bash
git commit -m "fix: recover admin role for credential settings"
```

## F2. Frontend Settings Entry

**Files:**

- `/Users/fan/JsProjects/ControlHub/app/(console)/settings/page.tsx`
- messages
- component/E2E tests

### Design

Add a Query Credential settings card/row to `/settings`:

- title: Query credential settings;
- description: administrators manage metadata references; DSN/password remain
  server-side;
- link to `/settings/query-credentials`;
- admin action: Open credential settings;
- non-admin copy: Managed by administrators.

Add command palette/navigation entry only if it matches existing IA and can be
tested without broad rewrites. The `/settings` page entry is required.

### Tests

- `/settings` renders the Query Credential settings entry;
- admin can navigate to `/settings/query-credentials`;
- non-admin does not see edit controls from the settings entry;
- i18n keys exist for en and zh-CN.

### Commit

```bash
git commit -m "feat: expose query credential settings entry"
```

## F3. Frontend Query Target Area Compression

**Files:**

- `/Users/fan/JsProjects/ControlHub/components/query/query-workbench.tsx`
- `/Users/fan/JsProjects/ControlHub/components/query/query-governance-panel.tsx`
- messages and tests

### Design

Replace the current split layout:

```text
target picker
search box + engine dropdown + mode dropdown + readiness dropdown
large selected-target facts block
```

with one compact target-selection surface:

- primary picker search covers name, engine, env, host, port, cluster, readiness;
- engine/query-kind/readiness filters live inside the picker as chips or an
  advanced filters popover;
- selected target summary uses compact chips only;
- owner/language/cluster/details move behind a disclosure or tooltip;
- governance panel does not repeat target facts.

### Tests

- no separate always-visible filter row below the target picker;
- selected target summary shows compact chips for engine/environment/readiness;
- owner/language/cluster appear only in details/tooltip/disclosure;
- picker search still matches host and engine;
- ready target remains sorted/highlighted;
- governance panel does not duplicate engine/environment/host facts.

### Commit

```bash
git commit -m "fix: consolidate query target selection surface"
```

## F4. Real E2E

Use backend Phase 38D plus dedicated query MySQL fixture.

Required checks:

- Query Workbench can run:
  - `SELECT 1`;
  - `SHOW DATABASES`;
  - `SHOW TABLES FROM query_e2e`;
  - `DESCRIBE query_e2e.query_e2e_items`;
  - unsafe `UPDATE` rejected.
- `/settings` links to Query Credential settings.
- Direct URL `/settings/query-credentials` as admin shows admin controls.
- No credential edit controls appear in `/query`.
- No fake backend.

## Documentation Evidence

Add one evidence note after implementation:

```text
docs/superpowers/notes/2026-07-05-phase-38d-query-workbench-admin-followups-evidence.md
```

Include:

- backend commits and verification;
- frontend commits and verification;
- real E2E result;
- known caveats;
- final scope confirmation.

## Final Verification Matrix

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

## Scope Confirmation

- No DSN/password browser state.
- No credential secret write API.
- No new query engines.
- No SQL guard relaxation beyond explicit read-only metadata statements.
- No Workbench credential edit controls.
- No fake backend final E2E.
- No tag/release/deploy.
