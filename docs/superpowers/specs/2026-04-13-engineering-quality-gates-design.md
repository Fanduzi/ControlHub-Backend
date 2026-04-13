# Engineering Quality Gates

## Goal

ControlHub development must stop relying on ad-hoc manual testing and main-worktree-only changes. The baseline needs three guardrails:

1. isolated implementation work by default
2. test-first discipline on meaningful code changes
3. a minimum end-to-end path for the frontend shell and resource flows

This is a process constraint for future phases, not a one-off cleanup.

## Decisions

### 1. Worktree First For Stage Work

Stage-level implementation should use a dedicated git worktree by default.

Required cases:

- frontend phase work
- backend phase work
- contract-altering integration work
- risky refactors

Exceptions:

- tiny doc-only changes
- small local hotfixes clearly scoped to one file

Reason:

- isolates unfinished work
- reduces dirty-tree collisions between frontend and backend streams
- makes it easier to abandon or compare partial implementations

### 2. TDD As Default Engineering Mode

ControlHub does not require performative TDD on every trivial edit, but meaningful feature work and bugfixes should follow test-first discipline.

Required cases:

- API contract changes
- bugfixes with reproducible behavior
- service/repository logic
- frontend interaction bugs
- view-model mapping changes

Expected loop:

1. write or update a failing test that captures the intended behavior
2. implement the smallest change to pass it
3. run the relevant test suite
4. refactor only after passing

Not required:

- pure copy changes
- doc-only edits
- trivial style-only UI tuning with no behavior change

### 3. Minimum Frontend E2E Coverage

The frontend now has unit and component tests, but no real end-to-end safety net. That is insufficient for the shell, routing, auth, and detail-sheet interactions.

The minimum required E2E coverage for the next frontend testing phase is:

1. login works with the seeded backend user
2. `/resources` renders live data
3. clicking a resource opens the right-side detail sheet
4. the detail sheet shows backend profile data
5. `/databases` opens the same sheet pattern without regression
6. `/settings` loads live dictionary-backed data

This suite should stay small, stable, and local-first.

### 4. Evidence Before Completion

For phase-level work, “implemented” is not enough. Completion reports must distinguish:

- unit/component tests
- build/lint
- live manual verification
- E2E verification

If one layer was not run, that must be stated explicitly.

### 5. Contract Work Must Prefer Stable Verification

When frontend and backend are changing in parallel:

- contract changes should be verified against OpenAPI and concrete JSON examples
- frontend should not silently widen types beyond confirmed backend support
- taxonomy and dictionaries should move toward backend-owned sources, not duplicated frontend assumptions
- comments or draft notes inside `openapi.yaml` do not count as contract; only
  formal OpenAPI paths and schemas count as backend API surface

## Immediate Application

The next process upgrade should do two things:

1. introduce a small frontend E2E suite for the critical resource-console path
2. start requiring stage-level workers to use worktree + TDD instructions by default

## Non-Goals

This document does not require:

- full browser-matrix testing
- visual regression infrastructure
- backend distributed integration tests
- large-scale CI redesign

The point is to raise the floor first.
