# Phase 26 Database List Operational Signal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `/databases` show list-level operational signals, remove wasted host/port columns, support host/port/role search, and fix the database search freeze.

**Architecture:** Two-phase delivery. Backend first adds read-only database operational rollup fields to the resource list read model and OpenAPI. Frontend then consumes those fields, renders an operational signal column, moves instance host/port under resource names, and fixes search interaction stability with focused E2E coverage.

**Tech Stack:** Go backend with existing resource repository/handler/OpenAPI patterns; Next.js App Router frontend, React, TypeScript, TanStack Table, next-intl, Vitest, Playwright E2E governance.

---

## File Structure

### Backend Phase 26A

- Modify `internal/model/pagination.go`
  - Add database operational summary model on resource list/detail response types.
- Modify resource repository files, likely:
  - `internal/repository/mysql/resource_repository.go`
  - Add rollup query/projection for database clusters.
- Modify API test fakes:
  - `internal/api/test_server.go`
  - Any fake repository constructors that build resource rows.
- Modify OpenAPI:
  - `internal/openapi/openapi.yaml`
- Add/update tests:
  - `internal/api/resource_handler_test.go`
  - `internal/integration/resource_test.go`
  - repository tests if present for resource list projections.

### Frontend Phase 26B

- Modify `types/resource.ts`
  - Add `DatabaseOperationalSummary`.
- Modify `types/view-models.ts`
  - Surface summary in `ResourceListViewModel`.
- Modify `lib/view-models.ts`
  - Map backend field to list/detail view models.
- Create `lib/database-operational-signal.ts`
  - Pure helper deriving row signal copy/tone from resource + summary.
- Add `tests/lib/database-operational-signal.test.ts`
  - Helper coverage.
- Modify `components/databases/database-table.tsx`
  - Add operational signal column.
  - Remove standalone hostname/port columns.
  - Render host/port under instance resource name.
  - Extend search.
  - Fix search freeze root cause.
- Modify `messages/en.json`
  - Add table and signal copy.
- Modify `messages/zh-CN.json`
  - Add matching Chinese copy.
- Modify tests:
  - `tests/components/database-table.test.tsx`
  - `e2e/operator-database-workflow.spec.ts`
  - Possibly `e2e/console-ux.spec.ts` if database search coverage exists.

## Phase Constraints

- Use dedicated worktrees.
- Backend and frontend must be separate branches/worktrees.
- No write operations.
- No work orders.
- No manual SQL execution.
- No topology layout changes.
- No full-page tabs.
- No frontend per-row API requests for rollups.
- No broad output suppression.
- No AI co-author.

---

## Backend Phase 26A: Database Operational Rollup Read Model

### Task A1: Add Database Operational Summary Types

**Files:**
- Modify: `internal/model/pagination.go`
- Test: existing Go compile tests.

- [ ] **Step 1: Run GitNexus impact before editing symbols**

In backend repo:

```bash
cd /Users/fan/GolangProjects/ControlHub
```

Run GitNexus impact for the resource response/model symbols you will edit.
Report d=1 callers and risk before editing.

- [ ] **Step 2: Add model fields**

Add a read-only summary type:

```go
type DatabaseOperationalSummary struct {
    MemberCount         int64  `json:"memberCount"`
    CriticalMemberCount int64  `json:"criticalMemberCount"`
    WarningMemberCount  int64  `json:"warningMemberCount"`
    StoppedMemberCount  int64  `json:"stoppedMemberCount"`
    DegradedMemberCount int64  `json:"degradedMemberCount"`
    UnknownRoleCount    int64  `json:"unknownRoleCount"`
    PrimaryMemberCount  int64  `json:"primaryMemberCount"`
    ReplicaMemberCount  int64  `json:"replicaMemberCount"`
    WorstMemberID       *int64 `json:"worstMemberId,omitempty"`
    WorstMemberName     string `json:"worstMemberName,omitempty"`
    WorstMemberStatus   string `json:"worstMemberStatus,omitempty"`
}
```

Attach to resource response/list model:

```go
DatabaseOperationalSummary *DatabaseOperationalSummary `json:"databaseOperationalSummary,omitempty"`
```

- [ ] **Step 3: Compile**

Run:

