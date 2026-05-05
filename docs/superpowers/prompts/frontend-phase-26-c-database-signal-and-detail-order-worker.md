# Frontend Phase 26C Worker Prompt — Database Signal And Detail Order Fix

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 26C — Database Signal And Detail Order Fix**

This is a frontend-only follow-up to Phase 26B. Backend Phase 26A and frontend
Phase 26B have already been merged to local `main`.

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-05-phase-26-database-list-operational-signal.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-05-phase-26-database-list-operational-signal.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-26c-database-signal-and-detail-order.md
```

If current frontend code differs from these documents, report the mismatch
before inventing a third approach.

## Mandatory Worktree Requirement

Do **not** develop directly on frontend `main`.

Create and use this dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-26c-database-signal-and-detail-order
```

Branch:

```text
feat/phase-26c-database-signal-and-detail-order
```

Base it on current frontend `main`, which must include Phase 26B commit:

```text
d74d874 fix: clarify database operational signal copy
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
git worktree add .worktrees/frontend-phase-26c-database-signal-and-detail-order -b feat/phase-26c-database-signal-and-detail-order main
cd .worktrees/frontend-phase-26c-database-signal-and-detail-order
git status --short --branch
git log --oneline -5
```

If the worktree already exists, verify it is on the correct branch and clean
before using it. Do not overwrite user changes.

## Confirm Backend Contract

Backend should be running on `localhost:8080` at Phase 26A or later.

Run:

```bash
curl -s 'http://localhost:8080/resources?resourceType=database_instance&resourceType=database_cluster&pageSize=80' \
  | jq '[.items[] | select(.resourceType=="database_instance") | {hasHealth:(.healthStatus!=null), hasLifecycle:(.lifecycleStatus!=null), hasProfile:(.profileSummary!=null), hasRole:(.profileSummary.role!=null), hasHost:(.profileSummary.hostname!=null), hasPort:(.profileSummary.port!=null), hasRollup:(.databaseOperationalSummary!=null)}] | {total:length, missingHealth:map(select(.hasHealth|not))|length, missingLifecycle:map(select(.hasLifecycle|not))|length, missingProfile:map(select(.hasProfile|not))|length, missingRole:map(select(.hasRole|not))|length, missingHost:map(select(.hasHost|not))|length, missingPort:map(select(.hasPort|not))|length, withRollup:map(select(.hasRollup))|length}'
```

Expected with current seed data:

```json
{
  "total": 33,
  "missingHealth": 0,
  "missingLifecycle": 0,
  "missingProfile": 0,
  "missingRole": 0,
  "missingHost": 0,
  "missingPort": 0,
  "withRollup": 0
}
```

This confirms `databaseOperationalSummary` is intentionally absent for
instances and direct instance status/profile metadata is sufficient.

Also confirm cluster rollup exists:

```bash
curl -s 'http://localhost:8080/resources/14' | jq '.resource.databaseOperationalSummary'
```

If backend is unavailable, report the blocker. Do not change backend code in
this phase.

## Problem 1: Instance Signals Incorrectly Show Unavailable

Current bug:

```text
Many database_instance rows show "未提供" / unavailable in the operational
signal column because databaseOperationalSummary is null.
```

Root cause:

`databaseOperationalSummary` is a cluster member rollup. It is only populated
for `database_cluster`. The frontend helper treats missing summary as unknown
for all resource types, so healthy instances with complete direct status become
unknown.

## Required Fix 1

Modify:

```text
lib/database-operational-signal.ts
tests/lib/database-operational-signal.test.ts
components/databases/database-table.tsx
tests/components/database-table.test.tsx
messages/zh-CN.json
messages/en.json
e2e/operator-database-workflow.spec.ts
```

Implement type-aware signal logic.

### Instance Logic

For `resourceType === "database_instance"`, ignore `databaseOperationalSummary`.

Use direct fields:

| Condition | Result |
| --- | --- |
| `healthStatus === "critical"` | needs attention; reason `instance_resource_critical` |
| `healthStatus === "warning"` | needs attention; reason `instance_resource_warning` |
| `lifecycleStatus === "stopped"` | needs attention; reason `instance_lifecycle_stopped` |
| `lifecycleStatus === "degraded"` | needs attention; reason `instance_lifecycle_degraded` |
| `healthStatus === "healthy"` and `lifecycleStatus === "running"` | healthy; reason `instance_healthy` |
| missing or unknown direct status | unknown; reason `instance_status_unknown` |

Chinese copy:

```text
实例资源状态严重
实例资源状态告警
实例已停止
实例生命周期降级
实例自身正常
实例状态未提供
```

English copy:

```text
Instance resource status is critical
Instance resource status is warning
Instance is stopped
Instance lifecycle is degraded
Instance is healthy
Instance status is unavailable
```

### Cluster Logic

For `resourceType === "database_cluster"`, continue to use
`databaseOperationalSummary`.

Rules:

