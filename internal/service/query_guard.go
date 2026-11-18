// Package service provides business logic for the read-only query sandbox.
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
	// Paginated marks statements guarded by GuardPaginatedSelect so a zero
	// ResultLimit (e.g. user LIMIT 0) still reports an empty page window.
	Paginated        bool
	StatementDigest  string
	StatementPreview string
}

var (
	ErrQueryStatementEmpty          = errors.New("query statement is empty")
	ErrQueryStatementNotAllowed     = errors.New("only read-only SQL statements are allowed")
	ErrQueryLimitInvalid            = errors.New("query maxRows must not be negative")
	ErrQueryLimitFormUnsupported    = errors.New("only literal integer LIMIT/OFFSET values are supported")
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
// /resource/locking constructs via AST walk, and bounds SELECT row release:
// the user's own literal LIMIT/OFFSET is honored and intersected with the
// governed cap rather than overridden.
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
	// window. Allowed metadata statements pass through as-is. Everything else
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
func (g *QueryGuard) GuardPaginatedSelect(statement string, page, pageSize, requestedMaxRows int) (GuardedQuery, error) {
	trimmed, err := trimQueryStatement(statement)
	if err != nil {
		return GuardedQuery{}, err
	}
	// Same limit validation as Guard: a negative cap is a limit error, never an
	// invalid pagination window.
	if requestedMaxRows < 0 {
		return GuardedQuery{}, ErrQueryLimitInvalid
	}
	stmt, err := g.parseSingleStatement(trimmed)
	if err != nil {
		return GuardedQuery{}, err
	}
	effectiveMaxRows := g.effectiveMaxRows(requestedMaxRows)

	switch s := stmt.(type) {
	case *sqlparser.Select:
		if err := g.validateSelect(s); err != nil {
			return GuardedQuery{}, err
		}
		browsable, userOffset, err := browsableRows(s, effectiveMaxRows)
		if err != nil {
			return GuardedQuery{}, err
		}
		if page < 1 || pageSize < 1 {
			return GuardedQuery{}, ErrQueryPaginationInvalid
		}
		pageStart, ok := checkedMulInt64(int64(page-1), int64(pageSize))
		if !ok {
			return GuardedQuery{}, ErrQueryPaginationInvalid
		}
		if pageStart >= browsable && !(browsable == 0 && page == 1) {
			return GuardedQuery{}, ErrQueryPaginationInvalid
		}
		fetchOffset, ok := checkedAddInt64(userOffset, pageStart)
		if !ok {
			return GuardedQuery{}, ErrQueryPaginationInvalid
		}
		remaining := browsable - pageStart
		resultLimit := min(int64(pageSize), remaining)
		fetchCount := min(int64(pageSize)+1, remaining)
		setSelectLimitWindow(s, fetchCount, fetchOffset)
		guarded := g.newGuardedQuery(trimmed, sqlparser.String(s), int(browsable), int(resultLimit))
		guarded.Paginated = true
		return guarded, nil
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

	// A stored statement must not smuggle a non-literal LIMIT/OFFSET past the
	// execution guard later (template placeholders, expressions) — the same
	// rule the PG save path enforces via the pagination gate.
	if _, _, err := browsableRows(sel, 1); err != nil {
		return "", err
	}

	return trimmed, nil
}

// guardSelect validates a SELECT statement: rejects INTO, locking clauses, and
// side-effect functions, then bounds the release window by the user's own
// LIMIT intersected with the governed cap.
func (g *QueryGuard) guardSelect(sel *sqlparser.Select, trimmed string, requestedMaxRows int) (GuardedQuery, error) {
	if err := g.validateSelect(sel); err != nil {
		return GuardedQuery{}, err
	}

	effective := g.effectiveMaxRows(requestedMaxRows)
	browsable, userOffset, fetch, err := governedFetch(sel, effective)
	if err != nil {
		return GuardedQuery{}, err
	}
	setSelectLimitWindow(sel, fetch, userOffset)
	return g.newGuardedQuery(trimmed, sqlparser.String(sel), int(browsable), 0), nil
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

	// Apply the governed LIMIT window to the inner SELECT: the user's own
	// LIMIT/OFFSET is honored and intersected with the cap.
	effective := g.effectiveMaxRows(requestedMaxRows)
	browsable, userOffset, fetch, err := governedFetch(innerSelect, effective)
	if err != nil {
		return GuardedQuery{}, err
	}
	setSelectLimitWindow(innerSelect, fetch, userOffset)

	// Rebuild the EXPLAIN wrapper with the guarded inner SELECT.
	explain.Statement = innerSelect

	return g.newGuardedQuery(trimmed, sqlparser.String(explain), int(browsable), 0), nil
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

// browsableRows resolves the total row window the user may browse: the
// governed cap intersected with the user's own literal LIMIT. It returns the
// browsable bound and the user's own OFFSET (0 when absent). Non-literal
// LIMIT/OFFSET forms (template placeholders, expressions) are rejected so they
// cannot smuggle an unbounded window past the guard.
func browsableRows(sel *sqlparser.Select, effectiveMaxRows int) (browsable, userOffset int64, err error) {
	limit := sel.Limit
	if limit == nil || limit.Rowcount == nil {
		return int64(effectiveMaxRows), 0, nil
	}
	userLimit, err := literalLimitValue(limit.Rowcount)
	if err != nil {
		return 0, 0, err
	}
	userOffset = int64(0)
	if limit.Offset != nil {
		userOffset, err = literalLimitValue(limit.Offset)
		if err != nil {
			return 0, 0, err
		}
	}
	browsable = int64(effectiveMaxRows)
	if userLimit < browsable {
		browsable = userLimit
	}
	return browsable, userOffset, nil
}

// governedFetch resolves the executable fetch window for a non-paginated
// guarded statement: browsable rows plus one probe row when the governed cap
// is the binding bound so the executor can flag truncation; exactly the user's
// own LIMIT when it is tighter so it is never reported truncated.
func governedFetch(sel *sqlparser.Select, effectiveMaxRows int) (browsable, userOffset, fetch int64, err error) {
	browsable, userOffset, err = browsableRows(sel, effectiveMaxRows)
	if err != nil {
		return 0, 0, 0, err
	}
	fetch = browsable + 1
	if sel.Limit != nil && sel.Limit.Rowcount != nil {
		if userLimit, lerr := literalLimitValue(sel.Limit.Rowcount); lerr == nil && userLimit <= int64(effectiveMaxRows) {
			fetch = userLimit
		}
	}
	return browsable, userOffset, fetch, nil
}

func literalLimitValue(expr sqlparser.Expr) (int64, error) {
	lit, ok := expr.(*sqlparser.Literal)
	if !ok || lit.Type != sqlparser.IntVal {
		return 0, ErrQueryLimitFormUnsupported
	}
	v, err := strconv.ParseUint(lit.Val, 10, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return math.MaxInt64, nil
		}
		return 0, ErrQueryLimitFormUnsupported
	}
	if v > math.MaxInt64 {
		return math.MaxInt64, nil
	}
	return int64(v), nil
}

// setSelectLimitWindow rewrites the executable LIMIT to the governed fetch
// window: `LIMIT fetchOffset, fetchCount`, with the offset literal omitted
// when it is 0.
func setSelectLimitWindow(sel *sqlparser.Select, fetchCount, fetchOffset int64) {
	sel.Limit = &sqlparser.Limit{
		Rowcount: &sqlparser.Literal{Type: sqlparser.IntVal, Val: strconv.FormatInt(fetchCount, 10)},
	}
	if fetchOffset > 0 {
		sel.Limit.Offset = &sqlparser.Literal{Type: sqlparser.IntVal, Val: strconv.FormatInt(fetchOffset, 10)}
	}
}

func checkedMulInt64(a, b int64) (int64, bool) {
	if a != 0 && b != 0 && (a > math.MaxInt64/b || a < math.MinInt64/b) {
		return 0, false
	}
	return a * b, true
}

func checkedAddInt64(a, b int64) (int64, bool) {
	if (b > 0 && a > math.MaxInt64-b) || (b < 0 && a < math.MinInt64-b) {
		return 0, false
	}
	return a + b, true
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
