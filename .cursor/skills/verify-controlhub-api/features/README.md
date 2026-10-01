# ControlHub API verification map

Maintained source for verifying user-facing ControlHub Backend HTTP behavior. Read this index before driving, then open the matching feature file.

## Baseline preconditions

- Go toolchain: `export PATH="/workspace/toolchains/go-1.26.2/bin:$PATH"` and `GOROOT=/workspace/toolchains/go-1.26.2` (expect `go1.26.2 linux/amd64`).
- Metadata MySQL listening on `127.0.0.1:3318`; `.env` present with `APP_PORT` / `DATABASE_DSN` / `JWT_SECRET` (never print secrets).
- Migrations applied (`make migrate-up`) when schema is empty; not auto-run on server start.
- If triggers fail: `SET GLOBAL log_bin_trust_function_creators=1` on this MySQL, then re-run migrate-up.
- Do **not** run checked-in `./server` (Mach-O). Use `${RUN_DIR}/bin/server` from `ensure-stack.sh`.
- `scripts/doctor.sh` passes; evidence under `${EVIDENCE_DIR}`.
- Docker-dependent paths (`run-query-dev`, query-e2e MySQL `:13306`, integration/fuzz) are out of scope for baseline features when Docker is down.

## Driving conventions

- Start from baseline unless a recipe says otherwise.
- Drive with `curl` (helpers under `scripts/`). Treat paths as literal from router/OpenAPI.
- Credentials: `/workspace/controlhub-qa-login.email` and `/workspace/controlhub-qa-login.pass` (paths only). Never echo contents; never write JWT to evidence.
- There is **no** backend `/databases` route — use filtered `/resources?resourceType=...`.
- Do not remove proof artifacts during cleanup.

## Proof and skip reporting

- HTTP proof: status + body (redact tokens). Login meta may assert keys without storing `token`.
- Record feature ID / entry point in artifact filenames.
- Report unreachable paths with attempted command and unmet precondition (MySQL closed, wrong-arch binary, missing admin scope for audits); do not claim a skipped entry point was verified via another path.

## Feature entry contract

Each feature file: H1 + one paragraph, then exactly four H2s in order: `Sub-features`, `How to get to it (user POV)`, `Driving it with curl`, `Gotchas`.

## Features

- [Health](./health.md) — unauthenticated `GET /health`.
- [Auth login](./auth-login.md) — `POST /auth/login` → token/role/userId.
- [Resources list](./resources-list.md) — bearer `GET /resources` (+ optional detail).
- [Databases as resources](./databases-as-resources.md) — filtered `resourceType` query on `/resources`.
- [Audit events](./audit-events.md) — `GET /audit-events` (admin / audit:read).
