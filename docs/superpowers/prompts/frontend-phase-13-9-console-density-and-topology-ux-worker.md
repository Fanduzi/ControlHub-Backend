# Frontend Phase 13.9: Console Density And Topology UX Cleanup

You are implementing the frontend console-density and topology-UX cleanup phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-8-resource-archive-ui-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-3-demo-data-and-topology-semantics-worker.md`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/databases/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/audits/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`
- `/Users/fan/JsProjects/ControlHub/components/app-shell/sidebar.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources`
- `/Users/fan/JsProjects/ControlHub/components/databases/database-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/audits/audit-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx`
- `/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts`

## Goal

ControlHub now has the right core capabilities, but several product-shell issues remain:

- data tables are too wide, too dense in the wrong places, and not dense enough in the right places
- multiple filter controls default to vague `all` states that do not explain themselves
- sidebar navigation is fixed-width and should collapse
- resource/database detail sheet action hierarchy is weak
- topology labels and layout are not semantically good enough for database-oriented views

This phase is a focused UX and layout cleanup phase. Do not widen product scope. Improve the current shell so the console feels intentional and easier to use.

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
```

Expected:

- worktree path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is dedicated to this phase
- base includes frontend Phase 13.8 on `main`
- backend archive lifecycle is available locally
- worktree is clean

Stop and report if the worktree path, branch, or base is wrong.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- This phase is a cleanup phase, not a new-feature phase.
- Do not add SQL work orders, query execution, discovery, or topology editing.
- Do not introduce a new visual language.
- Keep the console professional, compact, and table-first.
- Sidebar should become collapsible on desktop.
- Detail-sheet primary navigation actions belong in the header action area, not buried below content.
- Database topology should become more semantically readable, not just more animated.
- Default table density should be increased slightly by reducing default rows per page from 20 to a smaller value such as 15, unless a stronger repo-consistent value emerges during implementation.
- Filter controls should describe themselves without requiring the user to open them first.
- Use project-local worktree path under `/Users/fan/JsProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. tighten `/resources`, `/databases`, and `/audits` list layout and filter UX
2. make the desktop sidebar collapsible with remembered state
3. improve detail-sheet action hierarchy, especially “open full detail”
4. improve topology label localization and node layout semantics for database-like graphs
5. give topology more appropriate viewing space on the full detail page
6. add or update tests and E2E coverage for the changed interactions

Do not add entirely new pages.

## Required Cleanup Areas

### 1. Resources List

Problems to fix:

- four filter controls default to vague `all`
- filters are not self-describing enough at a glance
- current page density is too low for a console table

Required outcomes:

- filter trigger text is self-explanatory even before interaction
- default rows-per-page is reduced from 20 to a more usable console value
- subtype-oriented filtering support is added where justified by the existing backend contract, especially for database-oriented resource browsing
- controls should not visually overpower the table

### 2. Databases Page

Problems to fix:

- the right-side posture panel makes the main table too narrow
- missing search
- missing engine/type filter
- page density too low

Required outcomes:

- the primary database worklist gets the width priority
- summary/posture information should move to a lighter-weight top summary strip or similar compact pattern rather than occupying a full right rail
- add search
- add database engine/type filtering using existing backend/frontend data
- reduce default rows-per-page

### 3. Audits Page

Problems to fix:

- horizontal overflow appears too easily
- “recent change”/summary content dominates table width

Required outcomes:

- audit table should fit more naturally without habitual horizontal scrolling
- less important columns may be narrowed, truncated, or strategically reduced
- summary/change text should be width-bounded and readable

### 4. Sidebar

Required outcomes:

- desktop sidebar can collapse to an icon rail
- collapsed/expanded state is remembered locally
- mobile behavior should remain separate and not regress
- collapsed mode must still keep navigation understandable through affordances such as tooltips or clear iconography

### 5. Detail Sheet / Detail Page Actions

Required outcomes:

- “Open full detail” becomes a top-level header action
- archive/edit/full-detail actions should read as a coherent action cluster
- avoid burying primary navigation actions at the bottom of long sheet content

### 6. Topology UX

Required outcomes:

- localized status labels inside topology nodes must respect the active locale
- database-oriented topology layout should become more semantically readable
- do not rely only on raw `distance` columns when that makes primary/replica/proxy relationships hard to read
- improve the layout so edges are less likely to visually cut through important nodes
- the full detail page should provide a larger topology viewing area than the compact sheet

Do not add a brand-new topology editor or a full workflow canvas.

## Layout Guidance

Use the existing console style, but improve structure.

Preferred direction:

- top summary strips over right-rail summary cards when the main artifact is a table
- stronger width priority for tables
- more compact but clearer filters
- restrained tabs or URL-synced secondary views only if they are clearly justified for topology space

If you use tabs on the full resource detail page for topology/relations/audits, keep them URL-synced so shared links can land on the correct tab. Do not add tabs speculatively if a larger inline section solves the problem more cleanly.

## Testing

Follow TDD.

At minimum add or update tests for:

- sidebar collapse/expand and persisted state
- resources filter labels / query-param behavior
- databases search and engine/type filter behavior
- detail-sheet header action placement behavior where testable
- topology label localization
- topology layout or render expectations where a deterministic assertion is practical
- E2E for at least:
  - sidebar collapse persistence
  - one resource/archive/detail flow still working after layout changes
  - database page search/filter behavior
  - topology page rendering in the improved layout

Keep tests independent from exact seed row counts.

## Verification

You must run inside the worktree:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

After the branch is ready, also verify from the main checkout if practical:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

If main-checkout verification is not possible from your session, say so explicitly.

## Manual / Live Verification

Run against a live backend.

Confirm at minimum:

- `/resources` feels denser and filter controls read clearly
- `/databases` table has width priority and supports search/filter
- `/audits` is less prone to horizontal overflow
- sidebar collapse works and is remembered
- resource/database sheet places full-detail action in the header action area
- topology labels localize correctly in Chinese
- topology is easier to read for one database-cluster example than before

## Tooling Isolation Requirements

Verify and preserve:

- `tsconfig.json` excludes `.worktrees`
- `vitest.config.ts` excludes `.worktrees/**`
- `eslint.config.mjs` ignores `.worktrees/**`
- `eslint.config.mjs` also remains stable around `test-results/**`
- Playwright test discovery stays local to `e2e`
- `next.config.ts` keeps Turbopack root fixed to the frontend project directory

If any relevant tool still scans `.worktrees/**`, fix it in this phase.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only. Do not stage `.next`, `test-results`, `.worktrees`, logs, screenshots, or temporary files.

## Final Report

Your final report must include:

- worktree path and branch
- commit hash
- changed files
- exact table/filter density changes by page
- sidebar collapse behavior and persistence details
- detail-sheet action hierarchy changes
- topology localization/layout changes
- verification command results
- live backend verification result
- confirmation that `.worktrees`, `.next`, and `test-results` were not committed
- negative scope confirmation:
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not add a new visual language
  - did not add frontend-only mocks as final behavior
  - did not tag, push, release, or add AI co-author
- next phase input:
  - remaining topology UX limits
  - any backend contract gaps still affecting list/table quality
  - whether seed data now needs another cleanup pass

## Constraints

- use a dedicated worktree under `/Users/fan/JsProjects/ControlHub/.worktrees`
- use TDD for changed UI and interaction behavior
- do not reset the repo
- do not discard unrelated work
- do not turn this into a broad redesign phase
- do not rely on exact seed row counts
- do not let any tool scan `.worktrees/**` from the main checkout
