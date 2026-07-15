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
| query_execution.go | Query execution request/response/history types, execution status enum, QueryEnvironmentPolicy enum + Validate, ValidateCredentialRef, QueryCredentialMetadata, ErrInvalidCredentialMetadata |
| query_credential.go | Phase 38A query credential metadata request/response/runtime-status types + Validate (metadata only; never DSN/password) |
| resource_test.go | Validation and dictionary completeness tests |
| query_execution_test.go | Environment-policy and credential_ref fail-closed validator tests |
| query_credential_test.go | Runtime-status and upsert-request validation tests (fail-closed enum, all-environments confirmation) |

## Exports
- All domain structs (Resource, ResourceRelation, AuditEvent, etc.)
- Type constants and validation methods
- `ResourceTypeDictionary()`, `RelationTypeDictionary()`, `LifecycleStatusDictionary()`, `HealthStatusDictionary()`
- `QueryEnvironmentPolicy.Validate()`, `ValidateCredentialRef()` (query sandbox credential policy)
- `QueryCredentialRuntimeStatus.Validate()` / `.IsResolved()`, `QueryCredentialUpsertRequest.Validate()` (Phase 38A credential metadata contract)

## Dependencies
- Upstream: none (this is the base layer)
- Downstream: consumed by service, repository, and api layers

## Update Rule
If types, constants, or validation logic change, update this file.
