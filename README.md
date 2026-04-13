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

# 3. Apply migrations
mysql -u root controlhub < migrations/0001_initial_schema.sql
mysql -u root controlhub < migrations/0002_seed_reference_data.sql
mysql -u root controlhub < migrations/0003_expand_resource_type_constraint.sql

# 4. Copy .env and adjust DSN if needed
cp .env.example .env
```

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

## Test

```bash
go test ./internal/api -v
go test ./internal/model -v
go test ./internal/service -v
make test
go vet ./...
go build ./...
```

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
