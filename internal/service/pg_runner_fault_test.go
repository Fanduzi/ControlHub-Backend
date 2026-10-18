// Package service — shared-runner fault-matrix and ownership proofs across
// every claimed entry kind.
// input: fakeExecRepo + injected pgOutcomeFn/pgNavPrepare seams; sequential
// mutations only (no unsynchronized-concurrency promise is made or needed)
// output: TestPGClaimedRunner_* — identical internal-fault handling for
// ordinary/saved/navigation: contract faults finalize failed with the fixed
// code, status surfaces agree, delivered payloads are detached snapshots
// (nested provenance carriers included, both directions, and stable across
// the keyed finalize boundary); the closed entry kind rejects unknown
// values before any claim; caller slices are owned before preparation
// pos: PG-MULTI-ENTRY stage 4 packet item 7 — the shared runner owns the
// consistency gate for all kinds; entry-kind event selection and detachment
// are proven uniformly
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// entryDriver invokes one claimed entry with a fresh request so each case
// exercises the real front half into the shared runner.
func pgEntryDrivers() []struct {
	name string
	wire func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService
	run  func(svc *QueryExecutionService) (PGClaimedOutcome, error)
} {
	return []struct {
		name string
		wire func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService
		run  func(svc *QueryExecutionService) (PGClaimedOutcome, error)
	}{
		{
			name: "ordinary",
			wire: func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService {
				return newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
			},
			run: func(svc *QueryExecutionService) (PGClaimedOutcome, error) {
				return svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, validClaimedRequest())
			},
		},
		{
			name: "saved",
			wire: func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService {
				return newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)},
					&fakeSavedStatementRead{resp: savedPGFixture()})
			},
			run: func(svc *QueryExecutionService) (PGClaimedOutcome, error) {
				return svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, validSavedRequest())
			},
		},
		{
			name: "navigation",
			wire: func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService {
				return newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)},
					pgNavPrepBinding(pgNavBindingFixture()))
			},
			run: func(svc *QueryExecutionService) (PGClaimedOutcome, error) {
				out, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
				return out.PGClaimedOutcome, err
			},
		},
	}
}

// Every entry kind converges on the same consistency gate: each producer
// fault shape finalizes the claim as failed under the fixed internal code,
// the outcome/evidence/persisted statuses agree, and no rows escape.
func TestPGClaimedRunner_FaultMatrixAllKinds(t *testing.T) {
	t.Parallel()
	goodEv := func() *PGAttemptEvidence {
		return &PGAttemptEvidence{Stage: PGStageExecute, Status: model.QueryExecutionSuccess, BackendPID: 42}
	}
	safe := "safe"
	goodRes := func() *PGDisclosedResult {
		return &PGDisclosedResult{
			Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "v", DataTypeOID: 25}}},
			Rows:          [][]*string{{&safe}},
			CellTruncated: [][]bool{{false}},
		}
	}
	faults := map[string]struct {
		res *PGDisclosedResult
		ev  *PGAttemptEvidence
		err error
	}{
		"missing result":     {res: nil, ev: goodEv(), err: nil},
		"missing evidence":   {res: goodRes(), ev: nil, err: nil},
		"success plus error": {res: goodRes(), ev: goodEv(), err: ErrQueryTimeout},
		"bad matrix shape": {
			res: &PGDisclosedResult{
				Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "v", DataTypeOID: 25}}},
				Rows:          [][]*string{{&safe}},
				CellTruncated: [][]bool{{false, true}}, // wider than the row — misaligned
			},
			ev: goodEv(),
		},
	}
	for _, drv := range pgEntryDrivers() {
		drv := drv
		for fname, fault := range faults {
			fault := fault
			t.Run(drv.name+"/"+fname, func(t *testing.T) {
				t.Parallel()
				repo := claimedPGRepo(t)
				svc := drv.wire(repo, t)
				svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
					return fault.res, fault.ev, fault.err
				}
				out, err := drv.run(svc)
				if !errors.Is(err, ErrQueryBackendFailure) {
					t.Fatalf("err = %v, want backend failure on contract fault", err)
				}
				if out.Result != nil {
					t.Fatalf("contract fault delivered rows: %+v", out.Result)
				}
				if len(repo.finalizeCalls) != 1 {
					t.Fatalf("finalizeCalls = %d, want 1", len(repo.finalizeCalls))
				}
				rec := repo.finalizeCalls[0]
				if rec.Status != model.QueryExecutionFailed || rec.ErrorCode != "query_internal_error" {
					t.Fatalf("persisted = %q/%q, want failed/query_internal_error", rec.Status, rec.ErrorCode)
				}
				if out.Status != rec.Status || out.Evidence == nil || out.Evidence.Status != out.Status {
					t.Fatalf("outcome %q / evidence %+v / persisted %q disagree", out.Status, out.Evidence, rec.Status)
				}
				if rec.RowCount != 0 {
					t.Fatalf("persisted row_count = %d, want 0", rec.RowCount)
				}
			})
		}
	}
}

