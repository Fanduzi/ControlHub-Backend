# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run Commands

```bash
make test          # go test ./... (unit tests only, no Docker)
make test-integration  # Testcontainers MySQL integration tests (requires Docker)
make test-openapi-fuzz # Schemathesis OpenAPI fuzzing against disposable MySQL (requires Docker + schemathesis)
make run           # go run ./cmd/server (auto-loads .env)
go vet ./...       # static analysis
go build ./...     # compile check
```

Run a single test:
```bash
go test ./internal/api -v -run TestListResources
go test ./internal/model -v -run TestLifecycleStatusValidation
```

Run a single integration test:
```bash
go test -tags=integration -count=1 -v -run TestResourceRepository ./internal/integration
```

OpenAPI contract validation:
```bash
make openapi-validate
```

Race detection (not in Makefile):
```bash
go test -race ./...
```

## Architecture

**Module:** `github.com/fan/controlhub` | Go 1.26 | MySQL 8.0+

**Layered structure — strict one-direction dependency flow:**

```
cmd/server/main.go       — bootstrap, wire dependencies
internal/api/            — HTTP handlers + chi router (depends on service)
internal/service/        — business logic with interface-based repos (depends on model)
internal/repository/mysql/ — SQL implementations of service interfaces
internal/model/          — domain structs, taxonomy constants, validation (no external deps)
```

**Key pattern:** Each service defines its own repository interface where it's used (not where it's implemented). Fakes in `internal/api/test_server.go` satisfy these interfaces for handler tests.

**Dependency wiring:** Manual constructor injection in `cmd/server/main.go` via `api.Dependencies` struct. `DictionaryRepository` is shared across multiple services.

## Database

Migrations are managed by **goose** (`github.com/pressly/goose/v3`). Migration files live in `migrations/` in goose SQL format (`-- +goose Up` / `-- +goose Down`).

```bash
make migrate-up       # apply all pending migrations
make migrate-status   # show current migration state
make migrate-down-one # roll back one migration
```

**No auto-migration on server startup.** Migrations must be run explicitly via Makefile targets or the `goose` CLI.

**No ORM** — raw `database/sql` with manual `rows.Scan()`. JSON columns (`labels`, `spec`) use `json.Unmarshal`.

**Profile tables** are per-resource-type: `resource_profiles_host`, `resource_profiles_database_instance`, `resource_profiles_database_cluster`, `resource_profiles_service`. Newer resource types (`domain_name`, `virtual_ip`, `database_proxy`, `control_plane_component`) have no profile table yet.

**Audit events** are MySQL-backed for dev; ClickHouse migration planned. No FK constraints on `audit_events` table.

### Adopting an Existing Manually-Migrated DB

If your local `controlhub` DB was created before goose was introduced, you need to baseline goose tracking:

```bash
mysql -u root controlhub -e "
CREATE TABLE IF NOT EXISTS goose_db_version (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    version_id bigint NOT NULL,
    is_applied boolean NOT NULL,
    tstamp timestamp NULL DEFAULT CURRENT_TIMESTAMP
) ENGINE=InnoDB;

INSERT INTO goose_db_version (version_id, is_applied) VALUES
(0,true),(1,true),(2,true),(3,true),(4,true),(5,true),(6,true),(7,true);
"
```

Then run `make migrate-status` to confirm goose sees all 8 migrations as applied.

### Dev Reset (Destructive)

```bash
CONFIRM=yes make migrate-reset-dev   # drops and recreates DB, then applies all migrations
```

## Taxonomy System

All enum-like values live in `internal/model/taxonomy.go` as typed constants with:
- In-memory `[]DictionaryItem` slices (key/label/description)
- `Validate()` methods on each type
- `XxxDictionary()` clone functions used by repository layer

Resource types (8), relation types (7), lifecycle statuses (5), health statuses (4) — all static, not admin-managed.

## API Design

**19 endpoints** on a chi router with localhost:3000 CORS. All list endpoints return `{ "items": [...] }` envelope. Filtering via query params (`?type=`, `?environmentId=`).

**Auth:** Custom HMAC-SHA256 token (not standard JWT). Passwords hashed with SHA-256. Seed credentials: `admin@example.com` / `secret123`.

