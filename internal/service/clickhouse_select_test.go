package service

import (
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func TestCHWorksheetTypedPredicateAndUserWindow(t *testing.T) {
	c := chCatalog{types: map[string]map[string]string{"events": {"label": "String", "id": "UInt64"}}, modes: map[string]map[string]model.ResultDisclosureMode{"events": {"label": model.ResultDisclosureRawCopyAllowed, "id": model.ResultDisclosureRawCopyAllowed}}}
	compile := func(stmt string, max int) *chCompiled {
		t.Helper()
		q, err := parseCHSelect(stmt)
		if err != nil {
			t.Fatal(err)
		}
		r, err := q.compile("app", c, max)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	a := compile("SELECT id FROM events WHERE label = 'O''Brien' LIMIT 10", 3)
	b := compile("SELECT id FROM events WHERE label = 'Jane' LIMIT 2", 3)
	if a.params["worksheet_value"] != "O'Brien" || strings.Contains(a.sql, "Brien") || a.digest != b.digest || a.limit != 3 || a.fetch != 4 || b.limit != 2 || b.fetch != 2 {
		t.Fatalf("incorrect binding, evidence shape or bounded limit")
	}
}

func TestCHWorksheetRejectsOutsideGrammar(t *testing.T) {
	for _, s := range []string{"", "SELECT * FROM events", "SELECT id FROM db.events", "SELECT id FROM events; SELECT id FROM events", "SELECT id FROM events WHERE id =", "SELECT id FROM events LIMIT -1", "SELECT id FROM events ORDER BY missing", "SELECT id, count(*) FROM events", "SELECT id FROM events GROUP BY id", "SELECT id,id FROM events", "SELECT id FROM events WHERE label = 'unterminated"} {
		if _, err := parseCHSelect(s); err == nil {
			t.Errorf("expected rejection for %q", s)
		}
	}
}
