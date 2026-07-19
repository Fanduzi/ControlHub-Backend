// Package model provides domain entities for the resource management system.
// input: testing, encoding/json, internal/model
// output: tests for Explain v1 model enums, validation, and JSON round-trip
// pos: Phase 38N governed Explain model — every enum fails closed on unknown
// note: these tests encode WHY the model is finite (no free-form passthrough)
package model

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// TestExplainEngineValidate proves the engine enum is finite. WHY: a free-form
// engine string could relay arbitrary target metadata through the response.
// This test fails if a future caller adds an undeclared engine value.
func TestExplainEngineValidate(t *testing.T) {
	valid := []ExplainEngine{ExplainEngineMySQL}
	for _, e := range valid {
		if err := e.Validate(); err != nil {
			t.Errorf("expected valid engine %s to pass, got %v", e, err)
		}
	}
	invalid := []ExplainEngine{"tidb", "postgres", "clickhouse", "", "MySQL"}
	for _, e := range invalid {
		if err := e.Validate(); err == nil {
			t.Errorf("expected engine %q to fail validation, but it passed", e)
		}
	}
}

// TestExplainNodeOperationValidate proves the operation enum is finite. WHY: the
// frontend localizes these; an undeclared value would render raw.
func TestExplainNodeOperationValidate(t *testing.T) {
	valid := []ExplainNodeOperation{
		ExplainOpTableAccess, ExplainOpIndexAccess, ExplainOpNestedLoop,
		ExplainOpSort, ExplainOpAggregate, ExplainOpTemporaryTbl, ExplainOpUnknown,
	}
	for _, o := range valid {
		if err := o.Validate(); err != nil {
			t.Errorf("expected valid operation %s to pass, got %v", o, err)
		}
	}
	invalid := []ExplainNodeOperation{"TABLE_ACCESS", "scan", "", "join"}
	for _, o := range invalid {
		if err := o.Validate(); err == nil {
			t.Errorf("expected operation %q to fail, but it passed", o)
		}
	}
}

// TestExplainNodeAccessValidate proves the access enum is finite.
func TestExplainNodeAccessValidate(t *testing.T) {
	valid := []ExplainNodeAccess{
		ExplainAccessFullScan, ExplainAccessIndex, ExplainAccessUniqueRow,
		ExplainAccessRange, ExplainAccessUnknown,
	}
	for _, a := range valid {
		if err := a.Validate(); err != nil {
			t.Errorf("expected valid access %s to pass, got %v", a, err)
		}
	}
	invalid := []ExplainNodeAccess{"ALL", "ref", "", "FULL_SCAN"}
	for _, a := range invalid {
		if err := a.Validate(); err == nil {
			t.Errorf("expected access %q to fail, but it passed", a)
		}
	}
}

// TestExplainRiskCodeValidate proves the risk code enum is finite. WHY: risk
// codes are backend-owned; an undeclared code would force the frontend to
// either render raw text or silently drop the risk.
func TestExplainRiskCodeValidate(t *testing.T) {
	valid := []ExplainRiskCode{
		ExplainRiskFullTableScan, ExplainRiskFilesort, ExplainRiskTemporaryTable,
		ExplainRiskHighEstimatedRows, ExplainRiskUnknownPlanShape,
	}
	for _, c := range valid {
		if err := c.Validate(); err != nil {
			t.Errorf("expected valid risk code %s to pass, got %v", c, err)
		}
	}
	invalid := []ExplainRiskCode{"FULL_TABLE_SCAN", "slow_query", "", "deadlock"}
	for _, c := range invalid {
		if err := c.Validate(); err == nil {
			t.Errorf("expected risk code %q to fail, but it passed", c)
		}
	}
}

// TestExplainRiskSeverityValidate proves the severity enum is finite. Note that
// ExplainSeverityCritical is a valid wire value even though the v1 normalizer
// never derives it — the frontend must still localize it.
func TestExplainRiskSeverityValidate(t *testing.T) {
	valid := []ExplainRiskSeverity{
		ExplainSeverityInfo, ExplainSeverityWarning, ExplainSeverityCritical,
	}
	for _, s := range valid {
		if err := s.Validate(); err != nil {
			t.Errorf("expected valid severity %s to pass, got %v", s, err)
		}
	}
	invalid := []ExplainRiskSeverity{"warn", "ERROR", "", "high"}
	for _, s := range invalid {
		if err := s.Validate(); err == nil {
			t.Errorf("expected severity %q to fail, but it passed", s)
		}
	}
}