// The entry kind is a closed enum owned by the server: an out-of-range kind
// fails before claim admission — no key occupancy, no finalize.
func TestPGClaimedRunner_UnknownKindRejectedPreClaim(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	out, err := svc.executePGClaimedPrepared(context.Background(), pgClaimedPrepared{
		kind:      pgClaimedEntryKind(99),
		clientKey: "k-unknown-kind",
		digest:    "d",
	})
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("unknown kind err = %v, want backend failure", err)
	}
	if out.ExecutionID != 0 || len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatalf("unknown kind reached claim/finalize: %+v claims=%d", out, len(repo.claimInputs))
	}
}

// Producer-side mutation after the runner's snapshot — rows, matrix,
// columns, and every provenance carrier including nested records — never
// reaches the delivered result; mutating the delivered result never
// reaches the producer either. Proven identically on all three entries.
func TestPGClaimedRunner_DeliverySnapshotDetachAllKinds(t *testing.T) {
	t.Parallel()
	// A real GuardPG ref carries a real Path — synthetic refs have none.
	guard, err := pgsql.GuardPG("SELECT id FROM orders")
	if err != nil || len(guard.Refs) == 0 || len(guard.Refs[0].Path) == 0 {
		t.Fatalf("guard refs = %+v err=%v, want a real ref with a nonempty Path", guard, err)
	}
	realRef := guard.Refs[0]
	for _, drv := range pgEntryDrivers() {
		drv := drv
		t.Run(drv.name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := drv.wire(repo, t)
			safe := "safe"
			safe2 := "safe2"
			// Each parallel subtest owns its ref path copy — clear() inside
			// one subtest must never alias a sibling's fixture.
			ref := realRef
			ref.Path = slices.Clone(realRef.Path)
			res := &PGDisclosedResult{
				Columns: []PGDisclosedColumn{
					{PGResultColumn: PGResultColumn{Name: "v", DataTypeOID: 25}, CopyAllowed: true},
					{PGResultColumn: PGResultColumn{Name: "w", DataTypeOID: 25}, CopyAllowed: true},
				},
				Rows:          [][]*string{{&safe, &safe2}, {&safe2, &safe}},
				CellTruncated: [][]bool{{false, false}, {false, false}},
				Refs:          []pgsql.RangeVarRef{ref},
				PublicProofs: []pgsql.PublicColumnProof{{
					TransportIndex: 0,
					Resolution:     pgsql.ProofComplete,
					Dependencies: []pgsql.SourceRef{
						{Occurrence: 1, Slot: pgsql.SourceSlot{Kind: pgsql.SourceSlotColumn, Ordinal: 0}},
					},
					FDOrigin: pgsql.FDOrigin{Kind: pgsql.FDOriginKnownColumn, RelationOID: 123, AttributeNumber: 1},
				}},
				Sources: map[pgsql.SourceOccurrenceID]pgsql.SourceOccurrence{1: {ID: 1}},
				BoundRefs: map[pgsql.RefID]*PGBoundRef{ref.RefID: {
					RefID: ref.RefID, Name: "orders", CatalogName: "orders",
					Columns: []pgsql.ColumnMetadata{
						{Name: "id", Type: pgsql.ColumnType{OID: 20}, RelationOID: 123, AttributeNumber: 1},
					},
				}},
			}
			// Clone before clear() — wantPath must not alias the backing
			// array the producer is about to zero.
			wantPath := slices.Clone(res.Refs[0].Path)
			svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				return res, &PGAttemptEvidence{Stage: PGStageDeliver, Status: model.QueryExecutionSuccess,
					BackendPID: 7, RemoteState: model.QueryExecutionRemoteCompleted}, nil
			}
			out, err := drv.run(svc)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			got := out.Result
			if got == nil || len(got.Rows) != 2 || got.Rows[0][0] == nil || *got.Rows[0][0] != "safe" {
				t.Fatalf("rows = %+v, want the snapshot value", got)
			}
			// Producer mutation of every carrier — scalars, pointees,
			// nested cells, and backing slices — cannot reach the delivered
			// snapshot.
			*res.Rows[0][0] = "tampered"
			res.CellTruncated[0][0] = true
			res.Columns[0].Name = "renamed"
			res.Refs[0].RefID = 77
			clear(res.Refs[0].Path)
			res.PublicProofs[0].Dependencies[0].Occurrence = 99
			res.PublicProofs[0].FDOrigin.RelationOID = 999
			res.Sources[1] = pgsql.SourceOccurrence{ID: 44}
			res.BoundRefs[ref.RefID].RefID = 77
			res.BoundRefs[ref.RefID].Columns[0].Name = "renamed"
			if *got.Rows[0][0] != "safe" || got.CellTruncated[0][0] || got.Columns[0].Name != "v" ||
				got.Refs[0].RefID != ref.RefID || !reflect.DeepEqual(got.Refs[0].Path, wantPath) ||
				got.PublicProofs[0].Dependencies[0].Occurrence != 1 ||
				got.PublicProofs[0].FDOrigin.RelationOID != 123 ||
				got.Sources[1].ID != 1 ||
				got.BoundRefs[ref.RefID].RefID != ref.RefID ||
				got.BoundRefs[ref.RefID].Columns[0].Name != "id" {
				t.Fatalf("producer mutation reached the delivered result: %+v", got)
			}
			// The reverse direction: delivered-object writes never reach the
			// producer's records — every mutated carrier is checked, and the
			// matrix/column/row writes land on cells the producer did NOT
			// already mutate (a same-value write proves nothing).
			*got.Rows[1][0] = "tampered-back"
			got.CellTruncated[1][1] = true
			got.Columns[1].Name = "graffiti"
			got.Refs[0].Name = "graffiti"
			got.PublicProofs[0].FDOrigin.AttributeNumber = 7
			got.Sources[1] = pgsql.SourceOccurrence{ID: 55}
			got.BoundRefs[ref.RefID].Name = "graffiti"
			if *res.Rows[1][0] != "safe2" || res.CellTruncated[1][1] ||
				res.Columns[1].Name != "w" || res.Refs[0].Name != ref.Name ||
				res.PublicProofs[0].FDOrigin.AttributeNumber != 1 ||
				res.Sources[1].ID != 44 || res.BoundRefs[ref.RefID].Name != "orders" {
				t.Fatal("delivered-object mutation reached the producer")
			}
			if repo.finalizeCalls[0].Status != model.QueryExecutionSuccess {
				t.Fatalf("persisted = %q, want success", repo.finalizeCalls[0].Status)
			}
		})
	}
}