```bash
go test -count=1 ./...
```

Expected: compile failures where fake/test models need new fields or no failures
if the field is optional.

- [ ] **Step 4: Commit after compiling**

Do not commit until repository and handler tests are ready in later tasks if
this task alone leaves tests failing.

### Task A2: Compute Rollups In MySQL Resource Repository

**Files:**
- Modify: `internal/repository/mysql/resource_repository.go`
- Test: repository/integration tests.

- [ ] **Step 1: Add failing integration test**

In `internal/integration/resource_test.go`, add a test using seed data for
`analytics-ch-cluster-prod` or the actual seeded cluster name/id.

Expected assertions:

```go
require.NotNil(t, cluster.DatabaseOperationalSummary)
assert.Equal(t, int64(2), cluster.DatabaseOperationalSummary.MemberCount)
assert.Equal(t, int64(1), cluster.DatabaseOperationalSummary.CriticalMemberCount)
assert.Equal(t, "Analytics ClickHouse Node 02", cluster.DatabaseOperationalSummary.WorstMemberName)
assert.Equal(t, "critical", cluster.DatabaseOperationalSummary.WorstMemberStatus)
```

Also assert a healthy cluster has zero critical/warning counts.

- [ ] **Step 2: Run failing integration test**

Run:

```bash
make test-integration
```

Expected: fail because summary is absent.

- [ ] **Step 3: Implement rollup query**

In the MySQL repository, compute rollups for database clusters in list/detail
projection. Use existing resource/member relations:

```sql
SELECT
  rr.to_resource_id AS cluster_id,
  COUNT(*) AS member_count,
  SUM(CASE WHEN child.health_status = 'critical' THEN 1 ELSE 0 END) AS critical_member_count,
  SUM(CASE WHEN child.health_status = 'warning' THEN 1 ELSE 0 END) AS warning_member_count,
  SUM(CASE WHEN child.lifecycle_status = 'stopped' THEN 1 ELSE 0 END) AS stopped_member_count,
  SUM(CASE WHEN child.lifecycle_status = 'degraded' THEN 1 ELSE 0 END) AS degraded_member_count
FROM resource_relations rr
JOIN resources child ON child.id = rr.from_resource_id
WHERE rr.relation_type = 'member_of'
  AND rr.to_resource_id IN (?)
GROUP BY rr.to_resource_id
```

For role counts, use database instance profile table if available:

```sql
SUM(CASE WHEN LOWER(instance.role) IN ('primary','master','writer') THEN 1 ELSE 0 END)
SUM(CASE WHEN LOWER(instance.role) IN ('replica','secondary','reader') THEN 1 ELSE 0 END)
SUM(CASE WHEN instance.role IS NULL OR instance.role = '' THEN 1 ELSE 0 END)
```

For worst member:

- Prefer critical over warning over unknown over healthy.
- If multiple, stable sort by display name or id.
- Implement in Go after fetching member rows if SQL becomes too complex.

- [ ] **Step 4: Update fakes**

Update `internal/api/test_server.go` fake resource list/detail behavior so tests
can return `DatabaseOperationalSummary`.

- [ ] **Step 5: Run backend tests**

Run:

```bash
go test -count=1 ./...
make test-integration
```

- [ ] **Step 6: Commit**

```bash
git add internal/model/pagination.go internal/repository/mysql/resource_repository.go internal/api/test_server.go internal/integration/resource_test.go
git commit -m "feat: add database operational rollup read model"
```

### Task A3: OpenAPI And Contract Tests

**Files:**
- Modify: `internal/openapi/openapi.yaml`
- Modify: `internal/api/resource_handler_test.go`

- [ ] **Step 1: Add OpenAPI schema**

Add:

```yaml
DatabaseOperationalSummary:
  type: object
  description: Derived read-only database member rollup for list/operator views.
  properties:
    memberCount:
      type: integer
      format: int64
    criticalMemberCount:
      type: integer
      format: int64
    warningMemberCount:
      type: integer
      format: int64
    stoppedMemberCount:
      type: integer
      format: int64
    degradedMemberCount:
      type: integer
      format: int64
    unknownRoleCount:
      type: integer
      format: int64
    primaryMemberCount:
      type: integer
      format: int64
    replicaMemberCount:
      type: integer
      format: int64
    worstMemberId:
      type: integer
      format: int64
      nullable: true
    worstMemberName:
      type: string
    worstMemberStatus:
      type: string
```

