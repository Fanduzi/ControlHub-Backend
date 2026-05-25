# Phase 34 Schemathesis FK-Aware Generation Design

## Background

Phase 33E restored backend heavy CI by pinning Schemathesis to `4.15.2`.
That was a pragmatic release-readiness decision, not the desired long-term
state.

The blocked upgrade path is Schemathesis v4.19+ behavior:

```text
v4.15.2:
  runtime referential-integrity validation_mismatch remains non-blocking
  backend heavy CI exits 0

v4.19.0:
  validation_mismatch marks operations failed
  backend heavy CI exits 1
```

The remaining mismatch operations are tied to FK-like fields:

```text
POST /resources
  environmentId
  ownerId

POST /resources/{id}/relations
  path.id
  body.toResourceId
```

These values exist in the disposable Testcontainers seed database, but generic
OpenAPI fuzzing mutates them to random integers that the backend correctly
rejects.

## Goal

Investigate and implement FK-aware Schemathesis data generation so backend
heavy CI can eventually run on newer Schemathesis versions without:

- hardcoding seed FK IDs as public OpenAPI enums
- suppressing warnings
- swallowing exit codes
- skipping operations
- reducing checks or examples

## Non-Goals

- Do not remove the `schemathesis==4.15.2` CI pin until v4.19+ has a proven
  green replacement.
- Do not encode `environmentId`, `ownerId`, or `toResourceId` as OpenAPI enum
  values.
- Do not disable `validation_mismatch` display.
- Do not set `warnings = false`.
- Do not add `continue-on-error`.
- Do not filter Schemathesis exit codes in `openapi-fuzz.sh`.
- Do not change product behavior.
- Do not change SQL or seed migrations unless a separate product decision is
  made.
- Do not tag, release, or deploy.

## Current Working Theory

The existing TOML overrides help examples/path values but do not constrain
integer body-field mutations in the v4.19 fuzzing phase:

```toml
[[operations]]
include-operation-id = "createResourceRelation"
parameters = { "path.id" = 1, "body.toResourceId" = 2 }
```

Observed limitation:

```text
v4.19 fuzzing still mutates FK-like integer fields to values outside the seeded
database, causing runtime validation_mismatch.
```

Phase 34 should verify whether Schemathesis exposes a supported way to constrain
case generation or mutate cases before execution.

## Acceptable Approaches

### Approach A: Supported Schemathesis Hooks / API

If Schemathesis v4.19+ exposes supported Python hooks or APIs for case mutation,
use them to constrain FK-like fields:

```text
POST /resources:
  environmentId -> one of seeded environment IDs
  ownerId -> one of seeded owner IDs

POST /resources/{id}/relations:
  path.id -> seeded resource ID
  body.toResourceId -> different seeded resource ID
```

Constraints:

- Hook must be local to the fuzz harness.
- Hook must not alter response checks.
- Hook must not skip the operation.
- Hook must not hide warnings.

### Approach B: Custom Python Runner

If CLI hooks are unavailable but a supported Python API exists, create a small
runner that:

- starts from the same OpenAPI spec URL
- applies the same checks
- applies the same seed and max examples where possible
- injects valid FK values for selected operations
- emits clear pass/fail output
- exits non-zero on real check failures

This is acceptable only if it remains small and maintainable.

### Approach C: Keep Pin And Document Deferral

If v4.19+ cannot be made FK-aware without fragile or unsupported internals,
keep the v4.15.2 pin and record:

```text
Phase 34 outcome: newer Schemathesis blocked by lack of supported FK-aware
integer generation for runtime referential-integrity fields.
```

## Rejected Approaches

### OpenAPI FK Enums

Do not add:

```yaml
environmentId:
  enum: [1, 2, 3]
ownerId:
  enum: [1, 2, 3, 4, 5]
```

Reason:

```text
Those are seeded database rows, not public API enum values.
```

### Wrapper Exit-Code Filtering

Do not make `openapi-fuzz.sh` reinterpret Schemathesis exit codes.

Reason:

```text
This risks hiding future real contract failures.
```

### Warning Suppression

Do not hide `validation_mismatch`.

Reason:

```text
It remains useful audit signal even when accepted.
```

## Verification Strategy

Phase 34 must prove both local and CI behavior.

Local:

```text
Schemathesis v4.19+ run exits 0
27/27 operations exercised
checks unchanged
examples unchanged unless explicitly justified
no skipped operations
```

Remote:

```text
GitHub Actions manual heavy CI passes without pinning v4.15.2
```

If the pin remains, remote verification should state that the pin remains by
decision, not accident.

## Documentation Requirements

Update:

```text
docs/releases/candidates/2026-05-24-controlhub-rc-local.md
docs/release-hardening-checklist.md
```

Record:

- whether v4.19+ FK-aware generation was adopted or deferred
- exact Schemathesis version tested
- whether CI pin was removed or retained
- remaining warnings and classification
- why OpenAPI FK enums were not used

## Success Criteria

Phase 34 is successful if either:

1. **Upgrade path succeeds**
   - v4.19+ runs locally with FK-aware generation and exit 0
   - backend heavy CI uses newer Schemathesis and passes
   - no OpenAPI enum pollution
   - no warning suppression / operation skipping / exit-code filtering

2. **Deferral is proven**
   - supported v4.19+ FK-aware generation path is investigated
   - limitations are documented with evidence
   - v4.15.2 pin remains explicitly justified
   - release gate remains green

