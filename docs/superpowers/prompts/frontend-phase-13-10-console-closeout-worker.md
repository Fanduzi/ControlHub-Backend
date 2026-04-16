# Frontend Phase 13.10: Console Closeout After Live Review

You are implementing a strict frontend closeout phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

This phase exists because a human review of the running product found several issues still unresolved after Phase 13.9. Do not restate previous success reports. Fix the remaining issues and prove them with tests plus live verification.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-9-console-density-and-topology-ux-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-4-topology-semantics-followup-worker.md`
- `/Users/fan/JsProjects/ControlHub/components/app-shell/sidebar.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources/resource-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/databases/database-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/audits/audit-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx`
- `/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts`
- `/Users/fan/JsProjects/ControlHub/components/blocks/status-badge.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/audits/page.tsx`

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
- base includes frontend Phase 13.9 on `main`
- worktree is clean

Stop and report if the path, branch, or base is wrong.

## Parallel Coordination Rules

Frontend and backend workers cannot talk to each other during execution. This prompt is self-contained.

- You may implement the independent UI fixes immediately.
- Topology layout is allowed to start in parallel, but it is **not final** until you revalidate against the latest backend `main` that includes backend Phase 12.4.
- If backend Phase 12.4 lands graph-truth changes while you are working, you must sync latest `main` before claiming completion.
- Final completion requires a fresh live verification pass against the updated backend truth.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives.

- This is a closeout phase, not a new-feature phase.
- Do not add topology editing.
- Do not add SQL work orders, query execution, discovery, or new product areas.
- Do not redesign the entire visual language.
- Sidebar collapse must remain desktop-specific and locally persisted.
- The sidebar collapse control must become sticky / always discoverable.
- Sheet and detail-page actions must remain compact and professional.
- Topology is allowed to gain stronger full-detail-page emphasis.
- If tabs are used for topology/relations/audits, they must be URL-synced.
- If tabs are not used, the topology area must still become a meaningfully better full-detail-page surface.
- Continue to preserve `.worktrees/**` tooling isolation.

## Exact Remaining Problems To Fix

### 1. Sidebar collapse button discoverability

Current problem:

- the button exists
- but it sits at the bottom of the sidebar content
- users have to scroll the left rail to notice it

Required outcome:

- desktop sidebar collapse/expand control becomes sticky or floating at the bottom edge
- it stays visible while the nav content scrolls
- expanded state shows icon + label
- collapsed state shows icon-only affordance with tooltip or equivalent discoverability
- persisted state must keep working

### 2. `/resources` fourth filter still defaults to `all`

Current problem:

- the archive-state filter still reads as `all`
- that violates the self-describing-filter rule

Required outcome:

- default trigger text must be self-describing, such as “归档状态” / “Archive state”
- selected values may replace that default after interaction

### 3. `/resources` still lacks deeper database-oriented filtering

Current problem:

- top-level resource type is not enough
- a reviewer still cannot quickly separate mysql / redis / clickhouse / proxysql style resources

Required outcome:

- add subtype / engine-level filtering using existing frontend/backend truth
- prefer `resourceSubtype`
- do not invent unsupported backend fields
- keep the filter row usable and not overly crowded

### 4. Topology status localization is still leaking English

Current problem:

- in Chinese locale, live pages still show `degraded`
- this was directly observed on `/resources/41000000-0000-0000-0000-000000000013`

Required outcome:

- fix all related topology/status render paths so localized status text is consistent
- check:
  - full detail page
  - detail sheet
  - topology node text
  - any nearby status badges tied to the same value

### 5. Database topology layout is still too flat

Current problem:

- live topology still looks like root in one column and almost everything else stacked in another
- Payment / Config MySQL pages still do not read like a database control-console graph

Required outcome:

- make database-oriented topology visibly respect these layers:
  - entry: `domain_name`, `virtual_ip`
  - proxy: `database_proxy`
  - cluster: `database_cluster`
  - instances: `database_instance`
  - infra/control: `host`, `control_plane_component`
  - consumers: `service`
- it does not need to be a perfect graph-layout engine
- it **must** be materially better than “all non-root nodes in one column”
- non-database graphs must not regress; they still need sane traversal-distance ordering

### 6. Full-detail-page topology still lacks the right surface area

Current problem:

- full detail page gained more height, but topology is still just another section
- it is not yet a strong inspection surface

Required outcome:

- choose one:
  - stronger full-width / larger inline topology surface
  - URL-synced tabs with a dedicated topology view
- if you use tabs:
  - sync to URL
  - shared links must land on the same tab
- if you do not use tabs:
  - the topology surface must become meaningfully better, not just slightly taller
- keep the sheet compact

### 7. `/audits` “recent changes” still is not good enough

Current problem:

- the page no longer hard-breaks as badly as before
- but the “recent changes” area still reads like a cramped side timeline
- it is too narrow and competes with the table

Required outcome:

- rework the audits-page composition
- the table remains width-priority
- do **not** keep a cramped right-rail timeline
- preferred solution:
  - full-width table first
  - full-width timeline / recent-changes section below
- a URL-synced table/timeline switch is acceptable if clearly better
- do not treat `overflow-x-auto` as the main fix

### 8. Environment URL readability must move forward

Current problem:

- frontend URLs still primarily expose `environmentId=<uuid>`
- backend already exposes `id`, `name`, `slug`

Required outcome:

- make a real frontend improvement toward readable environment URLs
- do not break existing backend API contract
- acceptable directions:
  - URL compatibility layer
  - frontend slug support with mapping
  - progressive routing improvement
- do not leave this completely untouched

## Testing

Follow TDD. For each real behavior change:

1. write or update a failing test
2. implement the minimal fix
3. rerun the targeted tests
4. then rerun broader validation

At minimum add or update tests for:

- sticky / always-visible sidebar collapse control
- `/resources` archive filter default label
- `/resources` subtype filtering behavior
- topology status localization
- database-topology semantic layout determinism
- non-database graph ordering staying sane
- audits-page recent-changes composition
- URL-synced tab behavior if tabs are introduced
- environment URL readability improvement if implemented

E2E must cover at least:

- sidebar collapse control is visible and usable without scrolling the sidebar list
- `/resources` subtype-oriented filtering really changes the visible list
- one database topology page renders under the improved layout
- `/audits` revised structure renders correctly
- if you introduce URL tabs or slug-like environment URLs, cover them too

## Required Live Verification Pages

You must manually verify all of these against a live backend:

- `/resources`
- `/databases?environmentId=10000000-0000-0000-0000-000000000001`
- `/audits`
- `/resources/41000000-0000-0000-0000-000000000013`
- `/resources/41000000-0000-0000-0000-000000000010`

## Required Verification Commands

Inside the worktree:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

If possible after branch readiness, also verify from the main checkout:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

If main-checkout verification is not possible, say so explicitly.

## Tooling Isolation

Keep these true:

- `tsconfig.json` excludes `.worktrees`
- `vitest.config.ts` excludes `.worktrees/**`
- `eslint.config.mjs` ignores `.worktrees/**`
- Playwright test discovery stays scoped to local `e2e`
- `next.config.ts` keeps Turbopack root fixed to the frontend repo

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only.

Do not stage:

- `.next`
- `test-results`
- `.worktrees`
- logs
- screenshots
- temp files

## Final Report

Your final report must include:

- worktree path
- branch
- commit hash
- which of the 8 remaining problems were fully fixed
- which were only partially fixed, if any
- exact files changed
- tests added or updated
- results of all required verification commands
- live verification result for all 5 required pages
- whether backend 12.4 changes were incorporated before final verification
- `git status --short --branch`

Do not write a generic “all good” summary. Report against the 8 problems explicitly.
