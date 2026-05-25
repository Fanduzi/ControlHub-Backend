# Phase 33C — Schemathesis v4.19.0 Investigation Report

## Environment

| Field | Value |
|---|---|
| Worktree | `.worktrees/backend-phase-33c-schemathesis-v419-investigation` |
| Branch | `phase-33c-schemathesis-v419-investigation` |
| HEAD | `d0f8160` (same as main) |
| Schemathesis (global) | v4.15.2 |
| Schemathesis (investigation venv) | v4.19.0 (`.venv-schemathesis-419/`, now removed) |
| Investigation date | 2026-05-25 |

## Reproduction

| Field | Value |
|---|---|
| Reproduced? | **Yes** — reproduced locally with v4.19.0 |
| Exact command | `make test-openapi-fuzz` (using v4.19.0 via activated venv) |
| Underlying Schemathesis invocation | `schemathesis --config-file scripts/schemathesis.toml run http://localhost:{port}/openapi.yaml --max-examples 50 --seed 42 --checks not_a_server_error,status_code_conformance,content_type_conformance,response_schema_conformance --mode all --phases examples,fuzzing` |

### Run 1

| Field | Value |
|---|---|
| Exit code | **2** (Schemathesis: some failures) |
| Operations | 27 |
| Generated cases | 932 |
| Examples phase | 4 passed, 23 skipped |
| Failed operations | `❌ 1 failed` — reported as generic failure, JUnit showed 0 failures (misleading) |

### Run 2

| Field | Value |
|---|---|
| Exit code | **2** (Schemathesis: some failures) |
| Operations | 27 |
| Generated cases | ~950 |
| Failed operations | `❌ 2 failed` |

### Run 3

Docker/Testcontainers timeout — ryuk container failed to become ready within 63 seconds. Transient infrastructure issue, not related to code.

## Failure Detail

**Failed operation:** `PATCH /resources/{id}` (operation ID: `patchResource`)

**Check:** `not_a_server_error`

**Response:**
```json
{"error":"internal_error","message":"unexpected server failure"}
```

**Generated input:**
```
curl -X PATCH -H 'Content-Type: application/json' -d '{"name": "order-reporting-cluster-prod", "source": "manual", "labels": {"úë÷..."}}
```

**Root cause:** Schemathesis v4.19.0 detects a new capability:

```
Accepts backslash and control characters in URL paths: ✓
```

This causes v4.19.0 to generate JSON body data containing unicode control characters (binary-like data such as `úë÷...`). When the `labels` field in a PATCH request contains these characters, the backend returns HTTP 500 instead of rejecting the input with 400.

v4.15.2 does not have this capability detection and never generates such inputs, so this bug was invisible in local testing.

## JUnit / Report Evidence Summary

- JUnit reports saved to `.schemathesis-reports/junit-*.xml` (now removed after cleanup)
- Run 1 JUnit: 0 failures recorded — Schemathesis reporting inconsistency (summary says 1 failed, JUnit says 0)
- Run 2 JUnit: confirmed `not_a_server_error` failure on `PATCH /resources/{id}` with explicit curl reproduction

## Classification

**C — Real contract bug**

The backend returns 500 when `PATCH /resources/{id}` receives `labels` containing unicode control characters. This is a defensive coding gap — input validation should reject malformed label values before they reach the database layer.

This is NOT:
- A missing valid data issue (Schemathesis is generating adversarial fuzz data, not normal valid data)
- A schema mismatch (the OpenAPI spec allows `additionalProperties: string`, which technically accepts any string)
- A v4.19 config issue (the check — `not_a_server_error` — is legitimate; no config can or should suppress it)

## Recommended Next Action

Fix the backend to validate `labels` values in `PATCH /resources/{id}` (and likely `POST /resources`) — reject label values containing control characters with a 400 response and clear error message. The fix should be in `internal/api/resource_handler.go` or `internal/service/resource_service.go`, with corresponding unit tests.

## Scope Confirmation

| Constraint | Status |
|---|---|
| No product code changes | ✅ Confirmed — no `.go` files modified |
| No OpenAPI changes | ✅ Confirmed — `openapi.yaml` untouched |
| No fuzz harness changes | ✅ Confirmed — `openapi-fuzz.sh` untouched |
| No warning suppression | ✅ Confirmed — `[warnings]` config from Phase 33B only controls warning-level exit, not check failures |
| No skipped/deleted operations | ✅ Confirmed — all 27 operations exercised |
| No reduced checks/examples | ✅ Confirmed — all 4 checks, 50 examples, same seed |
| No tag/release/deploy | ✅ Confirmed |
| No push | ✅ Confirmed — no commits made, no push executed |
| No AI co-author | ✅ Confirmed — no commits made at all |

## Final Git Status

```
On branch phase-33c-schemathesis-v419-investigation
nothing to commit, working tree clean
HEAD: d0f8160 (same as main)
```

No code committed. Investigation artifacts (`.venv-schemathesis-419/`, `.schemathesis-reports/`) cleaned up.
