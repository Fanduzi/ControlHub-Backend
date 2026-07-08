# Phase 38F Query Workbench SQL Editor Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the plain Query Workbench textarea with a local multi-worksheet SQL editor foundation.

**Architecture:** Frontend-only phase. Introduce a client-only CodeMirror wrapper, a small SQL formatting helper, and a worksheet state model inside `QueryEditorShell`; lift target synchronization between `QueryWorkbench` and `QueryEditorShell` so schema/governance follow the active worksheet.

**Tech Stack:** Next.js 16, React 19, TypeScript, next-intl, `@uiw/react-codemirror`, `@codemirror/lang-sql`, `sql-formatter`, Vitest, Playwright.

---

## Scope

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repo is docs source only for this plan. Do not edit backend product code.

## Required Reading

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-08-phase-38f-query-workbench-sql-editor-foundation.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-07-phase-38e-query-workbench-ia-and-admin-feedback.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

## Files

Expected frontend files:

- Modify: `package.json`
- Modify: `package-lock.json`
- Create: `components/query/sql-code-editor.tsx`
- Create: `components/query/sql-code-editor-client.tsx`
- Create: `lib/query-sql-format.ts`
- Create: `tests/lib/query-sql-format.test.ts`
- Modify: `components/query/query-editor-shell.tsx`
- Modify: `components/query/query-workbench.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Modify: `tests/components/query-workbench.test.tsx`
- Modify: `e2e/query-workbench.spec.ts`

Out of scope:

- Backend files under `/Users/fan/GolangProjects/ControlHub/internal`
- `services/query-executions.ts` unless a failing test proves request shape was changed
- `services/query-credentials.ts`
- credential settings UI
- saved query/export/approval workflows

## Task F0: Dependencies

**Purpose:** Add only the packages required for editor/highlighting/formatting.

- [ ] **Step 1: Install dependencies**

Run:

```bash
npm install @uiw/react-codemirror @codemirror/lang-sql sql-formatter
```

Expected:

- `package.json` gains the three dependencies;
- `package-lock.json` updates;
- no unrelated packages are added.

- [ ] **Step 2: Verify package diff**

Run:

```bash
git diff -- package.json package-lock.json
```

Expected: only CodeMirror and SQL formatter dependency changes.

Suggested commit:

```bash
git add package.json package-lock.json
git commit -m "chore: add sql editor dependencies"
```

## Task F1: SQL Formatting Helper

**Purpose:** Keep formatting logic pure and testable outside React.

- [ ] **Step 1: Write failing tests**

Create `tests/lib/query-sql-format.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import {
  formatQueryStatement,
  formatterLanguageForEngine,
} from "@/lib/query-sql-format";

describe("query SQL formatter", () => {
  it("uses mysql formatting for mysql targets", () => {
    expect(formatterLanguageForEngine("mysql")).toBe("mysql");
  });

  it("uses mysql formatting for tidb targets", () => {
    expect(formatterLanguageForEngine("tidb")).toBe("mysql");
  });

  it("falls back to generic sql for unknown engines", () => {
    expect(formatterLanguageForEngine("clickhouse")).toBe("sql");
  });

  it("formats a messy readonly statement", () => {
    const result = formatQueryStatement("select id,name from query_e2e_items where id=1", "mysql");

    expect(result.ok).toBe(true);
    if (result.ok) {
      expect(result.value).toContain("SELECT");
      expect(result.value).toContain("FROM query_e2e_items");
      expect(result.value).toContain("WHERE id = 1");
    }
  });

  it("keeps an empty statement unchanged", () => {
    expect(formatQueryStatement("   ", "mysql")).toEqual({ ok: true, value: "   " });
  });

  it("returns a controlled error when the formatter throws", () => {
    const result = formatQueryStatement("select * from table", "mysql", () => {
      throw new Error("formatter failed");
    });

    expect(result.ok).toBe(false);
    if (!result.ok) {
      expect(result.message).toMatch(/format/i);
    }
  });
});
```

Run:

```bash
npm run test -- tests/lib/query-sql-format.test.ts
```

Expected: FAIL because `@/lib/query-sql-format` does not exist.

- [ ] **Step 2: Implement helper**

Create `lib/query-sql-format.ts`:

```ts
import { format, type SqlLanguage } from "sql-formatter";

