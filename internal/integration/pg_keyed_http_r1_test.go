//go:build integration

// Package integration — PG-HTTP-T12-R1 acceptance over the real router: connection sentinels (R1-1), limitApplied window (R1-2), HTTP matrix.
// input: shared MySQL fixture + disposable PG16 (nav catalog), the enabled and default-closed assemblies from setupPGHTTPFixture
// output: TestPGKeyedHTTP_AccessSentinels, TestPGKeyedHTTP_LimitApplied, TestPGKeyedHTTP_AccessSentinelPersistFailure, TestPGKeyedHTTP_FinalizeFailure, TestPGKeyedHTTP_ConcurrencyMatrix, TestPGKeyedHTTP_AuthScope, TestPGKeyedHTTP_ResponseContractAndCSV, TestPGKeyedHTTP_FailClosed, TestPGKeyedHTTP_VersionMatrix
// pos: R1 continuous-fix pass — real HTTP only; the closed assembly stays fail-closed and production wiring keeps no WithPGHTTPEntries call.
// note: if this file changes, update this header and module README.md.

package integration

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// ---------------------------------------------------------------------------
// R1-1. Connection sentinels over the real router: a missing, disabled, or
// rebound connection keeps its normalized identity through the shared
// rejection exit — 404 query_connection_not_found / 403
// query_connection_disabled / 403 dsn_binding_mismatch — while the persisted
// pair stays the generic NULL-key query_not_allowed denial contract and the
// key remains free for a post-restore first claim.
// ---------------------------------------------------------------------------

// pgSentinelTarget is one isolation cell: its own target (so credential rows
// and scoped triggers never touch the shared fixture) plus one authorized
// saved statement so the saved entry reaches connection resolution for real.
type pgSentinelTarget struct {
	targetID uint64
	savedID  uint64
	execute  string
	saved    string
	nav      string
}

// setupPGSentinelTarget creates the isolation target and, for each variant,
// the credential row variant: "missing" leaves no row, "disabled" inserts a
// disabled one, "mismatch" binds a DSN pointing at a different database.
// The saved statement is always authorized while access is resolvable — its
// database is discdb so ?database=discdb reaches ResolvePGExecution.
func setupPGSentinelTarget(t *testing.T, fx pgHTTPFixture, variant string) pgSentinelTarget {
	t.Helper()
	targetID := createPGInstance(t, fx.db,
		fmt.Sprintf("pg-r1-sent-%d", time.Now().UnixNano()),
		envStaging, fx.pg.host, fx.pg.port)
	st := pgSentinelTarget{
		targetID: targetID,
		execute:  fmt.Sprintf("/query-targets/%d/execute", targetID),
		nav:      fmt.Sprintf("/query-targets/%d/related-records", targetID),
	}
	ref := fmt.Sprintf("PG_R1_%d", targetID)
	correct := pgKeywordDSN(fx.pg.host, fx.pg.port, fx.pg.db, pgDiscRole, pgStmtPassword, 5)
	switch variant {
	case "missing":
		// No credential row — the (target, database) identity is absent.
	case "disabled":
		insertPGCredential(t, fx.db, targetID, "discdb", "app", ref, false, string(model.QueryEnvPolicyNonProdOnly))
		t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+ref, correct)
	case "mismatch":
		insertPGCredential(t, fx.db, targetID, "discdb", "app", ref, true, string(model.QueryEnvPolicyNonProdOnly))
		t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+ref,
			pgKeywordDSN(fx.pg.host, fx.pg.port, "otherdb", pgDiscRole, pgStmtPassword, 5))
	default:
		t.Fatalf("unknown variant %q", variant)
	}
	// Disclosure policies are per-target — the restored entries deliver real
	// rows, so the isolation target needs the same canonical coverage.
	disc := mysqlrepo.NewQueryDisclosureRepository(fx.db)
	for _, pol := range []struct{ schema, object, column string }{
		{"app", "orders", "id"}, {"app", "orders", "qty"}, {"app", "orders", "note"},
		{"app", "order_items", "id"}, {"app", "order_items", "order_id"},
	} {
		insertPolicy(t, disc, targetID, "discdb", pol.schema, pol.object, pol.column, model.ResultDisclosureRawCopyAllowed)
	}
	saved := createSavedPG(t, fx.db, fx.userID, targetID, model.QuerySavedStatementCreateRequest{
		Name: "sentinel-stmt", Statement: `SELECT id FROM app.orders ORDER BY id`,
		Scope: model.QuerySavedStatementPersonal,
	})
	st.savedID = saved.ID
	st.saved = fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute?database=discdb", targetID, saved.ID)
	return st
}

// restorePGSentinelTarget makes the variant's access resolvable again — the
// same credential row inserted/enabled, or the DSN rebound to the real
// endpoint — so the previously refused key may claim on first retry.
func restorePGSentinelTarget(t *testing.T, fx pgHTTPFixture, st pgSentinelTarget, variant string) {
	t.Helper()
	ref := fmt.Sprintf("PG_R1_%d", st.targetID)
	correct := pgKeywordDSN(fx.pg.host, fx.pg.port, fx.pg.db, pgDiscRole, pgStmtPassword, 5)
	switch variant {
	case "missing":
		insertPGCredential(t, fx.db, st.targetID, "discdb", "app", ref, true, string(model.QueryEnvPolicyNonProdOnly))
		t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+ref, correct)
	case "disabled":
		if _, err := fx.db.ExecContext(context.Background(),
			`UPDATE query_target_credentials SET enabled = 1 WHERE resource_id = ? AND database_name = 'discdb'`,
			st.targetID); err != nil {
			t.Fatalf("enable credential: %v", err)
		}
	case "mismatch":
		t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+ref, correct)
	}
}

// sentinelEntry is one of the three routed entries driven through the real
// router+auth chain on the isolation target.
type sentinelEntry struct {
	name string
	call func(t *testing.T, f pgHTTPFixture, st pgSentinelTarget, key string) (int, string)
}

