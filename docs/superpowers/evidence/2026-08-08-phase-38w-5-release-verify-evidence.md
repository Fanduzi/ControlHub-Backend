# Phase 38W-5 Release-Verify Evidence

Date: 2026-08-08
Issue: #6, `38W-5: Release-verify governed parameterized templates`
Parent: #1 (remains OPEN)
Status of this run: **release closure verified; awaiting independent final verification**
Tracker action: **Issue #6 remains OPEN until independent final verification**

## Candidates

| Item | Backend | Frontend |
|---|---|---|
| Base SHA (`origin/main` at preflight) | `5388a8d0a572948efe3f39c23c7969eb3befe2ce` | `917b1389977447e6362d309f0fc2967466581232` |
| Candidate branch | `issue-6-38w5-20260808215659` | `issue-6-38w5-20260808215659` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-issue6-38w5` | `/Users/fan/JsProjects/ControlHub-issue6-38w5` |
| Candidate product SHA | `5388a8d0a572948efe3f39c23c7969eb3befe2ce` (no product source change) | `917b1389977447e6362d309f0fc2967466581232` (no product source change) |
| Candidate docs SHA | `e7c5a327287a665ef7ace89f90edc3bc33209336` | n/a (docs live in backend repo) |
| Product diff | **empty** (verification-only) | **empty** (verification-only) |

## Scope decision

Issue #6 is an integration/release-verification ticket. After mapping every
acceptance criterion to existing tests (see verification matrix) and running
candidate gates against isolated worktrees:

- **No concrete acceptance gap** required production repair.
- **No RED to GREEN source change** was introduced.
- Tracked changes are limited to Phase 38W-5 verification matrix + this
  release-evidence document under `docs/superpowers/evidence/`.
- Historical Phase 38R / 38W-1..4 evidence files were **not** rewritten.
- Root untracked Phase 38W WIP and other root dirty paths were **not** touched.

## Root dirty-path whitelist and preservation

Preflight snapshots (byte-for-byte SHA-256 of every dirty/untracked file path
that is a regular file) matched post-run snapshots.

### Backend root (`/Users/fan/GolangProjects/ControlHub`)

Porcelain (unchanged): `M CLAUDE.md`, `M advisor-plans/README.md`; untracked
`AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`,
`CONTEXT.md`, `docs/agents/`,
`docs/decisions/2026-08-04-parameter-value-evidence-retention.md`,
`docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`,
`docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`.

The docs-only candidate fast-forwarded from
`5388a8d0a572948efe3f39c23c7969eb3befe2ce` to
`e7c5a327287a665ef7ace89f90edc3bc33209336`; the dirty-path whitelist remained
unchanged.
SHA-256 snapshot files: `/tmp/38w5-be-root-sha256.txt` identical after-run.

### Frontend root (`/Users/fan/JsProjects/ControlHub`)

Porcelain (unchanged): `M AGENTS.md`, `M CLAUDE.md`; untracked `.codegraph/`,
`AGENTS.md.bak-pre-gitnexus-uninstall`, `CLAUDE.md.bak-pre-gitnexus-uninstall`.

`origin/main` remained `917b1389977447e6362d309f0fc2967466581232`
because Issue #6 introduced no frontend commit; the dirty-path whitelist
remained unchanged.
SHA-256 snapshot files: `/tmp/38w5-fe-root-sha256.txt` identical after-run.

### Foreign listeners left untouched

| Listener | Owner | Action |
|---|---|---|
| `:8080` | pre-existing root `main` process | **not used, not stopped** |
| `:8082` | prior Issue #5 binary `controlhub-issue5-server` (CWD `ControlHub-issue5-38w4`) | **not used, not stopped** |
| `controlhub-query-e2e-mysql` `:13306` | dedicated query fixture | **reused read-only** (idempotent seed only) |
| unrelated Docker (`deltascope-pg-e2e`, `mac-connector`) | other projects | untouched |

## Isolated candidate services (E2E provenance)

| Field | Value |
|---|---|
| Backend binary | `/var/folders/…/T/opencode/controlhub-issue6-server` built from BE worktree |
| Backend PID | `28026` |
| Backend PORT | `8083` (chosen because `:8082` was occupied by Issue #5) |
| Backend CWD | `/Users/fan/GolangProjects/ControlHub-issue6-38w5` |
| Backend SHA | `5388a8d0a572948efe3f39c23c7969eb3befe2ce` |
| Health | `GET http://127.0.0.1:8083/health` → `{"status":"ok"}` |
| Target fixture | resource `616` / `Local MySQL Query Dev`; `QUERY_DEV_ALLOW_TARGET_FIXTURE=true QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO make seed-query-dev-target` (idempotent); `availableActions.run=true`, credential `LOCAL_QUERY_RO` secret_resolved |
| Query MySQL | container `controlhub-query-e2e-mysql`, host `127.0.0.1:13306`, db `query_e2e` |
| Playwright proxy | `:8081` (`PLAYWRIGHT_PROXY_TARGET=http://localhost:8083`) |
| Next dev | `:3100` (Playwright webServer) |
| Frontend CWD/SHA | `/Users/fan/JsProjects/ControlHub-issue6-38w5` @ `917b1389977447e6362d309f0fc2967466581232` |

