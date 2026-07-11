# Phase 38H Query Workbench Scalable IA Reset — Evidence

Status: **Implemented and verified locally. Not pushed.**
Branch `phase-38h-query-target-pagination` (backend),
`phase-38h-query-workbench-scalable-ia-reset` (frontend).

## Backend Commits (no AI co-author)

```
5d293ff feat(query): add paginated target read model
4c3c187 fix(query): update dev target readiness list calls
3dcd6a6 fix(query): filter credential target lookup by id
cfa08d7 fix(query): filter execution target lookup by id
48f07cd test(query): cover target pagination API contract
26981a8 test(query): update target list pagination callers
aee3efa test(query): cover repository pagination search
e2bf61d docs(query): document target pagination contract
e1f687f feat(pagination): add page navigation flags
7520a0c feat(pagination): propagate page navigation flags
3bceff8 test(api): cover pagination navigation flags
789391d fix(query): include port in target search
8a34ec5 docs(openapi): expose page navigation flags
```

## Frontend Commits (no AI co-author)

```
1d575c8 feat(query): add paginated target client
424d745 feat(query): preserve target deep links
925deba feat(query): bound connection navigator
a594fc2 feat(query): initialize active target from page
03bc1ea refactor(query): simplify governance credential panel
5d31dba feat(credentials): paginate credential settings targets
f1098f8 feat(pagination): expose page navigation state
4381395 test(pagination): cover shared page controls
863808f feat(databases): retain page size state
d1b1eab test(resources): cover paged resource requests
3fc5a6a test(audits): cover paged audit requests
b42eecc test(view-models): cover page navigation metadata
76f2802 feat(query): localize query page heading
8fa7174 feat(query): open connection navigator on demand
59891c1 fix(query): isolate navigator results from active worksheet
7e8a9e4 refactor(query): inline governance controls
12f97ed feat(credentials): paginate credential operations
```

## API Contract

Backend `GET /query-targets` now accepts:

- `q` — text search across display name, resource name, host, engine, environment, cluster
- `page` — positive integer, default `1`
- `pageSize` — positive integer, default `50`, max `100`
- `targetId` — exact target lookup
- existing `engine`, `environmentId` filters preserved

Response shape:

```json
{
  "items": [...],
  "pageInfo": {
    "page": 1,
    "pageSize": 50,
    "totalItems": N,
    "totalPages": M,
    "hasNextPage": bool,
    "hasPreviousPage": bool
  }
}
```

OpenAPI validates. Fuzz tests pass (1274 generated, 1274 passed).

## Frontend Bounded Loading Proof

- `services/query-targets.ts` sends `q`, `page`, `pageSize`, `targetId` to backend.
- `types/query-target.ts` defines `QueryTargetPageInfo` with `pageInfo: PageInfo`.
- Connection navigator opens on demand; empty state shows bounded current page.
- Credential status fan-out scoped to current server page IDs only.
- Active target preserved across search/page changes via URL `targetId`.

## API Path Verification

| Layer | Path |
|-------|------|
| Backend router | `GET /query-targets` (line 89, `internal/api/router.go`) |
| OpenAPI | `GET /query-targets` with `q`, `page`, `pageSize` params |
| Frontend service | `buildQueryTargetsPath()` in `services/query-targets.ts` |
| E2E proxy | `http://localhost:8080` via `e2e/api-proxy.mjs` |

## Backend Gates

| Gate | Result |
|------|--------|
| `git diff --check` | clean |
| `go test -count=1 ./...` | PASS (all packages) |
| `go vet ./...` | clean |
| `go build ./...` | clean |
| `make openapi-validate` | PASS |
| `make test-integration` | PASS |
| `make test-openapi-fuzz` | PASS (1274/1274) |

## Frontend Gates

| Gate | Result |
|------|--------|
| `git diff --check` | clean |
| `git diff --cached --check` | clean |
| `npm run check:e2e-preflight` | PASS |
| `npm run check:e2e-governance` | PASS (13 specs scanned) |
| `npx tsc --noEmit` | clean |
| `npm run lint` | 0 errors, 2 pre-existing warnings (not 38H) |
| `npm run test` | 68 files, 850 tests PASS |
| `npm run build` | PASS |

## Real E2E Results

Environment: real backend (`localhost:8080`), real dedicated query MySQL
(`127.0.0.1:13306`), real frontend Playwright proxy.

Backend commit: `8a34ec5`
Frontend commit: `12f97ed`

```
37 passed (34.0s)
0 failed
0 skipped
```

Tests covering Phase 38H scope:

- loads with real backend data and inline governance controls
- connection navigator surfaces at least one database target
- connection navigator opens as a mobile bottom sheet
- a locked query target hides Run and shows the blocker state
- switching the target updates the governance panel facts
- a ready target runs a guarded SELECT and shows the result
- a ready target runs SHOW TABLES and shows the result
- a ready target runs DESCRIBE and shows the result
- an unsafe statement is rejected with a controlled validation message
- query history shows the recent attempt after a run
- Format button visibly formats messy SQL
- Cmd/Ctrl+Enter runs the active worksheet
- two worksheets keep separate statements and results
- unsafe SQL remains rejected by backend
- dark-mode editor and result table maintain readable contrast
- SQL editor resize handle persists height across reload
- operations pagination navigates to next page with different rows
- operations pagination supports 25, 50, and 100 rows per page
- saving a configured credential shows success feedback
- selecting a credential target opens the credential dialog

## No-Secret Proof

- `.query-e2e-mysql.env` is gitignored, mode 0600, never printed.
- No DSN/password in router, response, audit, cache key, error, or log.
- No `actorUserId` in any request.
- Credential status fan-out scoped to current page IDs only.

## Scope Confirmation

- No SQL guard changes.
- No new repository methods beyond pagination support.
- No credential binding relaxation.
- No DSN/password browser state/request/display/logs.
- No `actorUserId`.
- No credential edit controls inside `/query`.
- No saved query/export/approval/JIT implementation.
- No worksheet backend persistence.
- No CI workflow changes.
- No tag/release/deploy.
- No AI co-author.

## Remaining Gaps

- `.omo/` directory in frontend worktree preserved as untracked (not committed).
- Phase 38I (schema intelligence) not started; this evidence unblocks it.
