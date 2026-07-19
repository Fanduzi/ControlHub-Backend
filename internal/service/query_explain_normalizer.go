// Package service provides the MySQL Explain plan normalizer for Phase 38N.
// input: encoding/json, errors, fmt, sort, strconv, strings, internal/model
// output: ExplainNormalizer, NormalizeExplainPlan, HighEstimatedRowsThreshold, MaxExplainNodes, MaxExplainRisks, MaxExplainPlanDepth
// pos: Translates MySQL EXPLAIN FORMAT=JSON into the v1 bounded, leak-free normalized model
// note: if this file changes, update header and README.md
package service

import (
	"math"
	"sort"
	"strconv"

	"github.com/fan/controlhub/internal/model"
)

// Normalizer caps (Oracle P1.2, P2.4). Output caps bound the response; the
// raw-plan byte cap lives in the executor. MaxExplainPlanDepth bounds
// recursion so a pathologically deep plan cannot consume unbounded stack.
const (
	MaxExplainNodes         = 64
	MaxExplainRisks         = 16
	MaxExplainPlanDepth     = 64
	HighEstimatedRowsThreshold uint64 = 100_000
)

// maxSafeJSInteger is Number.MAX_SAFE_INTEGER (2^53 - 1). The frontend
// cannot safely represent larger integers, so estimatedRows above this cap
// is omitted from the public response while still contributing to risk
// derivation (Oracle P2.5).
const maxSafeJSInteger uint64 = (1 << 53) - 1

// ExplainNormalizer translates an ExplainRawPlan into the v1 normalized
// model. It is the only consumer of the raw plan tree; no free-form engine
// string, relation name, index name, predicate, literal, or message leaves
// this type. Unknown shapes fail closed to ErrQueryExplainNotSupported.
type ExplainNormalizer struct{}

// NewExplainNormalizer builds the normalizer.
func NewExplainNormalizer() *ExplainNormalizer {
	return &ExplainNormalizer{}
}

// NormalizeResult is the normalizer's output: the bounded node/risk slices
// and the truncation flag.
type NormalizeResult struct {
	Nodes     []model.ExplainNode
	Risks     []model.ExplainRisk
	Truncated bool
}

// Normalize walks the raw plan tree depth-first, emits normalized nodes and
// risks, applies caps, and returns a leak-free result. An empty or malformed
// tree returns ErrQueryExplainNotSupported; the error never carries raw
// plan bytes or driver text.
func (n *ExplainNormalizer) Normalize(raw ExplainRawPlan) (NormalizeResult, error) {
	tree := raw.Tree()
	root, ok := tree.(map[string]interface{})
	if !ok {
		return NormalizeResult{}, ErrQueryExplainNotSupported
	}
	qb, ok := root["query_block"].(map[string]interface{})
	if !ok {
		return NormalizeResult{}, ErrQueryExplainNotSupported
	}
	ctx := &normCtx{
		nodes:        make([]model.ExplainNode, 0, MaxExplainNodes),
		risks:        make([]model.ExplainRisk, 0, MaxExplainRisks),
		seenRisks:    make(map[model.ExplainRiskCode]bool, MaxExplainRisks),
		nextID:       0,
		truncated:    false,
		unknownShape: false,
	}
	ctx.walk(qb, "", 0)
	if len(ctx.nodes) == 0 {
		return NormalizeResult{}, ErrQueryExplainNotSupported
	}
	return NormalizeResult{Nodes: ctx.nodes, Risks: ctx.risks, Truncated: ctx.truncated}, nil
}

// normCtx carries the walking state. The parent stack holds the ID of the
// enclosing node so children get a correct parentId (Oracle P2.4: monotonic
// IDs, parent precedes child, no cycles, stable for the same fixture).
type normCtx struct {
	nodes        []model.ExplainNode
	risks        []model.ExplainRisk
	seenRisks    map[model.ExplainRiskCode]bool
	nextID       int
	truncated    bool
	unknownShape bool
}

