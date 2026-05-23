# ControlHub Release Candidate Evidence

## Candidate

| Field | Value |
|---|---|
| Candidate ID | YYYY-MM-DD-controlhub-rc-local |
| Date | YYYY-MM-DD |
| Backend commit | BACKEND_COMMIT_SHA |
| Frontend commit | FRONTEND_COMMIT_SHA |
| Evaluator | EVALUATOR_NAME |
| Decision | GO / NO-GO |

## Backend Gates

| Gate | Command | Result | Notes |
|---|---|---|---|
| Backend local gates | `make release-local-gates` | PASS / FAIL | No Docker required |
| Backend Docker gates | `make release-docker-gates` | PASS / FAIL / NOT RUN | Requires Docker |

## Frontend Gates

| Gate | Command | Result | Notes |
|---|---|---|---|
| Frontend local gates | `npm run release:local` | PASS / FAIL | No backend required |
| Frontend browser gates | `npm run release:e2e` | PASS / FAIL | Requires backend |
| Frontend live smoke | `npm run release:smoke:cdp` | PASS / FAIL / NOT RUN | Requires Chrome remote debugging |

## Live Browser Smoke

| URL | Expected | Result | Notes |
|---|---|---|---|
| `/overview?environment=prod` | Attention queue loads | PASS / FAIL | |
| `/databases?environment=prod` | Database list controls usable | PASS / FAIL | |
| `/resources/14` | Abnormal cluster detail loads | PASS / FAIL | |
| `/resources/22` | Healthy instance detail loads | PASS / FAIL | |
| `/resources?page=1&pageSize=1` | Resource pagination loads | PASS / FAIL | |
| `/audits?page=1&pageSize=1` | Audit pagination loads | PASS / FAIL | |

## Known Gaps

- List accepted gaps here.

## Failure Classification

| Failure | Classification | Evidence | Owner / Next Action |
|---|---|---|---|
| | | | |

## Go / No-Go Decision

Decision:

Reason:
