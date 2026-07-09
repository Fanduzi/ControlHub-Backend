# Phase 38H Query Workbench Scalable IA Reset Implementation Plan

> **For agentic workers:** This is a full-stack phase. Use an isolated backend
> worktree and an isolated frontend worktree. Do not collapse backend and
> frontend commits together. Run review after each major block.

**Goal:** Replace the current all-target, three-column Query Workbench IA with
a scalable backend-backed target search/pagination contract and a database IDE
layout. Make Query Credential Management a paged operations table with
responsive modal/drawer detail.

**Architecture:** Add pagination/search to `GET /query-targets` while preserving
the top-level `{ items }` envelope. Frontend consumes the paged contract for
Query Workbench and credential admin. Governance moves from permanent layout to
inline status plus details. Credential detail editing moves to responsive
modal/drawer.

---

## Scope

Backend repo:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repo:

```text
/Users/fan/JsProjects/ControlHub
```

## Required Reading

```text
docs/superpowers/specs/2026-07-09-phase-38h-query-workbench-scalable-ia-reset.md
docs/superpowers/specs/2026-07-08-phase-38g-query-workbench-real-usability-cleanup.md
docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
```

Local frontend reference:

```text
/Users/fan/JsProjects/ControlHub/components/query/query-workbench.tsx
/Users/fan/JsProjects/ControlHub/components/query/query-governance-panel.tsx
/Users/fan/JsProjects/ControlHub/components/settings/query-credential-settings.tsx
```

Backend reference:

```text
internal/model/query_target.go
internal/api/query_target_handler.go
internal/service/query_target_service.go
internal/repository/mysql/query_target_repository.go
internal/openapi/openapi.yaml
```

## Non-Goals

- No SQL guard changes.
- No credential secret write/read API.
- No DSN/password browser state, request body, response display, or logs.
- No credential edit controls inside `/query`.
- No saved query/export/approval/JIT implementation.
- No worksheet backend persistence.
- No CI workflow changes.
- No tag/release/deploy.

## B0. Worktrees And Baseline

- [ ] Create backend worktree:

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
git worktree add .worktrees/backend-phase-38h-query-target-pagination -b phase-38h-query-target-pagination main
```

- [ ] Create frontend worktree:

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree add .worktrees/phase-38h-query-workbench-scalable-ia-reset -b phase-38h-query-workbench-scalable-ia-reset main
```

- [ ] Run baseline backend gates:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
```

- [ ] Run baseline frontend gates:

```bash
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Stop if baseline is red before edits.

## B1. Backend Paged Query Target Contract

**Files**

```text
internal/model/query_target.go
internal/api/query_target_handler.go
internal/service/query_target_service.go
internal/repository/mysql/query_target_repository.go
internal/openapi/openapi.yaml
internal/*/*query_target*_test.go
```

**Design**

- Extend `QueryTargetListQuery` with:
  - `Q string`
  - `Page int`
  - `PageSize int`
- Add `PageInfo` model:
  - `page`
  - `pageSize`
  - `totalItems`
  - `totalPages`
  - `hasNextPage`
  - `hasPreviousPage`
- Extend `QueryTargetListResponse` with `PageInfo PageInfo`.
- Defaults: `page=1`, `pageSize=50`, `max pageSize=100`.
- Existing `items` field remains unchanged.

**RED tests**

- invalid `page=0`, `page=-1`, `page=abc` returns 400;
- invalid `pageSize=0`, `pageSize=abc`, `pageSize=101` returns 400;
- default query returns `pageInfo.page=1` and bounded `pageSize`;
- `q` matches display/resource name, host, engine, environment, cluster;
- pagination returns a bounded page and correct totals;
- existing `engine`/`environmentId` filters still work with pagination.

**GREEN criteria**

- OpenAPI documents new query parameters and `pageInfo`.
- Existing clients reading `items` still work.
- No DSN/password introduced into search or response.

**Commit**

```bash
git commit -m "feat(query): add paged query target search"
```

## B2. Backend Large-Fixture/Integration Coverage

**Purpose**

Prove the API supports fleet-scale target selection.

**Tests**

- create enough query-target-like resources in integration setup to prove
  `pageSize=25` does not return all rows;
- search by host/name returns expected bounded results;
- `pageInfo.totalItems` and `totalPages` are stable.

**Gates**

```bash
go test -count=1 ./internal/service ./internal/api
make test-integration
make openapi-validate
make test-openapi-fuzz
```

**Commit**

```bash
git commit -m "test(query): cover paged query target search"
```

## F1. Frontend Query Target Service Pagination

**Files**

```text
services/query-targets.ts or existing query target service file
types/query-target.ts or existing query target types
tests/services/*
```

**Design**

- Add `QueryTargetPageInfo`.
- Parse `items` and `pageInfo`.
- Add query params `q`, `page`, `pageSize`, `engine`, `environmentId`.
- Keep compatibility where call sites only need `items`.

**Tests**

