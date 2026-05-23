# Phase 29 Release Readiness Mechanism Design

## Background

Phase 28 established ControlHub's quality baseline:

- backend gates are documented
- frontend gates are documented
- E2E preflight exists
- full frontend E2E is green
- backend integration and OpenAPI fuzz gates are known

The remaining gap is operational: there is still no single release-readiness
mechanism that says, "this pair of frontend/backend commits is a releasable
candidate, and here is the evidence."

Phase 29 turns the Phase 28 baseline into a repeatable release-readiness
workflow. This is not a product feature phase.

## External Patterns To Learn From

### DeltaScope Backend Gate Pattern

DeltaScope uses Makefile aggregate targets and workflow gates to separate:

- local unit gates
- dialect or feature confidence gates
- release surface gates
- release version contract gates
- local version smoke
- release workflow hygiene gates
- release contract gates

Relevant DeltaScope targets:

```text
release-test-gates
pg-unit-test-gates
pg-e2e-gates
pg-confidence-gates
release-surface-gates
release-version-surface-gates
release-local-version-smoke
release-dialect-hygiene-gates
release-workflow-hygiene-gates
release-contract-gates
decision-record-gate
```

ControlHub should adopt the same principle, not the same implementation:

```text
small named gates
aggregate release-readiness target
contract gate separated from local unit gate
Docker-heavy gates clearly separated
release evidence recorded in a predictable format
```

### MusicRadio CDP E2E Pattern

MusicRadio has CDP scripts that connect to a running app, evaluate real renderer
state, and assert the app is usable from a live runtime:

```text
scripts/cdp-helper.cjs
scripts/cdp-ui-test.cjs
scripts/cdp-playback-test.cjs
scripts/cdp-test-dislike.cjs
```

The useful pattern is not Electron-specific. The useful pattern is:

```text
connect to an already-running browser/app via CDP
query DOM and runtime state directly
assert visible UI text and app responsiveness
collect console/network signals
emit a compact pass/fail report
```

ControlHub should use this as a complement to Playwright, not a replacement.
Playwright remains the automated browser regression suite. CDP live smoke is
for release-candidate sanity checks against a running local browser session.

## Goal

Build a repeatable release-readiness mechanism for ControlHub that covers both
backend and frontend.

Phase 29 should make this command-and-evidence workflow explicit:

```text
candidate commits -> preflight -> backend gates -> frontend gates -> live smoke -> evidence bundle -> go/no-go decision
```

## Non-Goals

- Do not release or publish anything.
- Do not create tags.
- Do not push to remotes.
- Do not change product UI.
- Do not change backend API contracts.
- Do not execute SQL manually.
- Do not add write operations or work orders.
- Do not replace Playwright.
- Do not introduce a full CI platform unless explicitly approved later.
- Do not add broad retries or output suppression.

## Release Candidate Definition

A ControlHub release candidate is a pair of commits:

```text
backend_commit=<GolangProjects/ControlHub main SHA>
frontend_commit=<JsProjects/ControlHub main SHA>
```

It must include:

```text
candidate identifier
backend commit and status
frontend commit and status
backend gate results
frontend gate results
live browser smoke results
known gaps
go/no-go decision
```

No candidate is "ready" unless its evidence is written down.

## Gate Model

### Backend Gate Layers

Adopt a DeltaScope-style layered model:

```text
backend-local-gates
backend-contract-gates
backend-docker-gates
backend-release-gates
```

Suggested mapping:

```text
backend-local-gates:
  go test -count=1 ./...
  go vet ./...
  go build ./...
  make openapi-validate

backend-docker-gates:
  make test-integration
  make test-openapi-fuzz

backend-release-gates:
  backend-local-gates
  backend-docker-gates when Docker is available
```

### Frontend Gate Layers

Adopt the same layered model:

```text
frontend-local-gates
frontend-browser-gates
frontend-live-smoke
frontend-release-gates
```

Suggested mapping:

```text
frontend-local-gates:
  npm run check:e2e-preflight
  npm run check:e2e-governance
  npx tsc --noEmit -p tsconfig.json
  npm run lint
  npm run test
  npm run build

frontend-browser-gates:
  npm run test:e2e:smoke
  npm run test:e2e:interaction
  npm run test:e2e

frontend-live-smoke:
  CDP or equivalent live browser check against running localhost

frontend-release-gates:
  frontend-local-gates
  frontend-browser-gates
  frontend-live-smoke
```

## CDP Live Smoke Direction

Phase 29 should consider a small frontend CDP smoke helper inspired by
MusicRadio.

It should not replace E2E tests. It should answer:

```text
When backend and frontend are running, does the current browser-rendered app
show the critical release-candidate pages without console/network failures?
```

Candidate pages:

```text
/overview?environment=prod
/databases?environment=prod
/resources/14
/resources/22
/resources?page=1&pageSize=1
/audits?page=1&pageSize=1
```

Candidate checks:

```text
page text contains expected heading
database sort trigger does not leak raw enum values
database signal trigger does not leak raw enum values
resource 14 shows needs-attention/member-signal content
resource 22 shows healthy instance content
topology section renders
console errors = 0
network 4xx/5xx = 0 unless explicitly allowlisted
browser API calls use /__api
```

CDP smoke can be implemented as:

```text
scripts/cdp-release-smoke.mjs
npm script: release:smoke:cdp
tests/scripts/cdp-release-smoke.test.ts
```

It should connect to a user-started Chrome remote debugging port, not launch or
kill browsers automatically.

## Evidence Bundle

Phase 29 should define a stable evidence bundle format:

```text
docs/releases/candidates/<candidate-id>.md
```

Candidate ID example:

```text
YYYY-MM-DD-controlhub-rc-local
```

The document should include:

```text
candidate metadata
backend commit
frontend commit
backend gate matrix
frontend gate matrix
live smoke matrix
manual checks
known gaps
go/no-go decision
operator notes
```

Evidence files are not release notes. They are local release-readiness records.

## Expected Deliverables

At minimum:

```text
backend/docs release-readiness design docs
backend Makefile aggregate gate targets or documented reason not to add them
frontend npm aggregate gate scripts or documented reason not to add them
frontend CDP live-smoke design/implementation or documented reason to defer
release candidate evidence template
one dry-run evidence file using current backend/frontend main commits
```

## Acceptance Criteria

- Backend has a named release-readiness gate or a documented deferral.
- Frontend has a named release-readiness gate or a documented deferral.
- CDP live smoke is either implemented or explicitly deferred with rationale.
- A release candidate evidence template exists.
- One local dry-run evidence file exists for the current backend/frontend main
  commit pair.
- Dry run clearly reports go/no-go.
- No product behavior changes.
- No release, tag, push, or deployment occurs.

## Completion Standard

Phase 29 is complete when a future worker can answer:

```text
Which exact commits are being evaluated?
Which exact commands prove they are releasable?
Where is the evidence?
What failed, if anything?
Who decided go/no-go and why?
```

This mechanism should make future release-readiness checks repeatable instead
of relying on memory or chat history.
