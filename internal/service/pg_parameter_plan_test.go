// Package service proves the private trusted PG text-parameter substrate offline.
// input: template inputs, hand-built plans, minimal executor inputs
// output: compilePGPlan encoding/order/typing proofs, plan snapshot
// ownership and contract-fault tests, and the pre-wire native-parameter
// contract (an unbound attempt never lets a ParamRef reach transport)
// pos: PG-MULTI-ENTRY stage 1 — trusted text values bind by $k number and
// never enter SQL text; the ordinary path stays unbound and fails closed
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/pgsql"
)

func mustCompilePGPlan(t *testing.T, in TemplateStatementInput) *pgParameterPlan {
	t.Helper()
	plan, err := NewTemplateStatementCompiler().compilePGPlan(in)
	if err != nil {
		t.Fatalf("compilePGPlan: %v", err)
	}
	return plan
}

// Values encode to parameter text in marker-occurrence order — never
// definition or map order — and never enter the statement text. int64
// survives exactly at both int64 bounds, decimal keeps its literal text,
// booleans become true/false, and an empty string stays an empty value.
func TestPGParameterPlan_CompilesAndEncodes(t *testing.T) {
	t.Parallel()

	// Declaration order deliberately disagrees with occurrence order and
	// the values map is unordered — position must come from the markers.
	defs := []TemplateParameterDefinition{
		{Name: "flag", Type: TemplateParameterBoolean},
		{Name: "num", Type: TemplateParameterDecimal},
		{Name: "txt", Type: TemplateParameterString},
		{Name: "big", Type: TemplateParameterInteger},
	}
	for _, tc := range []struct {
		name       string
		values     map[string]any
		wantValues []string
	}{
		{
			name: "mixed types in occurrence order",
			values: map[string]any{
				"txt":  "plain",
				"big":  int64(9223372036854775807), // int64 max — no float rounding
				"num":  "12345678901234567890.0000000001",
				"flag": true,
			},
			wantValues: []string{"plain", "9223372036854775807", "12345678901234567890.0000000001", "true"},
		},
		{
			name: "int64 min and false",
			values: map[string]any{
				"txt":  "x",
				"big":  int64(-9223372036854775808),
				"num":  "0.5",
				"flag": false,
			},
			wantValues: []string{"x", "-9223372036854775808", "0.5", "false"},
		},
		{
			name: "empty string stays a value",
			values: map[string]any{
				"txt":  "",
				"big":  int64(0),
				"num":  "1",
				"flag": true,
			},
			wantValues: []string{"", "0", "1", "true"},
		},
		{
			name: "sql-looking string stays data",
			values: map[string]any{
				"txt":  `O'Hara \path\ 中文 '; DROP TABLE orders; --`,
				"big":  int64(1),
				"num":  "2",
				"flag": true,
			},
			wantValues: []string{`O'Hara \path\ 中文 '; DROP TABLE orders; --`, "1", "2", "true"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := mustCompilePGPlan(t, TemplateStatementInput{
				Statement:   `SELECT :txt::text, :big::bigint, :num::numeric, :flag::boolean`,
				Definitions: defs,
				Values:      tc.values,
			})
			if plan.statement != `SELECT $1::text, $2::bigint, $3::numeric, $4::boolean` {
				t.Fatalf("plan statement = %q", plan.statement)
			}
			if plan.source != pgParameterSourceTemplate {
				t.Fatalf("plan source = %d", plan.source)
			}
			if len(plan.values) != len(tc.wantValues) {
				t.Fatalf("values = %v", plan.values)
			}
			for i, want := range tc.wantValues {
				if plan.values[i] != want {
					t.Fatalf("values[%d] = %q, want %q", i, plan.values[i], want)
				}
				// A long enough value fragment must never appear inside the
				// statement text — binding happens on the wire, not in SQL.
				if len(want) > 3 && strings.Contains(plan.statement, want) {
					t.Fatalf("statement interpolates value %q", want)
				}
			}
		})
	}
}

// A parameterless statement still produces a legal plan — zero values is a
// valid binding set, not a missing plan.
func TestPGParameterPlan_StaticStatement(t *testing.T) {
	t.Parallel()

	plan := mustCompilePGPlan(t, TemplateStatementInput{Statement: `SELECT 1`})
	if plan.statement != `SELECT 1` || len(plan.values) != 0 || plan.source != pgParameterSourceTemplate {
		t.Fatalf("static plan = %+v", plan)
	}
}

// Only the compiler's three typed arg kinds can become parameter text: nil,
// every other Go type, NUL bytes, and invalid UTF-8 are rejected with the
// safe parameter error that never echoes the value.
func TestPGParameterPlan_RejectsUnencodableArgs(t *testing.T) {
	t.Parallel()

	bad := []struct {
		name string
		arg  any
	}{
		{"nil", nil},
		{"float", float64(1.5)},
		{"int not int64", int(7)},
		{"bytes", []byte("x")},
		{"nul byte", "a\x00b"},
		{"invalid utf8", "\xff\xfe"},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			got, err := encodePGParameterText(tc.arg)
			if !errors.Is(err, ErrTemplateParameterInvalid) {
				t.Fatalf("encode(%#v) = %q, %v — want ErrTemplateParameterInvalid", tc.arg, got, err)
			}
			if s, ok := tc.arg.(string); ok && len(s) > 0 && strings.Contains(err.Error(), s) {
				t.Fatalf("error echoes the rejected value: %v", err)
			}
		})
	}
}

