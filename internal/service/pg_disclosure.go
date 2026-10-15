// Package service wires disclosure governance onto the governed PostgreSQL
// execution chain.
// input: attempt context with deadline, PGExecuteInput, target resource id,
// QueryDisclosureReader (T9-A five-part canonical policy lookup)
// output: ExecutePGGovernedDisclosed, PGDisclosureInput, PGDisclosedResult,
// PGDisclosedColumn — the internal disclosed result (per-column
// DisplayMode/CopyAllowed, masked native-text rows) or the frozen classified
// error; the plan lives exactly one attempt and never survives past it
// pos: T9-B disclosure closure (issue #117, frozen G9 stage order +
// G10 mechanism 5b records) — the preflight consumes THIS attempt's bound and
// pre-wire-validated provenance before any user byte reaches the wire, and
// the transform runs only after verification, lock audit, and cleanup all
// converged; a failed or unverified attempt never reaches disclosure
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// PGDisclosureInput bundles the disclosure-decorated execution inputs. The
// policy reader is the T9-A canonical five-part lookup — preflight uses the
// attempt's remaining budget through the shared attempt context.
type PGDisclosureInput struct {
	Execute          PGExecuteInput
	TargetResourceID uint64
	Policies         QueryDisclosureReader
}

// PGDisclosedColumn is one delivered public column with the server-owned
// disclosure decision applied.
type PGDisclosedColumn struct {
	PGResultColumn
	DisplayMode model.ResultDisclosureMode
	CopyAllowed bool
}

// PGDisclosedResult is the internal output after disclosure transformation.
// Rows keep native text: nil is SQL NULL, a non-nil empty string is a
// zero-length value, and a masked non-NULL value is the fixed replacement —
// the original is never retained. Provenance records pass through read-only
// for downstream evidence stages.
type PGDisclosedResult struct {
	Columns      []PGDisclosedColumn
	Rows         [][]*string
	Page         pgsql.PageResult
	Refs         []pgsql.RangeVarRef
	PublicProofs []pgsql.PublicColumnProof
	Sources      map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence
	BoundRefs    map[pgsql.RefID]*PGBoundRef
}

// pgPolicyKey is one canonical five-part disclosure scope.
type pgPolicyKey struct {
	database string
	schema   string
	object   string
	column   string
}

// pgColumnDecision is one public column's synthesized verdict: the union of
// its governed sources' policies — raw only when every source allows raw,
// masked when any source is masked_no_copy.
type pgColumnDecision struct {
	mode        model.ResultDisclosureMode
	copyAllowed bool
	keys        []pgPolicyKey // resolved scopes, for diagnostics/evidence only
}

// pgDisclosurePlan is the per-attempt preflight output — aligned with the
// public columns by position, never by name.
type pgDisclosurePlan struct {
	columns []pgColumnDecision
}

// pgDisclosureStage carries one attempt's disclosure inputs and collects its
// plan. It is created per call, lives inside the attempt, and is consumed by
// the post-cleanup transform — no global or cross-attempt state.
type pgDisclosureStage struct {
	target uint64
	reader QueryDisclosureReader
	plan   *pgDisclosurePlan
}

// ExecutePGGovernedDisclosed runs the full governed attempt with disclosure
// governance woven in: provenance-bound policy preflight before the user
// statement is sent, then — only after verification, audit, and cleanup all
// succeed — the per-position transform into the disclosed result. A policy
// refusal, an internal evidence fault, a policy-store failure, or any T7
// failure all return no result.
func ExecutePGGovernedDisclosed(ctx context.Context, in PGDisclosureInput) (*PGDisclosedResult, error) {
	if in.Policies == nil || in.TargetResourceID == 0 {
		return nil, ErrQueryValidationFailed
	}
	stage := &pgDisclosureStage{target: in.TargetResourceID, reader: in.Policies}
	verified, err := runPGGovernedAttempt(ctx, in.Execute, stage)
	if err != nil {
		return nil, err
	}
	return applyPGDisclosure(stage.plan, verified)
}

