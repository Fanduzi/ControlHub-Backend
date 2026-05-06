# Frontend Phase 28 Worker Prompt — E2E Quality Baseline And Preflight

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 28 Frontend — E2E Quality Baseline And Preflight**

This is not a product UI phase. Do not redesign database pages, change layout,
change operational-signal semantics, or add new product functionality.

Your job is to audit frontend test coverage and, if justified, add small
frontend quality automation that protects known E2E environment risks.

## Required Input Documents

Read first:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-28-quality-baseline-release-hardening.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-06-phase-28-quality-baseline-release-hardening.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-28-quality-baseline-release-hardening-worker.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-06-phase-27b-e2e-query-param-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-28-frontend-e2e-governance-gate.md
```

## Mandatory Worktree Requirement

Do not edit frontend `main` directly.

Create and use this worktree:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-28-quality-baseline-release-hardening -b feat/phase-28-quality-baseline-release-hardening main
cd .worktrees/frontend-phase-28-quality-baseline-release-hardening
git status --short --branch
git log --oneline -5
```

If the worktree already exists, verify it is clean and on the correct branch.
Do not overwrite user changes.

## Scope

Frontend-owned scope:

```text
audit frontend test scripts and suites
verify E2E governance still matches current suite
decide whether stale :3100/:8081 preflight automation is needed
add small preflight diagnostics only if justified
run frontend gates
provide frontend findings for the backend/docs quality baseline
```

Likely files if automation is adopted:

```text
scripts/check-e2e-preflight.mjs
tests/scripts/check-e2e-preflight.test.ts
package.json
```

Do not modify product components, pages, services, view models, i18n product
copy, or backend files.

## Required Evidence Collection

Run:

```bash
node -e "const p=require('./package.json'); console.log(JSON.stringify(p.scripts,null,2))"
find e2e -maxdepth 2 -name '*.spec.ts' | sort
find tests -maxdepth 3 -name '*.test.ts' -o -name '*.test.tsx' | sort
find scripts -maxdepth 2 -type f | sort
sed -n '1,240p' playwright.config.ts
```

Record:

```text
current npm scripts
E2E spec list and rough purpose
unit/component test count from npm run test
known frontend gates
known E2E environment risks
```

## Required Decision: Preflight Automation

Known risk from Phase 27B:

```text
Playwright reuseExistingServer reused stale :3100 dev server
stale server did not have CONTROLHUB_API_BASE_URL=http://localhost:8081
server-side fetches bypassed :8081 E2E proxy
recorded-request harness timed out
```

Decide whether to add:

```text
scripts/check-e2e-preflight.mjs
npm script: check:e2e-preflight
tests/scripts/check-e2e-preflight.test.ts
```

Adopt only if it is small and non-invasive.

If adopted, behavior must be:

```text
detect listeners on :3100 and :8081
print PID and command where available
warn that stale frontend/proxy processes can break E2E proxy recording
exit non-zero only when explicitly configured to enforce strict mode, or document chosen behavior clearly
never kill processes automatically
never suppress output broadly
```

If not adopted, final report must explain why the existing Phase 27B proxy
recording guard plus release checklist is enough.

## Optional Implementation Shape

If adding `scripts/check-e2e-preflight.mjs`, prefer pure helper functions for
parsing command output so tests do not need real ports.

Possible exports:

```js
export function parseLsofOutput(output) {}
export function formatPortWarning(port, listeners) {}
export function shouldFailPreflight({ strict, listeners }) {}
```

Test cases should cover:

```text
empty lsof output
single listener
multiple listeners
strict mode fails when listener exists
non-strict mode prints warning but exits zero
```

Do not use this script to kill processes.

## Verification

Run:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

If a new preflight script is added, also run:

```bash
npm run check:e2e-preflight
npm run test -- tests/scripts/check-e2e-preflight.test.ts
```

If full E2E fails, classify it. Do not call it pre-existing without current
main comparison evidence.

## Commit Guidance

Use small commits:

```text
docs: record frontend quality baseline findings
test: add e2e preflight diagnostics
```

Only create a docs commit if you add frontend-local documentation. If you only
add the script/tests, one test commit is enough.

Do not include AI co-author trailers.

## Final Report Required

Report:

```text
worktree / branch / commits
frontend scripts and E2E suite inventory
preflight automation decision
files changed
verification matrix
full E2E result
known remaining frontend gaps
handoff notes for backend/docs quality baseline
clean git status
```

Scope confirmation:

```text
No product UI changes
No backend changes
No API contract changes
No SQL
No topology layout changes
No broad output suppression
No skipped/deleted tests
No timeout-only fix
No tag/push/release
No AI co-author
```
