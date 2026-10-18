//go:build integration

// Package integration — real MySQL trigger failures at the keyed finalize
// stages for both new entries.
// input: disposable PG16 stats fixture (the send count proves the retried
// key never re-executes), real MySQL production repositories — the failure
// is a scoped BEFORE-trigger SIGNAL, not a repository wrapper
// output: TestPGMultiEntryTrigger_* — a scoped audit-insert or claim-update
// trigger makes the keyed finalize fail atomically: backend error, zero
// Result/ExecutionID, claim occupied but unlinked, no partial rows, and the
// same key stays a non-executing duplicate
// pos: stage 4 packet item 4 — real persistence-failure paths through the
// production entries; distinct from the wrapper-injection fault tests
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// Scoped trigger bodies — they fire only on this test's own target/key so
// unrelated fixture rows (saved-statement audits, other claims) are never
// disturbed.
func installScopedTrigger(t *testing.T, db *sql.DB, name, ddl string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), ddl); err != nil {
		t.Fatalf("install trigger %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP TRIGGER IF EXISTS `+name)
	})
}

func auditFailDDL(name string, targetID uint64, event string) string {
	return fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON audit_events FOR EACH ROW
		BEGIN
			IF NEW.target_resource_id = %d AND NEW.event_type = '%s' THEN
				SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'inject audit failure';
			END IF;
		END`, name, targetID, event)
}

func linkFailDDL(name string, targetID uint64, key string) string {
	return fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE ON query_execution_claims FOR EACH ROW
		BEGIN
			IF NEW.target_resource_id = %d AND NEW.client_execution_id = '%s' THEN
				SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'inject link failure';
			END IF;
		END`, name, targetID, key)
}

// assertTriggerFailureOutcome is the shared contract for a real finalize
// failure: controlled backend error, empty delivery, claim occupied but
// unlinked, zero partial evidence rows, and a same-key retry that never
// reaches the server again. wantSends is the expected CUMULATIVE server
// count at this point — each subtest's failed attempt still sent its
// result statement once before the finalize failed.
func assertTriggerFailureOutcome(t *testing.T, db *sql.DB, admin *pgx.Conn, targetID uint64, key string,
	wantSends int64, execID uint64, haveResult bool, execErr error, retry func() error) {
	t.Helper()
	if !errors.Is(execErr, service.ErrQueryBackendFailure) {
		t.Fatalf("finalize trigger failure = %v, want backend failure", execErr)
	}
	if haveResult || execID != 0 {
		t.Fatalf("outcome Result/ExecutionID = %v/%d, want none", haveResult, execID)
	}
	if !claimOccupied(t, db, targetID, key) {
		t.Fatal("claim must remain occupied after the finalize failure")
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, key); execs != 0 || audits != 0 || linked != 0 {
		t.Fatalf("history/audit/link = %d/%d/%d, want 0/0/0 — no partial rows", execs, audits, linked)
	}
	if got := pgSendCount(t, admin); got != wantSends {
		t.Fatalf("server sends = %d, want %d", got, wantSends)
	}
	if err := retry(); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("same-key retry after finalize failure = %v, want already-exists", err)
	}
	if got := pgSendCount(t, admin); got != wantSends {
		t.Fatalf("server sends after retry = %d, want %d — duplicate re-executed", got, wantSends)
	}
}

// Saved entry under a real scoped audit-insert trigger: the attempt's user
// statement already ran on the server (count 1), the finalize transaction
// fails atomically, and the occupied key never executes again.
func TestPGMultiEntryTrigger_Saved(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGStatsFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-trig-s-"+strings.ReplaceAll(t.Name(), "/", "-"))
	// The saved statement must bind a real relation — only rewritten user
	// statements over bound refs carry the __chub_w marker the count query
	// measures; a tableless SELECT is invisible to it.
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	for _, col := range []string{"id", "qty", "note"} {
		insertPolicy(t, disc, targetID, "discdb", "app", "orders", col, model.ResultDisclosureRawCopyAllowed)
	}
	// Saved fixture is created before any trigger is installed.
	stmt := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "long no-cap",
		Statement: `SELECT o.id, repeat('z', 9000) AS v FROM app.orders o WHERE o.id = 1`,
		Scope:     model.QuerySavedStatementPersonal,
	})
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	resetPGStats(t, admin)

	cases := []struct {
		name string
		key  string
		trig string
		ddl  string
	}{
		{"audit insert", "pgmulti-trig-sa", "fail_pgmulti_audit_s",
			auditFailDDL("fail_pgmulti_audit_s", targetID, "query.executed")},
		{"claim link", "pgmulti-trig-sl", "fail_pgmulti_link_s",
			linkFailDDL("fail_pgmulti_link_s", targetID, "pgmulti-trig-sl")},
	}
	var sends int64
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installScopedTrigger(t, db, tc.trig, tc.ddl)
			out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
				savedRequest(tc.key, nil))
			sends++ // the attempt's result statement ran once before finalize failed
			assertTriggerFailureOutcome(t, db, admin, targetID, tc.key, sends,
				out.ExecutionID, out.Result != nil, err, func() error {
					_, rerr := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
						savedRequest(tc.key, nil))
					return rerr
				})
		})
	}
	pgSendDetail(t, admin)
}

// Navigation entry under the same real triggers — over a long no-cap
// result the finalize failure wins over the delivery gate identically.
func TestPGMultiEntryTrigger_Navigation(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGStatsFixture(t)
	setupPGClaimEnv(t, fix)
	svc := newPGClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-trig-n-"+strings.ReplaceAll(t.Name(), "/", "-"))
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	for _, stmt := range []string{
		`CREATE TABLE app.trig_target (id bigint PRIMARY KEY, note text)`,
		`INSERT INTO app.trig_target VALUES (1, repeat('z', 9000))`,
		`CREATE TABLE app.trig_src (id bigint PRIMARY KEY, tid bigint,
		    CONSTRAINT fk_trig FOREIGN KEY (tid) REFERENCES app.trig_target(id))`,
		`GRANT SELECT ON app.trig_target TO ` + pgDiscRole,
		`GRANT SELECT ON app.trig_src TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
	navPolicy(t, disc, targetID, "discdb", "app", "trig_target", "id", "note")
	resetPGStats(t, admin)

	src := navSource("discdb", "app", "trig_src", "fk_trig")
	cases := []struct {
		name string
		key  string
		trig string
		ddl  string
	}{
		{"audit insert", "pgmulti-trig-na", "fail_pgmulti_audit_n",
			auditFailDDL("fail_pgmulti_audit_n", targetID, "related_record_navigation")},
		{"claim link", "pgmulti-trig-nl", "fail_pgmulti_link_n",
			linkFailDDL("fail_pgmulti_link_n", targetID, "pgmulti-trig-nl")},
	}
	var sends int64
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installScopedTrigger(t, db, tc.trig, tc.ddl)
			out, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
				navRequest(tc.key, src, []string{"1"}))
			sends++ // the attempt's result statement ran once before finalize failed
			assertTriggerFailureOutcome(t, db, admin, targetID, tc.key, sends,
				out.ExecutionID, out.Result != nil, err, func() error {
					_, rerr := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
						navRequest(tc.key, src, []string{"1"}))
					return rerr
				})
		})
	}
	pgSendDetail(t, admin)
}
