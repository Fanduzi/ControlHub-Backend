# Service Module

Business logic layer with interface-based repository dependencies. Each service defines its own repository interface where it's used.

## Files
| File | Responsibility |
|------|---------------|
| resource_service.go | Resource reads/writes, list/detail server-derived completeness from governed identity, one typed-profile batch, one required relation batch, shared current-resource update validation, governed-identity validation, minimum manual typed-profile identity, health observation ingestion, explicit conflicts, and fail-closed audited inventory updates |
| resource_write_service_test.go | Resource/relation write-flow tests, including identity normalization/immutability, sensitive-label rejection, profile validation, and the manual-identity rule matrix |
| bulk_resource_preview.go | Pure bulk resource mutation preview contract: ordered per-target version checks, field/label diffs, validation, and SHA-256 drift fingerprinting |
| bulk_resource_preview_test.go | Bulk resource mutation preview contract tests: label semantics, invalid targets/operations, input purity, ordering, composite fields, and fingerprint drift |
| profile_service.go | Typed profile PUT/PATCH/DELETE validation plus fail-closed audited mutations for all typed-profile identities |
| profile_service_test.go | Profile write tests: validation, PATCH partial-merge semantics, not-found/archived/unsupported guards, and all typed-profile identity rules |
| relation_service.go | Shared relation read/write entry point; validates server-owned rules before fail-closed audited persistence |
| relation_rules.go | Single relationship matrix authority plus source-specific discovery response |
| completeness.go | Pure seven-group, server-derived resource completeness projection using typed-profile minima and matrix-valid structural endpoints; Domain Name has no structural-edge requirement |
| topology_service.go | Environment-scoped topology workspace and rooted graph traversal with default depth 2, deterministic node/edge caps, remaining-edge-bounded relation reads, and candidate starts |
| topology_service_test.go | Topology traversal tests for depth, direction, cycles, caps, bounded high-fan-out/remaining-budget reads, environment scope, and candidate starts |
| topology_semantics_test.go | Topology semantic classification tests for roles, layers, replication metadata, and problem summaries |
| ingestion_preview.go | Strict bounded CSV/JSON parsing, ordinary non-empty and collector empty-capable preview seams, format-independent fingerprints, exact-identity classification, additive observed diffs, scan conflicts, and repository-backed confirmation delegation |
| ingestion_preview_test.go | Parser equivalence/guard, collector-only empty preview/fingerprint, ordinary-preview rejection, pure preview precedence, immutable-type conflict, additive observed diff, and collector metadata delegation tests |
| audit_service.go | Audit event listing (global and per-resource), preserving optional target-resource environment filtering |
| audit_service_test.go | Audit list query forwarding regression coverage |
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
| query_execution_service.go | Governed query execution with validated user-or-machine identity and truthful evidence; successful User attempts persist owner-private full SQL, history projects exact caller-specific restore eligibility, and detail exposes SQL only through owner/target/success lookup; one private persistence implementation records every post-target attempt as an atomic Execution Evidence Pair |
| query_workspace_service.go | Thin owner-only query workspace Get/validated OCC Put boundary |
| query_workspace_service_test.go | Owner propagation, model validation, persisted aggregate, and OCC conflict service tests |
| query_template_execution_service.go | Fresh-query-actor saved-statement (template) execution — rereads the latest authorized statement, records every post-target terminal outcome without template identity, SQL, parameter names, or values, validates typed values, compiles server-side, then reuses the governed chain per page |
| query_executor_test.go | Executor scanning, result-cap, and compiler-owned template binding tests |
| navigate_related_records_test.go | Related-record navigation service tests: governance, parameter binding, history/audit, Apply-path exclusive `ErrQueryDisclosureBlocked` (Issue #48), and inspector-phase cancellation/deadline evidence (Issue #40) |
| query_execution_service_test.go | Query execution service tests, including successful-User full SQL, owner-only retrieval and history restore eligibility, machine/non-success omission, governed paging, disclosure, atomic persistence, and cancellation durability |
| query_template_execution_service_test.go | Template-execution tests for reread, authorization, typed values, paging, rejected/failed post-target no-value evidence, cancellation durability, and disclosure wrapping |
| query_disclosure_service.go | QueryDisclosureService — policy lookup, projection resolution, result transformation; governance refusals stay blocked while disclosure machinery failures use a distinct backend sentinel so the execution service records them as terminal failed/timeout/canceled evidence, not policy rejections (Issue #35) |
| query_disclosure_projection.go | Column provenance resolution from SQL AST and FK metadata |
| query_disclosure_mask.go | applyDisclosureMask for server-side value redaction |
| query_saved_statement_service.go | QuerySavedStatementService — authorized target-scoped saved statement CRUD with typed declaration validation and guard validation |
| named_inventory_view_service.go | NamedInventoryViewService — owner-only personal CRUD, admin-only shared mutation, user-visible listing, and shared-only read seam |
| machine_credential.go | `crypto/rand` opaque credential generation, stable lookup-ID parsing, and SHA-256 lookup hashing |
| machine_credential_test.go | Pure opaque-token format, entropy-size, parsing, and hash regression tests |
| machine_principal_service.go | Admin-only safe lifecycle list/create/rotate/revoke and scoped expiry/revoke-aware machine authentication with last-used updates |
| machine_principal_service_test.go | Safe list, one-time secret, scope, expiry, reload/revoke, overlap, and admin-boundary service tests |
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
- `ResourceService.ObserveHealth` — validated operational evidence ingestion that never emits inventory audit events
- `RelationService.Rules` returns source-specific relation types, target resource types, and environment constraints derived from the write validator's matrix
- `DeriveCompleteness` — pure read-only score/status/missing-requirements projection; labels never satisfy a requirement
- `ValidateResourceUpdate` — normal update validation against an already-read current resource, reused by locked bulk mutations
- `PreviewBulkResourceMutation` — pure preview of ordered targets against current snapshots; `ResourceService` delegates persisted preview/confirm reads to its resource repository
- `BulkResourceMutationRequest`, `BulkResourceMutationTarget`, `ResourceMutationSnapshot`, `LabelOperations`, `BulkResourcePreview` — bulk mutation preview contract values
- `TopologyService.BuildTopology` — builds rooted or environment-start topology responses with node/edge caps and `truncated`; rooted traversal requests at most the remaining edge budget plus one sentinel relation per hop
- `ParseIngestion` and `PreviewIngestion` provide controlled parsing and pure fingerprints; `ResourceService.PreviewIngestion` keeps ordinary previews non-empty, while `PreviewCollectorIngestion` admits an empty collector set and returns its canonical reviewed fingerprint for `ConfirmCollectorIngestion`; immutable CI-type mismatches remain conflicts and observed diffs contain only submitted fields
- `CollectorIngestionMetadata`, `model.CollectorScanResult`, `ValidateCollectorIngestionMetadata`, `ErrCollectorIngestionMetadataInvalid`, and `ErrCollectorScanConflict` define the bounded collector-confirmation and retry contract without exposing MySQL error types
- `ValidateIngestionRows`, `ValidateCollectorIngestionRows`, `ValidateIngestionRelationship`, `ErrIngestionConflict`, and `ErrIngestionFingerprintMismatch` support repository confirmation without duplicating service-owned validation rules
- `NewMemoryUserStore`, `AuthService.WithClock`, `AuthService.ChangeUserRole`/`SetUserActive`/`ResetUserPassword` — Authorization Version seams
- `AuthAuditEmitter`, `NoopEmitter` — fail-open authentication/authorization audit emission interface and discard implementation
- `BoundedAuthAuditEmitter`, `NewBoundedAuthAuditEmitter`, `BearerRejectBudget`, `NewBearerRejectBudget`, `ProcessBearerRejectBudget`, `BoundedBearerRejectedLimit`, `AuthAuditSuppressedRejections` — process-shared budget capping untrusted Bearer rejection persistence at 60 events/min per server process (all routers draw from one budget); suppression counter exposed only via the admin auth-audit metrics surface
- `HashPasswordArgon2id`, `VerifyPassword`, `IsLegacyHash`, `IsArgon2idHash` — password hashing and format detection
- `AuthService.LegacyHashCount` — non-identity-bearing count of remaining legacy-hash accounts
- `ErrResourceNotFound`, resource name/alias/external-identifier conflict sentinels, `ErrInvalidCredentials`, `ErrInvalidToken`, `ErrQueryDisclosureBlocked`
- `ErrQuerySavedStatementNotFound`, `ErrQueryForbidden` — saved-statement service sentinel errors
- `NamedInventoryViewService.List/Create/Update/Delete/ListShared` and controlled validation/forbidden/not-found errors
- `MachinePrincipalService.List/Create/Rotate/Revoke/Authenticate`, returning credential lifecycle IDs/timestamps on list, plaintext only from create/rotate, and never lookup IDs or other auth material from public metadata
- `ErrQueryDisclosurePolicyConflict`, `ErrQueryDisclosurePolicyNotFound` — disclosure policy CRUD sentinel errors (duplicate scope → 409, missing scope on update → 404)
- `QueryDisclosureReader`, `QueryDisclosureWriter` — narrow disclosure policy access interfaces
- `DisclosurePlan`, `ColumnDisclosure` — resolved per-column disclosure decisions
- `ColumnProvenance`, `ProjectionPlan` — column source identity from SQL/AST or FK metadata
- `QueryGuard.GuardSavedStatement` — save-route validation for bare parser-approved SELECT statements without LIMIT injection
- `QueryGuard.GuardPaginatedSelect` — page-window validation for bare parser-approved SELECT statements with AST-owned LIMIT/OFFSET
- `TemplateStatementCompiler.Compile/CompileAndGuard/CompileAndGuardPaginated` — server-owned named-placeholder compilation with positional driver bindings and AST-owned page windows
- `TemplateParameterDefinition`, `TemplateStatementInput`, `CompiledTemplateStatement`, `GuardedTemplateStatement` — compiler/guard seam values for governed execution
- `QueryDatabaseExecutor.QueryTemplate` — executes only compiler-produced guarded SQL with positional values in a read-only transaction
- `QueryExecutionService.ExecuteSavedStatement` — re-reads/authorizes the latest saved statement, records every post-target lookup/read/authorization/validation terminal outcome, validates typed values, and runs the governed chain per page; evidence never carries template identity, SQL, parameter names, or values
- `QueryExecutionService.Execute` — accepts only a validated user-or-machine `QueryExecutionIdentity`; ordinary machine execution records the principal ID while user auth freshness remains enforced at the router
- `QueryExecutionService.GetExecutionStatement` and `ErrQueryExecutionNotFound` — exact actor-user/target/execution successful-statement lookup with all mismatches collapsed to not-found
- `QueryWorkspaceService.Get/Put`, `QueryWorkspaceRepository`, and `ErrQueryWorkspaceConflict` — actor-owned aggregate read/validated OCC replacement contract
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
