// Package service tests the keyed saved-statement PostgreSQL entry.
// input: testing, context, errors, sql, json, internal/model fixtures,
// fake statement reader + claim repo
// output: TestPGSavedStatementClaimed_* — shape ordering, authorized single
// read, typed-value/compile rejections before claim, keyed finalize with
// withheld preview and empty full statement, digest shape, and the private
// parameter plan handed to the attempt
// pos: PG-MULTI-ENTRY stage 2 unit coverage — the saved entry converges on
// the shared claimed runner; every pre-claim refusal writes the fixed
// NULL-key pair with no statement text
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
)

// fakeSavedStatementRead records GetByID arguments so the entry proves it
// reads the statement exactly once under the requested (target, id).
type fakeSavedStatementRead struct {
	resp       model.QuerySavedStatement
	err        error
	calls      int
	gotTarget  uint64
	gotID      uint64
	listCalled bool
}

func (f *fakeSavedStatementRead) ListVisible(_ context.Context, _ model.QuerySavedStatementListQuery) (model.QuerySavedStatementListResponse, error) {
	f.listCalled = true
	return model.QuerySavedStatementListResponse{}, errors.New("list is not part of the execution read")
}

func (f *fakeSavedStatementRead) GetByID(_ context.Context, targetResourceID, id uint64) (model.QuerySavedStatement, error) {
	f.calls++
	f.gotTarget, f.gotID = targetResourceID, id
	return f.resp, f.err
}

// savedPGFixture returns a personal parameterized saved statement bound to
// the pgClaimFixture credential (appdb/public).
func savedPGFixture() model.QuerySavedStatement {
	return model.QuerySavedStatement{
		ID:               41,
		TargetResourceID: 9002,
		OwnerUserID:      7,
		DatabaseName:     "appdb",
		SchemaName:       "",
		Name:             "typed pick",
		Statement:        `SELECT :status::text FROM orders WHERE qty > :minimum`,
		Scope:            model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "status", Type: model.QuerySavedStatementParameterString},
			{Name: "minimum", Type: model.QuerySavedStatementParameterInteger},
		},
	}
}

func newSavedClaimedService(repo *fakeExecRepo, resolver *fakeResolver, statements *fakeSavedStatementRead) *QueryExecutionService {
	return newClaimedService(repo, resolver).
		WithTemplateExecution(statements, NewTemplateStatementCompiler())
}

func validSavedRequest() model.QuerySavedStatementExecuteRequest {
	return model.QuerySavedStatementExecuteRequest{
		ClientExecutionID: "req-saved-1",
		Values: map[string]json.RawMessage{
			"status":  json.RawMessage(`"paid"`),
			"minimum": json.RawMessage(`20`),
		},
	}
}

// Shape failures — bad identity, machine actor, zero statementId, bad key,
// malformed request — consume no target read, no statement read, no claim,
// no evidence.
func TestPGSavedStatementClaimed_ShapeFailures(t *testing.T) {
	t.Parallel()
	cases := map[string]func(*model.QuerySavedStatementExecuteRequest, *uint64, *model.QueryExecutionIdentity){
		"machine identity": func(_ *model.QuerySavedStatementExecuteRequest, _ *uint64, id *model.QueryExecutionIdentity) {
			*id = model.QueryExecutionIdentity{Kind: model.QueryExecutionActorMachine, ID: 7}
		},
		"invalid identity": func(_ *model.QuerySavedStatementExecuteRequest, _ *uint64, id *model.QueryExecutionIdentity) {
			*id = model.QueryExecutionIdentity{}
		},
		"zero statement id": func(_ *model.QuerySavedStatementExecuteRequest, sid *uint64, _ *model.QueryExecutionIdentity) {
			*sid = 0
		},
		"missing key": func(r *model.QuerySavedStatementExecuteRequest, _ *uint64, _ *model.QueryExecutionIdentity) {
			r.ClientExecutionID = ""
		},
		"oversized key": func(r *model.QuerySavedStatementExecuteRequest, _ *uint64, _ *model.QueryExecutionIdentity) {
			r.ClientExecutionID = strings.Repeat("x", 65)
		},
		"key with NUL": func(r *model.QuerySavedStatementExecuteRequest, _ *uint64, _ *model.QueryExecutionIdentity) {
			r.ClientExecutionID = "a\x00b"
		},
		"negative maxRows": func(r *model.QuerySavedStatementExecuteRequest, _ *uint64, _ *model.QueryExecutionIdentity) {
			r.MaxRows = -1
		},
		"bad pagination": func(r *model.QuerySavedStatementExecuteRequest, _ *uint64, _ *model.QueryExecutionIdentity) {
			r.Pagination = &model.QueryExecutePaginationRequest{Page: 1, PageSize: 11}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			target, _, _ := pgClaimFixture()
			var targetQueries []model.QueryTargetListQuery
			statements := &fakeSavedStatementRead{resp: savedPGFixture()}
			svc := newClaimedServiceOn(repo, &fakeResolver{dsn: mustDSN(t)},
				fakeTargetRepo{targets: []model.QueryTarget{target}, queries: &targetQueries}).
				WithTemplateExecution(statements, NewTemplateStatementCompiler())
			req := validSavedRequest()
			sid := uint64(41)
			id := userExecutionIdentity(7)
			mutate(&req, &sid, &id)
			_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), id, 9002, sid, req)
			if !errors.Is(err, ErrQueryValidationFailed) {
				t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
			}
			if len(targetQueries) != 0 || statements.calls != 0 {
				t.Fatal("shape failure reached target or statement read")
			}
			if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
				t.Fatal("shape failure claimed or wrote evidence")
			}
		})
	}
}

