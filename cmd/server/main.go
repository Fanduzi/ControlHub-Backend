package main

import (
	"context"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/repository/postgres"
	"github.com/fan/controlhub/internal/service"
)

func main() {
	cfg := config.Load()

	db, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	deps := api.Dependencies{
		ResourceService: service.NewResourceService(postgres.NewResourceRepository(db)),
		RelationService: service.NewRelationService(postgres.NewRelationRepository(db)),
		AuditService:    service.NewAuditService(postgres.NewAuditRepository(db)),
		AuthService:     service.NewAuthService(postgres.NewUserRepository(db), cfg.JWTSecret),
	}

	if err := http.ListenAndServe(cfg.HTTPAddress(), api.NewRouter(deps)); err != nil {
		log.Fatal(err)
	}
}
