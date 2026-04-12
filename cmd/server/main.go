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
	cfg := config.Load()

	db, err := sql.Open("mysql", cfg.DatabaseDSN)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	dictRepo := mysql.NewDictionaryRepository(db)

	deps := api.Dependencies{
		ResourceService:    service.NewResourceService(mysql.NewResourceRepository(db)),
		RelationService:    service.NewRelationService(mysql.NewRelationRepository(db)),
		AuditService:       service.NewAuditService(mysql.NewAuditRepository(db)),
		AuthService:        service.NewAuthService(mysql.NewUserRepository(db), cfg.JWTSecret),
		EnvironmentService: service.NewEnvironmentService(dictRepo),
		OwnerService:       service.NewOwnerService(dictRepo),
		RoleService:        service.NewRoleService(dictRepo),
	}

	if err := http.ListenAndServe(cfg.HTTPAddress(), api.NewRouter(deps)); err != nil {
		log.Fatal(err)
	}
}