// The attempt's plan snapshot is an independent copy under the closed
// trusted-source enum: caller mutation after snapshot cannot reach the
// attempt, and a plan describing different text or an unknown source fails
// closed.
func TestPGParameterPlan_SnapshotOwnsInput(t *testing.T) {
	t.Parallel()

	src := &pgParameterPlan{
		statement: `SELECT $1::text`,
		values:    []string{"a"},
		source:    pgParameterSourceTemplate,
	}
	snap, err := snapshotPGParameterPlan(src, `SELECT $1::text`)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap == src {
		t.Fatal("snapshot returned the caller's own plan")
	}
	src.values[0] = "mutated"
	if snap.values[0] != "a" {
		t.Fatalf("snapshot shares the caller's value slice: %v", snap.values)
	}

	if _, err := snapshotPGParameterPlan(src, `SELECT $2::text`); !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
		t.Fatalf("mismatched statement: err = %v, want evidence-unavailable", err)
	}
	for _, badSource := range []pgParameterSource{0, 99} {
		bad := &pgParameterPlan{statement: `SELECT $1::text`, source: badSource}
		if _, err := snapshotPGParameterPlan(bad, `SELECT $1::text`); !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
			t.Fatalf("source %d: err = %v, want evidence-unavailable", badSource, err)
		}
	}
	if plan, err := snapshotPGParameterPlan(nil, `SELECT 1`); err != nil || plan != nil {
		t.Fatalf("nil plan = (%v, %v)", plan, err)
	}
}

// Owned wire bytes keep the empty/NULL distinction: every element is a
// non-nil []byte — "" encodes a zero-length value, never SQL NULL.
func TestPGParameterPlan_WireValues(t *testing.T) {
	t.Parallel()

	plan := &pgParameterPlan{
		statement: `SELECT $1::text, $2::text`,
		values:    []string{"", "v"},
		source:    pgParameterSourceTemplate,
	}
	wire := plan.wireValues()
	if len(wire) != 2 || wire[0] == nil || len(wire[0]) != 0 || string(wire[1]) != "v" {
		t.Fatalf("wire values = %v", wire)
	}
}

// The executor's parameter contract runs before any connection work: raw
// native $k on the ordinary unbound path, a plan whose value count leaves a
// reference unbound, or a plan describing different text all fail closed
// without a transport call — proven by the send-adjacent counter.
func TestPGParameterContract_StopsBeforeWire(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	base := func(stmt string) PGExecuteInput {
		return PGExecuteInput{
			ConnConfig:   &pgx.ConnConfig{},
			Database:     "db",
			PinnedSchema: "app",
			Statement:    stmt,
			Page:         pgsql.PageOptions{Page: 1, PageSize: 25, MaxRows: 100},
			Timeout:      5 * time.Second,
		}
	}

	t.Run("native dollar param unbound", func(t *testing.T) {
		before := pgExecTransportCalls.Load()
		in := base(`SELECT $1::text`)
		res, ev, err := ExecutePGGovernedOutcome(ctx, in)
		if err == nil || res != nil {
			t.Fatalf("native $1 executed: res=%v err=%v", res, err)
		}
		if !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
			t.Fatalf("err = %v, want evidence-unavailable", err)
		}
		if got := pgExecTransportCalls.Load(); got != before {
			t.Fatalf("ExecParams calls %d→%d — the wire was reached", before, got)
		}
		if ev == nil || ev.UserSQLSent || ev.Stage != PGStageGuard {
			t.Fatalf("evidence = %+v", ev)
		}
	})

	t.Run("plan leaves reference unbound", func(t *testing.T) {
		before := pgExecTransportCalls.Load()
		stmt := `SELECT $1::text, $2::int`
		in := base(stmt)
		in.parameters = &pgParameterPlan{
			statement: stmt,
			values:    []string{"a"},
			source:    pgParameterSourceTemplate,
		}
		res, _, err := ExecutePGGovernedOutcome(ctx, in)
		if err == nil || res != nil {
			t.Fatalf("gap plan executed: res=%v err=%v", res, err)
		}
		if !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
			t.Fatalf("err = %v, want evidence-unavailable", err)
		}
		if got := pgExecTransportCalls.Load(); got != before {
			t.Fatalf("ExecParams calls %d→%d — the wire was reached", before, got)
		}
	})

	t.Run("plan for different statement", func(t *testing.T) {
		before := pgExecTransportCalls.Load()
		in := base(`SELECT 1`)
		in.parameters = &pgParameterPlan{
			statement: `SELECT $1::text`,
			values:    []string{"a"},
			source:    pgParameterSourceTemplate,
		}
		res, _, err := ExecutePGGovernedOutcome(ctx, in)
		if err == nil || res != nil {
			t.Fatalf("mismatched plan executed: res=%v err=%v", res, err)
		}
		if got := pgExecTransportCalls.Load(); got != before {
			t.Fatalf("ExecParams calls %d→%d — the wire was reached", before, got)
		}
	})
}
