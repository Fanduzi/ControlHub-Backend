# Service Module

Business logic layer with interface-based repository dependencies. Each service defines its own repository interface where it's used.

## Files
| File | Responsibility |
|------|---------------|
| resource_service.go | Resource listing, detail, profile projection |
| relation_service.go | Relation listing by resource |
| audit_service.go | Audit event listing (global and per-resource) |
| auth_service.go | Login, token generation, credential validation |
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
| query_executor.go | Read-only MySQL/TiDB execution, compiler-owned template binding, and bounded result scanning |
| query_execution_service.go | Governed query execution and Phase 38S result paging with per-page access, disclosure, history, and audit |
| query_executor_test.go | Executor scanning, result-cap, and compiler-owned template binding tests |
| query_execution_service_test.go | Query execution service tests, including governed per-page access, disclosure, and persistence guarantees |
| query_disclosure_service.go | QueryDisclosureService — policy lookup, projection resolution, result transformation |
| query_disclosure_projection.go | Column provenance resolution from SQL AST and FK metadata |
| query_disclosure_mask.go | applyDisclosureMask for server-side value redaction |
| query_saved_statement_service.go | QuerySavedStatementService — CRUD for target-scoped saved statements with authorization and guard validation |
| auth_service_test.go | Auth service tests |
| dictionary_service_test.go | Dictionary service tests |
| query_disclosure_service_test.go | Disclosure service tests (Preflight, PreflightRelatedRecords, Apply) |
| query_disclosure_mask_test.go | Disclosure mask unit tests |
| query_guard_test.go | Query guard allow/reject, limit, explain, and saved-statement tests |
| query_template_compiler_test.go | Template compiler source-order, guard, rejection, and driver-binding tests |
| query_saved_statement_service_test.go | Saved statement service tests (List, Create, Update, Delete authorization and validation) |

## Exports
- `NewXxxService(repo) *XxxService` constructors for all services
- `ErrResourceNotFound`, `ErrInvalidCredentials`, `ErrQueryDisclosureBlocked` sentinel errors
- `ErrQuerySavedStatementNotFound`, `ErrQueryForbidden` — saved-statement service sentinel errors
- `QueryDisclosureReader`, `QueryDisclosureWriter` — narrow disclosure policy access interfaces
- `DisclosurePlan`, `ColumnDisclosure` — resolved per-column disclosure decisions
- `ColumnProvenance`, `ProjectionPlan` — column source identity from SQL/AST or FK metadata
- `QueryGuard.GuardSavedStatement` — save-route validation for bare parser-approved SELECT statements without LIMIT injection
- `QueryGuard.GuardPaginatedSelect` — page-window validation for bare parser-approved SELECT statements with AST-owned LIMIT/OFFSET
- `TemplateStatementCompiler.Compile/CompileAndGuard` — server-owned named-placeholder compilation with positional driver bindings
- `TemplateParameterDefinition`, `TemplateStatementInput`, `CompiledTemplateStatement`, `GuardedTemplateStatement` — compiler/guard seam values for governed execution
- `QueryDatabaseExecutor.QueryTemplate` — executes only compiler-produced guarded SQL with positional values in a read-only transaction
- `QuerySavedStatementReader`, `QuerySavedStatementWriter`, `SavedStatementGuard` — saved statement data access interfaces
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
effective row cap for every page, then records the execution attempt in history
and audit. The service does not run a totals query, persist result rows, or
create a snapshot between pages.

## Dependencies
- Upstream: `internal/model` (domain types)
- Downstream: `internal/repository/mysql` (implements the interfaces)

## Update Rule
If services or repository interfaces change, update this file.
