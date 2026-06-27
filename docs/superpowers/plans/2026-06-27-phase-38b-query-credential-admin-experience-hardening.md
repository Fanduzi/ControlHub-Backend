# Phase 38B Query Credential Admin Experience Hardening Implementation Plan

> **Scope:** frontend-first hardening of the Phase 38A credential metadata
> management UI. Do not change backend APIs unless review proves the existing
> fan-out approach is insufficient.

## Objective

Turn `/settings/query-credentials` from a target-by-target configuration page
into an admin operations surface for query credential metadata coverage:

- coverage summary;
- environment/cluster grouping;
- status filtering;
- bulk metadata apply/remove;
- per-target operation results;
- preserved no-secret and non-admin boundaries.

## Non-Goals

- No DSN/password input.
- No secret manager UI.
- No backend migration.
- No new query engines.
- No approval flow.
- No export.
- No saved queries.
- No Query Workbench edit controls.
- No GitHub Actions workflow changes.
- No tag, release, or deployment.

## Current Baseline

Phase 38A frontend already provides:

- `types/query-credential.ts`;
- `services/query-credentials.ts`;
- `/settings/query-credentials`;
- `components/settings/query-credential-settings.tsx`;
- Query Workbench credential status section;
- admin gate;
- stale target guard;
- request body whitelist;
- `disabled` response-policy handling;
- real backend E2E evidence.

Phase 38B should build on this, not replace it.

## F1. Coverage Read Model In Frontend

**Files:**

- Modify `/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx`
- Consider creating `/Users/fan/JsProjects/ControlHub/lib/query-credential-operations.ts`
- Add tests under `/Users/fan/JsProjects/ControlHub/tests/lib/` or component tests.

### Step 1: Add pure derivation helpers

Create helper types and pure functions for:

- normalized credential operation row;
- coverage counts by runtime status;
- counts by environment;
- counts by cluster;
- selectable/not-selectable reason;
- ready/not-ready grouping.

Inputs should be existing `QueryTarget[]` plus credential status responses.

The helper must not call React hooks or network services.

Credential status responses must come from the existing per-target API:

```text
GET /query-targets/{id}/credential
```

Use `GET /query-targets` for target inventory and governance state. Use
`GET /query-targets/{id}/credential` for configured metadata fields such as
credential ref, enabled flag, environment policy, and runtime status. Do not
derive configured metadata fields from `governance.credentialState` alone.

### Step 2: Tests first

Add tests for:

- total count;
- ready count;
- missing metadata count;
- secret missing count;
- binding mismatch count;
- policy blocked count;
- disabled count;
- unsupported/incomplete count;
- environment grouping;
- cluster grouping;
- target selectability rules.

### Step 3: Implement derivation

Keep unknown statuses visible. Unknown values should fall back to humanized
labels and remain non-ready.

### Step 4: Add credential-status loading plan

Implement a bounded client-side fan-out for credential status fetches. It may be
sequential or use a small concurrency limit. It must show partial loading and
per-row fetch errors, not a permanent whole-page spinner.

If this proves too slow in review, stop and report the backend aggregate API
need. Do not add a speculative backend endpoint inside the frontend task.

### Step 5: Commit

Suggested commit:

```bash
git commit -m "feat: derive query credential coverage model"
```

## F2. Coverage Summary And Grouped Operations Table

**Files:**

- Modify `components/settings/query-credential-settings.tsx`
- Add subcomponents if the file becomes too large:
  - `components/settings/query-credential-coverage-summary.tsx`
  - `components/settings/query-credential-operations-table.tsx`

### Step 1: Add UI tests

Cover:

- summary cards render counts;
- filtering by status hides unrelated rows;
- environment grouping labels rows correctly;
- cluster grouping labels rows correctly;
- non-admin still sees only restricted view;
- admin still sees operations page after hydration.

### Step 2: Implement summary cards

Cards:

- total;
- ready;
- missing metadata;
- secret missing;
- binding mismatch;
- policy blocked;
- disabled.

The card labels must be localized in EN and zh-CN.

### Step 3: Implement grouping/filter controls

Controls:

- search;
- environment;
- cluster;
- engine;
- runtime status;
- configured/unconfigured;
- grouping mode: flat, by environment, by cluster.

Avoid raw enum leakage. Use existing label helpers or add centralized helpers.

### Step 4: Implement table/list

Each row must show:

- target name;
- engine;
- environment;
- cluster;
- host/port;
- runtime status;
- credential state;
- credential ref when configured;
- policy;
- enabled flag;
- last operation result in the current session.

### Step 5: Commit

Suggested commit:

```bash
git commit -m "feat: add query credential operations overview"
```

## F3. Bulk Metadata Apply

**Files:**

- Modify `components/settings/query-credential-settings.tsx`
- Modify or add service tests if a helper is introduced.
- Add component tests for bulk behavior.

### Step 1: Define UI contract

Bulk apply form fields:

- credential ref;
- enabled;
- environment policy;
- confirmation checkbox for `all_environments`;
- selected target count;
- dry summary of what will be changed.

