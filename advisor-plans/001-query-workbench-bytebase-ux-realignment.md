# Plan 001: Query workbench and credential admin UX realignment

> **Executor instructions**: This plan is written as a design/implementation handoff. Do not implement all items in one large change. Split into focused frontend/backend phases, each with its own tests and review.
>
> **Drift check (run first)**:
> - Backend: `git -C /Users/fan/GolangProjects/ControlHub rev-parse --short HEAD` should be at or after `564930c`.
> - Frontend: `git -C /Users/fan/JsProjects/ControlHub rev-parse --short HEAD` should be at or after `e632e44`.
> - If the cited files below changed materially, re-review them before executing.

## Status

- **Priority**: P1
- **Effort**: L
- **Risk**: MED
- **Depends on**: none
- **Category**: direction / UX / bug
- **Planned at**: backend `564930c`, frontend `e632e44`, 2026-07-07

## Recorded Issue

The credential admin detail button looks broken:

- User-visible symptom: clicking `编辑凭证元数据` appears to do nothing.
- Reproduction evidence: selecting a target on `/settings/query-credentials` and clicking the button sends `PUT /__api/query-targets/{id}/credential` and receives `200`, but the UI shows no success state.
- Root cause in current frontend: `CredentialDetailPanel.handleSave()` only calls `setCredential(result)` after save. It does not show a saved/success message and does not notify the parent operations table to refresh.
- Wording mismatch: the button label says "edit credential metadata", but the behavior is "save current credential metadata".

Relevant current code:

- `/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx:368` mounts `CredentialDetailPanel` without an `onSaved`/`onDeleted` callback.
- `/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx:1536` defines `handleSave()`.
- `/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx:1548` calls `saveQueryCredential()`.
- `/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx:1550` only updates local detail state.
- `/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx:1819` renders `detail.editButton` for already-configured credentials.

Minimum fix:

- Rename the action to "保存凭证元数据" / "Save credential metadata".
- Show a visible saved state after successful PUT.
- Notify the parent to refresh the target's credential status and operations table row.
- Add component tests proving success feedback and parent refresh.

## Bytebase Findings

Inspected local Bytebase at `/Users/fan/GolangProjects/bytebase`.

### 1. Query target context is routeable, not just local dropdown state

Bytebase SQL editor routes encode context:

- `/sql-editor`
- `/sql-editor/projects/:project`
- `/sql-editor/projects/:project/instances/:instance/databases/:database`
- `/sql-editor/projects/:project/instances/:instance`
- `/sql-editor/projects/:project/sheets/:sheet`
- `/sql-editor/projects/:project/queryHistories/:queryHistory`

Evidence: `/Users/fan/GolangProjects/bytebase/frontend/src/react/router/routes/sqlEditor.tsx:32`.

Learning for ControlHub:

- The current `/query` route keeps target choice as local component state. This is fine for a prototype, but weak for shareable/debuggable workflows.
- Future target/worksheet selection should be encoded in URL query/path state, at least `targetId` and worksheet id.

### 2. Target selection is a connection pane/tree, not a small dropdown

Bytebase uses `ConnectionPane` with:

- `AdvancedSearch` scopes for `instance`, `label`, and `engine`.
- Environment-grouped database tree.
- Database and database-group selection modes.
- Missing-query-database toggle.
- Persisted expanded tree state per user/workspace.
- Read-only data source default for selection.

Evidence: `/Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/ConnectionPane/ConnectionPane.tsx:121`, `:176`, `:181`, `:202`.

Learning for ControlHub:

- A popover picker is better than the old dropdown, but still not enough for a real database IDE.
- Query target selection should become a left connection pane/drawer grouped by environment/cluster/engine, with current target shown as a compact breadcrumb/header.
- Filters belong inside the target picker/pane, not as separate visual blocks competing with the active target.

### 3. Worksheets/tabs are first-class

Bytebase has a persistent `SQLEditorTab` model:

