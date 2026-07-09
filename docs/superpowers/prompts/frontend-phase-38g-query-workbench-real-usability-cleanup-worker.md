# Frontend Phase 38G Query Workbench Real Usability Cleanup Worker Prompt

claude code

Working directory: /Users/fan/JsProjects/ControlHub

Objective: implement Phase 38G Query Workbench Real Usability Cleanup. This phase must reduce placeholder UI and fix actual preview usability blockers after Phase 38F.

Required reading:

- /Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-08-phase-38g-query-workbench-real-usability-cleanup.md
- /Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-07-08-phase-38g-query-workbench-real-usability-cleanup.md
- /Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
- /Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md

Bytebase reference files to inspect before design/code:

- /Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/SQLEditorLayout.tsx
- /Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/ConnectionPane/ConnectionPane.tsx
- /Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/ConnectionPane/TreeNode/DatabaseNode.tsx
- /Users/fan/GolangProjects/bytebase/frontend/src/react/components/sql-editor/StandardPanel/SQLEditor.tsx

User-observed preview problems to fix:

- query target selector still feels like a dropdown, not a database IDE connection navigator;
- SQL editor is unreadable in dark mode;
- result table/output readability in dark mode is suspect and must be verified;
- SQL editor height cannot be resized and does not remember the user’s chosen size;
- credential settings row click opens detail at the bottom of the page, which feels like no response;
- too many primary buttons are placeholders for features that are not implemented.

Hard boundaries:

- Frontend product code only, except backend docs evidence after frontend verification.
- No backend product code edits.
- No SQL guard changes.
- No new query engines.
- No saved query/export/approval/JIT implementation.
- No worksheet persistence except editor height and optional editor theme preference.
- No credential edit controls on /query.
- No DSN/password browser state, request body, response display, or logs.
- No actorUserId in requests.
- No fake backend final E2E.
- No tag/release/deploy.
- No AI co-author.
- Do not delete unrelated untracked files.

Autonomous closure requirement:
Do not hand this back after the first implementation pass. After implementation, run the full gates, then perform an adversarial self-review of the diff focused on P1/P2 issues. If omo-style subagents are available in this environment, ask momus for read-only adversarial diff review and fix every P1/P2 finding. If oracle is available, ask oracle to review the UX/IA tradeoffs before coding or before final report because this phase changes workbench information architecture. Rerun targeted tests and full gates after fixes. Only return when no P1/P2 findings remain or when genuinely blocked with evidence.

Real E2E requirement:
You must attempt to start the backend and dedicated query MySQL fixture using documented commands. Do not report “backend unavailable” or “E2E not run” until you have tried to start backend/fixture, captured failure evidence, and confirmed there is no safe workaround.

Implementation requirements:

1. Create isolated frontend worktree/branch:
   - branch: phase-38g-query-workbench-real-usability-cleanup
   - worktree under /Users/fan/JsProjects/ControlHub/.worktrees/

2. Implement CodeMirror readability:
   - app dark mode editor must be dark and readable;
   - light mode remains readable;
   - high contrast preference is supported internally or exposed if cheap;
   - cursor, selection, gutter, active line, line numbers, and SQL tokens are visible.

3. Implement resizable SQL editor:
   - vertical resize handle;
   - clamp height between safe min/max;
   - persist height in localStorage;
   - invalid stored height ignored;
   - no worksheet SQL persistence.

4. Replace target dropdown with connection navigator:
   - grouped by environment and cluster;
   - search by display name, resource name, engine, environment, host, port, cluster, readiness;
   - ready targets prioritized;
   - active worksheet target highlighted;
   - filters inside navigator;
   - active connection summary visible even if filters exclude it;
   - schema/governance/editor context stays synchronized to active worksheet target.

5. Fix credential settings master-detail:
   - operations table row click opens visible detail inspector beside table on desktop;
   - selected row highlighted;
   - small screens use drawer/modal or immediate detail surface;
   - detail form must not appear at page bottom after a long list;
   - stale-target guards and save/delete feedback preserved.

6. Reduce placeholder primary actions:
   - Run and Format remain primary because they work;
   - Explain, Export, Save sheet, Access must not occupy primary toolbar space unless implemented end-to-end;
   - unimplemented actions may be hidden or demoted to compact disabled secondary UI;
   - governance panel should show primary blocker and compact status, not large education blocks.

7. Update backend docs after frontend verification:
   - /Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-07-query-workbench-preview-issues-status.md
   - add /Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-08-phase-38g-query-workbench-real-usability-cleanup-evidence.md
   - docs commit can be separate after frontend implementation evidence is final.

Verification gates from frontend worktree:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit
npm run lint
npm run test
npm run build
```

Real E2E setup from backend repo:

```bash
cd /Users/fan/GolangProjects/ControlHub
make query-e2e-mysql-up
export DATABASE_DSN="$(grep '^DATABASE_DSN=' .env | sed 's/^DATABASE_DSN=//')"
set -a
. ./.query-e2e-mysql.env
set +a
QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO make seed-query-dev-target
```

Start backend on :8080 with DATABASE_DSN, JWT_SECRET, and CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO set. Do not print DSNs. Confirm:

```bash
curl http://localhost:8080/health
```

Run real E2E:

```bash
cd /Users/fan/JsProjects/ControlHub/.worktrees/phase-38g-query-workbench-real-usability-cleanup
npm run test:e2e -- --grep "Query Workbench"
npm run test:e2e -- --grep "query credential"
```

Manual/visual verification:

- inspect /query in dark mode in a real browser;
- type SQL and confirm text is readable;
- run SELECT 1 and confirm result table is readable;
- resize editor, reload, confirm height persists;
- inspect /settings/query-credentials and confirm row click opens visible inspector without scrolling to the bottom.

Cleanup:

- stop backend :8080;
- run make query-e2e-mysql-down from backend repo;
- confirm :8080 is free and controlhub-query-e2e-mysql is gone.

Commit constraints:

- Use focused conventional commits.
- No AI co-author.
- Do not push/tag/release/deploy.
- If you must make backend docs evidence commit, keep it docs-only.

Final report must include:

- branch/worktree;
- commits;
- changed files summary;
- preview issues fixed;
- real E2E results;
- dark-mode manual/visual verification result;
- backend/fixture cleanup result;
- final frontend/backend git status;
- remaining P1/P2 findings: must be none or explicitly blocked with evidence;
- scope confirmation:
  no backend product edits, no SQL guard changes, no DSN/password browser state, no actorUserId, no Workbench credential edit controls, no saved query/export/approval/JIT implementation, no tag/release/deploy, no AI co-author.
