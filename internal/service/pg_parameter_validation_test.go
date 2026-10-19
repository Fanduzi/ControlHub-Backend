// Package service — R1-1 classifier unit proofs for the parameter-provenance
// seam and the main result path.
// input: constructed *pgconn.PgError / PrepareError-shaped inputs (a test
// As-carrier stands in for the driver's private PrepareError inner field —
// labeled construction, not a real Prepare), attempt contexts
// output: TestPGParameterProbeClassify, TestPGParameterDescribeClassify,
// TestPGMainTransportNoParameterMap — probe conversion codes map to the
// fixed rejection, describe maps only Parse-failed inference codes, and the
// main transport path never maps a parameter SQLSTATE regardless of plan
// pos: PG-MULTI-ENTRY R1-1 — the classifiers are the attribution boundary:
// context and 57014 keep priority everywhere, unknown detail stays backend
// note: if this file changes, update header and module README.md
package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/pgsql"
)

// fakePrepareError carries the PrepareError surface the driver's private
// constructor produces — errors.As sees a *pgconn.PrepareError with the
// chosen ParseComplete flag while Unwrap still exposes the inner PgError.
// Test-only construction: the production value comes from pgconn.Prepare.
type fakePrepareError struct {
	parseComplete bool
	inner         error
}

func (e *fakePrepareError) Error() string { return "prepare: " + e.inner.Error() }
func (e *fakePrepareError) Unwrap() error { return e.inner }
func (e *fakePrepareError) As(target any) bool {
	if p, ok := target.(**pgconn.PrepareError); ok {
		*p = &pgconn.PrepareError{ParseComplete: e.parseComplete}
		return true
	}
	return false
}

func wantParamReject(t *testing.T, label string, err error) {
	t.Helper()
	var re *pgsql.RejectError
	if !errors.As(err, &re) || re.Code != "query_not_allowed" ||
		re.Message != "pgsql: parameter type is not supported by statement context" {
		t.Fatalf("%s: err = %v, want the fixed parameter rejection", label, err)
	}
}

func wantNoParamReject(t *testing.T, label string, err error) {
	t.Helper()
	var re *pgsql.RejectError
	if errors.As(err, &re) {
		t.Fatalf("%s: err = %v, parameter rejection must not apply", label, err)
	}
}

