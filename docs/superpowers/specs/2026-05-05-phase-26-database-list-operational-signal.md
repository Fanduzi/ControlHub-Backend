# Phase 26 Database List Operational Signal Design

## Background

Phase 25 made database detail pages semantically clearer. It separated:

- operator verdict
- resource self status
- member signal
- page information check
- audit context
- audit history

The remaining entry-point problem is `/databases`.

Live review showed that the database list can still show:

```text
Analytics ClickHouse Cluster Production
资源自身状态: 健康 / 运行中
```

while the detail page shows:

```text
运维判定: 需关注
成员信号: 1 个成员处于告警或严重状态
Analytics ClickHouse Node 02: 严重
```

Phase 25 added the honest fallback text:

```text
仅资源自身状态；成员判定见详情
```

That avoids lying, but it still forces users to open the detail page to find
the actual operational signal. `/databases` should be an operator entry point,
not only a resource inventory table.

The approved preview is:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase26-database-list-operational-signal/content/index.html
```

## Goal

Make `/databases` directly show which database clusters and instances need
operator attention, while preserving the existing resource table facts.

The list should answer:

1. Which database should I look at first?
2. Is the issue from the cluster resource itself or from a member?
3. Which member caused the cluster to need attention?
4. Can I search by database name, host, or port without the page freezing?

## Non-Goals

- Do not add write operations.
- Do not add work orders.
- Do not execute SQL manually.
- Do not change topology layout.
- Do not change database detail page IA except where E2E assertions need updated copy.
- Do not use frontend per-row API requests to compute member rollups.
- Do not fake member rollups on the frontend when backend data is absent.
- Do not add full-page tabs.
- Do not add broad output suppression.

## Scope Split

Phase 26 should be delivered in two coordinated parts.

### Phase 26A — Backend Database List Read Model

Backend adds read-only rollup fields to resource list/detail view models where
database cluster rows need member-derived operational context.

### Phase 26B — Frontend Database List UX + Search Fix

Frontend consumes the rollup fields, updates `/databases`, fixes the current
search freeze, and supports searching instance hostname/port.

## Current UI Problems

### 1. Cluster Attention Signal Hidden In Detail Page

Current `/databases` table has the important factual columns:

```text
资源
环境
负责人
引擎
资源自身状态
主机名
端口
更新于
```

But for clusters, host/port are empty and member-derived health is absent.

A cluster with a critical member can look like a healthy row. This makes the
database list poor as an operational triage surface.

### 2. Hostname And Port Are Too Prominent As Separate Columns

The host and port columns mostly matter for instances. For clusters they are
empty, which wastes horizontal space and makes the table feel sparse.

Recommended direction:

- keep resource, environment, owner, engine, resource self status, updated
- move instance host/port under the instance display name as compact secondary
  metadata
- do not keep standalone host/port columns unless there is a strong product
  reason

Example:

```text
Analytics ClickHouse Node 01 Production
实例 · replica
prod-ch-host-01.internal · :8123
```

### 3. Search Freezes The Page

User-reported bug:

```text
数据库页面输入搜索内容后页面卡死，页面什么都点不了了。
```

This must be treated as P0 for Phase 26B:

- reproduce first
- identify root cause
- fix with tests
- verify row click, dropdown, sheet, and navigation still work after search

Likely suspects to investigate, not assume:

- `window.location.replace` in table filter handling
- search debounce + URL replacement interaction
- expanded tree state and row model recomputation
- overlay/sheet residue from previous interaction gates
- focus trap or inert residue after search

### 4. Search Does Not Include Hostname/Port

Current search appears to match display name and name, and maybe child display
names. It should also match:

- instance hostname
- instance port
- instance role
- cluster member hostnames/ports when the cluster row contains child rows

Example expected searches:

```text
prod-ch-host-02.internal
8123
replica
clickhouse node 02
```

## Product Direction

### Database List Columns

Preferred Phase 26 table:

```text
资源
运维信号
环境
负责人
引擎
资源自身状态
更新于
```

Host/port should not remain as standalone columns. They should be displayed
inside the resource cell for database instance rows.

The row still contains all important fields:

```text
资源: name + cluster/instance + node count or host/port metadata
运维信号: derived operator signal
环境: environment
负责人: owner
引擎: engine
资源自身状态: resource health/lifecycle
更新于: updatedAt
```

### Cluster Row Example

For `Analytics ClickHouse Cluster Production`:

```text
资源:
  Analytics ClickHouse Cluster Production
  集群 · 2 节点数

运维信号:
  需关注
  1 个成员严重
  原因：Analytics ClickHouse Node 02 严重

资源自身状态:
  健康
  运行中
  资源自身正常，成员信号触发关注
```

### Instance Row Example

For `Analytics ClickHouse Node 02`:

```text
资源:
  Analytics ClickHouse Node 02
  实例 · replica
  prod-ch-host-02.internal · :8123

运维信号:
  严重
  触发集群需关注

资源自身状态:
  严重
  运行中
```

### Healthy Cluster Row Example

```text
Order MySQL Cluster Prod
集群 · 3 节点数

运维信号:
  健康
  暂无成员异常信号

资源自身状态:
  健康
  运行中
