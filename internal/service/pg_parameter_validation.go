// Package service — the R1-1 parameter-provenance seam: a bound plan's
// values are validated on their own parameter-only protocol surface so a
// conversion failure's origin is proven, never inferred from SQLSTATE
// coincidence on the result path.
// input: the attempt-owned plan wire values and the rewritten user
// statement, on the live READ ONLY execution transaction under the armed
// remaining budget
// output: validatePGParameterInputs — describes the original statement
// (server-inferred ParamOIDs), then runs ONE fixed zero-row
// parameter-shaped probe carrying only the caller's values; a conversion
// failure there becomes the fixed safe rejection because the parameter is
// its only possible source. Describe failures count as parameter-origin
// only on PrepareError ParseComplete=false with an inference-specific
// SQLSTATE; everything else keeps the ordinary taxonomy.
// pos: PG-MULTI-ENTRY R1-1 — runs after disclosure preflight and before the
// single result ExecParams; no user identifier, type, value, or statement
// body enters the generated probe text, and no result statement executes
// twice.
// note: if this file changes, update header and module README.md
package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/fan/controlhub/internal/pgsql"
)

// pgParameterDescribePrefix marks the unnamed Parse/Describe of the
// original statement so activity-based tests can tell a provenance
// transmission apart from the result ExecParams. It changes no SQL
// semantics and never rides the result dispatch.
const pgParameterDescribePrefix = "/*__chub_parameter_probe__*/"

// pgParamTypeRejectCodes are the parameter-context SQLSTATEs that mean a
// bound text value cannot take the type the input context imposes: the
// data-conversion family (invalid text representation 22P02, numeric out of
// range 22003, bad datetime/interval/escape/character forms 22007/22008/
// 22018/22021), a parameter type the server could not infer (42P18), the
// context-imposed type the value cannot satisfy (42804), and an ambiguous
// parameter (42P08). They are consulted only on the isolated
// parameter-provenance surfaces — the result path never maps them.
var pgParamTypeRejectCodes = map[string]struct{}{
	"22P02": {}, "22003": {}, "22007": {}, "22008": {}, "22018": {}, "22021": {},
	"42P18": {}, "42804": {}, "42P08": {},
}

// buildPGParameterProbeSQL is the fixed parameter-only probe: a zero-row
// SELECT whose only inputs are the bound parameter slots. The generated
// text contains ordinals alone — no user identifiers, types, values, or
// statement body — so a conversion failure inside it can originate only in
// the caller's values.
func buildPGParameterProbeSQL(n int) string {
	var sb strings.Builder
	sb.WriteString("SELECT NULL WHERE FALSE")
	for i := 1; i <= n; i++ {
		sb.WriteString(" AND ($")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(" IS NULL)")
	}
	return sb.String()
}

// validatePGParameterInputs runs the two provenance steps on the same
// execution connection and transaction: an unnamed Parse/Describe of the
// rewritten statement yields the server-inferred parameter OIDs, then one
// fixed zero-row ExecParams binds the owned wire values against them. Any
// protocol error keeps the canonical taxonomy through the classifiers
// below; a count/OID anomaly is internal evidence loss, never a user
// verdict.
func (a *pgExecAttempt) validatePGParameterInputs(rw *pgsql.RewriteResult) error {
	if err := a.budget.arm(a.ctx, a.tx); err != nil {
		return err
	}
	// Parse/Describe transmits the rewritten statement text — the send fact
	// is recorded before the call and cleared only when the driver
	// positively proves nothing crossed the wire.
	a.parameterDescribeSent = true
	sd, err := a.tx.Conn().PgConn().Prepare(a.ctx, "", pgParameterDescribePrefix+rw.SQL, nil)
	if err != nil {
		if pgconn.SafeToRetry(err) {
			a.parameterDescribeSent = false
		}
		return a.classifyPGParameterDescribeError(err)
	}
	if len(sd.ParamOIDs) != len(a.paramValues) {
		return fmt.Errorf("%w: parameter describe returned %d param types for %d bound values",
			pgsql.ErrEvidenceUnavailable, len(sd.ParamOIDs), len(a.paramValues))
	}
	paramOIDs := make([]uint32, len(sd.ParamOIDs))
	copy(paramOIDs, sd.ParamOIDs)
	for _, oid := range paramOIDs {
		if oid == 0 {
			return fmt.Errorf("%w: parameter describe returned an uninferred type",
				pgsql.ErrEvidenceUnavailable)
		}
	}
	if err := a.budget.arm(a.ctx, a.tx); err != nil {
		return err
	}
	rr := a.tx.Conn().PgConn().ExecParams(a.ctx,
		buildPGParameterProbeSQL(len(a.paramValues)), a.paramValues, paramOIDs, nil, []int16{0})
	var probeErr error
	if rr.NextRow() {
		// The probe is generated zero-row SQL — a produced row is machinery
		// corruption, never caller data.
		probeErr = fmt.Errorf("%w: parameter probe returned a row", pgsql.ErrEvidenceUnavailable)
	}
	_, closeErr := rr.Close()
	if probeErr == nil {
		probeErr = closeErr
	}
	if probeErr != nil {
		return a.classifyPGParameterProbeError(probeErr)
	}
	return nil
}

// classifyPGParameterDescribeError attributes a Parse/Describe failure:
// the parameter verdict applies only when the driver's PrepareError proves
// the Parse itself failed (ParseComplete=false) on an inference-specific
// SQLSTATE — an uninferrable (42P18) or ambiguous (42P08) parameter is a
// caller-parameter refusal. Generic conversion and relation failures keep
// the ordinary taxonomy of the main transport path.
func (a *pgExecAttempt) classifyPGParameterDescribeError(err error) error {
	if cerr := a.ctx.Err(); cerr != nil {
		return cerr
	}
	var perr *pgconn.PrepareError
	if errors.As(err, &perr) && !perr.ParseComplete {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && (pgErr.Code == "42P18" || pgErr.Code == "42P08") {
			return &pgsql.RejectError{Code: "query_not_allowed",
				Message: "pgsql: parameter type is not supported by statement context"}
		}
	}
	return classifyPGExecTransportError(err)
}

// classifyPGParameterProbeError attributes a failure of the fixed
// parameter-only probe: the value source is proven — the probe contains no
// user table or expression — so a conversion SQLSTATE is the caller's
// value, answered by the fixed rejection. Attempt context keeps its
// priority; every other failure stays an ordinary backend fault.
func (a *pgExecAttempt) classifyPGParameterProbeError(err error) error {
	if cerr := a.ctx.Err(); cerr != nil {
		return cerr
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if _, typed := pgParamTypeRejectCodes[pgErr.Code]; typed {
			return &pgsql.RejectError{Code: "query_not_allowed",
				Message: "pgsql: parameter type is not supported by statement context"}
		}
	}
	return err
}
