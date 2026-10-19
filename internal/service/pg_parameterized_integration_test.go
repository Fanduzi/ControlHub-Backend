//go:build integration

// Package service proves the trusted PG text-parameter substrate end to end
// against the disposable PostgreSQL 16 lab.
// input: pgExecFixture, pgExecLabConfig ro_bind, compilePGPlan template
// inputs, *pgParameterPlan on PGExecuteInput
// output: TestPGExecute_Parameterized* — typed value binding through the
// full governed chain (text fidelity incl. t/f and int64 bounds, empty vs
// NULL, occurrence-order numbering, static plan, CTE/JOIN/Q+D shapes),
// LIMIT :n refused by the real pagination gate before the wire,
// server-side parameter type errors mapped to the fixed safe rejection, and
// R1-1 error-origin proofs (statement/data failures under a bound plan keep
// the backend taxonomy; only the parameter's own cast rejects)
// pos: PG-MULTI-ENTRY stage 1 real-PostgreSQL acceptance — parameters bind
// by $k number on the wire, values never enter statement text
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
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

// R1-1 error origins: with a bound plan in flight, a failure that originates
// in the STATEMENT or the STORED DATA — not in a parameter's own cast — must
// keep the safe backend taxonomy. The parameter-type rejection is reserved
// for values the server cannot cast in the $k slot. The native-$n oracle
// (real ExecParams binding, never interpolation) captures the server SQLSTATE
// first — code only, never messages/SQL/values — and is not part of the
// production chain.
func TestPGExecute_ParameterizedErrorOrigins(t *testing.T) {
	lab := pgExecFixture(t)
	ctx := context.Background()
	compiler := NewTemplateStatementCompiler()

	// Stored data that cannot take the cast the statement imposes — the
	// 22P02 originates in the row, not in $1.
	admin := lab.adminConn(t)
	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS app.raw_inputs (id bigint PRIMARY KEY, payload text)`,
		`GRANT SELECT ON app.raw_inputs TO ro_bind`,
		`DELETE FROM app.raw_inputs`,
		`INSERT INTO app.raw_inputs VALUES (1, 'stored-invalid-int-R1')`,
	} {
		if _, err := admin.Exec(ctx, stmt); err != nil {
			t.Fatalf("raw_inputs fixture: %v", err)
		}
	}
	_ = admin.Close(ctx)

	// oracleSQLSTATE runs the $1-equivalent natively and returns only the
	// server SQLSTATE — the wire truth the governed chain must preserve.
	oracleSQLSTATE := func(t *testing.T, nativeSQL string, values [][]byte) string {
		t.Helper()
		conn, err := pgx.ConnectConfig(ctx, pgExecLabConfig(t, lab))
		if err != nil {
			t.Fatalf("oracle connect: %v", err)
		}
		defer conn.Close(ctx)
		res := conn.PgConn().ExecParams(ctx, nativeSQL, values, nil, nil, nil).Read()
		var pgErr *pgconn.PgError
		if res == nil || !errors.As(res.Err, &pgErr) {
			t.Fatalf("oracle expected a PgError: %+v", res)
		}
		return pgErr.Code
	}

	// wantBackend asserts the safe backend taxonomy: a real server failure,
	// never the controlled parameter rejection, never a leaked PgError.
	wantBackend := func(t *testing.T, label string, err error, ev *PGAttemptEvidence) {
		t.Helper()
		var re *pgsql.RejectError
		if errors.As(err, &re) {
			t.Fatalf("%s: err = %v — a statement/data failure was misclassified as the parameter rejection", label, err)
		}
		if !errors.Is(err, ErrQueryBackendFailure) {
			t.Fatalf("%s: err = %v, want ErrQueryBackendFailure", label, err)
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			t.Fatalf("%s: raw server error surfaced: %v", label, err)
		}
		if ev != nil && ev.Status == model.QueryExecutionSuccess {
			t.Fatalf("%s: failed attempt reported success: %+v", label, ev)
		}
	}

	tagDefs := []TemplateParameterDefinition{{Name: "tag", Type: TemplateParameterString}}
	okVal := map[string]any{"tag": "ok"}

	// A — statement arithmetic overflow (server 22003) with a bound plan:
	// the failure is in the expression, not in the parameter slot.
	t.Run("A statement overflow is backend failure", func(t *testing.T) {
		if code := oracleSQLSTATE(t,
			`SELECT $1::text AS tag, (9223372036854775807::bigint + 1) AS n`,
			[][]byte{[]byte("ok")}); code != "22003" {
			t.Fatalf("oracle SQLSTATE = %s, want 22003", code)
		}
		plan, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :tag::text AS tag, (9223372036854775807::bigint + 1) AS n`,
			Definitions: tagDefs,
			Values:      okVal,
		})
		if err != nil {
			t.Fatalf("compilePGPlan A: %v", err)
		}
		before := pgExecTransportCalls.Load()
		_, ev, gerr := ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, plan, pageOpts(1, 25)))
		wantBackend(t, "A", gerr, ev)
		if got := pgExecTransportCalls.Load() - before; got != 1 {
			t.Fatalf("A result ExecParams delta = %d, want exactly 1 — the failure happened on the result path", got)
		}
		if ev == nil || ev.Stage != PGStageExecute || !ev.UserSQLSent {
			t.Fatalf("A evidence = %+v, want execute stage with the statement transmitted", ev)
		}
	})

	// B — stored-data cast failure (server 22P02) with a bound plan: the
	// invalid text lives in the table, the parameter itself is valid.
	t.Run("B stored-data cast is backend failure", func(t *testing.T) {
		if code := oracleSQLSTATE(t,
			`SELECT $1::text AS tag, r.payload::int FROM app.raw_inputs AS r`,
			[][]byte{[]byte("ok")}); code != "22P02" {
			t.Fatalf("oracle SQLSTATE = %s, want 22P02", code)
		}
		plan, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :tag::text AS tag, r.payload::int FROM app.raw_inputs AS r`,
			Definitions: tagDefs,
			Values:      okVal,
		})
		if err != nil {
			t.Fatalf("compilePGPlan B: %v", err)
		}
		before := pgExecTransportCalls.Load()
		_, ev, gerr := ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, plan, pageOpts(1, 25)))
		wantBackend(t, "B", gerr, ev)
		if got := pgExecTransportCalls.Load() - before; got != 1 {
			t.Fatalf("B result ExecParams delta = %d, want exactly 1 — the failure happened on the result path", got)
		}
		if ev == nil || ev.Stage != PGStageExecute || !ev.UserSQLSent {
			t.Fatalf("B evidence = %+v, want execute stage with the statement transmitted", ev)
		}
	})

	// C — a value the parameter's own cast cannot take stays the controlled
	// G8 rejection: same SQLSTATE family as B but raised by the parameter.
	t.Run("C parameter cast stays controlled rejection", func(t *testing.T) {
		// Native protocol control (oracle — real driver calls, never the
		// production chain): the unnamed describe of the original statement
		// succeeds and infers int4 (OID 23); the fixed parameter-only probe
		// then fails the caller's value with 22P02. The conversion is
		// proven to originate in the value slot, not guessed from a code.
		conn, err := pgx.ConnectConfig(ctx, pgExecLabConfig(t, lab))
		if err != nil {
			t.Fatalf("C oracle connect: %v", err)
		}
		defer conn.Close(ctx)
		sd, err := conn.PgConn().Prepare(ctx, "", `SELECT $1::int`, nil)
		if err != nil {
			t.Fatalf("C oracle describe: %v", err)
		}
		if len(sd.ParamOIDs) != 1 || sd.ParamOIDs[0] != 23 {
			t.Fatalf("C describe ParamOIDs = %v, want [23]", sd.ParamOIDs)
		}
		pres := conn.PgConn().ExecParams(ctx,
			`SELECT NULL WHERE FALSE AND ($1 IS NULL)`,
			[][]byte{[]byte("invalid-param-marker")}, sd.ParamOIDs, nil, []int16{0}).Read()
		var ppErr *pgconn.PgError
		if pres == nil || !errors.As(pres.Err, &ppErr) || ppErr.Code != "22P02" {
			t.Fatalf("C probe oracle result = %+v, want 22P02", pres)
		}

		plan, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :x::int`,
			Definitions: []TemplateParameterDefinition{{Name: "x", Type: TemplateParameterString}},
			Values:      map[string]any{"x": "invalid-param-marker"},
		})
		if err != nil {
			t.Fatalf("compilePGPlan C: %v", err)
		}
		before := pgExecTransportCalls.Load()
		_, ev, gerr := ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, plan, pageOpts(1, 25)))
		var re *pgsql.RejectError
		if !errors.As(gerr, &re) || re.Code != "query_not_allowed" ||
			re.Message != "pgsql: parameter type is not supported by statement context" {
			t.Fatalf("C: err = %v, want fixed parameter-type rejection", gerr)
		}
		if got := pgExecTransportCalls.Load() - before; got != 0 {
			t.Fatalf("C result ExecParams delta = %d, want 0 — the rejection precedes the result dispatch", got)
		}
		// The original statement's Parse/Describe DID cross the wire —
		// sent is a transmission fact, not an execution claim; the result
		// never ran, so no remote state exists for it.
		if ev == nil || ev.Stage != PGStageParameters || !ev.UserSQLSent ||
			ev.RemoteState != model.QueryExecutionRemoteNone {
			t.Fatalf("C evidence = %+v, want parameters stage + SQL transmitted + no result remote state", ev)
		}
	})

	// A caller cancel landing after the provenance describe but before the
	// result dispatch: the driver's pre-send mark on the result path must
	// not erase the transmission fact — UserSQLSent stays true while no
	// remote completion is fabricated for a result that never ran.
	t.Run("cancel after describe keeps the send fact", func(t *testing.T) {
		plan, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :tag::text AS tag`,
			Definitions: tagDefs,
			Values:      okVal,
		})
		if err != nil {
			t.Fatalf("compilePGPlan cancel: %v", err)
		}
		rctx, rcancel := context.WithCancel(ctx)
		defer rcancel()
		pgExecPreSendHook = func(context.Context) { rcancel() }
		defer func() { pgExecPreSendHook = nil }()
		_, ev, gerr := ExecutePGGovernedOutcome(rctx, pgParamInput(t, lab, plan, pageOpts(1, 25)))
		if !errors.Is(gerr, context.Canceled) {
			t.Fatalf("err = %v, want context.Canceled", gerr)
		}
		if ev == nil || !ev.UserSQLSent {
			t.Fatalf("evidence = %+v — the describe already transmitted; the result's pre-send mark must not erase it", ev)
		}
		if ev.RemoteState == model.QueryExecutionRemoteCompleted {
			t.Fatalf("remote_state = %v — the result never executed", ev.RemoteState)
		}
	})

	// Fallback boundary: a value that BINDS fine but fails a downstream SQL
	// cast, and a literal cast the statement itself cannot plan — neither
	// has a proven parameter source, so both keep the safe backend verdict.
	t.Run("D downstream casts stay backend failure", func(t *testing.T) {
		// :x::text binds as text (OID 25) — the probe accepts the value —
		// then the statement's own ::int cast fails the bound text at
		// execute. A parameter-adjacent expression with an unprovable
		// source is an ordinary backend failure, never the rejection.
		plan, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :x::text::int`,
			Definitions: []TemplateParameterDefinition{{Name: "x", Type: TemplateParameterString}},
			Values:      map[string]any{"x": "invalid-param-marker"},
		})
		if err != nil {
			t.Fatalf("compilePGPlan D: %v", err)
		}
		before := pgExecTransportCalls.Load()
		_, ev, gerr := ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, plan, pageOpts(1, 25)))
		wantBackend(t, "D downstream cast", gerr, ev)
		if got := pgExecTransportCalls.Load() - before; got != 1 {
			t.Fatalf("D result ExecParams delta = %d, want 1 — the cast failed on the result path", got)
		}
		if ev == nil || ev.Stage != PGStageExecute {
			t.Fatalf("D evidence = %+v, want execute stage", ev)
		}

		// A literal cast the statement itself cannot take is unrelated to
		// the bound parameter — it may fail inside the provenance describe
		// or at the result dispatch; the actual stage decides the honest
		// counter claim rather than asserting a phase that did not happen.
		literal, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :tag::text, 'bad-literal-r1'::int`,
			Definitions: tagDefs,
			Values:      okVal,
		})
		if err != nil {
			t.Fatalf("compilePGPlan literal: %v", err)
		}
		before = pgExecTransportCalls.Load()
		_, ev, gerr = ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, literal, pageOpts(1, 25)))
		wantBackend(t, "literal cast", gerr, ev)
		if ev == nil {
			t.Fatal("literal cast: no evidence")
		}
		switch ev.Stage {
		case PGStageParameters:
			if got := pgExecTransportCalls.Load() - before; got != 0 {
				t.Fatalf("literal cast failed pre-result but result delta = %d, want 0", got)
			}
		case PGStageExecute:
			if got := pgExecTransportCalls.Load() - before; got != 1 {
				t.Fatalf("literal cast failed on result path but delta = %d, want 1", got)
			}
		default:
			t.Fatalf("literal cast stage = %v, want parameters or execute", ev.Stage)
		}
	})

	// Controls — the taxonomy change must not move any non-parameter verdict.
	t.Run("controls keep their categories", func(t *testing.T) {
		// Static error, zero-value plan: backend.
		static, err := compiler.compilePGPlan(TemplateStatementInput{Statement: `SELECT 1/0`})
		if err != nil {
			t.Fatalf("static compilePGPlan: %v", err)
		}
		_, ev, gerr := ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, static, pageOpts(1, 25)))
		wantBackend(t, "static 22012", gerr, ev)

		// Bound plan, execution-time arithmetic: backend.
		bound, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :n::int / 0`,
			Definitions: []TemplateParameterDefinition{{Name: "n", Type: TemplateParameterInteger}},
			Values:      map[string]any{"n": int64(7)},
		})
		if err != nil {
			t.Fatalf("bound compilePGPlan: %v", err)
		}
		_, ev, gerr = ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, bound, pageOpts(1, 25)))
		wantBackend(t, "bound 22012", gerr, ev)

		// Bound plan, missing relation: the bind-stage controlled rejection,
		// not the parameter rejection.
		relPlan, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :tag::text AS tag, o.id FROM app.no_such_rel_r1 AS o`,
			Definitions: tagDefs,
			Values:      okVal,
		})
		if err != nil {
			t.Fatalf("rel compilePGPlan: %v", err)
		}
		_, _, gerr = ExecutePGGovernedOutcome(ctx, pgParamInput(t, lab, relPlan, pageOpts(1, 25)))
		if code := rejectCodeOf(t, gerr); code != "query_object_not_found" {
			t.Fatalf("missing relation code = %s, want query_object_not_found (err %v)", code, gerr)
		}

		// Bound plan, attempt deadline: timeout taxonomy.
		timeoutPlan, err := compiler.compilePGPlan(TemplateStatementInput{
			Statement:   `SELECT :tag::text AS tag, count(*) FROM generate_series(1, 2000000000)`,
			Definitions: tagDefs,
			Values:      okVal,
		})
		if err != nil {
			t.Fatalf("timeout compilePGPlan: %v", err)
		}
		timeoutIn := pgParamInput(t, lab, timeoutPlan, pageOpts(1, 25))
		timeoutIn.Timeout = 1500 * time.Millisecond
		_, _, gerr = ExecutePGGovernedOutcome(ctx, timeoutIn)
		if !errors.Is(gerr, ErrQueryTimeout) {
			t.Fatalf("timeout err = %v, want ErrQueryTimeout", gerr)
		}

		// Bound plan, pre-canceled request: canceled taxonomy.
		cctx, ccancel := context.WithCancel(ctx)
		ccancel()
		_, _, gerr = ExecutePGGovernedOutcome(cctx, pgParamInput(t, lab, bound, pageOpts(1, 25)))
		if !errors.Is(gerr, context.Canceled) {
			t.Fatalf("canceled err = %v, want context.Canceled", gerr)
		}
	})
	waitNoPGBackends(t, lab, "ch-t7s34-exec")
}
