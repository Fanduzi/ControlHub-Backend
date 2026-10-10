// Package service verifies the transport result against the rewrite's own
// proof records before any row is delivered.
// input: copied FieldDescriptions, *pgsql.RewriteResult, *PGBindResult,
// connection TypeMap, private row buffer
// output: verifyPGPositions, projectPGPublicResult, pgColumnTypeName —
// per-position identity comparison (direct columns by TableOID +
// TableAttributeNumber, witnesses by DataTypeOID = confirmed reltype) plus
// witness-stripped public projection and stable G7 type naming
// pos: T7-S3 per-position verification (frozen G10 mechanism 5b) —
// expectations come only from the S1/S2 record set and the touch-confirmed
// bindings, never reverse-derived from the actual FDs; structural gaps are
// internal evidence failures, real binding contradictions are the controlled
// query_binding_mismatch rejection
// note: if this file changes, update this header and module README.md.
package service

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/fan/controlhub/internal/pgsql"
)

// verifyPGPositions compares the rewrite's expected transport layout with the
// actual FieldDescriptions. Every check consumes only pre-execution evidence:
// PublicProofs/Witnesses/WitnessPositions/SourceCatalog from the rewrite and
// the gate's touch-confirmed BoundRefs. Missing or inconsistent records are
// ErrEvidenceUnavailable; well-formed records contradicted by the actual
// binding are the frozen query_binding_mismatch rejection.
func verifyPGPositions(fds []pgconn.FieldDescription, rw *pgsql.RewriteResult, bind *PGBindResult) error {
	if rw == nil || bind == nil {
		return fmt.Errorf("%w: verification input missing", pgsql.ErrEvidenceUnavailable)
	}
	total := len(rw.Public) + len(rw.Witnesses)
	if len(fds) != total {
		return fmt.Errorf("%w: transport has %d columns, expected %d",
			pgsql.ErrEvidenceUnavailable, len(fds), total)
	}
	if len(rw.PublicProofs) != len(rw.Public) {
		return fmt.Errorf("%w: %d proofs for %d public columns",
			pgsql.ErrEvidenceUnavailable, len(rw.PublicProofs), len(rw.Public))
	}
	if len(rw.WitnessPositions) != len(rw.Witnesses) {
		return fmt.Errorf("%w: %d witness positions for %d witnesses",
			pgsql.ErrEvidenceUnavailable, len(rw.WitnessPositions), len(rw.Witnesses))
	}
	if rw.SourceCatalog == nil {
		return fmt.Errorf("%w: source catalog missing", pgsql.ErrEvidenceUnavailable)
	}

	// Coordinate map: every transport column is claimed by exactly one
	// witness position or one public proof — no overlap, no gap.
	coord := make([]bool, total) // true = witness
	for i, pos := range rw.WitnessPositions {
		if pos < 0 || pos >= total {
			return fmt.Errorf("%w: witness position %d outside transport", pgsql.ErrEvidenceUnavailable, pos)
		}
		if coord[pos] {
			return fmt.Errorf("%w: duplicate transport coordinate %d", pgsql.ErrEvidenceUnavailable, pos)
		}
		coord[pos] = true
		if err := verifyPGWitness(fds[pos], rw.Witnesses[i], rw, bind); err != nil {
			return err
		}
	}
	for i, proof := range rw.PublicProofs {
		if proof.TransportIndex < 0 || proof.TransportIndex >= total {
			return fmt.Errorf("%w: public proof %d transport index %d outside transport",
				pgsql.ErrEvidenceUnavailable, i, proof.TransportIndex)
		}
		if coord[proof.TransportIndex] {
			return fmt.Errorf("%w: public proof %d overlaps witness coordinate %d",
				pgsql.ErrEvidenceUnavailable, i, proof.TransportIndex)
		}
		coord[proof.TransportIndex] = false
		if err := verifyPGPublicColumn(fds[proof.TransportIndex], i, proof, rw, bind); err != nil {
			return err
		}
	}
	// Coverage: every entity occurrence must reach at least one final witness
	// position. A RefID of 0 is a legal occurrence id — coverage is counted
	// by membership, never by nonzero checks.
	covered := map[pgsql.SourceOccurrenceID]bool{}
	for _, w := range rw.Witnesses {
		for _, src := range w.CoveredSources {
			covered[src] = true
		}
	}
	for id, occ := range rw.SourceCatalog {
		if occ.Kind != pgsql.SourceEntityRangeVar {
			continue
		}
		if !covered[id] {
			return fmt.Errorf("%w: entity occurrence %d has no final witness coverage",
				pgsql.ErrEvidenceUnavailable, id)
		}
	}
	return nil
}

