// Package service tests the keyed related-record navigation PG entry.
// input: testing, context, errors, internal/model fixtures, fake claim repo +
// injected pgNavPrepare/pgOutcomeFn seams
// output: TestPGNavigationClaimed_* / TestPGNavigation* — shape ordering,
// access/prep refusals with the NULL-key related_record_navigation pair,
// plan/binding consistency, keyed finalize evidence, and relation metadata
// scoped to terminal success
// pos: PG-MULTI-ENTRY stage 3 unit coverage — the navigation entry converges
// on the shared claimed runner; every pre-claim refusal writes the fixed
// NULL-key pair under the navigation audit family, and generated SQL/bound
// locators never reach errors or history
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// pgNavBindingFixture is a valid one-column FK mapping: app.order_items.order_id
// → app.orders.id — the prepared binding a real metadata probe would return.
func pgNavBindingFixture() *pgNavigationBinding {
	return &pgNavigationBinding{
		databaseOID:   100,
		constraintOID: 90001,
		fkName:        "fk_orders",
		source:        pgNavigationRelation{oid: 5001, reltype: 5002, kind: "r", schema: "app", name: "order_items"},
		target:        pgNavigationRelation{oid: 6001, reltype: 6002, kind: "r", schema: "app", name: "orders"},
		pairs: []pgNavigationColumnPair{
			{localAttnum: 2, localName: "order_id", localTypeOID: 20, localTypmod: -1,
				refAttnum: 1, refName: "id", refTypeOID: 20, refTypmod: -1},
		},
	}
}

func validPGNavRequest() PGClaimedNavigationRequest {
	return PGClaimedNavigationRequest{
		ClientExecutionID: "req-nav-1",
		Source: model.RelatedRecordNavigationSource{
			Kind:       string(model.ObjectKindTable),
			Database:   "appdb",
			Schema:     "app",
			Object:     "order_items",
			ForeignKey: "fk_orders",
		},
		LocalValues: []string{"7"},
	}
}

// newPGNavClaimedService wires the keyed service with an injected preparation
// seam — the production constructor installs preparePGNavigation; tests
// substitute bindings/errors without a live PostgreSQL.
func newPGNavClaimedService(repo *fakeExecRepo, resolver *fakeResolver, prep pgNavPrepareFunc) *QueryExecutionService {
	svc := newClaimedService(repo, resolver)
	svc.pgNavPrepare = prep
	return svc
}

func pgNavPrepBinding(b *pgNavigationBinding) pgNavPrepareFunc {
	return func(_ context.Context, _ *pgx.ConnConfig, _ string, _ model.RelatedRecordNavigationSource, _ time.Duration) (*pgNavigationBinding, error) {
		return b, nil
	}
}

