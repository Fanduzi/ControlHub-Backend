// Package pgsql implements the PostgreSQL governed read-only query front half.
// input: attempt-local RangeVar RefID mapping and rewritten FROM-item layouts
// output: SourceOccurrenceID/SourceRef/SourceOccurrence, FDOrigin, PublicColumnProof, ErrEvidenceUnavailable — exported verification records shared by rewrite output and the future executor
// pos: T7-S1 verification record types; identifies terminal source slots separately from propagation carriers and keeps public-column dependency completeness distinct from FD origin evidence
// note: if this file changes, update header and README.md
package pgsql

import (
	"errors"
	"fmt"
	"sort"
)

// SourceOccurrenceID identifies one source or carrier occurrence within a
// single rewrite attempt. Original RangeVar occurrences reuse their RefID;
// generated carriers receive negative IDs.
type SourceOccurrenceID int

type SourceOccurrenceKind string

const (
	SourceEntityRangeVar   SourceOccurrenceKind = "entity_range_var"
	SourceCTERangeVar      SourceOccurrenceKind = "cte_range_var"
	SourceDerived          SourceOccurrenceKind = "derived"
	SourceNonEntity        SourceOccurrenceKind = "non_entity"
	SourceGeneratedCarrier SourceOccurrenceKind = "generated_carrier"
)

// SourceOccurrence describes one occurrence in the attempt-local catalog.
// RefID is valid only when HasRefID is true.
type SourceOccurrence struct {
	ID       SourceOccurrenceID
	Kind     SourceOccurrenceKind
	RefID    RefID
	HasRefID bool
}

type SourceSlotKind int

const (
	SourceSlotUnknown SourceSlotKind = iota
	SourceSlotColumn
	SourceSlotWholeRow
)

// SourceSlot identifies one public column slot inside an occurrence. Ordinal
// is the occurrence-local public column index; WholeRow uses Ordinal -1.
type SourceSlot struct {
	Kind    SourceSlotKind
	Ordinal int
}

// SourceRef is an attempt-local reference to one terminal source slot.
type SourceRef struct {
	Occurrence SourceOccurrenceID
	Slot       SourceSlot
}

type FDOriginKind int

const (
	FDOriginUnresolved FDOriginKind = iota
	FDOriginNoColumnOrigin
	FDOriginKnownColumn
)

// FDOrigin records whether a transport column has an independently
// comparable PostgreSQL (TableOID, TableAttributeNumber) origin. Source must
// name one Dependency slot when Kind is FDOriginKnownColumn.
type FDOrigin struct {
	Kind            FDOriginKind
	Source          SourceRef
	RelationOID     uint32
	AttributeNumber int16
}

type ProofResolution int

const (
	ProofUnresolved ProofResolution = iota
	ProofComplete
)

// PublicColumnProof is aligned with InjectResult.Public and
// RewriteResult.Public by public-column index. TransportIndex uses the final
// FieldDescription coordinate system, which also contains witness columns.
type PublicColumnProof struct {
	TransportIndex int
	Resolution     ProofResolution
	Dependencies   []SourceRef
	FDOrigin       FDOrigin
}

// ErrEvidenceUnavailable marks missing or inconsistent internal verification
// evidence. It is not a governed rejection and must not be collapsed into a
// user-facing query rejection.
var ErrEvidenceUnavailable = errors.New("pgsql: verification evidence unavailable")

func columnSlot(ord int) SourceSlot {
	return SourceSlot{Kind: SourceSlotColumn, Ordinal: ord}
}

func wholeRowSlot() SourceSlot {
	return SourceSlot{Kind: SourceSlotWholeRow, Ordinal: -1}
}

