// Package model provides domain entities for the resource management system.
package model

import (
	"errors"
	"fmt"
)

// ExplainFormatVersion is the single version supported by the v1 normalizer.
// Unknown future versions must fail closed rather than silently passthrough.
const ExplainFormatVersion = 1

// ExplainEngine is the finite, validated enum of engines the v1 Explain
// normalizer can emit. ClickHouse support is limited to approved non-production worksheets.
type ExplainEngine string

const (
	ExplainEngineClickHouse ExplainEngine = "clickhouse"
	ExplainEngineMySQL      ExplainEngine = "mysql"
	ExplainEnginePostgreSQL ExplainEngine = "postgresql"
)

// Validate returns nil only for a known Explain engine. Unknown values fail
// closed so the response can never relay arbitrary target metadata.
func (e ExplainEngine) Validate() error {
	switch e {
	case ExplainEngineMySQL, ExplainEnginePostgreSQL, ExplainEngineClickHouse:
		return nil
	}
	return fmt.Errorf("invalid explain engine: %s", e)
}

// ExplainRequest carries the bare worksheet statement. PostgreSQL uses database/schema;
// ClickHouse uses the approval generation and optional Run budget and page window.
// Only the authenticated request supplies an actor.
type ExplainRequest struct {
	MaxRows              int                            `json:"maxRows,omitempty"`
	Pagination           *QueryExecutePaginationRequest `json:"pagination,omitempty"`
	ClickHouseGeneration string                         `json:"clickHouseGeneration,omitempty"`
	Statement            string                         `json:"statement"`
	Database             string                         `json:"database,omitempty"`
	Schema               string                         `json:"schema,omitempty"`
}

// ExplainNodeOperation is the finite enum of normalized plan node operations.
type ExplainNodeOperation string

const (
	ExplainOpTableAccess  ExplainNodeOperation = "table_access"
	ExplainOpIndexAccess  ExplainNodeOperation = "index_access"
	ExplainOpNestedLoop   ExplainNodeOperation = "nested_loop"
	ExplainOpHashJoin     ExplainNodeOperation = "hash_join"
	ExplainOpMergeJoin    ExplainNodeOperation = "merge_join"
	ExplainOpFilter       ExplainNodeOperation = "filter"
	ExplainOpSort         ExplainNodeOperation = "sort"
	ExplainOpAggregate    ExplainNodeOperation = "aggregate"
	ExplainOpTemporaryTbl ExplainNodeOperation = "temporary_table"
	// ExplainOpContainer marks known plan-structure nodes (LIMIT, Hash,
	// Gather, Append …) kept for tree fidelity. They carry no access/risk
	// semantics of their own.
	ExplainOpContainer ExplainNodeOperation = "container"
	ExplainOpUnknown   ExplainNodeOperation = "unknown"
)

// Validate returns nil only for a known operation.
func (o ExplainNodeOperation) Validate() error {
	switch o {
	case ExplainOpTableAccess, ExplainOpIndexAccess, ExplainOpNestedLoop,
		ExplainOpHashJoin, ExplainOpMergeJoin,
		ExplainOpFilter, ExplainOpSort, ExplainOpAggregate, ExplainOpTemporaryTbl,
		ExplainOpContainer, ExplainOpUnknown:
		return nil
	}
	return fmt.Errorf("invalid explain node operation: %s", o)
}

// ExplainNodeAccess is the finite enum of normalized access types.
type ExplainNodeAccess string

const (
	ExplainAccessFullScan  ExplainNodeAccess = "full_scan"
	ExplainAccessIndex     ExplainNodeAccess = "index"
	ExplainAccessUniqueRow ExplainNodeAccess = "unique_row"
	ExplainAccessRange     ExplainNodeAccess = "range"
	ExplainAccessUnknown   ExplainNodeAccess = "unknown"
)

// Validate returns nil only for a known access type.
func (a ExplainNodeAccess) Validate() error {
	switch a {
	case ExplainAccessFullScan, ExplainAccessIndex, ExplainAccessUniqueRow,
		ExplainAccessRange, ExplainAccessUnknown:
		return nil
	}
	return fmt.Errorf("invalid explain node access: %s", a)
}

