# Phase 38W-5 Release-Verify Evidence

Date: 2026-08-08  
Issue: #6, `38W-5: Release-verify governed parameterized templates`  
Parent: #1 (remains OPEN)  
Status of this run: **candidate complete — ready for human review**  
Tracker action: **Issue #6 left OPEN** (no close, no merge, no push)

## Candidates

| Item | Backend | Frontend |
|---|---|---|
| Base SHA (`origin/main` at preflight) | `5388a8d0a572948efe3f39c23c7969eb3befe2ce` | `917b1389977447e6362d309f0fc2967466581232` |
| Candidate branch | `issue-6-38w5-20260808215659` | `issue-6-38w5-20260808215659` |
| Candidate worktree | `/Users/fan/GolangProjects/ControlHub-issue6-38w5` | `/Users/fan/JsProjects/ControlHub-issue6-38w5` |
| Candidate product SHA | `5388a8d0a572948efe3f39c23c7969eb3befe2ce` (no product source change) | `917b1389977447e6362d309f0fc2967466581232` (no product source change) |
| Candidate docs SHA | evidence/matrix commit on backend branch (this delivery) | n/a (docs live in backend repo) |
| Product diff | **empty** (verification-only) | **empty** (verification-only) |

## Scope decision

Issue #6 is an integration/release-verification ticket. After mapping every
acceptance criterion to existing tests (see verification matrix) and running
candidate gates against isolated worktrees:

- **No concrete acceptance gap** required production repair.
- **No RED→GREEN source change** was introduced.
- Tracked changes are limited to Phase 38W-5 verification matrix + this
  release-evidence document under `docs/superpowers/evidence/`.
- Historical Phase 38R / 38W-1..4 evidence files were **not** rewritten.
- Root untracked Phase 38W WIP and other root dirty paths were **not** touched.

## Root dirty-path whitelist and preservation

Preflight snapshots (byte-for-byte SHA-256 of every dirty/untracked file path
that is a regular file) matched post-run snapshots.

### Backend root (`/Users/fan/GolangProjects/ControlHub`)

Porcelain (unchanged):

- `M CLAUDE.md`
- `M advisor-plans/README.md`
- `?? AGENTS.md.bak-pre-gitnexus-uninstall`
- `?? CLAUDE.md.bak-pre-gitnexus-uninstall`
- `?? CONTEXT.md`
- `?? docs/agents/`
- `?? docs/decisions/2026-08-04-parameter-value-evidence-retention.md`
- `?? docs/superpowers/plans/2026-08-04-phase-38w-governed-parameterized-saved-templates-design.md`
- `?? docs/superpowers/specs/2026-08-04-phase-38w-governed-parameterized-saved-templates.md`

`origin/main` stayed `5388a8d0a572948efe3f39c23c7969eb3befe2ce`.  
SHA-256 snapshot files: `/tmp/38w5-be-root-sha256.txt` ≡ after-run.

### Frontend root (`/Users/fan/JsProjects/ControlHub`)

Porcelain (unchanged):

- `M AGENTS.md`
- `M CLAUDE.md`
- `?? .codegraph/`
- `?? AGENTS.md.bak-pre-gitnexus-uninstall`
- `?? CLAUDE.md.bak-pre-gitnexus-uninstall`

`origin/main` stayed `917b1389977447e6362d309f0fc2967466581232`.  
SHA-256 snapshot files: `/tmp/38w5-fe-root-sha256.txt` ≡ after-run.

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
frontend unit/E2E proof. Residual notes (no log-capture harness, no Go-native
compiler fuzz, architectural load-side-effect proof) are non-blocking and do
not falsify the contract.

## Candidate gates

### Backend (worktree @ product SHA `5388a8d`)

| Command | Result |
|---|---|
| `git diff --check` | PASS (clean at base; docs-only commit after) |
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

### Template-focused suite ×3 (exact candidate heads)

Grep (Playwright):

`Phase 38W-3|parameterized personal template|shared template|Issue #5|template load is inert|template pagination|disclosure-policy change blocks|editing the SQL exits template|refresh while template|sign-out and re-login discards template|template form and execution remain operable|template execution and localized`

| Run | Total | Passed | Failed | Skipped | Duration | Log |
|---|---|---|---|---|---|---|
| 1 | 23 | 23 | 0 | 0 | ~1.2m | `/tmp/38w5-e2e-template-run1.txt` |
| 2 | 23 | 23 | 0 | 0 | ~1.2m | `/tmp/38w5-e2e-template-run2.txt` |
| 3 | 23 | 23 | 0 | 0 | ~1.2m | `/tmp/38w5-e2e-template-run3.txt` |

Service provenance identical for all four E2E entries: BE PID `28026`, port
`8083`, CWD/SHA as above; FE worktree `917b138`; fixture `controlhub-query-e2e-mysql:13306`.

Covered blocks include:

- desktop EN / 375px EN / desktop zh-CN personal parameterized load
- Phase 38W-3 execute route, inert load, pagination on template route, disclosure change on later page, SQL-edit exit, refresh discard, 375px + zh-CN execute
- Issue #5 shared admin affordances, non-admin load+execute+Next, no author/value leakage, refresh/sign-out disposal, list pagination

No route mocks, forced clicks, skips/fixmes, or global timeout relaxation were added.

## RED → GREEN corrections

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

Independent `/code-review` against each worktree merge-base runs after the
docs commit on the backend candidate; frontend product range is empty so
standards/spec review is N/A for product code (docs-only backend delta).

Expected remaining P1/P2 after docs-only review: **0**.

## Deferred merged-root / CI steps (`$delivery-closure`)

Do **not** perform in this run:

1. Fast-forward merge of `issue-6-38w5-20260808215659` into backend `main`.
2. Push to `origin/main` (backend and/or frontend).
3. Merged-root re-run of release gates from root worktrees.
4. CI confirmation on pushed SHAs.
5. Close GitHub Issue #6.
6. Delete candidate worktrees/branches.

Reproducible candidate commands are fully recorded above so delivery-closure
can re-run without rediscovery.

## Tracker

| Item | State |
|---|---|
| Issue #6 | **OPEN** (this run) |
| Issue #1 parent | OPEN |
| Issues #2–#5 | CLOSED (prior deliveries) |

## Final status

**ready for human review**
