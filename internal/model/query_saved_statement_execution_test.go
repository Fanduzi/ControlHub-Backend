// Package model provides domain entities for the resource management system.
// input: encoding/json, strings, testing
// output: TestQuerySavedStatementExecuteRequestDecode, TestQuerySavedStatementExecuteRequestDecodeRejectsMalformed, TestQuerySavedStatementExecuteRequestValidate
// pos: Strict template-execution request decoding and limits (values object size, maxRows, governed pagination)
package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuerySavedStatementExecuteRequestDecode(t *testing.T) {
	body := `{"values":{"status":"paid","minimum_total":"100.50","count":3,"flag":true},"maxRows":100,"pagination":{"page":1,"pageSize":10}}`
	var req QuerySavedStatementExecuteRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("unmarshal valid request: %v", err)
	}
	if req.MaxRows != 100 {
		t.Fatalf("maxRows = %d, want 100", req.MaxRows)
	}
	if req.Pagination == nil || req.Pagination.Page != 1 || req.Pagination.PageSize != 10 {
		t.Fatalf("pagination = %+v, want page 1 pageSize 10", req.Pagination)
	}
	if len(req.Values) != 4 {
		t.Fatalf("values count = %d, want 4", len(req.Values))
	}
	// RawMessage preserves the typed JSON bytes; integers stay integers and
	// booleans stay booleans so the service can re-decode with UseNumber.
	if got := string(req.Values["status"]); got != `"paid"` {
		t.Fatalf("status raw = %s, want %q", got, `"paid"`)
	}
	if got := string(req.Values["count"]); got != `3` {
		t.Fatalf("count raw = %s, want 3", got)
	}
	if got := string(req.Values["flag"]); got != `true` {
		t.Fatalf("flag raw = %s, want true", got)
	}
}

func TestQuerySavedStatementExecuteRequestDecodeRejectsMalformed(t *testing.T) {
	cases := []string{
		`{"values":[1,2]}`,
		`{"values":"paid"}`,
		`{"values":5}`,
		`{"maxRows":"100"}`,
		`{"pagination":[1]}`,
		// Unknown fields (SQL text, declarations, identities, credentials,
		// DSNs, policy/audit/result payloads) are rejected by the handler's
		// strict decoder (DisallowUnknownFields); the model only owns shape
		// and limit validation.
	}
	for _, body := range cases {
		var req QuerySavedStatementExecuteRequest
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			continue // malformed shape is an acceptable rejection
		}
		if err := req.Validate(); err == nil {
			t.Errorf("body %s: expected validation failure, got nil", body)
		}
	}
}

func TestQuerySavedStatementExecuteRequestValidate(t *testing.T) {
	oversized := `"` + strings.Repeat("x", 16*1024) + `"`
	oversizedValues := QuerySavedStatementExecuteRequest{
		Values: map[string]json.RawMessage{"a": json.RawMessage(oversized)},
	}
	if err := oversizedValues.Validate(); err == nil {
		t.Fatal("expected oversized values object to be rejected")
	}

	negativeMaxRows := QuerySavedStatementExecuteRequest{MaxRows: -1}
	if err := negativeMaxRows.Validate(); err == nil {
		t.Fatal("expected negative maxRows to be rejected")
	}

	badPage := QuerySavedStatementExecuteRequest{
		Pagination: &QueryExecutePaginationRequest{Page: 0, PageSize: 10},
	}
	if err := badPage.Validate(); err == nil {
		t.Fatal("expected page 0 to be rejected")
	}

	badPageSize := QuerySavedStatementExecuteRequest{
		Pagination: &QueryExecutePaginationRequest{Page: 1, PageSize: 7},
	}
	if err := badPageSize.Validate(); err == nil {
		t.Fatal("expected pageSize 7 to be rejected")
	}

	valid := QuerySavedStatementExecuteRequest{
		Values:     map[string]json.RawMessage{"status": json.RawMessage(`"paid"`)},
		MaxRows:    100,
		Pagination: &QueryExecutePaginationRequest{Page: 1, PageSize: 10},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}

	// A static statement executes with no values object at all.
	if err := (QuerySavedStatementExecuteRequest{}).Validate(); err != nil {
		t.Fatalf("empty request rejected: %v", err)
	}
}