Root `:8080` and foreign `:8082` were never used as `PLAYWRIGHT_PROXY_TARGET`.

## Verification matrix

Tracked path:

`docs/superpowers/evidence/2026-08-08-phase-38w-5-verification-matrix.md`

Every Issue #6 AC maps to existing backend unit/integration/OpenAPI/fuzz and
frontend unit/E2E proof, including an explicit accessibility section. Residual
notes (no log-capture harness, no Go-native compiler fuzz, architectural
load-side-effect proof) are non-blocking and do not falsify the contract.

## Candidate gates

### Backend (worktree @ product SHA `5388a8d`)

| Command | Result |
|---|---|
| `git diff --check` | PASS on product range; docs commits kept free of trailing whitespace |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `go test -count=1 ./...` | PASS: **1418** tests, 0 failed |
| `make openapi-validate` | PASS (`TestOpenAPIYAMLIsValid`) |
| `make test-integration` | PASS: **202** tests, 0 failed, 0 skipped |
| `make test-openapi-fuzz` | PASS: Schemathesis **2089 generated / 2089 passed**, 0 failures; includes `POST …/saved-statements/{statementId}/execute` |
| Explicit tagged integration (subset) | Covered inside `make test-integration` (`query_template_execution_test.go`, `query_saved_statement_test.go`) |
| Parameter-value evidence capture prod grep | **absent** |

### Frontend (worktree @ `917b138`)

| Command | Result |
|---|---|
| `git diff --check` | PASS |
| `npx tsc --noEmit -p tsconfig.json` | PASS, exit 0 |
| `npm run lint` | 0 errors; 5 warnings pre-existing on `origin/main` paths |
| `npm run test` | PASS: **1382** tests in 89 files |
| `npm run build` (`next build`) | PASS |
| `npm run check:e2e-preflight` | PASS (`:3100` / `:8081` free before each E2E entry) |
| `npm run check:e2e-governance` | PASS (13 spec files scanned) |

## Real Chromium E2E

### Full suite (candidate gate)

```bash
cd /Users/fan/JsProjects/ControlHub-issue6-38w5
PLAYWRIGHT_PROXY_TARGET=http://localhost:8083 \
BACKEND_URL=http://localhost:8083 \
CONTROLHUB_API_BASE_URL=http://localhost:8081 \
CONTROLHUB_API_PROXY_URL=http://localhost:8081 \
npm run test:e2e
```

| Metric | Value |
|---|---|
| Total | 163 |
| Passed | 163 |
| Failed | 0 |
| Skipped | 0 |
| Duration | ~4.2m |
| Log | `/tmp/38w5-e2e-full.txt` |

### Template-focused suite x3 (exact candidate heads)

Grep (Playwright):

`Phase 38W-3|parameterized personal template|shared template|Issue #5|template load is inert|template pagination|disclosure-policy change blocks|editing the SQL exits template|refresh while template|sign-out and re-login discards template|template form and execution remain operable|template execution and localized`

| Run | Total | Passed | Failed | Skipped | Duration | Log |
|---|---|---|---|---|---|---|
| 1 | 23 | 23 | 0 | 0 | ~1.2m | `/tmp/38w5-e2e-template-run1.txt` |
| 2 | 23 | 23 | 0 | 0 | ~1.2m | `/tmp/38w5-e2e-template-run2.txt` |
| 3 | 23 | 23 | 0 | 0 | ~1.2m | `/tmp/38w5-e2e-template-run3.txt` |

Service provenance identical for all four E2E entries: BE PID `28026`, port
`8083`, CWD/SHA as above; FE worktree `917b138`; fixture
`controlhub-query-e2e-mysql:13306`.

Covered blocks include:

- desktop EN / 375px EN / desktop zh-CN personal parameterized load
- Phase 38W-3 execute route, inert load, pagination on template route, disclosure change on later page, SQL-edit exit, refresh discard, 375px + zh-CN execute
- Issue #5 shared admin affordances, non-admin load+execute+Next, no author/value leakage, refresh/sign-out disposal, list pagination

No route mocks, forced clicks, skips/fixmes, or global timeout relaxation were added.

## Merged-root release gates and E2E

The backend docs-only branch was fast-forwarded into backend `main`. The
frontend had no candidate commit and remained at its existing `main` SHA.

| Repo | Merged product/docs SHA used for gates | Merge result |
|---|---|---|
| Backend | `e7c5a327287a665ef7ace89f90edc3bc33209336` | fast-forward from `5388a8d0a572948efe3f39c23c7969eb3befe2ce` |
| Frontend | `917b1389977447e6362d309f0fc2967466581232` | no-op; no Issue #6 frontend commit |