func pgSentinelEntries() []sentinelEntry {
	navBody := func(key string) string {
		return fmt.Sprintf(`{"source":{"database":"discdb","schema":"app","object":"order_items","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":%q}`, key)
	}
	return []sentinelEntry{
		{"execute", func(t *testing.T, f pgHTTPFixture, st pgSentinelTarget, key string) (int, string) {
			rr := doBearerWithBody(t, f.enabled, http.MethodPost, st.execute, f.token,
				fmt.Sprintf(`{"statement":"SELECT id FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":%q}`, key))
			code, _ := decodeError(t, rr)
			return rr.Code, code
		}},
		{"saved", func(t *testing.T, f pgHTTPFixture, st pgSentinelTarget, key string) (int, string) {
			rr := doBearerWithBody(t, f.enabled, http.MethodPost, st.saved, f.token,
				fmt.Sprintf(`{"clientExecutionId":%q}`, key))
			code, _ := decodeError(t, rr)
			return rr.Code, code
		}},
		{"navigation", func(t *testing.T, f pgHTTPFixture, st pgSentinelTarget, key string) (int, string) {
			rr := doBearerWithBody(t, f.enabled, http.MethodPost, st.nav, f.token, navBody(key))
			code, _ := decodeError(t, rr)
			return rr.Code, code
		}},
	}
}

func TestPGKeyedHTTP_AccessSentinels(t *testing.T) {
	fx := setupPGHTTPFixture(t)

	variants := []struct {
		name       string
		wantStatus int
		wantCode   string
	}{
		{"missing", http.StatusNotFound, "query_connection_not_found"},
		{"disabled", http.StatusForbidden, "query_connection_disabled"},
		{"mismatch", http.StatusForbidden, "dsn_binding_mismatch"},
	}
	entries := pgSentinelEntries()

	for _, v := range variants {
		st := setupPGSentinelTarget(t, fx, v.name)
		rejectedBefore := countWhere(t, fx.db,
			`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NULL`,
			st.targetID)
		for _, e := range entries {
			key := "r1-sent-" + v.name + "-" + e.name
			gotStatus, gotCode := e.call(t, fx, st, key)
			if gotStatus != v.wantStatus || gotCode != v.wantCode {
				t.Fatalf("%s/%s: got %d %q, want %d %q", v.name, e.name, gotStatus, gotCode, v.wantStatus, v.wantCode)
			}
			if claimOccupied(t, fx.db, st.targetID, key) {
				t.Fatalf("%s/%s: access refusal occupied the key", v.name, e.name)
			}
		}
		// Exactly one NULL-key rejected pair per entry — never a compensating
		// second row, and the persisted code is the generic denial contract.
		rejectedAfter := countWhere(t, fx.db,
			`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NULL
				AND status = 'rejected' AND error_code = 'query_not_allowed'`,
			st.targetID)
		if rejectedAfter-rejectedBefore != len(entries) {
			t.Fatalf("%s: NULL-key rejected pairs = %d, want %d (one per entry)",
				v.name, rejectedAfter-rejectedBefore, len(entries))
		}
		// Restore access — every refused key claims on its first retry.
		restorePGSentinelTarget(t, fx, st, v.name)
		for _, e := range entries {
			key := "r1-sent-" + v.name + "-" + e.name
			gotStatus, _ := e.call(t, fx, st, key)
			if gotStatus != http.StatusOK {
				t.Fatalf("%s/%s after restore: got %d, want 200 — refused key must claim fresh", v.name, e.name, gotStatus)
			}
			if !claimOccupied(t, fx.db, st.targetID, key) {
				t.Fatalf("%s/%s after restore: key never claimed", v.name, e.name)
			}
		}
	}
}

