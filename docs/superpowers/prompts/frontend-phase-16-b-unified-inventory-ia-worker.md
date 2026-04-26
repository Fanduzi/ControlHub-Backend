# Frontend Phase 16B: Unified Inventory IA And Database Operator Workflow

You are implementing the frontend unified inventory IA for ControlHub Phase 16.

Repository:
`/Users/fan/JsProjects/ControlHub`

This phase exists because `Resources`, `CMDB`, `Databases`, detail pages, and topology currently describe the same inventory through competing mental models. The frontend must consolidate the IA around one inventory model and one database operator workflow.

## Read First

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-25-phase-16-unified-inventory-operator-workflow-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-04-25-phase-16-unified-inventory-operator-workflow.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-04-25-phase-16-inventory-contract-audit.md`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/cmdb/page.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/databases/page.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources/resource-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/databases/database-table.tsx`
- `/Users/fan/JsProjects/ControlHub/components/resources/resource-detail-sheet.tsx`
- `/Users/fan/JsProjects/ControlHub/app/(console)/resources/[id]/page.tsx`
- `/Users/fan/JsProjects/ControlHub/services/resources.ts`
- `/Users/fan/JsProjects/ControlHub/types/resource.ts`

## Startup Check

Before changing files, report:

```bash
pwd
git status --short
git branch --show-current
git log --oneline -10
git worktree list
```

Expected:

- worktree path is under `/Users/fan/JsProjects/ControlHub/.worktrees`
- branch is dedicated to this phase, for example `feat/phase-16b-unified-inventory-ia`
- backend Phase 16A has landed or the final report will explicitly mark unverified backend assumptions
- worktree is clean

Stop and report if path, branch, base, or cleanliness is wrong.

## Dependency Rules

- Do not claim final completion until backend Phase 16A is merged and live backend verification has run.
- You may implement frontend IA scaffolding against documented Phase 16A contract.
- If backend Phase 16A is not available, final report must say exactly which assumptions were not verified.

## Fixed Decisions

- `Resources` is the canonical inventory and CRUD surface.
- `CMDB` must not remain a confusing separate product concept.
- `Databases` remains as a specialized database operator lens.
- `Topology` is secondary and must not become the primary detail workflow in this phase.
- Do not add topology redesign.
- Do not add bulk actions, onboarding, command palette, SQL work orders, or import/export.
- Do not add new dependencies.
- Do not modify backend code.

## Required Product Behavior

### Resources

- Resources page is the primary asset inventory.
- CMDB metadata is available as columns or detail metadata:
  - `externalId`
  - `source`
  - labels
  - archive metadata
  - profile summary where available
- Primary table text must avoid raw enum leaks and UUID-first display.

### CMDB

Choose one implementation and document it in final report:

- preferred: remove CMDB from primary navigation and keep `/cmdb` as redirect/compatibility
- acceptable temporary: make `/cmdb` explicitly a saved inventory metadata view over the same resources

Do not leave CMDB as an unexplained duplicate of Resources.

### Databases

- `/databases` is a database operator tree/table.
- It must use backend `profileSummary` where available.
- Cluster rows show member count or member summary.
- Instance rows show hostname/IP/port where available.
- Cluster detail page or sheet shows a visible member table above topology.
- Members and relations use readable names before UUIDs.

## TDD Requirements

Add failing tests before implementation.

At minimum cover:

- sidebar/navigation does not present CMDB as a competing inventory model
- resources page exposes CMDB metadata through inventory UI
- database table renders backend profile summary fields
- cluster detail renders member table
- relation/member display names appear before UUIDs
- no English/raw enum fallback in Chinese mode for primary labels

## Suggested Task Split

### Task 1: Canonical Inventory Navigation

Files likely touched:

- `components/app-shell/sidebar.tsx`
- `app/(console)/cmdb/page.tsx`
- `messages/en.json`
- `messages/zh-CN.json`
- `tests/components/sidebar.test.tsx`

Verification:

```bash
npx vitest run tests/components/sidebar.test.tsx
npx tsc --noEmit -p tsconfig.json
npm run lint
```

Commit:

```bash
git commit -m "fix: make resources the canonical inventory entry (Phase 16B)"
```

### Task 2: Resources Inventory Metadata

Files likely touched:

- `components/resources/resource-table.tsx`
- `app/(console)/resources/page.tsx`
- `types/resource.ts`
- `services/resources.ts`
- `tests/components/resource-table.test.tsx`

Verification:

```bash
npx vitest run tests/components/resource-table.test.tsx
npm run test
```

Commit:

```bash
git commit -m "feat: expose CMDB metadata in resources inventory (Phase 16B)"
```

### Task 3: Database Operator Workflow

Files likely touched:

- `components/databases/database-table.tsx`
- `app/(console)/databases/page.tsx`
- `components/resources/resource-detail-sheet.tsx`
- `app/(console)/resources/[id]/page.tsx`
- `tests/components/database-table.test.tsx`
- `tests/components/resource-detail-sheet.test.tsx`
- `tests/resource-detail-page.test.tsx`

Verification:

```bash
npx vitest run tests/components/database-table.test.tsx tests/components/resource-detail-sheet.test.tsx tests/resource-detail-page.test.tsx
npm run test
```

Commit:

```bash
git commit -m "feat: add database operator detail workflow (Phase 16B)"
```

### Task 4: Remove Demo-ID Fallbacks

Files likely touched:

- `lib/view-models.ts`
- `lib/resource-copy.ts`
- `tests/lib/view-models.test.ts`
- `tests/lib/resource-summary.test.ts`

Verification:

```bash
npx vitest run tests/lib/view-models.test.ts tests/lib/resource-summary.test.ts
npm run test
```

Commit:

```bash
git commit -m "fix: remove demo-id copy fallbacks from view models (Phase 16B)"
```

## Full Verification

Run after all tasks:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

Live verification against backend Phase 16A:

- `/overview`
- `/resources`
- `/cmdb` or its redirect/compatibility behavior
- `/databases?environment=prod`
- one database cluster detail page
- `/settings`
- `/audits`

Check:

- no unexpected console errors
- no raw enum leaks in primary Chinese labels
- no UUID-first primary member/relation display
- database profile summary fields appear where backend provides them

## Final Report Required

Report:

- worktree path and branch
- commits
- CMDB decision made
- backend contract assumptions verified
- files changed
- tests added
- command results
- live browser pages checked
- screenshots if captured
- known limitations
- `git status --short --branch`
- confirmation:
  - no backend code modified
  - no topology redesign
  - no new dependencies
  - no tag/push/release
  - no AI co-author

