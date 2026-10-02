// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: qualified *pg.ParseResult, ColumnResolver supplying FROM-position column names
// output: InjectWitnesses, InjectResult, WitnessRecord, EntityKey, ColumnResolver — layout-freeze + per-layer witness injection + DISTINCT Q+D transform
// pos: G10 mechanism 5a — records the original visible layout (* / x.* / NATURAL join / ordinals / VALUES / per-reference colnames) BEFORE appending witnesses; stars stay verbatim unless an injected column forces expansion, and forced USING/NATURAL merges follow PostgreSQL's side rule (INNER/LEFT left, RIGHT right, FULL COALESCE); emits CASE WHEN FALSE THEN alias.* END on ordinary/window-only layers and (array_agg(alias.*) FILTER (WHERE FALSE))[1] on aggregate/grouping layers; propagates through CTEs and derived tables, suppresses inside SubLinks, merges set-op branches through canonical representatives only when attested entities match, and rejects when any entity's witness cannot reach the output; plain SELECT DISTINCT becomes Q+D (D groups by original public columns only and receives ORDER BY/LIMIT/OFFSET exactly once); internal names live under the reserved __chub_ prefix
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"fmt"
	"strings"

	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// internalPrefix reserves the namespace for injected/internal identifiers.
// User-visible columns or aliases carrying it are rejected rather than
// colliding with injection bookkeeping.
const internalPrefix = "__chub_"

// ColumnResolver supplies the FROM-position column names of a real relation —
// the layout-freezing metadata. In production the binding gate feeds it from
// the post-touch attribute map; tests provide a stub. Consulted lazily:
// statements whose layout never needs concrete base-table names never call it.
type ColumnResolver interface {
	// Columns returns the column names of schema.name in FROM-position
	// order. schema=="" means the pinned schema (unqualified pre-rewrite).
	// An absent relation must return an error wrapping ErrRelationNotFound;
	// any other error is treated as an internal failure, not a user-facing
	// rejection — its text is never copied into a RejectError message.
	Columns(schema, name string) ([]string, error)
}

// ErrRelationNotFound is the sentinel a ColumnResolver wraps when the
// relation is absent — the only resolver outcome mapped to a governed
// rejection (query_object_not_found).
var ErrRelationNotFound = errors.New("relation not found")

// EntityKey identifies the entity a witness column attests — its qualified
// schema.name identity at rewrite time (the binding gate maps it to an OID).
type EntityKey struct {
	Schema string
	Name   string
}

// WitnessRecord maps one top-level-output witness column to the entity it
// attests. The executor compares each witness FieldDescription.DataTypeOID
// against the entity's bound reltype. Alias names the RTE it binds — the same
// relation joined twice attests twice under different aliases.
type WitnessRecord struct {
	Column string
	Entity EntityKey
	Alias  string
}

// InjectResult is the rewritten statement plus its injection record.
type InjectResult struct {
	Tree      *pg.ParseResult
	Witnesses []WitnessRecord // top-level witness columns, in output order
	// WitnessPositions are the zero-based output column indexes that carry
	// witnesses — the executor strips them by position, never by name.
	WitnessPositions []int
	Public           []string // user-visible output column names
}

// InjectWitnesses freezes the visible layout and injects witnesses through
// every SelectStmt layer of an already-qualified parse tree.
func InjectWitnesses(tree *pg.ParseResult, res ColumnResolver) (*InjectResult, error) {
	if tree == nil || len(tree.GetStmts()) != 1 {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v", ErrNotReadOnly)}
	}
	sel := tree.GetStmts()[0].GetStmt().GetSelectStmt()
	if sel == nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v", ErrNotReadOnly)}
	}
	in := &injector{res: res, colCache: map[*pg.RangeVar][]string{}, mergedInto: map[*EntityKey]*EntityKey{}}
	rep, layout := in.selectStmt(sel, map[string]*itemLayout{}, true)
	if in.err != nil {
		return nil, in.err
	}
	if rep != sel {
		tree.GetStmts()[0].GetStmt().Node = &pg.Node_SelectStmt{SelectStmt: rep}
	}
	out := &InjectResult{Tree: tree}
	covered := map[*EntityKey]bool{}
	for i, c := range layout.cols {
		if c.witness {
			out.Witnesses = append(out.Witnesses, WitnessRecord{
				Column: c.name, Entity: *c.entity, Alias: c.src,
			})
			out.WitnessPositions = append(out.WitnessPositions, i)
		} else {
			out.Public = append(out.Public, c.name)
		}
	}
	// Coverage: every entity leaf's witness must reach the final output —
	// a witness generated inside a CTE but referenced only through a
	// suppressed sublink leaves that entity's binding unproven. Set-op
	// merges alias the dropped branch's leaf onto the surviving column's
	// representative; rep() keeps traversal bounded even if a merge edge
	// were ever malformed.
	for _, c := range layout.cols {
		if c.witness {
			covered[in.rep(c.entity)] = true
		}
	}
	for _, leaf := range in.leaves {
		if in.err != nil {
			return nil, in.err
		}
		if !covered[in.rep(leaf)] {
			return nil, &RejectError{Code: "query_reference_not_witnessable",
				Message: fmt.Sprintf("%v: witness for entity never reaches the output", ErrNotWitnessable)}
		}
	}
	return out, nil
}

// ---------- layout machinery ----------

// outCol is one column in a FROM item's or SELECT layer's output.
type outCol struct {
	name    string
	src     string   // qualifying RTE alias this column reads from ("" when expr set)
	expr    *pg.Node // non-nil for merged USING columns — a COALESCE tree of both sides
	witness bool
	entity  *EntityKey // set when witness
}

// itemLayout describes a FROM item's (or SELECT layer's) output columns.
type itemLayout struct {
	cols   []outCol
	opaque bool // column set unknowable — `*` spanning it is unfreezable
}

// fromItemRef is one FROM-clause element plus children for join trees.
type fromItemRef struct {
	layout   *itemLayout
	alias    string
	entity   *EntityKey // non-nil for real relations
	node     *pg.Node
	children []*fromItemRef // join leaves
}

type injector struct {
	res      ColumnResolver
	colCache map[*pg.RangeVar][]string
	wSeq     int
	synth    int
	err      error
	// leaves registers every entity leaf's identity pointer in creation
	// order; mergedInto aliases a set-op branch's leaf key to the surviving
	// output column's key so coverage can resolve either side.
	leaves     []*EntityKey
	mergedInto map[*EntityKey]*EntityKey
}

// witnessName mints a fresh internal column name outside the avoid set —
// real relations can legitimately carry __chub_-named columns, so emission
// must never collide with names already visible in the layer.
func (in *injector) witnessName(avoid map[string]bool) string {
	for {
		in.wSeq++
		name := fmt.Sprintf("__chub_w%d", in.wSeq)
		if !avoid[name] {
			avoid[name] = true
			return name
		}
	}
}

// entityCols resolves (cached) column names for a real relation.
func (in *injector) entityCols(rv *pg.RangeVar) ([]string, error) {
	if rv == nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: entity without relation node", ErrLayoutUnfreezable)}
	}
	if c, ok := in.colCache[rv]; ok {
		return c, nil
	}
	if in.res == nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: column metadata unavailable", ErrLayoutUnfreezable)}
	}
	cols, err := in.res.Columns(rv.GetSchemaname(), rv.GetRelname())
	if err != nil {
		if errors.Is(err, ErrRelationNotFound) {
			return nil, &RejectError{Code: "query_object_not_found",
				Message: fmt.Sprintf("relation %s.%s not found", rv.GetSchemaname(), rv.GetRelname())}
		}
		// Any other resolver failure (timeout, cancel, backend fault) is an
		// internal error — never copy its text into an API-visible rejection
		// or mislabel it as a missing object.
		return nil, fmt.Errorf("resolve columns for %s.%s: %w",
			rv.GetSchemaname(), rv.GetRelname(), err)
	}
	in.colCache[rv] = cols
	return cols, nil
}

