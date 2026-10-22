# Server Bootstrap Module

The single dependency-injection seam that builds the production
`api.Dependencies` graph from repositories and services.

## Files

| File | Responsibility |
|------|---------------|
| serverboot.go | `Build(db, cfg)` — the exact production wiring (moved out of `cmd/server`), plus `Parts` (the concrete services a test-only assembly may additionally gate) and `RealClock` |

## Contract

- `Build` returns the production graph **unopened**: the PostgreSQL HTTP
  entries (`WithPGHTTPEntries`) and the connections[] read model
  (`WithConnectionLister`) are construction-time capabilities left off.
- `cmd/server` calls `Build` and discards `Parts` — the production binary
  cannot enable the PG HTTP routes at runtime.
- `cmd/e2e-pg-server` (test-only) calls the same `Build`, then flips the two
  construction-time gates on the returned concrete services. Any behavior
  change to the governed surface belongs in `Build` exactly once so the
  fixture can never silently diverge from production.
- `Build` performs the `pgsql.GuardPG` warm-up (the wazero WASM compile) so
  no governed attempt loses its frozen statement budget to the compile.

Dependency flow: `serverboot` → `api`, `service`, `repository/mysql`,
`config`, `pgsql`. Nothing imports `serverboot` except `cmd/*` entry points.
