# Phase 38I Schema Intelligence, Object Explorer, And SQL Autocomplete Implementation Plan

> **For OMO/agentic workers:** This is an architecture- and security-sensitive
> full-stack phase. Phase 38H must be merged first. Use separate backend and
> frontend worktrees, TDD, focused commits, and the autonomous review closure
> loop in the worker prompt. Do not stop after the first green test run.

**Goal:** Add governed MySQL/TiDB schema metadata, a lazy Object Explorer and
Quick Navigator, and schema-aware read-only SQL completion without exposing
credentials or weakening the existing SQL guard.

**Architecture:** Backend owns target lookup, credential policy/resolution,
host/port binding, fixed parameterized `information_schema` queries, bounded
cache, audit, and client-safe errors. Frontend owns a shared ephemeral metadata
store consumed by Object Explorer, Quick Navigator, and CodeMirror completion.
No live schema is embedded in the paged query-target list.

---

## Scope

Backend repository:

```text
/Users/fan/GolangProjects/ControlHub
```

Frontend repository:

```text
/Users/fan/JsProjects/ControlHub
```

Target worktrees after the Phase 38H dependency gate passes:

```text
/Users/fan/GolangProjects/ControlHub/.worktrees/backend-phase-38i-schema-intelligence
/Users/fan/JsProjects/ControlHub/.worktrees/phase-38i-schema-intelligence
```

Target branches:

```text
phase-38i-schema-intelligence
phase-38i-schema-intelligence
```

## Required Reading

```text
docs/superpowers/specs/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete.md
docs/superpowers/specs/2026-07-09-phase-38h-query-workbench-scalable-ia-reset.md
docs/superpowers/plans/2026-07-09-phase-38h-query-workbench-scalable-ia-reset.md
docs/superpowers/specs/2026-07-08-phase-38f-query-workbench-sql-editor-foundation.md
docs/superpowers/prompts/shared-worktree-and-tdd-guardrails.md
docs/superpowers/notes/2026-06-24-phase-37h-dedicated-query-e2e-mysql-fixture-evidence.md
```

ControlHub backend references:

```text
cmd/server/main.go
internal/api/router.go
internal/api/query_execution_handler.go
internal/model/query_target.go
internal/model/query_execution.go
internal/service/query_target_service.go
internal/service/query_execution_service.go
internal/service/query_executor.go
internal/service/query_credential_service.go
internal/repository/mysql/query_execution_repository.go
internal/openapi/openapi.yaml
```

ControlHub frontend references after Phase 38H merge:

```text
components/query/query-workbench.tsx
components/query/query-schema-browser.tsx
components/query/query-editor-shell.tsx
components/query/sql-code-editor.tsx
components/query/sql-code-editor-client.tsx
services/query-targets.ts
services/query-executions.ts
types/query-target.ts
e2e/query-workbench.spec.ts
```

Tabularis behavior references only:

```text
/Users/fan/JsProjects/tabularis/src/utils/autocomplete.ts
/Users/fan/JsProjects/tabularis/src/hooks/useSqlAutocompleteRegistration.ts
/Users/fan/JsProjects/tabularis/src/components/modals/QuickNavigatorModal.tsx
/Users/fan/JsProjects/tabularis/src/utils/quickNavigator.ts
/Users/fan/JsProjects/tabularis/src/components/layout/ExplorerSidebar.tsx
/Users/fan/JsProjects/tabularis/src/components/layout/sidebar/SidebarTableItem.tsx
```

Installed CodeMirror contract:

```text
node_modules/@codemirror/lang-sql/dist/index.d.ts
node_modules/@codemirror/lang-sql/README.md
```

## Hard Boundaries

- No SQL guard change.
- No new query engine.
- No browser database connection or credential secret API.
- No DSN/password/database username in browser state, response, audit, cache
  key, error, or log.
- No `actorUserId` request field.
- No credential editing inside `/query`.
- No schema persistence migration.
- No routines, triggers, grants, DDL definitions, write controls, ER diagram,
  Visual Explain, visual query builder, saved query, export, approval, JIT, AI,
  MCP, or notebook implementation.
- No Monaco migration.
- No fake backend as final E2E evidence.
- No CI workflow, push, tag, release, or deployment.
- No AI co-author trailer.
- Do not delete unrelated untracked files.

---

