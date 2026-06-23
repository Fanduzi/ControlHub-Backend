# Phase 37G Dev Ready Query Target Fixture — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an explicit, dev-only, idempotent `QUERY_DEV_ALLOW_TARGET_FIXTURE=true` mode to `cmd/querydev` that ensures a local `database_instance` MySQL target whose profile host/port matches `DATABASE_DSN`, then runs the existing credential seed so the target becomes `ready` — enabling the 3 frontend ready-target query E2E tests to pass locally.

**Architecture:** One new pure-orchestration service (`QueryDevTargetFixture`) ensures a resource + profile using only **existing** repository methods (no new repo method, no migration). `cmd/querydev` gains an explicit env-gated mode that calls it, then runs the **unchanged** credential seeder. A `make seed-query-dev-target` wrapper sets the flag. The credential-binding path, the read-only guard, and the release gates are untouched.

**Tech Stack:** Go 1.26, `database/sql`, `github.com/go-sql-driver/mysql` (DSN parse), Testcontainers MySQL (integration), goose migrations (unchanged), chi router (unchanged).

**Design spec:** [`docs/superpowers/specs/2026-06-23-phase-37g-dev-ready-query-target-fixture-design.md`](../specs/2026-06-23-phase-37g-dev-ready-query-target-fixture-design.md). Read it first — this plan does not repeat its rationale.

---

## 1. Scope / Non-goals

### Scope
- Dev/test-only ready query target fixture (one local `database_instance` MySQL target + profile).
- Explicit `QUERY_DEV_ALLOW_TARGET_FIXTURE=true` mode in `cmd/querydev` (ensure target → existing credential seed in one idempotent pass).
- `make seed-query-dev-target` wrapper.
- Backend unit + integration tests.
- Local frontend query E2E verification (no frontend code expected).
- Docs evidence note.

### Non-goals
- No formal credential UI; no export / saved query / approval / AI assistance.
- No new query engine (MySQL/TiDB SELECT only, unchanged).
- No frontend workflow/CI changes this phase (CI follow-up documented only).
- No production auto-enable; no tag/release/deploy; no push by default.
- **No new repository method.** The fixture reuses `DictionaryRepository.ListEnvironments/ListOwners`, `ResourceRepository.ListResources/CreateResource/UpsertDatabaseInstanceProfile`.
- **No new migration.** Reuses `resources`, `resource_profiles_database_instance`, `query_target_credentials`.
- **No credential-binding change.** `QueryDevCredentialSeeder` and `validateDSNBinding` are untouched.
- **No DSN/password storage or printing** anywhere.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/service/query_dev_target_fixture.go` | Create | `QueryDevTargetFixture.EnsureLocalQueryTarget` + config + narrow store interfaces + `ParseControlHubDSNHostPort` + sentinel errors |
| `internal/service/query_dev_target_fixture_test.go` | Create | Unit tests for config validation, DSN parser, ensure-or-create orchestration (fakes) |
| `cmd/querydev/main.go` | Modify | Add fixture mode: `loadFixtureConfig`, gate, wire fixture → existing seed; safe output |
| `cmd/querydev/main_test.go` | Create (if absent) | Unit tests for `loadFixtureConfig` flag detection/defaults + safe-output (no DSN) |
| `Makefile` | Modify | Add `seed-query-dev-target` target + `.PHONY` |
| `internal/integration/query_dev_target_fixture_test.go` | Create | Testcontainers end-to-end: ensure → ready → select 1 → unsafe reject → history → no-DSN → idempotent → host:port match |

All other files (resource repo, query execution repo, seeder, guard, migrations, OpenAPI, frontend) are **read-only** for this plan.

---

## Task Blocks

### B1 — `QueryDevTargetFixture` service + unit tests

**Files:**
- Create: `internal/service/query_dev_target_fixture.go`
- Create: `internal/service/query_dev_target_fixture_test.go`

**Design (contract — implementation bodies written during TDD execution):**

```go
// Narrow interfaces defined where used; satisfied by existing concrete repos.
type DevTargetDictionary interface {
	ListEnvironments() ([]model.Environment, error)
	ListOwners() ([]model.Owner, error)
}
type DevTargetResourceStore interface {
	ListResources(ctx context.Context, q model.ResourceListQuery) ([]model.Resource, int, error)
	CreateResource(ctx context.Context, input model.ResourceCreateInput) (*model.Resource, error)
	UpsertDatabaseInstanceProfile(ctx context.Context, resourceID uint64, engine, version, host string, port int, role string) error
}