// ExplainRiskCode is the finite enum of backend-derived risk signals.
type ExplainRiskCode string

const (
	ExplainRiskFullTableScan     ExplainRiskCode = "full_table_scan"
	ExplainRiskFilesort          ExplainRiskCode = "filesort"
	ExplainRiskTemporaryTable    ExplainRiskCode = "temporary_table"
	ExplainRiskHighEstimatedRows ExplainRiskCode = "high_estimated_rows"
	ExplainRiskUnknownPlanShape  ExplainRiskCode = "unknown_plan_shape"
)

// Validate returns nil only for a known risk code.
func (c ExplainRiskCode) Validate() error {
	switch c {
	case ExplainRiskFullTableScan, ExplainRiskFilesort, ExplainRiskTemporaryTable,
		ExplainRiskHighEstimatedRows, ExplainRiskUnknownPlanShape:
		return nil
	}
	return fmt.Errorf("invalid explain risk code: %s", c)
}

// ExplainRiskSeverity is the finite enum of risk severities. The v1
// normalizer never derives ExplainSeverityCritical; it is declared for
// forward-compatibility so the frontend localizes it as a wire value.
type ExplainRiskSeverity string

const (
	ExplainSeverityInfo     ExplainRiskSeverity = "info"
	ExplainSeverityWarning  ExplainRiskSeverity = "warning"
	ExplainSeverityCritical ExplainRiskSeverity = "critical"
)

// Validate returns nil only for a known severity.
func (s ExplainRiskSeverity) Validate() error {
	switch s {
	case ExplainSeverityInfo, ExplainSeverityWarning, ExplainSeverityCritical:
		return nil
	}
	return fmt.Errorf("invalid explain risk severity: %s", s)
}

// MaxExplainNodeLabelLen bounds the engine node label carried on the wire.
// Labels come only from fixed planner vocabulary (PostgreSQL "Node Type"),
// never from user-controlled text — the bound is a defense-in-depth wire cap.
const MaxExplainNodeLabelLen = 64

// MaxExplainIdentifierLen bounds a relation or index name carried on the wire
// (bytes). Names over the bound or containing control characters are dropped
// by the normalizers rather than failing the plan.
const MaxExplainIdentifierLen = 128

