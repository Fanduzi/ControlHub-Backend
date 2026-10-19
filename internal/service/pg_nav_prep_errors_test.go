// Package service — R1-3 preparation-fault taxonomy for the navigation entry.
// input: navPrepFail unit cases, injected pgNavPrepare seams (bare/wrapped/
// joined sentinels, unknown errors, markers), fakeExecRepo
// output: TestNavPrepFail* / TestPGNavigationPrep* — preparation errors
// always collapse to bare canonical sentinels through both the classifier
// and the full entry, each fault writes exactly one correct NULL-key
// navigation pair, and no claim, finalize, outcome call, or secret marker
// ever escapes the seam
// pos: PG-MULTI-ENTRY R1-3 — prep-fault injection only; a constructed
// ErrPGVersionUnsupported is not a real PG13/18 run, and version-reader
// behavior itself is covered by the pool factory's own tests
// note: if this file changes, update header and module README.md
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/model"
)

const navPrepMarker = "prep-secret-MRK-R13"

// navPrepFail canonicalizes every wrapped/joined shape to the bare sentinel —
// the returned value is the sentinel itself, never the caller's error.
func TestNavPrepFailCanonicalizes(t *testing.T) {
	t.Parallel()
	pg57014 := &pgconn.PgError{Code: "57014", Message: navPrepMarker}
	cases := map[string]struct {
		in   error
		want error
	}{
		"canceled bare":         {context.Canceled, context.Canceled},
		"canceled wrapped":      {fmt.Errorf("prep: %w", context.Canceled), context.Canceled},
		"canceled joined":       {errors.Join(context.Canceled, errors.New(navPrepMarker)), context.Canceled},
		"deadline bare":         {context.DeadlineExceeded, ErrQueryTimeout},
		"deadline wrapped":      {fmt.Errorf("prep: %w: %s", context.DeadlineExceeded, navPrepMarker), ErrQueryTimeout},
		"timeout sentinel wrap": {fmt.Errorf("prep: %w", ErrQueryTimeout), ErrQueryTimeout},
		"57014 bare":            {pg57014, ErrQueryTimeout},
		"57014 wrapped":         {fmt.Errorf("dial: %w", pg57014), ErrQueryTimeout},
		"version bare":          {ErrPGVersionUnsupported, ErrPGVersionUnsupported},
		"version wrapped":       {fmt.Errorf("open: %w: %s", ErrPGVersionUnsupported, navPrepMarker), ErrPGVersionUnsupported},
		"version joined":        {errors.Join(ErrPGVersionUnsupported, errors.New(navPrepMarker)), ErrPGVersionUnsupported},
		"connect bare":          {ErrPGConnectFailed, ErrPGConnectFailed},
		"connect wrapped":       {fmt.Errorf("open: %w", ErrPGConnectFailed), ErrPGConnectFailed},
		"version read bare":     {ErrPGVersionReadFailed, ErrPGVersionReadFailed},
		"version read joined":   {errors.Join(ErrPGVersionReadFailed, errors.New(navPrepMarker)), ErrPGVersionReadFailed},
		"no source bare":        {errPGNavNoSource, errPGNavNoSource},
		"no source wrapped":     {fmt.Errorf("probe: %w", errPGNavNoSource), errPGNavNoSource},
		"internal bare":         {errPGNavMetaInternal, errPGNavMetaInternal},
		"internal wrapped":      {fmt.Errorf("probe: %w", errPGNavMetaInternal), errPGNavMetaInternal},
		"unknown bare":          {errors.New(navPrepMarker), errPGNavMetaInternal},
		"unknown wrapped":       {fmt.Errorf("prep: %w", errors.New(navPrepMarker)), errPGNavMetaInternal},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := navPrepFail(tc.in)
			if got != tc.want {
				t.Fatalf("navPrepFail = %v, want bare sentinel %v", got, tc.want)
			}
			if strings.Contains(got.Error(), navPrepMarker) {
				t.Fatalf("marker leaked through navPrepFail: %v", got)
			}
		})
	}
	if navPrepFail(nil) != nil {
		t.Fatal("nil must pass through unchanged")
	}
}

