// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: qualified *pg.ParseResult, ColumnResolver supplying FROM-position column names (optional ColumnMetadataResolver for structured type/attribute evidence and native common-type verdicts)
// output: InjectWitnesses, InjectResult, WitnessRecord, EntityKey, ColumnResolver, ColumnType, ColumnMetadata, ColumnMetadataResolver, ErrTypeResolutionUnavailable — layout-freeze + per-layer witness injection + DISTINCT Q+D transform + public-column proofs/source catalog
// pos: G10 mechanism 5a — records the original visible layout (* / x.* / ordinals / VALUES / per-reference colnames — NATURAL always freezes to the original public-column intersection: explicit USING when nonempty so the join_using_alias stays declarable, ON TRUE keeping the join type when empty) BEFORE appending witnesses; stars stay verbatim unless an injected column forces expansion, and forced USING/NATURAL merges are projected through the join's join_using_alias (generated under the __chub_ prefix when absent) so PostgreSQL computes the merged column natively — common-type coercion included; emits CASE WHEN FALSE THEN alias.* END on ordinary/window-only layers and (array_agg(alias.*) FILTER (WHERE FALSE))[1] on aggregate/grouping layers; propagates through CTEs and derived tables, suppresses inside SubLinks, merges set-op branches through canonical representatives only when attested entities match, and rejects when any entity's witness cannot reach the output; plain SELECT DISTINCT becomes Q+D (D groups by original public columns only and receives ORDER BY/LIMIT/OFFSET exactly once; a decided merged column whose only source reference is itself is a terminal computed JOIN identity and binds to itself); internal names live under the reserved __chub_ prefix; provenance resolution runs through parent-linked FROM scopes (inner shadows outer, LATERAL sees only preceding same-level items plus enclosing levels), VALUES cells analyze every row per output position, named windows resolve through the layer's own WINDOW clause, and ORDER BY/GROUP BY keys bind per PostgreSQL SQL92 output-name/input-column precedence — cyclic or ambiguous bindings fail closed as unresolved
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

type ColumnType struct {
	OID    uint32
	Typmod int32
}

type ColumnMetadata struct {
	Name            string
	Type            ColumnType
	RelationOID     uint32
	AttributeNumber int16
}

type ColumnMetadataResolver interface {
	ColumnMetadata(schema, name string) ([]ColumnMetadata, error)
	CommonType(leftOID, rightOID uint32) (uint32, error)
}

var ErrTypeResolutionUnavailable = errors.New("pgsql: type resolution metadata unavailable")

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

// WitnessRecord maps one top-level-output witness column to the original
// entity occurrences it covers and the current carrier it binds. The executor
// compares each witness FieldDescription.DataTypeOID against every covered
// entity's bound reltype.
type WitnessRecord struct {
	Column            string
	Entity            EntityKey
	Alias             string
	CoveredSources    []SourceOccurrenceID
	CarrierOccurrence SourceOccurrenceID
}

// InjectResult is the rewritten statement plus its injection record.
type InjectResult struct {
	Tree      *pg.ParseResult
	Witnesses []WitnessRecord // top-level witness columns, in output order
	// WitnessPositions are the zero-based output column indexes that carry
	// witnesses — the executor strips them by position, never by name.
	WitnessPositions []int
	Public           []string            // user-visible output column names
	PublicProofs     []PublicColumnProof // PublicProofs[i] describes Public[i]
	SourceCatalog    map[SourceOccurrenceID]SourceOccurrence
}

// InjectWitnesses freezes the visible layout and injects witnesses through
// every SelectStmt layer of an already-qualified parse tree.
func InjectWitnesses(tree *pg.ParseResult, res ColumnResolver) (*InjectResult, error) {
	refs, byNode, err := collectRangeVarRefs(tree)
	if err != nil {
		return nil, err
	}
	return injectWitnesses(tree, res, refs, byNode)
}

func injectWitnesses(tree *pg.ParseResult, res ColumnResolver, refs []RangeVarRef, byNode map[*pg.RangeVar]RefID) (*InjectResult, error) {
	if tree == nil || len(tree.GetStmts()) != 1 {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v", ErrNotReadOnly)}
	}
	sel := tree.GetStmts()[0].GetStmt().GetSelectStmt()
	if sel == nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v", ErrNotReadOnly)}
	}
	in := &injector{
		res:        res,
		metaCache:  map[*pg.RangeVar]*entityMeta{},
		occByNode:  byNode,
		sources:    map[SourceOccurrenceID]SourceOccurrence{},
		synthID:    -1,
		subLayouts: map[*pg.SelectStmt]*itemLayout{},
	}
	for _, ref := range refs {
		occ := SourceOccurrenceID(ref.RefID)
		kind := SourceEntityRangeVar
		if ref.Kind == RefCTE {
			kind = SourceCTERangeVar
		}
		in.sources[occ] = SourceOccurrence{ID: occ, Kind: kind, RefID: ref.RefID, HasRefID: true}
	}
	rep, layout := in.selectStmt(sel, map[string]*itemLayout{}, true, nil)
	if in.err != nil {
		return nil, in.err
	}
	if rep != sel {
		tree.GetStmts()[0].GetStmt().Node = &pg.Node_SelectStmt{SelectStmt: rep}
	}
	out := &InjectResult{Tree: tree, SourceCatalog: in.sources}
	covered := map[SourceOccurrenceID]bool{}
	for i, c := range layout.cols {
		if c.witness {
			if len(c.coveredSources) == 0 || !c.carrierSet {
				return nil, fmt.Errorf("%w: final witness column %q lacks source identity", ErrEvidenceUnavailable, c.name)
			}
			out.Witnesses = append(out.Witnesses, WitnessRecord{
				Column: c.name, Entity: *c.entity, Alias: c.src,
				CoveredSources:    append([]SourceOccurrenceID(nil), c.coveredSources...),
				CarrierOccurrence: c.carrierOccurrence,
			})
			out.WitnessPositions = append(out.WitnessPositions, i)
			for _, src := range c.coveredSources {
				covered[src] = true
			}
		} else {
			out.Public = append(out.Public, c.name)
			out.PublicProofs = append(out.PublicProofs, PublicColumnProof{
				TransportIndex: i,
				Resolution:     c.proofResolution(),
				Dependencies:   append([]SourceRef(nil), c.deps...),
				FDOrigin:       c.fdOrigin,
			})
		}
	}
	// Coverage: every original entity occurrence must be covered by at least
	// one final witness. A CTE/carrier occurrence alone does not count.
	for _, leaf := range in.leaves {
		if !covered[leaf] {
			return nil, &RejectError{Code: "query_reference_not_witnessable",
				Message: fmt.Sprintf("%v: witness for entity never reaches the output", ErrNotWitnessable)}
		}
	}
	if err := out.validate(in.leaves); err != nil {
		return nil, err
	}
	return out, nil
}

// ---------- layout machinery ----------

// outCol is one column in a FROM item's or SELECT layer's output.
type outCol struct {
	name string
	src  string // qualifier an emitted reference to this column routes through (item alias or join_using_alias); "" = unqualified — never provenance
	// every scope-local spelling the ORIGINAL output item was legally
	// nameable through — provenance for ORDER BY rebinding, never SQL
	refs              []colRef
	typ               ColumnType
	relOID            uint32
	attNum            int16
	mergeUndecided    bool
	candidates        []colRef
	witness           bool
	entity            *EntityKey // set when witness
	deps              []SourceRef
	depsComplete      bool
	fdOrigin          FDOrigin
	coveredSources    []SourceOccurrenceID
	carrierOccurrence SourceOccurrenceID
	carrierSet        bool
}

func (c *outCol) proofResolution() ProofResolution {
	if c == nil || !c.depsComplete || c.fdOrigin.Kind == FDOriginUnresolved {
		return ProofUnresolved
	}
	return ProofComplete
}

type colRef struct {
	item *fromItemRef
	ord  int
	qual string
	name string
}

// itemLayout describes a FROM item's (or SELECT layer's) output columns.
type itemLayout struct {
	cols   []outCol
	opaque bool // column set unknowable — `*` spanning it is unfreezable
	// allDeps is the complete terminal-source summary for this SELECT layer,
	// including WHERE/JOIN/sort expressions — used when a SubLink contributes
	// dependencies without becoming an outer FROM carrier.
	allDeps         []SourceRef
	allDepsComplete bool
}

