// Package service — R1-2 fault taxonomy for the saved entry's snapshot read.
// input: scripted QuerySavedStatementReader seams (block-on-context, injected
// errors), fakeExecRepo, injected pgOutcomeFn
// output: TestPGSavedRead* — the GetByID failure branch returns bare
// canonical sentinels, writes exactly one NULL-key evidence pair with fixed
// status/code/message, never leaks wrapped detail, and leaves the client key
// free for a later real attempt
// pos: PG-MULTI-ENTRY R1-2 — reader fault injection only; no real MySQL
// timeout is simulated, the reader's error is the controlled seam
// note: if this file changes, update header and module README.md
package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

const savedReadMarker = "read-secret-MRK-R12"

// scriptedSavedRead injects a controlled error at snapshot-read time.
// entered is closed on the first GetByID so the test can cancel the request
// context while the read is genuinely in flight; errFn runs with the live
// request context so ctx.Err() reflects real cancellation.
type scriptedSavedRead struct {
	fakeSavedStatementRead
	entered chan struct{}
	errFn   func(ctx context.Context) error
}

func (f *scriptedSavedRead) GetByID(ctx context.Context, targetResourceID, id uint64) (model.QuerySavedStatement, error) {
	f.calls++
	f.gotTarget, f.gotID = targetResourceID, id
	if f.entered != nil {
		select {
		case <-f.entered:
		default:
			close(f.entered)
		}
	}
	if f.errFn != nil {
		if err := f.errFn(ctx); err != nil {
			return model.QuerySavedStatement{}, err
		}
	}
	return f.resp, f.err
}

func newScriptedSavedService(t *testing.T, repo *fakeExecRepo, reader *scriptedSavedRead) *QueryExecutionService {
	t.Helper()
	return newClaimedService(repo, &fakeResolver{dsn: mustDSN(t)}).
		WithTemplateExecution(reader, NewTemplateStatementCompiler())
}

// errorTreeText flattens every message in the error tree — both the single
// Unwrap() error and Unwrap() []error shapes — so a secret marker is proven
// absent from the entire chain, not just the outer layer.
func errorTreeText(err error) string {
	var sb strings.Builder
	seen := map[error]bool{}
	var walk func(error)
	walk = func(e error) {
		if e == nil || seen[e] {
			return
		}
		seen[e] = true
		sb.WriteString(e.Error())
		sb.WriteByte(0)
		if many, ok := e.(interface{ Unwrap() []error }); ok {
			for _, sub := range many.Unwrap() {
				walk(sub)
			}
			return
		}
		walk(errors.Unwrap(e))
	}
	walk(err)
	return sb.String()
}

// assertNullKeySavedReadRecord proves the single pre-claim evidence pair:
// fixed status/code/message, NULL key, no backend identity, no remote state,
// withheld preview, no full statement, no marker text anywhere.
func assertNullKeySavedReadRecord(t *testing.T, repo *fakeExecRepo, status model.QueryExecutionStatus, code, msg string) {
	t.Helper()
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairCalls = %d, want exactly 1 NULL-key evidence pair", len(repo.pairCalls))
	}
	rec := repo.pairCalls[0].rec
	if rec.Status != status || rec.ErrorCode != code || rec.ErrorMessage != msg {
		t.Fatalf("record = %+v, want status=%s code=%s msg=%q", rec, status, code, msg)
	}
	if rec.ClientExecutionID != nil || rec.BackendPID != nil || rec.RemoteState != "" ||
		rec.StatementPreview != pgClaimedWithheldPreview || rec.FullStatement != "" {
		t.Fatalf("record carries execution detail it must not have: %+v", rec)
	}
	for _, field := range []string{rec.ErrorCode, rec.ErrorMessage, rec.StatementPreview, rec.DatabaseName, rec.SchemaName} {
		if strings.Contains(field, savedReadMarker) {
			t.Fatalf("marker leaked into persisted record: %+v", rec)
		}
	}
}

func assertSavedReadNoExecutionWork(t *testing.T, repo *fakeExecRepo, outcomeCalls int) {
	t.Helper()
	if len(repo.claimInputs) != 0 {
		t.Fatalf("read failure claimed the key: %+v", repo.claimInputs)
	}
	if len(repo.finalizeCalls) != 0 {
		t.Fatalf("read failure finalized: %+v", repo.finalizeCalls)
	}
	if outcomeCalls != 0 {
		t.Fatalf("read failure reached the outcome function %d times", outcomeCalls)
	}
}

