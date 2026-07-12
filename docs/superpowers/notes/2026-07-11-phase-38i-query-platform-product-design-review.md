# Post-38I Query Platform Product Design Review

**Document role (calibrated after Phase 38I completion):** historical product
design review of the pre-remediation Query Workbench, plus a planning asset for
accepted Phase 38J+ follow-ups. This note preserves the review findings and
architecture decisions that drove the Phase 38I completion package. It is **not**
an open remediation contract.

**Phase 38I status:** **complete on `main`.** Remediation closed the P0/P1
findings below. Authoritative completion evidence:

```text
docs/superpowers/notes/2026-07-12-phase-38i-schema-intelligence-remediation-evidence.md
docs/superpowers/notes/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete-evidence.md
```

**Heads at completion (merged):**

| Repo | Tip | Notes |
|------|-----|-------|
| Backend `main` | `24272b8` | Includes `6ddb326` database-context fix + remediation evidence |
| Frontend `main` | `d1efc4d` | Final release-blocker repair (single initial worksheet, exact E2E HTTP errors) |

**Companion package (historical completion contract, now closed):**

```text
docs/superpowers/specs/2026-07-11-phase-38i-completion-query-workbench-productization.md
docs/superpowers/plans/2026-07-11-phase-38i-completion-query-workbench-productization.md
docs/superpowers/prompts/fullstack-phase-38i-completion-query-workbench-productization-worker.md
```

---

## Calibration Summary

| Class | Meaning after Phase 38I |
|-------|-------------------------|
| **Completed Phase 38I** | P0-1…P0-4, P1-1…P1-6, workbench IA, accessibility baseline, credential scope honesty, shared schema adapter, schema-aware completion, atomic worksheet context |
| **Accepted, deferred Phase 38J+** | Result-grid copy/navigation under masking policy; foreign-key record navigation; backend-normalized Visual Explain; global credential coverage/facets API; ER diagram; saved queries / governed collaboration; additional schema inspector engines |
| **Rejected / out of scope** | Browser DB connections; secret values in browser; DSN/password/username exposure; `actorUserId` request field; credential secret write API; credential edit on `/query`; SQL guard widening; new query engine; data-grid editing; Monaco; Tabularis clone of unrestricted write/DDL, detachable windows, notebooks, AI, MCP, visual query builder |

Do not rewrite the historical finding narrative below as if it still describes
`main`. Treat the P0/P1 sections as the pre-remediation defect ledger that the
completion package closed.

---

## Original Review Scope (historical)

The review covered (pre-remediation branches):

- `/query` on desktop and a 390x844 mobile viewport;
- target selection, worksheets, SQL editing, results, history, governance, the
  Object Explorer, Quick Navigator, and completion integration;
- `/settings/query-credentials` inventory, filters, summary, pagination, bulk
  operations, and single-target editing;
- frontend source at commit `9fec6c0`;
- backend Phase 38I source at commit `abf74fe`;
- the original Phase 38I design, plan, worker prompt, and evidence claims;
- applicable Tabularis source and product screenshots;
- a live signed-in run against the real backend and dedicated query MySQL
  fixture;
- a Lighthouse snapshot of `/query` in dark mode.

Three independent read-only specialists reviewed the same evidence:

- UI Designer;
- UX Architect;
- UX Researcher.

**Historical product-level conclusion (pre-remediation):** ControlHub had a
credible enterprise foundation, but the query experience behaved like an
administrative page containing an editor rather than a coherent governed SQL
workspace.

**Post-completion note:** Remediation productized `/query` into a contiguous
workbench (context bar, collapsible Objects pane, on-demand Connections,
Results/History output, mobile navigation). See remediation evidence for proof.

## Evidence Limits (historical review session)

The review captured desktop, mobile, connection-picker, Object Explorer,
credential-inventory, and credential-editor states. Those local screenshots were
review-session artifacts, not durable release evidence. Final remediation
evidence uses task-owned captures and the real E2E suite (41 passed, 0 failed,
0 skipped at frontend `d1efc4d`).

Light mode, mobile credential editing, full screen-reader output, zoom/reflow,
and the complete keyboard path were not fully manually certified in the original
review session. Automated findings and source inspection identified risks that
remediation then addressed with unit, component, and E2E coverage.

## Positive Foundation (still preserve)

The following foundations remain product constraints:

- IBM Plex typography and the existing ControlHub design tokens;
- restrained purple accent and semantic status colors;
- compact global navigation rail on desktop;
- managed targets rather than browser-created connections;
- server-side credential resolution and no-secret browser boundary;
- backend-enforced read-only SQL guard;
- explicit readiness, audit, timeout, and row-limit concepts;
- CodeMirror, local formatting, worksheet isolation, and dark/high-contrast
  editor themes;
- bounded target pagination and the on-demand connection picker.

The required work after the initial 38I implementation was product consolidation
and contract completion—not a visual rebrand. That consolidation is now done.

## P0 Findings (closed by Phase 38I remediation)

### P0-1: Object detail loses database context and fails silently — **closed**

**Pre-remediation observation:**

```text
GET /query-targets/616/schema/object-details
  ?database=
  &name=query_e2e_items
  &kind=table

400 Bad Request
```

The promise rejection was uncaught. The expanded table remained visually empty
and offered no explanation or retry action.