// Unknown target: nothing attributable — no read, no claim, no pair.
func TestPGSavedStatementClaimed_TargetNotFound(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	statements := &fakeSavedStatementRead{resp: savedPGFixture()}
	svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, statements)
	_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 4242, 41, validSavedRequest())
	if !errors.Is(err, ErrQueryTargetNotFound) {
		t.Fatalf("error = %v, want ErrQueryTargetNotFound", err)
	}
	if statements.calls != 0 || len(repo.claimInputs) != 0 || len(repo.pairCalls) != 0 {
		t.Fatal("unknown target read the statement, claimed, or wrote evidence")
	}
}

// Missing statement and foreign personal statement both land as the fixed
// saved-not-found refusal plus exactly one NULL-key rejected pair.
func TestPGSavedStatementClaimed_NotFoundAndForeign(t *testing.T) {
	t.Parallel()
	cases := map[string]func() *fakeSavedStatementRead{
		"no row":    func() *fakeSavedStatementRead { return &fakeSavedStatementRead{err: sql.ErrNoRows} },
		"not owner": func() *fakeSavedStatementRead { return &fakeSavedStatementRead{resp: savedPGFixture()} },
	}
	for name, reader := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, reader())
			actor := userExecutionIdentity(7)
			if name == "not owner" {
				actor = userExecutionIdentity(9) // a different user — incl. no admin bypass
			}
			_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), actor, 9002, 41, validSavedRequest())
			if !errors.Is(err, ErrQuerySavedStatementNotFound) {
				t.Fatalf("error = %v, want ErrQuerySavedStatementNotFound", err)
			}
			if len(repo.claimInputs) != 0 {
				t.Fatal("not-found/not-authorized occupied the key")
			}
			if len(repo.pairCalls) != 1 {
				t.Fatalf("pairCalls = %d, want 1 NULL-key rejected pair", len(repo.pairCalls))
			}
			rec := repo.pairCalls[0].rec
			if rec.Status != model.QueryExecutionRejected || rec.ClientExecutionID != nil ||
				rec.StatementPreview != pgClaimedWithheldPreview || rec.FullStatement != "" {
				t.Fatalf("null-key record = %+v, want rejected/withheld/no full statement", rec)
			}
		})
	}
}

// A backend read fault is the fixed backend sentinel plus one NULL-key
// failed pair — the raw driver/store error never escapes.
func TestPGSavedStatementClaimed_ReadFault(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	statements := &fakeSavedStatementRead{err: errors.New("driver: secret-dsn-do-not-leak")}
	svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, statements)
	_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, validSavedRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if strings.Contains(err.Error(), "secret-dsn-do-not-leak") {
		t.Fatal("raw read error escaped")
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionFailed {
		t.Fatalf("pairCalls = %+v, want one NULL-key failed pair", repo.pairCalls)
	}
}

