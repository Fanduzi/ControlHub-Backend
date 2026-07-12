# Phase 38I Completion Package — Historical Spec And Phase 38J+ Planning Asset

## Status (calibrated after Phase 38I completion)

**Phase 38I completion package: closed.** The remediation contract below was the
additive package that finished Phase 38I after the initial schema-intelligence
implementation. It is retained as:

1. **Historical design record** of what completion required;
2. **Planning asset** for accepted but deferred Phase 38J+ work;
3. **Boundary document** for rejected/out-of-scope ideas.

It is **not** an open implementation ticket. Do not create new
`phase-38i-schema-intelligence` worktrees to re-execute this package.

Authoritative completion evidence:

```text
docs/superpowers/notes/2026-07-12-phase-38i-schema-intelligence-remediation-evidence.md
docs/superpowers/notes/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete-evidence.md
docs/superpowers/notes/2026-07-11-phase-38i-query-platform-product-design-review.md
```

**Merged heads:**

| Repo | Tip |
|------|-----|
| Backend `main` | `24272b8` (includes `6ddb326` database-context fix) |
| Frontend `main` | `d1efc4d` (final release-blocker repair) |

Original design (still controlling for security/API rules):

```text
docs/superpowers/specs/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete.md
```

If this package conflicted with the original Phase 38I frontend layout or
completion wording, this remediation package controlled for frontend product
behavior. Backend security, no-secret, audit, SQL guard, target binding, bounded
metadata, and API rules from the original specification remain controlling.

## Calibration Matrix

### Completed Phase 38I capabilities

- Object metadata loads and recovers with explicit states and Retry.
- Object Explorer, Quick Navigator, and CodeMirror share one worksheet-scoped
  schema adapter over the bounded store.
- Table and column completion work against the real fixture through the public
  UI path.
- Each worksheet owns atomic target, database, SQL, result, and history context.
- Selecting another connection creates a new worksheet by default.
- Editor/result loop dominates the viewport (context bar, collapsible Objects,
  on-demand Connections, Results/History only).
- Placeholder primary tabs and raw/planned enum labels removed from primary
  surfaces.
- Desktop + mobile navigation and accessibility baseline for audited flows.
- Credential administration: page-scoped truth, reduced summary/filters/columns,
  simplified modal.
- Backend database-context invariant on object summaries and empty-database
  rejection.
- Real E2E: 41 passed, 0 failed, 0 skipped at frontend `d1efc4d`.

### Accepted but deferred — Phase 38J+ planning

Only after Phase 38I evidence is stable (it is):

| Candidate | Notes |
|-----------|--------|
| Phase 38J | Result-grid affordances and foreign-key record navigation under explicit masking/authorization policy |
| Phase 38K (or later) | Backend-normalized Visual Explain |
| Later | Global credential coverage/facets API (not client-side fake aggregates) |
| Later | ER diagram from the same governed schema API |
| Later | Saved queries and governed collaboration |
| Later | Additional schema inspector engines |

### Rejected / out of scope (do not smuggle into 38J+)

- SQL guard widening or behavior change.
- New query engine; browser database connection.
- DSN, password, database username, or secret value in browser state, requests,
  responses, display, cache keys, errors, audit, or logs.
- `actorUserId` request field; credential secret write API; credential edit
  controls inside `/query`.
- Schema persistence migration or browser schema persistence.
- Data-grid editing; unrestricted export; approval/JIT/access-request product
  without a dedicated design.
- Notebooks, AI assistant, MCP, visual query builder, connection split view,
  detachable OS windows, Monaco migration, frontend connection profile creation.
- Global credential aggregate endpoint faked client-side.

## Historical Baseline (pre-remediation worktrees)

At package authoring time the remediation targets were:

```text
Backend worktree:  .../backend-phase-38i-schema-intelligence
Backend branch:    phase-38i-schema-intelligence
Backend head:      abf74fe

Frontend worktree: .../phase-38i-schema-intelligence
Frontend branch:   phase-38i-schema-intelligence
Frontend head:     9fec6c0
```

Those branches were finished and merged to `main`. Do not treat the baseline
heads above as current incomplete tips.