- request URL includes `q/page/pageSize`;
- parses `pageInfo`;
- handles missing `pageInfo` only if old mocks still exist, but production
  path should expect it after backend merge.

**Commit**

```bash
git commit -m "feat(query): consume paged query target search"
```

## F2. Query Workbench Layout Reset

**Files**

```text
components/query/query-workbench.tsx
components/query/query-connection-navigator.tsx
components/query/query-governance-panel.tsx
components/query/query-editor-shell.tsx
messages/en.json
messages/zh-CN.json
tests/components/query-workbench.test.tsx
```

**Design**

- Remove permanent three-column layout.
- Keep one left explorer area and one dominant editor/result area.
- Left explorer has schema/object mode plus "Switch connection" search mode.
- Empty connection switcher state shows bounded current page, recents/favorites
  if available, and search input. It must not render all targets.
- Active target header sits above editor and shows compact facts once.
- Governance is inline:
  - primary blocker message near Run/editor;
  - compact badges;
  - details popover/drawer.

**RED tests**

- no permanent Governance & Access right column in desktop layout;
- active target facts render once;
- empty switcher renders at most `pageSize` targets;
- search calls paged query-target service;
- active target remains visible when not in the current result page;
- URL `targetId` initializes active target.

**Commit**

```bash
git commit -m "feat(query): reset workbench layout around bounded target search"
```

## F3. Credential Management Pagination

**Files**

```text
components/settings/query-credential-settings.tsx
services/query-credentials.ts
tests/components/query-credential-settings.test.tsx
e2e/query-credential-settings.spec.ts
messages/en.json
messages/zh-CN.json
```

**Design**

- Query target table uses paged backend list.
- Initial credential status fan-out only runs for visible page rows.
- Summary cards clearly state current page/current filter scope unless a
  backend aggregate is added.
- Page size options: `25`, `50`, `100`.
- Bulk actions apply only to explicitly selected visible rows.

**RED tests**

- initial load with 1000 mocked targets does not call credential status for
  all targets;
- page change loads status for new page only;
- stale status response for prior page is ignored;
- bulk action count matches visible selected rows only.

**Commit**

```bash
git commit -m "feat(credentials): paginate query credential inventory"
```

## F4. Credential Detail Modal/Drawer

**Design**

- Row click selects row.
- Manage button opens edit modal/drawer.
- Desktop may use side drawer or modal; small screens use full-screen modal or
  drawer.
- Remove inline table expansion and bottom-of-page detail form.
- Preserve save/delete success and stale-target guards.

**RED tests**

- row click highlights but does not append a bottom form;
- Manage opens modal/drawer;
- Esc closes modal/drawer;
- focus trap keeps keyboard inside;
- mobile viewport uses full-screen surface;
- delete success feedback appears in modal/drawer;
- stale save/delete response does not update a newly selected target.

**Commit**

```bash
git commit -m "feat(credentials): move target credential editing to modal drawer"
```

## F5. E2E And Visual QA

**E2E**

Run against real backend and dedicated query MySQL:

```bash
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
```

Required scenarios:

- query target search finds `Local MySQL Query Dev`;
- URL target id selects the ready target;
- ready target can run SELECT/SHOW/DESCRIBE;
- locked target shows one primary blocker, not a full governance column;
- credential table pagination is visible;
- Manage opens modal/drawer;
- no DSN/password fields appear.

**Visual QA**

Capture or inspect:

- `/query` desktop with ready target;
- `/query` desktop with locked target;
- `/query` mobile/tablet drawer behavior;
- `/settings/query-credentials` desktop table + modal/drawer;
- `/settings/query-credentials` mobile full-screen modal/drawer.

**Commit**

```bash
git commit -m "test(query): cover scalable workbench and credential flows"
```

## D1. Documentation And Evidence

Update backend docs after implementation:

```text
docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
docs/superpowers/notes/2026-07-09-phase-38h-query-workbench-scalable-ia-reset-evidence.md
docs/quality-baseline.md
docs/releases/candidates/2026-05-26-controlhub-release-readiness-summary.md
```

Document:

- backend pagination/search contract;
- frontend IA reset;
- 1000-target or large-fixture proof;
- E2E and visual QA evidence;
- remaining known gaps, if any.

**Commit**

```bash
git commit -m "docs: record phase 38h query workbench scalable ia evidence"
```

## Final Verification

Backend:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Frontend:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
```

Review:

- request UI/UX adversarial review before final report;
- fix every P1/P2 finding;
- rerun targeted tests and full gates after fixes.

## Final Report Requirements

Include:

- backend branch/worktree and commits;
- frontend branch/worktree and commits;
- changed files summary;
- backend API contract summary;
- proof that target list and credential admin are bounded at scale;
- real E2E result tied to final commits;
- visual QA summary;
- CI status if pushed;
- cleanup result;
- final git status for both repos;
- remaining P1/P2 findings, must be none or blocked with evidence;
- scope confirmation.