// The delivery decision is proven over the coordinator-owned pre-finalize
// snapshot: producer mutation at the keyed finalize boundary (sequential —
// no unsynchronized-concurrency promise) can neither widen a clean page
// nor rescue a refused one. Proven on all three production front halves.
func TestPGClaimedRunner_FinalizeBoundaryMutationAllKinds(t *testing.T) {
	t.Parallel()
	drivers := []struct {
		name string
		wire func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService
		run  func(svc *QueryExecutionService, caps []string) (PGClaimedOutcome, error)
	}{
		{
			name: "ordinary",
			wire: func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService {
				return newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
			},
			run: func(svc *QueryExecutionService, caps []string) (PGClaimedOutcome, error) {
				req := validClaimedRequest()
				req.Capabilities = caps
				return svc.ExecutePGClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
			},
		},
		{
			name: "saved",
			wire: func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService {
				return newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)},
					&fakeSavedStatementRead{resp: savedPGFixture()})
			},
			run: func(svc *QueryExecutionService, caps []string) (PGClaimedOutcome, error) {
				req := validSavedRequest()
				req.Capabilities = caps
				return svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, req)
			},
		},
		{
			name: "navigation",
			wire: func(repo *fakeExecRepo, t *testing.T) *QueryExecutionService {
				return newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)},
					pgNavPrepBinding(pgNavBindingFixture()))
			},
			run: func(svc *QueryExecutionService, caps []string) (PGClaimedOutcome, error) {
				req := validPGNavRequest()
				req.Capabilities = caps
				out, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
				return out.PGClaimedOutcome, err
			},
		},
	}
	goodEvidence := func() *PGAttemptEvidence {
		return &PGAttemptEvidence{Stage: PGStageDeliver, Status: model.QueryExecutionSuccess,
			BackendPID: 7, RemoteState: model.QueryExecutionRemoteCompleted}
	}
	for _, drv := range drivers {
		drv := drv
		t.Run(drv.name+"/safe snapshot", func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := drv.wire(repo, t)
			safe := "safe"
			res := &PGDisclosedResult{
				Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "v", DataTypeOID: 25}}},
				Rows:          [][]*string{{&safe}},
				CellTruncated: [][]bool{{false}},
			}
			svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				return res, goodEvidence(), nil
			}
			repo.finalizeHook = func() {
				// Rewrite the producer's page at the keyed finalize: the
				// delivery decision already owns its snapshot.
				long := strings.Repeat("x", 9000)
				*res.Rows[0][0] = long
				res.Rows[0] = append(res.Rows[0], &long)
				res.CellTruncated[0] = append(res.CellTruncated[0], true)
				res.Rows = append(res.Rows, []*string{&long})
				res.CellTruncated = append(res.CellTruncated, []bool{true})
			}
			out, err := drv.run(svc, nil)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			got := out.Result
			if got == nil || len(got.Rows) != 1 || len(got.Rows[0]) != 1 ||
				got.Rows[0][0] == nil || *got.Rows[0][0] != "safe" || got.CellTruncated[0][0] {
				t.Fatalf("delivered %+v, want the validated 1-cell pre-finalize snapshot", got)
			}
			if repo.finalizeCalls[0].Status != model.QueryExecutionSuccess {
				t.Fatalf("persisted = %q, want success", repo.finalizeCalls[0].Status)
			}
		})
		t.Run(drv.name+"/flagged snapshot", func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := drv.wire(repo, t)
			val := "over"
			res := &PGDisclosedResult{
				Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "v", DataTypeOID: 25}}},
				Rows:          [][]*string{{&val}},
				CellTruncated: [][]bool{{true}},
			}
			svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				return res, goodEvidence(), nil
			}
			repo.finalizeHook = func() {
				// Clearing the producer's flag after the snapshot must not
				// rescue the no-cap delivery decision.
				res.CellTruncated[0][0] = false
			}
			out, err := drv.run(svc, nil)
			if !errors.Is(err, ErrResultContractUpgradeRequired) {
				t.Fatalf("no-cap flagged page err = %v, want upgrade required", err)
			}
			if out.Result != nil || out.Status != model.QueryExecutionSuccess {
				t.Fatalf("refused outcome = %+v, want persisted success with no result", out)
			}
			if len(repo.finalizeCalls) != 1 || repo.finalizeCalls[0].Status != model.QueryExecutionSuccess {
				t.Fatalf("persisted = %+v, want one success finalize", repo.finalizeCalls)
			}
		})
	}
}

