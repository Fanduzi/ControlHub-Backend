# Frontend Phase 36B Query Workbench Shell Worker Prompt

You are implementing the frontend side of Phase 36 for ControlHub.

Frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repository is separate and must not be edited by this worker:

```text
/Users/fan/GolangProjects/ControlHub
```

## Objective

Build the `/query` Query Workbench shell using backend query target data.

This phase does **not** execute queries. The UI must look like a real governed
query workbench, but every execution surface must be locked or disabled.

Do not build a flat inventory-only page. The query target list is supporting
data inside the workbench.

## Required Reading

Backend/docs repo:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-20-query-workbench-roadmap.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-20-phase-36-query-workbench-foundation.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-06-20-phase-36-query-workbench-foundation.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-06-20-phase-36-bytebase-ui-research.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/previews/phase-36-query-workbench-ide/index.html
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-36a-query-targets-worker.md
```

Frontend repo:

```text
app
components
services
types
messages/en.json
messages/zh-CN.json
tests
e2e
package.json
playwright.config.ts
```

## Worktree

Create a frontend worktree:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-36b-query-workbench-shell -b feat/phase-36b-query-workbench-shell main
cd .worktrees/frontend-phase-36b-query-workbench-shell
git status --short --branch
```

Expected: clean branch `feat/phase-36b-query-workbench-shell`.

Do not edit the main worktree directly.

## Backend Dependency

Phase 36B depends on backend Phase 36A:

```text
GET /query-targets
```

If backend Phase 36A is not merged yet, you may scaffold frontend types and UI
against the documented contract, but final verification must use the real
backend endpoint.

Do not add a fake backend for final E2E.

## Scope

Allowed:

```text
/query route
query target service and types
Query Workbench UI shell
i18n
unit/component tests
E2E smoke after backend endpoint exists
```

Not allowed:

```text
query execution
enabled Run / Execute button
SQL execution API calls
Redis command API calls
Mongo query API calls
credential input or storage
export
approval workflow
Admin mode
batch query execution
AI query assistant
backend repo edits
tag / release / deploy
```

## Required UI Direction

Use this preview as the UX reference:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/previews/phase-36-query-workbench-ide/index.html
```

The final implementation should match the existing ControlHub design system, not
copy the static preview literally. Preserve the product structure:

```text
top target switcher
left connection/schema/object browser
center worksheet/editor/result shell
right governance/access panel
```

Required surfaces:

1. Safety banner:

```text
Query execution is not enabled in this phase.
```

2. Target switcher / connection context:

```text
connectionContext.engine
connectionContext.environment
connectionContext.host / connectionContext.port
connectionContext.owner
connectionContext.clusterName
capability.queryKind
capability.editorMode
readiness
```

3. Schema/object browser placeholder:

```text
SQL engines: database / schema / table / column placeholder
Redis: key pattern / read command placeholder
MongoDB: database / collection / field placeholder
```

Use `schemaPreview` from the backend when present. If it is empty, render an
honest locked placeholder. Do not imply live schema sync if the backend does not
provide schema metadata.

4. Worksheet/editor shell:

```text
worksheet tab
saved sheets placeholder
query history placeholder
access grants placeholder
disabled editor content
```

5. Locked action bar:

```text
Run locked
Explain locked
Save sheet placeholder
Export unavailable
```

No enabled Run/Execute action may be present.

6. Locked result area:

```text
Result grid
JSON
Explain
Logs
Masking
0 rows / not executed
```

7. Governance/access panel:

```text
governance.executionEnabled = false
governance.credentialState
governance.auditRequired
governance.safetyState
governance.safetyNote
governance.policyNotes
availableActions.run = false
availableActions.explain = false
availableActions.export = false
availableActions.saveSheet = false
availableActions.requestAccess = false
JIT/access planned
production policy notes
missing fields
```

## Copy Requirements

Avoid implying execution works.

Good:

```text
Run locked
Query execution is not enabled
Read-only credentials are required
Result grid is locked
Access request planned
```

Bad:

```text
Run
Execute
Connected
Ready
Export
Admin mode
```

Only show `Ready` if backend returns explicit readiness evidence.

## Service And Types

Add frontend types matching backend OpenAPI:

```text
QueryTarget
QueryTargetConnectionContext
QueryTargetCapability
QueryTargetGovernance
QueryTargetAvailableActions
QueryTargetSchemaPreviewNode
QueryKind
QueryTargetReadiness
QueryTargetSafetyState
QueryTargetListResponse
```

The frontend must consume the nested backend contract:

```text
connectionContext -> target switcher and facts
capability -> editor mode and language label
governance -> safety/access panel
availableActions -> locked action bar
schemaPreview -> schema/object browser placeholder
```

Do not reconstruct these fields from hardcoded frontend assumptions if the
backend returns them.

Add service:

```text
getQueryTargets()
```

Do not add:

```text
executeQuery()
runSql()
runRedisCommand()
runMongoQuery()
exportResult()
```

## Tests

Add tests for:

```text
query target service
/query route renders
target switcher/list renders backend targets
filters/search work
locked Run / Explain / Export states
no enabled Run/Execute button exists
schema placeholder renders
result placeholder renders
history/access placeholders render
i18n copy is localized
```

E2E after backend endpoint exists:

```text
/query loads with real backend data
target switcher has at least one database instance
execution disabled banner is visible
no enabled Run/Execute button exists
row/target selection updates governance panel
```

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

If backend Phase 36A is merged and available, also run the relevant E2E smoke or
manual cross-repo E2E:

```bash
npm run test:e2e -- --grep query
```

or trigger the existing manual cross-repo workflow after merge if instructed.

## Commit

Commit only after verification is complete.

Commit message suggestion:

```text
feat: add query workbench shell
```

Do not include `Co-Authored-By`.

Do not push, tag, release, or deploy unless explicitly instructed.

## Final Report Requirements

Report:

```text
worktree / branch / commit
files changed
API contract consumed
UI surfaces implemented
locked execution proof
tests added
verification matrix
live/browser evidence if run
scope confirmation
```

Scope confirmation must include:

```text
no query execution
no enabled Run/Execute button
no credentials in browser
no SQL/Redis/Mongo execution APIs
no export
no backend repo changes
no mocked backend in final E2E
tests added
no tag / release / deploy / push
no AI co-author
```
