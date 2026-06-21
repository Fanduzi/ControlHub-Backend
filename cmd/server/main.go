// Package main provides the ControlHub application entry point.
// input: config.LoadDotEnv/Load, mysql repositories, api.NewRouter, service constructors
// output: main() binary entry point
// pos: Application bootstrap, manual DI container
// note: if wiring changes, update this header and cmd/server/README.md
package main

import (
	"database/sql"
	"log"
	"net/http"
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

	cfg := config.Load()

	db, err := sql.Open("mysql", cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	deps := buildDependencies(db, cfg)

	log.Printf("ControlHub starting on %s", cfg.HTTPAddress())
	if err := http.ListenAndServe(cfg.HTTPAddress(), api.NewRouter(deps)); err != nil {
		log.Fatal(err)
	}
}

func buildDependencies(db *sql.DB, cfg config.Config) api.Dependencies {
	dictRepo := mysql.NewDictionaryRepository(db)
	relationRepo := mysql.NewRelationRepository(db)
	resourceRepo := mysql.NewResourceRepository(db)
	profileSvc := service.NewProfileService(resourceRepo, resourceRepo)

	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)

	// The query target service derives Phase 37 readiness from credential
	// metadata read through the query execution repository.
	queryTargetSvc := service.NewQueryTargetService(queryTargetRepo).WithCredentialReader(queryExecutionRepo)

	queryExecutionSvc := service.NewQueryExecutionService(
		queryTargetRepo,
		queryExecutionRepo,
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		realClock{},
	)

	return api.Dependencies{
		ResourceService:         service.NewResourceService(resourceRepo, profileSvc),
		RelationService:         service.NewRelationService(relationRepo),
		TopologyService:         service.NewTopologyService(relationRepo),
		AuditService:            service.NewAuditService(mysql.NewAuditRepository(db)),
		AuthService:             service.NewAuthService(mysql.NewUserRepository(db), cfg.JWTSecret),
		EnvironmentService:      service.NewEnvironmentService(dictRepo),
		OwnerService:            service.NewOwnerService(dictRepo),
		RoleService:             service.NewRoleService(dictRepo),
		ResourceTypeService:     service.NewResourceTypeService(dictRepo),
		RelationTypeService:     service.NewRelationTypeService(dictRepo),
		LifecycleStatusService:  service.NewLifecycleStatusService(dictRepo),
		HealthStatusService:     service.NewHealthStatusService(dictRepo),
		ResourceSubtypeService:  service.NewResourceSubtypeService(),
		ProfileService:          profileSvc,
		QueryTargetService:      queryTargetSvc,
		QueryExecutionService:   queryExecutionSvc,
		QueryExecutionAuth: api.QueryExecutionAuthConfig{
			TokenMaxAge: cfg.QueryExecutionTokenMaxAge,
			Clock:       time.Now,
		},
	}
}

// realClock implements service.Clock with the wall clock.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
