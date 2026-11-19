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

## Engine integration acceptance

The platform serves developers. Deliver each engine through the existing `/query` workbench: select the target and connection, inspect the available schema, author a read-only query in the worksheet, run it, and see bounded results or a controlled refusal. SQL engines use SQL; Redis and MongoDB use their native read-only query models. MySQL/TiDB and the PostgreSQL worksheet are the reference experience. The sections below record the original phase plan; use the accepted [multi-engine decisions (#98)](https://github.com/Fanduzi/ControlHub-Backend/issues/98), [minimum engine capability decision (#99)](https://github.com/Fanduzi/ControlHub-Backend/issues/99) and [PostgreSQL delivery spec (#108)](https://github.com/Fanduzi/ControlHub-Backend/issues/108) for later decisions.

**Planning and review.** Name the user action the change enables and the existing worksheet path it extends. Treat a request such as “seven-day volume” or “top services” as an example query for acceptance. A dedicated report or shortcut is a separate product feature when explicitly requested. A feasibility test can settle a driver, budget or disclosure question; record exactly which worksheet actions still need implementation. If a safety constraint limits the first slice, narrow the supported query grammar and describe its refusals. Surface a proposal to change the user workflow before building a different interface.

### ClickHouse recovery milestone

The shipped [table preview and fixed daily count](../../clickhouse-preview.md) remain available on their existing contracts. They supply evidence for Native TLS, target binding, budgets, cancellation, lossless values, masking and atomic history/audit. **Developer-authored ClickHouse SQL now supports a bounded single-table grammar in the shared worksheet.** The [worksheet guide](../../clickhouse-preview.md#developer-authored-sql-in-the-worksheet) documents the separate opt-in, supported operators and remaining limits. The preview-specific approval for a timestamp and a fixed count does not authorize arbitrary SQL. Keep the production rollout separate.

1. Extend the ordinary governed Run path with a ClickHouse dialect guard for a bounded, single-statement `SELECT` subset. Start with single-table projections and filters; extend to grouping and aggregation once their referenced columns can be verified. Check every reference, including those used only for filtering, grouping and ordering. Refuse unsupported shapes or uncertain provenance before returning rows. Reuse the proven connection, disclosure and evidence contracts where applicable.
2. Expose Run in the existing SQL editor only for eligible targets. Use the existing result grid and cancellation flow. Track schema browsing, completion, history/restore, paging, saved statements, Explain and export against the PostgreSQL reference, stating what each slice actually delivers.
3. Accept the first slice in a real browser against synthetic non-production data. Select ClickHouse, type `SELECT request_id FROM events WHERE request_id = 42 LIMIT 10`, run it, edit `42` to another seeded ID, and verify the new result and corresponding execution evidence. Verify that disallowed SQL and an unauthorized predicate receive a controlled refusal with no data. Accept aggregation by typing a second SQL statement with `GROUP BY` into the **same worksheet**. A preview-only test or a test that clicks a fixed count button covers only that feature.

## Phase 36 — Query Workbench Shell + Capability Inventory

Status: **Complete**.

Evidence:

```text
Backend Phase 36A: main commit 0579b29, Backend CI run 27875169683 PASS
Frontend Phase 36B: main commit ff2681a, Frontend CI run 27896150307 PASS
Cross-repo E2E: frontend workflow run 27896155506 PASS
```

First, do not execute queries. Build the visible shell of a governed query
workspace, backed by the database resources already tracked by ControlHub.

The important product correction after Bytebase research: Phase 36 must not be a
standalone inventory table. The inventory should live inside a workbench shell so
users can understand the future query platform shape without getting an enabled
execution path.

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
- Frontend page: `/query`
- Workbench shell:
  - target switcher / connection context
  - schema/object browser placeholder
  - worksheet tabs placeholder
  - editor placeholder with Run / Explain / Export locked
  - result grid / JSON / Explain / Logs placeholder
  - history placeholder
  - access / governance panel
- Each query target displays:
  - engine
  - environment
  - host / port
  - parent cluster
  - connection state: unknown / configurable / unavailable
  - query capability: SQL / Redis command / Mongo query
  - safety state: credential missing / read-only credential / production query disabled
- No query execution.

Implemented boundary:

```text
No query execution API
No query credentials
No SQL / Redis / Mongo execution path
No export
No saved-query API
No query-history API
```

Value:

```text
Turns "query platform" from an idea into an inspectable product model without
introducing execution risk.
```

Reference learning:

```text
docs/superpowers/notes/2026-06-20-phase-36-bytebase-ui-research.md
```

## Phase 37 — Read-Only Query Sandbox

Status: **Designed**.

Design and implementation plan:

```text
docs/superpowers/specs/2026-06-21-phase-37-read-only-query-sandbox.md
docs/superpowers/plans/2026-06-21-phase-37-read-only-query-sandbox.md
```

Add query execution for one low-risk engine family first:
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
- reject SELECT side-effect/resource functions (`SLEEP`, `BENCHMARK`, named-lock
  functions, `LOAD_FILE`), user-variable assignment, `INTO OUTFILE`/`DUMPFILE`,
  and locking clauses via AST walk, not string matching
- declared `safetyState` enum (no undeclared strings such as a raw
  `readonly_sandbox_enabled`)
- bounded query-execution token TTL (`QueryExecutionTokenMaxAge`)
- `environment_policy` enum that fails closed (`disabled` / `non_prod_only` /
  `all_environments`)
- `credential_ref` constrained to `[A-Z0-9_]+`; resolved DSN never returned or
  logged

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
