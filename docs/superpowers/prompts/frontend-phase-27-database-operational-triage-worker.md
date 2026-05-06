# Frontend Phase 27 Worker Prompt — Database Operational Triage

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 27 — Database Operational Triage**

This is a frontend-only follow-up to Phase 26A/B/C. Backend Phase 26A already
provides `databaseOperationalSummary` for database clusters. Frontend Phase 26B
and 26C already render correct operational signals and fix instance signal
fallbacks.

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27-database-operational-triage.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-06-phase-27-database-operational-triage.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-26c-database-signal-and-detail-order.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-05-phase-26-database-list-operational-signal.md
```

Preview reference:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase27-database-signal-filter-sort/content/index.html
```

Important preview clarification:

- The current database table has `Search input + Engine MultiSelectFilter`.
- Environment is page/topbar URL context (`?environment=prod`), not a
  table-local dropdown.
- Phase 27 must preserve search, environment context, and engine filtering.
- Phase 27 adds operational signal filtering and abnormal-first sorting.
- Do **not** replace or remove the engine filter.

## Mandatory Worktree Requirement

Do **not** develop directly on frontend `main`.

Create and use this dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-27-database-operational-triage
```

Branch:

```text
feat/phase-27-database-operational-triage
```

Base it on current frontend `main`, at or after:

```text
f6334ec fix: clarify database instance signals and cluster detail order
```

Before editing, report:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git log --oneline -5
git worktree list
```

Create the worktree:

```bash
git worktree add .worktrees/frontend-phase-27-database-operational-triage -b feat/phase-27-database-operational-triage main
cd .worktrees/frontend-phase-27-database-operational-triage
git status --short --branch
git log --oneline -5
```

If the worktree already exists, verify it is on the correct branch and clean
before using it. Do not overwrite user changes.

## Backend Contract Gate

Backend should be running on `localhost:8080` and include Phase 26A commit:

```text
6d367a3 feat: add database operational rollup read model
```

Verify:

```bash
curl -s http://localhost:8080/health
curl -s 'http://localhost:8080/resources/14' | jq '.resource.databaseOperationalSummary'
```

Expected for resource `14`:

```json
{
  "memberCount": 2,
  "criticalMemberCount": 1,
  "warningMemberCount": 0,
  "stoppedMemberCount": 0,
  "degradedMemberCount": 0,
  "unknownRoleCount": 0,
  "primaryMemberCount": 0,
  "replicaMemberCount": 2,
  "worstMemberName": "Analytics ClickHouse Node 02",
  "worstMemberStatus": "critical"
}
```

If backend is unavailable, report the blocker. Do not change backend code in
this phase.

## Goal

Make `/databases` answer:

```text
What should I look at first?
```

Deliver:

1. Operational signal filter.
2. Abnormal-first sorting.
3. Signal-aware summary counts.
4. Overview attention queue alignment with database operational signal.
5. Stabilized dropdown E2E path without broad timeouts or input bypass.

## Non-Negotiable Product Constraints

Do not remove or regress existing controls:

```text
Search input: keep
Engine filter: keep
Environment URL/topbar context: keep
```

Add new controls:

```text
Operational signal filter: all / needs_attention / healthy / unknown
Sort: abnormal_first / name / updated
```

Do not:

- remove engine filtering
- move environment into a fake table-local dropdown
- add backend calls per row
- fabricate backend rollups
- change topology layout
- add tabs
- add broad output suppression
- use `evaluate()` to bypass search input

## Expected Files

Likely files:

```text
lib/database-operational-signal.ts
tests/lib/database-operational-signal.test.ts
components/databases/database-table.tsx
tests/components/database-table.test.tsx
components/overview/overview-content.tsx
tests/components/overview-content.test.tsx
messages/en.json
messages/zh-CN.json
e2e/operator-database-workflow.spec.ts
e2e/console-ux.spec.ts
```

If current code structure differs, follow existing patterns and report it.

## Required Implementation

### 1. Pure triage helpers

Extend `lib/database-operational-signal.ts`.

Add types:

```ts
export type DatabaseSignalFilter = "all" | "needs_attention" | "healthy" | "unknown";
export type DatabaseSignalSort = "abnormal_first" | "name" | "updated";

export type DatabaseSignalCounts = {
  all: number;
  needs_attention: number;
  healthy: number;
  unknown: number;
};
```

Add helpers:

```ts
buildDatabaseSignalRank(row)
databaseRowMatchesSignal(row, filter)
sortDatabaseRowsBySignal(rows, sort)
countDatabaseSignals(rows)
```

Ranking rules:

1. critical instance/resource
2. cluster critical member
3. warning instance/resource
4. cluster warning member
5. stopped/degraded lifecycle
6. unknown/information insufficient
7. healthy

Use stable secondary sort by display name or updated time depending on selected
sort.

### 2. Database table controls

In `components/databases/database-table.tsx`:

Preserve:

```tsx
<Input ... />
<MultiSelectFilter label={t("common.fields.engine")} ... />
```

Add:

```text
Operational signal filter
Sort control
Signal counts
```

URL params:

```text
databaseSignal=needs_attention|healthy|unknown
databaseSort=abnormal_first|name|updated
```

