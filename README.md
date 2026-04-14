# ControlHub Backend

## Prerequisites

- Go `1.26.x`
- This workspace is aligned to local `asdf` default `Go 1.26.1`
- MySQL 8.0+

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
```

### Existing Local DB

If you already have a `controlhub` database that was migrated manually before goose was introduced, baseline it:

```bash
mysql -u root controlhub -e "
CREATE TABLE IF NOT EXISTS goose_db_version (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    version_id bigint NOT NULL,
    is_applied boolean NOT NULL,
    tstamp timestamp NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;
INSERT INTO goose_db_version (version_id, is_applied) VALUES
(0,true),(1,true),(2,true),(3,true),(4,true),(5,true),(6,true);
"
make migrate-status   # confirm all 6 show as Applied
```

### Migration Commands

| Command | Description |
|---------|-------------|
| `make migrate-up` | Apply all pending migrations |
| `make migrate-status` | Show current migration state |
| `make migrate-down-one` | Roll back one migration |
| `CONFIRM=yes make migrate-reset-dev` | Drop + recreate DB + apply all (destructive) |

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
JWT_SECRET="override-secret" \
make run
```

To confirm the local toolchain before running, use `go version`.

## Local Verification

Health check:

```bash
curl http://localhost:8080/health
```

Seeded login credentials:

- `admin@example.com` / `secret123`
- `editor@example.com` / `secret123`

Login smoke test:

```bash
curl -X POST http://localhost:8080/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"secret123"}'
```

Additional API smoke test:

```bash
curl http://localhost:8080/resources
curl http://localhost:8080/resources/40000000-0000-0000-0000-000000000002/profile
curl http://localhost:8080/environments
curl http://localhost:8080/owners
curl http://localhost:8080/roles
curl http://localhost:8080/resource-types
curl http://localhost:8080/relation-types
```

API documentation:

- **Docs UI**: http://localhost:8080/docs (Scalar API Reference)
- **Raw spec**: http://localhost:8080/openapi.yaml

## Test

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

## Architecture

ControlHub is a read-heavy resource management backend exposing dictionary-driven APIs for a frontend console. Resources are typed entities (8 types) linked by directed relations (7 types), with per-type profile projections and a taxonomy system of static dictionaries.

### Modules

| Module | Description | Doc |
|--------|-------------|-----|
| cmd/server | Application entry point, dependency wiring | [README](cmd/server/README.md) |
| internal/api | HTTP handlers, routing, CORS, test server | [README](internal/api/README.md) |
| internal/service | Business logic, repository interfaces | [README](internal/service/README.md) |
| internal/repository/mysql | MySQL data access, SQL queries | [README](internal/repository/mysql/README.md) |
| internal/model | Domain structs, taxonomy constants, validation | [README](internal/model/README.md) |
| internal/config | Environment and .env configuration loading | [README](internal/config/README.md) |

Dependency flow (strict, one-directional): `cmd/server` → `api` → `service` → `repository/mysql` → `model`. The `model` package has no upstream dependencies.

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | /auth/login | Login with local credentials |
| GET | /health | Health check |
| GET | /resources | List resources |
| GET | /resources/{id} | Get resource detail |
| GET | /resources/{id}/profile | Get resource typed profile projection |
| GET | /resources/{id}/relations | List relations for a resource |
| GET | /resources/{id}/audit-events | List audit events for a resource |
| GET | /audit-events | List audit events |
| GET | /environments | List environments |
| GET | /owners | List owners |
| GET | /roles | List roles |
| GET | /resource-types | List resource type dictionary items |
| GET | /relation-types | List relation type dictionary items |
| GET | /lifecycle-statuses | List lifecycle status dictionary items |
| GET | /health-statuses | List health status dictionary items |
| GET | /openapi.yaml | Raw OpenAPI 3.1.0 spec |
| GET | /docs | Scalar API Reference docs UI |

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
