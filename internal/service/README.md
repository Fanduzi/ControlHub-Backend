# Service Module

Business logic layer with interface-based repository dependencies. Each service defines its own repository interface where it's used.

## Files
| File | Responsibility |
|------|---------------|
| resource_service.go | Resource listing, detail, profile projection, create-with-profile atomicity (profile failure ⇒ create error, no partial resource) and strict profile field validation |
| resource_write_service_test.go | Resource/relation write-flow tests, including create-with-profile atomicity and profile field validation at the service seam |
| profile_service.go | Typed profile PUT (full replacement) / PATCH (partial merge) with strict field validation and archived-resource guard |
| profile_service_test.go | Profile write tests: validation, PATCH partial-merge semantics, not-found/archived/unsupported guards |
| relation_service.go | Relation listing by resource |
| audit_service.go | Audit event listing (global and per-resource) |
| auth_service.go | Login, versioned Backend Bearer issuance, current-state VerifyToken (Authorization Version), role/disable/password invalidation, legacy-to-Argon2id transparent migration |
| auth_audit_emitter.go | AuthAuditEmitter interface, NoopEmitter, and BoundedAuthAuditEmitter decorator capping untrusted Bearer rejection persistence at 60/min per process (fail-open) |
| password_hasher.go | Argon2id password hashing, legacy SHA-256 verification, hash format detection, resource budget enforcement |
| password_hasher_budget_test.go | Argon2id verification-budget gate (build-tagged, runs via `make argon2id-budget`): multi-sample VerifyPassword timing at the production seam, median/p95 statistics, fail-loud budget assertion |
| environment_service.go | Environment listing |
| owner_service.go | Owner listing |
| role_service.go | Role listing |
| resource_type_service.go | Resource type dictionary listing |
| relation_type_service.go | Relation type dictionary listing |
| lifecycle_status_service.go | Lifecycle status dictionary listing |
| health_status_service.go | Health status dictionary listing |
| query_schema_service.go | QuerySchemaService.GetTableDefinition returns governed MySQL table definitions |
| query_guard.go | AST-backed read-only validation for execute, paginated results, explain, and saved-query entry points |
| query_template_compiler.go | Server-owned AST placeholder compiler and guarded positional binding seam |
| query_template_compiler_declaration.go | Declaration-only placeholder validation shared by saved-statement persistence and runtime compilation |
| query_executor.go | Read-only MySQL/TiDB execution, compiler-owned template binding, and bounded result scanning |
| query_execution_service.go | Governed query execution and Phase 38S result paging with per-page access, disclosure, and atomically paired history+audit (`InsertExecutionWithAudit`, Issue #34) persisted in a fixed two-second Evidence Persistence Window detached from request cancellation/deadline, with client-cancellation evidence recorded as failed/query_canceled (Issue #35); exposes the `QueryEvidencePersistenceFailures` counter through the service layer |
| query_template_execution_service.go | Fresh-query-actor saved-statement (template) execution — rereads the latest authorized statement, validates typed values, compiles server-side, then reuses the existing governed chain per page |
| query_executor_test.go | Executor scanning, result-cap, and compiler-owned template binding tests |
| query_execution_service_test.go | Query execution service tests, including governed per-page access, disclosure, persistence guarantees, and cancellation-durable terminal evidence via the detached two-second Evidence Persistence Window (Issue #35) |
| query_template_execution_service_test.go | Template-execution service tests (reread, authorization matrix, typed value field errors, per-page chain, no-value persistence, cancellation-durable evidence) |
| query_disclosure_service.go | QueryDisclosureService — policy lookup, projection resolution, result transformation; governance refusals stay blocked while disclosure machinery failures use a distinct backend sentinel so the execution service records them as terminal failed/timeout/canceled evidence, not policy rejections (Issue #35) |
| query_disclosure_projection.go | Column provenance resolution from SQL AST and FK metadata |
| query_disclosure_mask.go | applyDisclosureMask for server-side value redaction |
| query_saved_statement_service.go | QuerySavedStatementService — authorized target-scoped saved statement CRUD with typed declaration validation and guard validation |
| auth_service_test.go | Auth service tests (login, versioned verify, invalidation causes, generic errors) |
| memory_user_store.go | In-memory UserCredentialRepository for unit/handler tests |
| dictionary_service_test.go | Dictionary service tests |
| query_disclosure_service_test.go | Disclosure service tests (Preflight, PreflightRelatedRecords, Apply) |
| query_disclosure_mask_test.go | Disclosure mask unit tests |
| query_guard_test.go | Query guard allow/reject, limit, explain, and saved-statement tests |
| query_template_compiler_test.go | Template compiler source-order, guard, rejection, and driver-binding tests |
| query_saved_statement_service_test.go | Saved statement service tests (List, Create, Update, Delete authorization and validation) |

## Exports
- `NewXxxService(repo) *XxxService` constructors for all services
- `NewMemoryUserStore`, `AuthService.WithClock`, `AuthService.ChangeUserRole`/`SetUserActive`/`ResetUserPassword` — Authorization Version seams
- `AuthAuditEmitter`, `NoopEmitter` — fail-open authentication/authorization audit emission interface and discard implementation
- `BoundedAuthAuditEmitter`, `NewBoundedAuthAuditEmitter`, `BearerRejectBudget`, `NewBearerRejectBudget`, `ProcessBearerRejectBudget`, `BoundedBearerRejectedLimit`, `AuthAuditSuppressedRejections` — process-shared budget capping untrusted Bearer rejection persistence at 60 events/min per server process (all routers draw from one budget); suppression counter exposed only via the admin auth-audit metrics surface
- `HashPasswordArgon2id`, `VerifyPassword`, `IsLegacyHash`, `IsArgon2idHash` — password hashing and format detection
- `AuthService.LegacyHashCount` — non-identity-bearing count of remaining legacy-hash accounts
- `ErrResourceNotFound`, `ErrInvalidCredentials`, `ErrInvalidToken`, `ErrQueryDisclosureBlocked` sentinel errors
- `ErrQuerySavedStatementNotFound`, `ErrQueryForbidden` — saved-statement service sentinel errors
- `ErrQueryDisclosurePolicyConflict`, `ErrQueryDisclosurePolicyNotFound` — disclosure policy CRUD sentinel errors (duplicate scope → 409, missing scope on update → 404)
- `QueryDisclosureReader`, `QueryDisclosureWriter` — narrow disclosure policy access interfaces
- `DisclosurePlan`, `ColumnDisclosure` — resolved per-column disclosure decisions
- `ColumnProvenance`, `ProjectionPlan` — column source identity from SQL/AST or FK metadata
- `QueryGuard.GuardSavedStatement` — save-route validation for bare parser-approved SELECT statements without LIMIT injection
- `QueryGuard.GuardPaginatedSelect` — page-window validation for bare parser-approved SELECT statements with AST-owned LIMIT/OFFSET
- `TemplateStatementCompiler.Compile/CompileAndGuard/CompileAndGuardPaginated` — server-owned named-placeholder compilation with positional driver bindings and AST-owned page windows
- `TemplateParameterDefinition`, `TemplateStatementInput`, `CompiledTemplateStatement`, `GuardedTemplateStatement` — compiler/guard seam values for governed execution
- `QueryDatabaseExecutor.QueryTemplate` — executes only compiler-produced guarded SQL with positional values in a read-only transaction
- `QueryExecutionService.ExecuteSavedStatement` — re-reads/authorizes the latest saved statement, validates typed values with controlled field errors, and runs the existing access/guard/disclosure/executor/history/audit chain per page
- `QueryExecutionRepository.InsertExecutionWithAudit` — repository-owned atomic Execution Evidence Pair (Issue #34): every governed execution — ordinary, paged, template, and related-record navigation — records history + its fixed audit event (`query.executed`, or `related_record_navigation` for navigation, Issue #36) in one transaction; the standalone `InsertExecution` split-write seam is removed (Issue #36)
- `TemplateValueValidationError` — per-parameter field codes (missing/unknown/invalid/oversized); never carries supplied values
- `QuerySavedStatementReader`, `QuerySavedStatementWriter`, `SavedStatementGuard` — saved statement data access interfaces
- Personal parameterized saved statements validate declarations against server-owned compiler placeholders; no parameter values or execution requests enter this service.
- `QuerySavedStatementService.List/Create/Update/Delete` — authorized CRUD for target-scoped saved statements

## Phase 38S governed result paging

`QueryGuard.GuardPaginatedSelect` accepts only a parser-approved bare `SELECT`.
It validates the requested page against the effective server-owned row cap and
adds the page window to the parsed AST. The executor receives that guarded SQL,
not browser-rewritten SQL. `SHOW`, `DESCRIBE`, and typed `EXPLAIN` return the
`ErrQueryPaginationNotApplicable` signal so `QueryExecutionService.Execute`
falls back to the normal single-response path.

Every paged `SELECT` is executed afresh. The service resolves target access,
credential binding, statement governance, disclosure policy, timeout, and the
effective row cap for every page, then records the execution attempt as one
atomic Execution Evidence Pair (history row + fixed `query.executed` audit
event in a single repository transaction). The service does not run a totals
query, persist result rows, or create a snapshot between pages.

## Dependencies
- Upstream: `internal/model` (domain types)
- Downstream: `internal/repository/mysql` (implements the interfaces)

## Update Rule
If services or repository interfaces change, update this file.
