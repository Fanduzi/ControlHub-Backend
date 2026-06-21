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
| query_target.go | QueryTarget read-model types and query capability/readiness/safety enums |
| query_execution.go | Query execution request/response/history types, execution status enum, QueryEnvironmentPolicy enum + Validate, ValidateCredentialRef, QueryCredentialMetadata |
| resource_test.go | Validation and dictionary completeness tests |
| query_execution_test.go | Environment-policy and credential_ref fail-closed validator tests |

## Exports
- All domain structs (Resource, ResourceRelation, AuditEvent, etc.)
- Type constants and validation methods
- `ResourceTypeDictionary()`, `RelationTypeDictionary()`, `LifecycleStatusDictionary()`, `HealthStatusDictionary()`
- `QueryEnvironmentPolicy.Validate()`, `ValidateCredentialRef()` (query sandbox credential policy)

## Dependencies
- Upstream: none (this is the base layer)
- Downstream: consumed by service, repository, and api layers

## Update Rule
If types, constants, or validation logic change, update this file.
