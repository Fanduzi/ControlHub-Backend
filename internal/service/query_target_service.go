// Package service provides business logic for resource reads and typed profile assembly.
// input: internal/model (QueryTarget, QueryTargetListQuery, QueryCredentialMetadata, QueryEnvironmentPolicy), context, strings
// output: QueryTargetRepository + QueryCredentialReader interfaces, NewQueryTargetService, WithCredentialReader, QueryTargetService.List, classifyQueryKind, completeQueryTarget, credentialAllowsExecution, isExecutableEngine
// pos: Query target read model — sources database_instance targets and derives workbench context + Phase 37 readiness
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

// QueryCredentialReader reads credential metadata for a query target. The
// concrete implementation is the query execution repository; it is an optional
// dependency of QueryTargetService — when absent, targets stay in their Phase 36
// locked state (no execution readiness is derived).
type QueryCredentialReader interface {
	GetCredentialByResourceID(ctx context.Context, resourceID uint64) (model.QueryCredentialMetadata, error)
}

// QueryTargetService assembles the query target read model: it sources raw
// database_instance targets from the repository and derives the full workbench
// context (capability, readiness, governance, locked actions) for each.
type QueryTargetService struct {
	repo        QueryTargetRepository
	credentials QueryCredentialReader
}

// NewQueryTargetService creates a QueryTargetService backed by the given
// query target repository. Without a credential reader it preserves the Phase 36
// behavior (all targets locked).
func NewQueryTargetService(repo QueryTargetRepository) *QueryTargetService {
	return &QueryTargetService{repo: repo}
}

// WithCredentialReader returns a copy of the service wired to read credential
// metadata, which unlocks Phase 37 readiness derivation (a mysql/tidb target
// with an allowed credential becomes ready). The receiver is not mutated.
func (s *QueryTargetService) WithCredentialReader(reader QueryCredentialReader) *QueryTargetService {
	cp := *s
	cp.credentials = reader
	return &cp
}

// List returns query targets for the Query Workbench. Raw targets from the
// repository carry only identity and connection context; this method derives the
// remaining capability/readiness/governance/action state. When a credential
// reader is wired, mysql/tidb targets with an allowed credential are derived as
// ready (Phase 37); otherwise targets remain in their Phase 36 locked state.
func (s *QueryTargetService) List(ctx context.Context, q model.QueryTargetListQuery) ([]model.QueryTarget, error) {
	targets, err := s.repo.ListQueryTargets(ctx, q)
	if err != nil {
		return nil, err
	}
	for i := range targets {
		var cred *model.QueryCredentialMetadata
		if s.credentials != nil {
			if c, cErr := s.credentials.GetCredentialByResourceID(ctx, targets[i].ResourceID); cErr == nil {
				cred = &c
			}
			// No row, invalid ref, or read error -> nil credential -> locked.
		}
		targets[i] = completeQueryTarget(targets[i], cred)
	}
	return targets, nil
}

// querySafetyNoteLocked is the boundary message shown when execution is not
// available for a target.
const querySafetyNoteLocked = "Query execution is not enabled in this phase."

