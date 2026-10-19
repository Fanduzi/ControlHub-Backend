// Package service holds the private related-record navigation substrate for
// the keyed PG entry.
// input: resolved BoundTargetAccess (validated *pgx.ConnConfig + database),
// the validated source identity and ordered locator strings
// output: pgNavigationBinding, pgNavigationColumnPair, preparePGNavigation,
// newPGNavigationPlan, snapshotPGNavigationBinding, errPGNavNoSource,
// navPrepFail — the immutable FK mapping the attempt's locked recheck must
// reproduce, plus bare-sentinel canonicalization of every preparation fault
// pos: PG-MULTI-ENTRY stage 3 — preparation reads the FK mapping once in its
// own bounded READ ONLY metadata transaction before claim (fixed probe SQL
// only, no Guard and no user result SQL); the claim attempt later re-runs the
// same SQL under held ACCESS SHARE locks and compares the entire mapping.
// The binding carries canonical catalog identity — never locator values,
// SQL, or caller-controlled OID lists.
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// pgNavigationMetadataSQL is the fixed FK-mapping probe — preparation and the
// in-attempt locked recheck run this exact statement with (source schema,
// source object, foreign-key name) as bind parameters. It returns at most 17
// ordered rows: db oid, constraint oid, source oid/reltype/namespace/relname/
// relkind, target oid/reltype/namespace/relname/relkind, key position, both
// key-array cardinalities, and per-side attnum/attname/atttypid/atttypmod.
const pgNavigationMetadataSQL = `SELECT db.oid::pg_catalog.oid,
       con.oid::pg_catalog.oid,
       src.oid::pg_catalog.oid, src.reltype::pg_catalog.oid,
       sn.nspname, src.relname, src.relkind::text,
       dst.oid::pg_catalog.oid, dst.reltype::pg_catalog.oid,
       dn.nspname, dst.relname, dst.relkind::text,
       keys.pos, pg_catalog.cardinality(con.conkey), pg_catalog.cardinality(con.confkey),
       la.attnum, la.attname, la.atttypid::pg_catalog.oid, la.atttypmod,
       ra.attnum, ra.attname, ra.atttypid::pg_catalog.oid, ra.atttypmod
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class src ON src.oid = con.conrelid
JOIN pg_catalog.pg_namespace sn ON sn.oid = src.relnamespace
JOIN pg_catalog.pg_class dst ON dst.oid = con.confrelid
JOIN pg_catalog.pg_namespace dn ON dn.oid = dst.relnamespace
JOIN pg_catalog.pg_database db ON db.datname = pg_catalog.current_database()
JOIN LATERAL pg_catalog.generate_subscripts(con.conkey, 1) AS keys(pos) ON true
JOIN pg_catalog.pg_attribute la
  ON la.attrelid = src.oid AND la.attnum = con.conkey[keys.pos]
 AND la.attnum > 0 AND NOT la.attisdropped
JOIN pg_catalog.pg_attribute ra
  ON ra.attrelid = dst.oid AND ra.attnum = con.confkey[keys.pos]
 AND ra.attnum > 0 AND NOT ra.attisdropped
WHERE con.contype = 'f'
  AND sn.nspname::text = $1::text AND src.relname::text = $2::text AND con.conname::text = $3::text
  AND src.relkind IN ('r', 'p') AND dst.relkind IN ('r', 'p')
  AND pg_catalog.has_schema_privilege(sn.oid, 'USAGE')
  AND pg_catalog.has_schema_privilege(dn.oid, 'USAGE')
  AND pg_catalog.has_table_privilege(src.oid, 'SELECT')
ORDER BY con.oid, keys.pos
LIMIT 17`

// errPGNavMetaInternal is the fixed internal failure for navigation metadata
// machinery — driver text, SQL, OIDs, and catalog detail never surface.
var errPGNavMetaInternal = fmt.Errorf("%w: pg navigation metadata", ErrQueryBackendFailure)

// errPGNavNoSource marks the empty mapping result: the named source relation
// or foreign key is invisible to this account — the caller maps it to the
// controlled not-found refusal, never to an internal failure.
var errPGNavNoSource = errors.New("pg navigation: no source mapping")

