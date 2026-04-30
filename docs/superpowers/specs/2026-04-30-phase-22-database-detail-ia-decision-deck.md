# Phase 22 Database Detail IA Decision Deck Design

## Background

Phases 17B, 19, 20, and 21 made database resource detail pages much more useful:

- database profile and cluster/member context
- operator verdict
- diagnostic evidence
- next-check runbook
- audit context
- topology entry points
- member sorting and abnormal-member topology links

The problem is now information architecture. The page has too many expanded
sections stacked vertically. Users must scroll too far before reaching topology
and operational context. Simply moving topology upward would only push other
important sections downward; it does not solve the underlying "everything is
expanded and equal priority" problem.

The chosen direction is **A. 首屏决策台 / First-Screen Decision Deck** from the
brainstorm preview:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase22-db-detail-ia/content/database-detail-ia-options.html
```

## Goal

Restructure database detail pages so the first screen presents:

1. resource identity and verdict
2. top diagnostic evidence
3. top next checks
4. topology analysis entry
5. abnormal member shortcut for clusters

Longer content remains available, but lower-priority sections become compact,
collapsed, or positioned after the decision deck.

## Non-Goals

- Do not add backend API changes.
- Do not add SQL execution.
- Do not add work orders.
- Do not add remediation actions.
- Do not add topology editing.
- Do not redesign ReactFlow topology layout.
- Do not add topology node highlighting.
- Do not implement full tabbed navigation in Phase 22.
- Do not restore `/cmdb` navigation.
- Do not restore demo `resourceSummaries`.
- Do not remove existing detail content; reorganize it.

## UX Direction

### Recommended IA: First-Screen Decision Deck

The first screen should be a compact operator deck:

```text
Identity + verdict
Top diagnostic evidence      Top next checks
Topology preview/entry
Abnormal members
Collapsed long sections
```

This is not a pure visual restyle. It changes default information priority.

### Why Not Tabs First

Tabs hide context. Database investigation often needs evidence, members,
topology, audit, relations, and profile to remain conceptually connected.

Phase 22 should use:

- compact summary cards
- explicit topology entry
- abnormal-member shortcut
- disclosure/collapsible low-priority sections

Do not split the whole page into tabs yet. A future phase can add URL-synced
tabs if the decision deck still leaves the page too long.

## Target Pages

Only database resource detail pages:

- `/resources/{id}` where `resourceType=database_cluster`
- `/resources/{id}` where `resourceType=database_instance`

Non-database resource details should continue to render without the database
decision deck.

## Required Layout

### 1. Decision Header

At the top of database detail:

- display name
- resource type/subtype
- environment
- owner if available
- health/lifecycle badges
- operator verdict badge
- primary action: open expanded topology

The operator verdict must be visible without scrolling.

### 2. Top Evidence And Top Checks

Use Phase 21 helpers and UI copy.

Default view should show:

- at most 3 diagnostic evidence items
- at most 3 next-check runbook items

If more exist, show a compact "show all" control or collapsed detail section.
Do not remove extra evidence.

### 3. Topology Entry Card

Topology must be reachable in the first screen.

The card should include:

- clear title
- small text explaining this opens expanded topology
- button/link to:

```text
/resources/{id}?topologyDepth=2&topologyExpanded=1
```

Do not embed a second full ReactFlow instance in the first-screen card. It can
be a compact card or preview shell. Reusing the existing full topology section
later in the page is acceptable.

### 4. Abnormal Member Shortcut

For database clusters:

- show abnormal members immediately after topology entry
- include display name, role/subtype if available, health/lifecycle badges
- include existing "查看拓扑" / topology link
- if no abnormal members exist, show a compact healthy state

The full member table should still exist, but it should not dominate the first
screen.

### 5. Long Sections Become Compact

The following should not all be expanded by default above topology:

- full diagnostic evidence list
- full runbook list
- full member table
- full audit event list
- relations
- profile fields

Recommended default:

- keep the decision deck expanded
- keep topology entry expanded
- keep abnormal member shortcut expanded
- show full member table below the deck
- show audit context as summary first, with event list below or collapsible
- keep relations/profile after topology/audit

If using collapsible UI, it must be accessible and E2E-tested.

### 6. Instance Variant

For database instances, the decision deck should prioritize:

- identity and verdict
- top evidence/checks
- parent cluster link
- connection info
- topology entry

Do not show cluster member summary for instances.

## Data Dependencies

Use existing frontend data:

- `ResourceDetailViewModel`
- `members`
- `clusterInfo`
- `profileSummary`
- `recentAudits`
- Phase 19 helper outputs
- Phase 20 member sorting/topology links
- Phase 21 diagnostic runbook helpers

No backend changes should be required.

## Acceptance Criteria

- Database cluster detail shows the decision deck above long detail sections.
- Database instance detail shows the instance decision deck above long detail
  sections.
- Operator verdict and expanded topology link are visible near the top.
- At most 3 evidence and 3 runbook items are shown in the first-screen summary.
- Abnormal cluster members are visible near the top.
- Full member table remains available.
- Full topology remains available and existing topology behavior is not
  regressed.
- Audit context remains available and still avoids causal claims.
- Relations and profile remain available.
- No `健康=严重` or machine-style diagnostic copy returns.
- No backend files are modified.
- No SQL execution, work orders, write operations, or topology editing are
  added.
- Full frontend QA gates pass.

