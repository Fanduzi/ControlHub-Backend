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
| Frontend manual E2E CI | workflow_dispatch | NOT RUN | Deferred: cross-repo backend bootstrap not encoded in CI |

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
| `npm run release:e2e` | PASS (smoke 7/7, interaction 3/3, full 50/50) | NOT RUN (deferred) |
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

## Known Accepted Warnings / Boundaries

1. **Runtime referential integrity validation mismatch** — Schemathesis fuzzes FK-like integer fields (`environmentId`, `ownerId`, `toResourceId`) to values that do not exist in seed data. Backend correctly rejects those values. This is not a 5xx, status code, content type, or schema contract bug. Remaining mismatch is accepted.

2. **Backend heavy CI pins Schemathesis `4.15.2`** — v4.19.0 treats DB-backed `validation_mismatch` as operation-level failure (exit 1). Pinning avoids CI failure from runtime referential integrity, not from product defects. Warnings remain visible. Configured checks still cause CI failure.

3. **Phase 34 deferred** — Investigate FK-aware Schemathesis data generation for v4.19+ to allow unpinning without CI failure from referential integrity validation.

4. **CDP live smoke not run** — Optional gate. Requires manually-started Chrome remote debugging session. Not included in `npm run release:check`. All automated E2E gates passed via Playwright.

5. **Frontend remote E2E deferred** — Cross-repo backend bootstrap is not encoded in CI. Local E2E passed (smoke 7/7, interaction 3/3, full 50/50).

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
| This summary | `docs/releases/candidates/2026-05-26-controlhub-release-readiness-summary.md` |

## Final Decision

**GO for RC baseline.**

All required gates passed locally and on GitHub Actions. All product defects discovered during heavy CI have been fixed. Backend heavy CI passes with Schemathesis pinned to `4.15.2`. Frontend fast CI passes. No unclassified failures. No tag, release, or deploy.

If formal release is desired, next step is explicit tag/release planning and authorization.