type QueryDevTargetFixtureConfig struct {
	EnvironmentSlug string // default "dev"
	OwnerEmail      string // default "dba@example.com"
	ResourceName    string // default "local-mysql-query-dev"
	DisplayName     string // default "Local MySQL Query Dev"
	Engine          string // default "mysql"
	Version         string // default "8.0"
	Role            string // default "primary"
	Host            string // parsed from DATABASE_DSN (never the DSN itself)
	Port            int    // parsed from DATABASE_DSN
}

type QueryDevTargetFixture struct {
	dictionary DevTargetDictionary
	resources  DevTargetResourceStore
}

func NewQueryDevTargetFixture(dictionary DevTargetDictionary, resources DevTargetResourceStore) *QueryDevTargetFixture

// EnsureLocalQueryTarget resolves env+owner, finds-or-creates the resource, upserts
// its profile, and returns the resource id. Host/port arrive pre-parsed in cfg; the
// DSN never enters this function. Failures happen before any write.
func (f *QueryDevTargetFixture) EnsureLocalQueryTarget(ctx context.Context, cfg QueryDevTargetFixtureConfig) (uint64, error)

// ParseControlHubDSNHostPort parses a go-sql-driver DSN and returns host+port only.
// It never returns or logs the full DSN.
func ParseControlHubDSNHostPort(dsn string) (host string, port int, err error)
```

**EnsureLocalQueryTarget logic (order matters — fail closed before writes):**
1. `cfg.validate()` (slug/email/name non-empty; host non-empty; port > 0; `isExecutableEngine(cfg.Engine)`).
2. Resolve `envID` from `dictionary.ListEnvironments()` where `Slug == cfg.EnvironmentSlug` → else `errFixtureEnvSlugNotFound`.
3. Resolve `ownerID` from `dictionary.ListOwners()` where `Email == cfg.OwnerEmail` → else `errFixtureOwnerEmailNotFound`.
4. Find existing: `resources.ListResources(ctx, ResourceListQuery{ResourceTypes:["database_instance"], EnvironmentIDs:[envID], Query:cfg.ResourceName, Page:1, PageSize:200, IncludeArchived:false})`; pick the item whose `Name == cfg.ResourceName`. If found → `resourceID = item.ID`.
5. Else `resources.CreateResource(ctx, ResourceCreateInput{ResourceType: ResourceTypeDatabaseInstance, ResourceSubtype: cfg.Engine, Name: cfg.ResourceName, DisplayName: cfg.DisplayName, EnvironmentID: envID, OwnerID: ownerID, LifecycleStatus: LifecycleStatusRunning, HealthStatus: HealthStatusHealthy, Source: "dev-fixture", Labels: map[string]string{}})`. On `service.ErrResourceConflict` → re-run step 4 and reuse (race-safe idempotency). On other error → wrap.
6. `resources.UpsertDatabaseInstanceProfile(ctx, resourceID, cfg.Engine, cfg.Version, cfg.Host, cfg.Port, cfg.Role)`.
7. Return `resourceID`.

**Sentinel errors** (fixed strings, never carry DSN/host beyond what's safe): `errFixtureMissingEnvSlug`, `errFixtureMissingOwnerEmail`, `errFixtureMissingResourceName`, `errFixtureInvalidHostPort`, `errFixtureUnsupportedEngine`, `errFixtureEnvSlugNotFound`, `errFixtureOwnerEmailNotFound`, `errFixtureEnsureFailed`.

**RED tests (write first, must fail to compile/run):**
- [ ] `TestQueryDevTargetFixtureConfig_Validate` — table: missing slug / missing email / missing name / empty host / port<=0 / non-executable engine (`postgres`) → respective sentinel; valid config → nil.
- [ ] `TestParseControlHubDSNHostPort_Valid` — `user:pass@tcp(127.0.0.1:3306)/db` → `("127.0.0.1", 3306, nil)`.
- [ ] `TestParseControlHubDSNHostPort_Rejects` — table: empty string, missing `tcp(...)`, non-numeric port, unparseable → non-nil error; assert error string contains **no** `tcp(`/`@`/password fragment.
- [ ] `TestEnsureLocalQueryTarget_ReusesExistingTarget` — fake store pre-seeds a resource with the exact name+env+type; assert `CreateResource` is **not** called and returned id == existing.
- [ ] `TestEnsureLocalQueryTarget_CreatesWhenMissing` — fake store has no match; assert `CreateResource` called once with `Source:"dev-fixture"` + `ResourceSubtype`==engine, profile upserted with cfg host/port.
- [ ] `TestEnsureLocalQueryTarget_CreateConflictThenRefetch` — fake `CreateResource` returns `service.ErrResourceConflict` on first call and a match appears on re-list; assert id reused, no duplicate create.
- [ ] `TestEnsureLocalQueryTarget_EnvironmentSlugNotFound_Rejects` → `errFixtureEnvSlugNotFound`; assert **no** `CreateResource`/`UpsertDatabaseInstanceProfile` called.
- [ ] `TestEnsureLocalQueryTarget_OwnerEmailNotFound_Rejects` → `errFixtureOwnerEmailNotFound`; assert no writes.
- [ ] `TestEnsureLocalQueryTarget_ProfileUpsertUsesHostPortOnly` — capture the args passed to `UpsertDatabaseInstanceProfile`; assert they are exactly `(id, engine, version, host, port, role)` and contain no DSN markers.
- [ ] `TestEnsureLocalQueryTarget_NoDSNInResultOrError` — feed a config whose host is `127.0.0.1`; on success and on each forced error, assert no returned value / error string contains `tcp(`, `@`, `://`, or the literal password.

**GREEN criteria:**
- All B1 unit tests pass (`go test ./internal/service -run 'QueryDevTargetFixture|ParseControlHubDSNHostPort'`).
- No repository contract widened (only the two narrow interfaces above; concrete repos unchanged).
- `go vet ./internal/service` clean.

**Commit:**
- [ ] `git add internal/service/query_dev_target_fixture.go internal/service/query_dev_target_fixture_test.go && git commit -m "feat: add dev query target fixture service"` (no AI co-author).

---

### B2 — `cmd/querydev` explicit fixture mode + env parsing

**Files:**
- Modify: `cmd/querydev/main.go`
- Create: `cmd/querydev/main_test.go`

**Env contract (documented in code comments + the help line):**

| Var | Required in fixture mode | Meaning |
|---|---|---|
| `QUERY_DEV_ALLOW_TARGET_FIXTURE` | yes (`true`) | Gate that allows target creation. Without it, querydev is byte-for-byte unchanged. |
| `DATABASE_DSN` | yes | Own DB DSN; parsed for host:port only, never printed. |
| `QUERY_DEV_CREDENTIAL_REF` | yes | Existing — opaque ref stored as metadata. |
| `CONTROLHUB_QUERY_CREDENTIAL_<REF>` | yes | Existing — readonly DSN (same MySQL as `DATABASE_DSN`); resolved only by the resolver. |
| `QUERY_DEV_ENVIRONMENT_POLICY` | no (default `non_prod_only`) | Existing. |
| `QUERY_DEV_TARGET_RESOURCE_ID` | **derived** (see decision) | See decision below. |
| `QUERY_DEV_TARGET_ENV_SLUG` | no (default `dev`) | Environment slug for the fixture target. |
| `QUERY_DEV_TARGET_OWNER_EMAIL` | no (default `dba@example.com`) | Owner email for the fixture target. |
| `QUERY_DEV_TARGET_NAME` | no (default `local-mysql-query-dev`) | Resource name (idempotency key with env). |
| `QUERY_DEV_TARGET_DISPLAY_NAME` | no (default `Local MySQL Query Dev`) | Resource display name. |

**Decision — `QUERY_DEV_TARGET_RESOURCE_ID` in fixture mode:** **derived, not read.** In fixture mode the target id comes from `EnsureLocalQueryTarget`. If `QUERY_DEV_TARGET_RESOURCE_ID` is **also** set, the command errors with a safe, fixed message (e.g. `QUERY_DEV_TARGET_RESOURCE_ID must be unset in fixture mode`) and exits non-zero — fail closed, no silent divergence. (Outside fixture mode, the existing requirement stands unchanged.)

**Behavior:**
- **Flag absent:** existing `loadSeedConfig` + `seeder.Seed` + `deriveReadiness` + `printReport` path, unchanged. Fixture code is not invoked.
- **Flag present:** `ParseControlHubDSNHostPort(DATABASE_DSN)` → build `QueryDevTargetFixtureConfig` (defaults + overrides) → `EnsureLocalQueryTarget` → `resourceID`. Reject if `QUERY_DEV_TARGET_RESOURCE_ID` set. Build `QueryDevCredentialSeedConfig{TargetResourceID: resourceID, ...}` → run the **existing** `seeder.Seed` → `deriveReadiness` → `printReport` (unchanged safe report).

**New testable helpers (so B2 is unit-testable without subprocess):**
- `loadFixtureConfig() (QueryDevTargetFixtureConfig, bool, error)` — returns `(cfg, allowFixture, err)`; `allowFixture` is false when the flag is absent/unset/false.
- `printReport` reuse is safe (already prints no DSN); add a `fixtureModeReport(...)` only if a distinct line is needed — otherwise reuse `printReport`.

**RED tests (write first):**
- [ ] `TestLoadFixtureConfig_FlagAbsent` — env without `QUERY_DEV_ALLOW_TARGET_FIXTURE` → `allowFixture == false`, no error.
- [ ] `TestLoadFixtureConfig_FlagPresent_AppliesDefaults` — only the flag set → cfg has `EnvironmentSlug=="dev"`, `OwnerEmail=="dba@example.com"`, `ResourceName=="local-mysql-query-dev"`, `Engine=="mysql"`.
- [ ] `TestLoadFixtureConfig_OverridesApplied` — custom slug/email/name/display via env → reflected in cfg; host/port left zero (filled by the DSN parser in `main`).
- [ ] `TestLoadFixtureConfig_BadFlagValue` — `QUERY_DEV_ALLOW_TARGET_FIXTURE=notabool` → safe error, `allowFixture == false` (do not proceed).
- [ ] `TestPrintReport_NoDSN` — call the report helper with a populated `QueryCredentialMetadata` + readiness; assert output contains **no** `tcp(`, `@`, `://`, password, or the literal DSN; assert it does contain resource id, ref, engine, policy, readiness, run flag.
- [ ] `TestFixtureMode_RejectsExplicitResourceID` — fixture mode + `QUERY_DEV_TARGET_RESOURCE_ID` set → command path returns a safe fixed error (no DSN), non-zero.

  *(End-to-end flag-gated behavior — flag present calls fixture then seeder — is covered by B4 integration.)*

**GREEN criteria:**
- B2 unit tests pass (`go test ./cmd/querydev`).
- `go build ./cmd/querydev` succeeds.
- With flag absent, `go run ./cmd/querydev` behaves exactly as before (regression covered by existing integration tests in B4 + the unchanged seeder path).

**Commit:**
- [ ] `git add cmd/querydev/main.go cmd/querydev/main_test.go && git commit -m "feat: add explicit dev target fixture mode to querydev"`.

---

### B3 — Makefile wrapper

**Files:**
- Modify: `Makefile`

**Change:**
- Add `seed-query-dev-target` to the `.PHONY` list (line 1).
- Add target:
```make
seed-query-dev-target: ## Dev-only: ensure a LOCAL database_instance query target (host/port from DATABASE_DSN) then seed its credential metadata in one idempotent pass. Requires QUERY_DEV_ALLOW_TARGET_FIXTURE implied, DATABASE_DSN, QUERY_DEV_CREDENTIAL_REF, CONTROLHUB_QUERY_CREDENTIAL_<REF>. DSN is never stored/printed.
	QUERY_DEV_ALLOW_TARGET_FIXTURE=true go run ./cmd/querydev
```

**RED check (manual/grep, no Go):**
- [ ] Before edit: `grep -c "seed-query-dev-target" Makefile` == 0.
- [ ] After edit: `make -n seed-query-dev-target` prints the `QUERY_DEV_ALLOW_TARGET_FIXTURE=true go run ./cmd/querydev` command.

**GREEN criteria:**
- [ ] `make seed-query-dev-target` exists and runs the binary with the flag set.
- [ ] `grep -E "release-local-gates|release-docker-gates|release-readiness-gates" Makefile` shows `seed-query-dev-target` is **not** a prerequisite or dependency of any release gate (dev-only, not auto-run).

**Commit:**
- [ ] `git add Makefile && git commit -m "chore: add seed-query-dev-target make wrapper"`.

---

### B4 — Integration tests (Testcontainers)

**Files:**
- Create: `internal/integration/query_dev_target_fixture_test.go` (build tag `//go:build integration`)

**Reuse:** `setupTestDB`, `globalEnv` (own DSN), `mustExec`, `newDevSeeder`, `newReadinessService`, `newExecutionService`, `findTargetByID`, `assertCredentialRowStoresNoDSN` from the existing `internal/integration` helpers. Do **not** duplicate them.

**Fixture wiring helper (new, in this file):**
- `ensureLocalTarget(t, db) (resourceID uint64)` — mirrors `cmd/querydev` wiring: `ParseControlHubDSNHostPort(globalEnv.dsn)` → `NewQueryDevTargetFixture(NewDictionaryRepository(db), NewResourceRepository(db))` → `EnsureLocalQueryTarget(ctx, cfg)` with defaults. Sets `CONTROLHUB_QUERY_CREDENTIAL_<REF>` to `globalEnv.dsn` (same disposable MySQL) so binding succeeds.

**Tests (write first, `//go:build integration`):**
- [ ] `TestQueryDevTargetFixture_EnsuresLocalTargetAndBecomesReady` — ensure target → seed via `newDevSeeder` → `newReadinessService(db).List` → assert target `Readiness == model.ReadinessReady`, `AvailableActions.Run == true`, `Governance.SafetyState == model.SafetyStateReadonlySandboxEnabled`, `Governance.ExecutionEnabled == true`.
- [ ] `TestQueryDevTargetFixture_Idempotent_NoDuplicateRows` — `ensureLocalTarget` twice; assert exactly **1** `database_instance` row named `local-mysql-query-dev` in the `dev` env, exactly **1** `resource_profiles_database_instance` row for it, and after seeding exactly **1** `query_target_credentials` row (reuse `assertCredentialRowStoresNoDSN` count check).
- [ ] `TestQueryDevTargetFixture_SelectOneExecutes` — ensure+seed; `newExecutionService(db).Execute(ctx, ownerDBA, id, {Statement:"select 1", MaxRows:10})` → `Status == Success`, `RowCount == 1`.
- [ ] `TestQueryDevTargetFixture_UnsafeSQLRejectedAndRecorded` — `Execute(... {Statement:"delete from qe_sandbox_fixtures"})` → error / non-success; assert a history row exists for the attempt with a rejection status.
- [ ] `TestQueryDevTargetFixture_HistoryRecordsSuccessAndRejection` — after a success and a rejection, `QueryExecutionRepository.ListExecutions` returns both (metadata only; assert no result rows / no DSN in preview/digest).
- [ ] `TestQueryDevTargetFixture_NoDSNStored` — extend the no-DSN assertion to also scan `resource_profiles_database_instance` columns (host/port/engine/version/role/spec) for the credential DSN / `tcp(`/`@` markers; assert the credential row (via `assertCredentialRowStoresNoDSN`) is clean.
- [ ] `TestQueryDevTargetFixture_ProfileHostPortMatchesDSN` — read the profile row; assert `host:port == ParseControlHubDSNHostPort(globalEnv.dsn)`.
- [ ] `TestQueryDevTargetFixture_FailClosed_OnBadBindingStaysLocked` — ensure target, then seed with a credential DSN whose host:port differs (set env to a mismatched DSN) → `seeder.Seed` returns an error; target stays `Readiness != Ready`, `Run == false`; `Execute` rejected. (Exercises the **unchanged** binding check.)

**GREEN criteria:**
- [ ] `make test-integration` passes (all B4 + existing tests).
- [ ] No DSN/password in any stored row or assertion output.

**Commit:**
- [ ] `git add internal/integration/query_dev_target_fixture_test.go && git commit -m "test: cover dev query target fixture integration"`.

---

### B5 — Local backend seed verification (manual; no code)

**No file changes.** Exact local commands (run after B1–B4 land):

1. Free port + start backend:
```bash
lsof -nP -iTCP:8080 -sTCP:LISTEN || true
mkdir -p /tmp/controlhub-local
go build -o /tmp/controlhub-local/ch-server ./cmd/server
nohup /tmp/controlhub-local/ch-server > /tmp/controlhub-local/backend.log 2>&1 &
echo $! > /tmp/controlhub-local/backend.pid
```
2. Health:
```bash
curl -fsS http://localhost:8080/health        # {"status":"ok"}
```
3. Seed the ready target. The credential DSN is supplied **via env only — never printed**. For local verification, reuse the `.env` `DATABASE_DSN` as the credential DSN (same host:port, so it binds; the Phase 37 read-only sandbox enforces SELECT-only regardless of the DB user's privileges):
```bash
# Load .env into the shell env WITHOUT printing anything (set -a exports; nothing is echoed).
set -a; . ./.env; set +a
QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO \
CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO="$DATABASE_DSN" \
make seed-query-dev-target
```
   - Required local-verification env: `QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO` and `CONTROLHUB_QUERY_CREDENTIAL_LOCAL_QUERY_RO` = a DSN with the **same host:port as `DATABASE_DSN`**.
   - This prints **only** safe metadata (resource id/name, engine, policy, readiness, run flag). The credential DSN value is never echoed. The report may only state "credential DSN supplied via env and matched `DATABASE_DSN` host:port" — never the value.
4. Verify ready target (read-only field check; do **not** echo full records containing DSNs):
```bash
curl -fsS http://localhost:8080/query-targets | jq '[.items[] | select(.readiness=="ready") | {id:.resourceId,name:.resourceName,readiness,run:.availableActions.run,safetyState:.governance.safetyState}]'
# Expect >=1 item with readiness=="ready" and run==true and safetyState=="readonly_sandbox_enabled"
```
5. Stop backend + confirm freed:
```bash
kill "$(cat /tmp/controlhub-local/backend.pid)"
for i in $(seq 1 10); do lsof -nP -iTCP:8080 -sTCP:LISTEN >/dev/null 2>&1 || break; sleep 1; done
lsof -nP -iTCP:8080 -sTCP:LISTEN || echo ":8080 freed"
```

**GREEN criteria:** `/health` ok; ≥1 ready target with `run=true`; backend stopped; `:8080` freed; no DSN/password printed at any step.

---

### F1 — Frontend query E2E verification (frontend repo; no frontend code expected)

**Repo:** `/Users/fan/JsProjects/ControlHub`. Run **with the local backend running and the ready target seeded** (B5 steps 1–4, before stop).

- [ ] `npm run check:e2e-preflight`
- [ ] `npm run check:e2e-governance`
- [ ] `npx tsc --noEmit`
- [ ] `npm run lint`
- [ ] `npm run test`
- [ ] `npm run build`
- [ ] `npm run test:e2e -- --grep query`

**Success criteria:**
- [ ] ready-target SELECT test **passes**
- [ ] unsafe-reject test **passes**
- [ ] history test **passes**
- [ ] **zero** `no ready query target seeded` skips
- [ ] locked-target tests still pass
- [ ] 0 failures overall

**If E2E still skips or fails:** stop, report the real cause, do **not** push frontend, do **not** edit frontend product code without first reporting.

---

### D1 — Docs evidence (only after implementation succeeds)

**File:** `docs/superpowers/notes/2026-06-23-phase-37g-dev-ready-query-target-fixture-evidence.md`

**Contents:**
- Command contract (env table from B2).
- Backend verification matrix (Section 3 results).
- Local ready-target verification results (B5).
- Frontend query E2E results (F1): passed/skipped/failed, explicit ready-target pass confirmation.
- CI follow-up status: deferred; command is CI-portable (host/port from `DATABASE_DSN`); cross-repo frontend-CI hookup is a documented follow-up, not implemented this phase.
- No-credential-leak confirmation (stored rows + command output audited).

**Commit:**
- [ ] `git add docs/superpowers/notes/2026-06-23-phase-37g-dev-ready-query-target-fixture-evidence.md && git commit -m "docs: record phase 37g dev query target fixture evidence"`.

---

## 3. Testing / Verification Matrix

Run after B1–B4 (and B5/F1 for local/E2E):

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Frontend (F1): `check:e2e-preflight`, `check:e2e-governance`, `tsc --noEmit`, `lint`, `test`, `build`, `test:e2e -- --grep query`.

All backend gates must pass; the dev-only fixture must not perturb `release-local-gates` / `release-docker-gates` / fuzz.

---

## 4. Safety Guards — how tests prove each invariant

| Invariant | Proven by |
|---|---|
| No DSN stored | B1 `…_NoDSNInResultOrError`; B4 `…_NoDSNStored` (scans `query_target_credentials` + `resource_profiles_database_instance`); existing `assertCredentialRowStoresNoDSN`. |
| No DSN printed | B2 `TestPrintReport_NoDSN`; B1 `ParseControlHubDSNHostPort` error has no DSN markers; B5 manual review of command output. |
| No credential-binding change | `QueryDevCredentialSeeder` / `validateDSNBinding` are not edited; B4 `…_FailClosed_OnBadBindingStaysLocked` re-proves binding still rejects mismatches; existing `TestQueryDevSeed_*` unchanged. |
| No production auto-enable | B2 `TestLoadFixtureConfig_FlagAbsent` (no fixture path without flag); B3 grep proves the target is not in any release gate; B4 `…_FailClosed_…` for config/binding. |
| No new repository method | B1 uses only existing methods; grep `git diff` shows no new `func (r *ResourceRepository…)` / `*DictionaryRepository`. |
| No migration | `migrations/` untouched; `git status migrations/` clean. |
| Idempotency | B1 `…_ReusesExistingTarget` + `…_CreateConflictThenRefetch`; B4 `…_Idempotent_NoDuplicateRows`. |
| Fail closed (missing flag / invalid DSN / missing env / missing owner / host:port mismatch) | B1 config + parser tests; B2 flag tests; B4 binding/config tests. |

---

## 5. Rollback / Cleanup

- **Stop backend:** `kill "$(cat /tmp/controlhub-local/backend.pid)"`; confirm `:8080` freed (B5 step 5).
- **Remove dev fixture data (local/dev DB only — never pushed to production):** the fixture creates exactly one `database_instance` resource named `local-mysql-query-dev` in the `dev` environment with `source='dev-fixture'`, plus its profile and one credential row. Resolve the exact fixture id first — constrained by name + type + source + env slug so a same-named resource in another environment is never touched and a multi-row subquery cannot fail — then delete by id:
  ```sql
  set @fixture_resource_id := (
    select r.id
    from resources r
    join environments e on e.id = r.environment_id
    where r.name = 'local-mysql-query-dev'
      and r.resource_type = 'database_instance'
      and r.source = 'dev-fixture'
      and e.slug = 'dev'
    limit 1
  );

  delete from query_target_credentials           where resource_id = @fixture_resource_id;
  delete from resource_profiles_database_instance where resource_id = @fixture_resource_id;
  delete from resources                           where id          = @fixture_resource_id;
  ```
  - If `@fixture_resource_id` is `NULL` (no fixture present), the three deletes are no-ops. Run the inner `select` alone first to confirm the id before deleting, if desired.
  - (Or `make migrate-reset-dev` for a full destructive local reset.) This data lives only in the local/dev `controlhub` DB; it is not part of any production migration or seed.
- **Worktree/branch:** work on `main` (per repo convention) or a short-lived feature branch; after merge, delete the branch. No long-lived branch.
- **No push by default:** commits stay local until explicit authorization; no tag/release/deploy.

---

## 6. Commit / Final Report Requirements

- **Commit messages:** conventional (`feat:`/`test:`/`chore:`/`docs:`), **no AI co-author** (attribution disabled globally; do not add Co-Authored-By trailers).
- **Frequent commits** per block (B1, B2, B3, B4, D1) as shown.
- **For this plan-only task:** if committing, commit only plan/spec docs; **no product code, no push.**
- **After implementation approval:** commit code per the block commits above; do not push unless explicitly authorized.
- **Final report must include:** plan doc path; commit hash(es) if committed; backend verification matrix results; local ready-target results (B5); frontend query E2E results (F1); CI follow-up status; final `git status` (backend + frontend); service stopped confirmation; scope confirmation (no tag/release/deploy, no credential leak, no credential UI, no production auto-enable, no new repo method, no migration, no AI co-author, no push by default).

---

## Self-Review (run before handing off)

- [ ] **Spec coverage:** every Success Criterion + invariant in the design spec maps to a task (B1–B4 / F1 / D1) — see Section 4 matrix.
- [ ] **Placeholder scan:** no leftover placeholders, no vague “add validation” / “handle edge cases” / “similar to Task N” steps — every step has a file path, named test, or exact command.
- [ ] **Type consistency:** `QueryDevTargetFixtureConfig`, `DevTargetDictionary`, `DevTargetResourceStore`, `EnsureLocalQueryTarget`, `ParseControlHubDSNHostPort`, `loadFixtureConfig` names are identical across B1/B2/B4.
- [ ] `isExecutableEngine`, `service.ErrResourceConflict`, `model.ResourceCreateInput`, `model.ResourceListQuery`, `model.Environment.Slug`, `model.Owner.Email` are all real (verified against the codebase).
