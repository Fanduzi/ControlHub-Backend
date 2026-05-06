# Phase 27 Database Operational Triage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add operational-signal filtering, abnormal-first sorting, signal counts, and Overview alignment so `/databases` becomes a direct triage surface.

**Architecture:** Frontend-only follow-up. Reuse Phase 26 `buildDatabaseOperationalSignal()` as the single source for list signal semantics, then add pure helper functions for ranking, filtering, sorting, and counts. Wire those helpers into `DatabaseTable` controls and Overview attention copy without adding backend calls.

**Tech Stack:** Next.js App Router, React, TypeScript, TanStack Table, next-intl, Vitest, Playwright E2E governance.

---

## Required Documents

Before implementation, read:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27-database-operational-triage.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-26c-database-signal-and-detail-order.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-05-phase-26-database-list-operational-signal.md
```

Preview reference:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase27-database-signal-filter-sort/content/index.html
```

## Worktree

Use a dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-27-database-operational-triage
```

Branch:

```text
feat/phase-27-database-operational-triage
```

Base:

```text
frontend main at or after f6334ec
```

Do not develop directly on frontend `main`.

## File Structure

Expected frontend files:

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

If current code structure differs, follow existing patterns and report the
mismatch in the final report.

## Phase Constraints

- No backend changes.
- No API contract changes.
- No SQL.
- No write operations.
- No topology layout changes.
- No full-page tabs.
- No removal of search.
- No removal of engine filter.
- No per-row frontend API calls.
- No broad output suppression.
- No AI co-author.

---

## Task 1: Add Pure Signal Triage Helpers

**Files:**
- Modify: `lib/database-operational-signal.ts`
- Modify: `tests/lib/database-operational-signal.test.ts`

- [ ] **Step 1: Add failing tests**

Add tests for helper behavior:

```ts
import {
  buildDatabaseOperationalSignal,
  buildDatabaseSignalRank,
  countDatabaseSignals,
  databaseRowMatchesSignal,
  sortDatabaseRowsBySignal,
} from "@/lib/database-operational-signal";
```

Test cases:

```ts
it("ranks critical instance before healthy cluster", () => {
  expect(buildDatabaseSignalRank(criticalInstance)).toBeLessThan(
    buildDatabaseSignalRank(healthyCluster),
  );
});

it("matches needs_attention filter for cluster with critical member", () => {
  expect(databaseRowMatchesSignal(clusterWithCriticalMember, "needs_attention")).toBe(true);
  expect(databaseRowMatchesSignal(clusterWithCriticalMember, "healthy")).toBe(false);
});

it("matches healthy filter for healthy instance", () => {
  expect(databaseRowMatchesSignal(healthyInstance, "healthy")).toBe(true);
});

it("counts signals across rows", () => {
  expect(countDatabaseSignals([clusterWithCriticalMember, healthyInstance, unknownCluster])).toEqual({
    all: 3,
    needs_attention: 1,
    healthy: 1,
    unknown: 1,
  });
});

it("sorts needs-attention rows before healthy rows with stable names", () => {
  expect(sortDatabaseRowsBySignal([healthyInstance, clusterWithCriticalMember])[0].id).toBe(clusterWithCriticalMember.id);
});
```

- [ ] **Step 2: Run tests and confirm failure**

```bash
npm run test -- tests/lib/database-operational-signal.test.ts
```

Expected: fail because exported helpers do not exist.

- [ ] **Step 3: Implement helper types and functions**

Add:

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

Implement:

```ts
export function buildDatabaseSignalRank(row: ResourceListViewModel): number {
  const signal = buildDatabaseOperationalSignal(row);
  if (signal.reason === "instance_resource_critical") return 10;
  if (signal.reason === "cluster_member_critical") return 20;
  if (signal.reason === "instance_resource_warning") return 30;
  if (signal.reason === "cluster_member_warning") return 40;
  if (
    signal.reason === "instance_lifecycle_stopped" ||
    signal.reason === "instance_lifecycle_degraded" ||
    signal.reason === "cluster_member_lifecycle"
  ) return 50;
  if (signal.level === "unknown") return 70;
  return 100;
}

