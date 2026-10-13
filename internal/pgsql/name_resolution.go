// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: *pg.ParseResult trees (post-parse), pinned schema name
// output: RefID/RefPath/RangeVarRef/RefKind, collectRangeVarRefs, QualifyEntities — scope-aware RangeVar classification + structural-path identity + byte-offset canonical qualification
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

// RefID is the immutable identity of one original RangeVar occurrence. IDs
// are assigned once by the collector in deterministic source order.
type RefID int

// RefPath is a structural path used only to verify that the original tree
// and the canonical reparsed tree contain the same RangeVar occurrence.
type RefPath []frame

// RangeVarRef is one collected RangeVar occurrence.
type RangeVarRef struct {
	RefID       RefID
	Path        RefPath
	Kind        RefKind
	Name        string // RangeVar.relname (unqualified final name)
	Schema      string // RangeVar.schemaname when qualified
	Catalog     string // RangeVar.catalogname when present
	Alias       string // RangeVar.alias.aliasname when present
	Loc         int    // byte offset of the name in the source statement
	Witnessable bool   // false → the whole query is rejected
}

// CollectRangeVarRefs is the exported entry for tests and downstream stages
// that need classification without running the whole GuardPG path.
func CollectRangeVarRefs(tree *pg.ParseResult) ([]RangeVarRef, error) {
	refs, _, err := collectRangeVarRefs(tree)
	return refs, err
}

type collector struct {
	refs  []RangeVarRef
	nodes []*pg.RangeVar
	err   error
}

func collectRangeVarRefs(tree *pg.ParseResult) ([]RangeVarRef, map[*pg.RangeVar]RefID, error) {
	c := &collector{}
	for i, raw := range tree.GetStmts() {
		if sel := raw.GetStmt().GetSelectStmt(); sel != nil {
			path := []frame{
				{parent: "ParseResult", field: "stmts", index: i, child: "RawStmt"},
				{parent: "RawStmt", field: "stmt", index: -1, child: "Node"},
				{parent: "Node", field: "select_stmt", index: -1, child: "SelectStmt"},
			}
			c.selectStmt(sel, map[string]struct{}{}, true, path)
		}
	}
	if c.err != nil {
		return nil, nil, c.err
	}
	byNode := make(map[*pg.RangeVar]RefID, len(c.nodes))
	for i, n := range c.nodes {
		byNode[n] = c.refs[i].RefID
	}
	return c.refs, byNode, nil
}

func pathTo(path []frame, parent protoreflect.ProtoMessage, field string, index int, child protoreflect.ProtoMessage) []frame {
	out := append([]frame(nil), path...)
	return append(out, frame{
		parent: string(parent.ProtoReflect().Descriptor().Name()),
		field:  field,
		index:  index,
		child:  string(child.ProtoReflect().Descriptor().Name()),
	})
}

func pathThroughNode(path []frame, parent protoreflect.ProtoMessage, field string, index int, n *pg.Node, child protoreflect.ProtoMessage) []frame {
	out := pathTo(path, parent, field, index, n)
	fd := n.ProtoReflect().WhichOneof(n.ProtoReflect().Descriptor().Oneofs().ByName("node"))
	if fd == nil {
		return out
	}
	return append(out, frame{
		parent: "Node",
		field:  string(fd.Name()),
		index:  -1,
		child:  string(child.ProtoReflect().Descriptor().Name()),
	})
}

