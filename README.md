# ControlHub Backend

## Prerequisites

- Go `1.26.x`
- This workspace is aligned to local `asdf` default `Go 1.26.1`
- MySQL 8.0+
- **Docker** (required for integration tests and OpenAPI fuzz tests only)
- **Schemathesis** (required for OpenAPI fuzz tests only: `pip install schemathesis`)

## Setup

```bash
# 1. Create the database
mysql -u root -e "CREATE DATABASE controlhub CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"

# 2. Create a dedicated user (recommended over using root)
mysql -u root -e "CREATE USER IF NOT EXISTS 'controlhub'@'%' IDENTIFIED BY 'controlhub_dev';"
mysql -u root -e "GRANT ALL PRIVILEGES ON controlhub.* TO 'controlhub'@'%'; FLUSH PRIVILEGES;"

# 3. Apply migrations via goose
make migrate-up

# 4. Copy .env and adjust DSN if needed
cp .env.example .env

# 5. Generate a real JWT_SECRET and record it in .env (startup rejects blank or placeholder values)
openssl rand -hex 32

# 6. Bootstrap the operator admin with deployment-supplied credentials.
#    No seeded login ships with the app; migration 00016 retired the old example.com accounts.
#    Requires DATABASE_DSN (from .env), BOOTSTRAP_ADMIN_EMAIL, and BOOTSTRAP_ADMIN_PASSWORD.
BOOTSTRAP_ADMIN_EMAIL="<operator-email>" \
BOOTSTRAP_ADMIN_PASSWORD="<operator-password>" \
go run ./cmd/bootstrap-admin
```

### Local Bigint Cutover With Historical Import

If your daily local `controlhub` database still contains the older UUID-backed schema and demo/mock/history data you want to preserve, use the repo-local preserve-then-import cutover flow instead of `migrate-reset-dev`.

What this does:

- Preserves the current runtime database as `controlhub_v1`
- Recreates a fresh bigint-backed `controlhub`
- Runs the current goose migration chain on the rebuilt target
- Imports historical UUID-backed data from `controlhub_v1` into the new bigint schema

Preconditions:

- Stop anything actively using `controlhub`
- Keep `DATABASE_DSN` pointed at `controlhub`
- Ensure the DSN includes `parseTime=true`
- Ensure `controlhub_v1` does not already contain tables you need to keep

Run the cutover:

```bash
cp .env.example .env
CONFIRM=yes make cutover-local
```

If the first run already preserved `controlhub_v1` and rebuilt `controlhub` but stopped before import finished, resume explicitly:

```bash
go run ./cmd/cutover-local --target-db controlhub --preserve-db controlhub_v1 --resume
```

Notes:

- `make cutover-local` is the safe historical-import path for this bigint redesign.
- `--resume` is intentionally explicit; it is only for continuing an interrupted preserve-then-import cutover.
- `CONFIRM=yes make migrate-reset-dev` is destructive and rebuilds from migrations only; it does not preserve your current local historical/mock data.
- Keep `controlhub_v1` after a successful cutover so you can compare or roll back later.

### Migration Commands

| Command | Description |
|---------|-------------|
| `make migrate-up` | Apply all pending migrations |
| `make migrate-status` | Show current migration state |
| `make migrate-down-one` | Roll back one migration |
| `CONFIRM=yes make cutover-local` | Preserve old `controlhub` as `controlhub_v1`, rebuild bigint `controlhub`, import historical data |
| `CONFIRM=yes make migrate-reset-dev` | Drop + recreate DB + apply all (destructive, no historical import) |

Migrations are **not** run automatically on server startup.

`make run` now attempts to load `.env` automatically during startup. If a variable is already exported in your shell, that exported value takes precedence over `.env`.

## Run

```bash
make run
```

The server starts on `http://localhost:8080` by default.

You can still override local defaults inline for a single run:

```bash
DATABASE_DSN="controlhub:controlhub_dev@tcp(127.0.0.1:3306)/controlhub?parseTime=true&charset=utf8mb4" \
JWT_SECRET="$(openssl rand -hex 32)" \
make run
```

### Local Query Workbench acceptance (cross-repo)

Start the backend ready for local Query Workbench acceptance — Query E2E fixture
ensured, Local MySQL Query Dev target metadata seeded (idempotent), server on
`APP_PORT` (default 8080) with a fresh ephemeral `JWT_SECRET` (overrides any
placeholder in `.env`):

```bash
make run-query-dev
```

