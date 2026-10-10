// Package service audits the attempt transaction's actually-held relation
// locks through an independent observation connection.
// input: attempt context with deadline, T2-validated *pgx.ConnConfig copy,
// PGBindResult approved/probe/edge records, recorded backend pid +
// virtualtransaction
// output: auditLockSet, collectDerivedIdentities, pgAuditVerdict —
// toast-attributed extra identities plus a three-state Verified/Mismatch/
// Unavailable verdict over the (database_oid, relation_oid) held set
// pos: T7-S4 lock-set audit (frozen G10 mechanism 5c) — the execution
// transaction stays open and locked while a second connection reads the
// backend's pg_locks rows in one shot; identity is (database, relation) with
// shared catalogs at database 0; only granted locks count as held evidence;
// every held lock must explain through approved ∪ probe whitelist ∪
// ownership edges (index/TOAST/inheritance), and every bound entity ref must
// appear in the held set (derived approved records are explainable, not
// required — a materialized view's deps are never locked)
// note: if this file changes, update this header and module README.md.
package service

import (
	"fmt"
	"strconv"
	"time"

	"github.com/fan/controlhub/internal/pgsql"
)

// pgAuditVerdict is the three-state lock-audit outcome.
type pgAuditVerdict int

const (
	pgAuditVerified pgAuditVerdict = iota
	pgAuditMismatch
	pgAuditUnavailable
)

// pgLockRow is one observed pg_locks row for the audited backend. Every
// nullable column arrives with a stable zero value so no record is dropped
// by scan shape — unexplained rows stay in the audit input.
type pgLockRow struct {
	locktype string
	mode     string
	granted  bool
	database uint32
	relation uint32
	vxid     string
}

