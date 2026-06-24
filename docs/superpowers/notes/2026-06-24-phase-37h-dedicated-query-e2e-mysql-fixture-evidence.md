# Phase 37H Dedicated Query E2E MySQL Fixture — Evidence

Status: **Implemented and verified locally. Not pushed.** Branch
`phase-37g-dev-ready-query-target-fixture` (continues the Phase 37G branch), worktree
`.worktrees/backend-phase-37g-dev-ready-query-target-fixture`. No tag/release/deploy.

## What changed vs Phase 37G

Phase 37G reused `DATABASE_DSN` as the query target credential DSN — i.e. the Query
Workbench queried the ControlHub metadata database. Phase 37H corrects that boundary:

- `DATABASE_DSN` is now **only** the ControlHub metadata database.
- The query target profile host:port is derived from the **credential DSN**
  (`CONTROLHUB_QUERY_CREDENTIAL_<REF>`), which points at a **dedicated Docker MySQL**.
- A new script + Makefile targets manage that dedicated query MySQL.

## Commits (no AI co-author)

- `856bfdb` test: add dedicated query e2e mysql fixture (B1: script + Makefile + .gitignore)
- `b9bbef0` fix: derive dev query target from credential dsn (B2: querydev uses credential DSN; rename ParseControlHubDSNHostPort → ParseMySQLDSNHostPort)

## Changed files

- `scripts/query-e2e-mysql.sh` (new) — up/down/status for the dedicated query MySQL.
- `Makefile` — `query-e2e-mysql-up/down/status` targets.
- `.gitignore` — `.query-e2e-mysql.env`.
- `cmd/querydev/main.go` (+`main_test.go`) — fixture mode derives host:port from the credential DSN via `resolveFixtureHostPort`; `DATABASE_DSN` opens the metadata DB only.
- `internal/service/query_dev_target_fixture.go` (+`_test.go`) — renamed `ParseControlHubDSNHostPort` → `ParseMySQLDSNHostPort` (neutral; parses any MySQL DSN).
- `internal/integration/query_dev_target_fixture_test.go` — renamed call sites.
- `CLAUDE.md` — GitNexus index-count block auto-refreshed by `gitnexus analyze`.

No migration, no new repository method, no credential-binding change, no `cmd/server`
change, no OpenAPI change, no frontend product code change.

## Dedicated Docker query MySQL

- Container: `controlhub-query-e2e-mysql`; host `127.0.0.1`; port `13306`; database `query_e2e`.
- Seed table `query_e2e_items` with stable rows (`alpha`, `beta`).
- Read-only user `query_e2e_ro` — `GRANT SELECT` only (verified: SELECT ok, INSERT denied `1142`).
- `make query-e2e-mysql-up` is idempotent (reuses container + stored password); `down`/`status` work.
- `.query-e2e-mysql.env` is written mode 0600, **gitignored**, and the DSN value is **never printed**
  (output is safe facts only; verified by leak check). The DSN value is double-quoted in the file
  so `set -a; . ./.query-e2e-mysql.env; set +a` sources cleanly (the DSN contains `&`).

## DATABASE_DSN is metadata-only (separation proven)

- `/query-targets` shows **1 ready target of 34** (resource 616, `local-mysql-query-dev`), with
  profile `host=127.0.0.1 port=13306` — the **dedicated query DB**, not the metadata DB.
- Unit proof: `TestResolveFixtureHostPort_UsesCredentialDSNNotDatabaseDSN` — fixture host:port
  comes from `CONTROLHUB_QUERY_CREDENTIAL_<REF>` (127.0.0.1:13306), ignoring `DATABASE_DSN`
  (a different host:port).
- End-to-end proof: an authenticated `POST /query-targets/616/execute` of
  `select name, category from query_e2e_items order by id` returned rows
  `["alpha","sample"]`, `["beta","sample"]` — data that exists **only** in the dedicated query DB
  (the metadata DB has no such table). The query therefore reached the dedicated DB, not the
  metadata DB. Phase 37 credential binding (resolved credential DSN host:port must equal the
  target profile host:port) enforces this at execute time.

## Backend verification matrix (worktree branch)

| Gate | Result |
|---|---|
| `git diff --check` | clean |
| `go test -count=1 ./...` | PASS (all packages) |
| `go vet ./...` / `go build ./...` | clean |
| `make openapi-validate` | PASS |
| `make test-integration` | PASS (74 PASS lines) |
| `make test-openapi-fuzz` | PASS |

GitNexus: refreshed the index with `npx gitnexus analyze` (5678 symbols), then
`detect_changes` vs `main`: 6 `cmd/querydev/main.go` symbols "touched" + the CLAUDE.md index
block, 10 changed files, risk=**high by heuristic**. Caveat: `cmd/querydev` is a **dev-only
tool**, not the production server; no production code / migration / repo method /
credential-binding changed; new fixture symbols are additions. No real HIGH/CRITICAL
production risk. Authoritative evidence = the green test matrix.

## Frontend query E2E (frontend repo untouched)

Frontend: `main`, ahead 5, HEAD `4f38d34`. Gates all green: `check:e2e-preflight`,
`check:e2e-governance`, `tsc --noEmit`, `lint`, `test` (vitest 632/632), `build`.

`npm run test:e2e -- --grep query` against the running backend + dedicated query DB:

- **10 passed / 0 skipped / 0 failed.**
- Ready-target tests executed with **zero skips**: SELECT shows result, unsafe statement
  rejected, query history recorded.

## Local run contract (for reproduction)

```bash
make query-e2e-mysql-up
export DATABASE_DSN="$(grep '^DATABASE_DSN=' .env | sed 's/^DATABASE_DSN=//')"   # metadata DB only
export JWT_SECRET="$(grep '^JWT_SECRET=' .env | sed 's/^JWT_SECRET=//')"
set -a; . ./.query-e2e-mysql.env; set +a                                            # dedicated query DB credential
QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO make seed-query-dev-target
# start the server with DATABASE_DSN + CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO in env
```

(The `.env` `DATABASE_DSN` contains `&`, so it cannot be shell-sourced; extract via grep+sed.
`.query-e2e-mysql.env` is quoted and sources cleanly.)

## Cleanup status

See the final report. Backend server and the dedicated query MySQL are stopped via
`make query-e2e-mysql-down` unless explicitly asked to keep them.

## Scope confirmation

No credential leak (DSN/password never stored/printed/logged; `.query-e2e-mysql.env` gitignored
+ mode 0600 + never printed; leak checks clean) · no credential UI · no production auto-enable
(fixture behind `QUERY_DEV_ALLOW_TARGET_FIXTURE`, default off; not in any release gate) · no
migration · no new repository method · no credential-binding relaxation
(`QueryDevCredentialSeeder`/`validateDSNBinding` untouched; binding still enforces
credential DSN host:port == profile host:port) · no frontend product code edits · no CI
workflow changes · no push · no tag/release/deploy · no AI co-author.
