# Phase 38G Query Workbench Real Usability Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the Phase 38F SQL editor foundation into a usable read-only query workbench by fixing dark-mode readability, editor resizing, connection navigation, credential detail layout, and placeholder action clutter.

**Architecture:** Frontend-only phase. Keep the existing backend contracts and query execution service. Add focused client utilities for editor theme and size preference, replace the dropdown-style target selector with a connection navigator component, and convert credential settings to a desktop master-detail inspector with mobile drawer behavior.

**Tech Stack:** Next.js 16, React 19, TypeScript, next-intl, CodeMirror 6, shadcn-style local UI components, Vitest, Playwright.

---

## Scope

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repo is documentation and local E2E environment only:

```text
/Users/fan/GolangProjects/ControlHub
```

Do not edit backend product code.

## Required Reading

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-08-phase-38g-query-workbench-real-usability-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-08-phase-38f-query-workbench-sql-editor-foundation.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

Bytebase reference files:

```text
/Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/SQLEditorLayout.tsx
/Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/ConnectionPane/ConnectionPane.tsx
/Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/ConnectionPane/TreeNode/DatabaseNode.tsx
/Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/StandardPanel/SQLEditor.tsx
```

## Expected Files

Create:

```text
lib/query-editor-preferences.ts
tests/lib/query-editor-preferences.test.ts
components/query/query-connection-navigator.tsx
tests/components/query-connection-navigator.test.tsx
```

Modify:

```text
components/query/sql-code-editor-client.tsx
components/query/sql-code-editor.tsx
components/query/query-editor-shell.tsx
components/query/query-workbench.tsx
components/query/query-governance-panel.tsx
components/settings/query-credential-settings.tsx
messages/en.json
messages/zh-CN.json
tests/components/query-workbench.test.tsx
tests/components/query-credential-settings.test.tsx
e2e/query-workbench.spec.ts
e2e/query-credential-settings.spec.ts
```

Backend docs to update after frontend completion:

```text
docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
docs/superpowers/notes/2026-07-08-phase-38g-query-workbench-real-usability-cleanup-evidence.md
docs/quality-baseline.md
```

## Task G0: Worktree And Baseline

- [ ] Create an isolated frontend worktree from current `main`.

Run:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree add .worktrees/phase-38g-query-workbench-real-usability-cleanup -b phase-38g-query-workbench-real-usability-cleanup main
cd .worktrees/phase-38g-query-workbench-real-usability-cleanup
```

Expected: clean worktree on the new branch.

- [ ] Run baseline gates.

Run:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Expected: all pass before edits. If a baseline gate fails, stop and report it
before changing files.

## Task G1: Editor Theme And Height Preference Utilities

**Purpose:** Add pure helpers before UI changes.

- [ ] Write tests in `tests/lib/query-editor-preferences.test.ts`.

Test cases:

```text
normalizeEditorTheme("system") -> "system"
normalizeEditorTheme("dark") -> "dark"
normalizeEditorTheme("light") -> "light"
normalizeEditorTheme("high_contrast") -> "high_contrast"
normalizeEditorTheme("bad") -> "system"
clampEditorHeight(100) -> 180
clampEditorHeight(220) -> 220
clampEditorHeight(1000) -> 640
parseStoredEditorHeight("360") -> 360
parseStoredEditorHeight("abc") -> null
parseStoredEditorHeight("50") -> null
```

Run:

```bash
npm run test -- tests/lib/query-editor-preferences.test.ts
```

Expected: fail because helper does not exist.

- [ ] Create `lib/query-editor-preferences.ts`.

Required exports:

```ts
export const QUERY_EDITOR_HEIGHT_STORAGE_KEY = "controlhub.query.editor.height";
export const QUERY_EDITOR_THEME_STORAGE_KEY = "controlhub.query.editor.theme";
export const MIN_QUERY_EDITOR_HEIGHT = 180;
export const MAX_QUERY_EDITOR_HEIGHT = 640;

export type QueryEditorThemePreference =
  | "system"
  | "light"
  | "dark"
  | "high_contrast";

export function normalizeEditorTheme(value: unknown): QueryEditorThemePreference {
  return value === "light" ||
    value === "dark" ||
    value === "high_contrast" ||
    value === "system"
    ? value
    : "system";
}

export function clampEditorHeight(value: number): number {
  if (!Number.isFinite(value)) return 260;
  return Math.min(MAX_QUERY_EDITOR_HEIGHT, Math.max(MIN_QUERY_EDITOR_HEIGHT, Math.round(value)));
}