- tab id/title/worksheet
- connection
- statement and selected statement
- mode (`WORKSHEET` / `ADMIN`)
- per-database query contexts/results
- tree/editor/view state

Evidence:

- `/Users/fan/GolangProjects/bytebase/frontend/src/types/sqlEditor/tab.ts:65`
- `/Users/fan/GolangProjects/bytebase/frontend/src/react/stores/sqlEditor/tab.ts:39`
- `/Users/fan/GolangProjects/bytebase/frontend/src/react/stores/sqlEditor/tab.ts:143`

Learning for ControlHub:

- One worksheet tied to one selected target is not enough.
- Add multiple worksheets/tabs with persisted draft state, per-tab target, and per-tab result/history state.

### 4. The editor is Monaco-based and operational

Bytebase uses Monaco editor with:

- SQL dialect/language derived from engine.
- Cmd/Ctrl+Enter run.
- Cmd/Ctrl+Shift+Enter run in new tab.
- Cmd/Ctrl+S save worksheet.
- format event through `formatEditorContent`.
- active statement under cursor/selection.

Evidence: `/Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/StandardPanel/SQLEditor.tsx:52`, `:90`, `:198`, `:230`.

Learning for ControlHub:

- Plain textarea/editor is below the bar for database workbench UX.
- Monaco + SQL formatting + keyboard shortcuts should be a P1 workbench improvement.

### 5. Query context lives next to Run

Bytebase puts data source selection and max row count in `QueryContextSettingPopover` next to the Run button:

- automatic query data source
- ADMIN / READ_ONLY data source options
- username display
- policy-based disabling of admin data source
- max row count

Evidence: `/Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/QueryContextSettingPopover.tsx:39`, `:72`, `:82`, `:116`, `:240`.

Learning for ControlHub:

- Runtime query settings should be next to Run, not buried in governance/status panels.
- For ControlHub, this should start as read-only display plus maxRows; later, if multiple credential refs/data sources exist, allow choosing among backend-authorized read-only refs there.

### 6. Credential/data source management is instance admin surface

Bytebase manages credentials as instance `DataSource` records:

- APIs: `AddDataSource`, `RemoveDataSource`, `UpdateDataSource`.
- Permission: `bb.instances.update`.
- Audit: enabled on data source mutations.
- Only READ_ONLY data sources can be added/removed separately.
- Password/TLS/secret fields are `INPUT_ONLY`.
- External secret integrations exist for Vault/AWS/GCP/Azure.

Evidence:

- `/Users/fan/GolangProjects/bytebase/proto/v1/v1/instance_service.proto:133`
- `/Users/fan/GolangProjects/bytebase/proto/v1/v1/instance_service.proto:361`
- `/Users/fan/GolangProjects/bytebase/proto/v1/v1/instance_service.proto:465`
- `/Users/fan/GolangProjects/bytebase/proto/v1/v1/instance_service.proto:544`
- `/Users/fan/GolangProjects/bytebase/proto/v1/v1/instance_service.proto:699`
- `/Users/fan/GolangProjects/bytebase/frontend/src/react/components/instance/DataSourceSection.tsx:22`
- `/Users/fan/GolangProjects/bytebase/frontend/src/react/stores/app/instance.ts:253`

Learning for ControlHub:

- Keeping credential metadata under Settings/Admin is the right direction.
- Query Workbench should not feel like a place where arbitrary users configure credentials.
- ControlHub's current ref-only model is safer than storing DSNs, but the UI should present it as admin-managed "read-only identity binding", not as query-page configuration.

## First-Principles UX Assessment

A query workbench has four jobs:

1. Find the correct data target quickly.
2. Compose and run a safe read-only statement.
3. Inspect schema/results/history with minimal friction.
4. Explain why execution is blocked and how to route the fix to the right admin.

Anything not serving those four jobs should be compressed, moved, or removed.

Current ControlHub issues:

