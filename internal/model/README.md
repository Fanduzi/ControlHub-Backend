# Model Module

Domain structs, taxonomy constants, validation methods, and dictionary definitions. No external dependencies — pure Go types.

## Files
| File | Responsibility |
|------|---------------|
| collector_scan.go | Completed-scan ledger values, exact retry matching, and pure capped per-CI omission/Missing transitions |
| collector_scan_test.go | COMPLETE-only omission, idempotency, rediscovery, and conflicting-retry regression tests |
| resource.go | Resource governed identity, immutable origin, aliases, external identifiers, effective health evidence, list/detail-only read-only Completeness, profile response, and ResourceType |
| resource_write.go | Resource create/update inputs, including managed identity collections and nullable manual health override |
| health_observation.go | HealthObservation value and exact fresh/stale/never boundary calculation |
| resource_effective_value.go | Effective CI value and observed/manual provenance response contracts |
| relation.go | ResourceRelation struct, RelationType type |
| audit.go | AuditEvent with at-most-one user/machine actor plus server-owned AuditChange field-diff contract |
| auth.go | UserCredential (incl. IsActive + AuthorizationVersion), LoginRequest, LoginResponse structs |
| settings.go | Environment, Owner, Role structs |
| pagination.go | PageInfo, ResourceListQuery with search, owner, and label filters; ResourceLabelFilter; AuditListQuery including actor/resource search; pagination helpers/constants |
| named_inventory_view.go | Minimal validated named-view contract containing inventory filters, sort, and columns without result/page snapshots |
| named_inventory_view_test.go | Positive-ID and reusable-state validation regression tests |
| machine_principal.go | Independent machine-principal metadata, safe admin credential-lifecycle list records, five read/query scopes plus reserved collector-only `inventory:ingest` and `health:write` scopes, and 30/90-day expiry rules |
| machine_principal_test.go | Closed read/query/collector-scope and bounded-expiry pure domain regression tests |
| dictionary.go | DictionaryItem struct (shared by all dictionaries) |
| taxonomy.go | All enum constants (8 resource types, 7 relation types, 5 lifecycle statuses, 4 health statuses), dictionary slices including service worker subtype, Validate() methods; Domain Name `dns` and Virtual IP `floating` subtypes |
| taxonomy.go | All enum constants (8 resource types, 7 relation types, 5 lifecycle statuses, 4 health statuses), dictionary slices, Validate() methods. Database Proxy technology subtypes and Control Plane ha_monitor; ambiguous ha is rejected. |
| query_target.go | QueryTarget read-model types, query capability/readiness/safety enums, QueryTargetSafetyStateDictionary + Validate |
| query_schema.go | Query schema response types, including TableDefinitionResponse |
| query_execution.go | Query execution request/response/history types, internal full statement and dedicated public statement response, validated user-or-machine QueryExecutionIdentity, truthful actor projection, execution status enum, credential policy/ref validation, and governed result paging |
| query_credential.go | Phase 38A query credential metadata request/response/runtime-status types + Validate (metadata only; never DSN/password) |
| query_disclosure.go | Phase 38Q governed result-disclosure policy: ResultDisclosureMode enum + Validate, ResultDisclosurePolicy, ResultDisclosurePolicyUpsertRequest + Validate, ResultDisclosurePolicyListQuery |
| query_saved_statement.go | Phase 38W governed saved statements: immutable scopes, typed parameter definitions, request validation, template-execution request/limits, and list response types |
| query_workspace.go | Bounded one-row-per-owner worksheet aggregate with optimistic version requests and opaque statement preservation |
| query_workspace_test.go | Workspace bounds/opaque-SQL tests and full-statement history JSON omission coverage |
| resource_test.go | Validation and dictionary completeness tests |
| health_observation_test.go | Freshness time-boundary contract tests |
| query_execution_test.go | User/machine execution-identity, environment-policy, credential_ref, and governed-result-paging validation tests |
| query_credential_test.go | Runtime-status and upsert-request validation tests (fail-closed enum, all-environments confirmation) |
| query_disclosure_test.go | Disclosure-mode and upsert-request validation tests (fail-closed mode, identifier syntax/length) |
| query_saved_statement_test.go | Saved-statement scope, create, and update request validation tests (fail-closed scope, name bounds/control chars, statement size) |

## Exports
- All domain structs (Resource, ResourceRelation, Completeness, AuditEvent, etc.)
- `HealthObservation`, `HealthFreshness`, and `HealthObservation.FreshnessAt()`
- `AuditChange`, `AuditChangeOperation`, and add/update/remove constants for extensible inventory evidence; `AuditListQuery` carries optional target-resource environment filtering
- `EffectiveValue` and `ValueProvenance` for effective CI projections
- Type constants and validation methods, including `ResourceOrigin`
- `ResourceTypeDictionary()`, `RelationTypeDictionary()`, `LifecycleStatusDictionary()`, `HealthStatusDictionary()`
- `QueryEnvironmentPolicy.Validate()`, `ValidateCredentialRef()` (query sandbox credential policy)
- `QueryCredentialRuntimeStatus.Validate()` / `.IsResolved()`, `QueryCredentialUpsertRequest.Validate()` (Phase 38A credential metadata contract)
- `ResultDisclosureMode.Validate()`, `ResultDisclosurePolicyUpsertRequest.Validate()` (Phase 38Q governed result-disclosure policy)
- `QuerySavedStatementScope.Validate()`, typed parameter definitions, `QuerySavedStatementCreateRequest.Validate()`, `QuerySavedStatementUpdateRequest.Validate()`, `QuerySavedStatementExecuteRequest.Validate()` + `MaxQuerySavedStatementExecuteValuesSize` (Phase 38W governed saved statements)
- `QueryWorkspaceWorksheet`, `QueryWorkspace`, and `QueryWorkspacePutRequest.Validate()` for bounded opaque worksheet persistence without query guarding or target lookup
- `QueryExecutionRecord.FullStatement` as internal-only persistence data and `QueryExecutionStatementResponse` as the dedicated public owner-reuse shape
- `NamedInventoryView`, personal/shared scopes, state/request types, and validation for reusable inventory presentation state
- `MachinePrincipal`, `MachineCredential`, safe `MachinePrincipalListItem`/`MachineCredentialLifecycle` projections, the seven closed `MachineScope` values, and bounded expiry normalization
- `CollectorScan`, `CollectorScanLedgerEntry`, `CollectorCIState`, and `ApplyCollectorScan()` for capped complete-scan Missing transitions and retry matching
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
