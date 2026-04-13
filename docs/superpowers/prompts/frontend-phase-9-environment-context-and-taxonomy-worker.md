# Frontend Phase 9: Environment Context and Taxonomy Integration

You are implementing the next frontend phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

## Goal

Turn the topbar environment selector into a real global environment context and start consuming backend taxonomy dictionaries instead of relying on frontend-only hardcoded values.

This is still foundation work for the resource console. Do not expand into SQL work orders, topology UI, asset edit flows, advanced permissions, or other upper-layer features.

## Current Backend Contract

The backend already provides:

- `GET /environments`
- `GET /resource-types`
- `GET /relation-types`
- `GET /resources`
- `GET /resources/{id}`
- `GET /resources/{id}/profile`
- `GET /resources/{id}/relations`
- `GET /resources/{id}/audit-events`

The OpenAPI contract uses camelCase.

## Current Frontend Gaps

1. The topbar environment selector is still a static shell:
   - it reads hardcoded `environmentOptions`
   - it does not persist a real environment context
   - it does not influence data on core pages

2. Frontend resource taxonomy is stale:
   - `types/resource.ts` still only knows 4 resource types
   - backend now supports:
     - `host`
     - `database_instance`
     - `database_cluster`
     - `service`
     - `domain_name`
     - `virtual_ip`
     - `database_proxy`
     - `control_plane_component`

3. Frontend is not yet consuming:
   - `GET /resource-types`
   - `GET /relation-types`

## Scope

Do exactly these things:

1. Make the topbar environment selector real.
2. Persist the current environment context.
3. Apply the current environment context to core console pages.
4. Consume backend taxonomy dictionaries where the frontend currently hardcodes resource/relation vocabulary.
5. Align frontend types with backend taxonomy support.

Do not do more.

## Requirements

### 1. Global Environment Context

Replace the current static topbar environment selector with a real environment context switcher.

Requirements:

- Load options from `GET /environments`
- Persist the selected environment id using a clear key such as:
  - `controlhub.environmentId`
- Restore the previously selected environment on refresh
- Keep the control compact and visually aligned with:
  - language switcher
  - theme toggle
  - accent switcher

The selector should represent **global console context**, not a page-local filter.

### 2. Apply Environment Context To Core Pages

At minimum, the current environment context must affect:

- `/overview`
- `/resources`
- `/cmdb`
- `/databases`

Acceptable first implementation:

- fetch the current page data as today
- apply frontend-side filtering by `environmentId`
- keep the code structured so later it can move to backend query-param filtering cleanly

Do not force URL query params for this phase.

If no environment is selected, preserve current behavior.

### 3. Taxonomy Integration

Start consuming backend dictionaries:

- `GET /resource-types`
- `GET /relation-types`

Use them for the places where frontend currently hardcodes resource/relation vocabulary.

Examples of acceptable usage in this phase:

- resource type filter options
- resource/relation label display support
- settings/dictionary surfaces that currently duplicate static resource-type assumptions

Do not over-engineer a full registry framework.

### 4. Align Frontend Types

Update frontend types to match backend-supported resource families.

At minimum, `ResourceType` should include:

- `host`
- `database_instance`
- `database_cluster`
- `service`
- `domain_name`
- `virtual_ip`
- `database_proxy`
- `control_plane_component`

Do not invent types not present in backend taxonomy.

### 5. UX Constraints

- Keep the UI restrained and console-like
- Do not turn the topbar into a settings form
- Do not add verbose banners or onboarding text
- If global environment context changes page data, keep it subtle
- Avoid duplicate controls between topbar and page-local filters

## Suggested Files To Inspect

- `components/app-shell/topbar.tsx`
- `services/settings.ts`
- `types/resource.ts`
- `types/settings.ts`
- `lib/navigation.ts`
- `lib/view-models.ts`
- `app/(console)/overview/page.tsx`
- `app/(console)/resources/page.tsx`
- `app/(console)/cmdb/page.tsx`
- `app/(console)/databases/page.tsx`

## Verification

You must run:

```bash
npm run lint
npm run build
npx vitest run
```

You must manually verify:

1. topbar environment options come from the backend
2. selecting an environment persists across refresh
3. `/overview` changes with environment context
4. `/resources` changes with environment context
5. `/cmdb` changes with environment context
6. `/databases` changes with environment context
7. language/theme/accent controls still align visually with the environment selector

## Final Report

Your final report must include:

- changed files
- persistence key used for environment context
- whether filtering is frontend-side or backend-side
- which pages now honor the global environment context
- where `resource-types` and `relation-types` are now consumed
- the updated `ResourceType` coverage
- `npm run lint` result
- `npm run build` result
- `npx vitest run` result
- commit hash
- remaining risks

## Constraints

- do not reset the repo
- do not discard unrelated work
- do not change backend contracts
- do not widen scope beyond environment context and taxonomy consumption