// Shape failures — bad identity, machine actor, bad key, malformed source or
// locator list — consume no target read, no preparation, no claim, no pair.
func TestPGNavigationClaimed_ShapeFailures(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*PGClaimedNavigationRequest, *model.QueryExecutionIdentity){
		"machine identity": func(_ *PGClaimedNavigationRequest, id *model.QueryExecutionIdentity) {
			*id = model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 7}
		},
		"invalid identity": func(_ *PGClaimedNavigationRequest, id *model.QueryExecutionIdentity) {
			*id = model.QueryExecutionIdentity{}
		},
		"missing key": func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.ClientExecutionID = "" },
		"oversized key": func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) {
			r.ClientExecutionID = strings.Repeat("x", 65)
		},
		"key with NUL":   func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.ClientExecutionID = "a\x00b" },
		"empty database": func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.Source.Database = "" },
		"empty schema":   func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.Source.Schema = "" },
		"empty object":   func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.Source.Object = "" },
		"empty fk":       func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.Source.ForeignKey = "" },
		"NUL source":     func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.Source.Object = "a\x00b" },
		"utf8 source":    func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.Source.Object = "\xff" },
		"bad kind":       func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.Source.Kind = "view" },
		"empty values":   func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.LocalValues = nil },
		"too many values": func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) {
			r.LocalValues = make([]string, model.MaxLocalValuesCount+1)
			for i := range r.LocalValues {
				r.LocalValues[i] = "1"
			}
		},
		"oversized locator": func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) {
			r.LocalValues = []string{strings.Repeat("x", model.MaxLocalValueLength+1)}
		},
		"NUL locator": func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) {
			r.LocalValues = []string{"a\x00b"}
		},
		"invalid utf8 locator": func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) {
			r.LocalValues = []string{"\xff"}
		},
		"negative maxRows": func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.MaxRows = -1 },
		"bad pagination":   func(r *PGClaimedNavigationRequest, _ *model.QueryExecutionIdentity) { r.Page = 1; r.PageSize = 11 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			target, _, _ := pgClaimFixture()
			var targetQueries []model.QueryTargetListQuery
			prepCalled := false
			svc := newClaimedServiceOn(repo, &fakeResolver{dsn: mustDSN(t)},
				fakeTargetRepo{targets: []model.QueryTarget{target}, queries: &targetQueries})
			svc.pgNavPrepare = func(_ context.Context, _ *pgx.ConnConfig, _ string, _ model.RelatedRecordNavigationSource, _ time.Duration) (*pgNavigationBinding, error) {
				prepCalled = true
				return pgNavBindingFixture(), nil
			}
			req := validPGNavRequest()
			id := userExecutionIdentity(7)
			mutate(&req, &id)
			_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), id, 9002, req)
			if !errors.Is(err, ErrQueryValidationFailed) {
				t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
			}
			if len(targetQueries) != 0 || prepCalled {
				t.Fatal("shape failure reached target or metadata preparation")
			}
			if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
				t.Fatal("shape failure claimed or wrote evidence")
			}
		})
	}
}

// Unknown target: nothing attributable — no preparation, no claim, no pair.
func TestPGNavigationClaimed_TargetNotFound(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(pgNavBindingFixture()))
	_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 4242, validPGNavRequest())
	if !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("error = %v, want ErrQueryTargetNotFound", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("unknown target claimed or wrote evidence")
	}
}

// Access refusal writes exactly one NULL-key rejected pair under the
// related_record_navigation family — the key stays unoccupied.
func TestPGNavigationClaimed_AccessDenied(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	_, cred, _ := pgClaimFixture()
	cred.Enabled = false
	repo.credentials[9002] = cred
	svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(pgNavBindingFixture()))
	_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
	if err == nil {
		t.Fatal("disabled connection must refuse")
	}
	if len(repo.claimInputs) != 0 {
		t.Fatal("access denial occupied the key")
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairCalls = %d, want 1 NULL-key rejected pair", len(repo.pairCalls))
	}
	pair := repo.pairCalls[0]
	if pair.event != "related_record_navigation" || pair.rec.Status != model.QueryExecutionRejected ||
		pair.rec.ClientExecutionID != nil || pair.rec.FullStatement != "" {
		t.Fatalf("null-key pair = %+v, want rejected navigation pair with no key/statement", pair)
	}
}

