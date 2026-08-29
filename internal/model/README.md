# Model Module

Domain structs, taxonomy constants, validation methods, and dictionary definitions. No external dependencies — pure Go types.

## Files
| File | Responsibility |
|------|---------------|
| resource.go | Resource struct, ResourceProfileResponse, ResourceType type |
| relation.go | ResourceRelation struct, RelationType type |
| audit.go | AuditEvent plus server-owned AuditChange field-diff contract |
| auth.go | UserCredential (incl. IsActive + AuthorizationVersion), LoginRequest, LoginResponse structs |
| settings.go | Environment, Owner, Role structs |
| pagination.go | PageInfo, ResourceListQuery, AuditListQuery, pagination helpers/constants |
| dictionary.go | DictionaryItem struct (shared by all dictionaries) |
| taxonomy.go | All enum constants (8 resource types, 7 relation types, 5 lifecycle statuses, 4 health statuses), dictionary slices including service worker subtype, Validate() methods; Domain Name `dns` and Virtual IP `floating` subtypes |
| query_target.go | QueryTarget read-model types, query capability/readiness/safety enums, QueryTargetSafetyStateDictionary + Validate |
| query_schema.go | Query schema response types, including TableDefinitionResponse |
| query_execution.go | Query execution request/response/history types, execution status enum, QueryEnvironmentPolicy enum + Validate, ValidateCredentialRef, QueryCredentialMetadata, ErrInvalidCredentialMetadata, Phase 38S governed-result-paging: QueryExecutePaginationRequest/Response, ValidatePagination |
| query_credential.go | Phase 38A query credential metadata request/response/runtime-status types + Validate (metadata only; never DSN/password) |
| query_disclosure.go | Phase 38Q governed result-disclosure policy: ResultDisclosureMode enum + Validate, ResultDisclosurePolicy, ResultDisclosurePolicyUpsertRequest + Validate, ResultDisclosurePolicyListQuery |
| query_saved_statement.go | Phase 38W governed saved statements: immutable scopes, typed parameter definitions, request validation, template-execution request/limits, and list response types |
| resource_test.go | Validation and dictionary completeness tests |
| query_execution_test.go | Environment-policy, credential_ref fail-closed validator tests, Phase 38S governed-result-paging contract tests (ValidatePagination, JSON omitempty) |
| query_credential_test.go | Runtime-status and upsert-request validation tests (fail-closed enum, all-environments confirmation) |
| query_disclosure_test.go | Disclosure-mode and upsert-request validation tests (fail-closed mode, identifier syntax/length) |
| query_saved_statement_test.go | Saved-statement scope, create, and update request validation tests (fail-closed scope, name bounds/control chars, statement size) |

## Exports
- All domain structs (Resource, ResourceRelation, AuditEvent, etc.)
- `AuditChange`, `AuditChangeOperation`, and add/update/remove constants for extensible inventory evidence
- Type constants and validation methods
- `ResourceTypeDictionary()`, `RelationTypeDictionary()`, `LifecycleStatusDictionary()`, `HealthStatusDictionary()`
- `QueryEnvironmentPolicy.Validate()`, `ValidateCredentialRef()` (query sandbox credential policy)
- `QueryCredentialRuntimeStatus.Validate()` / `.IsResolved()`, `QueryCredentialUpsertRequest.Validate()` (Phase 38A credential metadata contract)
- `ResultDisclosureMode.Validate()`, `ResultDisclosurePolicyUpsertRequest.Validate()` (Phase 38Q governed result-disclosure policy)
- `QuerySavedStatementScope.Validate()`, typed parameter definitions, `QuerySavedStatementCreateRequest.Validate()`, `QuerySavedStatementUpdateRequest.Validate()`, `QuerySavedStatementExecuteRequest.Validate()` + `MaxQuerySavedStatementExecuteValuesSize` (Phase 38W governed saved statements)
- `ValidatePagination()`, `QueryExecutePaginationRequest`, `QueryExecutePaginationResponse`, `AllowedPageSizes` (Phase 38S governed query-result paging)

## Phase 38S governed query-result paging

`QueryExecuteRequest.Pagination` is optional. When present, the request carries a
1-based `page` and a `pageSize` from `AllowedPageSizes`, which is currently
`[10, 25, 50, 100]`. When pagination is omitted, execute keeps its existing
single-response behavior.

`ValidatePagination` checks the page number, supported page size, and checked
page-window arithmetic. The service-layer guard additionally rejects a page
whose offset begins at or beyond the effective server-owned row cap. The server
owns the page window and cap, so callers do not supply rewritten SQL or a
client-controlled window boundary.

`QueryExecutePaginationResponse` reports only the requested page, page size, and
whether adjacent pages exist. It does not report totals or snapshot identifiers.
Each result page is a fresh governed execution with target access, credential,
statement guard, disclosure policy, timeout, row cap, history, and audit checks.
Result rows are not persisted, and paging does not create a result snapshot.

Metadata statements such as `SHOW`, `DESCRIBE`, and typed `EXPLAIN` remain a
single response even if a pagination block is supplied. Pagination applies to
bare `SELECT` statements only.

## Dependencies
- Upstream: none (this is the base layer)
- Downstream: consumed by service, repository, and api layers

## Update Rule
If types, constants, or validation logic change, update this file.