// materialize fills an entity item's layout from the resolver on demand.
func (in *injector) materialize(it *fromItemRef) {
	if in.err != nil || it == nil || it.entity == nil || !it.layout.opaque {
		return
	}
	cols, err := in.entityCols(it.node.GetRangeVar())
	if err != nil {
		in.err = err
		return
	}
	it.layout.cols = nil
	for _, n := range cols {
		it.layout.cols = append(it.layout.cols, outCol{name: n, src: it.alias})
	}
	it.layout.opaque = false
}

// ---------- layer processing ----------

// selectStmt injects one SELECT layer, returning the (possibly replaced —
// DISTINCT wraps Q in D) layer node and its output layout. cteLayouts maps
// visible CTE names to their already-processed layouts.
//
// emit=false suppresses ALL witness work (used for SubLink bodies — a
// subquery inside an expression can never propagate a witness outward, so
// nothing is appended; freezing still runs so `*`/`x.*` there expand to the
// original public columns only).
func (in *injector) selectStmt(sel *pg.SelectStmt, cteLayouts map[string]*itemLayout, emit bool) (*pg.SelectStmt, *itemLayout) {
	if in.err != nil || sel == nil {
		return sel, nil
	}
	in.checkInternalNames(sel)
	if in.err != nil {
		return sel, nil
	}

	// CTE bodies first — same per-position visibility as the collector.
	bodyLayouts := map[string]*itemLayout{}
	for k, v := range cteLayouts {
		bodyLayouts[k] = v
	}
	if wc := sel.GetWithClause(); wc != nil {
		// Recursive WITH: every sibling name is visible inside every body —
		// register opaque placeholders first (entities are impossible inside
		// recursive bodies; `*` over a placeholder rejects unfreezable).
		if wc.GetRecursive() {
			for _, n := range wc.GetCtes() {
				if c := n.GetCommonTableExpr(); c != nil {
					bodyLayouts[c.GetCtename()] = &itemLayout{opaque: true}
				}
			}
		}
		for _, n := range wc.GetCtes() {
			cte := n.GetCommonTableExpr()
			if cte == nil {
				continue
			}
			var body *itemLayout
			if node := cte.GetCtequery(); node != nil {
				if sub := node.GetSelectStmt(); sub != nil {
					var rep *pg.SelectStmt
					rep, body = in.selectStmt(sub, bodyLayouts, emit)
					if rep != sub {
						node.Node = &pg.Node_SelectStmt{SelectStmt: rep}
					}
				}
			}
			if in.err != nil {
				return sel, nil
			}
			if body == nil {
				body = &itemLayout{opaque: true}
			}
			// WITH c(x, y) renames the body's public output positionally.
			if cn := cte.GetAliascolnames(); len(cn) > 0 {
				body = cloneLayoutRenamed(body, cn)
			}
			bodyLayouts[cte.GetCtename()] = body
		}
	}

	// Set-operation node: process leaves through the full layer path, then
	// verify every leaf attests the same entity sequence — a merged witness
	// column must denote one entity, never two.
	if sel.GetOp() != pg.SetOperation_SETOP_NONE {
		return sel, in.setOpLayer(sel, bodyLayouts, emit)
	}

	var items []*fromItemRef
	for _, n := range sel.GetFromClause() {
		items = append(items, in.fromItem(n, bodyLayouts, emit))
	}
	if in.err != nil {
		return sel, nil
	}
	in.freezeTargets(sel, items)
	if in.err != nil {
		return sel, nil
	}
	publics, pubStarts, err := in.publicNames(sel, items)
	if err != nil {
		in.err = err
		return sel, nil
	}
	in.checkOrdinals(sel, len(publics))
	if in.err != nil {
		return sel, nil
	}

	if !emit {
		// A suppressed subtree (inside a SubLink) can never carry a witness
		// outward — an entity leaf here would execute unproven. The
		// collector already rejects these; keep the injector fail-closed
		// for direct InjectWitnesses callers too.
		for _, it := range items {
			for _, leaf := range flattenLeaves(it) {
				if leaf.entity != nil {
					in.err = &RejectError{Code: "query_reference_not_witnessable",
						Message: fmt.Sprintf("%v: entity inside suppressed subquery", ErrNotWitnessable)}
					return sel, nil
				}
			}
		}
	}
	aggregate := isAggregateLayer(sel)
	var emitted []outCol
	if emit {
		// Names already taken in this layer's output space — publics plus
		// every column any leaf item exposes (real tables may carry
		// __chub_-prefixed user columns).
		taken := map[string]bool{}
		for _, p := range publics {
			taken[p] = true
		}
		for _, it := range items {
			for _, leaf := range flattenLeaves(it) {
				for _, c := range leaf.layout.cols {
					taken[c.name] = true
				}
			}
		}
		for _, it := range items {
			in.emitItemWitnesses(sel, it, aggregate, taken, &emitted)
		}
	}
	if in.err != nil {
		return sel, nil
	}
	// Subqueries inside expression positions are processed with witnesses
	// suppressed — their rows never propagate a witness outward; freezing
	// there keeps `*`/`x.*` honest against injected inner columns.
	in.processSublinks(sel, bodyLayouts)
	if in.err != nil {
		return sel, nil
	}

	layout := &itemLayout{}
	for _, n := range publics {
		layout.cols = append(layout.cols, outCol{name: n})
	}
	layout.cols = append(layout.cols, emitted...)

	if isPlainDistinct(sel) {
		d, dLayout := in.wrapDistinct(sel, publics, pubStarts, emitted)
		return d, dLayout
	}
	return sel, layout
}

// setOpLayer injects into UNION/INTERSECT/EXCEPT leaves and merges their
// layouts. Leaves can only reference CTEs/derived sources (the guard rejected
// direct entity refs); leaves may themselves propagate inner witnesses — the
// merge is valid only when every leaf attests the SAME entity per position.
func (in *injector) setOpLayer(sel *pg.SelectStmt, cteLayouts map[string]*itemLayout, emit bool) *itemLayout {
	lrep, l := in.selectStmt(sel.GetLarg(), cteLayouts, emit)
	if lrep != sel.GetLarg() {
		sel.Larg = lrep
	}
	rrep, r := in.selectStmt(sel.GetRarg(), cteLayouts, emit)
	if rrep != sel.GetRarg() {
		sel.Rarg = rrep
	}
	if in.err != nil {
		return nil
	}
	if l == nil || r == nil {
		in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: set-operation leaf has no layout", ErrLayoutUnfreezable)}
		return nil
	}
	lw, rw := witnessEntities(l), witnessEntities(r)
	if len(lw) != len(rw) {
		in.err = &RejectError{Code: "query_reference_not_witnessable",
			Message: fmt.Sprintf("%v: set-operation branches differ in attested sources", ErrNotWitnessable)}
		return nil
	}
	for i := range lw {
		if *lw[i] != *rw[i] {
			in.err = &RejectError{Code: "query_reference_not_witnessable",
				Message: fmt.Sprintf("%v: set-operation merges different attested sources", ErrNotWitnessable)}
			return nil
		}
		// Merge through canonical representatives — a second branch pairing
		// the same entities in a different order (A,B vs B,A) must converge
		// on one root, not point A and B at each other.
		if a, b := in.rep(lw[i]), in.rep(rw[i]); a != b {
			in.mergedInto[b] = a
		}
	}
	if in.err != nil {
		return nil
	}
	return l // left leaf's names define the set-op's output names
}

// rep resolves an entity key to its merge representative (union-find root).
// Writes always link roots, so the map stays acyclic; the iteration bound is
// a fail-closed defense, never the mechanism that prevents the cycle.
func (in *injector) rep(k *EntityKey) *EntityKey {
	for hops := 0; hops <= len(in.mergedInto); hops++ {
		n, ok := in.mergedInto[k]
		if !ok {
			return k
		}
		k = n
	}
	in.err = &RejectError{Code: "query_not_allowed",
		Message: fmt.Sprintf("%v: witness merge cycle", ErrLayoutUnfreezable)}
	return k
}

func witnessEntities(l *itemLayout) []*EntityKey {
	var out []*EntityKey
	for _, c := range l.cols {
		if c.witness {
			out = append(out, c.entity)
		}
	}
	return out
}

// ---------- FROM items ----------