| Condition | Result |
| --- | --- |
| `criticalMemberCount > 0` | needs attention; critical member reason |
| `warningMemberCount > 0` | needs attention; warning member reason |
| `stoppedMemberCount + degradedMemberCount > 0` | needs attention; member lifecycle reason |
| summary exists and no abnormal counts | healthy; members normal |
| summary absent | unknown; member summary unavailable |

Chinese cluster-unavailable copy should be cluster-specific:

```text
成员汇总未提供
```

This copy must not appear for normal instance rows.

## Problem 2: Cluster Members Appear After Topology

Current cluster detail order is effectively:

```text
Decision deck
Topology
Information check
Workbench
Operator summary
Cluster members
Supporting sections
```

When the decision deck says a member is abnormal, the next section should show
which member. Topology should come after the member table.

## Required Fix 2

Modify:

```text
app/(console)/resources/[id]/page.tsx
tests/resource-detail-page.test.tsx
e2e/operator-database-workflow.spec.ts
```

For `database_cluster`, reorder primary sections:

```text
Decision deck
Cluster members
Topology
Page information check
Diagnostic/supporting details
Operator/profile/supporting sections
```

For `database_instance`, preserve:

```text
Decision deck
Topology
Instance facts
Diagnostic/supporting details
```

Constraints:

- Do not delete topology.
- Do not change topology layout.
- Do not add tabs.
- Do not duplicate cluster members.
- Do not change backend.

## Required Tests

### Unit Tests

Update `tests/lib/database-operational-signal.test.ts` to cover:

1. healthy database instance with no `databaseOperationalSummary` renders
   healthy / `instance_healthy`.
2. critical database instance with no `databaseOperationalSummary` renders
   needs attention / `instance_resource_critical`.
3. warning database instance renders needs attention /
   `instance_resource_warning`.
4. stopped database instance renders needs attention /
   `instance_lifecycle_stopped`.
5. degraded database instance renders needs attention /
   `instance_lifecycle_degraded`.
6. database cluster with no summary renders unknown /
   cluster summary unavailable.
7. database cluster with healthy summary renders healthy.
8. database cluster with critical member summary renders needs attention /
   critical member reason.

### Component Tests

Update `tests/components/database-table.test.tsx` to cover:

1. database instance row does not show "未提供" when health/lifecycle/profile
   exist.
2. critical instance row shows "运维信号：需关注" and "实例资源状态严重".
3. role `replica` localizes to "从库".
4. cluster missing summary still shows cluster-specific "成员汇总未提供".
5. standalone host/port columns remain absent.

### Page And E2E Tests

Update `tests/resource-detail-page.test.tsx` if possible:

- cluster members section renders before topology section.

Update `e2e/operator-database-workflow.spec.ts`:

1. `/databases?environment=prod`
   - `Analytics ClickHouse Node 02` does not show "未提供".
   - role shows "从库" in zh-CN.
   - reason shows "实例资源状态严重".
2. `/resources/14`
   - "集群成员" heading appears before "资源拓扑".
   - topology still renders.
3. Existing real-input search freeze coverage still passes.

## Bad Copy Audit

After changes, run:

```bash
rg -n "成员汇总未提供|实例状态未提供|未提供|Replica|Primary|触发集群需关注|^严重$" components lib app messages tests e2e
```

Explain all hits:

- `messages/*.json` may contain localized values.
- tests may contain expected copy.
- production JSX/TS must not hardcode these strings.
- `/databases` instance rows with complete status/profile must not render
  "未提供".

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

If full E2E still has known `list-pagination` failures, provide main comparison
evidence. Otherwise fix to green.

## Live Browser Verification

Use:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Verify `/databases?environment=prod`:

- normal instances no longer show "未提供".
- `Analytics ClickHouse Node 02`:
  - role is "从库"
  - operational signal is "需关注"
  - reason is "实例资源状态严重"
- healthy instances show healthy instance signal.
- cluster rows without summary, if any, use cluster-specific unavailable copy.
- search `prod-ch-host-02.internal`, `8123`, and `replica` still does not freeze.
- after search, row click, sheet close, engine dropdown, and resource links work.

Verify `/resources/14`:

- decision deck is still first.
- "集群成员" appears before "资源拓扑".
- topology still loads.
- console has zero unexpected errors/warnings.

Verify `/resources/22`:

- instance page does not get a cluster members table.
- topology and instance facts still render normally.

## Commit

If changes are made, commit:

```bash
git commit -m "fix: clarify database instance signals and cluster detail order"
```

No `Co-Authored-By`.

Do not tag, push, release, or merge.

## Final Report Required

Report:

1. Worktree path.
2. Branch.
3. Commit hash.
4. Root cause confirmation:
   - backend instance metadata completeness
   - `databaseOperationalSummary` is cluster-only
   - frontend misclassification point
5. Files changed.
6. Exact instance signal logic.
7. Detail page order before/after.
8. Test coverage.
9. Verification matrix.
10. Live browser evidence.
11. Bad-copy `rg` audit result.
12. Scope confirmation:

```text
- no backend changes
- no API contract changes
- no SQL
- no topology layout changes
- no broad output suppression
- no tag/push/release
- no AI co-author
- clean git status
```
