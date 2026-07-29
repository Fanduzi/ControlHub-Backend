// Package service provides business logic for the read-only query sandbox.
// input: errors, fmt, math, strconv, strings, vitess.io/vitess/go/vt/sqlparser
// output: QueryGuardConfig, GuardedQuery, NewQueryGuard, QueryGuard.Guard, QueryGuard.GuardPaginatedSelect, QueryGuard.GuardExplainSelect, QueryGuard.GuardSavedStatement, guard sentinel errors
// pos: AST-backed MySQL/TiDB read-only guard — rejects non-read statements, side-effecting functions, locking clauses, and enforces a backend-owned LIMIT
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

// QueryGuardConfig bounds the row limit the guard enforces. DefaultMaxRows is
// used when a caller passes a zero requestedMaxRows; HardMaxRows is the absolute
// upper bound a caller can never exceed.
type QueryGuardConfig struct {
	DefaultMaxRows int
	HardMaxRows    int
}

// GuardedQuery is the validated, backend-owned form of a user SELECT. The
// executor runs ExecutableSQL; the digest/preview feed history without leaking
// literal values.
type GuardedQuery struct {
	OriginalStatement string
	ExecutableSQL     string
	LimitApplied      int
	ResultLimit       int
	StatementDigest   string
	StatementPreview  string
}

var (
	ErrQueryStatementEmpty          = errors.New("query statement is empty")
	ErrQueryStatementNotAllowed     = errors.New("only read-only SQL statements are allowed")
	ErrQueryLimitInvalid            = errors.New("query maxRows must not be negative")
	ErrQueryPaginationInvalid       = errors.New("query pagination is invalid")
	ErrQueryPaginationNotApplicable = errors.New("query pagination is not applicable")
)

// QueryGuard parses and validates MySQL/TiDB read-only statements. All
// rejections are decided from the parsed AST, never by substring matching.
type QueryGuard struct {
	config QueryGuardConfig
	parser *sqlparser.Parser
}

// NewQueryGuard constructs a guard with its own Vitess parser. The parser
// constructor only fails on a malformed MySQL version; the default (empty
// Options) is always valid, so a failure here is unreachable and fails loud.
func NewQueryGuard(config QueryGuardConfig) *QueryGuard {
	parser, err := sqlparser.New(sqlparser.Options{})
	if err != nil {
		panic(fmt.Sprintf("query guard: construct parser: %v", err))
	}
	return &QueryGuard{config: config, parser: parser}
}

