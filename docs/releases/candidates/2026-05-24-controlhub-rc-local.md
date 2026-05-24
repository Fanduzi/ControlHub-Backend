# ControlHub Release Candidate Evidence

## Candidate

| Field | Value |
|---|---|
| Candidate ID | 2026-05-24-controlhub-rc-local |
| Date | 2026-05-24 |
| Backend commit | 2a275da |
| Frontend commit | 8881e25 |
| Backend worktree | CLEAN |
| Frontend worktree | CLEAN |
| Evaluator | Phase 31 RC evidence worker |
| Decision | GO |
| Decision reason | All required gates passed. Backend release-readiness-gates PASS. Frontend release:check PASS. Phase 31 resolved missing-test-data warning; remaining schema mismatch accepted. No unclassified failures. |

## Gate Policy

### Required Gates (must PASS for GO)

- Backend: `make release-readiness-gates` — PASS
- Frontend: `npm run release:check` — PASS

### Optional Gates (failure blocks GO only if run and failed)

- Frontend CDP smoke: `npm run release:smoke:cdp` — NOT RUN

## Backend Gates

| Gate | Command | Required | Result | Notes |
|---|---|---|---|---|
| Backend local gates | `make release-local-gates` | Yes | PASS | go test, go vet, go build, openapi-validate |
| Backend Docker gates | `make release-docker-gates` | Yes | PASS | 42 integration tests, 966 Schemathesis cases |

### Backend Local Gates Detail

- `go test -count=1 ./...` — 9/9 packages PASS
- `go vet ./...` — PASS
- `go build ./...` — PASS
- `make openapi-validate` — PASS

### Backend Docker Gates Detail

- `make test-integration` — 42/42 tests PASS (archive, legacy-import, mysql, relation, resource, topology, seed data)
- `make test-openapi-fuzz` — 27/27 operations, 966/966 cases PASS, all 4 Schemathesis checks passed (not_a_server_error, status_code_conformance, content_type_conformance, response_schema_conformance)

## Frontend Gates

| Gate | Command | Required | Result | Notes |
|---|---|---|---|---|
| Frontend local gates | `npm run release:local` | Yes | PASS | preflight, governance, typecheck, lint, unit, build |
| Frontend browser gates | `npm run release:e2e` | Yes | PASS | smoke 7/7, interaction 3/3, full E2E 50/50 |
| Frontend live smoke | `npm run release:smoke:cdp` | No | NOT RUN | No Chrome remote debugging target on port 9222 |

### Frontend Gate Detail

Frontend commit `19074c9` (branch `main`):
- `npm run release:local` PASS
- `npm run release:e2e` PASS
- Unit tests: 556/556 PASS
- E2E smoke: 7/7 PASS
- E2E interaction: 3/3 PASS
- Full E2E: 50/50 PASS

## Live Browser Smoke

| URL | Expected | Result | Notes |
|---|---|---|---|
| `/overview?environment=prod` | Attention queue loads | NOT RUN | CDP not available |
| `/databases?environment=prod` | Database list controls usable | NOT RUN | CDP not available |
| `/resources/14` | Abnormal cluster detail loads | NOT RUN | CDP not available |
| `/resources/22` | Healthy instance detail loads | NOT RUN | CDP not available |
| `/resources?page=1&pageSize=1` | Resource pagination loads | NOT RUN | CDP not available |
| `/audits?page=1&pageSize=1` | Audit pagination loads | NOT RUN | CDP not available |

## Warning Classification

| Warning | Classification | Justification |
|---|---|---|
| PATCH /resources/{id} repeatedly returned 404 due to missing valid generated ID | Resolved (Phase 31) | Fixed by providing Schemathesis with a known seed resource ID via TOML config parameter override and OpenAPI path parameter example. PATCH now exercises core update logic. |
| Schema validation mismatch on PATCH /resources/{id}, POST /auth/login, POST /resources | Partially Resolved (Phase 31) | POST /auth/login removed from mismatch list (seed credentials example exercises login correctly; no longer flagged by Schemathesis). PATCH /resources/{id} and POST /resources remain due to inherent referential integrity validation (environmentId, ownerId, resourceSubtype). OpenAPI schemas tightened with `additionalProperties: false` and `minProperties: 1` to match backend `DisallowUnknownFields()` and at-least-one-field validation. Remaining mismatch is accepted: reference IDs are dynamic and cannot be fully constrained in static schema. |

### Phase 31 Fuzz Warning Cleanup Results

Before (Phase 30):
- 2 warning types: Missing test data (1 op) + Schema mismatch (3 ops)
- 960 generated, 960 passed
- Examples phase: 1 passed, 26 skipped

After (Phase 31):
- 1 warning type: Schema mismatch (2 ops: PATCH /resources/{id}, POST /resources)
- 966 generated, 966 passed
- Examples phase: 4 passed, 23 skipped

Changes:
- Added `schemathesis.toml` config with path.id override for patchResource operation
- Added request body examples for POST /auth/login, POST /resources, PATCH /resources/{id}
- Added `additionalProperties: false` to ResourceCreateInput and ResourcePatchRequest (matches `DisallowUnknownFields()`)
- Added `minProperties: 1` to ResourcePatchRequest (matches "at least one mutable field" validation)
- Updated `openapi-fuzz.sh` to pass `--config-file`

## Skipped Optional Gates

| Gate | Status | Reason |
|---|---|---|
| Frontend CDP smoke | NOT RUN | No Chrome remote debugging target available on port 9222 |

## Known Gaps

1. **CDP live smoke NOT RUN** — No Chrome remote debugging target available on port 9222. This is an optional gate that requires a manually-started Chrome session. It is not included in `npm run release:check`. All automated E2E gates (smoke, interaction, full) passed via Playwright.

2. **GitHub Actions CI configured but not yet active remotely** — Both backend and frontend workflows are configured locally but have not been pushed to GitHub.
   - Backend: `.github/workflows/backend-ci.yml` at commit `2a275da`. Fast CI runs `make release-local-gates` on push/PR to main. Heavy CI runs `make release-docker-gates` via manual `workflow_dispatch` with `run_docker_gates=true`. Artifact: `.schemathesis-reports/` retained 7 days.
   - Frontend: `.github/workflows/frontend-ci.yml` at commit `8881e25`. Fast CI runs `npm run release:local` on push/PR to main. Manual E2E job (`npm run release:e2e`) is deferred: the frontend repo does not start a backend, and cross-repo backend bootstrap is not encoded in CI.
   - Neither workflow has run remotely. Status: configured locally; remote run pending push.

## Failure Classification

No failures.

| Failure | Classification | Evidence | Owner / Next Action |
|---|---|---|---|
| | | | |

## Go / No-Go Decision

Decision: **GO**

Reason: All required gates passed. Backend release-readiness-gates PASS (go test, go vet, go build, openapi-validate, 42 integration tests, 966 Schemathesis cases). Frontend release:check PASS (556 unit tests, 7 smoke E2E, 3 interaction E2E, 50 full E2E). Phase 31 resolved missing-test-data warning for PATCH /resources/{id}. Remaining schema mismatch on PATCH /resources/{id} and POST /resources is accepted: runtime referential integrity validation (environmentId, ownerId, resourceSubtype) cannot be fully encoded as static OpenAPI constraints without fragility. CDP smoke is optional and not run — not a blocker. Both backend and frontend worktrees are clean.
