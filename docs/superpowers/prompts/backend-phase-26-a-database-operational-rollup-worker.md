# Backend Phase 26A Worker Prompt — Database Operational Rollup Read Model

You are working in the backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

## Phase

**Phase 26A — Database Operational Rollup Read Model**

This is the backend prerequisite for frontend Phase 26B. Do not start frontend
work until this backend contract is implemented, verified, and merged to backend
`main`.

## Required Input Documents

Read these documents before changing code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-05-phase-26-database-list-operational-signal.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-05-phase-26-database-list-operational-signal.md
```

The implementation plan is authoritative. If the current backend code differs
from the plan, stop and report the mismatch before inventing a different
contract.

## Mandatory Worktree Requirement

Do **not** develop directly on backend `main`.

Create and use this dedicated backend worktree:

```text
/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-26a-database-operational-rollup
```

Branch:

```text
phase-26a-database-operational-rollup
```

Base it on current backend `main`.

Before editing, report:

```bash
git worktree list
git status --short --branch
git log --oneline -5
```

If the worktree already exists, verify it is on the correct branch and clean
before using it.

## Mandatory GitNexus Requirements

This backend repository is indexed by GitNexus. Follow `AGENTS.md` exactly.

Before editing any function, method, class, or type symbol:

1. Run GitNexus impact analysis for the symbol you will edit.
2. Report direct callers, affected processes, and risk level.
3. If impact is HIGH or CRITICAL, stop and report before editing.

Before committing:

1. Run GitNexus detect changes.
2. Confirm changed symbols and affected flows match Phase 26A scope.

After committing:

1. Run `npx gitnexus analyze` from the backend repo unless a project hook has
   already refreshed the index.

Do not ignore stale-index warnings. If GitNexus reports the index is stale, run:

```bash
npx gitnexus analyze
```

## Goal

Add a read-only database operational summary to backend resource list/detail
responses so `/databases` can show cluster member health without frontend
per-row API calls or fabricated rollups.

The frontend needs to distinguish:

```text
资源自身状态: cluster resource health/lifecycle
运维信号: member-derived operational attention signal
```

Example target behavior for `Analytics ClickHouse Cluster Production`:

```text
resource health: healthy/running
databaseOperationalSummary:
  memberCount: 2
  criticalMemberCount: 1
  worstMemberName: Analytics ClickHouse Node 02
  worstMemberStatus: critical
```

## Required Contract

Add an optional JSON field to resource list/detail rows:

```json
{
  "databaseOperationalSummary": {
    "memberCount": 2,
    "criticalMemberCount": 1,
    "warningMemberCount": 0,
    "stoppedMemberCount": 0,
    "degradedMemberCount": 0,
    "unknownRoleCount": 0,
    "primaryMemberCount": 0,
    "replicaMemberCount": 2,
    "worstMemberId": 15,
    "worstMemberName": "Analytics ClickHouse Node 02",
    "worstMemberStatus": "critical"
  }
}
```

Field rules:

- `databaseOperationalSummary` is present for database clusters when member data
  can be derived.
- It may be omitted for non-database resources.
- It may be omitted or zero-valued for database instances unless the current
  backend model already has a clean reason to populate it.
- Do not change existing field names or existing endpoint behavior.
- Do not add write APIs.
- Do not run manual SQL outside tests.

## Expected Files

Likely files to inspect and modify:

```text
internal/model/pagination.go
internal/repository/mysql/resource_repository.go
internal/api/test_server.go
internal/api/resource_handler_test.go
internal/integration/resource_test.go
internal/openapi/openapi.yaml
```

Do not assume these are the only files. Follow existing repository patterns.

## Implementation Requirements

### 1. Model

Add a Go model similar to:

```go
type DatabaseOperationalSummary struct {
    MemberCount         int64  `json:"memberCount"`
    CriticalMemberCount int64  `json:"criticalMemberCount"`
    WarningMemberCount  int64  `json:"warningMemberCount"`
    StoppedMemberCount  int64  `json:"stoppedMemberCount"`
    DegradedMemberCount int64  `json:"degradedMemberCount"`
    UnknownRoleCount    int64  `json:"unknownRoleCount"`
    PrimaryMemberCount  int64  `json:"primaryMemberCount"`
    ReplicaMemberCount  int64  `json:"replicaMemberCount"`
    WorstMemberID       *int64 `json:"worstMemberId,omitempty"`
    WorstMemberName     string `json:"worstMemberName,omitempty"`
    WorstMemberStatus   string `json:"worstMemberStatus,omitempty"`
}
```

Attach it to the resource response/list model as:

```go
DatabaseOperationalSummary *DatabaseOperationalSummary `json:"databaseOperationalSummary,omitempty"`
```

Use the existing model naming and file conventions if they differ.

### 2. Repository Rollup

Compute rollups from existing data:

- `resource_relations` with `relation_type = 'member_of'`
- child `resources.health_status`
- child `resources.lifecycle_status`
- database instance profile role field when available

Rollup counts:

- total members
- critical members
- warning members
- stopped members
- degraded members
- missing/unknown role members
- primary/master/writer members
- replica/secondary/reader members

Worst member selection:

1. critical
2. warning
3. unknown
4. degraded/stopped lifecycle if no health signal is worse
5. stable tie-break by display name or id

Return both worst member id/name/status.

Do not introduce N+1 list queries. For list responses, fetch summaries in batch
for the page of cluster IDs.

### 3. API Fakes And Tests

Update httptest fake repositories so handler tests can assert the new field.

Add tests covering:

- `GET /resources` includes `databaseOperationalSummary` for seeded database
  cluster rows.
- `GET /resources/{id}` includes the same field for a database cluster.
- Non-database resources do not unexpectedly gain fake database summaries.
- Integration seed data confirms ClickHouse cluster has the expected critical
  member rollup.
- Healthy cluster rollup has zero critical/warning member counts when seed data
  supports this.

### 4. OpenAPI

Update `internal/openapi/openapi.yaml`:

- Add `DatabaseOperationalSummary` schema.
- Reference it from the Resource schema.
- Document it as a derived read-only database member rollup for operator list
  views.

Run OpenAPI validation and fuzz tests.

## Verification Commands

Run all relevant checks before committing:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

If any command is unavailable in the environment, report it clearly with the
exact error and run the closest available substitute.

## Live HTTP Verification

Start backend if needed and verify with curl:

```bash
curl -s 'http://localhost:8080/resources?resourceType=database_cluster&resourceSubtype=clickhouse' | jq .
curl -s 'http://localhost:8080/resources/14' | jq .
```

Use actual seed IDs if they differ.

Report:

- one cluster with a critical/warning member summary
- one healthy or zero-abnormal cluster summary if available
- one non-database or instance row showing no misleading cluster rollup

## Commit Requirements

Use focused commits. Suggested commit:

```bash
git commit -m "feat: add database operational rollup read model"
```

No `Co-Authored-By`.

Do not tag, push, release, or merge.

## Final Report Required

At completion, report:

1. Worktree path, branch, commit hash.
2. Exact backend commit base.
3. Files changed.
4. Final JSON shape for `databaseOperationalSummary`.
5. Which endpoints include the field.
6. Integration seed IDs used.
7. Verification matrix.
8. GitNexus impact and detect-changes summary.
9. Live curl evidence.
10. Scope confirmation:

```text
- no frontend changes
- no write operations
- no manual SQL execution
- no topology layout changes
- no tag/push/release
- no AI co-author
- clean git status
```
