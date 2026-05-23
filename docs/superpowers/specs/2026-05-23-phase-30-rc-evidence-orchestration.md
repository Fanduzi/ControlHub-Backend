# Phase 30 RC Evidence Orchestration Design

## Background

Phase 29 added release-readiness gates on both sides of ControlHub:

- backend: `make release-local-gates`, `make release-docker-gates`, `make release-readiness-gates`
- frontend: `npm run release:local`, `npm run release:e2e`, `npm run release:check`, optional `npm run release:smoke:cdp`
- shared documentation: quality baseline, release checklist, release candidate evidence template

Those gates now pass on current `main`:

```text
frontend main:
  npm run release:check
  result: PASS
  full E2E: 50/50 PASS

backend main:
  make release-readiness-gates
  result: PASS
  OpenAPI fuzz: 960/960 PASS with 2 non-blocking warnings
```

The remaining gap is not test coverage. The gap is orchestration: there is no
small, repeatable process that turns the two independent gate runs into one
release-candidate evidence bundle with a clear `GO / NO-GO` decision.

Phase 30 closes that gap.

## Goal

Create a durable release-candidate evidence workflow that records:

```text
backend commit + backend gate results
frontend commit + frontend gate results
known warnings + failure classification
optional live-smoke status
GO / NO-GO decision
```

The output should be useful to a human reviewer without requiring them to parse
terminal logs.

## Non-Goals

- Do not add product UI.
- Do not change backend API contracts.
- Do not change SQL or migrations.
- Do not add write operations.
- Do not alter topology behavior.
- Do not publish, tag, deploy, or push.
- Do not add broad retries or output suppression.
- Do not weaken existing gates.
- Do not make optional CDP smoke a hard blocker unless a Chrome CDP session is
  reliably available.
- Do not build a full CI system in this phase.

## Release Candidate Evidence Model

A ControlHub release candidate is a commit pair:

```text
backend_commit=<GolangProjects/ControlHub SHA>
frontend_commit=<JsProjects/ControlHub SHA>
```

An evidence bundle is valid only if it records:

```text
candidate_id
date
backend_commit
frontend_commit
commands_run
gate_results
warning_classification
skipped_or_optional_gates
dirty_worktree_status
decision
decision_reason
```

## Gate Policy

### Required Gates

Backend:

```text
make release-readiness-gates
```

Frontend:

```text
npm run release:check
```

Both must pass for a `GO` decision.

### Optional Gates

Frontend CDP smoke:

```text
npm run release:smoke:cdp
```

This remains optional because it requires a manually started Chrome remote
debugging session on port `9222`.

If CDP smoke is not run, the evidence must state:

```text
NOT RUN - no Chrome remote debugging target available
```

If CDP smoke is run and fails, the candidate cannot be marked `GO` until the
failure is classified.

## Warning Classification

Warnings are not automatically blockers. They must be classified.

### Accepted Warning

Allowed only when:

- required gates pass
- behavior is understood
- no user-facing or contract risk is identified
- the warning is recorded in evidence

Current accepted backend fuzz warnings:

```text
PATCH /resources/{id} repeatedly returned 404 due to missing valid generated ID
POST /auth/login, POST /resources, PATCH /resources/{id} validation mismatch warnings
```

These are accepted because Schemathesis reports all selected operations tested
and all 960 generated cases passed the configured checks.

### Follow-Up Warning

Use when a warning is non-blocking but should become future hardening work.

Examples:

```text
OpenAPI schema constraints are broader than API validation
test data generation lacks known resource IDs
CDP live smoke unavailable in local environment
```

### Blocking Warning

A warning becomes blocking when it indicates:

- response schema mismatch
- status code conformance failure
- content-type conformance failure
- server error
- unclassified E2E failure
- missing required gate result
- dirty worktree after the gate run

## Evidence Format

Phase 30 should use the existing template:

```text
docs/releases/candidates/TEMPLATE.md
```

It may extend the template if needed, but must not remove the existing sections:

```text
Candidate
Backend Gates
Frontend Gates
Live Browser Smoke
Known Gaps
Failure Classification
Go / No-Go Decision
```

## Candidate ID

Use date-based local candidate IDs:

```text
YYYY-MM-DD-controlhub-rc-local
```

If multiple candidates are generated on the same date, append a suffix:

```text
YYYY-MM-DD-controlhub-rc-local-2
```

## Success Criteria

Phase 30 is complete when:

- a Phase 30 implementation plan exists
- the RC evidence format is clarified
- current frontend and backend `main` gate results are represented in a real
  candidate evidence file
- OpenAPI fuzz warnings are classified explicitly
- optional CDP smoke handling is explicit
- the final decision is justified as `GO` or `NO-GO`
- no product code changes are required

## Recommended Implementation Shape

Minimum viable implementation:

```text
docs-only:
  update template if needed
  add one real candidate evidence file using current gate results
  update release checklist with exact orchestration steps
```

Optional if justified:

```text
small validation script:
  checks candidate evidence has no placeholder markers
  checks required sections exist
  does not run gates
```

Do not add a large automation framework in Phase 30. The immediate value is
reviewable evidence and consistent classification.