// selectStmt collects refs from one SELECT with the given set of visible CTE
// names (outer scopes already folded in by the caller) and a witnessable
// verdict inherited from the edge that reached this SELECT.
func (c *collector) selectStmt(sel *pg.SelectStmt, outerVisible map[string]struct{}, witnessable bool, path []frame) {
	if c.err != nil || sel == nil {
		return
	}
	// CTE bodies: visibility computed per position.
	var localAll map[string]struct{}
	if wc := sel.GetWithClause(); wc != nil {
		wcPath := pathTo(path, sel, "with_clause", -1, wc)
		localAll = make(map[string]struct{}, len(wc.GetCtes()))
		for _, node := range wc.GetCtes() {
			if cte := node.GetCommonTableExpr(); cte != nil {
				localAll[cte.GetCtename()] = struct{}{}
			}
		}
		seen := make(map[string]struct{}, len(wc.GetCtes()))
		for i, node := range wc.GetCtes() {
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
				ctePath := pathThroughNode(wcPath, wc, "ctes", i, node, cte)
				ctePath = pathThroughNode(ctePath, cte, "ctequery", -1, cte.GetCtequery(), sub)
				c.selectStmt(sub, vis, witnessable, ctePath)
			}
		}
	}
	bodyVisible := cloneSet(outerVisible)
	mergeSet(bodyVisible, localAll)

	// FROM clause — the witnessable carrier.
	for i, item := range sel.GetFromClause() {
		itemPath := pathTo(path, sel, "from_clause", i, item)
		c.fromItem(item, bodyVisible, witnessable, itemPath)
	}
	// Set-operation leaves: refs inside can never carry a row-source witness.
	if sel.GetOp() != pg.SetOperation_SETOP_NONE {
		if l := sel.GetLarg(); l != nil {
			c.selectStmt(l, bodyVisible, false, pathTo(path, sel, "larg", -1, l))
		}
		if r := sel.GetRarg(); r != nil {
			c.selectStmt(r, bodyVisible, false, pathTo(path, sel, "rarg", -1, r))
		}
	}
	// Every remaining field is an expression context: subqueries inside are
	// non-witnessable. Walk them from their actual field/index path so two
	// identical expressions in different positions cannot share a RefPath.
	exprRoot := func(field string, index int, n *pg.Node) {
		if n == nil {
			return
		}
		c.exprWalk(msgOf(n), bodyVisible, pathTo(path, sel, field, index, n))
	}
	exprRoot("where_clause", -1, sel.GetWhereClause())
	exprRoot("having_clause", -1, sel.GetHavingClause())
	exprRoot("limit_offset", -1, sel.GetLimitOffset())
	exprRoot("limit_count", -1, sel.GetLimitCount())
	for i, tl := range sel.GetTargetList() {
		exprRoot("target_list", i, tl)
	}
	for i, sc := range sel.GetSortClause() {
		exprRoot("sort_clause", i, sc)
	}
	for i, wc := range sel.GetWindowClause() {
		exprRoot("window_clause", i, wc)
	}
	for i, dc := range sel.GetDistinctClause() {
		exprRoot("distinct_clause", i, dc)
	}
	// VALUES rows are expression lists — scalar subqueries inside them are
	// legal (`VALUES ((SELECT ...))`) and just as unwitnessable.
	for i, vl := range sel.GetValuesLists() {
		exprRoot("values_lists", i, vl)
	}
	// GROUP BY / locking clauses also parse expression trees — subqueries
	// there are rejected later anyway, but refs inside still classify
	// non-witnessable so nothing escapes collection.
	for i, g := range sel.GetGroupClause() {
		exprRoot("group_clause", i, g)
	}
	for i, lc := range sel.GetLockingClause() {
		exprRoot("locking_clause", i, lc)
	}
}