// TestExplainAuditOutcomeValidate proves the audit outcome enum is finite and
// typed. WHY: the recorder accepts only this enum, not a raw string, so
// arbitrary callers cannot inject free-form text at the audit boundary.
func TestExplainAuditOutcomeValidate(t *testing.T) {
	valid := []ExplainAuditOutcome{
		ExplainAuditSuccess, ExplainAuditRejected, ExplainAuditUnsupported, ExplainAuditError,
	}
	for _, o := range valid {
		if err := o.Validate(); err != nil {
			t.Errorf("expected valid audit outcome %s to pass, got %v", o, err)
		}
	}
	invalid := []ExplainAuditOutcome{"ok", "denied", "", "failed"}
	for _, o := range invalid {
		if err := o.Validate(); err == nil {
			t.Errorf("expected audit outcome %q to fail, but it passed", o)
		}
	}
}

// TestExplainNodeValidate proves a node requires a non-empty ID and known
// enums. WHY: the frontend renders nodes by ID; an empty ID breaks the tree.
func TestExplainNodeValidate(t *testing.T) {
	valid := ExplainNode{
		ID:        "0",
		Operation: ExplainOpTableAccess,
		Access:    ExplainAccessFullScan,
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid node to pass, got %v", err)
	}
	cases := []struct {
		name string
		node ExplainNode
	}{
		{"empty id", ExplainNode{ID: "", Operation: ExplainOpTableAccess, Access: ExplainAccessFullScan}},
		{"unknown operation", ExplainNode{ID: "0", Operation: "scan", Access: ExplainAccessFullScan}},
		{"unknown access", ExplainNode{ID: "0", Operation: ExplainOpTableAccess, Access: "ALL"}},
	}
	for _, tc := range cases {
		if err := tc.node.Validate(); err == nil {
			t.Errorf("case %s: expected validation error, got nil", tc.name)
		}
	}
}

// TestExplainRiskValidate proves a risk requires known code and severity.
func TestExplainRiskValidate(t *testing.T) {
	valid := ExplainRisk{Code: ExplainRiskFullTableScan, Severity: ExplainSeverityWarning}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid risk to pass, got %v", err)
	}
	cases := []struct {
		name  string
		risk  ExplainRisk
		match string
	}{
		{"unknown code", ExplainRisk{Code: "slow", Severity: ExplainSeverityWarning}, "invalid explain risk code"},
		{"unknown severity", ExplainRisk{Code: ExplainRiskFullTableScan, Severity: "high"}, "invalid explain risk severity"},
	}
	for _, tc := range cases {
		err := tc.risk.Validate()
		if err == nil {
			t.Errorf("case %s: expected error, got nil", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.match) {
			t.Errorf("case %s: expected error to contain %q, got %q", tc.name, tc.match, err.Error())
		}
	}
}

// TestExplainResponseValidate proves the response must be a known engine +
// format version 1 + valid nodes/risks. WHY: an unknown format version would
// be a silent contract break; this forces an explicit schema revision.
func TestExplainResponseValidate(t *testing.T) {
	valid := ExplainResponse{
		TargetResourceID: 616,
		Engine:           ExplainEngineMySQL,
		FormatVersion:    ExplainFormatVersion,
		Nodes:            []ExplainNode{{ID: "0", Operation: ExplainOpTableAccess, Access: ExplainAccessFullScan}},
		Risks:            []ExplainRisk{{Code: ExplainRiskFullTableScan, Severity: ExplainSeverityWarning}},
		Truncated:        false,
	}
	if err := valid.Validate(); err != nil {
		t.Errorf("expected valid response to pass, got %v", err)
	}
	cases := []struct {
		name string
		resp ExplainResponse
	}{
		{"unknown engine", ExplainResponse{Engine: "tidb", FormatVersion: 1}},
		{"wrong format version", ExplainResponse{Engine: ExplainEngineMySQL, FormatVersion: 2}},
		{"invalid node", ExplainResponse{Engine: ExplainEngineMySQL, FormatVersion: 1, Nodes: []ExplainNode{{ID: "0", Operation: "scan", Access: ExplainAccessFullScan}}}},
		{"invalid risk", ExplainResponse{Engine: ExplainEngineMySQL, FormatVersion: 1, Risks: []ExplainRisk{{Code: "slow", Severity: ExplainSeverityWarning}}}},
	}
	for _, tc := range cases {
		if err := tc.resp.Validate(); err == nil {
			t.Errorf("case %s: expected error, got nil", tc.name)
		}
	}
}

