# Frontend Phase 38C Query Workbench UX Stabilization Worker Prompt

You are implementing the frontend side of Phase 38C for ControlHub.

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repo is separate. Do not edit backend code in this frontend task.

## Objective

Fix immediate Query Workbench/credential settings bugs and tighten the Query
Workbench UI around the read-only SQL boundary.

Must fix:

- credential settings ICU `{ref}` formatting error;
- query governance panel hydration mismatch;
- query target selector readability/searchability;
- oversized governance and target facts;
- frontend copy that implies SELECT-only instead of read-only SQL.

## Required Reading

```text
/Users/fan/GolangProjects/ControlHub/docs/decisions/2026-06-30-phase-38c-query-workbench-readonly-sql-boundary.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-06-30-query-workbench-preview-findings.md
```

Frontend files to inspect:

```text
components/settings/query-credential-settings.tsx
components/query/query-governance-panel.tsx
components/query/query-workbench.tsx
components/query/query-editor-shell.tsx
lib/query-target-display.ts
messages/en.json
messages/zh-CN.json
tests/components/query-credential-settings.test.tsx
tests/components/query-workbench.test.tsx
e2e/query-workbench.spec.ts
e2e/query-credential-settings.spec.ts
```

## Rules

- Do not add credential edit controls to `/query`.
- Do not add DSN/password inputs or browser state.
- Do not send `actorUserId`.
- Do not add backend APIs.
- Do not fake backend for final E2E.
- Do not push/tag/release/deploy.
- Do not add AI co-author.

## Tasks

1. Fix ICU formatting:
   - remove `{ref}` placeholders from static copy, or pass a safe literal;
   - prefer `CONTROLHUB_QUERY_CREDENTIAL_your-ref`;
   - add tests for en and zh-CN rendering.
2. Fix query governance hydration:
   - no render-time `window` or `sessionStorage`;
   - initial admin state `null`;
   - stable SSR/first client markup;
   - admin link appears only after role is read.
3. Replace the query target selector with a searchable picker:
   - search target name, resource name, engine, env, host, cluster;
   - readable rows with name, engine, env, host:port, readiness/run state;
   - ready targets highlighted or sorted first;
   - no raw enums.
4. Compact governance and target facts:
   - badges for executable/locked, credential state, audit, sandbox;
   - details in tooltip/popover/details;
   - avoid duplicated large engine/env/host/cluster blocks.
5. Update worksheet copy:
   - say "read-only SQL";
   - do not say "SELECT only";
   - include examples for `SELECT` and `SHOW TABLES`.
6. Add E2E coverage once backend Phase 38C is available:
   - ready target runs `SHOW TABLES`;
   - ready target runs `DESCRIBE query_e2e_items`;
   - unsafe `UPDATE` remains rejected;
   - query history records the attempts.

## Verification

Run from the frontend worktree:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

For final E2E, use real backend plus the dedicated query MySQL fixture. Do not
claim E2E passed if backend Phase 38C is not running.

```bash
npm run test:e2e -- --grep query
```

Run GitNexus:

```bash
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Frontend
```

If analyze modifies generated docs stats blocks, restore them unless explicitly
authorized.

## Final Report

Include:

- commits;
- files changed;
- bug fixes;
- target picker behavior;
- governance/facts compression behavior;
- read-only SQL copy changes;
- tests added;
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