// pgNavigationRelation is the canonical physical identity of one side of the
// FK mapping — confirmed by the locked catalog, never by request spelling.
type pgNavigationRelation struct {
	oid     uint32
	reltype uint32
	kind    string
	schema  string
	name    string
}

// pgNavigationColumnPair is one ordered FK column pair in constraint order:
// the local (source-side) attribute then the referenced (target-side) one.
type pgNavigationColumnPair struct {
	localAttnum  int16
	localName    string
	localTypeOID uint32
	localTypmod  int32
	refAttnum    int16
	refName      string
	refTypeOID   uint32
	refTypmod    int32
}

// pgNavigationBinding is the immutable FK mapping preparation produced and
// the in-attempt verify stage re-proves: database and constraint OIDs, both
// relations' physical identities, the FK name, and the ordered column pairs.
type pgNavigationBinding struct {
	databaseOID   uint32
	constraintOID uint32
	fkName        string
	source        pgNavigationRelation
	target        pgNavigationRelation
	pairs         []pgNavigationColumnPair
}

// sameAs reports whether two bindings are byte-for-byte the same physical
// mapping — the verify stage's drift test.
func (b *pgNavigationBinding) sameAs(o *pgNavigationBinding) bool {
	if o == nil || b.databaseOID != o.databaseOID || b.constraintOID != o.constraintOID ||
		b.fkName != o.fkName || b.source != o.source || b.target != o.target ||
		len(b.pairs) != len(o.pairs) {
		return false
	}
	for i, p := range b.pairs {
		if p != o.pairs[i] {
			return false
		}
	}
	return true
}

