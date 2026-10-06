// Package service provides the PostgreSQL dialect of the governed template compiler.
// input: errors, fmt, strings, internal/pgsql
// output: TemplateStatementCompiler.CompilePG, validatePGDeclarations — declared :name markers become generated $k placeholders bound by the driver, never interpolated
// pos: G8 PostgreSQL template compilation (T11) — one lexical pass rewrites declared :name markers to positional $k parameters; casts, literals, comments, dollar-quoted bodies, and array slices pass through verbatim; native $n input is rejected; each declared parameter must appear exactly once; the compiled statement still owes GuardPG and the G6 pagination gate
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fan/controlhub/internal/pgsql"
)

// CompilePG compiles a PostgreSQL template statement. The returned Statement
// carries generated $k placeholders in marker-occurrence order; Args are the
// driver-bound values in the same order. Values never enter the SQL text.
// The compiled statement is not yet executed-legal: the caller must still run
// GuardPG and the pagination gate before any execution path consumes it.
func (c *TemplateStatementCompiler) CompilePG(input TemplateStatementInput) (CompiledTemplateStatement, error) {
	declaration, err := c.compilePGDeclaration(input.Statement, input.Definitions)
	if err != nil {
		return CompiledTemplateStatement{}, err
	}
	args, err := bindTemplateArguments(declaration, input.Values)
	if err != nil {
		return CompiledTemplateStatement{}, err
	}
	return CompiledTemplateStatement{
		Statement: declaration.statement,
		Args:      args,
	}, nil
}

// validatePGDeclarations checks a PostgreSQL template's declaration contract
// without values and returns the compiled placeholder statement. Callers then
// run GuardPG and the pagination gate on the returned text so a template can
// never be saved in a shape execution would reject.
func (c *TemplateStatementCompiler) validatePGDeclarations(statement string, definitions []TemplateParameterDefinition) (CompiledTemplateStatement, error) {
	declaration, err := c.compilePGDeclaration(statement, definitions)
	if err != nil {
		return CompiledTemplateStatement{}, err
	}
	return CompiledTemplateStatement{Statement: declaration.statement}, nil
}

// compilePGDeclaration is the PG dialect counterpart of compileDeclaration:
// one lexical pass finds :name markers, the statement is rebuilt with
// generated $k placeholders, and the declared/used sets must match exactly.
func (c *TemplateStatementCompiler) compilePGDeclaration(statement string, definitions []TemplateParameterDefinition) (templateStatementDeclaration, error) {
	trimmed := strings.TrimSpace(statement)
	if trimmed == "" {
		return templateStatementDeclaration{}, ErrTemplateStatementInvalid
	}
	declared, err := templateDeclarations(definitions)
	if err != nil {
		return templateStatementDeclaration{}, err
	}
	markers, err := pgsql.ScanTemplatePlaceholders(trimmed)
	if err != nil {
		if errors.Is(err, pgsql.ErrTemplateNativeParameter) {
			return templateStatementDeclaration{}, fmt.Errorf("%w: %v", ErrTemplateParameterInvalid, err)
		}
		return templateStatementDeclaration{}, fmt.Errorf("%w: %v", ErrTemplateStatementInvalid, err)
	}
	var rewritten strings.Builder
	rewritten.Grow(len(trimmed))
	bindings := make([]TemplateParameterDefinition, 0, len(markers))
	used := make(map[string]int, len(markers))
	pos := 0
	for index, marker := range markers {
		definition, ok := declared[marker.Name]
		if !ok {
			return templateStatementDeclaration{}, fmt.Errorf("%w: undeclared parameter %q", ErrTemplateParameterInvalid, marker.Name)
		}
		rewritten.WriteString(trimmed[pos:marker.Offset])
		fmt.Fprintf(&rewritten, "$%d", index+1)
		pos = marker.Offset + marker.Length
		used[marker.Name]++
		bindings = append(bindings, definition)
	}
	rewritten.WriteString(trimmed[pos:])
	for name := range declared {
		if used[name] == 0 {
			return templateStatementDeclaration{}, fmt.Errorf("%w: parameter %q has no placeholder", ErrTemplateParameterInvalid, name)
		}
		if used[name] != 1 {
			return templateStatementDeclaration{}, fmt.Errorf("%w: parameter %q must have exactly one placeholder", ErrTemplateParameterInvalid, name)
		}
	}
	return templateStatementDeclaration{
		statement:   rewritten.String(),
		definitions: declared,
		bindings:    bindings,
	}, nil
}
