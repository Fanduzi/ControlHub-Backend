# Backend Phase 28 Worker Prompt — Quality Baseline And Release Hardening

You are working in the backend/docs repository:

```text
/Users/fan/GolangProjects/ControlHub
```

## Phase

**Phase 28 Backend/Docs — Quality Baseline And Release Hardening**

This is not a backend feature phase. Do not change API behavior, database
schema, SQL, write operations, topology behavior, or product semantics.

Your job is to create the canonical quality baseline documentation and release
hardening checklist, using evidence from both backend and frontend repositories.

## Required Input Documents

Read first:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-28-quality-baseline-release-hardening.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-06-phase-28-quality-baseline-release-hardening.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27b-e2e-query-param-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-28-frontend-e2e-governance-gate.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md
```

## Mandatory Worktree Requirement

Do not edit backend `main` directly.

Create and use this worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-28-quality-baseline-release-hardening -b phase-28-quality-baseline-release-hardening main
cd .worktrees/backend-phase-28-quality-baseline-release-hardening
git status --short --branch
git log --oneline -5
```

If the worktree already exists, verify it is clean and on the correct branch.
Do not overwrite user changes.

## Scope

Create these files in the backend/docs repo:

```text
docs/quality-baseline.md
docs/release-hardening-checklist.md
docs/superpowers/notes/2026-05-06-phase-28-quality-research.md
```

Optional backend-only automation is allowed only if strongly justified:

```text
Makefile
scripts/check-quality-gates.sh
```

Do not create or modify frontend files in this backend worker.

## Required Evidence Collection

Backend commands:

```bash
cd /Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-28-quality-baseline-release-hardening
grep -n "^[a-zA-Z0-9_.:-].*:" Makefile
find internal -maxdepth 3 -name '*_test.go' | sort
find . -maxdepth 3 -name '*openapi*' -o -name '*schemathesis*'
```

Frontend commands, read-only:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
node -e "const p=require('./package.json'); console.log(JSON.stringify(p.scripts,null,2))"
find e2e -maxdepth 2 -name '*.spec.ts' | sort
find tests -maxdepth 3 -name '*.test.ts' -o -name '*.test.tsx' | sort
find scripts -maxdepth 2 -type f | sort
```

Use actual outputs. Do not invent commands or suites.

## Document 1: Quality Baseline

Create:

```text
docs/quality-baseline.md
```

It must include:

```text
Purpose
Backend gates
Frontend gates
Coverage matrix
Known remaining gaps
Merge blocking rules
```

Coverage matrix must cover at least:

```text
Login and auth session
Console shell navigation
Environment context
Resource list pagination/query params
Database list search/filter/sort/signal
Database detail cluster abnormal member workflow
Database detail healthy instance workflow
Overview attention queue
Topology load and same-origin API proxy
Audit list pagination/filtering
Settings dictionaries
Backend resource CRUD/read models
Backend database operational summary
OpenAPI schema validity
OpenAPI fuzz behavior
```

Columns:

```text
Backend unit
Backend integration
OpenAPI validation/fuzz
Frontend unit/component
E2E smoke
E2E interaction
E2E workflow
Manual browser
Gap / next action
```

## Document 2: Quality Research Notes

Create:

```text
docs/superpowers/notes/2026-05-06-phase-28-quality-research.md
```

Research only these topics:

```text
Playwright webServer reuseExistingServer and stale dev server risk
Playwright traces/reporters for flaky failure diagnosis
OpenAPI validation and Schemathesis fuzzing as API contract gates
PR vs merge vs nightly gate layering
Visual regression tradeoffs for this internal data console
```

Use primary/official documentation where possible. Include source links.

Classify every recommendation:

```text
Adopted
Deferred
Rejected
```

Every recommendation must map to a ControlHub risk. Do not write a generic
testing essay.

## Document 3: Release Hardening Checklist

Create:

```text
docs/release-hardening-checklist.md
```

It must include exact commands for:

```text
frontend preflight
backend preflight
backend gates
frontend gates
manual browser checks
failure classification
merge blockers
```

It must explicitly mention:

```text
:3100 stale frontend dev server
:8081 stale E2E proxy
/__api same-origin browser calls
database list search/filter/sort interactions
overview attention reason vs database detail reason
Docker-dependent backend gates
```

## Verification

Run from the backend Phase 28 worktree:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
```

Run a placeholder/conflict-marker scan over the three generated docs. It must
fail if any unfilled source marker, unfilled finding marker, fix-me marker, or
merge-conflict marker remains.

If Docker is available, also run:

```bash
make test-integration
make test-openapi-fuzz
```

If Docker is unavailable, state that explicitly.

Frontend verification is not required in this backend worker unless you modify
frontend files, which you should not do. Reference the latest known frontend
baseline:

```text
frontend main @ 72bcb27, full E2E 50/50 passed after Phase 27B
```

## Commit Guidance

Use small docs commits:

```text
docs: document controlhub quality baseline
docs: record phase 28 quality research
docs: add release hardening checklist
```

Do not include AI co-author trailers.

## Final Report Required

Report:

```text
worktree / branch / commits
documents created
coverage matrix summary
research sources
adopted recommendations
deferred recommendations
rejected recommendations
automation added or explicitly deferred
backend verification matrix
frontend baseline reference
known remaining gaps
release-blocking rules
clean git status
```

Scope confirmation:

```text
No product UI changes
No backend API contract changes
No SQL
No write operations
No topology layout changes
No broad output suppression
No skipped/deleted tests
No tag/push/release
No AI co-author
```
