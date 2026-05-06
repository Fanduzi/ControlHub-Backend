# Phase 28 Coordination Prompt — Quality Baseline And Release Hardening

Phase 28 is split into two worker prompts. Do not give this coordination prompt
to a worker as the implementation prompt.

Use these instead:

```text
Backend/docs worker:
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-28-quality-baseline-release-hardening-worker.md

Frontend worker:
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-28-quality-baseline-release-hardening-worker.md
```

## Execution Order

Recommended order:

1. Run the backend/docs worker first.
2. Run the frontend worker second.
3. Merge backend/docs output only after reviewing the quality baseline and
   release checklist.
4. Merge frontend output only after E2E preflight/gates pass.

## Shared Phase Goal

Phase 28 is not a product feature phase. It establishes the release-quality
baseline after Phase 27B:

```text
what is covered by tests
what remains manual
what risks are not covered
which checks block future merges
which small automation is worth adding now
```

## Shared Constraints

Both workers must preserve:

```text
No product UI changes
No backend API contract changes
No SQL
No write operations
No topology layout changes
No broad output suppression
No skipped/deleted tests
No tag/push/release
No AI co-author
```

## Handoff Between Workers

The backend/docs worker owns the canonical quality baseline documents:

```text
docs/quality-baseline.md
docs/release-hardening-checklist.md
docs/superpowers/notes/2026-05-06-phase-28-quality-research.md
```

The frontend worker may provide frontend-specific findings and, if justified,
small automation such as:

```text
scripts/check-e2e-preflight.mjs
tests/scripts/check-e2e-preflight.test.ts
package.json
```

If frontend automation changes the recommended commands, the backend/docs
quality baseline should be updated in a follow-up docs commit.
