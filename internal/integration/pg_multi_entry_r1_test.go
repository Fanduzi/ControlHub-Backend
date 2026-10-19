//go:build integration

// Package integration — PG-MULTI-ENTRY R1-1 saved-entry parameter-origin
// acceptance: with a bound plan in flight, a failure that originates in the
// statement expression or in stored data — not in a parameter's own cast —
// must persist the failed/backend taxonomy through the real keyed chain.
// input: shared MySQL fixture (real saved/claim/execution/disclosure
// repositories), disposable PG16 ro_disc, production
// ExecutePGSavedStatementClaimed end to end
// output: TestPGMultiEntryR1_ParameterOrigins — keyed status/code, returned
// outcome/evidence, nil result, sole claim/finalize for a statement-level
// 22003 overflow, a stored-data 22P02 cast, and the parameter-cast control
// pos: PG-MULTI-ENTRY R1-1 — the provenance seam attributes conversion
// failures to the caller's values only on the parameter's own protocol
// surface; statement/data failures under a bound plan keep the backend
// taxonomy through the whole keyed chain.
// note: if this file changes, update header and module README.md.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// tagParam is the declaration shared by the R1-1 statements: one bound text
// parameter so the parameter-verdict path is armed, while the asserted
// failure originates elsewhere.
func tagParam() []model.QuerySavedStatementParameterDefinition {
	return []model.QuerySavedStatementParameterDefinition{
		{Name: "tag", Type: model.QuerySavedStatementParameterString},
	}
}

func tagValues() map[string]json.RawMessage {
	return map[string]json.RawMessage{"tag": json.RawMessage(`"ok"`)}
}

