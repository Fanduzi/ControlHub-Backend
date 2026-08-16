# Server Entry Point

Application bootstrap and manual dependency injection.

## Files
| File | Responsibility |
|------|---------------|
| main.go | Load config (rejects removed QUERY_EXECUTION_TOKEN_MAX_AGE), validate JWT_SECRET before opening the DB, wire all services into api.Dependencies, start HTTP server |

## Modules Wired
| Module | Service | Repository | Phase |
|--------|---------|------------|-------|
| Resource | ResourceService | ResourceRepository | — |
| Profile | ProfileService | ResourceRepository | — |
| Relation | RelationService | RelationRepository | — |
| Topology | TopologyService | RelationRepository | — |
| Audit | AuditService | AuditRepository | — |
| Auth | AuthService | UserRepository | — |
| Auth Audit | AuthAuditEmitter | AuthAuditEmitter (MySQL) | 38X-2B |
| Dictionary | EnvironmentService, OwnerService, RoleService, ResourceTypeService, RelationTypeService, LifecycleStatusService, HealthStatusService | DictionaryRepository | — |
| Query Target | QueryTargetService | QueryTargetRepository | — |
| Query Credential | QueryCredentialService | QueryTargetRepository, QueryExecutionRepository | 38A |
| Query Execution | QueryExecutionService | QueryTargetRepository, QueryExecutionRepository, QuerySavedStatementRepository (template execution) | 38W |
| Query Schema | QuerySchemaService | QueryExecutionRepository | 38I |
| Query Explain | QueryExplainService | (none — reuses access resolver + audit repo) | 38N |
| Query Disclosure | QueryDisclosureService | QueryDisclosureRepository | 38Q |
| Query Saved Statement | QuerySavedStatementService | QuerySavedStatementRepository | 38W (personal typed declarations) |

Create-with-profile is atomic: `ResourceService.Create` routes embedded profiles through the repository's single transaction (`CreateResourceWithProfile`), so a failed initial profile write returns an error and leaves no resource row.

## Shared Guards
- `QueryGuard` is constructed once and reused by execution, explain, and saved-statement services.

## Exports
- (binary entry point, no exported Go symbols)

## Dependencies
- Upstream: `internal/config`, `internal/api`, `internal/service`, `internal/repository/mysql`
- Downstream: none (leaf in dependency tree)

## Update Rule
If services or wiring change, update this file and the root README.md Architecture section.
