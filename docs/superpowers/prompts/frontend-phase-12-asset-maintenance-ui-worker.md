# Frontend Phase 12: Asset Maintenance UI

You are implementing the next frontend phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-11-list-scale-and-pagination-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-10-asset-write-and-relation-maintenance-worker.md`

## Goal

ControlHub can now browse, filter, paginate, inspect, and test asset data. The next product foundation is manual asset maintenance:

- create resources
- edit resource base metadata
- add and remove resource relations

This is still the CMDB/asset foundation. Do not build SQL work orders, SQL query, discovery ingestion, or topology graph rendering in this phase.

## Backend Contract Dependency

This phase expects backend Phase 10 write APIs:

- `POST /resources`
- `PATCH /resources/{id}`
- `POST /resources/{id}/relations`
- relation delete endpoint chosen by backend, expected as either:
  - `DELETE /resource-relations/{id}`
  - or `DELETE /resources/{resourceId}/relations/{relationId}`

If backend Phase 10 is not available yet, you may implement UI structure behind service functions, but do not fake a final success path. Stop and report the contract gap before claiming integration is complete.

Do not use frontend-only mutation mocks as final behavior.

## Scope

Do exactly this:

1. add a create-resource flow
2. add an edit-resource-metadata flow
3. add relation create/delete controls
4. wire all mutations through the service layer
5. add unit/integration tests and E2E coverage

Keep the UI restrained and console-like. This is not a marketing form or an admin-template CRUD page.

## Pages And Components In Scope

Primary pages:

- `/resources`
- `/resources/[id]`

Allowed shared components:

- resource detail sheet
- relation panel
- data table shell actions
- settings/dictionary helpers if needed

Do not redesign unrelated pages.

## UX Requirements

### 1. Create Resource

Add a compact "New resource" entry point on `/resources`.

Recommended pattern:

- button in the list page header or table shell toolbar
- opens a side sheet or dialog
- fields are grouped into small sections:
  - identity
  - ownership/environment
  - status
  - labels

Required fields:

- resource type
- name
- display name
- environment
- owner
- lifecycle status
- health status
- source

Optional fields:

- resource subtype
- external id
- labels

All select options must come from backend endpoints:

- `GET /resource-types`
- `GET /environments`
- `GET /owners`
- `GET /lifecycle-statuses`
- `GET /health-statuses`

Do not hardcode these options except as explicit fallback for backend-unavailable rendering.

### 2. Edit Resource Metadata

Add an edit entry point in:

- resource detail sheet
- full resource detail page

Allow editing only backend-supported mutable fields:

- `resourceSubtype`
- `displayName`
- `environmentId`
- `ownerId`
- `lifecycleStatus`
- `healthStatus`
- `source`
- `externalId`
- `labels`

Do not offer editing for immutable fields:

- `id`
- `resourceType`
- `name`
- `createdAt`

After success:

- refresh the current detail view
- refresh or invalidate the current list page
- keep the user on the same page

### 3. Relation Maintenance

In the existing relation panel:

- add "Add relation"
- add remove action for each existing relation

Add relation fields:

- target resource search/select
- relation type

Use backend data:

- relation types from `GET /relation-types`
- target resources from `GET /resources` with search and pagination where practical

Keep relation creation scoped. Do not build a graph editor.

### 4. Error And Loading States

Handle:

- backend unavailable
- validation errors
- duplicate resource conflicts
- duplicate relation conflicts
- relation target not found
- unauthorized response if current auth token is invalid

Errors should be readable to an operator. Do not expose raw stack traces or opaque "failed" text.

### 5. i18n

All user-facing strings must be covered in:

- `messages/zh-CN.json`
- `messages/en.json`

Do not add hardcoded English-only form labels.

### 6. Theme And Layout

The new UI must work with:

- light mode
- dark mode
- all existing accent colors
- the current resizable sheet behavior

Avoid introducing a second form visual language. Reuse existing shadcn/base components consistently.

## Service Layer Requirements

Add or update service functions for:

- `createResource(input)`
- `updateResource(id, input)`
- `createResourceRelation(resourceId, input)`
- `deleteResourceRelation(...)`
- `listLifecycleStatuses()` if not already present
- `listHealthStatuses()` if not already present

Service functions should use the backend camelCase wire contract. Keep view-model enrichment separate from wire types.

## Testing

Follow TDD.

At minimum add/update tests covering:

- create resource form renders dictionary-driven options
- create resource submits correct `POST /resources` payload
- create resource displays backend validation error
- edit resource form excludes immutable fields
- edit resource submits correct `PATCH /resources/{id}` payload
- relation add submits correct relation payload
- relation delete calls the backend delete endpoint
- successful mutations trigger refresh/invalidation behavior

Do not rely only on snapshots.

## E2E Requirements

Extend Playwright coverage.

Required E2E flows:

1. login
2. navigate to `/resources`
3. create a manual resource with a unique test name
4. search/filter until the new resource is visible
5. open the resource detail sheet
6. edit the display name or health status
7. add a relation to an existing resource
8. remove that relation

If tests mutate the shared local database, use deterministic test names with a prefix such as:

`e2e-resource-<timestamp-or-random-suffix>`

Do not make E2E depend on exact row counts from seed data.

## Verification

You must run:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run test:e2e
```

Also manually verify with a live backend:

- `/resources`
- resource detail sheet
- `/resources/[id]`
- create resource
- edit resource
- add/delete relation

## Final Report

Your final report must include:

- changed files
- backend endpoints consumed
- final mutation payload examples
- pages/components changed
- i18n coverage notes
- unit/vitest results
- build result
- E2E result
- manual live verification result
- commit hash
- remaining risks

## Constraints

- use a dedicated worktree unless blocked
- use TDD
- do not reset the repo
- do not discard unrelated work
- do not widen scope beyond manual asset maintenance
- do not implement SQL work orders, topology graph rendering, discovery ingestion, or batch import
