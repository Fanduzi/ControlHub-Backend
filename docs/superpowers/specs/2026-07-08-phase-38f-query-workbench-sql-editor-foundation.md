# Phase 38F Query Workbench SQL Editor Foundation Design

## Background

Phase 38C/38D/38E closed the high-friction Query Workbench issues around
read-only SQL execution, credential navigation, target discovery, admin entry
points, hydration safety, and governance density.

The remaining manual-preview issue is issue 7 from:

- `docs/superpowers/notes/2026-06-30-query-workbench-preview-findings.md`
- `docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md`

Current frontend state:

- `components/query/query-editor-shell.tsx` still uses a native `<textarea>`;
- there is no SQL syntax highlighting;
- there is no SQL formatter;
- there is only one worksheet state per selected target;
- `QueryWorkbench` keys `<QueryEditorShell>` by `activeTarget.resourceId`, which
  intentionally remounts the editor on target switch but makes multiple local
  worksheets impossible.

Bytebase research remains relevant, but Phase 38F should not attempt a full IDE
rewrite. The right next step is a local SQL editor foundation that makes the
worksheet feel credible while preserving the current backend contract.

## Goal

Upgrade the Query Workbench worksheet area into a local multi-worksheet SQL
editor foundation:

- SQL syntax highlighting;
- SQL formatting;
- keyboard shortcuts for run and format;
- multiple local worksheet tabs;
- per-worksheet target, statement, max rows, result, error, history, and loading
  state;
- clear active target synchronization so the schema browser and governance panel
  always describe the worksheet that is currently active.

## Non-Goals

- No backend product code.
- No SQL guard changes.
- No new query engines.
- No saved query API.
- No worksheet persistence across refresh.
- No share/export/download.
- No approval/JIT workflow.
- No server-side worksheet history model.
- No credential edit controls on `/query`.
- No DSN/password browser state, request body, response rendering, or logging.
- No `actorUserId` in any request.
- No CI workflow changes.
- No tag, release, or deployment.

## Chosen Approach

Use CodeMirror 6 through `@uiw/react-codemirror`, plus `@codemirror/lang-sql` for
SQL highlighting and `sql-formatter` for formatting.

Rationale:

- CodeMirror has a smaller integration footprint than Monaco for this phase.
- The frontend is a Next.js client-heavy console; CodeMirror can be loaded
  client-only to avoid SSR/hydration risk.
- `sql-formatter` supports `mysql` and `tidb` dialects, matching the current
  backend-ready engines.
- Local worksheet tabs can be built around existing React state without new
  backend APIs.

Monaco remains a possible future editor if ControlHub needs richer database IDE
features such as language-server completion, schema-aware hover, or deeper
workspace/session state. It is intentionally not chosen for Phase 38F.

## Product Requirements

### F1. SQL Editor Component

Replace the worksheet `<textarea>` with a CodeMirror-backed SQL editor.

Required behavior:

- syntax highlighting for SQL;
- controlled `value` / `onChange` behavior;
- accessible label for the editor region;
- stable SSR/client first render by loading the editor client-only;
- visible fallback/loading shell while the editor bundle loads;
- no render-time access to `window`, `document`, or `sessionStorage`;
- component tests may mock CodeMirror as a textarea, but final E2E must exercise
  the real browser editor.

### F2. SQL Formatting

Add a format action near Run.

Required behavior:

- formats the current worksheet statement in place;
- uses `mysql` formatting for `mysql` and `tidb` targets;
- falls back to generic `sql` formatting for other engines if the editor is
  visible;
- does not execute SQL;
- shows controlled error feedback if formatting fails;
- formatting an empty statement is a no-op;
- never sends statement text to a server.

### F3. Keyboard Shortcuts

Add editor-focused shortcuts:

- `Cmd/Ctrl+Enter` runs the active worksheet when Run is enabled;
- `Cmd/Ctrl+Shift+F` formats the active worksheet;
- buttons remain available for mouse/touch users;
- shortcuts must not bypass backend run eligibility;
- shortcuts must not double-submit while execution is pending.

