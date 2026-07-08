# ControlHub Quality Baseline

## Purpose

This document defines the current quality baseline for ControlHub. It records
which commands protect which behavior, where coverage is missing, and which
checks block future phase completion.

Phase 28 established this baseline after Phases 16–27 added the full database
operator workflow across backend and frontend. No new product capability was
added in Phase 28.

## Backend Gates

| Command | What It Protects | Required Before Merge | Notes |
|---|---|---|---|
| `go test -count=1 ./...` | Unit and package-level behavior across all layers | Yes | 30 test files across api, model, service, repository, config, cutover, openapi. Runs without Docker. |
| `go vet ./...` | Static correctness checks | Yes | Runs without Docker |
| `go build ./...` | Compilation across packages | Yes | Runs without Docker |
| `make openapi-validate` | OpenAPI 3.1 YAML validity against JSON Schema | Yes | Runs without Docker |
| `make test-integration` | MySQL/Testcontainers: clean migration, write conflicts, topology SQL, unique constraints | Merge gate when Docker available | Requires Docker. Tests: archive, legacy-import, mysql, relation, resource, topology, testenv |
| `make test-openapi-fuzz` | Schemathesis fuzzing: 50 examples/operation, no-5xx, status conformance, content-type conformance, response schema conformance | Release/nightly gate or merge gate for API changes | Requires Docker + Schemathesis CLI |
| `make release-local-gates` | Composes `go test`, `go vet`, `go build`, `openapi-validate` into a single no-Docker gate | Every release candidate | Runs without Docker |
| `make release-docker-gates` | Composes `test-integration` and `test-openapi-fuzz` into a single Docker gate | Every release candidate when Docker available | Requires Docker |
| `make release-readiness-gates` | Composes `release-local-gates` + `release-docker-gates` into the strongest local backend readiness signal | Every release candidate | Requires Docker for full run |
| `.github/workflows/backend-ci.yml` (fast) | GitHub Actions CI: runs `make release-local-gates` on push/PR to main | Yes (after push) | Private repo: runner minutes and artifact storage count against allowance. No Docker needed. |
| `.github/workflows/backend-ci.yml` (heavy) | GitHub Actions CI: runs `make release-docker-gates` via manual `workflow_dispatch` | Manual only | Private repo: costs Docker minutes. Uploads `.schemathesis-reports/` for 7 days. Not required on every push until cost/runtime is observed. |

### Backend Test Inventory (33 files)

**API handler tests** (10 files):
- `audit_handler_test.go`, `auth_handler_test.go`, `dictionary_handler_test.go`
- `docs_handler_test.go`, `health_handler_test.go`, `query_credential_handler_test.go`
- `query_target_handler_test.go`
- `relation_handler_test.go`, `resource_handler_test.go`, `topology_handler_test.go`

**Model tests** (4 files):
- `pagination_test.go`, `query_credential_test.go`, `resource_test.go`, `taxonomy_test.go`

**Service tests** (10 files):
- `auth_service_test.go`, `dictionary_service_test.go`, `profile_service_test.go`
- `query_credential_service_test.go`, `query_target_service_test.go`
- `resource_read_service_test.go`, `resource_write_service_test.go`
- `topology_semantics_test.go`, `topology_service_test.go`

**Repository tests** (1 file):
- `audit_repository_test.go`

**Config tests** (1 file):
- `config_test.go`

**Cutover tests** (1 file):
- `local_test.go`

**OpenAPI tests** (1 file):
- `openapi_test.go`

**Integration tests** (11 files, require Docker):
- `archive_test.go`, `legacy_import_test.go`, `mysql_test.go`
- `openapi_fuzz_test.go`, `query_credential_api_test.go`, `query_credential_repository_test.go`
- `query_target_test.go`, `relation_test.go`
- `resource_test.go`, `testenv_test.go`, `topology_test.go`

## Frontend Gates