- Target selection still feels like a dropdown/picker, not a database connection navigator. It does not scale to many clusters/instances/databases.
- Target facts are still duplicated around the page. Engine/environment/host/readiness belong in one compact target header or breadcrumb, not in multiple panels.
- Governance/status copy is conceptually important but visually over-weighted. Most of it should be badges/tooltips/details, with only true blockers shown inline.
- Schema browsing should be a primary left pane. Users should not need to type `SHOW TABLES` or `DESCRIBE` for basic discovery, even though those commands should work.
- The editor lacks database-IDE basics: syntax highlight, format SQL, keyboard shortcuts, active statement execution, multiple worksheets.
- Credential settings has operational intent, but mutation feedback is weak. A save button with no success state is perceived as broken.
- Admin credential UI should default to actionable views: missing metadata, secret missing, binding mismatch, invalid ref, policy blocked. Showing all unsupported/locked targets equally makes admins hunt for work.

## Recommended Execution Order

### Phase A: Fix credential admin interaction defects

Scope:

- `components/settings/query-credential-settings.tsx`
- `tests/components/query-credential-settings.test.tsx`
- `messages/en.json`
- `messages/zh-CN.json`

Changes:

- Rename `editButton` to save semantics.
- Add success feedback after PUT.
- Add parent callback from `CredentialDetailPanel` to refresh row status after save/delete.
- Ensure operation results and detail state cannot diverge.

Verification:

- `npm run test -- tests/components/query-credential-settings.test.tsx`
- `npm run test`
- Real E2E for `query credential` when backend is available.

### Phase B: Rework query target navigation into a connection pane

Scope:

- `components/query/query-workbench.tsx`
- `components/query/query-schema-browser.tsx`
- query workbench tests and i18n

Changes:

- Replace top-heavy target picker with a left connection pane/drawer.
- Group by environment/cluster/engine.
- Keep active target as a compact breadcrumb/header.
- Move filters into the pane.
- Encode active target in URL query state.

Verification:

- Component tests for search/group/filter/selection.
- E2E for selecting target by name/engine/host/env.

### Phase C: Upgrade SQL editor ergonomics

Scope:

- `components/query/query-editor-shell.tsx`
- new editor abstraction if needed
- tests and e2e

Changes:

- Introduce Monaco-based SQL editor or equivalent.
- Add syntax highlighting, SQL formatting, active statement execution, Cmd/Ctrl+Enter.
- Add multiple worksheet tabs with persisted drafts and per-tab target/result/history isolation.

Verification:

- Unit/component tests for tab isolation and formatting command.
- E2E for multiple worksheets and active statement execution.

### Phase D: Make schema/results operational

Scope:

- `components/query/query-schema-browser.tsx`
- result grid/history components

Changes:

- Make schema/object browser the primary discovery path.
- Add table/view tree and actions to insert/select/describe.
- Keep `SHOW DATABASES`, `SHOW TABLES`, `DESCRIBE` supported, but do not rely on them as the main UI path.
- Improve result grid with virtualized rows and copy affordances before adding exports.

Verification:

- Real E2E against dedicated query MySQL fixture.
- Tests for schema action inserting SQL into the active worksheet.

## Non-Goals

- Do not put DSN/password fields in the Query Workbench.
- Do not weaken backend credential binding or SQL guard.
- Do not add arbitrary write/admin SQL mode to the normal query flow.
- Do not copy Bytebase wholesale; use it as evidence for information architecture, not visual skin.

## Done Criteria

- The recorded credential-save issue has an automated regression test and visible success feedback.
- Query target selection scales beyond a flat dropdown/picker.
- Target identity appears once, not duplicated across panels.
- Governance is compact and blocker-focused.
- Query editor supports syntax highlight, formatting, shortcuts, and multiple worksheets.
- Real E2E passes against backend + dedicated query MySQL fixture.

## STOP Conditions

- If implementing target navigation requires backend shape changes, stop and write a backend/frontend contract plan first.
- If adding Monaco significantly increases bundle/build instability, stop and do a spike branch before product integration.
- If credential settings begins to require real secret entry, stop and design a backend secret-manager/INPUT_ONLY contract; do not add browser-side DSN/password storage ad hoc.
