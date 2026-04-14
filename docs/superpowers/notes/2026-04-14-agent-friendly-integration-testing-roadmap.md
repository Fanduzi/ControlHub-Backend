# Agent-Friendly Integration Testing Roadmap

Date: 2026-04-14

## Context

ControlHub now has:

- Go backend with chi router
- MySQL persistence
- goose-managed SQL migrations
- YAML-first OpenAPI contract
- Scalar OpenAPI docs
- Next.js frontend
- Playwright E2E baseline

As the platform moves from asset CRUD into topology and later higher-level database workflows, AI workers need tests that catch real integration failures without relying on manual browser inspection or fake repositories only.

## Adoption Principle

Prefer tools that are:

- CLI- or library-driven
- deterministic enough for AI agents
- based on the existing OpenAPI contract or real MySQL behavior
- runnable in isolated test databases
- easy to summarize in worker final reports

Do not introduce a large test-tool stack all at once.

## Step 1: Backend Integration Harness

Introduce first when the next backend quality phase is planned.

Recommended phase:

```text
Backend Phase 11.5: Integration And Contract Test Harness
```

Scope:

- Add Testcontainers Go with MySQL 8.0.
- Add `make test-integration`.
- Use goose to migrate a disposable MySQL database from zero to latest.
- Exercise repository and critical API paths against real MySQL.
- Cover behavior fake repositories cannot catch.

High-value cases:

- goose clean DB migration
- resource create/update/list/delete-related reads
- duplicate key `1062` mapping to `409`
- relation create/delete and conflict behavior
- topology SQL relation neighborhood queries
- MySQL index/constraint behavior

Rationale:

ControlHub already hit real MySQL-only bugs:

- `LastInsertId()` returned `0` for `char(36)` UUID primary keys.
- Migration state could not be known before goose.
- Unique index behavior differed from service-layer assumptions.

Testcontainers should be the first integration tool because it validates the persistence layer that fake repositories cannot validate.

## Step 2: OpenAPI Fuzz And Contract Testing

Introduce after OpenAPI docs/validation and integration DB harness are stable.

Recommended phase:

```text
Backend Phase 11.6: Schemathesis OpenAPI Contract Fuzzing
```

Scope:

- Add a `make schemathesis` or `make contract-fuzz` target.
- Start backend against a disposable migrated database.
- Run Schemathesis against `http://localhost:<test-port>/openapi.yaml`.
- Fail on 5xx responses and schema violations.
- Store minimal reproducible examples when useful.

Rules:

- Never run mutating Schemathesis tests against the user's daily `controlhub` database.
- Use a disposable database.
- Treat initial findings as contract hardening input, not product feature requests.

Rationale:

Schemathesis is high ROI because it consumes the existing OpenAPI contract and can find:

- unhandled parameter boundaries
- schema/handler response mismatches
- accidental 500s
- inconsistent error shapes

## Step 3: Frontend Playwright API/E2E Hardening

Introduce alongside or immediately after topology UI work.

Recommended phase:

```text
Frontend Phase 13.5: Playwright API Setup Hardening
```

Scope:

- Use Playwright API requests to create and clean test resources/relations.
- Keep UI tests focused on user-visible behavior.
- Avoid depending on exact seed row counts.
- Validate critical frontend-to-backend request parameters where useful.
- Keep E2E data names deterministic and easy to clean.

Rules:

- Prefer API setup for test data over long UI setup flows.
- UI should verify user behavior, not act as the only data factory.
- Tests must not leave uncontrolled test data in the daily local database.

Rationale:

Playwright is already in the frontend. The next improvement is not another tool, but better test structure:

- API creates controlled preconditions.
- UI validates the actual workflow.
- Cleanup keeps repeated AI-agent runs safe.

## Tools To Defer

Do not introduce these until the core APIs are more stable:

- Pact: useful later for mature consumer-driven contracts, too heavy while contracts are still moving.
- oapi-codegen runtime middleware: valuable later, but changes backend development workflow.
- k6: useful after topology/list performance targets exist.
- Prism: useful for frontend-first work when backend is unavailable, but less valuable now that backend is moving quickly.
- WireMock MCP: interesting for AI-managed stubs, but lower ROI than Testcontainers and Schemathesis right now.

## Planning Reminder

When creating future phase prompts, consider whether one of these should be introduced:

1. After backend topology read model: add Testcontainers integration tests.
2. After integration harness is stable: add Schemathesis contract fuzzing.
3. After frontend topology view: harden Playwright API setup/cleanup.

Do not let new product phases pile on top of fake-only backend tests once topology and write APIs become central.