Reference it from the Resource schema:

```yaml
databaseOperationalSummary:
  $ref: '#/components/schemas/DatabaseOperationalSummary'
```

- [ ] **Step 2: Add handler tests**

Add tests that `GET /resources` includes the rollup for database cluster rows
and does not require a new query parameter.

- [ ] **Step 3: Run validation**

Run:

```bash
make openapi-validate
make test-openapi-fuzz
go test -count=1 ./...
go vet ./...
go build ./...
```

- [ ] **Step 4: GitNexus detect changes**

Before committing:

```bash
npx gitnexus detect-changes
```

Confirm affected flows are resource list/detail API and OpenAPI only.

- [ ] **Step 5: Commit**

```bash
git add internal/openapi/openapi.yaml internal/api/resource_handler_test.go
git commit -m "test: cover database operational rollup contract"
```

---

## Frontend Phase 26B: Database List Operational Signal + Search Stability

### Task B1: Add Frontend Types And View Model Mapping

**Files:**
- Modify: `types/resource.ts`
- Modify: `types/view-models.ts`
- Modify: `lib/view-models.ts`
- Test: `tests/lib/view-models.test.ts`

- [ ] **Step 1: Add failing view-model test**

In `tests/lib/view-models.test.ts`, add a resource with:

```ts
databaseOperationalSummary: {
  memberCount: 2,
  criticalMemberCount: 1,
  warningMemberCount: 0,
  stoppedMemberCount: 0,
  degradedMemberCount: 0,
  unknownRoleCount: 0,
  primaryMemberCount: 0,
  replicaMemberCount: 2,
  worstMemberId: 23,
  worstMemberName: "Analytics ClickHouse Node 02",
  worstMemberStatus: "critical",
}
```

Assert the view model preserves the same values.

- [ ] **Step 2: Run failing test**

```bash
npm run test -- tests/lib/view-models.test.ts
```

- [ ] **Step 3: Add types**

In `types/resource.ts`:

```ts
export type DatabaseOperationalSummary = {
  memberCount: number;
  criticalMemberCount: number;
  warningMemberCount: number;
  stoppedMemberCount: number;
  degradedMemberCount: number;
  unknownRoleCount: number;
  primaryMemberCount: number;
  replicaMemberCount: number;
  worstMemberId?: number;
  worstMemberName?: string;
  worstMemberStatus?: string;
};
```

Add optional field to `Resource`:

```ts
databaseOperationalSummary?: DatabaseOperationalSummary;
```

In `types/view-models.ts`, add same optional field to `ResourceListViewModel`.

In `lib/view-models.ts`, map the field directly.

- [ ] **Step 4: Run test and commit**

```bash
npm run test -- tests/lib/view-models.test.ts
git add types/resource.ts types/view-models.ts lib/view-models.ts tests/lib/view-models.test.ts
git commit -m "feat: map database operational summary"
```

### Task B2: Add Operational Signal Helper

**Files:**
- Create: `lib/database-operational-signal.ts`
- Create: `tests/lib/database-operational-signal.test.ts`

- [ ] **Step 1: Write helper tests**

