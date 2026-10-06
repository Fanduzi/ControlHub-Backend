// Package service binds governed PostgreSQL entity references inside the
// attempt transaction and exposes the attempt-scoped metadata resolver.
// input: attempt context with deadline, caller-held pgx.Tx (BEGIN READ ONLY,
// search_path="<pinned>,pg_catalog", TimeZone='UTC' and an initial
// statement_timeout already applied by the caller), resolved database/pinned
// schema, and the GuardResult the rewrite will reuse
// output: PGBind, PGBindInput, PGBindResult, PGBoundRef, PGRelationIdentity,
// PGViewDependency, PGInheritEdge, PGIndexEdge — RefID-keyed touch-confirmed
// bindings with locked attribute maps, view dependency closure, pg_inherits
// children, table-index approval, fixed probe whitelist, and the
// in-transaction pgsql.ColumnMetadataResolver
// pos: T7-S2 binding gate (frozen G4 step 6 / G10 mechanism 2) — pinned USAGE
// check, canonical to_regclass resolution, per-entity self-proof touch whose
// FieldDescription DataTypeOID must equal the gate-resolved reltype, attmap
// under the held ACCESS SHARE lock, recursive view-dependency namespace
// boundary, inheritance-child collection, and native common-type verdicts;
// probe SQL only — user statements and transport SQL never execute here
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/pgsql"
)

// errPGBindInternal is the fixed internal failure for the binding gate. Driver
// text, DSNs, and server error detail never surface — a gate failure is not a
// user-facing query verdict. errors.Is(ErrQueryBackendFailure) stays true.
var errPGBindInternal = fmt.Errorf("%w: pg binding gate", ErrQueryBackendFailure)

// pgProbeCatalogNames is the FIXED whitelist of catalog relations the gate's
// own probes can lock — including each listed table's indexes, since a catalog
// scan may lock whichever index the planner picks. It is never derived from
// observed locks — the set-audit layer compares held locks against exactly
// this resolved list plus approved bindings.
var pgProbeCatalogNames = []string{
	"pg_database",
	"pg_class",
	"pg_namespace",
	"pg_attribute",
	"pg_depend",
	"pg_rewrite",
	"pg_inherits",
	"pg_index",
	"pg_auth_members",
	"pg_authid",
	"pg_type",
}

// PGRelationIdentity is the lock-audit identity of one relation: (database
// OID, relation OID). Shared catalogs (pg_class.relisshared) use database 0,
// matching how pg_locks attributes their locks.
type PGRelationIdentity struct {
	DatabaseOID uint32
	RelationOID uint32
}

// PGBoundRef is one original entity occurrence bound inside the attempt
// transaction, keyed by the guard's original RefID. Two occurrences naming the
// same relation share physical metadata through the gate's caches but stay
// distinct BoundRef records.
type PGBoundRef struct {
	RefID          pgsql.RefID
	Schema         string // effective schema: pinned for unqualified entities
	Name           string
	Identity       PGRelationIdentity // lock-audit identity (database 0 when relisshared)
	RelTypeOID     uint32             // pg_class.reltype — expected witness DataTypeOID
	RelKind        string             // pg_class.relkind as text
	TouchConfirmed bool               // the touch FD self-witness matched RelTypeOID
	Columns        []pgsql.ColumnMetadata
}

// PGViewDependency records one view→relation dependency edge: view ViewOID
// expands (transitively, via pg_rewrite/pg_depend) to relation RelationOID.
type PGViewDependency struct {
	ViewOID     uint32
	RelationOID uint32
	Schema      string
	RelKind     string
}

// PGInheritEdge records one pg_inherits parent→child edge inside the approved
// set, so a later audit can explain a held child lock by ownership.
type PGInheritEdge struct {
	ParentOID uint32
	ChildOID  uint32
}

// PGIndexEdge records one table→index edge inside the approved set: parsing
// any reference to a table locks all of its indexes in ACCESS SHARE, so a held
// index lock is legitimate exactly when its table is approved.
type PGIndexEdge struct {
	RelationOID uint32
	IndexOID    uint32
}

// PGBindResult is the gate output the executor's rewrite and audit stages
// consume. Resolver answers Columns/ColumnMetadata from locked attribute maps
// and CommonType from live probes; it is valid only inside this attempt.
type PGBindResult struct {
	Refs         map[pgsql.RefID]*PGBoundRef
	DatabaseOID  uint32
	Approved     []PGRelationIdentity // entities ∪ view deps ∪ inherit children ∪ their indexes, deduplicated
	Dependencies []PGViewDependency   // view→dep edges within the boundary
	Inherits     []PGInheritEdge      // pg_inherits edges for held-lock attribution
	Indexes      []PGIndexEdge        // table→index edges for held-lock attribution
	Probes       []PGRelationIdentity // fixed probe whitelist identities
	Resolver     pgsql.ColumnResolver // also implements pgsql.ColumnMetadataResolver
}

