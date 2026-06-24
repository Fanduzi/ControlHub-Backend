// Package service provides business logic for the query target read model.
// input: testing, internal/model
// output: TestClassifyQueryKind, TestCompleteQueryTarget_*
// pos: Unit tests for the pure query target derivation (engine -> capability/readiness/governance)
// note: if this file changes, update header and README.md
package service

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestClassifyQueryKind(t *testing.T) {
	t.Parallel()
	cases := []struct {
		engine string
		want   model.QueryKind
	}{
		{"mysql", model.QueryKindSQL},
		{"tidb", model.QueryKindSQL},
		{"postgresql", model.QueryKindSQL},
		{"clickhouse", model.QueryKindSQL},
		{"redis", model.QueryKindRedis},
		{"mongodb", model.QueryKindMongo},
		// Unknown engines must stay visible as unsupported, never vanish.
		{"oracle", model.QueryKindUnsupported},
		{"", model.QueryKindUnsupported},
		// Matching is case-insensitive; seed data stores engines lowercase.
		{"MySQL", model.QueryKindSQL},
		{"ClickHouse", model.QueryKindSQL},
		{"PostgreSQL", model.QueryKindSQL},
	}
	for _, tc := range cases {
		got := classifyQueryKind(tc.engine)
		if got != tc.want {
			t.Errorf("classifyQueryKind(%q) = %q, want %q", tc.engine, got, tc.want)
		}
	}
}

// baseTarget builds a query target with identity + a complete mysql connection
// so individual tests can mutate the field under test.
func baseTarget() model.QueryTarget {
	return model.QueryTarget{
		ResourceID:   22,
		ResourceName: "analytics-ch-node-01-prod",
		DisplayName:  "Analytics ClickHouse Node 01 Production",
		ResourceType: model.ResourceTypeDatabaseInstance,
		ConnectionContext: model.QueryTargetConnectionContext{
			Environment: "Production",
			Owner:       "DBA Team",
			Engine:      "clickhouse",
			Host:        "prod-ch-host-01.internal",
			Port:        8123,
			ClusterID:   14,
			ClusterName: "Analytics ClickHouse Cluster Production",
		},
	}
}

func TestCompleteQueryTarget_CompleteConnectionBecomesCredentialRequired(t *testing.T) {
	t.Parallel()
	got := completeQueryTarget(baseTarget(), nil)

	// WHY: a target with engine+host+port still has no read-only credential
	// metadata in Phase 36, so it must surface as credential_required — never
	// ready. Returning ready here would imply query execution is possible.
	if got.Readiness != model.ReadinessCredentialRequired {
		t.Fatalf("readiness = %q, want credential_required", got.Readiness)
	}
	if got.Governance.SafetyState != model.SafetyStateCredentialMissing {
		t.Fatalf("safetyState = %q, want credential_missing", got.Governance.SafetyState)
	}
	if !slices.Contains(got.MissingFields, "readonlyCredential") {
		t.Fatalf("missingFields = %v, want to contain readonlyCredential", got.MissingFields)
	}
}

func TestCompleteQueryTarget_MissingHostBecomesMissingConnection(t *testing.T) {
	t.Parallel()
	in := baseTarget()
	in.ConnectionContext.Host = ""

	got := completeQueryTarget(in, nil)

	if got.Readiness != model.ReadinessMissingConnection {
		t.Fatalf("readiness = %q, want missing_connection", got.Readiness)
	}
	if got.Governance.SafetyState != model.SafetyStateConnectionIncomplete {
		t.Fatalf("safetyState = %q, want connection_incomplete", got.Governance.SafetyState)
	}
	if !slices.Contains(got.MissingFields, "host") {
		t.Fatalf("missingFields = %v, want to contain host", got.MissingFields)
	}
}

func TestCompleteQueryTarget_MissingPortBecomesMissingConnection(t *testing.T) {
	t.Parallel()
	in := baseTarget()
	in.ConnectionContext.Port = 0

	got := completeQueryTarget(in, nil)

	if got.Readiness != model.ReadinessMissingConnection {
		t.Fatalf("readiness = %q, want missing_connection", got.Readiness)
	}
	if !slices.Contains(got.MissingFields, "port") {
		t.Fatalf("missingFields = %v, want to contain port", got.MissingFields)
	}
}

func TestCompleteQueryTarget_MissingEngineBecomesMissingConnection(t *testing.T) {
	t.Parallel()
	in := baseTarget()
	in.ConnectionContext.Engine = ""

	got := completeQueryTarget(in, nil)

	// WHY: an empty engine is a connection-config gap (missing_connection),
	// distinct from a known-but-unsupported engine (unsupported_engine).
	if got.Readiness != model.ReadinessMissingConnection {
		t.Fatalf("readiness = %q, want missing_connection", got.Readiness)
	}
	if !slices.Contains(got.MissingFields, "engine") {
		t.Fatalf("missingFields = %v, want to contain engine", got.MissingFields)
	}
}