type FormatImpl = (
  statement: string,
  options: {
    language: SqlLanguage;
    keywordCase: "upper";
    tabWidth: number;
    linesBetweenQueries: number;
  },
) => string;

export type QueryFormatResult =
  | { ok: true; value: string }
  | { ok: false; message: string };

export function formatterLanguageForEngine(engine: string): SqlLanguage {
  const normalized = engine.trim().toLowerCase();
  if (normalized === "mysql" || normalized === "tidb") {
    return "mysql";
  }
  return "sql";
}

export function formatQueryStatement(
  statement: string,
  engine: string,
  formatImpl: FormatImpl = format,
): QueryFormatResult {
  if (statement.trim() === "") {
    return { ok: true, value: statement };
  }

  try {
    return {
      ok: true,
      value: formatImpl(statement, {
        language: formatterLanguageForEngine(engine),
        keywordCase: "upper",
        tabWidth: 2,
        linesBetweenQueries: 1,
      }),
    };
  } catch {
    return {
      ok: false,
      message: "SQL formatting failed. Check the statement syntax and try again.",
    };
  }
}
```

- [ ] **Step 3: Verify helper**

Run:

```bash
npm run test -- tests/lib/query-sql-format.test.ts
```

Expected: PASS.

Suggested commit:

```bash
git add lib/query-sql-format.ts tests/lib/query-sql-format.test.ts
git commit -m "feat: add query sql formatter helper"
```

## Task F2: Client-Only CodeMirror Wrapper

**Purpose:** Add syntax highlighting without SSR/hydration risk.

- [ ] **Step 1: Add a test mock pattern**

In `tests/components/query-workbench.test.tsx`, add a top-level mock before
component imports if the file does not already mock the editor:

```ts
vi.mock("@/components/query/sql-code-editor", () => ({
  SqlCodeEditor: ({
    value,
    onChange,
    onRunShortcut,
    onFormatShortcut,
    ariaLabel,
  }: {
    value: string;
    onChange: (value: string) => void;
    onRunShortcut: () => void;
    onFormatShortcut: () => void;
    ariaLabel: string;
  }) => (
    <textarea
      aria-label={ariaLabel}
      value={value}
      onChange={(event) => onChange(event.currentTarget.value)}
      onKeyDown={(event) => {
        if ((event.metaKey || event.ctrlKey) && event.key === "Enter") {
          onRunShortcut();
        }
        if ((event.metaKey || event.ctrlKey) && event.shiftKey && event.key.toLowerCase() === "f") {
          onFormatShortcut();
        }
      }}
    />
  ),
}));
```

This keeps component tests deterministic while E2E exercises the real browser
editor.

- [ ] **Step 2: Implement dynamic wrapper**

Create `components/query/sql-code-editor.tsx`:

```tsx
"use client";

import dynamic from "next/dynamic";

import { cn } from "@/lib/utils";

const SqlCodeEditorClient = dynamic(
  () =>
    import("@/components/query/sql-code-editor-client").then(
      (module) => module.SqlCodeEditorClient,
    ),
  {
    ssr: false,
    loading: () => (
      <div className="min-h-[220px] rounded-lg border border-border bg-muted/20 p-3 font-mono text-sm text-muted-foreground">
        Loading SQL editor...
      </div>
    ),
  },
);

export type SqlCodeEditorProps = {
  value: string;
  onChange: (value: string) => void;
  onRunShortcut: () => void;
  onFormatShortcut: () => void;
  ariaLabel: string;
  disabled?: boolean;
  className?: string;
};

