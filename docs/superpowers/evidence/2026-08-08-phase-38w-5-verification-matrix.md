# Phase 38W-5 Verification Matrix

Date: 2026-08-08
Issue: #6, `38W-5: Release-verify governed parameterized templates`
Parent: #1

This matrix maps every Issue #6 acceptance criterion to concrete proof at the
candidate heads. Proof is existing unit/integration/OpenAPI/E2E coverage unless
a gap required a new failing test. **No production-code gap was proven**; no
product source changed in this delivery.

## Candidate heads

| Repo | Branch | Base (`origin/main`) | HEAD | Worktree |
|---|---|---|---|---|
| Backend | `issue-6-38w5-20260808215659` | `5388a8d0a572948efe3f39c23c7969eb3befe2ce` | docs-only candidate (see evidence) | `/Users/fan/GolangProjects/ControlHub-issue6-38w5` |
| Frontend | `issue-6-38w5-20260808215659` | `917b1389977447e6362d309f0fc2967466581232` | `917b1389977447e6362d309f0fc2967466581232` (no product diff) | `/Users/fan/JsProjects/ControlHub-issue6-38w5` |

## Issue #6 acceptance criteria

| ID | Criterion | Backend proof | Frontend proof | Real Chromium proof | Verdict |
|---|---|---|---|---|---|
| AC1 | Backend unit/integration/OpenAPI/fuzz cover strict requests, compiler rejection, visibility, no value persistence, governed paged execution | See BE rows below | n/a | n/a | **PASS** |
| AC2 | Frontend type/lint/unit/build/preflight/governance cover request shape, value lifetime, accessibility, localization, no-side-effect loading | n/a | See FE rows below | n/a | **PASS** |
| AC3 | Real Chromium desktop EN, 375px EN, desktop zh-CN; zero failures/skips; load inert; every template page governed | n/a | n/a | Template suite 23/23 x3; full suite 163/163 | **PASS** |
| AC4 | Candidate and merged-root repeated E2E/release-evidence requirements | Candidate gates recorded in release evidence | Candidate gates recorded | Candidate E2E recorded; **merged-root deferred** to `$delivery-closure` | **PASS (candidate)** / deferred merge |
| AC5 | Parameter-value evidence capture remains absent (no KMS/archive/retention UI/plaintext forensic store) | Prod grep: no evidence-capture code; ADR retained | No UI/API for evidence capture | Release evidence asserts absence | **PASS** |

## Detailed criterion to test map

### Strict execution requests (ID + values + maxRows + pagination only)

| Seam | Test |
|---|---|
| Model decode/validate | `internal/model/query_saved_statement_execution_test.go` — `TestQuerySavedStatementExecuteRequestDecode`, `…DecodeRejectsMalformed`, `…Validate` |
| Handler strict JSON | `internal/api/query_saved_statement_execution_handler_test.go` — `TestTemplateExecute_RejectsUnknownAndForbiddenFields`, `…RejectsDuplicateKeysAndMalformedJSON`, `…RejectsOversizedValuesObject`, `…RejectsInvalidPaginationAndMaxRows`, `…RequiresBearer`, `…Success` |
| Service | `internal/service/query_template_execution_service_test.go` — `TestExecuteSavedStatementRunsPersonalTemplateThroughGovernedChain`, `…BindsTypedValuesInSourceOrder`, `…StaticStatementExecutesWithNoValues` |
| FE service | `tests/services/query-saved-statement-execution.test.ts` — POSTs values only; never sends SQL/definitions/identities/credentials |
| OpenAPI | `internal/openapi/openapi.yaml` `POST /query-targets/{id}/saved-statements/{statementId}/execute`; `QuerySavedStatementExecuteRequest` `additionalProperties: false`; `TestOpenAPIYAMLIsValid`; Schemathesis via `make test-openapi-fuzz` (2089/2089) |

### Compiler rejection (unsafe placement, mismatch, guard)

| Seam | Test |
|---|---|
| Compiler unit | `internal/service/query_template_compiler_test.go` — bind order, repeated placeholder, positional restore, static valid, paginated window, invalid declarations/values (14 cases), read-only left to guard, decimal/boolean bind |
| Declaration-only | `internal/service/query_template_compiler_validation_test.go` — `TestTemplateStatementCompilerValidateCompilesDeclarationsWithoutRuntimeValues` |
| Save-path validation | `internal/service/query_saved_statement_service_test.go` Create/Update reject undeclared placeholders and unsafe SQL before mutation |
| Integration stale defs | `internal/integration/query_template_execution_test.go` — `TestExecuteSavedStatementIntegrationStaleDefinitionsFailClosed` |

### Actor visibility (personal owner-only; shared admin mutate / fresh actor run)

| Seam | Test |
|---|---|
| Execute service | `TestExecuteSavedStatementRejectsForeignPersonalTemplate`, `…AllowsSharedTemplateForAnyActor` |
| Saved-statement service | Create/Update/Delete/List matrix in `query_saved_statement_service_test.go` (admin-only shared mutation; admin cannot see/mutate foreign personal by role) |
| Repository | `TestListVisible_ExcludesOtherUsersPersonal`, `…ReturnsSharedAndPersonalForOwner` |
| Integration | `TestExecuteSavedStatementIntegrationAuthorizationMatrix` |
| FE unit | `tests/components/query-saved-statements.test.tsx` — Edit/Delete gated by `canManageSharedTemplates` |
| E2E | Issue #5 block: manager affordances EN/375/zh-CN; non-manager Load only; non-admin execute+page |