// disclosurePreflight resolves every public column's governed sources into
// canonical policy scopes and evaluates them inside the attempt's remaining
// budget — before the transport statement exists on the wire. The records it
// reads are this attempt's already-validated rewrite output and confirmed
// bindings; it never re-parses the statement and never renames anything.
func (a *pgExecAttempt) disclosurePreflight(stage *pgDisclosureStage, rw *pgsql.RewriteResult, bind *PGBindResult) error {
	plan, err := resolvePGDisclosurePlan(a.ctx, stage.reader, stage.target, a.database, rw, bind)
	if err != nil {
		return err
	}
	stage.plan = plan
	return nil
}

// resolvePGDisclosurePlan maps each public column's terminal dependencies to
// canonical five-part policy keys — entity slots resolve through the bound
// ref's locked attribute map, whole-row slots expand over that map, and
// non-entity terminals carry no governed key. A column with a complete proof
// and no governed source is the definitive no-source case (same class as a
// literal-only projection): raw_copy_allowed without a lookup. Missing or
// contradictory internal records are evidence faults, never a guessed entity.
func resolvePGDisclosurePlan(ctx context.Context, reader QueryDisclosureReader, target uint64, database string, rw *pgsql.RewriteResult, bind *PGBindResult) (*pgDisclosurePlan, error) {
	if rw == nil || bind == nil {
		return nil, fmt.Errorf("%w: disclosure preflight input missing", pgsql.ErrEvidenceUnavailable)
	}
	plan := &pgDisclosurePlan{columns: make([]pgColumnDecision, 0, len(rw.PublicProofs))}
	cache := map[pgPolicyKey]model.ResultDisclosureMode{}
	for i, proof := range rw.PublicProofs {
		if proof.Resolution != pgsql.ProofComplete {
			return nil, fmt.Errorf("%w: public proof %d is incomplete", pgsql.ErrEvidenceUnavailable, i)
		}
		var keys []pgPolicyKey
		for _, dep := range proof.Dependencies {
			occ, ok := rw.SourceCatalog[dep.Occurrence]
			if !ok {
				return nil, fmt.Errorf("%w: public proof %d depends on unknown source %d",
					pgsql.ErrEvidenceUnavailable, i, dep.Occurrence)
			}
			switch occ.Kind {
			case pgsql.SourceEntityRangeVar:
				bound := bind.Refs[occ.RefID]
				if !occ.HasRefID || bound == nil || !bound.TouchConfirmed || bound.CatalogName == "" {
					return nil, fmt.Errorf("%w: public proof %d source %d lacks a confirmed binding",
						pgsql.ErrEvidenceUnavailable, i, dep.Occurrence)
				}
				switch dep.Slot.Kind {
				case pgsql.SourceSlotColumn:
					ord := dep.Slot.Ordinal
					if ord < 0 || ord >= len(bound.Columns) {
						return nil, fmt.Errorf("%w: public proof %d source %d column ordinal %d outside attribute map",
							pgsql.ErrEvidenceUnavailable, i, dep.Occurrence, ord)
					}
					keys = append(keys, pgPolicyKey{
						database: database, schema: bound.Schema,
						object: bound.CatalogName, column: bound.Columns[ord].Name,
					})
				case pgsql.SourceSlotWholeRow:
					// A whole-row reference carries every column of the
					// locked attribute map — expand it into the real column
					// scopes rather than fabricating a "*" key.
					if len(bound.Columns) == 0 {
						return nil, fmt.Errorf("%w: public proof %d source %d whole-row over empty attribute map",
							pgsql.ErrEvidenceUnavailable, i, dep.Occurrence)
					}
					for _, col := range bound.Columns {
						keys = append(keys, pgPolicyKey{
							database: database, schema: bound.Schema,
							object: bound.CatalogName, column: col.Name,
						})
					}
				default:
					return nil, fmt.Errorf("%w: public proof %d source %d has unknown slot kind %d",
						pgsql.ErrEvidenceUnavailable, i, dep.Occurrence, dep.Slot.Kind)
				}
			case pgsql.SourceDerived, pgsql.SourceNonEntity:
				// Terminal non-entity sources (derived locals, VALUES,
				// RangeFunction output) hold no governed relation identity —
				// the spec governs relation references only.
			default:
				return nil, fmt.Errorf("%w: public proof %d depends on non-terminal %s %d",
					pgsql.ErrEvidenceUnavailable, i, occ.Kind, dep.Occurrence)
			}
		}
		if len(keys) == 0 {
			plan.columns = append(plan.columns, pgColumnDecision{
				mode:        model.ResultDisclosureRawCopyAllowed,
				copyAllowed: true,
			})
			continue
		}
		dec := pgColumnDecision{mode: model.ResultDisclosureRawCopyAllowed, copyAllowed: true}
		seen := map[pgPolicyKey]bool{}
		for _, key := range keys {
			mode, ok := cache[key]
			if !ok {
				policy, err := reader.GetByScope(ctx, target, key.database, key.schema, key.object, key.column)
				if err != nil {
					if errors.Is(err, sql.ErrNoRows) {
						return nil, fmt.Errorf("%w: no policy for %s.%s.%s.%s",
							ErrQueryDisclosureBlocked, key.database, key.schema, key.object, key.column)
					}
					// Store failures surface only the fixed sentinel — the
					// driver/store error text and object never leave the
					// disclosure boundary. Context identity is preserved as
					// the bare standard sentinel so cancellation/deadline
					// still classify without re-wrapping sensitive text.
					if errors.Is(err, context.Canceled) {
						return nil, context.Canceled
					}
					if errors.Is(err, context.DeadlineExceeded) {
						return nil, context.DeadlineExceeded
					}
					return nil, ErrQueryDisclosureBackendFailure
				}
				if err := policy.Mode.Validate(); err != nil {
					// Fail closed without echoing the stored mode or its
					// validation error — only the scope is named.
					return nil, fmt.Errorf("%w: invalid stored mode for %s.%s.%s.%s",
						ErrQueryDisclosureBlocked, key.database, key.schema, key.object, key.column)
				}
				mode = policy.Mode
				cache[key] = mode
			}
			if !seen[key] {
				seen[key] = true
				dec.keys = append(dec.keys, key)
			}
			// Any masked source wins the column — no source is picked to
			// cover the others.
			if mode == model.ResultDisclosureMaskedNoCopy {
				dec.mode = model.ResultDisclosureMaskedNoCopy
				dec.copyAllowed = false
			}
		}
		plan.columns = append(plan.columns, dec)
	}
	return plan, nil
}

