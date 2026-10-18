// Package pgsql — unit tests for SafeStatementPreview.
// input: SQL text with literals, dollar-quoted bodies, comments, numbers
// output: proofs that no literal body or comment text survives the preview
// pos: T10-C-R1 preview masking coverage (issue #118)
// note: if this file changes, update header and README.md
package pgsql

import (
	"strings"
	"testing"
)

// The masked preview keeps statement shape but never leaks a literal body or
// comment text — the markers below are chosen to be unambiguously secret.
func TestSafeStatementPreview_MasksEveryLiteralKind(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		stmt   string
		forbid []string
		want   string
	}{
		"plain string": {
			stmt:   `SELECT * FROM t WHERE a = 'SECRET-PL1'`,
			forbid: []string{"SECRET-PL1"},
			want:   `SELECT * FROM t WHERE a = <literal>`,
		},
		"string with doubled quote": {
			stmt:   `SELECT 'it''s SECRET-PL2' FROM t`,
			forbid: []string{"SECRET-PL2"},
			want:   `SELECT <literal> FROM t`,
		},
		"E string with escaped quote": {
			stmt:   `SELECT E'ab\'c SECRET-PL3' FROM t`,
			forbid: []string{"SECRET-PL3"},
			want:   `SELECT <literal> FROM t`,
		},
		"E string continuation over newline": {
			stmt:   "SELECT E'one'\n  'two SECRET-PL4' FROM t",
			forbid: []string{"SECRET-PL4"},
			want:   `SELECT <literal> FROM t`,
		},
		"bit and hex strings": {
			stmt:   `SELECT B'1010', X'DEADCAFE'`,
			forbid: []string{"1010", "DEADCAFE"},
			want:   `SELECT <literal>, <literal>`,
		},
		"unicode string": {
			stmt:   `SELECT U&'d\0061t SECRET-PL5'`,
			forbid: []string{"SECRET-PL5", "0061"},
			want:   `SELECT <literal>`,
		},
		"dollar body untagged": {
			stmt:   `SELECT $$body SECRET-PL6 with 'quotes'$$`,
			forbid: []string{"SECRET-PL6"},
			want:   `SELECT <literal>`,
		},
		"dollar body tagged": {
			stmt:   `SELECT $fn$SECRET-PL7 'inside'$fn$ FROM t`,
			forbid: []string{"SECRET-PL7"},
			want:   `SELECT <literal> FROM t`,
		},
		"line comment": {
			stmt: `SELECT id -- SECRET-PL8 hides here
FROM t`,
			forbid: []string{"SECRET-PL8"},
			want:   "SELECT id  \nFROM t",
		},
		"block comment": {
			stmt:   `SELECT /* SECRET-PL9 */ id FROM t`,
			forbid: []string{"SECRET-PL9"},
			want:   `SELECT   id FROM t`,
		},
		"nested block comment": {
			stmt:   `SELECT /* outer /* SECRET-PL10 */ still */ id`,
			forbid: []string{"SECRET-PL10", "still"},
			want:   `SELECT   id`,
		},
		"numbers": {
			stmt:   `SELECT 42, 1.5, 0xFF, 1e-3 FROM t`,
			forbid: []string{"42", "1.5", "0xFF", "1e-3"},
			want:   `SELECT <number>, <number>, <number>, <number>-<number> FROM t`,
		},
		"quoted identifier kept": {
			stmt:   `SELECT "Weird Col" FROM "some table"`,
			forbid: nil,
			want:   `SELECT "Weird Col" FROM "some table"`,
		},
		"ident containing e": {
			stmt:   `SELECT some, exec FROM t`,
			forbid: nil,
			want:   `SELECT some, exec FROM t`,
		},
		"ident ending in u-amp": {
			stmt:   `SELECT my_u&'x' FROM t`,
			forbid: []string{"'x'"},
			want:   `SELECT my_u&<literal> FROM t`,
		},
		"native params kept": {
			stmt:   `SELECT a FROM t WHERE a = $1 AND b = $12`,
			forbid: nil,
			want:   `SELECT a FROM t WHERE a = $1 AND b = $12`,
		},
		"cast kept": {
			stmt:   `SELECT 1::int, 'x'::text`,
			forbid: []string{"'x'"},
			want:   `SELECT <number>::int, <literal>::text`,
		},
		"unterminated string masks rest": {
			stmt:   `SELECT a FROM t WHERE a = 'unterminated SECRET-PL11`,
			forbid: []string{"SECRET-PL11"},
			want:   `SELECT a FROM t WHERE a = <literal>`,
		},
		"unterminated dollar body masks rest": {
			stmt:   `SELECT a FROM t WHERE a = $x$SECRET-PL12`,
			forbid: []string{"SECRET-PL12"},
			want:   `SELECT a FROM t WHERE a = <literal>`,
		},
		"unterminated block comment masks rest": {
			stmt:   `SELECT a FROM t /* SECRET-PL13`,
			forbid: []string{"SECRET-PL13"},
			want:   `SELECT a FROM t  `,
		},
		"unterminated quoted identifier masks rest": {
			// The scan cannot prove where the name ends, so the tail is
			// content — a verbatim tail would leak it into history.
			stmt:   `SELECT a FROM t WHERE "SECRET-PL16`,
			forbid: []string{"SECRET-PL16"},
			want:   `SELECT a FROM t WHERE <literal>`,
		},
		"comment-looking text inside string": {
			stmt:   `SELECT 'a /* not comment SECRET-PL14 */ b'`,
			forbid: []string{"SECRET-PL14"},
			want:   `SELECT <literal>`,
		},
		"quote-looking text inside comment": {
			stmt: `-- SECRET-PL15 'quoted' here
SELECT 1`,
			forbid: []string{"SECRET-PL15", "quoted"},
			want:   " \nSELECT <number>",
		},
	}
	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := SafeStatementPreview(tc.stmt, 512)
			if got != tc.want {
				t.Fatalf("preview = %q, want %q", got, tc.want)
			}
			for _, marker := range tc.forbid {
				if strings.Contains(got, marker) {
					t.Fatalf("preview %q leaks %q", got, marker)
				}
			}
		})
	}
}

// The rune cap cuts at a rune boundary and never returns the original text.
func TestSafeStatementPreview_RuneBound(t *testing.T) {
	t.Parallel()
	got := SafeStatementPreview(`SELECT 'SECRET' FROM `+strings.Repeat("é", 600), 20)
	if len([]rune(got)) > 20 {
		t.Fatalf("preview exceeds rune cap: %d runes", len([]rune(got)))
	}
	if strings.Contains(got, "SECRET") {
		t.Fatalf("preview leaked literal: %q", got)
	}
	// A zero cap disables truncation.
	full := SafeStatementPreview(`SELECT 1`, 0)
	if full != `SELECT <number>` {
		t.Fatalf("uncapped preview = %q", full)
	}
}