// Preparation classifications: invisible source/FK is the controlled
// not-found refusal; cancel and deadline keep their real category; any other
// backend fault is the fixed internal failure — never raw driver text.
func TestPGNavigationClaimed_PrepFailures(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		prepErr    error
		wantErr    error
		wantStatus model.QueryExecutionStatus
		wantCode   string
	}{
		"no source": {errPGNavNoSource, ErrNavigationSourceNotFound, model.QueryExecutionRejected, "navigation_source_not_found"},
		"cancel":    {context.Canceled, context.Canceled, model.QueryExecutionCancelled, "query_cancelled"},
		"timeout":   {ErrQueryTimeout, ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout"},
		"backend":   {errPGNavMetaInternal, ErrQueryBackendFailure, model.QueryExecutionFailed, "query_backend_error"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)},
				func(_ context.Context, _ *pgx.ConnConfig, _ string, _ model.RelatedRecordNavigationSource, _ time.Duration) (*pgNavigationBinding, error) {
					return nil, c.prepErr
				})
			_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
			if !errors.Is(err, c.wantErr) {
				t.Fatalf("error = %v, want %v", err, c.wantErr)
			}
			if len(repo.claimInputs) != 0 {
				t.Fatal("pre-claim refusal occupied the key")
			}
			if len(repo.pairCalls) != 1 {
				t.Fatalf("pairCalls = %d, want 1 NULL-key pair", len(repo.pairCalls))
			}
			pair := repo.pairCalls[0]
			if pair.event != "related_record_navigation" || pair.rec.Status != c.wantStatus ||
				pair.rec.ErrorCode != c.wantCode || pair.rec.ClientExecutionID != nil {
				t.Fatalf("null-key pair = %+v, want %s/%s navigation pair", pair, c.wantStatus, c.wantCode)
			}
		})
	}
}

// A raw driver fault out of preparation is redacted to the internal sentinel.
func TestPGNavigationClaimed_PrepRawErrorRedacted(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)},
		func(_ context.Context, _ *pgx.ConnConfig, _ string, _ model.RelatedRecordNavigationSource, _ time.Duration) (*pgNavigationBinding, error) {
			return nil, errors.New("driver: secret-dsn-do-not-leak")
		})
	_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
	if !errors.Is(err, ErrQueryBackendFailure) || strings.Contains(err.Error(), "secret-dsn-do-not-leak") {
		t.Fatalf("error = %v, want redacted backend failure", err)
	}
}

// A preparation that returns no binding and no error — or a malformed one —
// is a fixed metadata machinery failure with one NULL-key failed pair: no
// panic, no key occupancy, no internal shape reaching the caller.
func TestPGNavigationClaimed_NilOrMalformedPreparation(t *testing.T) {
	t.Parallel()
	for name, binding := range map[string]*pgNavigationBinding{
		"nil success": nil,
		"view kind": func() *pgNavigationBinding {
			b := pgNavBindingFixture()
			b.source.kind = "v"
			return b
		}(),
		"zero type oid": func() *pgNavigationBinding {
			b := pgNavBindingFixture()
			b.pairs[0].localTypeOID = 0
			return b
		}(),
		"zero attnum": func() *pgNavigationBinding {
			b := pgNavBindingFixture()
			b.pairs[0].refAttnum = 0
			return b
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(binding))
			_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
			if !errors.Is(err, ErrQueryBackendFailure) {
				t.Fatalf("error = %v, want redacted backend failure", err)
			}
			if len(repo.claimInputs) != 0 {
				t.Fatal("malformed preparation occupied the key")
			}
			if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionFailed ||
				repo.pairCalls[0].rec.ClientExecutionID != nil || repo.pairCalls[0].event != "related_record_navigation" {
				t.Fatalf("pairCalls = %+v, want one NULL-key failed navigation pair", repo.pairCalls)
			}
		})
	}
}