export function databaseRowMatchesSignal(
  row: ResourceListViewModel,
  filter: DatabaseSignalFilter,
): boolean {
  if (filter === "all") return true;
  const signal = buildDatabaseOperationalSignal(row);
  if (filter === "needs_attention") {
    return signal.level === "needs_attention" || signal.level === "critical";
  }
  if (filter === "healthy") return signal.level === "healthy";
  return signal.level === "unknown";
}
```

Implement sorting and counting with stable secondary sort by `displayName`.

- [ ] **Step 4: Verify helper tests**

```bash
npm run test -- tests/lib/database-operational-signal.test.ts
```

Expected: pass.

- [ ] **Step 5: Commit**

```bash
git add lib/database-operational-signal.ts tests/lib/database-operational-signal.test.ts
git commit -m "feat: add database operational triage helpers"
```

---

## Task 2: Add Signal Filter And Sort Controls To Database Table

**Files:**
- Modify: `components/databases/database-table.tsx`
- Modify: `tests/components/database-table.test.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Add failing component tests**

Add tests asserting:

```text
Engine filter remains visible.
Operational signal filter is visible.
Sort control is visible.
Needs-attention filter hides healthy-only rows.
Abnormal-first sort puts Analytics ClickHouse Cluster Production before healthy-only clusters.
```

Expected copy:

```text
引擎
运维信号
排序
需关注
正常
信息不足
异常优先
名称
更新于
```

- [ ] **Step 2: Run tests and confirm failure**

```bash
npm run test -- tests/components/database-table.test.tsx
```

Expected: fail because new controls do not exist.

- [ ] **Step 3: Add i18n keys**

Add under `tables.databases`:

```json
{
  "signalFilter": "运维信号",
  "signalFilterAll": "全部信号",
  "signalFilterNeedsAttention": "需关注",
  "signalFilterHealthy": "正常",
  "signalFilterUnknown": "信息不足",
  "sortLabel": "排序",
  "sortAbnormalFirst": "异常优先",
  "sortName": "名称",
  "sortUpdated": "更新于",
  "signalCountNeedsAttention": "需关注 {count}",
  "signalCountHealthy": "正常 {count}",
  "signalCountUnknown": "信息不足 {count}"
}
```

Add matching English keys.

- [ ] **Step 4: Wire URL params**

Use params:

```text
databaseSignal=needs_attention|healthy|unknown
databaseSort=abnormal_first|name|updated
```

Rules:

- omit `databaseSignal` for `all`
- omit `databaseSort` for `abnormal_first`
- reset `page` to `1` when either changes
- preserve `environment`, `resourceSubtype`, `q`, and `pageSize`

- [ ] **Step 5: Add controls without removing engine filter**

Keep existing:

```tsx
<Input ... />
<MultiSelectFilter label={t("common.fields.engine")} ... />
```

Add signal and sort controls after engine. Prefer existing select/filter
components. If no single-select component pattern is appropriate, use the
smallest existing shadcn/select pattern already used in the project.

- [ ] **Step 6: Apply filtering and sorting**

Order in `DatabaseTable`:

```text
fullTree
search filter
engine filter
operational signal filter
sort
paginate
```

Preserve cluster tree behavior:

- If a cluster matches signal filter directly, keep the cluster row.
- If a child instance matches signal filter, keep the parent cluster visible.
- Do not duplicate child rows.
- Do not break row expansion.

- [ ] **Step 7: Verify component tests**

```bash
npm run test -- tests/components/database-table.test.tsx tests/lib/database-operational-signal.test.ts
```

Expected: pass.

- [ ] **Step 8: Commit**

```bash
git add components/databases/database-table.tsx tests/components/database-table.test.tsx messages/en.json messages/zh-CN.json
git commit -m "feat: add database signal filter and abnormal-first sort"
```

---

## Task 3: Add Signal Summary Counts

**Files:**
- Modify: `components/databases/database-table.tsx`
- Modify: `tests/components/database-table.test.tsx`

- [ ] **Step 1: Add failing tests**

Assert counts render near controls:

```text
需关注 4
正常 18
信息不足 0
```

Test filtered behavior:

```text
After engine=clickhouse, counts reflect the clickhouse-filtered result.
After search=prod-ch-host-02.internal, counts reflect the search result.
```

- [ ] **Step 2: Implement counts**

Use `countDatabaseSignals()` on the filtered tree before signal filter when
rendering available signal counts, or on fully filtered rows if that is clearer.
Pick one rule and document it in test names. Recommended:

```text
Counts reflect current search + engine + environment context, before applying
the signal filter itself.
```

This lets users see how many rows each signal bucket contains within current
search/engine context.

