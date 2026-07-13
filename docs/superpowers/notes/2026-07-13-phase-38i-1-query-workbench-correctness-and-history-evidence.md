# Phase 38I.1 Query Workbench Correctness And History — Evidence

Status: **Implementation complete in worktrees — ready for human review.**

## Worktrees

| Repo | Path | Branch | Base | Tip |
|------|------|--------|------|-----|
| Backend | `.worktrees/backend-phase-38i-1-query-history-contract` | `phase-38i-1-query-history-contract` | `cdb8249` (main) | `df0321d` (product), this commit (docs) |
| Frontend | `/Users/fan/JsProjects/ControlHub/.worktrees/phase-38i-1-query-workbench-correctness` | `phase-38i-1-query-workbench-correctness` | `87f20c9` (main) | **`6a8f7f7`** |

> Backend: product code `df0321d`, docs evidence in this commit.
> Frontend: product code `31083d3`, E2E repair `6a8f7f7`.
> Frontend product code tip: `31083d3`; final E2E-repair tip: `6a8f7f7`.

## Real E2E (final)

```text
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
41 passed
0 failed
0 skipped
```

## API path proof (verified in code + live)

| Layer | Path |
|-------|------|
| Router | `GET /query-targets/{id}/schema/object-details` (`internal/api/router.go`) |
| Router | `GET /query-targets/{id}/executions` (`internal/api/router.go`) |
| OpenAPI | same paths in `internal/openapi/openapi.yaml` |
| Frontend | `services/query-schema.ts` → `/query-targets/${id}/schema/object-details` |
| Frontend | `services/query-executions.ts` → `/query-targets/${id}/executions?page=&pageSize=` |
| E2E proxy | Playwright `api-proxy` → backend `:8080` (no `/api/v1` prefix) |

## Object-detail empty arrays

- `ObjectDetailResponse.MarshalJSON` + `EnsureNonNilCollections` force `columns` / `indexes` / `foreignKeys` and nested index/FK column lists to `[]`.
- `toModelObjectDetail` initializes non-nil slices at construction.
- Live check against target `616` / `query_e2e_items` and `schema_child`: no `"columns":null` / `"indexes":null` / `"foreignKeys":null` / `"referencedColumns":null`.

## History policy (enforced)

| Actor | Scope |
|-------|--------|
| `admin` | All rows for `target_resource_id` |
| non-admin | `target_resource_id` AND `actor_user_id = authenticated actor` |

- Target existence validated without credential readiness (`findTarget` → 404).
- Public item shape: `actor: { displayName }` only (no `actorUserId` in JSON).
- Missing user → `Unknown user` via `LEFT JOIN users` + service fallback.
- Live: admin history includes both Admin + Editor display names; editor sees only own rows.

## Frontend behavior

- `normalizeObjectDetail` at service boundary (top-level + nested).
- Object explorer: per-object `loading/ready/error` + localized Retry.
- Worksheet history state machine: `idle|loading|ready|error` + independent `generation`.
- No mount-time history fetch; first History-tab open triggers fetch for executable idle/error worksheets.
- Post-run refresh for originating worksheet only; stale writes rejected by worksheet id + target + generation.
- History table: relative time (+ absolute accessible label), actor display name, status, statement, formatted rows/duration, safe errors.

## E2E verification integrity (Chinese mobile Objects)

- Prior gap: Chinese mobile Objects test skipped when the default target was not executable.
- Fix: `ensureReadyTargetSelected` + locale-aware connection dialog (`Connections|连接`); open triggers use i18n `connectionNavigator.open` / `openMobile` (EN + ZH); Run readiness uses `/^(run|执行)$/i`.
- `waitForCommittedRunState` samples Run-enabled for 5 stable readings (~500ms) after each selection to prevent stale-ready misclassification across locked targets.
- If no ready fixture target exists, the test **fails** with a clear setup error (does not skip).
- The Chinese mobile Objects sheet test at `e2e/query-workbench.spec.ts` no longer contains a `test.skip`. Other spec tests retain fixture-condition skips (no locked target, no ready target, empty SHOW TABLES) that fire when the E2E fixture is incomplete — these are unrelated to this fix.

## Gates

### Backend (product `df0321d`, docs this commit)

| Gate | Result |
|------|--------|
| `git diff --check` | clean |
| `go test -count=1 ./...` | PASS |
| `go vet ./...` | clean |
| `go build ./...` | clean |
| `make openapi-validate` | PASS |
| `make test-integration` | PASS |
| `make test-openapi-fuzz` | PASS (1426 generated, 1426 passed) |

### Frontend (product `31083d3`, E2E repair `6a8f7f7`)

| Gate | Result |
|------|--------|
| `git diff --check` | clean |
| `npm run check:e2e-preflight` | PASS (ports free) |
| `npm run check:e2e-governance` | PASS |
| `npx tsc --noEmit` | clean |
| `npm run lint` | 0 errors (pre-existing warnings only) |
| `npm run test` | **988/988 pass** |
| `npm run build` | success |

### Real E2E (final)

```text
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
41 passed
0 failed
0 skipped
```

## Backend commits

1. `f390374` fix(query): serialize empty object-detail arrays as []
2. `3cf0174` fix(query): scope execution history and project actor displayName
3. `df0321d` test(query): cover history actor scope and display projection
4. `64617be` docs: record phase 38i.1 query workbench correctness evidence
5. docs: phase 38i.1 evidence note (this commit)

## Frontend commits

1. `d3551f8` fix(query): normalize object-detail null arrays and actor wire shape
2. `31083d3` fix(query): history state machine and safer object detail errors
3. `6a8f7f7` test(e2e): eliminate Chinese mobile Objects sheet skip

## Negative scope confirmation

- No SQL guard change
- No query-engine addition / browser DB connection
- No DSN/password/username secrets in browser state, API responses, or display
- No credential edit controls in `/query`
- No schema persistence / browser persistence
- No SHOW CREATE / DDL / ER diagram / Visual Explain / export / saved queries / approval/JIT / notebook / AI / MCP / visual builder / editable grid
- No migrations (users.display_name + query_executions.actor_user_id sufficient)
- No CI workflow, push, merge, tag, release, deployment
- No AI co-author trailers

## Remaining P1/P2

**None** after adversarial self-review and E2E verification integrity repair.
