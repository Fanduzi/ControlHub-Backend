// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: user SQL statement or matching *GuardResult, pinned schema, ColumnResolver, optional PageOptions (RewritePaginated/RewriteFromGuardPaginated)
// output: Rewrite, RewritePaginated, RewriteFromGuardPaginated, RewriteResult — executable transport SQL + witness records + public output names/proofs + original entity refs + qualified text (+ PageWindow when paginated)
// pos: the T4 transform pipeline — guard original text, qualify by byte insertion, reparse, G6-paginate the root window (RewritePaginated only), witness-inject, deparse once to internal transport, then reparse-verify the deparsed form before it may reach a connection; deparsed SQL is never persisted or displayed
// note: if this file changes, update header and README.md
package pgsql

import (
	"bytes"
	"fmt"

	pg "github.com/pganalyze/pg_query_go/v6"
	pgquery "github.com/wasilibs/go-pgquery"
)

// RewriteResult is the full pipeline output.
type RewriteResult struct {
	SQL              string              // executable transport SQL (deparsed)
	Witnesses        []WitnessRecord     // top-level witness columns to verify + strip
	WitnessPositions []int               // final transport indexes carrying witnesses
	Public           []string            // user-visible output column names
	PublicProofs     []PublicColumnProof // PublicProofs[i] describes Public[i]
	SourceCatalog    map[SourceOccurrenceID]SourceOccurrence
	Refs             []RangeVarRef // original entity + CTE references for the binding gate
	Qualified        string        // canonical schema-qualified text (pre-injection)
}

// Rewrite runs guard → canonical qualification → witness injection → deparse.
// resolve supplies relation column metadata for layout freezing; pass nil for
// statements that provably need none (a nil resolver still rejects freezes
// that need names, loudly).
func Rewrite(statement, pinnedSchema string, res ColumnResolver) (*RewriteResult, error) {
	r, _, err := rewrite(statement, pinnedSchema, res, nil)
	return r, err
}

func RewritePaginated(statement, pinnedSchema string, res ColumnResolver, opts PageOptions) (*RewriteResult, PageWindow, error) {
	return rewrite(statement, pinnedSchema, res, &opts)
}

// RewriteFromGuardPaginated reuses a GuardPG result produced for exactly the
// same statement. It is the thin cross-package seam the future T7 executor
// uses after its binding gate has inspected the original RefID set; callers
// cannot pair a GuardResult with different text or a mutated tree.
func RewriteFromGuardPaginated(statement, pinnedSchema string, gr *GuardResult, res ColumnResolver, opts PageOptions) (*RewriteResult, PageWindow, error) {
	return rewriteFromGuard(statement, pinnedSchema, gr, res, &opts)
}

func rewrite(statement, pinnedSchema string, res ColumnResolver, opts *PageOptions) (*RewriteResult, PageWindow, error) {
	gr, err := GuardPG(statement)
	if err != nil {
		return nil, PageWindow{}, err
	}
	return rewriteFromGuard(statement, pinnedSchema, gr, res, opts)
}

func rewriteFromGuard(statement, pinnedSchema string, gr *GuardResult, res ColumnResolver, opts *PageOptions) (*RewriteResult, PageWindow, error) {
	if err := validateGuardResult(statement, gr); err != nil {
		return nil, PageWindow{}, err
	}
	qualified, err := QualifyEntities(statement, pinnedSchema, gr.Refs)
	if err != nil {
		return nil, PageWindow{}, err
	}
	// Reparse the qualified text — the byte rewrite must produce compilable
	// SQL as written, and injection operates on its fresh tree.
	reTree, err := pgquery.Parse(qualified)
	if err != nil {
		return nil, PageWindow{}, &RejectError{Code: "query_not_allowed", Message: "pgsql: qualified rewrite failed to reparse"}
	}
	byNode, err := mapCanonicalRefs(gr.Refs, reTree, pinnedSchema)
	if err != nil {
		return nil, PageWindow{}, err
	}
	var win PageWindow
	if opts != nil {
		win, err = PaginatePG(reTree, *opts)
		if err != nil {
			return nil, PageWindow{}, err
		}
	}
	inj, err := injectWitnesses(reTree, res, gr.Refs, byNode)
	if err != nil {
		return nil, PageWindow{}, err
	}
	deparsed, err := deparseTree(inj.Tree)
	if err != nil {
		return nil, PageWindow{}, &RejectError{Code: "query_not_allowed", Message: "pgsql: injected rewrite failed to deparse"}
	}
	// The transport text is generated SQL — verify it reparses before it can
	// reach a connection, so a deparse defect fails closed here, not on the
	// server. The round-trip tree is discarded; positions/records come from
	// the injected AST.
	verify, err := pgquery.Parse(deparsed)
	if err != nil || len(verify.GetStmts()) != 1 {
		return nil, PageWindow{}, &RejectError{Code: "query_not_allowed", Message: "pgsql: injected rewrite failed to reparse"}
	}
	return &RewriteResult{
		SQL:              deparsed,
		Witnesses:        inj.Witnesses,
		WitnessPositions: inj.WitnessPositions,
		Public:           inj.Public,
		PublicProofs:     inj.PublicProofs,
		SourceCatalog:    inj.SourceCatalog,
		Refs:             gr.Refs,
		Qualified:        qualified,
	}, win, nil
}