Rules:

- omit `databaseSignal` for `all`
- omit `databaseSort` for `abnormal_first`
- changing signal or sort resets `page` to `1`
- preserve `environment`, `resourceSubtype`, `q`, and `pageSize`
- signal filter combines with search and engine filter

Filtering/sorting order:

```text
fullTree
search filter
engine filter
signal filter
sort
paginate
```

Tree behavior:

- If a cluster matches directly, keep the cluster row.
- If a child instance matches, keep the parent cluster visible.
- Do not duplicate child rows.
- Do not break expansion.

### 3. Signal counts

Show counts near toolbar:

```text
需关注 N
正常 N
信息不足 N
```

Counts should reflect current:

```text
environment context + search + engine filter
```

Recommended rule:

```text
Counts are computed after search and engine filters, before applying the signal filter itself.
```

This lets users see how many rows exist in each signal bucket inside the current
context.

### 4. Overview alignment

Update `components/overview/overview-content.tsx` so database attention queue
uses the same database signal semantics.

If a database cluster has:

```text
resource health: healthy
databaseOperationalSummary.criticalMemberCount: 1
```

Overview reason should say:

```text
成员信号：1 个成员严重
```

It must not imply the cluster resource itself is critical or healthy-only.

Do not overclaim causality. Do not say audit events caused the issue.

### 5. E2E dropdown stabilization

If dropdown timing remains flaky:

- use role/test-id locators that target the active popup
- wait for visibility instead of sleeping
- close previous popup before opening another
- keep console/network guards
- do not use broad timeouts
- do not remove tests
- do not bypass search input with `evaluate()`

## Tests Required

### Unit

Update `tests/lib/database-operational-signal.test.ts`:

- rank critical instance before healthy cluster
- match needs_attention filter for cluster with critical member
- match healthy filter for healthy instance
- count all/needs_attention/healthy/unknown
- sort abnormal first
- sort by name
- sort by updated

### Component

Update `tests/components/database-table.test.tsx`:

- engine filter remains visible
- operational signal filter visible
- sort control visible
- needs_attention filter hides healthy-only rows
- abnormal-first puts attention rows first
- engine + signal filters combine
- counts render and update with search/engine context
- no standalone host/port columns reintroduced

### Overview

Update `tests/components/overview-content.test.tsx`:

- database cluster with critical member appears as member signal
- healthy-only database cluster does not appear as attention item unless other
  existing rules require it
- no `健康=严重` or raw field-style copy

### E2E

Update `e2e/operator-database-workflow.spec.ts` and possibly
`e2e/console-ux.spec.ts`:

- `/databases?environment=prod` shows engine filter, signal filter, sort control
- default abnormal-first order puts needs-attention rows before healthy-only rows
- signal filter `需关注` works
- engine `clickhouse` + signal `需关注` works
- search `prod-ch-host-02.internal`, `8123`, `replica` still works using real input
- row click opens sheet after signal filter
- sheet closes
- engine dropdown opens after signal filter
- signal dropdown and sort dropdown remain interactive
- Overview attention queue copy aligns with database signal

## Verification Commands

Run:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

If full E2E has failures, do not hide them. Either fix them or provide
main-branch comparison evidence proving they are pre-existing.

## Live Browser Verification

Use:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/overview
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Verify `/databases`:

- search visible
- engine filter visible
- signal filter visible
- sort control visible
- abnormal-first default visible in row order
- `运维信号：需关注` filter hides healthy-only rows
- engine `clickhouse` + signal `需关注` shows ClickHouse cluster and Node 02
- search hostname/port/role does not freeze
- row click, sheet close, engine dropdown, signal dropdown, sort dropdown work
- API calls use `/__api`
- console/network clean

Verify `/overview`:

- database cluster with critical member uses member-signal copy
- no contradiction with `/databases`

Verify details:

- `/resources/14` still has `决策台 → 集群成员 → 资源拓扑`
- `/resources/22` still has no cluster members table

## Audit Commands

Run:

```bash
rg -n "触发集群需关注|健康=|status=|evaluate\\(|dispatchEvent|input\\.value|setAttribute\\(" components lib app e2e tests
```

Explain all hits. Production code must not hardcode bad copy or bypass search
input.

## Commit Guidance

Suggested commits:

```bash
git commit -m "feat: add database operational triage helpers"
git commit -m "feat: add database signal filter and abnormal-first sort"
git commit -m "feat: show database signal summary counts"
git commit -m "fix: align overview attention with database signals"
git commit -m "test: cover database operational triage workflow"
```

No `Co-Authored-By`.

Do not tag, push, release, or merge.

## Final Report Required

Report:

1. Worktree path.
2. Branch.
3. Commit hashes.
4. Files changed.
5. Final database toolbar controls.
6. Sorting/filtering/count behavior.
7. Overview alignment behavior.
8. E2E flaky root cause and fix, if touched.
9. Verification matrix.
10. Live browser evidence.
11. Audit result.
12. Scope confirmation:

```text
- no backend changes
- no API contract changes
- no SQL
- no work orders
- no topology layout changes
- no full-page tabs
- no broad output suppression
- no tag/push/release
- no AI co-author
- clean git status
```