// applyPGDisclosure transforms the verified result per the plan after the
// attempt fully converged. Structural damage — plan/column/row-shape
// mismatch — is disclosure machinery failure, never a policy refusal. The
// input rows and provenance records are never mutated; masked cells replace
// non-NULL values with the fixed marker while NULL stays nil.
func applyPGDisclosure(plan *pgDisclosurePlan, verified *PGVerifiedResult) (*PGDisclosedResult, error) {
	if plan == nil || verified == nil {
		return nil, fmt.Errorf("%w: disclosure transform input missing", ErrQueryDisclosureBackendFailure)
	}
	if len(plan.columns) == 0 {
		return nil, fmt.Errorf("%w: disclosure plan has no columns; cannot validate result safety", ErrQueryDisclosureBackendFailure)
	}
	if len(plan.columns) != len(verified.Columns) {
		return nil, fmt.Errorf("%w: plan has %d columns but result has %d",
			ErrQueryDisclosureBackendFailure, len(plan.columns), len(verified.Columns))
	}
	for i, dec := range plan.columns {
		if err := dec.mode.Validate(); err != nil {
			return nil, fmt.Errorf("%w: plan column %d has invalid mode", ErrQueryDisclosureBackendFailure, i)
		}
		if dec.mode == model.ResultDisclosureRawCopyAllowed && !dec.copyAllowed {
			return nil, fmt.Errorf("%w: plan column %d is raw but not copyable", ErrQueryDisclosureBackendFailure, i)
		}
		if dec.mode == model.ResultDisclosureMaskedNoCopy && dec.copyAllowed {
			return nil, fmt.Errorf("%w: plan column %d is masked but copyable", ErrQueryDisclosureBackendFailure, i)
		}
	}
	out := &PGDisclosedResult{
		Columns:      make([]PGDisclosedColumn, len(verified.Columns)),
		Rows:         make([][]*string, len(verified.Rows)),
		Page:         verified.Page,
		Refs:         clonePGRefSlice(verified.Refs),
		PublicProofs: clonePGProofSlice(verified.PublicProofs),
		Sources:      clonePGSourceCatalog(verified.Sources),
		BoundRefs:    clonePGBoundRefMap(verified.BoundRefs),
	}
	for i, dec := range plan.columns {
		out.Columns[i] = PGDisclosedColumn{
			PGResultColumn: verified.Columns[i],
			DisplayMode:    dec.mode,
			CopyAllowed:    dec.copyAllowed,
		}
	}
	for r, row := range verified.Rows {
		if len(row) != len(plan.columns) {
			return nil, fmt.Errorf("%w: row %d has %d cells but expected %d",
				ErrQueryDisclosureBackendFailure, r, len(row), len(plan.columns))
		}
		outRow := make([]*string, len(row))
		for c, dec := range plan.columns {
			cell := row[c]
			if cell == nil {
				continue // SQL NULL stays NULL
			}
			if dec.mode == model.ResultDisclosureMaskedNoCopy {
				masked := maskedReplacement
				outRow[c] = &masked
				continue // the original value pointer is never retained
			}
			v := *cell // fresh *string — the immutable bytes may be shared
			outRow[c] = &v
		}
		out.Rows[r] = outRow
	}
	return out, nil
}

