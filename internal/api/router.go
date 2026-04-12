package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/service"
)

type Dependencies struct {
	ResourceService    *service.ResourceService
	RelationService    *service.RelationService
	AuditService       *service.AuditService
	AuthService        *service.AuthService
	EnvironmentService *service.EnvironmentService
	OwnerService       *service.OwnerService
	RoleService        *service.RoleService
}

func corsLocalDev(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == "http://localhost:3000" {
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func NewRouter(deps Dependencies) *chi.Mux {
	router := chi.NewRouter()
	router.Use(corsLocalDev)
	router.Get("/health", handleHealth)
	router.Get("/resources", handleListResources(deps.ResourceService))
	router.Get("/resources/{id}", handleGetResource(deps.ResourceService))
	router.Get("/resources/{id}/relations", handleListResourceRelations(deps.RelationService))
	router.Get("/resources/{id}/audit-events", handleListResourceAuditEvents(deps.AuditService))
	router.Get("/audit-events", handleListAuditEvents(deps.AuditService))
	router.Post("/auth/login", handleLogin(deps.AuthService))
	router.Get("/environments", handleListEnvironments(deps.EnvironmentService))
	router.Get("/owners", handleListOwners(deps.OwnerService))
	router.Get("/roles", handleListRoles(deps.RoleService))
	return router
}
