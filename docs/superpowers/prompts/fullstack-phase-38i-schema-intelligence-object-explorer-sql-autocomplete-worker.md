ultrawork

Working directories:

- Backend repo: `/Users/fan/GolangProjects/ControlHub`
- Frontend repo: `/Users/fan/JsProjects/ControlHub`
- Tabularis reference repo: `/Users/fan/JsProjects/tabularis`

Objective:

Implement Phase 38I Schema Intelligence, Object Explorer, and SQL Autocomplete
end to end. Deliver governed MySQL/TiDB schema metadata APIs, a lazy bounded
Object Explorer, a keyboard Quick Navigator, per-worksheet database context,
and schema-aware CodeMirror completion whose visible vocabulary matches the
existing read-only SQL guard.

This is not a browser DBeaver clone and not a placeholder UI phase. The browser
must never receive database secrets or connect directly to a database. Backend
authorization, credential resolution, environment policy, DSN host/port
binding, parameterized metadata reads, audit, timeout, and response limits are
the source of truth.

Required reading:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-09-phase-38h-query-workbench-scalable-ia-reset.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-08-phase-38f-query-workbench-sql-editor-foundation.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-06-24-phase-37h-dedicated-query-e2e-mysql-fixture-evidence.md`

Reference behavior, do not copy architecture blindly:

- `/Users/fan/JsProjects/tabularis/src/utils/autocomplete.ts`
- `/Users/fan/JsProjects/tabularis/src/hooks/useSqlAutocompleteRegistration.ts`
- `/Users/fan/JsProjects/tabularis/src/components/modals/QuickNavigatorModal.tsx`
- `/Users/fan/JsProjects/tabularis/src/utils/quickNavigator.ts`
- `/Users/fan/JsProjects/tabularis/src/components/layout/ExplorerSidebar.tsx`
- `/Users/fan/JsProjects/tabularis/src/components/layout/sidebar/SidebarTableItem.tsx`

Phase 38H dependency gate:

Before editing anything, prove that Phase 38H has completed its finishing flow
in both repos. Backend `main` must contain paged/searchable `GET /query-targets`
with `pageInfo`; frontend `main` must contain the final bounded target navigator,
two-region workbench explorer, and responsive credential modal/drawer. Both
root repos must be clean and synchronized. Do not base implementation on the
temporary Phase 38H feature worktrees. If this gate is not satisfied, stop and
return a dependency-blocked report with exact git/router/OpenAPI evidence.

After the gate passes, create or verify these isolated worktrees:

- Backend worktree: `/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-38i-schema-intelligence`
- Backend branch: `phase-38i-schema-intelligence`
- Frontend worktree: `/Users/fan/JsProjects/ControlHub/.worktrees/phase-38i-schema-intelligence`
- Frontend branch: `phase-38i-schema-intelligence`

Keep backend and frontend commits separate. Do not implement in dirty main
worktrees. Do not delete unrelated untracked files.

Specialist-agent requirements before editing:

- Use `explore` in both repos to locate current symbols, tests, and Phase 38H
  contracts.
- Use `librarian` to verify the installed CodeMirror SQL/autocomplete API and
  the named Tabularis behavior. Local installed package/source versions are
  authoritative.
- Use `metis` to identify at most five hidden assumptions, scope-creep risks,
  and likely failure modes.
- Consult `oracle` for a read-only review of the shared target-access boundary,
  metadata API/cache/audit design, and Object Explorer/Quick Navigator UX.
- Ask `momus` to review the written implementation plan for reference validity,
  task startability, concrete QA scenarios, and contradictions.
- Resolve blockers from these reviews before editing.

Oracle, Momus, Metis, Librarian, Explore, and Multimodal Looker are read-only
reviewers. Do not ask them to write files or implement.

GitNexus requirements:

- Before modifying any existing function, class, or method, run upstream impact
  analysis and report its blast radius.
- Warn before proceeding on HIGH or CRITICAL impact.
- Before every commit series closeout, run detect-changes against `main` and
  inspect affected execution flows.
- Restore generated GitNexus-only AGENTS/CLAUDE stats diffs unless explicitly
  part of scope.

Hard boundaries:

- No SQL guard behavior change.
- No new query engine.
- No browser database connection or secret API.
- No DSN/password/database username in browser state, requests, responses,
  display, cache keys, audit rows, errors, or logs.
- No `actorUserId` request field.
- No credential edit controls on `/query`.
- No schema persistence migration or browser persistence.
- No routines, triggers, grants, DDL definitions, schema mutation, ER diagram,
  Visual Explain, visual query builder, saved query, export, approval, JIT,
  notebook, AI, or MCP implementation.
- No Monaco migration; extend the existing CodeMirror 6 editor.
- No fake backend as final E2E evidence.
- No CI workflow change, push, tag, release, or deploy.
- No AI co-author trailer.

Required backend outcome:

1. Add these fresh-bearer read routes and verify every path from router,
   OpenAPI, frontend service, and E2E proxy rather than from memory:

   - `GET /query-targets/{id}/schema/databases`
   - `GET /query-targets/{id}/schema/objects`
   - `GET /query-targets/{id}/schema/object-details`

2. Use the exact bounded query/response contract in the Phase 38I spec,
   including Phase 38H `pageInfo`, table/view kinds, object details, and explicit
   truncation flags. Keep `QueryTarget.SchemaPreview` empty/deprecated for
   compatibility; do not stuff live schema into the query-target list.

3. Extract the smallest shared governed target-access resolver so query
   execution and schema introspection enforce the same target lookup, engine,
   credential metadata, environment policy, server-side resolution, and DSN
   host/explicit-port binding. Add characterization tests before changing the
   execution path. Preserve SQL guard, history, audit, timeout, and response
   behavior exactly.

4. Implement fixed parameterized MySQL/TiDB `information_schema` queries for
   databases, tables/views, columns, indexes, and foreign keys. Never interpolate
   browser values into SQL. Escape LIKE wildcards. Use deterministic ordering,
   pagination, one read-only transaction/connection per request, production
   timeout tightening, and response caps.

5. Add an in-process bounded schema cache with five-minute positive TTL,
   30-second negative TTL, refresh bypass, capacity eviction, injected clock,
   and no DSN/password in keys. Authorization and audit still run on cache hits.

6. Audit every valid-target metadata request using fixed event/result strings.
   Do not store database/object names or secrets. Fail controlled rather than
   report unaudited success.

7. Extend the dedicated query MySQL fixture idempotently with two application
   databases, tables, one view, PK, composite/secondary index, and FK. Keep the
   user SELECT-only on dedicated fixture databases. Preserve mode 0600 env file,
   leak-free output, input validation, and separation from ControlHub metadata
   DB.

8. Add unit, API, integration, OpenAPI, and fuzz tests for auth, bounds,
   parameterization, wildcard/special identifiers, policy/binding failures,
   object grouping, cache behavior, audit failure, timeout, caps, and no-secret
   invariants.

Freeze and commit the backend contract before finalizing dependent frontend
types. Frontend may scaffold independent UI/tests in parallel, but it cannot be
reported final until reconciled against committed backend router and OpenAPI.

Required frontend outcome:

1. Add exact schema wire types, service calls, controlled errors, and one shared
   ephemeral metadata store for Object Explorer, Quick Navigator, and
   autocomplete. Use five-minute positive TTL, 30-second negative TTL, max 50
   object-detail entries, max five concurrent detail requests, in-flight
   dedupe, abort/request-generation stale guards, and no browser persistence.

2. Replace the locked schema placeholder inside the final Phase 38H on-demand
   explorer. Do not reintroduce a permanent third column. Load one database page
   on open, one object page on database expand, and one object detail on object
   expand. Distinguish tables/views and show compact read-only Columns, Keys,
   Indexes, and Foreign Keys groups. Include loading, empty, error, retry,
   refresh, truncation, search, and bounded load-more states.

3. Keep object lists server-searched/paginated. Cap accumulated visible objects
   at 500 and direct users to search beyond that. Large schemas must not produce
   an all-object DOM or all-column request fan-out.

4. Extend local worksheet state with `activeDatabase`, isolated per worksheet.
   Database changes affect explorer/completion context only and must never emit
   or execute `USE`. Insert fully qualified backtick identifiers for objects
   outside the active database.

5. Add accessible `Cmd/Ctrl+P` Quick Navigator for the active target. Search
   databases/tables/views through bounded server requests; include columns only
   from already loaded details. Up/Down/Enter/Escape, focus trap, visible focus,
   reveal behavior, and explicit insert action are required. Selection must not
   auto-execute SQL.

6. Enable CodeMirror autocomplete through a controlled override. Suggest loaded
   databases/tables/views, qualified objects, referenced-table columns, and
   `table.`/`alias.` columns. Add explicit Ctrl+Space. Insert correctly quoted
   identifiers. Keep each completion source scoped to worksheet, target, and
   database.

7. Completion keywords must match read-only product semantics. Include SELECT,
   SHOW, DESCRIBE/DESC, EXPLAIN and safe read clauses/functions. Do not suggest
   INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, TRUNCATE, CALL, SET, USE, GRANT,
   transactions, or locking clauses. Autocomplete is UX only; backend guard
   remains authoritative.

8. Network/metadata failure must degrade to editing plus loaded/keyword
   completions. It must not block normal query execution or leak raw errors.

9. Preserve Phase 38H target pagination/IA, Phase 38F worksheet isolation and
   editor preferences, run/format shortcuts, credential admin modal/drawer, no
   Workbench credential controls, dark/high-contrast readability, and mobile
   behavior.

TDD requirement:

- Add a failing test before each meaningful behavior change.
- Tests must encode why bounded loading, stale-write rejection, target binding,
  audit, and no-secret behavior matter.
- Do not delete, weaken, or skip existing required tests.
- Do not use frontend-only mocks as final full-stack proof.

Backend gates:

```bash
git diff --check
go test -count=1 ./...
go test -race ./internal/service -run 'QuerySchemaCache'
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
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Real E2E requirement:

