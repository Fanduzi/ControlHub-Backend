// Package serverboot assembles the production HTTP dependency graph once for
// both entry points that must serve the identical governed surface.
// input: database/sql handle, internal/config, mysql repositories, service constructors, api.Dependencies, pgsql.GuardPG warm-up (T10-C wasm compile outside attempt budgets)
// output: Build (production dependency assembly) + Parts (the concrete services a test-only assembly may additionally gate), RealClock
// pos: single dependency-injection seam shared by cmd/server (production, default-closed PG unless CONTROLHUB_PG_HTTP_ENTRIES=on) and cmd/e2e-pg-server (TEST-ONLY fixture listener that explicitly enables the PG HTTP entries); behavior changes belong here exactly once
// note: if this file changes, update header and README.md
package serverboot

import (
	"database/sql"
	"log"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/pgsql"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// Parts exposes the concrete services an explicit test-only assembly may
// additionally gate (e.g. the E2E fixture listener calls
// QueryExecutionService.WithPGHTTPEntries and
// QueryTargetService.WithConnectionLister). cmd/server discards it; the
// production binary can open the PostgreSQL HTTP gate only through the
// cfg.PGHTTPEntries deployment flag, never through Parts.
type Parts struct {
	// QueryExecutionService is the concrete execution service; its
	// WithPGHTTPEntries capability is enabled by Build only when
	// cfg.PGHTTPEntries is on.
	QueryExecutionService *service.QueryExecutionService
	// QueryTargetService is the concrete target read model; its
	// WithConnectionLister connections[] projection is wired by Build only
	// when cfg.PGHTTPEntries is on, in the same branch as the HTTP entries.
	QueryTargetService *service.QueryTargetService
	// QueryExecutions is the shared execution repository, also the
	// QueryConnectionLister a fixture assembly may wire onto the target read
	// model.
	QueryExecutions *mysql.QueryExecutionRepository
}

// RealClock implements service.Clock with the wall clock.
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

// Build wires the full production dependency graph from the repository layer
// to api.Dependencies. It is the exact assembly cmd/server serves. The
// PostgreSQL HTTP gate stays closed unless cfg.PGHTTPEntries is on — the
// explicit deployment configuration (CONTROLHUB_PG_HTTP_ENTRIES=on) — in
// which case Build opens both construction-time capabilities in one branch;
// a test-only assembly may instead flip them itself through Parts.
func Build(db *sql.DB, cfg config.Config) (api.Dependencies, Parts) {
	dictRepo := mysql.NewDictionaryRepository(db)
	relationRepo := mysql.NewRelationRepository(db)
	resourceRepo := mysql.NewResourceRepository(db)
	profileSvc := service.NewProfileService(resourceRepo, resourceRepo)

	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	queryWorkspaceSvc := service.NewQueryWorkspaceService(mysql.NewQueryWorkspaceRepository(db))
	credentialResolver := service.NewEnvCredentialResolver()

	// The query target service derives readiness from credential metadata and,
	// as of Phase 38A, the resolver-gated runtime status: a target is ready only
	// when its credential resolves and binds — never on metadata alone.
	queryTargetSvc := service.NewQueryTargetService(queryTargetRepo).
		WithCredentialReader(queryExecutionRepo).
		WithCredentialResolver(credentialResolver)

	// Query credential metadata service (Phase 38A) manages credential metadata
	// for MySQL/TiDB query targets — metadata only, never a DSN.
	queryCredentialSvc := service.NewQueryCredentialService(queryTargetRepo, queryExecutionRepo, credentialResolver)

	// Query disclosure policy service (Phase 38Q) manages per-column result
	// disclosure policies for query targets.
	queryDisclosureRepo := mysql.NewQueryDisclosureRepository(db)
	queryDisclosureSvc := service.NewQueryDisclosureService(
		queryDisclosureRepo,
		queryDisclosureRepo,
		service.NewMySQLSchemaInspector(),
		queryTargetRepo,
	)

	queryGuard := service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})

	// The saved-statement repository also backs the template-execution route:
	// the execution service re-reads the latest authorized statement per run.
	querySavedStatementRepo := mysql.NewQuerySavedStatementRepository(db)

	queryExecutionSvc := service.NewQueryExecutionService(
		queryTargetRepo,
		queryExecutionRepo,
		credentialResolver,
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		queryGuard,
		RealClock{},
		service.NewMySQLSchemaInspector(),
		queryDisclosureSvc,
	).WithTemplateExecution(querySavedStatementRepo, service.NewTemplateStatementCompiler()).
		// T10-C: the internal keyed PG entry shares the same five-part
		// disclosure policy store its provenance preflight consults. No HTTP
		// route exposes it — PG POST execution stays closed.
		WithPGClaimOrchestration(queryDisclosureRepo)

	// The wazero-backed pg_query compiler loads lazily on the first GuardPG
	// call — a multi-second, process-wide cost that would otherwise be paid
	// inside the first real attempt's frozen statement budget. Warm it at
	// startup so no governed attempt loses its budget to the compile.
	if _, err := pgsql.GuardPG("select 1"); err != nil {
		log.Fatalf("pg guard warm-up: %v", err)
	}

	accessResolver := service.NewTargetAccessResolver(queryTargetRepo, queryExecutionRepo, credentialResolver)
	querySchemaSvc := service.NewQuerySchemaService(
		accessResolver,
		service.NewMySQLSchemaInspector(),
		service.NewQuerySchemaCache(256, RealClock{}),
		queryExecutionRepo,
		RealClock{},
	)

	// Query saved statement service (Phase 38R) manages governed saved statements
	// with personal and shared_template scopes. The PG context wiring (T11)
	// enables (target, database, schema) persistence checks and the G12
	// restore revalidation for PostgreSQL targets.
	querySavedStatementSvc := service.NewQuerySavedStatementService(
		querySavedStatementRepo,
		querySavedStatementRepo,
		queryTargetRepo,
		queryGuard, // reuse the shared guard
	).WithPGContext(queryExecutionRepo, accessResolver)
	namedInventoryViewSvc := service.NewNamedInventoryViewService(mysql.NewNamedInventoryViewRepository(db))
	machinePrincipalSvc := service.NewMachinePrincipalService(mysql.NewMachinePrincipalRepository(db))

	// Query explain service (Phase 38N) — a distinct governed operation that
	// never executes the bare SELECT and never creates a query_executions
	// row. It reuses the shared guard, access resolver, and audit repo.
	queryExplainSvc := service.NewQueryExplainService(
		queryGuard,
		accessResolver,
		service.NewMySQLExplainExecutor(),
		service.NewExplainNormalizer(),
		RealClock{},
		service.NewExplainAuditRecorder(queryExecutionRepo),
	)

	// Release assembly gate: CONTROLHUB_PG_HTTP_ENTRIES=on opens the
	// construction-time capabilities atomically in this single branch —
	// the PG HTTP execution entries, the connections[] read model, and the
	// PG Explain dispatch. One flag flips all, so no deployment can reach a
	// half-enabled state (entries without the read model, or vice versa).
	// With the flag off the graph stays exactly the default-closed
	// production shape. Opening here grants no connection any privilege:
	// enabled, environment_policy, and credential binding still govern
	// every request.
	if cfg.PGHTTPEntries {
		queryExecutionSvc.WithPGHTTPEntries()
		queryTargetSvc = queryTargetSvc.WithConnectionLister(queryExecutionRepo)
		queryExplainSvc.WithPGExplain(service.NewPGExplainRunner(), service.NewPGExplainNormalizer())
	}

	return api.Dependencies{
			ResourceService:                service.NewResourceService(resourceRepo, relationRepo),
			RelationService:                service.NewRelationService(relationRepo),
			TopologyService:                service.NewTopologyService(relationRepo),
			AuditService:                   service.NewAuditService(mysql.NewAuditRepository(db)),
			AuthService:                    service.NewAuthService(mysql.NewUserRepository(db), cfg.JWTSecret),
			AuthAuditEmitter:               mysql.NewAuthAuditEmitter(db),
			EnvironmentService:             service.NewEnvironmentService(dictRepo),
			OwnerService:                   service.NewOwnerService(dictRepo),
			RoleService:                    service.NewRoleService(dictRepo),
			ResourceTypeService:            service.NewResourceTypeService(dictRepo),
			RelationTypeService:            service.NewRelationTypeService(dictRepo),
			LifecycleStatusService:         service.NewLifecycleStatusService(dictRepo),
			HealthStatusService:            service.NewHealthStatusService(dictRepo),
			ResourceSubtypeService:         service.NewResourceSubtypeService(),
			ProfileService:                 profileSvc,
			QueryTargetService:             queryTargetSvc,
			QueryCredentialService:         queryCredentialSvc,
			QueryExecutionService:          queryExecutionSvc,
			QueryExecutionStatementService: queryExecutionSvc,
			QueryWorkspaceService:          queryWorkspaceSvc,
			QuerySchemaService:             querySchemaSvc,
			QueryExplainService:            queryExplainSvc,
			QueryDisclosureService:         queryDisclosureSvc,
			QuerySavedStatementService:     querySavedStatementSvc,
			NamedInventoryViewService:      namedInventoryViewSvc,
			MachinePrincipalService:        machinePrincipalSvc,
			MachineCredentialService:       machinePrincipalSvc,
			QueryExecutionAuth: api.QueryExecutionAuthConfig{
				Clock: time.Now,
			},
		}, Parts{
			QueryExecutionService: queryExecutionSvc,
			QueryTargetService:    queryTargetSvc,
			QueryExecutions:       queryExecutionRepo,
		}
}
