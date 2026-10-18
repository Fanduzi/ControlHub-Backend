//go:build integration

// Package integration — server-side execution-count proofs for PG-MULTI-ENTRY.
// input: isolated PG16 container with pg_stat_statements preloaded (test-only
// fixture option), real MySQL production dependencies, lead-authored
// measurement SQL
// output: TestPGMultiEntry_ServerExecutionCount — actual completed user
// result statements counted on the server (the __chub_w transport marker
// excludes every fixed metadata/touch probe); duplicate, capability,
// cross-domain, ordinary, schema-changed, and alternate-schema-source
// retries send nothing; a fresh capable key sends once more
// pos: stage 4 packet item 2 — send-side counting complements the send-side
// UserSQLSent fact; persisted history/audit counts are never used as send
// proof, and completed-call statistics never count aborted statements
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"

	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// Lead-authored measurement statements — verbatim from
// lead-user-execution-count.sql / lead-user-execution-detail.sql. They count
// COMPLETED result queries carrying the injected __chub_w witness marker
// (present only in rewritten user statements, never in fixed probes),
// filtered to the restricted role's own executions on the fixture database.
const leadUserExecutionCountSQL = `SELECT COALESCE(SUM(s.calls), 0)::bigint
FROM public.pg_stat_statements s
WHERE s.userid = (SELECT r.oid FROM pg_catalog.pg_roles r WHERE r.rolname = $1)
  AND s.dbid = (SELECT d.oid FROM pg_catalog.pg_database d WHERE d.datname = $2)
  AND pg_catalog.strpos(s.query, '__chub_w') > 0`

const leadUserExecutionDetailSQL = `SELECT s.queryid::text, s.calls, s.query
FROM public.pg_stat_statements s
WHERE s.userid = (SELECT r.oid FROM pg_catalog.pg_roles r WHERE r.rolname = $1)
  AND s.dbid = (SELECT d.oid FROM pg_catalog.pg_database d WHERE d.datname = $2)
  AND pg_catalog.strpos(s.query, '__chub_w') > 0
ORDER BY s.queryid`

// newPGStatsFixture starts an isolated PG16 with pg_stat_statements
// preloaded — the only fixture variation; every other fixture keeps its
// default command line. The extension is created inside discdb by admin.
func newPGStatsFixture(t *testing.T) pgDiscFixture {
	t.Helper()
	fix := newPGDiscFixture(t,
		testcontainers.WithCmd("postgres", "-c", "shared_preload_libraries=pg_stat_statements"))
	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	execPG(t, admin, `CREATE EXTENSION pg_stat_statements`)
	return fix
}

// pgSendCount runs the lead count statement as admin — the measurement is
// instrumentation, never a decision input; parameters stay bound.
func pgSendCount(t *testing.T, admin *pgx.Conn) int64 {
	t.Helper()
	var sum int64
	if err := admin.QueryRow(context.Background(), leadUserExecutionCountSQL,
		pgDiscRole, "discdb").Scan(&sum); err != nil {
		t.Fatalf("execution count: %s", redactPGMeta(err.Error()))
	}
	return sum
}

// pgSendDetail logs the representative completed-statement rows (normalized
// query text only — bound values are never stored by pg_stat_statements) for
// the lead's inspection of exactly what the server ran.
func pgSendDetail(t *testing.T, admin *pgx.Conn) {
	t.Helper()
	rows, err := admin.Query(context.Background(), leadUserExecutionDetailSQL, pgDiscRole, "discdb")
	if err != nil {
		t.Fatalf("execution detail: %s", redactPGMeta(err.Error()))
	}
	defer rows.Close()
	for rows.Next() {
		var queryid string
		var calls int64
		var query string
		if err := rows.Scan(&queryid, &calls, &query); err != nil {
			t.Fatalf("execution detail scan: %s", redactPGMeta(err.Error()))
		}
		t.Logf("pg_stat_statements row queryid=%s calls=%d query=%s", queryid, calls, query)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("execution detail rows: %s", redactPGMeta(err.Error()))
	}
}