export function SqlCodeEditor(props: SqlCodeEditorProps) {
  return (
    <div className={cn("overflow-hidden rounded-lg border border-border", props.className)}>
      <SqlCodeEditorClient {...props} />
    </div>
  );
}
```

Create `components/query/sql-code-editor-client.tsx`:

```tsx
"use client";

import CodeMirror from "@uiw/react-codemirror";
import { sql, MySQL } from "@codemirror/lang-sql";
import { keymap } from "@codemirror/view";
import type { Extension } from "@codemirror/state";

import type { SqlCodeEditorProps } from "@/components/query/sql-code-editor";

function buildEditorExtensions({
  onRunShortcut,
  onFormatShortcut,
}: Pick<SqlCodeEditorProps, "onRunShortcut" | "onFormatShortcut">): Extension[] {
  return [
    sql({ dialect: MySQL }),
    keymap.of([
      {
        key: "Mod-Enter",
        run: () => {
          onRunShortcut();
          return true;
        },
      },
      {
        key: "Mod-Shift-f",
        run: () => {
          onFormatShortcut();
          return true;
        },
      },
    ]),
  ];
}

export function SqlCodeEditorClient({
  value,
  onChange,
  onRunShortcut,
  onFormatShortcut,
  ariaLabel,
  disabled = false,
}: SqlCodeEditorProps) {
  return (
    <CodeMirror
      value={value}
      minHeight="220px"
      basicSetup={{
        lineNumbers: true,
        foldGutter: true,
        highlightActiveLine: true,
        bracketMatching: true,
        closeBrackets: true,
        autocompletion: false,
      }}
      editable={!disabled}
      readOnly={disabled}
      indentWithTab={false}
      extensions={buildEditorExtensions({ onRunShortcut, onFormatShortcut })}
      aria-label={ariaLabel}
      onChange={(nextValue) => onChange(nextValue)}
    />
  );
}
```

- [ ] **Step 3: Verify wrapper compiles**

Run:

```bash
npx tsc --noEmit
npm run build
```

Expected: both pass.

Suggested commit:

```bash
git add components/query/sql-code-editor.tsx components/query/sql-code-editor-client.tsx tests/components/query-workbench.test.tsx
git commit -m "feat: add sql code editor wrapper"
```

## Task F3: Worksheet State Model

**Purpose:** Replace single editor state with local worksheet tabs.

- [ ] **Step 1: Write failing component tests**

In `tests/components/query-workbench.test.tsx`, add tests:

```ts
it("creates a second local worksheet with its own default statement", async () => {
  // Render with a ready target.
  // Click Add worksheet.
  // Expect tabs "Worksheet 1" and "Worksheet 2".
  // Expect active editor value "select 1".
});

it("keeps worksheet statements isolated when switching tabs", async () => {
  // Edit Worksheet 1 to "select 111".
  // Add Worksheet 2 and edit to "select 222".
  // Switch back to Worksheet 1 and expect "select 111".
});

it("renames the active worksheet locally", async () => {
  // Rename Worksheet 1 to "Orders lookup".
  // Expect tab label "Orders lookup".
});

it("closes a worksheet without closing the last remaining worksheet", async () => {
  // Add Worksheet 2.
  // Close Worksheet 2.
  // Expect Worksheet 1 remains.
  // Expect no close button when only one worksheet remains.
});
```

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: FAIL because worksheet controls do not exist.

- [ ] **Step 2: Add worksheet model in `query-editor-shell.tsx`**

Modify `QueryEditorShell` props:

```ts
type QueryEditorShellProps = {
  targets: QueryTarget[];
  activeTarget: QueryTarget;
  onActiveTargetChange: (resourceId: number) => void;
};
```

Add local model:

```ts
type LocalWorksheet = {
  id: string;
  name: string;
  targetResourceId: number;
  statement: string;
  maxRows: number;
  activeResultTab: ResultTab;
  isExecuting: boolean;
  result: QueryExecuteResponse | null;
  error: QueryExecuteError | null;
  formatError: string | null;
  history: QueryExecutionRecord[];
  historyLoading: boolean;
};

