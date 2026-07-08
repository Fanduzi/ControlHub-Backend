# ControlHub Release Readiness Summary

## Candidate

| Field | Value |
|---|---|
| Summary ID | 2026-05-26-controlhub-release-readiness-summary |
| Date | 2026-05-26 |
| Backend commit | 2276d63 |
| Frontend commit | 8881e25 |
| Decision | GO for release candidate |
| Release status | NOT RELEASED |
| Tag status | NO TAG |
| Deploy status | NO DEPLOY |

## Repository Status

| Repository | HEAD | Status | Synced with origin |
|---|---|---|---|
| Backend (GolangProjects/ControlHub) | `2276d63` | CLEAN | Yes |
| Frontend (JsProjects/ControlHub) | `8881e25` | CLEAN | Yes |

## GitHub Actions Results

| CI Run | Trigger | Result | Run URL |
|---|---|---|---|
| Backend fast CI (commit `a151278`) | push | PASS (33s) | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/26409101046 |
| Backend manual heavy CI (commit `a151278`) | workflow_dispatch | PASS (1m57s) | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/26409102650 |
| Backend evidence update fast CI (commit `2276d63`) | push | PASS (32s) | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/26409232813 |
| Frontend fast CI (commit `8881e25`) | push | PASS (3m19s) | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/26356705010 |
| Frontend manual E2E CI (Phase 35) | workflow_dispatch `run_e2e=true` | PASS | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/26519616558 |
| Frontend fast CI (Phase 35 commit `04256a7`) | push | PASS | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/26519432025 |
| Backend fast CI (Phase 36A commit `0579b29`) | push | PASS | https://github.com/Fanduzi/ControlHub-Backend/actions/runs/27875169683 |
| Frontend fast CI (Phase 36B commit `ff2681a`) | push | PASS | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/27896150307 |
| Frontend manual E2E CI (Phase 36B) | workflow_dispatch `run_e2e=true` | PASS | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/27896155506 |
| Frontend fast CI (Phase 38A commit `0974505`) | push | `release-local` PASS, `release-e2e` skipped (manual only) | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/28252354273 |
| Frontend fast CI (Phase 38F commit `499c235`) | push | success | https://github.com/Fanduzi/ControlHub-Frontend/actions/runs/28948152448 |

### Backend Heavy CI Detail (Run 26409102650)

- `release-local-gates`: PASS — go test, go vet, go build, openapi-validate
- `release-docker-gates`: PASS — integration tests + Schemathesis fuzz
- Schemathesis version: `4.15.2` (pinned in `.github/workflows/backend-ci.yml`)

## Gate Summary

### Backend Gates

| Gate | Local | Remote |
|---|---|---|
| `go test -count=1 ./...` | PASS (9/9 packages) | PASS (via release-local-gates) |
| `go vet ./...` | PASS | PASS (via release-local-gates) |
| `go build ./...` | PASS | PASS (via release-local-gates) |
| `make openapi-validate` | PASS | PASS (via release-local-gates) |
| `make test-integration` | PASS (49/49 tests) | PASS (via release-docker-gates) |
| `make test-openapi-fuzz` | PASS (27/27 ops, 969/969 cases) | PASS (via release-docker-gates) |

### Frontend Gates

| Gate | Local | Remote |
|---|---|---|
| `npm run release:local` | PASS (preflight, governance, typecheck, lint, unit, build) | PASS (556/556 unit tests) |
| `npm run release:e2e` | PASS (smoke 7/7, interaction 3/3, full 50/50) | PASS (smoke 7/7, interaction 3/3, full 50/50, local release 54 files 556 tests) |
| `npm run release:smoke:cdp` | NOT RUN (no Chrome CDP) | NOT RUN |

### OpenAPI Fuzz Detail

- Schemathesis v4.15.2, 27/27 operations, 969/969 cases, exit 0
- 4 checks: `not_a_server_error`, `status_code_conformance`, `content_type_conformance`, `response_schema_conformance` — all passed
- 1 accepted warning: `validation_mismatch` on 4 operations (PATCH /resources/{id}, POST /auth/login, POST /resources, POST /resources/{id}/relations)

