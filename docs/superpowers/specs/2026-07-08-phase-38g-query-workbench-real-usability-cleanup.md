# Phase 38G Query Workbench Real Usability Cleanup Design

## Background

Phase 38F delivered the SQL editor foundation: CodeMirror, local formatting,
keyboard execution, and multiple local worksheets. Real E2E passed, but manual
preview showed that the page still feels like a framework demo rather than a
database workbench.

The current problems are product-level usability failures:

- the query target selector is still perceived as a dropdown, not a database
  connection navigator;
- CodeMirror and result output are visually broken in dark mode;
- the SQL editor height is fixed and cannot be adjusted for real work;
- query credential administration shows the selected target detail at the
  bottom of the page instead of a master-detail inspector;
- Query Workbench still gives prominent space to actions that are not actually
  implemented;
- previous “fixed” preview issues are not fully closed from the user’s point of
  view.

Bytebase is the reference direction, not a component source to copy. Bytebase’s
SQL Editor uses a connection pane and database tree as workbench navigation, not
a single select dropdown. It also treats the editor as the central work surface,
with connection/schema context and result panels around it.

## Goal

Turn the current Query Workbench and Query Credential Settings surfaces from
“feature scaffolding” into a usable read-only SQL workbench.

Phase 38G must fix visible usability blockers and reduce placeholder UI. It must
not add new fake features.

## Non-Goals

- No backend product code changes.
- No SQL guard changes.
- No new query engines.
- No saved query persistence.
- No worksheet persistence beyond editor-height/theme preferences.
- No export implementation.
- No approval/JIT implementation.
- No Explain implementation unless the backend already supports it and the UI
  can prove the request works end-to-end; otherwise hide or demote it.
- No credential edit controls inside `/query`.
- No browser state, request body, logs, or display for DSN/password.
- No `actorUserId` in any request.
- No CI workflow change.
- No tag, release, or deploy.

## Product Principles

1. **Displayed primary controls must work.** If a feature is not implemented,
   it must not appear as a primary action.
2. **The editor is the work surface.** The SQL editor and result table get
   priority over education cards and passive status blocks.
3. **Connection context is navigation, not a form field.** Query targets should
   be browsed and searched like connections/databases.
4. **Admin metadata editing is master-detail.** Selecting a row should reveal an
   adjacent inspector or drawer, not append a form below a long table.
5. **Dark mode must be usable, not just themed elsewhere.** Editor text, cursor,
   selection, gutter, result grid, and empty/error states must be readable.

## Bytebase Learnings

Relevant local reference files in `/Users/fan/GolangProjects/bytebase`:

- `frontend/src/react/components/sql-editor/SQLEditorLayout.tsx`
- `frontend/src/react/components/sql-editor/ConnectionPane/ConnectionPane.tsx`
- `frontend/src/react/components/sql-editor/ConnectionPane/TreeNode/DatabaseNode.tsx`
- `frontend/src/react/components/sql-editor/StandardPanel/SQLEditor.tsx`
- `frontend/src/react/components/sql-editor/ResultView/ResultView.tsx`

Useful patterns:

- SQL editor route is a full-height workbench shell, not a card stack.
- Connection selection is a left connection pane with grouped searchable tree
  nodes.
- Database rows show engine icon, instance, database, and permission/request
  affordances inline.
- Editor theme is controlled as part of SQL Editor theme scope.
- Result rendering is a first-class workbench panel.

Patterns not to copy in Phase 38G:

- full Monaco migration;
- batch query mode;
- permission request workflow;
- saved worksheet backend model;
- large enterprise-only feature gates.

## Requirements

### G1. CodeMirror Dark Mode And Readability

CodeMirror must follow the application theme.

Required behavior:

- dark mode editor background is dark;
- text, SQL tokens, cursor, selection, active line, gutter, and line numbers are
  readable in dark mode;
- light mode remains readable;
- result table background/text also follows theme and stays readable;
- no white-on-white or white-on-near-white state in dark mode;
- no SSR hydration mismatch from theme detection.

Implementation direction:

- use CodeMirror theme extensions instead of relying on default styles;
- support at least `system`, `light`, `dark`, and `high_contrast` editor theme
  values internally;
- Phase 38G may expose a small editor theme selector only if it is cheap and
  does not distract from the main cleanup;
- if no selector is exposed, default to app theme and keep the type boundary
  ready for a future selector.

### G2. Resizable SQL Editor Height With Local Preference

The worksheet editor height must be adjustable.

Required behavior:

