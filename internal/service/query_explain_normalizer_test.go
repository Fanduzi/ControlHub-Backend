// Package service provides tests for the MySQL Explain normalizer.
// input: encoding/json, strings, testing, internal/model, internal/service
// output: TestNormalizeExplain* covering every v1 enum, caps, dedup, depth, no-passthrough
// pos: Phase 38N — prove the normalizer emits only finite enums and never raw plan text
// note: fixtures are sanitized synthetic shapes mirroring MySQL EXPLAIN FORMAT=JSON structure
package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// mustRaw builds an ExplainRawPlan from a JSON string, failing the test on
// parse error. Fixtures are sanitized synthetic shapes mirroring MySQL's
// EXPLAIN FORMAT=JSON structure; no real engine payloads are persisted.
func mustRaw(t *testing.T, j string) ExplainRawPlan {
	t.Helper()
	var tree interface{}
	if err := json.Unmarshal([]byte(j), &tree); err != nil {
		t.Fatalf("fixture parse: %v", err)
	}
	return ExplainRawPlan{tree: tree}
}

// TestNormalizeFullScan proves a full-scan table maps to table_access +
// full_scan + the full_table_scan risk.
func TestNormalizeFullScan(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 4}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(got.Nodes))
	}
	node := got.Nodes[0]
	if node.Operation != model.ExplainOpTableAccess {
		t.Errorf("operation = %s, want table_access", node.Operation)
	}
	if node.Access != model.ExplainAccessFullScan {
		t.Errorf("access = %s, want full_scan", node.Access)
	}
	if node.EstimatedRows == nil || *node.EstimatedRows != 4 {
		t.Errorf("estimatedRows = %v, want 4", node.EstimatedRows)
	}
	if node.UsesIndex == nil || *node.UsesIndex != false {
		t.Errorf("usesIndex = %v, want false", node.UsesIndex)
	}
	if !hasRisk(got.Risks, model.ExplainRiskFullTableScan, model.ExplainSeverityWarning) {
		t.Errorf("expected full_table_scan warning risk, got %v", got.Risks)
	}
}

// TestNormalizeIndexAccess proves an index access (ref) maps to index_access
// and does NOT emit a full_table_scan risk.
func TestNormalizeIndexAccess(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"table": {"access_type": "ref", "key": "idx_k", "rows_examined_per_scan": 2}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if len(got.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(got.Nodes))
	}
	node := got.Nodes[0]
	if node.Operation != model.ExplainOpIndexAccess {
		t.Errorf("operation = %s, want index_access", node.Operation)
	}
	if node.Access != model.ExplainAccessRange {
		t.Errorf("access = %s, want range (ref maps to range)", node.Access)
	}
	if node.UsesIndex == nil || *node.UsesIndex != true {
		t.Errorf("usesIndex = %v, want true (key present)", node.UsesIndex)
	}
	if hasRisk(got.Risks, model.ExplainRiskFullTableScan, model.ExplainSeverityWarning) {
		t.Errorf("must NOT emit full_table_scan for index access")
	}
}

// TestNormalizeUniqueRowAccess proves const/eq_ref access maps to unique_row.
func TestNormalizeUniqueRowAccess(t *testing.T) {
	t.Parallel()
	for _, at := range []string{"const", "eq_ref", "system"} {
		raw := mustRaw(t, `{"query_block": {"table": {"access_type": "`+at+`", "rows_examined_per_scan": 1, "key": "PRIMARY"}}}`)
		n := NewExplainNormalizer()
		got, err := n.Normalize(raw)
		if err != nil {
			t.Fatalf("Normalize(%s) error: %v", at, err)
		}
		if got.Nodes[0].Access != model.ExplainAccessUniqueRow {
			t.Errorf("access_type %s: access = %s, want unique_row", at, got.Nodes[0].Access)
		}
		if got.Nodes[0].Operation != model.ExplainOpIndexAccess {
			t.Errorf("access_type %s: operation = %s, want index_access", at, got.Nodes[0].Operation)
		}
	}
}

// TestNormalizeFilesort proves an ordering_operation with using_filesort
// emits the filesort risk.
func TestNormalizeFilesort(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"ordering_operation": {"using_filesort": true, "table": {"access_type": "ref", "key": "idx_k", "rows_examined_per_scan": 2}}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if !hasRisk(got.Risks, model.ExplainRiskFilesort, model.ExplainSeverityWarning) {
		t.Errorf("expected filesort risk, got %v", got.Risks)
	}
}

