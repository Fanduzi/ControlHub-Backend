# Fullstack Phase 38H Query Workbench Scalable IA Reset Worker Prompt

claude code

Working directories:

- Backend repo: `/Users/fan/GolangProjects/ControlHub`
- Frontend repo: `/Users/fan/JsProjects/ControlHub`

Objective:
Implement Phase 38H Query Workbench Scalable IA Reset. This is not another
frontend polish pass. Fix the scaling and information architecture problem:
backend must support paged/searchable query targets; frontend must stop showing
all connections and stop dedicating a permanent right column to governance.

Required reading:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-09-phase-38h-query-workbench-scalable-ia-reset.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-07-09-phase-38h-query-workbench-scalable-ia-reset.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-08-phase-38g-query-workbench-real-usability-cleanup.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`

User complaints this phase must address:

- `/query` connection selector became a permanent left column showing too many
  connections. This does not scale to 1000 instances.
- Governance & Access should not occupy a permanent right column.
- Query Credential Management has no pagination.
- Clicking a credential target should not put an editor at the bottom or force a
  squeezed right panel on small screens; use modal/drawer behavior.

Hard boundaries:

- No SQL guard behavior changes.
- No credential secret write/read API.
- No DSN/password browser state, request body, response display, or logs.
- No `actorUserId` in requests.
- No credential edit controls inside `/query`.
- No saved query/export/approval/JIT implementation.
- No worksheet backend persistence.
- No CI workflow changes.
- No tag/release/deploy.
- No AI co-author.
- Do not delete unrelated untracked files.

Architecture requirements:

1. Backend `GET /query-targets` must support scalable search and pagination.
   Preserve the existing `{ items }` response envelope and add `pageInfo`.
   Required query params: `q`, `page`, `pageSize`, plus existing filters.

2. Frontend Query Workbench must use a two-region database IDE layout:
   collapsible left explorer plus dominant editor/results. Do not keep a
   permanent all-target list column and a permanent governance right column.

3. Target switching must be search-first and bounded:
   empty state shows recent/favorite/current page, not all connections;
   server search finds targets by name, host, engine, environment, cluster,
   port, and readiness; URL target id is supported.

4. Governance must be inline status:
   one blocker near editor/run controls, compact badges, and details on demand
   through popover/drawer. No right-side Governance & Access column.

5. Credential Management must be a paged operations table:
   page size 25/50/100, bounded credential-status fan-out for visible rows only,
   and edit via modal/drawer. Mobile/tablet must use full-screen modal/drawer.

Autonomous closure requirement:
Do not return after the first implementation pass. After implementation, run
the full gates, then perform adversarial review. If available, ask UI/UX review
agents to inspect the final diff and screenshots. Fix every P1/P2 finding and
rerun targeted tests plus full gates. Only return when no P1/P2 findings remain
or when genuinely blocked with evidence.

Worktree requirements:

- Create backend worktree:
  `/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-38h-query-target-pagination`
  branch `phase-38h-query-target-pagination`
- Create frontend worktree:
  `/Users/fan/JsProjects/ControlHub/.worktrees/phase-38h-query-workbench-scalable-ia-reset`
  branch `phase-38h-query-workbench-scalable-ia-reset`
- Keep backend and frontend commits separate.

Backend implementation expectations:

- Update models, query parsing, service/repository, OpenAPI, tests.
- Defaults: `page=1`, `pageSize=50`, max `pageSize=100`.
- Invalid page/pageSize returns 400.
- Add integration coverage proving bounded pages and search.
- No credential secrets in search or response.

Frontend implementation expectations:

- Update query target service/types for `pageInfo`.
- Replace all-target navigator with bounded server-backed switcher/explorer.
- Remove permanent governance right column.
- Keep active target header visible and non-duplicative.
- Credential admin loads only current-page credential statuses.
- Credential edit opens modal/drawer with focus/close/stale-response coverage.
- Add tests proving 1000-target scenario does not render/fan out everything.

Backend gates:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Frontend gates:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Real E2E requirement:
You must attempt to start the backend and dedicated query MySQL fixture using
documented commands. Do not report "backend unavailable" or "E2E not run" until
you have tried to start backend/fixture, captured failure evidence, and
confirmed there is no safe workaround.

Run final real E2E:

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/phase-38h-query-workbench-scalable-ia-reset
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
```

Visual QA requirement:

- Inspect `/query` desktop ready target.
- Inspect `/query` desktop locked target.
- Inspect `/query` mobile/tablet explorer and governance details behavior.
- Inspect `/settings/query-credentials` desktop paged table and modal/drawer.
- Inspect `/settings/query-credentials` mobile full-screen modal/drawer.

Commit requirements:

- Use focused conventional commits.
- No AI co-author trailers.
- Do not push.
- Do not tag/release/deploy.

Final report must include:

- backend and frontend worktrees/branches;
- commit list;
- changed files grouped by backend/frontend/docs;
- API contract summary;
- proof that Query Workbench and Credential Management are bounded at large
  target counts;
- real E2E result tied to final commits;
- visual QA summary;
- review loop summary and fixed P1/P2 findings;
- remaining P1/P2 findings, must be none or blocked with evidence;
- final git status for both repos;
- cleanup result;
- scope confirmation:
  no SQL guard changes, no DSN/password browser state/request/display/logs, no
  actorUserId, no Workbench credential edit controls, no saved
  query/export/approval/JIT implementation, no tag/release/deploy, no AI
  co-author.
