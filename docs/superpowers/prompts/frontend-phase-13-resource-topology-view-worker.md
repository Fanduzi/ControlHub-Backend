# Frontend Phase 13: Resource Topology View

You are implementing the frontend topology view phase for ControlHub.

Repository:
`/Users/fan/JsProjects/ControlHub`

Read first:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-11-unified-resource-console-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-04-13-engineering-quality-gates-design.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-12-asset-maintenance-ui-worker.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-11-resource-topology-read-model-worker.md`

## Goal

ControlHub now supports asset CRUD and relation maintenance. The next frontend foundation is a read-only resource topology view that makes existing relations understandable.

This phase adds topology visualization using the backend read model.

Do not build topology editing.
Do not replace the relation maintenance form.
Do not implement SQL work orders, SQL query, or discovery ingestion.

## Fixed Decisions

These decisions are already made. Do not ask the user to choose alternatives before implementation.

- Consume `GET /resources/{id}/topology`.
- Add topology to resource detail surfaces.
- Initial topology is read-only.
- Use a graph library that is stable in React/Next.js. Prefer React Flow for the first implementation.
- Do not use D3 directly for this first topology phase unless React Flow is clearly blocked.
- Do not persist graph layout.
- Do not use frontend-fabricated topology data as final behavior.
- Use project-local worktree path under `/Users/fan/JsProjects/ControlHub/.worktrees`.
- Do not re-run broad brainstorming or present A/B/C options. This prompt is the implementation assignment.

## Backend Contract Dependency

This phase expects backend Phase 11:

```text
GET /resources/{id}/topology
```

Expected response:

- `rootResourceId`
- `depth`
- `direction`
- `nodes[]`
- `edges[]`
- optional `groups[]`

If the backend endpoint is not available yet:

- implement service types and UI skeleton only if useful
- show a clear unavailable state
- do not fake a successful final topology
- do not claim live integration complete

## Scope

Do exactly this:

1. add topology service function and types
2. add a reusable topology panel component
3. integrate topology into resource detail page
4. integrate topology into resource detail sheet if layout allows without clutter
5. add tests and E2E coverage

Keep the UI dense, readable, and consistent with the console. This is an operations workbench, not a decorative graph demo.

## Placement

Required:

- `/resources/[id]` must show topology in a dedicated section or tab.

Preferred if clean:

- resource detail sheet can show a compact topology preview or a link/action to the full detail topology.

Do not make the sheet unusably tall or noisy.

## UI Requirements

Topology panel must support:

- loading state
- empty/no-relations state
- backend error state
- depth selector: `1` / `2`
- direction selector: `both` / `upstream` / `downstream`
- optional relation type filter if backend supports it cleanly
- root node visually distinguished
- node labels using display name or name
- node metadata: resource type, health status
- edge labels using relation type
- clicking a node opens or navigates to that resource detail

Visual constraints:

- use the existing monochrome console language
- semantic health colors remain fixed
- do not introduce large gradients or decorative graph styling
- keep graph readable on light and dark mode
- handle small screens with a fallback stacked/list view or constrained scroll area

## Data Mapping

Add wire types matching backend response.

Map backend topology to graph nodes/edges in a separate helper:

- keep service wire types separate from React Flow types
- include stable ids
- keep node/edge ordering deterministic where possible
- avoid expensive recomputation loops

Watch for the prior TanStack/Base UI lesson:

- memoize derived arrays passed into graph/table libraries
- keep callback references stable when used by graph/dialog libraries
- do not introduce infinite render loops

## Testing

Follow TDD.

At minimum add/update tests covering:

- service calls `GET /resources/{id}/topology` with default params
- service includes `depth`, `direction`, and relation type params correctly
- topology mapper converts nodes/edges correctly
- root node is marked visually or semantically
- empty topology renders useful empty state
- backend failure renders useful error state
- depth/direction controls update request state
- node click navigates to or opens the target resource

## E2E Requirements

Extend Playwright coverage.

Required flow:

1. login
2. open a seeded resource detail page with known relations
3. verify topology section renders
4. verify at least one node and one edge are visible
5. change depth to `2`
6. verify topology reloads or updates
7. click a related node and verify navigation or detail behavior

Do not depend on exact total node count if seed data may change. Assert stable labels/types from current seed data where practical.

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

- `/resources/[id]` topology section
- depth selector
- direction selector
- empty topology behavior
- dark mode
- narrow viewport behavior

## Final Report

Your final report must include:

- changed files
- graph library used and why
- backend endpoint consumed
- final query params
- pages/components changed
- unit/vitest results
- build result
- E2E result
- live verification result
- commit hash
- remaining risks

## Constraints

- use a dedicated worktree under `/Users/fan/JsProjects/ControlHub/.worktrees`
- use TDD
- do not reset the repo
- do not discard unrelated work
- do not add topology editing
- do not add layout persistence
- do not replace relation maintenance UI
- do not implement SQL work orders or query execution
