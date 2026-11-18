// Package service provides tests for the PostgreSQL Explain plan normalizer.
package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func pgRawPlan(t *testing.T, payload string) ExplainRawPlan {
	t.Helper()
	var tree interface{}
	if err := json.Unmarshal([]byte(payload), &tree); err != nil {
		t.Fatalf("fixture json: %v", err)
	}
	return ExplainRawPlan{tree: tree}
}

func riskCodes(result NormalizeResult) []model.ExplainRiskCode {
	codes := make([]model.ExplainRiskCode, 0, len(result.Risks))
	for _, r := range result.Risks {
		codes = append(codes, r.Code)
	}
	return codes
}

func containsRisk(codes []model.ExplainRiskCode, want model.ExplainRiskCode) bool {
	for _, c := range codes {
		if c == want {
			return true
		}
	}
	return false
}

// TestPGSeqScanMapsToFullScan proves the canonical bad plan — Seq Scan with
// a big row estimate — becomes full_scan + the two warning risks. WHY: this
// is the signal a developer looks at to decide whether their query needs an
// index.
func TestPGSeqScanMapsToFullScan(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Seq Scan","Plan Rows":125000,"Plans":[]}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(got.Nodes))
	}
	n := got.Nodes[0]
	if n.Operation != model.ExplainOpTableAccess || n.Access != model.ExplainAccessFullScan {
		t.Errorf("node = %s/%s, want table_access/full_scan", n.Operation, n.Access)
	}
	if n.UsesIndex == nil || *n.UsesIndex != false {
		t.Errorf("usesIndex = %v, want false", n.UsesIndex)
	}
	if n.EstimatedRows == nil || *n.EstimatedRows != 125000 {
		t.Errorf("estimatedRows = %v, want 125000", n.EstimatedRows)
	}
	codes := riskCodes(got)
	if !containsRisk(codes, model.ExplainRiskFullTableScan) {
		t.Errorf("missing full_table_scan risk in %v", codes)
	}
	if !containsRisk(codes, model.ExplainRiskHighEstimatedRows) {
		t.Errorf("missing high_estimated_rows risk in %v", codes)
	}
}

// TestPGIndexScanMapsToIndexAccess proves index-driven scans report
// usesIndex=true with no scan risk.
func TestPGIndexScanMapsToIndexAccess(t *testing.T) {
	for _, nt := range []string{"Index Scan", "Index Only Scan", "Bitmap Index Scan", "Bitmap Heap Scan"} {
		raw := pgRawPlan(t, `[{"Plan":{"Node Type":"`+nt+`","Plan Rows":10}}]`)
		got, err := NewPGExplainNormalizer().Normalize(raw)
		if err != nil {
			t.Fatalf("%s: %v", nt, err)
		}
		n := got.Nodes[0]
		if n.Operation != model.ExplainOpIndexAccess || n.Access != model.ExplainAccessIndex {
			t.Errorf("%s: %s/%s, want index_access/index", nt, n.Operation, n.Access)
		}
		if n.UsesIndex == nil || !*n.UsesIndex {
			t.Errorf("%s: usesIndex = %v, want true", nt, n.UsesIndex)
		}
	}
}

// TestPGNestedPlanTree proves parent/child linking survives structural
// containers: Hash is kept as a labeled `container` node so the rendered
// tree matches the native plan shape, and its child Seq Scan links to it.
func TestPGNestedPlanTree(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{
		"Node Type":"Nested Loop","Plan Rows":50,
		"Plans":[
			{"Node Type":"Index Scan","Plan Rows":5},
			{"Node Type":"Hash Join","Plan Rows":30,"Plans":[
				{"Node Type":"Seq Scan","Plan Rows":200},
				{"Node Type":"Hash","Plans":[{"Node Type":"Seq Scan","Plan Rows":100}]}
			]}
		]
	}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	// Nodes: NestedLoop(0), IndexScan(1), HashJoin(2), SeqScan(3), Hash(4, container), SeqScan(5)
	if len(got.Nodes) != 6 {
		t.Fatalf("nodes = %d, want 6: %+v", len(got.Nodes), got.Nodes)
	}
	if got.Nodes[0].Operation != model.ExplainOpNestedLoop || got.Nodes[0].Label != "Nested Loop" {
		t.Errorf("root = %s/%q, want nested_loop/\"Nested Loop\"", got.Nodes[0].Operation, got.Nodes[0].Label)
	}
	if got.Nodes[1].ParentID == nil || *got.Nodes[1].ParentID != got.Nodes[0].ID {
		t.Errorf("index scan parent = %v, want %s", got.Nodes[1].ParentID, got.Nodes[0].ID)
	}
	if got.Nodes[2].Operation != model.ExplainOpHashJoin || got.Nodes[2].Label != "Hash Join" {
		t.Errorf("hash join = %s/%q, want hash_join/\"Hash Join\"", got.Nodes[2].Operation, got.Nodes[2].Label)
	}
	// Hash is a kept container: it links under the Hash Join node (2), and
	// the inner Seq Scan links to the Hash container (4).
	if got.Nodes[4].Operation != model.ExplainOpContainer || got.Nodes[4].Label != "Hash" {
		t.Errorf("hash node = %s/%q, want container/\"Hash\"", got.Nodes[4].Operation, got.Nodes[4].Label)
	}
	if got.Nodes[4].ParentID == nil || *got.Nodes[4].ParentID != got.Nodes[2].ID {
		t.Errorf("hash parent = %v, want %s", got.Nodes[4].ParentID, got.Nodes[2].ID)
	}
	if got.Nodes[5].ParentID == nil || *got.Nodes[5].ParentID != got.Nodes[4].ID {
		t.Errorf("inner seq scan parent = %v, want %s", got.Nodes[5].ParentID, got.Nodes[4].ID)
	}
}

