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

## Hardened Boundaries (Phase 37 Doc Review)

The Phase 37 doc review tightened the sandbox before implementation begins. These
are binding requirements, recorded here so the spec and plan cannot drift back to
softer wording:

- **`safetyState` is a declared enum.** `readonly_sandbox_enabled` must be added
  to the Go enum, OpenAPI schema, and frontend type with tests before any ready
  target is returned. Today `internal/model/query_target.go` has only the
  constants — `QueryTargetSafetyStateDictionary()` and `Validate()` do not exist
  and must be added (per the `taxonomy.go` pattern), with `internal/model/README.md`
  updated. The service never emits an undeclared string.
- **Auth is closed across the contract.** Execute/history declare `401` and a
  Bearer security scheme; handler tests cover missing/invalid bearer; fuzz treats
  expected unauthenticated `401` as conformance, not failure; E2E reuses the
  authenticated client and never sends `actorUserId` in the body/query.
- **SELECT is treated as potentially side-effecting.** The guard rejects
  `SLEEP`, `BENCHMARK`, named-lock functions, `LOAD_FILE`, user-variable
  assignment, `INTO OUTFILE`/`DUMPFILE`, and locking clauses via AST walk, not
  string matching. A parser spike must prove reachability before the rule is
  considered done.
- **Query execution has a bounded token TTL.** A `QueryExecutionTokenMaxAge`
  gate applies to execution routes only; existing read/list auth is unchanged.
- **`environment_policy` is an enum that fails closed.** Values are `disabled`,
  `non_prod_only`, `all_environments`. Production is executable only with
  `all_environments`; unknown/empty is locked.
- **`credential_ref` is constrained to `[A-Z0-9_]+`** with bounded length. Phase
  37 has no credential write API, so it is rejected on resolve and by
  migration/seed (fail closed); the resolved DSN/password is never returned or
  logged.

## References

- Spec: `docs/superpowers/specs/2026-06-21-phase-37-read-only-query-sandbox.md`
- Plan: `docs/superpowers/plans/2026-06-21-phase-37-read-only-query-sandbox.md`
- Phase 36 boundary: `docs/decisions/2026-06-21-query-workbench-phase-36-boundary.md`