function createWorksheet(index: number, targetResourceId: number): LocalWorksheet {
  return {
    id: `worksheet-${Date.now()}-${index}`,
    name: `Worksheet ${index}`,
    targetResourceId,
    statement: DEFAULT_STATEMENT,
    maxRows: DEFAULT_MAX_ROWS,
    activeResultTab: "grid",
    isExecuting: false,
    result: null,
    error: null,
    formatError: null,
    history: [],
    historyLoading: false,
  };
}
```

Initialize:

```ts
const [worksheets, setWorksheets] = useState<LocalWorksheet[]>(() => [
  createWorksheet(1, activeTarget.resourceId),
]);
const [activeWorksheetId, setActiveWorksheetId] = useState(worksheets[0]!.id);
```

Add immutable helpers:

```ts
function updateActiveWorksheet(patch: Partial<LocalWorksheet>) {
  setWorksheets((previous) =>
    previous.map((worksheet) =>
      worksheet.id === activeWorksheetId ? { ...worksheet, ...patch } : worksheet,
    ),
  );
}
```

Render a worksheet tab bar above `ReadyWorksheet` with:

- tab buttons named by worksheet `name`;
- add button;
- rename button or inline input;
- close button only when `worksheets.length > 1`.

Do not persist worksheets to storage.

- [ ] **Step 3: Verify worksheet tests**

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: new worksheet tests pass. If existing target-switch tests fail because
the old `key={resourceId}` remount boundary is being removed, do not commit a
partially failing state. Continue directly into Task F4 and commit the worksheet
model together with the target synchronization fix.

## Task F4: Per-Worksheet Target Synchronization

**Purpose:** Let each worksheet own a target while schema/governance remain accurate.

- [ ] **Step 1: Write failing target-isolation tests**

In `tests/components/query-workbench.test.tsx`, add tests:

```ts
it("updates the active worksheet target when the target navigator changes", async () => {
  // Start Worksheet 1 on ready target A.
  // Change target navigator to target B.
  // Expect editor keeps Worksheet 1 active but schema/governance show B.
  // Expect previous result/error/history for Worksheet 1 cleared.
});

it("switching worksheets restores each worksheet target in schema and governance", async () => {
  // Worksheet 1 target A, Worksheet 2 target B.
  // Switch between worksheet tabs.
  // Expect target header/schema/governance text follows A then B.
});

it("does not paint worksheet A execution result into worksheet B", async () => {
  // Start run in Worksheet A.
  // Switch to Worksheet B before promise resolves.
  // Resolve A.
  // Expect B does not show A result.
});
```

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: FAIL until synchronization is implemented.

- [ ] **Step 2: Lift target contract in `query-workbench.tsx`**

Change the editor render from:

```tsx
<QueryEditorShell key={activeTarget.resourceId} target={activeTarget} />
```

to:

```tsx
<QueryEditorShell
  targets={targets}
  activeTarget={activeTarget}
  targetSelectionVersion={targetSelectionVersion}
  onActiveTargetChange={setActiveTargetFromWorksheet}
/>
```

Add source-aware target setters in `QueryWorkbench`:

```ts
const [targetSelectionVersion, setTargetSelectionVersion] = useState(0);

function setActiveTargetFromNavigator(resourceId: number) {
  setActiveTargetId(resourceId);
  setTargetSelectionVersion((version) => version + 1);
}