## Defects Found and Fixed

| Defect | Phase | Fix |
|---|---|---|
| POST /resources/{id}/archive 500 for oversized reason | Phase 33 | `MaxArchiveReasonLength = 512` + service validation |
| Labels with unicode control characters caused 500 | Phase 33D | `validateResourceLabels()` + `containsControlChars()` |
| UpdateResource duplicate key 500 | Phase 33D | MySQL 1062 mapping to `ErrResourceConflict` → 409 |
| Schemathesis v4.19.0 `validation_mismatch` exit 1 | Phase 33E | Pin backend CI to Schemathesis `4.15.2` |
| v4.19 pin confirmed intentional after full investigation | Phase 34C | Option A: keep pin, no CLI/TOML override exists for engine-level `validation_mismatch` |
| Query target directory read model added | Phase 36A | `GET /query-targets` exposes derived database query target context; no credentials or execution |
| Locked Query Workbench shell added | Phase 36B | Frontend `/query` consumes query targets and keeps Run/Explain/Export disabled; cross-repo E2E passed |

## Known Accepted Warnings / Boundaries

1. **Runtime referential integrity validation mismatch** — Schemathesis fuzzes FK-like integer fields (`environmentId`, `ownerId`, `toResourceId`) to values that do not exist in seed data. Backend correctly rejects those values. This is not a 5xx, status code, content type, or schema contract bug. Remaining mismatch is accepted.

2. **Backend heavy CI pins Schemathesis `4.15.2`** — v4.19.0 treats DB-backed `validation_mismatch` as operation-level failure (exit 1). Pinning avoids CI failure from runtime referential integrity, not from product defects. Warnings remain visible. Configured checks still cause CI failure.

3. **Phase 34C resolved — Schemathesis pin confirmed intentional** — Phase 34C investigated all options for unpinning Schemathesis from 4.15.2. Decision: Option A (keep the pin). v4.19 `validation_mismatch` is an engine-level aggregate classification with no CLI/TOML override. Hooks (Phase 34B) can mutate FK fields but cannot change operation-level pass/fail. Option B (custom runner) and Option C (contract reshaping) rejected. Pin can be reconsidered when Schemathesis adds an engine-level toggle. See `docs/superpowers/notes/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md`.

4. **CDP live smoke not run** — Optional gate. Requires manually-started Chrome remote debugging session. Not included in `npm run release:check`. All automated E2E gates passed via Playwright.

5. **Frontend cross-repo E2E CI implemented (Phase 35)** — Manual `workflow_dispatch run_e2e=true` starts MySQL, runs backend migrations, starts backend server, then runs frontend `release:e2e`. Backend checkout uses `CONTROLHUB_BACKEND_CHECKOUT_TOKEN` (PAT, read-only access to Fanduzi/ControlHub-Backend). Artifacts uploaded: playwright-report, test-results, backend.log. Full E2E is manual only; push/PR still run fast frontend CI only. Evidence: run `26519616558` PASS.

6. **Query Workbench execution remains intentionally disabled (Phase 36)** — Backend Phase 36A exposes query targets from existing database resource metadata through `GET /query-targets`; frontend Phase 36B renders `/query` as a locked workbench shell. This is a capability directory and product shell only. There is no query execution API, no credential model, no SQL/Redis/Mongo execution path, no export path, no saved-query API, and no query-history API. Evidence: backend fast CI run `27875169683` PASS; frontend fast CI run `27896150307` PASS; manual cross-repo E2E run `27896155506` PASS.

