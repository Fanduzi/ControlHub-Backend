//go:build integration

// Package service proves the trusted PG text-parameter substrate end to end
// against the disposable PostgreSQL 16 lab.
// input: pgExecFixture, pgExecLabConfig ro_bind, compilePGPlan template
// inputs, *pgParameterPlan on PGExecuteInput
// output: TestPGExecute_Parameterized* — typed value binding through the
// full governed chain (text fidelity incl. t/f and int64 bounds, empty vs
// NULL, occurrence-order numbering, static plan, CTE/JOIN/Q+D shapes),
// LIMIT :n refused by the real pagination gate before the wire, and
// server-side parameter type errors mapped to the fixed safe rejection
// pos: PG-MULTI-ENTRY stage 1 real-PostgreSQL acceptance — parameters bind
// by $k number on the wire, values never enter statement text
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

func pgParamInput(t *testing.T, lab pgPoolLab, plan *pgParameterPlan, page pgsql.PageOptions) PGExecuteInput {
	t.Helper()
	in := pgExecInput(t, lab, plan.statement, page)
	in.parameters = plan
	return in
}

// Typed values bind through the whole governed chain — bind → rewrite →
// ExecParams → verify → audit — and come back as database-native text:
// quotes/backslashes/Unicode/SQL-looking bytes verbatim, int64 exact at
// both bounds, decimal literal preserved, booleans as native t/f.
func TestPGExecute_ParameterizedTypedValues(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()
	compiler := NewTemplateStatementCompiler()
	// Declaration order disagrees with marker-occurrence order on purpose —
	// numbering follows the markers.
	defs := []TemplateParameterDefinition{
		{Name: "flag", Type: TemplateParameterBoolean},
		{Name: "num", Type: TemplateParameterDecimal},
		{Name: "txt", Type: TemplateParameterString},
		{Name: "big", Type: TemplateParameterInteger},
	}
	stmt := `SELECT :txt::text, :big::bigint, :num::numeric, :flag::boolean`

	for _, tc := range []struct {
		name   string
		values map[string]any
		want   []string
	}{
		{"sql-looking text, int64 max, decimal, true",
			map[string]any{
				"txt":  `O'Hara \path\ 中文 '; DROP TABLE orders; --`,
				"big":  int64(9223372036854775807),
				"num":  "12345678901234567890.0000000001",
				"flag": true,
			},
			[]string{`O'Hara \path\ 中文 '; DROP TABLE orders; --`, "9223372036854775807", "12345678901234567890.0000000001", "t"}},
		{"int64 min, false",
			map[string]any{
				"txt":  "x",
				"big":  int64(-9223372036854775808),
				"num":  "0.5",
				"flag": false,
			},
			[]string{"x", "-9223372036854775808", "0.5", "f"}},
		{"empty string is not NULL",
			map[string]any{
				"txt":  "",
				"big":  int64(0),
				"num":  "1",
				"flag": true,
			},
			[]string{"", "0", "1", "t"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := compiler.compilePGPlan(TemplateStatementInput{
				Statement: stmt, Definitions: defs, Values: tc.values})
			if err != nil {
				t.Fatalf("compilePGPlan: %v", err)
			}
			res, ev, err := ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, plan, pageOpts(1, 25)))
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if ev == nil || ev.Status != model.QueryExecutionSuccess || !ev.UserSQLSent {
				t.Fatalf("evidence = %+v", ev)
			}
			if len(res.Rows) != 1 || len(res.Rows[0]) != 4 {
				t.Fatalf("rows = %v", res.Rows)
			}
			for i, want := range tc.want {
				if got := textCell(t, res.Rows, 0, i); got != want {
					t.Fatalf("cell %d = %q, want %q", i, got, want)
				}
			}
		})
	}

	// A bound empty string and a literal NULL stay distinct on the same row.
	plan, err := compiler.compilePGPlan(TemplateStatementInput{
		Statement:   `SELECT :txt::text, NULL::text`,
		Definitions: []TemplateParameterDefinition{{Name: "txt", Type: TemplateParameterString}},
		Values:      map[string]any{"txt": ""},
	})
	if err != nil {
		t.Fatalf("compilePGPlan: %v", err)
	}
	res, err := ExecutePGGoverned(ctx, pgParamInput(t, lab, plan, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(res.Rows) != 1 || res.Rows[0][0] == nil || *res.Rows[0][0] != "" {
		t.Fatalf("empty string cell = %v", res.Rows)
	}
	if res.Rows[0][1] != nil {
		t.Fatal("literal NULL must stay nil")
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// Parameters ride through every structural position the governed rewrite
// supports — CTE bodies, JOIN conditions, DISTINCT Q+D, ORDER BY — and keep
// their $k number binding through whatever duplication the transform makes.
func TestPGExecute_ParameterizedShapes(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()
	compiler := NewTemplateStatementCompiler()

	// A parameterless template is still a legal plan — zero bound values.
	static, err := compiler.compilePGPlan(TemplateStatementInput{Statement: `SELECT 1`})
	if err != nil {
		t.Fatalf("static compilePGPlan: %v", err)
	}
	res, err := ExecutePGGoverned(ctx, pgParamInput(t, lab, static, pageOpts(1, 25)))
	if err != nil {
		t.Fatalf("static execute: %v", err)
	}
	if len(res.Rows) != 1 || textCell(t, res.Rows, 0, 0) != "1" {
		t.Fatalf("static rows = %v", res.Rows)
	}

	for _, tc := range []struct {
		name   string
		stmt   string
		defs   []TemplateParameterDefinition
		values map[string]any
		want   [][]string // expected text cells per row
	}{
		{"cte body", `WITH c AS (SELECT :v::text AS val) SELECT c.val FROM c`,
			[]TemplateParameterDefinition{{Name: "v", Type: TemplateParameterString}},
			map[string]any{"v": "inside-cte"}, [][]string{{"inside-cte"}}},
		{"where bind", `SELECT o.id FROM orders o WHERE o.item = :item::text ORDER BY o.id`,
			[]TemplateParameterDefinition{{Name: "item", Type: TemplateParameterString}},
			map[string]any{"item": "beta"}, [][]string{{"2"}}},
		{"join condition", `SELECT o.id, s.tag FROM orders o JOIN smallvals s ON s.id = o.id AND s.tag = :tag::text ORDER BY o.id`,
			[]TemplateParameterDefinition{{Name: "tag", Type: TemplateParameterString}},
			map[string]any{"tag": "tag-1"}, [][]string{{"1", "tag-1"}}},
		{"distinct q+d", `SELECT DISTINCT o.qty FROM orders o WHERE o.qty > :minq::int ORDER BY o.qty`,
			[]TemplateParameterDefinition{{Name: "minq", Type: TemplateParameterInteger}},
			map[string]any{"minq": int64(10)}, [][]string{{"20"}}},
		{"order by expression", `SELECT o.id FROM orders o WHERE o.qty >= :floor::int ORDER BY o.id`,
			[]TemplateParameterDefinition{{Name: "floor", Type: TemplateParameterInteger}},
			map[string]any{"floor": int64(20)}, [][]string{{"2"}, {"3"}}},
		{"distinct q+d ordinal duplicate", `SELECT DISTINCT :a::bigint AS v, :b::bigint AS w ORDER BY 1`,
			[]TemplateParameterDefinition{
				{Name: "a", Type: TemplateParameterInteger},
				{Name: "b", Type: TemplateParameterInteger},
			},
			map[string]any{"a": int64(5), "b": int64(7)}, [][]string{{"5", "7"}}},
		{"set-op leaves", `SELECT :a::text AS x UNION ALL SELECT :b::text ORDER BY 1`,
			[]TemplateParameterDefinition{
				{Name: "a", Type: TemplateParameterString},
				{Name: "b", Type: TemplateParameterString},
			},
			map[string]any{"a": "left", "b": "right"}, [][]string{{"left"}, {"right"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := compiler.compilePGPlan(TemplateStatementInput{
				Statement: tc.stmt, Definitions: tc.defs, Values: tc.values})
			if err != nil {
				t.Fatalf("compilePGPlan: %v", err)
			}
			res, _, err := ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, plan, pageOpts(1, 25)))
			if err != nil {
				t.Fatalf("execute: %v", err)
			}
			if len(res.Rows) != len(tc.want) {
				t.Fatalf("rows = %v, want %v", res.Rows, tc.want)
			}
			for r, row := range tc.want {
				for c, want := range row {
					if got := textCell(t, res.Rows, r, c); got != want {
						t.Fatalf("row %d cell %d = %q, want %q", r, c, got, want)
					}
				}
			}
		})
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// `LIMIT :n` compiles fine — declaration and placeholder rules are
// satisfied — so the real G6 gate must refuse it with the frozen
// unsupported_limit_offset_form verdict before any byte reaches the wire.
func TestPGExecute_ParameterizedLimitRejectedBeforeWire(t *testing.T) {
	lab := pgExecFixture(t)
	plan, err := NewTemplateStatementCompiler().compilePGPlan(TemplateStatementInput{
		Statement:   `SELECT o.id FROM orders o ORDER BY o.id LIMIT :n`,
		Definitions: []TemplateParameterDefinition{{Name: "n", Type: TemplateParameterInteger}},
		Values:      map[string]any{"n": int64(10)},
	})
	if err != nil {
		t.Fatalf("compilePGPlan: %v", err)
	}
	before := pgExecTransportCalls.Load()
	_, ev, err := ExecutePGGovernedOutcome(context.Background(), pgParamInput(t, lab, plan, pageOpts(1, 25)))
	if code := rejectCodeOf(t, err); code != "unsupported_limit_offset_form" {
		t.Fatalf("err = %v, want unsupported_limit_offset_form", err)
	}
	if got := pgExecTransportCalls.Load(); got != before {
		t.Fatalf("ExecParams calls %d→%d — LIMIT $1 reached the wire", before, got)
	}
	if ev != nil && ev.UserSQLSent {
		t.Fatal("evidence claims the statement was sent")
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// A bound value the server cannot give the statement-context type is the
// fixed parameter-type rejection — never a raw PgError, never an internal
// failure — while the attempt's remote fact still records the delivered
// ErrorResponse.
func TestPGExecute_ParameterizedBadCastIsControlledRejection(t *testing.T) {
	lab := pgExecFixture(t)
	plan, err := NewTemplateStatementCompiler().compilePGPlan(TemplateStatementInput{
		Statement:   `SELECT :x::int`,
		Definitions: []TemplateParameterDefinition{{Name: "x", Type: TemplateParameterString}},
		Values:      map[string]any{"x": "not-an-int"},
	})
	if err != nil {
		t.Fatalf("compilePGPlan: %v", err)
	}
	_, ev, err := ExecutePGGovernedOutcome(context.Background(), pgParamInput(t, lab, plan, pageOpts(1, 25)))
	var re *pgsql.RejectError
	if !errors.As(err, &re) || re.Code != "query_not_allowed" ||
		re.Message != "pgsql: parameter type is not supported by statement context" {
		t.Fatalf("err = %v, want fixed parameter-type rejection", err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		t.Fatalf("raw server error surfaced: %v", err)
	}
	for _, marker := range []string{"22P02", "invalid input", "not-an-int"} {
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("error leaks raw server detail %q: %v", marker, err)
		}
	}
	if ev != nil && ev.Status == model.QueryExecutionSuccess {
		t.Fatalf("rejected attempt reported success: %+v", ev)
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// Transport taxonomy stays honest: only real parameter-context failures
// against a plan that carries values map to the fixed parameter rejection.
// A static zero-value plan's division-by-zero (22012) and a bound plan's
// arithmetic failure both keep the ordinary failed/backend taxonomy — the
// parameter verdict never swallows unrelated server errors.
func TestPGExecute_ParameterizedTransportTaxonomy(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()
	compiler := NewTemplateStatementCompiler()

	// Static plan, no bound values: 22012 is a backend failure, never the
	// parameter-type rejection.
	static, err := compiler.compilePGPlan(TemplateStatementInput{Statement: `SELECT 1/0`})
	if err != nil {
		t.Fatalf("static compilePGPlan: %v", err)
	}
	_, ev, err := ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, static, pageOpts(1, 25)))
	var re *pgsql.RejectError
	if errors.As(err, &re) {
		t.Fatalf("static division error = %v, must not be a controlled rejection", err)
	}
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("static division error = %v, want backend taxonomy", err)
	}
	if ev != nil && ev.Status == model.QueryExecutionSuccess {
		t.Fatalf("division failure reported success: %+v", ev)
	}

	// Bound plan, arithmetic failure at execution: 22012 is not in the
	// parameter-context set even though values are present.
	bound, err := compiler.compilePGPlan(TemplateStatementInput{
		Statement:   `SELECT :n::int / 0`,
		Definitions: []TemplateParameterDefinition{{Name: "n", Type: TemplateParameterInteger}},
		Values:      map[string]any{"n": int64(7)},
	})
	if err != nil {
		t.Fatalf("bound compilePGPlan: %v", err)
	}
	_, ev, err = ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, bound, pageOpts(1, 25)))
	if errors.As(err, &re) {
		t.Fatalf("bound arithmetic error = %v, must not be the parameter rejection", err)
	}
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("bound arithmetic error = %v, want backend taxonomy", err)
	}
	if ev != nil && ev.Status == model.QueryExecutionSuccess {
		t.Fatalf("division failure reported success: %+v", ev)
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}

// A raw native $1 on the ordinary unbound path fails the executor's
// parameter contract before any connection work — zero transport.
func TestPGExecute_ParameterizedNativeParamUnbound(t *testing.T) {
	lab := pgExecFixture(t)
	before := pgExecTransportCalls.Load()
	_, ev, err := ExecutePGGovernedOutcome(context.Background(), pgExecInput(t, lab,
		`SELECT $1::text`, pageOpts(1, 25)))
	if err == nil {
		t.Fatal("native $1 executed unbound")
	}
	if !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
		t.Fatalf("err = %v, want evidence-unavailable", err)
	}
	if got := pgExecTransportCalls.Load(); got != before {
		t.Fatalf("ExecParams calls %d→%d — the wire was reached", before, got)
	}
	if ev != nil && ev.UserSQLSent {
		t.Fatal("evidence claims the statement was sent")
	}
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}