function setActiveTargetFromWorksheet(resourceId: number) {
  setActiveTargetId(resourceId);
}
```

Pass `setActiveTargetFromNavigator` to `TargetSwitcher`. Remove the
`key={activeTarget.resourceId}` remount. The new worksheet model owns target
isolation.

- [ ] **Step 3: Implement synchronization in `QueryEditorShell`**

Build a target map:

```ts
const targetsById = useMemo(
  () => new Map(targets.map((candidate) => [candidate.resourceId, candidate])),
  [targets],
);
```

When the user changes the target navigator, update the active worksheet target
and clear target-owned fields. Key the effect off `targetSelectionVersion`, not
only `activeTarget.resourceId`, so worksheet switches that restore a saved target
do not clear that worksheet's result/history:

```ts
useEffect(() => {
  updateActiveWorksheet({
    targetResourceId: activeTarget.resourceId,
    result: null,
    error: null,
    history: [],
    historyLoading: false,
    isExecuting: false,
    activeResultTab: "grid",
  });
}, [targetSelectionVersion]);
```

When active worksheet changes, sync parent target:

```ts
useEffect(() => {
  const worksheet = worksheets.find((candidate) => candidate.id === activeWorksheetId);
  if (worksheet && worksheet.targetResourceId !== activeTarget.resourceId) {
    onActiveTargetChange(worksheet.targetResourceId);
  }
}, [activeWorksheetId, activeTarget.resourceId, onActiveTargetChange, worksheets]);
```

Guard async work by both worksheet id and target id:

```ts
const activeRequestRef = useRef({ worksheetId: activeWorksheetId, targetId: activeTarget.resourceId });
activeRequestRef.current = { worksheetId: activeWorksheetId, targetId: activeTarget.resourceId };
```

Before writing result/error/history/loading state after async work, verify both
ids still match.

- [ ] **Step 4: Verify synchronization**

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: worksheet target-isolation tests pass and old target stale-guard
regressions still pass with updated expectations. In particular, switching from
worksheet A to worksheet B must restore B's prior result/history instead of
clearing it as a side effect of parent active-target synchronization.

Suggested commit:

```bash
git add components/query/query-workbench.tsx components/query/query-editor-shell.tsx tests/components/query-workbench.test.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: add local query worksheets"
```

## Task F5: Format And Shortcut UI

**Purpose:** Add visible and keyboard access to format/run.

- [ ] **Step 1: Write failing tests**

In `tests/components/query-workbench.test.tsx`, add tests:

```ts
it("formats only the active worksheet statement", async () => {
  // Worksheet 1 messy SQL; Worksheet 2 different SQL.
  // Click Format in Worksheet 1.
  // Expect Worksheet 1 formatted and Worksheet 2 unchanged.
});

it("runs the active worksheet with Cmd Enter", async () => {
  // Focus editor.
  // Press meta+Enter.
  // Expect executeQueryTarget called with active worksheet statement and maxRows.
});

it("formats the active worksheet with Cmd Shift F", async () => {
  // Focus editor.
  // Press meta+shift+f.
  // Expect statement formatted.
});

it("does not run via shortcut when run is locked", async () => {
  // Render locked target.
  // Press meta+Enter.
  // Expect executeQueryTarget not called.
});
```

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: FAIL until UI actions are wired.

- [ ] **Step 2: Wire editor and formatter**

In `ReadyWorksheet`, replace the `<textarea>` with:

```tsx
<SqlCodeEditor
  value={statement}
  onChange={onStatementChange}
  onRunShortcut={onRun}
  onFormatShortcut={onFormat}
  ariaLabel={t("editor.statementLabel")}
  disabled={isExecuting}
/>
```

Add a Format button near Run:

```tsx
<Button type="button" size="sm" variant="outline" onClick={onFormat} disabled={isExecuting}>
  {t("editor.format")}
