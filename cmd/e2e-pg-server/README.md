# E2E PostgreSQL Fixture Listener (test/CI-only)

Long-lived HTTP listener for the governed PostgreSQL query-workbench browser
acceptance path (issues #121/#122). It serves the **exact** production
dependency graph from `internal/serverboot.Build` plus two construction-time
gates that are never opened by `cmd/server`:

- `QueryExecutionService.WithPGHTTPEntries()` — the T12 three-route
  PostgreSQL dispatch (`POST /query-targets/{id}/execute`,
  `/saved-statements/{statementId}/execute`, `/related-records`).
- `QueryTargetService.WithConnectionLister(...)` — the `connections[]`
  read-model projection so the browser can enumerate real
  `(target, database)` connections with per-connection resolver-verified
  runtime status.

No handler, claim, finalize, guard, disclosure, or parameter mechanic is
copied or weakened — every PostgreSQL request runs the real governed chain
against a real disposable PostgreSQL instance.

## Safety boundary (primary production guard)

The listener refuses to start unless ALL of the following hold, **before** it
opens the metadata database:

1. `CONTROLHUB_E2E_FIXTURE_MODE=1` — the explicit test-only capability.
2. `DATABASE_DSN` parses with the MySQL driver, uses a literal loopback host
   (`127.0.0.1`/`::1`; hostnames like `localhost` are refused), and names a
   disposable `controlhub_*_e2e` database. Driver parse details and DSN
   contents are never echoed.
3. `E2E_RUN_ID` is non-empty — the per-run identity.

The default `cmd/server` keeps PostgreSQL HTTP execution closed; no request
field, query parameter, header, or environment variable on the production
binary can open it. This fixture binary never gets deployed to production.

## Run identity

`GET /e2e/assembly` returns the run-identity document so Playwright never
mistakes a stale or foreign listener for this run's backend:

```json
{ "kind": "controlhub-e2e-pg-fixture", "runId": "...", "backendSha": "<vcs revision>", "pgEntries": true, "listenAddr": "127.0.0.1:8080" }
```

`backendSha` is the Go toolchain's embedded `vcs.revision`.

## Usage

```bash
CONTROLHUB_E2E_FIXTURE_MODE=1 \
DATABASE_DSN='controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub_e2e?parseTime=true' \
E2E_RUN_ID="$RUN_ID" \
JWT_SECRET='...' APP_PORT=8080 \
CONTROLHUB_QUERY_CREDENTIAL_<REF>='postgres://ro:pw@127.0.0.1:5432/<db>?sslmode=disable' \
go run ./cmd/e2e-pg-server
```

PostgreSQL connections are then provisioned through the real admin API: a
`database_instance` resource with `engine: postgresql`, plus one
`PUT /query-targets/{id}/credential?database=<db>` row per connection whose
`credentialRef` resolves to a DSN bound to that database.
