# Model Module

Domain structs, taxonomy constants, validation methods, and dictionary definitions. No external dependencies — pure Go types.

## Files
| File | Responsibility |
|------|---------------|
| resource.go | Resource struct, ResourceProfileResponse, ResourceType type |
| relation.go | ResourceRelation struct, RelationType type |
| audit.go | AuditEvent struct |
| auth.go | UserCredential, LoginRequest, LoginResponse structs |
| settings.go | Environment, Owner, Role structs |
| pagination.go | PageInfo, ResourceListQuery, AuditListQuery, pagination helpers/constants |
| dictionary.go | DictionaryItem struct (shared by all dictionaries) |
| taxonomy.go | All enum constants (8 resource types, 7 relation types, 5 lifecycle statuses, 4 health statuses), dictionary slices, Validate() methods |
| query_target.go | QueryTarget read-model types, query capability/readiness/safety enums, QueryTargetSafetyStateDictionary + Validate |
| query_schema.go | Query schema response types, including TableDefinitionResponse |
| query_execution.go | Query execution request/response/history types, execution status enum, QueryEnvironmentPolicy enum + Validate, ValidateCredentialRef, QueryCredentialMetadata, ErrInvalidCredentialMetadata, Phase 38S governed-result-paging: QueryExecutePaginationRequest/Response, ValidatePagination, ValidatePaginationPage |
| query_credential.go | Phase 38A query credential metadata request/response/runtime-status types + Validate (metadata only; never DSN/password) |
| query_disclosure.go | Phase 38Q governed result-disclosure policy: ResultDisclosureMode enum + Validate, ResultDisclosurePolicy, ResultDisclosurePolicyUpsertRequest + Validate, ResultDisclosurePolicyListQuery |
| query_saved_statement.go | Phase 38R governed saved statements: QuerySavedStatementScope enum + Validate, QuerySavedStatement, QuerySavedStatementCreateRequest/UpdateRequest + Validate, QuerySavedStatementListQuery/Response |
| resource_test.go | Validation and dictionary completeness tests |
| query_execution_test.go | Environment-policy, credential_ref fail-closed validator tests, Phase 38S governed-result-paging contract tests (ValidatePagination, ValidatePaginationPage, JSON omitempty) |
| query_credential_test.go | Runtime-status and upsert-request validation tests (fail-closed enum, all-environments confirmation) |
| query_disclosure_test.go | Disclosure-mode and upsert-request validation tests (fail-closed mode, identifier syntax/length) |
| query_saved_statement_test.go | Saved-statement scope, create, and update request validation tests (fail-closed scope, name bounds/control chars, statement size) |

## Exports
- All domain structs (Resource, ResourceRelation, AuditEvent, etc.)
- Type constants and validation methods
- `ResourceTypeDictionary()`, `RelationTypeDictionary()`, `LifecycleStatusDictionary()`, `HealthStatusDictionary()`
- `QueryEnvironmentPolicy.Validate()`, `ValidateCredentialRef()` (query sandbox credential policy)
- `QueryCredentialRuntimeStatus.Validate()` / `.IsResolved()`, `QueryCredentialUpsertRequest.Validate()` (Phase 38A credential metadata contract)
- `ResultDisclosureMode.Validate()`, `ResultDisclosurePolicyUpsertRequest.Validate()` (Phase 38Q governed result-disclosure policy)
- `QuerySavedStatementScope.Validate()`, `QuerySavedStatementCreateRequest.Validate()`, `QuerySavedStatementUpdateRequest.Validate()` (Phase 38R governed saved statements)
- `ValidatePagination()`, `ValidatePaginationPage()`, `QueryExecutePaginationRequest`, `QueryExecutePaginationResponse`, `AllowedPageSizes`, `QueryExecuteDefaultPageSize` (Phase 38S governed query-result paging)

## Dependencies
- Upstream: none (this is the base layer)
- Downstream: consumed by service, repository, and api layers

## Update Rule
If types, constants, or validation logic change, update this file.