export function parseStoredEditorHeight(value: string | null): number | null {
  if (value === null) return null;
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) return null;
  if (parsed < MIN_QUERY_EDITOR_HEIGHT || parsed > MAX_QUERY_EDITOR_HEIGHT) return null;
  return Math.round(parsed);
}
```

- [ ] Run tests and commit.

Run:

```bash
npm run test -- tests/lib/query-editor-preferences.test.ts
git add lib/query-editor-preferences.ts tests/lib/query-editor-preferences.test.ts
git commit -m "feat(query): add editor preference helpers"
```

## Task G2: CodeMirror Dark Theme And Readability

**Purpose:** Make the editor readable in dark mode and high contrast.

- [ ] Add theme support to `components/query/sql-code-editor-client.tsx`.

Implementation requirements:

- accept a `themePreference?: QueryEditorThemePreference` prop;
- derive CodeMirror theme extensions with `EditorView.theme`;
- define light, dark, and high-contrast editor colors;
- include readable foreground, background, gutter, cursor, selection, active line,
  and line number styles;
- keep `onEditorView` behavior from Phase 38F.

- [ ] Update `components/query/sql-code-editor.tsx` prop passthrough.

- [ ] Add/extend component tests in `tests/components/query-workbench.test.tsx`.

Test names:

```text
renders the SQL editor with dark theme preference
keeps editor controls readable in dark mode
```

Use the existing `SqlCodeEditor` mock only for shell tests; add assertions for
the prop passed to the mock. Do not rely on jsdom computed color for CodeMirror.

- [ ] Add a real E2E assertion in `e2e/query-workbench.spec.ts`.

Test name:

```text
dark mode SQL editor remains readable while typing
```

Required checks:

- switch app to dark mode using existing theme controls or stored theme state;
- open `/query`;
- select ready target;
- type SQL into `.cm-content`;
- assert `.cm-content` has non-empty text;
- inspect computed styles for editor background and foreground;
- assert foreground and background are not identical and not both very light.

- [ ] Run targeted tests and commit.

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
npx tsc --noEmit
git add components/query/sql-code-editor-client.tsx components/query/sql-code-editor.tsx tests/components/query-workbench.test.tsx e2e/query-workbench.spec.ts
git commit -m "fix(query): make sql editor readable in dark mode"
```

## Task G3: Resizable Editor Height With Local Preference

**Purpose:** Let users size the editor work surface and remember it locally.

- [ ] Modify `components/query/query-editor-shell.tsx`.

Required behavior:

- default editor height: 260px;
- read `QUERY_EDITOR_HEIGHT_STORAGE_KEY` after hydration;
- apply height to `SqlCodeEditor`;
- add a visible horizontal resize handle below the editor;
- pointer drag changes height;
- height is clamped by `clampEditorHeight`;
- persisted to localStorage on drag end;
- no statement/result/history persistence.

- [ ] Modify `components/query/sql-code-editor-client.tsx` and wrapper props.

Required prop:

```ts
height?: number;
```

Use `height` for CodeMirror `height` or container style. Do not hardcode
`minHeight="220px"` after this task.

- [ ] Add component tests.

Test names:

```text
loads stored editor height after hydration
clamps and persists editor height after resize
ignores invalid stored editor height
```

- [ ] Add E2E test.

Test name:

```text
editor resize persists after reload
```

Required checks:

- drag resize handle;
- capture editor height;
- reload page;
- assert editor height remains within 8px of captured height.

- [ ] Run targeted tests and commit.

Run:

```bash
npm run test -- tests/lib/query-editor-preferences.test.ts tests/components/query-workbench.test.tsx
npx tsc --noEmit
git add components/query/query-editor-shell.tsx components/query/sql-code-editor-client.tsx components/query/sql-code-editor.tsx tests/components/query-workbench.test.tsx e2e/query-workbench.spec.ts
git commit -m "feat(query): add resizable sql editor height"
```

## Task G4: Connection Navigator

**Purpose:** Replace the dropdown-style target selector with workbench navigation.

- [ ] Create `components/query/query-connection-navigator.tsx`.

Required props:

```ts
type QueryConnectionNavigatorProps = {
  targets: QueryTarget[];
  activeTargetId: number | null;
  filters: WorkbenchFilters;
  engines: string[];
  onSelect: (resourceId: number) => void;
  onFilterChange: (patch: Partial<WorkbenchFilters>) => void;
};
```

Required behavior:

- desktop left panel/rail layout;
- mobile drawer-compatible structure using existing popover/sheet primitives if
  available;
- groups by environment and cluster;
- search input inside navigator;
- filter controls inside navigator;
- ready targets visually prioritized;
- active target highlighted;
- current target remains represented even when filters exclude it, via an
  “Active connection” compact summary.

- [ ] Write `tests/components/query-connection-navigator.test.tsx`.

Test names:

```text
groups targets by environment and cluster
searches by host engine environment and readiness
highlights the active target
keeps active connection summary visible when filtered out
selecting a target calls onSelect with resource id
```

- [ ] Modify `components/query/query-workbench.tsx`.

Required changes:

- remove old `TargetSwitcher`;
- render `QueryConnectionNavigator` in the left column;
- keep schema browser as secondary context below or beside navigator;
- keep editor center;
- keep governance compact right side or below on smaller screens;
- remove detached filter bar behavior.

- [ ] Update messages.

Add keys under `queryWorkbench.connectionNavigator` for:

```text
title
searchPlaceholder
activeConnection
ready
locked
noMatches
filters
allEngines
allModes
allReadiness
```

Add English and Chinese translations.

- [ ] Update E2E.

Replace target switcher helpers with connection navigator helpers. Required
real E2E:

```text
connection navigator searches and selects Local MySQL Query Dev
ready target still runs SELECT 1 after navigator selection
```