// verifyPGWitness checks one witness column at its final position: every
// covered source resolves through the catalog to a touch-confirmed BoundRef,
// and the recorded reltype must equal the actual DataTypeOID — the identity
// proof that survives a full lock-set exchange.
func verifyPGWitness(fd pgconn.FieldDescription, w pgsql.WitnessRecord, rw *pgsql.RewriteResult, bind *PGBindResult) error {
	if len(w.CoveredSources) == 0 {
		return fmt.Errorf("%w: witness %q covers no source", pgsql.ErrEvidenceUnavailable, w.Column)
	}
	if _, ok := rw.SourceCatalog[w.CarrierOccurrence]; !ok {
		return fmt.Errorf("%w: witness %q carrier %d unknown",
			pgsql.ErrEvidenceUnavailable, w.Column, w.CarrierOccurrence)
	}
	for _, src := range w.CoveredSources {
		occ, ok := rw.SourceCatalog[src]
		if !ok {
			return fmt.Errorf("%w: witness %q covers unknown source %d",
				pgsql.ErrEvidenceUnavailable, w.Column, src)
		}
		if occ.Kind != pgsql.SourceEntityRangeVar || !occ.HasRefID {
			return fmt.Errorf("%w: witness %q covers non-entity source %d",
				pgsql.ErrEvidenceUnavailable, w.Column, src)
		}
		bound := bind.Refs[occ.RefID]
		if bound == nil || !bound.TouchConfirmed {
			return fmt.Errorf("%w: witness %q covers unconfirmed binding %d",
				pgsql.ErrEvidenceUnavailable, w.Column, occ.RefID)
		}
		if fd.DataTypeOID != bound.RelTypeOID {
			return &pgsql.RejectError{
				Code:    "query_binding_mismatch",
				Message: "pgsql: witness column bound an unexpected relation type",
			}
		}
	}
	return nil
}