// resetPGStats clears the stats view on a fresh fixture so the measured sum
// is attributable to this test alone — exact reset statement.
func resetPGStats(t *testing.T, admin *pgx.Conn) {
	t.Helper()
	if _, err := admin.Exec(context.Background(),
		`SELECT public.pg_stat_statements_reset()`); err != nil {
		t.Fatalf("stats reset: %s", redactPGMeta(err.Error()))
	}
}

// Six concurrent calls on one key for both new entries: exactly one attempt
// executes (server-side count 1, history/audit/link 1 each), five land the
// already-exists duplicate; post-terminal same-key retries — including a
// capability-changing one — still send nothing; a same-key different-digest
// call and a cross-entry-domain key collision stay conflict-only; a fresh
// capable key is the only second send.
func TestPGMultiEntry_ServerExecutionCount(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGStatsFixture(t)
	setupPGNavFixtureData(t, fix)
	setupPGClaimEnv(t, fix)
	svc := newPGSavedClaimedService(db)
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	ctx := context.Background()

	targetID := setupPGClaimTarget(t, db, fix, "pgmulti-count-"+strings.ReplaceAll(t.Name(), "/", "-"))
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetID, "discdb", "app", "orders", "note", model.ResultDisclosureRawCopyAllowed)

	stmt := createSavedPG(t, db, pgSavedOwner, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "count by id",
		Statement: `SELECT id, note FROM app.orders WHERE id = :i::bigint`,
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "i", Type: model.QuerySavedStatementParameterInteger},
		},
	})

	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	resetPGStats(t, admin)

	runSix := func(call func() error) (successes, duplicates int) {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				err := call()
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil:
					successes++
				case errors.Is(err, service.ErrQueryExecutionAlreadyExists):
					duplicates++
				default:
					t.Errorf("concurrent call error = %v", err)
				}
			}()
		}
		close(start)
		wg.Wait()
		return successes, duplicates
	}

	// --- saved entry ---
	sCall := func() error {
		_, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
			savedRequest("pgmulti-count-s", map[string]json.RawMessage{"i": json.RawMessage(`1`)}))
		return err
	}
	s, d := runSix(sCall)
	if s != 1 || d != 5 {
		t.Fatalf("saved six-samekey = %d success/%d duplicates, want 1/5", s, d)
	}
	if got := pgSendCount(t, admin); got != 1 {
		t.Fatalf("saved sends = %d, want 1", got)
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "pgmulti-count-s"); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("saved history/audit/link = %d/%d/%d, want 1/1/1", execs, audits, linked)
	}

	// Post-terminal same-key retry — a capability-changing one included —
	// returns already-exists and sends nothing.
	req := savedRequest("pgmulti-count-s", map[string]json.RawMessage{"i": json.RawMessage(`1`)})
	req.Capabilities = []string{service.CapabilityCellTruncated}
	if _, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID, req); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("cap retry = %v, want already-exists", err)
	}
	if got := pgSendCount(t, admin); got != 1 {
		t.Fatalf("saved sends after cap retry = %d, want 1", got)
	}
	// Same key, different digest (other value) → conflict, still nothing.
	if _, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
		savedRequest("pgmulti-count-s", map[string]json.RawMessage{"i": json.RawMessage(`2`)})); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("same-key different values = %v, want conflict", err)
	}
	if got := pgSendCount(t, admin); got != 1 {
		t.Fatalf("saved sends after conflict = %d, want 1", got)
	}
	// Cross-domain key collision: the navigation entry on the saved key's
	// digest family sees a conflict, no execution.
	if _, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-count-s", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"})); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("cross-domain key = %v, want conflict", err)
	}
	// The ordinary entry on the same key is a different digest family —
	// conflict, still nothing sent.
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("pgmulti-count-s", `SELECT id, note FROM app.orders WHERE id = 1`)); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("ordinary cross-domain key = %v, want conflict", err)
	}
	if got := pgSendCount(t, admin); got != 1 {
		t.Fatalf("saved sends after cross-domain conflicts = %d, want 1", got)
	}
	// Same key after a real schema change on the stored statement —
	// resolved schema is part of the digest, so this is a conflict even
	// with identical typed values. Then the original statement snapshot is
	// restored so the fresh key below is an independent second send.
	savedRepo := mysqlrepo.NewQuerySavedStatementRepository(db)
	if err := savedRepo.UpdateWithAudit(ctx, pgSavedOwner, targetID, stmt.ID, model.QuerySavedStatementUpdateRequest{
		Name:      "count by id",
		Statement: `SELECT id, note FROM app.orders WHERE id = :i::bigint`,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "i", Type: model.QuerySavedStatementParameterInteger},
		},
		Database: "discdb",
		Schema:   "analytics",
	}, false); err != nil {
		t.Fatalf("schema update: %v", err)
	}
	if _, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
		savedRequest("pgmulti-count-s", map[string]json.RawMessage{"i": json.RawMessage(`1`)})); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("same-key after schema change = %v, want conflict", err)
	}
	if got := pgSendCount(t, admin); got != 1 {
		t.Fatalf("saved sends after schema-change conflict = %d, want 1", got)
	}
	if err := savedRepo.UpdateWithAudit(ctx, pgSavedOwner, targetID, stmt.ID, model.QuerySavedStatementUpdateRequest{
		Name:      "count by id",
		Statement: `SELECT id, note FROM app.orders WHERE id = :i::bigint`,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "i", Type: model.QuerySavedStatementParameterInteger},
		},
		Database: "discdb",
		Schema:   "app",
	}, false); err != nil {
		t.Fatalf("schema restore: %v", err)
	}
	if rest, err := savedRepo.GetByID(ctx, targetID, stmt.ID); err != nil || rest.SchemaName != "app" ||
		len(rest.Parameters) != 1 || rest.Parameters[0].Name != "i" {
		t.Fatalf("restored snapshot = %+v/%v, want original schema+parameters", rest, err)
	}

	// Fresh capable key → the only second send.
	fresh := savedRequest("pgmulti-count-s2", map[string]json.RawMessage{"i": json.RawMessage(`1`)})
	fresh.Capabilities = []string{service.CapabilityCellTruncated}
	out, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID, fresh)
	if err != nil {
		t.Fatalf("fresh-key execute: %v", err)
	}
	if out.ExecutionID == 0 || out.Evidence == nil || !out.Evidence.UserSQLSent {
		t.Fatalf("fresh-key outcome = %+v, want real execution id + sent sql", out)
	}
	if got := pgSendCount(t, admin); got != 2 {
		t.Fatalf("saved sends after fresh key = %d, want 2", got)
	}
	if execs, _, linked := claimExecCounts(t, db, targetID, "pgmulti-count-s2"); execs != 1 || linked != 1 {
		t.Fatalf("saved fresh-key history/link = %d/%d, want 1/1", execs, linked)
	}
	// Representative completed-statement rows for the saved family before
	// the navigation section resets the stats view.
	pgSendDetail(t, admin)

	// --- navigation entry ---
	resetPGStats(t, admin)
	navPolicy(t, disc, targetID, "discdb", "app", "orders", "qty")
	// A same-named FK on a real cross-schema source: analytics.order_items
	// also carries fk_orders → app.orders, so a same-key request pinned to
	// the analytics schema is a genuine different-digest conflict — not a
	// not-found — while sending nothing.
	execPG(t, admin, `CREATE TABLE analytics.order_items (id bigint PRIMARY KEY, order_id bigint,
	    CONSTRAINT fk_orders FOREIGN KEY (order_id) REFERENCES app.orders(id))`)
	execPG(t, admin, `INSERT INTO analytics.order_items VALUES (1, 2)`)
	execPG(t, admin, `GRANT SELECT ON analytics.order_items TO `+pgDiscRole)
	nCall := func() error {
		_, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
			navRequest("pgmulti-count-n", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"}))
		return err
	}
	s, d = runSix(nCall)
	if s != 1 || d != 5 {
		t.Fatalf("nav six-samekey = %d success/%d duplicates, want 1/5", s, d)
	}
	if got := pgSendCount(t, admin); got != 1 {
		t.Fatalf("nav sends = %d, want 1", got)
	}
	if execs, _, linked := claimExecCounts(t, db, targetID, "pgmulti-count-n"); execs != 1 || linked != 1 {
		t.Fatalf("nav history/link = %d/%d, want 1/1", execs, linked)
	}
	if got := navAuditEvents(t, db, targetID); got != 1 {
		t.Fatalf("nav audit events = %d, want 1", got)
	}
	navReq := navRequest("pgmulti-count-n", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"})
	navReq.Capabilities = []string{service.CapabilityCellTruncated}
	if _, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID, navReq); !errors.Is(err, service.ErrQueryExecutionAlreadyExists) {
		t.Fatalf("nav cap retry = %v, want already-exists", err)
	}
	if _, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-count-n", navSource("discdb", "app", "order_items", "fk_orders"), []string{"2"})); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("nav same-key different values = %v, want conflict", err)
	}
	if _, err := svc.ExecutePGSavedStatementClaimed(ctx, userIdentity(pgSavedOwner), targetID, stmt.ID,
		savedRequest("pgmulti-count-n", map[string]json.RawMessage{"i": json.RawMessage(`1`)})); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("saved cross-domain on nav key = %v, want conflict", err)
	}
	// The ordinary entry on the nav key is likewise conflict-only.
	if _, err := svc.ExecutePGClaimed(ctx, userIdentity(claimUserActor), targetID,
		claimedRequest("pgmulti-count-n", `SELECT id, note FROM app.orders WHERE id = 1`)); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("ordinary cross-domain on nav key = %v, want conflict", err)
	}
	// Same key, alternate source schema — a real prepared binding under a
	// different digest: conflict, no send.
	if _, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID,
		navRequest("pgmulti-count-n", navSource("discdb", "analytics", "order_items", "fk_orders"), []string{"1"})); !errors.Is(err, service.ErrQueryExecutionConflict) {
		t.Fatalf("nav same-key alternate schema = %v, want conflict", err)
	}
	if got := pgSendCount(t, admin); got != 1 {
		t.Fatalf("nav sends after conflicts = %d, want 1", got)
	}
	// The fresh navigation key is genuinely capable: the send-side fact
	// (UserSQLSent) and the server count are asserted independently of the
	// persisted rows, which are counted separately.
	navFresh := navRequest("pgmulti-count-n2", navSource("discdb", "app", "order_items", "fk_orders"), []string{"1"})
	navFresh.Capabilities = []string{service.CapabilityCellTruncated}
	navOut, err := svc.NavigatePGRelatedRecordsClaimed(ctx, userIdentity(claimUserActor), targetID, navFresh)
	if err != nil {
		t.Fatalf("nav fresh-key: %v", err)
	}
	if navOut.Evidence == nil || !navOut.Evidence.UserSQLSent || navOut.ExecutionID == 0 {
		t.Fatalf("nav fresh-key outcome = %+v, want real execution id + sent sql", navOut)
	}
	if got := pgSendCount(t, admin); got != 2 {
		t.Fatalf("nav sends after fresh key = %d, want 2", got)
	}
	if execs, _, linked := claimExecCounts(t, db, targetID, "pgmulti-count-n2"); execs != 1 || linked != 1 {
		t.Fatalf("nav fresh-key history/link = %d/%d, want 1/1", execs, linked)
	}
	pgSendDetail(t, admin)
}
