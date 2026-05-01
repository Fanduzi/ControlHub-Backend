# Phase 23 Database Read-Model Consistency Design

## Background

Phases 19-22B turned database resource detail pages into a usable read-only
operator view:

- Phase 19 added the operator workbench.
- Phase 20 added diagnostic copy, member sorting, and topology links.
- Phase 21 added diagnostic evidence, next checks, and audit buckets.
- Phase 22 moved decision content and topology into the first screen.
- Phase 22B removed duplicate diagnostic walls and made healthy resources
  compact.

The remaining issue is trust. The page now looks cleaner, but operators still
need to know whether the database detail page is internally consistent:

- Does the member table agree with relations?
- Does topology agree with the member table?
- Does an instance page clearly explain its cluster, role, host, and upstream
  or downstream context?
- When profile data is missing, is that a data-quality gap or a resource
  health issue?

Phase 23 should close this read-only trust gap before any write operation,
work order, or remediation action is introduced.

## Goal

Make database detail pages answer one question reliably:

> "Can I trust the page's read-only database picture, and if not, what data is
> missing or inconsistent?"

Phase 23 adds a read-only consistency layer for database clusters and
instances. It compares the data already available to the frontend and explains
missing or inconsistent signals without claiming root cause.

## Non-Goals

- Do not add backend APIs.
- Do not change backend contracts.
- Do not execute SQL.
- Do not add work orders.
- Do not add remediation/write actions.
- Do not edit topology.
- Do not add topology node highlighting unless it is already supported by
  existing stable URL state.
- Do not redesign the Phase 22B decision deck.
- Do not restore full-page tabs.
- Do not restore CMDB navigation.

## Product Direction

### 1. Consistency, Not Remediation

The feature must stay read-only. It should say:

- "Member table and topology agree."
- "Topology is missing 1 member from the table."
- "This instance has no host/port profile from backend."
- "This relation references a resource that is not visible in this page."

It must not say:

- "This caused the incident."
- "Restart this instance."
- "Fail over now."
- "Create a work order."

### 2. Keep Healthy Pages Light

Healthy resources should remain compact. Phase 23 must not reintroduce a
large first-screen diagnostic wall for healthy resources.

For healthy resources, show consistency as a compact support section below
topology:

```text
Data consistency
All visible signals agree: 2 members, 2 topology nodes, cluster relation present.
```

If there are gaps:

```text
Data consistency
2 issues need data review:
- Topology is missing mysql-replica-02.
- Backend did not provide host/port for mysql-replica-01.
```

### 3. Make Instance Context Explicit

For database instances, the page must clearly answer:

- Parent cluster: present or missing.
- Role: primary/replica/unknown.
- Connection: hostname and port present or missing.
- Relation context: member_of relation present or missing.
- Topology context: instance appears in topology or not.

This should be presented as operator-readable facts, not raw IDs.

### 4. Compare Existing Frontend Data

Use existing data already loaded in Phase 17-22:

- `ResourceDetailViewModel`
- `members`
- `clusterInfo`
- `profileSummary`
- resolved `relations`
- topology data already rendered by the topology panel
- `recentAudits`

If topology data is not currently available to the consistency helper, Phase 23
may pass the already-fetched topology response into a pure comparison helper.
Do not add a new API.

## Proposed UI

### Database Cluster Page

Order remains:

```text
compact/diagnostic decision deck
resource topology
data consistency panel
member summary
audit context
cluster members table
profile / relations / audit history
```

The new data consistency panel should include:

- overall status: `consistent`, `needs_data_review`, or `unknown`
- counts: members, topology database nodes, member_of relations if available
- issues list when gaps exist
- explicit "read-only data check" copy

### Database Instance Page

Order remains:

```text
compact/diagnostic decision deck
resource topology
instance context panel
data consistency panel
parent cluster
connection info
profile / relations / audit history
```

Instance context should include:

- role
- hostname/port
- parent cluster link if known
- member relation status
- profile completeness

The existing parent cluster and connection panels may remain, but Phase 23
should avoid duplicating the same facts in three places. If the new instance
context panel covers the facts better, the old panels should be shortened or
kept as supporting detail lower on the page.

## Consistency Rules

### Cluster Rules

For a database cluster:

1. Every `members[]` row should have:
   - display name
   - resource type/subtype
   - health/lifecycle
   - role, or explicit missing-role issue
   - hostname and port, or explicit missing-connection issue
2. Every member should appear in topology when topology depth is enough to
   include cluster members.
3. A member should have a `member_of` relation to the cluster when relation
   data is available.
4. Topology database instance nodes not present in members should be reported
   as "topology-only database node" rather than silently ignored.

### Instance Rules

For a database instance:

1. `clusterInfo` should be present when the instance belongs to a cluster.
2. `profileSummary.role` should be present; missing role is a data issue.
3. `profileSummary.hostname` and `profileSummary.port` should be present;
   missing connection is a data issue.
4. The instance should appear in topology.
5. If parent cluster information exists in one source but not another, show
   a consistency issue instead of choosing one silently.

### Severity

Use three read-only severities:

- `ok`: data agrees or the field is not applicable.
- `warning`: data is missing or inconsistent but the resource can still be
  inspected.
- `unknown`: the frontend does not have enough data to decide.

Do not map consistency warnings directly to resource health.

## Copy Guidelines

Chinese copy must avoid machine-style field equations.

Use:

- `数据一致`
- `需要数据复核`
- `后端未提供角色信息`
- `拓扑未包含该成员`
- `成员关系缺失`

Avoid:

- `role=null`
- `health=critical`
- `caused by`
- `根因是`

## Testing Requirements

Unit tests must cover pure consistency helpers:

- healthy cluster with matching members/topology/relations
- cluster member missing role
- cluster member missing connection
- member in table but not topology
- topology database node not in members
- healthy instance with parent cluster, role, and connection
- instance missing role
- instance missing parent cluster
- audit-only resource remains compact and does not become diagnostic

Component tests must cover:

- consistency panel renders compact OK state
- consistency panel renders warning issue list
- instance context panel renders role/host/parent facts
- missing data uses localized copy

E2E must cover:

- `/resources/14` diagnostic deck still works and shows consistency panel
- `/resources/22` compact deck remains compact and shows instance context
- no console errors
- no CORS errors
- no English leakage in Chinese locale for new copy

## Acceptance Criteria

- Healthy database resources remain visually compact.
- Database detail pages include a read-only consistency panel below topology.
- Cluster pages identify member/topology/relation mismatches.
- Instance pages explicitly show parent cluster, role, connection, and missing
  profile data.
- Missing data is explained as data-quality evidence, not resource failure.
- No backend files are modified.
- No API contract changes are introduced.
- No SQL, work orders, write actions, or topology editing are added.
- Existing Phase 22B layout order is preserved.
- Full frontend quality gates pass.

