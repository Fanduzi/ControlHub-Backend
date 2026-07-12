# Phase 38I Completion Package — Historical Implementation Plan

> **Calibrated status:** This remediation plan was executed and closed as part of
> Phase 38I. It is a historical implementation record and a Phase 38J+ planning
> pointer. Do **not** create replacement branches, re-open incomplete checklists
> as active work, or treat pre-remediation baseline heads as current tips.

**Goal (fulfilled):** Close the real Phase 38I contract, make worksheet execution
context safe, and consolidate the Query Workbench and Credential Administration
into truthful, accessible, product-quality workflows.

**Architecture (shipped):** Backend preserved the governed schema API and added
the database-context invariant (`6ddb326`). Frontend introduced a worksheet-scoped
schema adapter, bound target/database to worksheets, and restructured `/query` as
a contiguous workbench. Credential administration was simplified without inventing
global aggregates or secret handling.

## Post-Completion Reference

| Item | Value |
|------|-------|
| Backend `main` tip at calibration | `24272b8` |
| Frontend `main` tip at calibration | `d1efc4d` |
| Remediation evidence | `docs/superpowers/notes/2026-07-12-phase-38i-schema-intelligence-remediation-evidence.md` |
| Product design review (calibrated) | `docs/superpowers/notes/2026-07-11-phase-38i-query-platform-product-design-review.md` |
| Spec (calibrated) | `docs/superpowers/specs/2026-07-11-phase-38i-completion-query-workbench-productization.md` |
| Worker prompt (historical) | `docs/superpowers/prompts/fullstack-phase-38i-completion-query-workbench-productization-worker.md` |

## Historical Worktrees (closed)

Backend (at plan authoring):

```text
/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-38i-schema-intelligence
branch: phase-38i-schema-intelligence
baseline head: abf74fe
```

Frontend (at plan authoring):

```text
/Users/fan/JsProjects/ControlHub/.worktrees/phase-38i-schema-intelligence
branch: phase-38i-schema-intelligence
baseline head: 9fec6c0
```

Implementation landed on those branches and was merged to `main`. Unrelated
worktrees must still be preserved when doing hygiene or later phases.

## Required Reading (still useful)

```text
docs/superpowers/notes/2026-07-11-phase-38i-query-platform-product-design-review.md
docs/superpowers/specs/2026-07-11-phase-38i-completion-query-workbench-productization.md
docs/superpowers/specs/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete.md
docs/superpowers/plans/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete.md
docs/superpowers/notes/2026-07-12-phase-38i-schema-intelligence-remediation-evidence.md
docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

## Hard Boundaries (still controlling)

- No SQL guard change.
- No new query engine.
- No browser database connection.
- No DSN/password/database username/secret value in browser state, request,
  response, display, cache key, error, audit, or log.
- No `actorUserId` request field.
- No credential secret write API.
- No credential edit controls on `/query`.
- No schema persistence migration or browser persistence.
- No saved query, export, approval, JIT, Visual Explain, ER, notebook, AI, MCP,
  query builder, editable data grid, or connection profile implementation in a
  38I-shaped package (see deferred 38J+).
- No Monaco migration.
- No global credential aggregate endpoint faked client-side.
- No AI co-author trailer on commits.

## Plan Tasks — Execution Ledger (all closed)

Checklist items below are retained as the historical execution ledger. Every
task is **done** relative to Phase 38I completion evidence. Do not treat open
checkboxes as unfinished work; they are archival.

### B0. Freeze The Failure Evidence — **done**

Reproduced empty-database object-detail 400, keyword-only completion,
destructive retarget, mobile/a11y baseline, and credential scope issues before
fixes. See product design review note.

### R0. Pre-Edit Specialist Review — **done (partial tooling timeouts)**

Explore agents mapped both repos. Oracle/Momus timed out without actionable
output during remediation; implementation closed from explore findings +
spec/plan. Final release-blocker repair `d1efc4d` was code-review approved for
finishing.

### B1. Restore Object Database Context — **done**

Backend commit `6ddb326`: per-item database on object summaries; empty-database
validation; RED tests.

### F1. One Worksheet Schema Adapter — **done**

`useWorksheetSchemaAdapter` + shared store consumption for Explorer, navigator,
and completion.

### F2. Real Schema-Aware CodeMirror Integration — **done**

`schemaNamespace` and `columnFetcher` wired through `ReadyWorksheet`; real E2E
table completion path.

### F3. Atomic Worksheet Execution Context — **done**

New worksheet on connection select; dirty close protection; no auto Worksheet 2
on mount (`d1efc4d`).

### F4. Consolidate `/query` Into One Workbench — **done**

Context bar, collapsible Objects, Results/History only, no hero/placeholder
modes.

### F5. Mobile Navigation And Accessibility — **done**

Mobile nav drawer, compact header/context, ARIA/keyboard fixes for audited
flows.

### F6. Credential Administration Without Faking Global Scope — **done**

Four summary cards, simplified filters/columns, modal cleanup, page-scoped truth.

### F7. Async And Localization Gaps — **done**

Empty vs error distinction; controlled localized status labels; known credential
states including `secret_resolved`.

### T1 / E1 / V1 / R1 / D1 — **done**

Full gates, real E2E 41/41, evidence notes, quality baseline update recorded in
remediation evidence. Specialist visual loop and adversarial reviewers had
tooling limits; residual release blockers were fixed via code review on
`d1efc4d`.

## Suggested Historical Commit Shape (for reference)

Backend:

```text
fix(query): preserve schema database context
docs: finalize phase 38i remediation evidence for finishing
```

Frontend (representative):

```text
fix(query): unify worksheet schema metadata and wire real completion
refactor(query): consolidate the governed workbench
fix(query): restore accessible responsive workbench flows
refactor(credentials): focus credential remediation workflows
fix(query): stop mount Worksheet 2 and exact E2E HTTP error matching
```

## Phase 38J+ Planning Extract

When starting the next phase, **do not** revive this full plan. Spawn a new plan
from only accepted deferred items:

1. Result-grid copy/navigation under explicit masking policy.
2. Foreign-key record navigation.
3. Backend-normalized Visual Explain (possibly 38K).
4. Global credential coverage/facets API.
5. ER diagram from governed schema API.
6. Saved queries / governed collaboration.
7. Additional schema inspector engines.

Rejected items remain rejected unless a dedicated design overturns them (see
spec Non-Goals).

## Final Status (calibrated)

| Outcome | Status |
|---------|--------|
| Ready for human review (remediation) | Achieved; finishing evidence on `main` |
| Push/merge of implementation | Completed separately via finishing flow |
| Tag / release / deploy | Not part of this package |
| Product code change from this docs calibration | None |

This plan is archival. Future agents should read remediation evidence and the
calibrated spec’s Phase 38J+ section instead of executing B0–D1 again.
