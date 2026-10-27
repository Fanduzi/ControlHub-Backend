// Package service provides the PostgreSQL Explain plan normalizer. It maps
// the EXPLAIN (FORMAT JSON) plan tree onto the same bounded, leak-free v1
// contract the MySQL normalizer emits — finite operation/access enums,
// estimated row counts, derived risks — plus two native-fidelity fields
// sourced only from fixed planner vocabulary: `label` (the "Node Type"
// string, ≤64 bytes) and numeric `startupCost`/`totalCost` ("Startup
// Cost"/"Total Cost", numeric JSON values only). Structural plan nodes
// (Result, Hash, Limit, …) emit `container` nodes so the rendered tree
// keeps the native shape. No relation name, index name, predicate, or
// literal ever leaves this file.
// input: internal/model — ExplainRawPlan parsed tree
// output: PGExplainNormalizer.Normalize — NormalizeResult or ErrQueryExplainNotSupported
// pos: PG sibling of ExplainNormalizer — shared normCtx caps (64 nodes / 16 risks / 64 depth)
// note: if this file changes, update this header and module README.md.

package service

import (
	"github.com/fan/controlhub/internal/model"
)

// PGExplainNormalizer translates a PostgreSQL EXPLAIN (FORMAT JSON) tree
// into the v1 normalized model. The wire shape is an array with one object
// whose "Plan" member is the root node; children recurse through "Plans".
type PGExplainNormalizer struct{}

// NewPGExplainNormalizer builds the normalizer.
func NewPGExplainNormalizer() *PGExplainNormalizer {
	return &PGExplainNormalizer{}
}

// pgStructuralNodes are container or decorator node types emitted as
// `container` nodes — real plan structure (Limit, Hash, Gather, Append…)
// kept for tree fidelity but carrying no access/risk semantics of their
// own. Mapped node types are handled by the pgWalk switch before this set
// is consulted; anything neither mapped nor listed becomes an unknown node
// plus the unknown_plan_shape risk.
var pgStructuralNodes = map[string]bool{
	"Result":                true,
	"ProjectSet":            true,
	"Limit":                 true,
	"Hash":                  true,
	"Gather":                true,
	"Gather Merge":          true,
	"Append":                true,
	"Merge Append":          true,
	"Unique":                true,
	"WindowAgg":             true,
	"Subquery Scan":         true,
	"LockRows":              true,
	"SetOp":                 true,
	"Recursive Union":       true,
	"Named Tuplestore Scan": true,
}

// Normalize walks the PG plan tree depth-first and emits the bounded v1
// nodes and risks. A malformed envelope or a plan with no mappable node
// returns ErrQueryExplainNotSupported — never raw plan bytes.
func (n *PGExplainNormalizer) Normalize(raw ExplainRawPlan) (NormalizeResult, error) {
	arr, ok := raw.Tree().([]interface{})
	if !ok || len(arr) == 0 {
		return NormalizeResult{}, ErrQueryExplainNotSupported
	}
	env, ok := arr[0].(map[string]interface{})
	if !ok {
		return NormalizeResult{}, ErrQueryExplainNotSupported
	}
	root, ok := env["Plan"].(map[string]interface{})
	if !ok {
		return NormalizeResult{}, ErrQueryExplainNotSupported
	}
	ctx := &normCtx{
		nodes:     make([]model.ExplainNode, 0, MaxExplainNodes),
		risks:     make([]model.ExplainRisk, 0, MaxExplainRisks),
		seenRisks: make(map[model.ExplainRiskCode]bool, MaxExplainRisks),
	}
	ctx.pgWalk(root, "", 0)
	if len(ctx.nodes) == 0 {
		return NormalizeResult{}, ErrQueryExplainNotSupported
	}
	return NormalizeResult{Nodes: ctx.nodes, Risks: ctx.risks, Truncated: ctx.truncated}, nil
}