func (in *injector) fromItem(n *pg.Node, cteLayouts map[string]*itemLayout, emit bool) *fromItemRef {
	if in.err != nil {
		return nil
	}
	switch v := n.GetNode().(type) {
	case *pg.Node_RangeVar:
		rv := v.RangeVar
		alias := rv.GetRelname()
		if a := rv.GetAlias(); a != nil {
			alias = a.GetAliasname()
		}
		if rv.GetSchemaname() == "" && rv.GetCatalogname() == "" {
			if l, isCTE := cteLayouts[rv.GetRelname()]; isCTE {
				// Clone per reference — the same CTE may be aliased
				// differently at each FROM site, and a column alias list
				// (FROM c AS d(x,y)) renames this reference's public columns.
				cl := l
				if cn := rv.GetAlias().GetColnames(); len(cn) > 0 {
					cl = cloneLayoutRenamed(cl, cn)
				}
				return &fromItemRef{layout: layoutWithAlias(cl, alias), alias: alias, node: n}
			}
		}
		it := &fromItemRef{
			layout: &itemLayout{opaque: true},
			alias:  alias,
			entity: &EntityKey{Schema: rv.GetSchemaname(), Name: rv.GetRelname()},
			node:   n,
		}
		in.leaves = append(in.leaves, it.entity)
		// Entity columns materialize eagerly — every downstream step (star
		// expansion, join merge, public names, DISTINCT keys) treats entity
		// layouts uniformly with derived ones.
		in.materialize(it)
		if in.err == nil {
			// FROM t AS o(x,y) renames the relation's exposed columns for
			// this reference only — clone, never touch the shared cache.
			if cn := rv.GetAlias().GetColnames(); len(cn) > 0 {
				it.layout = cloneLayoutRenamed(it.layout, cn)
			}
		}
		return it
	case *pg.Node_RangeSubselect:
		sub := v.RangeSubselect
		var layout *itemLayout
		if inner := sub.GetSubquery().GetSelectStmt(); inner != nil {
			if vl := inner.GetValuesLists(); len(vl) > 0 {
				layout = valuesLayout(inner, sub.GetAlias())
			} else {
				var rep *pg.SelectStmt
				rep, layout = in.selectStmt(inner, cteLayouts, emit)
				if rep != inner {
					sub.Subquery.Node = &pg.Node_SelectStmt{SelectStmt: rep}
				}
			}
		}
		if in.err != nil {
			return nil
		}
		if layout == nil {
			layout = &itemLayout{opaque: true}
		}
		alias := aliasName(sub.GetAlias())
		// FROM (SELECT ...) d(x,y) renames the subquery's public output
		// positionally — witnesses keep their internal names.
		if cn := sub.GetAlias().GetColnames(); len(cn) > 0 {
			layout = cloneLayoutRenamed(layout, cn)
		}
		if alias == "" && hasWitnessCols(layout) {
			// An unaliased derived table carrying witnesses still needs a
			// referable name for `x.*` expansion and propagation columns —
			// synthesize an internal one; user text can't address it anyway.
			in.synth++
			alias = fmt.Sprintf("__chub_d%d", in.synth)
			if sub.Alias == nil {
				sub.Alias = &pg.Alias{}
			}
			sub.Alias.Aliasname = alias
		}
		return &fromItemRef{layout: layoutWithAlias(layout, alias), alias: alias, node: n}
	case *pg.Node_JoinExpr:
		j := v.JoinExpr
		if j.GetAlias() != nil {
			// An aliased join hides its leaf RTE names — leaf whole-row
			// witnesses can't be referenced from this layer. Wrap the
			// (unaliased) join in a derived SELECT * layer: freezing expands
			// the publics inside and the layer machinery emits each leaf
			// witness where the leaf aliases are still visible. The outer
			// query then treats `j` as an ordinary derived table.
			alias := j.GetAlias()
			j.Alias = nil
			innerSel := &pg.SelectStmt{
				Op:         pg.SetOperation_SETOP_NONE,
				TargetList: []*pg.Node{targetNode(colRefNode("*"), "")},
				FromClause: []*pg.Node{{Node: &pg.Node_JoinExpr{JoinExpr: j}}},
			}
			rep, layout := in.selectStmt(innerSel, cteLayouts, emit)
			if in.err != nil {
				return nil
			}
			sub := &pg.RangeSubselect{
				Subquery: &pg.Node{Node: &pg.Node_SelectStmt{SelectStmt: rep}},
				Alias:    alias,
			}
			// n keeps the derived table; the join moved inside it — never
			// reuse n inside innerSel, which would create a cycle.
			n.Node = &pg.Node_RangeSubselect{RangeSubselect: sub}
			if cn := alias.GetColnames(); len(cn) > 0 {
				layout = cloneLayoutRenamed(layout, cn)
			}
			return &fromItemRef{layout: layoutWithAlias(layout, alias.GetAliasname()), alias: alias.GetAliasname(), node: n}
		}
		left := in.fromItem(j.GetLarg(), cteLayouts, emit)
		right := in.fromItem(j.GetRarg(), cteLayouts, emit)
		if in.err != nil {
			return nil
		}
		return &fromItemRef{layout: in.joinFreezeAndLayout(j, left, right), node: n,
			children: []*fromItemRef{left, right}}
	case *pg.Node_RangeTableSample:
		// TABLESAMPLE wraps the relation; any AS alias lives on the inner
		// RangeVar itself — the sampled row type is unchanged.
		return in.fromItem(v.RangeTableSample.GetRelation(), cteLayouts, emit)
	default:
		// RangeFunction/RangeTableFunc/VALUES etc.: no entity RangeVars
		// inside (stray ones were rejected); column set is opaque.
		return &fromItemRef{layout: &itemLayout{opaque: true}, alias: aliasOf(n), node: n}
	}
}

// joinFreezeAndLayout computes the merged output layout of a JOIN from its
// two child layouts — never from the raw leaf list, so nested joins keep
// their already-merged columns. When a merged column must be rendered
// explicitly (a forced `*` expansion), it follows PostgreSQL's own rule:
// INNER/LEFT take the left column, RIGHT the right column, and only FULL
// produces a COALESCE of both sides. Unforced `*` stays verbatim, so the
// common path lets PostgreSQL compute the merged value natively — including
// its type coercion — rather than approximating it with an expression.
func (in *injector) joinFreezeAndLayout(j *pg.JoinExpr, l, r *fromItemRef) *itemLayout {
	switch j.GetJointype() {
	case pg.JoinType_JOIN_SEMI, pg.JoinType_JOIN_ANTI, pg.JoinType_JOIN_RIGHT_ANTI,
		pg.JoinType_JOIN_UNIQUE_OUTER, pg.JoinType_JOIN_UNIQUE_INNER:
		// Planner-internal join kinds cannot come from user SQL — refuse to
		// guess their output layout.
		in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: semi/anti join", ErrLayoutUnfreezable)}
		return nil
	}
	needCols := j.GetIsNatural() || len(j.GetUsingClause()) > 0
	if needCols {
		in.materializeDeep(l)
		in.materializeDeep(r)
		if in.err != nil {
			return nil
		}
	}
	// Merged column set: explicit USING order, or — for NATURAL — the common
	// public columns in left order. Computed unconditionally: the layout is
	// needed for `*` expansion even when no witness conversion happens.
	var merged []string
	if j.GetIsNatural() {
		if l.layout.opaque || r.layout.opaque {
			in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: NATURAL join over opaque input", ErrLayoutUnfreezable)}
			return nil
		}
		rpub := map[string]bool{}
		for _, c := range r.layout.cols {
			if !c.witness {
				rpub[c.name] = true
			}
		}
		for _, c := range l.layout.cols {
			if !c.witness && rpub[c.name] {
				merged = append(merged, c.name)
			}
		}
		// Freeze to USING only when injected columns ride either side —
		// otherwise the original NATURAL text stays untouched.
		if hasWitnessCols(l.layout) || hasWitnessCols(r.layout) {
			j.IsNatural = false
			j.UsingClause = nil
			for _, name := range merged {
				j.UsingClause = append(j.UsingClause, strNode(name))
			}
		}
	} else {
		for _, u := range j.GetUsingClause() {
			if s := u.GetString_(); s != nil {
				merged = append(merged, s.GetSval())
			}
		}
	}
	using := map[string]bool{}
	out := &itemLayout{}
	for _, name := range merged {
		lc := findPublic(l.layout, name)
		rc := findPublic(r.layout, name)
		if lc == nil || rc == nil {
			in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: USING column absent from a join side", ErrLayoutUnfreezable)}
			return nil
		}
		using[name] = true
		col := outCol{name: name}
		switch j.GetJointype() {
		case pg.JoinType_JOIN_RIGHT:
			col.expr = refNode(rc)
		case pg.JoinType_JOIN_FULL:
			col.expr = coalesceNode(refNode(lc), refNode(rc))
		default: // INNER, LEFT — PostgreSQL projects the left column
			col.expr = refNode(lc)
		}
		out.cols = append(out.cols, col)
	}
	appendSide := func(layout *itemLayout) {
		for _, c := range layout.cols {
			if c.witness {
				out.cols = append(out.cols, c)
				continue
			}
			if using[c.name] {
				continue
			}
			out.cols = append(out.cols, c)
		}
	}
	appendSide(l.layout)
	appendSide(r.layout)
	out.opaque = l.layout.opaque || r.layout.opaque
	return out
}