</Button>
```

In the shell handler:

```ts
function handleFormat() {
  const worksheet = activeWorksheet;
  const target = targetsById.get(worksheet.targetResourceId);
  const formatted = formatQueryStatement(
    worksheet.statement,
    target?.connectionContext.engine ?? "sql",
  );
  if (formatted.ok) {
    updateActiveWorksheet({ statement: formatted.value, formatError: null });
  } else {
    updateActiveWorksheet({ formatError: formatted.message });
  }
}
```

Render `formatError` as a controlled alert near the editor.

Add i18n:

```json
"format": "Format",
"formatShortcut": "Cmd/Ctrl+Shift+F",
"formatError": "SQL formatting failed. Check the statement syntax and try again.",
"runShortcut": "Cmd/Ctrl+Enter"
```

Chinese:

```json
"format": "格式化",
"formatShortcut": "Cmd/Ctrl+Shift+F",
"formatError": "SQL 格式化失败。请检查语句语法后重试。",
"runShortcut": "Cmd/Ctrl+Enter"
```

- [ ] **Step 3: Verify actions**

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: format/shortcut tests pass.

Suggested commit:

```bash
git add components/query/query-editor-shell.tsx components/query/sql-code-editor.tsx components/query/sql-code-editor-client.tsx lib/query-sql-format.ts messages/en.json messages/zh-CN.json tests/components/query-workbench.test.tsx
git commit -m "feat: add query worksheet format and shortcuts"
```

## Task F6: History And Result Isolation

**Purpose:** Ensure multi-worksheet execution cannot leak state across tabs.

- [ ] **Step 1: Write failing tests**

In `tests/components/query-workbench.test.tsx`, add tests:

```ts
it("keeps worksheet histories isolated", async () => {
  // Worksheet 1 history returns statement A.
  // Worksheet 2 history returns statement B.
  // Switch between worksheets and expect each history tab shows only its own target history.
});

it("refreshes only the active worksheet history after execution settles", async () => {
  // Execute Worksheet 1.
  // Expect history refreshed for Worksheet 1 target only.
});

it("keeps worksheet result tabs independent", async () => {
  // Worksheet 1 switches result tab to JSON.
  // Worksheet 2 remains grid.
});
```

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: FAIL if any state remains global.

- [ ] **Step 2: Store all result/history fields per worksheet**

Move these fields out of component-level state and into `LocalWorksheet`:

- `activeResultTab`
- `isExecuting`
- `result`
- `error`
- `history`
- `historyLoading`

Update handlers to patch only the active worksheet by id.

Ensure `refreshHistory` accepts `(worksheetId: string, targetId: number)` and
only writes back when both are still current.

- [ ] **Step 3: Verify isolation**

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: all Query Workbench component tests pass.

Suggested commit:

```bash
git add components/query/query-editor-shell.tsx tests/components/query-workbench.test.tsx
git commit -m "test: cover query worksheet state isolation"
```

## Task F7: Real E2E

**Purpose:** Prove the browser uses the real editor against the real backend.

- [ ] **Step 1: Update E2E spec**

In `e2e/query-workbench.spec.ts`, add or update tests:

- ready target runs `SELECT 1` from CodeMirror editor;
- ready target runs `SHOW TABLES`;
- Format turns `select id,name from query_e2e_items where id=1` into visible
  formatted SQL containing `SELECT`, `FROM query_e2e_items`, and `WHERE id = 1`;
- `Cmd/Ctrl+Enter` runs the active worksheet;
- two worksheets keep separate statements and results;
- switching worksheets restores target context;
- unsafe SQL remains rejected.

- [ ] **Step 2: Run local gates**

Run:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Expected: all pass.

- [ ] **Step 3: Run real E2E with backend fixture**

Use the existing Phase 37H/38D backend fixture flow:

```bash
# Backend repo:
make query-e2e-mysql-up
# Start backend on :8080 with DATABASE_DSN, JWT_SECRET, and CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO set.
# Seed ready target using make seed-query-dev-target.

# Frontend repo:
npm run test:e2e -- --grep "Query Workbench"
```

Expected:

- no skipped query workbench tests;
- ready target SELECT/SHOW/DESCRIBE tests pass;
- new format/shortcut/multi-worksheet tests pass;
- unsafe SQL remains controlled.

Suggested commit:

```bash
git add e2e/query-workbench.spec.ts
git commit -m "test: cover query worksheet editor e2e"
```

## Final Verification

Run from frontend repo:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
npm run test:e2e -- --grep "Query Workbench"
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Frontend
```

If backend is unavailable, do not claim E2E passed. Report the blocker and the
exact command that remains.

## Final Report Requirements

Include:

- branch/worktree;
- commits;
- dependency additions;
- editor/highlighting behavior;
- formatter behavior and no-server guarantee;
- shortcut behavior;
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