You must actively start the dedicated query MySQL fixture and backend using the
documented Phase 37H commands. Source `.query-e2e-mysql.env` without printing
it, seed the dev target, verify `/health`, then run final frontend E2E through
the real proxy:

```bash
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
```

Do not report `backend unavailable` or `E2E not run` until startup was attempted,
the failure was captured, and no safe workaround exists. Final E2E must prove
real database/object/detail loading, server search, Quick Navigator, visible
table and alias-column completion, a successful guarded SELECT using completed
SQL, existing SHOW/DESCRIBE behavior, locked-target rejection, unsafe-SQL
rejection, worksheet isolation, and credential admin regression. Report exact
pass/fail/skip counts. No silent skips.

Visual QA requirement:

- Capture desktop ready-target Object Explorer and object detail.
- Capture large-object search/pagination behavior.
- Capture completion menu in light, dark, and high-contrast themes.
- Capture 375px mobile explorer drawer and Quick Navigator.
- Capture locked-target schema-not-allowed state.
- Capture credential admin regression state.
- Ask `multimodal-looker` to review hierarchy, clipping, density, focus,
  keyboard discoverability, CJK text, contrast, and whether editor/results
  remain the dominant work surface.

Autonomous closure requirement:

Do not hand this back after the first implementation pass. After implementation
and initial full gates:

