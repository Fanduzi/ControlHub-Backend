# Phase 36 Bytebase UI Research Notes

## Context

ControlHub is moving toward a governed Query Workbench. The first phase must not execute queries, but the UI should already feel like a real query workspace rather than a database inventory table.

Bytebase was reviewed locally from `/Users/fan/GolangProjects/bytebase`, focused on:

- `frontend/src/react/router/routes/sqlEditor.tsx`
- `frontend/src/react/components/sql-editor/`
- `frontend/src/types/sqlEditor/`
- `frontend/tests/e2e/sql-editor/`

This note records product learnings only. It is not a plan to copy Bytebase implementation or styling.

## Bytebase Capabilities Worth Preserving Conceptually

1. **SQL editor as a workspace**
   - Bytebase routes SQL Editor as its own top-level workspace with project, instance, database, worksheet, and query-history deep links.
   - ControlHub Phase 36 should therefore preview `/query` or `/databases/query` as a workbench shell, not as a flat capability inventory page.

2. **Connection and schema are first-class**
   - Bytebase has connection chooser, environment/grouped database tree, schema pane, table/column metadata, schema sync, and hover details.
   - ControlHub Phase 36 should show target selection plus schema/object browsing placeholders even before query execution exists.

3. **Worksheet lifecycle matters**
   - Bytebase models tabs, dirty/clean/saving state, saved sheets, sharing, and auto-save.
   - ControlHub Phase 36 should display worksheet tabs and saved-sheet placeholders, but keep save/share non-functional unless backed by real API later.

4. **Execution controls are policy-aware**
   - Bytebase action bar includes Run, row limit, query context, Admin mode, Save, Share, chooser group, and AI assist.
   - ControlHub Phase 36 should show Run/Explain/Export as locked controls, with a visible reason: missing read-only credential, missing guard, missing audit path.

5. **Result area is a core surface**
   - Bytebase has result tabs, batch-query result context, JSON/detail views, copy behavior, error views, and sensitive-data markers.
   - ControlHub Phase 36 should reserve bottom space for result grid / JSON / Explain / logs / masking state, even while locked.

6. **Access governance is not an afterthought**
   - Bytebase exposes request-query / JIT access, active/pending access grants, role request flow, and permission-aware actions.
   - ControlHub Phase 36 should show access status and request entry points as disabled or planned states, not hide them.

7. **History and shareable context are important**
   - Bytebase supports query history, history search, copy, and deep links.
   - ControlHub Phase 36 should include a visible query-history pane or tab with an empty/locked state.

8. **Multi-engine support needs different language per engine**
   - Bytebase is SQL-editor centric, but ControlHub must eventually handle SQL engines, Redis read commands, and MongoDB find/aggregate allowlists.
   - ControlHub Phase 36 should already label target capability as `SQL`, `Redis read command`, or `Mongo read query`.

## ControlHub-Specific Differentiation

ControlHub should not be a Bytebase clone. The stronger direction is:

- Use existing ControlHub resource data: environment, owner, cluster, health, topology relation, operational signal.
- Put governance context directly beside the editor: credential state, audit requirement, production policy, export disabled, future JIT status.
- Make Phase 36 explicit that execution is disabled by backend contract, not only by a frontend disabled button.
- Keep the visual language closer to an operations cockpit than an admin CRUD console.

## Phase 36 UI Preview Update

Updated preview:

`docs/superpowers/previews/phase-36-query-workbench-ide/index.html`

The preview now includes:

- Target switcher
- Connection context card
- Schema/object browser with column and sensitive-field placeholders
- Worksheet / saved sheets / query history / access grants tabs
- Locked Run / Explain / Export surfaces
- Read-only data source indicator
- Result grid / JSON / Explain / Logs / Masking placeholders
- Governance panel with execution, credential, audit, JIT, target facts, and policy checklist
- Bytebase research guardrails

## Explicit Phase 36 Non-Goals

- No live database connection.
- No query execution.
- No credential creation or storage.
- No SQL parser or guard implementation yet.
- No export.
- No approval workflow.
- No Admin mode.
- No batch query.
- No AI query assistant.

These should remain Phase 37+ work.