### F4. Multiple Local Worksheets

Add local worksheet tabs inside the existing worksheet surface.

Required behavior:

- default worksheet: `Worksheet 1`, statement `select 1`, current active target;
- add worksheet button creates `Worksheet N` with the current target and default
  statement;
- worksheet names can be edited locally;
- close worksheet is allowed when more than one worksheet exists;
- closing the active worksheet selects a neighboring worksheet;
- state is local and intentionally lost on page refresh;
- no saved query API and no localStorage/sessionStorage persistence.

### F5. Per-Worksheet State Isolation

Each worksheet owns:

- worksheet id;
- name;
- target resource id;
- statement;
- max rows;
- result;
- controlled error;
- history;
- history loading state;
- execution loading state;
- active result tab.

Switching worksheets must restore that worksheet's target and state. Results,
errors, history, and pending execution from worksheet A must never paint into
worksheet B.

### F6. Target Synchronization

The active target shown by the target navigator, schema browser, and governance
panel must reflect the active worksheet.

Required behavior:

- `QueryEditorShell` no longer remounts on every target change;
- `QueryWorkbench` passes the full target list, current active target id, and a
  target-change callback to `QueryEditorShell`;
- when the user changes the target navigator, the active worksheet target is
  updated and target-owned result/error/history for that worksheet is cleared;
- when the user switches worksheets, `QueryEditorShell` asks `QueryWorkbench` to
  switch the global active target to that worksheet's target;
- target synchronization must be source-aware: a user-initiated target navigator
  change clears the active worksheet's target-owned state, but a worksheet switch
  that merely restores that worksheet's saved target must not clear that
  worksheet's result/history;
- schema browser and governance panel always describe the active worksheet
  target, not a stale global target.

### F7. History Behavior

History remains backend-provided execution metadata.

Required behavior:

- active worksheet history tab shows history for that worksheet's target;
- executing a worksheet refreshes that worksheet's history after settlement;
- switching worksheets does not show another worksheet's stale history;
- locked targets do not fetch history;
- history remains metadata-only and never displays result rows or credentials.

### F8. Test And E2E Requirements

Component tests must cover:

- CodeMirror wrapper controlled value behavior with a test mock;
- formatting helper success/failure;
- format button updates the active worksheet only;
- `Cmd/Ctrl+Enter` run shortcut;
- `Cmd/Ctrl+Shift+F` format shortcut;
- adding, renaming, switching, and closing worksheets;
- per-worksheet statement/result/error/history isolation;
- target navigator changes active worksheet target;
- worksheet switch updates schema/governance target through parent target state;
- no credential edit controls on `/query`;
- no DSN/password/actorUserId request fields.

Real E2E with backend + dedicated query MySQL must cover:

- ready target runs `SELECT 1` from the CodeMirror editor;
- ready target runs `SHOW TABLES`;
- format action changes messy SQL into formatted SQL;
- shortcut `Cmd/Ctrl+Enter` runs the active worksheet;
- two worksheets keep separate statements/results;
- switching worksheets restores the target context;
- unsafe SQL remains rejected by the backend.

If backend or query MySQL is unavailable, the final report must explicitly say
E2E was not run and must not claim real execution passed.

## Acceptance Criteria

- `/query` no longer uses a plain textarea for the ready worksheet editor.
- SQL editor has highlighting in the browser and an accessible label.
- Format action is local, deterministic, and covered by tests.
- Run and format keyboard shortcuts work without bypassing existing guards.
- Users can create, rename, switch, and close multiple local worksheets.
- Each worksheet has independent target, statement, result, error, history, and
  loading state.
- Schema browser and governance panel follow the active worksheet target.
- No saved-query, export, approval, credential editing, DSN/password, or
  actorUserId behavior is introduced.
- Existing Phase 38E target navigation and credential admin behavior remains
  intact.