// walk recurses depth-first through a block. parentID is the ID of the
// enclosing node ("" for the root). depth guards against pathological
// nesting; beyond MaxExplainPlanDepth the walk stops and emits one unknown
// node plus the unknown_plan_shape risk.
func (c *normCtx) walk(block map[string]interface{}, parentID string, depth int) {
	if depth > MaxExplainPlanDepth {
		c.emitUnknown(parentID)
		c.addRisk(model.ExplainRiskUnknownPlanShape, model.ExplainSeverityInfo)
		c.truncated = true
		return
	}
	if _, ok := block["table"]; ok {
		c.emitTableNode(block, parentID)
	}
	if nl, ok := block["nested_loop"]; ok {
		c.emitNestedLoopNode(parentID)
		if arr, ok := nl.([]interface{}); ok {
			for _, child := range arr {
				if m, ok := child.(map[string]interface{}); ok {
					id := c.lastID()
					c.walk(m, id, depth+1)
				}
			}
		}
	}
	if ord, ok := block["ordering_operation"].(map[string]interface{}); ok {
		if using, ok := ord["using_filesort"].(bool); ok && using {
			c.addRisk(model.ExplainRiskFilesort, model.ExplainSeverityWarning)
		}
		id := c.lastID()
		c.walk(ord, parentID, depth+1)
		_ = id
	}
	if grp, ok := block["grouping_operation"].(map[string]interface{}); ok {
		if using, ok := grp["using_temporary_table"].(bool); ok && using {
			c.emitTempTableNode(parentID)
			c.addRisk(model.ExplainRiskTemporaryTable, model.ExplainSeverityWarning)
		}
		if using, ok := grp["using_filesort"].(bool); ok && using {
			c.addRisk(model.ExplainRiskFilesort, model.ExplainSeverityWarning)
		}
		c.walk(grp, parentID, depth+1)
	}
	if dut, ok := block["duplicates_removal"].(map[string]interface{}); ok {
		if using, ok := dut["using_temporary_table"].(bool); ok && using {
			c.emitTempTableNode(parentID)
			c.addRisk(model.ExplainRiskTemporaryTable, model.ExplainSeverityWarning)
		}
		c.walk(dut, parentID, depth+1)
	}
}

// emitTableNode emits a node for a table access block and derives its
// operation/access/estimatedRows/usesIndex from the raw fields.
func (c *normCtx) emitTableNode(block map[string]interface{}, parentID string) {
	tbl, ok := block["table"].(map[string]interface{})
	if !ok {
		c.emitUnknown(parentID)
		c.addRisk(model.ExplainRiskUnknownPlanShape, model.ExplainSeverityInfo)
		return
	}
	access := mapAccessType(tbl)
	op := model.ExplainOpTableAccess
	if access == model.ExplainAccessIndex || access == model.ExplainAccessUniqueRow || access == model.ExplainAccessRange {
		op = model.ExplainOpIndexAccess
	}
	node := model.ExplainNode{
		ID:        c.allocID(),
		ParentID:  c.maybeParent(parentID),
		Operation: op,
		Access:    access,
	}
	if est := extractEstimatedRows(tbl); est != nil {
		if *est >= HighEstimatedRowsThreshold {
			c.addRisk(model.ExplainRiskHighEstimatedRows, model.ExplainSeverityWarning)
		}
		if *est <= maxSafeJSInteger {
			node.EstimatedRows = est
		}
	}
	if _, hasKey := tbl["key"]; hasKey {
		b := true
		node.UsesIndex = &b
	} else if access == model.ExplainAccessFullScan {
		b := false
		node.UsesIndex = &b
	}
	c.appendNode(node)
	if access == model.ExplainAccessFullScan {
		c.addRisk(model.ExplainRiskFullTableScan, model.ExplainSeverityWarning)
	}
}

// emitNestedLoopNode emits a nested_loop operation node.
func (c *normCtx) emitNestedLoopNode(parentID string) {
	if c.atNodeCap() {
		c.truncated = true
		return
	}
	c.appendNode(model.ExplainNode{
		ID:        c.allocID(),
		ParentID:  c.maybeParent(parentID),
		Operation: model.ExplainOpNestedLoop,
		Access:    model.ExplainAccessUnknown,
	})
}

// emitTempTableNode emits a temporary_table operation node.
func (c *normCtx) emitTempTableNode(parentID string) {
	if c.atNodeCap() {
		c.truncated = true
		return
	}
	c.appendNode(model.ExplainNode{
		ID:        c.allocID(),
		ParentID:  c.maybeParent(parentID),
		Operation: model.ExplainOpTemporaryTbl,
		Access:    model.ExplainAccessUnknown,
	})
}