// PGBindInput bundles the gate inputs. The caller owns the transaction and
// must have already applied the G4 session settings (BEGIN READ ONLY,
// search_path="<pinned>,pg_catalog", TimeZone='UTC', an initial
// statement_timeout). The gate never starts/commits/rolls back the tx and
// never touches the pool.
type PGBindInput struct {
	Tx           pgx.Tx
	Database     string
	PinnedSchema string
	Guard        *pgsql.GuardResult
}

// PGBind runs the in-transaction binding gate. On success the result's Refs
// hold one BoundRef per entity RefID (CTE use sites are not bound) and every
// listed relation holds its ACCESS SHARE lock until the caller rolls back.
// Controlled rejections come back as *pgsql.RejectError; a spent or canceled
// budget comes back as ErrQueryTimeout or context.Canceled; anything else is
// an internal failure wrapped by errPGBindInternal.
func PGBind(ctx context.Context, in PGBindInput) (*PGBindResult, error) {
	budget, err := pgAttemptBudgetFromContext(ctx)
	if err != nil {
		return nil, err
	}
	return newPGBindingGate(ctx, in, budget).run()
}

func newPGBindingGate(ctx context.Context, in PGBindInput, budget pgAttemptBudget) *pgBindingGate {
	return &pgBindingGate{
		ctx:           ctx,
		tx:            in.Tx,
		database:      in.Database,
		pinned:        in.PinnedSchema,
		guard:         in.Guard,
		budget:        budget,
		byName:        map[pgRelKey]*pgBoundRelation{},
		byOID:         map[uint32]*pgBoundRelation{},
		refs:          map[pgsql.RefID]*PGBoundRef{},
		extraApproved: map[PGRelationIdentity]bool{},
	}
}

type pgRelKey struct {
	schema, name string
}

// pgBoundRelation is the physical record one or more BoundRefs may share.
type pgBoundRelation struct {
	oid     uint32
	reltype uint32
	relkind string
	shared  bool
	schema  string // canonical nspname confirmed at resolve time
	columns []pgsql.ColumnMetadata
}

type pgBindingGate struct {
	ctx      context.Context
	tx       pgx.Tx
	database string
	pinned   string
	guard    *pgsql.GuardResult
	budget   pgAttemptBudget

	dbOID         uint32
	byName        map[pgRelKey]*pgBoundRelation
	byOID         map[uint32]*pgBoundRelation
	refs          map[pgsql.RefID]*PGBoundRef
	extraApproved map[PGRelationIdentity]bool // deps/inherit children/indexes, keyed by their own relisshared
	deps          []PGViewDependency
	inherits      []PGInheritEdge
	indexes       []PGIndexEdge
	probes        []PGRelationIdentity

	// afterEntityResolve is a test-only seam letting integration tests
	// interleave a controlled fault (e.g. a namespace swap) between name
	// resolution and the self-proof touch. Production leaves it nil.
	afterEntityResolve func()
}

