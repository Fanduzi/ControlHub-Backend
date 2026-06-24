# Phase 37G Dev Ready Query Target Fixture — Evidence

Status: **Implemented and verified locally. Not pushed.** Branch
`phase-37g-dev-ready-query-target-fixture` (worktree
`.worktrees/backend-phase-37g-dev-ready-query-target-fixture`), based on `main`
(`ef404e5` docs commit). No tag/release/deploy.

> **Updated in Phase 37H:** the query target host:port now comes from the
> credential DSN (`CONTROLHUB_QUERY_CREDENTIAL_<REF>`), not from `DATABASE_DSN`.
> The DSN-specific lines below have been corrected to that semantics; the command
> contract and env table describe the current behavior. See the
> [Phase 37H evidence](2026-06-24-phase-37h-dedicated-query-e2e-mysql-fixture-evidence.md).

## Command contract

`make seed-query-dev-target` = `QUERY_DEV_ALLOW_TARGET_FIXTURE=true go run ./cmd/querydev`.
It ensures a local `database_instance` target + profile (host/port from the credential DSN
`CONTROLHUB_QUERY_CREDENTIAL_<REF>`), then runs the existing credential seed in one idempotent pass.

Required env (DSN values never stored/printed):

| Var | Purpose |
|---|---|
| `QUERY_DEV_ALLOW_TARGET_FIXTURE=true` | Gate that allows target creation (default off → bind-only behavior unchanged). |
| `DATABASE_DSN` | ControlHub metadata DB DSN only; never parsed for the query target host:port. |
| `QUERY_DEV_CREDENTIAL_REF` | Opaque ref stored as metadata (e.g. `LOCAL_QUERY_RO`). |
| `CONTROLHUB_QUERY_CREDENTIAL_<REF>` | Read-only DSN resolved by the env resolver; its host:port becomes the query target profile. |
| `QUERY_DEV_ENVIRONMENT_POLICY` | Default `non_prod_only`. |
| Optional | `QUERY_DEV_TARGET_ENV_SLUG` (default `dev`), `QUERY_DEV_TARGET_OWNER_EMAIL` (default `dba@example.com`), `QUERY_DEV_TARGET_NAME` (default `local-mysql-query-dev`), `QUERY_DEV_TARGET_DISPLAY_NAME`. |

**Key operational finding:** the credential env `CONTROLHUB_QUERY_CREDENTIAL_<REF>` must
be present in **both** (a) the seed tool's env (so the seed can validate binding) and
(b) **the server process's env** (so the execute path can resolve the DSN at run time).
If the server lacks it, `POST /query-targets/{id}/execute` returns 403 `query_not_allowed`
("credential could not be resolved") even though `/query-targets` reports `readiness=ready`.
The target is "ready" (metadata seeded) but not executable without the server-side env.

Local DSN handling note: shell-sourcing the DSN (`. .env`) breaks on the DSN's `&`
(e.g. `?parseTime=true&charset=utf8mb4`). Extract values instead, e.g.
`export DATABASE_DSN="$(grep '^DATABASE_DSN=' .env | sed 's/^DATABASE_DSN=//')"`, or copy
`.env` next to the binary so `godotenv` loads it. Never echo the value.

## Backend verification matrix (worktree branch)

| Gate | Result |
|---|---|
| `git diff --check` | clean |
| `go test -count=1 ./...` (unit) | PASS — all packages `ok` |
| `go vet ./...` | clean |
| `go build ./...` | clean |
| `make openapi-validate` | PASS |
| `make test-integration` | PASS (full suite incl. 8 new `TestQueryDevTargetFixture_*`) |
| `make test-openapi-fuzz` | PASS (2 advisory warnings: query-endpoint 401s without auth = expected; pre-existing resources schema note) |

GitNexus (index stale at `85f66c2` — new B1 symbols not indexed; reported with caveat):
pre-edit `impact` on `printReport`/`loadSeedConfig`/`main` = **LOW** (package-main, dev
tool). `detect_changes` vs `main`: 6 `cmd/querydev/main.go` symbols "touched" (file-level;
`deriveReadiness`/`parseUint64Env`/`parseBoolEnv` byte-unchanged), risk=high *by heuristic*
— treated as a false alarm: `cmd/querydev` is a dev-only tool, not the production server;
no production code touched. Authoritative evidence = the green test matrix.

## Commits (backend branch, no AI co-author)

