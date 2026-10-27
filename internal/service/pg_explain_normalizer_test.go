// Package service provides tests for the PostgreSQL Explain plan normalizer.
// input: encoding/json, testing, internal/model
// output: TestPGExplainNormalizer* covering mapped node types, structural
// pass-through, estimated rows, risk derivation, and fail-closed envelopes
// pos: PG sibling of query_explain_normalizer_test.go — the leak-free
// contract is identical; only the raw plan shape differs
// note: fixtures are real EXPLAIN (FORMAT JSON) envelopes minus all
// leak-prone fields (Relation Name, Index Name, Filter are never present)
package service

import (
	"encoding/json"
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
	if got.Nodes[2].Operation != model.ExplainOpNestedLoop || got.Nodes[2].Label != "Hash Join" {
		t.Errorf("hash join = %s/%q, want nested_loop/\"Hash Join\"", got.Nodes[2].Operation, got.Nodes[2].Label)
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

// TestPGSortAndMaterializeRisks proves the sort/temp-table warnings fire.
func TestPGSortAndMaterializeRisks(t *testing.T) {
	raw := pgRawPlan(t, `[{"Plan":{"Node Type":"Sort","Plans":[
		{"Node Type":"Materialize","Plans":[{"Node Type":"Seq Scan","Plan Rows":500}]}
	]}}]`)
	got, err := NewPGExplainNormalizer().Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got.Nodes[0].Operation != model.ExplainOpSort {
		t.Errorf("root op = %s, want sort", got.Nodes[0].Operation)
	}
	if got.Nodes[1].Operation != model.ExplainOpTemporaryTbl {
		t.Errorf("child op = %s, want temporary_table", got.Nodes[1].Operation)
	}
	codes := riskCodes(got)
	for _, want := range []model.ExplainRiskCode{model.ExplainRiskFilesort, model.ExplainRiskTemporaryTable, model.ExplainRiskFullTableScan} {
		if !containsRisk(codes, want) {
			t.Errorf("missing risk %s in %v", want, codes)
		}
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