// The coordinator owns the prepared binding from the moment preparation
// returns: a provider that mutates its own copy inside the finalize window
// cannot change the delivered relation metadata, and mutating the returned
// Relation never reaches the provider either.
func TestPGNavigationClaimed_PreparationSnapshotIsolation(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	provider := pgNavBindingFixture()
	svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(provider))
	val := "42"
	svc.pgOutcomeFn = func(_ context.Context, _ PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		return &PGDisclosedResult{
				Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "id", DataTypeOID: 20}}},
				Rows:          [][]*string{{&val}},
				CellTruncated: [][]bool{{false}},
			}, &PGAttemptEvidence{Stage: PGStageDeliver, Status: model.QueryExecutionSuccess,
				BackendPID: 7, RemoteState: model.QueryExecutionRemoteCompleted}, nil
	}
	repo.finalizeHook = func() {
		provider.source.name = "renamed_after_claim"
		provider.source.schema = "other_schema"
		provider.target.name = "retargeted"
		provider.pairs[0].localName = "swapped"
		provider.pairs[0].refName = "swapped"
	}
	out, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
	if err != nil {
		t.Fatalf("navigate: %v", err)
	}
	rel := out.Relation
	if rel == nil || rel.SourceSchema != "app" || rel.SourceObject != "order_items" ||
		rel.SourceColumns[0] != "order_id" || rel.ReferencedObject != "orders" ||
		rel.ReferencedColumns[0] != "id" {
		t.Fatalf("relation = %+v, want the pre-claim snapshot — provider mutation leaked", rel)
	}
	rel.SourceColumns[0] = "caller_graffiti"
	rel.ReferencedColumns[0] = "caller_graffiti"
	rel.ReferencedObject = "caller_graffiti"
	// The provider kept exactly its own post-finalize mutations in both
	// directions — neither the delivered snapshot nor caller graffiti
	// reaches back into it.
	want := pgNavBindingFixture()
	want.source.name, want.source.schema = "renamed_after_claim", "other_schema"
	want.target.name = "retargeted"
	want.pairs[0].localName, want.pairs[0].refName = "swapped", "swapped"
	if !provider.sameAs(want) {
		t.Fatalf("provider binding = %+v, want exactly its own mutations — delivered-relation graffiti leaked back", provider)
	}
}

// Locator cardinality must match the prepared FK column count — checked
// after preparation, before claim.
func TestPGNavigationClaimed_ValueMismatch(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(pgNavBindingFixture()))
	req := validPGNavRequest()
	req.LocalValues = []string{"7", "8"}
	_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
	if !errors.Is(err, ErrNavigationValueMismatch) {
		t.Fatalf("error = %v, want ErrNavigationValueMismatch", err)
	}
	if len(repo.claimInputs) != 0 {
		t.Fatal("value mismatch occupied the key")
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.ErrorCode != "navigation_value_mismatch" ||
		repo.pairCalls[0].rec.Status != model.QueryExecutionRejected {
		t.Fatalf("pairCalls = %+v, want one NULL-key navigation_value_mismatch rejection", repo.pairCalls)
	}
}

// Claim admitted: the captured outcome input carries the generated locator
// statement, the navigation-sourced plan, the binding snapshot, and the
// pinned-schema flag — while the keyed finalize persists the withheld
// preview, empty full statement, and the related-records digest under the
// related_record_navigation audit family.
func TestPGNavigationClaimed_KeyedAttemptEvidence(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(pgNavBindingFixture()))

	var captured PGDisclosureInput
	svc.pgOutcomeFn = func(_ context.Context, in PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		captured = in
		return nil, &PGAttemptEvidence{Stage: PGStageBind, Status: model.QueryExecutionRejected},
			&pgsql.RejectError{Code: "query_not_allowed", Message: "pgsql: forced rejection"}
	}

	_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
	if err == nil {
		t.Fatal("injected rejection must surface")
	}
	if len(repo.claimInputs) != 1 {
		t.Fatalf("claims = %d, want 1", len(repo.claimInputs))
	}
	wantDigest := DigestRelatedRecords(RelatedRecordsDigestInput{
		Database: "appdb", Schema: "app",
		SourceSchema: "app", SourceObject: "order_items", SourceColumns: []string{"order_id"},
		ForeignKey: "fk_orders", LocalValues: []string{"7"},
		Page: 1, PageSize: pgClaimDefaultPageSize, MaxRows: pgClaimDefaultMaxRows,
	})
	if repo.claimInputs[0].RequestDigest != wantDigest {
		t.Fatalf("claim digest = %q, want related-records digest %q", repo.claimInputs[0].RequestDigest, wantDigest)
	}
	if repo.claimInputs[0].SchemaName != "app" {
		t.Fatalf("claim schema = %q, want the explicit source schema pin", repo.claimInputs[0].SchemaName)
	}
	if captured.Execute.Statement != `SELECT * FROM "app"."orders" WHERE "id" = $1` {
		t.Fatalf("executed statement = %q, want generated locator SQL", captured.Execute.Statement)
	}
	if captured.Execute.parameters == nil || captured.Execute.parameters.source != pgParameterSourceNavigation ||
		len(captured.Execute.parameters.values) != 1 || captured.Execute.parameters.values[0] != "7" {
		t.Fatalf("parameter plan = %+v, want one navigation locator value", captured.Execute.parameters)
	}
	if captured.Execute.navigation == nil || captured.Execute.navigation.constraintOID != 90001 {
		t.Fatalf("navigation binding = %+v, want the prepared binding attached", captured.Execute.navigation)
	}
	if !captured.Execute.requirePinnedSchema {
		t.Fatal("navigation entry must strengthen the pinned-schema gate")
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalize = %d, want 1", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	if rec.StatementPreview != pgClaimedWithheldPreview || rec.FullStatement != "" {
		t.Fatalf("navigation keyed record preview=%q full=%q, want withheld + empty",
			rec.StatementPreview, rec.FullStatement)
	}
	if rec.StatementDigest != wantDigest || rec.Status != model.QueryExecutionRejected {
		t.Fatalf("keyed record = %+v, want related-records digest + rejected", rec)
	}
	if !strings.HasPrefix(repo.finalizeEvents[0], "related_record_navigation:") {
		t.Fatalf("navigation finalize event = %q, want the related_record_navigation family", repo.finalizeEvents[0])
	}
}