// assertNavPrepNullPair proves the single pre-claim evidence pair: the
// navigation event family, fixed status/code/message, NULL key, no backend
// identity, withheld preview, no full statement, no marker anywhere.
func assertNavPrepNullPair(t *testing.T, repo *fakeExecRepo, status model.QueryExecutionStatus, code, msg string) {
	t.Helper()
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairCalls = %d, want exactly 1 NULL-key navigation pair", len(repo.pairCalls))
	}
	pair := repo.pairCalls[0]
	if pair.event != "related_record_navigation" {
		t.Fatalf("audit event = %q, want related_record_navigation", pair.event)
	}
	rec := pair.rec
	if rec.Status != status || rec.ErrorCode != code || rec.ErrorMessage != msg {
		t.Fatalf("record = %+v, want status=%s code=%s msg=%q", rec, status, code, msg)
	}
	if rec.ClientExecutionID != nil || rec.BackendPID != nil || rec.RemoteState != "" ||
		rec.StatementPreview != pgClaimedWithheldPreview || rec.FullStatement != "" {
		t.Fatalf("record carries execution detail it must not have: %+v", rec)
	}
	for _, field := range []string{rec.ErrorCode, rec.ErrorMessage, rec.StatementPreview} {
		if strings.Contains(field, navPrepMarker) {
			t.Fatalf("marker leaked into persisted record: %+v", rec)
		}
	}
}

// A preparation fault injected at the seam — bare, wrapped, or joined — is
// normalized before outcome selection: the factory sentinels return their
// bare canonical identity with one NULL-key failed pair, the map keeps its
// not-found refusal, cancel/deadline keep their real categories, and unknown
// detail collapses to the fixed backend failure. No claim, no finalize, no
// outcome function, no user SQL.
func TestPGNavigationPrepFaultNormalization(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		prepErr    error
		wantErr    error
		wantStatus model.QueryExecutionStatus
		wantCode   string
		wantMsg    string
		wantPool   string
	}{
		"version unsupported bare":    {ErrPGVersionUnsupported, ErrPGVersionUnsupported, model.QueryExecutionFailed, "query_backend_error", "target database query failed", PGVersionUnsupportedCode},
		"version unsupported wrapped": {fmt.Errorf("prep: %w: %s", ErrPGVersionUnsupported, navPrepMarker), ErrPGVersionUnsupported, model.QueryExecutionFailed, "query_backend_error", "target database query failed", PGVersionUnsupportedCode},
		"version unsupported joined":  {errors.Join(ErrPGVersionUnsupported, errors.New(navPrepMarker)), ErrPGVersionUnsupported, model.QueryExecutionFailed, "query_backend_error", "target database query failed", PGVersionUnsupportedCode},
		"connect failed bare":         {ErrPGConnectFailed, ErrPGConnectFailed, model.QueryExecutionFailed, "query_backend_error", "target database query failed", "query_backend_error"},
		"connect failed wrapped":      {fmt.Errorf("prep: %w", ErrPGConnectFailed), ErrPGConnectFailed, model.QueryExecutionFailed, "query_backend_error", "target database query failed", "query_backend_error"},
		"version read failed bare":    {ErrPGVersionReadFailed, ErrPGVersionReadFailed, model.QueryExecutionFailed, "query_backend_error", "target database query failed", "query_backend_error"},
		"version read failed joined":  {errors.Join(ErrPGVersionReadFailed, errors.New(navPrepMarker)), ErrPGVersionReadFailed, model.QueryExecutionFailed, "query_backend_error", "target database query failed", "query_backend_error"},
		"unknown backend detail":      {errors.New("dial tcp: " + navPrepMarker), ErrQueryBackendFailure, model.QueryExecutionFailed, "query_backend_error", "target database query failed", "query_backend_error"},
		"canceled bare":               {context.Canceled, context.Canceled, model.QueryExecutionCancelled, "query_cancelled", "query canceled", "query_canceled"},
		"canceled wrapped":            {fmt.Errorf("prep: %w", context.Canceled), context.Canceled, model.QueryExecutionCancelled, "query_cancelled", "query canceled", "query_canceled"},
		"deadline wrapped":            {fmt.Errorf("prep: %w: %s", context.DeadlineExceeded, navPrepMarker), ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout", "database query timed out", "query_timeout"},
		"timeout sentinel joined":     {errors.Join(ErrQueryTimeout, errors.New(navPrepMarker)), ErrQueryTimeout, model.QueryExecutionTimeout, "query_timeout", "database query timed out", "query_timeout"},
		"no source bare":              {errPGNavNoSource, ErrNavigationSourceNotFound, model.QueryExecutionRejected, "navigation_source_not_found", "navigation source object or foreign key not found", ""},
		"no source wrapped":           {fmt.Errorf("prep: %w", errPGNavNoSource), ErrNavigationSourceNotFound, model.QueryExecutionRejected, "navigation_source_not_found", "navigation source object or foreign key not found", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			repo := claimedPGRepo(t)
			prepErr := tc.prepErr
			svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)},
				func(context.Context, *pgx.ConnConfig, string, model.RelatedRecordNavigationSource, time.Duration) (*pgNavigationBinding, error) {
					return nil, prepErr
				})
			outcomeCalls := 0
			svc.pgOutcomeFn = func(context.Context, PGDisclosureInput) (*PGDisclosedResult, *PGAttemptEvidence, error) {
				outcomeCalls++
				return nil, nil, errors.New("outcome must never run")
			}
			out, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
			if err != tc.wantErr {
				t.Fatalf("err = %v, want bare %v", err, tc.wantErr)
			}
			if strings.Contains(errorTreeText(err), navPrepMarker) {
				t.Fatalf("marker leaked into returned error: %v", err)
			}
			if tc.wantPool != "" && PGPoolErrorCode(err) != tc.wantPool {
				t.Fatalf("PGPoolErrorCode(err) = %q, want %q", PGPoolErrorCode(err), tc.wantPool)
			}
			if out != (PGClaimedNavigationOutcome{}) {
				t.Fatalf("outcome = %+v, want zero — no relation on a prep fault", out)
			}
			assertNavPrepNullPair(t, repo, tc.wantStatus, tc.wantCode, tc.wantMsg)
			if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 || outcomeCalls != 0 {
				t.Fatalf("prep fault reached claim/finalize/outcome: claims=%d finalize=%d outcome=%d",
					len(repo.claimInputs), len(repo.finalizeCalls), outcomeCalls)
			}
		})
	}
}

