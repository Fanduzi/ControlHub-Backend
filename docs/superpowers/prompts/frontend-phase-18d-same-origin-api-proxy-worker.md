# Frontend Phase 18D Worker Prompt — Same-Origin API Proxy Cleanup

You are working in the frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

## Phase

**Phase 18D — Same-Origin API Proxy Cleanup**

## Required Input Documents

Read these backend-repo documents before changing frontend code:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-29-frontend-same-origin-api-proxy-cleanup.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-29-frontend-same-origin-api-proxy-cleanup.md
```

The implementation plan is authoritative. Follow it task-by-task unless current
frontend code has a factual mismatch. If there is a mismatch, stop and report
the exact mismatch before inventing another approach.

## Branch And Worktree

Create a dedicated frontend worktree:

```text
/Users/fan/JsProjects/ControlHub/.worktrees/frontend-phase-18d-same-origin-api-proxy
```

Branch:

```text
feat/phase-18d-same-origin-api-proxy
```

Base it on current frontend `main`, after Phase 18C has been merged.

## Problem

Manual frontend acceptance on `http://localhost:3000` failed with CORS because
browser requests went directly to `http://localhost:8081`, while
`e2e/api-proxy.mjs` allowed only `http://localhost:3100`.

Running manual acceptance on `3100` is only a workaround. The correct fix is:

```text
browser → same-origin /__api → Next rewrite → backend/proxy
```

## Required Deliverables

Implement:

1. Runtime-aware API base in `services/api-client.ts`
   - browser default: `/__api`
   - server default: `http://localhost:8080`
   - server override: `CONTROLHUB_API_BASE_URL`
   - browser override: `NEXT_PUBLIC_API_BASE_URL`

2. Next rewrite in `next.config.ts`
   - `/__api/:path*` to `CONTROLHUB_API_PROXY_URL` or backend default

3. Playwright environment update
   - browser uses `/__api`
   - server and rewrite target use `http://localhost:8081`
   - preserve dev-server wrapper; do not reintroduce `stderr: "ignore"`

4. E2E API proxy CORS fallback
   - no hardcoded single origin
   - default allowed origins:
     - `http://localhost:3000`
     - `http://localhost:3100`
   - optional env override:
     - `PLAYWRIGHT_PROXY_ALLOWED_ORIGINS`
   - echo exact allowed request origin
   - do not use `Access-Control-Allow-Origin: *`

5. Tests
   - API base resolution tests
   - API proxy CORS origin resolution tests

6. Live/browser verification
   - manual `3000` topology load no longer CORS-fails
   - Playwright `3100` still passes full gates

## Constraints

Do not:

- modify backend code
- change backend API paths
- change authentication semantics
- add product UI
- remove E2E request recording
- add broad CORS wildcard
- add broad output suppression
- tag, push, release
- add AI co-author attribution

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

Also run a manual/live verification for port `3000`:

```bash
CONTROLHUB_API_BASE_URL=http://localhost:8080 CONTROLHUB_API_PROXY_URL=http://localhost:8080 NEXT_PUBLIC_API_BASE_URL=/__api npm run dev -- -p 3000
```

Then verify:

```text
http://localhost:3000/resources/14?topologyDepth=2
```

Expected:

- topology loads
- browser requests use `/__api/resources/14/topology`
- no CORS console errors

Backend must be running on `:8080`.

## Commit Requirements

Commit all intended changes. Suggested commit messages:

```text
fix: route browser api calls through same-origin proxy
fix: echo allowed origins in e2e api proxy
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
## Phase 18D Final Report

### Worktree / Branch / Commits
| Item | Value |
|---|---|

### API Routing Behavior
| Runtime | Base URL |
|---|---|
| Browser | |
| Server | |
| Next rewrite target | |
| E2E proxy | |

### CORS Behavior
| Origin | Result |
|---|---|

### Manual 3000 Verification
| Check | Result |
|---|---|

### Playwright 3100 Verification
| Command | Result |
|---|---|

### Files Changed
| File | Purpose |
|---|---|

### Tests
| Test | Result |
|---|---|

### Scope Confirmation
- No backend changes:
- No product UI changes:
- No broad CORS wildcard:
- No broad output suppression:
- No tag/push/release:
- No AI co-author:
- git status:
```

Do not claim completion until manual 3000 and Playwright 3100 are both verified.

