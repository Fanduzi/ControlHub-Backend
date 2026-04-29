# Phase 20 Database Diagnostic Workbench Completion Design

## Background

Phase 19 added a read-only database operator workbench. It gives operators a
verdict, member summary, recent audits, and topology entry point.

The next gap is diagnostic clarity. Some UI text still looks like a technical
field dump or machine translation:

```text
健康=严重
生命周期=已停止
```

The intended meaning is:

```text
healthStatus: critical
lifecycleStatus: stopped
```

That should be presented as product copy:

```text
健康状态：严重
生命周期状态：已停止
```

Operators also need ordering and context. A member table should put stopped,
critical, warning, degraded, and primary nodes where a DBA expects to look
first. A workbench summary should state why the resource needs attention.
Recent audits and topology entry points should help operators continue the
investigation without adding write actions.

## Goal

Complete the read-only database diagnostic workbench by improving diagnostic
copy, member ordering, summary reasoning, audit context, and topology entry
points.

Phase 20 is one milestone with two internal parts:

- **20A — Diagnostic expression and ordering**
- **20B — Diagnostic navigation context**

Both parts must ship together before the phase is complete.

## Non-Goals

- Do not add SQL execution.
- Do not add work orders.
- Do not add write operations.
- Do not add topology editing.
- Do not add backend contract changes unless a blocking gap is found and
  reported before frontend implementation continues.
- Do not implement unstable ReactFlow node highlighting unless it is small,
  deterministic, and covered by tests.
- Do not add AI-style root-cause speculation beyond available backend data.
- Do not restore `/cmdb` navigation.

## 20A: Diagnostic Expression And Ordering

### Status Reason Copy

Replace mechanical field/value concatenation with localized product copy.

Examples:

| Raw data | Bad copy | Required zh-CN copy | Required en copy |
|---|---|---|---|
| `healthStatus=critical` | `健康=严重` | `健康状态：严重` | `Health status: Critical` |
| `healthStatus=warning` | `健康=告警` | `健康状态：告警` | `Health status: Warning` |
| `healthStatus=unknown` | `健康=未知` | `健康状态：未知` | `Health status: Unknown` |
| `lifecycleStatus=stopped` | `生命周期=已停止` | `生命周期状态：已停止` | `Lifecycle status: Stopped` |
| `lifecycleStatus=degraded` | `生命周期=降级` | `生命周期状态：降级` | `Lifecycle status: Degraded` |

The implementation should centralize this in helper functions. Do not let
pages build their own strings.

### Natural Diagnostic Sentences

For the database workbench, verdict facts should be more natural than status
labels alone.

Examples:

```text
该资源当前处于严重健康状态。
存在 1 个成员处于告警或严重状态。
存在 1 个成员处于停止或降级状态。
后端未提供角色信息。
```

English examples:

```text
This resource is currently critical.
1 member is warning or critical.
1 member is stopped or degraded.
The backend did not provide role information.
```

### Member Sorting

Database cluster member tables should sort for operations:

1. Primary/master/writer before replicas/readers when severity is equal.
2. Critical before warning.
3. Stopped/degraded lifecycle before running lifecycle.
4. Unknown role after known primary/replica roles when severity is equal.
5. Healthy running replicas last.
6. Stable tie-breaker by display name.

The goal is not a fancy table feature. The default table order should make the
most important node visible first.

### Unknown And Missing Data

Unknown states must be explicit:

- missing role: `后端未提供角色信息`
- missing profile: `后端未提供画像信息`
- missing host/port: `连接地址未提供`
- missing recent audits: `暂无最近审计事件`

Do not show empty cards or raw `unknown` without context.

## 20B: Diagnostic Navigation Context

### Audit Context

Recent audits should not be just a raw list.

Show a small summary above the list:

- if audit events exist:
  - `最近有 3 条相关审计事件。`
  - `最近有资源状态或关系变更。` if event types indicate resource changes
- if none exist:
  - `暂无最近审计事件。`

Do not claim causality. Do not write "this caused the incident" unless backend
data explicitly says so.

Audit relevance can be simple and deterministic:

- events scoped to the current `targetResourceId` are relevant
- event types containing `resource.` or `relation.` can be described as resource
  or relation changes

### Topology Entry From Diagnostic Context

Add stable topology navigation affordances:

- workbench-level link:
  - `/resources/{id}?topologyDepth=2&topologyExpanded=1`
- abnormal member row link:
  - `/resources/{memberId}?topologyDepth=2&topologyExpanded=1`

If a member is warning, critical, stopped, degraded, or unknown, the member row
should make it easy to open that member's topology/detail context.

Do not implement node highlighting unless:

- URL semantics are defined
- ReactFlow behavior is deterministic
- unit and E2E tests cover it

If highlighting is risky, explicitly defer it and only ship expanded topology
links.

## Affected Surfaces

Phase 20 should cover:

- overview attention queue
- database operator workbench
- cluster member table
- resource detail database sections
- recent audit context in database detail pages

Do not change:

- backend APIs
- topology layout algorithm
- non-database resource behavior except shared status reason helpers where
  already used by attention queue

## Acceptance Criteria

- No user-visible copy like `健康=严重` or `生命周期=已停止` remains.
- Chinese UI does not leak `Running`, `Degraded`, `Critical`, or similar status
  enum labels in the affected surfaces.
- Attention queue uses normalized status reason copy.
- Database verdict facts use natural diagnostic sentences.
- Cluster member table sorts abnormal and primary nodes before healthy replicas.
- Missing role/profile/connection data has explicit empty-state text.
- Recent audit context has a summary and still links to the full audit page.
- Abnormal members have a clear topology/detail navigation path.
- No SQL execution, work orders, write actions, or topology editing are added.
- Full frontend QA gates pass.

