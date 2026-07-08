# Frontend Phase 38F Query Workbench SQL Editor Worker Prompt

You are implementing the frontend side of Phase 38F for ControlHub.

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repo is separate. Do not edit backend product code in this frontend
task.

## Objective

Upgrade the Query Workbench worksheet from a plain textarea into a local SQL
editor foundation:

- CodeMirror SQL syntax highlighting;
- SQL formatting;
- Cmd/Ctrl+Enter run shortcut;
- Cmd/Ctrl+Shift+F format shortcut;
- multiple local worksheet tabs;
- per-worksheet target, statement, max rows, result, error, history, and loading
  state;
- schema browser and governance panel synchronized to the active worksheet
  target.

## Required Reading

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-08-phase-38f-query-workbench-sql-editor-foundation.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-07-08-phase-38f-query-workbench-sql-editor-foundation.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

Frontend files to inspect first:

```text
package.json
components/query/query-editor-shell.tsx
components/query/query-workbench.tsx
components/query/query-governance-panel.tsx
components/query/query-schema-browser.tsx
messages/en.json
messages/zh-CN.json
tests/components/query-workbench.test.tsx
e2e/query-workbench.spec.ts
```

## OMO Guidance

If running this through omo / oh-my-openagent:

- use Oracle before implementation to sanity-check the editor/worksheet state
  design against the spec and plan;
- use Momus after implementation for adversarial review, especially around
  target-state isolation, stale async guards, and request-body boundaries;
- do not let either review expand scope into saved queries, export, approval, or
  backend changes.

## Rules

- Use TDD: write failing tests before production code.
- Run GitNexus impact before editing frontend symbols when the repo index can
  map the worktree.
- Do not edit backend product code.
- Do not change the backend SQL guard.
- Do not add saved query/export/approval workflow.
- Do not persist worksheets to backend, localStorage, or sessionStorage.
- Do not add credential edit controls to `/query`.
- Do not add DSN/password browser state, input, request body, response display,
  or logs.
- Do not send `actorUserId`.
- Do not fake backend for final E2E.
- Do not push/tag/release/deploy.
- Do not add AI co-author.

## Required Tasks

### F0. Dependencies

Add:

```bash
npm install @uiw/react-codemirror @codemirror/lang-sql sql-formatter
```

Verify only `package.json` and `package-lock.json` dependency changes are
introduced.

### F1. SQL formatter helper

Create:

```text
lib/query-sql-format.ts
tests/lib/query-sql-format.test.ts
```

Required behavior:

- `mysql` and `tidb` use formatter language `mysql`;
- unknown engines use generic `sql`;
- formatting is local only and never calls the backend;
- empty statements are unchanged;
- formatter failures return a controlled error string.

### F2. CodeMirror wrapper

Create:

```text
components/query/sql-code-editor.tsx
components/query/sql-code-editor-client.tsx
```

Required behavior:

- client-only dynamic import to avoid SSR/hydration issues;
- controlled `value` and `onChange`;
- SQL extension from `@codemirror/lang-sql`;
- CodeMirror keymap for `Mod-Enter` and `Mod-Shift-f`;
- accessible label;
- deterministic component tests may mock this wrapper as a textarea.

### F3. Local worksheet tabs

Modify:

```text
components/query/query-editor-shell.tsx
```

Required behavior:

- default `Worksheet 1`;
- add worksheet button creates `Worksheet N`;
- rename active worksheet locally;
- close worksheet when more than one exists;
- no persistence across refresh;
- each worksheet owns statement, max rows, result, error, history, loading, and
  active result tab.

### F4. Target synchronization

Modify:

```text
components/query/query-workbench.tsx
components/query/query-editor-shell.tsx
```

Required behavior:

- remove `key={activeTarget.resourceId}` from `<QueryEditorShell>`;
- pass `targets`, `activeTarget`, and `onActiveTargetChange`;
- changing the target navigator updates the active worksheet target and clears
  target-owned result/error/history for that worksheet;
- switching worksheets restores that worksheet target into the parent active
  target so schema browser and governance panel stay correct;
- stale async execution/history from worksheet A never writes into worksheet B.
- target synchronization must be source-aware: target navigator changes clear
  the active worksheet's target-owned state, but worksheet switches that restore
  an existing worksheet target must not clear that worksheet's result/history.

### F5. Format and shortcuts

Required behavior:

- replace ready worksheet textarea with `SqlCodeEditor`;
- add visible Format button near Run;
- `Cmd/Ctrl+Enter` runs only when Run is enabled;
- `Cmd/Ctrl+Shift+F` formats only the active worksheet;
- disabled/locked targets cannot run through shortcuts;
- format error renders as controlled alert.
- test formatter failures by injecting/mocking a throwing formatter; do not rely
  on a particular malformed SQL string throwing, because formatter parser
  behavior can change between versions.

### F6. E2E

Update:

```text
e2e/query-workbench.spec.ts
```

Required real-backend coverage:

- ready target runs SELECT from the CodeMirror editor;
- ready target runs SHOW TABLES;
- format action visibly formats messy SQL;
- Cmd/Ctrl+Enter runs the active worksheet;
- two worksheets keep separate statements/results;
- worksheet switch restores target context;
- unsafe SQL remains rejected.

## Verification

Run from the frontend worktree:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Run real E2E only with real backend + dedicated query MySQL fixture:

```bash
npm run test:e2e -- --grep "Query Workbench"
```

Run GitNexus before final report:

```bash
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Frontend
```

## Final Report

Include:

- branch/worktree;
- commits;
- dependency additions;
- files changed;
- SQL editor/highlighting proof;
- formatter proof and no-server guarantee;
- shortcut proof;
- worksheet state isolation proof;
- target synchronization proof;
- E2E result or explicit blocker;
- full verification matrix;
- GitNexus result and caveats;
- final git status;
- scope confirmation:
  - no backend edits;
  - no SQL guard changes;
  - no credential/DSN/password browser state;
  - no `actorUserId`;
  - no Workbench credential edit controls;
  - no saved query/export/approval;
  - no fake backend final E2E;
  - no push/tag/release/deploy;
  - no AI co-author.
