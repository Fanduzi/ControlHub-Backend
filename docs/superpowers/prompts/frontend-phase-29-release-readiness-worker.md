# Frontend Phase 29 Worker Prompt — Release Readiness Gates And CDP Smoke

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 29 Frontend — Release Readiness Gates And CDP Smoke**

This phase adds frontend release gate scripts and a small optional CDP live smoke
diagnostic. It is not a product UI phase.

## Required Input Documents

Read first:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-05-23-phase-29-release-readiness-mechanism.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-05-23-phase-29-release-readiness-mechanism.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-29-release-readiness-worker.md
/Users/fan/GolangProjects/ControlHub/docs/release-hardening-checklist.md
```

Reference pattern:

```text
/Users/fan/JsProjects/MusicRadio/scripts/cdp-helper.cjs
/Users/fan/JsProjects/MusicRadio/scripts/cdp-ui-test.cjs
/Users/fan/JsProjects/MusicRadio/scripts/cdp-playback-test.cjs
```

Use MusicRadio as a pattern for CDP live smoke mechanics, not as code to copy
blindly.

## Mandatory Worktree Requirement

Do not edit frontend `main` directly.

Create and use:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-29-release-readiness-mechanism -b feat/phase-29-release-readiness-mechanism main
cd .worktrees/frontend-phase-29-release-readiness-mechanism
git status --short --branch
git log --oneline -5
```

If the worktree already exists, verify it is clean and on the correct branch.
Do not overwrite user changes.

## Scope

Frontend-owned deliverables:

```text
package.json release scripts
scripts/cdp-release-smoke.mjs
tests/scripts/cdp-release-smoke.test.ts
frontend verification results for backend dry-run evidence
```

Do not modify backend files in this frontend worker.

Do not modify product components, pages, services, view models, layouts, or
product copy unless a test failure proves a P0/P1 bug. This phase should only
touch release/quality scripts and tests.

## Required npm Scripts

Add:

```json
"release:local": "npm run check:e2e-preflight && npm run check:e2e-governance && npx tsc --noEmit -p tsconfig.json && npm run lint && npm run test && npm run build",
"release:e2e": "npm run test:e2e:smoke && npm run test:e2e:interaction && npm run test:e2e",
"release:check": "npm run release:local && npm run release:e2e"
```

Preserve existing scripts.

## CDP Live Smoke

Implement:

```text
scripts/cdp-release-smoke.mjs
tests/scripts/cdp-release-smoke.test.ts
npm script: release:smoke:cdp
```

The script should:

```text
connect to existing Chrome remote debugging port
not launch Chrome
not kill Chrome
navigate critical release pages
check expected visible text
fail on raw enum leaks such as abnormal_first / needs_attention
collect runtime console errors
collect network 4xx/5xx
print compact summary
exit non-zero on failure
```

Default env:

```text
CDP_PORT=9222
CONTROLHUB_FRONTEND_URL=http://localhost:3000
```

Use Node 22 global `WebSocket`; do not add a new `ws` dependency unless you can
prove it is necessary.

Critical pages:

```text
/overview?environment=prod
/databases?environment=prod
/resources/14
/resources/22
/resources?page=1&pageSize=1
/audits?page=1&pageSize=1
```

Do not include `release:smoke:cdp` in `release:check`, because it requires a
manually started CDP browser.

## Required Tests

Create tests for pure helpers:

```text
hasForbiddenRawEnum
summarizeSmokeResult
```

Required assertions:

```text
detects abnormal_first and needs_attention in visible text
does not flag localized Chinese labels
summarizes failed page checks with URL and reason
returns a pass summary when all checks pass
```

## Verification

Run:

```bash
npm run release:local
npm run release:e2e
npm run test -- tests/scripts/cdp-release-smoke.test.ts
```

If a Chrome instance is available with remote debugging, also run:

```bash
CDP_PORT=9222 CONTROLHUB_FRONTEND_URL=http://localhost:3000 npm run release:smoke:cdp
```

If no CDP browser is available, state:

```text
CDP live smoke not run: no Chrome remote debugging target available.
```

Do not fake a CDP pass.

## Commit Guidance

Use small commits:

```text
build: add frontend release readiness scripts
test: add cdp release smoke diagnostics
```

Do not include AI co-author trailers.

## Final Report Required

Report:

```text
worktree / branch / commits
npm scripts added
CDP smoke files added
test coverage added
release:local result
release:e2e result
CDP live smoke result or NOT RUN reason
frontend commit hash for backend evidence
clean git status
```

Scope confirmation:

```text
No product UI changes
No backend changes
No API contract changes
No SQL
No topology layout changes
No publish/deploy/tag/push
No broad output suppression
No skipped/deleted tests
No AI co-author
```
