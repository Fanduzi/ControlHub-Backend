# Phase 38W-3 Template Execution Release Evidence

Date: 2026-08-06
Issue: #4, `38W-3: Execute personal templates through the governed query chain`

## Candidates

| Item | Backend | Frontend |
|---|---|---|
| Base SHA (`origin/main` merge-base) | `5ca76c076aabde9910f91270ffe5820501836001` | `2797c226337f9b205c78950fea2a14945d44a42d` |
| Candidate branch | `38w-3/execute-personal-templates` | `38w-3/execute-personal-templates` |
| Candidate SHA | `121b69c71d3c7171cf87415cf0c0264d787acb74` | `3cfc3ce7c321aa4faf697f0a52c565e667b313fd` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub/.worktrees/38w3-backend` | `/Users/fan/JsProjects/ControlHub/.worktrees/38w3-frontend` |
| Candidate commits | `0867ced` (feature), `121b69c` (review fix) | `773ecc1` (feature), `3cfc3ce` (review fix) |

## Merge And Push

- Merge type: fast-forward only (`git merge --ff-only 38w-3/execute-personal-templates`) in each root.
- Backend push range: `5ca76c0..121b69c` → `origin/main` = `121b69c71d3c7171cf87415cf0c0264d787acb74`. Local `main` == `origin/main` == merged SHA. No force push.
- Frontend push range: `2797c22..3cfc3ce` → `origin/main` = `3cfc3ce7c321aa4faf697f0a52c565e667b313fd`. Local `main` == `origin/main` == merged SHA. No force push.

## Root Dirty-Path Whitelist And Preservation

Preserved byte-for-byte before, during, and after merge/push (identical `git status --porcelain`).

- Backend root: `M CLAUDE.md`, `M advisor-plans/README.md`; untracked `AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md`, `docs/agents/`, `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`, `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`, `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`.
- Frontend root: `M AGENTS.md`, `M CLAUDE.md`; untracked `.codegraph/`, `AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`.
- None of the whitelisted paths are touched by either candidate diff; both fast-forward merges preserved them exactly.

## Candidate Gates (run against the exact candidate SHAs)

### Backend (candidate `121b69c`)

| Command | Result |
|---|---|
| `go build ./...` | PASS, exit 0 |
| `go vet ./...` | PASS, exit 0 |
| `go test ./... -count=1` | PASS: 1418 passed in 10 packages |
| `make openapi-validate` | PASS (`TestOpenAPIYAMLIsValid`) |
| `go test -tags=integration -count=1 ./internal/integration` and `make test-integration` | PASS: 202 passed, exit 0 (one earlier Testcontainers startup race re-run green) |
| `make test-openapi-fuzz` | PASS: 49 Schemathesis cases, 0 failures/errors (junit) |
| `check_three_level_doc.sh` | OK |

### Frontend (candidate `3cfc3ce`)

| Command | Result |
|---|---|
| `npx tsc --noEmit -p tsconfig.json` | PASS, no errors |
| `npm run lint` | 0 errors; 4 warnings all pre-existing in untouched `origin/main` (shell `1`, query-history-panel `1`, query-workbench.spec `1`, query-sql-format.test `2`) |
| `npm run test` | PASS: 1376 tests in 89 files |
| `npx next build` | PASS, 0 errors / 0 warnings |
| `npm run check:e2e-governance` | PASS (13 spec files scanned) |

## Real Chromium E2E

- Command: `PLAYWRIGHT_PROXY_TARGET=http://localhost:8082 npx playwright test` (full suite, `testDir ./e2e`).
- Totals: **152 passed, 0 failed, 0 skipped** (smoke + interaction + query-workbench, including 9 new Phase 38W-3 template tests).
- Focused block re-runs: `--grep "Phase 38W-3"` 9/9 green after each review fix; full suite re-run green at the final candidate SHA.
- Serving processes: backend binary built from the backend candidate worktree (`121b69c`), CWD = `/Users/fan/GolangProjects/ControlHub/.worktrees/38w3-backend`, served `:8082`; E2E API proxy `:8081` and Next dev server `:3100` started fresh by Playwright with `PLAYWRIGHT_PROXY_TARGET=http://localhost:8082`; query fixture `controlhub-query-e2e-mysql` container (`127.0.0.1:13306`, `query_e2e`) pre-existing, target 616 seeded idempotently (`QUERY_DEV_ALLOW_TARGET_FIXTURE=true QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO`). Root services (`:8080`, container) were never used or stopped; the `:8082` candidate server was stopped after the runs.
- New tests contain no route mocks, `page.evaluate`, forced clicks, skips/fixmes, or global timeout relaxation.

## Merged-Root Gates (re-run from merged root directories after FF merge)

| Repo (merged SHA) | Commands | Result |
|---|---|---|
| Backend `121b69c` | `go build ./...`, `go vet ./...`, `go test ./... -count=1`, `make openapi-validate` | All PASS (1418 unit) |
| Frontend `3cfc3ce` | `npx tsc --noEmit`, `npm run test` (1376), `npx next build` | All PASS |

## Review

Independent two-axis review (code-review skill; four parallel read-only sub-agents, one per repo per axis) against each merge-base, on the committed candidate ranges:

- Backend Standards: verdict **APPROVE** (P1 0 · P2 2 · P3 4). P2s: `WithTemplateExecution` setter vs constructor injection — **reclassified as accepted, non-blocking P3 design tradeoff** (unconditional wiring in `cmd/server/main.go`, route gated on the service existing, nil-guard yields a controlled 500; constructor churn across ~30 test call sites disproportionate per repo rules); production max-rows clamp duplicated between `Execute` and `ExecuteSavedStatement` — **fixed** in `121b69c` (`clampProductionMaxRows`).
- Backend Spec: verdict **APPROVE** (P1 0 · P2 0 · P3 3).
- Frontend Standards: verdict **APPROVE** (P1 0 · P2 1 · P3 3). P2: duplicated try/catch/finally page-outcome wiring across Run/Next/Previous/page-size — **fixed** in `3cfc3ce` (`applyWorksheetPage`).
- Frontend Spec: verdict **APPROVE** (P1 0 · P2 0 · P3 3).

Remaining P1/P2 count after fixes and reclassification: **0**. All remaining findings are accepted P3 cleanups (documented per review reports).

## CI

- Backend: `https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31102246835` — **success**. Required jobs: `release-local-gates` success, `release-docker-gates` success.
- Frontend: `https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/31102264278` — **success**. Required jobs: `release-local` success, `release-e2e` success.
- CI annotations are pre-existing lint warnings only (unused vars in `query-sql-format.test.ts`, `query-workbench.spec.ts`); no failures.

## Cleanup And Preserved State

- My isolated `:8082` candidate backend process stopped after the E2E runs. Root services (`:8080` backend process from a prior delivery, `controlhub-query-e2e-mysql` container) left untouched.
- Task worktrees and candidate branches intentionally preserved: `.worktrees/38w3-backend` @ `121b69c` and `.worktrees/38w3-frontend` @ `3cfc3ce` on `38w-3/execute-personal-templates`.

## Ticket

Issue #4 closed after this evidence was committed and verified: merged SHAs `121b69c` (backend) / `3cfc3ce` (frontend), evidence path `docs/superpowers/evidence/2026-08-06-phase-38w-3-template-execution-release-evidence.md`, CI URLs above.
