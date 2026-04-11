package api

import (
	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/service"
)

type Dependencies struct {
	ResourceService *service.ResourceService
	RelationService *service.RelationService
	AuditService    *service.AuditService
	AuthService     *service.AuthService
}

func NewRouter(deps Dependencies) *chi.Mux {
	router := chi.NewRouter()
	router.Get("/health", handleHealth)
	router.Get("/resources", handleListResources(deps.ResourceService))
	router.Get("/resources/{id}", handleGetResource(deps.ResourceService))
	router.Get("/resources/{id}/relations", handleListResourceRelations(deps.RelationService))
	router.Get("/resources/{id}/audit-events", handleListResourceAuditEvents(deps.AuditService))
	router.Get("/audit-events", handleListAuditEvents(deps.AuditService))
	router.Post("/auth/login", handleLogin(deps.AuthService))
	return router
}