// Typed-value failures — missing, unknown, nil JSON, wrong type, oversized —
// reject before claim with one NULL-key rejected pair and no occupancy.
func TestPGSavedStatementClaimed_InvalidValuesPreClaim(t *testing.T) {
	t.Parallel()
	mutations := map[string]func(map[string]json.RawMessage){
		"missing":   func(v map[string]json.RawMessage) { delete(v, "minimum") },
		"unknown":   func(v map[string]json.RawMessage) { v["extra"] = json.RawMessage(`1`) },
		"json null": func(v map[string]json.RawMessage) { v["status"] = json.RawMessage(`null`) },
		"wrong type": func(v map[string]json.RawMessage) {
			v["minimum"] = json.RawMessage(`"not-a-number"`)
		},
		"oversized": func(v map[string]json.RawMessage) {
			v["status"] = json.RawMessage(`"` + strings.Repeat("x", templateMaxValueBytes+1) + `"`)
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			statements := &fakeSavedStatementRead{resp: savedPGFixture()}
			svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, statements)
			req := validSavedRequest()
			mutate(req.Values)
			_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, req)
			var valueErr *TemplateValueValidationError
			if !errors.As(err, &valueErr) {
				t.Fatalf("error = %v, want TemplateValueValidationError", err)
			}
			if len(repo.claimInputs) != 0 {
				t.Fatal("invalid values occupied the key")
			}
			if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.Status != model.QueryExecutionRejected ||
				repo.pairCalls[0].rec.ErrorCode != "validation_failed" {
				t.Fatalf("pairCalls = %+v, want one NULL-key validation rejection", repo.pairCalls)
			}
		})
	}
}

// A stored statement carrying a native $n is a compile-time validation
// refusal before claim — the template surface never binds user text.
func TestPGSavedStatementClaimed_NativeParameterRejected(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	stmt := savedPGFixture()
	stmt.Statement = `SELECT $1::text`
	stmt.Parameters = nil
	statements := &fakeSavedStatementRead{resp: stmt}
	svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, statements)
	_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41,
		model.QuerySavedStatementExecuteRequest{ClientExecutionID: "req-saved-native"})
	if !errors.Is(err, ErrQueryValidationFailed) {
		t.Fatalf("error = %v, want ErrQueryValidationFailed", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 1 {
		t.Fatalf("claims=%d pairs=%d, want 0 claims + 1 NULL-key rejection", len(repo.claimInputs), len(repo.pairCalls))
	}
}

// Claim admitted: the captured outcome input carries the compiled $k
// statement, the private plan, and the strengthened pinned-schema flag —
// while the keyed finalize persists the withheld preview, empty full
// statement, and the saved-statement digest.
func TestPGSavedStatementClaimed_KeyedAttemptEvidence(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	statements := &fakeSavedStatementRead{resp: savedPGFixture()}
	svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, statements)

	var captured PGDisclosureInput
	svc.pgOutcomeFn = func(_ context.Context, in PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		captured = in
		return nil, &PGAttemptEvidence{Stage: PGStageGuard, Status: model.QueryExecutionRejected},
			&pgsql.RejectError{Code: "query_not_allowed", Message: "pgsql: forced rejection"}
	}

	_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, validSavedRequest())
	if err == nil {
		t.Fatal("injected rejection must surface")
	}
	if len(repo.claimInputs) != 1 {
		t.Fatalf("claims = %d, want 1", len(repo.claimInputs))
	}
	claim := repo.claimInputs[0]
	wantDigest := DigestSavedStatement(SavedStatementDigestInput{
		Database: "appdb", Schema: "public", StatementID: 41,
		Values: []ClaimDigestNamedValue{
			claimDigestNamed("status", ClaimDigestString("paid")),
			claimDigestNamed("minimum", ClaimDigestInteger(20)),
		},
		Page: 1, PageSize: pgClaimDefaultPageSize, MaxRows: pgClaimDefaultMaxRows,
	})
	if claim.RequestDigest != wantDigest {
		t.Fatalf("claim digest = %q, want saved-statement digest %q", claim.RequestDigest, wantDigest)
	}
	if claim.SchemaName != "public" {
		t.Fatalf("claim schema = %q, want credential default", claim.SchemaName)
	}
	if captured.Execute.Statement != `SELECT $1::text FROM orders WHERE qty > $2` {
		t.Fatalf("executed statement = %q, want compiled $k text", captured.Execute.Statement)
	}
	if captured.Execute.parameters == nil || len(captured.Execute.parameters.values) != 2 ||
		captured.Execute.parameters.values[0] != "paid" || captured.Execute.parameters.values[1] != "20" {
		t.Fatalf("parameter plan = %+v, want ordered text values", captured.Execute.parameters)
	}
	if !captured.Execute.requirePinnedSchema {
		t.Fatal("saved entry must strengthen the pinned-schema gate")
	}
	if len(repo.finalizeCalls) != 1 {
		t.Fatalf("finalize = %d, want 1", len(repo.finalizeCalls))
	}
	rec := repo.finalizeCalls[0]
	if rec.StatementPreview != pgClaimedWithheldPreview || rec.FullStatement != "" {
		t.Fatalf("saved keyed record preview=%q full=%q, want withheld + empty",
			rec.StatementPreview, rec.FullStatement)
	}
	if rec.StatementDigest != wantDigest || rec.Status != model.QueryExecutionRejected {
		t.Fatalf("keyed record = %+v, want saved digest + rejected", rec)
	}
	if !strings.HasPrefix(repo.finalizeEvents[0], "query.executed:") {
		t.Fatalf("saved finalize event = %q, want the query.executed family", repo.finalizeEvents[0])
	}
}

