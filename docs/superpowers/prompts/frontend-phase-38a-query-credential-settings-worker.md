# Frontend Phase 38A Query Credential Settings Worker Prompt

You are implementing the frontend side of Phase 38A for ControlHub.

Frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repository is separate and must not be edited by this worker:

```text
/Users/fan/GolangProjects/ControlHub
```

## Objective

Add an admin-only settings surface for query credential metadata management. The
UI lets an admin configure an opaque credential reference, enabled state, and
environment policy for a selected query target. The Query Workbench must only
display readiness/status and must not expose credential edit controls to normal
query users. The UI must never collect, store, or display the DSN/password.

## Required Reading

Backend docs:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-24-phase-38a-query-credential-metadata-management-design.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-06-24-phase-38a-query-credential-metadata-management.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-24-phase-38a-query-credential-ui-preview.md
```

Frontend code:

```text
app/(console)/settings/page.tsx
app/(console)/settings/
app/(console)/query/page.tsx
components/query/query-workbench.tsx
components/query/query-governance-panel.tsx
components/query/query-editor-shell.tsx
services/query-targets.ts
services/query-executions.ts
types/query-target.ts
messages/en.json
messages/zh-CN.json
e2e/query-workbench.spec.ts
```

## Worktree

Create a frontend worktree after the backend Phase 38A API is merged locally:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-38a-query-credential-settings -b feat/phase-38a-query-credential-settings main
cd .worktrees/frontend-phase-38a-query-credential-settings
git status --short --branch
```

Do not edit the backend repository.

## Backend Dependency

Backend must provide:

```text
GET    /query-targets/{id}/credential
PUT    /query-targets/{id}/credential
DELETE /query-targets/{id}/credential
GET    /query-targets
```

Final E2E must use a real backend plus the Phase 37H dedicated query MySQL
fixture. Do not mock backend responses for final E2E.

## Scope

Allowed:

```text
query credential TypeScript types
query credential service methods
settings/admin credential management UI
read-only Query Workbench credential status
runtime status labels
i18n
unit/component/service tests
query E2E
```

Not allowed:

```text
backend edits
DSN/password input
DSN/password browser state
actorUserId in request body/query
secret write API
secret manager UI
new query engines
export/saved query/approval features
fake backend for final E2E
push/tag/release/deploy
```

## Implementation

Follow frontend Tasks F1 through F3 in:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-06-24-phase-38a-query-credential-metadata-management.md
```

Critical requirements:

- Add service methods:
  - `getQueryCredential(targetResourceId)`
  - `saveQueryCredential(targetResourceId, input)`
  - `deleteQueryCredential(targetResourceId)`
- PUT body may contain only:
  - `credentialRef`
  - `enabled`
  - `environmentPolicy`
  - `confirmAllEnvironments`
- Never send `actorUserId`.
- Never render or retain DSN/password.
- Put credential editing under settings/admin, not inside `/query`.
- Query Workbench may show status and an admin/settings link, but no
  configure/edit/remove form.
- Render localized labels for every backend runtime status.
- Extend known `credentialState` labels for `secret_missing`,
  `binding_mismatch`, and any other backend-emitted value.
- `all_environments` save is disabled until the explicit confirmation checkbox
  is checked.
- After save/delete, refresh credential status and query targets.
- Run remains controlled only by backend `availableActions.run`.
- Locked targets stay locked.
- Support the DBA operating model in copy and layout:
  - standardized read-only account refs are provisioned server-side and reused
    by convention;
  - cluster-specific overrides bind selected targets to different refs.

## Verification

Run from the frontend worktree:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Then run real cross-repo query E2E with backend on `:8080`, frontend proxy/dev
server, and the Phase 37H dedicated query MySQL fixture:

```bash
npm run test:e2e -- --grep query
```

If backend is unavailable, stop and report. Do not claim E2E passed.

## Final Report

Include:

- worktree, branch, commit hashes
- files changed
- service request-shape proof:
  - no `actorUserId`
  - no `dsn`
  - no `password`
  - no `host`
  - no `port`
- UI state summary:
  - settings/admin page location
  - missing metadata
  - configured but secret missing
  - binding mismatch
  - ready
  - delete/remove flow
  - Query Workbench read-only status behavior
- i18n/no raw enum proof
- real backend E2E result
- full frontend verification matrix
- final git status
- scope confirmation:
  - no backend edits
  - no credential input/storage
  - no fake backend in final E2E
  - no push/tag/release/deploy
  - no AI co-author
