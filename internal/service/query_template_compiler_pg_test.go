// Package service provides tests for the PostgreSQL template compiler dialect.
// input: errors, reflect, strings, testing, internal/pgsql
// output: TestTemplateStatementCompilerPG* — $k binding, lexical boundary, and real-guard/pagination rejection proofs
// pos: G8 compile-path evidence (T11) — compiled statements keep generated $k markers and bound args, never interpolate values, and still owe GuardPG and the G6 pagination gate; compiler-level proofs, not governed execution-chain proof
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/pgsql"
)

func TestTemplateStatementCompilerPG_BindsValuesAsPositionalMarkers(t *testing.T) {
	t.Parallel()

	compiler := NewTemplateStatementCompiler()
	compiled, err := compiler.CompilePG(TemplateStatementInput{
		Statement: `SELECT id FROM orders WHERE status = :status AND id > :minimum_id`,
		Definitions: []TemplateParameterDefinition{
			{Name: "status", Type: TemplateParameterString},
			{Name: "minimum_id", Type: TemplateParameterInteger},
		},
		Values: map[string]any{"status": "paid", "minimum_id": int64(42)},
	})
	if err != nil {
		t.Fatalf("CompilePG error: %v", err)
	}
	if got, want := compiled.Statement, `SELECT id FROM orders WHERE status = $1 AND id > $2`; got != want {
		t.Fatalf("compiled statement = %q, want %q", got, want)
	}
	if want := []any{"paid", int64(42)}; !reflect.DeepEqual(compiled.Args, want) {
		t.Fatalf("compiled args = %#v, want %#v", compiled.Args, want)
	}
	if strings.Contains(compiled.Statement, "paid") || strings.Contains(compiled.Statement, "42") {
		t.Fatalf("compiled statement leaks a bound value: %q", compiled.Statement)
	}
}

func TestTemplateStatementCompilerPG_LexicalConstructsAreNotMarkers(t *testing.T) {
	t.Parallel()

	// One statement that exercises every lexical boundary at once: a :: cast,
	// quoted identifier, plain string, E'' escape string, U&'' unicode string,
	// dollar-quoted body, nested block comment, line comment, and an array
	// slice — none of their ':' bytes may become placeholders, and the text
	// must survive verbatim.
	statement := `SELECT "weird:col", 'lit:x', E'esc\n:y', U&'uni\+0061:z', ` +
		`$body$inside :w$body$, arr[1:2], total::numeric ` +
		`/* outer /* inner :c */ :d */ -- tail :e
FROM t WHERE a = :alpha AND b = :beta`

	compiler := NewTemplateStatementCompiler()
	compiled, err := compiler.CompilePG(TemplateStatementInput{
		Statement: statement,
		Definitions: []TemplateParameterDefinition{
			{Name: "alpha", Type: TemplateParameterString},
			{Name: "beta", Type: TemplateParameterInteger},
		},
		Values: map[string]any{"alpha": "x", "beta": int64(7)},
	})
	if err != nil {
		t.Fatalf("CompilePG error: %v", err)
	}
	if strings.Count(compiled.Statement, "$1") != 1 || strings.Count(compiled.Statement, "$2") != 1 {
		t.Fatalf("compiled statement = %q, want exactly one $1 and one $2", compiled.Statement)
	}
	for _, keep := range []string{`"weird:col"`, `'lit:x'`, `:y`, `:z`, `:w`, `:c`, `:d`, `:e`, `arr[1:2]`, `total::numeric`} {
		if !strings.Contains(compiled.Statement, keep) {
			t.Fatalf("compiled statement lost %q: %q", keep, compiled.Statement)
		}
	}
	if strings.Contains(compiled.Statement, ":alpha") || strings.Contains(compiled.Statement, ":beta") {
		t.Fatalf("declared markers survived compilation: %q", compiled.Statement)
	}
}