// A persistence failure while writing the access-refusal pair outranks the
// connection verdict itself: the transport publishes 502 backend, never the
// connection code, and no partial evidence row survives.
func TestPGKeyedHTTP_AccessSentinelPersistFailure(t *testing.T) {
	fx := setupPGHTTPFixture(t)
	st := setupPGSentinelTarget(t, fx, "missing")

	for _, tc := range []struct{ entry, trig, event string }{
		{"execute", "fail_r1_ref_exec", "query.executed"},
		{"saved", "fail_r1_ref_saved", "query.executed"},
		{"navigation", "fail_r1_ref_nav", "related_record_navigation"},
	} {
		t.Run(tc.entry, func(t *testing.T) {
			installScopedTrigger(t, fx.db, tc.trig, auditFailDDL(tc.trig, st.targetID, tc.event))
			entry := pgSentinelEntries()
			var call func(t *testing.T, f pgHTTPFixture, st pgSentinelTarget, key string) (int, string)
			for _, e := range entry {
				if e.name == tc.entry {
					call = e.call
				}
			}
			gotStatus, gotCode := call(t, fx, st, "r1-sent-pf-"+tc.entry)
			if gotStatus != http.StatusBadGateway || gotCode != "query_backend_error" {
				t.Fatalf("persist failure got %d %q, want 502 query_backend_error", gotStatus, gotCode)
			}
			if n := countWhere(t, fx.db,
				`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, st.targetID); n != 0 {
				t.Fatalf("failed rejection pair left %d partial rows", n)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// R1-2. limitApplied is the executed-window fact (the LIMIT the rewritten
// statement actually carried — PaginatePG's FetchCount, sentinel-inclusive),
// never the row budget and never a len(rows) guess.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_LimitApplied(t *testing.T) {
	fx := setupPGHTTPFixture(t)

	// A wide-enough real table so every window case sees genuine pages.
	admin := connectPG(t, fx.pg.host, fx.pg.port, fx.pg.db, pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE TABLE app.many_rows (id bigint PRIMARY KEY, note text)`,
		`INSERT INTO app.many_rows SELECT g, 'r' || g::text FROM generate_series(1, 150) g`,
		`GRANT SELECT ON app.many_rows TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
	_ = admin.Close(context.Background())
	disc := mysqlrepo.NewQueryDisclosureRepository(fx.db)
	insertPolicy(t, disc, fx.targetID, "discdb", "app", "many_rows", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, fx.targetID, "discdb", "app", "many_rows", "note", model.ResultDisclosureRawCopyAllowed)

	type decResp struct {
		Status        string   `json:"status"`
		RowCount      int      `json:"rowCount"`
		Truncated     bool     `json:"truncated"`
		LimitApplied  int      `json:"limitApplied"`
		Rows          [][]any  `json:"rows"`
		CellTruncated [][]bool `json:"cellTruncated"`
		Pagination    *struct {
			Page            int  `json:"page"`
			PageSize        int  `json:"pageSize"`
			HasPreviousPage bool `json:"hasPreviousPage"`
			HasNextPage     bool `json:"hasNextPage"`
		} `json:"pagination"`
	}
	post := func(key, stmt string, page, pageSize int) decResp {
		t.Helper()
		body := fmt.Sprintf(`{"statement":%q,"database":"discdb","clientExecutionId":%q,"pagination":{"page":%d,"pageSize":%d}}`,
			stmt, key, page, pageSize)
		rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token, body)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status = %d; body=%s", key, rr.Code, rr.Body.String())
		}
		var r decResp
		if err := json.Unmarshal(rr.Body.Bytes(), &r); err != nil {
			t.Fatalf("%s: decode: %v", key, err)
		}
		return r
	}
	check := func(key string, r decResp, wantLimit, wantRows int, wantPrev, wantNext, wantTrunc bool) {
		t.Helper()
		if r.LimitApplied != wantLimit {
			t.Fatalf("%s: limitApplied = %d, want %d (executed window, not budget)", key, r.LimitApplied, wantLimit)
		}
		if r.RowCount != wantRows || len(r.Rows) != wantRows {
			t.Fatalf("%s: rowCount = %d / len(rows) = %d, want %d", key, r.RowCount, len(r.Rows), wantRows)
		}
		if r.Pagination == nil {
			t.Fatalf("%s: missing pagination", key)
		}
		if r.Pagination.HasPreviousPage != wantPrev || r.Pagination.HasNextPage != wantNext {
			t.Fatalf("%s: prev/next = %v/%v, want %v/%v", key, r.Pagination.HasPreviousPage, r.Pagination.HasNextPage, wantPrev, wantNext)
		}
		if r.Truncated != wantTrunc {
			t.Fatalf("%s: truncated = %v, want %v", key, r.Truncated, wantTrunc)
		}
		if len(r.CellTruncated) != wantRows {
			t.Fatalf("%s: cellTruncated rows = %d, want %d", key, len(r.CellTruncated), wantRows)
		}
		for ri, row := range r.CellTruncated {
			if len(row) != len(r.Rows[ri]) {
				t.Fatalf("%s: cellTruncated row %d width %d != row width %d", key, ri, len(row), len(r.Rows[ri]))
			}
		}
	}

	// Budget 100, page 1 @ 25, no user LIMIT → sentinel-inclusive fetch 26.
	check("r1-la-p1", post("r1-la-p1", `SELECT id FROM app.many_rows ORDER BY id`, 1, 25),
		26, 25, false, true, false)
	// User LIMIT 3 clamps the window — the executed LIMIT is 3, not 100.
	check("r1-la-l3", post("r1-la-l3", `SELECT id FROM app.many_rows ORDER BY id LIMIT 3`, 1, 25),
		3, 3, false, false, false)
	// LIMIT 30 OFFSET 10, page 2 @ 25 → remaining browsable 5 → LIMIT 5.
	check("r1-la-l30", post("r1-la-l30", `SELECT id FROM app.many_rows ORDER BY id LIMIT 30 OFFSET 10`, 2, 25),
		5, 5, true, false, false)
	// LIMIT 0 → the executed statement carries LIMIT 0; zero rows.
	check("r1-la-l0", post("r1-la-l0", `SELECT id FROM app.many_rows ORDER BY id LIMIT 0`, 1, 25),
		0, 0, false, false, false)
	// Budget 100, page 4 @ 25 → final window LIMIT 25, no sentinel → the
	// budget boundary reports truncated while the cell matrix stays clean.
	check("r1-la-p4", post("r1-la-p4", `SELECT id FROM app.many_rows ORDER BY id`, 4, 25),
		25, 25, true, false, true)
	// Empty result under a real nonzero window — zero rows must not turn
	// limitApplied into 0.
	check("r1-la-empty", post("r1-la-empty", `SELECT id FROM app.many_rows WHERE id > 99999`, 1, 25),
		26, 0, false, false, false)

	// Saved entry consumes the same fact through the same delivery chain.
	saved := createSavedPG(t, fx.db, fx.userID, fx.targetID, model.QuerySavedStatementCreateRequest{
		Name: "la-many", Statement: `SELECT id FROM app.many_rows ORDER BY id`,
		Scope: model.QuerySavedStatementPersonal,
	})
	rr := doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, saved.ID), fx.token,
		`{"clientExecutionId":"r1-la-saved","pagination":{"page":2,"pageSize":25}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("saved window: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var sresp decResp
	if err := json.Unmarshal(rr.Body.Bytes(), &sresp); err != nil {
		t.Fatalf("saved decode: %v", err)
	}
	check("r1-la-saved", sresp, 26, 25, true, true, false)

	// Navigation consumes the same fact too — the fixed default window
	// (page 1 @ 50, budget 100) executes LIMIT 51.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.nav, fx.token,
		`{"source":{"database":"discdb","schema":"app","object":"order_items","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":"r1-la-nav"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("nav: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var nresp struct {
		RowCount     int `json:"rowCount"`
		LimitApplied int `json:"limitApplied"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &nresp); err != nil {
		t.Fatalf("nav decode: %v", err)
	}
	if nresp.LimitApplied != 51 || nresp.RowCount != 1 {
		t.Fatalf("nav limitApplied/rowCount = %d/%d, want 51/1", nresp.LimitApplied, nresp.RowCount)
	}
}

// ---------------------------------------------------------------------------
// C. HTTP-visible persistence/finalize failure priority: a real scoped
// trigger makes the keyed finalize fail — 502 backend outranks any delivery
// refusal, no rows leak, the claim stays occupied-but-unlinked, no partial
// evidence, and the same key never executes a second result statement.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_FinalizeFailure(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGStatsFixture(t)
	setupPGNavFixtureData(t, fix)

	userID := insertAuthzTestUser(t, db, "pg-r1-ff@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	targetID := createPGInstance(t, db, "pg-r1-ff-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	ref := "PG_R1_FF"
	insertPGCredential(t, db, targetID, "discdb", "app", ref, true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+ref,
		pgKeywordDSN(fix.host, fix.port, fix.db, pgDiscRole, pgStmtPassword, 5))

	savedRepo := mysqlrepo.NewQuerySavedStatementRepository(db)
	targetRepo := mysqlrepo.NewQueryTargetRepository(db)
	guard := service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	execSvc := newPGClaimedService(db).
		WithTemplateExecution(savedRepo, service.NewTemplateStatementCompiler()).
		WithPGHTTPEntries()
	handler := api.NewRouter(api.Dependencies{
		AuthService:                service.NewAuthService(mysqlrepo.NewUserRepository(db), authzIntegrationSecret),
		AuditService:               service.NewAuditService(mysqlrepo.NewAuditRepository(db)),
		QueryExecutionService:      execSvc,
		QuerySavedStatementService: service.NewQuerySavedStatementService(savedRepo, savedRepo, targetRepo, guard),
		MachineCredentialService:   service.NewMachinePrincipalService(mysqlrepo.NewMachinePrincipalRepository(db)),
		QueryExecutionAuth:         api.QueryExecutionAuthConfig{Clock: time.Now},
	})
	token := mustLogin(t, handler, "pg-r1-ff@example.com", "secret123")
	executePath := fmt.Sprintf("/query-targets/%d/execute", targetID)
	savedBase := fmt.Sprintf("/query-targets/%d/saved-statements", targetID)
	navPath := fmt.Sprintf("/query-targets/%d/related-records", targetID)

	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	navPolicy(t, disc, targetID, "discdb", "app", "orders", "id", "qty", "note")
	insertPolicy(t, disc, targetID, "discdb", "app", "order_items", "id", model.ResultDisclosureRawCopyAllowed)

	stmt := createSavedPG(t, db, userID, targetID, model.QuerySavedStatementCreateRequest{
		Name: "ff-long", Statement: `SELECT o.id, repeat('z', 9000) AS v FROM app.orders o WHERE o.id = 1`,
		Scope: model.QuerySavedStatementPersonal,
	})

	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	resetPGStats(t, admin)

	// Control: the truncated-cell page without the capability is a 409
	// refusal whose committed success pair stays — no rows in the body.
	rr := doBearerWithBody(t, handler, http.MethodPost, executePath, token,
		`{"statement":"SELECT o.id, repeat('z', 9000) AS v FROM app.orders o WHERE o.id = 1","database":"discdb","clientExecutionId":"r1-ff-ctrl"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("control refusal: %d; body=%s", rr.Code, rr.Body.String())
	}
	if code, _ := decodeError(t, rr); code != "result_contract_upgrade_required" {
		t.Fatalf("control code = %q", code)
	}
	if strings.Contains(rr.Body.String(), `"rows"`) {
		t.Fatalf("refusal body leaked rows")
	}
	if execs, audits, linked := claimExecCounts(t, db, targetID, "r1-ff-ctrl"); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("control evidence = %d/%d/%d, want 1/1/1", execs, audits, linked)
	}
	// Same key + the capability now declared → still already-exists, no rerun.
	rr = doBearerWithBody(t, handler, http.MethodPost, executePath, token,
		`{"statement":"SELECT o.id, repeat('z', 9000) AS v FROM app.orders o WHERE o.id = 1","database":"discdb","clientExecutionId":"r1-ff-ctrl","capabilities":["cellTruncated"]}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("cap-changing replay: %d, want 409", rr.Code)
	}
	if got := pgSendCount(t, admin); got != 1 {
		t.Fatalf("control sends = %d, want 1", got)
	}
	// A fresh key + capability delivers the same truncated data.
	rr = doBearerWithBody(t, handler, http.MethodPost, executePath, token,
		`{"statement":"SELECT o.id, repeat('z', 9000) AS v FROM app.orders o WHERE o.id = 1","database":"discdb","clientExecutionId":"r1-ff-cap","capabilities":["cellTruncated"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("capable fresh key: %d; body=%s", rr.Code, rr.Body.String())
	}
	if got := pgSendCount(t, admin); got != 2 {
		t.Fatalf("sends after fresh key = %d, want 2", got)
	}

	// Finalize-stage failures via real scoped triggers, per entry: 502
	// outranks everything, claim stays occupied-but-unlinked, zero partial
	// rows, and the same key never sends a second result statement.
	entries := []struct {
		name  string
		event string
		post  func(key string) *httptest.ResponseRecorder
	}{
		{"execute", "query.executed", func(key string) *httptest.ResponseRecorder {
			return doBearerWithBody(t, handler, http.MethodPost, executePath, token,
				fmt.Sprintf(`{"statement":"SELECT id FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":%q}`, key))
		}},
		{"saved", "query.executed", func(key string) *httptest.ResponseRecorder {
			return doBearerWithBody(t, handler, http.MethodPost,
				fmt.Sprintf("%s/%d/execute?database=discdb", savedBase, stmt.ID), token,
				fmt.Sprintf(`{"clientExecutionId":%q}`, key))
		}},
		{"navigation", "related_record_navigation", func(key string) *httptest.ResponseRecorder {
			return doBearerWithBody(t, handler, http.MethodPost, navPath, token,
				fmt.Sprintf(`{"source":{"database":"discdb","schema":"app","object":"order_items","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":%q}`, key))
		}},
	}
	var sends int64 = 2
	for _, e := range entries {
		for _, stage := range []string{"audit", "link"} {
			key := "r1-ff-" + e.name + "-" + stage
			trig := "fail_r1_" + e.name + "_" + stage
			switch stage {
			case "audit":
				installScopedTrigger(t, db, trig, auditFailDDL(trig, targetID, e.event))
			case "link":
				installScopedTrigger(t, db, trig, linkFailDDL(trig, targetID, key))
			}
			// audits are target-scoped — baseline them; exec/link are key-scoped.
			_, auditsBase, _ := claimExecCounts(t, db, targetID, key)
			rr := e.post(key)
			sends++ // the attempt's result statement ran once before finalize failed
			if rr.Code != http.StatusBadGateway {
				t.Fatalf("%s/%s: status = %d, want 502; body=%s", e.name, stage, rr.Code, rr.Body.String())
			}
			code, _ := decodeError(t, rr)
			if code != "query_backend_error" {
				t.Fatalf("%s/%s: code = %q, want query_backend_error", e.name, stage, code)
			}
			if strings.Contains(rr.Body.String(), `"rows"`) {
				t.Fatalf("%s/%s: failure body leaked rows", e.name, stage)
			}
			if !claimOccupied(t, db, targetID, key) {
				t.Fatalf("%s/%s: claim not occupied after finalize failure", e.name, stage)
			}
			if execs, audits, linked := claimExecCounts(t, db, targetID, key); execs != 0 || audits != auditsBase || linked != 0 {
				t.Fatalf("%s/%s: partial evidence %d/%d/%d, want 0/%d/0", e.name, stage, execs, audits, linked, auditsBase)
			}
			if got := pgSendCount(t, admin); got != sends {
				t.Fatalf("%s/%s: server sends = %d, want %d", e.name, stage, got, sends)
			}
			// Same-key retry: occupied claim never grants a second execution.
			rr = e.post(key)
			if rr.Code != http.StatusConflict {
				t.Fatalf("%s/%s retry: status = %d, want 409", e.name, stage, rr.Code)
			}
			if got := pgSendCount(t, admin); got != sends {
				t.Fatalf("%s/%s retry sent again: %d, want %d", e.name, stage, got, sends)
			}
		}
	}
	pgSendDetail(t, admin)
}

// ---------------------------------------------------------------------------
// D. Three-entry same-key concurrency and cross-entry key collision over the
// real router: exactly one result statement executes per key; losers land the
// controlled 409 without evidence or sends.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_ConcurrencyMatrix(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGStatsFixture(t)
	setupPGNavFixtureData(t, fix)

	userID := insertAuthzTestUser(t, db, "pg-r1-d@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })
	targetID := createPGInstance(t, db, "pg-r1-d-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	ref := "PG_R1_D"
	insertPGCredential(t, db, targetID, "discdb", "app", ref, true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+ref,
		pgKeywordDSN(fix.host, fix.port, fix.db, pgDiscRole, pgStmtPassword, 5))

	savedRepo := mysqlrepo.NewQuerySavedStatementRepository(db)
	targetRepo := mysqlrepo.NewQueryTargetRepository(db)
	guard := service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	execSvc := newPGClaimedService(db).
		WithTemplateExecution(savedRepo, service.NewTemplateStatementCompiler()).
		WithPGHTTPEntries()
	handler := api.NewRouter(api.Dependencies{
		AuthService:                service.NewAuthService(mysqlrepo.NewUserRepository(db), authzIntegrationSecret),
		AuditService:               service.NewAuditService(mysqlrepo.NewAuditRepository(db)),
		QueryExecutionService:      execSvc,
		QuerySavedStatementService: service.NewQuerySavedStatementService(savedRepo, savedRepo, targetRepo, guard),
		MachineCredentialService:   service.NewMachinePrincipalService(mysqlrepo.NewMachinePrincipalRepository(db)),
		QueryExecutionAuth:         api.QueryExecutionAuthConfig{Clock: time.Now},
	})
	token := mustLogin(t, handler, "pg-r1-d@example.com", "secret123")
	executePath := fmt.Sprintf("/query-targets/%d/execute", targetID)
	savedBase := fmt.Sprintf("/query-targets/%d/saved-statements", targetID)
	navPath := fmt.Sprintf("/query-targets/%d/related-records", targetID)

	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	for _, col := range []string{"id", "qty", "note"} {
		insertPolicy(t, disc, targetID, "discdb", "app", "orders", col, model.ResultDisclosureRawCopyAllowed)
	}
	insertPolicy(t, disc, targetID, "discdb", "app", "order_items", "id", model.ResultDisclosureRawCopyAllowed)

	stmt := createSavedPG(t, db, userID, targetID, model.QuerySavedStatementCreateRequest{
		Name: "conc-stmt", Statement: `SELECT id FROM app.orders ORDER BY id`,
		Scope: model.QuerySavedStatementPersonal,
	})

	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	defer func() { _ = admin.Close(context.Background()) }()
	resetPGStats(t, admin)

	postExecute := func(key string) *httptest.ResponseRecorder {
		return doBearerWithBody(t, handler, http.MethodPost, executePath, token,
			fmt.Sprintf(`{"statement":"SELECT id FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":%q}`, key))
	}
	postSaved := func(key string) *httptest.ResponseRecorder {
		return doBearerWithBody(t, handler, http.MethodPost,
			fmt.Sprintf("%s/%d/execute?database=discdb", savedBase, stmt.ID), token,
			fmt.Sprintf(`{"clientExecutionId":%q}`, key))
	}
	postNav := func(key string) *httptest.ResponseRecorder {
		return doBearerWithBody(t, handler, http.MethodPost, navPath, token,
			fmt.Sprintf(`{"source":{"database":"discdb","schema":"app","object":"order_items","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":%q}`, key))
	}

	// Six concurrent same-key POSTs per entry: exactly one 200, the rest the
	// controlled 409 family — and exactly one result statement on the server.
	entries := []struct {
		name string
		key  string
		post func(string) *httptest.ResponseRecorder
	}{
		{"execute", "r1-conc-x", postExecute},
		{"saved", "r1-conc-s", postSaved},
		{"navigation", "r1-conc-n", postNav},
	}
	var sends int64
	for _, e := range entries {
		start := make(chan struct{})
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok, conflicts := 0, 0
		for i := 0; i < 6; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				rr := e.post(e.key)
				mu.Lock()
				defer mu.Unlock()
				switch rr.Code {
				case http.StatusOK:
					ok++
				case http.StatusConflict:
					conflicts++
				default:
					t.Errorf("%s concurrent: %d; body=%s", e.name, rr.Code, rr.Body.String())
				}
			}()
		}
		close(start)
		wg.Wait()
		if ok != 1 || conflicts != 5 {
			t.Fatalf("%s: %d OK + %d conflicts, want 1+5", e.name, ok, conflicts)
		}
		sends++
		if got := pgSendCount(t, admin); got != sends {
			t.Fatalf("%s: server sends = %d, want %d", e.name, got, sends)
		}
		if execs, _, linked := claimExecCounts(t, db, targetID, e.key); execs != 1 || linked != 1 {
			t.Fatalf("%s: history/link = %d/%d, want 1/1", e.name, execs, linked)
		}
	}

	// Cross-entry same-key: three entry digests differ — the claim admits
	// one, the other two land client_execution_id_conflict; still one send.
	crossKey := "r1-conc-x-entry"
	start := make(chan struct{})
	var wg sync.WaitGroup
	codes := make([]int, 3)
	bodies := make([]string, 3)
	posts := []func(string) *httptest.ResponseRecorder{postExecute, postSaved, postNav}
	for i, p := range posts {
		wg.Add(1)
		go func(i int, p func(string) *httptest.ResponseRecorder) {
			defer wg.Done()
			<-start
			rr := p(crossKey)
			codes[i], bodies[i] = rr.Code, rr.Body.String()
		}(i, p)
	}
	close(start)
	wg.Wait()
	ok, conflicts := 0, 0
	for i, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("cross-entry %d: %d; body=%s", i, c, bodies[i])
		}
	}
	if ok != 1 || conflicts != 2 {
		t.Fatalf("cross-entry same-key = %d OK + %d conflicts, want 1+2", ok, conflicts)
	}
	sends++
	if got := pgSendCount(t, admin); got != sends {
		t.Fatalf("cross-entry sends = %d, want %d", got, sends)
	}
	// Post-terminal same-key retries on every entry: conflict, no sends.
	for i, p := range posts {
		if rr := p(crossKey); rr.Code != http.StatusConflict {
			t.Fatalf("cross-entry retry %d: %d, want 409", i, rr.Code)
		}
	}
	if got := pgSendCount(t, admin); got != sends {
		t.Fatalf("post-terminal retries sent again: %d, want %d", got, sends)
	}
	pgSendDetail(t, admin)
}

// ---------------------------------------------------------------------------
// E. Auth scope on the real router: anonymous, malformed, expired, and
// wrong-scope machine identities refuse before dispatch; a scoped machine
// credential executes the ordinary route only; saved ownership is invisible
// to non-owners — admin included; shared follows its contract.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_AuthScope(t *testing.T) {
	fx := setupPGHTTPFixture(t)
	execBody := `{"statement":"SELECT id FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"r1-auth-x"}`

	// Anonymous and malformed bearer never reach dispatch.
	rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, "", execBody)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d, want 401", rr.Code)
	}
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, "not-a-token", execBody)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("malformed: %d, want 401", rr.Code)
	}
	// Expired-but-well-signed user bearer: past the fixed freshness bound.
	stale := mintStaleBearer(t, fx.userID)
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, stale, execBody)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("stale bearer: %d, want 401", rr.Code)
	}
	if got := fx.pgClaimCount(t); got != 0 {
		t.Fatalf("auth refusals occupied claims: %d", got)
	}

	machineSvc := service.NewMachinePrincipalService(mysqlrepo.NewMachinePrincipalRepository(fx.db))
	mkMachine := func(name string, scopes []model.MachineScope) string {
		t.Helper()
		issued, err := machineSvc.Create(context.Background(),
			service.AuthenticatedUser{ID: fx.userID, Role: "admin"},
			model.MachinePrincipalCreateRequest{Name: name, Scopes: scopes})
		if err != nil {
			t.Fatalf("create machine %s: %v", name, err)
		}
		return issued.Secret
	}

	// A governed-select machine credential executes the ordinary route.
	machOK := mkMachine("pg-r1-mach-ok", []model.MachineScope{model.MachineScopeGovernedSelect})
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, machOK, execBody)
	if rr.Code != http.StatusOK {
		t.Fatalf("machine execute: %d; body=%s", rr.Code, rr.Body.String())
	}
	// User-only surfaces refuse the machine credential.
	saved := createSavedPG(t, fx.db, fx.userID, fx.targetID, model.QuerySavedStatementCreateRequest{
		Name: "auth-stmt", Statement: `SELECT id FROM app.orders`, Scope: model.QuerySavedStatementPersonal,
	})
	shared := createSavedPG(t, fx.db, fx.userID, fx.targetID, model.QuerySavedStatementCreateRequest{
		Name: "auth-shared", Statement: `SELECT id FROM app.orders WHERE id = 1`, Scope: model.QuerySavedStatementSharedTemplate,
	})
	for _, tc := range []struct{ name, method, path, body string }{
		{"mach-saved", http.MethodPost,
			fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, saved.ID), `{"clientExecutionId":"r1-auth-ms"}`},
		{"mach-nav", http.MethodPost, fx.nav,
			`{"source":{"database":"discdb","schema":"app","object":"order_items","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":"r1-auth-mn"}`},
	} {
		rr = doBearerWithBody(t, fx.enabled, tc.method, tc.path, machOK, tc.body)
		if rr.Code != http.StatusForbidden {
			t.Fatalf("%s: %d, want 403 machine_scope_denied", tc.name, rr.Code)
		}
		if code, _ := decodeError(t, rr); code != "machine_scope_denied" {
			t.Fatalf("%s code = %q, want machine_scope_denied", tc.name, code)
		}
	}
	// Wrong-scope machine credential on the execute route.
	machWeak := mkMachine("pg-r1-mach-weak", []model.MachineScope{model.MachineScopeInventoryRead})
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, machWeak,
		`{"statement":"SELECT 1","database":"discdb","clientExecutionId":"r1-auth-weak"}`)
	if rr.Code != http.StatusForbidden && rr.Code != http.StatusUnauthorized {
		t.Fatalf("weak machine: %d, want 401/403", rr.Code)
	}

	// Ownership: a personal statement is invisible to a non-owner — admin or
	// not — while the shared statement honors its contract for a second user.
	other := insertAuthzTestUser(t, fx.db, "pg-r1-other@example.com", "editor")
	t.Cleanup(func() { deleteAuthzTestUser(t, fx.db, other) })
	otherTok := mustLogin(t, fx.enabled, "pg-r1-other@example.com", "secret123")
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, saved.ID), otherTok,
		`{"clientExecutionId":"r1-auth-o1"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("non-owner personal saved: %d, want 404 invisible; body=%s", rr.Code, rr.Body.String())
	}
	admin2 := insertAuthzTestUser(t, fx.db, "pg-r1-admin2@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, fx.db, admin2) })
	adminTok := mustLogin(t, fx.enabled, "pg-r1-admin2@example.com", "secret123")
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, saved.ID), adminTok,
		`{"clientExecutionId":"r1-auth-a1"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("admin non-owner personal saved: %d, want 404 invisible", rr.Code)
	}
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, shared.ID), otherTok,
		`{"clientExecutionId":"r1-auth-sh"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("shared saved for member: %d; body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// F. Real response contract + CSV eligibility: decode real success/error
// bodies and feed the delivered page into DecideCSVExport — clean pages are
// eligible, truncated cells and masked columns deny, zero-row pages deny only
// when the matrix evidence is corrupt.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_ResponseContractAndCSV(t *testing.T) {
	fx := setupPGHTTPFixture(t)

	// Masked-column table: a masked_no_copy policy marks the column
	// non-copyable — the delivered text is the fixed mask, never the raw.
	admin := connectPG(t, fx.pg.host, fx.pg.port, fx.pg.db, pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE TABLE app.masked_probe (id bigint PRIMARY KEY, secret text)`,
		`INSERT INTO app.masked_probe VALUES (1, 'ssn-123')`,
		`GRANT SELECT ON app.masked_probe TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
	_ = admin.Close(context.Background())
	disc := mysqlrepo.NewQueryDisclosureRepository(fx.db)
	insertPolicy(t, disc, fx.targetID, "discdb", "app", "masked_probe", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, fx.targetID, "discdb", "app", "masked_probe", "secret", model.ResultDisclosureMaskedNoCopy)

	decode := func(rr *httptest.ResponseRecorder) pgHTTPExecuteResp {
		t.Helper()
		return decodeExecuteResp(t, rr.Body.String())
	}
	toModelCols := func(cols []struct {
		Name         string `json:"name"`
		DatabaseType string `json:"databaseType"`
		Nullable     bool   `json:"nullable"`
		DisplayMode  string `json:"displayMode"`
		CopyAllowed  bool   `json:"copyAllowed"`
	}) []model.QueryResultColumn {
		out := make([]model.QueryResultColumn, len(cols))
		for i, c := range cols {
			out[i] = model.QueryResultColumn{
				Name: c.Name, DisplayMode: model.ResultDisclosureMode(c.DisplayMode), CopyAllowed: c.CopyAllowed,
			}
		}
		return out
	}

	// Clean raw page → CSV-eligible.
	rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id, qty FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"r1-csv-clean"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("clean page: %d; body=%s", rr.Code, rr.Body.String())
	}
	resp := decode(rr)
	if err := service.DecideCSVExport(toModelCols(resp.Columns), resp.Rows, resp.CellTruncated); err != nil {
		t.Fatalf("clean page CSV denied: %v", err)
	}

	// Truncated cell delivered under capability → CSV denies.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT pad FROM app.type_probe WHERE id = 2","database":"discdb","clientExecutionId":"r1-csv-tr","capabilities":["cellTruncated"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("truncated page: %d; body=%s", rr.Code, rr.Body.String())
	}
	resp = decode(rr)
	if !resp.CellTruncated[0][0] {
		t.Fatalf("expected truncated pad cell, matrix = %v", resp.CellTruncated)
	}
	if err := service.DecideCSVExport(toModelCols(resp.Columns), resp.Rows, resp.CellTruncated); err == nil {
		t.Fatal("truncated page passed CSV eligibility")
	}

	// Masked column: masked text + copyAllowed=false → CSV denies.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id, secret FROM app.masked_probe WHERE id = 1","database":"discdb","clientExecutionId":"r1-csv-mask"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("masked page: %d; body=%s", rr.Code, rr.Body.String())
	}
	resp = decode(rr)
	if len(resp.Columns) != 2 || resp.Columns[1].CopyAllowed || resp.Columns[1].DisplayMode != "masked_no_copy" {
		t.Fatalf("masked column contract = %+v", resp.Columns)
	}
	if resp.Rows[0][1] == "ssn-123" {
		t.Fatalf("masked cell leaked raw text: %v", resp.Rows[0][1])
	}
	if err := service.DecideCSVExport(toModelCols(resp.Columns), resp.Rows, resp.CellTruncated); err == nil {
		t.Fatal("masked page passed CSV eligibility")
	}

	// Zero-row page under a real window: eligible evidence is complete —
	// DecideCSVExport carries the verdict, the contract requires the aligned
	// empty matrix, not a fabricated flag.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id FROM app.orders WHERE id > 9999","database":"discdb","clientExecutionId":"r1-csv-zero"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("zero page: %d; body=%s", rr.Code, rr.Body.String())
	}
	resp = decode(rr)
	if resp.RowCount != 0 || len(resp.Rows) != 0 {
		t.Fatalf("zero page rows = %v", resp.Rows)
	}
	if err := service.DecideCSVExport(toModelCols(resp.Columns), resp.Rows, resp.CellTruncated); err != nil {
		t.Fatalf("zero-row page CSV denied: %v", err)
	}

	// Error envelope contract: the controlled codes carry no internal detail.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT nope FROM app.orders","database":"discdb","clientExecutionId":"r1-csv-err"}`)
	if rr.Code == http.StatusOK {
		t.Fatal("unknown column executed")
	}
	code, msg := decodeError(t, rr)
	if code == "" || strings.Contains(msg, "pgsql") || strings.Contains(msg, "42P") {
		t.Fatalf("error envelope = %q/%q — must stay controlled", code, msg)
	}
}