// Relation metadata is populated only on actual terminal success — including
// the persisted-success capability refusal — and carries canonical catalog
// names, never locators or OIDs.
func TestPGNavigationClaimed_RelationOnSuccessOnly(t *testing.T) {
	t.Parallel()
	for name, caps := range map[string][]string{
		"delivered":         nil,
		"capability refuse": {"other-cap"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(pgNavBindingFixture()))
			val := "42"
			svc.pgOutcomeFn = func(_ context.Context, _ PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				return &PGDisclosedResult{
						Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "id", DataTypeOID: 20}}},
						Rows:          [][]*string{{&val}},
						CellTruncated: [][]bool{{name == "capability refuse"}},
					}, &PGAttemptEvidence{Stage: PGStageDeliver, Status: model.QueryExecutionSuccess,
						BackendPID: 7, RemoteState: model.QueryExecutionRemoteCompleted}, nil
			}
			req := validPGNavRequest()
			req.Capabilities = caps
			out, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, req)
			if name == "capability refuse" {
				if !errors.Is(err, ErrResultContractUpgradeRequired) {
					t.Fatalf("error = %v, want ErrResultContractUpgradeRequired", err)
				}
			} else if err != nil {
				t.Fatalf("delivered error = %v, want nil", err)
			}
			if out.Status != model.QueryExecutionSuccess || out.Relation == nil {
				t.Fatalf("outcome = %+v, want terminal success with relation", out)
			}
			rel := out.Relation
			if rel.Database != "appdb" || rel.SourceSchema != "app" || rel.SourceObject != "order_items" ||
				rel.ForeignKey != "fk_orders" || rel.ReferencedSchema != "app" || rel.ReferencedObject != "orders" ||
				len(rel.SourceColumns) != 1 || rel.SourceColumns[0] != "order_id" ||
				len(rel.ReferencedColumns) != 1 || rel.ReferencedColumns[0] != "id" {
				t.Fatalf("relation = %+v, want canonical mapping names", rel)
			}
		})
	}

	t.Run("failure carries nil relation", func(t *testing.T) {
		t.Parallel()
		repo := claimedPGRepo(t)
		svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(pgNavBindingFixture()))
		svc.pgOutcomeFn = func(_ context.Context, _ PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
			return nil, &PGAttemptEvidence{Stage: PGStageBind, Status: model.QueryExecutionRejected},
				&pgsql.RejectError{Code: "query_not_allowed", Message: "pgsql: forced rejection"}
		}
		out, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
		if err == nil {
			t.Fatal("injected rejection must surface")
		}
		if out.Relation != nil {
			t.Fatalf("relation = %+v on a rejected attempt, want nil", out.Relation)
		}
	})
}