7. **Phase 38A Query Credential Metadata Management complete** — Backend Phase 38A (B1–B6) added credential metadata APIs (`GET/PUT/DELETE /query-targets/{id}/credential`) with admin-only authorization, runtime status inspection, readiness correction, and no-secret storage. Frontend Phase 38A (F1–F3) added admin credential settings UI at `/settings/query-credentials`, read-only Query Workbench credential status, and real E2E against backend + Phase 37H dedicated query MySQL fixture. Credential metadata configuration lives in settings/admin, not Query Workbench. Backend stores metadata only; frontend sends only `credentialRef`, `enabled`, `environmentPolicy`, optional `confirmAllEnvironments`. DSN/password never enters request/response/browser state/audit rows. Evidence: backend evidence `docs/superpowers/notes/2026-06-24-phase-38a-query-credential-metadata-management-evidence.md`; frontend main `0974505`; frontend CI run `28252354273` (`release-local` succeeded, `release-e2e` skipped as expected for non-manual push); real E2E query credential 11/11 passed, query workbench 7/7 passed; no fake backend; no DSN/password leak; no `actorUserId` sent; no Workbench edit controls.

8. **Phase 38F Query Workbench SQL Editor Foundation complete** — Frontend-only phase. CodeMirror 6 SQL editor replaces the plain textarea; local SQL formatting via `sql-formatter`; multi-worksheet state model with per-worksheet target synchronization and race guards; keyboard shortcuts (`Cmd/Ctrl+Enter` run, `Cmd/Ctrl+Shift+F` format). 800/800 unit/component tests. Real Query Workbench E2E: 16/16 passed against backend `:8080` + dedicated query MySQL fixture (feature branch and post-merge main). Lint: 0 errors, 4 warnings. Frontend CI run `28948152448` (success). Stale claim corrected: earlier "backend unavailable / E2E not run" was wrong. No backend product edits, no SQL guard changes, no DSN/password browser state, no `actorUserId`, no Workbench credential edit controls, no saved query/export/approval/worksheet persistence, no tag/release/deploy. Evidence: `docs/superpowers/notes/2026-07-08-phase-38f-query-workbench-sql-editor-evidence.md`.

## Non-actions

- No tag created
- No release published
- No deploy executed
- No publish to any registry or CDN

## Evidence Trail

| Document | Path |
|---|---|
| RC evidence (original) | `docs/releases/candidates/2026-05-24-controlhub-rc-local.md` |
| Release hardening checklist | `docs/release-hardening-checklist.md` |
| Phase 33C investigation report | `docs/releases/candidates/phase-33c-investigation-report.md` |
| Phase 33E spec | `docs/superpowers/specs/2026-05-25-phase-33e-schemathesis-ci-version-policy.md` |
| Phase 33E plan | `docs/superpowers/plans/2026-05-25-phase-33e-schemathesis-ci-version-policy.md` |
| Phase 34B hook feasibility note | `docs/superpowers/notes/2026-05-26-phase-34b-schemathesis-hook-feasibility.md` |
| Phase 34C spec | `docs/superpowers/specs/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md` |
| Phase 34C plan | `docs/superpowers/plans/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md` |
| Phase 34C evidence note | `docs/superpowers/notes/2026-05-26-phase-34c-schemathesis-validation-mismatch-policy.md` |
| Phase 36 Query Workbench roadmap | `docs/superpowers/specs/2026-06-20-query-workbench-roadmap.md` |
| Phase 36 Query Workbench design | `docs/superpowers/specs/2026-06-20-phase-36-query-workbench-foundation.md` |
| Phase 36 Query Workbench implementation plan | `docs/superpowers/plans/2026-06-20-phase-36-query-workbench-foundation.md` |
| Phase 36 Bytebase UI research | `docs/superpowers/notes/2026-06-20-phase-36-bytebase-ui-research.md` |
| This summary | `docs/releases/candidates/2026-05-26-controlhub-release-readiness-summary.md` |
| Phase 38F evidence note | `docs/superpowers/notes/2026-07-08-phase-38f-query-workbench-sql-editor-evidence.md` |

## Final Decision

**GO for RC baseline.**

All required gates passed locally and on GitHub Actions. All product defects discovered during heavy CI have been fixed. Backend heavy CI passes with Schemathesis pinned to `4.15.2`. Frontend fast CI passes. No unclassified failures. No tag, release, or deploy.

If formal release is desired, next step is explicit tag/release planning and authorization.