func (g *pgBindingGate) run() (*PGBindResult, error) {
	if g.tx == nil || g.guard == nil || g.database == "" || g.pinned == "" {
		return nil, errPGBindInternal
	}
	for _, step := range []func() error{
		g.stageDatabaseIdentity,
		g.stagePinnedUsage,
		g.stageEntityBind,
		g.stageAttributes,
		g.stageViewDeps,
		g.stageInheritChildren,
		g.stageEntityIndexes,
		g.stageProbeWhitelist,
	} {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return g.result(), nil
}

// query arms the remaining budget and runs one probe statement. The caller
// must drain and close the returned rows before the next statement.
func (g *pgBindingGate) query(sql string, args ...any) (pgx.Rows, error) {
	return g.queryStage(nil, sql, args...)
}

// queryStage is query with a stage-specific classifier: the stage sees the
// raw driver error while it is still typed, before fail redacts it.
func (g *pgBindingGate) queryStage(classify func(error) error, sql string, args ...any) (pgx.Rows, error) {
	if err := g.arm(); err != nil {
		return nil, err
	}
	rows, err := g.tx.Query(g.ctx, sql, args...)
	if err != nil {
		return nil, g.failStage(err, classify)
	}
	return rows, nil
}

// row arms the remaining budget and returns a single-row probe handle. Scan
// errors still go through fail at the call site.
func (g *pgBindingGate) row(sql string, args ...any) (pgx.Row, error) {
	if err := g.arm(); err != nil {
		return nil, err
	}
	return g.tx.QueryRow(g.ctx, sql, args...), nil
}

func (g *pgBindingGate) arm() error {
	if err := g.budget.arm(g.ctx, g.tx); err != nil {
		return g.fail(err)
	}
	return nil
}

// fail maps an error onto the frozen terminal vocabulary: ctx cancellation is
// authoritative for cancelled-vs-timeout, SQLSTATE 57014 and deadline both
// classify timeout, RejectError passes through, and everything else is the
// fixed internal failure — driver text never leaks.
func (g *pgBindingGate) fail(err error) error {
	return g.failStage(err, nil)
}

// failStage is fail plus one hook: after the ctx check (cancellation and
// deadline stay authoritative) the stage classifier may map a still-typed
// driver error onto its controlled verdict before it is redacted to internal.
func (g *pgBindingGate) failStage(err error, classify func(error) error) error {
	if err == nil {
		return nil
	}
	var re *pgsql.RejectError
	if errors.As(err, &re) {
		return re
	}
	if cerr := g.ctx.Err(); cerr != nil {
		if errors.Is(cerr, context.Canceled) {
			return context.Canceled
		}
		return ErrQueryTimeout
	}
	if classify != nil {
		if classified := classify(err); classified != nil {
			return classified
		}
	}
	switch {
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		return ErrQueryTimeout
	case errors.Is(err, ErrQueryTimeout), errors.Is(err, ErrQueryBackendFailure):
		return err // already a classified sentinel — pass through
	case isPGQueryCanceled(err):
		return ErrQueryTimeout
	default:
		return errPGBindInternal
	}
}

// classifyPGRelationAccess is the classifier for statements that name a user
// relation: undefined table/schema or denied access means the object is
// invisible to this account at this stage — the frozen not-found verdict.
func classifyPGRelationAccess(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return nil
	}
	switch pgErr.Code {
	case "42P01", "3F000", "42501":
		return &pgsql.RejectError{Code: "query_object_not_found", Message: "pgsql: relation not found"}
	}
	return nil
}

// classifyPGNameRemap is the recheck stage's verdict: a name that resolved a
// moment ago and now fails with a relation-access error moved out from under
// the binding — the bound object stands proven, so this is a name mismatch.
func classifyPGNameRemap(err error) error {
	if classifyPGRelationAccess(err) != nil {
		return &pgsql.RejectError{Code: "query_binding_mismatch", Message: "pgsql: relation name remapped during binding"}
	}
	return nil
}

// identityFor derives the lock-audit identity from the relation's own
// pg_class.relisshared — every relation entering Approved (bound entity, view
// dep, inherit child, or index) goes through here so a shared object always
// carries database 0, matching how pg_locks attributes its lock.
func (g *pgBindingGate) identityFor(oid uint32, shared bool) PGRelationIdentity {
	id := PGRelationIdentity{DatabaseOID: g.dbOID, RelationOID: oid}
	if shared {
		id.DatabaseOID = 0
	}
	return id
}

func (g *pgBindingGate) identityOf(rel *pgBoundRelation) PGRelationIdentity {
	return g.identityFor(rel.oid, rel.shared)
}

// noteApproved records a derived relation's audit identity — the caller
// supplies relisshared read alongside the OID, never a guessed database.
func (g *pgBindingGate) noteApproved(oid uint32, shared bool) {
	g.extraApproved[g.identityFor(oid, shared)] = true
}

// stageDatabaseIdentity confirms the transaction's current_database() matches
// the caller-resolved database and records its OID for lock identities.
func (g *pgBindingGate) stageDatabaseIdentity() error {
	row, err := g.row(
		`SELECT d.datname, d.oid::pg_catalog.oid
		   FROM pg_catalog.pg_database d
		  WHERE d.datname = pg_catalog.current_database()`)
	if err != nil {
		return err
	}
	var name string
	var oid uint32
	if err := row.Scan(&name, &oid); err != nil {
		return g.fail(err)
	}
	if name != g.database {
		// The caller bound the transaction to a different database than the
		// connection row promised — a configuration defect, not a query verdict.
		return errPGBindInternal
	}
	g.dbOID = oid
	return nil
}

