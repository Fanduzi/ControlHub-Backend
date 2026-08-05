package service

import (
	"fmt"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

type templateStatementDeclaration struct {
	statement   string
	definitions map[string]TemplateParameterDefinition
	bindings    []TemplateParameterDefinition
}

func (c *TemplateStatementCompiler) validateDeclarations(statement string, definitions []TemplateParameterDefinition) (CompiledTemplateStatement, error) {
	declaration, err := c.compileDeclaration(statement, definitions)
	if err != nil {
		return CompiledTemplateStatement{}, err
	}
	return CompiledTemplateStatement{Statement: declaration.statement}, nil
}

func (c *TemplateStatementCompiler) compileDeclaration(statement string, definitions []TemplateParameterDefinition) (templateStatementDeclaration, error) {
	trimmed := strings.TrimSpace(statement)
	if trimmed == "" {
		return templateStatementDeclaration{}, ErrTemplateStatementInvalid
	}
	pieces, err := c.parser.SplitStatementToPieces(trimmed)
	if err != nil {
		return templateStatementDeclaration{}, ErrTemplateStatementInvalid
	}
	nonEmptyPieces := 0
	for _, piece := range pieces {
		if strings.TrimSpace(piece) != "" {
			nonEmptyPieces++
		}
	}
	if nonEmptyPieces != 1 {
		return templateStatementDeclaration{}, ErrTemplateStatementInvalid
	}
	parsed, err := c.parser.Parse(trimmed)
	if err != nil {
		return templateStatementDeclaration{}, ErrTemplateStatementInvalid
	}
	parsedQuery := sqlparser.NewParsedQuery(parsed)
	declared, err := templateDeclarations(definitions)
	if err != nil {
		return templateStatementDeclaration{}, err
	}
	used := make(map[string]int)
	bindings := make([]TemplateParameterDefinition, 0, len(parsedQuery.BindLocations()))
	for _, location := range parsedQuery.BindLocations() {
		if location.Offset < 0 || location.Length < 2 || location.Offset+location.Length > len(parsedQuery.Query) {
			return templateStatementDeclaration{}, ErrTemplateStatementInvalid
		}
		marker := parsedQuery.Query[location.Offset : location.Offset+location.Length]
		if strings.HasPrefix(marker, "::") {
			return templateStatementDeclaration{}, fmt.Errorf("%w: list parameters are unsupported", ErrTemplateParameterInvalid)
		}
		if !strings.HasPrefix(marker, ":") {
			return templateStatementDeclaration{}, ErrTemplateStatementInvalid
		}
		definition, ok := declared[marker[1:]]
		if !ok {
			return templateStatementDeclaration{}, fmt.Errorf("%w: undeclared parameter %q", ErrTemplateParameterInvalid, marker[1:])
		}
		used[definition.Name]++
		bindings = append(bindings, definition)
	}
	for name := range declared {
		if used[name] == 0 {
			return templateStatementDeclaration{}, fmt.Errorf("%w: parameter %q has no placeholder", ErrTemplateParameterInvalid, name)
		}
		if used[name] != 1 {
			return templateStatementDeclaration{}, fmt.Errorf("%w: parameter %q must have exactly one placeholder", ErrTemplateParameterInvalid, name)
		}
	}
	return templateStatementDeclaration{
		statement:   replaceTemplateBindLocations(parsedQuery),
		definitions: declared,
		bindings:    bindings,
	}, nil
}
