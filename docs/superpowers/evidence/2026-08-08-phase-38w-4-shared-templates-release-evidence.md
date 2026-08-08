# Phase 38W-4 Shared Templates Release Evidence

Date: 2026-08-08
Issue: #5, `38W-4: Govern shared templates and responsive template sessions`

## Candidates

| Item | Backend | Frontend |
|---|---|---|
| Base SHA (`origin/main` before merge) | `c2b2a1354937e404f88c07cfbae2ff09ff20d2d3` | `3cfc3ce7c321aa4faf697f0a52c565e667b313fd` |
| Candidate branch | `issue-5-38w4-20260807` | `issue-5-38w4-20260807` |
| Candidate / merged SHA | `8564429bc717bac05f57d5aee1548389ca11989d` | `917b1389977447e6362d309f0fc2967466581232` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-issue5-38w4` | `/Users/fan/JsProjects/ControlHub-issue5-38w4` |
| Candidate commits | `8564429` docs-only ADR (template session disposal) | `752aafb` gate shared mutations; `a409d59` affordance E2E; `b3a0b88` disposal unit tests; `ba89d85` L3 headers; `894000f` E2E fixtures/disposal; `76a7ffa` target-switch return; `917b138` fixture harden + 375/zh-CN session |

## Merge And Push

- Merge type: fast-forward only (`git merge --ff-only issue-5-38w4-20260807`) in each root worktree on `main`.
- Backend push range: `c2b2a13..8564429` → `origin/main` = `8564429bc717bac05f57d5aee1548389ca11989d`. Local `main` == `origin/main` == merged SHA. No force push.
- Frontend push range: `3cfc3ce..917b138` → `origin/main` = `917b1389977447e6362d309f0fc2967466581232`. Local `main` == `origin/main` == merged SHA. No force push.

## Root Dirty-Path Whitelist And Preservation

Preserved byte-for-byte before, during, and after merge/push (identical `git status --porcelain`).

- Backend root: `M CLAUDE.md`, `M advisor-plans/README.md`; untracked `AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`, `CONTEXT.md`, `docs/agents/`, `docs/decisions/2026-08-04-parameter-value-evidence-retention.md`, `docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`, `docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`.
- Frontend root: `M AGENTS.md`, `M CLAUDE.md`; untracked `.codegraph/`, `AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`.
- None of the whitelisted paths are touched by either candidate diff; both fast-forward merges preserved them exactly. Root Phase 38W WIP specs were never edited by this delivery.

## Candidate Gates (run against the exact candidate SHAs)

### Backend (candidate `8564429`, docs-only ADR)

| Command | Result |
|---|---|
| `make release-local-gates` (`go test -count=1 ./...`, `go vet ./...`, `go build ./...`, `make openapi-validate`) | PASS: 1418 unit tests in 10 packages; OpenAPI valid |
| `make release-docker-gates` (`make test-integration`, `make test-openapi-fuzz`) | PASS: integration package green; Schemathesis 2089 generated / 2089 passed, 0 failures |

### Frontend (candidate `917b138`)

| Command | Result |
|---|---|
| `npx tsc --noEmit -p tsconfig.json` | PASS, exit 0 |
| `npm run lint` | 0 errors; 5 warnings pre-existing on `origin/main` paths |
| `npm run test` | PASS: 1382 tests in 89 files |
| `npx next build` | PASS |
| `npm run check:e2e-governance` | PASS (13 spec files scanned) |
| `npm run check:e2e-preflight` | PASS (:3100 / :8081 free) |

## Real Chromium E2E

- Command (candidate worktree at `917b138`):
  `PLAYWRIGHT_PROXY_TARGET=http://localhost:8082 BACKEND_URL=http://localhost:8082 CONTROLHUB_API_BASE_URL=http://localhost:8081 CONTROLHUB_API_PROXY_URL=http://localhost:8081 npm run test:e2e`
- Totals: **163 passed, 0 failed, 0 skipped** (~4.1m).
- Issue #5 block alone: 11 passed (affordance, 375/zh-CN shared param session, non-admin execute+paging, disposal, pagination cleanup).
- Serving processes: backend binary from Issue #5 backend candidate CWD `/Users/fan/GolangProjects/ControlHub-issue5-38w4` (SHA `8564429`) on `:8082`; Playwright-started API proxy `:8081` and Next `:3100`; query fixture `controlhub-query-e2e-mysql` at `127.0.0.1:13306`. Root `:8080` process was not used for E2E.
- No route mocks, `page.evaluate` (except pre-existing helper patterns outside Issue #5 repair paths), forced clicks, skips/fixmes, or broad timeout increases in the Issue #5 repair tests.

## Merged-Root Gates (re-run from merged root directories after FF merge)

| Repo (merged SHA) | Commands | Result |
|---|---|---|
| Backend `8564429` | `make release-local-gates` | PASS (1418 unit) |
| Frontend `917b138` | `npx tsc --noEmit`, `npm run test` (1382), `npx next build` | All PASS |

## Review

Human re-review after P2 fixes and ADR:

- Final verdict: **APPROVE** (P1: 0, P2: 0).
- Prior P2s fixed on frontend `917b138`: target-switch return asserts Worksheet 1 + empty values; shared fixtures validate statement/parameters; 375px EN and desktop zh-CN load shared param template with controlled validation, focus, and template-execute.
- Backend ADR `docs/decisions/2026-08-08-phase-38w-template-value-session-disposal.md` records the locked product boundary (no standalone close control; SQL-edit exit only; worksheet close/switch/target switch/refresh/sign-out dispose values).
- Historical Phase 38R specs not modified. Root untracked Phase 38W WIP not modified.

Remaining P1/P2 count: **0**.

## CI

- Backend: `https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31252495009` — **success**. Required jobs: `release-local-gates` success, `release-docker-gates` success.
- Frontend: `https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/31252491705` — **success** (first attempt failed once on pre-existing flaky `query history records SHOW TABLES blocked attempt` network-error consumption with empty observed errors while UI/history already showed disclosure block; `--failed` re-run succeeded). Required jobs: `release-local` success, `release-e2e` success.
- CI annotations are pre-existing lint warnings / Node 20 deprecation notices only; no remaining failures.

## Cleanup And Preserved State

- Issue #5 candidate worktrees and branches intentionally preserved:
  `/Users/fan/GolangProjects/ControlHub-issue5-38w4` @ `8564429` and
  `/Users/fan/JsProjects/ControlHub-issue5-38w4` @ `917b138` on `issue-5-38w4-20260807`.
- Root dirty whitelist paths left untouched.
- Local `:8082` Issue #5 backend process and `controlhub-query-e2e-mysql` fixture left running (not stopped by this delivery); root `:8080` never used for gates.

## Ticket

Issue #5 closed after independent verifier checklist passed:

- Frontend merged SHA: `917b1389977447e6362d309f0fc2967466581232`
- Backend feature merged SHA: `8564429bc717bac05f57d5aee1548389ca11989d`
- Backend evidence HEAD: `4bb342ab25dc2f72cdeb9da0fab33a27cfb12c54` (includes this file + whitespace fix)
- Evidence path: `docs/superpowers/evidence/2026-08-08-phase-38w-4-shared-templates-release-evidence.md`
- Backend CI (feature): https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31252495009
- Frontend CI: https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/31252491705
- Parent Issue #1 remains OPEN.
