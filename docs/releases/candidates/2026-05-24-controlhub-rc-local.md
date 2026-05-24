# ControlHub Release Candidate Evidence

## Candidate

| Field | Value |
|---|---|
| Candidate ID | 2026-05-24-controlhub-rc-local |
| Date | 2026-05-24 |
| Backend commit | e11575a |
| Frontend commit | 19074c9 |
| Backend worktree | CLEAN |
| Frontend worktree | CLEAN |
| Evaluator | Phase 30 RC evidence worker |
| Decision | GO |
| Decision reason | All required gates passed. Backend release-readiness-gates PASS. Frontend release:check PASS. All warnings classified. No unclassified failures. |

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
| Backend Docker gates | `make release-docker-gates` | Yes | PASS | 34 integration tests, 960 Schemathesis cases |

### Backend Local Gates Detail

- `go test -count=1 ./...` — 10/10 packages PASS
- `go vet ./...` — PASS
- `go build ./...` — PASS
- `make openapi-validate` — PASS

### Backend Docker Gates Detail

- `make test-integration` — 34/34 tests PASS (archive, legacy-import, mysql, relation, resource, topology, seed data)
- `make test-openapi-fuzz` — 27/27 operations, 960/960 cases PASS, all 4 Schemathesis checks passed (not_a_server_error, status_code_conformance, content_type_conformance, response_schema_conformance)

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
| PATCH /resources/{id} repeatedly returned 404 due to missing valid generated ID | Accepted | Schemathesis generates random IDs that rarely match existing resources. All configured checks passed (not_a_server_error, status_code_conformance, content_type_conformance, response_schema_conformance). No user-facing or contract risk. |
| Schema validation mismatch on PATCH /resources/{id}, POST /auth/login, POST /resources | Follow-Up | API validation is stricter than the OpenAPI schema for generated invalid data. All configured Schemathesis checks passed. Should tighten OpenAPI schema constraints in future hardening. |

## Skipped Optional Gates

| Gate | Status | Reason |
|---|---|---|
| Frontend CDP smoke | NOT RUN | No Chrome remote debugging target available on port 9222 |

## Known Gaps

1. **CDP live smoke NOT RUN** — No Chrome remote debugging target available on port 9222. This is an optional gate that requires a manually-started Chrome session. It is not included in `npm run release:check`. All automated E2E gates (smoke, interaction, full) passed via Playwright.

2. **No CI runner** — All gates are local-only. No GitHub Actions or equivalent pipeline.

## Failure Classification

No failures.

| Failure | Classification | Evidence | Owner / Next Action |
|---|---|---|---|
| | | | |

## Go / No-Go Decision

Decision: **GO**

Reason: All required gates passed. Backend release-readiness-gates PASS (go test, go vet, go build, openapi-validate, 34 integration tests, 960 Schemathesis cases). Frontend release:check PASS (556 unit tests, 7 smoke E2E, 3 interaction E2E, 50 full E2E). All warnings classified as Accepted or Follow-Up with no blocking warnings. CDP smoke is optional and not run — not a blocker. Both backend and frontend worktrees are clean.
