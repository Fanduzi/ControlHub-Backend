# Frontend Phase 26B Worker Prompt — Database List Operational Signal + Search Fix

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 26B — Database List Operational Signal + Search Fix**

This phase depends on backend Phase 26A. Do **not** implement fake member
rollups in the frontend. Do **not** start until backend `main` includes the
Phase 26A `databaseOperationalSummary` contract and the local backend used for
verification is running that commit.

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-05-phase-26-database-list-operational-signal.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-05-phase-26-database-list-operational-signal.md
```

Use the approved static preview as the product reference:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase26-database-list-operational-signal/content/index.html
```

If the preview and plan differ, stop and report the mismatch before choosing a
third design.

## Mandatory Worktree Requirement

Do **not** develop directly on frontend `main`.

Create and use this dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-26b-database-list-operational-signal
```

Branch:

```text
feat/phase-26b-database-list-operational-signal
```

Base it on current frontend `main`, after Phase 25 and the latest copy fix are
merged.

Before editing, report:

```bash
git worktree list
git status --short --branch
git log --oneline -5
```

If the worktree already exists, verify it is on the correct branch and clean
before using it.

## Backend Contract Gate

Before writing frontend code, verify the backend contract by curl against the
running backend:

```bash
curl -s 'http://localhost:8080/resources?resourceType=database_cluster&resourceSubtype=clickhouse' | jq .
curl -s 'http://localhost:8080/resources/14' | jq .
```

Use actual seed IDs if `14` is not the ClickHouse cluster in your environment.

Required backend field:

```text
databaseOperationalSummary
```

If the field is absent for database cluster rows, stop and report:

```text
BLOCKED — Backend Phase 26A contract not available
```

Do not work around this with per-row calls or frontend-only inference.

## Goal

Make `/databases` an operational triage page:

1. Users can see which database clusters or instances need attention directly
   in the list.
2. Cluster rows explain whether attention comes from member health, not the
   cluster resource itself.
3. Instance hostname/port are visible under the instance name instead of
   wasting standalone columns.
4. Search no longer freezes the page.
5. Search supports database display name, internal name, engine/subtype,
   hostname, port, role, and cluster child rows.

## Current Bugs To Fix

User-reported live bug:

```text
数据库页面输入搜索内容后页面卡死，页面什么都点不了了。
```

Treat this as P0:

- reproduce first
- capture frontend terminal output
- capture browser console/network errors
- identify root cause
- fix with regression tests
- verify row click, dropdown, sheet close, and navigation still work after search

Do not claim this fixed just because the table visually filters once.

## Required UI Result

Target columns:

```text
资源
运维信号
环境
负责人
引擎
资源自身状态
更新于
```

Do not keep standalone `主机名` and `端口` columns.

For instances, render hostname and port below the display name inside the
resource cell, for example:

```text
Analytics ClickHouse Node 01 Production
实例 · replica
prod-ch-host-01.internal · :8123
```

For clusters, render node count or member summary under the display name:

```text
Analytics ClickHouse Cluster Production
集群 · 2 节点
```

## Operational Signal Copy

Use explicit subject labels. Do not create new contradictions like:

```text
健康 + 严重
```

Instead:

```text
运维信号: 需关注
1 个成员严重
Analytics ClickHouse Node 02 严重
```

For healthy cluster:

```text
运维信号: 正常
成员状态正常
```

For cluster whose own resource is healthy but member is critical:

```text
资源自身状态: 健康 / 运行中
运维信号: 需关注
成员信号: 1 个成员严重
```

For instance:

```text
运维信号: 严重
触发集群需关注
```

Use localized messages in both `messages/zh-CN.json` and `messages/en.json`.
No hardcoded production copy.

## Expected Files

Likely files to create/modify:

```text
types/resource.ts
types/view-models.ts
lib/view-models.ts
lib/database-operational-signal.ts
tests/lib/database-operational-signal.test.ts
components/databases/database-table.tsx
tests/components/database-table.test.tsx
messages/en.json
messages/zh-CN.json
e2e/console-ux.spec.ts
e2e/operator-database-workflow.spec.ts
```

Do not assume these are the only files. Follow existing frontend patterns.

## Implementation Requirements

### 1. Types And Mapping

Add frontend type for backend `databaseOperationalSummary`.

Expose it on database list view models without changing existing field names.

The UI should degrade honestly:

- if summary is present: render operational signal
- if summary is absent on a cluster: render explicit “成员汇总未提供” style copy
- do not silently show “normal” when the backend did not provide summary

### 2. Pure Signal Helper

Create `lib/database-operational-signal.ts`.

It should return a small render model, not JSX:

```ts
type DatabaseOperationalSignalTone = "normal" | "attention" | "critical" | "unknown";