- `ef404e5` docs: add phase 37g dev query target fixture plan (spec + plan; on `main`)
- `98bae2d` feat: add dev query target fixture service (B1)
- `4d98f35` feat: add explicit dev target fixture mode to querydev (B2)
- `0b2cf35` chore: add seed-query-dev-target make wrapper (B3)
- `f969d93` test: cover dev query target fixture integration (B4)

Changed files: `internal/service/query_dev_target_fixture.go` (+ test),
`cmd/querydev/main.go` (+ `main_test.go`), `Makefile`,
`internal/integration/query_dev_target_fixture_test.go`. No migration, no new repository
method, no credential-binding change, no `cmd/server` change, no OpenAPI change.

## Local ready-target verification (B5)

- Built the server binary from the worktree; started it on `:8080` with `DATABASE_DSN`,
  `JWT_SECRET`, and `CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO` extracted from the main
  repo `.env` (grep+sed; never printed).
- `make seed-query-dev-target` → created resource **616** `local-mysql-query-dev`
  (dev env, `source='dev-fixture'`, engine mysql). Seed report (safe metadata only):
  readiness **`ready`**, run **`true`**, environment policy `non_prod_only`,
  `stored dsn: none`. Leak check on full output: clean (no `tcp(`/`@`/`://`).
- `/query-targets`: **1 ready of 34** (the fixture target; 33 pre-existing all
  `credential_required`). Ready target: `safetyState=readonly_sandbox_enabled`,
  `executionEnabled=true`, `availableActions.run=true`.
- The DSN value was never printed at any step.

## Frontend query E2E (F1) — frontend repo untouched

Frontend: `main`, ahead 5, HEAD `4f38d34`, clean. Gates all green: `check:e2e-preflight`,
`check:e2e-governance`, `tsc --noEmit`, `lint`, `test` (vitest 632/632), `build`.

`npm run test:e2e -- --grep query` against the running backend + seeded ready target:

- **10 passed, 0 skipped, 0 failed.**
- The 3 previously-skipped ready-target tests now **pass for real**:
  - `a ready target runs a guarded SELECT and shows the result` ✓
  - `an unsafe statement is rejected with a controlled validation message` ✓
  - `query history shows the recent attempt after a run` ✓
- Locked-target, target-switching, governance, and the 3 list-pagination "query params"
  tests pass. **Zero** `no ready query target seeded` skips.

No frontend product code was changed. The single failure observed on the first run (403
`query_not_allowed` on execute) was a local server-env gap (server lacked
`CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO`), fixed by restarting the server with the
credential env — not a frontend or fixture bug.

## CI follow-up (deferred)

The fixture command derives host/port from the credential DSN (`CONTROLHUB_QUERY_CREDENTIAL_<REF>`),
so it is CI-portable by design.
Cross-repo frontend-CI wiring (CI starts backend → runs the fixture → runs the ready-target
E2E in CI) is an explicit follow-up, **out of scope for this phase**.

## No credential leak confirmation

- DSN/password is never stored: B4 `TestQueryDevTargetFixture_NoDSNStored` asserts both the
  `query_target_credentials` and `resource_profiles_database_instance` rows carry no
  DSN-looking value; `assertCredentialRowStoresNoDSN` (existing) covers the credential row.
- DSN is never printed: the seed report prints only safe metadata; B2
  `TestPrintReport_NoDSN` and the B5 leak check confirm no `tcp(`/`@`/`://`/secret in output.
- History is metadata-only: B4 `assertHistoryRowHasNoDSN`.
- Server env was injected via grep+sed extraction (no echo); no DSN appears in logs or reports.

## Scope confirmation

No push · no tag/release/deploy · no credential leak · no credential UI · no production
auto-enable (fixture is behind `QUERY_DEV_ALLOW_TARGET_FIXTURE`, default off; not in any
release gate) · no migration · no new repository method · no credential-binding change
(`QueryDevCredentialSeeder`/`validateDSNBinding` untouched) · no frontend product code
edits · no AI co-author.

Boundary: the fixture refuses to reuse or overwrite a same-name non-fixture resource. It
only reuses `source='dev-fixture'` resources; a same-name `manual`/other resource fails
closed with `errFixtureExistingResourceNotFixture` (no profile upsert, no create) — on both
the initial lookup and the post-conflict re-list. This keeps the fixture inside its dev
boundary and matches the rollback filter (`source='dev-fixture'`). Covered by unit tests
`TestEnsureLocalQueryTarget_ExistingNonFixtureSameNameRejectsWithoutProfileUpsert` and
`TestEnsureLocalQueryTarget_CreateConflictThenRefetchNonFixtureRejects`.