// fromItemRef is one FROM-clause element plus children for join trees.
type fromItemRef struct {
	layout      *itemLayout
	alias       string
	entity      *EntityKey // non-nil for real relations
	occ         SourceOccurrenceID
	relOID      uint32 // bound relation OID when resolver metadata supplies it
	node        *pg.Node
	children    []*fromItemRef // join leaves
	usingAlias  string
	mergedCount int
}

type entityMeta struct {
	names []string
	metas []ColumnMetadata
}

type injector struct {
	res        ColumnResolver
	metaCache  map[*pg.RangeVar]*entityMeta
	occByNode  map[*pg.RangeVar]RefID
	sources    map[SourceOccurrenceID]SourceOccurrence
	synthID    SourceOccurrenceID
	subLayouts map[*pg.SelectStmt]*itemLayout
	wSeq       int
	synth      int
	err        error
	// leaves registers every original entity RangeVar occurrence that must be
	// covered by a final witness position.
	leaves []SourceOccurrenceID
}

func (in *injector) syntheticOccurrence(kind SourceOccurrenceKind) SourceOccurrenceID {
	id := in.synthID
	in.synthID--
	in.sources[id] = SourceOccurrence{ID: id, Kind: kind}
	return id
}

func (in *injector) occurrenceForNode(n *pg.Node) (SourceOccurrenceID, bool) {
	rv := n.GetRangeVar()
	if rv == nil {
		return 0, false
	}
	id, ok := in.occByNode[rv]
	return SourceOccurrenceID(id), ok
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

func resolverErr(kind string, rv *pg.RangeVar, err error) error {
	if errors.Is(err, ErrRelationNotFound) {
		return &RejectError{Code: "query_object_not_found",
			Message: fmt.Sprintf("relation %s.%s not found", rv.GetSchemaname(), rv.GetRelname())}
	}
	return fmt.Errorf("resolve %s for %s.%s: %w",
		kind, rv.GetSchemaname(), rv.GetRelname(), err)
}

// entityMetaFor resolves (cached) column names plus — when the resolver
// implements ColumnMetadataResolver — aligned structured metadata.
// Claimed metadata disagreeing with Columns or carrying zero identity is
// an internal resolver defect, not a query outcome.
func (in *injector) entityMetaFor(rv *pg.RangeVar) (*entityMeta, error) {
	if rv == nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: entity without relation node", ErrLayoutUnfreezable)}
	}
	if m, ok := in.metaCache[rv]; ok {
		return m, nil
	}
	if in.res == nil {
		return nil, &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: column metadata unavailable", ErrLayoutUnfreezable)}
	}
	names, err := in.res.Columns(rv.GetSchemaname(), rv.GetRelname())
	if err != nil {
		return nil, resolverErr("columns", rv, err)
	}
	m := &entityMeta{names: names}
	if mr, ok := in.res.(ColumnMetadataResolver); ok {
		metas, err := mr.ColumnMetadata(rv.GetSchemaname(), rv.GetRelname())
		if err != nil {
			return nil, resolverErr("column metadata", rv, err)
		}
		if len(metas) != len(names) {
			return nil, fmt.Errorf("column metadata for %s.%s: %d entries for %d columns: %w",
				rv.GetSchemaname(), rv.GetRelname(), len(metas), len(names), ErrTypeResolutionUnavailable)
		}
		for i, md := range metas {
			if md.Name != names[i] {
				return nil, fmt.Errorf("column metadata for %s.%s: position %d is %q, Columns says %q: %w",
					rv.GetSchemaname(), rv.GetRelname(), i, md.Name, names[i], ErrTypeResolutionUnavailable)
			}
			if md.Type.OID == 0 || md.RelationOID == 0 || md.AttributeNumber <= 0 {
				return nil, fmt.Errorf("column metadata for %s.%s.%s: incomplete identity: %w",
					rv.GetSchemaname(), rv.GetRelname(), md.Name, ErrTypeResolutionUnavailable)
			}
		}
		m.metas = metas
	}
	in.metaCache[rv] = m
	return m, nil
}

// materialize fills an entity item's layout from the resolver on demand,
// attaching whatever catalog evidence the resolver supplied.
func (in *injector) materialize(it *fromItemRef) {
	if in.err != nil || it == nil || it.entity == nil || !it.layout.opaque {
		return
	}
	m, err := in.entityMetaFor(it.node.GetRangeVar())
	if err != nil {
		in.err = err
		return
	}
	it.layout.cols = nil
	for i, n := range m.names {
		srcRef := SourceRef{Occurrence: it.occ, Slot: columnSlot(i)}
		c := outCol{name: n, src: it.alias,
			deps:         []SourceRef{srcRef},
			depsComplete: true,
			fdOrigin:     FDOrigin{Kind: FDOriginUnresolved},
		}
		if m.metas != nil {
			c.typ = m.metas[i].Type
			c.relOID = m.metas[i].RelationOID
			c.attNum = m.metas[i].AttributeNumber
			c.fdOrigin = FDOrigin{
				Kind:            FDOriginKnownColumn,
				Source:          srcRef,
				RelationOID:     m.metas[i].RelationOID,
				AttributeNumber: m.metas[i].AttributeNumber,
			}
			if i == 0 {
				it.relOID = m.metas[i].RelationOID
			}
		}
		it.layout.cols = append(it.layout.cols, c)
	}
	it.layout.opaque = false
}

