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

	dictRepo := mysql.NewDictionaryRepository(db)
	relationRepo := mysql.NewRelationRepository(db)

	resourceRepo := mysql.NewResourceRepository(db)

	deps := api.Dependencies{
		ResourceService:         service.NewResourceService(resourceRepo),
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
		ProfileService:          service.NewProfileService(resourceRepo, resourceRepo),
	}

	log.Printf("ControlHub starting on %s", cfg.HTTPAddress())
	if err := http.ListenAndServe(cfg.HTTPAddress(), api.NewRouter(deps)); err != nil {
		log.Fatal(err)
	}
}
