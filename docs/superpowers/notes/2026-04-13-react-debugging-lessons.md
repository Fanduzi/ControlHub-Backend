# 2026-04-13 React Debugging Lessons

## Context

This note captures a real debugging miss from the ControlHub frontend during resource-detail sheet integration.

Observed symptom:

- `/resources` clicked into a frozen state after opening a resource
- the page became non-interactive
- `/databases` could still open the same sheet pattern correctly

Actual root cause:

- unstable `filteredResources` array identity in `ResourceTable`
- unstable inline `onOpenChange` callback identity passed into the sheet loader
- together they created a positive re-render loop across React, TanStack Table, and Base UI dialog store synchronization

This was not primarily a portal, overlay, or CORS problem.

## What Went Wrong

The investigation spent too long on visible UI symptoms:

- overlay behavior
- sheet portal layering
- Base UI close button behavior
- browser extension interference

Those were secondary signals. The correct comparison should have started with:

- bad page: `/resources`
- good page: `/databases`

Because one page failed and another page worked with the same sheet concept, the first diagnostic step should have been a React data-flow comparison instead of modal/overlay speculation.

## Required Debugging Order For Similar Bugs

When a React page freezes, locks, or appears to open a dialog without rendering it:

1. Find the closest working comparison page.
2. Compare state shape and render inputs before touching UI primitives.
3. Check unstable references first:
   - filtered arrays
   - mapped objects
   - inline callbacks
   - derived props rebuilt on every render
4. If TanStack Table, Base UI, Radix, or any store-backed primitive is involved, assume identity churn may be amplified by internal sync effects.
5. Only after that inspect:
   - portal structure
   - overlay layering
   - focus trap behavior
   - browser extensions

## Frontend Heuristics To Apply Early

- If a page uses `filter`, `map`, or `sort` before passing data into a table or store-backed component, check whether `useMemo` is needed.
- If a dialog, sheet, menu, or select receives callbacks from a parent render path, check whether `useCallback` is needed.
- If one route works and another route fails with the same primitive, compare the route-specific state and props first.
- Distinguish root cause from cleanup fixes. A valid cleanup fix is not proof that the main bug is solved.

## Minimal Diagnostics To Prefer

Before patching primitives, add or inspect the smallest possible signals:

- render count
- whether `data` prop identity changes every render
- whether callback identities change every render
- whether opening the component starts a render loop

This is usually higher signal than staring at the browser surface.

## Decision

Do not create a standalone skill yet.

Reason:

- this lesson is valuable, but still fits better as a debugging note and future checklist input
- a standalone skill should wait until there are multiple repeated cases with the same workflow boundary

If similar failures repeat across React + table/store-backed UI integrations, revisit and consider a dedicated skill later.