func stampRefs(l *itemLayout, it *fromItemRef) {
	if l == nil {
		return
	}
	ord := 0
	for i := range l.cols {
		c := &l.cols[i]
		if c.witness {
			continue
		}
		c.refs = []colRef{{item: it, ord: ord, qual: it.alias, name: c.name}}
		c.mergeUndecided = false
		c.candidates = nil
		ord++
	}
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
//
// outerScope is the column scope of the immediately enclosing query level —
// the correlated-reference fallback for names the layer's own FROM items
// cannot resolve. nil marks the outermost analyzed level.
func (in *injector) selectStmt(sel *pg.SelectStmt, cteLayouts map[string]*itemLayout, emit bool, outerScope *fromScope) (*pg.SelectStmt, *itemLayout) {
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
					// CTE bodies see the visible CTE names but never the
					// enclosing query's columns — nil outer scope.
					var rep *pg.SelectStmt
					rep, body = in.selectStmt(sub, bodyLayouts, emit, nil)
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
		return sel, in.setOpLayer(sel, bodyLayouts, emit, outerScope)
	}

	var items []*fromItemRef
	for _, n := range sel.GetFromClause() {
		// items collected so far are the LATERAL-visible preceding items.
		items = append(items, in.fromItem(n, bodyLayouts, emit, items, outerScope))
	}
	if in.err != nil {
		return sel, nil
	}
	sc := buildScope(items)
	sc.parent = outerScope
	in.freezeTargets(sel, items)
	if in.err != nil {
		return sel, nil
	}
	dr := &depResolver{
		in: in, sc: sc, layouts: bodyLayouts, windows: windowIndex(sel),
		pubExpr: map[int]*pg.Node{}, done: map[int]bool{}, active: map[int]bool{},
	}
	pubCols, pubStarts, err := in.publicCols(sel, items, dr)
	if err != nil {
		in.err = err
		return sel, nil
	}
	var publics []string
	for _, c := range pubCols {
		publics = append(publics, c.name)
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
	// there keeps `*`/`x.*` honest against injected inner columns. They see
	// this layer's column scope for correlated references.
	in.processSublinks(sel, bodyLayouts, sc)
	if in.err != nil {
		return sel, nil
	}

	layout := &itemLayout{}
	layout.cols = append(cloneCols(pubCols), emitted...)
	in.fillLayerDeps(sel, dr, layout)
	if in.err != nil {
		return sel, nil
	}

	if isPlainDistinct(sel) {
		d, dLayout := in.wrapDistinct(sel, pubCols, pubStarts, emitted, sc)
		if dLayout != nil {
			dLayout.allDeps = append([]SourceRef(nil), layout.allDeps...)
			dLayout.allDepsComplete = layout.allDepsComplete
		}
		return d, dLayout
	}
	return sel, layout
}

// setOpLayer injects into UNION/INTERSECT/EXCEPT leaves and merges their
// layouts. Leaves can only reference CTEs/derived sources (the guard rejected
// direct entity refs); leaves may themselves propagate inner witnesses — the
// merge is valid only when every leaf attests the SAME entity per position.
func (in *injector) setOpLayer(sel *pg.SelectStmt, cteLayouts map[string]*itemLayout, emit bool, outerScope *fromScope) *itemLayout {
	lrep, l := in.selectStmt(sel.GetLarg(), cteLayouts, emit, outerScope)
	if lrep != sel.GetLarg() {
		sel.Larg = lrep
	}
	rrep, r := in.selectStmt(sel.GetRarg(), cteLayouts, emit, outerScope)
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
	lw, rw := witnessCols(l), witnessCols(r)
	if len(lw) != len(rw) {
		in.err = &RejectError{Code: "query_reference_not_witnessable",
			Message: fmt.Sprintf("%v: set-operation branches differ in attested sources", ErrNotWitnessable)}
		return nil
	}
	for i := range lw {
		if *lw[i].entity != *rw[i].entity {
			in.err = &RejectError{Code: "query_reference_not_witnessable",
				Message: fmt.Sprintf("%v: set-operation merges different attested sources", ErrNotWitnessable)}
			return nil
		}
	}
	if in.err != nil {
		return nil
	}
	setOpCarrier := in.syntheticOccurrence(SourceGeneratedCarrier)
	out := &itemLayout{opaque: l.opaque, cols: cloneCols(l.cols)}
	rp := publicColsOf(r)
	pub := 0
	wit := 0
	for i := range out.cols {
		if out.cols[i].witness {
			if wit < len(rw) {
				out.cols[i].coveredSources = sortSourceIDs(unionSourceOccurrences(out.cols[i].coveredSources, rw[wit].coveredSources))
				out.cols[i].carrierOccurrence = setOpCarrier
				out.cols[i].carrierSet = true
			}
			wit++
			continue
		}
		out.cols[i].refs = nil
		out.cols[i].mergeUndecided = false
		out.cols[i].candidates = nil
		out.cols[i].relOID = 0
		out.cols[i].attNum = 0
		if pub < len(rp) {
			out.cols[i].deps = sortSourceRefs(unionSourceRefs(out.cols[i].deps, rp[pub].deps))
			out.cols[i].depsComplete = out.cols[i].depsComplete && rp[pub].depsComplete
			if out.cols[i].depsComplete {
				out.cols[i].fdOrigin = FDOrigin{Kind: FDOriginNoColumnOrigin}
			} else {
				out.cols[i].fdOrigin = FDOrigin{Kind: FDOriginUnresolved}
			}
		} else {
			out.cols[i].depsComplete = false
			out.cols[i].fdOrigin = FDOrigin{Kind: FDOriginUnresolved}
		}
		lt := out.cols[i].typ
		out.cols[i].typ = ColumnType{}
		if pub < len(rp) && lt.OID != 0 && rp[pub].typ.OID != 0 {
			rt := rp[pub].typ
			common := lt.OID
			if lt.OID != rt.OID {
				if mr, ok := in.res.(ColumnMetadataResolver); ok {
					co, err := mr.CommonType(lt.OID, rt.OID)
					if err != nil {
						in.err = fmt.Errorf("resolve set-op common type of %d/%d: %w", lt.OID, rt.OID, err)
						return nil
					}
					if co == 0 {
						in.err = fmt.Errorf("set-op common type of %d/%d: provider returned no verdict: %w", lt.OID, rt.OID, ErrTypeResolutionUnavailable)
						return nil
					}
					common = co
				} else {
					common = 0
				}
			}
			if common != 0 {
				tm := int32(-1)
				if lt.OID == common && rt.OID == common && lt.Typmod == rt.Typmod {
					tm = lt.Typmod
				}
				out.cols[i].typ = ColumnType{OID: common, Typmod: tm}
			}
		}
		pub++
	}
	out.allDeps = sortSourceRefs(unionSourceRefs(l.allDeps, r.allDeps))
	out.allDepsComplete = l.allDepsComplete && r.allDepsComplete
	return out
}

func publicColsOf(l *itemLayout) []*outCol {
	var out []*outCol
	for i := range l.cols {
		if !l.cols[i].witness {
			out = append(out, &l.cols[i])
		}
	}
	return out
}

func witnessCols(l *itemLayout) []*outCol {
	var out []*outCol
	for i := range l.cols {
		if l.cols[i].witness {
			out = append(out, &l.cols[i])
		}
	}
	return out
}

// ---------- FROM items ----------

// fromItem builds one FROM element. preceding lists the FROM items already
// built in the same clause — visible to this item only under LATERAL;
// outerScope is the enclosing query level's scope, which every nested
// subquery may correlate against regardless of LATERAL.
func (in *injector) fromItem(n *pg.Node, cteLayouts map[string]*itemLayout, emit bool, preceding []*fromItemRef, outerScope *fromScope) *fromItemRef {
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
		occ, ok := in.occurrenceForNode(n)
		if !ok {
			in.err = fmt.Errorf("%w: RangeVar occurrence lacks RefID mapping", ErrEvidenceUnavailable)
			return nil
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
				it := &fromItemRef{layout: layoutWithAlias(cl, alias), alias: alias, occ: occ, node: n}
				stampRefs(it.layout, it)
				return it
			}
		}
		it := &fromItemRef{
			layout: &itemLayout{opaque: true},
			alias:  alias,
			entity: &EntityKey{Schema: rv.GetSchemaname(), Name: rv.GetRelname()},
			occ:    occ,
			node:   n,
		}
		in.leaves = append(in.leaves, occ)
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
		stampRefs(it.layout, it)
		return it
	case *pg.Node_RangeSubselect:
		sub := v.RangeSubselect
		occ := in.syntheticOccurrence(SourceDerived)
		var layout *itemLayout
		if inner := sub.GetSubquery().GetSelectStmt(); inner != nil {
			// Without LATERAL the item sees the enclosing query levels but
			// no same-level siblings; LATERAL adds the preceding FROM items.
			visible := outerScope
			if sub.GetLateral() {
				visible = buildScope(preceding)
				visible.parent = outerScope
			}
			if vl := inner.GetValuesLists(); len(vl) > 0 {
				dr := &depResolver{
					in: in, sc: visible, layouts: cteLayouts,
					pubExpr: map[int]*pg.Node{}, done: map[int]bool{}, active: map[int]bool{},
				}
				layout = in.valuesLayout(inner, sub.GetAlias(), dr)
			} else {
				var rep *pg.SelectStmt
				rep, layout = in.selectStmt(inner, cteLayouts, emit, visible)
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
		it := &fromItemRef{layout: layoutWithAlias(layout, alias), alias: alias, occ: occ, node: n}
		stampRefs(it.layout, it)
		return it
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
			rep, layout := in.selectStmt(innerSel, cteLayouts, emit, outerScope)
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
			it := &fromItemRef{layout: layoutWithAlias(layout, alias.GetAliasname()), alias: alias.GetAliasname(),
				occ: in.syntheticOccurrence(SourceDerived), node: n}
			stampRefs(it.layout, it)
			return it
		}
		left := in.fromItem(j.GetLarg(), cteLayouts, emit, preceding, outerScope)
		// A LATERAL right side sees every item the join itself follows —
		// the clause's earlier items plus the join's own left side.
		rightPreceding := append(append([]*fromItemRef(nil), preceding...), left)
		right := in.fromItem(j.GetRarg(), cteLayouts, emit, rightPreceding, outerScope)
		if in.err != nil {
			return nil
		}
		it := &fromItemRef{node: n, occ: in.syntheticOccurrence(SourceGeneratedCarrier), children: []*fromItemRef{left, right}}
		it.layout = in.joinFreezeAndLayout(j, left, right, it)
		return it
	case *pg.Node_RangeTableSample:
		// TABLESAMPLE wraps the relation; any AS alias lives on the inner
		// RangeVar itself — the sampled row type is unchanged.
		return in.fromItem(v.RangeTableSample.GetRelation(), cteLayouts, emit, preceding, outerScope)
	default:
		// RangeFunction/RangeTableFunc/VALUES etc.: no entity RangeVars
		// inside (stray ones were rejected); column set is opaque.
		it := &fromItemRef{layout: &itemLayout{opaque: true}, alias: aliasOf(n),
			occ: in.syntheticOccurrence(SourceNonEntity), node: n}
		stampRefs(it.layout, it)
		return it
	}
}

