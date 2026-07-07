# Frontend Phase 38E Query Workbench IA And Admin Feedback Worker Prompt

You are implementing the frontend side of Phase 38E for ControlHub.

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repo is separate. Do not edit backend product code in this frontend task.

## Objective

Tighten Query Workbench information architecture and query credential admin
feedback without adding backend APIs or starting the full database IDE rewrite.

Must deliver:

- visible save feedback for single-target credential metadata edits;
- operations table refresh after single-target save/delete;
- query target navigation that behaves like a connection navigator, not a flat
  dropdown;
- compact active target facts shown once;
- governance panel that is blocker-focused and does not duplicate target facts;
- `/settings` entry and direct URL admin recovery preserved;
- no Workbench credential edit controls.

## Required Reading

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-07-phase-38e-query-workbench-ia-and-admin-feedback.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-07-07-phase-38e-query-workbench-ia-and-admin-feedback.md
/Users/fan/GolangProjects/ControlHub/advisor-plans/001-query-workbench-bytebase-ux-realignment.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

Frontend files to inspect:

```text
components/settings/query-credential-settings.tsx
components/query/query-workbench.tsx
components/query/query-governance-panel.tsx
components/query/query-schema-browser.tsx
lib/query-target-display.ts
messages/en.json
messages/zh-CN.json
tests/components/query-credential-settings.test.tsx
tests/components/query-workbench.test.tsx
e2e/query-credential-settings.spec.ts
e2e/query-workbench.spec.ts
```

## Rules

- Use TDD: write failing tests before production code.
- Run GitNexus impact before editing frontend symbols.
- Do not add backend APIs.
- Do not add credential edit controls to `/query`.
- Do not add DSN/password browser state, input, request body, or logs.
- Do not send `actorUserId`.
- Do not add Monaco/CodeMirror, SQL formatting, multiple worksheet tabs, export,
  saved queries, or approval workflow in this phase.
- Do not fake backend for final E2E.
- Do not push/tag/release/deploy.
- Do not add AI co-author.

## Tasks

### F0. Credential save feedback

If not already present, implement:

- configured-target primary action text: `Save credential metadata`;
- zh-CN text: `保存凭据元数据`;
- success status after save: `Credential metadata saved.`;
- zh-CN success: `凭据元数据已保存。`;
- parent operations table refresh after single-target save/delete;
- stale target guard preserved.

Tests:

- save button uses save wording;
- successful save shows status;
- operations row refreshes to the new credential ref;
- stale save result does not leak after target switch.

### F1. Query target connection navigation

Replace dropdown-shaped target picking with a scalable connection navigation
surface.

Required:

- search by display name, resource name, engine, environment, host, port,
  readiness, cluster;
- group by environment and cluster;
- ready targets promoted but locked targets still visible;
- filters live inside navigation surface;
- keyboard/screen-reader access preserved;
- target switch still remounts target-owned editor state.

### F2. Active target header and fact dedupe

Show active target identity once:

- display name;
- engine;
- environment;
- readiness;
- host:port when complete;
- details disclosure for owner/language/cluster.

Do not repeat these large facts in governance.

### F3. Governance compression

Governance should answer "can I run, and what blocks me?"

Required:

- localized credential state badge;
- action availability badges with available/locked semantics;
- primary blocker inline for locked targets;
- long policy copy behind tooltip/details;
- hydration-safe admin link;
- no credential edit controls.

### F4. E2E and gates

Update E2E:

- `/settings` entry exists;
- direct `/settings/query-credentials` admin recovery works;
- credential save feedback appears;
- operations row refreshes;
- query target navigation search/grouping works;
- ready target still runs SELECT/SHOW/DESCRIBE against real backend when
  available.

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

Run real E2E only with real backend + dedicated query MySQL fixture:

```bash
npm run test:e2e -- --grep "query credential"
npm run test:e2e -- --grep "Query Workbench"
```

Run GitNexus before final report:

```bash
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Frontend
```

## Final Report

Include:

- branch/worktree;
- commits;
- files changed;
- P1 save-feedback proof;
- target navigation behavior;
- governance/fact dedupe behavior;
- E2E result or explicit blocker;
- full verification matrix;
- GitNexus result and caveats;
- final git status;
- scope confirmation:
  - no backend edits;
  - no credential/DSN/password browser state;
  - no `actorUserId`;
  - no Workbench credential edit controls;
  - no fake backend final E2E;
  - no push/tag/release/deploy;
  - no AI co-author.