- [ ] Run targeted tests and commit.

Run:

```bash
npm run test -- tests/components/query-connection-navigator.test.tsx tests/components/query-workbench.test.tsx
npm run check:e2e-governance
npx tsc --noEmit
git add components/query/query-connection-navigator.tsx components/query/query-workbench.tsx messages/en.json messages/zh-CN.json tests/components/query-connection-navigator.test.tsx tests/components/query-workbench.test.tsx e2e/query-workbench.spec.ts
git commit -m "feat(query): replace target dropdown with connection navigator"
```

## Task G5: Credential Settings Inspector

**Purpose:** Replace bottom detail form with visible master-detail editing.

- [ ] Modify `components/settings/query-credential-settings.tsx`.

Required behavior:

- operations table and detail panel share a desktop two-column layout;
- selected row is highlighted with `aria-selected=true`;
- detail inspector is sticky on desktop;
- when no row is selected, inspector shows “Select a target” empty state;
- on small screens, detail opens in a drawer/dialog or a full-width panel
  directly after the selected row;
- existing stale-target guards remain in `CredentialDetailPanel`;
- save/delete success stays in the inspector.

- [ ] Update tests in `tests/components/query-credential-settings.test.tsx`.

Test names:

```text
selecting an operations row opens the detail inspector beside the table
selected operations row is highlighted
detail form is not appended after the full table on desktop
delete success remains visible in the inspector
```

- [ ] Update E2E in `e2e/query-credential-settings.spec.ts`.

Test name:

```text
selecting a credential target opens the visible inspector
```

Required checks:

- open credential settings as admin;
- click an operations row;
- assert inspector heading and form are visible without scrolling to page bottom;
- assert selected row is highlighted.

- [ ] Run targeted tests and commit.

Run:

```bash
npm run test -- tests/components/query-credential-settings.test.tsx
npx tsc --noEmit
git add components/settings/query-credential-settings.tsx tests/components/query-credential-settings.test.tsx e2e/query-credential-settings.spec.ts messages/en.json messages/zh-CN.json
git commit -m "fix(query): show credential detail in an inspector"
```

## Task G6: Placeholder Action Reduction

**Purpose:** Remove primary UI weight from unimplemented actions.

- [ ] Modify `components/query/query-editor-shell.tsx`.

Required behavior:

- primary toolbar contains Run and Format only;
- Explain, Export, Save sheet, and Access are not rendered as primary buttons;
- if retained, put them in compact disabled secondary text or a More menu with
  clear unavailable labels;
- locked state shows one primary blocker, not a row of unavailable buttons.

- [ ] Modify `components/query/query-governance-panel.tsx`.

Required behavior:

- governance panel shows primary blocker and compact badges;
- no large education cards for unimplemented future features;
- no duplicate active target facts.

- [ ] Add/update component tests.

Test names:

```text
shows only implemented primary actions in the worksheet toolbar
does not show export save sheet or access as primary buttons
locked target shows one primary blocker
```

- [ ] Run targeted tests and commit.

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
npx tsc --noEmit
git add components/query/query-editor-shell.tsx components/query/query-governance-panel.tsx tests/components/query-workbench.test.tsx messages/en.json messages/zh-CN.json
git commit -m "fix(query): reduce placeholder workbench actions"
```

## Task G7: Preview Tracking And Evidence

- [ ] Update backend preview issue tracking doc.

Modify:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
```

Required update:

- Phase 38F solved editor foundation but not full workbench usability;
- Phase 38G owns target navigation, dark readability, editor resize,
  credential inspector, placeholder action reduction.

- [ ] Add evidence note after implementation.

Create:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-08-phase-38g-query-workbench-real-usability-cleanup-evidence.md
```

Include:

- frontend commit range;
- changed files summary;
- real E2E result;
- dark mode manual/automated readability evidence;
- scope confirmation.

Commit backend docs separately after frontend implementation is complete and
verified.

## Final Verification

Run from frontend worktree:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Start real backend and fixture from backend repo:

```bash
cd /Users/fan/GolangProjects/ControlHub
make query-e2e-mysql-up
export DATABASE_DSN="$(grep '^DATABASE_DSN=' .env | sed 's/^DATABASE_DSN=//')"
set -a
. ./.query-e2e-mysql.env
set +a
QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO make seed-query-dev-target
```

Start backend on `:8080` with `DATABASE_DSN`, `JWT_SECRET`, and
`CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO` set. Do not print DSNs.

Run frontend E2E:

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/phase-38g-query-workbench-real-usability-cleanup
npm run test:e2e -- --grep "Query Workbench"
npm run test:e2e -- --grep "query credential"
```

Cleanup:

```bash
cd /Users/fan/GolangProjects/ControlHub
make query-e2e-mysql-down
```

Stop backend and confirm `:8080` is free.

## Final Report Requirements

Report:

- branch/worktree;
- commits;
- changed files;
- which preview issues were fixed;
- real E2E results;
- dark mode readability evidence;
- backend/fixture cleanup result;
- final git status;
- scope confirmation.

Do not claim completion if real E2E or dark-mode preview was skipped.
