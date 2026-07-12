# Phase 38I Schema Intelligence Remediation Evidence

Status: **Remediation complete — ready for human review.**

This note records the Phase 38I remediation that corrected the pre-remediation
object-detail and autocomplete integration defects, productized the Query
Workbench, and closed the credential administration scope gaps.

## Pre-Remediation Defects Acknowledged

The initial Phase 38I implementation (backend `abf74fe`, frontend `9fec6c0`) had
the following defects that were NOT caught by the original evidence claims:

1. **ObjectSummary per-item Database always empty**: `toModelObjects` in
   `query_schema_service.go:265` set `Database: ""` with comment "filled by
   caller" but no caller filled it. The envelope `ObjectListResponse.Database`
   was set correctly, but individual items had empty Database fields.

2. **Schema-aware completion not integrated**: `ReadyWorksheet` did not pass
   `schemaNamespace` or `columnFetcher` to `SqlCodeEditor`. Completion was
   keyword-only, not schema-aware.

3. **Connection selection destructively retargeted worksheets**: Selecting a
   different connection changed the active worksheet target while preserving SQL,
   creating wrong-environment execution risk.

4. **No mobile navigation**: Sidebar was `hidden lg:block` with no mobile
   replacement. Users had no way to navigate between sections on mobile.

5. **Raw enum display**: `environmentPolicy.replaceAll("_", " ")` and other raw
   enum values appeared in the UI instead of localized labels.

6. **9 equal summary cards in credential admin**: Every credential state had equal
   prominence, making it hard to focus on actionable exceptions.

## Remediation Scope

### Backend (1 commit)

| Commit | Description |
|--------|-------------|
| `6ddb326` | fix(query): preserve schema database context in object summaries |

**Changes:**
- `toModelObjects` now accepts `database` parameter and sets `Database: database`
  on each ObjectSummary item
- Added service-layer empty-database validation for `ListObjects` and
  `GetObjectDetails` as defense-in-depth
- Added RED tests proving the database-context invariant

**Files changed:**
- `internal/service/query_schema_service.go` (3 surgical edits)
- `internal/service/query_schema_service_test.go` (3 new tests)

### Frontend (productization + release-blocker repair)

Productization and hardening commits (through `d599238`), plus final
release-blocker repair:

| Commit | Description |
|--------|-------------|
| `7f8ac61` | fix(query): unify worksheet schema metadata and wire real completion |
| `03c283a` | refactor(query): simplify credentials and close localization gaps |
| `f1c9e7d` | refactor(query): consolidate workbench IA and repair mobile accessibility |
| `3c06990` | fix(query): restore checkbox column and add missing i18n keys |
| `5bbb006` | test(query): update tests for Phase 38I behavior changes |
| `eae42ad` | test(query): fix all remaining Phase 38I test failures |
| `11a38e3` | test(e2e): update E2E specs for Phase 38I UI changes |
| `d10bb61` | fix(query): add secret_resolved to known credential states |
| `376e66f` | fix(query): restore select-all checkbox and add i18n key |
| `d599238` | chore: add E2E failure screenshot patterns to gitignore |
| `1f2d640` | fix(query): close Phase 38I E2E masking and workbench release blockers |
| `d1efc4d` | **Final repair:** stop mount Worksheet 2; exact E2E HTTP error matching; remove `allowedNetworkErrors` |

**Key product changes:**
- Created `useWorksheetSchemaAdapter` hook for worksheet-scoped schema namespace
- Wired `schemaNamespace` and `columnFetcher` through `ReadyWorksheet` to
  `SqlCodeEditor` — completion is now real schema-aware
- Connection selection creates new worksheet (not destructive retarget)
- Added dirty state tracking, close confirmation, dirty markers
- Removed hero and floating connection card from `/query`
- Added compact context bar (40-48px) with target/database/environment/readiness
- Added collapsible objects pane (240-280px) on large screens
- Added mobile navigation (hamburger → Sheet drawer)
- Fixed CodeMirror `aria-label` on contenteditable surface
- Fixed tab/tabpanel relationships with keyboard navigation
- Fixed tree disclosure semantics (`aria-expanded`, `aria-selected`)
- Fixed splitter keyboard support (Arrow/Home/End)
- Reduced credential summary from 9 to 4 cards (Total, Ready, Needs attention,
  Unsupported)
- Simplified credential filters (Search, Runtime, Environment, More)
- Reduced credential table from 10 to 6 semantic columns
- Simplified credential modal (one title, one close, flat form, dirty protection)
- Removed placeholder tabs (Saved Scripts, Access)
- Replaced hardcoded strings with i18n keys
- Fixed raw enum display with proper i18n labels

### Final frontend release-blocker repair (`d1efc4d`)

Code-review rejected an earlier “ready for human review” claim. Three fixes:

1. **P1 — no automatic `Worksheet 2` on initial mount**
   - Root cause: `targetSelectionVersion` starts at `0` while
     `lastSeenVersionRef` was `undefined`, so the target-selection effect always
     fired on mount and appended a worksheet.
   - Fix: seed `lastSeenVersionRef` with `useRef(targetSelectionVersion)` so
     only navigator-driven version bumps create a worksheet.
   - Required behavior: initial load has exactly one tab (`Worksheet 1`);
     user target selection creates exactly one new worksheet and activates it.

2. **P2 — intentional E2E HTTP errors match full normalized URL**
   - Root cause: `takeExpectedNetworkError` used `url.includes(urlIncludes)` so
     specs that passed `"/execute"` could consume another target’s execute 400.
   - Fix: `ExpectedHttpError.url` is a full request URL; matching uses
     `normalizeRequestUrl`. Unsafe-SQL E2E derives the target-specific execute
     URL after ready selection, then consumes only that exact one 400.