Do not add DSN/password fields.

### Step 2: Tests first

Add tests:

- selected safe targets can be bulk-applied;
- unsupported/incomplete targets are not selectable;
- `all_environments` disables apply until confirmed;
- polluted internal input cannot send forbidden fields;
- per-target success/failure is displayed;
- partial failure does not appear as full success.

### Step 3: Implement fan-out using existing API

For each selected target:

```text
PUT /query-targets/{id}/credential
```

Body whitelist remains:

```json
{
  "credentialRef": "ORDER_MYSQL_RO",
  "enabled": true,
  "environmentPolicy": "non_prod_only"
}
```

For all environments:

```json
{
  "credentialRef": "ORDER_MYSQL_RO",
  "enabled": true,
  "environmentPolicy": "all_environments",
  "confirmAllEnvironments": true
}
```

Use concurrency conservatively. A simple sequential implementation is acceptable
for Phase 38B because it preserves clear progress and avoids accidental request
bursts.

### Step 4: Refresh statuses

After each successful target update, refresh that target's credential status.
After the batch completes, refresh query targets if the existing page model
requires it for updated governance state.

### Step 5: Commit

Suggested commit:

```bash
git commit -m "feat: add bulk query credential metadata apply"
```

## F4. Bulk Metadata Remove

**Files:**

- Modify `components/settings/query-credential-settings.tsx`
- Add component tests.

### Step 1: Add confirmation UI

Bulk remove must show:

- selected target count;
- list or sample of selected targets;
- warning that only metadata binding is removed;
- statement that actual server-side credentials are not deleted.

### Step 2: Tests first

Cover:

- remove disabled until targets selected;
- confirmation required;
- per-target delete status shown;
- partial delete failure displayed;
- no Workbench edit controls appear.

### Step 3: Implement fan-out delete

For each selected target:

```text
DELETE /query-targets/{id}/credential
```

### Step 4: Commit

Suggested commit:

```bash
git commit -m "feat: add bulk query credential metadata removal"
```

## F5. Query Workbench Boundary Recheck

**Files:**

- `components/query/query-governance-panel.tsx`
- `tests/components/query-workbench.test.tsx`
- `e2e/query-workbench.spec.ts`

### Step 1: Tests

Ensure:

- Workbench shows credential status labels;
- admin link can point to settings;
- non-admin sees contact-admin copy;
- no credential ref input;
- no enabled checkbox;
- no policy select;
- no save/remove/configure controls.

### Step 2: Keep Workbench read-only

Do not add a shortcut that opens inline configuration. If an admin link exists,
it must navigate to settings/admin.

### Step 3: Commit

Suggested commit:

```bash
git commit -m "test: preserve query workbench credential boundary"
```

## F6. E2E

**Files:**

- `e2e/query-credential-settings.spec.ts`
- `e2e/query-workbench.spec.ts`

### Step 1: Add targeted E2E cases

With real backend and Phase 37H fixture:

- admin opens credential operations page;
- coverage summary renders;
- filters/grouping work;
- admin bulk-applies metadata to one safe target or fixture target;
- per-target success appears;
- Query Workbench still runs the ready target;
- Workbench has no edit controls;
- non-admin route shows restricted page or cannot access management controls.

Do not mock backend responses for final E2E evidence.

### Step 2: Local run

Run:

```bash
npm run test:e2e -- --grep "query credential|query workbench"
```

If backend or fixture is unavailable, stop and report. Do not claim real E2E
passed.

### Step 3: Commit

Suggested commit:

```bash
git commit -m "test: cover query credential operations experience"
```

## D1. Evidence Handoff

The frontend implementation worker should produce an evidence report in its
final response. Do not edit the backend repository from the frontend worktree
unless explicitly instructed.

After the frontend branch is merged and pushed, run a separate backend docs-only
sync to add:

```text
docs/superpowers/notes/2026-06-27-phase-38b-query-credential-admin-experience-hardening-evidence.md
```

Update release/readiness docs only after implementation is merged and verified.

Evidence must include:

- frontend commit range;
- changed files;
- no-secret proof;
- admin-only proof;
- bulk operation behavior;
- real backend E2E result or explicit not-run reason;
- frontend CI result after push.

## Verification Matrix

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

Run real E2E only with backend and fixture available:

```bash
npm run test:e2e -- --grep "query credential|query workbench"
```

Run GitNexus before commit/merge as required by the frontend repo instructions:

```bash
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Frontend
```

If the index is stale, run:

```bash
npx gitnexus analyze
```

If analyze updates generated `AGENTS.md` or `CLAUDE.md` stats blocks, do not
include them unless explicitly authorized.

## Finishing Criteria

Before merge:

- all local gates pass;
- no real E2E claim without backend + fixture;
- no DSN/password fields or request body entries;
- no `actorUserId` sent;
- Workbench remains read-only;
- non-admin cannot see management controls;
- bulk partial failure is visible;
- no backend code changed unless separately reviewed.

After merge/push:

- Frontend CI run recorded;
- evidence docs updated in backend repo;
- no tag/release/deploy.