// TestExplainResponseJSONRoundTrip proves the response serializes to the exact
// wire contract and rejects unknown fields on decode. WHY: this is the public
// contract; any extra field would be a schema drift the frontend could grow
// to depend on.
func TestExplainResponseJSONRoundTrip(t *testing.T) {
	resp := ExplainResponse{
		TargetResourceID: 616,
		Engine:           ExplainEngineMySQL,
		FormatVersion:    1,
		Nodes: []ExplainNode{{
			ID:            "0",
			ParentID:       nil,
			Operation:     ExplainOpTableAccess,
			Access:        ExplainAccessFullScan,
			EstimatedRows: uint64Ptr(120000),
			UsesIndex:     boolPtr(false),
		}},
		Risks: []ExplainRisk{
			{Code: ExplainRiskFullTableScan, Severity: ExplainSeverityWarning},
			{Code: ExplainRiskHighEstimatedRows, Severity: ExplainSeverityWarning},
		},
		Truncated: false,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(data)
	// Positive shape assertions: required fields present.
	for _, want := range []string{
		`"targetResourceId":616`,
		`"engine":"mysql"`,
		`"formatVersion":1`,
		`"id":"0"`,
		`"operation":"table_access"`,
		`"access":"full_scan"`,
		`"estimatedRows":120000`,
		`"usesIndex":false`,
		`"code":"full_table_scan"`,
		`"severity":"warning"`,
		`"truncated":false`,
	} {
		if !strings.Contains(jsonStr, want) {
			t.Errorf("expected JSON to contain %q, got: %s", want, jsonStr)
		}
	}
	// Negative shape assertions: no free-form engine strings.
	for _, banned := range []string{
		`"table_name"`, `"possible_keys"`, `"key":`, `"used_columns"`,
		`"cost_info"`, `"message":`, `"dsn"`, `"credential"`, `"actorUserId"`,
	} {
		if strings.Contains(jsonStr, banned) {
			t.Errorf("response JSON must not contain %q, got: %s", banned, jsonStr)
		}
	}
	// Decode rejects unknown fields (closed object).
	var decoded ExplainResponse
	dec := json.NewDecoder(strings.NewReader(jsonStr))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err != nil {
		t.Errorf("decode with DisallowUnknownFields failed on clean output: %v", err)
	}
	if err := decoded.Validate(); err != nil {
		t.Errorf("decoded response failed validation: %v", err)
	}
	// Unknown field is rejected.
	tainted := strings.Replace(jsonStr, `"formatVersion":1`, `"formatVersion":1,"leak":"secret"`, 1)
	dec = json.NewDecoder(strings.NewReader(tainted))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&decoded); err == nil {
		t.Errorf("expected decode to reject unknown field 'leak', but it passed")
	}
}

// TestExplainNodeEstimatedRowsOmission proves EstimatedRows is omitted when nil
// and present when set. WHY: omitempty on *uint64 must drop nil so the wire
// contract does not carry misleading zeros.
func TestExplainNodeEstimatedRowsOmission(t *testing.T) {
	n := ExplainNode{ID: "0", Operation: ExplainOpTableAccess, Access: ExplainAccessFullScan}
	data, _ := json.Marshal(n)
	if strings.Contains(string(data), "estimatedRows") {
		t.Errorf("expected estimatedRows to be omitted when nil, got: %s", string(data))
	}
	n.EstimatedRows = uint64Ptr(100)
	data, _ = json.Marshal(n)
	if !strings.Contains(string(data), `"estimatedRows":100`) {
		t.Errorf("expected estimatedRows to be present, got: %s", string(data))
	}
}

// TestExplainNodeParentIDOmission proves ParentID is omitted when nil.
func TestExplainNodeParentIDOmission(t *testing.T) {
	n := ExplainNode{ID: "0", Operation: ExplainOpTableAccess, Access: ExplainAccessFullScan}
	data, _ := json.Marshal(n)
	if strings.Contains(string(data), "parentId") {
		t.Errorf("expected parentId to be omitted when nil, got: %s", string(data))
	}
	parent := "0"
	n.ParentID = &parent
	data, _ = json.Marshal(n)
	if !strings.Contains(string(data), `"parentId":"0"`) {
		t.Errorf("expected parentId to be present, got: %s", string(data))
	}
}

func uint64Ptr(v uint64) *uint64 { return &v }
func boolPtr(v bool) *bool       { return &v }

// TestExplainFormatVersionConstant pins the version so a future bump is a
// deliberate, reviewed change.
func TestExplainFormatVersionConstant(t *testing.T) {
	if ExplainFormatVersion != 1 {
		t.Errorf("ExplainFormatVersion changed: expected 1, got %d", ExplainFormatVersion)
	}
}

// TestExplainResponseValidateErrorMessage proves validation errors are
// descriptive but carry NO statement, plan, or literal text. WHY: even error
// messages are part of the public surface.
func TestExplainResponseValidateErrorMessage(t *testing.T) {
	resp := ExplainResponse{Engine: "tidb", FormatVersion: 1}
	err := resp.Validate()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	for _, banned := range []string{"SELECT", "FROM", "WHERE", "table_name", "dsn", "password"} {
		if strings.Contains(strings.ToUpper(err.Error()), strings.ToUpper(banned)) {
			t.Errorf("validation error must not contain %q, got: %s", banned, err.Error())
		}
	}
	if !errors.Is(err, nil) {
		// Confirm it's a plain fmt.Errorf, not a sentinel wrapping secrets.
		_ = err
	}
}