// stagePinnedUsage checks the pinned schema only when unqualified entity
// references exist (they would resolve there): a missing pinned schema is
// schema_not_found, an unusable one is query_schema_not_usable.
func (g *pgBindingGate) stagePinnedUsage() error {
	unqualified := false
	for _, r := range g.guard.Refs {
		if r.Kind == pgsql.RefEntity {
			unqualified = true
			break
		}
	}
	if !unqualified {
		return nil
	}
	row, err := g.row(
		`SELECT EXISTS (SELECT 1 FROM pg_catalog.pg_namespace WHERE nspname = $1)`, g.pinned)
	if err != nil {
		return err
	}
	var exists bool
	if err := row.Scan(&exists); err != nil {
		return g.fail(err)
	}
	if !exists {
		return &pgsql.RejectError{Code: "schema_not_found", Message: "pgsql: pinned schema does not exist"}
	}
	// has_schema_privilege itself raises 3F000 on a missing name — only run it
	// once existence is proven.
	usageRow, err := g.row(
		`SELECT COALESCE(pg_catalog.has_schema_privilege($1, 'USAGE'), false)`, g.pinned)
	if err != nil {
		return err
	}
	var usage bool
	if err := usageRow.Scan(&usage); err != nil {
		return g.fail(err)
	}
	if !usage {
		return &pgsql.RejectError{Code: "query_schema_not_usable", Message: "pgsql: pinned schema is not usable for this account"}
	}
	return nil
}

// stageEntityBind resolves and self-proofs every entity reference in RefID
// order. CTE references are use sites only — never resolved as relations.
func (g *pgBindingGate) stageEntityBind() error {
	for _, ref := range g.guard.Refs {
		if ref.Kind == pgsql.RefCTE {
			continue
		}
		if err := g.bindEntity(ref); err != nil {
			return err
		}
	}
	return nil
}

func (g *pgBindingGate) bindEntity(ref pgsql.RangeVarRef) error {
	if ref.Catalog != "" && ref.Catalog != g.database {
		return &pgsql.RejectError{Code: "query_object_not_found", Message: "pgsql: cross-database reference"}
	}
	schema := ref.Schema
	if ref.Kind == pgsql.RefEntity {
		schema = g.pinned
	}
	canonical := pgQualifiedName(schema, ref.Name)

	row, err := g.row(
		`SELECT c.oid::pg_catalog.oid, c.reltype::pg_catalog.oid, c.relkind::text,
		        c.relisshared, n.nspname
		   FROM pg_catalog.pg_class c
		   JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		  WHERE c.oid = pg_catalog.to_regclass($1)`, canonical)
	if err != nil {
		return err
	}
	var (
		oid, reltype uint32
		relkind      string
		shared       bool
		nspname      string
	)
	if err := row.Scan(&oid, &reltype, &relkind, &shared, &nspname); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &pgsql.RejectError{Code: "query_object_not_found", Message: "pgsql: relation not found"}
		}
		// to_regclass checks schema USAGE itself: an inaccessible schema
		// makes the object invisible to this account.
		return g.failStage(err, classifyPGRelationAccess)
	}

	if g.afterEntityResolve != nil {
		g.afterEntityResolve()
	}

	touchOID, err := g.touch(schema, ref.Name)
	if err != nil {
		return err
	}
	if touchOID != reltype {
		// The touch self-witness bound a different composite type than the
		// gate-resolved relation — the name moved between resolution and lock.
		return &pgsql.RejectError{Code: "query_binding_mismatch", Message: "pgsql: touch bound an unexpected relation"}
	}

	// Supplementary name re-check: it can only report the name already moved
	// (a stale syscache answer still equals the expected OID); it never
	// substitutes for the touch's own FD proof.
	recheck, err := g.row(
		`SELECT (pg_catalog.to_regclass($1))::pg_catalog.oid`, canonical)
	if err != nil {
		return err
	}
	var again *uint32
	if err := recheck.Scan(&again); err != nil {
		return g.failStage(err, classifyPGNameRemap)
	}
	if again == nil || *again != oid {
		return &pgsql.RejectError{Code: "query_binding_mismatch", Message: "pgsql: relation name remapped during binding"}
	}

	key := pgRelKey{schema: schema, name: ref.Name}
	rel, dup := g.byName[key]
	if !dup {
		rel = &pgBoundRelation{oid: oid, reltype: reltype, relkind: relkind, shared: shared, schema: nspname}
		g.byName[key] = rel
		g.byOID[oid] = rel
	} else if rel.oid != oid || rel.reltype != reltype {
		// Same (schema, name) produced an incompatible record — fail loud
		// instead of picking one or silently overwriting.
		return errPGBindInternal
	}
	g.refs[ref.RefID] = &PGBoundRef{
		RefID:          ref.RefID,
		Schema:         nspname,
		Name:           ref.Name,
		Identity:       g.identityOf(rel),
		RelTypeOID:     reltype,
		RelKind:        relkind,
		TouchConfirmed: true,
	}
	return nil
}

