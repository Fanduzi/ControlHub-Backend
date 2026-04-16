# Frontend Phase 14A: Console IA And Multi-Select Filter Cleanup

You are implementing a focused frontend cleanup phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

This phase exists because live review found unresolved console information-architecture and filtering problems. The decisions are already made. Do not reopen product debates that are fixed in this prompt.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-10-console-closeout-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-5-multi-select-query-contracts-worker.md`
- `/Users/fan/JsProjects/ControlHub/app/(console)/overview/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/cmdb/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/databases/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/audits/page.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources/resource-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/databases/database-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/audits/audit-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/overview/overview-content.tsx`
- `/Users/fan/JsProjects/ControlHub/components/settings/*`

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
- base includes frontend Phase 13.10 on `main`
- worktree is clean

Stop and report if the path, branch, base, or cleanliness is wrong.

## Parallel Coordination Rules

Frontend and backend workers cannot talk to each other during execution. This prompt is self-contained.

- You may implement the page composition and local filter-state architecture immediately.
- Final multi-select request wiring is **not final** until backend Phase 12.5 lands on `main`.
- Before claiming completion, sync latest frontend `main` and latest backend-supported contract, then rerun full validation and live verification.
- Do not invent a frontend-only final contract that diverges from backend repeated-parameter behavior.
- Recommended execution order is:
  1. backend Phase 12.5 lands first
  2. frontend Phase 14A rebases or syncs latest `main`
  3. frontend completes real request wiring, E2E, and live verification
- True parallel work is allowed only for non-final UI scaffolding, layout cleanup, and local state architecture.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives.

- Multi-select filters are required on list pages for non-conflicting filter families.
- Use URL-synced multi-select state, not local-only widget state.
- Keep pagination working with multi-select.
- `/overview` must be simplified:
  - resource posture gets its own row
  - recent audit section is removed
  - attention queue remains but must stop looking like unfinished mock/demo filler
- The current “environment lane” concept must either become immediately understandable or be removed. Prefer replacement with clearer per-environment summary cards.
- `/resources` keeps one creation entry only. The action is named `新建资源` / `Create resource`.
- `/resources` and `/databases` must stop leaking UUIDs as primary secondary text in list rows when a better human-readable identifier exists.
- `/cmdb` must be explicitly defined as the configuration/inventory view, distinct from the runtime-oriented `/resources` view.
- `/resources` is the runtime operations list:
  - posture
  - health
  - lifecycle
  - quick operational inspection
- `/cmdb` is the configuration/inventory list:
  - structured identifiers
  - ownership
  - environment
  - source
  - external ID
  - labels / profile presence / archive state where available
  - it must not read like a second copy of the runtime list
- `/settings` support-dictionary explanatory text must localize in Chinese.
- Do not add dictionary editing in this phase. This is intentionally deferred until backend dictionary CRUD exists.
- Do not add database vendor logos/icons in this phase. This is intentionally deferred until the IA cleanup and topology semantics are stable.

## Exact Problems To Fix

### 1. Overview page composition

Required outcome:

- resource posture / status metrics occupy a full-width row
- attention queue becomes a full-width section below, with pagination if needed
- remove the recent audit block from overview
- replace the confusing environment-lane presentation with explicit per-environment summary cards, unless doing so proves impossible from current data

Environment summary cards must show:

- environment name
- total resource count
- health distribution counts
- top 3 abnormal resources sorted by severity first
- explicit copy such as “showing top 3 of N” when not all resources are listed

Severity ordering must be rational and visible:

- `critical` before `warning`
- `warning` before `healthy`
- healthy resources should not bury more important abnormal ones

### 2. Attention queue quality

Current problem:

- too much filler
- unfinished summary fallback text
- no pagination

Required outcome:

- no `No supplemental resource summary has been defined yet` in the live UI
- clear secondary text strategy
- page size / pagination for larger result sets
- do not hardcode a fake summary just to hide bad data
- if the root cause is missing backend/seed summary truth, use the best existing deterministic fallback order on the frontend and report the exact missing backend truth in the final report

### 3. `/resources` page cleanup

Required outcome:

- one create action only
- final label is `新建资源` / `Create resource`
- resource type filtering works
- multi-select filters are supported
- filter trigger labels are self-describing before selection
- row secondary text uses a human-readable priority order, not random UUID fallback

### 4. `/databases` page cleanup

Required outcome:

- database-oriented filters work
- multi-select filters are supported
- page remains readable at the default page size
- summary strip and table width stay balanced

### 5. `/cmdb` page meaning

Required outcome:

- make the page visibly and conceptually distinct from `/resources`
- treat it as the configuration/inventory view, not the runtime posture view
- include enough interaction that it is not a dead table:
  - search
  - relevant filters
  - row click to detail or equivalent drill-in
- prioritize fields that reinforce configuration/inventory meaning, such as:
  - display name
  - resource type / subtype
  - environment
  - owner
  - external ID
  - source
  - archive state
  - structured labels/profile summary if available
- if a narrow rename/description change is needed to clarify purpose, do it

Do not remove the page in this phase unless truly impossible to salvage without backend work.

### 6. `/audits` page readability

Required outcome:

- “recent changes” no longer shows broken timestamp wrapping
- table/timeline composition remains readable with larger data sets

### 7. Settings dictionary localization

Required outcome:

- explanatory support text under backend taxonomy/dictionary sections is localized in Chinese mode

## Multi-Select Requirements

Implement URL-synced multi-select behavior for appropriate families on:

- `/resources`
- `/databases`
- `/audits` if backed by backend Phase 12.5

Required UX behavior:

- checkbox or equivalent multi-select UI
- selected chips or compact selected summary
- clear-all action
- remove-one behavior where practical
- URL persistence
- pagination reset to page 1 when filter set changes

Do not implement mutually exclusive logic where it does not belong.

## TDD Requirements

Use TDD. Add failing tests first.

At minimum add or update tests for:

- overview full-width posture section
- overview attention queue pagination
- recent audit block removed from overview
- environment summary replacement/removal behavior
- resources page single create action
- resources multi-select URL behavior
- resources human-readable secondary text fallback order
- databases multi-select URL behavior
- audits timestamp no-wrap / readable layout
- settings support-dictionary localization

E2E must cover at least:

- overview revised composition
- resources multi-select filters really change query params and visible rows
- databases multi-select filters really change query params and visible rows
- CMDB is visibly distinct from resources and supports its chosen drill-in behavior
- audits page remains readable with larger rows

## Required Live Verification Pages

You must manually verify:

- `/overview`
- `/resources`
- `/databases`
- `/cmdb`
- `/audits`
- `/settings`

Verify in both Chinese mode and English mode where localization changes are involved.

## Required Verification Commands

Inside the worktree:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

If practical after branch readiness, also verify from the main checkout:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

## Final Report

Your final report must include:

- worktree path
- branch
- commit hash
- exact pages changed
- exact multi-select families supported
- whether backend Phase 12.5 was required for final wiring
- how overview was simplified
- how environment summary cards replaced the old lane concept, or why they were removed
- how CMDB was differentiated
- how resource secondary-text fallback was changed
- whether attention-queue quality was limited by backend data truth, and the exact remaining gap if so
- localization fixes made in settings
- all verification command results
- live verification outcomes
- `git status --short --branch`

Do not say “filters improved” or “UI cleaned up”. Report the final interaction contract concretely.