// validateGuardResult rejects a GuardResult that was not produced by GuardPG
// for this exact statement/tree pair, or whose exported fields were changed.
func validateGuardResult(statement string, gr *GuardResult) error {
	if gr == nil || gr.statement == "" || gr.statement != statement {
		return fmt.Errorf("%w: statement and GuardResult come from different analyses", ErrEvidenceUnavailable)
	}
	fp, err := guardFingerprint(gr.statement, gr.Tree)
	if err != nil {
		return err
	}
	if !bytes.Equal(fp[:], gr.fingerprint[:]) {
		return fmt.Errorf("%w: GuardResult tree does not match its analysis", ErrEvidenceUnavailable)
	}
	if len(gr.Tree.GetStmts()) != 1 || gr.Stmt == nil || gr.Tree.GetStmts()[0].GetStmt().GetSelectStmt() != gr.Stmt {
		return fmt.Errorf("%w: GuardResult statement pointer is inconsistent", ErrEvidenceUnavailable)
	}
	actual, _, err := collectRangeVarRefs(gr.Tree)
	if err != nil {
		return err
	}
	if !rangeVarRefsEqual(actual, gr.Refs) {
		return fmt.Errorf("%w: GuardResult refs do not match its original tree", ErrEvidenceUnavailable)
	}
	return nil
}

func rangeVarRefsEqual(a, b []RangeVarRef) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].RefID != b[i].RefID || a[i].Kind != b[i].Kind || a[i].Name != b[i].Name ||
			a[i].Schema != b[i].Schema || a[i].Catalog != b[i].Catalog || a[i].Alias != b[i].Alias ||
			a[i].Loc != b[i].Loc || a[i].Witnessable != b[i].Witnessable || !refPathEqual(a[i].Path, b[i].Path) {
			return false
		}
	}
	return true
}

func refPathEqual(a, b RefPath) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// mapCanonicalRefs aligns the original refs to RangeVar nodes in the
// canonical reparsed tree by RefPath only. RefEntity → pinned-schema
// RefQualified is the sole permitted shape change; there is no traversal
// order or name fallback.
func mapCanonicalRefs(original []RangeVarRef, tree *pg.ParseResult, pinnedSchema string) (map[*pg.RangeVar]RefID, error) {
	canonical, byCanonicalNode, err := collectRangeVarRefs(tree)
	if err != nil {
		return nil, err
	}
	if len(canonical) != len(original) {
		return nil, fmt.Errorf("%w: canonical rewrite has %d RangeVars for %d original refs", ErrEvidenceUnavailable, len(canonical), len(original))
	}
	canonicalByID := make(map[RefID]RangeVarRef, len(canonical))
	for _, ref := range canonical {
		canonicalByID[ref.RefID] = ref
	}
	out := make(map[*pg.RangeVar]RefID, len(original))
	for node, canonicalID := range byCanonicalNode {
		idx := int(canonicalID)
		if idx < 0 || idx >= len(original) {
			return nil, fmt.Errorf("%w: canonical RangeVar id %d outside original ref set", ErrEvidenceUnavailable, canonicalID)
		}
		orig, canon := original[idx], canonicalByID[canonicalID]
		if !refPathEqual(orig.Path, canon.Path) {
			return nil, fmt.Errorf("%w: RangeVar %d changed structural path during qualification", ErrEvidenceUnavailable, orig.RefID)
		}
		if !canonicalRefShapeMatches(orig, canon, pinnedSchema) {
			return nil, fmt.Errorf("%w: RangeVar %d changed shape during qualification", ErrEvidenceUnavailable, orig.RefID)
		}
		out[node] = orig.RefID
	}
	if len(out) != len(original) {
		return nil, fmt.Errorf("%w: canonical RangeVar mapping is incomplete", ErrEvidenceUnavailable)
	}
	return out, nil
}

func canonicalRefShapeMatches(orig, canon RangeVarRef, pinnedSchema string) bool {
	if orig.Name != canon.Name || orig.Alias != canon.Alias || orig.Witnessable != canon.Witnessable {
		return false
	}
	switch orig.Kind {
	case RefEntity:
		return orig.Schema == "" && orig.Catalog == "" &&
			canon.Kind == RefQualified && canon.Schema == pinnedSchema && canon.Catalog == ""
	case RefQualified:
		return canon.Kind == RefQualified && orig.Schema == canon.Schema && orig.Catalog == canon.Catalog
	case RefCTE:
		return canon.Kind == RefCTE && canon.Schema == "" && canon.Catalog == ""
	default:
		return false
	}
}

// deparseTree deparses a (possibly injected) parse tree to SQL text.
func deparseTree(tree *pg.ParseResult) (string, error) {
	return pgquery.Deparse(tree)
}