// touch runs the self-proof touch and returns the FD's DataTypeOID. The
// FieldDescriptions slice aliases the connection buffer, so the OID is copied
// before the rows are drained and closed. Both error exits — the initial
// query call and the row-read phase — share the relation-access classifier:
// pgx can surface a server error at either point.
func (g *pgBindingGate) touch(schema, name string) (uint32, error) {
	stmt := fmt.Sprintf(
		`SELECT CASE WHEN FALSE THEN "__chub_lock".* END
		   FROM %s.%s AS "__chub_lock" LIMIT 0`,
		pgQuoteIdent(schema), pgQuoteIdent(name))
	rows, err := g.queryStage(classifyPGRelationAccess, stmt)
	if err != nil {
		return 0, err
	}
	fds := rows.FieldDescriptions()
	oids := make([]uint32, len(fds))
	for i, fd := range fds {
		oids[i] = fd.DataTypeOID
	}
	for rows.Next() {
	}
	drainErr := rows.Err()
	rows.Close()
	if drainErr != nil {
		return 0, g.failStage(drainErr, classifyPGRelationAccess)
	}
	if len(oids) != 1 {
		return 0, errPGBindInternal
	}
	return oids[0], nil
}

// stageAttributes reads the locked attribute map for every touch-confirmed
// relation OID — addressing by OID, never re-picking an object by name.
func (g *pgBindingGate) stageAttributes() error {
	oids := make([]uint32, 0, len(g.byOID))
	for oid := range g.byOID {
		oids = append(oids, oid)
	}
	sort.Slice(oids, func(i, j int) bool { return oids[i] < oids[j] })
	for _, oid := range oids {
		cols, err := g.attributes(oid)
		if err != nil {
			return err
		}
		g.byOID[oid].columns = cols
	}
	for _, ref := range g.refs {
		ref.Columns = g.byOID[ref.Identity.RelationOID].columns
	}
	return nil
}

func (g *pgBindingGate) attributes(oid uint32) ([]pgsql.ColumnMetadata, error) {
	rows, err := g.query(
		`SELECT a.attnum, a.attname, a.atttypid::pg_catalog.oid, a.atttypmod
		   FROM pg_catalog.pg_attribute a
		  WHERE a.attrelid = $1
		    AND a.attnum > 0
		    AND NOT a.attisdropped
		  ORDER BY a.attnum`, oid)
	if err != nil {
		return nil, err
	}
	var cols []pgsql.ColumnMetadata
	for rows.Next() {
		var (
			name   string
			typoid uint32
			typmod int32
			attnum int16
		)
		if err := rows.Scan(&attnum, &name, &typoid, &typmod); err != nil {
			rows.Close()
			return nil, g.fail(err)
		}
		cols = append(cols, pgsql.ColumnMetadata{
			Name:            name,
			Type:            pgsql.ColumnType{OID: typoid, Typmod: typmod},
			RelationOID:     oid,
			AttributeNumber: attnum,
		})
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, g.fail(err)
	}
	rows.Close()
	return cols, nil
}

// stageViewDeps expands every bound view/materialized view through
// pg_rewrite/pg_depend (transitively, deduplicated, self excluded) and rejects
// any relation dependency whose namespace leaves {pinned ∪ view schema}.
// Index/type/function entries can never appear here: only pg_class
// dependencies are collected.
func (g *pgBindingGate) stageViewDeps() error {
	seen := map[uint32]bool{}
	for _, ref := range g.refs {
		rel := g.byOID[ref.Identity.RelationOID]
		if rel.relkind != "v" && rel.relkind != "m" {
			continue
		}
		if seen[rel.oid] {
			continue // repeated references to one view expand it once
		}
		seen[rel.oid] = true
		if err := g.viewDeps(rel); err != nil {
			return err
		}
	}
	return nil
}