| Command | What It Protects | Required Before Merge | Notes |
|---|---|---|---|
| `npx tsc --noEmit -p tsconfig.json` | TypeScript contract and type safety | Yes | Runs without backend |
| `npm run lint` | ESLint rules and unused code | Yes | Runs without backend |
| `npm run test` | Vitest unit/component behavior (64 test files, 769 tests after Phase 38D) | Yes | Runs without backend |
| `npm run build` | Next.js production build and SSR compatibility | Yes | Runs without backend |
| `npm run check:e2e-governance` | Browser-test policy: no stderr suppression, no success-path screenshots, console/network guards, UI login for SSR | Yes | Runs without backend |
| `npm run check:e2e-preflight` | Stale `:3100` dev server and `:8081` E2E proxy detection before Playwright runs | Yes (before E2E) | Detects and reports stale listeners; does not kill processes |
| `npm run test:e2e:smoke` | Core console reachability (login, shell, dictionaries) | Yes when backend available | Requires running backend |
| `npm run test:e2e:interaction` | Sheets/dropdowns/back navigation/accent stability | Yes when frontend interaction code changes | Requires running backend |
| `npm run test:e2e` | Full 11-spec browser regression | Merge gate before phase close | Requires running backend. 50/50 passed after Phase 28 frontend. |
| `.github/workflows/frontend-ci.yml` (fast) | GitHub Actions CI: runs `npm run release:local` on push/PR to main | Yes (after push) | Private repo: runner minutes count against allowance. No backend needed. |
| `.github/workflows/frontend-ci.yml` (E2E) | GitHub Actions CI: runs `npm run release:e2e` via manual `workflow_dispatch run_e2e=true` | Manual only (Phase 35) | Cross-repo E2E implemented. Starts MySQL, runs backend migrations, starts backend server, then runs frontend `release:e2e`. Backend checkout via `CONTROLHUB_BACKEND_CHECKOUT_TOKEN` (PAT, read-only). Artifacts: playwright-report, test-results, backend.log. Evidence: run `26519616558` PASS (smoke 7/7, interaction 3/3, full 50/50). Push/PR still run fast frontend CI only. |

### Frontend Test Inventory

**E2E specs** (13 files):
- `console-ux.spec.ts` — Console shell navigation, environment context
- `databases-sheet.spec.ts` — Database list interactions
- `list-pagination.spec.ts` — Resource and audit list query params
- `login.spec.ts` — Login and auth session
- `operator-console-smoke.spec.ts` — Core smoke reachability
- `operator-database-workflow.spec.ts` — Full database operator workflow
- `operator-interaction-stability.spec.ts` — Sheet/dropdown/back-nav stability
- `query-credential-settings.spec.ts` — Query credential admin settings E2E (Phase 38A)
- `query-workbench.spec.ts` — Query Workbench target selection, read-only SQL execution, and history E2E (Phase 38D)
- `resource-archive.spec.ts` — Resource archive action
- `resources-sheet.spec.ts` — Resource detail sheet
- `settings.spec.ts` — Settings dictionaries
- `topology.spec.ts` — Topology load and same-origin API proxy

**Unit/component tests** (64 files, 769 tests after Phase 38D; representative families):
- Component tests: shell/navigation, settings, resource/database panels, Query Workbench, and Query Credential Settings.
- E2E harness tests: console guards and interaction stability.
- Lib tests: auth-role recovery, query credential operations, query target display, database operational models, environment params, pagination/search params, resource copy/summary, and view models.
- Page integration tests: list pagination and resource detail page.
- Service tests: API client, auth/query credential/query execution/query target/resource/settings service wrappers.
- Script tests: E2E preflight and governance checks.
- Topology tests: topology mapper, semantic mapper, panel, and service behavior.
- Hook/API proxy tests: sidebar state and same-origin proxy/CORS behavior.

**Scripts** (2 files):
- `scripts/check-e2e-governance.mjs` — Policy compliance checker
- `scripts/check-e2e-preflight.mjs` — Stale dev server/proxy port detection (does not kill processes)