// findPublic returns the public column of a layout by output name.
func findPublic(l *itemLayout, name string) *outCol {
	for i := range l.cols {
		if !l.cols[i].witness && l.cols[i].name == name {
			return &l.cols[i]
		}
	}
	return nil
}

// refNode renders the reference an expansion must emit for a column:
// the stored merge expression, or an alias-qualified name.
func refNode(c *outCol) *pg.Node {
	if c.expr != nil {
		return proto.Clone(c.expr).(*pg.Node)
	}
	if c.src != "" {
		return colRefNode(c.src, c.name)
	}
	return colRefNode(c.name)
}

// coalesceNode builds COALESCE(a, b).
func coalesceNode(a, b *pg.Node) *pg.Node {
	return &pg.Node{Node: &pg.Node_CoalesceExpr{CoalesceExpr: &pg.CoalesceExpr{
		Args: []*pg.Node{a, b},
	}}}
}

// layoutWithAlias clones a layout stamping the referencing alias as each
// public column's source qualifier — used when a FROM item's alias is the
// only name its columns may be reached through. Witness cols keep their
// recorded src (the attested RTE alias inside the source's own scope).
func layoutWithAlias(l *itemLayout, alias string) *itemLayout {
	if l == nil {
		return &itemLayout{opaque: true}
	}
	out := &itemLayout{opaque: l.opaque, cols: append([]outCol(nil), l.cols...)}
	for i := range out.cols {
		if !out.cols[i].witness && out.cols[i].src == "" && alias != "" {
			out.cols[i].src = alias
		}
	}
	return out
}

// valuesLayout computes a `(VALUES ...)` derived table's columns: the row
// arity comes from the first row; names are the alias column list or PG's
// columnN defaults. No entity can occur inside VALUES rows' expressions —
// a SubLink there was already classified non-witnessable.
func valuesLayout(sel *pg.SelectStmt, alias *pg.Alias) *itemLayout {
	rows := sel.GetValuesLists()
	if len(rows) == 0 {
		return &itemLayout{opaque: true}
	}
	arity := len(rows[0].GetList().GetItems())
	names := make([]string, arity)
	for i := range names {
		names[i] = fmt.Sprintf("column%d", i+1)
	}
	for i, c := range alias.GetColnames() {
		if i >= arity {
			break
		}
		if s := c.GetString_(); s != nil {
			names[i] = s.GetSval()
		}
	}
	out := &itemLayout{}
	for _, n := range names {
		out.cols = append(out.cols, outCol{name: n})
	}
	return out
}

// materializeDeep resolves entity leaves inside an item tree (join legs).
func (in *injector) materializeDeep(it *fromItemRef) {
	if it == nil {
		return
	}
	in.materialize(it)
	for _, ch := range it.children {
		in.materializeDeep(ch)
	}
}

// ---------- freezing ----------

// freezeTargets expands `*`/`x.*` against original public columns and rejects
// whole-row composite consumption of witness-bearing items.
func (in *injector) freezeTargets(sel *pg.SelectStmt, items []*fromItemRef) {
	byAlias := map[string]*fromItemRef{}
	for _, it := range items {
		for _, leaf := range flattenLeaves(it) {
			if leaf.alias != "" {
				byAlias[leaf.alias] = leaf
			}
		}
	}
	var frozen []*pg.Node
	for _, t := range sel.GetTargetList() {
		rt := t.GetResTarget()
		cr := rt.GetVal().GetColumnRef()
		if rt == nil || cr == nil || !hasStar(cr.GetFields()) {
			if rt != nil {
				if e := in.checkCompositeRef(rt.GetVal(), byAlias); e != nil {
					in.err = e
					return
				}
			}
			frozen = append(frozen, t)
			continue
		}
		switch {
		case len(cr.GetFields()) == 1:
			// Bare `*` — keep it verbatim whenever nothing carries injected
			// columns: PostgreSQL then computes USING/NATURAL merges, join
			// types, and column source metadata itself. Expansion is forced
			// only when a witness-carrying item would leak internal columns.
			needExpand := false
			for _, it := range items {
				if it.layout == nil || it.layout.opaque {
					in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: * over opaque FROM item", ErrLayoutUnfreezable)}
					return
				}
				if hasWitnessCols(it.layout) {
					needExpand = true
				}
			}
			if !needExpand {
				frozen = append(frozen, t)
				continue
			}
			for _, it := range items {
				frozen = append(frozen, in.publicExpansion(it)...)
				if in.err != nil {
					return
				}
			}
		default:
			// `x.*` — keep verbatim when the addressed item exposes only
			// original columns (entities, clean derived tables, CTE names
			// after column-aliasing — PostgreSQL expands those correctly).
			// Expand explicitly only when injected witness cols must be
			// filtered out of the visible layout.
			name := starQualifierName(cr.GetFields())
			if name == "" {
				frozen = append(frozen, t)
				continue
			}
			it := byAlias[name]
			switch {
			case it == nil || it.entity != nil || !hasWitnessCols(it.layout):
				frozen = append(frozen, t)
			default:
				for _, c := range it.layout.cols {
					if !c.witness {
						frozen = append(frozen, targetNode(colRefNode(name, c.name), rt.GetName()))
					}
				}
			}
		}
	}
	sel.TargetList = frozen
	// Every other expression position in the layer gets the same
	// composite-consumption check (quals, predicates, window/sort keys).
	var quals []*pg.Node
	var walkQuals func(n *pg.Node)
	walkQuals = func(n *pg.Node) {
		if n == nil {
			return
		}
		if j := n.GetJoinExpr(); j != nil {
			quals = append(quals, j.GetQuals())
			walkQuals(j.GetLarg())
			walkQuals(j.GetRarg())
		}
	}
	for _, f := range sel.GetFromClause() {
		walkQuals(f)
	}
	quals = append(quals,
		sel.GetWhereClause(), sel.GetHavingClause(),
		sel.GetLimitCount(), sel.GetLimitOffset())
	quals = append(quals, sel.GetSortClause()...)
	quals = append(quals, sel.GetWindowClause()...)
	quals = append(quals, sel.GetDistinctClause()...)
	for _, m := range quals {
		if e := in.checkCompositeRef(m, byAlias); e != nil {
			in.err = e
			return
		}
	}
}

