# Phase 29 Coordination Prompt — Release Readiness Mechanism

Phase 29 is split into backend and frontend worker prompts. Do not give this
coordination prompt to a worker as the implementation prompt.

Use:

```text
Backend worker:
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/backend-phase-29-release-readiness-worker.md

Frontend worker:
/Users/fan/GolangProjects/ControlHub/docs/superpowers/prompts/frontend-phase-29-release-readiness-worker.md
```

## Recommended Order

1. Start backend worker and frontend worker in parallel.
2. Backend worker should stop after backend gates/template if frontend results
   are not available.
3. Frontend worker reports its final commit and gate results.
4. Backend worker creates the dry-run evidence file using final frontend
   results.
5. Merge backend Phase 29 and frontend Phase 29 separately after review.

## Dependency

The dry-run evidence file depends on both streams:

```text
backend final commit
frontend final commit
backend gate results
frontend gate results
CDP live smoke result or not-run reason
go/no-go decision
```

## Shared Constraints

```text
No product UI changes
No backend API contract changes
No SQL
No write operations
No topology layout changes
No publish/deploy/tag/push
No broad output suppression
No skipped/deleted tests
No AI co-author
```
