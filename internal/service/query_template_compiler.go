// Package service provides the server-owned compiler for governed template statements.
// input: encoding/json, errors, fmt, math, regexp, strings, vitess.io/vitess/go/vt/sqlparser
// output: TemplateParameterType, TemplateParameterDefinition, TemplateStatementInput, CompiledTemplateStatement, GuardedTemplateStatement, TemplateStatementCompiler, NewTemplateStatementCompiler
// pos: AST-recognized named placeholders become positional driver bindings without interpolating values or changing guard ownership
package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

const (
	templateMaxParameters = 20
	templateMaxValueBytes = 4 * 1024
)

var (
	ErrTemplateStatementInvalid  = errors.New("template statement is invalid")
	ErrTemplateParameterInvalid  = errors.New("template parameter is invalid")
	templateParameterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	templateDecimalPattern       = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]+)?|\.[0-9]+)$`)
)

type TemplateParameterType string

const (
	TemplateParameterString  TemplateParameterType = "string"
	TemplateParameterInteger TemplateParameterType = "integer"
	TemplateParameterDecimal TemplateParameterType = "decimal"
	TemplateParameterBoolean TemplateParameterType = "boolean"
)

type TemplateParameterDefinition struct {
	Name string
	Type TemplateParameterType
}

type TemplateStatementInput struct {
	Statement   string
	Definitions []TemplateParameterDefinition
	Values      map[string]any
}

type CompiledTemplateStatement struct {
	Statement string
	Args      []any
}

type GuardedTemplateStatement struct {
	query GuardedQuery
	args  []any
}

type TemplateStatementCompiler struct {
	parser *sqlparser.Parser
}

func NewTemplateStatementCompiler() *TemplateStatementCompiler {
	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		panic(fmt.Sprintf("template compiler: construct parser: %v", err))
	}
	return &TemplateStatementCompiler{parser: parser}
}

func (c *TemplateStatementCompiler) Compile(input TemplateStatementInput) (CompiledTemplateStatement, error) {
	trimmed := strings.TrimSpace(input.Statement)
	if trimmed == "" {
		return CompiledTemplateStatement{}, ErrTemplateStatementInvalid
	}
	pieces, err := c.parser.SplitStatementToPieces(trimmed)
	if err != nil {
		return CompiledTemplateStatement{}, ErrTemplateStatementInvalid
	}
	nonEmptyPieces := 0
	for _, piece := range pieces {
		if strings.TrimSpace(piece) != "" {
			nonEmptyPieces++
		}
	}
	if nonEmptyPieces != 1 {
		return CompiledTemplateStatement{}, ErrTemplateStatementInvalid
	}
	parsed, err := c.parser.Parse(trimmed)
	if err != nil {
		return CompiledTemplateStatement{}, ErrTemplateStatementInvalid
	}
	parsedQuery := sqlparser.NewParsedQuery(parsed)

	declared, err := templateDeclarations(input.Definitions)
	if err != nil {
		return CompiledTemplateStatement{}, err
	}
	for name := range input.Values {
		if _, ok := declared[name]; !ok {
			return CompiledTemplateStatement{}, fmt.Errorf("%w: unknown parameter %q", ErrTemplateParameterInvalid, name)
		}
	}

	used := make(map[string]int)
	args := make([]any, 0, len(parsedQuery.BindLocations()))
	for _, location := range parsedQuery.BindLocations() {
		if location.Offset < 0 || location.Length < 2 || location.Offset+location.Length > len(parsedQuery.Query) {
			return CompiledTemplateStatement{}, ErrTemplateStatementInvalid
		}
		marker := parsedQuery.Query[location.Offset : location.Offset+location.Length]
		if strings.HasPrefix(marker, "::") {
			return CompiledTemplateStatement{}, fmt.Errorf("%w: list parameters are unsupported", ErrTemplateParameterInvalid)
		}
		if !strings.HasPrefix(marker, ":") {
			return CompiledTemplateStatement{}, ErrTemplateStatementInvalid
		}
		name := marker[1:]
		definition, ok := declared[name]
		if !ok {
			return CompiledTemplateStatement{}, fmt.Errorf("%w: undeclared parameter %q", ErrTemplateParameterInvalid, name)
		}
		value, ok := input.Values[name]
		if !ok {
			return CompiledTemplateStatement{}, fmt.Errorf("%w: missing parameter %q", ErrTemplateParameterInvalid, name)
		}
		bound, err := compileTemplateValue(definition, value)
		if err != nil {
			return CompiledTemplateStatement{}, err
		}
		used[name]++
		args = append(args, bound)
	}
	for name := range declared {
		if used[name] == 0 {
			return CompiledTemplateStatement{}, fmt.Errorf("%w: parameter %q has no placeholder", ErrTemplateParameterInvalid, name)
		}
	}
	return CompiledTemplateStatement{
		Statement: replaceTemplateBindLocations(parsedQuery),
		Args:      args,
	}, nil
}

func (c *TemplateStatementCompiler) CompileAndGuard(guard *QueryGuard, input TemplateStatementInput, requestedMaxRows int) (GuardedTemplateStatement, error) {
	compiled, err := c.Compile(input)
	if err != nil {
		return GuardedTemplateStatement{}, err
	}
	guarded, err := guard.Guard(compiled.Statement, requestedMaxRows)
	if err != nil {
		return GuardedTemplateStatement{}, err
	}
	guarded.ExecutableSQL, err = c.restorePositionalMarkers(guarded.ExecutableSQL, len(compiled.Args))
	if err != nil {
		return GuardedTemplateStatement{}, err
	}
	return GuardedTemplateStatement{query: guarded, args: append([]any(nil), compiled.Args...)}, nil
}

func (c *TemplateStatementCompiler) restorePositionalMarkers(statement string, expected int) (string, error) {
	parsed, err := c.parser.Parse(statement)
	if err != nil {
		return "", ErrTemplateStatementInvalid
	}
	parsedQuery := sqlparser.NewParsedQuery(parsed)
	locations := parsedQuery.BindLocations()
	if len(locations) != expected {
		return "", ErrTemplateStatementInvalid
	}
	for _, location := range locations {
		marker := parsedQuery.Query[location.Offset : location.Offset+location.Length]
		if !strings.HasPrefix(marker, ":v") {
			return "", ErrTemplateStatementInvalid
		}
	}
	return replaceTemplateBindLocations(parsedQuery), nil
}

func templateDeclarations(definitions []TemplateParameterDefinition) (map[string]TemplateParameterDefinition, error) {
	if len(definitions) > templateMaxParameters {
		return nil, ErrTemplateParameterInvalid
	}
	declared := make(map[string]TemplateParameterDefinition, len(definitions))
	for _, definition := range definitions {
		if !templateParameterNamePattern.MatchString(definition.Name) {
			return nil, fmt.Errorf("%w: invalid parameter name %q", ErrTemplateParameterInvalid, definition.Name)
		}
		if _, exists := declared[definition.Name]; exists {
			return nil, fmt.Errorf("%w: duplicate parameter %q", ErrTemplateParameterInvalid, definition.Name)
		}
		switch definition.Type {
		case TemplateParameterString, TemplateParameterInteger, TemplateParameterDecimal, TemplateParameterBoolean:
		default:
			return nil, fmt.Errorf("%w: unsupported parameter type for %q", ErrTemplateParameterInvalid, definition.Name)
		}
		declared[definition.Name] = definition
	}
	return declared, nil
}

func compileTemplateValue(definition TemplateParameterDefinition, value any) (any, error) {
	switch definition.Type {
	case TemplateParameterString:
		text, ok := value.(string)
		if !ok || len(text) > templateMaxValueBytes {
			return nil, fmt.Errorf("%w: invalid value for %q", ErrTemplateParameterInvalid, definition.Name)
		}
		return text, nil
	case TemplateParameterDecimal:
		text, ok := value.(string)
		if !ok || len(text) > templateMaxValueBytes || !templateDecimalPattern.MatchString(text) {
			return nil, fmt.Errorf("%w: invalid value for %q", ErrTemplateParameterInvalid, definition.Name)
		}
		return text, nil
	case TemplateParameterInteger:
		switch integer := value.(type) {
		case int64:
			return integer, nil
		case json.Number:
			parsed, err := integer.Int64()
			if err == nil {
				return parsed, nil
			}
		case float64:
			if !math.IsNaN(integer) && !math.IsInf(integer, 0) && math.Trunc(integer) == integer && integer >= -1<<63 && integer < 1<<63 {
				return int64(integer), nil
			}
		}
	case TemplateParameterBoolean:
		if boolean, ok := value.(bool); ok {
			return boolean, nil
		}
	}
	return nil, fmt.Errorf("%w: invalid value for %q", ErrTemplateParameterInvalid, definition.Name)
}

func replaceTemplateBindLocations(parsedQuery *sqlparser.ParsedQuery) string {
	locations := parsedQuery.BindLocations()
	if len(locations) == 0 {
		return parsedQuery.Query
	}
	var builder strings.Builder
	builder.Grow(len(parsedQuery.Query))
	current := 0
	for _, location := range locations {
		builder.WriteString(parsedQuery.Query[current:location.Offset])
		builder.WriteByte('?')
		current = location.Offset + location.Length
	}
	builder.WriteString(parsedQuery.Query[current:])
	return builder.String()
}
