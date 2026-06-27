# Frontend Phase 38B Query Credential Admin Experience Worker Prompt

You are implementing the frontend side of Phase 38B for ControlHub.

Frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

Backend repository is separate and must not be edited by this worker:

```text
/Users/fan/GolangProjects/ControlHub
```

## Objective

Harden the existing Phase 38A `/settings/query-credentials` page into an admin
operations surface for query credential metadata coverage.

This phase improves the management experience. It must not change the secret
boundary:

- no DSN/password input;
- no DSN/password browser state;
- no DSN/password request body;
- no secret manager UI;
- no Query Workbench edit controls.

## Required Reading

Backend planning docs:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-27-phase-38b-query-credential-admin-experience-hardening.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-06-27-phase-38b-query-credential-admin-experience-hardening.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-06-24-phase-38a-query-credential-metadata-management-design.md
/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-06-24-phase-38a-query-credential-metadata-management-evidence.md
```

Frontend code:

```text
app/(console)/settings/query-credentials/page.tsx
components/settings/query-credential-settings.tsx
components/query/query-governance-panel.tsx
services/query-credentials.ts
services/query-targets.ts
types/query-credential.ts
types/query-target.ts
lib/query-target-display.ts
messages/en.json
messages/zh-CN.json
tests/components/query-credential-settings.test.tsx
tests/services/query-credentials.test.ts
e2e/query-credential-settings.spec.ts
e2e/query-workbench.spec.ts
```

## Worktree

Create a frontend worktree from current frontend `main`:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
git worktree add .worktrees/frontend-phase-38b-query-credential-admin-experience -b feat/phase-38b-query-credential-admin-experience main
cd .worktrees/frontend-phase-38b-query-credential-admin-experience
git status --short --branch
```

Do not edit the backend repository.

## Backend Dependency

Use existing Phase 38A APIs only:

```text
GET    /query-targets
GET    /query-targets/{id}/credential
PUT    /query-targets/{id}/credential
DELETE /query-targets/{id}/credential
```

Do not add or require backend aggregate/bulk APIs in this phase. If bounded
client-side fan-out is not acceptable during implementation or review, stop and
report the backend API need. Do not silently create a frontend workaround that
changes the security boundary.

Final E2E must use a real backend plus the Phase 37H dedicated query MySQL
fixture. Do not mock backend responses for final E2E evidence.

## Scope

Allowed:

```text
coverage summary cards
credential-status loading state
environment/cluster grouping
status filters
bulk metadata apply via existing PUT fan-out
bulk metadata remove via existing DELETE fan-out
per-target operation results
read-only Query Workbench boundary tests
i18n
unit/component/service/E2E tests
```

Not allowed:

```text
backend edits
new backend API
DSN/password input
DSN/password browser state
actorUserId in request body/query
host/port/engine in credential PUT body
secret write API
secret manager UI
new query engines
export/saved query/approval features
inline Query Workbench credential edit controls
fake backend for final E2E
push/tag/release/deploy
```

## Implementation

Follow frontend tasks F1 through F6 in:

```text
/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-06-27-phase-38b-query-credential-admin-experience-hardening.md
```

Critical requirements:

- Build a frontend coverage read model from:
  - `GET /query-targets` for target inventory and governance state;
  - `GET /query-targets/{id}/credential` for configured metadata fields and
    runtime status.
- Fetch per-target credential status with bounded client-side fan-out.
- Show loading and per-row fetch errors; do not leave a permanent whole-page
  spinner.
- Do not infer credential ref, enabled flag, policy, or runtime status from
  display-only governance strings.
- Add summary cards for total, ready, missing metadata, secret missing,
  binding mismatch, policy blocked, and disabled.
- Add environment/cluster grouping and status filters.
- Add bulk apply with existing `PUT /query-targets/{id}/credential` fan-out.
- Add bulk remove with existing `DELETE /query-targets/{id}/credential`
  fan-out.
- Show per-target pending/success/failure. Partial failure must not look like
  full success.
- Keep operations sequential or with a small explicit concurrency limit.
- Require `all_environments` confirmation before any matching bulk apply.
- Keep non-admin restricted view hydration-safe.
- Keep Query Workbench read-only; admin link may navigate to settings, but no
  inline edit controls are allowed.
- PUT body may contain only:
  - `credentialRef`
  - `enabled`
  - `environmentPolicy`
  - `confirmAllEnvironments`
- Never send:
  - `actorUserId`
  - `dsn`
  - `password`
  - `host`
  - `port`
  - `engine`

## Testing Requirements

Add or update tests for:

- coverage model derivation;
- unknown statuses stay visible and non-ready;
- admin gate remains hydration-safe;
- non-admin never sees management controls;
- credential status fan-out loading and per-row failure;
- filters and grouping;
- bulk apply request body whitelist;
- polluted internal objects do not leak forbidden fields;
- `all_environments` confirmation;
- bulk partial success/failure display;
- bulk remove confirmation and per-target result;
- Query Workbench has no credential edit controls.

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

Then, only if backend and fixture are available, run real cross-repo E2E with
backend on `:8080`, frontend proxy/dev server, and the Phase 37H dedicated query
MySQL fixture:

```bash
npm run test:e2e -- --grep "query credential|query workbench"
```

If backend or fixture is unavailable, stop and report. Do not claim E2E passed.

## GitNexus

Follow the frontend repo instructions:

```bash
npx gitnexus detect-changes --scope compare --base-ref main --repo ControlHub-Frontend
```

If the index is stale:

```bash
npx gitnexus analyze
```

If analyze modifies generated `AGENTS.md` or `CLAUDE.md` stats blocks, restore
them unless explicitly authorized.

## Commits

Use focused commits. Suggested sequence:

```text
feat: derive query credential coverage model
feat: add query credential operations overview
feat: add bulk query credential metadata apply
feat: add bulk query credential metadata removal
test: preserve query workbench credential boundary
test: cover query credential operations experience
```

Do not add AI co-author attribution.

## Final Report

Include:

- worktree, branch, commit hashes;
- files changed;
- coverage summary behavior;
- grouping/filter behavior;
- bulk apply/remove behavior;
- request-shape proof:
  - no `actorUserId`;
  - no `dsn`;
  - no `password`;
  - no `host`;
  - no `port`;
  - no `engine`;
- admin-only proof;
- non-admin restricted-view proof;
- Query Workbench read-only proof;
- no raw enum / i18n proof;
- real backend E2E result or explicit not-run reason;
- full frontend verification matrix;
- GitNexus detect_changes summary and caveats;
- final git status;
- scope confirmation:
  - no backend edits;
  - no credential input/storage;
  - no new backend API;
  - no fake backend final E2E;
  - no push/tag/release/deploy;
  - no AI co-author.