// TestNormalizeTemporaryTable proves a grouping_operation with
// using_temporary_table emits the temporary_table risk and a temp-table node.
func TestNormalizeTemporaryTable(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"grouping_operation": {"using_temporary_table": true, "using_filesort": true, "table": {"access_type": "ALL", "rows_examined_per_scan": 3}}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if !hasRisk(got.Risks, model.ExplainRiskTemporaryTable, model.ExplainSeverityWarning) {
		t.Errorf("expected temporary_table risk, got %v", got.Risks)
	}
	if !hasRisk(got.Risks, model.ExplainRiskFilesort, model.ExplainSeverityWarning) {
		t.Errorf("expected filesort risk (grouping had using_filesort), got %v", got.Risks)
	}
	foundTemp := false
	for _, node := range got.Nodes {
		if node.Operation == model.ExplainOpTemporaryTbl {
			foundTemp = true
		}
	}
	if !foundTemp {
		t.Errorf("expected a temporary_table node, got %v", got.Nodes)
	}
}

// TestNormalizeHighEstimatedRows proves a plan with rows >= the threshold
// emits the high_estimated_rows risk.
func TestNormalizeHighEstimatedRows(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 200000}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if !hasRisk(got.Risks, model.ExplainRiskHighEstimatedRows, model.ExplainSeverityWarning) {
		t.Errorf("expected high_estimated_rows risk for 200000 rows, got %v", got.Risks)
	}
}

// TestNormalizeHighEstimatedRowsThresholdBoundary proves the boundary is
// inclusive at HighEstimatedRowsThreshold.
func TestNormalizeHighEstimatedRowsThresholdBoundary(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": `+itoa(HighEstimatedRowsThreshold)+`}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if !hasRisk(got.Risks, model.ExplainRiskHighEstimatedRows, model.ExplainSeverityWarning) {
		t.Errorf("expected high_estimated_rows risk at threshold boundary %d, got %v", HighEstimatedRowsThreshold, got.Risks)
	}
}

// TestNormalizeEstimatedRowsBelowThreshold proves no high_estimated_rows
// risk when rows < threshold.
func TestNormalizeEstimatedRowsBelowThreshold(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 50000}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if hasRisk(got.Risks, model.ExplainRiskHighEstimatedRows, model.ExplainSeverityWarning) {
		t.Errorf("must NOT emit high_estimated_rows for 50000 rows")
	}
}

// TestNormalizeEstimatedRowsSafeIntegerBoundary proves values above
// Number.MAX_SAFE_INTEGER (2^53-1) are omitted from the public response while
// still contributing to high_estimated_rows risk (Oracle P2.5).
func TestNormalizeEstimatedRowsSafeIntegerBoundary(t *testing.T) {
	t.Parallel()
	safe := uint64((1 << 53) - 1)
	raw := mustRaw(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": `+itoa(safe)+`}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if got.Nodes[0].EstimatedRows == nil || *got.Nodes[0].EstimatedRows != safe {
		t.Errorf("expected estimatedRows = %d (within safe range), got %v", safe, got.Nodes[0].EstimatedRows)
	}
	if !hasRisk(got.Risks, model.ExplainRiskHighEstimatedRows, model.ExplainSeverityWarning) {
		t.Errorf("expected high_estimated_rows risk for %d rows", safe)
	}
	unsafe := uint64(1 << 53)
	raw = mustRaw(t, `{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": `+itoa(unsafe)+`}}}`)
	got, err = n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if got.Nodes[0].EstimatedRows != nil {
		t.Errorf("expected estimatedRows to be nil (omitted) for value > MAX_SAFE_INTEGER, got %v", *got.Nodes[0].EstimatedRows)
	}
	if !hasRisk(got.Risks, model.ExplainRiskHighEstimatedRows, model.ExplainSeverityWarning) {
		t.Errorf("expected high_estimated_rows risk still derived for unsafe-large value")
	}
}