```

## Backend Read Model Requirements

The frontend must not fetch each cluster's members one row at a time. Backend
should provide list-safe rollup fields.

Recommended new type:

```go
type DatabaseOperationalSummary struct {
    MemberCount          int64   `json:"memberCount"`
    CriticalMemberCount  int64   `json:"criticalMemberCount"`
    WarningMemberCount   int64   `json:"warningMemberCount"`
    StoppedMemberCount   int64   `json:"stoppedMemberCount"`
    DegradedMemberCount  int64   `json:"degradedMemberCount"`
    UnknownRoleCount     int64   `json:"unknownRoleCount"`
    PrimaryMemberCount   int64   `json:"primaryMemberCount"`
    ReplicaMemberCount   int64   `json:"replicaMemberCount"`
    WorstMemberID        *int64  `json:"worstMemberId,omitempty"`
    WorstMemberName      string  `json:"worstMemberName,omitempty"`
    WorstMemberStatus    string  `json:"worstMemberStatus,omitempty"`
}
```

Attach it to resources where applicable:

```json
{
  "profileSummary": {...},
  "databaseOperationalSummary": {
    "memberCount": 2,
    "criticalMemberCount": 1,
    "warningMemberCount": 0,
    "worstMemberId": 23,
    "worstMemberName": "Analytics ClickHouse Node 02",
    "worstMemberStatus": "critical"
  }
}
```

For instances, backend can either omit the summary or provide a minimal self
summary. The frontend can use the instance's own health/profile data for
instance rows.

### Backend Query Semantics

The backend should compute cluster rollups as part of list read models using
existing tables:

- `resources`
- `resource_relations` (`member_of`)
- database profile tables for role/host/port where already joined or available

No schema migration is expected unless the backend author proves a read-model
cache is necessary. Start with query/projection only.

### OpenAPI

OpenAPI must document:

- `databaseOperationalSummary` is nullable/optional
- fields are derived read-only list context
- counts are best-effort from current read model
- no write semantics

## Frontend Requirements

### Consume Rollup

Add the new type to frontend resource types/view models.

Use existing helper-style architecture:

- pure helper for deriving `DatabaseRowOperationalSignal`
- component rendering stays small
- tests cover helper logic independently

Recommended frontend helper shape:

```ts
type DatabaseRowOperationalSignal = {
  level: "healthy" | "needs_attention" | "critical" | "unknown";
  primaryLabelKey: string;
  secondaryLabelKey?: string;
  memberName?: string;
};
```

### Table Layout

Update `/databases` to:

- add `运维信号` / `Operational signal` column
- keep `资源`, `环境`, `负责人`, `引擎`, `资源自身状态`, `更新于`
- remove standalone hostname and port columns
- render instance hostname/port under resource name
- preserve expand/collapse behavior
- reduce spacing between expand arrow and database icon if still visually loose

### Search

Search must include:

- row display name
- row name
- resource subtype / engine
- profile summary hostname
- profile summary port
- profile summary role
- child row display names
- child row hostnames
- child row ports
- child row roles

Search must not freeze the page.

After search:

- row click opens sheet
- resource link navigates
- engine dropdown still opens
- blank click closes sheet
- accent color does not reset

### Search Bug Investigation

The worker must reproduce the bug before fixing:

```text
/databases?environment=prod
type search text
observe freeze / click dead state
capture console/network/process output
```

The final report must include root cause. Do not report "fixed" without root
cause.

## UX Copy

Chinese:

```text
运维信号
需关注
成员严重 {count}
成员告警 {count}
暂无成员异常信号
资源自身状态
资源自身正常，成员信号触发关注
主机 {hostname}
端口 {port}
```

English:

```text
Operational signal
Needs attention
{count} critical members
{count} warning members
No abnormal member signals
Resource status
Resource itself is healthy; member signal needs attention
Host {hostname}
Port {port}
```

## Acceptance Criteria

### `/databases?environment=prod`

- Includes these columns:
  - resource
  - operational signal
  - environment
  - owner
  - engine
  - resource self status
  - updated
- Does not include standalone hostname and port columns.
- Instance host/port appears under instance resource name.
- `Analytics ClickHouse Cluster Production` shows:
  - operator signal: needs attention
  - member signal: one critical member
  - resource self status: healthy/running
- `Analytics ClickHouse Node 02` shows host/port under its name and critical
  signal.
- Healthy clusters show healthy operator signal and no abnormal member signal.

### Search

- Searching `prod-ch-host-02.internal` returns `Analytics ClickHouse Node 02`
  and its cluster context.
- Searching `8123` returns ClickHouse instances.
- Searching `replica` returns replica instances.
- Searching does not freeze the page.
- After search, row click, link click, dropdown, and sheet close still work.

### Backend

- No per-row frontend member fetches.
- Backend list response includes rollup fields.
- OpenAPI validates.
- Integration tests cover a cluster with a critical member.

## Verification

Backend:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Frontend:

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

Live browser:

```text
http://localhost:3000/databases?environment=prod
```

Required live checks:

- search hostname
- search port
- search role
- open and close sheet after search
- click resource link after search
- open engine filter after search
- no console errors
- no CORS errors

## Scope Confirmation

Final reports must explicitly confirm:

- no write operations
- no work orders
- no SQL execution outside tests
- no topology layout changes
- no full-page tabs
- no frontend per-row API rollup fetches
- no tag/push/release
- no AI co-author

