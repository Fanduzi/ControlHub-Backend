// Package model provides tests for query-workspace validation.
// input: encoding/json, strings, testing
// output: bounded/control-free metadata, opaque SQL preservation, and execution-statement JSON contract tests
// pos: Regression coverage for query workspace and reusable execution statement models
// note: if this file changes, update this header and module README.md.
package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQueryWorkspacePutRequestAcceptsOpaqueStatementsUnchanged(t *testing.T) {
	t.Parallel()
	database := "orders db"
	statements := []string{"", "select", "not sql at all", "DELETE FROM orders", "\n\t SELECT  *\nFROM orders \t", "SELECT\x00invalid"}
	worksheets := make([]QueryWorkspaceWorksheet, len(statements))
	for i, statement := range statements {
		worksheets[i] = QueryWorkspaceWorksheet{
			ID:               "worksheet-" + string(rune('a'+i)),
			Name:             "Worksheet",
			TargetResourceID: uint64(i + 1),
			Statement:        statement,
			ActiveDatabase:   &database,
		}
	}
	req := QueryWorkspacePutRequest{ExpectedVersion: 7, Worksheets: worksheets}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate() rejected opaque worksheet SQL: %v", err)
	}
	for i, statement := range statements {
		if got := req.Worksheets[i].Statement; got != statement {
			t.Fatalf("statement %d changed to %q, want exact %q", i, got, statement)
		}
	}
}

func TestQueryWorkspacePutRequestRejectsInvalidAggregateShape(t *testing.T) {
	t.Parallel()
	database := "orders"
	valid := QueryWorkspaceWorksheet{ID: "worksheet-1", Name: "Worksheet 1", TargetResourceID: 9, Statement: "select 1", ActiveDatabase: &database}
	tests := []struct {
		name       string
		worksheets []QueryWorkspaceWorksheet
	}{
		{name: "too many worksheets", worksheets: make([]QueryWorkspaceWorksheet, MaxQueryWorkspaceWorksheets+1)},
		{name: "empty id", worksheets: []QueryWorkspaceWorksheet{{Name: valid.Name, TargetResourceID: valid.TargetResourceID}}},
		{name: "long id", worksheets: []QueryWorkspaceWorksheet{{ID: strings.Repeat("i", MaxQueryWorkspaceWorksheetIDLength+1), Name: valid.Name, TargetResourceID: valid.TargetResourceID}}},
		{name: "id control character", worksheets: []QueryWorkspaceWorksheet{{ID: "worksheet\n2", Name: valid.Name, TargetResourceID: valid.TargetResourceID}}},
		{name: "empty name", worksheets: []QueryWorkspaceWorksheet{{ID: valid.ID, TargetResourceID: valid.TargetResourceID}}},
		{name: "long name", worksheets: []QueryWorkspaceWorksheet{{ID: valid.ID, Name: strings.Repeat("n", MaxQueryWorkspaceWorksheetNameLength+1), TargetResourceID: valid.TargetResourceID}}},
		{name: "name control character", worksheets: []QueryWorkspaceWorksheet{{ID: valid.ID, Name: "Worksheet\t2", TargetResourceID: valid.TargetResourceID}}},
		{name: "zero target", worksheets: []QueryWorkspaceWorksheet{{ID: valid.ID, Name: valid.Name}}},
		{name: "long statement", worksheets: []QueryWorkspaceWorksheet{{ID: valid.ID, Name: valid.Name, TargetResourceID: valid.TargetResourceID, Statement: strings.Repeat("s", MaxSavedStatementSize+1)}}},
		{name: "empty active database", worksheets: []QueryWorkspaceWorksheet{{ID: valid.ID, Name: valid.Name, TargetResourceID: valid.TargetResourceID, ActiveDatabase: new(string)}}},
		{name: "long active database", worksheets: []QueryWorkspaceWorksheet{{ID: valid.ID, Name: valid.Name, TargetResourceID: valid.TargetResourceID, ActiveDatabase: stringPointer(strings.Repeat("d", MaxQueryWorkspaceDatabaseNameLength+1))}}},
		{name: "active database control character", worksheets: []QueryWorkspaceWorksheet{{ID: valid.ID, Name: valid.Name, TargetResourceID: valid.TargetResourceID, ActiveDatabase: stringPointer("orders\narchive")}}},
		{name: "duplicate ids", worksheets: []QueryWorkspaceWorksheet{valid, valid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := (QueryWorkspacePutRequest{Worksheets: tt.worksheets}).Validate(); err == nil {
				t.Fatal("Validate() = nil, want error")
			}
		})
	}
}

func TestQueryWorkspacePutRequestRejectsOversizedJSON(t *testing.T) {
	t.Parallel()
	worksheets := make([]QueryWorkspaceWorksheet, MaxQueryWorkspaceWorksheets)
	for i := range worksheets {
		worksheets[i] = QueryWorkspaceWorksheet{
			ID:               "worksheet-" + strings.Repeat("i", i+1),
			Name:             "Worksheet",
			TargetResourceID: uint64(i + 1),
			Statement:        strings.Repeat("s", MaxSavedStatementSize),
		}
	}
	raw, err := json.Marshal(worksheets)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if len(raw) <= MaxQueryWorkspaceJSONSize {
		t.Fatalf("fixture JSON = %d bytes, want over %d", len(raw), MaxQueryWorkspaceJSONSize)
	}
	if err := (QueryWorkspacePutRequest{Worksheets: worksheets}).Validate(); err == nil {
		t.Fatal("Validate() = nil, want total JSON size error")
	}
}

func TestQueryExecutionListJSONOmitsFullStatement(t *testing.T) {
	t.Parallel()
	recordJSON, err := json.Marshal(QueryExecutionRecord{ID: 1, FullStatement: "SELECT secret_value"})
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	if strings.Contains(string(recordJSON), "secret_value") || strings.Contains(string(recordJSON), "fullStatement") {
		t.Fatalf("history JSON leaked full statement: %s", recordJSON)
	}

	responseJSON, err := json.Marshal(QueryExecutionStatementResponse{Statement: "SELECT secret_value"})
	if err != nil {
		t.Fatalf("marshal statement response: %v", err)
	}
	if got, want := string(responseJSON), `{"statement":"SELECT secret_value"}`; got != want {
		t.Fatalf("statement response JSON = %s, want %s", got, want)
	}
}

func stringPointer(value string) *string { return &value }