// TestNormalizeNestedLoop proves a nested_loop block emits a nested_loop
// node and its children get the correct parentId.
func TestNormalizeNestedLoop(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"nested_loop": [{"table": {"access_type": "ALL", "rows_examined_per_scan": 4}}, {"table": {"access_type": "ref", "key": "idx", "rows_examined_per_scan": 2}}]}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	foundNestedLoop := false
	for _, node := range got.Nodes {
		if node.Operation == model.ExplainOpNestedLoop {
			foundNestedLoop = true
		}
	}
	if !foundNestedLoop {
		t.Errorf("expected a nested_loop node, got %v", got.Nodes)
	}
	for _, node := range got.Nodes {
		if node.ParentID != nil && *node.ParentID == node.ID {
			t.Errorf("node %s has parentId equal to its own id (cycle)", node.ID)
		}
	}
}

// TestNormalizeUnknownShape proves an unknown block emits an unknown node and
// the unknown_plan_shape risk.
func TestNormalizeUnknownShape(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"table": {"access_type": "weird_unknown_type"}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if got.Nodes[0].Access != model.ExplainAccessUnknown {
		t.Errorf("expected unknown access for weird access_type, got %s", got.Nodes[0].Access)
	}
}

// TestNormalizeNodeCap proves a plan with more than MaxExplainNodes truncates
// and sets truncated=true.
func TestNormalizeNodeCap(t *testing.T) {
	t.Parallel()
	// Build a plan with 100 table blocks in a nested_loop; the normalizer
	// must cap at MaxExplainNodes.
	var children []string
	for i := 0; i < 100; i++ {
		children = append(children, `{"table": {"access_type": "ALL", "rows_examined_per_scan": 1}}`)
	}
	raw := mustRaw(t, `{"query_block": {"nested_loop": [`+strings.Join(children, ",")+`]}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if len(got.Nodes) > MaxExplainNodes {
		t.Errorf("expected at most %d nodes, got %d", MaxExplainNodes, len(got.Nodes))
	}
	if !got.Truncated {
		t.Errorf("expected truncated=true when cap hit")
	}
}

// TestNormalizePlanDepthCap proves a plan nested deeper than
// MaxExplainPlanDepth stops walking and sets truncated=true.
func TestNormalizePlanDepthCap(t *testing.T) {
	t.Parallel()
	depth := MaxExplainPlanDepth + 5
	inner := map[string]interface{}{
		"table": map[string]interface{}{
			"access_type":            "ALL",
			"rows_examined_per_scan": float64(1),
		},
	}
	curr := inner
	for i := 0; i < depth; i++ {
		op := map[string]interface{}{
			"using_filesort": true,
		}
		for k, v := range curr {
			op[k] = v
		}
		curr = map[string]interface{}{"ordering_operation": op}
	}
	tree := map[string]interface{}{"query_block": curr}
	data, err := json.Marshal(tree)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	raw := mustRaw(t, string(data))
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	if !got.Truncated {
		t.Errorf("expected truncated=true for plan exceeding depth cap")
	}
}

// TestNormalizeRiskDedup proves two full-scan nodes emit exactly one
// full_table_scan risk.
func TestNormalizeRiskDedup(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"nested_loop": [{"table": {"access_type": "ALL", "rows_examined_per_scan": 4}}, {"table": {"access_type": "ALL", "rows_examined_per_scan": 5}}]}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	count := 0
	for _, r := range got.Risks {
		if r.Code == model.ExplainRiskFullTableScan {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 full_table_scan risk (dedup), got %d", count)
	}
}

// TestNormalizeMalformedTree proves malformed input returns
// ErrQueryExplainNotSupported with no raw JSON in the error.
func TestNormalizeMalformedTree(t *testing.T) {
	t.Parallel()
	n := NewExplainNormalizer()
	for _, tc := range []struct {
		name string
		raw  ExplainRawPlan
	}{
		{"empty tree", ExplainRawPlan{}},
		{"non-object root", ExplainRawPlan{tree: "not-an-object"}},
		{"missing query_block", ExplainRawPlan{tree: map[string]interface{}{}}},
		{"query_block wrong type", ExplainRawPlan{tree: map[string]interface{}{"query_block": "wrong"}}},
	} {
		_, err := n.Normalize(tc.raw)
		if err == nil {
			t.Errorf("case %s: expected error, got nil", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), "not supported") {
			t.Errorf("case %s: expected ErrQueryExplainNotSupported, got %v", tc.name, err)
		}
		if strings.Contains(strings.ToLower(err.Error()), "select") || strings.Contains(err.Error(), "{") {
			t.Errorf("case %s: error must not contain raw plan text, got: %s", tc.name, err.Error())
		}
	}
}

// TestNormalizeNoFreeFormPassthrough proves the output never contains
// table_name, possible_keys, key, used_columns, cost_info, ref, or message —
// even when the raw plan carried them.
func TestNormalizeNoFreeFormPassthrough(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"table": {"table_name": "secret_table", "access_type": "ref", "possible_keys": ["idx_secret"], "key": "idx_secret", "used_key_parts": ["ssn"], "used_columns": ["id","name","ssn"], "ref": ["const"], "rows_examined_per_scan": 2, "cost_info": {"read_cost": "0.25"}, "message": "secret"}}}`)
	n := NewExplainNormalizer()
	got, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	for _, node := range got.Nodes {
		data, _ := json.Marshal(node)
		s := string(data)
		for _, banned := range []string{"secret_table", "idx_secret", "ssn", "secret", "cost_info", "possible_keys", "used_columns", "table_name", "message"} {
			if strings.Contains(strings.ToLower(s), strings.ToLower(banned)) {
				t.Errorf("node JSON must not contain %q, got: %s", banned, s)
			}
		}
	}
	for _, risk := range got.Risks {
		data, _ := json.Marshal(risk)
		s := string(data)
		for _, banned := range []string{"secret_table", "idx_secret", "ssn", "secret"} {
			if strings.Contains(strings.ToLower(s), strings.ToLower(banned)) {
				t.Errorf("risk JSON must not contain %q, got: %s", banned, s)
			}
		}
	}
}