**E2E harness** (5 files):
- `e2e/auth.ts`, `e2e/backend-health.ts`, `e2e/console-guards.ts`
- `e2e/dev-server-wrapper.sh`, `e2e/interaction-stability.ts`

## Coverage Matrix

| Capability | Backend Unit | Backend Integration | OpenAPI/Fuzz | Frontend Unit/Component | E2E Smoke | E2E Interaction | E2E Workflow | Manual Browser | Gap / Next Action |
|---|---|---|---|---|---|---|---|---|---|
| Login and auth session | `auth_service_test`, `auth_handler_test` | No | Schema covers POST /auth/login | No | Yes (`login.spec`) | No | Yes (`operator-database-workflow`) | Yes | E2E covers this end-to-end. Backend token/session mechanics tested at unit level. |
| Console shell navigation | No | No | No | `sidebar.test`, `topbar.test`, `use-sidebar-state.test` | Yes (`operator-console-smoke`) | Yes (`operator-interaction-stability`) | Yes | Yes | Covered. |
| Environment context | No | No | No | `environment-provider.test` | Partial (`operator-console-smoke`) | Partial (`operator-interaction-stability`) | Yes (`operator-database-workflow`) | Yes | Add cases only if regressions recur. |
| Resource list pagination/query params | `pagination_test`, `resource_handler_test` | `resource_test` (integration) | Schema covers GET /resources | `pagination-controls.test`, `resource-table.test`, `list-page-search-params.test`, `pages.list-pagination.test` | No | No | Yes (`list-pagination.spec`) | Yes | Covered by list-pagination E2E after Phase 27B. |
| Database list search/filter/sort/signal | No | No | Schema covers GET /resources?type=database_* | `database-table.test`, `database-operational-signal.test`, `multi-select-filter.test` | No | Yes (`databases-sheet.spec`) | Yes (`operator-database-workflow`) | Yes | Backend rollup tested at service level. Frontend signal logic tested at unit level. |
| Database detail cluster abnormal member workflow | `resource_read_service_test` | `resource_test` (integration) | Schema covers GET /resources/:id | `database-decision-deck.test`, `database-consistency-panel.test`, `cluster-members-table.test`, `database-read-model-consistency.test` | No | No | Yes (`operator-database-workflow`) | Yes | Covered by full E2E workflow. |
| Database detail healthy instance workflow | `resource_read_service_test` | `resource_test` (integration) | Schema covers GET /resources/:id | `database-instance-facts-panel.test`, `database-supporting-details.test`, `database-operator-workbench.test` | No | No | Yes (`operator-database-workflow`) | Yes | Covered by full E2E workflow. |
| Overview attention queue | No | No | No | `overview-content.test` | Partial (`operator-console-smoke`) | No | Yes (`operator-database-workflow`) | Yes | Covered by database workflow E2E path. |
| Topology load and same-origin API proxy | `topology_service_test`, `topology_semantics_test` | `topology_test` (integration) | Schema covers GET /topology | `topology-mapper.test`, `topology-mapper-semantic.test`, `topology-panel.test`, `topology-service.test`, `e2e-api-proxy-cors.test` | Yes (`topology.spec`) | Yes (`operator-interaction-stability`) | Yes (`topology.spec`) | Yes | Covered. Same-origin /__api proxy tested at E2E and CORS unit level. |
| Audit list pagination/filtering | `audit_handler_test`, `audit_repository_test` | No | Schema covers GET /audit-events | `audit-table.test`, `audits.test` | No | No | Yes (`list-pagination.spec`) | Yes | Covered by list-pagination E2E. |
| Settings dictionaries | `dictionary_handler_test`, `dictionary_service_test` | No | Schema covers GET /dictionaries/* | `settings.test` | Yes (`settings.spec`) | No | No | Yes | Smoke-only is acceptable for this static data page. |
| Backend resource CRUD/read models | `resource_handler_test`, `resource_read_service_test`, `resource_write_service_test`, `resource_test` (model) | `resource_test` (integration), `relation_test` | Schema covers all resource endpoints | `resource-detail-sheet.test`, `resource-detail-sheet-loader.test`, `create-resource-sheet.test`, `edit-resource-sheet.test` | Partial (`resources-sheet.spec`) | Yes (`resources-sheet.spec`) | Yes | Yes | Covered. |
| Backend database operational summary | `resource_read_service_test` | Yes (via resource integration) | OpenAPI schema for GET /resources/:id includes operational summary fields | `database-operational-signal.test`, `database-read-model-consistency.test` | No | No | Yes (`operator-database-workflow`) | Yes | Covered. Operational summary absence on instances is by design and tested at unit level. |
| Query Workbench target directory (Phase 36) | `query_target_service_test`, `query_target_handler_test` | `query_target_test` | Schema covers GET /query-targets; fuzz exercised 28/28 ops after Phase 36A | `query-target-display.test`, `query-workbench-search-params.test`, `query-targets.test`, `query-workbench.test`, `pages.query.test` | Yes (`query-workbench.spec`) | No | Yes (manual cross-repo E2E run 27896155506) | Yes | Covered as a locked directory/workbench shell only. No query execution, credentials, SQL/Redis/Mongo execution, export, saved query, or query history API exists in Phase 36. |
| Query credential metadata management (Phase 38A) | `query_credential_service_test`, `query_credential_handler_test`, `query_credential_test` (model) | `query_credential_api_test`, `query_credential_repository_test` | Schema covers GET/PUT/DELETE /query-targets/{id}/credential; fuzz exercised 33/33 ops after Phase 38A | `query-credential-settings.test` (service + component), i18n label tests | No | No | Yes (`query-credential-settings.spec`, 11/11 credential + 7/7 workbench passed) | Yes | Admin credential settings at `/settings/query-credentials`; read-only workbench credential status. No DSN/password in request/response/browser/audit. Real E2E with backend + Phase 37H dedicated query MySQL fixture. Frontend CI run `28252354273` (`release-local` PASS). |
| Query Workbench read-only SQL and admin follow-ups (Phase 38C/38D) | `query_guard_test`, `query_executor` tests | `query_execution_test` (SHOW DATABASES, SHOW TABLES FROM db, SHOW COLUMNS FROM db.table, DESCRIBE db.table, forbidden SHOW/USE rejection) | OpenAPI copy updated from SELECT-only to read-only SQL; Schemathesis 1274 cases passed after backend Phase 38D | `query-workbench.test`, `auth-role.test`, `query-credential-settings.test` | No | No | Yes (`query-workbench.spec` 12/12 and `query-credential-settings.spec` 15/15 against real backend + query fixture) | Yes | Allows explicit read-only metadata statements while preserving write/session/privilege guardrails. Query target selection is searchable and consolidated; settings entry and direct URL admin-role recovery are covered. Browser role decode is presentation-only; server remains authorization boundary. Frontend CI run `28759996006` (`release-local` PASS). |
| Query Workbench SQL editor foundation (Phase 38F) | No backend changes | No backend changes | No OpenAPI changes | `query-workbench.test`, `query-sql-format.test` | No | No | Yes (`query-workbench.spec` 16/16 against real backend + query fixture) | Yes | Frontend-only phase. CodeMirror 6 editor, local SQL formatting, multi-worksheet state with per-worksheet target sync and race guards, keyboard shortcuts (Mod-Enter run, Mod-Shift-f format). 800/800 unit/component tests. Lint 0 errors / 4 warnings. Frontend CI run `28948152448` (success). Real E2E 16/16 passed on feature branch and post-merge main. No backend edits, no SQL guard changes, no DSN/password browser state, no `actorUserId`, no credential edit controls, no worksheet persistence. |
| OpenAPI schema validity | `openapi_test.go` | No | `make openapi-validate` + Schemathesis fuzz | N/A | N/A | N/A | N/A | N/A | Dual validation: JSON Schema validation + runtime fuzzing. |
| OpenAPI fuzz behavior | N/A | `openapi_fuzz_test.go` | Schemathesis: 50 examples/operation, no-5xx, status conformance, content-type conformance, response schema conformance | N/A | N/A | N/A | N/A | N/A | Requires Docker. Run before merge when API changes or as nightly gate. |

## Known Remaining Gaps

1. **CI runners active remotely**: Backend GitHub Actions CI runs `make release-local-gates` on push/PR to main; heavy CI runs `make release-docker-gates` via manual `workflow_dispatch`. Frontend GitHub Actions CI runs `npm run release:local` on push/PR to main. Phase 35 implemented cross-repo E2E: manual `workflow_dispatch run_e2e=true` starts MySQL, runs backend migrations, starts backend server, then runs frontend `release:e2e` (evidence: run `26519616558` PASS). Backend checkout via `CONTROLHUB_BACKEND_CHECKOUT_TOKEN` (PAT, read-only). Artifacts: playwright-report, test-results, backend.log. Private repo minutes and artifact storage count against the repository owner's allowance.

2. **Integration test coverage for audit repository**: `audit_repository_test.go` is a unit test with fakes. The MySQL-backed audit query paths are not exercised in integration tests.

3. **No automated contract smoke between OpenAPI schema and frontend TypeScript types**: TypeScript types are derived from backend responses during development, but there is no automated check that they stay in sync. The OpenAPI schema and E2E recorded-request harness provide indirect coverage.

4. **Visual regression**: Not adopted. Current pain is semantic/interactivity, not pixel drift. See research notes for tradeoff analysis.

5. **Cross-browser testing**: Deferred. All E2E runs use Chromium. No Firefox/WebKit matrix until cross-browser issues appear.

6. **Seed data constants**: E2E tests reference seed resources by name (`analytics-ch-cluster-prod`, `Analytics ClickHouse Node 02`) and ID (`14`, `22`). These are stable in the seed migration but not centralized as typed constants.

## Frontend Baseline

Frontend Phase 38F commit `499c235` (main, Query Workbench SQL editor foundation):
- CodeMirror 6 SQL editor replaces the plain textarea; syntax highlighting, line numbers, bracket matching, fold gutter.
- Local SQL formatting via `sql-formatter` with engine-aware dialect selection (MySQL/TiDB → mysql, fallback → sql). No server round-trip.
- Multi-worksheet state model: per-worksheet tab bar, add/rename/close, local-only state (no persistence).
- Per-worksheet target synchronization: each worksheet owns its target; switching worksheets restores target context in schema/governance.
- Race guards: async work keyed by `(worksheetId, targetId)`; stale results never paint into the wrong worksheet.
- Keyboard shortcuts: `Cmd/Ctrl+Enter` run, `Cmd/Ctrl+Shift+F` format.
- Unit/component tests: 800/800 passed.
- Real Query Workbench E2E: 16/16 passed against backend `:8080` + dedicated query MySQL fixture. Feature branch 16/16, post-merge main 16/16.
- Lint: 0 errors, 4 warnings.
- Frontend CI run `28948152448`: success.
- Stale claim corrected: earlier "backend unavailable / E2E not run" was wrong; real E2E passed.
- No backend product edits, no SQL guard changes, no DSN/password browser state, no `actorUserId`, no Workbench credential edit controls, no saved query/export/approval/worksheet persistence, no tag/release/deploy.

Frontend Phase 38D commit `e632e44` (main, Query Workbench admin follow-ups):
- `/query` target selection consolidated into a searchable picker with inline filters and compact target chips.
- `/settings` exposes Query Credential settings; `/settings/query-credentials` supports direct URL access for admin users when only the auth cookie remains.
- Frontend role recovery decodes the already-issued bearer token only as a presentation hint; browser code does not verify HMACs and does not authorize credential writes. Backend token verification and admin checks remain the enforcement boundary.
- Read-only SQL copy and E2E align with backend Phase 38C/38D guard behavior: SELECT, SHOW TABLES, DESCRIBE, SHOW DATABASES, and qualified metadata exploration are covered while unsafe statements remain rejected.
- Real E2E against backend + Phase 37H dedicated query MySQL fixture: query credential 15/15 passed, Query Workbench 12/12 passed. No fake backend. No DSN/password leak.
- Frontend CI run `28759996006`: `release-local` succeeded, `release-e2e` skipped (manual/live-backend path only).
- No backend product edits, no `actorUserId` sent, no Workbench credential edit controls, no tag/release/deploy.

Frontend Phase 38A commit `0974505` (main, Query Credential Settings UI):
- `/settings/query-credentials` page added as admin-only credential metadata management surface.
- Query Workbench remains read-only for credential status; no edit controls exposed to query users.
- Credential metadata configuration boundary: settings/admin only, not `/query`.
- Backend/frontend boundary: frontend sends only `credentialRef`, `enabled`, `environmentPolicy`, optional `confirmAllEnvironments`; never sends `actorUserId`, DSN/password, host, port, or engine.
- Real E2E against backend + Phase 37H dedicated query MySQL fixture: query credential 11/11 passed, query workbench 7/7 passed. No fake backend. No DSN/password leak.
- Frontend CI run `28252354273`: `release-local` succeeded, `release-e2e` skipped (manual only, expected for non-manual push).
- No backend product edits, no secret UI, no `actorUserId` sent, no Workbench edit controls, no tag/release/deploy.

Frontend Phase 36B commit `ff2681a` (main, Query Workbench shell):
- `/query` page added as a locked Query Workbench shell backed by backend Phase 36A `GET /query-targets`.
- Query execution remains disabled: no execute API, no credentials, no export, no SQL/Redis/Mongo execution.
- Local frontend gates passed: preflight, E2E governance, typecheck, lint, full unit/component tests, build.
- Frontend fast CI PASS: run `27896150307`.
- Manual cross-repo E2E PASS: run `27896155506` (`release-local` + `release-e2e`; query workbench smoke included).
- Tests after Phase 36B: 12 E2E specs, 59 unit/component/service/page test files, 586 tests.

Frontend Phase 29 commit `72ec317` (branch `feat/phase-29-release-readiness-mechanism`):
- release:local PASS
- release:e2e PASS
- unit tests 556 PASS
- E2E smoke 7 PASS
- E2E interaction 3 PASS
- full E2E 50 PASS
- CDP live smoke NOT RUN — no Chrome remote debugging target available on port 9222
- `check:e2e-preflight` detects stale `:3100`/`:8081` listeners (does not kill processes)
- `check:e2e-governance` enforces browser-test policy

Frontend Phase 28 commit `0342ec9` (base `72bcb27`):
- 10 quality npm scripts
- 11 E2E specs, 50/50 passed
- 53 unit/component test files, 547/547 tests passed
- `check:e2e-preflight` detects stale `:3100`/`:8081` listeners (does not kill processes)
- `check:e2e-governance` enforces browser-test policy

## Merge Blocking Rules

1. **Do not** complete a phase with failing unit, lint, typecheck, or build gates.
2. **Do not** call full E2E failures pre-existing without identical main-branch comparison evidence.
3. **Do not** merge browser-facing changes without at least smoke E2E.
4. **Do not** merge interaction changes (sheets, dropdowns, navigation) without interaction E2E.
5. **Do not** merge backend API or read-model changes without OpenAPI validation and targeted backend tests.
6. **Do not** skip Docker-dependent backend gates without stating why in the phase report.
7. **Do not** add `stderr: "ignore"`, `stdout: "ignore"`, success-path screenshots, or broad output suppression in E2E specs.
8. **Do not** merge with dirty worktree or untracked artifacts that are not intentionally ignored.
