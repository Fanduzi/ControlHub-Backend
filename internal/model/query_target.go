// Package model provides domain entities for the resource management system.
// input: (none)
// output: QueryTarget read-model types and query capability/readiness/safety enums
// pos: Read-only query target context for the Query Workbench shell (Phase 36)
// note: if this file changes, update header and README.md
package model

// QueryKind classifies how a target would be queried in a future workbench.
type QueryKind string

const (
	QueryKindSQL         QueryKind = "sql"
	QueryKindRedis       QueryKind = "redis"
	QueryKindMongo       QueryKind = "mongo"
	QueryKindUnsupported QueryKind = "unsupported"
)

// QueryTargetReadiness is the explicit readiness state of a query target.
// Phase 36 must not return ready unless concrete read-only credential
// metadata already exists.
type QueryTargetReadiness string

const (
	ReadinessReady              QueryTargetReadiness = "ready"
	ReadinessMissingConnection  QueryTargetReadiness = "missing_connection"
	ReadinessCredentialRequired QueryTargetReadiness = "credential_required"
	ReadinessUnsupportedEngine  QueryTargetReadiness = "unsupported_engine"
	ReadinessDisabled           QueryTargetReadiness = "disabled"
)

// QueryTargetSafetyState is the safety boundary that explains why a target
// cannot execute queries in the current phase.
type QueryTargetSafetyState string

const (
	SafetyStateCredentialMissing    QueryTargetSafetyState = "credential_missing"
	SafetyStateExecutionDisabled    QueryTargetSafetyState = "execution_disabled"
	SafetyStateUnsupportedEngine    QueryTargetSafetyState = "unsupported_engine"
	SafetyStateConnectionIncomplete QueryTargetSafetyState = "connection_incomplete"
)

// QueryTargetConnectionContext is the resolved connection context a workbench
// needs to display beside a target. No credentials live here.
type QueryTargetConnectionContext struct {
	Environment string `json:"environment"`
	Owner       string `json:"owner"`
	Engine      string `json:"engine"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	ClusterID   uint64 `json:"clusterId,omitempty"`
	ClusterName string `json:"clusterName,omitempty"`
}

// QueryTargetCapability tells the frontend which editor and language label to
// use for a target without guessing from raw engine strings.
type QueryTargetCapability struct {
	QueryKind     QueryKind `json:"queryKind"`
	EditorMode    string    `json:"editorMode"`
	LanguageLabel string    `json:"languageLabel"`
}

// QueryTargetGovernance is the explicit, backend-owned governance state for a
// target. Phase 36 always reports execution disabled.
type QueryTargetGovernance struct {
	ExecutionEnabled bool                 `json:"executionEnabled"`
	CredentialState  string               `json:"credentialState"`
	AuditRequired    bool                 `json:"auditRequired"`
	SafetyState      QueryTargetSafetyState `json:"safetyState"`
	SafetyNote       string               `json:"safetyNote"`
	PolicyNotes      []string             `json:"policyNotes"`
}

// QueryTargetAvailableActions are the locked/unlocked action flags for the
// workbench action bar. All are false in Phase 36.
type QueryTargetAvailableActions struct {
	Run           bool `json:"run"`
	Explain       bool `json:"explain"`
	Export        bool `json:"export"`
	SaveSheet     bool `json:"saveSheet"`
	RequestAccess bool `json:"requestAccess"`
}

// QueryTargetSchemaPreviewNode is a lightweight schema/object placeholder node
// derived only from existing ControlHub metadata. Phase 36 never introspects
// live databases.
type QueryTargetSchemaPreviewNode struct {
	Kind     string                       `json:"kind"`
	Name     string                       `json:"name"`
	Children []QueryTargetSchemaPreviewNode `json:"children,omitempty"`
}

// QueryTarget is the read-only query capability context for one database
// resource, ready to drive a locked Query Workbench shell.
type QueryTarget struct {
	ResourceID        uint64                       `json:"resourceId"`
	ResourceName      string                       `json:"resourceName"`
	DisplayName       string                       `json:"displayName"`
	ResourceType      ResourceType                 `json:"resourceType"`
	ConnectionContext QueryTargetConnectionContext `json:"connectionContext"`
	Capability        QueryTargetCapability        `json:"capability"`
	Readiness         QueryTargetReadiness         `json:"readiness"`
	MissingFields     []string                     `json:"missingFields"`
	Governance        QueryTargetGovernance        `json:"governance"`
	AvailableActions  QueryTargetAvailableActions  `json:"availableActions"`
	SchemaPreview     []QueryTargetSchemaPreviewNode `json:"schemaPreview"`
}

// QueryTargetListResponse is the { items: [...] } envelope for GET /query-targets.
type QueryTargetListResponse struct {
	Items []QueryTarget `json:"items"`
}

// QueryTargetListQuery carries the cheap, conventional filters for
// GET /query-targets. Readiness and query kind are derived client-side, so
// they are intentionally not part of the server-side query.
type QueryTargetListQuery struct {
	Engine        string
	EnvironmentID uint64
}
