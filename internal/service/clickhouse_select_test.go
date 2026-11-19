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

func TestCHWorksheetDialectBindings(t *testing.T) {
	c := chCatalog{types: map[string]map[string]string{"events": {"id": "UInt64", "label": "String"}}, modes: map[string]map[string]model.ResultDisclosureMode{"events": {"id": model.ResultDisclosureRawCopyAllowed, "label": model.ResultDisclosureRawCopyAllowed}}}
	compile := func(sql string) (*chCompiled, error) {
		q, err := parseCHSelect(sql)
		if err != nil {
			return nil, err
		}
		return q.compile("app", c, 10)
	}
	a, err := compile("SELECT id FROM events WHERE (id >= 42 AND id < 84) OR label = 'O''Brien'")
	if err != nil {
		t.Fatal(err)
	}
	b, err := compile("SELECT id FROM events WHERE (id >= 100 AND id < 120) OR label = 'hello'")
	if err != nil {
		t.Fatal(err)
	}
	if a.digest != b.digest || len(a.params) != 3 || a.params["worksheet_value"] != "42" || a.params["worksheet_value2"] != "84" || a.params["worksheet_value3"] != "O'Brien" || strings.Contains(a.sql, "Brien") {
		t.Fatal("each predicate needs its own typed parameter; evidence must ignore literals")
	}
	for _, sql := range []string{"SELECT DISTINCT id FROM events", "SELECT id FROM events FINAL", "SELECT id FROM events WHERE id IN (1,2)", "SELECT id FROM events WHERE id < label", "SELECT id FROM events HAVING id > 5", "SELECT id FROM events JOIN other ON events.id = other.id", "SELECT id FROM events LIMIT 2 OFFSET 1", "SELECT id FROM events UNION ALL SELECT id FROM events", "SELECT id FROM events FORMAT CSV", "SELECT id FROM events WHERE " + strings.Repeat("id=1 OR ", 32) + "id=2"} {
		if _, err := compile(sql); err == nil {
			t.Errorf("unexpectedly accepted %q", sql)
		}
	}
}

func TestCHWorksheetRejectsOutsideGrammar(t *testing.T) {
	for _, s := range []string{"", "SELECT * FROM events", "SELECT id FROM db.events", "SELECT id FROM events; SELECT id FROM events", "SELECT id FROM events WHERE id =", "SELECT id FROM events LIMIT -1", "SELECT id FROM events ORDER BY missing", "SELECT id, count(*) FROM events", "SELECT id FROM events GROUP BY id", "SELECT id,id FROM events", "SELECT id FROM events WHERE label = 'unterminated"} {
		if _, err := parseCHSelect(s); err == nil {
			t.Errorf("expected rejection for %q", s)
		}
	}
}
