# Phase 25 Database Detail Semantic Cleanup Design

## Background

Phases 19-24 made database detail pages useful, navigable, and less
duplicative. The remaining problem is semantic consistency.

Recent live review of:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

found that users can still reasonably ask:

```text
到底是健康还是异常，严重，到底问题是啥？
```

The UI currently mixes several different signals without naming their subject:

- resource self health: `resource.healthStatus`
- resource lifecycle: `resource.lifecycleStatus`
- derived operator verdict: member health/lifecycle/profile evidence
- read-model consistency: relation/member/topology/profile availability
- audit context: diagnostic summary
- audit history: full event list

When these appear as peer badges and peer cards, they look contradictory even
when the underlying data is technically consistent.

Phase 25 should not add new backend data or new operator actions. It should make
the existing database detail and database list semantics self-explanatory.

## Goal

Make database list rows and database detail pages answer three questions without
contradiction:

1. What is the operator verdict?
2. What signal caused that verdict?
3. Which supporting facts are raw details rather than primary diagnosis?

Success criteria:

```text
Users should no longer ask "is this healthy or serious?"
Users should no longer see "recent 5 audits" and "no audit activity" together.
Users should be able to click an instance's parent cluster from the facts panel.
Audit history should not be squeezed into a half-width card.
```

## Non-Goals

- Do not change backend APIs.
- Do not add backend code.
- Do not execute SQL.
- Do not add write operations or work orders.
- Do not edit topology layout.
- Do not add tabs.
- Do not add new diagnostic intelligence beyond the existing helper logic.
- Do not hide topology.
- Do not remove profile, relations, audit history, or member data.
- Do not restore duplicated instance parent/connection cards.

## Current Problems

### 1. Status Subject Conflict

On `/databases?environment=prod`, `Analytics ClickHouse Cluster Production`
shows:

```text
健康 / 运行中
```

On `/resources/14`, the same resource shows:

```text
健康 / 运行中
需关注
1 个成员处于告警或严重状态
Analytics ClickHouse Node 02 严重
```

The data means:

```text
Cluster resource self status: healthy/running.
Derived operator verdict: needs attention because one member is critical.
```

The UI does not say that. The fix is to label the subject of each signal.

### 2. Read-Model Consistency Looks Like Health Consistency

The current `数据一致性` panel can show:

```text
数据一致
当前可见的数据库信号一致。
```

near a diagnostic deck that says `需关注`.

The panel is not health consistency. It is read-model consistency:

- members returned by backend
- relations
- topology database instance nodes
- profile availability

The copy must say that explicitly.

### 3. Audit Context Duplicates Audit History

Current page can show:

```text
审计上下文
该资源最近 5 条审计事件。
暂无最近审计事件。

审计历史
No audit activity yet
Recent resource changes will appear here once the backend audit feed is connected.
```

Problems:

- `该资源最近 5 条审计事件。` reads like a factual count even when count is zero.
- Audit context and audit history both try to display event detail.
- Chinese locale leaks English empty-state copy.

Audit context should be a diagnostic summary only. Audit history should be the
only event-detail area.

### 4. Instance Parent Cluster Is Not Navigable In Facts Panel

On `/resources/22`, the facts panel shows:

```text
所属集群
Analytics ClickHouse Cluster Production
```

but it is plain text. The relation list lower on the page has the link, but the
facts panel is the primary context. The cluster name must be clickable when the
cluster id is known.

### 5. Audit History Is Half Width

`DatabaseSupportingDetails` currently places all children into:

```tsx
<div className="grid gap-4 xl:grid-cols-[1fr_1fr]">{children}</div>
```

That makes:

```text
运行画像: half width
关系: half width
审计历史: half width on next row
```

Audit history should be full width. Profile and relations can remain two-column.

### 6. Raw Field Hints Are Too Prominent

The diagnostic deck currently exposes raw hints such as:

```text
字段: members[].healthStatus
```

This is useful for debugging but not for the first-screen operator summary. Raw
field hints belong in collapsed diagnostic details, not the decision deck.

### 7. Low-Value Repeated Sections

When only one evidence item exists, the page still shows:

```text
诊断明细
查看全部诊断证据（1 条，含字段来源）
```

That repeats the decision deck. It should only appear when there is additional
detail beyond the visible deck summary.

`成员摘要` and `集群成员` also overlap. This is acceptable if member summary is
compact and clearly supports the member table, but it should not dominate the
page.

## Product Direction

### Primary Mental Model

Use these names consistently:

| Concept | Meaning | User Copy |
| --- | --- | --- |
| Operator verdict | Derived page-level judgement from resource + members + profile evidence | `运维判定` / `Operator verdict` |
| Resource self status | Backend status fields on the current resource only | `资源自身状态` / `Resource status` |
| Member signal | Health/lifecycle issues in child instances | `成员信号` / `Member signal` |
| Read-model consistency | Whether backend read models agree: members, relations, topology, profile | `读模型一致性` / `Read-model consistency` |
| Audit context | Count-only diagnostic context from recent audits | `审计上下文` / `Audit context` |
| Audit history | Detailed audit event list | `审计历史` / `Audit history` |