## Goal (fulfilled)

Turn the incomplete initial implementation into one coherent, trustworthy,
governed SQL workspace that delivers the original Phase 38I value in real use.
Product identity remains a governed company query platform, not a browser clone
of DBeaver, DataGrip, or Tabularis.

## Non-Goals (unchanged boundaries)

- No SQL guard widening or behavior change.
- No new query engine.
- No browser database connection.
- No DSN, password, database username, or secret value in browser state,
  requests, responses, display, cache keys, errors, audit, or logs.
- No `actorUserId` request field.
- No credential secret write API.
- No credential edit controls inside `/query`.
- No schema persistence migration or browser schema persistence.
- No data-grid editing.
- No saved query implementation (deferred).
- No export implementation (deferred under policy).
- No approval, JIT, or access-request implementation.
- No Visual Explain, ER diagram, notebook, AI assistant, MCP, visual query
  builder, connection split view, or detachable window.
- No Monaco migration.
- No frontend connection profile creation.
- No global credential aggregate endpoint in this package.
- No CI workflow, release, tag, or deployment change as part of this package.

## Product Principles (still controlling)

1. **Make the primary loop real before adding breadth.** Target, schema, SQL,
   result, and history must work end to end before new tabs or modes appear.
2. **Execution context is atomic.** Worksheet SQL, target, database, result, and
   history cannot drift independently.
3. **One metadata truth.** Explorer, navigator, and completion consume one
   bounded worksheet-scoped adapter over the shared store.
4. **No silent ambiguity.** Loading, empty, denied, stale, and error are distinct
   states with explicit recovery.
5. **Work surface over page scaffolding.** Editor, object context, and results
   occupy one contiguous workbench plane.
6. **Connections are on demand; objects are working context.** A target fleet is
   searched in a dialog/sheet. The active schema may remain visible in a
   collapsible workbench pane on large screens.
7. **Show only shipped jobs.** Primary tabs and controls never advertise
   placeholders or roadmap scope.
8. **Governance is concise and authoritative.** Show one actionable blocker or
   one ready outcome; detailed policy remains on demand.
9. **Scope labels must be truthful.** Page-scoped coverage and filters cannot
   look global.
10. **Accessibility is product correctness.** Keyboard, screen reader, focus,
    and mobile navigation are release gates.

## Contract Corrections (shipped)

### Object Database Context Invariant

For every successful object-list response:

```text
response.database == requested database
response.items[i].database == requested database
```

Backend enforces and tests this invariant (`6ddb326`). Frontend carries the
expanded parent database into every object-detail key and request and does not
dispatch detail with an empty database.

Existing routes remain:

```text
GET /query-targets/{id}/schema/databases
GET /query-targets/{id}/schema/objects
GET /query-targets/{id}/schema/object-details
```

### Shared Frontend Schema Adapter

Worksheet-scoped adapter keyed by:

```text
worksheetId + targetResourceId + activeDatabase
```

Owns/exposes database pages, object pages/search, object-detail states, bounded
in-flight detail loading, namespace generation for CodeMirror, column loading for
`table.` / alias completion, reveal state, request generations / AbortController
ownership, and explicit loading/stale/empty/denied/error states. Uses bounded
in-memory `QuerySchemaStore`. No browser schema persistence.

### Completion Contract

CodeMirror receives active worksheet schema namespace and bounded column
fetcher. Required completion behaviors (table, qualified object, quoted
identifiers, `table.` / `alias.` columns, Ctrl+Space, no cross-worksheet
leakage, keyword-only fallback, no write/DDL/session/transaction/locking
keywords) are part of the closed Phase 38I contract and proven via unit + E2E.

## Worksheet Safety Model (shipped)

Each local worksheet owns id, name, targetResourceId, activeDatabase, statement,
dirty state, maxRows, execution/result/error/history state, and request
generations.

### Selecting A Different Connection (shipped default)

1. User opens Connections.
2. User selects a target different from the active worksheet target.
3. ControlHub creates and activates a new worksheet bound to that target.
4. The new worksheet initializes its database from the target's governed schema
   metadata.
5. The original worksheet, SQL, result, history, target, and database remain
   unchanged.