// publicExpansion emits explicit targets for one item's original public cols.
func (in *injector) publicExpansion(it *fromItemRef) []*pg.Node {
	switch {
	case it.entity != nil:
		// Real relation: `"alias".*` expands only user columns — keep it.
		return []*pg.Node{targetNode(colRefNode(it.alias, "*"), "")}
	case len(it.children) > 0:
		// Join: expand the merged output — qualified refs via each column's
		// source alias; USING-merged cols unqualified (unique in the output).
		in.materializeDeep(it)
		if in.err != nil {
			return nil
		}
		if it.layout.opaque {
			in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: * over opaque join", ErrLayoutUnfreezable)}
			return nil
		}
		var out []*pg.Node
		for _, c := range it.layout.cols {
			if c.witness {
				continue
			}
			switch {
			case c.expr != nil:
				// Merged USING column — emit the COALESCE tree and restore
				// the output name; the plain name form could bind elsewhere.
				out = append(out, targetNode(refNode(&c), c.name))
			case c.src != "":
				out = append(out, targetNode(colRefNode(c.src, c.name), ""))
			default:
				out = append(out, targetNode(colRefNode(c.name), ""))
			}
		}
		return out
	case it.layout.opaque:
		in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: * over opaque FROM item", ErrLayoutUnfreezable)}
		return nil
	default:
		var out []*pg.Node
		for _, c := range it.layout.cols {
			if !c.witness {
				out = append(out, targetNode(colRefNode(it.alias, c.name), ""))
			}
		}
		return out
	}
}

// publicNames returns the layer's user-visible output names post-freeze,
// expanding any retained `x.*` target (entity aliases) via the resolver.
// The second return maps each target node to the public column index where
// its output begins — needed because a retained `x.*` covers several
// columns with one node.
func (in *injector) publicNames(sel *pg.SelectStmt, items []*fromItemRef) ([]string, []int, error) {
	byAlias := map[string]*fromItemRef{}
	for _, it := range items {
		for _, leaf := range flattenLeaves(it) {
			if leaf.alias != "" {
				byAlias[leaf.alias] = leaf
			}
		}
	}
	var out []string
	var starts []int
	for _, t := range sel.GetTargetList() {
		rt := t.GetResTarget()
		if rt == nil || rt.GetVal() == nil {
			return nil, nil, fmt.Errorf("res-target missing value")
		}
		starts = append(starts, len(out))
		if rt.GetName() != "" {
			out = append(out, rt.GetName())
			continue
		}
		if cr := rt.GetVal().GetColumnRef(); cr != nil && hasStar(cr.GetFields()) {
			// A retained star target contributes its referenced columns —
			// enumerate them from the item layouts (which already carry
			// per-reference column aliases).
			names, err := in.starNames(cr.GetFields(), items, byAlias)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, names...)
			continue
		}
		out = append(out, outputNameOf(rt.GetVal()))
	}
	return out, starts, nil
}

// starNames enumerates the public column names a retained `*`/`x.*` target
// expands to — the item layouts already carry per-reference column aliases,
// so this list matches what PostgreSQL will emit. Retained stars are only
// kept over witness-free items, so no internal names can appear here.
func (in *injector) starNames(fields []*pg.Node, items []*fromItemRef, byAlias map[string]*fromItemRef) ([]string, error) {
	publicNames := func(l *itemLayout) ([]string, error) {
		if l == nil || l.opaque {
			return nil, fmt.Errorf("%v: cannot enumerate star columns of opaque item", ErrLayoutUnfreezable)
		}
		var names []string
		for _, c := range l.cols {
			if !c.witness {
				names = append(names, c.name)
			}
		}
		return names, nil
	}
	if len(fields) == 1 {
		var out []string
		for _, it := range items {
			names, err := publicNames(it.layout)
			if err != nil {
				return nil, err
			}
			out = append(out, names...)
		}
		return out, nil
	}
	// Qualified star: fields[:-1] address a FROM item — by alias first, then
	// by a real relation's schema.name (schema.t.* stays legal SQL).
	var q []string
	for _, f := range fields[:len(fields)-1] {
		if s := f.GetString_(); s == nil {
			return nil, fmt.Errorf("%v: unsupported star qualifier", ErrLayoutUnfreezable)
		} else {
			q = append(q, s.GetSval())
		}
	}
	qual := strings.Join(q, ".")
	if it := byAlias[qual]; it != nil {
		return publicNames(it.layout)
	}
	for _, top := range items {
		for _, leaf := range flattenLeaves(top) {
			if leaf.entity == nil {
				continue
			}
			if leaf.entity.Schema+"."+leaf.entity.Name == qual ||
				(leaf.entity.Schema == "" && leaf.entity.Name == qual) {
				return publicNames(leaf.layout)
			}
		}
	}
	return nil, fmt.Errorf("%v: star qualifier %q resolves to no FROM item", ErrLayoutUnfreezable, qual)
}

// outputNameOf derives a target's public name the way PostgreSQL's
// FigureColname does for the node kinds this workbench can emit or receive —
// a wrong name here would rename a user-visible column after Q+D wrapping.
func outputNameOf(v *pg.Node) string {
	switch n := v.GetNode().(type) {
	case *pg.Node_ColumnRef:
		f := n.ColumnRef.GetFields()
		if len(f) > 0 {
			if s := f[len(f)-1].GetString_(); s != nil {
				return s.GetSval()
			}
		}
	case *pg.Node_FuncCall:
		if name := funcName(n.FuncCall); name != "" {
			return name
		}
	case *pg.Node_TypeCast:
		// `expr::type` keeps the argument's name when it is a column
		// reference (SELECT id::text → column "id"); otherwise the output
		// takes the target type's final name segment.
		if cr := n.TypeCast.GetArg().GetColumnRef(); cr != nil {
			f := cr.GetFields()
			if len(f) > 0 {
				if s := f[len(f)-1].GetString_(); s != nil {
					return s.GetSval()
				}
			}
		}
		if tn := n.TypeCast.GetTypeName(); tn != nil && len(tn.GetNames()) > 0 {
			if s := tn.GetNames()[len(tn.GetNames())-1].GetString_(); s != nil {
				return strings.ToLower(s.GetSval())
			}
		}
	case *pg.Node_CaseExpr:
		return "case"
	case *pg.Node_CoalesceExpr:
		return "coalesce"
	case *pg.Node_MinMaxExpr:
		if n.MinMaxExpr.GetOp() == pg.MinMaxOp_IS_GREATEST {
			return "greatest"
		}
		return "least"
	case *pg.Node_SubLink:
		if n.SubLink.GetSubLinkType() == pg.SubLinkType_EXISTS_SUBLINK {
			return "exists"
		}
	case *pg.Node_XmlExpr:
		return "xml"
	}
	return "?column?"
}

// checkCompositeRef rejects whole-row references to witness-bearing items —
// `d` alone or `d.*`-as-composite of an injected derived/CTE cannot attest
// which public columns were visible.
func (in *injector) checkCompositeRef(n *pg.Node, byAlias map[string]*fromItemRef) error {
	var err error
	walkTree(msgOf(n), func(ctx *walkCtx, m protoreflect.Message) bool {
		if err != nil {
			return false
		}
		cr, ok := m.Interface().(*pg.ColumnRef)
		if !ok {
			return true
		}
		f := cr.GetFields()
		var name string
		if len(f) == 1 {
			if s := f[0].GetString_(); s != nil {
				name = s.GetSval()
			}
		} else if hasStar(f) {
			// nested x.* inside an expression (ROW(x.*), (x).*) — composite
			// expansion of a witnessed source can't be safely stripped
			name = starQualifierName(f)
		}
		if name != "" {
			if it, hit := byAlias[name]; hit && it.entity == nil && hasWitnessCols(it.layout) {
				err = &RejectError{Code: "query_reference_not_witnessable",
					Message: fmt.Sprintf("%v: whole-row composite of witnessed source", ErrNotWitnessable)}
				return false
			}
		}
		return true
	})
	return err
}

// checkOrdinals rejects GROUP/ORDER ordinals outside the original public
// column count — otherwise an originally-invalid ordinal would silently bind
// an appended witness column.
func (in *injector) checkOrdinals(sel *pg.SelectStmt, publicCount int) {
	check := func(n *pg.Node) bool {
		v, ok := ordinalConst(n)
		return !ok || v <= int64(publicCount)
	}
	for _, g := range sel.GetGroupClause() {
		if !check(g) {
			in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: GROUP BY ordinal out of range", ErrLayoutUnfreezable)}
			return
		}
	}
	for _, s := range sel.GetSortClause() {
		if sb := s.GetSortBy(); sb != nil && !check(sb.GetNode()) {
			in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: ORDER BY ordinal out of range", ErrLayoutUnfreezable)}
			return
		}
	}
}