func (g *pgBindingGate) viewDeps(view *pgBoundRelation) error {
	rows, err := g.query(
		`WITH RECURSIVE dep(relid) AS (
		     SELECT d.refobjid
		       FROM pg_catalog.pg_depend d
		       JOIN pg_catalog.pg_rewrite rw ON rw.oid = d.objid
		      WHERE d.classid = 'pg_catalog.pg_rewrite'::pg_catalog.regclass
		        AND d.refclassid = 'pg_catalog.pg_class'::pg_catalog.regclass
		        AND rw.ev_class = $1::pg_catalog.oid
		     UNION
		     SELECT d.refobjid
		       FROM dep
		       JOIN pg_catalog.pg_rewrite rw ON rw.ev_class = dep.relid
		       JOIN pg_catalog.pg_depend d ON d.objid = rw.oid
		      WHERE d.classid = 'pg_catalog.pg_rewrite'::pg_catalog.regclass
		        AND d.refclassid = 'pg_catalog.pg_class'::pg_catalog.regclass
		 )
		 SELECT dep.relid::pg_catalog.oid, c.relkind::text, n.nspname, c.relisshared
		   FROM dep
		   JOIN pg_catalog.pg_class c ON c.oid = dep.relid
		   JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace`, view.oid)
	if err != nil {
		return err
	}
	allowed := map[string]bool{g.pinned: true, view.schema: true}
	defer rows.Close()
	for rows.Next() {
		var (
			depOID       uint32
			relkind, nsp string
			shared       bool
		)
		if err := rows.Scan(&depOID, &relkind, &nsp, &shared); err != nil {
			return g.fail(err)
		}
		if depOID == view.oid {
			continue
		}
		if !allowed[nsp] {
			return &pgsql.RejectError{Code: "query_dependency_not_approved", Message: "pgsql: relation dependency outside approved schemas"}
		}
		g.deps = append(g.deps, PGViewDependency{ViewOID: view.oid, RelationOID: depOID, Schema: nsp, RelKind: relkind})
		g.noteApproved(depOID, shared)
	}
	if err := rows.Err(); err != nil {
		return g.fail(err)
	}
	return nil
}

// stageInheritChildren collects recursive pg_inherits descendants of every
// approved parent (entities and view-dep relations): partition/inheritance
// expansion locks children at parse, so they belong to the approved set with
// recorded ownership edges.
func (g *pgBindingGate) stageInheritChildren() error {
	parentSet := map[uint32]bool{}
	for _, ref := range g.refs {
		parentSet[ref.Identity.RelationOID] = true
	}
	for _, d := range g.deps {
		parentSet[d.RelationOID] = true
	}
	parents := make([]int64, 0, len(parentSet))
	for oid := range parentSet {
		parents = append(parents, int64(oid))
	}
	if len(parents) == 0 {
		return nil
	}
	rows, err := g.query(
		`WITH RECURSIVE inh AS (
		     SELECT i.inhrelid::pg_catalog.oid AS oid, i.inhparent::pg_catalog.oid AS parent
		       FROM pg_catalog.pg_inherits i
		      WHERE i.inhparent::bigint = ANY($1::bigint[])
		     UNION ALL
		     SELECT i.inhrelid::pg_catalog.oid, i.inhparent::pg_catalog.oid
		       FROM pg_catalog.pg_inherits i
		       JOIN inh ON i.inhparent = inh.oid
		 )
		 SELECT inh.oid, inh.parent, c.relisshared
		   FROM inh
		   JOIN pg_catalog.pg_class c ON c.oid = inh.oid`, parents)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var child, parent uint32
		var shared bool
		if err := rows.Scan(&child, &parent, &shared); err != nil {
			return g.fail(err)
		}
		g.inherits = append(g.inherits, PGInheritEdge{ParentOID: parent, ChildOID: child})
		g.noteApproved(child, shared)
	}
	if err := rows.Err(); err != nil {
		return g.fail(err)
	}
	return nil
}

// stageEntityIndexes collects the indexes of every approved base relation
// (entities, view deps, and inheritance children): a query referencing a table
// locks all of its indexes in ACCESS SHARE at parse, so they are legitimate
// held locks and must be pre-approved, not mistaken for unexplained locks.
func (g *pgBindingGate) stageEntityIndexes() error {
	baseSet := map[uint32]bool{}
	for _, ref := range g.refs {
		baseSet[ref.Identity.RelationOID] = true
	}
	for _, d := range g.deps {
		baseSet[d.RelationOID] = true
	}
	for _, e := range g.inherits {
		baseSet[e.ChildOID] = true
	}
	if len(baseSet) == 0 {
		return nil
	}
	bases := make([]int64, 0, len(baseSet))
	for oid := range baseSet {
		bases = append(bases, int64(oid))
	}
	rows, err := g.query(
		`SELECT i.indexrelid::pg_catalog.oid, i.indrelid::pg_catalog.oid, ic.relisshared
		   FROM pg_catalog.pg_index i
		   JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
		  WHERE i.indrelid::bigint = ANY($1::bigint[])`, bases)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var indexOID, tableOID uint32
		var shared bool
		if err := rows.Scan(&indexOID, &tableOID, &shared); err != nil {
			return g.fail(err)
		}
		g.indexes = append(g.indexes, PGIndexEdge{RelationOID: tableOID, IndexOID: indexOID})
		g.noteApproved(indexOID, shared)
	}
	if err := rows.Err(); err != nil {
		return g.fail(err)
	}
	return nil
}

