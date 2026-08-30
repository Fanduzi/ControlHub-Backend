// Package api provides HTTP handlers and routing for the ControlHub REST API.
// input: chi/v5, time, internal/model, internal/service (all services)
// output: Dependencies, NewRouter, CORS, user-or-machine scoped reads/collector writes/ordinary governed execute, and user-only workspace, sibling query, health observation, bulk mutation, ingestion, and admin routes
// pos: HTTP routing entry point for the closed machine route/scope matrix and authenticated inventory, collector, named-view, topology, workspace, query, and admin operations
// note: if this file changes, update this header and module README.md.
package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/fan/controlhub/internal/model"
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
	QueryExecutionService          queryExecutionAPI
	QueryExecutionStatementService queryExecutionStatementAPI
	QueryExecutionAuth             QueryExecutionAuthConfig
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
	// Query disclosure policies (Phase 38Q). queryDisclosureAPI is the thin
	// interface the handlers depend on; the concrete
	// *service.QueryDisclosureService satisfies it. All four routes require a
	// fresh bearer token; POST/PUT/DELETE additionally require the admin role
	// (enforced in the handler).
	QueryDisclosureService queryDisclosureAPI
	// Query saved statements (Phase 38R). querySavedStatementAPI is the thin
	// interface the handlers depend on; the concrete
	// *service.QuerySavedStatementService satisfies it. All four routes require
	// a fresh bearer token (same freshness policy as query execution).
	QuerySavedStatementService querySavedStatementAPI
	QueryWorkspaceService      queryWorkspaceAPI
	NamedInventoryViewService  namedInventoryViewAPI
	MachinePrincipalService    machinePrincipalAPI
	MachineCredentialService   machineCredentialAPI
	// AuthAuditEmitter records authentication and authorization outcomes.
	// Nil is treated as NoopEmitter; fail-open semantics apply.
	AuthAuditEmitter service.AuthAuditEmitter
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
	emitter := deps.AuthAuditEmitter
	if emitter == nil {
		emitter = service.NoopEmitter{}
	}
	// Bound untrusted Bearer rejection persistence at 60/min per process
	// (ADR 2026-08-15): the decorator caps only auth.bearer/rejected events
	// with no verified actor; logins, verified-actor rejections, and role
	// denials pass through unbounded. Missing Authorization emits no event at
	// all (handled in verifyBearer). All routers in the process share one
	// budget, so the bound holds per server process.
	emitter = service.NewBoundedAuthAuditEmitter(emitter, service.ProcessBearerRejectBudget)

	router := chi.NewRouter()
	router.Use(corsLocalDev)
	router.Get("/health", handleHealth)
	router.Post("/auth/login", handleLogin(deps.AuthService, emitter))
	router.Get("/openapi.yaml", handleOpenAPIYAML)
	router.Get("/docs", handleDocs)
	requireUser := requireAuthenticatedActor(deps.AuthService, deps.QueryExecutionAuth, emitter)
	requireAdmin := func(next http.Handler) http.Handler {
		return requireUser(requireAdminActor(emitter)(next))
	}
	router.Group(func(r chi.Router) {
		r.Use(requireUserOrMachineCredential(deps.MachineCredentialService, model.MachineScopeInventoryRead, requireUser))
		r.Get("/resources", handleListResources(deps.ResourceService))
		r.Get("/resources/{id}", handleGetResource(deps.ResourceService))
		r.Get("/resources/{id}/profile", handleGetResourceProfile(deps.ResourceService))
		r.Get("/resources/{id}/effective-values", handleGetResourceEffectiveValues(deps.ResourceService))
		r.Get("/environments", handleListEnvironments(deps.EnvironmentService))
		r.Get("/owners", handleListOwners(deps.OwnerService))
		r.Get("/roles", handleListRoles(deps.RoleService))
		r.Get("/resource-types", handleListResourceTypes(deps.ResourceTypeService))
		r.Get("/relation-types", handleListRelationTypes(deps.RelationTypeService))
		r.Get("/lifecycle-statuses", handleListLifecycleStatuses(deps.LifecycleStatusService))
		r.Get("/health-statuses", handleListHealthStatuses(deps.HealthStatusService))
		r.Get("/resource-subtypes", handleListResourceSubtypes(deps.ResourceSubtypeService))
	})
	router.Group(func(r chi.Router) {
		r.Use(requireUserOrMachineCredential(deps.MachineCredentialService, model.MachineScopeRelationsRead, requireUser))
		r.Get("/resources/{id}/relations", handleListResourceRelations(deps.RelationService))
		r.Get("/resources/{id}/relation-rules", handleGetResourceRelationRules(deps.RelationService))
		r.Get("/resources/{id}/members", handleGetResourceMembers(deps.RelationService))
		r.Get("/resources/{id}/topology", handleGetTopology(deps.TopologyService))
		r.Get("/environments/{id}/topology", handleGetEnvironmentTopology(deps.TopologyService))
	})
	router.Group(func(r chi.Router) {
		r.Use(requireUserOrMachineCredential(deps.MachineCredentialService, model.MachineScopeGovernedSelect, requireUser))
		r.Get("/query-targets", handleListQueryTargets(deps.QueryTargetService))
	})
	if deps.NamedInventoryViewService != nil {
		router.With(requireUserOrMachineCredential(deps.MachineCredentialService, model.MachineScopeNamedViewsRead, requireUser)).
			Get("/inventory/views", handleListNamedInventoryViews(deps.NamedInventoryViewService))
		router.With(requireUser).Post("/inventory/views", handleCreateNamedInventoryView(deps.NamedInventoryViewService))
		router.With(requireUser).Put("/inventory/views/{viewId}", handleUpdateNamedInventoryView(deps.NamedInventoryViewService))
		router.With(requireUser).Delete("/inventory/views/{viewId}", handleDeleteNamedInventoryView(deps.NamedInventoryViewService))
	}
	router.Group(func(r chi.Router) {
		r.Use(requireUserOrMachineCredential(deps.MachineCredentialService, model.MachineScopeAuditRead, requireAdmin))
		r.Get("/resources/{id}/audit-events", handleListResourceAuditEvents(deps.AuditService))
		r.Get("/audit-events", handleListAuditEvents(deps.AuditService))
	})
	router.With(requireUserOrMachineCredential(deps.MachineCredentialService, model.MachineScopeInventoryIngest, requireAdmin)).
		Post("/admin/ingestions/preview", handlePreviewIngestion(deps.ResourceService))
	router.With(requireUserOrMachineCredential(deps.MachineCredentialService, model.MachineScopeInventoryIngest, requireAdmin)).
		Post("/admin/ingestions/confirm", handleConfirmIngestion(deps.ResourceService))
	router.With(requireUserOrMachineCredential(deps.MachineCredentialService, model.MachineScopeHealthWrite, requireAdmin)).
		Post("/resources/{id}/health-observations", handleRecordHealthObservation(deps.ResourceService))
	router.Group(func(r chi.Router) {
		r.Use(requireUser)
		if deps.QueryWorkspaceService != nil {
			r.Get("/query-workspace", handleGetQueryWorkspace(deps.QueryWorkspaceService))
			r.Put("/query-workspace", handlePutQueryWorkspace(deps.QueryWorkspaceService))
		}
		r.Group(func(r chi.Router) {
			r.Use(requireAdminActor(emitter))
			r.Post("/resources", handleCreateResource(deps.ResourceService))
			r.Patch("/resources/{id}", handlePatchResource(deps.ResourceService))
			r.Put("/resources/{id}/overrides/{field}", handleSetResourceOverride(deps.ResourceService))
			r.Delete("/resources/{id}/overrides/{field}", handleClearResourceOverride(deps.ResourceService))
			r.Post("/resources/bulk-mutations/preview", handlePreviewBulkResourceMutation(deps.ResourceService))
			r.Post("/resources/bulk-mutations/confirm", handleConfirmBulkResourceMutation(deps.ResourceService))
			r.Post("/resources/{id}/archive", handleArchiveResource(deps.ResourceService))
			r.Post("/resources/{id}/unarchive", handleUnarchiveResource(deps.ResourceService))
			r.Put("/resources/{id}/profile", handlePutResourceProfile(deps.ProfileService))
			r.Patch("/resources/{id}/profile", handlePatchResourceProfile(deps.ProfileService))
			r.Delete("/resources/{id}/profile", handleDeleteResourceProfile(deps.ProfileService))
			r.Post("/resources/{id}/relations", handleCreateResourceRelation(deps.RelationService))
			r.Delete("/resource-relations/{id}", handleDeleteResourceRelation(deps.RelationService))
			r.Get("/admin/legacy-hash-count", handleGetLegacyHashCount(deps.AuthService))
			r.Get("/ops/auth-audit-metrics", handleAuthAuditMetrics())
			r.Get("/ops/query-evidence-metrics", handleQueryEvidenceMetrics(deps.QueryExecutionService))
			if deps.MachinePrincipalService != nil {
				r.Get("/admin/machine-principals", handleListMachinePrincipals(deps.MachinePrincipalService))
				r.Post("/admin/machine-principals", handleCreateMachinePrincipal(deps.MachinePrincipalService))
				r.Post("/admin/machine-credentials/{credentialId}/rotate", handleRotateMachineCredential(deps.MachinePrincipalService))
				r.Post("/admin/machine-credentials/{credentialId}/revoke", handleRevokeMachineCredential(deps.MachinePrincipalService))
			}
		})
	})

	// Ordinary governed SELECT accepts either a scoped Machine Credential or a
	// fresh current User bearer. Machine evidence uses the principal ID, never
	// the credential ID or a fabricated User.
	if deps.QueryExecutionService != nil {
		router.With(requireUserOrMachineCredential(
			deps.MachineCredentialService,
			model.MachineScopeGovernedSelect,
			requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth, emitter),
		)).Post("/query-targets/{id}/execute", handleExecuteQuery(deps.QueryExecutionService))
	}

	// Sibling query routes remain fresh-User-only via requireFreshQueryActor
	// (base signature/current Authorization Version check + bounded TTL).
	// All protected routes, including inventory and dictionary reads above,
	// enforce the fixed MaxQueryTokenAge freshness bound (Issue #21).
	//
	// The group is created when EITHER the execute or explain service is
	// configured, so Explain does not accidentally depend on the execute
	// service being wired (Oracle P2.9).
	if deps.QueryExecutionService != nil || deps.QueryExecutionStatementService != nil || deps.QueryExplainService != nil {
		router.Group(func(r chi.Router) {
			r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth, emitter))
			if deps.QueryExecutionService != nil {
				r.Get("/query-targets/{id}/executions", handleListQueryExecutions(deps.QueryExecutionService))
				r.Post("/query-targets/{id}/related-records", handleNavigateRelatedRecords(deps.QueryExecutionService))
			}
			if deps.QueryExecutionStatementService != nil {
				r.Get("/query-targets/{id}/executions/{executionId}/statement", handleGetQueryExecutionStatement(deps.QueryExecutionStatementService))
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
			r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth, emitter))
			r.Get("/query-targets/{id}/credential", handleGetQueryCredential(deps.QueryCredentialService))
			r.Group(func(r chi.Router) {
				r.Use(requireAdminActor(emitter))
				r.Put("/query-targets/{id}/credential", handlePutQueryCredential(deps.QueryCredentialService))
				r.Delete("/query-targets/{id}/credential", handleDeleteQueryCredential(deps.QueryCredentialService))
			})
		})
	}
	// Query schema metadata routes (Phase 38I). All three require a fresh
	// bearer token (same freshness policy as query execution).
	if deps.QuerySchemaService != nil {
		router.Group(func(r chi.Router) {
			r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth, emitter))
			r.Get("/query-targets/{id}/schema/databases", handleListSchemaDatabases(deps.QuerySchemaService))
			r.Get("/query-targets/{id}/schema/objects", handleListSchemaObjects(deps.QuerySchemaService))
			r.Get("/query-targets/{id}/schema/object-details", handleGetObjectDetails(deps.QuerySchemaService))
			r.Get("/query-targets/{id}/schema/table-definition", handleGetTableDefinition(deps.QuerySchemaService))
			r.Get("/query-targets/{id}/schema/relationship-map", handleGetRelationshipMap(deps.QuerySchemaService))
		})
	}
	// Query disclosure policy routes (Phase 38Q). All four require a fresh
	// bearer token (same freshness policy as query execution). All four,
	// including GET, enforce the admin role inside the handler.
	if deps.QueryDisclosureService != nil {
		router.Group(func(r chi.Router) {
			r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth, emitter))
			r.Get("/query-disclosure-policies", handleListPolicies(deps.QueryDisclosureService))
			r.Post("/query-disclosure-policies", handleCreatePolicy(deps.QueryDisclosureService))
			r.Put("/query-disclosure-policies", handleUpdatePolicy(deps.QueryDisclosureService))
			r.Delete("/query-disclosure-policies", handleDeletePolicy(deps.QueryDisclosureService))
		})
	}
	// Query saved statement routes (Phase 38R). All require a fresh
	// bearer token (same freshness policy as query execution). The
	// template-execution route (Phase 38W) lives in this group and is
	// registered only when the execution service is wired.
	if deps.QuerySavedStatementService != nil {
		router.Group(func(r chi.Router) {
			r.Use(requireFreshQueryActor(deps.AuthService, deps.QueryExecutionAuth, emitter))
			r.Get("/query-targets/{id}/saved-statements", handleListSavedStatements(deps.QuerySavedStatementService))
			r.Post("/query-targets/{id}/saved-statements", handleCreateSavedStatement(deps.QuerySavedStatementService))
			r.Put("/query-targets/{id}/saved-statements/{statementId}", handleUpdateSavedStatement(deps.QuerySavedStatementService))
			r.Delete("/query-targets/{id}/saved-statements/{statementId}", handleDeleteSavedStatement(deps.QuerySavedStatementService))
			if deps.QueryExecutionService != nil {
				r.Post("/query-targets/{id}/saved-statements/{statementId}/execute", handleExecuteSavedStatement(deps.QueryExecutionService))
			}
		})
	}
	return router
}