// collectDerivedIdentities supplements the gate's approved set with the
// ownership edges S2 does not record: each approved relation's TOAST table
// (pg_class.reltoastrelid) and each TOAST table's own indexes. The probe runs
// inside the attempt transaction under the same remaining budget and keys by
// OID — never by name — so its output is traceable to approved relations,
// not learned from the observed lock set.
func (a *pgExecAttempt) collectDerivedIdentities() error {
	if a.bind == nil || len(a.bind.Approved) == 0 {
		return nil
	}
	bases := make([]int64, 0, len(a.bind.Approved))
	seen := map[uint32]bool{}
	for _, id := range a.bind.Approved {
		if seen[id.RelationOID] {
			continue
		}
		seen[id.RelationOID] = true
		bases = append(bases, int64(id.RelationOID))
	}
	if err := a.budget.arm(a.ctx, a.tx); err != nil {
		return err
	}
	rows, err := a.tx.Query(a.ctx,
		`SELECT c.oid::pg_catalog.oid, c.reltoastrelid::pg_catalog.oid, c.relisshared
		   FROM pg_catalog.pg_class c
		  WHERE c.oid::bigint = ANY($1::bigint[])`, bases)
	if err != nil {
		return err
	}
	var toast []int64
	for rows.Next() {
		var base, toastOID uint32
		var shared bool
		if err := rows.Scan(&base, &toastOID, &shared); err != nil {
			rows.Close()
			return err
		}
		if toastOID != 0 {
			toast = append(toast, int64(toastOID))
			a.derived = append(a.derived, PGRelationIdentity{DatabaseOID: a.bind.DatabaseOID, RelationOID: toastOID})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(toast) == 0 {
		return nil
	}
	// TOAST tables are never relisshared; their indexes follow the same rule.
	if err := a.budget.arm(a.ctx, a.tx); err != nil {
		return err
	}
	idx, err := a.tx.Query(a.ctx,
		`SELECT i.indexrelid::pg_catalog.oid, i.indrelid::pg_catalog.oid
		   FROM pg_catalog.pg_index i
		  WHERE i.indrelid::bigint = ANY($1::bigint[])`, toast)
	if err != nil {
		return err
	}
	defer idx.Close()
	for idx.Next() {
		var indexOID, toastOID uint32
		if err := idx.Scan(&indexOID, &toastOID); err != nil {
			return err
		}
		a.derived = append(a.derived, PGRelationIdentity{DatabaseOID: a.bind.DatabaseOID, RelationOID: indexOID})
	}
	return idx.Err()
}

// auditLockSet observes the attempt transaction's held locks through a second
// independent pool while the execution transaction still holds them. It maps
// onto the frozen three-state contract: Verified only when the transaction
// identity is confirmed, every held relation lock is explainable, and every
// approved entity has held-lock evidence; Mismatch rejects via
// query_binding_mismatch; Unavailable maps to the actual failure (timeout /
// cancelled / failed), never to a rejection.
func (a *pgExecAttempt) auditLockSet() error {
	if a.bind == nil || a.backendPID == 0 || a.virtualXID == "" {
		return fmt.Errorf("%w: lock audit lacks transaction identity evidence",
			pgsql.ErrEvidenceUnavailable)
	}
	obsPool, err := OpenPostgresPool(a.ctx, a.cfg)
	if err != nil {
		return err
	}
	a.obsPool = obsPool
	conn, err := obsPool.Acquire(a.ctx)
	if err != nil {
		return err
	}
	a.obsConn = conn
	// The observation share of the attempt budget: the server-side cap is
	// armed once at session level from the same remaining milliseconds.
	if ms, ok := pgRemainingStatementTimeoutMs(a.budget.deadline, time.Now()); ok {
		if _, err := conn.Exec(a.ctx,
			`SELECT pg_catalog.set_config('statement_timeout', $1, false)`,
			strconv.FormatInt(ms, 10)+"ms"); err != nil {
			return err
		}
	} else {
		return ErrQueryTimeout
	}
	rows, err := conn.Query(a.ctx,
		`SELECT l.locktype::text, l.mode::text, l.granted,
		        COALESCE(l.database, 0)::pg_catalog.oid,
		        COALESCE(l.relation, 0)::pg_catalog.oid,
		        COALESCE(l.virtualtransaction, '')
		   FROM pg_catalog.pg_locks l
		  WHERE l.pid = $1`, a.backendPID)
	if err != nil {
		return err
	}
	var observed []pgLockRow
	for rows.Next() {
		var r pgLockRow
		if err := rows.Scan(&r.locktype, &r.mode, &r.granted, &r.database, &r.relation, &r.vxid); err != nil {
			rows.Close()
			return err
		}
		observed = append(observed, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	explainable := map[PGRelationIdentity]bool{}
	for _, id := range a.bind.Approved {
		explainable[id] = true
	}
	for _, id := range a.bind.Probes {
		explainable[id] = true
	}
	for _, id := range a.derived {
		explainable[id] = true
	}
	// Required = the bound entity references themselves: parse takes a lock
	// on every referenced RangeVar, so each must appear in the held set.
	// Derived approved records (view deps, inherit children, indexes, TOAST)
	// stay in the explainable set only — a materialized view legitimately
	// carries dependency records for base relations that are never locked.
	required := map[PGRelationIdentity]bool{}
	var requiredSet []PGRelationIdentity
	for _, ref := range a.bind.Refs {
		if !required[ref.Identity] {
			required[ref.Identity] = true
			requiredSet = append(requiredSet, ref.Identity)
		}
	}
	verdict, _ := evaluatePGLockAudit(observed, a.virtualXID, explainable, requiredSet)
	switch verdict {
	case pgAuditVerified:
		return nil
	case pgAuditMismatch:
		return &pgsql.RejectError{
			Code:    "query_binding_mismatch",
			Message: "pgsql: held lock set contains unexplained access",
		}
	default:
		return fmt.Errorf("%w: lock audit evidence unavailable", ErrQueryBackendFailure)
	}
}

// evaluatePGLockAudit is the pure three-state verdict over one observed
// pg_locks snapshot: the identity proof (this backend's own granted
// virtualxid ExclusiveLock row matching the recorded virtualtransaction),
// held-set explainability, and approved-set coverage. Ungranted rows or a
// foreign virtualtransaction on our pid mean the evidence is inconsistent —
// Unavailable, not Mismatch.
func evaluatePGLockAudit(rows []pgLockRow, wantVXID string, explainable map[PGRelationIdentity]bool, required []PGRelationIdentity) (pgAuditVerdict, []PGRelationIdentity) {
	if len(rows) == 0 || wantVXID == "" {
		return pgAuditUnavailable, nil
	}
	identityOK := false
	held := map[PGRelationIdentity]bool{}
	for _, r := range rows {
		if !r.granted {
			// A waiting lock means the audited transaction is mid-block —
			// evidence inconsistent for an idle attempt.
			return pgAuditUnavailable, nil
		}
		if r.vxid != wantVXID {
			// A lock attributed to our pid but another transaction is
			// evidence we cannot explain.
			return pgAuditUnavailable, nil
		}
		switch r.locktype {
		case "virtualxid":
			if r.mode == "ExclusiveLock" {
				identityOK = true
			}
		case "relation":
			held[PGRelationIdentity{DatabaseOID: r.database, RelationOID: r.relation}] = true
		}
	}
	if !identityOK {
		return pgAuditUnavailable, nil
	}
	var unexplained []PGRelationIdentity
	for id := range held {
		if !explainable[id] {
			unexplained = append(unexplained, id)
		}
	}
	if len(unexplained) > 0 {
		return pgAuditMismatch, unexplained
	}
	for _, id := range required {
		if !held[id] {
			// An approved entity without held-lock evidence is absent
			// proof, not a proven contradiction.
			return pgAuditUnavailable, nil
		}
	}
	return pgAuditVerified, nil
}