// TestPGMergeJoinMapsToDistinctOp proves Merge Join is its own operation,
// not collapsed into nested_loop or hash_join.
func TestPGMergeJoinMapsToDistinctOp(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Merge Join","Plan Rows":10,
		"Plans":[{"Node Type":"Index Scan","Plan Rows":5},{"Node Type":"Index Scan","Plan Rows":5}]}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.Nodes[0].Operation != model.ExplainOpMergeJoin || got.Nodes[0].Label != "Merge Join" {
		t.Errorf("merge join = %s/%q, want merge_join/\"Merge Join\"", got.Nodes[0].Operation, got.Nodes[0].Label)
	}
}

// TestPGSortCacheAndWorktableNodesEmitNoWarnings proves PG plan nodes that
// carry no engine spill evidence (Sort, Incremental Sort, Materialize,
// Memoize, CTE Scan, WorkTable Scan) never emit the filesort or
// temporary-table warnings, while a real Seq Scan still warns.
func TestPGSortCacheAndWorktableNodesEmitNoWarnings(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Sort","Plans":[
		{"Node Type":"Incremental Sort","Plans":[
			{"Node Type":"Materialize","Plans":[
				{"Node Type":"Memoize","Plans":[
					{"Node Type":"CTE Scan","Plans":[
						{"Node Type":"WorkTable Scan","Plans":[{"Node Type":"Seq Scan","Plan Rows":500}]}
					]}
				]}
			]}
		]}
	]}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	wantOps := []model.ExplainNodeOperation{
		model.ExplainOpSort,
		model.ExplainOpSort,
		model.ExplainOpContainer,
		model.ExplainOpContainer,
		model.ExplainOpTableAccess,
		model.ExplainOpTableAccess,
		model.ExplainOpTableAccess,
	}
	if len(got.Nodes) != len(wantOps) {
		t.Fatalf("nodes = %d, want %d: %+v", len(got.Nodes), len(wantOps), got.Nodes)
	}
	for i, want := range wantOps {
		if got.Nodes[i].Operation != want {
			t.Errorf("node %d op = %s, want %s", i, got.Nodes[i].Operation, want)
		}
	}
	codes := riskCodes(got)
	for _, unwanted := range []model.ExplainRiskCode{model.ExplainRiskFilesort, model.ExplainRiskTemporaryTable} {
		if containsRisk(codes, unwanted) {
			t.Errorf("unexpected risk %s in %v", unwanted, codes)
		}
	}
	if !containsRisk(codes, model.ExplainRiskFullTableScan) {
		t.Errorf("missing risk %s in %v", model.ExplainRiskFullTableScan, codes)
	}
}

// TestPGUnknownNodeTypeFailsVisible proves unmapped node types emit an
// unknown node plus the unknown_plan_shape risk rather than being silently
// dropped.
func TestPGUnknownNodeTypeFailsVisible(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Custom Scan","Plan Rows":3}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.Nodes[0].Operation != model.ExplainOpUnknown {
		t.Errorf("op = %s, want unknown", got.Nodes[0].Operation)
	}
	if !containsRisk(riskCodes(got), model.ExplainRiskUnknownPlanShape) {
		t.Errorf("missing unknown_plan_shape risk")
	}
}

