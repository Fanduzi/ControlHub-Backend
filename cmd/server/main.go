// Package main provides the ControlHub application entry point.
// input: config.LoadDotEnv/Load/ValidateJWTSecret/ErrQueryExecutionTokenMaxAgeRejected, mysql repositories, api.NewRouter, service constructors, os/signal Notify (SIGTERM/SIGINT), net.Listen
// output: main() binary entry point; runServer() graceful-drain lifecycle
// pos: Application bootstrap and lifecycle: validates the signing secret before opening the database, wires resource completeness, governed query, owner workspace/private statement reads, saved-statement template reads, and independent machine credentials, then serves HTTP; SIGTERM/SIGINT stop new traffic and drain in-flight handlers for at most ten seconds (Issue #37)
// note: if wiring, startup validation, or the shutdown contract changes, update this header and cmd/server/README.md
package main

import (
	"database/sql"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func main() {
	if err := config.LoadDotEnv(); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := config.ValidateJWTSecret(cfg.JWTSecret); err != nil {
		log.Fatalf("config: %v", err)
	}

	db, err := sql.Open("mysql", cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	deps := buildDependencies(db, cfg)

	// Issue #37: SIGTERM/SIGINT begin a bounded graceful drain instead of
	// killing the process mid-query, so in-flight governed queries can finish
	// their existing query deadline and two-second Evidence Persistence Window.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	ln, err := net.Listen("tcp", cfg.HTTPAddress())
	if err != nil {
		log.Fatalf("ControlHub listen: %v", err)
	}

	log.Printf("ControlHub starting on %s", cfg.HTTPAddress())
	os.Exit(runServer(&http.Server{Handler: api.NewRouter(deps)}, ln, sigCh, 0, log.Printf))
}

func buildDependencies(db *sql.DB, cfg config.Config) api.Dependencies {
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
		realClock{},
		service.NewMySQLSchemaInspector(),
		queryDisclosureSvc,
	).WithTemplateExecution(querySavedStatementRepo, service.NewTemplateStatementCompiler())

	accessResolver := service.NewTargetAccessResolver(queryTargetRepo, queryExecutionRepo, credentialResolver)
	querySchemaSvc := service.NewQuerySchemaService(
		accessResolver,
		service.NewMySQLSchemaInspector(),
		service.NewQuerySchemaCache(256, realClock{}),
		queryExecutionRepo,
		realClock{},
	)

	// Query saved statement service (Phase 38R) manages governed saved statements
	// with personal and shared_template scopes.
	querySavedStatementSvc := service.NewQuerySavedStatementService(
		querySavedStatementRepo,
		querySavedStatementRepo,
		queryTargetRepo,
		queryGuard, // reuse the shared guard
	)
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
		realClock{},
		service.NewExplainAuditRecorder(queryExecutionRepo),
	)

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
	}
}

// realClock implements service.Clock with the wall clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
