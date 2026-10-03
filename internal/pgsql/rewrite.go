// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: user SQL statement, pinned schema, ColumnResolver
// output: Rewrite, RewriteResult — executable transport SQL + witness records + public output names + entity refs + qualified text
// pos: the T4 transform pipeline — guard original text, qualify by byte insertion, reparse, witness-inject, deparse once to internal transport, then reparse-verify the deparsed form before it may reach a connection; deparsed SQL is never persisted or displayed
// note: if this file changes, update header and README.md
package pgsql

import (
	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

// RewriteResult is the full pipeline output.
type RewriteResult struct {
	SQL       string          // executable transport SQL (deparsed)
	Witnesses []WitnessRecord // top-level witness columns to verify + strip
	Public    []string        // user-visible output column names
	Refs      []RangeVarRef   // entity references for the binding gate
	Qualified string          // canonical schema-qualified text (pre-injection)
}

// Rewrite runs guard → canonical qualification → witness injection → deparse.
// resolve supplies relation column metadata for layout freezing; pass nil for
// statements that provably need none (a nil resolver still rejects freezes
// that need names, loudly).
func Rewrite(statement, pinnedSchema string, res ColumnResolver) (*RewriteResult, error) {
	gr, err := GuardPG(statement)
	if err != nil {
		return nil, err
	}
	qualified, err := QualifyEntities(statement, pinnedSchema, gr.Refs)
	if err != nil {
		return nil, err
	}
	// Reparse the qualified text — the byte rewrite must produce compilable
	// SQL as written, and injection operates on its fresh tree.
	reTree, err := pgquery.Parse(qualified)
	if err != nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: "pgsql: qualified rewrite failed to reparse"}
	}
	inj, err := InjectWitnesses(reTree, res)
	if err != nil {
		return nil, err
	}
	deparsed, err := deparseTree(inj.Tree)
	if err != nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: "pgsql: injected rewrite failed to deparse"}
	}
	// The transport text is generated SQL — verify it reparses before it can
	// reach a connection, so a deparse defect fails closed here, not on the
	// server. The round-trip tree is discarded; positions/records come from
	// the injected AST.
	verify, err := pgquery.Parse(deparsed)
	if err != nil || len(verify.GetStmts()) != 1 {
		return nil, &RejectError{Code: "query_not_allowed", Message: "pgsql: injected rewrite failed to reparse"}
	}
	return &RewriteResult{
		SQL:       deparsed,
		Witnesses: inj.Witnesses,
		Public:    inj.Public,
		Refs:      gr.Refs,
		Qualified: qualified,
	}, nil
}

// deparseTree deparses a (possibly injected) parse tree to SQL text.
func deparseTree(tree *pg.ParseResult) (string, error) {
	return pgquery.Deparse(tree)
}
