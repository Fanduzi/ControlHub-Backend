# Phase 28 Worker Prompt — Quality Baseline And Release Hardening

You are preparing ControlHub's release-quality baseline after Phase 27B.

This is not a product feature phase. Do not redesign UI. Do not add database
operator functionality. This phase is about test coverage, quality gates,
release hardening, and small high-value automation only if evidence justifies it.

## Repositories

Backend/docs repo:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

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

Do not edit either main worktree directly.

Backend/docs worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/backend-phase-28-quality-baseline-release-hardening -b phase-28-quality-baseline-release-hardening main
cd .worktrees/backend-phase-28-quality-baseline-release-hardening
git status --short --branch
```

Frontend worktree, only if frontend files are changed:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-28-quality-baseline-release-hardening -b feat/phase-28-quality-baseline-release-hardening main
cd .worktrees/frontend-phase-28-quality-baseline-release-hardening
git status --short --branch
```

If a worktree already exists, verify it is clean and on the correct branch
before using it. Do not overwrite user changes.

## Goal

Produce a quality baseline that answers:

```text
What is covered by tests?
What is only covered manually?
What risks are not covered?
What should block future merges?
What small automation should be added now?
```

## Constraints

Do not:

- add product UI
- redesign database pages
- change backend API contracts
- execute SQL
- add write operations or work orders
- change topology layout
- skip/delete tests
- hide failures with broad output suppression
- solve failures by timeout-only changes
- tag, push, release, or merge
- add `Co-Authored-By`

## Required Deliverables

At minimum, create these backend/docs repo files:

```text
docs/quality-baseline.md
docs/release-hardening-checklist.md
docs/superpowers/notes/2026-05-06-phase-28-quality-research.md
```

Optional frontend files only if justified:

```text
scripts/check-e2e-preflight.mjs
tests/scripts/check-e2e-preflight.test.ts
package.json
```

Optional backend files only if justified:

```text
Makefile
scripts/check-quality-gates.sh
```

## Required Work

### 1. Inventory actual gates

Backend:

```bash
cd /Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-28-quality-baseline-release-hardening
grep -n "^[a-zA-Z0-9_.:-].*:" Makefile
find internal -maxdepth 3 -name '*_test.go' | sort
```

Frontend:

```bash
cd /Users/fan/JsProjects/ControlHub
node -e "const p=require('./package.json'); console.log(JSON.stringify(p.scripts,null,2))"
find e2e -maxdepth 2 -name '*.spec.ts' | sort
find tests -maxdepth 3 -name '*.test.ts' -o -name '*.test.tsx' | sort
```

Use the actual outputs. Do not invent commands.

### 2. Write coverage matrix

`docs/quality-baseline.md` must include:

```text
Backend gates
Frontend gates
Capability-by-test coverage matrix
Merge blocking rules
Known remaining gaps
```

The matrix must cover at least:

```text
login/auth
console shell navigation
environment context
resource list pagination/query params
database list search/filter/sort/signal
database detail cluster abnormal member workflow
database detail healthy instance workflow
overview attention queue
topology load and same-origin API proxy
audit list pagination/filtering
settings dictionaries
backend database operational summary
OpenAPI schema/fuzz
```

### 3. Research quality options

Research only these targeted questions:

```text
Playwright stale dev server / webServer / reuseExistingServer handling
Playwright traces/reporters for flaky failure diagnosis
OpenAPI validation and Schemathesis fuzzing as contract gates
PR vs merge vs nightly gate layering
Visual regression tradeoffs for this internal data console
```

Use primary/official documentation where possible. If you use web research,
include source links in the research note.

`docs/superpowers/notes/2026-05-06-phase-28-quality-research.md` must classify
recommendations as:

```text
Adopted
Deferred
Rejected
```

Every recommendation must map to a ControlHub risk. Do not write a generic
testing essay.

### 4. Write release checklist

`docs/release-hardening-checklist.md` must include exact commands for:

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
```

### 5. Decide on small automation

Do not add automation by default.

Add small automation only if the audit proves it protects a known recurring
risk. Candidate:

```text
frontend scripts/check-e2e-preflight.mjs
```

If added, it should:

```text
detect :3100 and :8081 listeners
print PID/command
not kill anything automatically
provide actionable next steps
```

If not added, explicitly explain why documentation + existing 27B proxy guard
is enough for now.

## Verification

For backend/docs changes, run:

```bash
cd /Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-28-quality-baseline-release-hardening
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
```

Also run a placeholder/conflict-marker scan across the three generated
documents. The scan must fail if any unfilled source marker, unfilled finding
marker, fix-me marker, or merge-conflict marker remains.

If Docker is available, also run:

```bash
make test-integration
make test-openapi-fuzz
```

If Docker is unavailable, state that explicitly.

If frontend files are changed, run:

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-28-quality-baseline-release-hardening
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

If frontend files are not changed, state the latest known baseline:

```text
frontend main @ 72bcb27, full E2E 50/50 passed after Phase 27B
```

## Commit Guidance

Use small commits:

```text
docs: document controlhub quality baseline
docs: record phase 28 quality research
docs: add release hardening checklist
test: add e2e preflight diagnostics
```

Only use the automation commit if automation is actually added.

Do not include AI co-author trailers.

## Final Report Required

Report:

```text
worktrees / branches / commits per repo
documents created
coverage matrix summary
research sources
adopted recommendations
deferred recommendations
rejected recommendations
automation added or explicitly deferred
backend verification matrix
frontend verification matrix if frontend changed
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
