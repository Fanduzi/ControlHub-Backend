# Decision: Phase 36 Query Workbench Is a Locked Directory and Shell

## Status

Accepted.

## Context

ControlHub now has enough database resource metadata to present query-capable
targets: engine, environment, owner, host, port, parent cluster, capability,
readiness, missing fields, and governance state.

Query execution is a separate high-risk capability. Opening it requires
read-only credentials, backend statement enforcement, timeout, row limit, audit,
production defaults, and later governance. A frontend-disabled Run button is not
a sufficient safety control.

## Decision

Phase 36 is limited to:

- backend `GET /query-targets` read model derived from existing resource data
- frontend `/query` locked Query Workbench shell
- explicit governance and missing-configuration states
- cross-repo E2E proving the shell loads and execution remains disabled

Phase 36 does not authorize or implement:

- query execution
- credentials
- SQL, Redis, or Mongo command execution
- export
- saved queries
- query history
- live schema introspection

## Evidence

- Backend Phase 36A: `0579b29`, `GET /query-targets`, backend CI run `27875169683` PASS.
- Frontend Phase 36B: `ff2681a`, `/query` locked shell, frontend CI run `27896150307` PASS.
- Cross-repo E2E: frontend workflow run `27896155506` PASS.
- Roadmap: `docs/superpowers/specs/2026-06-20-query-workbench-roadmap.md`.
- Design: `docs/superpowers/specs/2026-06-20-phase-36-query-workbench-foundation.md`.

## Consequences

Phase 37 must start from backend enforcement, not from enabling the frontend
Run button. The first execution milestone should be a single-engine read-only
sandbox with statement guard, audit, timeout, row limit, and production-safe
defaults.

Any future code path that executes SQL, Redis commands, or Mongo queries must be
treated as a new product capability with separate design, tests, and release
evidence.