### No parameter-value persistence / leak

| Seam | Test |
|---|---|
| Service history | `TestExecuteSavedStatementRunsPersonalTemplateThroughGovernedChain` — preview/digest never contain bound values |
| Field errors | `TestExecuteSavedStatementValidatesTypedValuesWithFieldErrors`; handler `TestTemplateExecute_ControlledFieldErrorsNeverEchoValues` |
| Integration | `assertNoValuePersisted` in `query_template_execution_test.go` (bind, pagination, stale, history placeholder) |
| Schema | migrations: parameters store name/type/ordinal only; executions store digest/preview/error only |
| FE | unit error messages never echo values; E2E controlled validation without leak; E2E “no owner/author/value leakage” |
| Logs/telemetry | **Structural**: zero log/telemetry statements on template path; no log-capture harness exists (accepted residual; not a product gap) |

### Governed paged execution + disclosure re-preflight

| Seam | Test |
|---|---|
| Unit | `TestExecuteSavedStatementPagesThroughTemplateRouteWithFreshHistory`, `…DisclosureChangeAffectsLaterPage` |
| Shared chain | `TestExecute_PagedSecondPageRunsFreshAccessAndDisclosure`, `…RechecksChangedPolicy` |
| Integration | `TestExecuteSavedStatementIntegrationTemplatePagination`, `…DisclosureChangeAffectsLaterPage` |
| E2E | template pagination stays on saved-statement execute route; later disclosure-policy change blocks subsequent page |

### Load is inert

| Seam | Test |
|---|---|
| FE unit | `query-saved-statements.test.tsx` / `query-editor-shell.test.tsx` — load does not fire execute/explain/schema |
| E2E | `template load is inert: no execute/explain/schema/history/related/disclosure requests` |

### Accessibility (parameter form a11y)

| Seam | Test |
|---|---|
| FE unit field errors | `tests/components/query-editor-shell.test.tsx` — `shows localized accessible field errors and retains entered values` (`aria-invalid`, `aria-describedby`) |
| FE unit zh-CN errors | same file — `renders template-mode field errors in zh-CN` |
| E2E focus + validation | Issue #5 — `375px EN: load shared param template, controlled validation, focus, and execute` |
| E2E controlled validation no leak | Phase 38W-3 — `controlled field validation shows a localized error without leaking the value` |
| Mobile form scroll | `tests/components/query-saved-statements.test.tsx` — `keeps the mobile multi-parameter form scrollable` |

### Session disposal ADR (`2026-08-08-phase-38w-template-value-session-disposal.md`)

| Rule | Proof |
|---|---|
| No standalone close-session control | `query-editor-shell.test.tsx` asserts zero `close template session` buttons; E2E Issue #5 non-admin execute asserts `toHaveCount(0)` for that control |
| SQL edit is only template-mode exit | Unit: `editing the SQL exits template mode and restores the ordinary run route`; `editing SQL exits template mode, clears values, invalidates stale response, and restores ordinary Run`; `formatting that changes SQL exits template mode and clears values`. E2E: matching Phase 38W-3 cases |
| Worksheet close/switch discard | Unit: `clears template values on worksheet switch; returning keeps the session with empty values`; `closing a non-last worksheet destroys its template session` |
| Target switch discard | Unit: `target switch creates a clean non-template worksheet and cannot restore old values` |
| Refresh / sign-out discard | E2E: `refresh while template values are present discards values on reload`; `sign-out and re-login discards template values` |
| All pages on template execute route while in template mode | Unit: `pages a template through the saved-statement execute route`. E2E: template pagination + non-admin Next page stay on `/query-targets/{id}/saved-statements/{statementId}/execute` |

### Desktop EN / 375px EN / desktop zh-CN

| Flow | Tests |
|---|---|
| Personal load form | E2E Saved statements (Phase 38R) three viewport/locale cases |
| Template execute + validation | Phase 38W-3 375px + zh-CN cases |
| Shared affordance + param session | Issue #5 EN/375/zh-CN manager + param execute cases |
| Locale parity unit | `tests/lib/messages-template-parity.test.ts` |

### Parameter-value evidence capture remains absent

| Check | Result |
|---|---|
| Prod code grep (KMS, forensic archive, evidence retention UI, plaintext evidence store) | **None** outside docs/ADRs |
| ADR `docs/decisions/2026-08-04-parameter-value-evidence-retention.md` | Accepted; Phase 38W does not implement capture |
| New APIs/migrations/UI in this candidate | **None** |

## Residual non-blocking notes (not product gaps)

1. No dedicated log-capture unit test; guarantee is structural (no logging on template path) plus history/audit/error envelope tests.
2. No Go-native `FuzzXxx` for the compiler; OpenAPI Schemathesis covers the HTTP surface.
3. Backend load side-effect absence is architectural (saved-statement read path never touches executor); request-level proof is frontend E2E.
4. CodeGraph unavailable inside candidate worktrees (no `.codegraph/`); discovery used root index at identical base SHA + direct inspection of callers. GitNexus unavailable (intentionally uninstalled); scope verified manually via `git status`/`git diff` and test inventory.
5. Merged-root re-runs and CI green after push are **deferred** to `$delivery-closure` (this run must not merge/push/close #6).

## RED to GREEN corrections

None. Inventory + gates proved the contract true at the candidate heads without a failing regression that required production repair.

## Gate command index

See companion file
`docs/superpowers/evidence/2026-08-08-phase-38w-5-release-verify-evidence.md`.
