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
	TopologyService        *service.TopologyService
	AuditService           *service.AuditService
	AuthService            *service.AuthService
	EnvironmentService     *service.EnvironmentService
	OwnerService           *service.OwnerService
	RoleService            *service.RoleService
	ResourceTypeService    *service.ResourceTypeService
	RelationTypeService    *service.RelationTypeService
	LifecycleStatusService *service.LifecycleStatusService
	HealthStatusService    *service.HealthStatusService
	ResourceSubtypeService *service.ResourceSubtypeService
	ProfileService         *service.ProfileService
	QueryTargetService     *service.QueryTargetService
	// Query execution (Phase 37). QueryExecutionService is the thin interface
	// the handlers depend on; the concrete *service.QueryExecutionService
	// satisfies it. QueryExecutionAuth carries the bounded token-freshness TTL.
	QueryExecutionService queryExecutionAPI
	QueryExecutionAuth    QueryExecutionAuthConfig
	// Query credential metadata (Phase 38A). queryCredentialAPI is the thin
	// interface the handlers depend on; the concrete *service.QueryCredentialService
	// satisfies it. All three routes require a fresh bearer token; PUT/DELETE
	// additionally require the admin role (enforced in the handler).
	QueryCredentialService queryCredentialAPI
	// Query schema metadata (Phase 38I). querySchemaAPI is the thin interface
	// the handlers depend on; the concrete *service.QuerySchemaService satisfies
	// it. All three routes require a fresh bearer token.
	QuerySchemaService querySchemaAPI
	// Query explain (Phase 38N). queryExplainAPI is the thin interface the
	// handlers depend on; the concrete *service.QueryExplainService satisfies
	// it. The route requires a fresh bearer token (same freshness policy as
	// query execution). Explain is a distinct governed operation: it never
	// executes the bare SELECT and never creates a query_executions row.
	QueryExplainService queryExplainAPI
}

func corsLocalDev(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") == "http://localhost:3000" {
			w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3000")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
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
	router.Post("/resources/{id}/archive", handleArchiveResource(deps.ResourceService))
	router.Post("/resources/{id}/unarchive", handleUnarchiveResource(deps.ResourceService))
	router.Get("/resources/{id}/profile", handleGetResourceProfile(deps.ResourceService))
	router.Put("/resources/{id}/profile", handlePutResourceProfile(deps.ProfileService))
	router.Patch("/resources/{id}/profile", handlePatchResourceProfile(deps.ProfileService))
	router.Delete("/resources/{id}/profile", handleDeleteResourceProfile(deps.ProfileService))
	router.Get("/resources/{id}/relations", handleListResourceRelations(deps.RelationService))
	router.Get("/resources/{id}/members", handleGetResourceMembers(deps.RelationService))
	router.Get("/resources/{id}/topology", handleGetTopology(deps.TopologyService))
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
	router.Get("/resource-subtypes", handleListResourceSubtypes(deps.ResourceSubtypeService))
	router.Get("/query-targets", handleListQueryTargets(deps.QueryTargetService))
	router.Get("/openapi.yaml", handleOpenAPIYAML)
	router.Get("/docs", handleDocs)

	// Query execution routes (Phase 37) require a fresh bearer token via
	// requireFreshQueryActor (base signature/structure check + bounded TTL). The
	// base requireAuthenticatedActor is NOT mounted here because it does not
	// enforce token freshness. Existing read/list routes are unchanged.
	//
	// The group is created when EITHER the execute or explain service is
	// configured, so Explain does not accidentally depend on the execute
	// service being wired (Oracle P2.9).
	if deps.QueryExecutionService != nil || deps.QueryExplainService != nil {
		router.Group(func(r chi.Router) {
			r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth))
			if deps.QueryExecutionService != nil {
				r.Post("/query-targets/{id}/execute", handleExecuteQuery(deps.QueryExecutionService))
				r.Get("/query-targets/{id}/executions", handleListQueryExecutions(deps.QueryExecutionService))
				r.Post("/query-targets/{id}/related-records", handleNavigateRelatedRecords(deps.QueryExecutionService))
			}
			if deps.QueryExplainService != nil {
				r.Post("/query-targets/{id}/explain", handleExplainQuery(deps.QueryExplainService))
			}
		})
	}
	// Query credential metadata routes (Phase 38A). All three require a fresh
	// bearer token (same freshness policy as query execution). PUT/DELETE enforce
	// the admin role inside the handler; GET is available to any authenticated
	// actor. The response/contract carries metadata only — never a DSN.
	if deps.QueryCredentialService != nil {
		router.Group(func(r chi.Router) {
			r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth))
			r.Get("/query-targets/{id}/credential", handleGetQueryCredential(deps.QueryCredentialService))
			r.Put("/query-targets/{id}/credential", handlePutQueryCredential(deps.QueryCredentialService))
			r.Delete("/query-targets/{id}/credential", handleDeleteQueryCredential(deps.QueryCredentialService))
		})
	}
	// Query schema metadata routes (Phase 38I). All three require a fresh
	// bearer token (same freshness policy as query execution).
	if deps.QuerySchemaService != nil {
		router.Group(func(r chi.Router) {
			r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth))
			r.Get("/query-targets/{id}/schema/databases", handleListSchemaDatabases(deps.QuerySchemaService))
			r.Get("/query-targets/{id}/schema/objects", handleListSchemaObjects(deps.QuerySchemaService))
			r.Get("/query-targets/{id}/schema/object-details", handleGetObjectDetails(deps.QuerySchemaService))
			r.Get("/query-targets/{id}/schema/table-definition", handleGetTableDefinition(deps.QuerySchemaService))
		})
	}
	return router
}