// Claim admission classifications execute no SQL and write no evidence.
func TestPGNavigationClaimed_ClaimClassifications(t *testing.T) {
	t.Parallel()
	for name, claimErr := range map[string]error{
		"exists":   model.ErrQueryExecutionClaimExists,
		"conflict": model.ErrQueryExecutionClaimConflict,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			repo.claimErr = claimErr
			svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, pgNavPrepBinding(pgNavBindingFixture()))
			_, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
			want := ErrQueryExecutionAlreadyExists
			if name == "conflict" {
				want = ErrQueryExecutionConflict
			}
			if !errors.Is(err, want) {
				t.Fatalf("error = %v, want %v", err, want)
			}
			if len(repo.pairCalls) != 0 || len(repo.finalizeCalls) != 0 {
				t.Fatal("duplicate/conflict wrote evidence or finalized")
			}
		})
	}
}

// --- trusted substrate: plan builder, binding snapshot, mapping equality ---

func TestPGNavigationPlan_BuilderContract(t *testing.T) {
	t.Parallel()
	binding := pgNavBindingFixture()
	plan, err := newPGNavigationPlan(binding, []string{"7"})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if plan.statement != `SELECT * FROM "app"."orders" WHERE "id" = $1` {
		t.Fatalf("statement = %q", plan.statement)
	}
	if plan.source != pgParameterSourceNavigation {
		t.Fatalf("source = %v, want navigation", plan.source)
	}
	// Caller slice mutation cannot reach the plan.
	values := []string{"7"}
	plan2, _ := newPGNavigationPlan(binding, values)
	values[0] = "poisoned"
	if plan2.values[0] != "7" {
		t.Fatal("plan shares caller value storage")
	}

	// Two-column composite orders columns in FK order and numbers $1..$n.
	composite := pgNavBindingFixture()
	composite.pairs = append(composite.pairs, pgNavigationColumnPair{
		localAttnum: 3, localName: "z", localTypeOID: 20, localTypmod: -1,
		refAttnum: 2, refName: "z", refTypeOID: 20, refTypmod: -1})
	composite.source.name = "comp_src"
	composite.target.name = "comp tgt"
	composite.target.schema = "Schéma"
	plan3, err := newPGNavigationPlan(composite, []string{"1", "2"})
	if err != nil {
		t.Fatalf("composite plan: %v", err)
	}
	want := `SELECT * FROM "Schéma"."comp tgt" WHERE "id" = $1 AND "z" = $2`
	if plan3.statement != want {
		t.Fatalf("composite statement = %q, want %q", plan3.statement, want)
	}

	for name, mutate := range map[string]func(*pgNavigationBinding) []string{
		"nil binding":    func(b *pgNavigationBinding) []string { *b = pgNavigationBinding{}; return nil },
		"count mismatch": func(_ *pgNavigationBinding) []string { return []string{"1", "2", "3"} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := pgNavBindingFixture()
			if _, err := newPGNavigationPlan(b, mutate(b)); !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
				t.Fatalf("error = %v, want evidence-unavailable contract fault", err)
			}
		})
	}
	// NUL / invalid UTF-8 locators fail the shared text contract.
	if _, err := newPGNavigationPlan(binding, []string{"a\x00b"}); !errors.Is(err, ErrTemplateParameterInvalid) {
		t.Fatalf("NUL value error = %v, want ErrTemplateParameterInvalid", err)
	}
}