func TestTemplateStatementCompilerPG_RejectsNativePositionalParameters(t *testing.T) {
	t.Parallel()

	for _, statement := range []string{
		`SELECT * FROM t WHERE id = $1`,
		`SELECT * FROM t WHERE id = $42`,
	} {
		_, err := NewTemplateStatementCompiler().CompilePG(TemplateStatementInput{Statement: statement})
		if !errors.Is(err, ErrTemplateParameterInvalid) {
			t.Fatalf("CompilePG(%q) error = %v, want native-parameter rejection", statement, err)
		}
	}
}

func TestTemplateStatementCompilerPG_DeclarationBoundaries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		statement   string
		definitions []TemplateParameterDefinition
		values      map[string]any
	}{
		{
			name:      "missing value",
			statement: `SELECT * FROM t WHERE a = :alpha`,
			definitions: []TemplateParameterDefinition{
				{Name: "alpha", Type: TemplateParameterString},
			},
			values: map[string]any{},
		},
		{
			name:      "extra value",
			statement: `SELECT * FROM t WHERE a = :alpha`,
			definitions: []TemplateParameterDefinition{
				{Name: "alpha", Type: TemplateParameterString},
			},
			values: map[string]any{"alpha": "x", "ghost": "y"},
		},
		{
			name:      "duplicate declaration",
			statement: `SELECT * FROM t WHERE a = :alpha`,
			definitions: []TemplateParameterDefinition{
				{Name: "alpha", Type: TemplateParameterString},
				{Name: "alpha", Type: TemplateParameterInteger},
			},
			values: map[string]any{"alpha": "x"},
		},
		{
			name:      "invalid declaration name",
			statement: `SELECT * FROM t WHERE a = :alpha`,
			definitions: []TemplateParameterDefinition{
				{Name: "Alpha", Type: TemplateParameterString},
			},
			values: map[string]any{"alpha": "x"},
		},
		{
			name:      "undeclared placeholder",
			statement: `SELECT * FROM t WHERE a = :alpha AND b = :ghost`,
			definitions: []TemplateParameterDefinition{
				{Name: "alpha", Type: TemplateParameterString},
			},
			values: map[string]any{"alpha": "x"},
		},
		{
			name:      "unused declaration",
			statement: `SELECT * FROM t WHERE a = :alpha`,
			definitions: []TemplateParameterDefinition{
				{Name: "alpha", Type: TemplateParameterString},
				{Name: "beta", Type: TemplateParameterInteger},
			},
			values: map[string]any{"alpha": "x", "beta": int64(1)},
		},
		{
			name:      "repeated placeholder",
			statement: `SELECT * FROM t WHERE a = :alpha OR b = :alpha`,
			definitions: []TemplateParameterDefinition{
				{Name: "alpha", Type: TemplateParameterString},
			},
			values: map[string]any{"alpha": "x"},
		},
		{
			name:      "unsupported type",
			statement: `SELECT * FROM t WHERE a = :alpha`,
			definitions: []TemplateParameterDefinition{
				{Name: "alpha", Type: TemplateParameterType("uuid")},
			},
			values: map[string]any{"alpha": "x"},
		},
		{
			name:      "wrong value type",
			statement: `SELECT * FROM t WHERE a = :alpha`,
			definitions: []TemplateParameterDefinition{
				{Name: "alpha", Type: TemplateParameterInteger},
			},
			values: map[string]any{"alpha": "not-an-int"},
		},
	}

	compiler := NewTemplateStatementCompiler()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := compiler.CompilePG(TemplateStatementInput{
				Statement:   tc.statement,
				Definitions: tc.definitions,
				Values:      tc.values,
			})
			if !errors.Is(err, ErrTemplateParameterInvalid) {
				t.Fatalf("CompilePG error = %v, want ErrTemplateParameterInvalid", err)
			}
		})
	}
}