3. **P2 — remove broad `allowedNetworkErrors`**
   - Removed from public guard API, implementation, types, and tests.
   - Intentional failures use only one-shot exact consumption after the test
     asserts response and UI. Console status echoes remain one-shot only.

## Backend Database-Context Invariant Proof

The fix ensures:

```go
// toModelObjects now sets Database on each item
func (s *QuerySchemaService) toModelObjects(database string, items []ObjectSummary) []model.ObjectSummary {
    result := make([]model.ObjectSummary, len(items))
    for i, o := range items {
        result[i] = model.ObjectSummary{
            Database: database, // echoes requested database
            Name:     o.Name,
            Kind:     model.ObjectKind(o.Kind),
        }
    }
    return result
}
```

RED tests prove:
- `resp.Database == "mydb"` (envelope)
- `resp.Items[0].Database == "mydb"` (per-item)
- Empty database returns `ErrSchemaValidationFailed`

## Shared Schema Adapter Proof

`useWorksheetSchemaAdapter` hook:
- Keyed by `worksheetId + targetResourceId + activeDatabase`
- Derives `SchemaNamespace` from loaded databases/objects and store detail state
- Creates `columnFetcher` that reads from store or fetches on demand
- Provides `loadDetail` with concurrency slot management (max 5)
- Explorer, QuickNavigator, and CodeMirror consume one metadata truth

## Worksheet Safety Proof

- Initial mount keeps exactly one worksheet (`Worksheet 1`); no automatic
  `Worksheet 2` or target-switch-only history/schema work on mount (`d1efc4d`)
- Selecting a different connection creates exactly one new worksheet (not retarget)
- Original worksheet keeps target, database, SQL, result, and history
- Dirty state tracked per worksheet
- Close confirmation for non-empty/dirty worksheets
- URL reflects active target/database (no SQL/secrets in URL)

## E2E Guard Policy (corrected one-shot errors)

- Intentional HTTP failures use **one-shot exact** consumption:
  method + full normalized request URL + status
- A second 400, another target’s execute 400, unrelated 403, 500, or connection
  failure remains and fails `assertClean`
- `allowedNetworkErrors` and all broad network-error regex allowlists are
  **removed** from the public guard API
- Console status echoes (Chromium “status of N”) are one-shot only; no broad
  console regex suppression for intentional product errors

## Gate Results

### Backend (feature tip `6ddb326`)

| Gate | Result |
|------|--------|
| `go test -count=1 ./...` | ✅ PASS |
| `go vet ./...` | ✅ clean |
| `go build ./...` | ✅ clean |
| `make openapi-validate` | ✅ PASS |
| `git diff --check` | ✅ clean |

### Frontend (final repair `d1efc4d`)

| Gate | Result |
|------|--------|
| `npx tsc --noEmit` | ✅ clean |
| `npm run lint` | ✅ 0 errors (pre-existing warnings only) |
| `npm run test` | ✅ 986/986 pass |
| `npm run build` | ✅ successful |
| `npm run check:e2e-preflight` | ✅ pass |
| `npm run check:e2e-governance` | ✅ pass |
| `git diff --check` | ✅ clean |

### Real E2E (final repair `d1efc4d`)

| Spec | Result |
|------|--------|
| `e2e/query-workbench.spec.ts` | ✅ pass (including schema intelligence) |
| `e2e/query-credential-settings.spec.ts` | ✅ pass |
| **Total** | **41 passed, 0 failed, 0 skipped** |

Environment: backend `:8080`, API proxy `:8081`, frontend `:3100`, dedicated
query MySQL fixture `controlhub-query-e2e-mysql` on `127.0.0.1:13306`, ready
target resource `616`.

E2E proves:
- Real database/object/detail loading
- Columns, primary key, composite index, foreign key visible
- Quick Navigator reveal
- Visible real table completion (via schema adapter)
- Successful guarded SELECT built with completion
- Original worksheet preservation after selecting another connection
- Results/History and history reuse
- SHOW, DESCRIBE, unsafe rejection (exact target execute URL), formatting, resize
- Credential inventory/edit/save/delete/bulk regression
- Mobile navigation, Connections sheet, Objects sheet, editor, results
- No broad network error suppression; intentional 400 consumed by exact URL only

## Scope Confirmation

- ✅ No SQL guard change
- ✅ No new query engine
- ✅ No browser database connection
- ✅ No DSN/password/database username in browser state
- ✅ No `actorUserId` request field
- ✅ No credential secret write API
- ✅ No credential edit controls on `/query`
- ✅ No schema persistence migration or browser persistence
- ✅ No saved query, export, approval, JIT, Visual Explain, ER, notebook, AI,
  MCP, query builder, editable grid, connection profile, split window, or Monaco
  implementation
- ✅ No global credential aggregate API
- ✅ No CI workflow change
- ✅ No tag, release, or deploy (merge/push of main is a separate finishing step)
- ✅ No AI co-author trailer

## Specialist Review Status

Oracle and Momus both timed out (30m each) without producing actionable output.
The implementation proceeded based on comprehensive explore agent findings (5
parallel agents) and the detailed spec/plan documents. All blocking issues found
during implementation were fixed autonomously. Final release-blocker repair
(`d1efc4d`) was code-review approved for finishing.

## Deferred Work

Only after this evidence is stable:
- Result-grid copy/navigation under explicit masking policy
- Foreign-key record navigation
- Backend-normalized Visual Explain
- Global credential coverage/facets API
- ER diagram
- Saved queries and governed collaboration
- Additional schema inspector engines