// Real cancellation mid-read: the reader announces entry, the test cancels
// the request context, and the read returns ctx.Err() — the entry answers
// with the bare context.Canceled sentinel and a cancelled NULL-key pair.
func TestPGSavedRead_CancelBarrier(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	reader := &scriptedSavedRead{
		entered: make(chan struct{}),
		errFn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	svc := newScriptedSavedService(t, repo, reader)
	outcomeCalls := 0
	svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		outcomeCalls++
		return nil, nil, errors.New("outcome must never run")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		out, err := svc.ExecutePGSavedStatementClaimed(ctx, userExecutionIdentity(7), 9002, 41, validSavedRequest())
		if out != (PGClaimedOutcome{}) {
			done <- fmt.Errorf("nonzero outcome: %+v", out)
			return
		}
		done <- err
	}()
	select {
	case <-reader.entered:
	case err := <-done:
		t.Fatalf("entry returned before the read was entered: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("reader never entered before cancel")
	}
	cancel()
	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("err = %v, want bare context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("entry never returned after cancel")
	}
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("attempt goroutine never finished")
	}
	assertNullKeySavedReadRecord(t, repo, model.QueryExecutionCancelled, "query_cancelled", "query canceled")
	// The evidence write rode the detached persistence window — the request
	// context was already canceled, the window context must be live.
	if repo.pairCtxErr != nil || !repo.pairCtxHasDeadline {
		t.Fatalf("pair ctx err=%v hasDeadline=%v — want live detached window", repo.pairCtxErr, repo.pairCtxHasDeadline)
	}
	assertSavedReadNoExecutionWork(t, repo, outcomeCalls)
}

// Real deadline mid-read: same barrier shape with a finite attempt context —
// the deadline error returns as bare ErrQueryTimeout with a timeout pair.
func TestPGSavedRead_DeadlineBarrier(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	reader := &scriptedSavedRead{
		errFn: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	svc := newScriptedSavedService(t, repo, reader)
	outcomeCalls := 0
	svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		outcomeCalls++
		return nil, nil, errors.New("outcome must never run")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userExecutionIdentity(7), 9002, 41, validSavedRequest())
	if err != ErrQueryTimeout {
		t.Fatalf("err = %v, want bare ErrQueryTimeout", err)
	}
	if out != (PGClaimedOutcome{}) {
		t.Fatalf("outcome = %+v, want zero", out)
	}
	if reader.calls != 1 {
		t.Fatalf("GetByID calls = %d, want exactly 1", reader.calls)
	}
	assertNullKeySavedReadRecord(t, repo, model.QueryExecutionTimeout, "query_timeout", "query exceeded the time limit")
	if repo.pairCtxErr != nil || !repo.pairCtxHasDeadline {
		t.Fatalf("pair ctx err=%v hasDeadline=%v — want live detached window", repo.pairCtxErr, repo.pairCtxHasDeadline)
	}
	assertSavedReadNoExecutionWork(t, repo, outcomeCalls)
}