// querySafetyNoteFor returns the governance safety note appropriate to a
// readiness state. A ready target explicitly states execution is enabled; every
// other state stays on the locked boundary message.
func querySafetyNoteFor(readiness model.QueryTargetReadiness) string {
	if readiness == model.ReadinessReady {
		return "Read-only SELECT execution is enabled under the backend sandbox."
	}
	return querySafetyNoteLocked
}

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
// context (and optional credential metadata) into the full workbench context:
// capability, readiness, missing fields, governance, actions, and schema
// preview. It returns a new target and does not mutate its input.
//
// Derivation rules:
//   - known engine   -> sql/redis/mongo capability
//   - unknown engine -> unsupported (stays visible)
//   - empty engine / missing host / missing port -> missing_connection
//   - mysql/tidb with a complete connection and an allowed credential -> ready
//     (readonly_sandbox_enabled, Run action enabled)
//   - mysql/tidb with a credential that is disabled or policy-disallowed -> disabled
//   - otherwise -> credential_required
//   - schemaPreview is always empty (no live introspection, no stored names)
func completeQueryTarget(in model.QueryTarget, cred *model.QueryCredentialMetadata) model.QueryTarget {
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
	credentialState := "missing_readonly_credential"
	executionEnabled := false

	switch {
	case in.ConnectionContext.Engine != "" && out.Capability.QueryKind == model.QueryKindUnsupported:
		readiness = model.ReadinessUnsupportedEngine
		safety = model.SafetyStateUnsupportedEngine
	case len(missing) > 0:
		readiness = model.ReadinessMissingConnection
		safety = model.SafetyStateConnectionIncomplete
	case !isExecutableEngine(in.ConnectionContext.Engine):
		// SQL engine but not mysql/tidb (postgres/clickhouse): not executable in
		// Phase 37. Stays locked as credential_required (unchanged from Phase 36).
		readiness = model.ReadinessCredentialRequired
		safety = model.SafetyStateCredentialMissing
		missing = append(missing, "readonlyCredential")
	case cred != nil && credentialAllowsExecution(*cred, in.ConnectionContext.Engine, in.ConnectionContext.Environment):
		// READY: backend-enforced read-only SELECT sandbox.
		readiness = model.ReadinessReady
		safety = model.SafetyStateReadonlySandboxEnabled
		credentialState = "configured_readonly_credential"
		executionEnabled = true
		out.AvailableActions.Run = true
	case cred != nil:
		// Credential present but disabled or environment policy disallows -> locked.
		readiness = model.ReadinessDisabled
		safety = model.SafetyStateExecutionDisabled
	default:
		readiness = model.ReadinessCredentialRequired
		safety = model.SafetyStateCredentialMissing
		missing = append(missing, "readonlyCredential")
	}

	out.Readiness = readiness
	out.MissingFields = missing
	out.Governance = model.QueryTargetGovernance{
		ExecutionEnabled: executionEnabled,
		CredentialState:  credentialState,
		AuditRequired:    true,
		SafetyState:      safety,
		SafetyNote:       querySafetyNoteFor(readiness),
		PolicyNotes:      policyNotesFor(readiness, in.ConnectionContext.Environment),
	}
	return out
}

// isExecutableEngine reports whether an engine is supported for Phase 37
// read-only execution (MySQL/TiDB only). Other SQL engines (postgres/clickhouse)
// are known but deferred to a later phase.
func isExecutableEngine(engine string) bool {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "mysql", "tidb":
		return true
	}
	return false
}

// credentialAllowsExecution applies the credential binding + environment-policy
// matrix. It is shared by readiness derivation (QueryTargetService) and execute
// gating (QueryExecutionService) so the two paths agree. A credential whose
// engine does not match the target engine, an unknown/empty policy, or a
// disabled credential fails closed (locked / rejected). Production is executable
// only under all_environments.
func credentialAllowsExecution(cred model.QueryCredentialMetadata, targetEngine, environment string) bool {
	if !cred.Enabled {
		return false
	}
	if !engineHostMatches(cred.Engine, targetEngine) {
		// The credential is for a different engine than the selected target.
		return false
	}
	if err := cred.EnvironmentPolicy.Validate(); err != nil {
		return false
	}
	switch cred.EnvironmentPolicy {
	case model.QueryEnvPolicyAllEnvironments:
		return true
	case model.QueryEnvPolicyNonProdOnly:
		return !isProductionEnvironment(environment)
	default:
		return false
	}
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

// isProductionEnvironment is a heuristic over the resolved environment name. In
// Phase 37 it gates execution policy (non_prod_only credentials and tighter
// timeout/row limits) as well as advisory copy. The resolved environment name is
// the only signal available — there is no explicit is_production flag.
func isProductionEnvironment(environment string) bool {
	env := strings.ToLower(strings.TrimSpace(environment))
	return env == "production" || strings.HasPrefix(env, "prod")
}
