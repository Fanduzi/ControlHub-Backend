# Phase 26C Database Signal And Detail Order Fix Design

## Background

Phase 26A added `databaseOperationalSummary` to backend database cluster rows.
Phase 26B rendered the `/databases` operational signal column and fixed search
freeze. Live review after the merge found two remaining defects:

1. Many database instance rows show an operational signal equivalent to
   "not provided".
2. On cluster detail pages, the user sees "1 member is critical" in the
   decision deck, but must scroll past topology before seeing the member table.

Both issues weaken the operator workflow. The first is a logic bug. The second
is an information-ordering problem.

## Evidence

Backend contract and live data confirm this is not a backend metadata gap.

`databaseOperationalSummary` is intentionally cluster-scoped:

- OpenAPI describes it as populated for `database_cluster` resources.
- Go model comments describe it as a database cluster member-health rollup.
- `database_instance.databaseOperationalSummary = null` is expected.

Live API inspection showed all current database instances have enough direct
metadata to derive an instance operational signal:

```text
total database_instance rows: 33
missing healthStatus: 0
missing lifecycleStatus: 0
missing profileSummary: 0
missing role: 0
missing hostname: 0
missing port: 0
with databaseOperationalSummary: 0
```

Example:

```json
{
  "id": 23,
  "name": "analytics-ch-node-02-prod",
  "displayName": "Analytics ClickHouse Node 02",
  "resourceType": "database_instance",
  "healthStatus": "critical",
  "lifecycleStatus": "running",
  "profileSummary": {
    "hostname": "prod-ch-host-02.internal",
    "port": 8123,
    "engine": "clickhouse",
    "version": "24.3",
    "role": "replica"
  },
  "databaseOperationalSummary": null
}
```

This row should render an instance-level signal:

```text
运维信号：需关注
原因：实例资源状态严重
```

It must not render "未提供".

## Goals

1. Fix instance operational signal logic so instances use their own
   `healthStatus` and `lifecycleStatus`, not cluster rollup.
2. Keep cluster operational signal logic based on `databaseOperationalSummary`.
3. Restrict "成员汇总未提供" copy to database cluster rows only.
4. Move cluster members above topology on cluster detail pages.
5. Preserve topology, search, sheet, filter, and detail interactions.

## Non-Goals

- No backend changes.
- No API contract changes.
- No SQL changes.
- No topology layout changes.
- No tabs.
- No write operations or work orders.
- No broad output suppression.

## Product Rules

### Database Instance Signal

For `resourceType = database_instance`, ignore `databaseOperationalSummary`.

Use direct resource fields:

| Condition | Signal | Reason |
| --- | --- | --- |
| `healthStatus = critical` | needs attention | instance resource status is critical |
| `healthStatus = warning` | needs attention | instance resource status is warning |
| `lifecycleStatus = stopped` | needs attention | instance is stopped |
| `lifecycleStatus = degraded` | needs attention | instance lifecycle is degraded |
| `healthStatus = healthy` and `lifecycleStatus = running` | normal | instance is healthy |
| missing/unknown direct status | unknown | instance status unavailable |

The Chinese page should not show bare strings such as:

```text
严重
触发集群需关注
Replica
Primary
```

The row should explain the subject:

```text
运维信号：需关注
原因：实例资源状态严重
```

### Database Cluster Signal

For `resourceType = database_cluster`, continue to use
`databaseOperationalSummary`.

| Condition | Signal | Reason |
| --- | --- | --- |
| critical member count > 0 | needs attention | N members critical |
| warning member count > 0 | needs attention | N members warning |
| stopped/degraded member count > 0 | needs attention | N members lifecycle abnormal |
| summary present and no abnormal counts | normal | member status normal |
| summary absent | unknown | member rollup unavailable |

The "member rollup unavailable" message is cluster-specific and must not appear
for normal instance rows.

### Cluster Detail Order

For database cluster detail pages, order the primary operator sections as:

```text
Decision deck
Cluster members
Topology
Page information check
Diagnostic/supporting details
Operator/profile/supporting sections
```

Reasoning:

- Decision deck says there is a member issue.
- Cluster members immediately answers which member is affected.
- Topology then explains where that member sits in relationships.
- Page information check and audit details are supporting evidence.

For database instance detail pages, do not add a members table. Preserve:

```text
Decision deck
Topology
Instance facts
Diagnostic/supporting details
```

## Verification

The implementation must verify:

- `/databases?environment=prod` no longer shows "未提供" for normal instances
  with complete status/profile data.
- `Analytics ClickHouse Node 02` shows localized role and direct instance
  critical reason.
- Healthy instances show a normal instance signal.
- Cluster rows without rollup still have cluster-specific unavailable copy.
- `/resources/14` renders cluster members before topology.
- Search by hostname, port, and role still works and does not freeze.
- Row click, sheet close, engine dropdown, and resource links still work.