// ---------- witness emission ----------

// emitItemWitnesses appends this layer's witness targets for one top-level
// FROM item: a fresh witness per entity LEAF plus propagation of every
// witness leaf items already carry (per the receiving layer's shape).
func (in *injector) emitItemWitnesses(sel *pg.SelectStmt, it *fromItemRef, aggregate bool, taken map[string]bool, emitted *[]outCol) {
	for _, leaf := range flattenLeaves(it) {
		if in.err != nil {
			return
		}
		if leaf.entity != nil {
			name := in.witnessName(taken)
			sel.TargetList = append(sel.GetTargetList(),
				targetNode(in.newWitnessExpr(leaf.alias, aggregate), name))
			*emitted = append(*emitted, outCol{name: name, witness: true, entity: leaf.entity, src: leaf.alias})
			continue
		}
		for _, c := range leaf.layout.cols {
			if !c.witness {
				continue
			}
			name := in.witnessName(taken)
			sel.TargetList = append(sel.GetTargetList(),
				targetNode(in.propagateExpr(leaf.alias, c.name, aggregate), name))
			cc := c
			cc.src = leaf.alias
			*emitted = append(*emitted, outCol{name: name, witness: true, entity: cc.entity, src: leaf.alias})
		}
	}
}

// flattenLeaves yields the leaf items of a FROM item (joins recurse).
func flattenLeaves(it *fromItemRef) []*fromItemRef {
	if it == nil {
		return nil
	}
	if len(it.children) == 0 {
		return []*fromItemRef{it}
	}
	var out []*fromItemRef
	for _, ch := range it.children {
		out = append(out, flattenLeaves(ch)...)
	}
	return out
}

// newWitnessExpr builds the layer-shape-appropriate witness expression.
func (in *injector) newWitnessExpr(alias string, aggregate bool) *pg.Node {
	rowRef := colRefNode(alias, "*")
	if !aggregate {
		return &pg.Node{Node: &pg.Node_CaseExpr{CaseExpr: &pg.CaseExpr{
			Args: []*pg.Node{{Node: &pg.Node_CaseWhen{CaseWhen: &pg.CaseWhen{
				Expr:   boolConst(false),
				Result: rowRef,
			}}}},
		}}}
	}
	return filterAggSubscript(rowRef)
}

func (in *injector) propagateExpr(alias, col string, aggregate bool) *pg.Node {
	ref := colRefNode(alias, col)
	if !aggregate {
		return ref
	}
	return filterAggSubscript(ref)
}

// filterAggSubscript wraps an expression as
// (pg_catalog.array_agg(<expr>) FILTER (WHERE FALSE))[1] — always NULL, typed
// as the wrapped expression's element type (the bound reltype).
func filterAggSubscript(inner *pg.Node) *pg.Node {
	agg := &pg.Node{Node: &pg.Node_FuncCall{FuncCall: &pg.FuncCall{
		Funcname:  []*pg.Node{strNode("pg_catalog"), strNode("array_agg")},
		Args:      []*pg.Node{inner},
		AggFilter: boolConst(false),
	}}}
	return &pg.Node{Node: &pg.Node_AIndirection{AIndirection: &pg.A_Indirection{
		Arg: agg,
		Indirection: []*pg.Node{{Node: &pg.Node_AIndices{AIndices: &pg.A_Indices{
			Uidx: intConst32(1),
		}}}},
	}}}
}

// cloneLayoutRenamed returns a copy of l with the first len(colnames) public
// columns renamed positionally (WITH c(a,b) semantics: a shorter list renames
// a prefix, trailing names survive).
func cloneLayoutRenamed(l *itemLayout, colnames []*pg.Node) *itemLayout {
	out := &itemLayout{opaque: l.opaque, cols: append([]outCol(nil), l.cols...)}
	pub := 0
	for i := range out.cols {
		if out.cols[i].witness {
			continue
		}
		if pub < len(colnames) {
			out.cols[i].name = colnames[pub].GetString_().GetSval()
		}
		pub++
	}
	return out
}

// processSublinks rewrites subquery expressions hanging off this layer
// (predicates, target expressions, join quals, sort/window keys, VALUES
// rows). Their bodies are processed with witnesses suppressed — a sublink
// can never carry a witness outward, but `*` inside it must still be frozen
// so injected inner columns never leak into its row width.
func (in *injector) processSublinks(sel *pg.SelectStmt, layouts map[string]*itemLayout) {
	var exprs []*pg.Node
	collect := func(n *pg.Node) {
		if n != nil {
			exprs = append(exprs, n)
		}
	}
	for _, t := range sel.GetTargetList() {
		collect(t.GetResTarget().GetVal())
	}
	collect(sel.GetWhereClause())
	collect(sel.GetHavingClause())
	collect(sel.GetLimitOffset())
	collect(sel.GetLimitCount())
	for _, s := range sel.GetSortClause() {
		collect(s.GetSortBy().GetNode())
	}
	for _, d := range sel.GetDistinctClause() {
		collect(d)
	}
	for _, w := range sel.GetWindowClause() {
		wd := w.GetWindowDef()
		for _, p := range wd.GetPartitionClause() {
			collect(p)
		}
		for _, g := range wd.GetOrderClause() {
			collect(g.GetSortBy().GetNode())
		}
		collect(wd.GetStartOffset())
		collect(wd.GetEndOffset())
	}
	for _, vl := range sel.GetValuesLists() {
		for _, e := range vl.GetList().GetItems() {
			collect(e)
		}
	}
	var walkFrom func(n *pg.Node)
	walkFrom = func(n *pg.Node) {
		if n == nil {
			return
		}
		if j := n.GetJoinExpr(); j != nil {
			collect(j.GetQuals())
			walkFrom(j.GetLarg())
			walkFrom(j.GetRarg())
			return
		}
		if rf := n.GetRangeFunction(); rf != nil {
			for _, f := range rf.GetFunctions() {
				for _, arg := range f.GetList().GetItems() {
					collect(arg)
				}
			}
		}
	}
	for _, f := range sel.GetFromClause() {
		walkFrom(f)
	}
	for _, e := range exprs {
		in.sublinksIn(e, layouts)
		if in.err != nil {
			return
		}
	}
}

// sublinksIn finds every SubLink below an expression root and processes its
// subselect with witness emission suppressed.
func (in *injector) sublinksIn(root *pg.Node, layouts map[string]*itemLayout) {
	walkTree(msgOf(root), func(_ *walkCtx, m protoreflect.Message) bool {
		if in.err != nil {
			return false
		}
		n, ok := m.Interface().(*pg.Node)
		if !ok {
			return true
		}
		sl := n.GetSubLink()
		if sl == nil {
			return true
		}
		// testexpr can itself contain sublinks (e.g. `(sub) IN (sub)`).
		if te := sl.GetTestexpr(); te != nil {
			in.sublinksIn(te, layouts)
		}
		if sub := sl.GetSubselect().GetSelectStmt(); sub != nil {
			rep, _ := in.selectStmt(sub, layouts, false)
			if rep != sub {
				sl.Subselect.Node = &pg.Node_SelectStmt{SelectStmt: rep}
			}
		}
		return false // nested sublinks are reached by the recursive calls
	})
}

// ---------- DISTINCT → Q + D ----------

