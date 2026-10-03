// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: synthetic SQL strings over stub schemas
// output: intent tests for CTE visibility classification, witnessable-position verdicts, canonical byte-offset qualification, and guard rejections
// pos: tests encode WHY (governance contract), not just parser behavior
// note: if this file changes, update header and README.md
package pgsql

import (
	"strings"
	"testing"
)

// refSummary renders refs compactly for table assertions: "kind:name@loc".
func refSummary(refs []RangeVarRef) []string {
	out := make([]string, 0, len(refs))
	for _, r := range refs {
		out = append(out, string(r.Kind)+":"+r.Name)
	}
	return out
}

func refsFor(t *testing.T, sql string) []RangeVarRef {
	t.Helper()
	gr, err := GuardPG(sql)
	if err != nil {
		t.Fatalf("GuardPG(%q) = %v", sql, err)
	}
	return gr.Refs
}

func TestCollectRangeVarRefs_CTEVisibility(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{
			name: "plain entity",
			sql:  "SELECT * FROM orders",
			want: []string{"e:orders"},
		},
		{
			name: "qualified entity",
			sql:  "SELECT * FROM app.orders",
			want: []string{"q:orders"},
		},
		{
			name: "cte ref in main body",
			sql:  "WITH c AS (SELECT 1) SELECT * FROM c",
			want: []string{"c:c"},
		},
		{
			name: "cte shadows nothing, entity inside cte body",
			sql:  "WITH c AS (SELECT * FROM orders) SELECT * FROM c",
			want: []string{"e:orders", "c:c"},
		},
		{
			name: "non-recursive preceding sibling visible",
			sql:  "WITH a AS (SELECT * FROM orders), b AS (SELECT * FROM a) SELECT * FROM b",
			want: []string{"e:orders", "c:a", "c:b"},
		},
		{
			name: "non-recursive forward ref falls back to entity",
			// a's body cannot see later sibling b; an unqualified `b`
			// inside a's body is an entity reference (binding will decide).
			sql:  "WITH a AS (SELECT * FROM b), b AS (SELECT 1) SELECT * FROM a",
			want: []string{"e:b", "c:a"},
		},
		{
			name: "non-recursive self ref falls back to entity",
			sql:  "WITH c AS (SELECT * FROM c) SELECT * FROM c",
			want: []string{"e:c", "c:c"},
		},
		{
			name: "recursive self ref is CTE",
			sql:  "WITH RECURSIVE c AS (SELECT 1 UNION ALL SELECT * FROM c) SELECT * FROM c",
			// inside c: 'c' is a CTE ref but lives in a set-op leaf → still 'c'
			// kind; witnessability rejected separately.
			want: []string{"c:c", "c:c"},
		},
		{
			name: "recursive mutual visibility",
			sql: "WITH RECURSIVE a AS (SELECT * FROM b), " +
				"b AS (SELECT * FROM a) SELECT * FROM a",
			want: []string{"c:b", "c:a", "c:a"},
		},
		{
			name: "inner WITH shadows outer name",
			// outer c visible inside subquery, but subquery defines own c.
			sql: "WITH c AS (SELECT 1) SELECT * FROM (WITH c AS (SELECT 2) " +
				"SELECT * FROM c) d JOIN c ON TRUE",
			want: []string{"c:c", "c:c"},
		},
		{
			name: "outer cte visible inside derived table",
			sql:  "WITH c AS (SELECT 1) SELECT * FROM (SELECT * FROM c) d",
			want: []string{"c:c"},
		},
		{
			name: "qualified name never resolves to CTE",
			sql:  "WITH app AS (SELECT 1) SELECT * FROM x.app",
			want: []string{"q:app"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := refSummary(refsFor(t, tc.sql))
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("refs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCollectRangeVarRefs_Witnessable(t *testing.T) {
	cases := []struct {
		name    string
		sql     string
		wantErr bool
	}{
		{"top-level FROM entity", "SELECT * FROM orders", false},
		{"entity inside CTE body", "WITH c AS (SELECT * FROM orders) SELECT * FROM c", false},
		{"entity inside FROM derived", "SELECT * FROM (SELECT * FROM orders) d", false},
		{"entity inside JOIN legs", "SELECT * FROM a JOIN b ON a.id = b.id", false},
		{"entity in WHERE subquery", "SELECT * FROM orders WHERE id IN (SELECT id FROM other)", true},
		{"entity in EXISTS", "SELECT * FROM orders WHERE EXISTS (SELECT 1 FROM other)", true},
		{"entity in scalar subquery", "SELECT (SELECT x FROM other) FROM orders", true},
		{"entity in JOIN ON subquery", "SELECT * FROM orders JOIN a ON EXISTS (SELECT 1 FROM other)", true},
		{"entity in HAVING subquery", "SELECT count(*) FROM orders GROUP BY id HAVING EXISTS (SELECT 1 FROM other)", true},
		{"entity in UNION leaf", "SELECT id FROM orders UNION SELECT id FROM other", true},
		{"entity in UNION inside derived", "SELECT * FROM (SELECT id FROM orders UNION SELECT id FROM other) d", true},
		{"UNION of pure literals", "SELECT 1 UNION SELECT 2", false},
		{"entity in lateral derived", "SELECT * FROM orders, LATERAL (SELECT * FROM items i WHERE i.o = orders.id) d", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GuardPG(tc.sql)
			if tc.wantErr {
				re, ok := err.(*RejectError)
				if !ok || re.Code != "query_reference_not_witnessable" {
					t.Fatalf("GuardPG(%q) err = %v, want query_reference_not_witnessable", tc.sql, err)
				}
			} else if err != nil {
				t.Fatalf("GuardPG(%q) unexpected err = %v", tc.sql, err)
			}
		})
	}
}

func TestQualifyEntities(t *testing.T) {
	cases := []struct {
		name   string
		sql    string
		pinned string
		want   string
	}{
		{
			name:   "single entity",
			sql:    "SELECT * FROM orders",
			pinned: "app",
			want:   `SELECT * FROM "app".orders`,
		},
		{
			name:   "preserves odd whitespace and quoting",
			sql:    "SELECT  *\nFROM   orders  o  JOIN items ON o.id = items.oid",
			pinned: "app",
			want:   "SELECT  *\nFROM   \"app\".orders  o  JOIN \"app\".items ON o.id = items.oid",
		},
		{
			name:   "CTE refs untouched",
			sql:    "WITH c AS (SELECT * FROM orders) SELECT * FROM c JOIN real_t ON c.x = real_t.x",
			pinned: "app",
			want:   `WITH c AS (SELECT * FROM "app".orders) SELECT * FROM c JOIN "app".real_t ON c.x = real_t.x`,
		},
		{
			name:   "qualified refs untouched",
			sql:    "SELECT * FROM other.orders JOIN items ON true",
			pinned: "app",
			want:   `SELECT * FROM other.orders JOIN "app".items ON true`,
		},
		{
			name:   "ONLY keyword before name",
			sql:    "SELECT * FROM ONLY orders",
			pinned: "app",
			want:   `SELECT * FROM ONLY "app".orders`,
		},
		{
			name:   "quoted relname",
			sql:    `SELECT * FROM "Weird Name"`,
			pinned: "app",
			want:   `SELECT * FROM "app"."Weird Name"`,
		},
		{
			name:   "pinned schema with quote",
			sql:    "SELECT * FROM orders",
			pinned: `a"b`,
			want:   `SELECT * FROM "a""b".orders`,
		},
		{
			name:   "entity inside subquery also qualified",
			sql:    "SELECT * FROM (SELECT * FROM deep) d JOIN shallow ON true",
			pinned: "app",
			want:   `SELECT * FROM (SELECT * FROM "app".deep) d JOIN "app".shallow ON true`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gr, err := GuardPG(tc.sql)
			if err != nil {
				t.Fatalf("GuardPG(%q) = %v", tc.sql, err)
			}
			got, err := QualifyEntities(tc.sql, tc.pinned, gr.Refs)
			if err != nil {
				t.Fatalf("QualifyEntities = %v", err)
			}
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
		})
	}
}

func TestGuardRejections(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"insert", "INSERT INTO t VALUES (1)"},
		{"update", "UPDATE t SET a = 1"},
		{"delete", "DELETE FROM t"},
		{"ddl", "CREATE TABLE x (a int)"},
		{"tx", "BEGIN"},
		{"multi", "SELECT 1; SELECT 2"},
		{"select into", "SELECT * INTO n FROM t"},
		{"for update", "SELECT * FROM t FOR UPDATE"},
		{"for share nested", "SELECT * FROM (SELECT * FROM t FOR SHARE) d"},
		{"data-modifying cte", "WITH d AS (DELETE FROM t RETURNING *) SELECT * FROM d"},
		{"pg_sleep", "SELECT pg_sleep(1)"},
		{"set_config", "SELECT set_config('x','y',false)"},
		{"nextval", "SELECT nextval('s')"},
		{"schema-qualified forbidden", "SELECT pg_catalog.pg_sleep(1)"},
		{"dblink", "SELECT * FROM dblink('c','SELECT 1') x"},
		{"txid_current", "SELECT txid_current()"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := GuardPG(tc.sql)
			re, ok := err.(*RejectError)
			if !ok || re.Code != "query_not_allowed" {
				t.Fatalf("GuardPG(%q) err = %v, want query_not_allowed", tc.sql, err)
			}
		})
	}
}

