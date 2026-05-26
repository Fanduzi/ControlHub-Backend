# Phase 34C Schemathesis Validation Mismatch Policy Design

## Background

Phase 33E restored backend heavy CI by pinning Schemathesis to `4.15.2`.
Phase 34B then verified that Schemathesis v4.19 hooks load and mutate FK-like
fields during fuzzing, but hooks do not solve the v4.19 exit-code problem.

Observed v4.19 behavior:

```text
baseline without hooks:
  25 passed, 2 failed operations, 883/883 cases passed, exit 1

with FK hooks:
  25 passed, 2 failed operations, 891/891 cases passed, exit 1

failed operations:
  POST /resources
  POST /resources/{id}/relations
```

Key finding:

```text
validation_mismatch is an operation-level aggregate classification. It can fail
an operation even when every generated case passes configured checks.
```

The remaining problem is no longer "can hooks set FK values?". They can. The
remaining problem is deciding how ControlHub should treat Schemathesis v4.19
`validation_mismatch` for endpoints with business validation and runtime
referential integrity.

## Goal

Define and validate a durable backend policy for Schemathesis v4.19+
`validation_mismatch` handling without weakening the release gate.

The phase should answer one question:

```text
Can ControlHub safely move backend heavy CI from Schemathesis 4.15.2 to v4.19+
without hiding real contract failures?
```

If the answer is no, the phase must document why the pin remains and what would
change that decision.

## Non-Goals

- Do not remove the `schemathesis==4.15.2` CI pin during investigation.
- Do not add OpenAPI enum values for seed database IDs.
- Do not suppress all warnings.
- Do not skip operations.
- Do not reduce checks or examples.
- Do not swallow Schemathesis exit codes in `scripts/openapi-fuzz.sh`.
- Do not change product validation behavior to satisfy fuzz input.
- Do not change SQL or migrations.
- Do not tag, release, deploy, or push without explicit approval.

## Policy Options To Evaluate

### Option A: Keep The 4.15.2 Pin

Keep current CI behavior.

Accept this if:

```text
v4.19+ cannot separate hard API failures from expected business-validation
rejections without brittle workarounds.
```

Pros:

- stable remote heavy CI
- known to catch real 5xx/product defects
- no contract distortion

Cons:

- pinned tool version must be revisited later
- future Schemathesis improvements are not adopted automatically

### Option B: Custom Python Runner

Use Schemathesis Python APIs to run the same checks while classifying outcomes
explicitly:

```text
hard failure:
  5xx
  undocumented status for valid examples
  content-type mismatch
  response schema mismatch

accepted validation mismatch:
  generated invalid business input returns documented 400/404/409 with expected
  error envelope
```

This option is acceptable only if the runner remains small, deterministic, and
fails loudly on real check failures.

### Option C: Contract / Test Data Reshaping

Adjust OpenAPI examples, Schemathesis config, seed data, or request schemas so
v4.19 exits 0 natively.

Allowed:

- examples that reflect stable integration seed data
- schema tightening that matches existing backend validation
- documenting expected 400/404/409 error envelopes where currently under-modeled

Rejected:

- OpenAPI FK enums for seed row IDs
- relaxing backend validation
- fake success responses for invalid business input

## Acceptance Criteria

Phase 34C is successful if it produces one of these outcomes:

### Outcome A: Pin Remains By Policy

Evidence required:

```text
v4.19 behavior reproduced
all generated cases pass checks
operation-level validation_mismatch still exits 1
available config/hooks do not make the classification safe
4.15.2 remains intentional, not accidental
```

### Outcome B: Replacement Path Approved

Evidence required:

```text
v4.19+ local run exits 0
27/27 operations exercised
checks/examples not reduced
real 5xx or schema failures still fail the gate
manual backend heavy CI passes after the change
```

### Outcome C: Follow-Up Product Contract Fix Identified

Evidence required:

```text
specific endpoint/schema/backend behavior is shown to be inconsistent
fix requires product contract work outside this phase
CI pin remains until that fix lands
```

## Required Evidence

The final note must record:

```text
Schemathesis version tested
exact command lines
exit codes
operation pass/fail counts
case counts
failed operation names
whether JUnit/check failures exist
whether failures are validation_mismatch only
chosen policy option
why rejected options were rejected
CI pin status
```

## Scope Control

Phase 34C should start as docs/investigation. Do not change CI until the chosen
policy is proven locally and reviewed.
