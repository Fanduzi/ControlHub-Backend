# Phase 21 Database Diagnostic Runbook Design

## Background

Phase 20 made the database operator workbench readable: diagnostic copy no
longer looks like machine field dumps, cluster members sort by operational
priority, abnormal members have topology links, and recent audit context has a
summary.

The remaining gap is operational guidance. The page can now say "this resource
needs attention", but it does not yet clearly explain:

- which facts produced the verdict
- which data source each fact came from
- what an operator should check next
- whether nearby audit events are resource changes, relation changes, or other
  activity
- what missing backend data means

Phase 21 turns the workbench from "diagnostic summary" into a read-only
diagnostic runbook. It must still avoid write operations and must not imply
causality that backend data does not prove.

## Goal

Add a structured, read-only diagnostic runbook to database detail pages so an
operator can move from "needs attention" to a short, concrete investigation
path.

The runbook should answer four questions:

1. What facts caused this verdict?
2. Where did those facts come from?
3. What should I check next?
4. What recent audit activity is nearby, without claiming it caused the issue?

## Non-Goals

- Do not add SQL execution.
- Do not add work orders.
- Do not add remediation buttons.
- Do not add topology editing.
- Do not add backend API changes.
- Do not invent metrics such as replication lag, disk usage, backup status,
  QPS, or slow query count unless backend already provides them.
- Do not claim root cause or causality from audit events.
- Do not restore `/cmdb` navigation.
- Do not redesign topology layout.
- Do not add unstable ReactFlow node highlighting.

## Target Surfaces

Phase 21 is frontend-only and affects database resource detail pages:

- `/resources/{id}` for `database_cluster`
- `/resources/{id}` for `database_instance`

It can modify shared helpers used by those pages, but should not change
overview/resource/database list behavior unless required to keep shared types
consistent.

## UX Model

### 1. Diagnostic Evidence

Add an evidence section inside the database operator workbench.

Each evidence item should contain:

- severity: `critical`, `warning`, `info`, or `unknown`
- title: localized human sentence
- source label: where the fact came from
- raw field hint: stable developer/operator hint, not the main copy

Examples:

| Data | User-facing title | Source | Raw hint |
|---|---|---|---|
| `resource.healthStatus=critical` | `资源健康状态为严重。` | `资源状态` | `healthStatus=critical` |
| member `healthStatus=warning` | `1 个成员处于告警或严重状态。` | `成员健康` | `members[].healthStatus` |
| member `lifecycleStatus=stopped` | `1 个成员已停止或降级。` | `成员生命周期` | `members[].lifecycleStatus` |
| missing role | `后端未提供 1 个成员的角色信息。` | `成员画像` | `profileSummary.role` |
| no audit events | `暂无最近审计事件。` | `审计事件` | `auditEvents` |

The raw hint is allowed because it is secondary evidence metadata. It must not
replace the localized main sentence.

### 2. Runbook Checklist

Add a compact "下一步排查" / "Next checks" card.

Rules:

- Suggestions must be deterministic from existing fields.
- Suggestions must be read-only.
- Suggestions must use cautious copy.
- Suggestions must not say "修复", "执行", "重启", "切主", or similar actions
  that imply the console can perform changes.

Examples:

| Condition | Suggested check |
|---|---|
| resource or member critical | `检查实例进程、连接地址和最近资源变更。` |
| warning member | `查看告警成员详情和拓扑上下游。` |
| stopped/degraded lifecycle | `确认该状态是否来自计划停机或最近变更。` |
| missing role/profile | `检查后端画像同步是否提供角色、主机和端口。` |
| recent resource/relation audit | `对照最近资源或关系变更，确认是否与当前异常时间接近。` |
| no findings | `当前没有明确异常信号，继续查看拓扑和审计历史。` |

### 3. Audit Buckets

Recent audits should be grouped before listing:

- resource changes: `eventType` starts with `resource.`
- relation changes: `eventType` starts with `relation.`
- access/operation events: all other events

Display a summary such as:

```text
最近 5 条审计事件：2 条资源变更，1 条关系变更，2 条其他操作。
```

If resource/relation changes exist, add cautious context:

```text
这些事件只表示时间邻近的变更，不代表已确认根因。
```

Do not say "caused by".

### 4. Page Structure

The database detail page is already dense. Phase 21 should not make it longer
without structure.

The recommended order inside the workbench:

1. verdict badge and 2-4 facts
2. diagnostic evidence
3. next-check runbook
4. member summary for clusters
5. audit context
6. topology action

If this becomes too tall, use existing `DetailPanel` sections. Do not add tabs
or custom navigation unless necessary.

### 5. Empty And Unknown States

Unknown or missing data must be explicit:

- `后端未提供角色信息`
- `后端未提供画像信息`
- `连接地址未提供`
- `暂无最近审计事件`
- `没有可用于生成排查建议的异常信号`

Do not render blank cells or bare `unknown` as an explanation.

## Data Dependencies

Use existing frontend data only:

- `ResourceDetailViewModel`
- `members`
- `profileSummary`
- `clusterInfo`
- `recentAudits`
- `healthStatus`
- `lifecycleStatus`
- `resourceType`
- `resourceSubtype`

If the worker finds that a desired suggestion needs missing backend data, it
must defer that suggestion instead of adding fake data.

## Acceptance Criteria

- Database detail pages show a structured diagnostic evidence section.
- Evidence items have localized title, source, severity, and raw hint.
- Workbench shows a deterministic next-check runbook.
- Audit context groups recent events into resource, relation, and other
  buckets.
- Audit copy explicitly avoids causal claims.
- Missing role/profile/connection/audit states are explicit.
- Existing Phase 20 behavior is preserved:
  - no `健康=严重` style copy
  - member sorting remains operational-priority based
  - abnormal members keep topology links
- No backend files are modified.
- No write operations, SQL execution, work orders, or topology editing are
  added.
- Full frontend gates pass:
  - `npm run check:e2e-governance`
  - `npx tsc --noEmit -p tsconfig.json`
  - `npm run lint`
  - `npm run test`
  - `npm run build`
  - `npm run test:e2e:smoke`
  - `npm run test:e2e:interaction`
  - `npm run test:e2e`

