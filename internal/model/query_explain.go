// Package model provides domain entities for the resource management system.
// input: errors, fmt packages
// output: Explain v1 request/response model, finite node/risk enums, ExplainAuditOutcome enum
// pos: Phase 38N governed Explain public contract — versioned, normalized, leak-free
// note: if this file changes, update header, README.md, and internal/openapi/openapi.yaml
package model

import (
	"errors"
	"fmt"
)

// ExplainFormatVersion is the single version supported by the v1 normalizer.
// Unknown future versions must fail closed rather than silently passthrough.
const ExplainFormatVersion = 1

// ExplainEngine is the finite, validated enum of engines the v1 Explain
// normalizer can emit. v1 ships MySQL only; TiDB fails closed because it
// cannot satisfy the same bounded source contract.
type ExplainEngine string

const (
	ExplainEngineMySQL ExplainEngine = "mysql"
)

// Validate returns nil only for a known Explain engine. Unknown values fail
// closed so the response can never relay arbitrary target metadata.
func (e ExplainEngine) Validate() error {
	switch e {
	case ExplainEngineMySQL:
		return nil
	}
	return fmt.Errorf("invalid explain engine: %s", e)
}

// ExplainRequest is the body of POST /query-targets/{id}/explain. Only the
// worksheet statement is accepted; the actor is derived from the verified
// token and the engine/governance context from target resolution.
type ExplainRequest struct {
	Statement string `json:"statement"`
}

// ExplainNodeOperation is the finite enum of normalized plan node operations.
type ExplainNodeOperation string

const (
	ExplainOpTableAccess   ExplainNodeOperation = "table_access"
	ExplainOpIndexAccess   ExplainNodeOperation = "index_access"
	ExplainOpNestedLoop    ExplainNodeOperation = "nested_loop"
	ExplainOpSort          ExplainNodeOperation = "sort"
	ExplainOpAggregate     ExplainNodeOperation = "aggregate"
	ExplainOpTemporaryTbl  ExplainNodeOperation = "temporary_table"
	ExplainOpUnknown       ExplainNodeOperation = "unknown"
)

// Validate returns nil only for a known operation.
func (o ExplainNodeOperation) Validate() error {
	switch o {
	case ExplainOpTableAccess, ExplainOpIndexAccess, ExplainOpNestedLoop,
		ExplainOpSort, ExplainOpAggregate, ExplainOpTemporaryTbl, ExplainOpUnknown:
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
	ExplainRiskFullTableScan    ExplainRiskCode = "full_table_scan"
	ExplainRiskFilesort         ExplainRiskCode = "filesort"
	ExplainRiskTemporaryTable   ExplainRiskCode = "temporary_table"
	ExplainRiskHighEstimatedRows ExplainRiskCode = "high_estimated_rows"
	ExplainRiskUnknownPlanShape ExplainRiskCode = "unknown_plan_shape"
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

// ExplainNode is one normalized plan node. No free-form engine strings,
// relation names, index names, predicates, or literals leave the normalizer.
// EstimatedRows is omitted (nil) when the engine does not report it or when
// the value would exceed Number.MAX_SAFE_INTEGER (2^53-1) on the wire.
type ExplainNode struct {
	ID            string               `json:"id"`
	ParentID      *string              `json:"parentId,omitempty"`
	Operation     ExplainNodeOperation `json:"operation"`
	Access        ExplainNodeAccess    `json:"access"`
	EstimatedRows *uint64              `json:"estimatedRows,omitempty"`
	UsesIndex     *bool                `json:"usesIndex,omitempty"`
}

// Validate returns nil only if every enum field is known and the ID is non-empty.
func (n ExplainNode) Validate() error {
	if n.ID == "" {
		return errors.New("explain node id is required")
	}
	if err := n.Operation.Validate(); err != nil {
		return err
	}
	if err := n.Access.Validate(); err != nil {
		return err
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
// carries only finite enums, bounded scalars, and the target resource ID.
// No raw plan JSON, table names, index names, predicates, literals, DSNs,
// credentials, actor IDs, or raw driver errors ever reach this struct.
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