// ---------------------------------------------------------------------------
// G. Fail-closed representatives through the real router + bounded-concurrency
// cleanup: unknown objects get no catalog fallback, a missing disclosure
// policy refuses, and a burst of distinct keys converges without pool
// exhaustion — a later request still executes.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_FailClosed(t *testing.T) {
	fx := setupPGHTTPFixture(t)

	// No catalog fallback: an object absent from the bound catalog fails
	// closed — the request never guesses a relation identity.
	rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id FROM app.no_such_relation","database":"discdb","clientExecutionId":"r1-fc-ncat"}`)
	if rr.Code == http.StatusOK {
		t.Fatal("unknown relation executed — catalog fallback is forbidden")
	}
	code, _ := decodeError(t, rr)
	if code != "query_object_not_found" && code != "query_not_allowed" && code != "schema_not_found" {
		t.Fatalf("unknown object code = %q, want a controlled fail-closed code", code)
	}

	// Missing disclosure policy: a column with no policy row is refused, not
	// delivered raw.
	admin := connectPG(t, fx.pg.host, fx.pg.port, fx.pg.db, pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE TABLE app.unpolicy (id bigint PRIMARY KEY, payload text)`,
		`INSERT INTO app.unpolicy VALUES (1, 'no-policy')`,
		`GRANT SELECT ON app.unpolicy TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
	_ = admin.Close(context.Background())
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id, payload FROM app.unpolicy WHERE id = 1","database":"discdb","clientExecutionId":"r1-fc-nopol"}`)
	if rr.Code == http.StatusOK {
		t.Fatal("unpolicy column delivered — missing policy must refuse")
	}
	code, _ = decodeError(t, rr)
	if code != "query_result_disclosure_blocked" && code != "query_not_allowed" {
		t.Fatalf("missing policy code = %q, want disclosure refusal", code)
	}
	if strings.Contains(rr.Body.String(), "no-policy") {
		t.Fatal("missing-policy refusal leaked the raw cell")
	}

	// Bounded concurrency over distinct keys: the single-slot pools never
	// deadlock; every request gets a terminal answer and the pool converges.
	const burst = 4
	var wg sync.WaitGroup
	codes := make([]int, burst)
	bodies := make([]string, burst)
	for i := 0; i < burst; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
				fmt.Sprintf(`{"statement":"SELECT id FROM app.orders WHERE id = %d","database":"discdb","clientExecutionId":"r1-fc-burst-%d"}`, i+1, i))
			codes[i], bodies[i] = rr.Code, rr.Body.String()
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("burst %d: %d; body=%s", i, c, bodies[i])
		}
	}
	// After the burst, a later request still executes — connections and
	// locks converged, nothing wedged.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id FROM app.orders WHERE id = 1","database":"discdb","clientExecutionId":"r1-fc-after"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("post-burst execute: %d; body=%s", rr.Code, rr.Body.String())
	}
}