// wrapDistinct turns a plain-DISTINCT layer (already frozen + injected per
// its own shape) into the independent dedup layer D. D wraps Q in a derived
// table with a positional internal column list, groups ONLY by the original
// user columns (never global aggregation → empty input yields zero rows),
// carries witnesses via the aggregate template, restores original public
// names (duplicates included), and receives ORDER BY/LIMIT/OFFSET once.
func (in *injector) wrapDistinct(q *pg.SelectStmt, publics []string, pubStarts []int, emitted []outCol) (*pg.SelectStmt, *itemLayout) {
	sortKeys := q.GetSortClause()
	limitCount, limitOffset := q.GetLimitCount(), q.GetLimitOffset()
	limitOption := q.GetLimitOption()

	// Original public target expressions — for rebinding non-positional
	// ORDER BY keys. Witness targets always sit at the tail, so the matchable
	// prefix is exactly the non-witness slice (a retained `x.*` node simply
	// never proto-equals an ORDER key). Positional/name keys bind against
	// `publics` (the expanded column list), expression keys against these.
	targets := q.GetTargetList()
	var targetExprs []*pg.Node
	var exprPublicIdx []int
	for i, t := range targets[:len(targets)-len(emitted)] {
		targetExprs = append(targetExprs, t.GetResTarget().GetVal())
		exprPublicIdx = append(exprPublicIdx, pubStarts[i])
	}

	q.DistinctClause = nil
	q.SortClause = nil
	q.LimitCount = nil
	q.LimitOffset = nil
	q.LimitOption = 0 // UNDEFINED — the flag moves to D with the values

	colNames := make([]*pg.Node, 0, len(publics)+len(emitted))
	userAlias := make([]string, 0, len(publics))
	for i := range publics {
		userAlias = append(userAlias, fmt.Sprintf("__chub_u%d", i+1))
		colNames = append(colNames, strNode(userAlias[i]))
	}
	for _, w := range emitted {
		colNames = append(colNames, strNode(w.name))
	}

	// Q is a fresh node — the caller substitutes D for q in its parent edge.
	d := &pg.SelectStmt{
		Op: pg.SetOperation_SETOP_NONE, // proto3 zero is UNDEFINED, not NONE
		FromClause: []*pg.Node{{Node: &pg.Node_RangeSubselect{RangeSubselect: &pg.RangeSubselect{
			Subquery: &pg.Node{Node: &pg.Node_SelectStmt{SelectStmt: q}},
			Alias:    &pg.Alias{Aliasname: "__chub_q", Colnames: colNames},
		}}}},
		LimitCount:  limitCount,
		LimitOffset: limitOffset,
		LimitOption: limitOption, // LIMIT/FETCH/WITH-TIES flag rides with it
	}
	var newEmitted []outCol
	for i, name := range publics {
		d.TargetList = append(d.TargetList, targetNode(colRefNode("__chub_q", userAlias[i]), name))
		d.GroupClause = append(d.GroupClause, colRefNode("__chub_q", userAlias[i]))
	}
	taken := map[string]bool{}
	for _, p := range publics {
		taken[p] = true
	}
	for _, w := range emitted {
		fresh := in.witnessName(taken)
		d.TargetList = append(d.TargetList, targetNode(
			filterAggSubscript(colRefNode("__chub_q", w.name)), fresh))
		newEmitted = append(newEmitted, outCol{name: fresh, witness: true, entity: w.entity, src: w.src})
	}
	// Retained `x.*` targets: map each alias's expanded columns to their
	// public positions so a qualified ORDER BY key (o.id) rebinds to the
	// position its star expansion produced in Q.
	stars := map[string]map[string]int{}
	for i, t := range targetExprs {
		if cr := t.GetColumnRef(); cr != nil && hasStar(cr.GetFields()) && len(cr.GetFields()) == 2 {
			if s := cr.GetFields()[0].GetString_(); s != nil {
				end := len(publics)
				if i+1 < len(pubStarts) {
					end = pubStarts[i+1]
				}
				m := map[string]int{}
				for j := pubStarts[i]; j < end && j < len(publics); j++ {
					m[publics[j]] = j
				}
				stars[s.GetSval()] = m
			}
		}
	}
	for _, sk := range sortKeys {
		if sb := sk.GetSortBy(); sb != nil {
			key := sb.GetNode()
			if idx := rebindSortKey(key, publics, targetExprs, exprPublicIdx, stars); idx >= 0 {
				sb.Node = intConst(int64(idx + 1))
			} else if containsQualifiedRef(key) {
				// An unrebound key that still names an inner alias (o.id)
				// would parse against D but fail at name resolution — D's
				// FROM holds only __chub_q. Reject rather than emit
				// unrunnable SQL.
				in.err = &RejectError{Code: "query_not_allowed",
					Message: fmt.Sprintf("%v: DISTINCT ORDER BY key not resolvable in dedup layer", ErrLayoutUnfreezable)}
				return nil, nil
			}
		}
		d.SortClause = append(d.SortClause, sk)
	}
	out := &itemLayout{}
	for _, n := range publics {
		out.cols = append(out.cols, outCol{name: n})
	}
	out.cols = append(out.cols, newEmitted...)
	return d, out
}

// rebindSortKey maps an original ORDER BY key to the 0-based user-column
// index it denotes, or -1 when it matches nothing (already-invalid queries
// keep their verbatim key and still fail against the internal columns).
func rebindSortKey(key *pg.Node, publics []string, exprs []*pg.Node, exprPublicIdx []int, stars map[string]map[string]int) int {
	if n, ok := ordinalConst(key); ok {
		if n >= 1 && n <= int64(len(publics)) {
			return int(n - 1)
		}
		return -1
	}
	if cr := key.GetColumnRef(); cr != nil {
		f := cr.GetFields()
		if len(f) == 1 {
			if s := f[0].GetString_(); s != nil {
				for i, name := range publics {
					if name == s.GetSval() {
						return i
					}
				}
			}
		}
		if len(f) == 2 {
			// `alias.col` — bind through a retained `alias.*` expansion; the
			// named column is exactly what the original target projected.
			if a := f[0].GetString_(); a != nil {
				if c := f[1].GetString_(); c != nil {
					if m, ok := stars[a.GetSval()]; ok {
						if idx, ok := m[c.GetSval()]; ok {
							return idx
						}
					}
				}
			}
		}
	}
	for i, e := range exprs {
		if protoEqualIgnoringLocations(key, e) {
			return exprPublicIdx[i]
		}
	}
	return -1
}

// ---------- classification helpers ----------

// isPlainDistinct reports `SELECT DISTINCT` (empty placeholder nodes) vs
// DISTINCT ON (real expression nodes) or no distinctness.
func isPlainDistinct(sel *pg.SelectStmt) bool {
	dc := sel.GetDistinctClause()
	if len(dc) == 0 {
		return false
	}
	for _, n := range dc {
		if n.GetNode() != nil {
			return false
		}
	}
	return true
}

