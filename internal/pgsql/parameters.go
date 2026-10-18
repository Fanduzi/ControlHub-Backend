// Package pgsql implements the PostgreSQL governed read-only query front half
// (spec G4/G5/G10 stage: parse → guard → classify → qualify → inject).
// input: a *GuardResult from GuardPG and an expected bound-parameter count
// output: ValidateParameterBindings — dense {1..count} ParamRef coverage on
// the immutable same-analysis tree; rewrite-internal paramRefNumbers /
// requireSameParamSet keeping the $k number set identical across qualify,
// inject, and the deparse round-trip
// pos: PG-MULTI-ENTRY stage 1 parameter contract — binding is by ParamRef
// number, never traversal order; duplicated references stay legal (set
// semantics), and an unbound statement permits no ParamRef at all. No
// parameter value ever enters this package — only the numeric shape
// note: if this file changes, update header and README.md
package pgsql

import (
	"fmt"

	pg "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	// errParamRefOutOfRange — a ParamRef number falls outside the dense
	// {1..count} bound set (or exists at all when nothing is bound).
	errParamRefOutOfRange = fmt.Errorf("%w: parameter reference outside the bound set", ErrEvidenceUnavailable)
	// errParamUnreferenced — a bound parameter number has no reference in
	// the statement: the driver would get a value it cannot place.
	errParamUnreferenced = fmt.Errorf("%w: bound parameter has no statement reference", ErrEvidenceUnavailable)
)

// ValidateParameterBindings enforces the executor's parameter contract on a
// guard-verified statement: every ParamRef number must fall inside the dense
// set {1..count} and every number in that set must be referenced at least
// once. The GuardResult is revalidated against its own immutable fingerprint
// first — a swapped or mutated analysis can never validate. Duplicated
// references to one number are legal; only the numeric identity set is
// compared. Mismatches are fixed evidence-unavailable errors carrying no
// statement text or values — the contract fails closed, never by guessing.
func ValidateParameterBindings(gr *GuardResult, count int) error {
	if count < 0 {
		return fmt.Errorf("%w: parameter count is negative", ErrEvidenceUnavailable)
	}
	if gr == nil {
		return fmt.Errorf("%w: parameter bindings need a guard result", ErrEvidenceUnavailable)
	}
	if err := validateGuardResult(gr.statement, gr); err != nil {
		return err
	}
	have := paramRefNumbers(gr.Tree)
	for n := range have {
		if n < 1 || int64(n) > int64(count) {
			return errParamRefOutOfRange
		}
	}
	for i := int64(1); i <= int64(count); i++ {
		if _, ok := have[int32(i)]; !ok {
			return errParamUnreferenced
		}
	}
	return nil
}

// paramRefNumbers collects the numeric identity set of ParamRef nodes in a
// parse tree — duplicates collapse into the set.
func paramRefNumbers(tree *pg.ParseResult) map[int32]struct{} {
	out := map[int32]struct{}{}
	if tree == nil {
		return out
	}
	walkTree(tree.ProtoReflect(), func(ctx *walkCtx, m protoreflect.Message) bool {
		if pr, ok := m.Interface().(*pg.ParamRef); ok {
			out[pr.GetNumber()] = struct{}{}
		}
		return true
	})
	return out
}

// requireSameParamSet is the rewrite pipeline's parameter checkpoint: the
// qualified reparse, the injected tree, and the deparse round-trip must all
// carry exactly the same ParamRef number set as the guard's original tree.
// Rewrites may legitimately duplicate a reference node (Q+D, ordinal
// propagation) — only the set of numbers must be identical.
func requireSameParamSet(want map[int32]struct{}, tree *pg.ParseResult) error {
	got := paramRefNumbers(tree)
	if len(got) != len(want) {
		return fmt.Errorf("%w: rewrite changed the statement's parameter set", ErrEvidenceUnavailable)
	}
	for n := range want {
		if _, ok := got[n]; !ok {
			return fmt.Errorf("%w: rewrite changed the statement's parameter set", ErrEvidenceUnavailable)
		}
	}
	return nil
}
