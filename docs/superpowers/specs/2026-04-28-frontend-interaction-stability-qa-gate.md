# Frontend Interaction Stability QA Gate Design

## Background

The frontend recently regressed in ways that ordinary unit tests and single-page smoke tests did not catch:

- Database engine filter selection left the menu open and made the page feel frozen.
- Opening a database row sheet worked, but closing it by clicking blank space left follow-up interactions broken.
- Closing a sheet and clicking a resource link could hang.
- Navigating from a table to a resource detail page and then using browser Back could restore a stale page with a default blue accent and detached interaction handlers.
- Database table visual density regressed because the expander column and database icon column were too far apart.

The root pattern is not one bad component. The missing control is a browser-level interaction-chain gate that verifies table controls, sheets, links, browser history restoration, accent state, and modal cleanup together.

## Goal

Add a mandatory browser QA gate for operator table interaction stability across `/resources` and `/databases`.

The gate must prove that core table interactions still work after:

- filter changes
- sheet open and close
- resource detail navigation
- browser back/forward restoration
- repeated interaction after returning

## Non-Goals

- Do not redesign resource or database tables.
- Do not change backend APIs.
- Do not add mock-only UI behavior.
- Do not replace existing smoke E2E tests.
- Do not broaden this into full product walkthrough coverage.
- Do not suppress browser console errors or warnings.

## User-Critical Flows

### Flow A: Resources Table History Recovery

1. Login with real backend authentication.
2. Visit `/resources?environment=prod&page=1`.
3. Set accent to `purple`.
4. Click the first resource name link in the table.
5. Confirm navigation to `/resources/:id`.
6. Use browser Back.
7. Confirm the restored list still has `data-accent="purple"`.
8. Confirm there is no modal residue: no `[role="dialog"]`, no `[data-slot="sheet-overlay"]`, no `[inert]`.
9. Click a table row and confirm the detail sheet opens.
10. Close the sheet by clicking blank space and confirm it is removed.
11. Open a multi-select filter and confirm the menu opens.

### Flow B: Resources Sheet-to-Detail History Recovery

1. Login with real backend authentication.
2. Visit `/resources?environment=prod&page=1`.
3. Set accent to `purple`.
4. Click a table row to open the resource detail sheet.
5. Click `Open full detail` / `打开完整详情`.
6. Confirm navigation to `/resources/:id`.
7. Use browser Back.
8. Confirm accent, modal cleanup, row click, blank-close, and filter menu still work.

### Flow C: Databases Table Filter, Sheet, Link, Back

1. Login with real backend authentication.
2. Visit `/databases?environment=prod&page=1`.
3. Set accent to `purple`.
4. Open the engine multi-select filter and choose `mysql`.
5. Confirm URL uses `resourceSubtype=mysql` and the menu closes.
6. Click a database row to open the sheet.
7. Close the sheet by clicking blank space.
8. Click the first database resource name link.
9. Confirm navigation to `/resources/:id`.
10. Use browser Back.
11. Confirm accent, modal cleanup, row click, blank-close, and filter menu still work.

## Required Invariants

Every interaction-chain test must assert all of the following:

- `document.documentElement.dataset.accent === "purple"`
- CSS variable `--primary` is not the default blue value after history restoration
- `[role="dialog"]` count is zero after blank-close
- `[data-slot="sheet-overlay"]` count is zero after blank-close
- `[inert]` count is zero after blank-close/history restoration
- a row click opens a sheet after history restoration
- a dropdown/multi-select opens after history restoration
- console errors and unexpected warnings are empty
- network 4xx/5xx responses are empty

## Test Placement

Create a new E2E spec instead of bloating `operator-console-smoke.spec.ts`:

- New helper: `e2e/harness/interaction-stability.ts`
- New spec: `e2e/operator-interaction-stability.spec.ts`
- New npm script: `test:e2e:interaction`

This keeps the existing smoke suite fast and lets interaction-stability checks be run explicitly after changes to:

- `components/ui/sheet.tsx`
- `components/ui/dropdown-menu.tsx`
- `components/blocks/multi-select-filter.tsx`
- `components/blocks/resource-link.tsx`
- `components/resources/resource-table.tsx`
- `components/databases/database-table.tsx`
- `app/layout.tsx`
- navigation, theme, accent, or provider code

## Acceptance Criteria

- `npm run test:e2e:interaction` passes with real backend on `:8080`.
- `npm run test:e2e:smoke` still passes.
- `npm run test:e2e` still passes.
- The interaction test fails if accent is lost after browser Back.
- The interaction test fails if sheet overlay, dialog, or inert state remains after close.
- The interaction test fails if row click or dropdown click stops working after browser Back.
- No broad console-warning suppression is introduced.