// readPGNavigationBinding drains the fixed metadata probe and validates the
// frozen mapping contract: 1..16 rows, keys.pos contiguous 1..n, both
// cardinalities equal n, every row carrying the same constraint/source/target
// identity, and no missing attributes. A malformed catalog answer is the
// machinery fault, never a partial binding.
func readPGNavigationBinding(rows pgx.Rows, fkName string) (*pgNavigationBinding, error) {
	defer rows.Close()
	var binding *pgNavigationBinding
	var conkey, confkey, firstConkey, firstConfkey int32
	for rows.Next() {
		var (
			dbOID, conOID      uint32
			srcOID, srcReltype uint32
			srcSchema, srcName string
			srcKind            string
			dstOID, dstReltype uint32
			dstSchema, dstName string
			dstKind            string
			pos                int32
			laNum              int16
			laName             string
			laType             uint32
			laTypmod           int32
			raNum              int16
			raName             string
			raType             uint32
			raTypmod           int32
		)
		if err := rows.Scan(&dbOID, &conOID,
			&srcOID, &srcReltype, &srcSchema, &srcName, &srcKind,
			&dstOID, &dstReltype, &dstSchema, &dstName, &dstKind,
			&pos, &conkey, &confkey,
			&laNum, &laName, &laType, &laTypmod,
			&raNum, &raName, &raType, &raTypmod); err != nil {
			return nil, err
		}
		if binding == nil {
			binding = &pgNavigationBinding{
				databaseOID:   dbOID,
				constraintOID: conOID,
				fkName:        fkName,
				source:        pgNavigationRelation{oid: srcOID, reltype: srcReltype, kind: srcKind, schema: srcSchema, name: srcName},
				target:        pgNavigationRelation{oid: dstOID, reltype: dstReltype, kind: dstKind, schema: dstSchema, name: dstName},
			}
			firstConkey, firstConfkey = conkey, confkey
		} else if dbOID != binding.databaseOID || conOID != binding.constraintOID ||
			conkey != firstConkey || confkey != firstConfkey ||
			srcOID != binding.source.oid || srcReltype != binding.source.reltype ||
			srcSchema != binding.source.schema || srcName != binding.source.name || srcKind != binding.source.kind ||
			dstOID != binding.target.oid || dstReltype != binding.target.reltype ||
			dstSchema != binding.target.schema || dstName != binding.target.name || dstKind != binding.target.kind {
			return nil, errPGNavMetaInternal
		}
		if pos != int32(len(binding.pairs)+1) || conkey != confkey ||
			laNum <= 0 || raNum <= 0 || laName == "" || raName == "" {
			return nil, errPGNavMetaInternal
		}
		binding.pairs = append(binding.pairs, pgNavigationColumnPair{
			localAttnum: laNum, localName: laName, localTypeOID: laType, localTypmod: laTypmod,
			refAttnum: raNum, refName: raName, refTypeOID: raType, refTypmod: raTypmod,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, nil
	}
	// The probe caps at 17 rows so overflow arrives as data, not a truncated
	// fill: more than 16 pairs or a cardinality disagreement is a fault.
	if len(binding.pairs) > model.MaxLocalValuesCount ||
		int32(len(binding.pairs)) != firstConkey || int32(len(binding.pairs)) != firstConfkey {
		return nil, errPGNavMetaInternal
	}
	return binding, nil
}

// pgNavPrepareFunc is the service's navigation metadata preparation seam —
// production installs preparePGNavigation through the constructor.
type pgNavPrepareFunc func(ctx context.Context, cfg *pgx.ConnConfig, database string, source model.RelatedRecordNavigationSource, timeout time.Duration) (*pgNavigationBinding, error)

// preparePGNavigation is the production preparation function: one bounded
// READ ONLY metadata transaction on the resolved connection — fixed probe
// only, no Guard, no user SQL. It returns the immutable binding; errors are
// classified onto the entry's taxonomy: context.Canceled, ErrQueryTimeout,
// errPGNavNoSource, or the internal machinery sentinel.
func preparePGNavigation(ctx context.Context, cfg *pgx.ConnConfig, database string, source model.RelatedRecordNavigationSource, timeout time.Duration) (_ *pgNavigationBinding, err error) {
	prepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	budget, berr := pgAttemptBudgetFromContext(prepCtx)
	if berr != nil {
		return nil, navPrepFail(berr)
	}
	pool, err := OpenPostgresPool(prepCtx, cfg)
	if err != nil {
		return nil, navPrepFail(err)
	}
	defer pool.Close()
	conn, err := pool.Acquire(prepCtx)
	if err != nil {
		return nil, navPrepFail(err)
	}
	defer conn.Release()
	tx, err := conn.BeginTx(prepCtx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, navPrepFail(err)
	}
	// Every exit rolls back under an independent bounded context — a canceled
	// request still releases the transaction promptly.
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.WithoutCancel(prepCtx), pgExecCleanupWindow)
		defer cleanupCancel()
		if rbErr := tx.Rollback(cleanup); rbErr != nil && err == nil {
			err = errPGNavMetaInternal
		}
	}()
	if err := budget.arm(prepCtx, tx); err != nil {
		return nil, navPrepFail(err)
	}
	// The pool binding is only as good as the database it actually landed
	// on — the fixed probe keys by current_database(), so confirm it.
	var current string
	if err := tx.QueryRow(prepCtx, `SELECT pg_catalog.current_database()`).Scan(&current); err != nil {
		return nil, navPrepFail(err)
	}
	if current != database {
		return nil, errPGNavMetaInternal
	}
	rows, err := tx.Query(prepCtx, pgNavigationMetadataSQL, source.Schema, source.Object, source.ForeignKey)
	if err != nil {
		return nil, navPrepFail(err)
	}
	binding, err := readPGNavigationBinding(rows, source.ForeignKey)
	if err != nil {
		return nil, navPrepFail(err)
	}
	if binding == nil {
		return nil, errPGNavNoSource
	}
	return binding, nil
}

// navPrepFail classifies preparation failures: cancellation and deadlines
// keep their real category, the safe fixed sentinels (pool factory, no-source,
// internal machinery) canonicalize to their bare form — a wrapped or joined
// error can carry secrets, so the returned value is always the sentinel
// itself, never the caller's error — and everything else is the fixed
// internal machinery sentinel.
func navPrepFail(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.Canceled):
		return context.Canceled
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, ErrQueryTimeout), isPGQueryCanceled(err):
		return ErrQueryTimeout
	case errors.Is(err, ErrPGVersionUnsupported):
		return ErrPGVersionUnsupported
	case errors.Is(err, ErrPGConnectFailed):
		return ErrPGConnectFailed
	case errors.Is(err, ErrPGVersionReadFailed):
		return ErrPGVersionReadFailed
	case errors.Is(err, errPGNavNoSource):
		return errPGNavNoSource
	case errors.Is(err, errPGNavMetaInternal):
		return errPGNavMetaInternal
	default:
		return errPGNavMetaInternal
	}
}

