# Phase 38E Query Workbench IA And Admin Feedback Design

## Background

Phase 38C/38D made the Query Workbench usable with real backend execution,
read-only metadata statements, searchable target picking, compact governance
badges, admin navigation, and direct URL role recovery.

Manual preview on 2026-07-07 exposed remaining UX issues:

- query target selection still feels like a dropdown/picker rather than a
  database connection navigator;
- target filters and active target identity compete for space;
- engine, environment, host, owner, language, readiness, and cluster facts are
  still visually heavy or duplicated;
- governance details are useful but should not dominate the query workflow;
- credential metadata save looked broken because the action saved successfully
  without visible feedback;
- `/settings/query-credentials` needs stronger mutation feedback so
  administrators can trust single-target edits and bulk operations.

Bytebase's local implementation in `/Users/fan/GolangProjects/bytebase` provides
a useful reference point:

- SQL editor context is routeable by project, instance, database, sheet, and
  query history;
- connection selection is a searchable environment-grouped tree/pane, not a
  small flat dropdown;
- data source selection and max rows live near the Run action;
- credentials are instance/admin data sources, while the SQL editor only uses
  permitted read-only data sources;
- worksheets/tabs and Monaco editor ergonomics are first-class, but those are
  larger follow-up phases for ControlHub.

## Goal

Make the current Query Workbench and query credential admin page feel coherent
and operational without starting the full database-IDE rewrite:

- target selection becomes a connection-navigation surface instead of a cramped
  dropdown;
- active target identity is compact and appears once;
- filters live with target navigation, not as disconnected controls;
- governance and access details are blocker-focused, compact, and progressive;
- credential admin mutations provide immediate, trustworthy feedback;
- query page never exposes credential edit controls, DSN fields, passwords, or
  actor identity inputs.

## Non-Goals

- No backend product code.
- No SQL guard changes.
- No new query engines.
- No DSN/password browser input.
- No secret-manager UI.
- No credential secret write API.
- No Monaco/CodeMirror migration.
- No SQL formatter.
- No multiple worksheet tabs.
- No saved query feature.
- No export.
- No approval/JIT workflow.
- No GitHub Actions workflow change.
- No tag, release, or deployment.

## Product Principles

### Query Workbench

The Query Workbench should optimize for four user jobs:

1. find the right query target quickly;
2. understand whether it can run and why;
3. write/read-only SQL and inspect results/history;
4. route blocked execution to the right admin action.

Static policy education is secondary. If a piece of UI does not help one of
those jobs, it should be compressed, moved behind a tooltip/details disclosure,
or removed.

### Credential Administration

Credential metadata is an administrator operation, not a query-authoring
operation. The query page may show credential state and link administrators to
settings, but it must not render credential reference inputs, DSN/password
fields, or metadata save/remove controls.

The admin page should make mutations observable:

- save/delete/bulk actions show success/failure feedback;
- single-target detail state and operations table state refresh together;
- stale target guards remain in place;
- raw credential refs are shown only as metadata refs, never as DSNs/passwords.

## Frontend Requirements

### F0. Credential Detail Save Feedback

If the current frontend branch does not already contain this fix, implement it
first:

- rename the configured-target primary action from "Edit credential metadata"
  to "Save credential metadata";
- show a visible success status after successful single-target save;
- refresh the operations table row after single-target save/delete;
- preserve stale target guards;
- add component tests for the feedback and parent refresh.

### F1. Connection Navigation Surface

Replace the top-heavy target picker with a connection-navigation surface.

Acceptable implementation for Phase 38E:

- a left panel in the Query Workbench grid; or
- a drawer/popover opened from a compact active-target header.

Required behavior:

- search target display name, resource name, engine, environment, host, port,
  readiness, and cluster;
- group targets by environment and then cluster when data is available;
- show ready targets prominently without hiding locked targets;
- show active filters inside the navigation surface;
- keep keyboard and screen-reader access for target selection;
- preserve target-owned state isolation when changing targets.

### F2. Active Target Header And Fact Deduplication

The active target identity should be shown once near the top of the workbench:

- display name;
- engine;
- environment;
- readiness;
- host:port when complete;
- optional details disclosure for owner, language, cluster.

Do not repeat the same facts in the governance panel. If `QuerySchemaBrowser`
also needs target context, keep it compact and avoid duplicating the header.

### F3. Governance Panel Compression

The governance panel should answer "can I run and what blocks me?" rather than
read like product documentation.

Required behavior:

- show credential state as a localized badge;
- show run/explain/export/save/request-access actions as compact badges with
  available/locked semantics;
- show only the primary blocker inline for locked targets;
- move longer explanations to tooltip/details;
- keep admin settings link only after admin role is resolved;
- no render-time `window`/`sessionStorage` access.

### F4. Query Runtime Controls

Move runtime controls toward the Run area where possible:

- max rows stays close to Run;
- read-only SQL copy remains clear;
- future data-source/credential-ref choices are represented as read-only status
  or disabled affordance, not editable credential fields.

This phase does not add multi-data-source selection unless the existing backend
contract already supports it safely.

### F5. Admin Settings Entry And Mutation Consistency

Keep `/settings` as the discoverable entry point for query credential settings.

Required behavior:

- admin users see an entry link to `/settings/query-credentials`;
- non-admin users see managed-by-admin copy;
- direct URL role recovery remains hydration-safe;
- save/delete/bulk feedback uses consistent success/failure language;
- operations table rows refresh after single-target detail mutations.

## Acceptance Criteria

- `/query` target navigation is searchable and grouped; it is no longer a flat
  dropdown-shaped control with detached filters.
- Active target facts are not duplicated across large panels.
- Governance panel is compact and blocker-focused.
- Query Workbench has no credential edit controls.
- `/settings/query-credentials` single-target save shows visible success feedback.
- Single-target save/delete refreshes the operations row.
- No DSN/password/actorUserId is sent or rendered.
- Component tests cover target navigation, fact dedupe, governance compression,
  and credential mutation feedback.
- Real E2E with backend + dedicated query MySQL covers query workbench and query
  credential settings; if backend is unavailable, final report must say so and
  must not claim E2E passed.
