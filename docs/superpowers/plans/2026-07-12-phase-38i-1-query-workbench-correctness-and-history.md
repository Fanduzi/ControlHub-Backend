# Phase 38I.1 Query Workbench Correctness And History Plan

## Goal

Close the two observed P1 product failures before beginning 38J: Object
Explorer must tolerate valid empty metadata, and History must load real prior
records on first use while enforcing safe actor visibility.

## Inputs

- Specification:
  `docs/superpowers/specs/2026-07-12-phase-38i-1-query-workbench-correctness-and-history.md`
- Root-cause research: current `main` code in
  `internal/service/query_schema_service.go`,
  `components/query/query-object-explorer.tsx`,
  `components/query/query-editor-shell.tsx`, and
  `components/query/query-history-panel.tsx`.
- Baselines: backend and frontend `main` must contain the merged Phase 38I
  heads and be clean before worktree creation.

## Worktrees

| Repo | Worktree | Branch |
| --- | --- | --- |
| Backend | `/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-38i-1-query-history-contract` | `phase-38i-1-query-history-contract` |
| Frontend | `/Users/fan/JsProjects/ControlHub/.worktrees/phase-38i-1-query-workbench-correctness` | `phase-38i-1-query-workbench-correctness` |

Do not implement in either root `main` checkout. Preserve unrelated worktrees
and untracked files. Backend owns and freezes wire/API changes first; frontend
may prepare isolated normalizer and state-machine tests in parallel, but final
frontend verification must use the frozen backend contract.

## Execution Plan

### B0. Freeze Current Contract And Reproduce — backend

1. Verify router, OpenAPI, handler, service, repository, frontend service, and
   E2E proxy routes rather than relying on remembered paths.
2. Add RED serialization/API tests for an object detail with empty top-level and
   nested collections. The original behavior must demonstrate `null` failure.
3. Add RED history visibility tests: multiple actors on one target, non-admin
   isolation, admin aggregate access, missing target, and actor fallback.
4. Run GitNexus impact analysis before each existing symbol change and surface
   HIGH/CRITICAL risk before editing.

### B1. Restore Object-Detail Array Invariant — backend

1. Normalize `ObjectDetailResponse` at the backend model/service boundary so all
   declared collections, including nested index/FK columns, are non-nil slices.
2. Preserve existing fixed metadata query/caching/auditing paths and controlled
   errors.
3. Add serialization and integration assertions that the wire body has `[]`.

### B2. Secure And Enrich History Contract — backend

1. Read actor ID/role from established auth context helpers in the handler.
2. Verify target existence without resolving credentials or gating on current
   readiness.
3. Introduce an explicit service/repository scope, parameterized SQL, and
   stable actor display projection using `LEFT JOIN users` or a bounded
   equivalent.
4. Scope ordinary users to their own rows; allow admins the target's complete
   history. Return `Unknown user` for a missing user row.
5. Update model, handler, OpenAPI, and tests atomically. Retain shared
   pagination semantics.
6. Commit the backend contract in focused commits before frontend wire work is
   considered final.

### F0. Lock In Frontend Failure Tests — frontend

1. Add a failing service/store/component case for `null` object detail arrays,
   nested null arrays, visible error/retry, and zero-count ready state.
2. Add failing worksheet history tests proving no mount fetch, first History
   click fetch, explicit loading/error/empty states, retry, and isolation across
   target/worksheet changes.
3. Add a view test for actor, status, statement, rows, duration, and safe error
   display in EN/ZH and narrow viewport behavior.

### F1. Harden Object Explorer — frontend

1. Normalize current schema detail wire data at the service/store boundary.
2. Make rendering robust even if a future wire regression bypasses the
   normalizer.
3. Represent per-object detail failure explicitly with localized Retry.
4. Do not add definition SQL, metadata browsing breadth, context menus, or new
   object-inspector product scope.

### F2. Implement Per-Worksheet History State — frontend

1. Replace ambiguous `historyLoading` semantics with explicit history state,
   error, target binding, and independent generation/token.
2. Trigger first load only when History is opened for an idle/error executable
   worksheet; preserve no mount request.
3. Continue post-run refresh for the originating worksheet, but do not tie a
   history write to execution request ID.
4. Invalidate state on a worksheet's target change; reject late responses whose
   worksheet/target/generation no longer match.
5. Adapt frontend types and service to the frozen backend actor shape and
   `pageInfo` contract.

### F3. Make History Readable — frontend

1. Render actor display name, readable execution time, semantic status,
   statement preview, formatted rows/duration, and safe error information.
2. Do not make execution ID or numeric actor ID a primary column.
3. Keep responsive/mobile behavior concise and localized; do not introduce an
   all-history fan-out or client-side fake pagination.

### T0. Cross-Repo Verification And Review

1. Reconcile frontend against committed backend OpenAPI and route evidence.
2. Run backend and frontend required gates.
3. Start real backend and dedicated query MySQL fixture. Seed stable history
   fixtures before Playwright starts; use real auth and proxy paths.
4. Run focused E2E plus affected query/credential regression specs; exact
   counts required.
5. Conduct self-review across API authorization, null-array wire shape,
   cross-worksheet races, actor leakage, and mobile/keyboard UX. Fix all P1/P2
   findings autonomously, rerun relevant tests, then do one final review. Do
   not hand an iterative review/fix loop back to the requester.
6. Run GitNexus `detect_changes` against `main` before every commit closeout.
7. Document evidence in a new backend note only after implementation proves the
   final results. Do not change release/tag/deploy state.

## Required Gates

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

Real E2E:

```bash
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
```

## Completion Evidence

Report:

- exact backend/frontend commit hashes and clean status;
- API path evidence from router, OpenAPI, frontend client, and E2E proxy;
- the actual policy enforced for admin/non-admin history;
- top-level and nested `[]` serialization proof;
- first-open history proof without an execution; and
- exact gate/E2E pass/fail/skip counts.

No push, merge, tag, release, or deployment is part of this implementation plan.
