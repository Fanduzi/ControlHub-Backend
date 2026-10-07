// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: synthetic template SQL strings
// output: TestScanTemplatePlaceholders* — G8 lexical matrix: recognized markers, preserved constructs, controlled rejections
// pos: G8 scan contract (T11) — placeholder recognition is lexical, never a global replace; casts, literals, comments, dollar quotes, and array slices pass through verbatim while native $n input fails loud
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"

	pgquery "github.com/wasilibs/go-pgquery"
)

func markerNames(ps []TemplatePlaceholder) []string {
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.Name
	}
	return names
}

func TestScanTemplatePlaceholdersRecognized(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		sql   string
		names []string
	}{
		{"simple", "SELECT * FROM orders WHERE status = :status", []string{"status"}},
		{"two markers in source order", "SELECT :a, :b FROM t WHERE c = :c", []string{"a", "b", "c"}},
		{"same name twice reported twice", "SELECT :a + :a", []string{"a", "a"}},
		{"underscore and digits", "SELECT :a_1 FROM t", []string{"a_1"}},
		{"marker then cast", "SELECT :v::text FROM t", []string{"v"}},
		{"cast before marker", "SELECT x::int, :v FROM t", []string{"v"}},
		{"chinese adjacent", "SELECT :名称为x FROM t WHERE y = :v", []string{"名称为x", "v"}},
		{"emoji adjacent", "SELECT '🚀' AS tag, :v FROM t", []string{"v"}},
		{"marker at statement end", "SELECT * FROM t WHERE id = :id", []string{"id"}},
		{"marker at start of predicate", "SELECT * FROM t WHERE :flag AND id = 1", []string{"flag"}},
		{"uppercase name collected for declaration check", "SELECT :Name FROM t", []string{"Name"}},
		{"tab and newline separators", "SELECT *\nFROM t\tWHERE a = :a", []string{"a"}},
		{"carriage return separates markers", "SELECT :a\r, :b", []string{"a", "b"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ScanTemplatePlaceholders(tc.sql)
			if err != nil {
				t.Fatalf("scan %q: %v", tc.sql, err)
			}
			if !reflect.DeepEqual(markerNames(got), tc.names) {
				t.Fatalf("scan %q names = %v, want %v", tc.sql, markerNames(got), tc.names)
			}
		})
	}
}

func TestScanTemplatePlaceholdersPreservesConstructs(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		sql  string
	}{
		{"plain string colon", "SELECT ':not_a_param' AS lit, ':a:b' FROM t WHERE x = :ok"},
		{"doubled quote inside string", "SELECT 'it''s :x' FROM t WHERE y = :ok"},
		{"E string backslash escape", `SELECT E'esc\':x\'' FROM t WHERE y = :ok`},
		{"U& string", `SELECT U&'\0061 :x' FROM t WHERE y = :ok`},
		{"U& dollar quote style escape", `SELECT U&'!0041' UESCAPE '!' FROM t WHERE y = :ok`},
		{"U& backslash is plain content under custom UESCAPE", `SELECT U&'abc\' UESCAPE '!', :ok`},
		{"newline string continuation plain", "SELECT 'a'\n':not', :ok"},
		{"E string continuation keeps escape state", "SELECT E'first'\n'\\' :fake', :ok"},
		{"continuation over comment holding newline", "SELECT 'a' /* c\n */ ':not', :ok"},
		{"same-line adjacent quote is a new literal", "SELECT 'a' ':not', :ok"},
		{"double-quoted identifier", `SELECT "has:colon" FROM t WHERE y = :ok`},
		{"U& quoted identifier", `SELECT * FROM "sch".U&"ta:ble" WHERE y = :ok`},
		{"line comment", "SELECT 1 -- :nope\nFROM t WHERE x = :ok"},
		{"line comment at EOF", "SELECT :ok -- trailing :nope"},
		{"line comment ended by carriage return", "SELECT 1 -- ignored\r + :ok"},
		{"line comment ended by CRLF", "SELECT 1 -- ignored\r\n + :ok"},
		{"carriage return is code whitespace", "SELECT 1\r-- :nope\nFROM t WHERE x = :ok"},
		{"block comment", "SELECT /* :nope */ :ok"},
		{"nested block comment", "SELECT /* outer /* inner :deep */ still :nope */ :ok"},
		{"block comment around string", "SELECT /* ':n' /* :m */ */ :ok"},
		{"dollar quote", "SELECT $$body :nope $$, :ok"},
		{"tagged dollar quote", "SELECT $fn$BEGIN :nope END$fn$, :ok"},
		{"dollar quote holding quotes", "SELECT $tag$'a'':b'$tag$, :ok"},
		{"empty dollar tag", "SELECT $$$$ || :ok"},
		{"array slice", "SELECT arr[1:2] FROM t WHERE x = :ok"},
		{"array subscript both bounds", "SELECT arr[:lo + 1] FROM t WHERE x = :ok"},
		{"cast operator preserved", "SELECT a::text, b::int[] FROM t WHERE x = :ok"},
		{"cast inside expression", "SELECT (v::numeric) * :ok FROM t"},
		{"colon in bracket only", "SELECT arr[2:5], :ok FROM t"},
		{"dollar sign in identifier", "SELECT a$b$c FROM t WHERE x = :ok"},
		{"lone dollar", "SELECT a $ b, :ok FROM t"},
		{"percent-like colon", "SELECT 'a%' || :ok FROM t"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ScanTemplatePlaceholders(tc.sql)
			if err != nil {
				t.Fatalf("scan %q: %v", tc.sql, err)
			}
			if want := []string{"ok"}; !reflect.DeepEqual(markerNames(got), want) {
				t.Fatalf("scan %q names = %v, want %v", tc.sql, markerNames(got), want)
			}
		})
	}
}