- [ ] **Step 3: Verify**

```bash
npm run test -- tests/components/database-table.test.tsx
```

- [ ] **Step 4: Commit**

```bash
git add components/databases/database-table.tsx tests/components/database-table.test.tsx
git commit -m "feat: show database signal summary counts"
```

---

## Task 4: Align Overview Attention Queue

**Files:**
- Modify: `components/overview/overview-content.tsx`
- Modify: `tests/components/overview-content.test.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`

- [ ] **Step 1: Inspect current Overview data**

Check whether Overview receives `databaseOperationalSummary` in resource rows.
If it does not, inspect whether Overview uses the same resource list API and can
receive the field after Phase 26A. Do not add backend code in this phase.

- [ ] **Step 2: Add failing tests**

Add a fixture where:

```text
resourceType=database_cluster
healthStatus=healthy
databaseOperationalSummary.criticalMemberCount=1
worstMemberName=Analytics ClickHouse Node 02
```

Assert attention queue reason says:

```text
成员信号：1 个成员严重
```

It must not say only:

```text
健康
```

- [ ] **Step 3: Reuse signal helper**

Use `buildDatabaseOperationalSignal()` for database resources when building
attention reasons. Keep existing generic resource reason logic for non-database
resources.

Avoid root-cause language.

- [ ] **Step 4: Verify**

```bash
npm run test -- tests/components/overview-content.test.tsx tests/lib/database-operational-signal.test.ts
```

- [ ] **Step 5: Commit**

```bash
git add components/overview/overview-content.tsx tests/components/overview-content.test.tsx messages/en.json messages/zh-CN.json
git commit -m "fix: align overview attention with database signals"
```

---

## Task 5: Stabilize E2E For Signal Filters And Dropdowns

**Files:**
- Modify: `e2e/operator-database-workflow.spec.ts`
- Modify: `e2e/console-ux.spec.ts` if existing database filter coverage lives there.

- [ ] **Step 1: Add E2E scenarios**

Cover:

```text
/databases?environment=prod
engine filter remains available
signal filter opens and filters to needs attention
engine=clickhouse + signal=needs_attention shows Analytics ClickHouse Cluster Production
search prod-ch-host-02.internal still works with real fill()
row click opens sheet after signal filter
sheet closes
engine dropdown opens after signal filter
sort dropdown changes to updated/name and remains interactive
```

- [ ] **Step 2: Fix flaky dropdown timing by root cause**

If dropdown close/open is flaky:

- use role-based locators when available
- wait for menu visibility instead of arbitrary sleep
- ensure previous menu is closed before opening next
- do not use broad timeouts
- do not use `evaluate()` to fake user input

- [ ] **Step 3: Run targeted E2E**

```bash
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
```

If the script does not accept file args, use:

```bash
npx playwright test e2e/operator-database-workflow.spec.ts
```

- [ ] **Step 4: Commit**

```bash
git add e2e/operator-database-workflow.spec.ts e2e/console-ux.spec.ts
git commit -m "test: cover database operational triage workflow"
```

---

## Task 6: Full Verification And Live QA

**Files:**
- No code files unless verification finds bugs.

- [ ] **Step 1: Run full checks**

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

- [ ] **Step 2: Live browser verification**

Verify:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/overview
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Check `/databases`:

- engine filter still exists
- signal filter exists
- sort control exists
- abnormal-first default puts needs-attention rows before healthy-only rows
- `运维信号：需关注` filter works
- engine `clickhouse` + signal `需关注` works
- search `prod-ch-host-02.internal`, `8123`, `replica` works
- row click, sheet close, engine dropdown, signal dropdown, sort dropdown work
- API calls use `/__api`
- console/network clean

Check `/overview`:

- database cluster with critical member is represented by member signal copy
- no contradiction with `/databases`

- [ ] **Step 3: Bad-copy and bypass audit**

```bash
rg -n "触发集群需关注|健康=|status=|evaluate\\(|dispatchEvent|input\\.value|setAttribute\\(" components lib app e2e tests
```

Explain any hits.

- [ ] **Step 4: Final report**

Report:

1. Worktree path, branch, commits.
2. Files changed.
3. Final database toolbar controls.
4. Sort/filter/count behavior.
5. Overview alignment behavior.
6. E2E flaky root cause and fix, if touched.
7. Verification matrix.
8. Live browser evidence.
9. Scope confirmation:

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

Do not merge. Do not push. Do not tag.
