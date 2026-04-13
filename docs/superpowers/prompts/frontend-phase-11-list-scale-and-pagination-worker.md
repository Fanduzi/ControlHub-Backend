# Frontend Phase 11: List Scale And Pagination

You are implementing the next frontend phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-10-minimal-e2e-worker.md`

## Goal

Richer demo data now exposes list-scale issues:

- no pagination
- too many rows on one page
- dense tables need stronger control layout
- local-only filtering does not scale

This phase upgrades the core list pages to handle larger data volumes cleanly.

## Scope

Do exactly this:

1. integrate backend pagination/filtering on list pages
2. add practical pagination UI
3. keep the console look dense and restrained
4. expand E2E coverage for the scaled list behavior

Do not widen into asset editing, topology UI, SQL work orders, or major visual redesign.

## Pages In Scope

- `/resources`
- `/cmdb`
- `/databases`
- `/audits`

## Expected Backend Support

This phase expects backend support for paginated list endpoints, especially:

- `GET /resources`
- `GET /audit-events`

If backend pagination is not ready yet, stop and report the contract gap instead of hardcoding a fake local pagination solution as the final implementation.

Temporary local pagination is acceptable only if clearly marked as transitional and tightly scoped.

## Requirements

### 1. Resources / CMDB / Databases

Use backend list filtering and pagination instead of fetching everything and filtering entirely on the client.

At minimum support:

- page change
- page size change
- environment context integration
- resource type filtering where relevant
- search where already present

`/cmdb` and `/databases` may share underlying resource list mechanics, but their page-specific presentation should remain distinct.

### 2. Audits

Add pagination to `/audits`.

Do not leave the audit page as an unbounded long table once seed data grows.

### 3. Pagination UI

Keep it compact and console-like.

Required controls:

- current page
- next / previous
- visible total count or page count

Optional:

- page size selector

Do not build an over-designed data-grid toolbar.

### 4. Environment Context

Preserve the topbar global environment context introduced in phase 9.

Where practical, it should feed backend filters instead of client-only post-filtering.

### 5. E2E Expansion

Extend the current E2E suite to cover at least:

- `/resources` pagination or filtered navigation
- `/audits` rendering with larger data
- environment context affecting a list page

Keep the suite small and stable.

## UX Constraints

- do not add giant pagers or admin-template controls
- do not add card views
- tables remain the primary representation
- controls should fit the existing monochrome console language

## Suggested Files To Inspect

- `app/(console)/resources/page.tsx`
- `app/(console)/cmdb/page.tsx`
- `app/(console)/databases/page.tsx`
- `app/(console)/audits/page.tsx`
- `components/resources/resource-table.tsx`
- `components/databases/database-table.tsx`
- `components/audits/audit-table.tsx`
- `components/blocks/data-table-shell.tsx`
- `services/resources.ts`
- `services/audits.ts`
- `components/providers/environment-provider.tsx`

## Verification

You must run:

```bash
npm run lint
npm run build
npx vitest run
npm run test:e2e
```

You must also manually verify, with richer seed data:

- `/resources`
- `/cmdb`
- `/databases`
- `/audits`

Specifically verify:

- pagination works
- tables remain readable with more rows
- environment context still behaves coherently
- no obvious overflow or density regressions

## Final Report

Your final report must include:

- changed files
- whether pagination is backend-driven or temporarily frontend-driven
- which pages now paginate
- how environment context integrates with list filtering
- E2E additions
- lint/build/vitest/e2e results
- commit hash
- remaining risks

## Constraints

- use a dedicated worktree unless blocked
- do not reset the repo
- do not discard unrelated work
- do not widen scope beyond list scaling and pagination