func TestPGNavigationBinding_SnapshotContract(t *testing.T) {
	t.Parallel()
	if got, err := snapshotPGNavigationBinding(nil); err != nil || got != nil {
		t.Fatalf("nil snapshot = %+v/%v, want nil/nil", got, err)
	}
	original := pgNavBindingFixture()
	snap, err := snapshotPGNavigationBinding(original)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if !snap.sameAs(original) {
		t.Fatal("snapshot must equal the prepared binding")
	}
	original.pairs[0].refName = "moved"
	if snap.pairs[0].refName != "id" {
		t.Fatal("snapshot shares the caller's pair storage")
	}
	broken := pgNavBindingFixture()
	broken.constraintOID = 0
	if _, err := snapshotPGNavigationBinding(broken); !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
		t.Fatalf("incomplete binding error = %v, want evidence unavailable", err)
	}
	// NUL and invalid UTF-8 names are rejected without any trimming or
	// normalization of otherwise legal catalog names.
	for name, corrupt := range map[string]func(*pgNavigationBinding){
		"fk NUL":            func(b *pgNavigationBinding) { b.fkName = "fk_\x00x" },
		"source name utf8":  func(b *pgNavigationBinding) { b.source.name = "bad\xff" },
		"target schema NUL": func(b *pgNavigationBinding) { b.target.schema = "app\x00" },
		"local name NUL":    func(b *pgNavigationBinding) { b.pairs[0].localName = "id\x00" },
		"ref name utf8":     func(b *pgNavigationBinding) { b.pairs[0].refName = "\xfeid" },
	} {
		bad := pgNavBindingFixture()
		corrupt(bad)
		if _, err := snapshotPGNavigationBinding(bad); !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
			t.Fatalf("%s: snapshot error = %v, want evidence unavailable", name, err)
		}
	}
}

// The attempt's entry-time consistency: navigation plan/binding must pair
// correctly — mismatches fail before any guard or connection work.
func TestPGExecute_NavigationInputContract(t *testing.T) {
	t.Parallel()
	base := PGExecuteInput{
		ConnConfig:   &pgx.ConnConfig{},
		Database:     "appdb",
		PinnedSchema: "app",
		Timeout:      time.Second,
	}
	cases := map[string]func(*PGExecuteInput){
		"nav binding without plan": func(in *PGExecuteInput) {
			in.navigation = pgNavBindingFixture()
			in.Statement = `SELECT * FROM "app"."orders" WHERE "id" = $1`
		},
		"nav plan without binding": func(in *PGExecuteInput) {
			in.Statement = `SELECT * FROM "app"."orders" WHERE "id" = $1`
			in.parameters = &pgParameterPlan{statement: in.Statement, values: []string{"7"}, source: pgParameterSourceNavigation}
		},
		"template plan with binding": func(in *PGExecuteInput) {
			in.navigation = pgNavBindingFixture()
			in.Statement = `SELECT $1::text`
			in.parameters = &pgParameterPlan{statement: in.Statement, values: []string{"7"}, source: pgParameterSourceTemplate}
		},
		"value count mismatch": func(in *PGExecuteInput) {
			in.navigation = pgNavBindingFixture()
			in.Statement = `SELECT * FROM "app"."orders" WHERE "id" = $1 AND "id" = $2`
			in.parameters = &pgParameterPlan{statement: in.Statement, values: []string{"7", "8"}, source: pgParameterSourceNavigation}
		},
		"mutated predicate same target": func(in *PGExecuteInput) {
			in.navigation = pgNavBindingFixture()
			in.Statement = `SELECT * FROM "app"."orders" WHERE "qty" = $1`
			in.parameters = &pgParameterPlan{statement: in.Statement, values: []string{"7"}, source: pgParameterSourceNavigation}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			in := base
			mutate(&in)
			res, ev, err := ExecutePGGovernedOutcome(context.Background(), in)
			if res != nil || err == nil || !errors.Is(err, pgsql.ErrEvidenceUnavailable) {
				t.Fatalf("outcome = %+v/%v, want evidence-unavailable contract fault before connection", res, err)
			}
			if ev == nil || ev.Status != model.QueryExecutionFailed {
				t.Fatalf("evidence = %+v, want failed", ev)
			}
		})
	}
}