func TestScanTemplatePlaceholdersNoMarkers(t *testing.T) {
	t.Parallel()
	for _, sql := range []string{
		"SELECT 1",
		"",
		"SELECT '::' , x::int, arr[1:3], $$:a$$, -- :b\n U&':c', \"d:e\" FROM t",
		"SELECT /* /* :a */ :b */ 1",
	} {
		got, err := ScanTemplatePlaceholders(sql)
		if err != nil {
			t.Fatalf("scan %q: %v", sql, err)
		}
		if len(got) != 0 {
			t.Fatalf("scan %q found %v, want none", sql, got)
		}
	}
}

func TestScanTemplatePlaceholdersRejectsNativeParams(t *testing.T) {
	t.Parallel()
	for _, sql := range []string{
		"SELECT $1",
		"SELECT * FROM t WHERE a = $2 AND b = :ok",
		"SELECT arr[$1]",
		"SELECT $12 FROM t",
		"SELECT $0",
		"SELECT :x -- ignored\r + $1",
	} {
		got, err := ScanTemplatePlaceholders(sql)
		if !errors.Is(err, ErrTemplateNativeParameter) {
			t.Fatalf("scan %q error = %v, want ErrTemplateNativeParameter", sql, err)
		}
		if got != nil {
			t.Fatalf("scan %q returned markers alongside error: %v", sql, got)
		}
	}
	// $n inside lexical constructs is content, not a parameter.
	for _, sql := range []string{
		"SELECT '$1', \"$2\", $$ $3 $$, /* $4 */ :ok",
		"SELECT E'\\'$5' || :ok",
	} {
		got, err := ScanTemplatePlaceholders(sql)
		if err != nil {
			t.Fatalf("scan %q: %v", sql, err)
		}
		if want := []string{"ok"}; !reflect.DeepEqual(markerNames(got), want) {
			t.Fatalf("scan %q names = %v, want %v", sql, markerNames(got), want)
		}
	}
}

func TestScanTemplatePlaceholdersRejectsUnterminated(t *testing.T) {
	t.Parallel()
	for _, sql := range []string{
		"SELECT ':open",
		`SELECT E':open`,
		`SELECT U&':open`,
		`SELECT ":open`,
		"SELECT /* :open",
		"SELECT /* nest /* :open */ ",
		"SELECT $$ :open",
		"SELECT $tag$ :open",
	} {
		if _, err := ScanTemplatePlaceholders(sql); !errors.Is(err, ErrTemplateScanInvalid) {
			t.Fatalf("scan %q error = %v, want ErrTemplateScanInvalid", sql, err)
		}
	}
}

// TestScanTemplatePlaceholdersOffsets checks that recorded spans index the
// original bytes exactly — the compiler splices $k markers at these offsets.
func TestScanTemplatePlaceholdersOffsets(t *testing.T) {
	t.Parallel()
	sql := "SELECT ':x' AS lit, /* :y */ a::int, arr[1:9], :first || :second FROM t WHERE z = :third"
	got, err := ScanTemplatePlaceholders(sql)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	want := []string{"first", "second", "third"}
	if !reflect.DeepEqual(markerNames(got), want) {
		t.Fatalf("names = %v, want %v", markerNames(got), want)
	}
	for i, m := range got {
		if sql[m.Offset:m.Offset+m.Length] != ":"+m.Name {
			t.Fatalf("marker %d span = %q, want :%s", i, sql[m.Offset:m.Offset+m.Length], m.Name)
		}
	}
	// A splice using the recorded spans must leave every preserved construct
	// byte-identical and produce valid $k positions.
	var sb strings.Builder
	pos := 0
	for i, m := range got {
		sb.WriteString(sql[pos:m.Offset])
		sb.WriteString("$")
		sb.WriteString(strconv.Itoa(i + 1))
		pos = m.Offset + m.Length
	}
	sb.WriteString(sql[pos:])
	if _, err := pgquery.Parse(sb.String()); err != nil {
		t.Fatalf("spliced statement does not parse: %v\n%s", err, sb.String())
	}
}