func TestPGParameterProbeClassify(t *testing.T) {
	t.Parallel()
	attempt := func(ctx context.Context) *pgExecAttempt {
		return &pgExecAttempt{ctx: ctx}
	}
	live := context.Background()

	for _, code := range []string{"22P02", "22003", "22007", "22008", "22018", "22021", "42P18", "42804", "42P08"} {
		err := attempt(live).classifyPGParameterProbeError(&pgconn.PgError{Code: code})
		wantParamReject(t, "probe "+code, err)
	}
	// Non-parameter server failures on the probe keep the raw error for
	// the ordinary backend taxonomy.
	for _, code := range []string{"22012", "23505", "42P01"} {
		err := attempt(live).classifyPGParameterProbeError(&pgconn.PgError{Code: code})
		wantNoParamReject(t, "probe "+code, err)
	}
	// 57014 is never a parameter verdict — it reaches fail() and becomes
	// the timeout sentinel.
	if err := attempt(live).classifyPGParameterProbeError(&pgconn.PgError{Code: "57014"}); isPGQueryCanceled(err) != true {
		t.Fatalf("probe 57014 = %v, want the raw canceled error for timeout mapping", err)
	}
	if err := attempt(live).fail(attempt(live).classifyPGParameterProbeError(&pgconn.PgError{Code: "57014"})); err != ErrQueryTimeout {
		t.Fatalf("probe 57014 through fail = %v, want ErrQueryTimeout", err)
	}
	// A dead context outranks even a parameter-shaped probe error.
	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	if err := attempt(cctx).classifyPGParameterProbeError(&pgconn.PgError{Code: "22P02"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ctx probe err = %v, want context.Canceled", err)
	}
	dctx, dcancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer dcancel()
	if err := attempt(dctx).fail(attempt(dctx).classifyPGParameterProbeError(&pgconn.PgError{Code: "22P02"})); err != ErrQueryTimeout {
		t.Fatalf("expired ctx probe err = %v, want ErrQueryTimeout", err)
	}
}

func TestPGParameterDescribeClassify(t *testing.T) {
	t.Parallel()
	attempt := func(ctx context.Context) *pgExecAttempt {
		return &pgExecAttempt{ctx: ctx}
	}
	live := context.Background()

	// Parse-failed inference-specific codes are the caller's parameter.
	for _, code := range []string{"42P18", "42P08"} {
		err := attempt(live).classifyPGParameterDescribeError(
			&fakePrepareError{parseComplete: false, inner: &pgconn.PgError{Code: code}})
		wantParamReject(t, "describe "+code, err)
	}
	// The same inference code after ParseComplete is not parameter-origin.
	wantNoParamReject(t, "describe 42P18 parse-complete",
		attempt(live).classifyPGParameterDescribeError(
			&fakePrepareError{parseComplete: true, inner: &pgconn.PgError{Code: "42P18"}}))
	// Generic conversion codes are never the parameter verdict even when
	// the Parse itself failed — they stay ordinary server failures.
	wantNoParamReject(t, "describe 22P02 parse-failed",
		attempt(live).classifyPGParameterDescribeError(
			&fakePrepareError{parseComplete: false, inner: &pgconn.PgError{Code: "22P02"}}))
	// A bare PgError with no PrepareError shape is never parameter-origin.
	wantNoParamReject(t, "bare PgError 42P18",
		attempt(live).classifyPGParameterDescribeError(&pgconn.PgError{Code: "42P18"}))
	// A vanished relation inside the describe keeps the existing
	// controlled relation-access category.
	if err := attempt(live).classifyPGParameterDescribeError(
		&fakePrepareError{parseComplete: false, inner: &pgconn.PgError{Code: "42P01"}}); err == nil {
		t.Fatal("describe 42P01 produced no error")
	} else {
		var re *pgsql.RejectError
		if !errors.As(err, &re) || re.Code != "query_object_not_found" {
			t.Fatalf("describe 42P01 = %v, want the relation-access rejection", err)
		}
	}
	// A dead context outranks even an inference-shaped describe error.
	cctx, ccancel := context.WithCancel(context.Background())
	ccancel()
	if err := attempt(cctx).classifyPGParameterDescribeError(
		&fakePrepareError{parseComplete: false, inner: &pgconn.PgError{Code: "42P18"}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ctx describe err = %v, want context.Canceled", err)
	}
}

// The main result transport must never map a parameter SQLSTATE — with or
// without a bound plan, a 22003/22P02 on the result path is an ordinary
// backend failure because provenance was already proven on the probe.
func TestPGMainTransportNoParameterMap(t *testing.T) {
	t.Parallel()
	plan := &pgParameterPlan{statement: "SELECT $1", values: []string{"x"}, source: pgParameterSourceTemplate}
	for _, code := range []string{"22003", "22P02"} {
		attempt := &pgExecAttempt{ctx: context.Background(), parameters: plan}
		got := attempt.classifyTransportError(&pgconn.PgError{Code: code})
		wantNoParamReject(t, "main "+code+" with plan", got)
		if err := attempt.fail(got); err != errPGExecInternal {
			t.Fatalf("main %s with plan through fail = %v, want errPGExecInternal", code, err)
		}
		// Zero-value plan keeps the same answer — plan presence is irrelevant.
		plain := &pgExecAttempt{ctx: context.Background()}
		wantNoParamReject(t, "main "+code+" no plan", plain.classifyTransportError(&pgconn.PgError{Code: code}))
	}
}
