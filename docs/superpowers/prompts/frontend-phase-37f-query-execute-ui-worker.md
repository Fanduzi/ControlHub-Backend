# Frontend Phase 37F Query Execute UI Worker Prompt

You are implementing the frontend side of Phase 37F for ControlHub.

Frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repository is separate and must not be edited by this worker:

```text
/Users/fan/GolangProjects/ControlHub
```

## Objective

Wire the existing `/query` Query Workbench shell to the backend Phase 37 execute
and history APIs. A ready MySQL/TiDB target should be able to run one guarded
SELECT and display results. Locked targets must remain locked.

## Required Reading

Backend docs:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-22-phase-37f-query-execute-ui-and-dev-credentials.md
/Users/fan/GolangProjects/ControlHub/docs/decisions/2026-06-22-phase-37f-query-execute-ui-boundary.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-06-22-phase-37f-query-execute-ui-and-dev-credentials.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-21-phase-37-read-only-query-sandbox.md
```

Frontend code:

```text
app/(console)/query/page.tsx
components/query/query-workbench.tsx
components/query/query-editor-shell.tsx
components/query/query-governance-panel.tsx
services/query-targets.ts
types/query-target.ts
messages/en.json
messages/zh-CN.json
e2e/query-workbench.spec.ts
package.json
```

## Worktree

Create a frontend worktree after backend Phase 37F has merged:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-37f-query-execute-ui -b feat/phase-37f-query-execute-ui main
cd .worktrees/frontend-phase-37f-query-execute-ui
git status --short --branch
```

Do not edit the backend repository.

## Backend Dependency

Backend must provide:

```text
GET /query-targets
POST /query-targets/{id}/execute
GET /query-targets/{id}/executions
```

Final E2E must use a real backend with one ready target created by the backend
Phase 37F dev credential seed. Do not mock backend responses for final E2E.

## Scope

Allowed:

```text
query execution TypeScript types
query execution service methods
Run button wiring
result table
controlled error states
history tab
i18n
unit/component tests
cross-repo E2E
```

Not allowed:

```text
backend edits
credential input/storage
actorUserId in request body/query
export
saved queries
approval workflow
new query engines
AI query assistance
fake backend for final E2E
push/tag/release/deploy
```

## Implementation

Follow frontend Tasks F1, F2, and F3 in:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-06-22-phase-37f-query-execute-ui-and-dev-credentials.md
```

Critical requirements:

- Run enabled only when `selectedTarget.availableActions.run === true`.
- Request body contains only `statement` and `maxRows`.
- Never send `actorUserId`.
- Use existing authenticated API/session mechanism.
- Disable Run while request is pending.
- Render backend controlled errors without raw stack traces.
- Render SQL NULL as a visible `NULL` marker, never `0` or `undefined`.
- Refresh history after execution settles.
- Locked targets stay locked.

## Verification

Run:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Then run real cross-repo E2E with backend on `:8080` and frontend proxy/dev
server:

```bash
npm run test:e2e -- --grep query
npm run test:e2e
```

If backend is unavailable, stop and report. Do not claim E2E passed.

## Final Report

Include:

- worktree, branch, commit hash
- files changed
- service request shape proof: no `actorUserId`
- ready/locked UI behavior
- result/history rendering behavior
- E2E result with real backend
- full verification matrix
- final git status
- scope confirmation:
  - no backend edits
  - no credential storage/input
  - no fake backend in final E2E
  - no push/tag/release/deploy
  - no AI co-author
