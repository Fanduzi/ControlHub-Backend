# Backend Phase 29 Worker Prompt — Release Readiness Mechanism

You are working in the backend/docs repository:

```text
/Users/fan/GolangProjects/ControlHub
```

## Phase

**Phase 29 Backend — Release Readiness Mechanism**

This phase turns the Phase 28 quality baseline into named backend release gates
and a release-candidate evidence format. It is not a product feature phase.

## Required Input Documents

Read first:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-23-phase-29-release-readiness-mechanism.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-23-phase-29-release-readiness-mechanism.md
/Users/fan/GolangProjects/ControlHub/docs/quality-baseline.md
/Users/fan/GolangProjects/ControlHub/docs/release-hardening-checklist.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-05-06-phase-28-quality-research.md
```

Reference pattern:

```text
/Users/fan/GolangProjects/DeltaScope/Makefile
/Users/fan/GolangProjects/DeltaScope/.github/workflows/release.yml
/Users/fan/GolangProjects/DeltaScope/.github/workflows/release-smoke.yml
```

Use DeltaScope as a pattern for layered gates, not as code to copy blindly.

## Mandatory Worktree Requirement

Do not edit backend `main` directly.

Create and use:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-29-release-readiness-mechanism -b phase-29-release-readiness-mechanism main
cd .worktrees/backend-phase-29-release-readiness-mechanism
git status --short --branch
git log --oneline -5
```

If the worktree already exists, verify it is clean and on the correct branch.
Do not overwrite user changes.

## Scope

Backend/docs owned deliverables:

```text
Makefile release gate targets
docs/quality-baseline.md updates
docs/release-hardening-checklist.md updates
docs/releases/candidates/TEMPLATE.md
docs/releases/candidates/2026-05-23-controlhub-rc-local.md
```

Do not modify frontend files in this backend worker. The frontend worker will
own npm scripts and CDP smoke.

## Required Backend Gate Targets

Add these Make targets:

```text
release-local-gates
release-docker-gates
release-readiness-gates
```

Expected behavior:

```text
release-local-gates:
  go test -count=1 ./...
  go vet ./...
  go build ./...
  make openapi-validate

release-docker-gates:
  make test-integration
  make test-openapi-fuzz

release-readiness-gates:
  release-local-gates + release-docker-gates
```

Extend the existing `.PHONY` line. Do not duplicate it.

## Required Docs Updates

Update:

```text
docs/quality-baseline.md
docs/release-hardening-checklist.md
```

Add backend release gate shortcuts:

```bash
make release-local-gates
make release-docker-gates
make release-readiness-gates
```

Explain:

```text
release-local-gates is the no-Docker baseline
release-docker-gates requires Docker
release-readiness-gates composes both
```

## Release Candidate Evidence Template

Create:

```text
docs/releases/candidates/TEMPLATE.md
```

It must include sections:

```text
Candidate metadata
Backend gates
Frontend gates
Live browser smoke
Known gaps
Failure classification
Go / No-Go decision
```

Use explicit fill markers such as:

```text
BACKEND_COMMIT_SHA
FRONTEND_COMMIT_SHA
EVALUATOR_NAME
```

The final dry-run evidence file must replace all markers.

## Dry-run Evidence File

After the frontend worker finishes, create:

```text
docs/releases/candidates/2026-05-23-controlhub-rc-local.md
```

Use the template and fill actual results:

```text
backend commit
frontend commit
backend gates
frontend gates
CDP live smoke result or NOT RUN reason
known accepted gaps
GO / NO-GO decision
```

If frontend worker is not finished yet, stop after template/docs commits and
report that dry-run evidence is blocked on frontend Phase 29 results.

## Verification

Run:

```bash
git diff --check
make release-local-gates
```

If Docker is available:

```bash
make release-docker-gates
```

Also scan generated docs:

```text
No conflict markers
No unfilled markers in final dry-run evidence
No PASS / FAIL placeholders in final dry-run evidence
```

Do not run destructive migration reset targets.

## Commit Guidance

Use small commits:

```text
build: add backend release readiness gates
docs: add release candidate evidence template
docs: record phase 29 release readiness dry run
```

Only create the dry-run commit after frontend Phase 29 results are available.
Do not include AI co-author trailers.

## Final Report Required

Report:

```text
worktree / branch / commits
Make targets added
docs updated
candidate template path
dry-run evidence path or blocked reason
backend verification matrix
Docker-dependent gate result
frontend result consumed, if any
clean git status
```

Scope confirmation:

```text
No product UI changes
No backend API contract changes
No SQL
No write operations
No topology layout changes
No publish/deploy/tag/push
No broad output suppression
No skipped/deleted tests
No AI co-author
```