func TestCompleteQueryTarget_UnknownEngineStaysVisibleAsUnsupported(t *testing.T) {
	t.Parallel()
	in := baseTarget()
	in.ConnectionContext.Engine = "oracle"

	got := completeQueryTarget(in, nil)

	// WHY: unknown engines must remain visible so operators can see them,
	// never silently dropped.
	if got.Capability.QueryKind != model.QueryKindUnsupported {
		t.Fatalf("queryKind = %q, want unsupported", got.Capability.QueryKind)
	}
	if got.Readiness != model.ReadinessUnsupportedEngine {
		t.Fatalf("readiness = %q, want unsupported_engine", got.Readiness)
	}
	if got.Governance.SafetyState != model.SafetyStateUnsupportedEngine {
		t.Fatalf("safetyState = %q, want unsupported_engine", got.Governance.SafetyState)
	}
}

func TestCompleteQueryTarget_CapabilityPerEngine(t *testing.T) {
	t.Parallel()
	cases := []struct {
		engine        string
		queryKind     model.QueryKind
		editorMode    string
		languageLabel string
	}{
		{"mysql", model.QueryKindSQL, "sql", "SQL"},
		{"tidb", model.QueryKindSQL, "sql", "SQL"},
		{"postgresql", model.QueryKindSQL, "sql", "SQL"},
		{"clickhouse", model.QueryKindSQL, "sql", "SQL"},
		{"redis", model.QueryKindRedis, "redis", "Redis command"},
		{"mongodb", model.QueryKindMongo, "mongo", "Mongo query"},
		{"oracle", model.QueryKindUnsupported, "text", "Unsupported"},
	}
	for _, tc := range cases {
		in := baseTarget()
		in.ConnectionContext.Engine = tc.engine
		got := completeQueryTarget(in, nil)

		// editorMode must agree with queryKind so the frontend never has to
		// guess the editor language from two divergent fields.
		if got.Capability.QueryKind != tc.queryKind {
			t.Errorf("engine %q: queryKind = %q, want %q", tc.engine, got.Capability.QueryKind, tc.queryKind)
		}
		if got.Capability.EditorMode != tc.editorMode {
			t.Errorf("engine %q: editorMode = %q, want %q", tc.engine, got.Capability.EditorMode, tc.editorMode)
		}
		if got.Capability.LanguageLabel != tc.languageLabel {
			t.Errorf("engine %q: languageLabel = %q, want %q", tc.engine, got.Capability.LanguageLabel, tc.languageLabel)
		}
	}
}

func TestCompleteQueryTarget_GovernanceExecutionAlwaysDisabled(t *testing.T) {
	t.Parallel()
	// Every readiness path must report execution disabled and audit required.
	// If execution ever flips true, the Phase 36 no-execution boundary is broken.
	engines := []string{"mysql", "redis", "mongodb", "oracle"}
	for _, engine := range engines {
		in := baseTarget()
		in.ConnectionContext.Engine = engine
		got := completeQueryTarget(in, nil)

		if got.Governance.ExecutionEnabled {
			t.Errorf("engine %q: executionEnabled = true, want false", engine)
		}
		if !got.Governance.AuditRequired {
			t.Errorf("engine %q: auditRequired = false, want true", engine)
		}
		if got.Governance.CredentialState != "missing_readonly_credential" {
			t.Errorf("engine %q: credentialState = %q, want missing_readonly_credential", engine, got.Governance.CredentialState)
		}
		if got.Governance.SafetyNote == "" {
			t.Errorf("engine %q: expected non-empty safetyNote", engine)
		}
	}
}

func TestCompleteQueryTarget_AvailableActionsAllLocked(t *testing.T) {
	t.Parallel()
	got := completeQueryTarget(baseTarget(), nil)

	// No action may be available in Phase 36: there is no execution, sheet,
	// export, or access API. The frontend must render these as locked.
	aa := got.AvailableActions
	if aa.Run || aa.Explain || aa.Export || aa.SaveSheet || aa.RequestAccess {
		t.Fatalf("availableActions must all be false, got %+v", aa)
	}
}

func TestCompleteQueryTarget_SchemaPreviewEmptyWithoutMetadata(t *testing.T) {
	t.Parallel()
	got := completeQueryTarget(baseTarget(), nil)

	// WHY: Phase 36 never introspects live databases and ControlHub stores no
	// database/table/collection names, so schemaPreview must be empty rather
	// than fabricated.
	if len(got.SchemaPreview) != 0 {
		t.Fatalf("schemaPreview = %v, want empty", got.SchemaPreview)
	}
}

func TestCompleteQueryTarget_DoesNotMutateInput(t *testing.T) {
	t.Parallel()
	in := baseTarget()
	before := in.ConnectionContext
	_ = completeQueryTarget(in, nil)

	// Immutability: derivation must not mutate the caller's connection context.
	if in.ConnectionContext != before {
		t.Fatalf("completeQueryTarget mutated input connection context")
	}
}

func TestCompleteQueryTarget_PreservesIdentityAndConnection(t *testing.T) {
	t.Parallel()
	in := baseTarget()
	got := completeQueryTarget(in, nil)

	if got.ResourceID != in.ResourceID || got.ResourceName != in.ResourceName ||
		got.DisplayName != in.DisplayName || got.ResourceType != in.ResourceType {
		t.Fatalf("identity fields not preserved: %+v", got)
	}
	if got.ConnectionContext != in.ConnectionContext {
		t.Fatalf("connection context not preserved: %+v", got.ConnectionContext)
	}
}