func sortSourceIDs(ids []SourceOccurrenceID) []SourceOccurrenceID {
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func sortSourceRefs(refs []SourceRef) []SourceRef {
	sort.Slice(refs, func(i, j int) bool {
		a, b := refs[i], refs[j]
		if a.Occurrence != b.Occurrence {
			return a.Occurrence < b.Occurrence
		}
		if a.Slot.Kind != b.Slot.Kind {
			return a.Slot.Kind < b.Slot.Kind
		}
		return a.Slot.Ordinal < b.Slot.Ordinal
	})
	return refs
}

func unionSourceOccurrences(dst []SourceOccurrenceID, src []SourceOccurrenceID) []SourceOccurrenceID {
	seen := make(map[SourceOccurrenceID]struct{}, len(dst)+len(src))
	out := make([]SourceOccurrenceID, 0, len(dst)+len(src))
	for _, id := range dst {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	for _, id := range src {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

func unionSourceRefs(dst []SourceRef, src []SourceRef) []SourceRef {
	seen := make(map[SourceRef]struct{}, len(dst)+len(src))
	out := make([]SourceRef, 0, len(dst)+len(src))
	for _, r := range dst {
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	for _, r := range src {
		if _, ok := seen[r]; !ok {
			seen[r] = struct{}{}
			out = append(out, r)
		}
	}
	return out
}

func sourceRefIn(refs []SourceRef, want SourceRef) bool {
	for _, r := range refs {
		if r == want {
			return true
		}
	}
	return false
}

// validate checks that exported proof/witness records are internally
// addressable. It does not claim PostgreSQL execution evidence — only that
// the rewrite-time record set is complete enough for a later transaction to
// compare against.
func (out *InjectResult) validate(required []SourceOccurrenceID) error {
	if out == nil {
		return fmt.Errorf("%w: injection result is nil", ErrEvidenceUnavailable)
	}
	if out.SourceCatalog == nil {
		return fmt.Errorf("%w: source catalog is nil", ErrEvidenceUnavailable)
	}
	for id, src := range out.SourceCatalog {
		if src.ID != id {
			return fmt.Errorf("%w: source catalog key %d does not match occurrence %d", ErrEvidenceUnavailable, id, src.ID)
		}
		if id >= 0 {
			if !src.HasRefID || RefID(id) != src.RefID {
				return fmt.Errorf("%w: original occurrence %d lacks matching RefID", ErrEvidenceUnavailable, id)
			}
		} else if src.HasRefID {
			return fmt.Errorf("%w: generated occurrence %d carries an original RefID", ErrEvidenceUnavailable, id)
		}
	}
	if len(out.PublicProofs) != len(out.Public) {
		return fmt.Errorf("%w: %d public proofs for %d public columns", ErrEvidenceUnavailable, len(out.PublicProofs), len(out.Public))
	}
	if len(out.WitnessPositions) != len(out.Witnesses) {
		return fmt.Errorf("%w: %d witness positions for %d witness records", ErrEvidenceUnavailable, len(out.WitnessPositions), len(out.Witnesses))
	}
	total := len(out.Public) + len(out.Witnesses)
	coord := make(map[int]bool, total) // true = witness, false = public
	for i, pos := range out.WitnessPositions {
		if pos < 0 || pos >= total {
			return fmt.Errorf("%w: witness position %d outside %d output columns", ErrEvidenceUnavailable, pos, total)
		}
		if _, dup := coord[pos]; dup {
			return fmt.Errorf("%w: duplicate output coordinate %d", ErrEvidenceUnavailable, pos)
		}
		coord[pos] = true
		w := out.Witnesses[i]
		if len(w.CoveredSources) == 0 {
			return fmt.Errorf("%w: witness %q has empty covered sources", ErrEvidenceUnavailable, w.Column)
		}
		if _, ok := out.SourceCatalog[w.CarrierOccurrence]; !ok {
			return fmt.Errorf("%w: witness %q uses unknown carrier %d", ErrEvidenceUnavailable, w.Column, w.CarrierOccurrence)
		}
		for j, src := range w.CoveredSources {
			if j > 0 && src <= w.CoveredSources[j-1] {
				return fmt.Errorf("%w: witness %q covered sources are not sorted/deduplicated", ErrEvidenceUnavailable, w.Column)
			}
			occ, ok := out.SourceCatalog[src]
			if !ok {
				return fmt.Errorf("%w: witness %q covers unknown source %d", ErrEvidenceUnavailable, w.Column, src)
			}
			if occ.Kind != SourceEntityRangeVar || !occ.HasRefID {
				return fmt.Errorf("%w: witness %q covers non-entity source %d", ErrEvidenceUnavailable, w.Column, src)
			}
		}
	}
	for i, proof := range out.PublicProofs {
		if proof.TransportIndex < 0 || proof.TransportIndex >= total {
			return fmt.Errorf("%w: public proof %d uses invalid transport index %d", ErrEvidenceUnavailable, i, proof.TransportIndex)
		}
		if isWitness, dup := coord[proof.TransportIndex]; dup {
			return fmt.Errorf("%w: duplicate output coordinate %d", ErrEvidenceUnavailable, proof.TransportIndex)
		} else if isWitness {
			return fmt.Errorf("%w: public proof %d points at witness coordinate %d", ErrEvidenceUnavailable, i, proof.TransportIndex)
		}
		coord[proof.TransportIndex] = false
		for j, dep := range proof.Dependencies {
			if j > 0 && compareSourceRefs(dep, proof.Dependencies[j-1]) <= 0 {
				return fmt.Errorf("%w: public proof %d dependencies are not sorted/deduplicated", ErrEvidenceUnavailable, i)
			}
			occ, ok := out.SourceCatalog[dep.Occurrence]
			if !ok {
				return fmt.Errorf("%w: public proof %d uses unknown source %d", ErrEvidenceUnavailable, i, dep.Occurrence)
			}
			switch occ.Kind {
			case SourceEntityRangeVar, SourceDerived, SourceNonEntity:
			default:
				return fmt.Errorf("%w: public proof %d depends on non-terminal %s %d", ErrEvidenceUnavailable, i, occ.Kind, dep.Occurrence)
			}
		}
		switch proof.Resolution {
		case ProofComplete:
			if proof.FDOrigin.Kind == FDOriginUnresolved {
				return fmt.Errorf("%w: public proof %d is complete with unresolved FD origin", ErrEvidenceUnavailable, i)
			}
		case ProofUnresolved:
		default:
			return fmt.Errorf("%w: public proof %d has invalid resolution %d", ErrEvidenceUnavailable, i, proof.Resolution)
		}
		if proof.FDOrigin.Kind == FDOriginKnownColumn {
			if !sourceRefIn(proof.Dependencies, proof.FDOrigin.Source) {
				return fmt.Errorf("%w: public proof %d FD origin is outside dependencies", ErrEvidenceUnavailable, i)
			}
			if proof.FDOrigin.RelationOID == 0 {
				return fmt.Errorf("%w: public proof %d lacks relation OID", ErrEvidenceUnavailable, i)
			}
			if proof.FDOrigin.AttributeNumber <= 0 && proof.FDOrigin.Source.Slot.Kind != SourceSlotWholeRow {
				return fmt.Errorf("%w: public proof %d lacks attribute number", ErrEvidenceUnavailable, i)
			}
		}
	}
	if len(coord) != total {
		return fmt.Errorf("%w: output coordinate coverage has %d entries for %d columns", ErrEvidenceUnavailable, len(coord), total)
	}
	covered := map[SourceOccurrenceID]bool{}
	for _, w := range out.Witnesses {
		for _, src := range w.CoveredSources {
			covered[src] = true
		}
	}
	for _, req := range required {
		if _, ok := out.SourceCatalog[req]; !ok {
			return fmt.Errorf("%w: required source %d lacks catalog entry", ErrEvidenceUnavailable, req)
		}
		if !covered[req] {
			return fmt.Errorf("%w: required source %d has no final witness coverage", ErrEvidenceUnavailable, req)
		}
	}
	return nil
}

func compareSourceRefs(a, b SourceRef) int {
	if a.Occurrence < b.Occurrence {
		return -1
	}
	if a.Occurrence > b.Occurrence {
		return 1
	}
	if a.Slot.Kind < b.Slot.Kind {
		return -1
	}
	if a.Slot.Kind > b.Slot.Kind {
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
