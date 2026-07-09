# Phase 38G Query Workbench Real Usability Cleanup Evidence

## Scope

Frontend-only implementation in:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/phase-38g-query-workbench-real-usability-cleanup
```

Backend repository changes are documentation/evidence only. Backend product code
was not intentionally edited for this phase.

## Frontend Branch

- Branch: `phase-38g-query-workbench-real-usability-cleanup`
- Worktree: `/Users/fan/JsProjects/ControlHub/.worktrees/phase-38g-query-workbench-real-usability-cleanup`
- Commit range: not committed yet at evidence capture time; changes remain in
  the frontend worktree for review/commit.

## Changed Frontend Areas

- Query Workbench shell and governance panel:
  - readable CodeMirror dark/high-contrast styling;
  - persisted editor height preference and resize handle;
  - primary worksheet actions reduced to Run and Format;
  - locked targets show one primary blocker state;
  - post-review stale disabled-execution copy replaced with governed execution
    copy, and governance status badges now reflect each target state.
- Connection navigation:
  - new grouped/searchable connection navigator components;
  - active target highlight and active summary when filters exclude it;
  - E2E helpers updated away from the old target switcher.
- Query Credential Settings:
  - desktop master-detail inspector;
  - selected row highlight;
  - empty inspector state;
  - existing stale-target guards preserved.
- Tests/i18n:
  - unit/component coverage for editor preferences, navigator, workbench, and
    credential settings;
  - focused Query Workbench and credential settings E2E updated;
  - English and Chinese copy updated.

## Verification Evidence

Commands run from the frontend worktree unless noted otherwise:

```text
npx tsc --noEmit -p tsconfig.json
```

Result: passed with no output.

LSP diagnostics were requested for changed TypeScript/TSX files, but the
TypeScript LSP server is not installed and the user previously declined
installation. `tsc` is the type-safety evidence for this run.

```text
npm run lint
```

Result: passed with 0 errors and 2 existing warnings in
`tests/lib/query-sql-format.test.ts` for unused `stmt` and `opts` parameters.

```text
npm run check:e2e-governance
```

Result: passed; 13 spec files scanned.

```text
npm run test -- tests/lib/query-editor-preferences.test.ts tests/components/query-connection-navigator.test.tsx tests/components/query-workbench.test.tsx tests/components/query-credential-settings.test.tsx
```

Result: 4 test files passed, 131 tests passed. Known React `act(...)` warnings
remain in existing component tests.

```text
npm run test
```

Result: 67 test files passed, 820 tests passed. Existing warning noise remains:
React `act(...)` warnings in several component tests and one duplicate-key
warning in `database-decision-deck.test.tsx`.

Post-review focused verification for the stale execution-copy fix:

```text
npx vitest run tests/components/query-workbench.test.tsx tests/components/query-connection-navigator.test.tsx tests/lib/query-editor-preferences.test.ts
```

Result: 3 test files passed, 82 tests passed. Known React `act(...)` warnings
remain in `query-workbench.test.tsx`.

```text
npx eslint components/query/query-governance-panel.tsx tests/components/query-workbench.test.tsx e2e/query-workbench.spec.ts tests/fixtures/query-targets.ts components/query/query-connection-navigator.tsx components/query/query-connection-navigator-body.tsx components/query/query-connection-navigator-list.tsx lib/query-editor-preferences.ts tests/components/query-connection-navigator.test.tsx tests/lib/query-editor-preferences.test.ts
```

Result: passed with no output.

```text
rg -n "Query execution is not enabled|does not connect to databases|run queries|Execution disabled|Read-only credential missing" components/query messages/en.json messages/zh-CN.json tests/components/query-workbench.test.tsx e2e/query-workbench.spec.ts tests/fixtures/query-targets.ts
```

Result: no stale disabled-execution page/banner copy remains for ready targets;
remaining `Execution disabled` / `Read-only credential missing` matches are
locked-target status labels, credential-state enum labels, or tests asserting
ready targets do not render the disabled label.

```text
npm run build
```

Result: passed. Next.js reported the existing middleware-to-proxy deprecation
warning, then compiled, type-checked, generated 13 static pages, and finalized
route optimization.

The same build command was rerun after the post-review stale-copy fix and passed
with the same existing middleware-to-proxy deprecation warning.

```text
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
```

Result: passed earlier in the Phase 38G run, 30 focused E2E tests passed against
the real backend/proxy stack.

```text
npm run check:e2e-preflight -- --strict
```

Result before cleanup: failed as expected because the manually started frontend
dev server on `:3100` and API proxy on `:8081` were still running for manual QA.
After cleanup, the same command passed and reported `:3100` and `:8081` free.

```text
GIT_MASTER=1 git diff --check
```

Result: passed in the frontend worktree.

## Manual Browser QA

Live stack:

- backend: `http://localhost:8080`, `/health` returned `{"status":"ok"}`;
- frontend: `http://localhost:3100`;
- API proxy: `http://localhost:8081`, `/__health` returned `{"ok":true}`;
- dedicated query MySQL fixture seeded `Local MySQL Query Dev`.

Browser route evidence:

- `/query`, locked/unsupported target:
  - no rendered hits for raw secret patterns: `password`, `dsn`, `mysql://`,
    `postgres://`, `secret123`, `root:`, `@tcp(`, `jdbc:`, `credential`;
  - only credential-related control was the navigation link
    `打开凭据设置` to `/settings/query-credentials`;
  - no inline credential edit controls on `/query`.
- `/query`, ready `Local MySQL Query Dev` target:
  - visible Run control (`执行`) and Format control (`格式化`);
  - editor contained `select 1`;
  - executing the query returned 1 row with value `1`;
  - rendered result had no raw DSN/password-like hits.
- `/settings/query-credentials`:
  - page explained DSN/password stay server-side;
  - no password inputs;
  - selected ready credential response contained only metadata:
    `resourceId`, `configured`, `engine`, `credentialRef`, `enabled`,
    `environmentPolicy`, `runtimeStatus`, `executionEligible`, and `message`;
  - `credentialRef` was `LOCAL_QUERY_RO`; no DSN/password was returned.

Screenshots captured during manual QA:

```text
phase38g-query-page.png
phase38g-query-ready-before-run.png
phase38g-query-ready-after-run.png
phase38g-query-credentials-settings.png
phase38g-query-credentials-selected.png
```

The screenshots are evidence artifacts only and should not be committed unless
explicitly requested.

## Scope Confirmation

- No backend product code changes are required for Phase 38G.
- No DSN/password browser state, request body, response display, or logs were
  intentionally introduced.
- No `actorUserId` request field was introduced.
- `/query` remains free of credential edit controls; credential metadata editing
  remains isolated to `/settings/query-credentials`.
- `/query` no longer claims execution is disabled globally; ready targets use
  governed read-only execution copy while locked targets retain blocker labels.
- Cleanup completed: frontend `:3100`, proxy `:8081`, and backend `:8080` were
  stopped, `make query-e2e-mysql-down` removed the dedicated fixture, and a port
  check returned no listeners on `:3100`, `:8081`, or `:8080`.
