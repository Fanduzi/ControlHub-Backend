# ControlHub Release Candidate Evidence

## Candidate

| Field | Value |
|---|---|
| Candidate ID | YYYY-MM-DD-controlhub-rc-local |
| Date | YYYY-MM-DD |
| Backend commit | BACKEND_COMMIT_SHA |
| Frontend commit | FRONTEND_COMMIT_SHA |
| Backend worktree | CLEAN / DIRTY (describe) |
| Frontend worktree | CLEAN / DIRTY (describe) |
| Evaluator | EVALUATOR_NAME |
| Decision | GO / NO-GO |
| Decision reason | REQUIRED_REASON |

## Gate Policy

### Required Gates (must PASS for GO)

- Backend: `make release-readiness-gates`
- Frontend: `npm run release:check`

### Optional Gates (failure blocks GO only if run and failed)

- Frontend CDP smoke: `npm run release:smoke:cdp`

## Backend Gates

| Gate | Command | Required | Result | Notes |
|---|---|---|---|---|
| Backend local gates | `make release-local-gates` | Yes | PASS / FAIL | No Docker required |
| Backend Docker gates | `make release-docker-gates` | Yes (when Docker available) | PASS / FAIL / NOT RUN | Requires Docker |

## Frontend Gates

| Gate | Command | Required | Result | Notes |
|---|---|---|---|---|
| Frontend local gates | `npm run release:local` | Yes | PASS / FAIL | No backend required |
| Frontend browser gates | `npm run release:e2e` | Yes | PASS / FAIL | Requires backend |
| Frontend live smoke | `npm run release:smoke:cdp` | No | PASS / FAIL / NOT RUN | Requires Chrome remote debugging |

## Live Browser Smoke

| URL | Expected | Result | Notes |
|---|---|---|---|
| `/overview?environment=prod` | Attention queue loads | PASS / FAIL / NOT RUN | |
| `/databases?environment=prod` | Database list controls usable | PASS / FAIL / NOT RUN | |
| `/resources/14` | Abnormal cluster detail loads | PASS / FAIL / NOT RUN | |
| `/resources/22` | Healthy instance detail loads | PASS / FAIL / NOT RUN | |
| `/resources?page=1&pageSize=1` | Resource pagination loads | PASS / FAIL / NOT RUN | |
| `/audits?page=1&pageSize=1` | Audit pagination loads | PASS / FAIL / NOT RUN | |

## Warning Classification

| Warning | Classification | Justification |
|---|---|---|
| | Accepted / Follow-Up / Blocking | |

Accepted: non-blocking, understood, no user-facing risk.
Follow-Up: non-blocking, should become future hardening work.
Blocking: indicates response schema mismatch, conformance failure, or unclassified failure.

## Skipped Optional Gates

| Gate | Status | Reason |
|---|---|---|
| Frontend CDP smoke | NOT RUN | State reason here if not run |

## Known Gaps

- List accepted gaps here.

## Failure Classification

| Failure | Classification | Evidence | Owner / Next Action |
|---|---|---|---|
| | | | |

## Go / No-Go Decision

Decision: GO / NO-GO

Reason: REQUIRED — state why this candidate is ready or blocked. GO only if backend required gates PASS and frontend required gates PASS. NO-GO if either required gate fails or any failure is unclassified.