func TestCompleteQueryTarget_ProductionPolicyNote(t *testing.T) {
	t.Parallel()
	in := baseTarget()
	in.ConnectionContext.Environment = "Production"
	got := completeQueryTarget(in, nil)

	found := false
	for _, note := range got.Governance.PolicyNotes {
		if note == "Production queries require stricter defaults." {
			found = true
		}
	}
	if !found {
		t.Fatalf("production target policyNotes = %v, want production note", got.Governance.PolicyNotes)
	}
}

func TestCompleteQueryTarget_NonProductionOmitsProductionNote(t *testing.T) {
	t.Parallel()
	in := baseTarget()
	in.ConnectionContext.Environment = "Staging"
	got := completeQueryTarget(in, nil)

	for _, note := range got.Governance.PolicyNotes {
		if note == "Production queries require stricter defaults." {
			t.Fatalf("staging target must not carry production note, got %v", got.Governance.PolicyNotes)
		}
	}
}

// TestCompleteQueryTarget_EmptyArraysSerializeAsEmptyNotNull locks the
// contract that optional arrays are emitted as [] rather than null. The
// OpenAPI schema declares schemaPreview and missingFields as non-nullable
// arrays, and the spec requires "return an empty array" when there is no
// metadata. A Go nil slice would serialize to null and violate the contract.
func TestCompleteQueryTarget_EmptyArraysSerializeAsEmptyNotNull(t *testing.T) {
	t.Parallel()
	// Unsupported engine with a complete connection leaves missingFields empty.
	in := baseTarget()
	in.ConnectionContext.Engine = "oracle"
	got := completeQueryTarget(in, nil)

	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(raw)

	if strings.Contains(body, `"schemaPreview":null`) {
		t.Fatalf("schemaPreview must serialize as [], got null: %s", body)
	}
	if !strings.Contains(body, `"schemaPreview":[]`) {
		t.Fatalf("schemaPreview must serialize as []: %s", body)
	}
	if strings.Contains(body, `"missingFields":null`) {
		t.Fatalf("missingFields must serialize as [], got null: %s", body)
	}
	if !strings.Contains(body, `"missingFields":[]`) {
		t.Fatalf("missingFields must serialize as []: %s", body)
	}
}

// TestCompleteQueryTargetWithRuntime_ReadyOnlyOnSecretResolved is the Phase 38A
// readiness correction: a target with credential metadata present is ready ONLY
// when the runtime status is secret_resolved. secret_missing/binding_mismatch
// must keep it locked — never ready on metadata alone. WHY: a target whose
// stored metadata cannot actually resolve+bind to a live secret must not be
// exposed as runnable; otherwise the frontend would offer a Run that fails.
func TestCompleteQueryTargetWithRuntime_ReadyOnlyOnSecretResolved(t *testing.T) {
	t.Parallel()
	in := model.QueryTarget{ResourceID: 22, ConnectionContext: model.QueryTargetConnectionContext{
		Environment: "Staging", Engine: "mysql", Host: "db.internal", Port: 3306,
	}}
	cred := &model.QueryCredentialMetadata{
		ResourceID: 22, Engine: "mysql", CredentialRef: "ORDER_MYSQL_RO",
		Enabled: true, EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}

	// secret_resolved -> ready, runnable, execution enabled.
	got := completeQueryTargetWithRuntime(in, cred, model.QueryCredentialRuntimeSecretResolved)
	if got.Readiness != model.ReadinessReady {
		t.Fatalf("secret_resolved readiness = %q, want ready", got.Readiness)
	}
	if !got.AvailableActions.Run || !got.Governance.ExecutionEnabled {
		t.Fatal("secret_resolved must enable run + execution")
	}
	if got.Governance.CredentialState != "secret_resolved" {
		t.Fatalf("credentialState = %q, want secret_resolved", got.Governance.CredentialState)
	}

	// Metadata present but secret missing -> LOCKED. This is the core bug fix.
	locked := completeQueryTargetWithRuntime(in, cred, model.QueryCredentialRuntimeSecretMissing)
	if locked.Readiness == model.ReadinessReady {
		t.Fatal("secret_missing must NOT be ready (metadata alone is insufficient)")
	}
	if locked.AvailableActions.Run || locked.Governance.ExecutionEnabled {
		t.Fatal("secret_missing must not enable run/execution")
	}
	if locked.Governance.CredentialState != "secret_missing" {
		t.Fatalf("credentialState = %q, want secret_missing", locked.Governance.CredentialState)
	}

	// binding mismatch -> locked.
	bm := completeQueryTargetWithRuntime(in, cred, model.QueryCredentialRuntimeBindingMismatch)
	if bm.Readiness == model.ReadinessReady {
		t.Fatal("binding_mismatch must NOT be ready")
	}
	if bm.Governance.CredentialState != "binding_mismatch" {
		t.Fatalf("credentialState = %q, want binding_mismatch", bm.Governance.CredentialState)
	}
}