// verifyPGPublicColumn checks one public column's proof at its transport
// position: the record must be complete and resolvable, and a KnownColumn FD
// origin must equal the actual (TableOID, TableAttributeNumber) at that
// position. NoColumnOrigin makes no TableOID claim; Unresolved origins can
// never appear on a complete record.
func verifyPGPublicColumn(fd pgconn.FieldDescription, i int, proof pgsql.PublicColumnProof, rw *pgsql.RewriteResult, bind *PGBindResult) error {
	if proof.Resolution != pgsql.ProofComplete {
		return fmt.Errorf("%w: public proof %d is incomplete", pgsql.ErrEvidenceUnavailable, i)
	}
	for _, dep := range proof.Dependencies {
		occ, ok := rw.SourceCatalog[dep.Occurrence]
		if !ok {
			return fmt.Errorf("%w: public proof %d depends on unknown source %d",
				pgsql.ErrEvidenceUnavailable, i, dep.Occurrence)
		}
		switch occ.Kind {
		case pgsql.SourceEntityRangeVar, pgsql.SourceDerived, pgsql.SourceNonEntity:
		default:
			return fmt.Errorf("%w: public proof %d depends on non-terminal %s %d",
				pgsql.ErrEvidenceUnavailable, i, occ.Kind, dep.Occurrence)
		}
	}
	switch proof.FDOrigin.Kind {
	case pgsql.FDOriginKnownColumn:
		occ, ok := rw.SourceCatalog[proof.FDOrigin.Source.Occurrence]
		if !ok {
			return fmt.Errorf("%w: public proof %d FD origin source %d unknown",
				pgsql.ErrEvidenceUnavailable, i, proof.FDOrigin.Source.Occurrence)
		}
		if !pgSourceRefIn(proof.Dependencies, proof.FDOrigin.Source) {
			return fmt.Errorf("%w: public proof %d FD origin outside its dependencies",
				pgsql.ErrEvidenceUnavailable, i)
		}
		if occ.Kind != pgsql.SourceEntityRangeVar || !occ.HasRefID {
			return fmt.Errorf("%w: public proof %d FD origin is not a bound entity",
				pgsql.ErrEvidenceUnavailable, i)
		}
		bound := bind.Refs[occ.RefID]
		if bound == nil || !bound.TouchConfirmed {
			return fmt.Errorf("%w: public proof %d FD origin lacks a confirmed binding",
				pgsql.ErrEvidenceUnavailable, i)
		}
		if proof.FDOrigin.RelationOID != bound.Identity.RelationOID {
			return fmt.Errorf("%w: public proof %d FD origin relation %d is not the bound relation %d",
				pgsql.ErrEvidenceUnavailable, i, proof.FDOrigin.RelationOID, bound.Identity.RelationOID)
		}
		if fd.TableOID != proof.FDOrigin.RelationOID ||
			int16(fd.TableAttributeNumber) != proof.FDOrigin.AttributeNumber {
			return &pgsql.RejectError{
				Code:    "query_binding_mismatch",
				Message: "pgsql: public column bound an unexpected source position",
			}
		}
	case pgsql.FDOriginNoColumnOrigin:
		// A computed column can carry no (TableOID, AttrNum) origin and still
		// hold real dependencies — TableOID is never asserted here.
	case pgsql.FDOriginUnresolved:
		return fmt.Errorf("%w: public proof %d has an unresolved FD origin",
			pgsql.ErrEvidenceUnavailable, i)
	default:
		return fmt.Errorf("%w: public proof %d has invalid FD origin kind %d",
			pgsql.ErrEvidenceUnavailable, i, proof.FDOrigin.Kind)
	}
	return nil
}

func pgSourceRefIn(refs []pgsql.SourceRef, want pgsql.SourceRef) bool {
	for _, r := range refs {
		if r == want {
			return true
		}
	}
	return false
}

// projectPGPublicResult strips witness columns by recorded position and
// names each public column's database type from the connection TypeMap.
// Cells keep their materialized text verbatim — nothing is reformatted.
func projectPGPublicResult(fds []pgconn.FieldDescription, rows [][]*string, rw *pgsql.RewriteResult, tm *pgtype.Map) *PGVerifiedResult {
	cols := make([]PGResultColumn, len(rw.Public))
	for i, p := range rw.PublicProofs {
		fd := fds[p.TransportIndex]
		cols[i] = PGResultColumn{
			Name:         rw.Public[i],
			DatabaseType: pgColumnTypeName(tm, fd.DataTypeOID),
			DataTypeOID:  fd.DataTypeOID,
			TypeModifier: fd.TypeModifier,
			TableOID:     fd.TableOID,
			AttrNum:      int16(fd.TableAttributeNumber),
		}
	}
	out := make([][]*string, len(rows))
	for r, row := range rows {
		pub := make([]*string, len(rw.Public))
		for i, p := range rw.PublicProofs {
			pub[i] = row[p.TransportIndex]
		}
		out[r] = pub
	}
	return &PGVerifiedResult{Columns: cols, Rows: out}
}

// pgColumnTypeName resolves a FieldDescription OID through the connection
// TypeMap into the G7 type name (INT8/NUMERIC/TIMESTAMPTZ/_TEXT/BYTEA…). An
// OID the map cannot name is a stable "oid:<n>" marker — never guessed as
// TEXT and never silently dropped; the cell text is unaffected either way.
func pgColumnTypeName(tm *pgtype.Map, oid uint32) string {
	if tm != nil {
		if t, ok := tm.TypeForOID(oid); ok && t != nil && t.Name != "" {
			return strings.ToUpper(t.Name)
		}
	}
	return "oid:" + strconv.FormatUint(uint64(oid), 10)
}