**OpenAPI spec** at `internal/openapi/openapi.yaml` (3.1.0) — YAML-first, not annotation-generated. Served at `GET /openapi.yaml`. Interactive docs at `GET /docs` via Scalar API Reference (CDN-loaded). Validate with `make openapi-validate`. Do not use swaggo/swag or Swagger UI.

## Testing Conventions

Tests use **fake repositories** (not mocks) defined in `internal/api/test_server.go`. The `NewTestServer()` function wires all fakes into a real chi router — handlers are tested via `httptest.NewRecorder()`.

Service tests use anonymous structs implementing the repository interface with hardcoded data.

### Integration Tests

Integration tests live in `internal/integration/` behind a `//go:build integration` build tag. They use **Testcontainers** to start a disposable MySQL 8.0 container, run goose migrations, and exercise real repository code.

```bash
make test-integration   # runs go test -tags=integration -count=1 -v ./internal/integration
```

Integration tests cover behavior that fake repositories cannot catch:
- Goose clean migration (all migrations apply from zero, expected tables/indexes exist)
- Resource write conflicts (MySQL 1062 duplicate key mapping to service errors)
- Relation unique constraint enforcement
- Topology SQL neighborhood queries against real data
- Name-per-environment unique index behavior

Integration tests **do not** touch the daily local `controlhub` database.

### OpenAPI Fuzz Testing

Schemathesis-based fuzz testing lives in `internal/integration/openapi_fuzz_test.go` behind the `//go:build integration` tag. It starts a real HTTP server on a random port backed by disposable Testcontainers MySQL, then runs Schemathesis against `/openapi.yaml`.

```bash
make test-openapi-fuzz
```

- Requires **Docker** and **Schemathesis CLI** (`pip install schemathesis`).
- Uses disposable MySQL — does **not** touch the daily `controlhub` database.
- Bounded run: 50 examples per operation, seed 42, checks: no 5xx, status code conformance, content type conformance, response schema conformance.
- Write endpoints are exercised freely (the database is disposable).
- Reports saved to `.schemathesis-reports/`.

When to run:

| Command | When | Docker? | Extra deps |
|---------|------|---------|------------|
| `make test` | Every commit | No | — |
| `make test-integration` | Before merge | Yes | — |
| `make test-openapi-fuzz` | Before merge, after API changes | Yes | schemathesis |

## Configuration

Environment variables loaded via `godotenv` from `.env` (gitignored). Shell exports override `.env` values.

| Variable | Default | Purpose |
|----------|---------|---------|
| `APP_PORT` | `8080` | HTTP listen port |
| `DATABASE_DSN` | required | MySQL connection string |
| `JWT_SECRET` | required | Token signing key |

## Demo Data

Migration `0004` seeds ~64 resources across 5 business domains (Order, Payment, User, Analytics, Config) in 3 environments. Includes status variety (warning, critical, degraded, stopped, provisioning), intentionally missing profiles, and long display names for frontend edge-case testing.

<!-- gitnexus:start -->
# GitNexus — Code Intelligence

This project is indexed by GitNexus as **ControlHub-Backend** (5678 symbols, 13645 relationships, 181 execution flows). Use the GitNexus MCP tools to understand code, assess impact, and navigate safely.