// fromItem handles one FROM-clause element.
func (c *collector) fromItem(n *pg.Node, visible map[string]struct{}, witnessable bool, path []frame) {
	if c.err != nil || n == nil {
		return
	}
	switch v := n.GetNode().(type) {
	case *pg.Node_RangeVar:
		c.rangeVar(v.RangeVar, visible, witnessable,
			append(path, frame{parent: "Node", field: "range_var", index: -1, child: "RangeVar"}))
	case *pg.Node_JoinExpr:
		j := v.JoinExpr
		joinPath := append(path, frame{parent: "Node", field: "join_expr", index: -1, child: "JoinExpr"})
		for i, side := range []*pg.Node{j.GetLarg(), j.GetRarg()} {
			field := "larg"
			if i == 1 {
				field = "rarg"
			}
			c.fromItem(side, visible, witnessable, pathTo(joinPath, j, field, -1, side))
		}
		if q := j.GetQuals(); q != nil {
			c.exprWalk(msgOf(q), visible, pathTo(joinPath, j, "quals", -1, q)) // JOIN ON subqueries → non-witnessable
		}
		for i, u := range j.GetUsingClause() {
			c.exprWalk(msgOf(u), visible, pathTo(joinPath, j, "using_clause", i, u))
		}
	case *pg.Node_RangeSubselect:
		// FROM-side derived table — witness propagates through its output.
		rs := v.RangeSubselect
		rsPath := append(path, frame{parent: "Node", field: "range_subselect", index: -1, child: "RangeSubselect"})
		if sub := rs.GetSubquery().GetSelectStmt(); sub != nil {
			c.selectStmt(sub, visible, witnessable,
				pathThroughNode(rsPath, rs, "subquery", -1, rs.GetSubquery(), sub))
		}
	case *pg.Node_RangeTableSample:
		s := v.RangeTableSample
		samplePath := append(path, frame{parent: "Node", field: "range_table_sample", index: -1, child: "RangeTableSample"})
		c.fromItem(s.GetRelation(), visible, witnessable,
			pathTo(samplePath, s, "relation", -1, s.GetRelation()))
		for i, a := range s.GetArgs() {
			c.exprWalk(msgOf(a), visible, pathTo(samplePath, s, "args", i, a))
		}
		if r := s.GetRepeatable(); r != nil {
			c.exprWalk(msgOf(r), visible, pathTo(samplePath, s, "repeatable", -1, r))
		}
	case *pg.Node_RangeFunction:
		rf := v.RangeFunction
		funcPath := append(path, frame{parent: "Node", field: "range_function", index: -1, child: "RangeFunction"})
		for i, f := range rf.GetFunctions() {
			c.exprWalk(msgOf(f), visible, pathTo(funcPath, rf, "functions", i, f))
		}
	case *pg.Node_RangeTableFunc:
		rtf := v.RangeTableFunc
		funcPath := append(path, frame{parent: "Node", field: "range_table_func", index: -1, child: "RangeTableFunc"})
		if n := rtf.GetRowexpr(); n != nil {
			c.exprWalk(msgOf(n), visible, pathTo(funcPath, rtf, "rowexpr", -1, n))
		}
		if n := rtf.GetDocexpr(); n != nil {
			c.exprWalk(msgOf(n), visible, pathTo(funcPath, rtf, "docexpr", -1, n))
		}
		for i, ns := range rtf.GetNamespaces() {
			c.exprWalk(msgOf(ns), visible, pathTo(funcPath, rtf, "namespaces", i, ns))
		}
		for i, col := range rtf.GetColumns() {
			c.exprWalk(msgOf(col), visible, pathTo(funcPath, rtf, "columns", i, col))
		}
	default:
		// Unknown FROM shape — collect any embedded subqueries as
		// non-witnessable; a RangeVar found this way is flagged too.
		c.exprWalk(msgOf(n), visible, path)
	}
}

// rangeVar classifies one RangeVar.
func (c *collector) rangeVar(rv *pg.RangeVar, visible map[string]struct{}, witnessable bool, path []frame) {
	if c.err != nil || rv == nil {
		return
	}
	ref := RangeVarRef{
		RefID:       RefID(len(c.refs)),
		Path:        append(RefPath(nil), path...),
		Name:        rv.GetRelname(),
		Schema:      rv.GetSchemaname(),
		Catalog:     rv.GetCatalogname(),
		Alias:       rv.GetAlias().GetAliasname(),
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
	c.nodes = append(c.nodes, rv)
}

// exprWalk descends an expression subtree collecting refs inside SubLinks
// (non-witnessable) and flagging any stray RangeVar/SelectStmt shape
// conservatively.
func (c *collector) exprWalk(m protoreflect.Message, visible map[string]struct{}, basePath []frame) {
	if c.err != nil || m == nil || !m.IsValid() {
		return
	}
	ctx := &walkCtx{path: append([]frame(nil), basePath...)}
	walkInto(ctx, m, func(ctx *walkCtx, cur protoreflect.Message) bool {
		if c.err != nil {
			return false
		}
		switch v := cur.Interface().(type) {
		case *pg.SubLink:
			// testexpr (e.g. the LHS of IN) is a sibling of subselect —
			// both can hold nested subqueries; handle each, then stop.
			if te := v.GetTestexpr(); te != nil {
				tePath := pathTo(ctx.path, v, "testexpr", -1, te)
				c.exprWalk(msgOf(te), visible, tePath)
			}
			if sub := v.GetSubselect().GetSelectStmt(); sub != nil {
				subPath := pathThroughNode(ctx.path, v, "subselect", -1, v.GetSubselect(), sub)
				c.selectStmt(sub, visible, false, subPath)
			}
			return false // both children handled
		case *pg.RangeVar:
			// A RangeVar reached through an expression edge is outside every
			// witnessable carrier — flag it rather than guess.
			c.rangeVar(v, visible, false, ctx.path)
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