No DSN, password, or token is printed; the quoted credential DSN from the
per-user state file under `${XDG_STATE_HOME:-$HOME/.local/state}/controlhub/`
(isolated per fixture identity: container/port/database/read-only user) is
sourced, never parsed. The first run migrates a legacy
`.query-e2e-mysql.env` when present and matching the current identity. Requires Docker (fixture), `DATABASE_DSN`
(from `.env` or the shell), and `openssl`.

Manual acceptance sequence with the frontend (see the frontend README):

1. Backend: `make run-query-dev`
2. Frontend: `npm run dev:local` (Next on `http://localhost:3000`)
3. Log in and run a governed query against the **Local MySQL Query Dev** target.

Restarting either service invalidates the local session: the backend signs
tokens with a fresh ephemeral `JWT_SECRET` each start, and the frontend seals
sessions with a fresh ephemeral BFF key each start — log in again after any
restart.

To confirm the local toolchain before running, use `go version`.

## Local Verification

Health check:

```bash
curl http://localhost:8080/health
```

Bootstrap an admin account before logging in (no seeded login ships with the
app; the old example.com accounts were retired by migration 00016):

```bash
BOOTSTRAP_ADMIN_EMAIL="<operator-email>" \
BOOTSTRAP_ADMIN_PASSWORD="<operator-password>" \
go run ./cmd/bootstrap-admin
```

Login smoke test with the bootstrapped credentials:

```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"<operator-email>","password":"<operator-password>"}'
```

Additional API smoke test (authenticated — every operational endpoint requires
a Backend Bearer Credential):

```bash
# 1. Run the login curl above and copy the "token" value from its JSON response.
TOKEN="<paste-token-here>"

# 2. Smoke requests with the bearer token:
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/resources
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/resources/1/profile
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/environments
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/owners
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/roles
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/resource-types
curl -H "Authorization: Bearer $TOKEN" http://localhost:8080/relation-types
```

API documentation:

- **Docs UI**: http://localhost:8080/docs (Scalar API Reference)
- **Raw spec**: http://localhost:8080/openapi.yaml

## Test

Unit tests (no Docker required):

```bash
go test ./internal/api -v
go test ./internal/model -v
go test ./internal/service -v
make test
go vet ./...
go build ./...
```

OpenAPI contract validation:

```bash
make openapi-validate
```

### Integration Tests

Integration tests exercise real MySQL behavior using disposable Testcontainers containers.

- **Requires Docker** running locally.
- Tests start a fresh MySQL 8.0 container, run all goose migrations, and terminate it after.
- Does **not** touch your daily `controlhub` database.

```bash
make test-integration
```

Run a specific integration test:

```bash
go test -tags=integration -count=1 -v -run TestResourceRepository ./internal/integration
```

Run the issue #83 ingestion confirmation suite:

```bash
make test-ingestion-integration
```

When to run which:

| Command | When | Docker? | Extra deps |
|---------|------|---------|------------|
| `make test` | Every commit — fast unit tests | No | — |
| `make test-integration` | Before merge — real MySQL validation | Yes | — |
| `make test-ingestion-integration` | Issue #83 atomic confirmation validation | Yes | — |
| `make test-openapi-fuzz` | Before merge — contract fuzzing | Yes | schemathesis |

### OpenAPI Fuzz Testing

Schemathesis-based fuzz testing exercises API endpoints against a real server backed by disposable Testcontainers MySQL.

- **Requires Docker** and the **Schemathesis CLI** (`pip install schemathesis` or `pipx install schemathesis`).
- Starts a disposable MySQL 8.0 container, runs goose migrations, starts the ControlHub HTTP server on a random port, and runs Schemathesis against `/openapi.yaml`.
- Does **not** touch your daily `controlhub` database.
- Writes are exercised freely (the database is disposable).
- `executeSavedStatement` is excluded from fuzzing: the disposable fuzz DB has no stable saved-statement fixture data to execute against. Dedicated handler and template-execution tests cover that operation instead.

```bash
make test-openapi-fuzz
```

The run uses bounded settings suitable for AI agents and local development:
- `--max-examples 50` — 50 generated test cases per operation
- `--seed 42` — reproducible runs
- `--checks not_a_server_error,status_code_conformance,content_type_conformance,response_schema_conformance`
- `--mode all` — both positive and negative data generation
- `--phases examples,fuzzing`

If Schemathesis finds contract violations, the test fails with a summary. Reports are saved to `.schemathesis-reports/`.

## Architecture