func TestGuard_ExprHiddenSubqueriesUnwitnessable(t *testing.T) {
	// Subqueries nested inside expression positions can never propagate a
	// witness — every parser-legal hiding spot must classify the entity ref
	// as non-witnessable (spec: JOIN ON / HAVING / expr args / VALUES rows /
	// GROUP BY all follow the same rule).
	cases := []string{
		`VALUES ((SELECT id FROM orders))`,
		`SELECT * FROM (VALUES ((SELECT id FROM orders))) v`,
		`SELECT a FROM t GROUP BY (SELECT a FROM orders)`,
		`SELECT (SELECT id FROM orders LIMIT 1) FROM t`,
		`SELECT * FROM t WHERE id IN (SELECT id FROM orders)`,
		`SELECT * FROM t WHERE EXISTS (SELECT 1 FROM orders)`,
		`SELECT * FROM t JOIN items ON t.a > (SELECT id FROM orders)`,
		`SELECT * FROM t GROUP BY a HAVING count(*) > (SELECT id FROM orders)`,
		`SELECT * FROM t TABLESAMPLE BERNOULLI ((SELECT id FROM orders))`,
	}
	for _, sql := range cases {
		_, err := GuardPG(sql)
		re, ok := err.(*RejectError)
		if !ok || re.Code != "query_reference_not_witnessable" {
			t.Fatalf("GuardPG(%q) err = %v, want query_reference_not_witnessable", sql, err)
		}
	}
}