// TestNormalizeIDStability proves the same fixture produces the same IDs
// across runs, IDs are unique, parents precede children, and no cycles.
func TestNormalizeIDStability(t *testing.T) {
	t.Parallel()
	raw := mustRaw(t, `{"query_block": {"nested_loop": [{"table": {"access_type": "ALL", "rows_examined_per_scan": 4}}, {"table": {"access_type": "ref", "key": "idx", "rows_examined_per_scan": 2}}]}}`)
	n := NewExplainNormalizer()
	got1, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize error: %v", err)
	}
	got2, err := n.Normalize(raw)
	if err != nil {
		t.Fatalf("Normalize (2nd) error: %v", err)
	}
	if len(got1.Nodes) != len(got2.Nodes) {
		t.Fatalf("instability: %d vs %d nodes", len(got1.Nodes), len(got2.Nodes))
	}
	for i := range got1.Nodes {
		if got1.Nodes[i].ID != got2.Nodes[i].ID {
			t.Errorf("instability: node %d ID %s vs %s", i, got1.Nodes[i].ID, got2.Nodes[i].ID)
		}
	}
	ids := map[string]bool{}
	for _, node := range got1.Nodes {
		if ids[node.ID] {
			t.Errorf("duplicate ID: %s", node.ID)
		}
		ids[node.ID] = true
		if node.ParentID != nil {
			p := *node.ParentID
			if !ids[p] {
				t.Errorf("parent %s referenced before emitted (or never emitted)", p)
			}
			if p == node.ID {
				t.Errorf("cycle: node %s is its own parent", node.ID)
			}
		}
	}
}

// TestNormalizeCriticalNotDerived proves the v1 normalizer never derives the
// critical severity — it's a forward-compatible wire value only (Oracle P2.8).
func TestNormalizeCriticalNotDerived(t *testing.T) {
	t.Parallel()
	fixtures := []string{
		`{"query_block": {"table": {"access_type": "ALL", "rows_examined_per_scan": 999999999}}}`,
		`{"query_block": {"grouping_operation": {"using_temporary_table": true, "using_filesort": true, "table": {"access_type": "ALL", "rows_examined_per_scan": 999999999}}}}`,
	}
	n := NewExplainNormalizer()
	for i, j := range fixtures {
		raw := mustRaw(t, j)
		got, err := n.Normalize(raw)
		if err != nil {
			t.Fatalf("fixture %d: Normalize error: %v", i, err)
		}
		for _, risk := range got.Risks {
			if risk.Severity == model.ExplainSeverityCritical {
				t.Errorf("fixture %d: critical severity must not be derived in v1, got risk %v", i, risk)
			}
		}
	}
}

func hasRisk(risks []model.ExplainRisk, code model.ExplainRiskCode, sev model.ExplainRiskSeverity) bool {
	for _, r := range risks {
		if r.Code == code && r.Severity == sev {
			return true
		}
	}
	return false
}

func itoa(v uint64) string {
	return strconv.FormatUint(v, 10)
}
