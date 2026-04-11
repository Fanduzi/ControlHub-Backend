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
