# Frontend Phase 15A: Console Trust, Table Density, And Topology Correction

You are implementing a focused frontend correction phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

This phase exists because live product review found that multiple console pages technically render but do not communicate trustworthy operational information. Do not treat this as a cosmetic polish pass. Fix the concrete UX and contract failures listed below.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-04-17-console-closeout-checklist.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-7-resource-subtype-filter-contract-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-14-a-console-ia-and-filter-cleanup-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-14-b-database-topology-semantic-ux-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-14-c-env-url-and-e2e-worker.md` if present
- `/Users/fan/JsProjects/ControlHub/app/(console)/overview/page.tsx`
- `/Users/fan/JsProjects/ControlHub/components/overview/overview-content.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources/resource-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/databases/database-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources/resource-detail-sheet.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`
- `/Users/fan/JsProjects/ControlHub/components/blocks/topology-panel.tsx`
- `/Users/fan/JsProjects/ControlHub/lib/topology-mapper.ts`
- `/Users/fan/JsProjects/ControlHub/services/resources.ts`
- `/Users/fan/JsProjects/ControlHub/lib/list-page-search-params.ts`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -10
git worktree list
```

Expected:

- worktree path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is dedicated to this phase, for example `feat/phase-15a-console-trust-topology`
- base includes frontend Phase 14E on `main`
- worktree is clean

Stop and report if the path, branch, base, or cleanliness is wrong.

## Parallel Coordination Rules

Frontend and backend workers cannot communicate during execution. This prompt is self-contained.

- Backend Phase 12.7 is responsible for making `GET /resources?resourceSubtype=...` a real backend filter.
- You may implement frontend UI and request wiring in parallel.
- You may not claim final completion until backend Phase 12.7 has landed on backend `main`, the local backend has been restarted from that updated backend, and live browser verification proves `/databases?environment=prod&page=1&resourceSubtype=mysql` only shows mysql resources.
- If backend Phase 12.7 is not available locally, write an implementation progress report, not a final closeout report.

## Required Skills / Method

Apply these methods explicitly:

- use root-cause debugging for broken filters and topology edge direction before fixing
- use test-driven development for every behavior change
- use a UI layout / information-architecture critique mindset for the overview page before editing it
- use current official `@xyflow/react` documentation or local installed package docs/source before changing handle/edge behavior; do not guess handle IDs or edge routing behavior
- use browser-based live verification before final report

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives.

- Overview should prioritize actionable operational attention, not decorative cards.
- A row in an attention list must explain why it needs attention and must link to the resource detail.
- English status text must not leak in Chinese UI.
- Resource and database tables should be dense operational tables, not two-line marketing cards.
- The first column of resource/database tables must not show raw `externalId` as an unexplained second line.
- `externalId` is configuration metadata. It belongs in detail surfaces and CMDB, not as default table subtitle.
- Default table page size should be `10`.
- Page size choices should be `10`, `20`, `50`.
- Database subtype filtering must work with `resourceSubtype`.
- Topology must follow operator mental models:
  - app/service layer above
  - ProxySQL / VIP / entry layer above database
  - MySQL replication cluster below proxy
  - primary/master on the left
  - replicas expand to the right by replication depth
  - replication edges connect source right handle to target left handle
  - proxy/traffic edges should use vertical handles where appropriate
  - do not connect a right-side target by using a left-side source/target pair that forces lines through nodes
- Do not add database vendor logos/icons in this phase.
- Do not add dictionary editing in this phase.
- Do not add backend API changes in this frontend phase.

## Exact Problems To Fix

### 1. Overview attention queue is not trustworthy

Current live review findings:

- too many items and no pagination
- Chinese page still shows English `Degraded`, `Running`
- items like `e2e-sheet-mnzm0cgz-4vho` show `未知`, but the UI does not explain why unknown status deserves attention
- it is pure display; rows are not clickable
- a table would be clearer than the current card/list display

Required outcome:

- Replace the attention queue presentation with a clear, data-dense table or table-like list.
- Every row must show:
  - resource display name
  - type/subtype
  - environment
  - health/lifecycle badges in current locale
  - a clear reason column, for example `healthStatus=critical`, `lifecycleStatus=degraded`, or a localized explanation
  - updated time if available
- Every row must be clickable or include an explicit link to `/resources/{id}`.
- Add pagination or a strict top-N display with a visible “view all” route/state. If only top-N is shown, label it clearly and do not pretend it is the full queue.
- Do not include resources in the attention queue solely because they are `unknown` unless the UI explains why `unknown` is actionable. Prefer excluding `unknown`-only rows from attention unless paired with a real warning/critical/degraded signal.
- Do not show filler strings such as “No supplemental resource summary...” or raw test-looking data without context.