ControlHub is a read-heavy resource management backend exposing dictionary-driven APIs for a frontend console. Resources are typed entities (8 types) linked by directed relations (7 types), with immutable origin, environment-scoped aliases, globally unique external identifiers, per-type profile projections, named personal/shared Inventory views, and a taxonomy system of static dictionaries. Manual registration of host, database instance, database cluster, and service requires that type's typed-profile minimum identity; service taxonomy includes worker. Domain Name uses subtype `dns` and a required normalized FQDN; Virtual IP uses subtype `floating` and a required single IP address.

### Modules

| Module | Description | Doc |
|--------|-------------|-----|
| cmd/server | Application entry point, dependency wiring, graceful shutdown drain (Issue #37) | [README](cmd/server/README.md) |
| cmd/e2e-fixture-bootstrap | TEST/CI-ONLY admin+editor fixture provisioning for isolated E2E runs; refuses the retired 0002 seed identities | [README](cmd/e2e-fixture-bootstrap/README.md) |
| internal/api | HTTP handlers, routing, CORS, test server | [README](internal/api/README.md) |
| internal/service | Business logic, repository interfaces | [README](internal/service/README.md) |
| internal/repository/mysql | MySQL data access, SQL queries | [README](internal/repository/mysql/README.md) |
| internal/model | Domain structs, taxonomy constants, validation | [README](internal/model/README.md) |
| internal/config | Environment and .env configuration loading | [README](internal/config/README.md) |
| internal/openapi | Embedded OpenAPI contract and validation | [README](internal/openapi/README.md) |
| internal/integration | MySQL-backed Testcontainers coverage | [README](internal/integration/README.md) |
| internal/testsupport | Test-only shared authorization metadata and fixtures | [README](internal/testsupport/README.md) |
| internal/cutover | One-shot legacy UUID→bigint data preservation and import | [README](internal/cutover/README.md) |

Dependency flow (strict, one-directional): `cmd/server` → `api` → `service` → `repository/mysql` → `model`. The `model` package has no upstream dependencies.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | /auth/login | Login with local credentials |
| GET | /health | Health check |
| GET | /resources | List resources |
| GET | /resources/{id} | Get resource detail |
| POST | /resources/{id}/health-observations | Admin User or `health:write` Machine Credential route for latest health evidence without inventory audit |
| GET | /resources/{id}/profile | Get resource typed profile projection |
| GET | /resources/{id}/relations | List relations for a resource |
| GET | /resources/{id}/topology | Get a rooted topology graph |
| GET | /environments/{id}/topology | Get an environment-scoped topology workspace |
| GET | /resources/{id}/audit-events | List audit events for a resource |
| GET | /audit-events | List audit events; optional server-side `environmentId` filter by target resource environment |
| GET | /inventory/views | List owned personal and shared Inventory views |
| POST | /inventory/views | Create a personal or admin-shared Inventory view |
| PUT | /inventory/views/{viewId} | Update an owned personal or admin-managed shared view |
| DELETE | /inventory/views/{viewId} | Delete an owned personal or admin-managed shared view |
| POST | /admin/ingestions/preview | Admin User or `inventory:ingest` Machine Credential route for bounded exact-match CSV/JSON preview |
| POST | /admin/ingestions/confirm | Admin User or `inventory:ingest` Machine Credential route for reviewed atomic confirmation |
| GET | /environments | List environments |
| GET | /owners | List owners |
| GET | /roles | List roles |
| GET | /resource-types | List resource type dictionary items |
| GET | /relation-types | List relation type dictionary items |
| GET | /lifecycle-statuses | List lifecycle status dictionary items |
| GET | /health-statuses | List health status dictionary items |
| GET | /query-targets/{id}/schema/table-definition | Get MySQL table definition (base tables only) |
| POST | /query-targets/{id}/execute | Execute a governed read-only statement, with optional result paging for SELECT |
| POST | /query-targets/{id}/saved-statements/{statementId}/execute | Execute a saved statement (governed template execution) through the existing governed chain |
| GET | /ops/query-evidence-metrics | Admin-only query-evidence persistence-failure counter for atomic Execution Evidence Pair writes (Issue #34) |
| GET | /openapi.yaml | Raw OpenAPI 3.1.0 spec |
| GET | /docs | Scalar API Reference docs UI |

### Operator Access Boundary

`/health`, `/auth/login`, `/openapi.yaml`, and `/docs` are public. All
operational APIs require a Backend Bearer Credential. Authenticated editors can
read Inventory, dictionaries, query-target lists, saved-statement lists, and
fresh governed query/schema surfaces. Router-admin operations cover Inventory
mutations, audit reads, and operational metrics reads (auth-audit and
query-evidence metrics). Handler-admin operations cover credential writes and
all disclosure-policy operations, including GET. Saved-statement mutations are
not a router-wide admin gate: Phase 38R authorizes personal statements by owner
and shared templates by admin role. Named Inventory views follow the same
owner/admin split while remaining readable as shared views by every user.

Admin CI ingestion uses one strict multipart upload shape: `format=csv|json` and
one `file`; confirmation resubmits the same reviewed data with its fingerprint.
Preview is read-only. Confirmation rechecks the exact parsed input and inventory
inside the existing single MySQL transaction, so conflicts, drift, or any write
failure leave no partial batch committed.

Opaque `chmp_` Machine Credentials use a separate, closed scope matrix for
Inventory, relation/topology, query-target discovery, audit, shared Named View
reads, ordinary governed query execution, and the reserved collector-only
`inventory:ingest` and `health:write` routes. Collector actor/observer binding
and ingestion wiring remain deferred. Migration 00027 now provides a
per-principal completed-scan ledger and capped per-principal/per-CI omission
state; only successful complete scans can make a CI Missing after three
consecutive omissions, and rediscovery resets that state. Only
`POST /query-targets/{id}/execute` accepts the `governed-select` machine scope;
all sibling query routes remain user-only. Machine execution evidence records
the machine principal identity, never a synthetic User or credential material.
All ordinary mutations and unlisted routes fail with controlled machine
authorization errors.

Resource list and detail responses include effective `healthStatus`,
`healthFreshness` (`fresh`, `stale`, or `never`), `healthObservedAt`,
`healthObserver`, and the nullable `manualHealthOverride`. Effective health is
the worst fresh observation; stale or never-observed evidence fails closed and
never appears healthy. A per-resource-type threshold can be supplied to the
repository, with a 24-hour fallback. Observation upserts keep one latest row per
observer and never create inventory audit events. Setting or clearing the
manual override through resource PATCH commits with the existing #72 audit
evidence in the same MySQL transaction.

The API, OpenAPI, and MySQL integration authorization tests consume the same
test-only operation table at `internal/testsupport/operatoraccess/policy.go`.

### Governed query result paging

`POST /query-targets/{id}/execute` accepts optional page-number pagination for
bare `SELECT` statements. Send `pagination.page` as a 1-based page number and
`pagination.pageSize` as one of 10, 25, 50, or 100. The response reports the
requested page, page size, and whether adjacent pages exist. It does not expose
totals or snapshot identifiers.

The server owns page-window and row-cap enforcement. Each page is a fresh
governed execution with access, credential, statement, disclosure, timeout,
cap, history, and audit checks. The browser does not rewrite SQL. Result rows
are not persisted, and no result snapshot is retained between pages.

`SHOW`, `DESCRIBE`, and typed `EXPLAIN` remain single-response metadata
statements. Supplying pagination does not split those responses or create page
navigation metadata.

## Audit Storage Strategy

Phase-1 keeps a minimal `audit_events` table in MySQL for local
development and demo purposes only.  The table has no foreign-key
constraints on the resource or user tables, so resource write paths
never depend on it transactionally.

The long-term backing store for audit events is **ClickHouse**, which
is better suited for append-only, high-write, time-range queries.  The
current HTTP contract (`GET /audit-events`,
`GET /resources/{id}/audit-events`) will remain unchanged when the
migration to ClickHouse happens — only the repository implementation
will be swapped.

Health observations are operational evidence, not inventory mutations, and are
therefore intentionally absent from this audit stream. Manual override changes
remain governed inventory mutations and are audited atomically.

## Demo Data

Migration `0004_seed_demo_data.sql` provides scenario-based demo data
for frontend integration testing:

- **~64 resources** across 3 environments (production, staging, development)
- **8 resource types**: host, database_instance, database_cluster, service,
  domain_name, virtual_ip, database_proxy, control_plane_component
- **5 business domains**: Order, Payment, User, Analytics, Config
- **~60 relations** covering member_of, runs_on, depends_on, fronts,
  points_to, manages
- **25 audit events** with varied event types and results
- **Status variety**: running/healthy (majority), warning, critical,
  degraded, stopped/unknown, provisioning

The data is structured to exercise frontend edge cases: empty profiles,
sparse labels, long display names, mixed health/lifecycle states, and
cross-type relation chains.