Explicit `Change worksheet target` (if present) requires confirmation for
non-empty/dirty SQL and never mutates another worksheet.

### Close And Reload Protection (shipped baseline)

- Closing a non-empty or dirty worksheet requires confirmation.
- Active worksheet tab shows a dirty marker.
- Worksheets remain temporary scratch work until durable saving ships (38J+).
- Reload/unload protection while any worksheet contains unsaved non-empty SQL.

## Query Workbench Information Architecture (shipped baseline)

### Desktop At 1280px And Above

```text
+-----------------------------------------------------------------------+
| Context bar: target | database | environment | readiness | actions    |
+----------------------+------------------------------------------------+
| Objects              | Worksheet tabs                                 |
| 240-280px            +------------------------------------------------+
| collapsible          | Run toolbar                                    |
|                      +------------------------------------------------+
| databases            | SQL editor                                     |
| tables/views         +------------------------------------------------+
| columns/keys         | Results | History                              |
| search/reveal        |                                                 |
+----------------------+------------------------------------------------+
```

### Tablet / Mobile

Objects and Connections use overlay/full-height sheets. Mobile has primary
navigation. No permanent squeezed explorer column. No horizontal page overflow
at 320/375/390px as an acceptance rule.

## Visible Feature Policy (shipped)

Primary surface must not show Saved Scripts, Access, JSON, Explain, Logs,
Masking, or roadmap badges until those features are implemented. Controlled
status labels replace raw credential/policy enums.

## Object Explorer / Connection Picker / Results / History / Governance

The detailed experience requirements in the original remediation package remain
the product standard for shipped surfaces. New work must not regress:

- explicit async states at every metadata level;
- on-demand connection picker pagination and scope honesty;
- Results/History as the only primary output modes;
- History `Use in worksheet` without auto-execution;
- one ready outcome or primary blocker in the context bar.

Copy/export of result grids remains **deferred** until masking and authorization
policy is defined (Phase 38J+).

## Credential Administration Productization (shipped baseline)

- No global credential-aggregate API in 38I.
- Summaries state current-page scope.
- Default summary: Total, Ready, Needs attention, Unsupported.
- Default filters: Search, Runtime status, Environment, More filters.
- Desktop columns: Target, Context, Runtime, Binding, Policy, Action.
- Simplified modal/full-screen edit with dirty protection and no-secret boundary.

Global facets/coverage remain a separately designed backend contract (deferred).

## Async State, Accessibility, Visual System Contracts

These contracts remain acceptance standards for any future query UI change.
They are not invitations to re-open Phase 38I. Phase 38J+ must preserve them.

## Test Contract (historical gates — closed)

Backend, frontend unit/component, real E2E (14-step sequence), and visual QA
requirements in the original package were the completion gates. Final evidence
records full gate matrices and **41/41** real E2E at `d1efc4d`. Do not re-run
this package’s open checklist as if incomplete; use quality baseline and
remediation evidence for current status.

## Completion Gates (met)

Phase 38I is complete because:

- P0 and P1 remediation requirements were implemented;
- original backend Phase 38I security and API gates remained green;
- object details and schema-aware completion proven through real public UI paths;
- worksheet target/database context cannot drift silently on connection select;
- placeholder modes and raw enums removed from primary surfaces;
- desktop and mobile IA matches this package’s workbench model;
- backend and frontend full gates passed with focused commits on `main`;
- final real E2E had zero failed and zero skipped required tests (41 passed);
- no push/tag/release/deploy was required of the worker beyond separate finishing.

## Deferred Work → Phase 38J+ Planning

Only after this completion evidence is stable (now true):

1. **Phase 38J candidate:** result-grid copy/navigation under an explicit
   masking policy; foreign-key record navigation.
2. **Later:** backend-normalized Visual Explain.
3. **Later:** global credential coverage/facets API.
4. **Later:** ER diagram.
5. **Later:** saved queries and governed collaboration.
6. **Later:** additional schema inspector engines.

When planning 38J+, start a **new** spec/plan/prompt pair. Do not rename this
closed package into an active 38J implementation ticket without rewriting scope
around only the deferred items above.