### 2. Overview environment summary is unclear and duplicates posture

Current live review findings:

- “Production 共 16 个资源 / 1 降级 / 4 告警 / 11 托管资产总数” is hard to interpret.
- It duplicates resource posture but appears below more important content.
- It shows “前 3 / 5 项” without pagination and without explaining why those 3 are shown.
- If something needs attention, placing it at the bottom is wrong.

Required outcome:

Choose one of these concrete approaches and document which one you chose:

- Preferred: remove the environment summary section from overview if it does not add a distinct decision value beyond the posture + attention table.
- Acceptable: convert it to compact environment health cards that only show aggregate counts and a link/filter into resources, not embedded partial resource lists.

If retained:

- It must not duplicate the attention queue.
- It must explain what it is for.
- It must not show an unpaginated partial list of resources.
- Counts must be labeled clearly and consistently.

### 3. Resource and database tables are too tall

Current live review finding:

- Rows are too high because the first column shows two lines:
  - `Payment MySQL Primary Production`
  - `dbaas-payment-mysql-primary-prod-inst`
- The second line is `externalId`, but it is unlabeled and not visible in the detail sheet, so it looks like unexplained duplicate identity.

Required outcome:

- Resource table first column is single-line by default:
  - display `displayName`
  - do not show raw `externalId` as subtitle
  - do not show raw UUID as subtitle
- Database table first column is single-line by default:
  - display `displayName`
  - do not show raw `externalId` as subtitle
- Reduce table row vertical padding enough that default 10 rows fit comfortably on a normal laptop viewport.
- If secondary metadata is necessary, move it into existing explicit columns, badges, tooltip, or detail sheet. Do not create another unexplained second line.

### 4. `externalId` detail consistency

Required outcome:

- Resource detail sheet must show `externalId` with an explicit localized label if it exists.
- Database detail sheet / shared resource detail sheet must show the same value consistently.
- Full detail page already shows external ID; keep or improve it.
- CMDB may continue to show external ID because CMDB is configuration metadata oriented.

### 5. Default page size and page-size options

Required outcome:

- Resource table default page size: `10`
- Database table default page size: `10`
- User-selectable page size options: `10`, `20`, `50`
- Preserve `pageSize` in URL.
- Changing page size resets `page=1`.
- Existing pagination tests must be updated or extended.

### 6. Database resource subtype filter must actually work

Current failing live URL:

`http://localhost:3000/databases?environment=prod&page=1&resourceSubtype=mysql`

Required outcome:

- When `resourceSubtype=mysql`, the database table shows only mysql rows.
- When `resourceSubtype=clickhouse`, the database table shows only clickhouse rows.
- When repeated params are present, for example `resourceSubtype=mysql&resourceSubtype=clickhouse`, the UI and backend use OR semantics.
- Preserve readable `environment=prod` in browser URL.
- Do not leak `environmentId=<uuid>` back into the browser URL.

Final verification depends on backend Phase 12.7. Do not claim final completion without live verification against backend Phase 12.7.

### 7. Topology layout and edge attachment are still wrong

Current live review findings:

- The user supplied orchestrator-style references and ASCII diagrams.
- The current graph still has edge-through-node cases.
- Root/cluster nodes show connection dots on both sides, but edges connect to the wrong side.
- A replica to the right should be connected from the primary/right side to the replica/left side.
- ProxySQL should be above the MySQL replication cluster, not mixed into the same horizontal chain.

Target topology model:

```text
Application / Service layer
          |
          v
ProxySQL / VIP / entry layer
          |
          v
MySQL Replication Cluster
  primary/master on left
  replicas expand right by replicationDepth
  replication edges: source-right -> target-left

Control plane / orchestrator separate from replication chain
Host placement subordinate to database path
```

Required outcome:

- Implement explicit handle placement and edge handle selection where `@xyflow/react` supports it:
  - replication source: right handle
  - replication target: left handle
  - traffic/proxy vertical path: bottom-to-top or top-to-bottom handles as appropriate
  - management/monitoring/control-plane edges should not cut through the replication corridor
