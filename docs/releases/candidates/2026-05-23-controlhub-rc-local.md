# ControlHub Release Candidate Evidence

## Candidate

| Field | Value |
|---|---|
| Candidate ID | 2026-05-23-controlhub-rc-local |
| Date | 2026-05-23 |
| Backend commit | ec6b9c6 |
| Frontend commit | 72ec317 |
| Evaluator | Phase 29 backend worker |
| Decision | GO |

## Backend Gates

| Gate | Command | Result | Notes |
|---|---|---|---|
| Backend local gates | `make release-local-gates` | PASS | 10 packages tested, 0 failures |
| Backend Docker gates | `make release-docker-gates` | PASS | 34 integration tests PASS, 960 Schemathesis cases PASS |

### Backend Local Gates Detail

- `go test -count=1 ./...` — 10/10 packages PASS
- `go vet ./...` — PASS
- `go build ./...` — PASS
- `make openapi-validate` — PASS

### Backend Docker Gates Detail

- `make test-integration` — 34/34 tests PASS (archive, legacy-import, mysql, relation, resource, topology, seed data)
- `make test-openapi-fuzz` — 27/27 operations, 960/960 cases PASS, all 4 Schemathesis checks passed (not_a_server_error, status_code_conformance, content_type_conformance, response_schema_conformance)

## Frontend Gates

| Gate | Command | Result | Notes |
|---|---|---|---|
| Frontend local gates | `npm run release:local` | PASS | preflight, governance, typecheck, lint, unit, build |
| Frontend browser gates | `npm run release:e2e` | PASS | smoke 7, interaction 3, full E2E 50 |
| Frontend live smoke | `npm run release:smoke:cdp` | NOT RUN | No Chrome remote debugging target on port 9222 |

### Frontend Gate Detail

Frontend commit `72ec317` (branch `feat/phase-29-release-readiness-mechanism`):
- `npm run release:local` PASS
- `npm run release:e2e` PASS
- Unit tests: 556 PASS
- E2E smoke: 7 PASS
- E2E interaction: 3 PASS
- Full E2E: 50 PASS

## Live Browser Smoke

| URL | Expected | Result | Notes |
|---|---|---|---|
| `/overview?environment=prod` | Attention queue loads | NOT RUN | CDP not available |
| `/databases?environment=prod` | Database list controls usable | NOT RUN | CDP not available |
| `/resources/14` | Abnormal cluster detail loads | NOT RUN | CDP not available |
| `/resources/22` | Healthy instance detail loads | NOT RUN | CDP not available |
| `/resources?page=1&pageSize=1` | Resource pagination loads | NOT RUN | CDP not available |
| `/audits?page=1&pageSize=1` | Audit pagination loads | NOT RUN | CDP not available |

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

Reason: All required gates passed. Backend local gates PASS (10 packages, 0 failures). Backend Docker gates PASS (34 integration tests, 960 Schemathesis cases). Frontend local gates PASS. Frontend browser gates PASS (smoke 7, interaction 3, full E2E 50). CDP live smoke is the only gate not run, and it is explicitly optional — it requires a manually-started Chrome remote debugging session and is not part of the automated release:check pipeline.
