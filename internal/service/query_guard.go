// Package service provides business logic for the read-only query sandbox.
// input: errors, fmt, strconv, strings, vitess.io/vitess/go/vt/sqlparser
// output: QueryGuardConfig, GuardedQuery, NewQueryGuard, QueryGuard.Guard, guard sentinel errors
// pos: AST-backed MySQL/TiDB read-only guard — rejects non-read statements, side-effecting functions, locking clauses, and enforces a backend-owned LIMIT
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"fmt"
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
	StatementDigest   string
	StatementPreview  string
}

var (
	ErrQueryStatementEmpty      = errors.New("query statement is empty")
	ErrQueryStatementNotAllowed = errors.New("only read-only SQL statements are allowed")
	ErrQueryLimitInvalid        = errors.New("query maxRows must not be negative")
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
	trimmed := strings.TrimSpace(statement)
	if trimmed == "" {
		return GuardedQuery{}, ErrQueryStatementEmpty
	}
	if requestedMaxRows < 0 {
		return GuardedQuery{}, ErrQueryLimitInvalid
	}

	// Multi-statement rejection: split into real statements and require exactly
	// one. This catches the classic "; drop table" amplifier before parsing.
	if multi, err := g.isMultiStatement(trimmed); err != nil {
		return GuardedQuery{}, fmt.Errorf("%w: %v", ErrQueryStatementNotAllowed, err)
	} else if multi {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}

	stmt, err := g.parser.Parse(trimmed)
	if err != nil {
		return GuardedQuery{}, fmt.Errorf("%w: %v", ErrQueryStatementNotAllowed, err)
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
	trimmed := strings.TrimSpace(statement)
	if trimmed == "" {
		return GuardedQuery{}, ErrQueryStatementEmpty
	}
	if multi, err := g.isMultiStatement(trimmed); err != nil {
		return GuardedQuery{}, fmt.Errorf("%w: %v", ErrQueryStatementNotAllowed, err)
	} else if multi {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	stmt, err := g.parser.Parse(trimmed)
	if err != nil {
		return GuardedQuery{}, fmt.Errorf("%w: %v", ErrQueryStatementNotAllowed, err)
	}
	sel, ok := stmt.(*sqlparser.Select)
	if !ok {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	if sel.Into != nil {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	if sel.Lock != sqlparser.NoLock {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	if err := g.rejectForbiddenNodes(sel); err != nil {
		return GuardedQuery{}, err
	}
	preview := trimmed
	if len(preview) > statementPreviewMax {
		preview = preview[:statementPreviewMax]
	}
	return GuardedQuery{
		OriginalStatement: trimmed,
		ExecutableSQL:     sqlparser.String(sel),
		LimitApplied:      0,
		StatementDigest:   g.digest(trimmed),
		StatementPreview:  preview,
	}, nil
}

// guardSelect validates a SELECT statement: rejects INTO, locking clauses, and
// side-effect functions, then injects a backend-owned LIMIT.
func (g *QueryGuard) guardSelect(sel *sqlparser.Select, trimmed string, requestedMaxRows int) (GuardedQuery, error) {
	// INTO (OUTFILE / DUMPFILE / S3 / @var) — any INTO is rejected. INTO OUTFILE
	// and DUMPFILE write server-side files; INTO @var carries variable state.
	if sel.Into != nil {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	// Locking clauses (FOR UPDATE / LOCK IN SHARE MODE and their NOWAIT/SKIP
	// LOCKED variants) — not read-only.
	if sel.Lock != sqlparser.NoLock {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}

	// Walk the parsed tree for side-effecting / resource / lock functions and
	// user-variable assignment. Decided by node type, never substring match.
	if err := g.rejectForbiddenNodes(sel); err != nil {
		return GuardedQuery{}, err
	}

	// Backend-owned LIMIT: zero/omitted requestedMaxRows -> DefaultMaxRows,
	// capped at HardMaxRows. Overwrites any LIMIT the user supplied so the cap
	// is authoritative. The SQL LIMIT is effective+1 so the executor can scan one
	// extra row to detect truncation; limitApplied reports the real cap
	// (effective), not the +1.
	effective := requestedMaxRows
	if effective == 0 {
		effective = g.config.DefaultMaxRows
	}
	if effective > g.config.HardMaxRows {
		effective = g.config.HardMaxRows
	}
	sel.Limit = &sqlparser.Limit{
		Rowcount: &sqlparser.Literal{Type: sqlparser.IntVal, Val: strconv.Itoa(effective + 1)},
	}

	preview := trimmed
	if len(preview) > statementPreviewMax {
		preview = preview[:statementPreviewMax]
	}

	return GuardedQuery{
		OriginalStatement: trimmed,
		ExecutableSQL:     sqlparser.String(sel),
		LimitApplied:      effective,
		StatementDigest:   g.digest(trimmed),
		StatementPreview:  preview,
	}, nil
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

	preview := trimmed
	if len(preview) > statementPreviewMax {
		preview = preview[:statementPreviewMax]
	}

	return GuardedQuery{
		OriginalStatement: trimmed,
		ExecutableSQL:     sqlparser.String(show),
		LimitApplied:      0, // SHOW statements have no row cap
		StatementDigest:   g.digest(trimmed),
		StatementPreview:  preview,
	}, nil
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
	// Validate the inner SELECT through the side-effect guard (INTO, locking,
	// forbidden functions). We do NOT call guardSelect because that would
	// overwrite ExecutableSQL with just the SELECT + LIMIT, stripping EXPLAIN.
	if innerSelect.Into != nil {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	if innerSelect.Lock != sqlparser.NoLock {
		return GuardedQuery{}, ErrQueryStatementNotAllowed
	}
	if err := g.rejectForbiddenNodes(innerSelect); err != nil {
		return GuardedQuery{}, err
	}

	// Apply backend-owned LIMIT to the inner SELECT.
	effective := requestedMaxRows
	if effective == 0 {
		effective = g.config.DefaultMaxRows
	}
	if effective > g.config.HardMaxRows {
		effective = g.config.HardMaxRows
	}
	innerSelect.Limit = &sqlparser.Limit{
		Rowcount: &sqlparser.Literal{Type: sqlparser.IntVal, Val: strconv.Itoa(effective + 1)},
	}

	// Rebuild the EXPLAIN wrapper with the guarded inner SELECT.
	explain.Statement = innerSelect

	preview := trimmed
	if len(preview) > statementPreviewMax {
		preview = preview[:statementPreviewMax]
	}

	return GuardedQuery{
		OriginalStatement: trimmed,
		ExecutableSQL:     sqlparser.String(explain),
		LimitApplied:      effective,
		StatementDigest:   g.digest(trimmed),
		StatementPreview:  preview,
	}, nil
}

// guardExplainTab validates a DESCRIBE/DESC <table> statement. These are safe
// read-only metadata commands. Cross-schema qualifiers (e.g. DESCRIBE
// db.table) are allowed because the read-only credential controls actual access;
// the guard's job is to reject writes and session mutation, not to second-guess
// schema visibility.
func (g *QueryGuard) guardExplainTab(tab *sqlparser.ExplainTab, trimmed string) (GuardedQuery, error) {
	preview := trimmed
	if len(preview) > statementPreviewMax {
		preview = preview[:statementPreviewMax]
	}

	return GuardedQuery{
		OriginalStatement: trimmed,
		ExecutableSQL:     trimmed,
		LimitApplied:      0, // DESCRIBE statements have no row cap
		StatementDigest:   g.digest(trimmed),
		StatementPreview:  preview,
	}, nil
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