# Phase 28 Quality Baseline And Release Hardening Design

## Background

Phases 16 through 27 added the database operator workflow across backend and
frontend:

- backend resource read models
- database operational rollups
- frontend database list operational signals
- database detail decision panels
- topology, member, audit, and overview alignment
- E2E governance and query-param cleanup

The product surface is now broad enough that continuing to add UI features
without a quality baseline is counterproductive. Phase 28 is a stop-the-line
engineering phase: no new product capability, no IA churn, no more add-then-
remove loops.

## Goal

Establish a practical release-quality baseline for ControlHub.

Phase 28 must answer:

1. What is currently protected by automated tests?
2. What is only protected by manual browser checks?
3. Which risks still lack a reliable gate?
4. Which high-value, low-scope tests or scripts should be added now?
5. What command set should block future merges/releases?

## Non-Goals

- Do not add new product UI.
- Do not redesign database pages.
- Do not change database operational-signal semantics.
- Do not change backend API contracts.
- Do not add write operations, work orders, or SQL execution.
- Do not introduce a large CI platform redesign.
- Do not add heavyweight visual regression infrastructure unless the research
  produces a concrete, low-maintenance recommendation.
- Do not hide flaky tests behind retries or broad suppression.

## Current Quality Gates

### Backend

Known backend commands:

```bash
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Observed backend coverage areas:

- unit tests for API handlers, repositories, services, and models
- Testcontainers integration tests for MySQL-backed flows
- OpenAPI YAML validation
- Schemathesis fuzzing against OpenAPI operations

### Frontend

Known frontend commands:

```bash
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
npm run check:e2e-governance
npm run test:e2e:smoke
npm run test:e2e:interaction
npm run test:e2e
```

Observed frontend coverage areas:

- unit and component tests with Vitest
- E2E smoke for console reachability
- E2E interaction stability for sheets, dropdowns, back navigation, and accent
  state
- E2E database operator workflow
- E2E governance checker for auth, console/network guards, output suppression,
  and screenshot policy
- E2E recorded-request diagnostics for list query params

## Known Risk Themes

Phase 28 should audit these risk themes explicitly:

### E2E Environment Drift

Past failures came from stale dev servers and stale API proxy processes:

```text
frontend :3100 server started without E2E env vars
api proxy :8081 not recording requests
Playwright reuseExistingServer using stale processes
```

Phase 28 should decide whether to add a reusable E2E preflight check or improve
the existing Playwright setup so stale process reuse is detected before tests
start.

### Frontend/Backend Contract Drift

Risk examples:

```text
backend field exists only on clusters
frontend treats missing cluster-only field as instance unknown state
OpenAPI schema and TypeScript types diverge
frontend assumes seed IDs or response fields without a contract smoke
```

Phase 28 should audit whether the current OpenAPI, TypeScript, E2E, and live
contract checks are enough.

### Database Operator Semantic Drift

Risk examples:

```text
resource self health vs member signal
cluster signal vs instance signal
healthy resource with critical member
databaseOperationalSummary absent on instances by design
overview attention reason diverges from database list reason
```

Phase 28 should verify these are covered by tests and documented as product
semantics, not tribal knowledge.

### Flaky Test Management

Phase 28 should document:

- how failures are classified
- when retries are acceptable
- when to compare against main
- how traces/logs are collected
- which failures block merge

The answer cannot be "call it pre-existing" without evidence.

### Test Data Strategy

Recent phases relied on stable seed resources such as:

```text
analytics-ch-cluster-prod
Analytics ClickHouse Node 02
resource id 14
resource id 22
```

Phase 28 should decide whether seed IDs/names need a single documented
reference or helper constants for E2E tests.

## Required Research

This phase includes focused engineering-quality research. The research must be
specific to ControlHub's current problems and must not become a generic testing
essay.

Research questions:

1. How should Playwright projects prevent stale dev server reuse when
   environment-specific proxies are required?
2. What is a reasonable split between PR gates, merge gates, and nightly gates
   for a local-first full-stack app?
3. How should OpenAPI validation, contract smoke tests, and frontend TypeScript
   types be combined without duplicating too much work?
4. When is visual regression worth adding, and why is it either worth or not
   worth adding now?
5. How should flaky E2E failures be classified and reported so they are not
   normalized?

Use primary documentation where possible for tool behavior. Final output should
name the sources used and explain which recommendations are adopted, deferred,
or rejected.

## Product Quality Baseline

Phase 28 should produce a coverage matrix with rows like:

```text
Login and auth session
Console shell navigation
Environment context
Resource list pagination/query params
Database list search/filter/sort/signal
Database detail cluster abnormal member workflow
Database detail healthy instance workflow
Overview attention queue
Topology load and same-origin API proxy
Audit list pagination/filtering
Settings dictionaries
Backend resource CRUD/read models
Backend database operational summary
OpenAPI schema validity
OpenAPI fuzz behavior
```

Columns should include:

```text
Backend unit
Backend integration
OpenAPI validation/fuzz
Frontend unit/component
E2E smoke
E2E interaction
E2E workflow
Manual browser
Gap / next action
```

## Expected Deliverables

At minimum:

```text
quality baseline report
release hardening checklist
test coverage matrix
engineering-quality research notes
worker final report with adopted/deferred recommendations
```

Optional code changes are allowed only if they are small and directly protect a
known risk:

```text
E2E preflight script
frontend npm quality script
backend make quality target
seed-data constants for E2E
additional high-value E2E smoke assertions
```

## Acceptance Criteria

- Quality baseline document exists and covers both frontend and backend.
- Test coverage matrix identifies protected areas and gaps.
- Research notes are specific to ControlHub risks and cite the sources used.
- Release checklist tells a worker exactly which commands to run before merge.
- If code/scripts are added, they are covered by tests where practical.
- Frontend gates pass.
- Backend gates pass, or Docker-dependent gates are explicitly marked as not
  run with the reason.
- No product behavior changes unless a P0/P1 bug is discovered and explicitly
  fixed.

## Completion Standard

Phase 28 is complete when the project has a clear release-quality contract:

```text
what must pass
what each gate protects
what remains manual
what is intentionally deferred
what blocks future merges
```

The final report must not simply say "tests pass." It must explain whether the
current tests are sufficient, where they are insufficient, and which
insufficiencies are accepted versus fixed.