// emitUnknown emits an unknown-shape node and records that an unknown was
// seen so the caller can add the unknown_plan_shape risk.
func (c *normCtx) emitUnknown(parentID string) {
	if c.atNodeCap() {
		c.truncated = true
		return
	}
	c.appendNode(model.ExplainNode{
		ID:        c.allocID(),
		ParentID:  c.maybeParent(parentID),
		Operation: model.ExplainOpUnknown,
		Access:    model.ExplainAccessUnknown,
	})
	c.unknownShape = true
}

// mapAccessType converts MySQL access_type strings to the finite enum.
func mapAccessType(tbl map[string]interface{}) model.ExplainNodeAccess {
	at, _ := tbl["access_type"].(string)
	switch at {
	case "ALL":
		return model.ExplainAccessFullScan
	case "index":
		return model.ExplainAccessIndex
	case "const", "eq_ref", "system", "NULL":
		return model.ExplainAccessUniqueRow
	case "range", "ref", "ref_or_null", "fulltext", "index_merge":
		return model.ExplainAccessRange
	default:
		return model.ExplainAccessUnknown
	}
}

// extractEstimatedRows reads rows_examined_per_scan (preferred) or
// rows_produced_per_join (fallback) as a non-negative uint64. Returns nil if
// absent, unparseable, or negative.
func extractEstimatedRows(tbl map[string]interface{}) *uint64 {
	for _, key := range []string{"rows_examined_per_scan", "rows_produced_per_join"} {
		if v, ok := tbl[key]; ok {
			if u, ok := toUint64(v); ok {
				return &u
			}
		}
	}
	return nil
}

// toUint64 coerces a JSON number (float64) or numeric string to uint64.
// Returns ok=false for negative, NaN, Inf, or unparseable values.
func toUint64(v interface{}) (uint64, bool) {
	switch n := v.(type) {
	case float64:
		if n < 0 || math.IsNaN(n) || math.IsInf(n, 0) {
			return 0, false
		}
		return uint64(n), true
	case string:
		u, err := strconv.ParseUint(n, 10, 64)
		if err != nil {
			return 0, false
		}
		return u, true
	}
	return 0, false
}

// allocID returns the next monotonic ID and advances the counter.
func (c *normCtx) allocID() string {
	id := strconv.Itoa(c.nextID)
	c.nextID++
	return id
}

// lastID returns the most recently allocated ID, or "" if none.
func (c *normCtx) lastID() string {
	if c.nextID == 0 {
		return ""
	}
	return strconv.Itoa(c.nextID - 1)
}

// maybeParent returns a *string for parentID when non-empty, nil for the root.
func (c *normCtx) maybeParent(parentID string) *string {
	if parentID == "" {
		return nil
	}
	p := parentID
	return &p
}

// appendNode appends a node, honoring the node cap.
func (c *normCtx) appendNode(n model.ExplainNode) {
	if len(c.nodes) >= MaxExplainNodes {
		c.truncated = true
		return
	}
	c.nodes = append(c.nodes, n)
}

// atNodeCap reports whether the node cap has been reached.
func (c *normCtx) atNodeCap() bool {
	if len(c.nodes) >= MaxExplainNodes {
		c.truncated = true
		return true
	}
	return false
}

// addRisk adds a risk if its code has not already been added (dedup by code,
// first-seen severity wins). Honors the risk cap.
func (c *normCtx) addRisk(code model.ExplainRiskCode, sev model.ExplainRiskSeverity) {
	if c.seenRisks[code] {
		return
	}
	if len(c.risks) >= MaxExplainRisks {
		c.truncated = true
		return
	}
	c.seenRisks[code] = true
	c.risks = append(c.risks, model.ExplainRisk{Code: code, Severity: sev})
}

// SortNodesByID is a helper for tests that want a deterministic order after
// normalization. Production does not need to call this — the normalizer
// emits nodes in walk order, which is already monotonic by ID.
func SortNodesByID(nodes []model.ExplainNode) {
	sort.Slice(nodes, func(i, j int) bool {
		ii, _ := strconv.Atoi(nodes[i].ID)
		ji, _ := strconv.Atoi(nodes[j].ID)
		return ii < ji
	})
}