// ExplainIdentifierOK reports whether s fits the explain identifier wire
// bound: at most MaxExplainIdentifierLen bytes and free of control
// characters (< 0x20 and 0x7f).
func ExplainIdentifierOK(s string) bool {
	if len(s) > MaxExplainIdentifierLen {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// ExplainNode is one normalized plan node. Relation and Index carry the
// engine's own relation/index names (bounded by MaxExplainIdentifierLen);
// predicates, literals, and free-form engine strings never leave the
// normalizer. Label carries the engine's planner-vocabulary node name
// (e.g. "Seq Scan"), StartupCost/TotalCost the planner's own scalar
// estimates; all three are engine metadata, never user data. EstimatedRows
// is omitted (nil) when the engine does not report it or when the value
// would exceed Number.MAX_SAFE_INTEGER (2^53-1) on the wire.
type ExplainNode struct {
	PrimaryKeyGranules *ExplainGranules     `json:"primaryKeyGranules,omitempty"`
	ID                 string               `json:"id"`
	ParentID           *string              `json:"parentId,omitempty"`
	Operation          ExplainNodeOperation `json:"operation"`
	Access             ExplainNodeAccess    `json:"access"`
	EstimatedRows      *uint64              `json:"estimatedRows,omitempty"`
	UsesIndex          *bool                `json:"usesIndex,omitempty"`
	Label              string               `json:"label,omitempty"`
	Relation           string               `json:"relation,omitempty"`
	Index              string               `json:"index,omitempty"`
	StartupCost        *float64             `json:"startupCost,omitempty"`
	TotalCost          *float64             `json:"totalCost,omitempty"`
}

// ExplainGranules is the pair reported for one MergeTree primary-key pruning stage.
type ExplainGranules struct {
	Selected uint64 `json:"selected"`
	Initial  uint64 `json:"initial"`
}

// Validate returns nil only if every enum field is known, the ID is non-empty,
// and the label stays inside its wire bound.
func (n ExplainNode) Validate() error {
	if g := n.PrimaryKeyGranules; g != nil && (g.Selected > g.Initial || g.Initial > 1<<53-1) {
		return errors.New("invalid primary-key granule counts")
	}
	if n.ID == "" {
		return errors.New("explain node id is required")
	}
	if err := n.Operation.Validate(); err != nil {
		return err
	}
	if err := n.Access.Validate(); err != nil {
		return err
	}
	if len(n.Label) > MaxExplainNodeLabelLen {
		return fmt.Errorf("explain node label exceeds %d bytes", MaxExplainNodeLabelLen)
	}
	if n.Relation != "" && !ExplainIdentifierOK(n.Relation) {
		return fmt.Errorf("explain node relation identifier out of bounds")
	}
	if n.Index != "" && !ExplainIdentifierOK(n.Index) {
		return fmt.Errorf("explain node index identifier out of bounds")
	}
	return nil
}

// ExplainRisk is one backend-derived risk signal with a finite severity.
type ExplainRisk struct {
	Code     ExplainRiskCode     `json:"code"`
	Severity ExplainRiskSeverity `json:"severity"`
}

// Validate returns nil only if both code and severity are known.
func (r ExplainRisk) Validate() error {
	if err := r.Code.Validate(); err != nil {
		return err
	}
	return r.Severity.Validate()
}

// ExplainResponse is the versioned, normalized, leak-free Explain output. It
// carries only finite enums, bounded scalars and identifiers, and the target
// resource ID. No raw plan JSON, predicates, literals, DSNs, credentials,
// actor IDs, or raw driver errors ever reach this struct.
type ExplainResponse struct {
	TargetResourceID uint64        `json:"targetResourceId"`
	Engine           ExplainEngine `json:"engine"`
	FormatVersion    int           `json:"formatVersion"`
	Nodes            []ExplainNode `json:"nodes"`
	Risks            []ExplainRisk `json:"risks"`
	Truncated        bool          `json:"truncated"`
}

// Validate returns nil only if the response is well-formed: known engine,
// FormatVersion == 1, and every node and risk validates. It does NOT verify
// caps (the normalizer enforces those during construction).
func (r ExplainResponse) Validate() error {
	if err := r.Engine.Validate(); err != nil {
		return err
	}
	if r.FormatVersion != ExplainFormatVersion {
		return fmt.Errorf("unsupported explain format version: %d", r.FormatVersion)
	}
	for i, n := range r.Nodes {
		if n.PrimaryKeyGranules != nil && (r.Engine != ExplainEngineClickHouse || n.Operation != ExplainOpTableAccess) {
			return fmt.Errorf("unexpected primary-key granule evidence on node %d", i)
		}
		if err := n.Validate(); err != nil {
			return fmt.Errorf("explain node %d: %w", i, err)
		}
	}
	for i, risk := range r.Risks {
		if err := risk.Validate(); err != nil {
			return fmt.Errorf("explain risk %d: %w", i, err)
		}
	}
	return nil
}

// ExplainAuditOutcome is the typed enum of fixed audit outcomes the Explain
// service may record. The recorder accepts ONLY this enum — no raw string
// overload — so arbitrary callers cannot inject free-form text at the audit
// boundary.
type ExplainAuditOutcome string

const (
	ExplainAuditSuccess     ExplainAuditOutcome = "success"
	ExplainAuditRejected    ExplainAuditOutcome = "rejected"
	ExplainAuditUnsupported ExplainAuditOutcome = "unsupported"
	ExplainAuditError       ExplainAuditOutcome = "error"
)

// Validate returns nil only for a known audit outcome.
func (o ExplainAuditOutcome) Validate() error {
	switch o {
	case ExplainAuditSuccess, ExplainAuditRejected, ExplainAuditUnsupported, ExplainAuditError:
		return nil
	}
	return fmt.Errorf("invalid explain audit outcome: %s", o)
}
