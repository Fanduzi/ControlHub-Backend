# Phase 19 Database Operator Workbench Design

## Background

Phases 17B and 17C added a read-only database operator detail workflow. The
frontend can now show profile fields, cluster members, parent cluster context,
relations, topology, and audit surfaces.

The current experience is still mostly "data is visible". It does not yet help
a DBA quickly answer:

- Is this database healthy right now?
- Which node needs attention?
- Which node is primary, replica, stopped, degraded, or critical?
- What is the likely reason for the warning?
- How do the member table, topology, and detail links connect?
- What changed recently for this cluster or instance?

Phase 19 should turn the read-only detail pages into a diagnosis-oriented
operator workbench without adding write operations.

## Goal

Improve database cluster and instance detail pages so operators can quickly
understand health, topology role, membership, connection info, recent audit
context, and next navigation target.

The result should be read-only, data-dense, and actionable without pretending to
execute remediation.

## Non-Goals

- Do not add SQL execution.
- Do not add work orders.
- Do not add topology editing.
- Do not add archive/unarchive changes.
- Do not change backend API contracts unless a factual gap is found and
  explicitly reported.
- Do not restore `/cmdb` navigation.
- Do not add write operations.
- Do not add alerting integrations.

## Target Pages

### Database Cluster Detail

Example:

```text
/resources/14
```

Cluster detail should prioritize:

- operator summary
- member health distribution
- primary/replica/unknown role counts
- stopped/degraded/critical member count
- cluster endpoint/topology mode/node count
- member table with role, host, port, version, health, lifecycle
- topology section with clear link to expanded view
- recent relevant audit events

### Database Instance Detail

Example:

```text
/resources/22
```

Instance detail should prioritize:

- parent cluster link
- role and read-only state
- host/port/version/engine
- lifecycle and health reason
- relation context
- topology context
- recent audit events for this instance

## Required UX Model

### 1. Operator Verdict

Add a top-level read-only verdict card for database resources.

Verdict states:

- `healthy`
- `needs_attention`
- `critical`
- `unknown`

Inputs:

- resource `healthStatus`
- resource `lifecycleStatus`
- member health/lifecycle statuses for clusters
- `problems` if available

Display:

- status badge
- one sentence diagnosis
- 2-4 facts behind the diagnosis

Examples:

```text
Needs attention
2 members are warning or critical. 1 member is stopped.
```

```text
Healthy
All known members are running and no critical health signals are present.
```

Do not invent monitoring metrics that are not present in data.

### 2. Member Health Summary

For database clusters, add compact member summary cards:

- total members
- primary count
- replica count
- warning/critical count
- stopped/degraded count

If role data is missing, show `Role unknown` rather than guessing.

### 3. Member Table Navigation

The member table should be an operator navigation surface:

- member display name is clickable
- role is visible
- host/port are visible when profileSummary has them
- health and lifecycle are localized badges
- row click or link should navigate consistently, without breaking sheet/back
  interactions already guarded by Phase 18A/18B

### 4. Topology Coordination

Do not redesign topology layout in this phase.

Improve coordination:

- detail page should show a clear "Open expanded topology" action
- member rows should include enough identity to match topology nodes
- if direct topology-node highlighting is too large, defer it explicitly; do
  not half-implement it

### 5. Recent Audit Context

Add a small read-only recent audit section on database detail pages:

- show up to 5 events scoped to the resource
- display timestamp, event type, result, actor if available
- link to full audit page with `targetResourceId`

Do not restore the overview page's recent audit block. This is detail-page
context only.

### 6. Localization

Chinese and English must both be complete for new strings.

No title-cased enum leaks such as:

- `Running`
- `Degraded`
- `Database Cluster`

Use existing status/type localization helpers where possible.

## Data Dependencies

Use existing frontend/backend contracts from Phase 17A/17B:

- `GET /resources/{id}`
- `GET /resources/{id}/profile`
- `GET /resources/{id}/members`
- `GET /resources/{id}/relations?view=resolved`
- `GET /audit-events?targetResourceId=...`
- `GET /resources/{id}/topology`

If any required field is missing, do not fake it. Display an explicit
unavailable state and report the gap.

## Acceptance Criteria

- Cluster detail has an operator verdict.
- Cluster detail has member summary cards.
- Cluster member table remains readable and navigable.
- Instance detail has parent cluster and connection context.
- Detail pages show up to 5 recent scoped audit events.
- Topology action is clear and does not regress existing topology rendering.
- Chinese and English strings are complete.
- Existing E2E gates still pass:
  - `npm run test:e2e:smoke`
  - `npm run test:e2e:interaction`
  - `npm run test:e2e`
- `npm run check:e2e-governance` passes.
- No backend files are modified unless the worker stops and reports a blocking
  contract gap first.

