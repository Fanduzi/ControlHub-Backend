# Backend Phase 30 RC Evidence Orchestration Worker Prompt

You are working in the backend/docs repository:

```text
/Users/fan/GolangProjects/ControlHub
```

## Objective

Execute Phase 30: turn the already-passing frontend/backend release gates into
a committed RC evidence bundle.

This is a documentation and release-governance phase. Do not modify product
frontend code, backend API code, SQL, migrations, topology behavior, or test
semantics unless explicitly instructed after reporting a blocker.

## Required Context

Read these files first:

```text
docs/superpowers/specs/2026-05-23-phase-30-rc-evidence-orchestration.md
docs/superpowers/plans/2026-05-23-phase-30-rc-evidence-orchestration.md
docs/releases/candidates/TEMPLATE.md
docs/releases/candidates/2026-05-23-controlhub-rc-local.md
docs/release-hardening-checklist.md
docs/quality-baseline.md
```

Reference frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Worktree

Create and use a backend/docs worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-30-rc-evidence-orchestration -b phase-30-rc-evidence-orchestration main
cd .worktrees/backend-phase-30-rc-evidence-orchestration
git status --short --branch
```

Expected: clean branch `phase-30-rc-evidence-orchestration`.

Do not edit the main worktree directly.

## Current Verified Gate Results

These were already run on current `main` and should be recorded in evidence.
Re-run only if commit SHAs changed, the worktree is dirty, or evidence appears
stale.

### Frontend

Repository:

```text
/Users/fan/JsProjects/ControlHub
```

Command:

```bash
npm run release:check
```

Verified result:

```text
PASS
release:local PASS
release:e2e PASS
unit/component tests: 556/556 PASS
E2E smoke: 7/7 PASS
E2E interaction: 3/3 PASS
Full E2E: 50/50 PASS
final git status: clean
```

### Backend

Repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Command:

```bash
make release-readiness-gates
```

Verified result:

```text
PASS
go test PASS
go vet PASS
go build PASS
openapi-validate PASS
integration PASS
OpenAPI fuzz: 960 generated / 960 passed
final git status: clean
```

OpenAPI fuzz warnings:

```text
Missing test data:
  PATCH /resources/{id} repeatedly returned 404 because generated IDs did not
  hit an existing resource.

Schema validation mismatch:
  PATCH /resources/{id}
  POST /auth/login
  POST /resources
```

Classify these warnings explicitly:

```text
PATCH /resources/{id} missing generated ID data:
  Accepted warning for this RC. All configured Schemathesis checks passed.

Schema validation mismatch:
  Follow-up warning. API validation is stricter than the OpenAPI schema for
  generated invalid data; not blocking because not_a_server_error,
  status_code_conformance, content_type_conformance, and
  response_schema_conformance all passed.
```

## Tasks

### Task 1: Update RC Evidence Template

Modify:

```text
docs/releases/candidates/TEMPLATE.md
docs/release-hardening-checklist.md
```

Requirements:

- Keep existing template sections.
- Add explicit fields for backend/frontend worktree status.
- Add required gates vs optional gates.
- Add warning classification.
- Add skipped optional gate handling.
- Add decision reason.
- In `docs/release-hardening-checklist.md`, add an "RC Evidence
  Orchestration" section with this order:

```text
1. confirm backend/frontend git status
2. record backend/frontend commits
3. run or reference backend release-readiness gates
4. run or reference frontend release:check
5. classify warnings and optional gates
6. write candidate evidence
7. decide GO / NO-GO
```

### Task 2: Generate Current RC Evidence

Create:

```text
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
```

Use the template, but fill all placeholders.

Record:

```bash
cd /Users/fan/GolangProjects/ControlHub
git rev-parse --short HEAD
git status --short --branch

cd /Users/fan/JsProjects/ControlHub
git rev-parse --short HEAD
git status --short --branch
```

The evidence must include:

- backend commit
- frontend commit
- backend gate command and result
- frontend gate command and result
- frontend E2E counts
- backend OpenAPI fuzz count
- warning classification
- CDP smoke status
- dirty worktree status
- final `GO` or `NO-GO`

CDP smoke status rule:

```text
If not run, record:
NOT RUN - no Chrome remote debugging target available on port 9222
```

Do not pretend CDP smoke passed unless it actually ran.

Decision rule:

```text
GO only if backend required gates PASS and frontend required gates PASS.
NO-GO if either required gate fails or any failure is unclassified.
```

### Task 3: Verification

Run:

```bash
git diff --check
rg -n "BACKEND_COMMIT_SHA|FRONTEND_COMMIT_SHA|PASS / FAIL|GO / NO-GO|TODO|TBD" docs/releases/candidates/2026-05-24-controlhub-rc-local.md
```

Expected:

```text
git diff --check: no output
placeholder scan: no matches
```

Also run:

```bash
git status --short --branch
```

Do not run full frontend/backend gates again unless commits changed or evidence
is stale.

### Task 4: Commit

Stage only directly related docs:

```bash
git add docs/releases/candidates/TEMPLATE.md \
        docs/releases/candidates/2026-05-24-controlhub-rc-local.md \
        docs/release-hardening-checklist.md
```

Commit:

```bash
git commit -m "docs: add rc evidence orchestration"
```

No `Co-Authored-By`.

## Final Report

Report:

```text
worktree
branch
commit hash
files changed
backend commit recorded
frontend commit recorded
required gate results
warning classifications
optional CDP smoke status
GO / NO-GO decision
verification commands and results
final git status
```

Scope confirmation:

```text
No product UI changes
No backend API contract changes
No SQL
No write operations
No topology behavior changes
No publish/deploy/tag/push
No skipped/deleted tests
No broad output suppression
No AI co-author
```