// Wrapped and joined errors — including a marker that must never surface —
// canonicalize to the same bare sentinels through the full entry.
func TestPGSavedRead_WrappedJoinedCanonicalize(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		in     error
		want   error
		status model.QueryExecutionStatus
		code   string
		msg    string
	}{
		"wrapped canceled":     {fmt.Errorf("store read: %w", context.Canceled), context.Canceled, model.QueryExecutionCancelled, "query_cancelled", "query canceled"},
		"joined canceled":      {errors.Join(context.Canceled, errors.New(savedReadMarker)), context.Canceled, model.QueryExecutionCancelled, "query_cancelled", "query canceled"},
		"wrapped deadline":     {fmt.Errorf("store read: %w: %s", context.DeadlineExceeded, savedReadMarker), ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout", "query exceeded the time limit"},
		"joined deadline":      {errors.Join(context.DeadlineExceeded, errors.New(savedReadMarker)), ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout", "query exceeded the time limit"},
		"wrapped timeout sent": {fmt.Errorf("store read: %w: %s", ErrQueryTimeout, savedReadMarker), ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout", "query exceeded the time limit"},
		"generic store error":  {errors.New("store: " + savedReadMarker), ErrQueryBackendFailure, model.QueryExecutionFailed, "query_backend_error", "target database query failed"},
		"wrapped no rows":      {fmt.Errorf("store read: %w: %s", sql.ErrNoRows, savedReadMarker), ErrQuerySavedStatementNotFound, model.QueryExecutionRejected, "saved_statement_not_found", "saved statement not found"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			reader := &scriptedSavedRead{errFn: func(context.Context) error { return tc.in }}
			svc := newScriptedSavedService(t, repo, reader)
			outcomeCalls := 0
			svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				outcomeCalls++
				return nil, nil, errors.New("outcome must never run")
			}
			out, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, validSavedRequest())
			if err != tc.want {
				t.Fatalf("err = %v, want bare %v", err, tc.want)
			}
			if out != (PGClaimedOutcome{}) {
				t.Fatalf("outcome = %+v, want zero", out)
			}
			if strings.Contains(errorTreeText(err), savedReadMarker) {
				t.Fatalf("marker leaked into returned error: %v", err)
			}
			assertNullKeySavedReadRecord(t, repo, tc.status, tc.code, tc.msg)
			assertSavedReadNoExecutionWork(t, repo, outcomeCalls)
		})
	}
}

// A failed persistence attempt outranks the read failure: the caller sees
// only the fixed persist sentinel and a zero outcome.
func TestPGSavedRead_PersistFailureWins(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	repo.pairErr = errors.New("pair-store-down")
	reader := &scriptedSavedRead{errFn: func(context.Context) error {
		return fmt.Errorf("store read: %w", context.Canceled)
	}}
	svc := newScriptedSavedService(t, repo, reader)
	out, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, validSavedRequest())
	if !errors.Is(err, errPersistAttempt) {
		t.Fatalf("err = %v, want errPersistAttempt", err)
	}
	if out != (PGClaimedOutcome{}) {
		t.Fatalf("outcome = %+v, want zero", out)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatal("failed persistence must never claim or finalize")
	}
}

// A NULL-key read failure never occupies the client key: the same key on a
// fresh context, with the reader now healthy and a real injected outcome,
// runs the normal claim → outcome → keyed finalize path exactly once.
func TestPGSavedRead_FailedReadLeavesKeyFree(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	fail := true
	reader := &scriptedSavedRead{
		fakeSavedStatementRead: fakeSavedStatementRead{resp: savedPGFixture()},
		errFn: func(context.Context) error {
			if fail {
				return errors.New("store: " + savedReadMarker)
			}
			return nil
		},
	}
	svc := newScriptedSavedService(t, repo, reader)
	outcomeCalls := 0
	svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		outcomeCalls++
		v := "ok"
		return &PGDisclosedResult{
				Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "out", DataTypeOID: 25}}},
				Rows:          [][]*string{{&v}},
				CellTruncated: [][]bool{{false}},
			}, &PGAttemptEvidence{Stage: PGStageDeliver, Status: model.QueryExecutionSuccess,
				BackendPID: 7, RemoteState: model.QueryExecutionRemoteCompleted}, nil
	}
	req := validSavedRequest()
	if _, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, req); !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("first err = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.claimInputs) != 0 || len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.ClientExecutionID != nil {
		t.Fatalf("first attempt: claims=%d pairs=%d — NULL-key pair only", len(repo.claimInputs), len(repo.pairCalls))
	}
	fail = false
	out, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, req)
	if err != nil {
		t.Fatalf("second execute: %v", err)
	}
	if out.Status != model.QueryExecutionSuccess || out.ExecutionID == 0 {
		t.Fatalf("second outcome = %+v, want keyed success", out)
	}
	if len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 || outcomeCalls != 1 {
		t.Fatalf("second attempt: claims=%d finalize=%d outcome=%d, want exactly one each",
			len(repo.claimInputs), len(repo.finalizeCalls), outcomeCalls)
	}
}