// joinFreezeAndLayout computes the merged output layout of a JOIN from its
// two child layouts — never from the raw leaf list, so nested joins keep
// their already-merged columns. When a merged column must be rendered
// explicitly (a forced `*` expansion), it is referenced through the join's
// join_using_alias — user-supplied or generated — so PostgreSQL computes
// the merged var natively: side selection, common-type coercion, typmod,
// GROUP BY legality and source FDs all stay server-side. Unforced `*` stays
// verbatim, so the common path never names the merged column at all.
func (in *injector) joinFreezeAndLayout(j *pg.JoinExpr, l, r, join *fromItemRef) *itemLayout {
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
		// NATURAL always freezes — injected witness columns must never
		// become join keys. A nonempty intersection becomes an explicit
		// USING (unconditionally, not just when injected columns ride
		// either side: a lateral FROM item's witness can still force `*`
		// expansion later, and the join_using_alias the merged columns
		// reference is only declarable through an explicit USING clause).
		// An empty intersection becomes ON TRUE with the jointype kept —
		// CROSS-substituting would alter outer-join null-extension.
		j.IsNatural = false
		if len(merged) == 0 {
			j.UsingClause = nil
			j.JoinUsingAlias = nil
			j.Quals = boolConst(true)
		} else {
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
	if len(merged) > 0 {
		// Name the USING merged-column group so explicit expansion can
		// reference PostgreSQL's own merged vars — value, common-type
		// coercion and source FDs stay native instead of approximating
		// buildMergedJoinVar with side picks or COALESCE. An existing
		// user alias (`USING(x) AS u`) is reused as-is.
		ualias := j.GetJoinUsingAlias().GetAliasname()
		if ualias == "" {
			in.synth++
			ualias = fmt.Sprintf("__chub_uj%d", in.synth)
			if j.JoinUsingAlias == nil {
				j.JoinUsingAlias = &pg.Alias{}
			}
			j.JoinUsingAlias.Aliasname = ualias
		}
		join.usingAlias = ualias
		join.mergedCount = len(merged)
		for _, name := range merged {
			lc := findPublic(l.layout, name)
			rc := findPublic(r.layout, name)
			if lc == nil || rc == nil {
				in.err = &RejectError{Code: "query_not_allowed", Message: fmt.Sprintf("%v: USING column absent from a join side", ErrLayoutUnfreezable)}
				return nil
			}
			using[name] = true
			col := outCol{name: name, src: ualias,
				refs: []colRef{{item: join, ord: len(out.cols), qual: ualias, name: name}}}
			in.mergeLeafProof(j.GetJointype(), lc, rc, &col)
			if in.err != nil {
				return nil
			}
			out.cols = append(out.cols, col)
		}
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

// mergeLeafProof applies PostgreSQL's buildMergedJoinVar outcome for one
// merged USING column: the merged output IS a leaf Var verbatim only when
// that Var needs no coercion — INNER prefers left then right, LEFT keeps
// left, RIGHT keeps right, FULL keeps none. "No coercion" follows
// select_common_type plus select_common_typmod: the shared typmod survives
// only when both inputs already have the common type and equal typmods.
// Absent evidence (no metadata capability, untyped column) records the
// merge undecided — a later sort-key proof must fail with
// ErrTypeResolutionUnavailable rather than guess.
func (in *injector) mergeLeafProof(jt pg.JoinType, lc, rc *outCol, col *outCol) {
	col.deps = sortSourceRefs(unionSourceRefs(lc.deps, rc.deps))
	col.depsComplete = lc.depsComplete && rc.depsComplete
	if lc.typ.OID == 0 || rc.typ.OID == 0 {
		col.mergeUndecided = true
		col.candidates = mergeCandidates(jt, lc, rc)
		col.fdOrigin = FDOrigin{Kind: FDOriginUnresolved}
		return
	}
	common := lc.typ.OID
	if common != rc.typ.OID {
		mr, ok := in.res.(ColumnMetadataResolver)
		if !ok {
			col.mergeUndecided = true
			col.candidates = mergeCandidates(jt, lc, rc)
			col.fdOrigin = FDOrigin{Kind: FDOriginUnresolved}
			return
		}
		co, err := mr.CommonType(lc.typ.OID, rc.typ.OID)
		if err != nil {
			in.err = fmt.Errorf("resolve common type of %d/%d: %w",
				lc.typ.OID, rc.typ.OID, err)
			return
		}
		if co == 0 {
			in.err = fmt.Errorf("common type of %d/%d: provider returned no verdict: %w",
				lc.typ.OID, rc.typ.OID, ErrTypeResolutionUnavailable)
			return
		}
		common = co
	}
	commonTypmod := int32(-1)
	if lc.typ.OID == common && rc.typ.OID == common && lc.typ.Typmod == rc.typ.Typmod {
		commonTypmod = lc.typ.Typmod
	}
	col.typ = ColumnType{OID: common, Typmod: commonTypmod}
	plainL := lc.typ.OID == common && lc.typ.Typmod == commonTypmod
	plainR := rc.typ.OID == common && rc.typ.Typmod == commonTypmod
	var keep *outCol
	switch jt {
	case pg.JoinType_JOIN_INNER:
		if plainL {
			keep = lc
		} else if plainR {
			keep = rc
		}
	case pg.JoinType_JOIN_LEFT:
		if plainL {
			keep = lc
		}
	case pg.JoinType_JOIN_RIGHT:
		if plainR {
			keep = rc
		}
	}
	if keep != nil {
		col.refs = append(col.refs, keep.refs...)
		col.relOID = keep.relOID
		col.attNum = keep.attNum
		col.fdOrigin = keep.fdOrigin
	} else {
		col.fdOrigin = FDOrigin{Kind: FDOriginNoColumnOrigin}
	}
}

func mergeCandidates(jt pg.JoinType, lc, rc *outCol) []colRef {
	switch jt {
	case pg.JoinType_JOIN_LEFT:
		return append([]colRef(nil), lc.refs...)
	case pg.JoinType_JOIN_RIGHT:
		return append([]colRef(nil), rc.refs...)
	case pg.JoinType_JOIN_INNER:
		out := append([]colRef(nil), lc.refs...)
		return append(out, rc.refs...)
	}
	return nil
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

// layoutWithAlias clones a layout stamping the referencing alias as each
// public column's source qualifier — under an alias the inner qualifiers
// (leaf aliases, internal USING names) are unreachable, so every public
// column must route through the alias. Witness cols keep their recorded
// src (the attested RTE alias inside the source's own scope).
func layoutWithAlias(l *itemLayout, alias string) *itemLayout {
	if l == nil {
		return &itemLayout{opaque: true}
	}
	out := &itemLayout{opaque: l.opaque, cols: cloneCols(l.cols)}
	for i := range out.cols {
		if !out.cols[i].witness && alias != "" {
			out.cols[i].src = alias
		}
	}
	return out
}

func cloneCols(cols []outCol) []outCol {
	out := make([]outCol, len(cols))
	for i, c := range cols {
		c.refs = append([]colRef(nil), c.refs...)
		c.candidates = append([]colRef(nil), c.candidates...)
		c.deps = append([]SourceRef(nil), c.deps...)
		c.coveredSources = append([]SourceOccurrenceID(nil), c.coveredSources...)
		out[i] = c
	}
	return out
}

// valuesLayout computes a `(VALUES ...)` derived table's columns: the row
// arity comes from the first row; names are the alias column list or PG's
// columnN defaults. Each output position's dependencies are the union of
// that position's expression across all rows — a LATERAL column reference
// or a correlated SubLink therefore reaches the real terminal source
// instead of the derived occurrence's own slot.
func (in *injector) valuesLayout(sel *pg.SelectStmt, alias *pg.Alias, dr *depResolver) *itemLayout {
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
	for pos := range names {
		c := outCol{name: names[pos]}
		var deps []SourceRef
		complete := true
		for _, row := range rows {
			items := row.GetList().GetItems()
			if pos >= len(items) {
				// Ragged VALUES lists are a parse-time error upstream; keep
				// the record unresolved rather than guess.
				complete = false
				break
			}
			sub, ok := dr.exprDeps(items[pos])
			deps = unionSourceRefs(deps, sub)
			complete = complete && ok
			if in.err != nil {
				return out
			}
		}
		c.deps = sortSourceRefs(deps)
		c.depsComplete = complete
		if complete {
			c.fdOrigin = FDOrigin{Kind: FDOriginNoColumnOrigin}
		} else {
			c.fdOrigin = FDOrigin{Kind: FDOriginUnresolved}
		}
		out.cols = append(out.cols, c)
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
			if c.src != "" {
				out = append(out, targetNode(colRefNode(c.src, c.name), ""))
			} else {
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

type scopeEntry struct {
	item *fromItemRef
	ords []int
}

type fromScope struct {
	byQual map[string]*scopeEntry
	items  []*fromItemRef
	// parent is the scope of the immediately enclosing query level —
	// correlated references resolve innermost-first then walk outward;
	// nil at the outermost analyzed level.
	parent *fromScope
}

func partsKey(parts ...string) string {
	return strings.Join(parts, "\x00")
}

func buildScope(items []*fromItemRef) *fromScope {
	sc := &fromScope{byQual: map[string]*scopeEntry{}, items: items}
	var walk func(it *fromItemRef)
	walk = func(it *fromItemRef) {
		if it == nil {
			return
		}
		for _, ch := range it.children {
			walk(ch)
		}
		if len(it.children) > 0 {
			if it.usingAlias != "" {
				e := &scopeEntry{item: it, ords: make([]int, it.mergedCount)}
				for i := range e.ords {
					e.ords[i] = i
				}
				sc.byQual[partsKey(it.usingAlias)] = e
			}
			return
		}
		e := &scopeEntry{item: it}
		if it.alias != "" {
			sc.byQual[partsKey(it.alias)] = e
		}
		if it.entity != nil && it.entity.Schema != "" &&
			it.node.GetRangeVar().GetAlias() == nil {
			sc.byQual[partsKey(it.entity.Schema, it.entity.Name)] = e
		}
	}
	for _, it := range items {
		walk(it)
	}
	return sc
}

func (e *scopeEntry) lookup(qual, name string) (colRef, bool) {
	var found colRef
	n := 0
	pub := 0
	for i := range e.item.layout.cols {
		c := &e.item.layout.cols[i]
		if c.witness {
			continue
		}
		if (e.ords == nil || ordIn(e.ords, pub)) && c.name == name {
			found = colRef{item: e.item, ord: pub, qual: qual, name: name}
			n++
		}
		pub++
	}
	if n == 1 {
		return found, true
	}
	return colRef{}, false
}

// lookupBare resolves an unqualified column name innermost-first: a name
// that matches anything at the current level never falls through to the
// parent scope, even when ambiguous (PostgreSQL would report that error).
func (s *fromScope) lookupBare(name string) (colRef, bool) {
	for sc := s; sc != nil; sc = sc.parent {
		var found colRef
		n := 0
		for _, it := range sc.items {
			pub := 0
			for i := range it.layout.cols {
				c := &it.layout.cols[i]
				if c.witness {
					continue
				}
				if c.name == name {
					found = colRef{item: it, ord: pub, name: name}
					n++
				}
				pub++
			}
		}
		if n > 0 {
			return found, n == 1
		}
	}
	return colRef{}, false
}

func (s *fromScope) resolveFields(fields []*pg.Node) (colRef, bool) {
	var parts []string
	for _, f := range fields {
		sv := f.GetString_()
		if sv == nil {
			return colRef{}, false
		}
		parts = append(parts, sv.GetSval())
	}
	if len(parts) == 1 {
		return s.lookupBare(parts[0])
	}
	// A qualifier visible at this level owns the lookup — an absent column
	// there is an error upstream, not a reason to escape outward.
	for sc := s; sc != nil; sc = sc.parent {
		if e := sc.byQual[partsKey(parts[:len(parts)-1]...)]; e != nil {
			return e.lookup(strings.Join(parts[:len(parts)-1], "."), parts[len(parts)-1])
		}
	}
	return colRef{}, false
}

// itemByFields resolves a whole-item reference such as `orders` or
// `app.orders` to its FROM item, as opposed to a public column slot.
func (s *fromScope) itemByFields(fields []*pg.Node) *fromItemRef {
	var parts []string
	for _, f := range fields {
		sv := f.GetString_()
		if sv == nil {
			return nil
		}
		parts = append(parts, sv.GetSval())
	}
	if len(parts) == 0 {
		return nil
	}
	for sc := s; sc != nil; sc = sc.parent {
		if e := sc.byQual[partsKey(parts...)]; e != nil {
			return e.item
		}
	}
	return nil
}

func ordIn(ords []int, ord int) bool {
	for _, o := range ords {
		if o == ord {
			return true
		}
	}
	return false
}

func colAtPublic(it *fromItemRef, ord int) *outCol {
	pub := 0
	for i := range it.layout.cols {
		if it.layout.cols[i].witness {
			continue
		}
		if pub == ord {
			return &it.layout.cols[i]
		}
		pub++
	}
	return nil
}

// publicCols builds the layer's user-visible output columns post-freeze —
// names plus each column's type/attribute evidence and provenance
// references resolved in the ORIGINAL FROM scope (walking its correlated
// parent chain). Names are materialized before expression dependencies run
// so SQL92 output-name binding (ORDER BY inside and outside window
// definitions) can reach every target. The second return maps each target
// node to the public index where its output begins — a retained `x.*`
// covers several columns with one node.
func (in *injector) publicCols(sel *pg.SelectStmt, items []*fromItemRef, dr *depResolver) ([]outCol, []int, error) {
	var out []outCol
	var starts []int
	for _, t := range sel.GetTargetList() {
		rt := t.GetResTarget()
		if rt == nil || rt.GetVal() == nil {
			return nil, nil, fmt.Errorf("res-target missing value")
		}
		starts = append(starts, len(out))
		if cr := rt.GetVal().GetColumnRef(); cr != nil && hasStar(cr.GetFields()) {
			// A retained star target contributes its referenced columns —
			// enumerate them from the item layouts (which already carry
			// per-reference column aliases).
			cols, err := in.starCols(cr.GetFields(), items, dr.sc)
			if err != nil {
				return nil, nil, err
			}
			out = append(out, cols...)
			continue
		}
		oc := outCol{name: rt.GetName()}
		if oc.name == "" {
			oc.name = outputNameOf(rt.GetVal())
		}
		if cr := rt.GetVal().GetColumnRef(); cr != nil {
			if ref, ok := dr.sc.resolveFields(cr.GetFields()); ok {
				if src := colAtPublic(ref.item, ref.ord); src != nil {
					oc.typ = src.typ
					oc.relOID = src.relOID
					oc.attNum = src.attNum
					oc.mergeUndecided = src.mergeUndecided
					oc.refs = append([]colRef(nil), src.refs...)
					oc.candidates = append([]colRef(nil), src.candidates...)
					oc.deps = append([]SourceRef(nil), src.deps...)
					oc.depsComplete = src.depsComplete
					oc.fdOrigin = src.fdOrigin
				}
			} else if it := dr.sc.itemByFields(cr.GetFields()); it != nil {
				deps, complete := in.wholeRowDeps(it)
				oc.deps = deps
				oc.depsComplete = complete
				oc.fdOrigin = FDOrigin{Kind: FDOriginUnresolved}
				if complete && it.entity != nil && it.relOID != 0 {
					src := SourceRef{Occurrence: it.occ, Slot: wholeRowSlot()}
					oc.fdOrigin = FDOrigin{Kind: FDOriginKnownColumn, Source: src,
						RelationOID: it.relOID, AttributeNumber: 0}
				}
			} else {
				oc.depsComplete = false
				oc.fdOrigin = FDOrigin{Kind: FDOriginUnresolved}
			}
		} else {
			// Expression deps run in phase two — the resolver must already
			// know every public name for SQL92 output-column binding.
			dr.pubExpr[len(out)] = rt.GetVal()
		}
		out = append(out, oc)
	}
	dr.pub = out
	for i := range dr.pub {
		if _, isExpr := dr.pubExpr[i]; isExpr {
			dr.publicDep(i)
			if in.err != nil {
				return nil, nil, in.err
			}
		}
	}
	return dr.pub, starts, nil
}

// wholeRowDeps returns the terminal dependency for a whole-item reference.
// An entity has a dedicated whole-row slot; a non-entity carrier expands to
// the terminal dependencies of its public columns.
func (in *injector) wholeRowDeps(it *fromItemRef) ([]SourceRef, bool) {
	if it == nil || it.layout == nil || it.layout.opaque {
		return nil, false
	}
	if it.entity != nil {
		return []SourceRef{{Occurrence: it.occ, Slot: wholeRowSlot()}}, true
	}
	var deps []SourceRef
	complete := true
	for i := range it.layout.cols {
		c := &it.layout.cols[i]
		if c.witness {
			continue
		}
		deps = unionSourceRefs(deps, c.deps)
		complete = complete && c.depsComplete
	}
	return sortSourceRefs(deps), complete
}

// depResolver carries one SELECT layer's name-resolution context for
// terminal-source dependency analysis: the FROM scope (whose parent chain
// gives correlated references their innermost-first, outward fallback), the
// visible CTE layouts, this layer's named window definitions, and the
// public output columns used by SQL92 ORDER BY/GROUP BY binding.
type depResolver struct {
	in      *injector
	sc      *fromScope
	layouts map[string]*itemLayout
	windows map[string]*pg.WindowDef
	// pub is the layer's public column list; pubExpr marks which entries
	// still owe expression analysis. done/active memoize publicDep so an
	// output-name reference can reach a not-yet-visited target without
	// recursing forever through mutually referencing windows.
	pub     []outCol
	pubExpr map[int]*pg.Node
	done    map[int]bool
	active  map[int]bool
}

// publicDep returns the terminal dependencies of public column i,
// computing expression columns on demand. A cyclic output-name binding
// degrades to incomplete rather than recursing.
func (d *depResolver) publicDep(i int) ([]SourceRef, bool) {
	if d.done[i] {
		return append([]SourceRef(nil), d.pub[i].deps...), d.pub[i].depsComplete
	}
	root, isExpr := d.pubExpr[i]
	if !isExpr {
		d.done[i] = true
		return append([]SourceRef(nil), d.pub[i].deps...), d.pub[i].depsComplete
	}
	if d.active[i] {
		return nil, false
	}
	d.active[i] = true
	deps, ok := d.exprDeps(root)
	delete(d.active, i)
	d.done[i] = true
	d.pub[i].deps = sortSourceRefs(deps)
	d.pub[i].depsComplete = ok
	if ok {
		d.pub[i].fdOrigin = FDOrigin{Kind: FDOriginNoColumnOrigin}
	} else {
		d.pub[i].fdOrigin = FDOrigin{Kind: FDOriginUnresolved}
	}
	return append([]SourceRef(nil), deps...), ok
}

// publicByName finds the output column a bare SQL92 identifier denotes.
// idx>=0 is a usable binding; ambiguous=true reports a duplicate output
// name the caller must not guess at.
func (d *depResolver) publicByName(name string) (idx int, ambiguous bool) {
	idx = -1
	for i := range d.pub {
		if d.pub[i].name == name {
			if idx >= 0 {
				return -1, true
			}
			idx = i
		}
	}
	return idx, false
}

// windowDeps unions the terminal dependencies every clause of one window
// definition contributes: the inherited base window (refname), partition
// keys, ordering keys — which bind output names like top-level ORDER BY —
// and frame offsets. named=true marks an `OVER w`-style inline definition
// whose name field references a WINDOW-clause member; a member's own name
// field is its declaration, never a reference.
func (d *depResolver) windowDeps(wd *pg.WindowDef, seen map[string]bool, named bool) ([]SourceRef, bool) {
	if wd == nil {
		return nil, true
	}
	var deps []SourceRef
	complete := true
	merge := func(sub []SourceRef, ok bool) {
		deps = unionSourceRefs(deps, sub)
		complete = complete && ok
	}
	refs := []string{wd.GetRefname()}
	if named {
		refs = append(refs, wd.GetName())
	}
	for _, name := range refs {
		if name == "" {
			continue
		}
		base := d.windows[name]
		if base == nil || seen[name] {
			merge(nil, false) // unknown or cyclic window — evidence stays incomplete
			continue
		}
		seen[name] = true
		merge(d.windowDeps(base, seen, false))
		delete(seen, name)
	}
	for _, p := range wd.GetPartitionClause() {
		merge(d.exprDeps(p))
	}
	for _, s := range wd.GetOrderClause() {
		if sb := s.GetSortBy(); sb != nil {
			merge(d.sortKeyDeps(sb.GetNode()))
		}
	}
	merge(d.exprDeps(wd.GetStartOffset()))
	merge(d.exprDeps(wd.GetEndOffset()))
	return sortSourceRefs(deps), complete
}

// sortKeyDeps resolves an ORDER BY key under SQL92 rules: ordinals and bare
// identifiers bind the public output column first; anything else resolves
// as a FROM-scope expression.
func (d *depResolver) sortKeyDeps(n *pg.Node) ([]SourceRef, bool) {
	if v, ok := ordinalConst(n); ok {
		if v >= 1 && v <= int64(len(d.pub)) {
			return d.publicDep(int(v - 1))
		}
		return nil, false
	}
	if cr := n.GetColumnRef(); cr != nil && !hasStar(cr.GetFields()) {
		f := cr.GetFields()
		if len(f) == 1 {
			if s := f[0].GetString_(); s != nil {
				idx, ambiguous := d.publicByName(s.GetSval())
				if ambiguous {
					return nil, false
				}
				if idx >= 0 {
					return d.publicDep(idx)
				}
			}
		}
	}
	return d.exprDeps(n)
}

// groupKeyDeps resolves a GROUP BY key: ordinals bind positions, and bare
// names resolve as input columns BEFORE output names — the reverse of
// ORDER BY's precedence.
func (d *depResolver) groupKeyDeps(n *pg.Node) ([]SourceRef, bool) {
	if v, ok := ordinalConst(n); ok {
		if v >= 1 && v <= int64(len(d.pub)) {
			return d.publicDep(int(v - 1))
		}
		return nil, false
	}
	if cr := n.GetColumnRef(); cr != nil && !hasStar(cr.GetFields()) {
		if _, ok := d.sc.resolveFields(cr.GetFields()); ok {
			return d.exprDeps(n) // input column wins over an output name
		}
		f := cr.GetFields()
		if len(f) == 1 {
			if s := f[0].GetString_(); s != nil {
				idx, ambiguous := d.publicByName(s.GetSval())
				if ambiguous {
					return nil, false
				}
				if idx >= 0 {
					return d.publicDep(idx)
				}
			}
		}
	}
	return d.exprDeps(n)
}

// exprDeps collects every terminal source slot an expression reads.
// ColumnRefs resolve in the correlated scope chain, SubLinks contribute
// their suppressed-layer summary (never a false empty set), and FuncCall
// windows pull in the addressed definition's own clauses.
func (d *depResolver) exprDeps(root *pg.Node) ([]SourceRef, bool) {
	var deps []SourceRef
	complete := true
	merge := func(sub []SourceRef, ok bool) {
		deps = unionSourceRefs(deps, sub)
		complete = complete && ok
	}
	walkTree(msgOf(root), func(_ *walkCtx, m protoreflect.Message) bool {
		if d.in.err != nil {
			return false
		}
		switch v := m.Interface().(type) {
		case *pg.ColumnRef:
			fields := v.GetFields()
			if hasStar(fields) {
				it := d.sc.itemByFields(fields[:len(fields)-1])
				merge(d.in.wholeRowDeps(it))
				return false
			}
			if ref, ok := d.sc.resolveFields(fields); ok {
				if src := colAtPublic(ref.item, ref.ord); src != nil {
					deps = unionSourceRefs(deps, src.deps)
					complete = complete && src.depsComplete
				} else {
					complete = false
				}
			} else if it := d.sc.itemByFields(fields); it != nil {
				merge(d.in.wholeRowDeps(it))
			} else {
				complete = false
			}
			return false
		case *pg.FuncCall:
			// `over` is the FuncCall's window definition — handle it here
			// (named/indirect references included) and descend only into the
			// call's own argument/filter subtrees.
			merge(d.windowDeps(v.GetOver(), map[string]bool{}, true))
			for _, a := range v.GetArgs() {
				merge(d.exprDeps(a))
			}
			for _, a := range v.GetAggOrder() {
				merge(d.exprDeps(a))
			}
			merge(d.exprDeps(v.GetAggFilter()))
			return false
		case *pg.SubLink:
			if te := v.GetTestexpr(); te != nil {
				merge(d.exprDeps(te))
			}
			merge(d.in.subLinkDeps(v, d))
			return false
		}
		return true
	})
	if d.in.err != nil {
		return nil, false
	}
	return sortSourceRefs(deps), complete
}

// subLinkDeps processes the subselect in suppressed mode and returns the
// layer's complete dependency summary. The inner layer inherits the
// enclosing scope as its correlated parent. It runs against the real
// subtree so RangeVar node → RefID mapping stays pointer-local and exact.
func (in *injector) subLinkDeps(sl *pg.SubLink, d *depResolver) ([]SourceRef, bool) {
	if sl == nil || sl.GetSubselect() == nil {
		return nil, true
	}
	sub := sl.GetSubselect().GetSelectStmt()
	if sub == nil {
		return nil, false
	}
	layout, ok := in.subLayouts[sub]
	if !ok {
		rep, got := in.selectStmt(sub, d.layouts, false, d.sc)
		if rep != sub {
			sl.Subselect.Node = &pg.Node_SelectStmt{SelectStmt: rep}
			sub = rep
		}
		layout = got
		if layout != nil {
			in.subLayouts[sub] = layout
		}
	}
	if in.err != nil || layout == nil {
		return nil, false
	}
	return append([]SourceRef(nil), layout.allDeps...), layout.allDepsComplete
}

// fillLayerDeps stores the complete dependency summary for one SELECT
// layer. Public targets plus predicate/sort/group/window/VALUES/join
// expressions are all part of a SubLink summary when this layer is
// referenced from one. Sort keys bind output names like PostgreSQL's
// ORDER BY; GROUP BY keys prefer input columns over output names.
func (in *injector) fillLayerDeps(sel *pg.SelectStmt, dr *depResolver, layout *itemLayout) {
	var deps []SourceRef
	complete := true
	merge := func(sub []SourceRef, ok bool) {
		deps = unionSourceRefs(deps, sub)
		complete = complete && ok
	}
	for i := range dr.pub {
		deps = unionSourceRefs(deps, dr.pub[i].deps)
		complete = complete && dr.pub[i].depsComplete
	}
	merge(dr.exprDeps(sel.GetWhereClause()))
	merge(dr.exprDeps(sel.GetHavingClause()))
	merge(dr.exprDeps(sel.GetLimitOffset()))
	merge(dr.exprDeps(sel.GetLimitCount()))
	for _, n := range sel.GetSortClause() {
		if sb := n.GetSortBy(); sb != nil {
			merge(dr.sortKeyDeps(sb.GetNode()))
		}
	}
	for _, n := range sel.GetDistinctClause() {
		merge(dr.exprDeps(n))
	}
	for _, n := range sel.GetGroupClause() {
		merge(dr.groupKeyDeps(n))
	}
	for _, n := range sel.GetWindowClause() {
		merge(dr.windowDeps(n.GetWindowDef(), map[string]bool{}, false))
	}
	for _, vl := range sel.GetValuesLists() {
		merge(dr.exprDeps(vl))
	}
	var walkFrom func(n *pg.Node)
	walkFrom = func(n *pg.Node) {
		if n == nil {
			return
		}
		if j := n.GetJoinExpr(); j != nil {
			merge(dr.exprDeps(j.GetQuals()))
			walkFrom(j.GetLarg())
			walkFrom(j.GetRarg())
			return
		}
		if rf := n.GetRangeFunction(); rf != nil {
			for _, f := range rf.GetFunctions() {
				for _, arg := range f.GetList().GetItems() {
					merge(dr.exprDeps(arg))
				}
			}
			return
		}
		if rtf := n.GetRangeTableFunc(); rtf != nil {
			merge(dr.exprDeps(rtf.GetRowexpr()))
			merge(dr.exprDeps(rtf.GetDocexpr()))
			for _, ns := range rtf.GetNamespaces() {
				merge(dr.exprDeps(ns))
			}
			for _, c := range rtf.GetColumns() {
				merge(dr.exprDeps(c))
			}
			return
		}
		if ts := n.GetRangeTableSample(); ts != nil {
			for _, a := range ts.GetArgs() {
				merge(dr.exprDeps(a))
			}
			merge(dr.exprDeps(ts.GetRepeatable()))
		}
	}
	for _, n := range sel.GetFromClause() {
		walkFrom(n)
	}
	if in.err != nil {
		return
	}
	layout.allDeps = sortSourceRefs(deps)
	layout.allDepsComplete = complete
}

// windowIndex maps this layer's WINDOW-clause definitions by name —
// the namespace `OVER w` and window inheritance (`refname`) resolve in.
func windowIndex(sel *pg.SelectStmt) map[string]*pg.WindowDef {
	wc := sel.GetWindowClause()
	if len(wc) == 0 {
		return nil
	}
	idx := make(map[string]*pg.WindowDef, len(wc))
	for _, n := range wc {
		if wd := n.GetWindowDef(); wd != nil && wd.GetName() != "" {
			idx[wd.GetName()] = wd
		}
	}
	return idx
}

// starCols enumerates the public columns a retained `*`/`x.*` target
// expands to — item layouts carry per-reference aliases and provenance,
// so the list matches what PostgreSQL will emit. Retained stars are only
// kept over witness-free items, so no internal names can appear here.
func (in *injector) starCols(fields []*pg.Node, items []*fromItemRef, sc *fromScope) ([]outCol, error) {
	publics := func(l *itemLayout) ([]outCol, error) {
		if l == nil || l.opaque {
			return nil, fmt.Errorf("%v: cannot enumerate star columns of opaque item", ErrLayoutUnfreezable)
		}
		var cols []outCol
		for _, c := range l.cols {
			if !c.witness {
				cols = append(cols, c)
			}
		}
		return cloneCols(cols), nil
	}
	if len(fields) == 1 {
		var out []outCol
		for _, it := range items {
			cols, err := publics(it.layout)
			if err != nil {
				return nil, err
			}
			out = append(out, cols...)
		}
		return out, nil
	}
	var q []string
	for _, f := range fields[:len(fields)-1] {
		if s := f.GetString_(); s == nil {
			return nil, fmt.Errorf("%v: unsupported star qualifier", ErrLayoutUnfreezable)
		} else {
			q = append(q, s.GetSval())
		}
	}
	var e *scopeEntry
	for s := sc; s != nil; s = s.parent {
		if e = s.byQual[partsKey(q...)]; e != nil {
			break
		}
	}
	if e == nil {
		return nil, fmt.Errorf("%v: star qualifier %q resolves to no FROM item", ErrLayoutUnfreezable, strings.Join(q, "."))
	}
	cols, err := publics(e.item.layout)
	if err != nil {
		return nil, err
	}
	if e.ords == nil {
		return cols, nil
	}
	var out []outCol
	for i, c := range cols {
		if ordIn(e.ords, i) {
			out = append(out, c)
		}
	}
	return out, nil
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
			*emitted = append(*emitted, outCol{name: name, witness: true, entity: leaf.entity, src: leaf.alias,
				coveredSources:    []SourceOccurrenceID{leaf.occ},
				carrierOccurrence: leaf.occ, carrierSet: true})
			continue
		}
		for _, c := range leaf.layout.cols {
			if !c.witness {
				continue
			}
			name := in.witnessName(taken)
			sel.TargetList = append(sel.GetTargetList(),
				targetNode(in.propagateExpr(leaf.alias, c.name, aggregate), name))
			*emitted = append(*emitted, outCol{name: name, witness: true, entity: c.entity, src: leaf.alias,
				coveredSources:    append([]SourceOccurrenceID(nil), c.coveredSources...),
				carrierOccurrence: leaf.occ, carrierSet: true})
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
	out := &itemLayout{opaque: l.opaque, cols: cloneCols(l.cols)}
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
func (in *injector) processSublinks(sel *pg.SelectStmt, layouts map[string]*itemLayout, outer *fromScope) {
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
		in.sublinksIn(e, layouts, outer)
		if in.err != nil {
			return
		}
	}
}

// sublinksIn finds every SubLink below an expression root and processes its
// subselect with witness emission suppressed. The inner layer inherits the
// enclosing scope as its correlated parent.
func (in *injector) sublinksIn(root *pg.Node, layouts map[string]*itemLayout, outer *fromScope) {
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
			in.sublinksIn(te, layouts, outer)
		}
		if sub := sl.GetSubselect().GetSelectStmt(); sub != nil {
			if _, done := in.subLayouts[sub]; !done {
				rep, layout := in.selectStmt(sub, layouts, false, outer)
				if rep != sub {
					sl.Subselect.Node = &pg.Node_SelectStmt{SelectStmt: rep}
					sub = rep
				}
				if layout != nil {
					in.subLayouts[sub] = layout
				}
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
func (in *injector) wrapDistinct(q *pg.SelectStmt, pubCols []outCol, pubStarts []int, emitted []outCol, sc *fromScope) (*pg.SelectStmt, *itemLayout) {
	sortKeys := q.GetSortClause()
	limitCount, limitOffset := q.GetLimitCount(), q.GetLimitOffset()
	limitOption := q.GetLimitOption()

	publics := make([]string, 0, len(pubCols))
	for _, c := range pubCols {
		publics = append(publics, c.name)
	}

	// Original public target expressions — for rebinding non-positional
	// ORDER BY keys. Witness targets always sit at the tail, so the matchable
	// prefix is exactly the non-witness slice (a retained `x.*` node simply
	// never proto-equals an ORDER key). ColumnRef keys bind through the
	// ORIGINAL FROM scope's reference identities, expression keys against
	// these target exprs.
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
	dCarrier := in.syntheticOccurrence(SourceGeneratedCarrier)
	for _, w := range emitted {
		fresh := in.witnessName(taken)
		d.TargetList = append(d.TargetList, targetNode(
			filterAggSubscript(colRefNode("__chub_q", w.name)), fresh))
		newEmitted = append(newEmitted, outCol{name: fresh, witness: true, entity: w.entity, src: "__chub_q",
			coveredSources:    append([]SourceOccurrenceID(nil), w.coveredSources...),
			carrierOccurrence: dCarrier, carrierSet: true})
	}
	for _, sk := range sortKeys {
		if sb := sk.GetSortBy(); sb != nil {
			key := sb.GetNode()
			idx, missing := rebindSortKey(key, pubCols, targetExprs, exprPublicIdx, sc)
			if idx >= 0 {
				sb.Node = intConst(int64(idx + 1))
			} else if missing {
				in.err = fmt.Errorf("%w: DISTINCT ORDER BY key requires USING merge-source proof", ErrTypeResolutionUnavailable)
				return nil, nil
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
	out := &itemLayout{cols: cloneCols(pubCols)}
	out.cols = append(out.cols, newEmitted...)
	return d, out
}

func hasRef(refs []colRef, ref colRef) bool {
	for _, r := range refs {
		if r.item == ref.item && r.ord == ref.ord {
			return true
		}
	}
	return false
}

func appendRefs(dst []colRef, src []colRef) []colRef {
	for _, r := range src {
		if !hasRef(dst, r) {
			dst = append(dst, r)
		}
	}
	return dst
}

func refSetsShare(a, b []colRef) bool {
	for _, r := range a {
		if hasRef(b, r) {
			return true
		}
	}
	return false
}

func refSetEqual(a, b []colRef) bool {
	if len(a) != len(b) {
		return false
	}
	for _, r := range a {
		if !hasRef(b, r) {
			return false
		}
	}
	return true
}

func possibleLeafsOf(r colRef) (leafs []colRef, open bool) {
	if r.item == nil {
		return nil, false
	}
	if len(r.item.children) == 0 {
		return []colRef{r}, false
	}
	c := colAtPublic(r.item, r.ord)
	if c == nil {
		return nil, false
	}
	return possibleLeafs(c, r.item, r.ord)
}

func possibleLeafs(c *outCol, join *fromItemRef, ord int) (leafs []colRef, open bool) {
	src := c.refs
	if c.mergeUndecided {
		src = c.candidates
		open = true
	}
	for _, r := range src {
		if join != nil && r.item == join && r.ord == ord {
			if !c.mergeUndecided && len(src) == 1 {
				return []colRef{r}, false
			}
			continue
		}
		sub, o := possibleLeafsOf(r)
		leafs = appendRefs(leafs, sub)
		open = open || o
	}
	return leafs, open
}

func colLeafs(c *outCol) (leafs []colRef, open bool) {
	if c.mergeUndecided {
		var out []colRef
		for _, cand := range c.candidates {
			sub, _ := possibleLeafsOf(cand)
			out = appendRefs(out, sub)
		}
		return out, true
	}
	var out []colRef
	open = false
	for _, r := range c.refs {
		sub, o := possibleLeafsOf(r)
		out = appendRefs(out, sub)
		open = open || o
	}
	return out, open
}

func matchPublicRef(publics []outCol, ref colRef) (int, bool) {
	for i := range publics {
		if hasRef(publics[i].refs, ref) {
			return i, false
		}
	}
	keyLeafs, keyOpen := possibleLeafsOf(ref)
	for i := range publics {
		colLeafs, colOpen := colLeafs(&publics[i])
		if !refSetsShare(keyLeafs, colLeafs) {
			continue
		}
		if keyOpen || colOpen {
			return -1, true
		}
		return i, false
	}
	return -1, false
}

func samePublicOutputs(publics []outCol, match []int, exprAt map[int]*pg.Node) (equal, missing bool) {
	f0, open0 := colLeafs(&publics[match[0]])
	e0 := exprAt[match[0]]
	for _, m := range match[1:] {
		fm, openM := colLeafs(&publics[m])
		if !open0 && !openM && len(f0) > 0 && refSetEqual(f0, fm) {
			continue
		}
		if e0 != nil && exprAt[m] != nil && protoEqualIgnoringLocations(e0, exprAt[m]) {
			continue
		}
		if open0 || openM {
			return false, true
		}
		return false, false
	}
	return true, false
}

// rebindSortKey maps an original ORDER BY key to the 0-based user-column
// index it denotes. A bare identifier binds output names first (SQL92 —
// output columns shadow same-named inputs); qualified keys resolve in the
// ORIGINAL FROM scope and match publics by surviving-Var identity, so leaf
// qualifiers, bare merged names, and join_using_alias spellings all bind
// when they denote the same Var. The second return flags the missing-
// evidence case: only an unproven USING merge could make the key denote
// an output.
func rebindSortKey(key *pg.Node, publics []outCol, exprs []*pg.Node, exprPublicIdx []int, sc *fromScope) (int, bool) {
	if n, ok := ordinalConst(key); ok {
		if n >= 1 && n <= int64(len(publics)) {
			return int(n - 1), false
		}
		return -1, false
	}
	exprAt := map[int]*pg.Node{}
	for i, e := range exprs {
		if cr := e.GetColumnRef(); cr != nil && hasStar(cr.GetFields()) {
			continue
		}
		exprAt[exprPublicIdx[i]] = e
	}
	if cr := key.GetColumnRef(); cr != nil && !hasStar(cr.GetFields()) {
		f := cr.GetFields()
		if len(f) == 1 {
			if s := f[0].GetString_(); s != nil {
				var match []int
				for i := range publics {
					if publics[i].name == s.GetSval() {
						match = append(match, i)
					}
				}
				switch {
				case len(match) == 1:
					return match[0], false
				case len(match) > 1:
					eq, miss := samePublicOutputs(publics, match, exprAt)
					if eq {
						return match[0], false
					}
					return -1, miss
				}
			}
		}
		if ref, ok := sc.resolveFields(f); ok {
			if idx, missing := matchPublicRef(publics, ref); idx >= 0 || missing {
				return idx, missing
			}
		}
	}
	for i, e := range exprs {
		if protoEqualIgnoringLocations(key, e) {
			return exprPublicIdx[i], false
		}
	}
	return -1, false
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