type DatabaseOperationalSignalView = {
  tone: DatabaseOperationalSignalTone;
  labelKey: string;
  summaryKey: string;
  values?: Record<string, string | number>;
  worstMemberName?: string;
  worstMemberStatus?: string;
};
```

Test cases must cover:

- healthy cluster summary
- cluster with one critical member
- cluster with warning members
- cluster with stopped/degraded members
- summary absent on cluster
- critical instance
- healthy instance with host/port metadata

### 3. Database Table Layout

Update `components/databases/database-table.tsx`:

- add `运维信号` / `Operational signal` column
- remove standalone hostname/port columns
- render host/port under instance names
- keep environment, owner, engine, resource self status, updated
- keep existing row expansion behavior
- keep sheet open behavior
- keep resource link behavior
- ensure resource name/link cell does not trap clicks after search

Spacing should be denser but readable. Do not add a huge card layout inside
each row.

### 4. Search Fix And Search Scope

Reproduce the freeze first.

Investigate likely causes, but do not assume:

- URL replacement loop
- debounce + search param sync loop
- table row model recomputation
- expanded row state explosion
- overlay/focus-trap/inert residue
- interaction between search input and sheet state

Add or update tests so search supports:

```text
Analytics ClickHouse Node 02
prod-ch-host-02.internal
8123
replica
clickhouse
```

Expected behavior:

- typing search does not freeze
- dropdowns still open after search
- row click still opens sheet after search
- sheet can close after search
- resource link still navigates after search

If backend search does not yet support host/port globally, frontend may filter
currently loaded database tree rows by local profile fields only if this matches
the existing database page architecture. Report exactly whether search is
server-side, client-side, or hybrid after implementation.

### 5. Tests

Add/update unit tests:

```text
tests/lib/database-operational-signal.test.ts
tests/components/database-table.test.tsx
```

Add/update E2E:

```text
e2e/console-ux.spec.ts
e2e/operator-database-workflow.spec.ts
```

E2E must cover:

- `/databases?environment=prod`
- ClickHouse cluster row shows member-derived operational signal
- instance row shows host/port under resource name
- search by hostname works
- search by port works
- after search, row click opens sheet
- after search, dropdown/filter can open
- no console errors
- no network 4xx/5xx except explicitly expected auth cases

Follow existing E2E governance:

```bash
npm run check:e2e-governance
```

### 6. i18n

Add all user-facing copy to:

```text
messages/en.json
messages/zh-CN.json
```

Run a hardcoded text audit. Forbidden examples:

```text
Healthy
Critical
Operational signal
member rollup missing
```

These strings are allowed in locale files and tests, not production JSX.

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

If full E2E cannot run because backend is unavailable, start backend or report
the exact blocker. Do not skip browser verification for the search freeze.

## Live Browser Verification

Use the running app:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Verify:

- database list shows operational signal column
- ClickHouse cluster no longer looks simply “healthy” when a member is critical
- instance host/port appears under the instance name
- no standalone host/port columns
- search by `prod-ch-host-02.internal` works
- search by `8123` works
- search by `replica` works
- after search, row click opens sheet
- sheet closes by blank click
- dropdowns still open
- resource links still navigate
- browser Back does not break accent color or interactions
- console has zero unexpected errors/warnings
- all browser API calls use `/__api`

## Commit Requirements

Use focused commits. Suggested commit sequence:

```bash
git commit -m "feat: add database operational signal helper"
git commit -m "feat: render database operational signal column"
git commit -m "fix: stabilize database search interactions"
git commit -m "test: cover database operational signal workflow"
```

No `Co-Authored-By`.

Do not tag, push, release, or merge.

## Final Report Required

At completion, report:

1. Worktree path, branch, commit hashes.
2. Backend Phase 26A commit verified.
3. Files changed.
4. Final database list columns.
5. Search root cause and fix.
6. Whether hostname/port search is server-side, client-side, or hybrid.
7. Test coverage added.
8. Verification matrix.
9. Live browser evidence for `/databases`, `/resources/14`, `/resources/22`.
10. Scope confirmation:

```text
- no backend changes
- no API contract changes
- no SQL
- no work orders
- no write operations
- no topology layout changes
- no broad output suppression
- no tag/push/release
- no AI co-author
- clean git status
```