// A read that dies inside a canceled or expired request context writes only
// a NULL-key pair — the client key is untouched. The same key on a fresh
// context, with the reader healthy and a real injected outcome, runs the
// normal claim → outcome → keyed finalize path exactly once, and the prior
// NULL-key pair stays as evidence.
func TestPGSavedRead_CancelDeadlineLeaveKeyFree(t *testing.T) {
	t.Parallel()

	newOutcomeFn := func(calls *int) func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
		return func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
			*calls++
			v := "ok"
			return &PGDisclosedResult{
					Columns:       []PGDisclosedColumn{{PGResultColumn: PGResultColumn{Name: "out", DataTypeOID: 25}}},
					Rows:          [][]*string{{&v}},
					CellTruncated: [][]bool{{false}},
				}, &PGAttemptEvidence{Stage: PGStageDeliver, Status: model.QueryExecutionSuccess,
					BackendPID: 7, RemoteState: model.QueryExecutionRemoteCompleted}, nil
		}
	}

	runHealthyRetry := func(t *testing.T, repo *fakeExecRepo, reader *scriptedSavedRead, healthy *bool, req model.QuerySavedStatementExecuteRequest) {
		t.Helper()
		*healthy = true
		outcomeCalls := 0
		svc := newScriptedSavedService(t, repo, reader)
		svc.pgOutcomeFn = newOutcomeFn(&outcomeCalls)
		out, err := svc.ExecutePGSavedStatementClaimed(context.Background(), userExecutionIdentity(7), 9002, 41, req)
		if err != nil {
			t.Fatalf("same-key retry after pre-claim read failure: %v", err)
		}
		if out.Status != model.QueryExecutionSuccess || out.ExecutionID == 0 {
			t.Fatalf("retry outcome = %+v, want keyed success", out)
		}
		if len(repo.claimInputs) != 1 || len(repo.finalizeCalls) != 1 || outcomeCalls != 1 {
			t.Fatalf("retry path: claims=%d finalize=%d outcome=%d, want exactly one each",
				len(repo.claimInputs), len(repo.finalizeCalls), outcomeCalls)
		}
		// The retained NULL-key read-failure pair is the only evidence-pair
		// write; the keyed success lands through the keyed finalize.
		if len(repo.pairCalls) != 1 || repo.pairCalls[0].rec.ClientExecutionID != nil {
			t.Fatalf("pairs = %d — want the one retained NULL-key pair", len(repo.pairCalls))
		}
	}

	t.Run("canceled read leaves key free", func(t *testing.T) {
		t.Parallel()
		repo := claimedPGRepo(t)
		healthy := false
		reader := &scriptedSavedRead{
			fakeSavedStatementRead: fakeSavedStatementRead{resp: savedPGFixture()},
			entered:                make(chan struct{}),
			errFn: func(ctx context.Context) error {
				if healthy {
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			},
		}
		svc := newScriptedSavedService(t, repo, reader)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			_, err := svc.ExecutePGSavedStatementClaimed(ctx, userExecutionIdentity(7), 9002, 41, validSavedRequest())
			done <- err
		}()
		select {
		case <-reader.entered:
		case err := <-done:
			t.Fatalf("entry returned before the read was entered: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("reader never entered")
		}
		cancel()
		select {
		case err := <-done:
			if err != context.Canceled {
				t.Fatalf("err = %v, want bare context.Canceled", err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("entry never returned after cancel")
		}
		select {
		case <-finished:
		case <-time.After(10 * time.Second):
			t.Fatal("attempt goroutine never finished")
		}
		runHealthyRetry(t, repo, reader, &healthy, validSavedRequest())
	})

	t.Run("deadline read leaves key free", func(t *testing.T) {
		t.Parallel()
		repo := claimedPGRepo(t)
		healthy := false
		reader := &scriptedSavedRead{
			fakeSavedStatementRead: fakeSavedStatementRead{resp: savedPGFixture()},
			errFn: func(ctx context.Context) error {
				if healthy {
					return nil
				}
				<-ctx.Done()
				return ctx.Err()
			},
		}
		svc := newScriptedSavedService(t, repo, reader)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
		defer cancel()
		_, err := svc.ExecutePGSavedStatementClaimed(ctx, userExecutionIdentity(7), 9002, 41, validSavedRequest())
		if err != ErrQueryTimeout {
			t.Fatalf("err = %v, want bare ErrQueryTimeout", err)
		}
		runHealthyRetry(t, repo, reader, &healthy, validSavedRequest())
	})
}