Create `tests/lib/database-operational-signal.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { buildDatabaseOperationalSignal } from "@/lib/database-operational-signal";
import type { ResourceListViewModel } from "@/types/view-models";

function row(overrides: Partial<ResourceListViewModel>): ResourceListViewModel {
  return {
    id: 14,
    name: "analytics-ch-cluster-prod",
    displayName: "Analytics ClickHouse Cluster Production",
    resourceType: "database_cluster",
    resourceSubtype: "clickhouse",
    environmentName: "Production",
    ownerName: "DBA Team",
    healthStatus: "healthy",
    lifecycleStatus: "running",
    labels: {},
    source: "manual",
    updatedAt: "2026-04-14T06:56:00Z",
    ...overrides,
  } as ResourceListViewModel;
}

describe("buildDatabaseOperationalSignal", () => {
  it("marks a healthy cluster with critical member as needs attention", () => {
    const signal = buildDatabaseOperationalSignal(row({
      databaseOperationalSummary: {
        memberCount: 2,
        criticalMemberCount: 1,
        warningMemberCount: 0,
        stoppedMemberCount: 0,
        degradedMemberCount: 0,
        unknownRoleCount: 0,
        primaryMemberCount: 0,
        replicaMemberCount: 2,
        worstMemberId: 23,
        worstMemberName: "Analytics ClickHouse Node 02",
        worstMemberStatus: "critical",
      },
    }));

    expect(signal.level).toBe("needs_attention");
    expect(signal.memberSignal).toBe("critical");
    expect(signal.memberCount).toBe(1);
    expect(signal.worstMemberName).toBe("Analytics ClickHouse Node 02");
  });

  it("uses resource self warning when resource is warning", () => {
    const signal = buildDatabaseOperationalSignal(row({
      healthStatus: "warning",
      lifecycleStatus: "degraded",
    }));

    expect(signal.level).toBe("needs_attention");
    expect(signal.reason).toBe("resource_status");
  });

  it("marks healthy cluster with no abnormal members as healthy", () => {
    const signal = buildDatabaseOperationalSignal(row({
      databaseOperationalSummary: {
        memberCount: 3,
        criticalMemberCount: 0,
        warningMemberCount: 0,
        stoppedMemberCount: 0,
        degradedMemberCount: 0,
        unknownRoleCount: 0,
        primaryMemberCount: 1,
        replicaMemberCount: 2,
      },
    }));

    expect(signal.level).toBe("healthy");
    expect(signal.reason).toBe("no_abnormal_members");
  });
});
```

- [ ] **Step 2: Run failing test**

```bash
npm run test -- tests/lib/database-operational-signal.test.ts
```

- [ ] **Step 3: Implement helper**

Create `lib/database-operational-signal.ts`:

```ts
import type { ResourceListViewModel } from "@/types/view-models";

export type DatabaseOperationalSignal = {
  level: "healthy" | "needs_attention" | "critical" | "unknown";
  reason:
    | "resource_status"
    | "critical_member"
    | "warning_member"
    | "member_lifecycle"
    | "no_abnormal_members"
    | "unknown";
  memberSignal?: "critical" | "warning" | "lifecycle";
  memberCount?: number;
  worstMemberName?: string;
};

export function buildDatabaseOperationalSignal(
  row: ResourceListViewModel,
): DatabaseOperationalSignal {
  if (row.healthStatus === "critical") {
    return { level: "critical", reason: "resource_status" };
  }
  if (
    row.healthStatus === "warning" ||
    row.lifecycleStatus === "stopped" ||
    row.lifecycleStatus === "degraded"
  ) {
    return { level: "needs_attention", reason: "resource_status" };
  }

  const summary = row.databaseOperationalSummary;
  if (summary?.criticalMemberCount && summary.criticalMemberCount > 0) {
    return {
      level: "needs_attention",
      reason: "critical_member",
      memberSignal: "critical",
      memberCount: summary.criticalMemberCount,
      worstMemberName: summary.worstMemberName,
    };
  }
  if (summary?.warningMemberCount && summary.warningMemberCount > 0) {
    return {
      level: "needs_attention",
      reason: "warning_member",
      memberSignal: "warning",
      memberCount: summary.warningMemberCount,
      worstMemberName: summary.worstMemberName,
    };
  }
  const lifecycleCount =
    (summary?.stoppedMemberCount ?? 0) + (summary?.degradedMemberCount ?? 0);
  if (lifecycleCount > 0) {
    return {
      level: "needs_attention",
      reason: "member_lifecycle",
      memberSignal: "lifecycle",
      memberCount: lifecycleCount,
      worstMemberName: summary?.worstMemberName,
    };
  }

  if (summary) {
    return { level: "healthy", reason: "no_abnormal_members" };
  }

  return { level: "healthy", reason: "unknown" };
}
```

- [ ] **Step 4: Run test and commit**

```bash
npm run test -- tests/lib/database-operational-signal.test.ts
git add lib/database-operational-signal.ts tests/lib/database-operational-signal.test.ts
git commit -m "feat: derive database operational row signal"
```

### Task B3: Update Database Table Layout

**Files:**
- Modify: `components/databases/database-table.tsx`
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Test: `tests/components/database-table.test.tsx`

