# Backend Phase 16A: Inventory Contract Freeze

You are implementing the backend inventory contract freeze for ControlHub Phase 16.

Repository:
`/Users/fan/GolangProjects/ControlHub`

This phase exists to freeze the backend read contract that the frontend unified inventory and database operator workflow will consume.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-25-phase-16-unified-inventory-operator-workflow-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-25-phase-16-unified-inventory-operator-workflow.md`
- `/Users/fan/GolangProjects/ControlHub/internal/model/resource.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/resource_handler.go`
- `/Users/fan/GolangProjects/ControlHub/internal/repository/mysql/resource_repository.go`
- `/Users/fan/GolangProjects/ControlHub/internal/service/resource_service.go`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`
- `/Users/fan/GolangProjects/ControlHub/internal/integration/resource_test.go`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -10
git worktree list
```

Expected:

- worktree path is under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- branch is dedicated to this phase, for example `phase-16-a-inventory-contract-freeze`
- base includes Phase 16.0 stabilization
- `go test ./...` is green before you start contract work
- worktree is clean

Stop and report if path, branch, base, test baseline, or cleanliness is wrong.

## Dependency Rules

- Backend Phase 16.0 must land first.
- Frontend Phase 16B depends on this backend contract.
- Frontend and backend workers cannot communicate during execution, so this prompt must produce a clear final contract report.

## Fixed Decisions

- Do not change storage schema unless you prove a required contract cannot be met without schema changes.
- Prefer response-layer enrichment over migrations.
- Do not modify frontend code.
- Do not start topology redesign.
- Do not add SQL work orders, auth redesign, notifications, bulk actions, or import/export.
- Keep OpenAPI as the public source of truth.

## Contract Surfaces To Freeze

Audit and, if needed, fix:

- `GET /resources`
- `GET /resources/{id}`
- `GET /resources/{id}/relations`
- `GET /resource-subtypes`

Required decisions:

- Is `profileSummary` populated in list responses where documented?
- Is `clusterId` present where database tree/detail needs it?
- Does `GET /resources/{id}` consistently return `{ resource, members? }`?
- Do relations include readable related resource data, or is a separate lookup path explicitly accepted?
- Do OpenAPI examples match real JSON?

## Task 1: Contract Audit

Create:

- `docs/superpowers/notes/2026-04-25-phase-16-inventory-contract-audit.md`

Use this structure:

```markdown
# Phase 16 Inventory Contract Audit

## Backend Commit

## Endpoints Audited

| Endpoint | Current Shape | OpenAPI Match | Frontend Need | Decision |
|----------|---------------|---------------|---------------|----------|
| GET /resources | | | | |
| GET /resources/{id} | | | | |
| GET /resources/{id}/relations | | | | |
| GET /resource-subtypes | | | | |

## Live JSON Evidence

## Gaps

## Required Backend Fixes

## Required Frontend Assumptions
```

Run backend:

```bash
go run ./cmd/server
```

In another shell, collect:

```bash
curl -sS "http://localhost:8080/resources?page=1&pageSize=2" | jq .
curl -sS "http://localhost:8080/resources/41000000-0000-0000-0000-000000000010" | jq .
curl -sS "http://localhost:8080/resources/41000000-0000-0000-0000-000000000010/relations" | jq .
curl -sS "http://localhost:8080/resource-subtypes?resourceType=database_instance" | jq .
```

Run:

```bash
make openapi-validate
rg -n "ResourceDetailResponse|profileSummary|clusterId|relatedResource|members|resource-subtypes" internal/openapi/openapi.yaml
```

Commit the audit before implementation:

```bash
git add docs/superpowers/notes/2026-04-25-phase-16-inventory-contract-audit.md
git commit -m "docs: audit inventory contract for Phase 16"
```

## Task 2: Implement Only Blocking Contract Fixes

Do this task only for gaps found in Task 1.

Allowed fixes:

- response envelope mismatch
- OpenAPI field missing for existing JSON
- JSON type mismatch
- `profileSummary` documented but not populated where frontend requires it
- `clusterId` inconsistency that breaks database tree/detail
- relation/member display data needed to remove UUID-first UI

Required TDD:

- Add handler tests for response shape.
- Add service/repository tests for enrichment logic if SQL or service logic changes.
- Add integration tests for MySQL-backed contract facts.
- Add OpenAPI validation.

Required verification:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
npx gitnexus detect_changes --scope all
```

Commit:

```bash
git add <changed-files>
git diff --cached --stat
git diff --check --cached
git commit -m "fix: freeze inventory contract for database workflow (Phase 16A)"
```

## Final Report Required

Report:

- worktree path and branch
- commits
- audit decisions table
- endpoints changed
- OpenAPI changes
- live JSON evidence summary
- verification command results
- GitNexus detect_changes result
- explicit frontend assumptions after freeze
- `git status --short --branch`
- confirmation:
  - no frontend code modified
  - no unrelated migrations
  - no topology redesign
  - no tag/push/release
  - no AI co-author