// stageProbeWhitelist resolves the fixed probe catalog names to their
// (database, relation) identities. A missing catalog is an internal defect —
// the whitelist is a constant, not learned evidence.
func (g *pgBindingGate) stageProbeWhitelist() error {
	rows, err := g.query(
		`SELECT c.relname, c.oid::pg_catalog.oid, c.relisshared, true AS is_table
		   FROM pg_catalog.pg_class c
		   JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		  WHERE n.nspname = 'pg_catalog'
		    AND c.relname = ANY($1::text[])
		 UNION ALL
		 SELECT ic.relname, ic.oid::pg_catalog.oid, ic.relisshared, false
		   FROM pg_catalog.pg_class ic
		   JOIN pg_catalog.pg_index i ON i.indexrelid = ic.oid
		   JOIN pg_catalog.pg_class t ON t.oid = i.indrelid
		   JOIN pg_catalog.pg_namespace tn ON tn.oid = t.relnamespace
		  WHERE tn.nspname = 'pg_catalog'
		    AND t.relname = ANY($1::text[])`, pgProbeCatalogNames)
	if err != nil {
		return err
	}
	foundTables := map[string]bool{}
	var probes []PGRelationIdentity
	defer rows.Close()
	for rows.Next() {
		var (
			name    string
			oid     uint32
			shared  bool
			isTable bool
		)
		if err := rows.Scan(&name, &oid, &shared, &isTable); err != nil {
			return g.fail(err)
		}
		if isTable {
			foundTables[name] = true
		}
		probes = append(probes, g.identityFor(oid, shared))
	}
	if err := rows.Err(); err != nil {
		return g.fail(err)
	}
	for _, name := range pgProbeCatalogNames {
		if !foundTables[name] {
			return errPGBindInternal
		}
	}
	sort.Slice(probes, func(i, j int) bool {
		if probes[i].DatabaseOID != probes[j].DatabaseOID {
			return probes[i].DatabaseOID < probes[j].DatabaseOID
		}
		return probes[i].RelationOID < probes[j].RelationOID
	})
	g.probes = probes
	return nil
}

// result assembles the deterministic gate output: RefID-keyed bindings plus
// the deduplicated, sorted approved identity set and the attempt resolver.
func (g *pgBindingGate) result() *PGBindResult {
	approved := map[PGRelationIdentity]bool{}
	for _, ref := range g.refs {
		approved[ref.Identity] = true
	}
	for id := range g.extraApproved {
		approved[id] = true
	}
	set := make([]PGRelationIdentity, 0, len(approved))
	for id := range approved {
		set = append(set, id)
	}
	sort.Slice(set, func(i, j int) bool {
		if set[i].DatabaseOID != set[j].DatabaseOID {
			return set[i].DatabaseOID < set[j].DatabaseOID
		}
		return set[i].RelationOID < set[j].RelationOID
	})
	sort.Slice(g.deps, func(i, j int) bool {
		if g.deps[i].ViewOID != g.deps[j].ViewOID {
			return g.deps[i].ViewOID < g.deps[j].ViewOID
		}
		return g.deps[i].RelationOID < g.deps[j].RelationOID
	})
	sort.Slice(g.inherits, func(i, j int) bool {
		if g.inherits[i].ParentOID != g.inherits[j].ParentOID {
			return g.inherits[i].ParentOID < g.inherits[j].ParentOID
		}
		return g.inherits[i].ChildOID < g.inherits[j].ChildOID
	})
	sort.Slice(g.indexes, func(i, j int) bool {
		if g.indexes[i].RelationOID != g.indexes[j].RelationOID {
			return g.indexes[i].RelationOID < g.indexes[j].RelationOID
		}
		return g.indexes[i].IndexOID < g.indexes[j].IndexOID
	})
	return &PGBindResult{
		Refs:         g.refs,
		DatabaseOID:  g.dbOID,
		Approved:     set,
		Dependencies: g.deps,
		Inherits:     g.inherits,
		Indexes:      g.indexes,
		Probes:       g.probes,
		Resolver: &pgTxResolver{
			ctx:    g.ctx,
			tx:     g.tx,
			budget: g.budget,
			pinned: g.pinned,
			byName: g.byName,
			common: map[[2]uint32]uint32{},
			fail:   g.fail,
		},
	}
}

// pgTxResolver is the in-transaction pgsql resolver: Columns/ColumnMetadata
// are served only from touch-confirmed bound relations and CommonType from
// native probes — all under the same attempt budget and connection.
type pgTxResolver struct {
	ctx    context.Context
	tx     pgx.Tx
	budget pgAttemptBudget
	pinned string
	byName map[pgRelKey]*pgBoundRelation
	common map[[2]uint32]uint32
	fail   func(error) error
}

