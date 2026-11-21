package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	ast "github.com/AfterShip/clickhouse-sql-parser/parser"
	"github.com/fan/controlhub/internal/model"
)

func (s *QueryExecutionService) executeCHSavedQuery(ctx context.Context, identity model.QueryExecutionIdentity, target model.QueryTarget, statementID uint64, req model.QuerySavedStatementExecuteRequest) (model.QueryExecuteResponse, error) {
	worksheet := model.QueryExecuteRequest{
		ClickHouseGeneration: req.ClickHouseGeneration,
		Database:             req.ScopeDatabase,
		ClientExecutionID:    req.ClientExecutionID,
		MaxRows:              req.MaxRows,
		Pagination:           req.Pagination,
	}
	return s.clickHouse.runWorksheet(ctx, identity, target, worksheet, func(attempt context.Context) (string, *chTemplate, error) {
		stmt, err := s.statements.GetByID(attempt, target.ResourceID, statementID)
		if err != nil {
			switch {
			case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
				return "", nil, err
			case errors.Is(err, sql.ErrNoRows):
				return "", nil, ErrQuerySavedStatementNotFound
			default:
				return "", nil, ErrQueryBackendFailure
			}
		}
		if stmt.ID != statementID || stmt.TargetResourceID != target.ResourceID || stmt.Scope != model.QuerySavedStatementPersonal || stmt.OwnerUserID != identity.ID || stmt.DatabaseName != "" || stmt.SchemaName != "" {
			return "", nil, ErrQuerySavedStatementNotFound
		}
		if len(stmt.Parameters) == 0 && len(req.Values) == 0 {
			return stmt.Statement, nil, nil
		}
		return prepareCHTemplate(stmt.Statement, stmt.Parameters, req.Values, false)
	})
}

// Rewrite only known lexical tokens. Quoted text and comments are consumed as
// whole tokens; the dialect AST must place every generated marker in a predicate.
var chTemplateTokens = regexp.MustCompile("(?s)'(?:[^'\\\\]|\\\\.|'')*'|\"[^\"]*\"|`[^`]*`|/\\*.*?\\*/|--[^\\r\\n]*|:[A-Za-z_][A-Za-z0-9_]*|['\"`{}]|/\\*")

type chTemplate struct {
	markers map[ast.Pos]model.QuerySavedStatementParameterDefinition
	used    int
	values  map[string]any // nil when validating declarations on save
}

func prepareCHTemplate(statement string, definitions []model.QuerySavedStatementParameterDefinition, raw map[string]json.RawMessage, validateOnly bool) (string, *chTemplate, error) {
	if len(statement) > chWorksheetMaxSQLBytes || len(definitions) > model.MaxSavedStatementParameters {
		return "", nil, ErrQueryValidationFailed
	}
	decl := make(map[string]model.QuerySavedStatementParameterDefinition, len(definitions))
	typed := make([]TemplateParameterDefinition, 0, len(definitions))
	for _, d := range definitions {
		if d.Type != model.QuerySavedStatementParameterString && d.Type != model.QuerySavedStatementParameterInteger {
			return "", nil, ErrQueryValidationFailed
		}
		if _, dup := decl[d.Name]; dup || !templateParameterNamePattern.MatchString(d.Name) || len(d.Name) > model.MaxSavedStatementParameterNameLength {
			return "", nil, ErrQueryValidationFailed
		}
		decl[d.Name] = d
		typed = append(typed, TemplateParameterDefinition{Name: d.Name, Type: TemplateParameterType(d.Type)})
	}
	t := &chTemplate{markers: map[ast.Pos]model.QuerySavedStatementParameterDefinition{}}
	var b strings.Builder
	end := 0
	seen := map[string]bool{}
	for _, m := range chTemplateTokens.FindAllStringIndex(statement, -1) {
		token := statement[m[0]:m[1]]
		switch token[0] {
		case '{', '}':
			return "", nil, ErrQueryValidationFailed
		case '\'', '"', '`':
			if len(token) == 1 {
				return "", nil, ErrQueryValidationFailed
			}
		case '/':
			if token == "/*" || strings.Contains(token[2:], "/*") {
				return "", nil, ErrQueryValidationFailed
			}
		case ':':
			name := token[1:]
			d, ok := decl[name]
			if !ok || seen[name] {
				return "", nil, ErrQueryValidationFailed
			}
			seen[name] = true
			b.WriteString(statement[end:m[0]])
			t.markers[ast.Pos(b.Len())] = d
			b.WriteString("{" + name + ":String}")
			end = m[1]
		}
	}
	if len(seen) != len(decl) {
		return "", nil, ErrQueryValidationFailed
	}
	b.WriteString(statement[end:])
	if validateOnly {
		return b.String(), t, nil
	}
	// Unknown input keys are not trusted field names and must not be echoed.
	for name := range raw {
		if _, ok := decl[name]; !ok {
			return "", nil, ErrQueryValidationFailed
		}
	}
	values, fields := validateTemplateValues(typed, raw)
	if len(fields) > 0 {
		return "", nil, &TemplateValueValidationError{Fields: fields}
	}
	t.values = values
	return b.String(), t, nil
}

func (t *chTemplate) bind(marker *ast.QueryParam, name, typ string) (string, string, error) {
	if t == nil {
		return "", "", ErrQueryValidationFailed
	}
	d, ok := t.markers[marker.Pos()]
	if !ok || marker.Name.Name != d.Name {
		return "", "", ErrQueryValidationFailed
	}
	t.used++
	numeric := typ == "UInt32" || typ == "UInt64"
	if numeric != (d.Type == model.QuerySavedStatementParameterInteger) {
		return "", "", ErrQueryValidationFailed
	}
	// Representative values exercise the same column-type validation on save.
	value := "placeholder"
	switch {
	case numeric:
		value = "0"
	case typ == "Date":
		value = "2000-01-01"
	case chTimestamp(typ):
		value = "2000-01-01T00:00:00Z"
	}
	if t.values != nil {
		if numeric {
			value = strconv.FormatInt(t.values[d.Name].(int64), 10)
		} else {
			value = t.values[d.Name].(string)
		}
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", "", &TemplateValueValidationError{Fields: map[string]string{d.Name: "invalid"}}
	}
	param, encoded, err := chFilterValue(name, typ, value, !numeric)
	if err != nil {
		return "", "", &TemplateValueValidationError{Fields: map[string]string{d.Name: "invalid"}}
	}
	return param, encoded, nil
}
