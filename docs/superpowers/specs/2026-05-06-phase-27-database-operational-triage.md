# Phase 27 Database Operational Triage Design

## Background

Phase 26 made `/databases` a better operator entry point:

- Backend exposes `databaseOperationalSummary` for database clusters.
- Frontend renders an operational signal column.
- Instance rows derive signals from their own health/lifecycle status.
- Search supports name, hostname, port, role, and engine without freezing.
- Cluster detail now shows members before topology.

The remaining gap is triage efficiency. The page can now show the signal, but
users still need to scan rows manually to find what needs attention.

The approved preview is:

```text
/Users/fan/JsProjects/ControlHub/.superpowers/brainstorm/phase27-database-signal-filter-sort/content/index.html
```

## Goal

Make `/databases` answer "what should I look at first?" without manual row
scanning.

Phase 27 adds:

1. Operational signal filtering.
2. Abnormal-first sorting.
3. Signal-aware summary counts.
4. Overview attention queue alignment with database list signal semantics.
5. Stabilization for the known dropdown timing flaky E2E path.

## Non-Goals

- Do not remove search.
- Do not remove engine filtering.
- Do not move environment into a table-local dropdown.
- Do not add backend writes.
- Do not add work orders.
- Do not execute SQL manually.
- Do not change topology layout.
- Do not add full-page tabs.
- Do not fabricate backend rollups.
- Do not add broad output suppression.

## Current Facts

Current `/databases` table-local controls are:

```text
Search input
Engine MultiSelectFilter backed by resourceSubtype
```

Environment is not a table-local dropdown. It is page/topbar URL context:

```text
?environment=prod
```

Phase 27 must preserve this fact:

```text
Search + environment context + engine filter + operational signal filter + sort
```

The new controls extend the current interface. They do not replace existing
filters.

## Product Direction

### Toolbar

Recommended toolbar model:

```text
Search: name / host / port / role
Environment: Production (page context, unchanged)
Engine: All / mysql / clickhouse / redis / ...
Operational signal: All / Needs attention / Normal / Not enough information
Sort: Abnormal first / Name / Updated
```

Implementation can keep environment outside the table if current app shell owns
environment. The key requirement is that users can combine the existing
environment context and engine filter with the new signal filter and sort.

### Default Sorting

Default sort should prioritize operational work:

1. Critical or needs-attention rows.
2. Unknown / information insufficient rows.
3. Healthy rows.
4. Stable secondary sort by resource type and name.

Within needs-attention:

1. Resource health critical.
2. Critical member signal.
3. Resource warning.
4. Warning member signal.
5. Stopped/degraded lifecycle.

This sort applies to both top-level rows and expanded member rows. It should not
break the cluster tree structure.

### Operational Signal Filter

Filter values:

```text
all
needs_attention
healthy
unknown
```

The filter should be URL-synced for shareability:

```text
?databaseSignal=needs_attention
```

Rules:

- `all` is default and omitted from URL.
- When a filter changes, reset `page` to `1`.
- Signal filter must combine with existing search and engine filters.
- For cluster rows, if any expanded member matches the filter, keep the cluster
  visible and keep matching member rows visible.
- For cluster rows that match directly, keep the cluster visible and preserve
  its child rows according to existing expansion behavior.

### Summary Counts

The page should expose quick counts near the toolbar:

```text
需关注 4
正常 18
信息不足 0
```

These counts should reflect the current environment and engine/search filters
where practical. If search is active, counts should apply to the filtered result
set. Avoid expensive per-row API calls.

### Overview Alignment

Overview attention queue should not contradict `/databases`.

If a database cluster has:

```text
resource health: healthy
member signal: one critical member
```

Overview should not present it as simply healthy. It should use the same
database operational signal helper or an equivalent shared view model rule.

Target copy:

```text
成员信号：1 个成员严重
```

No root-cause overclaim. Do not say the audit event caused the issue.

### E2E Flaky Cleanup

Recent E2E reports mention one dropdown timing flaky path. Phase 27 should
stabilize it while preserving realistic user input:

- no broad timeouts
- no deleting tests
- no `evaluate()` input bypass for search
- no broad warning suppression

## Data And Architecture

Frontend should reuse the Phase 26 helper:

```text
lib/database-operational-signal.ts
```

Add narrowly-scoped helpers if needed:

```text
buildDatabaseSignalRank(row)
databaseRowMatchesSignal(row, filter)
sortDatabaseTreeBySignal(tree)
countDatabaseSignals(tree)
```

These should be pure functions with unit tests.

Do not compute cluster member rollups with per-row frontend API calls.

## Acceptance Criteria

### `/databases?environment=prod`

- Engine filter remains visible and usable.
- Search remains visible and usable.
- New operational signal filter is visible.
- New abnormal-first sort is visible.
- Default view puts `Analytics ClickHouse Cluster Production` and
  `Analytics ClickHouse Node 02` above healthy-only rows.
- Selecting `运维信号：需关注` hides healthy-only rows.
- Selecting engine `clickhouse` and signal `需关注` shows the ClickHouse cluster
  and critical ClickHouse node.
- Searching `prod-ch-host-02.internal`, `8123`, and `replica` still works and
  does not freeze.
- Row click, sheet close, engine dropdown, signal dropdown, sort dropdown, and
  resource links remain interactive after search/filter changes.

### `/overview`

- Database attention queue entries use the same subject clarity as `/databases`.
- Cluster member-derived attention is represented as member signal, not as
  resource self health.

### Tests

- Unit tests cover ranking, filtering, sorting, counts.
- Component tests cover toolbar controls and row order.
- E2E covers combined search + engine + signal filter + row interactions.
- Full E2E has no new failures. If a pre-existing failure remains, it must have
  main-branch comparison evidence.