// clonePGRefSlice copies the ref slice plus each entry's RefPath — the frame
// values are immutable, only the slice header must be detached.
func clonePGRefSlice(in []pgsql.RangeVarRef) []pgsql.RangeVarRef {
	if in == nil {
		return nil
	}
	out := make([]pgsql.RangeVarRef, len(in))
	for i, ref := range in {
		out[i] = ref
		out[i].Path = append(pgsql.RefPath(nil), ref.Path...)
	}
	return out
}

// clonePGProofSlice copies proofs and their dependency slices.
func clonePGProofSlice(in []pgsql.PublicColumnProof) []pgsql.PublicColumnProof {
	if in == nil {
		return nil
	}
	out := make([]pgsql.PublicColumnProof, len(in))
	for i, p := range in {
		out[i] = p
		out[i].Dependencies = append([]pgsql.SourceRef(nil), p.Dependencies...)
	}
	return out
}

// clonePGSourceCatalog copies the occurrence map — the records are all-value
// structs, so a map copy detaches them.
func clonePGSourceCatalog(in map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence) map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence {
	if in == nil {
		return nil
	}
	out := make(map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// clonePGBoundRefMap copies the map, each bound-ref record, and each locked
// attribute slice — the whole structure the plan read must be unreachable
// from caller-held mutations.
func clonePGBoundRefMap(in map[pgsql.RefID]*PGBoundRef) map[pgsql.RefID]*PGBoundRef {
	if in == nil {
		return nil
	}
	out := make(map[pgsql.RefID]*PGBoundRef, len(in))
	for k, v := range in {
		if v == nil {
			out[k] = nil
			continue
		}
		c := *v
		c.Columns = append([]pgsql.ColumnMetadata(nil), v.Columns...)
		out[k] = &c
	}
	return out
}
