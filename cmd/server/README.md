# Server Entry Point

Application bootstrap and manual dependency injection.

## Files
| File | Responsibility |
|------|---------------|
| main.go | Load config (rejects removed QUERY_EXECUTION_TOKEN_MAX_AGE), validate JWT_SECRET before opening the DB, wire resource/relation read projections plus independent machine principal/credential services into api.Dependencies, start HTTP server |
| shutdown.go | Graceful drain seam: serve until SIGTERM/SIGINT, stop new traffic and drain in-flight handlers for at most a fixed ten seconds (Issue #37) |

## Modules Wired
| Module | Service | Repository | Phase |
|--------|---------|------------|-------|
| Resource | ResourceService | ResourceRepository | — |
| Profile | ProfileService | ResourceRepository | — |
| Relation | RelationService | RelationRepository | — |
| Machine principal | MachinePrincipalService | MachinePrincipalRepository | Issue #86 |
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
| Named Inventory View | NamedInventoryViewService | NamedInventoryViewRepository | T13 |

Create-with-profile is atomic: `ResourceService.Create` routes embedded profiles through the repository's single transaction (`CreateResourceWithProfile`), so a failed initial profile write returns an error and leaves no resource row.

## Shutdown Contract (Issue #37)

SIGTERM and SIGINT stop accepting new traffic and begin a bounded graceful drain so in-flight governed queries can finish their existing five-second query deadline and two-second Evidence Persistence Window. The drain bound is a fixed ten seconds (`shutdownDrainTimeout` in `shutdown.go`) — a product invariant, not environment configuration; it covers the five-second query deadline, the two-second evidence window, and scheduling margin. A clean drain exits 0. Drain bound exhaustion or an HTTP server failure emits only a fixed safe log message (no error values, request data, or DSNs) and exits non-zero. A second signal during the drain forces immediate exit. Shutdown introduces no background queue or disk buffer, and process crash, host loss, power loss, forced second signal, and `kill -9` remain outside the durability guarantee.

## Shared Guards
- `QueryGuard` is constructed once and reused by execution, explain, and saved-statement services.

## Exports
- (binary entry point, no exported Go symbols)

## Dependencies
- Upstream: `internal/config`, `internal/api`, `internal/service`, `internal/repository/mysql`
- Downstream: none (leaf in dependency tree)

## Update Rule
If services or wiring change, update this file and the root README.md Architecture section.
