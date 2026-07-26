# Phase 38Q Oracle Backend Adversarial Diff Review

## Metadata
- Reviewer: Oracle
- Scope: `f0c6d81...9de01f6` (backend)
- Timestamp: 2026-07-26T03:19:02Z
- Result: **P1=1, P2=1, P3=0 — Merge blocked**

## Findings

### P1: Literal exemption accepts a FROM clause
`isNoTableProjection` (internal/service/query_disclosure_projection.go:146) explicitly treats `FROM dual` as no-table, then `resolveLiteralOnlyProjection` (internal/service/query_disclosure_projection.go:173) marks `SELECT 1 FROM dual` as `raw_copy_allowed`. The required exemption is strictly no-FROM, so this violates the defined boundary even though `dual` has no user data.

**Fix required**: Remove the `dual` special case and add a regression test rejecting `SELECT 1 FROM dual` (including aliases).

### P2: Invalid policy mode leaks raw cells rather than failing closed
`buildDisclosurePlan` (internal/service/query_disclosure_service.go:231) copies `policy.Mode` without validating it. `applyDisclosureMask` (internal/service/query_disclosure_mask.go:9) masks only `masked_no_copy`; a malformed mode, including `blocked`, therefore yields `copyAllowed=false` but leaves the raw value in `rows`.

**Fix required**: Validate every repository-read mode and return `ErrQueryDisclosureBlocked` unless it is exactly `raw_copy_allowed` or `masked_no_copy`; add a test proving a returned `blocked`/unknown mode never reaches `Apply` or the response.

## Positive Findings (no P1/P2)

- **Exact-policy fail closed**: passes for absent rows. `GetByScope` matches all four scope fields; `buildDisclosurePlan` blocks `sql.ErrNoRows`.
- **Server-owned mask/copy/FK boundary**: generally passes. Execute and FK navigation preflight before execution, then call `Apply` before constructing responses.
- **Test stability**: no async UI-test or wait-only changes exist in this backend range. Changed tests seed exact fixture policies and replace bare `SELECT 1` with governed table queries while retaining their business assertions; no weakening found.
- Verification passed: `go test -count=1 ./internal/service ./internal/model ./internal/api` (1083 tests).