- user can resize the SQL editor vertically;
- height is clamped to a safe range, for example 180px to 640px;
- chosen height persists in `localStorage`;
- invalid stored values are ignored;
- resizing does not affect statement/result state;
- preference is local-only and not synced to backend.

This is the only persistence allowed in Phase 38G besides optional editor theme
preference. Worksheet SQL text and results must remain non-persistent.

### G3. Connection Navigator Replaces Target Dropdown

The query target surface must stop feeling like a single dropdown.

Required behavior:

- replace the large target dropdown with a workbench connection navigator;
- desktop layout: left-side connection panel or persistent connection rail;
- mobile layout: drawer/sheet opened by a “Connections” button;
- targets grouped by environment and cluster;
- ready targets sorted or visually prioritized;
- search matches display name, resource name, engine, environment, host, port,
  cluster, and readiness;
- filters live inside the connection navigator, not as detached controls;
- active worksheet target is highlighted in the navigator;
- active connection summary is compact and always visible above the editor;
- no duplicate engine/environment/host/readiness blocks in the governance panel.

Acceptance criteria:

- a user can discover `Local MySQL Query Dev` without scanning a cramped
  dropdown;
- selecting a connection updates schema, governance, and active worksheet target;
- filtering the navigator never makes the current worksheet target disappear
  from the editor context.

### G4. Credential Settings Master-Detail Inspector

Query Credential Settings must use a master-detail layout.

Required behavior:

- clicking an operations-table target selects the row and opens a detail
  inspector adjacent to the table on desktop;
- selected row is visibly highlighted;
- detail inspector remains visible while scrolling the table;
- small screens use a drawer/modal detail surface;
- the detail form no longer appears at the bottom after a long list;
- save/delete success feedback remains visible in the detail surface;
- stale-target guards from Phase 38A/38E remain intact.

Acceptance criteria:

- clicking a row has an immediate visible response above the fold on desktop;
- user does not need to scroll to the bottom to find the edit form.

### G5. Placeholder Action Reduction

Query Workbench must stop presenting unimplemented features as primary actions.

Required behavior:

- Run remains the primary action when available;
- Format remains available because it is implemented;
- Explain, Export, Save sheet, and Access must not occupy primary toolbar space
  unless they are actually implemented end-to-end;
- unimplemented actions may appear in a compact “More” menu, disabled with
  short “Not available yet” text, or be removed entirely;
- saved-query/export/approval functionality is not implemented in Phase 38G;
- governance panel should show the primary blocker and compact status badges,
  not large permanent education blocks.

### G6. Preview Issue Tracking Update

Update the preview issue tracking document to state that Phase 38F solved the
technical editor foundation but did not fully solve the workbench usability.

Document new Phase 38G ownership for:

- target dropdown still not adequate;
- editor/result dark mode readability;
- editor resize and height preference;
- credential detail placement;
- placeholder action clutter;
- CodeMirror theme setting as optional follow-up if not exposed in Phase 38G.

## Test Requirements

Component tests:

- editor theme extension chooses dark/light/high-contrast classes or extensions;
- localStorage editor height reads valid values and rejects invalid values;
- resize handler clamps height and persists it;
- connection navigator groups by environment/cluster and highlights active
  worksheet target;
- connection search matches host, engine, environment, and readiness;
- credential row click opens adjacent inspector and highlights the row;
- unimplemented toolbar actions are hidden or demoted from primary toolbar;
- no credential edit controls appear on `/query`;
- no DSN/password/actorUserId fields are added to requests.

Real browser/E2E tests:

- dark mode Query Workbench editor text is visible after typing SQL;
- dark mode result table text is visible after running `SELECT 1`;
- editor resize persists after reload;
- connection navigator can search and select `Local MySQL Query Dev`;
- ready target still runs `SELECT 1`, `SHOW TABLES`, and `DESCRIBE`;
- credential settings row click opens visible inspector without scrolling to
  page bottom.

Manual preview requirement:

- capture or inspect the page in dark mode before final report;
- final report must explicitly state whether the editor/result readability was
  checked in a real browser.

## Acceptance Criteria

- Query Workbench no longer looks like a card stack with a target dropdown on
  top.
- SQL editor and result table are readable in dark mode.
- SQL editor height can be changed and is remembered locally.
- Credential settings row selection opens a visible inspector, not a bottom form.
- Primary Query Workbench toolbar contains only actions that work now.
- Real E2E passes against backend `:8080` and dedicated query MySQL fixture.
- No backend product code is touched.
