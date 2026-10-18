// Package service holds the private trusted PG text-parameter substrate.
// input: TemplateStatementInput (declared :name markers + typed values) and
// the *pgParameterPlan a caller attaches to PGExecuteInput
// output: pgParameterPlan, compilePGPlan, encodePGParameterText,
// snapshotPGParameterPlan — owned text values bound by generated $k
// position; values never enter SQL text
// pos: PG-MULTI-ENTRY stage 1 — only the template compiler (and later the
// navigation builder) may produce a plan through the closed source enum;
// the executor snapshots it independently at attempt entry, proves dense
// {1..n} ParamRef coverage on the guard tree, and transports owned [][]byte
// text with server-side type inference. No public arbitrary SQL+Args API
// exists
// note: if this file changes, update header and README.md
package service

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/pgsql"
)

// pgParameterSource is the closed enum of trusted plan producers — anything
// else is an internal contract fault, never a user error.
type pgParameterSource int

const (
	pgParameterSourceTemplate pgParameterSource = iota + 1
	pgParameterSourceNavigation
)

// pgParameterPlan is the private contract between a trusted producer and one
// governed attempt: the compiled $k statement plus the wire text of each
// bound value in $k order — values[i] feeds parameter i+1 by number, never
// by traversal order. All fields stay package-private: there is no public
// arbitrary SQL+args path into the governed executor.
type pgParameterPlan struct {
	statement string
	values    []string
	source    pgParameterSource
}

// compilePGPlan builds a template-sourced plan: CompilePG emits the $k
// statement and occurrence-ordered typed args, then each arg must encode
// losslessly to text — string verbatim, int64 decimal, bool true/false.
func (c *TemplateStatementCompiler) compilePGPlan(input TemplateStatementInput) (*pgParameterPlan, error) {
	compiled, err := c.CompilePG(input)
	if err != nil {
		return nil, err
	}
	values := make([]string, 0, len(compiled.Args))
	for _, arg := range compiled.Args {
		text, err := encodePGParameterText(arg)
		if err != nil {
			return nil, err
		}
		values = append(values, text)
	}
	return &pgParameterPlan{
		statement: compiled.Statement,
		values:    values,
		source:    pgParameterSourceTemplate,
	}, nil
}

// encodePGParameterText renders one typed arg as wire text. Only the
// compiler's three value kinds are legal — nil, every other Go type, NUL
// bytes, and invalid UTF-8 are rejected under the shared parameter sentinel
// without echoing the value or any statement text.
func encodePGParameterText(arg any) (string, error) {
	var text string
	switch v := arg.(type) {
	case string:
		text = v
	case int64:
		text = strconv.FormatInt(v, 10)
	case bool:
		text = strconv.FormatBool(v)
	default:
		return "", fmt.Errorf("%w: parameter value cannot be encoded", ErrTemplateParameterInvalid)
	}
	if strings.IndexByte(text, 0) >= 0 || !utf8.ValidString(text) {
		return "", fmt.Errorf("%w: parameter value is not valid text", ErrTemplateParameterInvalid)
	}
	return text, nil
}

// snapshotPGParameterPlan takes the attempt's independent copy of the
// caller's plan: the source must be a member of the closed enum and the plan
// must describe exactly this attempt's statement. The snapshot shares no
// backing storage with the caller — the executor never reads caller-owned
// mutable value slices.
func snapshotPGParameterPlan(plan *pgParameterPlan, statement string) (*pgParameterPlan, error) {
	if plan == nil {
		return nil, nil
	}
	switch plan.source {
	case pgParameterSourceTemplate, pgParameterSourceNavigation:
	default:
		return nil, fmt.Errorf("%w: parameter plan has no trusted source", pgsql.ErrEvidenceUnavailable)
	}
	if plan.statement != statement {
		return nil, fmt.Errorf("%w: parameter plan does not match the attempt statement", pgsql.ErrEvidenceUnavailable)
	}
	return &pgParameterPlan{
		statement: plan.statement,
		source:    plan.source,
		values:    append([]string(nil), plan.values...),
	}, nil
}

// wireValues renders the snapshot's text values as transport parameters. An
// empty string becomes a non-nil empty slice — a zero-length value, never
// SQL NULL; no nil element is legal.
func (p *pgParameterPlan) wireValues() [][]byte {
	out := make([][]byte, len(p.values))
	for i, v := range p.values {
		b := []byte(v)
		if b == nil {
			b = []byte{}
		}
		out[i] = b
	}
	return out
}