// Guard validates a user statement and returns the backend-owned executable
// form. It allows a small read-only allow-list (SELECT, SHOW DATABASES, SHOW
// TABLES, SHOW COLUMNS, DESCRIBE/DESC, EXPLAIN SELECT), rejects side-effecting
// /resource/locking constructs via AST walk, and injects a backend-owned LIMIT
// for SELECT.
// Guard is the execute-route entry point. It accepts SELECT, SHOW, typed
// EXPLAIN, and DESCRIBE/DESC. Do NOT call it from the Explain route — use
// GuardExplainSelect instead, which accepts a bare SELECT only.
func (g *QueryGuard) Guard(statement string, requestedMaxRows int) (GuardedQuery, error) {
	trimmed, err := trimQueryStatement(statement)
	if err != nil {
		return GuardedQuery{}, err
	}
	if requestedMaxRows < 0 {
		return GuardedQuery{}, ErrQueryLimitInvalid
	}

	stmt, err := g.parseSingleStatement(trimmed)
	if err != nil {
		return GuardedQuery{}, err
	}

	// Dispatch on statement type. SELECT gets the full side-effect walk + LIMIT
	// injection. Allowed metadata statements pass through as-is. Everything else
	// is rejected.
	switch s := stmt.(type) {
	case *sqlparser.Select:
		return g.guardSelect(s, trimmed, requestedMaxRows)
	case *sqlparser.Show:
		return g.guardShow(s, trimmed)
	case *sqlparser.ExplainStmt:
		return g.guardExplain(s, trimmed, requestedMaxRows)
	case *sqlparser.ExplainTab:
		return g.guardExplainTab(s, trimmed)
	default:
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
}

// GuardPaginatedSelect validates a SELECT and injects an AST-owned page window.
// Metadata statements report ErrQueryPaginationNotApplicable so their caller can
// fall back to Guard without changing their single-response semantics.
func (g *QueryGuard) GuardPaginatedSelect(statement string, page, pageSize, effectiveMaxRows int) (GuardedQuery, error) {
	trimmed, err := trimQueryStatement(statement)
	if err != nil {
		return GuardedQuery{}, err
	}
	stmt, err := g.parseSingleStatement(trimmed)
	if err != nil {
		return GuardedQuery{}, err
	}

	switch s := stmt.(type) {
	case *sqlparser.Select:
		if err := g.validateSelect(s); err != nil {
			return GuardedQuery{}, err
		}
		offset, pageRows, err := paginationWindow(page, pageSize, effectiveMaxRows)
		if err != nil {
			return GuardedQuery{}, err
		}
		setPaginatedSelectLimit(s, pageRows, offset)
		return g.newGuardedQuery(trimmed, sqlparser.String(s), effectiveMaxRows, pageRows), nil
	case *sqlparser.Show, *sqlparser.ExplainStmt, *sqlparser.ExplainTab:
		return GuardedQuery{}, ErrQueryPaginationNotApplicable
	default:
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
}

// GuardExplainSelect is the narrow Explain-route entry point. It accepts a
// bare parser-approved SELECT only — never user-typed EXPLAIN, SHOW,
// DESCRIBE/DESC, DML, DDL, multi-statement, or unsafe functions. It reuses
// the same parser, multi-statement splitter, and rejectForbiddenNodes AST
// walker as Guard, but does NOT call guardShow / guardExplain /
// guardExplainTab (those are execute-route helpers that accept
// user-typed EXPLAIN/SHOW/DESCRIBE). It does NOT inject LIMIT — Explain
// must reflect the user's actual plan shape, and LIMIT would mask
// high_estimated_rows. The returned GuardedQuery.ExecutableSQL is the bare
// parser-approved SELECT; the executor owns prepending EXPLAIN FORMAT=JSON.
// This structural seam prevents the executor from ever seeing arbitrary SQL.
func (g *QueryGuard) GuardExplainSelect(statement string) (GuardedQuery, error) {
	trimmed, err := trimQueryStatement(statement)
	if err != nil {
		return GuardedQuery{}, err
	}
	stmt, err := g.parseSingleStatement(trimmed)
	if err != nil {
		return GuardedQuery{}, err
	}
	sel, ok := stmt.(*sqlparser.Select)
	if !ok {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	if err := g.validateSelect(sel); err != nil {
		return GuardedQuery{}, err
	}
	return g.newGuardedQuery(trimmed, sqlparser.String(sel), 0, 0), nil
}

// GuardSavedStatement validates a statement for saving in the query library.
// It accepts only a bare parser-approved SELECT — never SHOW, DESCRIBE/DESC,
// typed EXPLAIN, DML, DDL, multi-statements, locking clauses, or unsafe
// functions. It reuses the same parser, multi-statement splitter, and
// rejectForbiddenNodes AST walker as Guard and GuardExplainSelect, but does
// NOT inject LIMIT — saved statements store the user's original text.
//
// This is the save-route entry point. Do NOT call it from execute or explain
// routes — use Guard or GuardExplainSelect instead.
func (g *QueryGuard) GuardSavedStatement(statement string) (string, error) {
	trimmed, err := trimQueryStatement(statement)
	if err != nil {
		return "", err
	}
	stmt, err := g.parseSingleStatement(trimmed)
	if err != nil {
		return "", err
	}

	// Only bare SELECT is allowed for saved statements
	sel, ok := stmt.(*sqlparser.Select)
	if !ok {
		return "", ErrQueryStatementNotAllowed
	}

	if err := g.validateSelect(sel); err != nil {
		return "", err
	}

	return trimmed, nil
}

// guardSelect validates a SELECT statement: rejects INTO, locking clauses, and
// side-effect functions, then injects a backend-owned LIMIT.
func (g *QueryGuard) guardSelect(sel *sqlparser.Select, trimmed string, requestedMaxRows int) (GuardedQuery, error) {
	if err := g.validateSelect(sel); err != nil {
		return GuardedQuery{}, err
	}

	effective := g.effectiveMaxRows(requestedMaxRows)
	setSelectLimit(sel, effective)
	return g.newGuardedQuery(trimmed, sqlparser.String(sel), effective, 0), nil
}

// guardShow validates a SHOW statement against the read-only allow-list.
// Allowed: SHOW DATABASES, SHOW TABLES [FROM <db>], SHOW COLUMNS FROM
// [<db>.]<table>. Cross-schema qualifiers are permitted because the read-only
// credential controls actual access; the guard's job is to reject writes,
// session mutation, and privilege commands.
// Rejected: SHOW PROCESSLIST, SHOW GRANTS, and everything else.
func (g *QueryGuard) guardShow(show *sqlparser.Show, trimmed string) (GuardedQuery, error) {
	basic, ok := show.Internal.(*sqlparser.ShowBasic)
	if !ok {
		// ShowGrants, ShowCreate, ShowEngine, etc. — not in the allow-list.
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	switch basic.Command {
	case sqlparser.Database:
		// SHOW DATABASES — safe read-only metadata command.
	case sqlparser.Table:
		// SHOW TABLES [FROM <db>] — both unqualified and cross-schema forms are
		// allowed. The credential controls what schemas are actually accessible.
	case sqlparser.Column:
		// SHOW COLUMNS FROM [<db>.]<table> — both unqualified and cross-schema
		// forms are allowed. The credential controls actual schema access.
	default:
		// SHOW PROCESSLIST, SHOW VARIABLES, etc. — rejected.
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}

	return g.newGuardedQuery(trimmed, sqlparser.String(show), 0, 0), nil
}

// guardExplain validates an EXPLAIN statement. Only EXPLAIN SELECT is allowed —
// the inner statement must be a SELECT that passes the full side-effect guard.
// The final ExecutableSQL preserves the EXPLAIN wrapper so the executor runs
// EXPLAIN (returning the execution plan), not the bare SELECT (returning data).
func (g *QueryGuard) guardExplain(explain *sqlparser.ExplainStmt, trimmed string, requestedMaxRows int) (GuardedQuery, error) {
	innerSelect, ok := explain.Statement.(*sqlparser.Select)
	if !ok {
		// EXPLAIN of non-SELECT (e.g. EXPLAIN UPDATE) — rejected.
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	// Validate the inner SELECT through the side-effect guard. We do NOT call
	// guardSelect because that would strip the EXPLAIN wrapper.
	if err := g.validateSelect(innerSelect); err != nil {
		return GuardedQuery{}, err
	}

	// Apply backend-owned LIMIT to the inner SELECT.
	effective := g.effectiveMaxRows(requestedMaxRows)
	setSelectLimit(innerSelect, effective)

	// Rebuild the EXPLAIN wrapper with the guarded inner SELECT.
	explain.Statement = innerSelect

	return g.newGuardedQuery(trimmed, sqlparser.String(explain), effective, 0), nil
}

// guardExplainTab validates a DESCRIBE/DESC <table> statement. These are safe
// read-only metadata commands. Cross-schema qualifiers (e.g. DESCRIBE
// db.table) are allowed because the read-only credential controls actual access;
// the guard's job is to reject writes and session mutation, not to second-guess
// schema visibility.
func (g *QueryGuard) guardExplainTab(tab *sqlparser.ExplainTab, trimmed string) (GuardedQuery, error) {
	return g.newGuardedQuery(trimmed, sqlparser.String(tab), 0, 0), nil
}

func trimQueryStatement(statement string) (string, error) {
	trimmed := strings.TrimSpace(statement)
	if trimmed == "" {
		return "", ErrQueryStatementEmpty
	}
	return trimmed, nil
}

func (g *QueryGuard) parseSingleStatement(statement string) (sqlparser.Statement, error) {
	if multi, err := g.isMultiStatement(statement); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrQueryStatementNotAllowed, err)
	} else if multi {
		return nil, ErrQueryStatementNotAllowed
	}
	stmt, err := g.parser.Parse(statement)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrQueryStatementNotAllowed, err)
	}
	return stmt, nil
}

func (g *QueryGuard) validateSelect(sel *sqlparser.Select) error {
	if sel.Into != nil {
		return ErrQueryStatementNotAllowed
	}
	if sel.Lock != sqlparser.NoLock {
		return ErrQueryStatementNotAllowed
	}
	return g.rejectForbiddenNodes(sel)
}

func (g *QueryGuard) effectiveMaxRows(requestedMaxRows int) int {
	effective := requestedMaxRows
	if effective == 0 {
		effective = g.config.DefaultMaxRows
	}
	if effective > g.config.HardMaxRows {
		effective = g.config.HardMaxRows
	}
	return effective
}

func setSelectLimit(sel *sqlparser.Select, limit int) {
	sel.Limit = &sqlparser.Limit{
		Rowcount: &sqlparser.Literal{Type: sqlparser.IntVal, Val: strconv.Itoa(limit + 1)},
	}
}

func setPaginatedSelectLimit(sel *sqlparser.Select, pageRows, offset int) {
	sel.Limit = &sqlparser.Limit{
		Offset:   &sqlparser.Literal{Type: sqlparser.IntVal, Val: strconv.Itoa(offset)},
		Rowcount: &sqlparser.Literal{Type: sqlparser.IntVal, Val: strconv.Itoa(pageRows + 1)},
	}
}

func paginationWindow(page, pageSize, effectiveMaxRows int) (offset, pageRows int, err error) {
	if page < 1 || pageSize < 1 || effectiveMaxRows < 1 {
		return 0, 0, ErrQueryPaginationInvalid
	}
	pageIndex := page - 1
	if pageIndex > math.MaxInt/pageSize {
		return 0, 0, ErrQueryPaginationInvalid
	}
	offset = pageIndex * pageSize
	remaining := effectiveMaxRows - offset
	if remaining <= 0 {
		return 0, 0, ErrQueryPaginationInvalid
	}
	pageRows = min(pageSize, remaining)
	if pageRows == math.MaxInt {
		return 0, 0, ErrQueryPaginationInvalid
	}
	return offset, pageRows, nil
}

func (g *QueryGuard) newGuardedQuery(trimmed, executableSQL string, limitApplied, resultLimit int) GuardedQuery {
	preview := trimmed
	if len(preview) > statementPreviewMax {
		preview = preview[:statementPreviewMax]
	}
	return GuardedQuery{
		OriginalStatement: trimmed,
		ExecutableSQL:     executableSQL,
		LimitApplied:      limitApplied,
		ResultLimit:       resultLimit,
		StatementDigest:   g.digest(trimmed),
		StatementPreview:  preview,
	}
}

const statementPreviewMax = 512

// forbiddenFunctions are SELECT-callable functions that consume resources
// (SLEEP, BENCHMARK) or read server files (LOAD_FILE). Named-lock advisory
// functions are rejected separately via the *LockingFunc node.
var forbiddenFunctions = map[string]struct{}{
	"SLEEP":     {},
	"BENCHMARK": {},
	"LOAD_FILE": {},
}

// rejectForbiddenNodes walks the SELECT and returns a wrapped
// ErrQueryStatementNotAllowed on the first forbidden construct. It inspects
// *FuncExpr (resource/file functions), *LockingFunc (advisory named locks:
// GET_LOCK/RELEASE_LOCK/IS_FREE_LOCK/IS_USED_LOCK), and *AssignmentExpr
// (@var := ... user-variable assignment).
func (g *QueryGuard) rejectForbiddenNodes(sel *sqlparser.Select) error {
	err := sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		switch n := node.(type) {
		case *sqlparser.FuncExpr:
			if _, bad := forbiddenFunctions[strings.ToUpper(n.Name.String())]; bad {
				return false, fmt.Errorf("%w: function %s is not allowed", ErrQueryStatementNotAllowed, n.Name.String())
			}
		case *sqlparser.LockingFunc:
			return false, fmt.Errorf("%w: advisory named-lock function is not allowed", ErrQueryStatementNotAllowed)
		case *sqlparser.AssignmentExpr:
			return false, fmt.Errorf("%w: user variable assignment is not allowed", ErrQueryStatementNotAllowed)
		}
		return true, nil
	}, sel)
	return err
}

// isMultiStatement reports whether the input contains more than one real
// statement. It uses the parser's SQL-aware splitter so semicolons inside
// string literals are not miscounted.
func (g *QueryGuard) isMultiStatement(statement string) (bool, error) {
	pieces, err := g.parser.SplitStatementToPieces(statement)
	if err != nil {
		return false, err
	}
	count := 0
	for _, p := range pieces {
		if strings.TrimSpace(p) != "" {
			count++
		}
	}
	return count > 1, nil
}

// digest returns the statement shape with every literal masked as "?" so
// history records the query pattern, not parameter values. It parses a fresh
// copy of the original statement (before LIMIT injection) so the digest reflects
// the user's query, then mutates literal leaf nodes and re-serializes.
func (g *QueryGuard) digest(statement string) string {
	digestStmt, err := g.parser.Parse(statement)
	if err != nil {
		// Already parsed successfully in Guard; fall back to the trimmed text.
		return statement
	}
	_ = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
		if lit, ok := node.(*sqlparser.Literal); ok {
			lit.Type = sqlparser.IntVal
			lit.Val = "?"
		}
		return true, nil
	}, digestStmt)
	return sqlparser.String(digestStmt)
}
