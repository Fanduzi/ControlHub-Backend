# Frontend Phase 38D Query Admin Navigation Worker Prompt

You are implementing the frontend side of Phase 38D for ControlHub.

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repo is separate. Do not edit backend code in this frontend task unless
the prompt is explicitly expanded.

## Objective

Fix the remaining Query Workbench and Query Credential admin usability issues:

- admin direct navigation to `/settings/query-credentials` must not show a false
  non-admin view;
- `/settings` must expose a Query Credential settings entry;
- `/query` target selection must be consolidated so target search/filter/facts do
  not consume multiple large page regions.

## Required Reading

```text
/Users/fan/GolangProjects/ControlHub/docs/decisions/2026-07-05-phase-38d-query-workbench-admin-followups.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-05-phase-38d-query-workbench-admin-followups.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-07-05-phase-38d-query-workbench-admin-followups.md
```

Frontend files to inspect:

```text
app/login/page.tsx
app/(console)/settings/page.tsx
app/(console)/settings/query-credentials/page.tsx
components/settings/query-credential-settings.tsx
components/query/query-workbench.tsx
components/query/query-governance-panel.tsx
components/app-shell/command-palette.tsx
lib/navigation.ts
services/auth.ts
services/api-client.ts
messages/en.json
messages/zh-CN.json
tests/components/query-credential-settings.test.tsx
tests/components/query-workbench.test.tsx
e2e/query-credential-settings.spec.ts
e2e/query-workbench.spec.ts
```

## Rules

- Do not add credential edit controls to `/query`.
- Do not add DSN/password inputs or browser state.
- Do not send `actorUserId`.
- Do not fake admin role in the browser.
- Do not fake backend for final E2E.
- Do not push/tag/release/deploy.
- Do not add AI co-author.

## Tasks

### F1. Admin Role Recovery

Fix `/settings/query-credentials` so direct URL loads work for admin users.

Requirements:

- Hydration-safe initial render.
- If `sessionStorage["controlhub.role"]` exists, use it.
- If role is missing but the user is authenticated, recover the role from the
  current session using existing frontend/backend auth capability.
- If no backend endpoint exists to recover role, stop and report the backend gap;
  do not invent admin status client-side.
- Do not show the final non-admin restricted state while role recovery is still
  pending.
- Non-admin users still never see credential form fields or save/delete actions.

Tests:

- admin direct URL load/refresh shows management UI after role recovery;
- missing role while recovery is pending shows loading/unknown state;
- viewer/non-admin shows restricted view;
- non-admin never calls save/delete and never sees edit controls;
- hydration-safe first render.

### F2. Settings Page Entry

Add a Query Credential settings entry to `/settings`.

Requirements:

- Card or row title: Query credential settings / 查询凭据设置.
- Description explains metadata references only; DSN/password stay server-side.
- Link to `/settings/query-credentials`.
- Admin action: Open credential settings.
- Non-admin copy: Managed by administrators, no edit controls.
- Add i18n keys in English and Chinese.

Optional:

- Add command palette entry if it fits existing IA and can be tested safely.

Tests:

- `/settings` renders the entry;
- clicking it navigates to `/settings/query-credentials`;
- i18n labels render in en and zh-CN;
- non-admin does not see credential edit controls from the settings entry.

### F3. Query Target Area Consolidation

Remove the split target-selection experience.

Current problem:

```text
target picker
separate search/filter row
large selected-target facts block
```

Target state:

- one primary target picker/search area;
- picker search covers name, resource name, engine, env, host, port, cluster,
  readiness;
- engine/query-kind/readiness filters move into picker chips or advanced filter
  popover;
- selected target summary is compact chips only;
- owner/language/cluster/details live in disclosure/tooltip/popover;
- governance panel does not duplicate target facts.

Tests:

- no separate always-visible filter row below the picker;
- selected target summary shows compact engine/env/readiness chips;
- details are available but not always expanded;
- picker search still matches host and engine;
- ready target remains easy to find;
- governance panel does not repeat engine/environment/host facts.

### F4. Real E2E

Use real backend plus dedicated query MySQL fixture.

Required:

- `/settings` exposes Query Credential settings entry;
- direct `/settings/query-credentials` as admin shows admin controls;
- `/query` can run:
  - `SELECT 1`;
  - `SHOW DATABASES`;
  - `SHOW TABLES FROM query_e2e`;
  - `DESCRIBE query_e2e.query_e2e_items`;
- unsafe `UPDATE` remains rejected;
- no Workbench credential edit controls.

## Verification

Run:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
npm run test:e2e -- --grep query
npm run test:e2e -- --grep "query credential"
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Frontend
```

If GitNexus analyze modifies generated `AGENTS.md` or `CLAUDE.md` stats blocks,
restore them unless explicitly authorized.

## Final Report

Include:

- commit hashes;
- admin role recovery design and tests;
- `/settings` entry behavior;
- target-area consolidation behavior;
- real E2E result or explicit blocker;
- full verification matrix;
- GitNexus result and caveats;
- final git status;
- scope confirmation:
  - no backend edits;
  - no credential/DSN/password browser state;
  - no actorUserId;
  - no Workbench credential edit controls;
  - no fake backend final E2E;
  - no push/tag/release/deploy;
  - no AI co-author.
