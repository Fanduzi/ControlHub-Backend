# Decision: Phase 37 Starts With Backend-Enforced MySQL/TiDB SELECT Sandbox

## Status

Accepted for Phase 37 planning.

## Context

Phase 36 completed the query target directory and locked `/query` workbench shell
without execution. The next step is the first real query execution path.

Execution is high risk because it can become a write console, data exfiltration
path, or production reliability risk if safeguards are incomplete.

## Decision

Phase 37 starts with MySQL/TiDB `SELECT` only.

Backend enforcement is mandatory:

- configured backend credential reference
- no plaintext credentials in database, API, logs, or browser
- parser-backed SQL guard
- no multi-statement execution
- no writes, DDL, admin statements, `CALL`, `SET`, or transaction control
- timeout
- maximum returned rows
- maximum columns and response size
- audit/history row for every attempt

Frontend may enable Run only when the backend returns
`availableActions.run=true`. The frontend must not become the enforcement
layer.

## Why MySQL/TiDB First

ControlHub already uses the MySQL driver and Testcontainers MySQL. This makes
MySQL/TiDB the smallest useful slice that still proves the hard platform
mechanics: credential resolution, SQL guard, read-only execution, audit, and
history.

ClickHouse, PostgreSQL, Redis, and MongoDB need different drivers and guard
models. They are deferred until the single-engine sandbox is proven safe.

## Consequences

Phase 37 implementation must not add multi-engine execution. Any support for
ClickHouse, PostgreSQL, Redis, or MongoDB belongs in Phase 38 or later.

Any future decision to store credentials in a database, return credentials to a
client, enable export, or allow non-SELECT statements requires a separate design
and explicit approval.

## References

- Spec: `docs/superpowers/specs/2026-06-21-phase-37-read-only-query-sandbox.md`
- Plan: `docs/superpowers/plans/2026-06-21-phase-37-read-only-query-sandbox.md`
- Phase 36 boundary: `docs/decisions/2026-06-21-query-workbench-phase-36-boundary.md`