// isAggregateLayer picks the witness template: GROUP BY / GROUPING SETS /
// HAVING / aggregate calls make the layer aggregate; window-only stays
// ordinary (an aggregate witness there would force single-row grouping).
func isAggregateLayer(sel *pg.SelectStmt) bool {
	if len(sel.GetGroupClause()) > 0 || sel.GetHavingClause() != nil {
		return true
	}
	// Aggregates anywhere at THIS level collapse the layer's rows — not just
	// the target list: ORDER BY and named-window definitions evaluate at the
	// same scope. SubLink bodies carry their own scope and never count;
	// FuncCall.over is a window function, not a plain aggregate.
	var roots []*pg.Node
	roots = append(roots, sel.GetTargetList()...)
	roots = append(roots, sel.GetSortClause()...)
	roots = append(roots, sel.GetWindowClause()...)
	roots = append(roots, sel.GetDistinctClause()...)
	for _, t := range roots {
		found := false
		walkTree(msgOf(t), func(ctx *walkCtx, m protoreflect.Message) bool {
			if found {
				return false
			}
			// A SubLink's subquery has its own aggregation scope — aggregates
			// inside it never make THIS layer an aggregate layer.
			if _, isSub := m.Interface().(*pg.SubLink); isSub {
				return false
			}
			if fc, ok := m.Interface().(*pg.FuncCall); ok {
				if fc.GetOver() == nil && isAggregateFunc(fc) {
					found = true
					return false
				}
				// fc.over != nil → window function; keep descending — an
				// aggregate inside window args still groups this layer.
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// isAggregateFunc recognizes aggregate calls in parse trees — either by
// aggregate-only structure (FILTER/WITHIN GROUP/DISTINCT/star) or by builtin
// name on the unqualified final segment.
func isAggregateFunc(fc *pg.FuncCall) bool {
	if fc.GetAggFilter() != nil || len(fc.GetAggOrder()) > 0 || fc.GetAggStar() || fc.GetAggDistinct() || fc.GetAggWithinGroup() {
		return true
	}
	_, ok := builtinAggregates[funcName(fc)]
	return ok
}

var builtinAggregates = map[string]struct{}{
	"count": {}, "sum": {}, "avg": {}, "min": {}, "max": {},
	"array_agg": {}, "string_agg": {}, "json_agg": {}, "jsonb_agg": {},
	"json_object_agg": {}, "jsonb_object_agg": {},
	"bit_and": {}, "bit_or": {}, "bit_xor": {}, "bool_and": {}, "bool_or": {},
	"every": {}, "variance": {}, "var_samp": {}, "var_pop": {},
	"stddev": {}, "stddev_samp": {}, "stddev_pop": {},
	"corr": {}, "covar_pop": {}, "covar_samp": {},
	"regr_avgx": {}, "regr_avgy": {}, "regr_count": {}, "regr_intercept": {},
	"regr_r2": {}, "regr_slope": {}, "regr_sxx": {}, "regr_sxy": {}, "regr_syy": {},
	"percentile_cont": {}, "percentile_disc": {}, "mode": {},
	"grouping": {}, "xmlagg": {},
}

// checkInternalNames rejects user-visible names inside the reserved internal
// namespace — output names, CTE names, and FROM aliases alike.
func (in *injector) checkInternalNames(sel *pg.SelectStmt) {
	bad := func(name string) bool { return strings.HasPrefix(name, internalPrefix) }
	for _, t := range sel.GetTargetList() {
		if rt := t.GetResTarget(); rt != nil && bad(rt.GetName()) {
			in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: reserved internal column name", ErrNotReadOnly)}
			return
		}
	}
	if wc := sel.GetWithClause(); wc != nil {
		for _, n := range wc.GetCtes() {
			if c := n.GetCommonTableExpr(); c != nil && bad(c.GetCtename()) {
				in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: reserved internal CTE name", ErrNotReadOnly)}
				return
			}
		}
	}
	var walkItems func(n *pg.Node)
	walkItems = func(n *pg.Node) {
		if in.err != nil || n == nil {
			return
		}
		if bad(aliasOf(n)) {
			in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: reserved internal alias", ErrNotReadOnly)}
			return
		}
		// An unaliased relation's effective RTE alias is its relname.
		if rv := n.GetRangeVar(); rv != nil && rv.GetAlias() == nil && bad(rv.GetRelname()) {
			in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: reserved internal alias", ErrNotReadOnly)}
			return
		}
		if j := n.GetJoinExpr(); j != nil {
			walkItems(j.GetLarg())
			walkItems(j.GetRarg())
		}
	}
	for _, f := range sel.GetFromClause() {
		walkItems(f)
	}
}

// containsQualifiedRef reports whether an expression references a qualified
// (alias.name) column — i.e. an inner-scope RTE name the D layer cannot see.
func containsQualifiedRef(n *pg.Node) bool {
	found := false
	walkTree(msgOf(n), func(_ *walkCtx, m protoreflect.Message) bool {
		if found {
			return false
		}
		if cr, ok := m.Interface().(*pg.ColumnRef); ok && len(cr.GetFields()) >= 2 && !hasStar(cr.GetFields()) {
			found = true
			return false
		}
		return true
	})
	return found
}

// ---------- node builders & small helpers ----------

func strNode(s string) *pg.Node {
	return &pg.Node{Node: &pg.Node_String_{String_: &pg.String{Sval: s}}}
}

func intConst(n int64) *pg.Node {
	return &pg.Node{Node: &pg.Node_AConst{AConst: &pg.A_Const{
		Val: &pg.A_Const_Ival{Ival: &pg.Integer{Ival: int32(n)}},
	}}}
}

func intConst32(n int32) *pg.Node {
	return &pg.Node{Node: &pg.Node_AConst{AConst: &pg.A_Const{
		Val: &pg.A_Const_Ival{Ival: &pg.Integer{Ival: n}},
	}}}
}

func boolConst(b bool) *pg.Node {
	return &pg.Node{Node: &pg.Node_AConst{AConst: &pg.A_Const{
		Val: &pg.A_Const_Boolval{Boolval: &pg.Boolean{Boolval: b}},
	}}}
}

// colRefNode builds a ColumnRef; col "*" yields the x.* form.
func colRefNode(names ...string) *pg.Node {
	f := make([]*pg.Node, 0, len(names))
	for _, n := range names {
		if n == "*" {
			f = append(f, &pg.Node{Node: &pg.Node_AStar{AStar: &pg.A_Star{}}})
		} else {
			f = append(f, strNode(n))
		}
	}
	return &pg.Node{Node: &pg.Node_ColumnRef{ColumnRef: &pg.ColumnRef{Fields: f}}}
}

// targetNode builds `expr` or `expr AS name`.
func targetNode(val *pg.Node, name string) *pg.Node {
	rt := &pg.ResTarget{Val: val}
	if name != "" {
		rt.Name = name
	}
	return &pg.Node{Node: &pg.Node_ResTarget{ResTarget: rt}}
}

func hasStar(fields []*pg.Node) bool {
	for _, f := range fields {
		if f.GetAStar() != nil {
			return true
		}
	}
	return false
}

// starQualifierName returns the qualifier of `x.*` — "" for multi-part
// (schema.table.*) qualifiers, which can only address real relations.
func starQualifierName(fields []*pg.Node) string {
	if len(fields) != 2 {
		return ""
	}
	if s := fields[0].GetString_(); s != nil {
		return s.GetSval()
	}
	return ""
}

func aliasOf(n *pg.Node) string {
	if n == nil {
		return ""
	}
	switch v := n.GetNode().(type) {
	case *pg.Node_RangeVar:
		if a := v.RangeVar.GetAlias(); a != nil {
			return a.GetAliasname()
		}
	case *pg.Node_RangeSubselect:
		if a := v.RangeSubselect.GetAlias(); a != nil {
			return a.GetAliasname()
		}
	case *pg.Node_RangeFunction:
		if a := v.RangeFunction.GetAlias(); a != nil {
			return a.GetAliasname()
		}
	case *pg.Node_RangeTableFunc:
		if a := v.RangeTableFunc.GetAlias(); a != nil {
			return a.GetAliasname()
		}
	}
	return ""
}

func aliasName(a *pg.Alias) string {
	if a == nil {
		return ""
	}
	return a.GetAliasname()
}

func hasWitnessCols(l *itemLayout) bool {
	if l == nil {
		return false
	}
	for _, c := range l.cols {
		if c.witness {
			return true
		}
	}
	return false
}

func ordinalConst(n *pg.Node) (int64, bool) {
	if n == nil {
		return 0, false
	}
	if ac := n.GetAConst(); ac != nil {
		if iv := ac.GetIval(); iv != nil {
			return int64(iv.GetIval()), true
		}
	}
	return 0, false
}

// protoEqualIgnoringLocations compares two expression nodes ignoring every
// location scalar — ORDER BY keys must match a target expression's
// structure, not its byte position.
func protoEqualIgnoringLocations(a, b *pg.Node) bool {
	ca, ok1 := proto.Clone(a).(*pg.Node)
	cb, ok2 := proto.Clone(b).(*pg.Node)
	if !ok1 || !ok2 {
		return false
	}
	zeroLocations(msgOf(ca))
	zeroLocations(msgOf(cb))
	return proto.Equal(ca, cb)
}

func zeroLocations(m protoreflect.Message) {
	if m == nil || !m.IsValid() {
		return
	}
	md := m.Descriptor()
	for i := 0; i < md.Fields().Len(); i++ {
		fd := md.Fields().Get(i)
		switch fd.Name() {
		case "location", "stmt_location", "stmt_len", "expr_location", "expr_len":
			// Position metadata differs between the SELECT target and its
			// ORDER BY twin — clear, don't skip: a set field still compares.
			m.Clear(fd)
			continue
		}
		if fd.Kind() != protoreflect.MessageKind {
			continue
		}
		if fd.IsList() {
			l := m.Get(fd).List()
			for j := 0; j < l.Len(); j++ {
				zeroLocations(l.Get(j).Message())
			}
		} else if m.Has(fd) {
			zeroLocations(m.Get(fd).Message())
		}
	}
}