> Index stale? Run `node .gitnexus/run.cjs analyze` from the project root — it auto-selects an available runner. No `.gitnexus/run.cjs` yet? `npx gitnexus analyze` (npm 11 crash → `npm i -g gitnexus`; #1939).

## Always Do

- **MUST run impact analysis before editing any symbol.** Before modifying a function, class, or method, run `impact({target: "symbolName", direction: "upstream"})` and report the blast radius (direct callers, affected processes, risk level) to the user.
- **MUST run `detect_changes()` before committing** to verify your changes only affect expected symbols and execution flows. For regression review, compare against the default branch: `detect_changes({scope: "compare", base_ref: "main"})`.
- **MUST warn the user** if impact analysis returns HIGH or CRITICAL risk before proceeding with edits.
- When exploring unfamiliar code, use `query({query: "concept"})` to find execution flows instead of grepping. It returns process-grouped results ranked by relevance.
- When you need full context on a specific symbol — callers, callees, which execution flows it participates in — use `context({name: "symbolName"})`.

## Never Do

- NEVER edit a function, class, or method without first running `impact` on it.
- NEVER ignore HIGH or CRITICAL risk warnings from impact analysis.
- NEVER rename symbols with find-and-replace — use `rename` which understands the call graph.
- NEVER commit changes without running `detect_changes()` to check affected scope.

## Resources

| Resource | Use for |
|----------|---------|
| `gitnexus://repo/ControlHub-Backend/context` | Codebase overview, check index freshness |
| `gitnexus://repo/ControlHub-Backend/clusters` | All functional areas |
| `gitnexus://repo/ControlHub-Backend/processes` | All execution flows |
| `gitnexus://repo/ControlHub-Backend/process/{name}` | Step-by-step execution trace |

## CLI

| Task | Read this skill file |
|------|---------------------|
| Understand architecture / "How does X work?" | `.claude/skills/gitnexus/gitnexus-exploring/SKILL.md` |
| Blast radius / "What breaks if I change X?" | `.claude/skills/gitnexus/gitnexus-impact-analysis/SKILL.md` |
| Trace bugs / "Why is X failing?" | `.claude/skills/gitnexus/gitnexus-debugging/SKILL.md` |
| Rename / extract / split / refactor | `.claude/skills/gitnexus/gitnexus-refactoring/SKILL.md` |
| Tools, resources, schema reference | `.claude/skills/gitnexus/gitnexus-guide/SKILL.md` |
| Index, status, clean, wiki CLI commands | `.claude/skills/gitnexus/gitnexus-cli/SKILL.md` |

<!-- gitnexus:end -->

# 12-rule

These rules apply to every task in this project unless explicitly overridden.
Bias: caution over speed on non-trivial work. Use judgment on trivial tasks.

## Rule 1 — Think Before Coding
State assumptions explicitly. If uncertain, ask rather than guess.
Present multiple interpretations when ambiguity exists.
Push back when a simpler approach exists.
Stop when confused. Name what's unclear.

## Rule 2 — Simplicity First
Minimum code that solves the problem. Nothing speculative.
No features beyond what was asked. No abstractions for single-use code.
Test: would a senior engineer say this is overcomplicated? If yes, simplify.

## Rule 3 — Surgical Changes
Touch only what you must. Clean up only your own mess.
Don't "improve" adjacent code, comments, or formatting.
Don't refactor what isn't broken. Match existing style.

## Rule 4 — Goal-Driven Execution
Define success criteria. Loop until verified.
Don't follow steps. Define success and iterate.
Strong success criteria let you loop independently.

## Rule 5 — Use the model only for judgment calls
Use me for: classification, drafting, summarization, extraction.
Do NOT use me for: routing, retries, deterministic transforms.
If code can answer, code answers.

## Rule 6 — Token budgets are not advisory
Per-task: 4,000 tokens. Per-session: 30,000 tokens.
If approaching budget, summarize and start fresh.
Surface the breach. Do not silently overrun.

## Rule 7 — Surface conflicts, don't average them
If two patterns contradict, pick one (more recent / more tested).
Explain why. Flag the other for cleanup.
Don't blend conflicting patterns.

## Rule 8 — Read before you write
Before adding code, read exports, immediate callers, shared utilities.
"Looks orthogonal" is dangerous. If unsure why code is structured a way, ask.

## Rule 9 — Tests verify intent, not just behavior
Tests must encode WHY behavior matters, not just WHAT it does.
A test that can't fail when business logic changes is wrong.

## Rule 10 — Checkpoint after every significant step
Summarize what was done, what's verified, what's left.
Don't continue from a state you can't describe back.
If you lose track, stop and restate.

## Rule 11 — Match the codebase's conventions, even if you disagree
Conformance > taste inside the codebase.
If you genuinely think a convention is harmful, surface it. Don't fork silently.

## Rule 12 — Fail loud
"Completed" is wrong if anything was skipped silently.
"Tests pass" is wrong if any were skipped.
Default to surfacing uncertainty, not hiding it.
