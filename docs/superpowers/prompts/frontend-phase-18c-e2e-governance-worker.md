# Frontend Phase 18C Worker Prompt — E2E Governance Gate

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 18C — Frontend E2E Governance Gate**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-28-frontend-e2e-governance-gate.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-28-frontend-e2e-governance-gate.md
```

The implementation plan is authoritative. Follow it task by task unless current
frontend code has a factual mismatch. If there is a mismatch, report it before
inventing a different approach.

## Branch And Worktree

Create a dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-18c-e2e-governance
```

Branch:

```text
feat/phase-18c-e2e-governance
```

Base it on current frontend `main`, after Phase 18B has been merged.

## Context

Phase 18B made the E2E suite green and removed broad stderr suppression. Phase
18C must prevent the same mistakes from coming back.

Known rules to preserve:

- SSR page E2E should use `loginViaUI()`, not `loginViaApi()`.
- Browser E2E specs must use console/network guards.
- `stderr: "ignore"` and `stdout: "ignore"` are forbidden.
- Success-path screenshots are forbidden.
- Known runtime noise can only be filtered by exact documented allowlist.
- Full E2E failures need classification; do not write "pre-existing" without a
  table.

## Deliverables

Implement:

```text
docs/e2e-governance.md
scripts/check-e2e-governance.mjs
package.json script: check:e2e-governance
```

The checker must fail on:

- `stderr: "ignore"` or `stdout: "ignore"` in Playwright config
- shell output redirection to `/dev/null` in Playwright config
- `loginViaApi()` in E2E specs without marker:
  `e2e-governance-allow-loginViaApi`
- application-page E2E specs without `collectConsoleMessages`,
  `collectNetworkErrors`, and `assertClean`
- `page.screenshot()` not guarded by:
  `testInfo.status !== testInfo.expectedStatus`

## Constraints

Do not:

- modify backend code
- add product UI
- redesign pages
- restore `/cmdb` navigation
- restore demo `resourceSummaries`
- add new dependencies
- weaken the checker to pass a real violation
- suppress process output broadly
- tag, push, release
- add AI co-author attribution

If the checker reports real existing violations, fix the violating E2E spec or
config. Do not hide violations.

## Required Commands

Run all:

```bash
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
git status --short --branch
```

Backend must be running on `:8080` for E2E. If needed:

```bash
cd /Users/fan/GolangProjects/ControlHub
go run ./cmd/server
```

## Commit Requirements

Commit all intended changes.

Suggested commit messages:

```text
docs: add frontend e2e governance rules
test: add frontend e2e governance checker
```

Do not include:

```text
Co-Authored-By
Claude
Anthropic
AI
```

## Final Report Format

Return:

```markdown
## Phase 18C Final Report

### Worktree / Branch / Commits
| Item | Value |
|---|---|

### Files Changed
| File | Purpose |
|---|---|

### Governance Rules Enforced
| Rule | Enforcement |
|---|---|

### Verification
| Command | Result |
|---|---|

### Scope Confirmation
- No backend changes:
- No product UI changes:
- No broad output suppression:
- No success-path screenshots:
- No temp artifacts committed:
- No tag/push/release:
- No AI co-author:
- git status:
```

Do not claim completion until every required command has been run and reported.