**Closure:** Backend `6ddb326` preserves per-item `Database` on object
summaries and rejects empty-database detail requests. Frontend shared worksheet
schema adapter carries parent database into detail keys and shows inline Retry.
Proven in real E2E object expansion.

### P0-2: Schema-aware autocomplete implemented but not integrated — **closed**

**Pre-remediation:** `SqlCodeEditorClient` accepted `schemaNamespace` and
`columnFetcher`, but `ReadyWorksheet` passed neither.

**Closure:** `useWorksheetSchemaAdapter` feeds Explorer, Quick Navigator, and
CodeMirror. Real table/alias completion proven through public UI + E2E.

### P0-3: Connection changes can retarget existing SQL without confirmation — **closed**

**Pre-remediation:** Navigator changed the active worksheet target while
preserving SQL.

**Closure:** Selecting a different connection creates a new worksheet by
default; original worksheet keeps SQL, target, database, result, and history.
Dirty close protection and dirty markers landed. Final repair `d1efc4d` ensures
initial mount does not auto-create Worksheet 2.

### P0-4: Mobile navigation and accessibility below public baseline — **closed**

**Pre-remediation:** At 390x844, global controls dominated the first viewport;
Lighthouse accessibility scored 67 on a snapshot.

**Closure:** Mobile primary navigation drawer, compact context bar, work views,
CodeMirror accessible name, tab/tabpanel, tree disclosure, keyboard splitters,
and localized control names. E2E covers 390px navigation, Connections, and
Objects sheets.

## P1 Findings (closed by Phase 38I remediation)

### P1-1: Roadmap scaffolding on primary surface — **closed**

Saved Scripts / Access placeholders and raw/planned enums removed from primary
surfaces. Only implemented Results and History remain in output.

### P1-2: `/query` vertically stacked instead of workspace-oriented — **closed**

Hero and floating connection card replaced by compact context bar; collapsible
Objects pane; editor/output split; on-demand Connections.

### P1-3: Fleet filtering appears broader than real scope — **closed**

Credential and target summaries/filters labeled as current-page scoped; no fake
global aggregates.

### P1-4: Credential administration equal prominence — **closed**

Default summary reduced to Total, Ready, Needs attention, Unsupported; filters
simplified; desktop table reduced to six semantic columns.

### P1-5: Credential editing redundant chrome — **closed**

One title, one close, flat form, sticky footer, dirty protection, separate
destructive Remove confirmation, localized outcomes.

### P1-6: Failures look like empty states — **closed**

Async surfaces distinguish loading, empty, filtered empty, denied, error, and
Retry across schema, history, targets, and credentials.

## Product Architecture Decision (still controlling)

The target identity is a **governed enterprise workbench**.

Desktop target layout:

```text
Context bar: target | database | environment | readiness | blocker | actions

+----------------------+-----------------------------------------------+
| Objects              | Worksheet tabs                                |
| 240-280px collapsible| Run toolbar                                   |
| databases            +-----------------------------------------------+
| tables/views         | SQL editor                                    |
| columns/keys         +-----------------------------------------------+
| search/reveal        | Results | History                             |
+----------------------+-----------------------------------------------+
```

This decision superseded the original Phase 38I instruction that the Object
Explorer must never become a persistent desktop region. The corrected
interpretation (now shipped):

- Connections remain on demand in a searchable dialog on desktop and a sheet
  on mobile;
- Objects may be a user-collapsible 240-280px pane inside the workbench at large
  widths because schema context is part of the authoring task;
- tablet and mobile continue to use an on-demand sheet;
- the pane must remember only its width/open preference, never schema data.

## Tabularis Adaptation Boundary (still controlling)

Learn from Tabularis:

- explorer, editor, and result in one contiguous viewport;
- compact active-state chrome;
- persistent object context for authoring;
- editor/result split and immediately visible execution feedback;
- keyboard Quick Navigator and explicit identifier insertion;
- dense but repeatable table/list row geometry.

Do not copy (**rejected / out of scope** for 38I and still rejected unless a
future phase explicitly designs them under governance):

- user-created connection profiles or browser-visible secrets;
- unrestricted write/DDL completion;
- editable result grids;
- unrestricted export;
- detachable OS windows or connection split view;
- eager loading of every database/schema;
- notebooks, AI, MCP, ER, Visual Explain, or query builder as ungoverned clones.

## Public Preview Gate (updated)

Phase 38I remediation closed the public-preview blockers listed in the original
review for the target → schema → SQL → result → history loop on the dedicated
fixture. Remaining preview-quality work that is **accepted but deferred** lives
under Phase 38J+ (masking-aware grid affordances, FK navigation, Visual Explain,
global credential facets, saved queries, multi-engine inspectors).

Do not treat “public preview ready for all enterprise query jobs” as true until
those deferred product surfaces are designed and shipped. Do treat Phase 38I as
complete for the schema-intelligence + workbench productization contract.

## Decision (calibrated)

1. **Phase 38I is complete** on backend `main` (`24272b8`) and frontend `main`
   (`d1efc4d`). Do not re-open the completion worker as an active implementation
   task.
2. **Do not** re-implement closed P0/P1 items from this note.
3. **Phase 38J+ planning** should start from the deferred list in the remediation
   evidence and the completion spec’s Deferred Work section—not from replaying
   this pre-remediation defect ledger.
4. Preserve this note as product-review context: jobs-to-be-done, Tabularis
   boundary, workbench IA decision, and scope honesty rules remain design
   constraints.