func TestTemplateStatementCompilerPG_PreservesExactValues(t *testing.T) {
	t.Parallel()

	compiled, err := NewTemplateStatementCompiler().CompilePG(TemplateStatementInput{
		Statement: `SELECT * FROM t WHERE big = :big AND price = :price`,
		Definitions: []TemplateParameterDefinition{
			{Name: "big", Type: TemplateParameterInteger},
			{Name: "price", Type: TemplateParameterDecimal},
		},
		Values: map[string]any{
			"big":   int64(9223372036854775807), // int64 max — must not round
			"price": "12345678901234567890.000001",
		},
	})
	if err != nil {
		t.Fatalf("CompilePG error: %v", err)
	}
	if want := []any{int64(9223372036854775807), "12345678901234567890.000001"}; !reflect.DeepEqual(compiled.Args, want) {
		t.Fatalf("compiled args = %#v, want %#v (exact int64 and decimal text)", compiled.Args, want)
	}
	if strings.Contains(compiled.Statement, "9223372036854775807") || strings.Contains(compiled.Statement, "12345678901234567890") {
		t.Fatalf("compiled statement leaks bound values: %q", compiled.Statement)
	}
}

func TestTemplateStatementCompilerPG_DecimalRejectsNonDecimalText(t *testing.T) {
	t.Parallel()

	_, err := NewTemplateStatementCompiler().CompilePG(TemplateStatementInput{
		Statement:   `SELECT * FROM t WHERE price = :price`,
		Definitions: []TemplateParameterDefinition{{Name: "price", Type: TemplateParameterDecimal}},
		Values:      map[string]any{"price": "1; DROP TABLE t"},
	})
	if !errors.Is(err, ErrTemplateParameterInvalid) {
		t.Fatalf("CompilePG error = %v, want decimal rejection", err)
	}
}

func TestTemplateStatementCompilerPG_CompiledStatementPassesGuardAndPagination(t *testing.T) {
	t.Parallel()

	compiler := NewTemplateStatementCompiler()
	compiled, err := compiler.CompilePG(TemplateStatementInput{
		Statement:   `SELECT id FROM orders WHERE status = :status`,
		Definitions: []TemplateParameterDefinition{{Name: "status", Type: TemplateParameterString}},
		Values:      map[string]any{"status": "paid"},
	})
	if err != nil {
		t.Fatalf("CompilePG error: %v", err)
	}
	guarded, err := pgsql.GuardPG(compiled.Statement)
	if err != nil {
		t.Fatalf("GuardPG compiled statement: %v", err)
	}
	if _, err := pgsql.PaginatePG(guarded.Tree, pgsql.PageOptions{Page: 1, PageSize: 10, MaxRows: 100}); err != nil {
		t.Fatalf("PaginatePG compiled statement: %v", err)
	}
}

func TestTemplateStatementCompilerPG_LimitPlaceholderRejectedByRealPaginationGate(t *testing.T) {
	t.Parallel()

	// G8/G6 seam: `LIMIT :n` compiles cleanly to `LIMIT $1` — declaration and
	// placeholder rules are satisfied — so only the real pagination gate can
	// reject it, and it must reject with unsupported_limit_offset_form.
	compiler := NewTemplateStatementCompiler()
	compiled, err := compiler.CompilePG(TemplateStatementInput{
		Statement:   `SELECT id FROM orders LIMIT :n`,
		Definitions: []TemplateParameterDefinition{{Name: "n", Type: TemplateParameterInteger}},
		Values:      map[string]any{"n": int64(10)},
	})
	if err != nil {
		t.Fatalf("CompilePG unexpectedly rejected LIMIT template: %v", err)
	}
	if !strings.Contains(compiled.Statement, "LIMIT $1") {
		t.Fatalf("compiled statement = %q, want generated LIMIT $1", compiled.Statement)
	}
	guarded, err := pgsql.GuardPG(compiled.Statement)
	if err != nil {
		t.Fatalf("GuardPG compiled statement: %v", err)
	}
	_, err = pgsql.PaginatePG(guarded.Tree, pgsql.PageOptions{Page: 1, PageSize: 10, MaxRows: 100})
	var reject *pgsql.RejectError
	if !errors.As(err, &reject) || reject.Code != "unsupported_limit_offset_form" {
		t.Fatalf("PaginatePG error = %v, want unsupported_limit_offset_form", err)
	}
}