### Detail Page Order

Keep the Phase 22/24 top order:

```text
decision deck
topology
context / consistency
supporting details
```

Refine the sections:

#### Abnormal Cluster

```text
运维判定: 需关注
Reason: 1 critical/warning member
Resource self status: healthy/running
Topology
Read-model consistency
Member table with compact summary
Audit context summary only
Supporting details: profile + relations two-column, audit history full-width
```

#### Healthy Instance

```text
Compact health strip
Topology
Instance context and read-model consistency
Audit context summary only if useful
Supporting details: profile + relations two-column, audit history full-width
```

### Database List

The database list should stop contradicting the detail page.

Minimum acceptable change:

- Keep existing `状态` column for resource self status.
- Add a small derived signal for cluster rows when member data is available:
  `成员严重 1` / `1 critical member`.

Preferred change if it can be implemented without new API calls:

- Rename status column label or cell subcopy to make it clear it is resource
  self status.
- Add operator signal badge only when current row has enough member/profile
  data to compute it.

If list rows cannot compute member-derived verdict without expensive per-row
fetches, do not fake it. Instead, make copy explicit:

```text
状态: 资源自身状态
详情页显示成员派生判定
```

But the preferred fix is to use already available `profileSummary.nodeCount`
and any existing list data if enough.

## Copy Requirements

### Chinese

Use:

```text
运维判定
资源自身状态
成员信号
1 个成员严重
1 个成员处于告警或严重状态
读模型一致
读模型需检查
所属集群
该实例未出现在拓扑中
最近无审计事件
最近审计中没有资源或关系变更
最近审计中有 {count} 条资源或关系变更，仅作为排查线索，不代表根因
```

Avoid:

```text
健康=严重
数据一致
该资源最近 5 条审计事件
No audit activity yet
Recent resource changes will appear here...
字段: members[].healthStatus
```

`字段: ...` may exist only inside collapsed diagnostic details.

### English

Use:

```text
Operator verdict
Resource status
Member signal
1 critical member
Read-model consistent
Read-model needs review
No recent audit events
Recent audits include {count} resource or relation changes. Use as context, not root cause.
```

## UI Requirements

### Decision Deck

- Primary badge should be the operator verdict.
- Resource self status must be labeled as resource status, not shown as an
  unlabeled peer of the verdict.
- Remove raw field hints from the visible decision deck.
- Keep raw field hints in collapsed diagnostic details when details are shown.

### Read-Model Consistency Panel

- Rename user-facing title/copy from generic data consistency to read-model
  consistency.
- Keep the helper logic and status types unless a small rename is safe.
- Do not imply health consistency.

### Audit Context

- Remove event list rendering from `DatabaseOperatorWorkbench`.
- Render a concise summary only:
  - no events
  - events exist but no resource/relation changes
  - resource/relation changes exist
- Link to audit history or `/audits?targetResourceId=...` only if events exist.
- Do not duplicate individual event rows.

### Audit History

- Use localized empty state in Chinese and English.
- Remain the only detailed event list on the detail page.
- Render full width inside supporting details.

### Instance Facts Panel

- Parent cluster must be a link when `clusterInfo.id` is present.
- If name exists without id, render text.
- If missing, use explicit missing copy:
  `后端未提供所属集群信息`.

### Supporting Details Layout

Desktop:

```text
profile | relations
audit history full width
```

Mobile:

```text
profile
relations
audit history
```

## Acceptance Criteria

### `/resources/14`

- User can tell:
  - operator verdict is needs attention
  - resource self status is healthy/running
  - reason is one critical/warning member
- No unlabeled `健康` badge sits beside `需关注` as if they are same-level
  verdicts.
- Read-model panel does not say generic `数据一致`.
- Audit context does not list events.
- Audit history empty state is localized.
- Audit history full width.
- Console has no errors.

### `/resources/22`

- Compact healthy strip remains quiet.
- Instance facts panel links parent cluster to `/resources/14`.
- Audit context does not claim five events when none exist.
- Audit history empty state is localized.
- Audit history full width.
- Console has no errors.

### `/databases?environment=prod`

- `Analytics ClickHouse Cluster Production` no longer appears purely healthy
  without any indication that detail page has member-derived attention.
- If derived member signal is not feasible in list without new API calls, status
  copy must clearly say it is resource self status.

## Verification

Required:

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

Live browser checks:

```text
http://localhost:3000/databases?environment=prod
http://localhost:3000/resources/14
http://localhost:3000/resources/22
```

Forbidden text audit:

```bash
rg -n "健康=|No audit activity yet|Recent resource changes will appear|该资源最近 5 条审计事件|字段: members\\[\\]\\.healthStatus|数据一致" app components messages tests e2e
```

`字段: members[].healthStatus` is allowed only if scoped to collapsed diagnostic
details tests/copy and not visible in the decision deck.

## Scope Confirmation

Final report must explicitly confirm:

- no backend changes
- no API contract changes
- no SQL
- no work orders
- no write operations
- no topology layout editing
- no full-page tabs
- no tag/push/release
- no AI co-author

