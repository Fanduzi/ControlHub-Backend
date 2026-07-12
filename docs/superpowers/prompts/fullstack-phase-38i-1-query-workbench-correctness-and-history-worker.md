ultrawork

Implement Phase 38I.1 Query Workbench Correctness And History end to end. You
own the complete engineering loop: reproduce, write RED tests, implement,
review, fix your own P1/P2 findings, verify against real services, commit, and
report. Do not ask the requester to run a review/fix loop for you.

Working directories:

- Backend: `/Users/fan/GolangProjects/ControlHub`
- Frontend: `/Users/fan/JsProjects/ControlHub`

Required reading before changes:

- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/specs/2026-07-12-phase-38i-1-query-workbench-correctness-and-history.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/plans/2026-07-12-phase-38i-1-query-workbench-correctness-and-history.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md`
- `/Users/fan/GolangProjects/ControlHub/docs/superpowers/notes/2026-07-12-phase-38i-schema-intelligence-remediation-evidence.md`

Objective:

1. Fix the Object Explorer crash caused by schema object-detail array fields
   serializing as JSON `null` while the UI calls `.length`.
2. Fix Query History's false “No executions yet” state on first visit without
   reintroducing mount-time history/worksheet races.
3. Fix the cross-user history-preview exposure and make history useful by
   showing actor, timing, rows, status, statement preview, and safe errors.

Fixed decisions. Do not ask the requester to choose these again:

- Ordinary non-admin users see only their own execution history for a target.
- Admins see all execution history for that target.
- The history UI uses `actor.displayName` only. Do not display actor email or
  numeric actor ID as primary UI.
- Missing users render as `Unknown user` through a controlled API projection.
- Unknown targets return controlled `404` for history.
- History remains readable when a target is currently locked/unresolved; do not
  require a ready credential just to inspect prior audit records.
- Detail fetch errors show a localized per-object error with Retry. They are not
  silently presented as empty metadata.

Create isolated worktrees from clean, current `main` only:

- Backend: `/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-38i-1-query-history-contract`
  on branch `phase-38i-1-query-history-contract`
- Frontend: `/Users/fan/JsProjects/ControlHub/.worktrees/phase-38i-1-query-workbench-correctness`
  on branch `phase-38i-1-query-workbench-correctness`

Stop only if `main` is missing Phase 38I or an unrelated dirty change blocks
safe worktree setup. Otherwise work autonomously. Preserve unrelated worktrees,
untracked files, and root-checkout changes.

Mandatory pre-edit investigation:

1. Prove the exact current API path from backend router, OpenAPI, frontend
   service, and E2E proxy.
2. Trace `ObjectDetailResponse`, `toModelObjectDetail`, JSON serialization,
   schema store/service, and explorer detail rendering.
3. Trace history from route through handler/service/repository, auth-context
   helpers, users table/repository, worksheet state, refresh call sites, and
   history panel.
4. Before modifying every existing function/class/method, run required
   GitNexus upstream impact analysis. Surface HIGH/CRITICAL risk, then choose
   the smallest safe implementation.
5. Use read-only specialist review to challenge authorization scope, stale async
   writes, null-array boundaries, and mobile/a11y behavior. Write the concrete
   plan before implementation; use Momus to critique it. Resolve all blockers
   yourself before coding.

Backend requirements:

1. Restore OpenAPI's required-array invariant for successful object-detail
   responses. `columns`, `indexes`, `foreignKeys`, index columns, FK columns,
   and referenced columns serialize as `[]`, never `null`.
2. Add RED JSON serialization plus API/integration tests using an object that
   has empty index/FK metadata. Preserve the existing governed metadata path,
   response caps, cache, audit, and no-secret behavior.
3. Keep the verified history route `GET /query-targets/{id}/executions` and
   fresh bearer auth. Read actor ID and role from request context. Validate
   target existence without credential resolution/readiness gating.
4. Implement explicit history visibility scope: admin gets all target rows;
   non-admin gets `target_resource_id = ? AND actor_user_id = ?` only.
5. Add a parameterized `LEFT JOIN users` or equivalent bounded projection and
   return nested `actor: { displayName }`; fallback to `Unknown user`. Do not
   return email, secrets, raw driver errors, result rows, or introduce an
   `actorUserId` request field.
6. Update model, handler, service, repository, OpenAPI, tests, and frontend
   contract coherently. Preserve shared `pageInfo` pagination and deterministic
   ordering. Do not invent client-side totals.
7. Freeze and commit backend contract work before reporting frontend final.

Frontend requirements:

1. Normalize object-detail wire data at the service/store boundary so missing or
   `null` top-level/nested collections become empty arrays. Rendering must also
   be defensive.
2. Model per-object detail `idle/loading/ready/error`; visible localized Retry
   on error; valid zero metadata renders as ready and zero, never error/crash.
3. Replace ambiguous history `[] + historyLoading=false` with a worksheet-local
   state machine: `idle`, `loading`, `ready`, `error`, plus error, bound target,
   and a history-specific generation/token.
4. Do not fetch history on initial page mount. On first History-tab opening for
   an executable idle/error worksheet, fetch first-page history for that
   worksheet's bound target. Refresh the originating worksheet after a
   successful run.
5. Reject stale history responses by worksheet ID, target ID, and independent
   history generation. Never couple independent history writes to execution
   request IDs. Switching or retargeting worksheets must not leak rows.
6. Render a responsive/localized history table with executed time (relative +
   absolute accessible label/tooltip), actor display name, status, statement
   preview, formatted row count, formatted duration, and safe error detail.
   Execution ID and raw actor ID are not primary columns.
7. Preserve existing target selection, worksheet isolation, SQL guard behavior,
   dark/high contrast, mobile Object Explorer Sheet, credential UI boundaries,
   and exact one-shot E2E error policy.

Hard boundaries:

- No SQL guard change, query-engine addition, or browser DB connection.
- No DSN/password/database username/secret in browser state, request, response,
  display, cache key, audit, error, or log.
- No credential edit controls in `/query` and no credential secret write API.
- No schema persistence/browser persistence.
- No SHOW CREATE/DDL definition, arbitrary information-schema browsing,
  context menu/right-click feature, ER diagram, Visual Explain, export, saved
  queries, approval/JIT, notebook, AI, MCP, visual builder, or editable grid.
- No migrations unless source inspection proves existing `users` or execution
  fields are insufficient; they are expected to be sufficient.
- No fake backend as final E2E proof. No CI workflow, push, merge, tag,
  release, deployment, or AI co-author trailer.

TDD and review loop:

1. Write failing tests before each meaningful backend/frontend behavior change.
2. Run targeted tests after each smallest fix.
3. Perform an adversarial self-review after implementation for: nil arrays,
   nested arrays, OpenAPI shape, actor scoping, target existence, deleted users,
   first History load, retry, concurrent requests, target change, worksheet
   switch, post-run refresh, localization, mobile, and secrets.
4. Fix every P1/P2 you find without asking for another review cycle.
5. Perform one final independent review of the exact final diff. If it finds a
   P1/P2, fix and repeat relevant gates. Only report clean if no P1/P2 remains.

Required gates:

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
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Real E2E is mandatory. Start the dedicated query MySQL fixture and backend from
the documented commands, source credentials without printing secrets, verify
health, and run:

```bash
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
```

Seed/reuse real historical records before opening History. E2E must prove: an
object with empty metadata does not crash; first History open displays existing
records before a new run; normal/admin visibility policy where harness accounts
permit; actor/duration/rows/status visibility; history retry and worksheet
isolation; existing guarded query, unsafe SQL rejection, locked target, schema,
and credential regressions. Report exact pass/fail/skip counts. If live E2E is
blocked, attempt startup and diagnosis; do not call the phase complete.

Before each commit, run GitNexus `detect_changes` against `main`, stage only
intended files, run staged diff checks, and inspect affected flows. Make focused
backend/frontend commits separately. Add a backend evidence note only after all
final evidence is true.

Final report must include:

- worktree/branch/base/final commit hashes;
- exact changed files and API-path proof;
- serialization proof for every required empty array level;
- enforced admin/non-admin history policy and actor projection;
- first-open History and stale-write proof;
- all gate and exact E2E counts;
- final Git status for both worktrees;
- remaining P1/P2 (or explicitly none); and
- negative scope confirmation for every hard boundary.

Do not claim completion unless both implementation worktrees are committed,
clean, all gates pass, and real E2E has passed against the final committed
heads.