// A failed evidence write outranks the preparation error: the caller sees
// only the persist sentinel and a zero outcome.
func TestPGNavigationPrepPersistFailureWins(t *testing.T) {
	t.Parallel()
	repo := claimedPGRepo(t)
	repo.pairErr = errors.New("pair-store-down")
	svc := newPGNavClaimedService(repo, &fakeResolver{dsn: mustDSN(t)},
		func(context.Context, *pgx.ConnConfig, string, model.RelatedRecordNavigationSource, time.Duration) (*pgNavigationBinding, error) {
			return nil, fmt.Errorf("prep: %w: %s", ErrPGVersionUnsupported, navPrepMarker)
		})
	out, err := svc.NavigatePGRelatedRecordsClaimed(context.Background(), userExecutionIdentity(7), 9002, validPGNavRequest())
	if !errors.Is(err, errPersistAttempt) {
		t.Fatalf("err = %v, want errPersistAttempt", err)
	}
	if out != (PGClaimedNavigationOutcome{}) {
		t.Fatalf("outcome = %+v, want zero", out)
	}
	if len(repo.claimInputs) != 0 || len(repo.finalizeCalls) != 0 {
		t.Fatal("failed persistence must never claim or finalize")
	}
	// Exactly one NULL-key evidence write was attempted — the pair holds the
	// fixed record only, never the wrapped prep detail.
	if len(repo.pairCalls) != 1 {
		t.Fatalf("pairCalls = %d, want exactly one attempted NULL-key pair", len(repo.pairCalls))
	}
	rec := repo.pairCalls[0].rec
	if rec.ClientExecutionID != nil {
		t.Fatalf("record key = %v, want NULL", *rec.ClientExecutionID)
	}
	for _, field := range []string{rec.ErrorCode, rec.ErrorMessage, rec.StatementPreview} {
		if strings.Contains(field, navPrepMarker) {
			t.Fatalf("marker leaked into persisted record: %+v", rec)
		}
	}
}
