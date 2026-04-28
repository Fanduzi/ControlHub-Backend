# Backend Phase 17A: Database Operator Read Model

You are implementing Backend Phase 17A for ControlHub: complete the read-only database operator contract.

Repository:
`/Users/fan/GolangProjects/ControlHub`

## Read First

- `/Users/fan/GolangProjects/ControlHub/AGENTS.md`
- `/Users/fan/GolangProjects/ControlHub/CLAUDE.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-27-phase-17-database-operator-drilldown-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-27-phase-17-database-operator-drilldown.md`

## Startup Check

Create a dedicated worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree add .worktrees/backend-phase-17a-database-operator-read-model -b phase-17a-database-operator-read-model main
cd .worktrees/backend-phase-17a-database-operator-read-model
git status --short --branch
git log --oneline -8
```

Expected:

- path is under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- branch is `phase-17a-database-operator-read-model`
- worktree is clean

Stop and report if any condition is false.

## GitNexus Requirement

This repo requires GitNexus impact checks before editing symbols.

If GitNexus reports stale index, run:

```bash
npx gitnexus analyze
```

Before modifying each target function/method, run impact analysis. At minimum assess:

- resource list/detail handler symbols
- relation handler/service symbols
- repository methods for resource detail/list/relations
- OpenAPI test paths if touched

Before committing, run `gitnexus_detect_changes()`.

## Exact Scope

Implement read-only contract improvements only:

1. Populate `profileSummary` where existing data supports it.
2. Add readable relation display fields while preserving bare IDs.
3. Add a stable database cluster members read path.
4. Update OpenAPI and tests.

Do not:

- add SQL execution
- add work orders
- add topology editing
- modify frontend
- restore demo `resourceSummaries`
- change auth behavior
- add tags, push, or release

## Contract Requirements

### Profile Summary

Populate nullable fields only when backed by existing data:

- `hostname`
- `ip`
- `port`
- `engine`
- `version`
- `nodeCount`
- `role`

Do not invent data.

### Relations

`GET /resources/{id}/relations` must preserve existing fields and add readable related resource metadata:

- `relatedResourceId`
- `relatedResourceName`
- `relatedResourceDisplayName`
- `relatedResourceType`
- `relatedResourceSubtype`
- `relatedResourceHealthStatus`
- `relatedResourceLifecycleStatus`

If you choose different names, update OpenAPI and final report with exact names.

### Cluster Members

Preferred endpoint:

`GET /resources/{id}/members`

Response shape:

```json
{
  "members": [
    {
      "resourceId": 22,
      "name": "payment-mysql-primary-prod",
      "displayName": "Payment MySQL Primary Production",
      "resourceType": "database_instance",
      "resourceSubtype": "mysql",
      "lifecycleStatus": "running",
      "healthStatus": "healthy",
      "profileSummary": {
        "hostname": "payment-mysql-01.prod",
        "port": 3306,
        "role": "primary"
      }
    }
  ]
}
```

If you decide not to add a new endpoint, stop and justify the alternative before coding.

## TDD Requirements

Write failing tests first for:

- readable relation fields
- cluster member endpoint
- profile summary population
- missing resource behavior
- non-cluster member request behavior

Then implement minimal production code.

## Verification

Run all:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make test
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Also run live HTTP checks against a local backend if practical:

- `GET /resources/{clusterId}/members`
- `GET /resources/{instanceId}`
- `GET /resources/{id}/relations`

## Commit

Commit after all checks pass:

```bash
git add internal cmd migrations scripts Makefile README.md CLAUDE.md
git commit -m "feat: complete database operator read model (Phase 17A)"
```

Only add files you actually changed. Do not include temp files.

No AI co-author. No tag. No push. No release.

## Final Report

Return:

1. Worktree path, branch, commit hash.
2. Exact endpoints changed or added.
3. Exact response fields added.
4. Whether a migration was added. If yes, explain why.
5. Integration seed IDs used for live verification.
6. Verification matrix for every command above.
7. GitNexus impact/detect_changes summary.
8. Confirmation:
   - no frontend changes
   - no SQL execution/work orders
   - no topology editing
   - no tag/push/release
   - no AI co-author
   - clean `git status`

