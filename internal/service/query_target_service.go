// Package service provides business logic for resource reads and typed profile assembly.
// input: internal/model (QueryTarget, QueryTargetListQuery), context, strings
// output: QueryTargetRepository interface, NewQueryTargetService, QueryTargetService.List, classifyQueryKind, completeQueryTarget
// pos: Query target read model — sources database_instance targets and derives workbench context
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"strings"

	"github.com/fan/controlhub/internal/model"
)

// QueryTargetRepository defines the data access interface for the query target
// read model. The concrete MySQL implementation lives in
// internal/repository/mysql/query_target_repository.go.
type QueryTargetRepository interface {
	ListQueryTargets(ctx context.Context, q model.QueryTargetListQuery) ([]model.QueryTarget, error)
}

// QueryTargetService assembles the query target read model: it sources raw
// database_instance targets from the repository and derives the full workbench
// context (capability, readiness, governance, locked actions) for each.
type QueryTargetService struct {
	repo QueryTargetRepository
}

// NewQueryTargetService creates a QueryTargetService backed by the given
// query target repository.
func NewQueryTargetService(repo QueryTargetRepository) *QueryTargetService {
	return &QueryTargetService{repo: repo}
}

// List returns query targets for the locked Query Workbench shell. Raw targets
// from the repository carry only identity and connection context; this method
// derives the remaining capability/readiness/governance/action state.
func (s *QueryTargetService) List(ctx context.Context, q model.QueryTargetListQuery) ([]model.QueryTarget, error) {
	targets, err := s.repo.ListQueryTargets(ctx, q)
	if err != nil {
		return nil, err
	}
	for i := range targets {
		targets[i] = completeQueryTarget(targets[i])
	}
	return targets, nil
}

// querySafetyNote is the fixed Phase 36 boundary message. It must make clear
// that query execution is not enabled, regardless of target readiness.
const querySafetyNote = "Query execution is not enabled in this phase."

// classifyQueryKind maps a database engine to the query kind a future workbench
// would use. Unknown or empty engines resolve to unsupported so they stay
// visible instead of vanishing.
func classifyQueryKind(engine string) model.QueryKind {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "mysql", "tidb", "postgresql", "clickhouse":
		return model.QueryKindSQL
	case "redis":
		return model.QueryKindRedis
	case "mongodb":
		return model.QueryKindMongo
	default:
		return model.QueryKindUnsupported
	}
}

// capabilityFor derives the editor mode and language label for a query kind so
// the frontend never has to guess from raw engine strings.
func capabilityFor(kind model.QueryKind) model.QueryTargetCapability {
	switch kind {
	case model.QueryKindSQL:
		return model.QueryTargetCapability{QueryKind: kind, EditorMode: "sql", LanguageLabel: "SQL"}
	case model.QueryKindRedis:
		return model.QueryTargetCapability{QueryKind: kind, EditorMode: "redis", LanguageLabel: "Redis command"}
	case model.QueryKindMongo:
		return model.QueryTargetCapability{QueryKind: kind, EditorMode: "mongo", LanguageLabel: "Mongo query"}
	default:
		return model.QueryTargetCapability{QueryKind: kind, EditorMode: "text", LanguageLabel: "Unsupported"}
	}
}

// completeQueryTarget is the pure derivation that turns a target's connection
// context into the full workbench context: capability, readiness, missing
// fields, governance, locked actions, and schema preview. It returns a new
// target and does not mutate its input.
//
// Derivation rules:
//   - known engine   -> sql/redis/mongo capability
//   - unknown engine -> unsupported (stays visible)
//   - empty engine / missing host / missing port -> missing_connection
//   - complete connection, no read-only credential metadata -> credential_required
//   - executionEnabled is always false; all availableActions are always false
//   - schemaPreview is always empty (no live introspection, no stored names)
func completeQueryTarget(in model.QueryTarget) model.QueryTarget {
	out := in
	out.Capability = capabilityFor(classifyQueryKind(in.ConnectionContext.Engine))
	out.AvailableActions = model.QueryTargetAvailableActions{}
	// Non-nil empty slices serialize to [] (not null), matching the OpenAPI
	// contract that requires these as non-nullable arrays.
	out.SchemaPreview = []model.QueryTargetSchemaPreviewNode{}

	missing := make([]string, 0, 4)
	if in.ConnectionContext.Engine == "" {
		missing = append(missing, "engine")
	}
	if in.ConnectionContext.Host == "" {
		missing = append(missing, "host")
	}
	if in.ConnectionContext.Port == 0 {
		missing = append(missing, "port")
	}

	var readiness model.QueryTargetReadiness
	var safety model.QueryTargetSafetyState
	switch {
	case in.ConnectionContext.Engine != "" && out.Capability.QueryKind == model.QueryKindUnsupported:
		readiness = model.ReadinessUnsupportedEngine
		safety = model.SafetyStateUnsupportedEngine
	case len(missing) > 0:
		readiness = model.ReadinessMissingConnection
		safety = model.SafetyStateConnectionIncomplete
	default:
		readiness = model.ReadinessCredentialRequired
		safety = model.SafetyStateCredentialMissing
		missing = append(missing, "readonlyCredential")
	}

	out.Readiness = readiness
	out.MissingFields = missing
	out.Governance = model.QueryTargetGovernance{
		ExecutionEnabled: false,
		CredentialState:  "missing_readonly_credential",
		AuditRequired:    true,
		SafetyState:      safety,
		SafetyNote:       querySafetyNote,
		PolicyNotes:      policyNotesFor(readiness, in.ConnectionContext.Environment),
	}
	return out
}

// policyNotesFor returns short, human-readable reasons or future constraints
// for the governance panel.
func policyNotesFor(readiness model.QueryTargetReadiness, environment string) []string {
	var notes []string
	switch readiness {
	case model.ReadinessUnsupportedEngine:
		notes = append(notes, "Engine is not supported for query execution.")
	case model.ReadinessMissingConnection:
		notes = append(notes, "Connection metadata is incomplete before execution can be considered.")
	default:
		notes = append(notes, "Read-only credentials are required before execution.")
	}
	if isProductionEnvironment(environment) {
		notes = append(notes, "Production queries require stricter defaults.")
	}
	return notes
}

// isProductionEnvironment is a soft, display-only heuristic over the resolved
// environment name. It only affects advisory copy, never execution policy.
func isProductionEnvironment(environment string) bool {
	env := strings.ToLower(strings.TrimSpace(environment))
	return env == "production" || strings.HasPrefix(env, "prod")
}
