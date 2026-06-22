# Decision: Phase 37F Query Execute UI Boundary

## Status

Accepted.

## Context

Phase 37 backend execution is complete, but the visible product remains locked:
the frontend does not call the execute API, and local seed data has no ready
query target. The next useful milestone is an end-to-end dev-safe execution
slice, not multi-engine expansion.

## Decision

Phase 37F will implement only:

- local/dev credential metadata seeding for one MySQL/TiDB target
- frontend execute/history wiring for backend Phase 37 APIs
- real cross-repo E2E proving one guarded SELECT can run

Phase 37F will not add production credential management, credential write APIs,
exports, approvals, saved queries, Redis, MongoDB, PostgreSQL, or ClickHouse
execution.

## Rationale

The query platform must prove the narrowest useful loop before adding engine
surface area:

```text
target readiness -> authenticated Run -> guarded backend execution -> results -> history
```

Adding more engines before the frontend can run one query would increase
complexity without proving user value. Adding a credential management UI before
one local/dev credential path works would also create security surface area too
early.

## Consequences

- Local viewing becomes meaningful: at least one target can become ready.
- Production stays fail-closed by default.
- Frontend workers have a concrete API to wire, not a speculative shell.
- Later phases can decide how to manage credentials and approvals.

## Guardrails

- DSNs remain environment variables only.
- `query_target_credentials` stores metadata only.
- Ready state must come from backend `availableActions.run=true`.
- Frontend must never send actor id or credential material.
- Backend remains the only enforcement layer.

