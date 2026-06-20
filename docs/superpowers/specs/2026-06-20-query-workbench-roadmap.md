# Query Workbench Roadmap

## Purpose

ControlHub should eventually support a query platform for database-like systems:

```text
SQL engines:
  MySQL, TiDB, PostgreSQL, ClickHouse

Command/document engines:
  Redis, MongoDB
```

This capability is high-risk. It must be built in phases so ControlHub does not
accidentally become a production write console or a data exfiltration path.

The roadmap below records the intended sequence and safety boundaries.

## Phase 36 — Query Capability Inventory

First, do not execute queries. Convert the database resources already tracked by
ControlHub into a query target directory.

Questions to answer:

- Which resources support querying?
- Which engine is each target: MySQL, TiDB, PostgreSQL, ClickHouse, Redis, or
  MongoDB?
- Is connection information complete?
- Is there a read-only credential configured?
- Is querying currently allowed?
- What configuration is missing?
- What safety restrictions apply?

Expected outputs:

- Backend read model: `queryTargets`
- Frontend page: `/query` or `/databases/query`
- Each target displays:
  - engine
  - environment
  - host / port
  - parent cluster
  - connection state: unknown / configurable / unavailable
  - query capability: SQL / Redis command / Mongo query
  - safety state: credential missing / read-only credential / production query disabled
- No query execution.

Value:

```text
Turns "query platform" from an idea into an inspectable product model without
introducing execution risk.
```

## Phase 37 — Read-Only Query Sandbox

Add query execution for one low-risk engine first, likely ClickHouse or
MySQL/TiDB `SELECT`.

Required safeguards:

- read-only credential
- SQL parsing or strict statement guard
- reject `INSERT`, `UPDATE`, `DELETE`, DDL, `CALL`, `SET`
- reject multi-statement input
- enforce or validate `LIMIT`
- query timeout
- maximum returned row count
- audit record for every execution
- query history
- no export support
- stricter production-environment defaults

Do not add multi-engine support until the single-engine sandbox proves safe.

## Phase 38 — Multi-Engine Query Workbench

Extend the sandbox after Phase 37 is stable.

Targets:

- PostgreSQL
- ClickHouse
- Redis read commands
- MongoDB `find` / `aggregate` allowlist
- per-engine editor hints
- result table and JSON viewer
- saved queries
- query templates

Each new engine needs its own guard model. Do not assume SQL guard rules apply
to Redis or MongoDB.

## Phase 39 — Query Governance

Add governance only after query execution exists and is useful.

Potential capabilities:

- permission request flow
- approval
- query risk score
- result masking
- export approval
- kill query
- connection pool management
- secret rotation

Governance is intentionally later because it depends on the actual query
execution model and audit surface.

## Permanent Boundaries

These boundaries apply to every phase:

- Never execute writes through the query workbench.
- Never store plaintext credentials in frontend code or browser storage.
- Never add production query execution without explicit safety defaults.
- Never allow query execution without audit.
- Never add export before masking and approval are designed.
- Never treat Redis or MongoDB commands as "just SQL with different syntax".
- Never skip backend enforcement because the frontend hides a button.