func TestPGMultiEntryR1_ParameterOrigins(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	repo := mysqlrepo.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-r1-origin-"+strings.ReplaceAll(t.Name(), "/", "-"))

	// Stored invalid-integer text: the cast failure originates in the row,
	// never in $1.
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE TABLE app.raw_inputs (id bigint PRIMARY KEY, payload text)`,
		`GRANT SELECT ON app.raw_inputs TO ` + pgDiscRole,
		`INSERT INTO app.raw_inputs VALUES (1, 'stored-invalid-int-R1')`,
	} {
		execPG(t, admin, stmt)
	}
	admin.Close(ctx)
	insertPolicy(t, disc, targetID, "discdb", "app", "raw_inputs", "payload", model.ResultDisclosureRawCopyAllowed)

	// The persisted keyed row must carry exactly what the returned evidence
	// claims — backend identity and remote state are compared, never
	// re-derived.
	assertStoredEvidence := func(t *testing.T, key string, ev *service.PGAttemptEvidence) {
		t.Helper()
		view, err := repo.GetClaimWithExecution(ctx, targetID, fix.db, key, userIdentity(pgSavedOwner))
		if err != nil {
			t.Fatalf("point read %s: %v", key, err)
		}
		if view.Execution == nil {
			t.Fatalf("%s: no linked execution row", key)
		}
		if ev.BackendPID != 0 && (view.Execution.BackendPID == nil || *view.Execution.BackendPID != int64(ev.BackendPID)) {
			t.Fatalf("%s: stored backend_pid = %v, want evidence pid %d", key, view.Execution.BackendPID, ev.BackendPID)
		}
		if view.Execution.RemoteState != ev.RemoteState {
			t.Fatalf("%s: stored remote_state = %q, want evidence %q", key, view.Execution.RemoteState, ev.RemoteState)
		}
	}

	// A — statement-level bigint overflow (server 22003) under a bound plan.
	t.Run("A statement overflow stays backend failure", func(t *testing.T) {
		stmt := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
			Name:       "r1 overflow",
			Statement:  `SELECT :tag::text AS tag, (9223372036854775807::bigint + 1) AS n`,
			Scope:      model.QuerySavedStatementPersonal,
			Parameters: tagParam(),
		})
		auditsBefore := countWhere(t, db,
			`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type IN ('query.executed','related_record_navigation')`, targetID)
		out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
			savedRequest("pgmulti-r1-a", tagValues()))
		if !errors.Is(err, service.ErrQueryBackendFailure) {
			t.Fatalf("A err = %v, want ErrQueryBackendFailure — statement-level 22003 is not a parameter rejection", err)
		}
		if out.Result != nil {
			t.Fatalf("A result = %+v, want nil on backend failure", out.Result)
		}
		if out.Status != model.QueryExecutionFailed || out.Evidence == nil ||
			out.Evidence.Status != model.QueryExecutionFailed || !out.Evidence.UserSQLSent ||
			out.Evidence.Stage != service.PGStageExecute {
			t.Fatalf("A outcome = %+v, want failed status + execute stage + sent evidence", out)
		}
		status, code := keyedSavedRow(t, db, targetID, "pgmulti-r1-a")
		if status != "failed" || code != "query_backend_error" {
			t.Fatalf("A keyed row = %s/%s, want failed/query_backend_error", status, code)
		}
		if execs, _, linked := claimExecCounts(t, db, targetID, "pgmulti-r1-a"); execs != 1 || linked != 1 {
			t.Fatalf("A history/link = %d/%d, want exactly one finalize", execs, linked)
		}
		assertStoredEvidence(t, "pgmulti-r1-a", out.Evidence)
		if auditsAfter := countWhere(t, db,
			`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type IN ('query.executed','related_record_navigation')`, targetID); auditsAfter-auditsBefore != 1 {
			t.Fatalf("A audit delta = %d, want exactly one audit event", auditsAfter-auditsBefore)
		}
	})

	// B — stored-data cast failure (server 22P02) under a bound plan: the
	// invalid text lives in the table, the parameter itself is valid.
	t.Run("B stored-data cast stays backend failure", func(t *testing.T) {
		stmt := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
			Name:       "r1 stored cast",
			Statement:  `SELECT :tag::text AS tag, r.payload::int FROM app.raw_inputs AS r`,
			Scope:      model.QuerySavedStatementPersonal,
			Parameters: tagParam(),
		})
		auditsBefore := countWhere(t, db,
			`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type IN ('query.executed','related_record_navigation')`, targetID)
		out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
			savedRequest("pgmulti-r1-b", tagValues()))
		if !errors.Is(err, service.ErrQueryBackendFailure) {
			t.Fatalf("B err = %v, want ErrQueryBackendFailure — a data-side 22P02 is not a parameter rejection", err)
		}
		if out.Result != nil {
			t.Fatalf("B result = %+v, want nil on backend failure", out.Result)
		}
		if out.Status != model.QueryExecutionFailed || out.Evidence == nil ||
			out.Evidence.Status != model.QueryExecutionFailed || !out.Evidence.UserSQLSent ||
			out.Evidence.Stage != service.PGStageExecute {
			t.Fatalf("B outcome = %+v, want failed status + execute stage + sent evidence", out)
		}
		status, code := keyedSavedRow(t, db, targetID, "pgmulti-r1-b")
		if status != "failed" || code != "query_backend_error" {
			t.Fatalf("B keyed row = %s/%s, want failed/query_backend_error", status, code)
		}
		if execs, _, linked := claimExecCounts(t, db, targetID, "pgmulti-r1-b"); execs != 1 || linked != 1 {
			t.Fatalf("B history/link = %d/%d, want exactly one finalize", execs, linked)
		}
		assertStoredEvidence(t, "pgmulti-r1-b", out.Evidence)
		if auditsAfter := countWhere(t, db,
			`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type IN ('query.executed','related_record_navigation')`, targetID); auditsAfter-auditsBefore != 1 {
			t.Fatalf("B audit delta = %d, want exactly one audit event", auditsAfter-auditsBefore)
		}
	})

	// C — the parameter's own cast failure stays the controlled rejection:
	// same SQLSTATE family as B but raised by the bound value.
	t.Run("C parameter cast stays controlled rejection", func(t *testing.T) {
		stmt := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
			Name:      "r1 param cast",
			Statement: `SELECT :x::int`,
			Scope:     model.QuerySavedStatementPersonal,
			Parameters: []model.QuerySavedStatementParameterDefinition{
				{Name: "x", Type: model.QuerySavedStatementParameterString},
			},
		})
		auditsBefore := countWhere(t, db,
			`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type IN ('query.executed','related_record_navigation')`, targetID)
		out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
			savedRequest("pgmulti-r1-c", map[string]json.RawMessage{"x": json.RawMessage(`"invalid-param-marker"`)}))
		var re *pgsql.RejectError
		if !errors.As(err, &re) || re.Code != "query_not_allowed" {
			t.Fatalf("C err = %v, want controlled parameter rejection", err)
		}
		if out.Result != nil {
			t.Fatalf("C result = %+v, want nil", out.Result)
		}
		if out.Status != model.QueryExecutionRejected {
			t.Fatalf("C status = %v, want rejected", out.Status)
		}
		// The provenance describe transmitted the original statement —
		// UserSQLSent records transmission, the parameters stage shows the
		// result never dispatched, and no remote state is fabricated.
		if out.Evidence == nil || out.Evidence.Stage != service.PGStageParameters ||
			!out.Evidence.UserSQLSent || out.Evidence.RemoteState != model.QueryExecutionRemoteNone {
			t.Fatalf("C evidence = %+v, want parameters stage + sent + no result remote state", out.Evidence)
		}
		status, code := keyedSavedRow(t, db, targetID, "pgmulti-r1-c")
		if status != "rejected" || code != "query_not_allowed" {
			t.Fatalf("C keyed row = %s/%s, want rejected/query_not_allowed", status, code)
		}
		if out.Evidence.Status != out.Status {
			t.Fatalf("C evidence status = %v, want outcome status %v", out.Evidence.Status, out.Status)
		}
		if execs, _, linked := claimExecCounts(t, db, targetID, "pgmulti-r1-c"); execs != 1 || linked != 1 {
			t.Fatalf("C history/link = %d/%d, want exactly one finalize", execs, linked)
		}
		assertStoredEvidence(t, "pgmulti-r1-c", out.Evidence)
		if auditsAfter := countWhere(t, db,
			`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type IN ('query.executed','related_record_navigation')`, targetID); auditsAfter-auditsBefore != 1 {
			t.Fatalf("C audit delta = %d, want exactly one audit event", auditsAfter-auditsBefore)
		}
	})
}
