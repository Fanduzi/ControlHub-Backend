package service

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCHExplainOptionalGranules(t *testing.T) {
	for _, tc := range []struct {
		indexes string
		want    bool
	}{
		{`[]`, false}, {`[{"Type":"MinMax","Initial Granules":9,"Selected Granules":2}]`, false},
		{`[{"Type":"PrimaryKey","Initial Granules":0,"Selected Granules":0}]`, true},
		{`[{"Type":"PrimaryKey","Initial Granules":20}]`, false},
		{`[{"Type":"PrimaryKey","Initial Granules":1,"Selected Granules":2}]`, false},
		{`[{"Type":"PrimaryKey","Initial Granules":9007199254740992,"Selected Granules":1}]`, false},
		{`[{"Type":"PrimaryKey","Initial Granules":10,"Selected Granules":1.5}]`, false},
		{`[{"Type":"PrimaryKey","Initial Granules":10,"Selected Granules":2},{"Type":"PrimaryKey","Initial Granules":10,"Selected Granules":1}]`, false},
	} {
		raw := `[{"Plan":{"Node Type":"ReadFromMergeTree","Description":"private-field","Indexes":` + tc.indexes + `}}]`
		plan, err := normalizeCHExplain(raw)
		if err != nil || len(plan.Nodes) != 1 || (plan.Nodes[0].PrimaryKeyGranules != nil) != tc.want {
			t.Fatalf("unexpected evidence for %s: %+v %v", tc.indexes, plan, err)
		}
		published, _ := json.Marshal(plan)
		if strings.Contains(string(published), "private-field") {
			t.Fatal("raw description leaked")
		}
	}
}

func TestCHExplainBoundsAndUnknown(t *testing.T) {
	for _, raw := range []string{`{}`, `[]`, `[{"Plan":{}}]`, strings.Repeat("x", MaxExplainPlanBytes+1)} {
		if _, err := normalizeCHExplain(raw); !errors.Is(err, ErrQueryExplainNotSupported) {
			t.Fatal("expected controlled plan refusal")
		}
	}
	nodes := strings.Repeat(`{"Node Type":"unrecognized-private-text"},`, MaxExplainNodes) + `{"Node Type":"Filter"}`
	plan, err := normalizeCHExplain(`[{"Plan":{"Node Type":"Expression","Plans":[` + nodes + `]}}]`)
	if err != nil || !plan.Truncated || len(plan.Nodes) != MaxExplainNodes || len(plan.Risks) != 1 {
		t.Fatalf("cap/unknown mismatch: %v", err)
	}
	wire, _ := json.Marshal(plan)
	if strings.Contains(string(wire), "unrecognized-private-text") {
		t.Fatal("unknown engine text leaked")
	}
}
