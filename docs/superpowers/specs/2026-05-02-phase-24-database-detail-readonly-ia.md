# Phase 24 Database Detail Read-Only IA Closure Design

## Background

Phases 19-23 made database detail pages functionally useful:

- decision deck
- topology near the top
- diagnostic evidence and next checks
- member summary
- audit context
- data consistency checks
- instance context
- profile, relations, and audit history

The problem is no longer missing capability. The problem is density and
duplication. In particular, database instance pages can now show the same facts
three times:

- compact deck: role, connection, parent cluster
- instance context panel: role, connection, parent cluster
- parent cluster and connection info panels: same facts again

Cluster pages also risk becoming a stack of equally important cards:

```text
decision deck
topology
data consistency
member summary
audit context
cluster members table
profile
relations
audit history
```

Phase 24 should not add new data or new actions. It should close the read-only
view by making the page structure intentional.

The approved preview is:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase24-db-detail-readonly-ia/content/index.html
```

## Goal

Turn database detail pages into a stable read-only operator view with three
clear layers:

1. **First-screen judgement**: compact or diagnostic decision deck.
2. **Topology and trust**: topology plus consistency/context facts.
3. **Supporting details**: members, profile, relations, audit history.

The page should feel lighter after Phase 24, not heavier.

## Non-Goals

- Do not change backend APIs.
- Do not add backend code.
- Do not execute SQL.
- Do not add work orders.
- Do not add write operations.
- Do not add remediation actions.
- Do not edit topology.
- Do not add full-page tabs.
- Do not restore CMDB navigation.
- Do not reintroduce duplicate topology buttons in healthy compact decks.
- Do not remove required information; reorganize and de-duplicate it.

## Product Direction

### 1. Preserve The Phase 22B / 23 Top Structure

Keep the top of database detail pages:

```text
compact/diagnostic decision deck
resource topology
```

Do not move topology down again. Do not put topology behind tabs.

### 2. Merge Instance Facts

For database instances, replace the current repeated panels with a single
operator fact panel below topology.

The panel should combine:

- parent cluster
- role
- connection
- topology presence
- consistency status

The old standalone parent cluster and connection info panels should no longer
appear as separate full cards for database instances when the merged panel is
available. Raw profile remains available lower on the page.

Recommended title:

```text
实例上下文与一致性
Instance context and consistency
```

### 3. Keep Healthy Pages Quiet

Healthy database resources should avoid large explanatory walls.

For healthy instances:

```text
compact health deck
topology
instance context and consistency
supporting details
```

No "next checks" wall, no duplicate topology button, no repeated connection
cards.

For healthy clusters:

```text
compact health deck
topology
data consistency summary
member summary
supporting details
```

### 4. Keep Abnormal Pages Diagnostic

Abnormal or unknown database resources should still show:

- diagnostic deck
- top evidence
- next checks
- abnormal members
- topology
- data consistency issues

Phase 24 must not weaken abnormal workflows.

### 5. Supporting Details Become Lower Priority

The following sections should be treated as supporting details:

- full profile fields
- relations
- audit history
- raw deployed/detail metadata

Recommended default:

- keep the section present
- group into a clearly labeled supporting-details area
- use compact/collapsible presentation where the existing design system
  supports it

Do not hide critical diagnostic evidence inside a collapsed section. Only
supporting raw/detail sections should be lower priority.

## Proposed Page Orders

### Abnormal Cluster

```text
diagnostic decision deck
resource topology
data consistency panel
member summary / audit context
cluster members table
supporting details: profile, relations, audit history
```

### Healthy Cluster

```text
compact health deck
resource topology
data consistency panel
member summary
supporting details: cluster members, profile, relations, audit history
```

### Healthy Instance

```text
compact health deck
resource topology
instance context and consistency panel
supporting details: profile, relations, audit history
```

The old separate parent cluster and connection info full cards should not
remain immediately below the new instance context panel.

### Abnormal Instance

```text
diagnostic decision deck
resource topology
instance context and consistency panel with issues
supporting details: profile, relations, audit history
```

## UI Requirements

### Merged Instance Context And Consistency Panel

The merged panel should show:

- status badge: data consistent / needs data review / not enough data
- parent cluster fact
- role fact
- connection fact
- topology fact
- issue list when consistency warnings exist

If a fact is missing, show explicit localized copy:

```text
后端未提供
Not provided by backend
```

Do not show `0 members` on instance pages.

### Supporting Details Group

Introduce a database-only supporting details area. It may be a component or
well-structured page section.

It should include:

- operational profile / raw profile
- relations
- audit history
- cluster member table when it is not already the primary diagnostic focus

The group must not remove information. It only lowers visual priority and
reduces repeated facts.

### Healthy Compact Deck

Keep the current Phase 22B behavior:

- no topology button inside compact deck
- concise role/connection/parent-cluster summary
- no top evidence / next checks / abnormal members

### Diagnostic Deck

Keep the current abnormal behavior:

- top evidence
- next checks
- topology entry
- abnormal members

Do not collapse the diagnostic deck for abnormal resources.

## Data Dependencies

Use existing frontend data:

- `ResourceDetailViewModel`
- `profileSummary`
- `clusterInfo`
- `members`
- `relations`
- `recentAudits`
- `TopologyResponse` already fetched for Phase 23
- `buildClusterConsistency`
- `buildInstanceConsistency`

No backend change is required.

## Testing Requirements

Unit/component tests must cover:

- merged instance panel renders parent cluster, role, connection, topology, and
  consistency status
- instance page no longer renders duplicate standalone parent cluster and
  connection cards above supporting details
- healthy compact deck still has no topology button
- abnormal cluster still renders diagnostic deck
- supporting details group contains profile, relations, and audit history
- missing values use localized copy

E2E must cover:

- `/resources/22` healthy instance:
  - compact deck
  - topology
  - merged instance context and consistency
  - no duplicate parent cluster / connection full cards
  - no `0 members`
- `/resources/14` cluster:
  - diagnostic deck
  - topology
  - data consistency
  - cluster members still available
- no console errors
- no CORS errors

## Acceptance Criteria

- The page has fewer repeated cards than Phase 23.
- Healthy instance pages show one combined context/consistency panel, not three
  separate cards with the same facts.
- Healthy pages remain compact.
- Abnormal pages remain diagnostic.
- Topology remains directly below the decision deck.
- Supporting details are still accessible.
- No backend files are modified.
- No API contract changes are introduced.
- Full frontend quality gates pass.