// claimDigestNamed pins the parameter name onto the typed digest value.
func claimDigestNamed(name string, v ClaimDigestNamedValue) ClaimDigestNamedValue {
	v.Name = name
	return v
}

// Duplicate/conflict admission classify without executing or writing.
func TestPGSavedStatementClaimed_ClaimClassifications(t *testing.T) {
	t.Parallel()
	for name, claimErr := range map[string]error{
		"exists":   model.ErrQueryExecutionClaimExists,
		"conflict": model.ErrQueryExecutionClaimConflict,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			repo.claimErr = claimErr
			statements := &fakeSavedStatementRead{resp: savedPGFixture()}
			svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, statements)
			_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, validSavedRequest())
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

// A claim-store failure writes one NULL-key failed pair and never lets the
// key masquerade as occupied — same contract as the ordinary entry.
func TestPGSavedStatementClaimed_ClaimStoreFailure(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	repo.claimErr = errors.New("connection reset")
	statements := &fakeSavedStatementRead{resp: savedPGFixture()}
	svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, statements)
	_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, validSavedRequest())
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.ClientExecutionID != nil ||
		repo.pairCalls[0].rec.Status != model.QueryExecutionFailed {
		t.Fatalf("pairCalls = %+v, want one NULL-key failed pair", repo.pairCalls)
	}
}

// A shared template is visible to any user identity; the keyed finalize runs
// under the caller, never the owner.
func TestPGSavedStatementClaimed_SharedVisible(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	stmt := savedPGFixture()
	stmt.Scope = model.QuerySavedStatementSharedTemplate
	stmt.Statement = `SELECT :status::text AS s`
	stmt.Parameters = stmt.Parameters[:1]
	statements := &fakeSavedStatementRead{resp: stmt}
	svc := newSavedClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}, statements)
	req := model.QuerySavedStatementExecuteRequest{
		ClientExecutionID: "req-saved-shared",
		Values:            map[string]json.RawMessage{"status": json.RawMessage(`"open"`)},
	}
	// Different user id — shared templates execute for non-owners. The dead
	// DSN lands a keyed failed finalize, which is the claim-occupied proof.
	_, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(9), 9002, 41, req)
	if err == nil {
		t.Fatal("dead DSN must surface a classified failure")
	}
	if len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 {
		t.Fatalf("claims=%d finalize=%d, want 1/1", len(repo.claimInputs), len(repo.finalizeCalls))
	}
	if repo.finalizeCalls[0].ActorUserID != 9 {
		t.Fatalf("actor = %d, want the executing user 9", repo.finalizeCalls[0].ActorUserID)
	}
}