## B0. Phase 38H Dependency And Baseline Gate

**Purpose:** Prevent Phase 38I from building against temporary 38H worktree
contracts.

- [ ] In both root repositories, fetch remote state and inspect `main`.
- [ ] Verify backend `main` exposes paged `GET /query-targets` with `pageInfo`,
  `q`, `targetId`, `page`, and `pageSize` in router/OpenAPI/tests.
- [ ] Verify frontend `main` consumes that contract and contains the final 38H
  on-demand explorer/modal IA.
- [ ] Verify Phase 38H implementation branches/worktrees are no longer the only
  source of those changes.
- [ ] Verify both root worktrees are clean except explicitly approved untracked
  files.
- [ ] Stop with evidence if any check fails. Do not branch from an unfinished
  Phase 38H feature worktree.

Create isolated worktrees only after the gate passes:

```bash
cd /Users/fan/GolangProjects/ControlHub
git worktree add .worktrees/backend-phase-38i-schema-intelligence -b phase-38i-schema-intelligence main

cd /Users/fan/JsProjects/ControlHub
git worktree add .worktrees/phase-38i-schema-intelligence -b phase-38i-schema-intelligence main
```

Baseline backend gates:

```bash
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Baseline frontend gates:

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Record pre-existing warnings separately. Stop on a real baseline failure.

## R0. Pre-Implementation Specialist Review

- [ ] Use `explore` in each repo to map current Phase 38H symbols and tests.
- [ ] Use `librarian` to verify the installed CodeMirror SQL/autocomplete API
  and the named Tabularis reference behavior. Local installed source is the
  version authority; do not design against a different online version.
- [ ] Use `metis` for at most five hidden assumptions/failure modes.
- [ ] Ask `oracle` to review the target-access boundary, metadata API shape,
  cache/privacy model, and Object Explorer IA.
- [ ] Ask `momus` to review this plan for reference validity, task startability,
  testable acceptance criteria, and contradictions.
- [ ] Resolve review blockers in the execution notes before editing.

Oracle, Momus, Metis, Librarian, and Explore are read-only reviewers. Do not ask
them to implement or write files.

---

## B1. Schema API Models And OpenAPI Contract

**Likely files**

```text
internal/model/query_schema.go
internal/model/query_schema_test.go
internal/model/README.md
internal/openapi/openapi.yaml
internal/openapi/openapi_test.go
internal/integration/openapi_fuzz_test.go
```

**Before editing**

- [ ] Run GitNexus `impact` on every existing symbol to be modified.
- [ ] Warn before proceeding if any result is HIGH or CRITICAL.

**RED tests**

- [ ] database/object list responses require `targetResourceId`, `items`, and
  complete `pageInfo`;
- [ ] object kind accepts only `table` or `view` in detail requests;
- [ ] `truncated` contains explicit column/index/FK flags;
- [ ] all schemas use `additionalProperties: false` where the project requires;
- [ ] response models contain no credential, DSN, password, username, host, or
  actor field;
- [ ] all three OpenAPI operations declare auth and controlled errors.

**Implementation**

- Add typed models for database summary, object summary, column, index, foreign
  key, truncation flags, and three responses.
- Reuse Phase 38H `PageInfo`; do not invent a second pagination model.
- Keep database/object names as strings with documented response limits.
- Add `GET /query-targets/{id}/schema/databases` to OpenAPI.
- Add `GET /query-targets/{id}/schema/objects` to OpenAPI.
- Add `GET /query-targets/{id}/schema/object-details` to OpenAPI.
- Keep `QueryTarget.SchemaPreview` empty/deprecated for compatibility; do not
  put live metadata in the paged target response.

**Targeted gates**

```bash
go test -count=1 ./internal/model ./internal/openapi
make openapi-validate
```

**Commit**

```bash
git commit -m "feat(query): define schema metadata API contract"
```

## B2. Shared Governed Target Access Resolver

**Purpose:** Prevent schema introspection and query execution from drifting on
target, credential, environment, resolver, and DSN-binding checks.

**Likely files**

```text
internal/service/query_target_access.go
internal/service/query_target_access_test.go
internal/service/query_execution_service.go
internal/service/query_execution_service_test.go
internal/service/query_credential_service.go (only if the smallest shared boundary requires it)
internal/service/README.md
```

**Characterization RED tests before refactor**

- [ ] missing target remains the same controlled not-found response;
- [ ] unsupported engine remains rejected;
- [ ] missing/invalid/disabled/policy-blocked credential remains rejected;
- [ ] resolver failure remains fixed-string and leak-free;
- [ ] host mismatch, port mismatch, missing port, non-TCP, and malformed DSN
  remain fail-closed;
- [ ] production policy and timeout behavior remain unchanged;
- [ ] successful execution still receives the same target and internal DSN;
- [ ] no error or audit assertion contains DSN markers.

**Implementation**

- Extract the smallest internal resolver that returns a target plus resolved DSN
  only to trusted service code.
- Keep the DSN out of public models and logs.
- Use exact target lookup (`TargetID`) from Phase 38H.
- Have `QueryExecutionService` use the shared resolver without changing guard,
  history, audit, timeout, or response behavior.
- Make the resolver available to `QuerySchemaService` in B4.

**Targeted gates**

```bash
go test -count=1 ./internal/service -run 'Query(TargetAccess|Execution|Credential)'
go vet ./internal/service
```

**Commit**

```bash
git commit -m "refactor(query): share governed target access resolution"
```

## B3. Parameterized MySQL/TiDB Schema Inspector

**Likely files**

```text
internal/service/query_schema_inspector.go
internal/service/query_schema_inspector_test.go
internal/service/README.md
```

**RED tests**

- [ ] database search escapes literal `%`, `_`, and escape characters;
- [ ] object search uses bind parameters, never identifier interpolation;
- [ ] system database exclusion defaults on and can be explicitly disabled;
- [ ] tables and views are classified and ordered deterministically;
- [ ] object details group composite indexes and FKs in ordinal order;
- [ ] detail caps set `truncated` instead of silently growing payloads;
- [ ] one connection/read-only transaction is used per call;
- [ ] timeout/cancel closes rows and connection;
- [ ] raw driver errors are not converted to client messages here.

**Implementation**

- Define a narrow `QuerySchemaInspector` interface.
- Implement MySQL/TiDB fixed `information_schema` queries only.
- Use placeholders for every external filter value.
- Add a pure helper for LIKE escaping and test it directly.
- Set max open connections to one for the per-request target DB handle.
- Reuse production/non-production timeout selection from the shared target
  context without changing query execution timeout constants.

**Targeted gates**

```bash
go test -count=1 ./internal/service -run 'QuerySchemaInspector|EscapeSchemaSearch'
go vet ./internal/service
```

**Commit**

```bash
git commit -m "feat(query): inspect bounded mysql schema metadata"
```

## B4. Schema Cache, Service, Audit, And Controlled Errors

**Likely files**

```text
internal/service/query_schema_cache.go
internal/service/query_schema_cache_test.go
internal/service/query_schema_service.go
internal/service/query_schema_service_test.go
internal/model/query_schema.go
internal/service/README.md
```

**RED tests**

- [ ] schema access independently enforces the shared target-access resolver;
- [ ] unsupported/locked/binding-mismatch targets never call the inspector;
- [ ] positive cache lasts five minutes;
- [ ] empty-result cache lasts 30 seconds;
- [ ] refresh bypasses and replaces only the requested key;
- [ ] oldest entries are evicted at capacity;
- [ ] cache keys never contain a DSN/password/database username;
- [ ] cache hits still write actor/target audit;
- [ ] success is not returned when audit persistence fails;
- [ ] audit event/result strings are fixed and contain no object identifiers;
- [ ] raw inspector errors map to controlled service sentinels.

**Implementation**

- Build a concurrency-safe bounded in-memory cache with an injected clock.
- Include target id and non-secret credential ref in internal keys so credential
  changes do not reuse an unrelated entry.
- Implement `ListDatabases`, `ListObjects`, and `GetObjectDetails`.
- Normalize pagination through the existing model helper.
- Persist one audit event per request attempt after target resolution, including
  cache hits.
- Expose fixed service sentinels for handler mapping.

**Targeted gates**

```bash
go test -count=1 ./internal/service -run 'QuerySchema(Cache|Service)'
go test -race ./internal/service -run 'QuerySchemaCache'
```

**Commit**

```bash
git commit -m "feat(query): govern and cache schema metadata reads"
```

## B5. HTTP Handlers, Auth, Wiring, And Fuzz Contract

**Likely files**

```text
internal/api/query_schema_handler.go
internal/api/query_schema_handler_test.go
internal/api/router.go
internal/api/router_test.go
internal/api/test_server.go
internal/api/README.md
cmd/server/main.go
cmd/server/main_test.go
cmd/server/README.md
internal/openapi/openapi.yaml
internal/integration/openapi_fuzz_test.go
```

**RED tests**

- [ ] all three routes reject missing/stale bearer tokens;
- [ ] actor id comes from verified token, never query/body;
- [ ] `page`, `pageSize`, `kind`, booleans, and length limits reject invalid
  values with 400;
- [ ] every service sentinel maps to the specified status/code;
- [ ] object names with spaces, Unicode, `%`, `_`, and quotes parse safely;
- [ ] strict unknown query policy follows current API conventions;
- [ ] responses and errors pass no-secret assertions;
- [ ] OpenAPI fuzz has no unexpected 5xx.

**Implementation**

- Add a narrow handler interface and parser helpers.
- Mount routes under `requireFreshQueryActor` beside execution/credential routes.
- Wire shared target access, inspector, cache, audit repository, and service in
  `cmd/server/main.go`.
- Do not expose an admin-only distinction; schema reads follow the current
  authenticated query-execution eligibility boundary.

**Targeted gates**

```bash
go test -count=1 ./internal/api ./cmd/server
make openapi-validate
make test-openapi-fuzz
```

**Commit**

```bash
git commit -m "feat(query): expose governed schema metadata endpoints"
```

## B6. Dedicated Fixture And Backend Integration Evidence

**Likely files**

```text
scripts/query-e2e-mysql.sh
internal/integration/query_schema_api_test.go
internal/integration/query_schema_inspector_test.go
```

**Fixture additions**

- Keep `query_e2e` as the credential DSN default database.
- Add a second application database such as `query_e2e_aux`.
- Grant the existing fixture read-only user SELECT on both dedicated fixture
  databases only.
- Add a parent table, child table, view, primary key, composite/secondary index,
  and foreign key with stable names.
- Preserve idempotency, input validation, mode 0600 env file, and no-secret
  output. Do not touch ControlHub's metadata DB container.

**Integration tests**

- [ ] database list/default/system filtering;
- [ ] object list kind/search/pagination;
- [ ] table and view details;
- [ ] PK/composite index/FK ordering;
- [ ] read-only fixture user cannot insert;
- [ ] binding mismatch and locked target rejection;
- [ ] target DB failure maps to controlled error;
- [ ] audit rows contain only fixed metadata;
- [ ] no DSN/password markers in body, error, test output, or audit.

**Gates**

```bash
bash -n scripts/query-e2e-mysql.sh
git diff --check
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
```

Before backend contract freeze:

- [ ] run GitNexus `detect_changes({scope: "compare", base_ref: "main"})`;
- [ ] inspect every affected execution flow;
- [ ] request Momus adversarial backend diff review;
- [ ] fix all P1/P2 findings and rerun full backend gates;
- [ ] request Oracle security-boundary re-review if shared access resolution
  changed after its first review.

**Commit**

```bash
git commit -m "test(query): prove live schema metadata boundaries"
```

Backend contract is frozen for frontend only after B1-B6 are committed and the
backend worktree is clean.

---

## F0. Sync Frozen Backend Contract

- [ ] Confirm backend B1-B6 commit hashes and clean status.
- [ ] Re-read backend router, OpenAPI, and response models; do not copy paths
  from this plan without code verification.
- [ ] If frontend work began in parallel, rebase it onto latest frontend `main`
  only after Phase 38H is merged, then reconcile types against frozen OpenAPI.
- [ ] Record any contract drift before editing frontend service code.

## F1. Schema Types, Service, And Shared Ephemeral Store

**Likely files**

```text
types/query-schema.ts
services/query-schema.ts
tests/services/query-schema.test.ts
lib/query-schema-store.ts
tests/lib/query-schema-store.test.ts
```

**RED tests**

- [ ] exact route and query parameter serialization for all three APIs;
- [ ] request URLs safely encode unusual identifiers;
- [ ] no actor/credential/DSN/password/username fields;
- [ ] positive/negative TTL behavior with fake clock;
- [ ] maximum 50 detail entries and oldest eviction;
- [ ] maximum five concurrent detail requests;
- [ ] identical in-flight requests are deduplicated;
- [ ] refresh bypasses only the selected key;
- [ ] stale/aborted target/database/object writes are ignored;
- [ ] auth failure clears the store;
- [ ] no browser persistence APIs are called.

**Implementation**

- Mirror frozen wire types exactly.
- Use one client module for database/object/detail requests.
- Implement an injectable in-memory store, not a component-local collection of
  unrelated caches.
- Keep network error objects controlled and free of raw response bodies.

**Targeted gates**

```bash
npx vitest run tests/services/query-schema.test.ts tests/lib/query-schema-store.test.ts
npx tsc --noEmit -p tsconfig.json
```

**Commit**

```bash
git commit -m "feat(query): add bounded schema metadata client"
```

## F2. Lazy Object Explorer

**Likely files**

```text
components/query/query-schema-browser.tsx
components/query/query-object-explorer.tsx
components/query/query-object-tree.tsx
components/query/query-workbench.tsx
messages/en.json
messages/zh-CN.json
tests/components/query-object-explorer.test.tsx
tests/components/query-workbench.test.tsx
```

Use existing component/design-system primitives and the final Phase 38H explorer
surface. Do not create a new permanent column.

**RED tests**

- [ ] opening explorer fetches one database page only;
- [ ] expanding one database fetches one object page only;
- [ ] expanding one object fetches one detail only;
- [ ] Tables and Views are distinct and localized;
- [ ] load-more respects pageInfo and 500-object UI cap;
- [ ] server search is debounced and resets page scope;
- [ ] target/database switches discard stale responses;
- [ ] loading, empty, error, retry, refresh, and truncated states are visible;
- [ ] object insert/copy uses correct MySQL quoting and never runs SQL;
- [ ] tree/disclosure keyboard and ARIA behavior is correct;
- [ ] mobile uses drawer behavior with no horizontal overflow;
- [ ] no mutation actions or credential controls appear.

**Implementation**

- Replace placeholder copy with real states; remove obsolete `schemaPreview`
  consumption from the frontend surface.
- Prefer semantic disclosures and bounded lists over a deeply recursive tree.
- Keep active object selection independent from active connection selection.
- Use request generations/AbortController at every async boundary.

**Targeted gates**

```bash
npx vitest run tests/components/query-object-explorer.test.tsx tests/components/query-workbench.test.tsx
npx tsc --noEmit -p tsconfig.json
npm run lint
```

**Commit**

```bash
git commit -m "feat(query): add lazy database object explorer"
```

## F3. Per-Worksheet Database Context And Quick Navigator

**Likely files**

```text
components/query/query-editor-shell.tsx
components/query/query-object-quick-navigator.tsx
components/query/query-workbench.tsx
lib/query-identifiers.ts
messages/en.json
messages/zh-CN.json
tests/components/query-workbench.test.tsx
tests/components/query-object-quick-navigator.test.tsx
tests/lib/query-identifiers.test.ts
```

**RED tests**

- [ ] worksheet initializes from backend default database;
- [ ] each worksheet restores its own active database;
- [ ] database changes do not emit `USE` or execute requests;
- [ ] same-database identifiers insert as correctly quoted object names;
- [ ] cross-database objects insert fully qualified quoted names;
- [ ] insertion replaces the active CodeMirror selection/cursor range;
- [ ] `Cmd/Ctrl+P` opens only on `/query` and prevents browser print there;
- [ ] navigator has focus trap, Up/Down, Enter, Escape, empty/error/retry states;
- [ ] search is bounded/server-backed;
- [ ] columns appear only from already loaded details;
- [ ] selection reveals object but does not auto-run SQL.

**Implementation**

- Add `activeDatabase` to local worksheet state and race guards.
- Keep database context separate from target id and statement.
- Add pure identifier quoting/insertion helpers with special-name tests.
- Connect navigator selection to the shared store and Object Explorer reveal
  state without duplicating metadata requests.

**Targeted gates**

```bash
npx vitest run tests/components/query-workbench.test.tsx tests/components/query-object-quick-navigator.test.tsx tests/lib/query-identifiers.test.ts
npx tsc --noEmit -p tsconfig.json
```

**Commit**

```bash
git commit -m "feat(query): add worksheet database quick navigation"
```

## F4. Read-Only CodeMirror SQL Autocomplete

**Likely files**

```text
package.json
package-lock.json
components/query/sql-code-editor-client.tsx
components/query/sql-code-editor.tsx
components/query/query-editor-shell.tsx
lib/query-sql-completion.ts
tests/lib/query-sql-completion.test.ts
tests/components/query-workbench.test.tsx
```

Add `@codemirror/autocomplete` as a direct dependency only if imported directly
and not already declared.

**RED tests**

- [ ] approved read-only keywords are suggested;
- [ ] write/DDL/session/transaction/locking keywords are absent;
- [ ] active-database tables/views are suggested after FROM/JOIN context;
- [ ] database-qualified objects are suggested;
- [ ] `table.` and `alias.` return the correct loaded/fetched columns;
- [ ] aliases do not cross statement or worksheet boundaries;
- [ ] explicit completion works with Ctrl+Space;
- [ ] dot completion and ordinary typing work without request storms;
- [ ] concurrent detail fetches remain capped at five;
- [ ] completion failure falls back to keywords/loaded metadata;
- [ ] accepted identifiers use backticks without double quoting;
- [ ] completion menu is readable in light/dark/high-contrast themes;
- [ ] run/format shortcuts and editor resize preferences remain intact.

**Implementation**

- Keep MySQL dialect for MySQL/TiDB and existing syntax themes.
- Use an autocomplete override so built-in unrestricted keyword suggestions do
  not leak write/DDL vocabulary.
- Feed currently loaded namespace data into CodeMirror.
- Implement a worksheet-scoped async completion source for alias columns.
- Use syntax tree or a conservative tested current-statement parser. This parser
  is UX-only and must never become an execution security boundary.
- Reconfigure completion extensions when target/database/cache generation
  changes without recreating unrelated worksheet state.

**Targeted gates**

```bash
npx vitest run tests/lib/query-sql-completion.test.ts tests/components/query-workbench.test.tsx
npx tsc --noEmit -p tsconfig.json
npm run lint
```

**Commit**

```bash
git commit -m "feat(query): add governed schema-aware SQL completion"
```

## F5. Frontend Regression, Accessibility, And E2E Specs

**Likely files**

```text
e2e/query-workbench.spec.ts
e2e/query-credential-settings.spec.ts (only if shared 38H behavior needs assertion updates)
tests/components/query-workbench.test.tsx
tests/components/query-object-explorer.test.tsx
tests/components/query-object-quick-navigator.test.tsx
tests/setup.ts (only for required browser API mocks)
```

**Required component coverage**

- lazy request counts at 1000-object scale;
- no stale response writes across target/database/worksheet changes;
- keyboard explorer, navigator, and completion behavior;
- no raw enum/placeholder copy;
- no credential/DSN/password/username/actor rendering or requests;
- no Workbench credential edit controls;
- no regression in worksheet rename/close/isolation, run/format shortcuts,
  target URL selection, governance details, and credential admin modal/drawer.

**Add E2E scenarios**

- object explorer loads actual databases, tables, view, columns, index, and FK;
- object search is server-backed and bounded;
- Quick Navigator finds/reveals an object;
- table completion inserts a visible quoted table name;
- alias-dot completion inserts a visible column name;
- completed SELECT runs and renders fixture data;
- unsafe SQL remains rejected;
- locked target cannot introspect;
- mobile explorer remains a drawer and editor remains primary.

**Frontend full gates**

```bash
git diff --check
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
```

Before commit:

- [ ] run frontend GitNexus `detect_changes` against `main`;
- [ ] inspect expected flows and line-shift artifacts;
- [ ] update required file headers/module README files;
- [ ] stage explicit files and run `git diff --check --cached`.

**Commit**

```bash
git commit -m "test(query): cover schema explorer and SQL completion"
```

---

## E1. Real Full-Stack E2E And Visual QA

Do not call E2E blocked before actively attempting environment startup.

Backend fixture setup from backend Phase 38I worktree:

```bash
make query-e2e-mysql-up
set -a
. ./.query-e2e-mysql.env
set +a
QUERY_DEV_CREDENTIAL_REF=LOCAL_QUERY_RO make seed-query-dev-target
```

Start backend with the ControlHub metadata `DATABASE_DSN` and sourced dedicated
query credential. Verify:

```bash
curl -fsS http://localhost:8080/health
```

Run frontend final E2E from the Phase 38I frontend worktree:

```bash
npm run check:e2e-preflight
npm run test:e2e -- e2e/query-workbench.spec.ts e2e/query-credential-settings.spec.ts
```

Required evidence:

- command, final frontend/backend commit hashes, and exact pass/fail/skip count;
- database/object/detail API requests observed through the real proxy;
- real autocomplete insertion followed by successful guarded SELECT;
- no fake backend and no silently skipped tests;
- leak scan of server/frontend output for DSN/password markers.

Visual QA screenshots at minimum:

- `/query` desktop ready target with Object Explorer expanded;
- large-object search and object detail groups;
- CodeMirror completion menu in light mode;
- CodeMirror completion menu in dark mode;
- 375px mobile explorer drawer and Quick Navigator;
- locked target schema-not-allowed state;
- `/settings/query-credentials` regression view from Phase 38H.

Ask `multimodal-looker` or equivalent visual reviewer to check hierarchy,
clipping, density, keyboard/focus affordances, CJK text, dark contrast, and
whether the editor remains the dominant surface.

After E2E, stop services started by this task and run:

```bash
make query-e2e-mysql-down
```

Do not kill a pre-existing unrelated process. Confirm task-owned ports are free.

## R1. Autonomous Adversarial Closure Loop

This loop is mandatory and repeats until clean:

1. Ask Momus for read-only adversarial diff review across both repos.
2. Ask Oracle for architecture/security/UX re-review because this phase changes
   cross-repo behavior and credential-gated metadata access.
3. Review specifically for unbounded fan-out, stale async writes, DSN leaks,
   cache-key leaks, SQL interpolation, audit gaps, keyword/guard mismatch,
   target/worksheet context drift, and inaccessible tree/dialog behavior.
4. Fix every P1/P2 finding with a regression test.
5. Rerun targeted tests, full backend/frontend gates, real E2E, and affected
   visual QA.
6. Repeat reviews after fixes. Do not hand work back while any P1/P2 remains.

If genuinely blocked, provide concrete command output and explain why no safe
local workaround exists. A service merely being stopped is not a blocker.

## D1. Evidence And Baseline Documentation

Backend docs worktree/repo:

```text
docs/superpowers/notes/2026-07-11-phase-38i-schema-intelligence-object-explorer-sql-autocomplete-evidence.md
docs/quality-baseline.md
docs/releases/candidates/2026-05-26-controlhub-release-readiness-summary.md (only when preparing push)
relevant query roadmap/status note
```

Evidence must state:

- exact backend/frontend commits and final contract;
- fixture schema and read-only grant proof;
- cache/concurrency/page bounds;
- audit and no-secret proof;
- autocomplete allowed/forbidden keyword matrix;
- real E2E and visual QA results;
- honest deferred ER/Visual Explain/other-engine scope.

Commit docs separately from product code:

```bash
git commit -m "docs: record phase 38i schema intelligence evidence"
```

## Final Verification And Status

Backend:

```bash
git diff --check main...HEAD
go test -count=1 ./...
go vet ./...
go build ./...
make openapi-validate
make test-integration
make test-openapi-fuzz
git status --short --branch
```

Frontend:

```bash
git diff --check main...HEAD
npm run check:e2e-preflight
npm run check:e2e-governance
npx tsc --noEmit -p tsconfig.json
npm run lint
npm run test
npm run build
git status --short --branch
```

Closeout is valid only when:

- backend and frontend changes are committed in focused commits;
- both implementation worktrees are clean except explicitly preserved evidence
  excluded by the prompt;
- real E2E is tied to the final commits;
- no P1/P2 remains after the final review loop;
- no push/tag/release/deploy occurred;
- no AI co-author trailer exists.

Final report must include:

- dependency-gate proof and base commits;
- backend/frontend worktrees, branches, and commit lists;
- API paths verified from router/OpenAPI/frontend service/proxy;
- changed files grouped by backend/frontend/docs;
- runtime access, parameterization, cache, audit, and no-secret proof;
- Object Explorer, Quick Navigator, worksheet database, and autocomplete proof;
- allowed/forbidden completion matrix;
- all local gates and exact real E2E counts;
- specialist review ledger and every P1/P2 fixed;
- visual QA evidence paths;
- cleanup and final git status;
- next-phase input and deferred work;
- scope confirmation matching the spec.
