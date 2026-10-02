// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: *pg.ParseResult trees (post-parse), pinned schema name
// output: RangeVarRef/RefKind, collectRangeVarRefs, QualifyEntities — scope-aware RangeVar classification + byte-offset canonical qualification
// pos: WITH-position visibility classifier (non-recursive sees earlier same-level+outer, recursive sees all same-level, inner shadows outer, qualified names are always entities, ambiguous ownership rejects) plus witnessable-position verdicts; qualification inserts "schema". at recorded byte offsets only — never rewrites user bytes
// note: if this file changes, update header and README.md
package pgsql

import (
	"fmt"
	"sort"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// RefKind classifies a RangeVar occurrence.
type RefKind byte

const (
	// RefEntity — unqualified relation reference; must be pinned-schema
	// qualified by the canonical rewrite.
	RefEntity RefKind = 'e'
	// RefQualified — already schema/catalog qualified; binding gate resolves
	// it as written (never a CTE).
	RefQualified RefKind = 'q'
	// RefCTE — resolves to a visible common table expression; left untouched.
	RefCTE RefKind = 'c'
)

// RangeVarRef is one collected RangeVar occurrence.
type RangeVarRef struct {
	Kind        RefKind
	Name        string // RangeVar.relname (unqualified final name)
	Schema      string // RangeVar.schemaname when qualified
	Catalog     string // RangeVar.catalogname when present
	Loc         int    // byte offset of the name in the source statement
	Witnessable bool   // false → the whole query is rejected
}

// CollectRangeVarRefs is the exported entry for tests and downstream stages
// that need classification without running the whole GuardPG path.
func CollectRangeVarRefs(tree *pg.ParseResult) ([]RangeVarRef, error) {
	return collectRangeVarRefs(tree)
}

type collector struct {
	refs []RangeVarRef
	err  error
}

func collectRangeVarRefs(tree *pg.ParseResult) ([]RangeVarRef, error) {
	c := &collector{}
	for _, raw := range tree.GetStmts() {
		if sel := raw.GetStmt().GetSelectStmt(); sel != nil {
			c.selectStmt(sel, map[string]struct{}{}, true)
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	return c.refs, nil
}

// selectStmt collects refs from one SELECT with the given set of visible CTE
// names (outer scopes already folded in by the caller) and a witnessable
// verdict inherited from the edge that reached this SELECT.
func (c *collector) selectStmt(sel *pg.SelectStmt, outerVisible map[string]struct{}, witnessable bool) {
	if c.err != nil || sel == nil {
		return
	}
	// CTE bodies: visibility computed per position.
	var localAll map[string]struct{}
	if wc := sel.GetWithClause(); wc != nil {
		localAll = make(map[string]struct{}, len(wc.GetCtes()))
		for _, node := range wc.GetCtes() {
			if cte := node.GetCommonTableExpr(); cte != nil {
				localAll[cte.GetCtename()] = struct{}{}
			}
		}
		seen := make(map[string]struct{}, len(wc.GetCtes()))
		for _, node := range wc.GetCtes() {
			cte := node.GetCommonTableExpr()
			if cte == nil {
				continue
			}
			vis := cloneSet(outerVisible)
			if wc.GetRecursive() {
				mergeSet(vis, localAll)
			} else {
				mergeSet(vis, seen)
			}
			seen[cte.GetCtename()] = struct{}{}
			if sub := cte.GetCtequery().GetSelectStmt(); sub != nil {
				c.selectStmt(sub, vis, witnessable)
			}
		}
	}
	bodyVisible := cloneSet(outerVisible)
	mergeSet(bodyVisible, localAll)

	// FROM clause — the witnessable carrier.
	for _, item := range sel.GetFromClause() {
		c.fromItem(item, bodyVisible, witnessable)
	}
	// Set-operation leaves: refs inside can never carry a row-source witness.
	if sel.GetOp() != pg.SetOperation_SETOP_NONE {
		if l := sel.GetLarg(); l != nil {
			c.selectStmt(l, bodyVisible, false)
		}
		if r := sel.GetRarg(); r != nil {
			c.selectStmt(r, bodyVisible, false)
		}
	}
	// Every remaining field is an expression context: subqueries inside are
	// non-witnessable. Walk them generically hunting SubLinks.
	nonWitnessableFields := []protoreflect.Message{
		msgOf(sel.GetWhereClause()), msgOf(sel.GetHavingClause()),
		msgOf(sel.GetLimitOffset()), msgOf(sel.GetLimitCount()),
	}
	for _, tl := range sel.GetTargetList() {
		nonWitnessableFields = append(nonWitnessableFields, msgOf(tl))
	}
	for _, sc := range sel.GetSortClause() {
		nonWitnessableFields = append(nonWitnessableFields, msgOf(sc))
	}
	for _, wc := range sel.GetWindowClause() {
		nonWitnessableFields = append(nonWitnessableFields, msgOf(wc))
	}
	for _, dc := range sel.GetDistinctClause() {
		nonWitnessableFields = append(nonWitnessableFields, msgOf(dc))
	}
	// VALUES rows are expression lists — scalar subqueries inside them are
	// legal (`VALUES ((SELECT ...))`) and just as unwitnessable.
	for _, vl := range sel.GetValuesLists() {
		nonWitnessableFields = append(nonWitnessableFields, msgOf(vl))
	}
	// GROUP BY / locking clauses also parse expression trees — subqueries
	// there are rejected later anyway, but refs inside still classify
	// non-witnessable so nothing escapes collection.
	for _, g := range sel.GetGroupClause() {
		nonWitnessableFields = append(nonWitnessableFields, msgOf(g))
	}
	for _, lc := range sel.GetLockingClause() {
		nonWitnessableFields = append(nonWitnessableFields, msgOf(lc))
	}
	for _, m := range nonWitnessableFields {
		c.exprWalk(m, bodyVisible)
	}
}

// fromItem handles one FROM-clause element.
func (c *collector) fromItem(n *pg.Node, visible map[string]struct{}, witnessable bool) {
	if c.err != nil || n == nil {
		return
	}
	switch v := n.GetNode().(type) {
	case *pg.Node_RangeVar:
		c.rangeVar(v.RangeVar, visible, witnessable)
	case *pg.Node_JoinExpr:
		j := v.JoinExpr
		c.fromItem(j.GetLarg(), visible, witnessable)
		c.fromItem(j.GetRarg(), visible, witnessable)
		c.exprWalk(msgOf(j.GetQuals()), visible) // JOIN ON subqueries → non-witnessable
		for _, u := range j.GetUsingClause() {
			c.exprWalk(msgOf(u), visible)
		}
	case *pg.Node_RangeSubselect:
		// FROM-side derived table — witness propagates through its output.
		if sub := v.RangeSubselect.GetSubquery().GetSelectStmt(); sub != nil {
			c.selectStmt(sub, visible, witnessable)
		}
	case *pg.Node_RangeTableSample:
		s := v.RangeTableSample
		c.fromItem(s.GetRelation(), visible, witnessable)
		for _, a := range s.GetArgs() {
			c.exprWalk(msgOf(a), visible)
		}
		c.exprWalk(msgOf(s.GetRepeatable()), visible)
	case *pg.Node_RangeFunction:
		for _, f := range v.RangeFunction.GetFunctions() {
			c.exprWalk(msgOf(f), visible)
		}
	case *pg.Node_RangeTableFunc:
		rtf := v.RangeTableFunc
		c.exprWalk(msgOf(rtf.GetRowexpr()), visible)
		c.exprWalk(msgOf(rtf.GetDocexpr()), visible)
		for _, ns := range rtf.GetNamespaces() {
			c.exprWalk(msgOf(ns), visible)
		}
		for _, col := range rtf.GetColumns() {
			c.exprWalk(msgOf(col), visible)
		}
	default:
		// Unknown FROM shape — collect any embedded subqueries as
		// non-witnessable; a RangeVar found this way is flagged too.
		c.exprWalk(msgOf(n), visible)
	}
}

// rangeVar classifies one RangeVar.
func (c *collector) rangeVar(rv *pg.RangeVar, visible map[string]struct{}, witnessable bool) {
	if c.err != nil || rv == nil {
		return
	}
	ref := RangeVarRef{
		Name:        rv.GetRelname(),
		Schema:      rv.GetSchemaname(),
		Catalog:     rv.GetCatalogname(),
		Loc:         int(rv.GetLocation()),
		Witnessable: witnessable,
	}
	switch {
	case rv.GetSchemaname() != "" || rv.GetCatalogname() != "":
		ref.Kind = RefQualified
	default:
		if _, isCTE := visible[rv.GetRelname()]; isCTE {
			ref.Kind = RefCTE
		} else {
			ref.Kind = RefEntity
		}
	}
	c.refs = append(c.refs, ref)
}

// exprWalk descends an expression subtree collecting refs inside SubLinks
// (non-witnessable) and flagging any stray RangeVar/SelectStmt shape
// conservatively.
func (c *collector) exprWalk(m protoreflect.Message, visible map[string]struct{}) {
	if c.err != nil || m == nil || !m.IsValid() {
		return
	}
	walkTree(m, func(ctx *walkCtx, cur protoreflect.Message) bool {
		if c.err != nil {
			return false
		}
		switch v := cur.Interface().(type) {
		case *pg.SubLink:
			// testexpr (e.g. the LHS of IN) is a sibling of subselect —
			// both can hold nested subqueries; handle each, then stop.
			if te := v.GetTestexpr(); te != nil {
				c.exprWalk(msgOf(te), visible)
			}
			if sub := v.GetSubselect().GetSelectStmt(); sub != nil {
				c.selectStmt(sub, visible, false)
			}
			return false // both children handled
		case *pg.RangeVar:
			// A RangeVar reached through an expression edge is outside every
			// witnessable carrier — flag it rather than guess.
			c.rangeVar(v, visible, false)
			return false
		}
		return true
	})
}

func cloneSet(s map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(s)+4)
	for k := range s {
		out[k] = struct{}{}
	}
	return out
}

func mergeSet(dst, src map[string]struct{}) {
	for k := range src {
		dst[k] = struct{}{}
	}
}

// quoteIdent renders an identifier the way the deparser does: always
// double-quoted with internal quotes doubled. The pinned schema comes from
// connection metadata, not from the user statement — quoting it defensively
// keeps the rewrite correct for any stored value.
func quoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}

// QualifyEntities performs the canonical byte-offset rewrite: for every
// collected unqualified entity RangeVar it inserts `"<pinned>".` at the
// RangeVar's location. CTE references and already-qualified references are
// untouched; user-authored bytes are preserved verbatim around each insertion.
// Insertions run in descending offset order so earlier offsets stay valid.
func QualifyEntities(statement, pinnedSchema string, refs []RangeVarRef) (string, error) {
	if pinnedSchema == "" {
		return "", &RejectError{Code: "query_schema_not_usable", Message: "pgsql: pinned schema is empty"}
	}
	prefix := []byte(quoteIdent(pinnedSchema) + ".")
	var locs []int
	for _, r := range refs {
		if r.Kind != RefEntity {
			continue
		}
		if r.Loc < 0 || r.Loc > len(statement) {
			return "", &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: RangeVar offset out of bounds", ErrAmbiguousScope)}
		}
		locs = append(locs, r.Loc)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(locs)))
	out := []byte(statement)
	for _, l := range locs {
		next := make([]byte, 0, len(out)+len(prefix))
		next = append(next, out[:l]...)
		next = append(next, prefix...)
		next = append(next, out[l:]...)
		out = next
	}
	return string(out), nil
}
