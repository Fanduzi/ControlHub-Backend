# Release Hardening Checklist

## Purpose

Use this checklist before merging a phase or preparing a release. It separates
fast local gates, backend contract gates, browser gates, and manual checks.

Every section includes exact commands. Run them in order. Record results in the
phase report.

## Frontend Preflight

```bash
cd /Users/fan/JsProjects/ControlHub
git status --short --branch
git worktree list
npm run check:e2e-preflight
```

`npm run check:e2e-preflight` detects stale `:3100` frontend dev server and
`:8081` E2E API proxy listeners. It prints PID and command diagnostics for any
occupied port. It **does not kill processes automatically**. Default mode exits
0 with warnings; strict mode can exit non-zero.

If preflight reports listeners, confirm whether they are the current Playwright
webServer and proxy. A stale `:3100` started without E2E environment variables
(e.g., missing `E2E_API_PROXY_PORT`) will cause server-side fetches to bypass
the `:8081` proxy, producing false E2E failures.

Fallback manual diagnostics if preflight is unavailable or E2E behaves
unexpectedly despite a clean preflight:

```bash
lsof -nP -iTCP:3000 -sTCP:LISTEN || true   # Next.js default dev
lsof -nP -iTCP:3100 -sTCP:LISTEN || true   # Playwright webServer target
lsof -nP -iTCP:8081 -sTCP:LISTEN || true   # E2E API proxy
```

Kill stale processes before running full E2E. Do not auto-kill — verify first.

## Backend Preflight

```bash
cd /Users/fan/GolangProjects/ControlHub
git status --short --branch
curl -fsS http://localhost:8080/health
```

If the backend is not running, start it explicitly:

```bash
cd /Users/fan/GolangProjects/ControlHub
source .env 2>/dev/null || true
go run ./cmd/server &
```

Record the backend PID in the phase report.

## Backend Gates

### Individual Backend Gates

Run from the backend worktree or main repo:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
```

All four must pass. These run without Docker.

For backend API changes, read-model changes, or schema changes, also run when
Docker is available:

```bash
make test-integration
make test-openapi-fuzz
```

If Docker is unavailable, state explicitly:

> Docker-dependent gates not run: Docker is not available in this environment.

Docker-dependent gates include:
- `make test-integration` — starts disposable MySQL 8.0 via Testcontainers
- `make test-openapi-fuzz` — starts disposable MySQL + runs Schemathesis

### Backend Release Gate Shortcuts

```bash
make release-local-gates
make release-docker-gates
make release-readiness-gates
```

`release-local-gates` is the no-Docker baseline. `release-docker-gates` requires
Docker. `release-readiness-gates` composes both and is the strongest local backend
readiness signal.

### Backend GitHub Actions CI

Fast CI (push/pull_request to main):

```text
.github/workflows/backend-ci.yml
push/pull_request -> make release-local-gates
```

Manual heavy CI (workflow_dispatch):

```text
workflow_dispatch with run_docker_gates=true -> make release-docker-gates
uploads .schemathesis-reports/ for 7 days when present
```

## Frontend Gates

### Individual Frontend Gates

```bash
cd /Users/fan/JsProjects/ControlHub
npm run check:e2e-governance
npm run check:e2e-preflight
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

These six do not require a running backend. `check:e2e-preflight` detects stale
dev server/proxy listeners but does not kill them.

When backend is running:

```bash
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

All E2E specs use `/__api` same-origin browser calls through the E2E API proxy
at `:8081`. If the proxy is stale or missing, E2E will fail with network errors
rather than application errors.

### Frontend Release Gate Shortcuts

```bash
npm run release:local
npm run release:e2e
npm run release:check
npm run release:smoke:cdp
```

`release:local` runs preflight, governance, typecheck, lint, test, and build.
`release:e2e` runs smoke, interaction, and full E2E (requires backend).
`release:check` composes both. `release:smoke:cdp` requires a manually-started
Chrome remote debugging session and is not included in `release:check`.

## Manual Browser Checks

Minimum pages to visit:

| URL | What to verify |
|---|---|
| `/overview?environment=prod` | Attention queue loads, reasons display, no console errors |
| `/databases?environment=prod` | Database list renders, search/filter/sort remain interactive |
| `/resources/14` | Database cluster detail with abnormal member shows decision deck |
| `/resources/22` | Healthy database instance shows operational summary |
| `/resources?page=1&pageSize=1` | Pagination controls work, query params update |
| `/audits?page=1&pageSize=1` | Audit list loads, pagination works |
| `/settings` | All dictionaries render |
| `/topology` | Topology loads, `/__api` same-origin calls succeed |

Check on each page:

- console errors = 0
- network 4xx/5xx = 0 unless expected and documented
- all API calls use `/__api` in browser (not `:8080` direct)
- database list search, filter, and sort controls remain interactive after use
- sheet/dialog opens and closes without errors
- topology renders without blank canvas
- overview attention reason matches the corresponding database detail reason
  (both should show the same health/signal state for the same resource)

### Key semantic checks:

- **overview attention reason vs database detail reason**: The overview attention
  queue and the database detail decision deck must agree on why a resource needs
  attention. If they diverge, that is a product bug, not a display issue.
- **database list search/filter/sort**: Type a search query, apply a filter,
  change sort, then clear each. All controls must remain responsive and the URL
  must reflect current state.

## Failure Classification

Do not write "pre-existing" without this table:

| Test | Branch | Main comparison | Error | Classification | Owner / next action |
|---|---|---|---|---|---|
| (example) `list-pagination:93` | `feat/phase-27` | Same failure on `main@f6334ec` | `expect.poll() timed out` | `environment_gap` | Phase 27B |

Allowed classifications:

| Classification | Meaning | Action |
|---|---|---|
| `real_regression` | This branch introduced the failure | Fix before merge |
| `environment_gap` | Test infrastructure (proxy, server, env vars) was misconfigured | Fix environment, re-run |
| `obsolete_test` | Test expectation no longer matches product intent | Update test with evidence |
| `needs_product_decision` | Product behavior is ambiguous | Escalate, do not merge until resolved |
| `main_preexisting_with_identical_evidence` | Same test fails identically on current main | Document with exact commit, do not block merge, file issue |

Every classification must include the failing locator/assertion, affected URL,
root cause, and next action.

## Merge Blockers

Do not merge if any of these conditions hold:

1. **Dirty worktree** — uncommitted changes in the phase worktree.
2. **Untracked artifacts** — files that are not intentionally gitignored.
3. **Failing typecheck, lint, unit, or build** — any of the non-Docker gates.
4. **Failing E2E without classification** — every failure needs the table above.
5. **Broad output suppression** — `stderr: "ignore"`, `stdout: "ignore"`,
   broad regex filters on Playwright output.
6. **Skipped or deleted tests** — unless explicitly approved with a reason.
7. **Backend API change without OpenAPI validation** — `make openapi-validate`
   must pass.
8. **Docker-dependent backend gate skipped without stating why** — if Docker is
   available, integration and fuzz tests must run. If Docker is unavailable, the
   phase report must state this explicitly.

## Gate Summary

| Gate | Command | Docker? | When |
|---|---|---|---|
| Backend unit tests | `go test -count=1 ./...` | No | Every commit |
| Backend static analysis | `go vet ./...` | No | Every commit |
| Backend compilation | `go build ./...` | No | Every commit |
| OpenAPI validation | `make openapi-validate` | No | Every commit |
| Backend integration | `make test-integration` | Yes | Before merge when Docker available |
| OpenAPI fuzz | `make test-openapi-fuzz` | Yes | Before merge for API changes, or nightly |
| Frontend typecheck | `npx tsc --noEmit` | No | Every commit |
| Frontend lint | `npm run lint` | No | Every commit |
| Frontend unit tests | `npm run test` | No | Every commit |
| Frontend build | `npm run build` | No | Every commit |
| E2E governance | `npm run check:e2e-governance` | No | Every commit |
| E2E preflight | `npm run check:e2e-preflight` | No | Before E2E runs |
| E2E smoke | `npm run test:e2e:smoke` | Backend required | Before merge |
| E2E interaction | `npm run test:e2e:interaction` | Backend required | Before merge for interaction changes |
| Full E2E | `npm run test:e2e` | Backend required | Before phase close |
| Manual browser | See table above | Backend required | Before phase close |
| Backend local gates | `make release-local-gates` | No | Every release candidate |
| Backend Docker gates | `make release-docker-gates` | Yes | Every release candidate when Docker available |
| Backend readiness gates | `make release-readiness-gates` | Yes | Every release candidate |
| Frontend local gates | `npm run release:local` | No | Every release candidate |
| Frontend browser gates | `npm run release:e2e` | Backend required | Every release candidate |
| Frontend readiness | `npm run release:check` | Backend required | Every release candidate |
| CDP live smoke | `npm run release:smoke:cdp` | Chrome CDP | Optional, not in release:check |
| Backend CI fast | `.github/workflows/backend-ci.yml` (push/PR) | No | Every push/PR to main |
| Backend CI heavy | `.github/workflows/backend-ci.yml` (workflow_dispatch) | Yes | Manual only, uploads .schemathesis-reports/ for 7 days |

## RC Evidence Orchestration

Follow this order when producing a release candidate evidence bundle:

1. **Confirm backend/frontend git status** — both worktrees must be clean before recording gates
2. **Record backend/frontend commits** — capture `git rev-parse --short HEAD` from each repo
3. **Run or reference backend release-readiness gates** — `make release-readiness-gates`
4. **Run or reference frontend release:check** — `npm run release:check`
5. **Classify warnings and optional gates** — assign Accepted, Follow-Up, or Blocking to every warning; record skipped optional gates with reason
6. **Write candidate evidence** — fill `docs/releases/candidates/YYYY-MM-DD-controlhub-rc-local.md` from template
7. **Decide GO / NO-GO** — GO only if backend required gates PASS and frontend required gates PASS; NO-GO if either required gate fails or any failure is unclassified

Re-run gates only if commit SHAs changed, the worktree is dirty, or evidence appears stale.

## Evidence Bundle

Every release-readiness run must create a candidate evidence document from:

```text
docs/releases/candidates/TEMPLATE.md
```

Store local dry-run evidence under:

```text
docs/releases/candidates/YYYY-MM-DD-controlhub-rc-local.md
```

The evidence file records backend commit, frontend commit, gate results, known
gaps, and a go/no-go decision. No candidate is "ready" unless its evidence is
written down.