// newPGNavigationPlan renders the trusted locator plan from the prepared
// binding: SELECT * FROM the quoted referenced schema/object WHERE each
// quoted referenced column equals its ordinal $k — values[i] binds parameter
// i+1. No LIMIT is generated; the G6 rewrite owns the window. Values are
// re-encoded through the shared text contract and copied — no caller slice
// reaches the attempt.
func newPGNavigationPlan(binding *pgNavigationBinding, values []string) (*pgParameterPlan, error) {
	if binding == nil || len(binding.pairs) == 0 || len(values) != len(binding.pairs) {
		return nil, fmt.Errorf("%w: navigation plan does not match binding", pgsql.ErrEvidenceUnavailable)
	}
	var sb strings.Builder
	sb.WriteString("SELECT * FROM " + pgQualifiedName(binding.target.schema, binding.target.name) + " WHERE ")
	owned := make([]string, 0, len(values))
	for i, pair := range binding.pairs {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(pgQuoteIdent(pair.refName) + " = $" + strconv.Itoa(i+1))
		text, err := encodePGParameterText(values[i])
		if err != nil {
			return nil, err
		}
		owned = append(owned, text)
	}
	return &pgParameterPlan{
		statement: sb.String(),
		values:    owned,
		source:    pgParameterSourceNavigation,
	}, nil
}

// validPGNavKind limits physical mapping sides to the ordinary/partitioned
// table kinds the fixed probe itself requires.
func validPGNavKind(kind string) bool {
	return kind == "r" || kind == "p"
}

// pgNavNameOK limits binding names to valid non-empty text — catalog names
// can legally carry trailing spaces and mixed case, so nothing is trimmed or
// normalized; NUL and invalid UTF-8 are rejected.
func pgNavNameOK(s string) bool {
	return s != "" && utf8.ValidString(s) && strings.IndexByte(s, 0) < 0
}

// snapshotPGNavigationBinding takes an independent copy of a prepared
// binding — required identities must be complete (positive OIDs, r/p kinds,
// valid names) and every column pair well-formed (positive attnums, nonzero
// type OIDs, valid names) within the 1..16 contract. The snapshot shares no
// backing storage, so a preparation result mutated after the fact can never
// reach the attempt.
func snapshotPGNavigationBinding(nav *pgNavigationBinding) (*pgNavigationBinding, error) {
	if nav == nil {
		return nil, nil
	}
	if nav.databaseOID == 0 || nav.constraintOID == 0 || !pgNavNameOK(nav.fkName) ||
		nav.source.oid == 0 || nav.source.reltype == 0 || !validPGNavKind(nav.source.kind) ||
		!pgNavNameOK(nav.source.schema) || !pgNavNameOK(nav.source.name) ||
		nav.target.oid == 0 || nav.target.reltype == 0 || !validPGNavKind(nav.target.kind) ||
		!pgNavNameOK(nav.target.schema) || !pgNavNameOK(nav.target.name) ||
		len(nav.pairs) == 0 || len(nav.pairs) > model.MaxLocalValuesCount {
		return nil, fmt.Errorf("%w: navigation binding is incomplete", pgsql.ErrEvidenceUnavailable)
	}
	for _, p := range nav.pairs {
		if p.localAttnum <= 0 || p.refAttnum <= 0 || p.localTypeOID == 0 || p.refTypeOID == 0 ||
			!pgNavNameOK(p.localName) || !pgNavNameOK(p.refName) {
			return nil, fmt.Errorf("%w: navigation binding is incomplete", pgsql.ErrEvidenceUnavailable)
		}
	}
	cp := *nav
	cp.pairs = append([]pgNavigationColumnPair(nil), nav.pairs...)
	return &cp, nil
}