- [ ] **Step 1: Add failing table tests**

In `tests/components/database-table.test.tsx`, assert:

```tsx
expect(screen.getByText("Operational signal")).toBeInTheDocument();
expect(screen.getByText("Resource status")).toBeInTheDocument();
expect(screen.queryByText("Hostname")).not.toBeInTheDocument();
expect(screen.queryByText("Port")).not.toBeInTheDocument();
expect(screen.getByText("1 critical member")).toBeInTheDocument();
expect(screen.getByText("Analytics ClickHouse Node 02")).toBeInTheDocument();
expect(screen.getByText("prod-ch-host-02.internal")).toBeInTheDocument();
expect(screen.getByText(":8123")).toBeInTheDocument();
```

- [ ] **Step 2: Run failing test**

```bash
npm run test -- tests/components/database-table.test.tsx
```

- [ ] **Step 3: Add i18n**

In `messages/en.json` under `tables.databases`:

```json
{
  "operationalSignal": "Operational signal",
  "needsAttention": "Needs attention",
  "healthySignal": "Healthy",
  "criticalMembers": "{count, plural, one {# critical member} other {# critical members}}",
  "warningMembers": "{count, plural, one {# warning member} other {# warning members}}",
  "memberLifecycleIssues": "{count, plural, one {# member stopped/degraded} other {# members stopped/degraded}}",
  "noAbnormalMembers": "No abnormal member signals",
  "resourceSelfHealthyMemberAttention": "Resource itself is healthy; member signal needs attention",
  "triggeredByMember": "Triggered by {name}"
}
```

In `messages/zh-CN.json`:

```json
{
  "operationalSignal": "运维信号",
  "needsAttention": "需关注",
  "healthySignal": "健康",
  "criticalMembers": "{count} 个成员严重",
  "warningMembers": "{count} 个成员告警",
  "memberLifecycleIssues": "{count} 个成员已停止或降级",
  "noAbnormalMembers": "暂无成员异常信号",
  "resourceSelfHealthyMemberAttention": "资源自身正常，成员信号触发关注",
  "triggeredByMember": "原因：{name}"
}
```

- [ ] **Step 4: Update resource cell**

Remove standalone hostname/port columns.

In resource cell for database instances, render:

```tsx
{!isCluster && (
  <div className="mt-1 flex flex-wrap gap-1.5 text-xs text-muted-foreground">
    {row.original.profileSummary?.role ? (
      <span>{formatLabel(row.original.profileSummary.role)}</span>
    ) : null}
    {row.original.profileSummary?.hostname ? (
      <span className="rounded border border-border px-1.5 py-0.5 font-mono">
        {row.original.profileSummary.hostname}
      </span>
    ) : null}
    {row.original.profileSummary?.port != null ? (
      <span className="rounded border border-border px-1.5 py-0.5 font-mono">
        :{row.original.profileSummary.port}
      </span>
    ) : null}
  </div>
)}
```

- [ ] **Step 5: Add operational signal column**

Use `buildDatabaseOperationalSignal(row.original)`.

Render:

```tsx
<div className="space-y-1">
  <div className="flex flex-wrap gap-2">
    <span className={...}>{signal.level === "healthy" ? t("tables.databases.healthySignal") : t("tables.databases.needsAttention")}</span>
    {signal.memberSignal === "critical" ? (
      <span className="...">{t("tables.databases.criticalMembers", { count: signal.memberCount ?? 0 })}</span>
    ) : null}
  </div>
  {signal.worstMemberName ? (
    <p className="text-xs text-muted-foreground">
      {t("tables.databases.triggeredByMember", { name: signal.worstMemberName })}
    </p>
  ) : signal.reason === "no_abnormal_members" ? (
    <p className="text-xs text-muted-foreground">{t("tables.databases.noAbnormalMembers")}</p>
  ) : null}
</div>
```

- [ ] **Step 6: Run tests and commit**

```bash
npm run test -- tests/components/database-table.test.tsx tests/lib/database-operational-signal.test.ts
git add components/databases/database-table.tsx messages/en.json messages/zh-CN.json tests/components/database-table.test.tsx
git commit -m "feat: show database operational signal in list"
```

