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
```

Check for stale processes that will conflict with Playwright:

```bash
lsof -nP -iTCP:3000 -sTCP:LISTEN || true   # Next.js default dev
lsof -nP -iTCP:3100 -sTCP:LISTEN || true   # Playwright webServer target
lsof -nP -iTCP:8081 -sTCP:LISTEN || true   # E2E API proxy
```

If `:3100` is occupied before E2E, confirm whether it is the current Playwright
webServer instance. A stale `:3100` frontend dev server started without E2E
environment variables (e.g., missing `E2E_API_PROXY_PORT`) will cause tests to
connect to the wrong backend or fail to record requests.

If `:8081` is occupied before E2E, confirm it is the current E2E API proxy. A
stale `:8081` proxy will not record requests or will return stale data.

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

## Frontend Gates

```bash
cd /Users/fan/JsProjects/ControlHub
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

These five do not require a running backend.

When backend is running:

```bash
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

All E2E specs use `/__api` same-origin browser calls through the E2E API proxy
at `:8081`. If the proxy is stale or missing, E2E will fail with network errors
rather than application errors.

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
| E2E smoke | `npm run test:e2e:smoke` | Backend required | Before merge |
| E2E interaction | `npm run test:e2e:interaction` | Backend required | Before merge for interaction changes |
| Full E2E | `npm run test:e2e` | Backend required | Before phase close |
| Manual browser | See table above | Backend required | Before phase close |