Merged-root commands and results:

| Repo | Command | Result |
|---|---|---|
| Backend | `make release-local-gates` | PASS: 10 Go packages, vet, build, OpenAPI validation |
| Backend | `make release-docker-gates` | PASS: integration suite; Schemathesis 49/49 operations and 2089/2089 generated cases |
| Frontend | `ASDF_NODEJS_VERSION=22.22.0 npm run release:local` | PASS: runtime check, governance, typecheck, lint 0 errors, 1382/1382 unit tests, build |
| Frontend | `PLAYWRIGHT_PROXY_TARGET=http://localhost:8083 npm run release:e2e` | PASS: smoke 7/7, interaction 3/3, full Chromium 163/163; 0 failed, 0 skipped |

Merged-root E2E service provenance:

| Field | Value |
|---|---|
| Backend listener | PID `68647`, port `8083` |
| Backend CWD | `/Users/fan/GolangProjects/ControlHub` |
| Backend SHA | `e7c5a327287a665ef7ace89f90edc3bc33209336` (docs-only delta; product remains `5388a8d0a572948efe3f39c23c7969eb3befe2ce`) |
| Backend health | `GET http://127.0.0.1:8083/health` returned `{"status":"ok"}` |
| Frontend CWD/SHA | `/Users/fan/JsProjects/ControlHub` @ `917b1389977447e6362d309f0fc2967466581232` |
| Proxy / frontend | Playwright-managed `:8081` to `:8083`; Next `:3100` |
| Query fixture | existing `controlhub-query-e2e-mysql` on `:13306` |
| Cleanup | task-owned backend stopped after E2E; `:8083`, `:8081`, and `:3100` released |

Root `:8080`, foreign `:8082`, and the existing query fixture were not
stopped or repurposed.

## RED to GREEN corrections

**None.** No failing characterization test was required; the accepted end-to-end
contract was already demonstrably true at the candidate heads.

## Change-scope detection

- GitNexus: unavailable (intentionally removed from this workspace).
- CodeGraph: available on backend root index only; candidate worktrees have no
  `.codegraph/`. Because no production symbols were edited, impact analysis was
  limited to confirming zero product diffs (`git status` / `git diff` empty for
  non-docs paths) and inventorying callers via prior-phase evidence + test map.
- Manual scope: backend candidate adds only `docs/superpowers/evidence/*38w-5*`;
  frontend candidate has zero commits beyond branch tip at `origin/main`.

## Three-level documentation

- New files are evidence/matrix under `docs/superpowers/evidence/` (release
  audit artifacts, not runtime modules).
- No L2 module README or L3 production file-header changes required (no
  production source touched).
- Historical evidence/ADRs left intact.

## Review

Independent two-axis `/code-review` against backend merge-base `origin/main`
(`5388a8d`) on the docs-only candidate range:

| Axis | Verdict | P1 | P2 | P3 |
|---|---|---|---|---|
| Standards | **APPROVE** | 0 | 0 | 2 (cosmetic title/list style vs 38W-4 prior art) |
| Spec | **APPROVE** after follow-up | 0 | 0 | 0 |

First Spec pass flagged P2: matrix omitted an explicit accessibility section
despite AC2 naming a11y. Fixed in the follow-up docs commit by adding
`### Accessibility (parameter form a11y)` with concrete unit/E2E names, plus
named disposal tests. Trailing-whitespace/`git diff --check` noise from the
first docs commit cleaned in the same follow-up.

Frontend product range is empty (`917b138` == `origin/main`); product
standards/spec review is N/A.

Remaining P1/P2 count: **0**.

## Push and CI

Backend `main` was pushed normally, without force, from
`5388a8d0a572948efe3f39c23c7969eb3befe2ce` to the docs-only merged SHA
`e7c5a327287a665ef7ace89f90edc3bc33209336`. Frontend required no push because
Issue #6 introduced no frontend commit.

| Repo | Run | Exact head SHA | Required jobs | Conclusion |
|---|---|---|---|---|
| Backend | [31262384273](https://github.com/Fanduzi/ControlHub-Backend/actions/runs/31262384273) | `e7c5a327287a665ef7ace89f90edc3bc33209336` | `release-local-gates`, `release-docker-gates` | success |
| Frontend | [31252491705](https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/31252491705) | `917b1389977447e6362d309f0fc2967466581232` | `release-local`, `release-e2e` | success |

This evidence update is a separate docs-only closure commit and intentionally
does not name its own commit SHA. The independent verifier records the final
evidence-commit SHA and confirms its exact CI run, avoiding a self-referential
SHA/documentation loop.

## Tracker

| Item | State |
|---|---|
| Issue #6 | **OPEN until independent final verification completes** |
| Issue #1 parent | OPEN |
| Issues #2–#5 | CLOSED (prior deliveries) |

## Final status

**release evidence complete; independent final verification required before ticket closure and cleanup**