### Task B4: Fix Search Fields And Search Freeze

**Files:**
- Modify: `components/databases/database-table.tsx`
- Test: `tests/components/database-table.test.tsx`
- Test: `e2e/operator-database-workflow.spec.ts` or new `e2e/database-list-search.spec.ts`

- [ ] **Step 1: Reproduce search freeze**

With frontend/backend running:

```text
http://localhost:3000/databases?environment=prod
```

Type:

```text
clickhouse
```

Observe:

- whether page freezes
- console errors
- network errors
- whether row click works after search
- whether engine dropdown opens after search

Record root cause before fixing.

- [ ] **Step 2: Add failing unit tests for searchable fields**

If search filtering is inline inside `DatabaseTable`, extract a pure helper:

```ts
export function databaseRowMatchesSearch(row: TreeRow, query: string): boolean
```

Tests:

```tsx
expect(databaseRowMatchesSearch(rowWithHost, "prod-ch-host-02.internal")).toBe(true);
expect(databaseRowMatchesSearch(rowWithPort, "8123")).toBe(true);
expect(databaseRowMatchesSearch(rowWithRole, "replica")).toBe(true);
expect(databaseRowMatchesSearch(clusterWithChildHost, "prod-ch-host-02.internal")).toBe(true);
```

- [ ] **Step 3: Implement search helper**

Search fields:

```ts
const searchable = [
  row.displayName,
  row.name,
  row.resourceSubtype,
  row.profileSummary?.hostname,
  row.profileSummary?.port != null ? String(row.profileSummary.port) : undefined,
  row.profileSummary?.role,
  ...(row.subRows ?? []).flatMap((child) => [
    child.displayName,
    child.name,
    child.resourceSubtype,
    child.profileSummary?.hostname,
    child.profileSummary?.port != null ? String(child.profileSummary.port) : undefined,
    child.profileSummary?.role,
  ]),
];
```

Match:

```ts
return searchable.some((value) =>
  value?.toLowerCase().includes(query.toLowerCase()),
);
```

- [ ] **Step 4: Fix freeze root cause**

Do not guess. Fix based on evidence.

Potential likely fix if URL replacement is the root cause:

- avoid `window.location.replace` for filter/search updates
- use `router.replace` consistently
- ensure sheet/overlay state is cleared on search param changes
- ensure no stale `inert`/modal overlay remains

If another cause is found, document it and fix at the source.

- [ ] **Step 5: Add E2E search stability test**

Add to E2E:

```ts
test("database search supports hostname and remains interactive", async ({ page }) => {
  await loginViaUI(page);
  await page.goto("/databases?environment=prod");
  await page.getByPlaceholder(/Search|搜索/).fill("prod-ch-host-02.internal");
  await expect(page.getByText("Analytics ClickHouse Node 02")).toBeVisible();
  await page.getByText("Analytics ClickHouse Node 02").click();
  await expect(page.getByRole("dialog")).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("dialog")).not.toBeVisible();
  await page.getByText(/Engine|引擎/).click();
  await expect(page.getByRole("menu")).toBeVisible();
});
```

Adapt selectors to the existing app patterns and E2E governance guards.

- [ ] **Step 6: Run tests and commit**

```bash
npm run test -- tests/components/database-table.test.tsx
npm run test:e2e -- e2e/operator-database-workflow.spec.ts
git add components/databases/database-table.tsx tests/components/database-table.test.tsx e2e/operator-database-workflow.spec.ts
git commit -m "fix: stabilize database search and include connection fields"
```

### Task B5: Full Frontend Verification

**Files:**
- Modify only if verification finds a real bug.

- [ ] **Step 1: Run full verification**

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
```

Checks:

- operational signal column visible
- resource, environment, owner, engine, resource self status, updated columns
  visible
- standalone hostname and port columns gone
- instance host/port under instance name
- `prod-ch-host-02.internal` search works
- `8123` search works
- `replica` search works
- after search, row click opens sheet
- blank click/Escape closes sheet
- engine dropdown opens
- no console errors
- no CORS errors

- [ ] **Step 3: Final report**

Report:

- root cause of search freeze
- backend rollup data source
- frontend no per-row API confirmation
- changed files
- verification matrix
- live browser results
- scope confirmation

