// Package service verifies the transport result against the rewrite's own
// proof records before any row is delivered.
// input: *pgsql.RewriteResult, *PGBindResult, copied FieldDescriptions,
// private row buffer, connection TypeMap
// output: verifyPGExpectedLayout, verifyPGPositions,
// projectPGPublicResult, pgColumnTypeName — a pre-ExecParams expected-record
// validation (completeness, coordinate map, source/binding/coverage
// correspondence, slot and attribute-map legality) plus the post-execution
// per-position FD comparison, witness-stripped public projection, and stable
// G7 type naming
// pos: T7-S3 verification split (frozen G10 mechanism 5b) — expectations are
// validated against the S1/S2 record set and the touch-confirmed bindings
// BEFORE the transport statement is sent, never reverse-derived from actual
// FDs; structural gaps are internal evidence failures, real binding
// contradictions are the controlled query_binding_mismatch rejection
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

// pgCoordOwner tri-states one transport column coordinate. "Unclaimed" and
// "public" are distinct states: a proof pointing at a witness slot and a
// column nobody claims are different evidence defects.
type pgCoordOwner int

const (
	pgCoordUnclaimed pgCoordOwner = iota
	pgCoordWitness
	pgCoordPublic
)

// verifyPGExpectedLayout validates the expected record set BEFORE the
// transport SQL is sent. Everything here consumes only pre-execution
// evidence: the rewrite's Public/PublicProofs/Witnesses/WitnessPositions/
// SourceCatalog/Refs and the gate's touch-confirmed BoundRefs. Any defect is
// an internal evidence failure — the user statement is never executed
// against it.
func verifyPGExpectedLayout(rw *pgsql.RewriteResult, bind *PGBindResult) error {
	if rw == nil || bind == nil {
		return fmt.Errorf("%w: verification input missing", pgsql.ErrEvidenceUnavailable)
	}
	if len(rw.Public) > pgMaxResultColumns {
		return ErrQueryResultTooLarge
	}
	if len(rw.Witnesses) > pgMaxWitnessColumns {
		return fmt.Errorf("%w: %d witness columns exceed transport bound %d",
			pgsql.ErrEvidenceUnavailable, len(rw.Witnesses), pgMaxWitnessColumns)
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
	total := len(rw.Public) + len(rw.Witnesses)

	// Coordinate map: every transport column is claimed by exactly one
	// witness position or one public proof — no overlap, no gap, and no
	// overloaded boolean conflating "unclaimed" with "public".
	coord := make([]pgCoordOwner, total)
	for i, pos := range rw.WitnessPositions {
		if pos < 0 || pos >= total {
			return fmt.Errorf("%w: witness position %d outside transport", pgsql.ErrEvidenceUnavailable, pos)
		}
		if coord[pos] != pgCoordUnclaimed {
			return fmt.Errorf("%w: duplicate transport coordinate %d", pgsql.ErrEvidenceUnavailable, pos)
		}
		coord[pos] = pgCoordWitness
		if err := verifyPGWitnessExpected(rw.Witnesses[i], rw, bind); err != nil {
			return err
		}
	}
	for i, proof := range rw.PublicProofs {
		if proof.TransportIndex < 0 || proof.TransportIndex >= total {
			return fmt.Errorf("%w: public proof %d transport index %d outside transport",
				pgsql.ErrEvidenceUnavailable, i, proof.TransportIndex)
		}
		switch coord[proof.TransportIndex] {
		case pgCoordWitness:
			return fmt.Errorf("%w: public proof %d overlaps witness coordinate %d",
				pgsql.ErrEvidenceUnavailable, i, proof.TransportIndex)
		case pgCoordPublic:
			return fmt.Errorf("%w: duplicate transport coordinate %d",
				pgsql.ErrEvidenceUnavailable, proof.TransportIndex)
		}
		coord[proof.TransportIndex] = pgCoordPublic
		if err := verifyPGPublicProofExpected(i, proof, rw, bind); err != nil {
			return err
		}
	}
	for pos, owner := range coord {
		if owner == pgCoordUnclaimed {
			return fmt.Errorf("%w: transport coordinate %d is not claimed by any record",
				pgsql.ErrEvidenceUnavailable, pos)
		}
	}

	// Coverage must hold in both directions, iterated over the binding
	// record — never only over the catalog a rewrite defect could shrink:
	// every bound entity occurrence exists in the catalog as an entity source
	// and reaches at least one final witness position; every catalog entity
	// maps back to a confirmed binding and to the guard's own ref.
	covered := map[pgsql.SourceOccurrenceID]bool{}
	for _, w := range rw.Witnesses {
		for _, src := range w.CoveredSources {
			covered[src] = true
		}
	}
	refsByID := make(map[pgsql.RefID]pgsql.RangeVarRef, len(rw.Refs))
	for _, ref := range rw.Refs {
		if _, dup := refsByID[ref.RefID]; dup {
			return fmt.Errorf("%w: duplicate guard ref id %d", pgsql.ErrEvidenceUnavailable, ref.RefID)
		}
		refsByID[ref.RefID] = ref
	}
	for id, occ := range rw.SourceCatalog {
		if occ.Kind != pgsql.SourceEntityRangeVar {
			continue
		}
		if !occ.HasRefID {
			return fmt.Errorf("%w: entity occurrence %d lacks a RefID", pgsql.ErrEvidenceUnavailable, id)
		}
		ref, ok := refsByID[occ.RefID]
		if !ok || (ref.Kind != pgsql.RefEntity && ref.Kind != pgsql.RefQualified) {
			return fmt.Errorf("%w: entity occurrence %d does not map to a guard entity ref",
				pgsql.ErrEvidenceUnavailable, id)
		}
		if bind.Refs[occ.RefID] == nil || !bind.Refs[occ.RefID].TouchConfirmed {
			return fmt.Errorf("%w: entity occurrence %d lacks a confirmed binding",
				pgsql.ErrEvidenceUnavailable, id)
		}
		if !covered[id] {
			return fmt.Errorf("%w: entity occurrence %d has no final witness coverage",
				pgsql.ErrEvidenceUnavailable, id)
		}
	}
	for refID, bound := range bind.Refs {
		id := pgsql.SourceOccurrenceID(refID)
		occ, ok := rw.SourceCatalog[id]
		if !ok || occ.Kind != pgsql.SourceEntityRangeVar {
			return fmt.Errorf("%w: bound entity %d missing from rewrite coverage",
				pgsql.ErrEvidenceUnavailable, refID)
		}
		if !covered[id] || bound == nil {
			return fmt.Errorf("%w: bound entity %d has no final witness coverage",
				pgsql.ErrEvidenceUnavailable, refID)
		}
	}
	return nil
}

// verifyPGWitnessExpected validates one witness record's shape against the
// catalog and the binding: non-empty sorted coverage, a known carrier, every
// covered source an entity occurrence carrying a touch-confirmed BoundRef.
// The actual FD DataTypeOID comparison happens after execution.
func verifyPGWitnessExpected(w pgsql.WitnessRecord, rw *pgsql.RewriteResult, bind *PGBindResult) error {
	if len(w.CoveredSources) == 0 {
		return fmt.Errorf("%w: witness %q covers no source", pgsql.ErrEvidenceUnavailable, w.Column)
	}
	for i := 1; i < len(w.CoveredSources); i++ {
		if w.CoveredSources[i] <= w.CoveredSources[i-1] {
			return fmt.Errorf("%w: witness %q covered sources not sorted/deduplicated",
				pgsql.ErrEvidenceUnavailable, w.Column)
		}
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
	}
	return nil
}

// verifyPGPublicProofExpected validates one public column proof's expected
// shape: complete resolution, resolvable sorted dependencies on terminal
// sources, legal entity source slots resolvable inside the locked attribute
// map, and an FDOrigin whose declared attribute number is the one the source
// slot actually owns — a proof cannot claim an attnum its slot does not hold.
func verifyPGPublicProofExpected(i int, proof pgsql.PublicColumnProof, rw *pgsql.RewriteResult, bind *PGBindResult) error {
	if proof.Resolution != pgsql.ProofComplete {
		return fmt.Errorf("%w: public proof %d is incomplete", pgsql.ErrEvidenceUnavailable, i)
	}
	for j, dep := range proof.Dependencies {
		if j > 0 && comparePGSourceRefs(dep, proof.Dependencies[j-1]) <= 0 {
			return fmt.Errorf("%w: public proof %d dependencies are not sorted/deduplicated",
				pgsql.ErrEvidenceUnavailable, i)
		}
		occ, ok := rw.SourceCatalog[dep.Occurrence]
		if !ok {
			return fmt.Errorf("%w: public proof %d depends on unknown source %d",
				pgsql.ErrEvidenceUnavailable, i, dep.Occurrence)
		}
		switch occ.Kind {
		case pgsql.SourceEntityRangeVar:
			if err := verifyPGEntitySlot(dep.Slot, occ, bind); err != nil {
				return fmt.Errorf("%w: public proof %d dependency %d: %v",
					pgsql.ErrEvidenceUnavailable, i, dep.Occurrence, err)
			}
		case pgsql.SourceDerived, pgsql.SourceNonEntity:
			// Non-entity terminals own no physical attribute map; their
			// occurrence-local ordinals carry no attmap claim by S1 rule.
			if dep.Slot.Kind == pgsql.SourceSlotUnknown {
				return fmt.Errorf("%w: public proof %d dependency %d has unknown slot kind",
					pgsql.ErrEvidenceUnavailable, i, dep.Occurrence)
			}
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
		if proof.FDOrigin.Source.Slot.Kind == pgsql.SourceSlotColumn {
			ord := proof.FDOrigin.Source.Slot.Ordinal
			// The declared attribute number must be the attnum this slot
			// actually owns inside the locked attribute map — a proof and
			// an actual FD both claiming attnum2 still lie when slot0 owns
			// attnum1.
			if proof.FDOrigin.AttributeNumber != bound.Columns[ord].AttributeNumber ||
				proof.FDOrigin.RelationOID != bound.Columns[ord].RelationOID {
				return fmt.Errorf("%w: public proof %d FD origin attnum %d is not slot %d's attribute",
					pgsql.ErrEvidenceUnavailable, i, proof.FDOrigin.AttributeNumber, ord)
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

// verifyPGEntitySlot validates one entity source reference's slot: a legal
// kind, and for a column slot an ordinal inside the bound relation's locked
// attribute map. WholeRow uses Ordinal -1 by S1 rule.
func verifyPGEntitySlot(slot pgsql.SourceSlot, occ pgsql.SourceOccurrence, bind *PGBindResult) error {
	switch slot.Kind {
	case pgsql.SourceSlotWholeRow:
		if slot.Ordinal != -1 {
			return fmt.Errorf("whole-row slot carries ordinal %d", slot.Ordinal)
		}
		return nil
	case pgsql.SourceSlotColumn:
		if slot.Ordinal < 0 {
			return fmt.Errorf("column slot carries negative ordinal %d", slot.Ordinal)
		}
		bound := bind.Refs[occ.RefID]
		if bound == nil || !bound.TouchConfirmed {
			return fmt.Errorf("entity source lacks a confirmed binding")
		}
		if slot.Ordinal >= len(bound.Columns) {
			return fmt.Errorf("column ordinal %d outside attribute map of %d columns",
				slot.Ordinal, len(bound.Columns))
		}
		return nil
	default:
		return fmt.Errorf("unknown slot kind %d", slot.Kind)
	}
}

func comparePGSourceRefs(a, b pgsql.SourceRef) int {
	if a.Occurrence != b.Occurrence {
		if a.Occurrence < b.Occurrence {
			return -1
		}
		return 1
	}
	if a.Slot.Kind != b.Slot.Kind {
		if a.Slot.Kind < b.Slot.Kind {
			return -1
		}
		return 1
	}
	if a.Slot.Ordinal < b.Slot.Ordinal {
		return -1
	}
	if a.Slot.Ordinal > b.Slot.Ordinal {
		return 1
	}
	return 0
}

// verifyPGPositions runs the post-execution half: the actual
// FieldDescriptions are compared against the already-validated expectations
// at their recorded transport positions. Structural record validity is the
// pre-execution check's job — this layer only asks whether the physical
// result matches what was bound.
func verifyPGPositions(fds []pgconn.FieldDescription, rw *pgsql.RewriteResult, bind *PGBindResult) error {
	if rw == nil || bind == nil {
		return fmt.Errorf("%w: verification input missing", pgsql.ErrEvidenceUnavailable)
	}
	if len(fds) != len(rw.Public)+len(rw.Witnesses) {
		return fmt.Errorf("%w: transport has %d columns, expected %d",
			pgsql.ErrEvidenceUnavailable, len(fds), len(rw.Public)+len(rw.Witnesses))
	}
	for i, pos := range rw.WitnessPositions {
		if err := verifyPGWitnessFD(fds[pos], rw.Witnesses[i], rw, bind); err != nil {
			return err
		}
	}
	for i, proof := range rw.PublicProofs {
		if err := verifyPGPublicFD(fds[proof.TransportIndex], i, proof); err != nil {
			return err
		}
	}
	return nil
}

// verifyPGWitnessFD compares one witness column's actual DataTypeOID at its
// final position against every covered source's touch-confirmed reltype —
// the identity proof that survives a full lock-set exchange.
func verifyPGWitnessFD(fd pgconn.FieldDescription, w pgsql.WitnessRecord, rw *pgsql.RewriteResult, bind *PGBindResult) error {
	for _, src := range w.CoveredSources {
		occ, ok := rw.SourceCatalog[src]
		if !ok || occ.Kind != pgsql.SourceEntityRangeVar || !occ.HasRefID {
			return fmt.Errorf("%w: witness %q covers invalid source %d",
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

// verifyPGPublicFD compares one public column's actual (TableOID,
// TableAttributeNumber) at its transport position against the proof's
// declared KnownColumn origin. NoColumnOrigin makes no claim by S1 rule.
func verifyPGPublicFD(fd pgconn.FieldDescription, i int, proof pgsql.PublicColumnProof) error {
	if proof.FDOrigin.Kind != pgsql.FDOriginKnownColumn {
		return nil
	}
	if fd.TableOID != proof.FDOrigin.RelationOID ||
		int16(fd.TableAttributeNumber) != proof.FDOrigin.AttributeNumber {
		return &pgsql.RejectError{
			Code:    "query_binding_mismatch",
			Message: "pgsql: public column bound an unexpected source position",
		}
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