// The navigation entry owns its request copies before preparation: a caller
// mutating its LocalValues/Capabilities backing arrays inside the
// preparation seam can never shift the digest, the bound plan values, or the
// delivery gate.
func TestPGNavigationClaimed_CallerSliceOwnership(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	req := validPGNavRequest()
	req.Capabilities = []string{CapabilityCellTruncated}
	var capturedValues []string
	svc := newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)})
	svc.pgNavPrepare = func(_ context.Context, _ *pgx.ConnConfig, _ string, _ model.RelatedRecordNavigationSource, _ time.Duration) (*pgNavigationBinding, error) {
		// The caller mutates its own slices during preparation — the entry
		// must already own copies, so neither write is observable.
		req.LocalValues[0] = "mutated"
		req.Capabilities[0] = "not-a-capability"
		return pgNavBindingFixture(), nil
	}
	svc.pgOutcomeFn = func(_ context.Context, in PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		capturedValues = append([]string(nil), in.Execute.parameters.values...)
		trunc := "9000-byte cell"
		return &PGDisclosedResult{
			Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "id", DataTypeOID: 20}}},
			Rows:          [][]*string{{&trunc}},
			CellTruncated: [][]bool{{true}},
		}, &PGAttemptEvidence{Stage: PGStageDeliver, Status: model.QueryExecutionSuccess, BackendPID: 5}, nil
	}
	if _, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, req); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if len(capturedValues) != 1 || capturedValues[0] != "7" {
		t.Fatalf("bound values = %+v, want the pre-mutation locator '7'", capturedValues)
	}
	// The truncated cell was delivered only because the gate still saw the
	// ORIGINAL capability — a mutated capability list would have refused.
	if len(repo.claimInputs) != 1 {
		t.Fatal("claim was not recorded")
	}
	// Digest proven over the original locator: same-key retry with the
	// original request duplicates; the mutated locator conflicts.
	dup := validPGNavRequest()
	dup.Capabilities = []string{CapabilityCellTruncated}
	if _, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, dup); !errors.Is(err, ErrQueryExecutionAlreadyExists) {
		t.Fatalf("same-key original retry err = %v, want duplicate", err)
	}
	other := validPGNavRequest()
	other.LocalValues = []string{"mutated"}
	if _, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, other); !errors.Is(err, ErrQueryExecutionConflict) {
		t.Fatalf("same-key mutated retry err = %v, want conflict", err)
	}
	if len(repo.claimInputs) != 1 {
		t.Fatalf("claimInputs = %d, retries must never re-admit", len(repo.claimInputs))
	}
}