var (
	_ pgsql.ColumnResolver         = (*pgTxResolver)(nil)
	_ pgsql.ColumnMetadataResolver = (*pgTxResolver)(nil)
)

func (r *pgTxResolver) arm() error {
	if err := r.budget.arm(r.ctx, r.tx); err != nil {
		return r.fail(err)
	}
	return nil
}

// bound maps a caller (schema, name) to the bound set: "" means the pinned
// schema per the ColumnResolver contract, any other schema keeps its value.
// A name outside the bound set is an evidence defect — internal failure,
// never a catalog lookup and never a user-facing not-found.
func (r *pgTxResolver) bound(schema, name string) (*pgBoundRelation, error) {
	if schema == "" {
		schema = r.pinned
	}
	rel, ok := r.byName[pgRelKey{schema: schema, name: name}]
	if !ok {
		return nil, fmt.Errorf("pgsql: resolver has no bound relation for this name: %w", pgsql.ErrTypeResolutionUnavailable)
	}
	return rel, nil
}

// Columns returns column names in attnum order for a bound relation.
func (r *pgTxResolver) Columns(schema, name string) ([]string, error) {
	rel, err := r.bound(schema, name)
	if err != nil {
		return nil, err
	}
	out := make([]string, len(rel.columns))
	for i, c := range rel.columns {
		out[i] = c.Name
	}
	return out, nil
}

// ColumnMetadata returns the locked attribute map for a bound relation:
// attnum-ordered live columns with original attribute numbers, type OIDs, and
// typmods preserved — no truncation, no display-model filtering.
func (r *pgTxResolver) ColumnMetadata(schema, name string) ([]pgsql.ColumnMetadata, error) {
	rel, err := r.bound(schema, name)
	if err != nil {
		return nil, err
	}
	return rel.columns, nil
}

// CommonType returns PostgreSQL's own common-type verdict for two type OIDs —
// the same select_common_type a USING merge applies — probed natively on the
// attempt connection. The format_type names are never trusted: the probe
// reports which type each side actually bound and both must still equal the
// input OIDs, or the answer is evidence-unavailable rather than a guess.
func (r *pgTxResolver) CommonType(leftOID, rightOID uint32) (uint32, error) {
	if leftOID == rightOID {
		return leftOID, nil
	}
	key := [2]uint32{leftOID, rightOID}
	if v, ok := r.common[key]; ok {
		return v, nil
	}
	if err := r.arm(); err != nil {
		return 0, err
	}
	var lt, rt string
	if err := r.tx.QueryRow(r.ctx,
		`SELECT pg_catalog.format_type($1::pg_catalog.oid, NULL),
		        pg_catalog.format_type($2::pg_catalog.oid, NULL)`,
		leftOID, rightOID).Scan(&lt, &rt); err != nil {
		return 0, r.fail(err)
	}
	probe := fmt.Sprintf(
		`SELECT pg_catalog.pg_typeof((SELECT l.id FROM (SELECT NULL::%s AS id) AS l LIMIT 0))::pg_catalog.oid,
		        pg_catalog.pg_typeof((SELECT r.id FROM (SELECT NULL::%s AS id) AS r LIMIT 0))::pg_catalog.oid,
		        pg_catalog.pg_typeof((SELECT j.id FROM ((SELECT NULL::%s AS id) AS l
		                                       JOIN (SELECT NULL::%s AS id) AS r
		                                       USING (id)) AS j LIMIT 0))::pg_catalog.oid`,
		lt, rt, lt, rt)
	if err := r.arm(); err != nil {
		return 0, err
	}
	var boundL, boundR, merged uint32
	if err := r.tx.QueryRow(r.ctx, probe).Scan(&boundL, &boundR, &merged); err != nil {
		return 0, r.fail(err)
	}
	if boundL != leftOID || boundR != rightOID {
		return 0, fmt.Errorf("pgsql: common-type probe rebound %d/%d to %d/%d: %w",
			leftOID, rightOID, boundL, boundR, pgsql.ErrTypeResolutionUnavailable)
	}
	if merged == 0 {
		return 0, fmt.Errorf("pgsql: common-type probe returned no verdict: %w", pgsql.ErrTypeResolutionUnavailable)
	}
	r.common[key] = merged
	return merged, nil
}

// pgQualifiedName renders "<schema>"."<name>" for to_regclass; pgQuoteIdent
// mirrors the deparser's quoting so unusual identifiers stay exact.
func pgQualifiedName(schema, name string) string {
	return pgQuoteIdent(schema) + "." + pgQuoteIdent(name)
}

func pgQuoteIdent(ident string) string {
	return `"` + strings.ReplaceAll(ident, `"`, `""`) + `"`
}