// mintStaleBearer forges a correctly-signed bearer whose issuance time is
// just past the fixed MaxQueryTokenAge freshness bound — the middleware must
// refuse it as stale even though the signature verifies.
func mintStaleBearer(t *testing.T, userID uint64) string {
	t.Helper()
	payload := fmt.Sprintf("%d:%d:%d", userID, 1,
		time.Now().Add(-(8*time.Hour + time.Minute)).Unix())
	mac := hmac.New(sha256.New, []byte(authzIntegrationSecret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + sig))
}

// ---------------------------------------------------------------------------
// Version compatibility attempts (PG-HTTP-T12-R1 §六): the PG16 fixture owns
// the complete matrix; PG14/15/17 get an honest attempt at the same governed
// HTTP execute on a local image — a missing image with a blocked registry is
// recorded as environment-unverified, never as passed and never as failed.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_VersionMatrix(t *testing.T) {
	for _, image := range []string{"postgres:14", "postgres:15", "postgres:17"} {
		image := image
		t.Run(image, func(t *testing.T) {
			if _, err := exec.Command("docker", "image", "inspect", image).Output(); err != nil {
				t.Skipf("%s absent locally and registry pull is blocked — version unverified, not failed", image)
			}
			fx := setupPGHTTPFixtureOn(t, image)
			rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
				`{"statement":"SELECT id FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"r1-ver-`+strings.ReplaceAll(image, ":", "-")+`"}`)
			if rr.Code != http.StatusOK {
				t.Fatalf("%s governed execute: %d; body=%s", image, rr.Code, rr.Body.String())
			}
			var resp struct {
				Status       string `json:"status"`
				RowCount     int    `json:"rowCount"`
				LimitApplied int    `json:"limitApplied"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("%s decode: %v", image, err)
			}
			if resp.LimitApplied != 51 || resp.RowCount != 2 {
				t.Fatalf("%s limitApplied/rowCount = %d/%d, want 51/2 (default window 50+sentinel, 2 rows)", image, resp.LimitApplied, resp.RowCount)
			}
		})
	}
}