// pgWalk recurses depth-first. Structural nodes are traversed in place;
// mapped and unknown nodes become the parent for their children.
func (c *normCtx) pgWalk(node map[string]interface{}, parentID string, depth int) {
	if depth > MaxExplainPlanDepth {
		c.emitUnknown(parentID)
		c.addRisk(model.ExplainRiskUnknownPlanShape, model.ExplainSeverityInfo)
		c.truncated = true
		return
	}
	nodeType, _ := node["Node Type"].(string)

	emitParent := parentID
	switch nodeType {
	case "Seq Scan":
		emitParent = c.emitPGNode(parentID, model.ExplainOpTableAccess, model.ExplainAccessFullScan, node, boolPtr(false))
		c.addRisk(model.ExplainRiskFullTableScan, model.ExplainSeverityWarning)
	case "Index Scan", "Index Only Scan", "Bitmap Index Scan":
		emitParent = c.emitPGNode(parentID, model.ExplainOpIndexAccess, model.ExplainAccessIndex, node, boolPtr(true))
	case "Bitmap Heap Scan":
		emitParent = c.emitPGNode(parentID, model.ExplainOpIndexAccess, model.ExplainAccessIndex, node, boolPtr(true))
	case "Tid Scan":
		emitParent = c.emitPGNode(parentID, model.ExplainOpTableAccess, model.ExplainAccessUniqueRow, node, boolPtr(true))
	case "Tid Range Scan":
		emitParent = c.emitPGNode(parentID, model.ExplainOpTableAccess, model.ExplainAccessRange, node, nil)
	case "Nested Loop", "Hash Join", "Merge Join":
		emitParent = c.emitPGNode(parentID, model.ExplainOpNestedLoop, model.ExplainAccessUnknown, node, nil)
	case "Sort", "Incremental Sort":
		emitParent = c.emitPGNode(parentID, model.ExplainOpSort, model.ExplainAccessUnknown, node, nil)
		c.addRisk(model.ExplainRiskFilesort, model.ExplainSeverityWarning)
	case "Aggregate", "HashAggregate", "GroupAggregate", "Partial HashAggregate":
		emitParent = c.emitPGNode(parentID, model.ExplainOpAggregate, model.ExplainAccessUnknown, node, nil)
	case "Materialize", "CTE Scan", "WorkTable Scan", "Memoize":
		emitParent = c.emitPGNode(parentID, model.ExplainOpTemporaryTbl, model.ExplainAccessUnknown, node, nil)
		c.addRisk(model.ExplainRiskTemporaryTable, model.ExplainSeverityWarning)
	case "Function Scan", "Values Scan", "Table Function Scan", "Sample Scan", "Foreign Scan":
		emitParent = c.emitPGNode(parentID, model.ExplainOpTableAccess, model.ExplainAccessUnknown, node, nil)
	default:
		if pgStructuralNodes[nodeType] {
			// Known structural node: kept as a labeled container so the
			// rendered tree matches the native plan shape.
			emitParent = c.emitPGNode(parentID, model.ExplainOpContainer, model.ExplainAccessUnknown, node, nil)
		} else {
			emitParent = c.emitPGNode(parentID, model.ExplainOpUnknown, model.ExplainAccessUnknown, node, nil)
			c.addRisk(model.ExplainRiskUnknownPlanShape, model.ExplainSeverityInfo)
		}
	}

	if children, ok := node["Plans"].([]interface{}); ok {
		for _, child := range children {
			if m, ok := child.(map[string]interface{}); ok {
				c.pgWalk(m, emitParent, depth+1)
			}
		}
	}
}

// emitPGNode appends one mapped node and returns its ID for child linking.
// Plan Rows feeds estimatedRows with the same high-rows risk and the
// MAX_SAFE_INTEGER wire bound as the MySQL path. The node's Label is the
// planner's own "Node Type" vocabulary (bounded, never user text) and the
// scalar cost fields pass through as numbers only.
func (c *normCtx) emitPGNode(parentID string, op model.ExplainNodeOperation, access model.ExplainNodeAccess, node map[string]interface{}, usesIndex *bool) string {
	if c.atNodeCap() {
		return parentID
	}
	n := model.ExplainNode{
		ID:        c.allocID(),
		ParentID:  c.maybeParent(parentID),
		Operation: op,
		Access:    access,
		UsesIndex: usesIndex,
		Label:     pgNodeLabel(node),
	}
	if v, ok := toFloat64(node["Startup Cost"]); ok {
		n.StartupCost = &v
	}
	if v, ok := toFloat64(node["Total Cost"]); ok {
		n.TotalCost = &v
	}
	if est := extractPGPlanRows(node); est != nil {
		if *est >= HighEstimatedRowsThreshold {
			c.addRisk(model.ExplainRiskHighEstimatedRows, model.ExplainSeverityWarning)
		}
		if *est <= maxSafeJSInteger {
			n.EstimatedRows = est
		}
	}
	c.appendNode(n)
	return n.ID
}

// pgNodeLabel carries the planner's fixed "Node Type" vocabulary onto the
// wire, bounded by MaxExplainNodeLabelLen. It is engine metadata — relation
// names, predicates, and literals live in separate plan fields and never
// enter the label.
func pgNodeLabel(node map[string]interface{}) string {
	label, _ := node["Node Type"].(string)
	if len(label) > model.MaxExplainNodeLabelLen {
		return ""
	}
	return label
}

// toFloat64 reads a JSON number. Absent/non-numeric values return false —
// planner cost fields are optional on the wire.
func toFloat64(v interface{}) (float64, bool) {
	f, ok := v.(float64)
	return f, ok
}

// extractPGPlanRows reads "Plan Rows" as a non-negative uint64. Absent or
// unparseable values return nil — estimatedRows is optional on the wire.
func extractPGPlanRows(node map[string]interface{}) *uint64 {
	if v, ok := node["Plan Rows"]; ok {
		if u, ok := toUint64(v); ok {
			return &u
		}
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }
