# Frontend Phase 13.8: Resource Archive UI

You are implementing the frontend resource archive UI phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-12-asset-maintenance-ui-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-13-7-archive-based-e2e-cleanup-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-1-resource-archive-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-12-2-resource-archive-lifecycle-worker.md`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources`
- `/Users/fan/JsProjects/ControlHub/services/resources.ts`
- `/Users/fan/JsProjects/ControlHub/types/resource.ts`

## Goal

Backend archive semantics already exist, and frontend E2E cleanup already uses archive. But real product users still cannot:

- archive a resource from the UI
- unarchive a resource from the UI
- inspect archived resources intentionally
- distinguish archived resources cleanly when they are included

This phase adds the minimal product UI for archive lifecycle management while preserving the console style and keeping default operational lists focused.

This is a product UI phase. Keep it restrained, reusable, and consistent with the existing resource console.

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -8
git worktree list
```

Expected:

- worktree path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is dedicated to this phase
- base includes frontend Phase 13.7 on `main`
- backend archive lifecycle contract is available locally
- worktree is clean

Stop and report if the worktree path, branch, or base is wrong.

## Dependency / Merge Order

Backend Phase 12.2 may proceed in parallel, but this frontend phase must not freeze the wrong contract.

Rules:

- You may start from the current backend Phase 12.1 archive contract.
- If backend Phase 12.2 is already available locally, use the full contract:
  - `includeArchived`
  - optional `archivedOnly`
  - `POST /resources/{id}/unarchive`
- If backend Phase 12.2 is not yet available, do not invent unsupported final behavior.
- It is acceptable to implement the archive UI first and leave unarchive / archived-only UI behind a clearly documented capability check if the backend contract is not available yet.
- Do not silently mock missing backend archive lifecycle APIs as final behavior.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Default resource lists stay focused on active resources.
- Archived resources should only appear when the user explicitly asks for them.
- Archive and unarchive actions belong in the existing resource maintenance surfaces, not in a new admin area.
- Reuse the current shell, page header, table, detail sheet, and detail page patterns.
- Keep styling restrained and console-like. No new visual language.
- Do not add hard delete UI.
- Do not add archived-resource bulk actions in this phase.
- Do not add SQL work orders, query execution, topology editing, or audit redesign.
- Use project-local worktree path under `/Users/fan/JsProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Scope

Do exactly this:

1. add archive and unarchive service calls using the backend contract
2. surface archive action in the resource detail page and resource detail sheet
3. add archive filtering to the resources list UI
4. render archived state clearly but quietly in list/detail surfaces
5. refresh list/detail state correctly after archive or unarchive
6. add tests and E2E coverage for the archive lifecycle UI

Do not add a new top-level page.

## Required UI Behavior

### Resource List

Default behavior:

- archived resources remain hidden by default

Add explicit filtering:

- control for including archived resources
- if backend Phase 12.2 is available, support archived-only mode too
- if backend Phase 12.2 is not available, do not fake archived-only mode as final behavior

When archived resources are shown:

- rows remain readable
- archived state is visible with restrained styling
- archived rows should not look identical to active rows

### Resource Detail Page / Sheet

Add:

- archive action for active resources
- unarchive action for archived resources when backend supports it
- visible archive metadata when present:
  - archivedAt
  - archiveReason
  - archivedBy if available

Behavior:

- action buttons reflect loading/submitting state
- success refreshes the current view
- backend 409/404/unavailable states show clear inline feedback
- after archive, default list should no longer surface the resource unless archived filters are enabled

## Data / Service Layer

Update resource types and services as needed for:

- archive metadata fields
- `archiveResource(id, reason?)`
- `unarchiveResource(id)` if backend supports it
- list query params for archive filters

Do not widen types with guessed fields. Stay aligned to backend contract.

## Testing

Follow TDD.

At minimum add or update tests for:

- resource service archive call shape
- unarchive call shape if implemented
- resource list archive filter query params
- detail sheet archive button visible only in the correct state
- detail page archive metadata rendering
- archive action success refreshes or updates visible state
- backend error state is shown clearly
- E2E covers one archive lifecycle flow end-to-end against the real backend

Keep tests independent from exact seed row counts.

## Verification

You must run inside the worktree:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

After the branch is ready, also verify from the main checkout if practical:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

If main-checkout verification is not possible from your session, say so explicitly.

## Manual / Live Verification

Run against a live backend with archive support.

Confirm at minimum:

- archive a resource from the UI
- default `/resources` no longer surfaces it
- resource remains directly inspectable if navigated by ID
- include-archived filter can surface it intentionally
- unarchive restores it to normal list visibility if backend supports unarchive

Do not rely on exact total row counts.

## Tooling Isolation Requirements

Verify and preserve:

- `tsconfig.json` excludes `.worktrees`
- `vitest.config.ts` excludes `.worktrees/**`
- `eslint.config.mjs` ignores `.worktrees/**`
- Playwright test discovery stays local to `e2e`
- `next.config.ts` keeps Turbopack root fixed to the frontend project directory

If any relevant tool still scans `.worktrees/**`, fix it in this phase.

## Pre-Commit Scope Check

Before commit:

```bash
git status --short
git diff --cached --stat
git diff --check --cached
```

Stage explicit files only. Do not stage `.next`, `test-results`, `.worktrees`, logs, screenshots, or temporary files.

## Final Report

Your final report must include:

- worktree path and branch
- commit hash
- changed files
- exact archive / unarchive UI surfaces added
- exact list-filter behavior shipped
- whether unarchive was fully implemented or capability-gated, and why
- verification command results
- live backend verification result
- confirmation that `.worktrees`, `.next`, and `test-results` were not committed
- negative scope confirmation:
  - did not add hard delete UI
  - did not add a new admin section
  - did not add topology editing
  - did not add SQL work orders or query execution
  - did not add frontend-only mocks as final behavior
  - did not tag, push, release, or add AI co-author
- next phase input:
  - any remaining archive lifecycle gaps
  - whether backend 12.2 is still needed for full UX completion
  - any remaining E2E or edge-case gaps

## Constraints

- use a dedicated worktree under `/Users/fan/JsProjects/ControlHub/.worktrees`
- use TDD for changed UI and service behavior
- do not reset the repo
- do not discard unrelated work
- do not add a new visual language
- do not rely on exact seed row counts
- do not let any tool scan `.worktrees/**` from the main checkout
