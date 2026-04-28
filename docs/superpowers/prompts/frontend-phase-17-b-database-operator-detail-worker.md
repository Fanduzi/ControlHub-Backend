# Frontend Phase 17B: Database Operator Detail UX

You are implementing Frontend Phase 17B for ControlHub: database cluster/instance read-only operator detail workflow.

Repository:
`/Users/fan/JsProjects/ControlHub`

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-27-phase-17-database-operator-drilldown-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-27-phase-17-database-operator-drilldown.md`
- `/Users/fan/JsProjects/ControlHub/package.json`
- `/Users/fan/JsProjects/ControlHub/types/resource.ts`
- `/Users/fan/JsProjects/ControlHub/services/resources.ts`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`
- `/Users/fan/JsProjects/ControlHub/components/blocks/cluster-members-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/blocks/resource-relation-panel.tsx`

## Startup Check

Create a dedicated worktree from frontend main after Backend 17A is merged:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree add .worktrees/frontend-phase-17b-database-operator-detail -b feat/phase-17b-database-operator-detail main
cd .worktrees/frontend-phase-17b-database-operator-detail
git status --short --branch
git log --oneline -8
```

Expected:

- path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is `feat/phase-17b-database-operator-detail`
- worktree is clean
- frontend main includes Phase 16C and the database icon size fix

Stop and report if any condition is false.

## Backend Dependency

This phase depends on Backend Phase 17A.

Before coding, verify the backend contract exists:

- `GET /resources/{id}` includes `profileSummary` where supported.
- `GET /resources/{id}/relations` includes readable related resource fields.
- `GET /resources/{id}/members` exists, unless Backend 17A documented an explicit alternative.

If Backend 17A is not merged or endpoints are absent, stop and report. Do not fake the contract in frontend.

## Exact Scope

Allowed:

- Update frontend types and services to consume Backend 17A.
- Add database-specific operator sections to resource detail page.
- Improve relation/member display names.
- Add tests.

Not allowed:

- backend changes
- SQL execution
- work orders
- topology editing
- CMDB navigation restoration
- demo `resourceSummaries` restoration
- broad UI redesign outside database detail workflow

## Required UX

### Database Cluster Detail

Must show:

- identity and ownership
- environment and owner
- health/lifecycle status
- operator summary with node count/engine/version if present
- member instances table
- readable relation names
- topology section still present
- audit context still present if already available

### Database Instance Detail

Must show:

- parent cluster card with link
- hostname/ip/port if present
- engine/version/role if present
- readable relation names
- topology section still present
- audit context still present if already available

Empty states must be concise and honest:

- "Profile data not provided" is acceptable.
- Do not show large empty panels.
- Do not show raw IDs as primary text when display names exist.

## TDD Requirements

Write tests before implementation:

- service parses profileSummary fields
- service parses cluster members
- service parses readable relation fields
- cluster detail page renders members table with links
- instance detail page renders parent cluster and connection fields
- no demo `resourceSummaries` text appears

## Verification

Run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

If backend is running:

```bash
npm run test:e2e:smoke
```

Do not claim E2E passed unless it was run.

## Live Browser Checks

With backend running, manually or with Playwright verify:

- `/resources`
- one database cluster detail page
- one database instance detail page
- `/databases`

Confirm:

- readable relation/member names
- profile fields visible when backend returns them
- no console warnings/errors
- no oversized database icons
- no `/cmdb` nav restoration

## Commit

Commit after checks pass:

```bash
git add app components lib services types tests messages
git commit -m "feat: add database operator detail UX (Phase 17B)"
```

Only add files you changed.

No AI co-author. No tag. No push. No release.

## Final Report

Return:

1. Worktree path, branch, commit hash.
2. Backend Phase 17A commit/hash assumed.
3. Changed files table.
4. Exact frontend fields consumed from Backend 17A.
5. Cluster detail behavior.
6. Instance detail behavior.
7. Verification matrix.
8. Live browser evidence.
9. Confirmation:
   - no backend changes
   - no SQL execution/work orders
   - no topology editing
   - no CMDB nav restore
   - no demo resourceSummaries restore
   - no tag/push/release
   - no AI co-author
   - clean `git status`