- Do not set edge handle IDs unless matching `Handle id` props actually exist on nodes.
- If custom handle IDs are used, add tests that assert generated edges reference existing handle IDs.
- Use current official `@xyflow/react` docs or installed package source for handle/edge APIs before implementation.
- Preserve fullscreen/expanded topology mode and URL sync from Phase 14B.
- Preserve the generic non-database fallback layout.

### 8. Topology visual grouping

Required outcome:

- Visually separate high-level bands:
  - Application Layer
  - ProxySQL / Entry Layer
  - MySQL Replication Cluster
  - Orchestrator / Control Plane
  - Host / Placement
- This can be done with group labels, subtle background bands, section headers, or React Flow parent/group nodes if practical.
- Do not make the inline panel too crowded. The expanded topology view is the primary analysis surface.

### 9. Chinese localization cleanup

Required outcome:

- Chinese UI must not show raw English status values such as `Degraded`, `Running`.
- Check overview, resource table, database table, topology node labels, detail sheet, and pagination/page-size controls.
- Add i18n keys where needed.
- Tests must assert at least the known leaked values no longer appear in Chinese mode.

## TDD Requirements

Use TDD. Add failing tests first.

At minimum add or update tests for:

### Overview

- attention rows exclude or explain `unknown`-only resources
- attention rows show localized health/lifecycle values
- attention rows link to resource detail
- attention list paginates or clearly shows top-N with correct label
- environment summary is removed or converted to non-duplicative aggregate cards

### Tables

- resource table first column does not render `externalId` subtitle
- database table first column does not render `externalId` subtitle
- detail sheet renders `externalId` with explicit label
- default page size is `10`
- page size options are `10`, `20`, `50`
- changing page size sets `page=1`

### Database subtype filtering

- `resourceSubtype=mysql` is preserved in URL and service request
- repeated `resourceSubtype` values are serialized as repeated params
- database table only renders rows matching selected subtype in mocked/service-level tests

### Topology

- database topology places proxy/entry band above database cluster/replication band
- primary is left of replicas
- replicas with `replicationDepth=1` are right of primary
- deeper replicas are further right
- replication edges use source-right to target-left handles or equivalent verified handle model
- no generated edge references a nonexistent handle ID
- generic non-database fallback still preserves traversal-distance ordering
- expanded topology mode still works

## Required Verification

Run all:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npx playwright test
```

If full Playwright is blocked by backend availability, do not write a final closeout report. Start the required backend and rerun. This phase requires live browser verification.

## Live Browser Verification

Use the local backend and frontend. Verify in Chinese locale.

Required pages:

- `/overview`
- `/resources`
- `/databases?environment=prod&page=1&resourceSubtype=mysql`
- `/databases?environment=prod&page=1&resourceSubtype=clickhouse`
- at least one MySQL cluster resource detail page with topology expanded
- at least one database instance detail sheet

Required live checks:

- overview attention rows are clickable and show reasons
- overview does not show `Degraded`, `Running`, or filler English/status text in Chinese mode
- overview environment section is either removed or clearly non-duplicative
- resource table defaults to 10 rows
- database table defaults to 10 rows
- table first columns are single-line and no longer show unlabeled external IDs
- detail sheet shows external ID with a localized label
- mysql subtype URL shows only mysql database resources
- clickhouse subtype URL shows only clickhouse database resources
- browser URL uses `environment=prod`, not `environmentId=<uuid>`
- topology expanded view shows proxy/entry above database replication
- primary/master is visually left of replicas
- replication edges do not connect through the wrong side of nodes
- browser console has no React Flow handle warnings and no NaN geometry warnings

Use screenshots only as temporary evidence. Do not commit screenshots.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage only intended frontend files. Do not commit screenshots, `.next`, `test-results`, logs, local env files, or `.worktrees`.

## Commit

Commit after verification passes.

Suggested message:

```bash
git commit -m "fix: restore console trust with dense tables and corrected topology layout (Phase 15A)"
```

Do not add AI co-author trailers.

## Final Report Requirements

Only write a final closeout report if all Closeout Gate requirements from the shared guardrails are satisfied.

The final report must include:

- commit hash
- worktree path and branch
- clean git status
- changed files
- before/after summary for overview
- table density changes
- `externalId` display decision and where it is now shown
- page-size behavior
- database subtype filtering live verification
- topology layout/handle model used
- proof that Chinese status leaks are fixed
- command results for all required verification
- Playwright result
- live browser verification results
- negative scope confirmation:
  - did not change backend code
  - did not add database logos/icons
  - did not add dictionary editing
  - did not add topology editing
  - did not tag, push, release, or add AI co-author
- next phase input, if any remains
