# Backend Phase 16.0: Stabilization Patch

You are implementing a focused backend stabilization patch for ControlHub.

Repository:
`/Users/fan/GolangProjects/ControlHub`

This phase exists because backend `main` currently fails `go test ./...`:

```text
TestListResources_PageSizeCap
expected pageSize capped to 100, got 500
```

Do not start new product work until this is green.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-25-phase-16-unified-inventory-operator-workflow-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-25-phase-16-unified-inventory-operator-workflow.md`
- `/Users/fan/GolangProjects/ControlHub/internal/model/pagination.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/resource_handler.go`
- `/Users/fan/GolangProjects/ControlHub/internal/api/resource_handler_test.go`
- `/Users/fan/GolangProjects/ControlHub/internal/openapi/openapi.yaml`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
```

Expected:

- worktree path is under `/Users/fan/GolangProjects/ControlHub/.worktrees`
- branch is dedicated to this phase, for example `phase-16-0-backend-stabilization`
- base includes recent backend `main` commits such as `4c359bc`, `8d8be36`, and `05272da`
- worktree is clean

Stop and report if path, branch, base, or cleanliness is wrong.

## Fixed Decisions

- This phase fixes backend test drift only.
- Keep `MaxPageSize = 500` unless code evidence proves this is wrong.
- Do not change frontend code.
- Do not change product behavior outside pagination cap contract.
- Do not add migrations.
- Do not change topology, archive, resource profile, auth, or dictionary behavior.
- Do not tag, push, release, or add AI co-author.

## Exact Scope

Allowed files:

- `internal/model/pagination.go`
- `internal/api/resource_handler_test.go`
- `internal/openapi/openapi.yaml`
- tests directly required to align page-size cap behavior

If another file is required, stop and report why before editing it.

## TDD Requirements

You must first reproduce the current failure:

```bash
go test ./...
```

Expected current failure:

```text
TestListResources_PageSizeCap
expected pageSize capped to 100, got 500
```

Then fix the test/code contract.

## Implementation Steps

1. Confirm `internal/model/pagination.go` has the intended constants.

Expected preferred contract:

```go
const (
	DefaultPage     = 1
	DefaultPageSize = 10
	MaxPageSize     = 500
	MaxPage         = 1_000_000_000
)
```

2. Update `TestListResources_PageSizeCap` to expect `model.MaxPageSize`, not hardcoded `100`.

Use this assertion style:

```go
if repo.lastQuery.PageSize != model.MaxPageSize {
	t.Fatalf("expected pageSize capped to %d, got %d", model.MaxPageSize, repo.lastQuery.PageSize)
}
```

3. Search OpenAPI for stale page-size limits:

```bash
rg -n "pageSize|maximum: 100|maximum: 500" internal/openapi/openapi.yaml internal/model internal/api
```

If OpenAPI says `maximum: 100` for resource/audit page-size params while code caps at `500`, update the spec to `500`.

4. Run focused verification:

```bash
go test -count=1 ./internal/model ./internal/api
make openapi-validate
```

5. Run full backend verification:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
```

6. Run GitNexus scope check before commit:

```bash
npx gitnexus detect_changes --scope all
```

If GitNexus is unavailable, record the exact failure and use `git diff --stat` as fallback.

7. Commit:

```bash
git status --short
git add internal/model/pagination.go internal/api/resource_handler_test.go internal/openapi/openapi.yaml
git diff --cached --stat
git diff --check --cached
git commit -m "fix: align resource page size cap contract (Phase 16.0)"
```

Only stage files that actually changed.

## Final Report Required

Report:

- worktree path and branch
- commit hash
- exact page-size decision (`MaxPageSize`)
- whether OpenAPI changed
- commands run and results
- GitNexus detect_changes result
- `git status --short --branch`
- confirmation:
  - no frontend code modified
  - no migrations added
  - no topology/archive/profile/auth/dictionary behavior changed
  - no tag/push/release
  - no AI co-author

