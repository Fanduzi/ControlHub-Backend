// Package service provides tests for controlled CI ingestion parsing and preview reconciliation.
// input: internal/service ingestion APIs, internal/model, testing
// output: TestParseIngestion* and TestPreviewIngestion* functions
// pos: Validates strict parsing and pure exact-identity preview behavior
// note: if this file changes, update header and README.md
package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestParseIngestionCSVJSONEquivalent(t *testing.T) {
	j := `[{
		"environmentId": 1,
		"ciType": "host",
		"name": "web-1",
		"aliases": ["WEB"],
		"externalIdentifiers": [{"system": "AWS", "value": "i-1"}],
		"profile": {"hostname": "web-1"},
		"observedValues": {"ip": {"source": "agent", "value": "10.0.0.1"}},
		"relations": [{"type": "depends_on", "targetId": 2}]
	}]`
	c := "environmentId,ciType,name,displayName,aliases,externalIdentifiers,profile,observedValues,relations\n1,host,web-1,,\"[\"\"WEB\"\"]\",\"[{\"\"system\"\":\"\"AWS\"\",\"\"value\"\":\"\"i-1\"\"}]\",\"{\"\"hostname\"\":\"\"web-1\"\"}\",\"{\"\"ip\"\":{\"\"source\"\":\"\"agent\"\",\"\"value\"\":\"\"10.0.0.1\"\"}}\",\"[{\"\"type\"\":\"\"depends_on\"\",\"\"targetId\"\":2}]\"\n"
	jsonRows, err := ParseIngestion("json", []byte(j))
	if err != nil {
		t.Fatal(err)
	}
	csvRows, err := ParseIngestion("csv", []byte(c))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := mustJSON(t, csvRows), mustJSON(t, jsonRows); !bytes.Equal(got, want) {
		t.Fatalf("CSV != JSON\n%s\n%s", got, want)
	}
}

func TestParseIngestionRejectsUnsafeInput(t *testing.T) {
	cases := map[string]string{
		"unknown":       `[{"environmentId":1,"ciType":"host","name":"a","surprise":true}]`,
		"secret":        `[{"environmentId":1,"ciType":"host","name":"a","profile":{"password":"x"}}]`,
		"duplicate":     `[{"environmentId":1,"ciType":"host","name":"a"},{"environmentId":1,"ciType":"host","name":"a"}]`,
		"too many":      `[` + strings.Repeat(`{"environmentId":1,"ciType":"host","name":"x"},`, MaxIngestionRows) + `{"environmentId":2,"ciType":"host","name":"last"}]`,
		"row too large": `[{"environmentId":1,"ciType":"host","name":"a","profile":{"note":"` + strings.Repeat("x", MaxIngestionRowBytes) + `"}}]`,
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseIngestion("json", []byte(payload)); err == nil {
				t.Fatal("expected rejection")
			}
		})
	}
}

func TestPreviewIngestionExactPrecedenceAndNoFuzzy(t *testing.T) {
	current := []IngestionSnapshot{
		{ID: 1, EnvironmentID: 1, CIType: "host", Name: "exact-name", Aliases: []string{"old"}, ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: "aws", Value: "i-1"}}},
		{ID: 2, EnvironmentID: 1, CIType: "host", Name: "named", Aliases: []string{"alias"}},
	}
	rows := []IngestionRow{
		{EnvironmentID: 9, CIType: "service", Name: "different", ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: "aws", Value: "i-1"}}},
		{EnvironmentID: 1, CIType: "host", Name: "other", Aliases: []string{"alias"}},
		{EnvironmentID: 1, CIType: "host", Name: "exact-name"},
		{EnvironmentID: 1, CIType: "host", Name: "exact-nam"},
	}
	p := PreviewIngestion(rows, current)
	if got := []PreviewAction{p.Rows[0].Action, p.Rows[1].Action, p.Rows[2].Action, p.Rows[3].Action}; mustJSONText(t, got) != `["update","update","update","create"]` {
		t.Fatalf("actions: %v", got)
	}
	if p.Rows[0].MatchedID != 1 || p.Rows[1].MatchedID != 2 || p.Rows[2].MatchedID != 1 {
		t.Fatalf("wrong matches: %+v", p.Rows)
	}
}

func TestPreviewIngestionConflictsOnAmbiguityOrIdentityDisagreement(t *testing.T) {
	current := []IngestionSnapshot{
		{ID: 1, EnvironmentID: 1, CIType: "host", Name: "one", Aliases: []string{"shared", "a"}, ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: "aws", Value: "i-1"}}},
		{ID: 2, EnvironmentID: 1, CIType: "host", Name: "two", Aliases: []string{"shared", "b"}},
	}
	rows := []IngestionRow{
		{EnvironmentID: 1, CIType: "host", Name: "new", Aliases: []string{"shared"}},
		{EnvironmentID: 1, CIType: "host", Name: "two", Aliases: []string{"a"}, ExternalIdentifiers: []model.ResourceExternalIdentifier{{System: "aws", Value: "i-1"}}},
	}
	p := PreviewIngestion(rows, current)
	if p.Confirmable || p.Rows[0].Action != PreviewConflict || p.Rows[1].Action != PreviewConflict {
		t.Fatalf("expected non-confirmable conflicts: %+v", p)
	}
}

func TestPreviewIngestionDiffFingerprintAndPurity(t *testing.T) {
	rows := []IngestionRow{{EnvironmentID: 1, CIType: "host", Name: "one", DisplayName: "New", Profile: map[string]any{"hostname": "new"}, ObservedValues: map[string]ObservedValueInput{"ip": {Source: "agent", Value: "2"}}, Relations: []IngestionRelation{{Type: model.RelationTypeDependsOn, TargetID: 3}}}}
	current := []IngestionSnapshot{{ID: 1, EnvironmentID: 1, CIType: "host", Name: "one", DisplayName: "Old", Profile: map[string]any{"hostname": "old"}, ObservedValues: map[string]ObservedValueInput{"ip": {Source: "agent", Value: "1"}}, Relations: []IngestionRelation{{Type: model.RelationTypeDependsOn, TargetID: 2}}, ManualOverrides: map[string]any{"displayName": "Manual"}}}
	before := mustJSONText(t, rows)
	p1 := PreviewIngestion(rows, current)
	if mustJSONText(t, rows) != before {
		t.Fatal("preview mutated input")
	}
	d := p1.Rows[0].Diff
	if d.Fields["displayName"].Before != "Old" || len(d.Profile) != 1 || len(d.Observed) != 1 || len(d.Relations.Added) != 1 || len(d.Relations.Removed) != 1 {
		t.Fatalf("incomplete diff: %+v", d)
	}
	if strings.Contains(mustJSONText(t, p1), "Manual") {
		t.Fatal("manual override leaked")
	}
	current[0].DisplayName = "Drifted"
	p2 := PreviewIngestion(rows, current)
	if p1.Fingerprint == p2.Fingerprint {
		t.Fatal("inventory drift did not change fingerprint")
	}
	rows[0].DisplayName = "Input drift"
	p3 := PreviewIngestion(rows, current)
	if p2.Fingerprint == p3.Fingerprint {
		t.Fatal("input drift did not change fingerprint")
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func mustJSONText(t *testing.T, v any) string { return string(mustJSON(t, v)) }
