// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: chi/v5, internal/service (all services)
// output: Dependencies struct, NewRouter, corsLocalDev
// pos: HTTP routing entry point, wires service dependencies to handlers
// note: if this file changes, update header and README.md
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/service"
)

type Dependencies struct {
	ResourceService        *service.ResourceService
	RelationService        *service.RelationService
	AuditService           *service.AuditService
	AuthService            *service.AuthService
	EnvironmentService     *service.EnvironmentService
	OwnerService           *service.OwnerService
	RoleService            *service.RoleService
	ResourceTypeService    *service.ResourceTypeService
	RelationTypeService    *service.RelationTypeService
	LifecycleStatusService *service.LifecycleStatusService
	HealthStatusService    *service.HealthStatusService
}

func corsLocalDev(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == "http://localhost:3000" {
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
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
	router.Post("/resources", handleCreateResource(deps.ResourceService))
	router.Get("/resources/{id}", handleGetResource(deps.ResourceService))
	router.Patch("/resources/{id}", handlePatchResource(deps.ResourceService))
	router.Get("/resources/{id}/profile", handleGetResourceProfile(deps.ResourceService))
	router.Get("/resources/{id}/relations", handleListResourceRelations(deps.RelationService))
	router.Post("/resources/{id}/relations", handleCreateResourceRelation(deps.RelationService))
	router.Delete("/resource-relations/{id}", handleDeleteResourceRelation(deps.RelationService))
	router.Get("/resources/{id}/audit-events", handleListResourceAuditEvents(deps.AuditService))
	router.Get("/audit-events", handleListAuditEvents(deps.AuditService))
	router.Post("/auth/login", handleLogin(deps.AuthService))
	router.Get("/environments", handleListEnvironments(deps.EnvironmentService))
	router.Get("/owners", handleListOwners(deps.OwnerService))
	router.Get("/roles", handleListRoles(deps.RoleService))
	router.Get("/resource-types", handleListResourceTypes(deps.ResourceTypeService))
	router.Get("/relation-types", handleListRelationTypes(deps.RelationTypeService))
	router.Get("/lifecycle-statuses", handleListLifecycleStatuses(deps.LifecycleStatusService))
	router.Get("/health-statuses", handleListHealthStatuses(deps.HealthStatusService))
	router.Get("/openapi.yaml", handleOpenAPIYAML)
	router.Get("/docs", handleDocs)
	return router
}