1. Ask Momus for read-only adversarial diff review across both repos.
2. Ask Oracle for final architecture, security-boundary, cache/privacy, and
   UX/IA re-review.
3. Explicitly inspect unbounded fan-out, stale async writes, SQL interpolation,
   DSN/cache/log leaks, audit gaps, keyword/guard mismatch, target/worksheet
   context drift, and accessibility.
4. Fix every P1/P2 finding with regression tests.
5. Rerun targeted tests, complete backend/frontend gates, final real E2E, and
   affected visual QA.
6. Repeat review and repair until no P1/P2 findings remain.

Only return when both branches are ready for human review or genuinely blocked
with evidence. `Code written`, `tests pass`, or one review round is not closure.

Commit requirements:

- Use focused conventional commits, separated by backend/frontend/docs.
- Stage explicit files only and run cached diff checks before each commit.
- Update required file headers and module README files.
- Add Phase 38I evidence and update quality baseline only after final evidence
  exists.
- Every implementation worktree must be clean before a Final Report.
- Do not push, merge, open a PR, tag, release, or deploy.
- Do not add AI co-author attribution.

Cleanup requirements:

- Stop only services/processes started by this task.
- Run `make query-e2e-mysql-down` after final E2E unless explicitly asked to
  keep it running.
- Confirm task-owned ports are free.
- Preserve unrelated untracked files and keep visual evidence out of commits
  unless the plan explicitly requires it.

Final report must include:

- Phase 38H dependency-gate proof and both base commits;
- backend/frontend worktrees, branches, final commit hashes, and commit lists;
- changed files grouped by backend/frontend/docs;
- API paths verified from router, OpenAPI, frontend service, and E2E proxy;
- schema contract, runtime target-access, parameterization, cache, audit, caps,
  timeout, and no-secret proof;
- Object Explorer request-count/bounds proof at large schema size;
- Quick Navigator and per-worksheet database behavior;
- autocomplete allowed/forbidden keyword matrix and alias-column proof;
- backend/frontend full gate matrix;
- final real E2E command, environment, commit hashes, and exact counts;
- visual QA evidence paths and reviewer result;
- specialist review ledger: Metis/Oracle/Momus/Librarian/Explore/Multimodal
  used, findings found, findings fixed;
- remaining P1/P2 findings, which must be none or an evidence-backed blocker;
- GitNexus impact/detect-changes summary and caveats;
- cleanup and final git status for both repos;
- next-phase input and explicitly deferred ER/Visual Explain/other engines;
- scope confirmation: no SQL guard change, no new engine, no DSN/password/user
  browser state/request/response/display/cache/log, no actorUserId, no Workbench
  credential editing, no schema persistence, no routines/triggers/grants/DDL
  mutation, no ER/Visual Explain/VQB/saved query/export/approval/JIT/AI/MCP, no
  fake final E2E, no push/tag/release/deploy, and no AI co-author.
