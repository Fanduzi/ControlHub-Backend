# Phase 30 RC Evidence Orchestration Implementation Plan

> **For agentic workers:** Use this plan as an execution checklist. Keep changes
> documentation-first. Do not modify product frontend or backend code unless a
> missing evidence validation helper is explicitly approved.

**Goal:** Turn the Phase 29 frontend/backend release gates into a repeatable RC
evidence workflow with explicit warning classification and a current dry-run
candidate record.

**Architecture:** Backend/docs repository owns canonical release evidence. The
frontend repository supplies gate output and commit identity, but Phase 30 does
not need frontend product changes.

**Primary repo:** `/Users/fan/GolangProjects/ControlHub`

**Reference frontend repo:** `/Users/fan/JsProjects/ControlHub`

---

## Required Reading

```text
docs/superpowers/specs/2026-05-23-phase-30-rc-evidence-orchestration.md
docs/superpowers/prompts/backend-phase-30-rc-evidence-orchestration-worker.md
docs/releases/candidates/TEMPLATE.md
docs/releases/candidates/2026-05-23-controlhub-rc-local.md
docs/release-hardening-checklist.md
docs/quality-baseline.md
```

Reference current gate commands:

```text
frontend:
  cd /Users/fan/JsProjects/ControlHub
  npm run release:check

backend:
  cd /Users/fan/GolangProjects/ControlHub
  make release-readiness-gates
```

---

## Worktree

Create a backend/docs worktree:

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

---

## Constraints

- No product UI changes.
- No backend API contract changes.
- No SQL or migrations.
- No write operations.
- No topology behavior changes.
- No publish, deploy, tag, or push.
- No skipped or deleted tests.
- No broad output suppression.
- No AI co-author in commits.

---

## Task 1: Update RC Evidence Template

**Files:**

```text
docs/releases/candidates/TEMPLATE.md
docs/release-hardening-checklist.md
```

- [ ] Inspect existing template:

```bash
sed -n '1,240p' docs/releases/candidates/TEMPLATE.md
```

- [ ] Add or verify these fields exist in the template:

```text
Backend worktree status
Frontend worktree status
Required gates
Optional gates
Warning classification
Skipped gates
Decision reason
```

- [ ] Keep existing template sections intact:

```text
Candidate
Backend Gates
Frontend Gates
Live Browser Smoke
Known Gaps
Failure Classification
Go / No-Go Decision
```

- [ ] Update `docs/release-hardening-checklist.md` with an "RC Evidence
  Orchestration" section that states the order:

```text
1. confirm backend/frontend git status
2. record backend/frontend commits
3. run backend release-readiness gates
4. run frontend release:check
5. classify warnings and optional gates
6. write candidate evidence
7. decide GO / NO-GO
```

Do not add a new required gate unless it already exists and passes.

---

## Task 2: Generate Current RC Evidence

**Files:**

```text
docs/releases/candidates/2026-05-23-controlhub-rc-local-2.md
```

- [ ] Record current backend commit:

```bash
cd /Users/fan/GolangProjects/ControlHub
git rev-parse --short HEAD
git status --short --branch
```

- [ ] Record current frontend commit:

```bash
cd /Users/fan/JsProjects/ControlHub
git rev-parse --short HEAD
git status --short --branch
```

- [ ] Use the latest verified gate results:

Frontend:

```text
npm run release:check
result: PASS
release:local: PASS
unit/component tests: 556 PASS
build: PASS
release:e2e: PASS
smoke: 7/7 PASS
interaction: 3/3 PASS
full E2E: 50/50 PASS
```

Backend:

```text
make release-readiness-gates
result: PASS
go test: PASS
go vet: PASS
go build: PASS
openapi-validate: PASS
integration: PASS
OpenAPI fuzz: 960/960 PASS
```

- [ ] Classify the current OpenAPI fuzz warnings:

```text
Accepted warning:
  PATCH /resources/{id} missing valid generated ID caused repeated 404.

Follow-up warning:
  API validation is stricter than schema for PATCH /resources/{id},
  POST /auth/login, and POST /resources.
```

These are non-blocking only because Schemathesis reported all selected
operations tested and all configured checks passed.

- [ ] Record CDP smoke as one of:

```text
PASS
FAIL
NOT RUN - no Chrome remote debugging target available on port 9222
```

Do not mark CDP smoke as required unless it was actually run.

- [ ] Decision rule:

```text
GO only if backend required gates PASS and frontend required gates PASS.
NO-GO if either required gate fails or any failure is unclassified.
```

---

## Task 3: Optional Lightweight Evidence Validation

Only do this if it stays small.

**Possible file:**

```text
scripts/check-release-evidence.sh
```

Allowed behavior:

```text
check required headings exist
check no BACKEND_COMMIT_SHA / FRONTEND_COMMIT_SHA placeholders remain
check Decision is GO or NO-GO
check required gate rows exist
```

Forbidden behavior:

```text
do not run backend gates
do not run frontend gates
do not parse terminal logs
do not hide warnings
```

If the script starts becoming complex, do not add it. Keep Phase 30 docs-only.

---

## Task 4: Verification

Run backend/docs verification:

```bash
git diff --check
rg -n "BACKEND_COMMIT_SHA|FRONTEND_COMMIT_SHA|PASS / FAIL|GO / NO-GO|TODO|TBD" docs/releases/candidates/2026-05-23-controlhub-rc-local-2.md
```

Expected:

```text
git diff --check: no output
placeholder scan: no unfilled placeholders in the real candidate evidence file
```

If a validation script was added, run it.

Do not rerun the full frontend/backend gates unless the evidence is stale or the
worker changed gate scripts.

---

## Task 5: Commit

Commit documentation updates:

```bash
git add docs/releases/candidates/TEMPLATE.md \
        docs/releases/candidates/2026-05-23-controlhub-rc-local-2.md \
        docs/release-hardening-checklist.md \
        docs/superpowers/specs/2026-05-23-phase-30-rc-evidence-orchestration.md \
        docs/superpowers/plans/2026-05-23-phase-30-rc-evidence-orchestration.md

git commit -m "docs: add rc evidence orchestration plan"
```

If additional docs were changed, include them only if directly related.

No `Co-Authored-By`.

---

## Final Report Requirements

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
verification commands
final git status
scope confirmation
```

Scope confirmation must state:

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
