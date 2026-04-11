# ControlHub Backend

## Prerequisites

- Go `1.26.x`
- This workspace is aligned to local `asdf` default `Go 1.26.1`
- PostgreSQL

## Run

1. Copy `.env.example` to `.env`.
2. Create a PostgreSQL database named `controlhub`.
3. Apply the SQL files in `migrations/`:
   `psql "$DATABASE_URL" -f migrations/0001_initial_schema.sql`
   `psql "$DATABASE_URL" -f migrations/0002_seed_reference_data.sql`
4. Start the server with `make run`.

To confirm the local toolchain before running, use `go version`.

The seeded login credentials are:

- `admin@example.com` / `secret123`
- `editor@example.com` / `secret123`

## Test

Run `go test ./internal/api -v`

Run `go test ./internal/model -v`

Run `go test ./internal/service -v`

Run `make test`

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| POST | /auth/login | Login with local credentials |
| GET | /health | Health check |
| GET | /resources | List resources |
| GET | /resources/{id} | Get resource detail |
| GET | /resources/{id}/relations | List relations for a resource |
| GET | /resources/{id}/audit-events | List audit events for a resource |
| GET | /audit-events | List audit events |
| GET | /environments | List environments |
| GET | /owners | List owners |
| GET | /roles | List roles |
