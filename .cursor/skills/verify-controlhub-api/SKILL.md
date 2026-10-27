---
name: verify-controlhub-api
description: "Drive the ControlHub Backend Go API (HTTP on :8080) the way an operator/agent does — health, login, resources, database-typed inventory, audit-events. Use when proving API behavior, regression-checking a release, or after changing those surfaces."
---

# Verify ControlHub API

Project-local control skill for `/workspace/ControlHub-Backend`. Written for an agent reading it cold mid-task.

**Primary surface:** HTTP JSON REST API (`cmd/server`, chi router) on `http://127.0.0.1:8080`.  
**Also:** goose migrations, `cmd/bootstrap-admin`, OpenAPI at `/openapi.yaml` + `/docs`.  
**Not covered here:** Testcontainers integration/fuzz (need Docker), `make run-query-dev` query-fixture MySQL, or executing the checked-in Mach-O `./server` binary.

Metadata MySQL must listen on `127.0.0.1:3318` (DSN from `.env` — never print credentials). Env slugs are `prod` / `staging` / `dev` (not `production`).

## Launch

```bash
export PATH="/workspace/toolchains/go-1.26.2/bin:$PATH"
export RUN_ID="manual-$(date +%Y%m%d-%H%M%S)"
cd /workspace/ControlHub-Backend
eval "$(.cursor/skills/verify-controlhub-api/scripts/ensure-stack.sh)"
.cursor/skills/verify-controlhub-api/scripts/doctor.sh
```

Ready when: `doctor.sh` prints `doctor OK` and `curl -fsS http://127.0.0.1:${APP_PORT:-8080}/health` → `{"status":"ok"}`.

`ensure-stack.sh` builds a **Linux** binary into `${RUN_DIR}/bin/server` (never use checked-in `./server`), starts it with `.env`, waits for `/health`, writes `${RUN_DIR}/server.pid`. Isolation: override `APP_PORT` for a second instance; do not kill a server you did not start.

Migration note for this box: triggers need `SET GLOBAL log_bin_trust_function_creators=1` on MySQL. `CONFIRM=yes make migrate-reset-dev` uses bare `mysql -u root` (socket) — prefer TCP recreate:

```bash
mysql -h127.0.0.1 -P3318 -uroot -p… -e "SET GLOBAL log_bin_trust_function_creators=1; DROP DATABASE IF EXISTS controlhub; CREATE DATABASE controlhub CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;"
make migrate-up
BOOTSTRAP_ADMIN_EMAIL="$(cat /workspace/controlhub-qa-login.email)" \
BOOTSTRAP_ADMIN_PASSWORD="$(cat /workspace/controlhub-qa-login.pass)" \
  go run ./cmd/bootstrap-admin
```

Teardown: `.cursor/skills/verify-controlhub-api/scripts/cleanup.sh` (keeps `${RUN_DIR}/evidence`).

## Doctor

```bash
.cursor/skills/verify-controlhub-api/scripts/doctor.sh
```

Answers: Go toolchain present, `${RUN_DIR}/bin/server` is ELF (or shared instance answering `/health`), `/health` ok, optional port ownership by `${RUN_DIR}/server.pid`.

## Drive

Harness = plain `curl` + JSON evidence files. Stable paths (from OpenAPI/router):

| Concern | Method + path |
|---------|---------------|
| Health | `GET /health` |
| Login | `POST /auth/login` body `{email,password}` → `{token,role,userId}` |
| Resources | `GET /resources` (+ `GET /resources/{id}`) |
| Databases | `GET /resources?resourceType=database_instance&resourceType=database_cluster&resourceType=database_proxy` (**no** `/databases` route) |
| Audits | `GET /audit-events` (admin) |

Credentials: read from `/workspace/controlhub-qa-login.email` and `/workspace/controlhub-qa-login.pass` at drive time — **paths only**, never echo.

```bash
.cursor/skills/verify-controlhub-api/scripts/drive-login-resources.sh
# evidence: health.json, login-meta.txt (no token), resources.json
```

Feature recipes under `features/`. Read `features/README.md` first.

## Evidence

- Location: `${RUN_DIR}/evidence/` (`EVIDENCE_DIR`). Survives cleanup.
- HTTP proof: status code + body (redact `token` from saved login JSON — script writes `login-meta.txt` with role/userId only).
- Standards: real HTTP paths, not Go unit tests. Capture action + resulting JSON fields (`items`, `pageInfo`, `status`).

## Cleanup

```bash
.cursor/skills/verify-controlhub-api/scripts/cleanup.sh
```

Kills only `${RUN_DIR}/server.pid`. Never `pkill` by name. Never deletes evidence. Shared servers (`server.shared`) are left running.

## Helpers

| Script | Purpose |
|--------|---------|
| `ensure-stack.sh` | Require MySQL `:3318`; build + start server; print exports |
| `doctor.sh` | Read-only readiness |
| `drive-login-resources.sh` | Health → login → resources list evidence |
| `cleanup.sh` | Stop this run's server pid; keep evidence |

Invoke from repo root after `eval "$(.../ensure-stack.sh)"`.
