# Phase 38E Query Workbench IA And Admin Feedback Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Tighten Query Workbench information architecture and query credential admin feedback without adding backend APIs or a full SQL IDE editor.

**Architecture:** Frontend-only phase. Keep backend contracts unchanged, move target discovery toward a connection-navigation surface, compress duplicated target/governance facts, and make credential metadata mutations visibly successful and table-synchronized.

**Tech Stack:** Next.js 16, React, TypeScript, next-intl, existing shadcn/base UI components, Vitest, Playwright.

---

## Scope

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repo is docs source only for this plan. Do not edit backend product code.

## Non-Goals

- No backend product code.
- No SQL guard changes.
- No DSN/password browser input.
- No credential secret write API.
- No Monaco/CodeMirror migration.
- No SQL formatter.
- No multiple worksheet tabs.
- No export/saved-query/approval workflow.
- No CI workflow changes.
- No push/tag/release/deploy unless explicitly authorized after review.

## Required Reading

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-07-phase-38e-query-workbench-ia-and-admin-feedback.md
/Users/fan/GolangProjects/ControlHub/advisor-plans/001-query-workbench-bytebase-ux-realignment.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-30-phase-38c-query-workbench-readonly-sql-and-ux-stabilization.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-05-phase-38d-query-workbench-admin-followups.md
```

## Files

Expected frontend files:

- Modify: `components/settings/query-credential-settings.tsx`
- Modify: `components/query/query-workbench.tsx`
- Modify: `components/query/query-governance-panel.tsx`
- Modify: `components/query/query-schema-browser.tsx` only if needed for target fact dedupe
- Modify: `messages/en.json`
- Modify: `messages/zh-CN.json`
- Modify: `tests/components/query-credential-settings.test.tsx`
- Modify: `tests/components/query-workbench.test.tsx`
- Modify: `e2e/query-credential-settings.spec.ts`
- Modify: `e2e/query-workbench.spec.ts`

Out of scope:

- `services/query-credentials.ts` unless a failing test proves the request body changed accidentally.
- backend files under `/Users/fan/GolangProjects/ControlHub/internal`.

## Task F0: Credential Detail Save Feedback

**Purpose:** Fix the current "edit button does nothing" perception.

- [ ] **Step 1: Write failing component tests**

In `tests/components/query-credential-settings.test.tsx`, add or verify tests that prove:

- configured targets render a primary button named `Save credential metadata`;
- saving shows `Credential metadata saved.`;
- the operations table row refreshes to the saved credential ref;
- stale save responses from a previous target do not show success on the new target.

Run:

```bash
npm run test -- tests/components/query-credential-settings.test.tsx
```

Expected before implementation: the new assertions fail because the UI still uses edit/configure semantics or lacks feedback.

- [ ] **Step 2: Implement minimal UI behavior**

In `components/settings/query-credential-settings.tsx`:

- pass an `onCredentialChanged` callback from `QueryCredentialSettings` to `CredentialDetailPanel`;
- call it after successful `saveQueryCredential` and `deleteQueryCredential`;
- add local `success` state and clear it on target load, save start, delete start, and errors;
- render success with `role="status"`;
- use `detail.saveButton` for the primary action label.

In `messages/en.json`:

```json
"saveButton": "Save credential metadata",
"saved": "Credential metadata saved."
```

In `messages/zh-CN.json`:

```json
"saveButton": "保存凭据元数据",
"saved": "凭据元数据已保存。"
```

- [ ] **Step 3: Verify F0**

Run:

```bash
npm run test -- tests/components/query-credential-settings.test.tsx
```

Expected: all tests in that file pass.

Suggested commit:

```bash
git commit -m "fix: show query credential metadata save feedback"
```

## Task F1: Connection Navigation Surface

**Purpose:** Replace dropdown-shaped target picking with a scalable connection navigator.

- [ ] **Step 1: Write failing tests**

In `tests/components/query-workbench.test.tsx`, add tests that prove:

- target navigation groups targets by environment and cluster;
- search matches name, engine, environment, host, port, readiness, and cluster;
- ready targets are visually/semantically promoted;
- filters are inside the target navigation surface;
- selecting a target still remounts/isolates target-owned editor state.

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected before implementation: grouping/navigation assertions fail.

- [ ] **Step 2: Implement navigation**

In `components/query/query-workbench.tsx`:

- split the current `TargetSwitcher` into smaller local components if needed:
  `TargetNavigation`, `TargetNavigationGroup`, `ActiveTargetHeader`;
- keep search/filter state in `QueryWorkbench`;
- group `filteredTargets` by `connectionContext.environment`, then by
  `connectionContext.clusterName || "Ungrouped"`;
- keep ready targets first inside each group;
- move engine/query kind/readiness filters into the navigation surface;
- keep active target facts in a compact header.

Do not add a new backend fetch.

- [ ] **Step 3: Verify F1**

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: target navigation tests pass.

Suggested commit:

```bash
git commit -m "feat: add query target connection navigation"
```

## Task F2: Target Fact Deduplication And Governance Compression

**Purpose:** Reduce page noise and reserve prime space for editor/results.

- [ ] **Step 1: Write failing tests**

In `tests/components/query-workbench.test.tsx`, add tests that prove:

- engine/environment/host/readiness appear in the active target header;
- governance panel does not repeat engine/environment/host/cluster;
- governance shows credential state and action availability as compact badges;
- the primary blocker is visible for locked targets;
- longer policy copy is behind tooltip/details.

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected before implementation: dedupe/compression assertions fail.

- [ ] **Step 2: Implement compression**

In `components/query/query-governance-panel.tsx`:

- keep credential status, missing fields, policy checklist, and available actions;
- remove target fact duplication;
- make long static explanations tooltip/details-only;
- preserve hydration-safe `useAdminRole` behavior;
- ensure no credential edit controls render on `/query`.

In `components/query/query-workbench.tsx`:

- keep active target facts in one compact header;
- keep owner/language/cluster in a disclosure.

- [ ] **Step 3: Verify F2**

Run:

```bash
npm run test -- tests/components/query-workbench.test.tsx
```

Expected: governance/fact tests pass.

Suggested commit:

```bash
git commit -m "fix: dedupe query target facts and compact governance"
```

## Task F3: Settings Entry And E2E Coverage

**Purpose:** Ensure the final user flow matches the preview workflow.

- [ ] **Step 1: Update E2E specs**

In `e2e/query-credential-settings.spec.ts`, cover:

- `/settings` exposes query credential settings entry for admin;
- direct `/settings/query-credentials` URL recovers admin role;
- saving a target credential shows success feedback;
- operations row refreshes after save.

In `e2e/query-workbench.spec.ts`, cover:

- connection navigation search/grouping;
- ready target selection;
- no Workbench credential edit controls;
- existing SELECT/SHOW/DESCRIBE execution still passes when backend is available.

- [ ] **Step 2: Run frontend gates**

Run:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Expected: all exit 0.

- [ ] **Step 3: Run real E2E when backend is available**

Use the real backend on `:8080` plus the dedicated query MySQL fixture.

Run:

```bash
npm run test:e2e -- --grep "query credential"
npm run test:e2e -- --grep "Query Workbench"
```

Expected: query credential and Query Workbench specs pass with no fake backend.

If backend is unavailable, do not claim E2E passed; report the blocker.

Suggested commit:

```bash
git commit -m "test: cover query workbench ia and credential feedback"
```

## GitNexus

Before editing frontend symbols, run impact in the frontend repo:

```bash
npx gitnexus impact QueryCredentialSettings --direction upstream --repo ControlHub-Frontend
npx gitnexus impact CredentialDetailPanel --direction upstream --repo ControlHub-Frontend
npx gitnexus impact QueryWorkbench --direction upstream --repo ControlHub-Frontend
npx gitnexus impact QueryGovernancePanel --direction upstream --repo ControlHub-Frontend
```

Before commit:

```bash
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Frontend
```

If `npx gitnexus analyze` updates `AGENTS.md` or `CLAUDE.md` stats blocks,
restore those generated changes unless explicitly authorized.

## Final Report Requirements

Include:

- commits;
- changed files;
- P1 save-feedback proof;
- target navigation behavior;
- governance/fact dedupe behavior;
- settings entry behavior;
- request-shape proof: no `actorUserId`, no DSN/password;
- real E2E result or explicit blocker;
- full verification matrix;
- GitNexus result and caveats;
- final git status;
- scope confirmation.