// TestPGContainerNodePreserved proves a plan that is only structural
// machinery (Result) still normalizes — as one labeled container node —
// instead of collapsing to the empty-plan fail-closed sentinel. Costs pass
// through as scalars.
func TestPGContainerNodePreserved(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Result","Plan Rows":1,"Startup Cost":0.0,"Total Cost":0.01}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1: %+v", len(got.Nodes), got.Nodes)
	}
	n := got.Nodes[0]
	if n.Operation != model.ExplainOpContainer || n.Label != "Result" {
		t.Errorf("node = %s/%q, want container/\"Result\"", n.Operation, n.Label)
	}
	if n.StartupCost == nil || *n.StartupCost != 0.0 || n.TotalCost == nil || *n.TotalCost != 0.01 {
		t.Errorf("costs = %v..%v, want 0..0.01", n.StartupCost, n.TotalCost)
	}
}

// TestPGMalformedEnvelopesFailClosed proves non-PG shapes return the
// controlled sentinel — including a MySQL-shaped plan, which must never be
// interpreted as PG.
func TestPGMalformedEnvelopesFailClosed(t *testing.T) {
	n := NewPGExplainNormalizer()
	for name, payload := range map[string]string{
		"mysql-shaped": `{"query_block":{"select_id":1}}`,
		"empty array":  `[]`,
		"missing plan": `[{"Planning Time":0.1}]`,
		"scalar":       `42`,
	} {
		raw := pgRawPlan(t, payload)
		if _, err := n.Normalize(raw); err != ErrQueryExplainNotSupported {
			t.Errorf("%s: err = %v, want ErrQueryExplainNotSupported", name, err)
		}
	}
}

// TestPGValidateResponse proves the normalized PG output passes the same
// response contract validation the MySQL path relies on.
func TestPGValidateResponse(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Seq Scan","Plan Rows":10}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	resp := model.ExplainResponse{
		TargetResourceID: 1,
		Engine:           model.ExplainEnginePostgreSQL,
		FormatVersion:    model.ExplainFormatVersion,
		Nodes:            got.Nodes,
		Risks:            got.Risks,
	}
	if err := resp.Validate(); err != nil {
		t.Fatalf("response failed contract validation: %v", err)
	}
}

// TestPGNoFreeFormPassthrough proves bounded identifiers (Relation Name →
// relation, Index Name → index) are the ONLY values that cross the
// boundary: Filter and Index Cond strings never leave the normalizer.
func TestPGNoFreeFormPassthrough(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Index Scan","Plan Rows":10,
		"Relation Name":"secret_orders","Index Name":"idx_secret_key",
		"Index Cond":"(ssn = '123'::text)","Filter":"(balance > 100)"}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("nodes = %d, want 1", len(got.Nodes))
	}
	n := got.Nodes[0]
	if n.Relation != "secret_orders" || n.Index != "idx_secret_key" {
		t.Errorf("relation/index = %q/%q, want secret_orders/idx_secret_key", n.Relation, n.Index)
	}
	data, _ := json.Marshal(n)
	s := strings.ToLower(string(data))
	for _, banned := range []string{"ssn", "balance", "filter", "index cond"} {
		if strings.Contains(s, banned) {
			t.Errorf("node JSON must not contain %q, got: %s", banned, s)
		}
	}
}

// TestPGRelationFallbackAndBounds proves CTE Name is used when Relation
// Name is absent, and over-bound or control-char names are dropped rather
// than failing the plan.
func TestPGRelationFallbackAndBounds(t *testing.T) {
	long := strings.Repeat("x", model.MaxExplainIdentifierLen+1)
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Append","Plan Rows":3,"Plans":[
		{"Node Type":"CTE Scan","Plan Rows":1,"CTE Name":"my_cte"},
		{"Node Type":"Seq Scan","Plan Rows":1,"Relation Name":"`+long+`","Index Name":"idx\tbad"},
		{"Node Type":"Index Scan","Plan Rows":1,"Relation Name":"ok_rel","Index Name":"ok_idx"}
	]}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(got.Nodes) != 4 {
		t.Fatalf("nodes = %d, want 4: %+v", len(got.Nodes), got.Nodes)
	}
	if got.Nodes[1].Relation != "my_cte" {
		t.Errorf("CTE node relation = %q, want my_cte", got.Nodes[1].Relation)
	}
	if got.Nodes[2].Relation != "" || got.Nodes[2].Index != "" {
		t.Errorf("invalid names must be dropped, got %q/%q", got.Nodes[2].Relation, got.Nodes[2].Index)
	}
	if got.Nodes[3].Relation != "ok_rel" || got.Nodes[3].Index != "ok_idx" {
		t.Errorf("valid names must be kept, got %q/%q", got.Nodes[3].Relation, got.Nodes[3].Index)
	}
}
