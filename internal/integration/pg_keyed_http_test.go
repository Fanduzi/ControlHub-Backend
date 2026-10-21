//go:build integration

// Package integration — real-HTTP vertical proof for keyed PostgreSQL ordinary, saved-statement, and related-navigation execution (PG-HTTP-T12, issue #120).
// input: shared MySQL fixture (real router/auth/repository/claim/evidence stack), a disposable PostgreSQL 16 with a restricted role and the nav fixture catalog, two in-process assemblies — the default closed instance and the construction-time-enabled instance
// output: TestPGKeyedHTTP_* — the closed assembly refuses all three PG POSTs, plus setupPGHTTPFixtureOn (image-parametrized build for version attempts) without claim occupancy; the enabled assembly carries each request through handler → adapter → keyed PG orchestration → native executor → disclosure → finalize → delivered envelope; evidence, replay, capability, scope, and response-fidelity assertions all run against real persisted rows
// pos: T12 production-path acceptance — requests travel the real api.NewRouter; no test hand-composes claim/finalize calls. The WithPGHTTPEntries capability is wired only on this fixture's second assembly; the default assembly in the same process stays fail-closed and cmd/server is unchanged.
// note: if this file changes, update this header and module README.md.

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const pgHTTPCredRef = "PG_HTTP_RO"

// pgHTTPFixture carries the two same-process assemblies: the default closed
// router and the fixture-only enabled router, both over the same real
// MySQL repositories and the same disposable PG16.
type pgHTTPFixture struct {
	db        *sql.DB
	pg        pgDiscFixture
	targetID  uint64
	userID    uint64
	token     string
	enabled   http.Handler
	closed    http.Handler
	execute   string
	savedBase string
	nav       string
}

// setupPGHTTPFixture builds the full production wiring — real router, real
// auth, real repositories — twice: once with WithPGHTTPEntries (the test
// opt-in) and once without (the default production shape). Both share the
// same process, database, and target, proving the capability is
// construction-bound, not request-bound.
func setupPGHTTPFixture(t *testing.T) pgHTTPFixture {
	t.Helper()
	return setupPGHTTPFixtureOn(t, "postgres:16-alpine")
}

func setupPGHTTPFixtureOn(t *testing.T, image string) pgHTTPFixture {
	t.Helper()
	db := setupTestDB(t)
	pg := newPGNavFixtureOn(t, image)

	userID := insertAuthzTestUser(t, db, "pghttp-admin@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	targetID := createPGInstance(t, db, "pg-http-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, pg.host, pg.port)
	insertPGCredential(t, db, targetID, pg.db, "app", pgHTTPCredRef, true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+pgHTTPCredRef,
		pgKeywordDSN(pg.host, pg.port, pg.db, pgDiscRole, pgStmtPassword, 5))

	// Disclosure policies — every delivered column needs an explicit raw
	// policy on its canonical scope.
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	for _, pol := range []struct{ schema, object, column string }{
		{"app", "orders", "id"}, {"app", "orders", "qty"}, {"app", "orders", "note"},
		{"analytics", "metrics", "id"}, {"analytics", "metrics", "label"},
		{"app", "tag_target", "tag"},
		{"app", "blob_target", "id"}, {"app", "blob_target", "payload"},
		{"app", "type_probe", "bi"}, {"app", "type_probe", "num"},
		{"app", "type_probe", "b"}, {"app", "type_probe", "ts"},
		{"app", "type_probe", "byt"}, {"app", "type_probe", "arr"},
		{"app", "type_probe", "j"}, {"app", "type_probe", "pad"},
		{"app", "type_probe", "emp"}, {"app", "type_probe", "txt"},
	} {
		insertPolicy(t, disc, targetID, pg.db, pol.schema, pol.object, pol.column, model.ResultDisclosureRawCopyAllowed)
	}

	// Typed fixture table: values that must survive the text boundary
	// verbatim — a bigint beyond JS safe range, a high-precision numeric,
	// bool, timestamptz, bytea, int array, jsonb, and NULL/empty/'NULL'
	// text triple.
	admin := connectPG(t, pg.host, pg.port, pg.db, pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE TABLE app.type_probe (id bigint PRIMARY KEY, bi bigint, num numeric(38,20), b boolean,
		    ts timestamptz, byt bytea, arr int[], j jsonb, pad text, emp text, txt text)`,
		`INSERT INTO app.type_probe VALUES (1, 9223372036854775807, 3.14159265358979323846, true,
		    '2024-01-02 03:04:05.678901+00', '\xdeadbeef', '{1,2,3}', '{"k":1}',
		    repeat('x', 8192), '', 'NULL')`,
		`INSERT INTO app.type_probe VALUES (2, -9007199254740993, -0.00000000000000000001, false,
		    '1999-12-31 23:59:59+00', '\x00', '{}', 'null',
		    repeat('y', 8193), 'nonempty', NULL)`,
		`GRANT SELECT ON app.type_probe TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
	admin.Close(context.Background())

	build := func(enable bool) http.Handler {
		savedRepo := mysqlrepo.NewQuerySavedStatementRepository(db)
		targetRepo := mysqlrepo.NewQueryTargetRepository(db)
		guard := service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
		execSvc := newPGClaimedService(db).
			WithTemplateExecution(savedRepo, service.NewTemplateStatementCompiler())
		if enable {
			execSvc = execSvc.WithPGHTTPEntries()
		}
		return api.NewRouter(api.Dependencies{
			AuthService:                service.NewAuthService(mysqlrepo.NewUserRepository(db), authzIntegrationSecret),
			AuditService:               service.NewAuditService(mysqlrepo.NewAuditRepository(db)),
			QueryExecutionService:      execSvc,
			QuerySavedStatementService: service.NewQuerySavedStatementService(savedRepo, savedRepo, targetRepo, guard),
			MachineCredentialService:   service.NewMachinePrincipalService(mysqlrepo.NewMachinePrincipalRepository(db)),
			QueryExecutionAuth:         api.QueryExecutionAuthConfig{Clock: time.Now},
		})
	}

	enabled := build(true)
	closed := build(false)
	token := mustLogin(t, enabled, "pghttp-admin@example.com", "secret123")
	return pgHTTPFixture{
		db: db, pg: pg, targetID: targetID, userID: userID, token: token,
		enabled: enabled, closed: closed,
		execute:   fmt.Sprintf("/query-targets/%d/execute", targetID),
		savedBase: fmt.Sprintf("/query-targets/%d/saved-statements", targetID),
		nav:       fmt.Sprintf("/query-targets/%d/related-records", targetID),
	}
}

// pgClaimCount counts claim rows on the fixture target — the occupancy proof
// for both "never claimed" (closed) and "exactly one" (enabled) boundaries.
func (f pgHTTPFixture) pgClaimCount(t *testing.T) int {
	t.Helper()
	return countWhere(t, f.db,
		`SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ?`, f.targetID)
}

// pgExecStatus returns the persisted terminal status for a key, or "" when
// nothing landed.
func (f pgHTTPFixture) pgExecStatus(t *testing.T, key string) string {
	t.Helper()
	var status string
	err := f.db.QueryRow(
		`SELECT status FROM query_executions WHERE target_resource_id = ? AND client_execution_id = ?`,
		f.targetID, key).Scan(&status)
	if err == sql.ErrNoRows {
		return ""
	}
	if err != nil {
		t.Fatalf("keyed status read: %v", err)
	}
	return status
}

// assertPGLinkedScope reads the claim+execution join for one key and asserts
// the PG composite scope: the claim and the terminal row both name
// (discdb, app), the key is on the execution row, a real backend PID and
// remote_state landed, and the actor is the authenticated user.
func (f pgHTTPFixture) assertPGLinkedScope(t *testing.T, key string) {
	t.Helper()
	var (
		execID        int64
		execDB        string
		execSchema    string
		execStatus    string
		execKey       sql.NullString
		execPID       sql.NullInt64
		execRemote    sql.NullString
		execActor     int64
		execMachine   sql.NullInt64
		claimExecID   sql.NullInt64
		claimDB       string
		claimSchema   string
		claimActor    int64
		claimActorMac sql.NullInt64
	)
	if err := f.db.QueryRow(`SELECT e.id, e.database_name, e.schema_name, e.status,
       e.client_execution_id, e.backend_pid, e.remote_state,
       e.actor_user_id, e.actor_machine_principal_id,
       c.execution_id, c.database_name, c.schema_name,
       c.actor_user_id, c.actor_machine_principal_id
FROM query_execution_claims c
LEFT JOIN query_executions e ON e.id = c.execution_id
WHERE c.target_resource_id = ? AND c.client_execution_id = ?`,
		f.targetID, key).
		Scan(&execID, &execDB, &execSchema, &execStatus, &execKey,
			&execPID, &execRemote, &execActor, &execMachine,
			&claimExecID, &claimDB, &claimSchema,
			&claimActor, &claimActorMac); err != nil {
		t.Fatalf("pg claim+execution join: %v", err)
	}
	if !claimExecID.Valid || claimExecID.Int64 != execID || execID <= 0 {
		t.Fatalf("claim not linked to its terminal row: claim.execution_id=%v e.id=%d", claimExecID, execID)
	}
	if claimDB != "discdb" || claimSchema != "app" {
		t.Fatalf("claim scope = (%q,%q), want (discdb,app)", claimDB, claimSchema)
	}
	if execDB != "discdb" || execSchema != "app" {
		t.Fatalf("execution scope = (%q,%q), want (discdb,app)", execDB, execSchema)
	}
	if execStatus != string(model.QueryExecutionSuccess) {
		t.Fatalf("execution status = %q, want success", execStatus)
	}
	if !execKey.Valid || execKey.String != key {
		t.Fatalf("execution must carry key %q, got %v", key, execKey)
	}
	if !execPID.Valid || execPID.Int64 == 0 {
		t.Fatalf("backend_pid must be the real PG pid, got %v", execPID)
	}
	if !execRemote.Valid || execRemote.String != "completed" {
		t.Fatalf("remote_state = %v, want completed", execRemote)
	}
	if execActor != int64(f.userID) || execMachine.Valid {
		t.Fatalf("execution actor = user %d machine %v, want user %d", execActor, execMachine, f.userID)
	}
	if claimActor != int64(f.userID) || claimActorMac.Valid {
		t.Fatalf("claim actor = user %d machine %v, want user %d", claimActor, claimActorMac, f.userID)
	}
}

// pgHTTPExecuteResp is the decoded ordinary/saved execute envelope subset the
// assertions need; unknown extra fields are fine — the strict envelope is
// already guarded by decode tests.
type pgHTTPExecuteResp struct {
	ExecutionID uint64 `json:"executionId"`
	Status      string `json:"status"`
	Engine      string `json:"engine"`
	RowCount    int    `json:"rowCount"`
	Columns     []struct {
		Name         string `json:"name"`
		DatabaseType string `json:"databaseType"`
		Nullable     bool   `json:"nullable"`
		DisplayMode  string `json:"displayMode"`
		CopyAllowed  bool   `json:"copyAllowed"`
	} `json:"columns"`
	Rows          [][]any  `json:"rows"`
	CellTruncated [][]bool `json:"cellTruncated"`
	Pagination    *struct {
		Page            int  `json:"page"`
		PageSize        int  `json:"pageSize"`
		HasPreviousPage bool `json:"hasPreviousPage"`
		HasNextPage     bool `json:"hasNextPage"`
	} `json:"pagination"`
	Context *struct {
		Database string `json:"database"`
		Schema   string `json:"schema"`
	} `json:"context"`
}

func decodeExecuteResp(t *testing.T, body string) pgHTTPExecuteResp {
	t.Helper()
	var resp pgHTTPExecuteResp
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatalf("decode execute response: %v — %s", err, body)
	}
	return resp
}

// ---------------------------------------------------------------------------
// A. Assembly boundary — the default instance refuses all three PG POSTs in
// the same process where the enabled instance executes them.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_DefaultClosedBoundary(t *testing.T) {
	fx := setupPGHTTPFixture(t)

	// Saved statement exists so the saved POST reaches the engine gate —
	// a 404 would mask whether dispatch happened.
	saved := createSavedPG(t, fx.db, fx.userID, fx.targetID, model.QuerySavedStatementCreateRequest{
		Name: "closed-gate", Statement: `SELECT id FROM app.orders`, Scope: model.QuerySavedStatementPersonal,
	})

	cases := []struct {
		name, method, path, body string
		wantStatus               int
		wantCode                 string
	}{
		// Ordinary without PG scope fields → engine denial via the legacy
		// NULL-key rejected pair (the preserved access boundary).
		{"execute-no-scope", http.MethodPost, fx.execute,
			`{"statement":"SELECT id FROM app.orders","clientExecutionId":"closed-1"}`,
			http.StatusForbidden, "query_not_allowed"},
		// Ordinary carrying PG scope fields → shape rejection before engine
		// dispatch, same as before the capability existed.
		{"execute-pg-scope", http.MethodPost, fx.execute,
			`{"statement":"SELECT id FROM app.orders","database":"discdb","clientExecutionId":"closed-2"}`,
			http.StatusBadRequest, "validation_failed"},
		// Saved on the PG target → legacy path engine denial.
		{"saved", http.MethodPost, fmt.Sprintf("%s/%d/execute", fx.savedBase, saved.ID),
			`{"clientExecutionId":"closed-3"}`,
			http.StatusForbidden, "query_not_allowed"},
		// Saved with a PG-style scope selector → validation rejection.
		{"saved-scope", http.MethodPost, fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, saved.ID),
			`{"clientExecutionId":"closed-4"}`,
			http.StatusBadRequest, "validation_failed"},
		// Navigation with source.schema → validation rejection.
		{"nav-schema", http.MethodPost, fx.nav,
			`{"source":{"database":"discdb","schema":"app","object":"order_items","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":"closed-5"}`,
			http.StatusBadRequest, "validation_failed"},
		// Navigation without schema on the PG target → legacy engine denial.
		{"nav-no-schema", http.MethodPost, fx.nav,
			`{"source":{"database":"discdb","object":"order_items","kind":"table","foreignKey":"fk_orders"},"localValues":["1"],"clientExecutionId":"closed-6"}`,
			http.StatusForbidden, "query_not_allowed"},
	}
	for _, tc := range cases {
		rr := doBearerWithBody(t, fx.closed, tc.method, tc.path, fx.token, tc.body)
		if rr.Code != tc.wantStatus {
			t.Fatalf("%s: status = %d, want %d; body=%s", tc.name, rr.Code, tc.wantStatus, rr.Body.String())
		}
		code, _ := decodeError(t, rr)
		if code != tc.wantCode {
			t.Fatalf("%s: error = %q, want %q", tc.name, code, tc.wantCode)
		}
	}

	// No claim may ever be occupied by a closed-instance request; the only
	// evidence allowed is the legacy NULL-key rejected pair — every
	// persisted execution row is a rejection, never a user-SQL attempt.
	if got := fx.pgClaimCount(t); got != 0 {
		t.Fatalf("closed instance occupied %d claims, want 0", got)
	}
	if got := countWhere(t, fx.db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND status != 'rejected'`, fx.targetID); got != 0 {
		t.Fatalf("closed instance wrote %d non-rejected executions, want 0", got)
	}

	// The enabled instance on the same data succeeds — the gate is the
	// assembly, not the target, role, or any request field.
	rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"gate-proof"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("enabled execute: status = %d; body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// B+C+E. Ordinary execute vertical: golden execution, response fidelity,
// keyed evidence, replay/conflict, point read, request boundary.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_Execute_Vertical(t *testing.T) {
	fx := setupPGHTTPFixture(t)
	key := "pg-http-vert-1"

	rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id, note FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"`+key+`"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("execute: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeExecuteResp(t, rr.Body.String())
	if resp.ExecutionID == 0 || resp.Status != "success" || resp.Engine != "postgresql" {
		t.Fatalf("response envelope: %+v", resp)
	}
	// PG native text: bigint renders "1"/"2"; NULL stays JSON null.
	if len(resp.Rows) != 2 || resp.Rows[0][0] != "1" || resp.Rows[0][1] != "n1" ||
		resp.Rows[1][0] != "2" || resp.Rows[1][1] != nil {
		t.Fatalf("rows = %v, want [[1 n1] [2 nil]] as text cells", resp.Rows)
	}
	if resp.Context == nil || resp.Context.Database != "discdb" || resp.Context.Schema != "app" {
		t.Fatalf("context = %+v, want resolved discdb/app", resp.Context)
	}
	if resp.Pagination == nil || resp.Pagination.Page != 1 || resp.Pagination.PageSize <= 0 {
		t.Fatalf("PG executions always report the resolved window: %+v", resp.Pagination)
	}
	if len(resp.Columns) != 2 || resp.Columns[0].Name != "id" || resp.Columns[1].Name != "note" {
		t.Fatalf("columns = %+v", resp.Columns)
	}
	// cellTruncated must be delivered with the rows — an all-false matrix
	// for this clean page, never an omitted sentinel.
	if len(resp.CellTruncated) != 2 || len(resp.CellTruncated[0]) != 2 || resp.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want 2x2 all-false", resp.CellTruncated)
	}

	fx.assertPGLinkedScope(t, key)
	if execs, audits, linked := claimExecCounts(t, fx.db, fx.targetID, key); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("history/audit/link = %d/%d/%d, want 1/1/1", execs, audits, linked)
	}

	// Same-key identical replay → 409 already_exists, zero new evidence.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id, note FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"`+key+`"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("replay: status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	code, _ := decodeError(t, rr)
	if code != "execution_already_exists" {
		t.Fatalf("replay code = %q, want execution_already_exists", code)
	}
	if execs, audits, linked := claimExecCounts(t, fx.db, fx.targetID, key); execs != 1 || audits != 1 || linked != 1 {
		t.Fatalf("replay wrote evidence: %d/%d/%d", execs, audits, linked)
	}

	// Same key, different digest → 409 conflict, zero new evidence.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT id FROM app.orders","database":"discdb","clientExecutionId":"`+key+`"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("conflict: status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	code, _ = decodeError(t, rr)
	if code != "client_execution_id_conflict" {
		t.Fatalf("conflict code = %q, want client_execution_id_conflict", code)
	}

	// Point read by key: the single terminal row, kind=execution.
	rr = doBearer(t, fx.enabled, http.MethodGet,
		fmt.Sprintf("/query-targets/%d/executions?clientExecutionId=%s", fx.targetID, key), fx.token)
	if rr.Code != http.StatusOK {
		t.Fatalf("point read: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0]["kind"] != "execution" ||
		listed.Items[0]["id"] != float64(resp.ExecutionID) || listed.Items[0]["status"] != "success" {
		t.Fatalf("point read items = %v", listed.Items)
	}

	// Request boundary: PG-required fields are enforced before any claim.
	for _, tc := range []struct {
		name, body string
	}{
		{"missing-database", `{"statement":"SELECT 1","clientExecutionId":"b1"}`},
		{"missing-key", `{"statement":"SELECT 1","database":"discdb"}`},
		{"empty-database", `{"statement":"SELECT 1","database":"","clientExecutionId":"b2"}`},
		{"empty-key", `{"statement":"SELECT 1","database":"discdb","clientExecutionId":""}`},
	} {
		rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token, tc.body)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s: status = %d, want 400; body=%s", tc.name, rr.Code, rr.Body.String())
		}
	}
	if got := fx.pgClaimCount(t); got != 1 {
		t.Fatalf("validation failures occupied claims: %d, want 1 total", got)
	}
}

// ---------------------------------------------------------------------------
// C+I. Native-text fidelity across the HTTP boundary plus the cellTruncated
// capability gate: 8192-byte exact, 8193 truncated, NULL/empty/'NULL'.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_Execute_TypesAndCells(t *testing.T) {
	fx := setupPGHTTPFixture(t)

	rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT bi, num, b, ts, byt, arr, j FROM app.type_probe WHERE id = 1","database":"discdb","clientExecutionId":"pg-http-types"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("typed execute: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeExecuteResp(t, rr.Body.String())
	if len(resp.Rows) != 1 || len(resp.Rows[0]) != 7 {
		t.Fatalf("typed rows = %v", resp.Rows)
	}
	row := resp.Rows[0]
	// Every cell arrives as native text — no number/boolean coercion.
	if row[0] != "9223372036854775807" {
		t.Fatalf("bigint cell = %v (%T), want verbatim text", row[0], row[0])
	}
	if row[1] != "3.14159265358979323846" {
		t.Fatalf("numeric cell = %v, want verbatim text", row[1])
	}
	if row[2] != "t" {
		t.Fatalf("bool cell = %v (%T), want PG's own text form 't'", row[2], row[2])
	}
	if s, ok := row[3].(string); !ok || !strings.HasPrefix(s, "2024-01-02 03:04:05.678901") {
		t.Fatalf("timestamptz cell = %v", row[3])
	}
	if s, ok := row[4].(string); !ok || !strings.HasPrefix(s, `\x`) {
		t.Fatalf("bytea cell = %v, want hex text", row[4])
	}
	if row[5] != "{1,2,3}" {
		t.Fatalf("int[] cell = %v, want array literal text", row[5])
	}
	if row[6] != `{"k": 1}` && row[6] != `{"k":1}` {
		t.Fatalf("jsonb cell = %v", row[6])
	}

	// NULL / empty / 'NULL' distinctness on row 2: NULL → nil, '' → "",
	// 'NULL' → "NULL".
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT pad, emp, txt FROM app.type_probe WHERE id = 2","database":"discdb","clientExecutionId":"pg-http-nulls","capabilities":["cellTruncated"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("null-triple execute: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	resp = decodeExecuteResp(t, rr.Body.String())
	row = resp.Rows[0]
	if row[0] == nil || len(row[0].(string)) != 8192 {
		t.Fatalf("pad cell: got %v, want the 8192-safe prefix of the 8193-byte value", row[0])
	}
	if row[1] != "nonempty" || row[2] != nil {
		t.Fatalf("emp/txt cells = %v/%v, want 'nonempty'/null", row[1], row[2])
	}
	if len(resp.CellTruncated) != 1 || len(resp.CellTruncated[0]) != 3 ||
		!resp.CellTruncated[0][0] || resp.CellTruncated[0][1] || resp.CellTruncated[0][2] {
		t.Fatalf("cellTruncated = %v, want [true false false]", resp.CellTruncated)
	}
	if fx.pgExecStatus(t, "pg-http-nulls") != string(model.QueryExecutionSuccess) {
		t.Fatalf("truncated delivery must still record success")
	}

	// The same truncated cell without the capability → controlled 409 with
	// no rows, while history still records success (finalized before gate).
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT pad FROM app.type_probe WHERE id = 2","database":"discdb","clientExecutionId":"pg-http-nocap"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("no-capability truncated page: status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	code, _ := decodeError(t, rr)
	if code != "result_contract_upgrade_required" {
		t.Fatalf("code = %q, want result_contract_upgrade_required", code)
	}
	if strings.Contains(rr.Body.String(), `"rows"`) {
		t.Fatalf("refusal leaked rows: %s", rr.Body.String())
	}
	if got := fx.pgExecStatus(t, "pg-http-nocap"); got != string(model.QueryExecutionSuccess) {
		t.Fatalf("refused attempt history status = %q, want success", got)
	}

	// Exact-8192 cell: not truncated, all-false matrix, no capability needed.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token,
		`{"statement":"SELECT pad FROM app.type_probe WHERE id = 1","database":"discdb","clientExecutionId":"pg-http-edge"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("8192-exact execute: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	resp = decodeExecuteResp(t, rr.Body.String())
	if len(resp.Rows) != 1 || len(resp.Rows[0][0].(string)) != 8192 ||
		len(resp.CellTruncated) != 1 || resp.CellTruncated[0][0] {
		t.Fatalf("exact-8192 cell must deliver whole, flag false: rows=%v matrix=%v", resp.Rows, resp.CellTruncated)
	}
}

// ---------------------------------------------------------------------------
// D. Saved statement HTTP vertical: ?database scope required + snapshot
// match, parameter binding, replay/conflict, and values privacy.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_Saved_Vertical(t *testing.T) {
	fx := setupPGHTTPFixture(t)

	static := createSavedPG(t, fx.db, fx.userID, fx.targetID, model.QuerySavedStatementCreateRequest{
		Name: "orders-static", Statement: `SELECT id, note FROM app.orders ORDER BY id`,
		Scope: model.QuerySavedStatementPersonal,
	})
	param := createSavedPG(t, fx.db, fx.userID, fx.targetID, model.QuerySavedStatementCreateRequest{
		Name: "orders-by-qty", Statement: `SELECT id FROM app.orders WHERE qty = :q`,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "q", Type: model.QuerySavedStatementParameterInteger},
		},
		Scope: model.QuerySavedStatementPersonal,
	})

	// ?database is required on a PG target — missing, empty, and repeated
	// are all shape rejections before claim occupancy.
	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"missing", "", http.StatusBadRequest},
		{"empty", "?database=", http.StatusBadRequest},
		{"repeated", "?database=discdb&database=discdb", http.StatusBadRequest},
		{"too-long", "?database=" + strings.Repeat("d", 200), http.StatusBadRequest},
	} {
		rr := doBearerWithBody(t, fx.enabled, http.MethodPost,
			fmt.Sprintf("%s/%d/execute%s", fx.savedBase, static.ID, tc.query), fx.token,
			`{"clientExecutionId":"scope-`+tc.name+`"}`)
		if rr.Code != tc.want {
			t.Fatalf("%s ?database: status = %d, want %d; body=%s", tc.name, rr.Code, tc.want, rr.Body.String())
		}
	}

	// A scope value that differs from the authorized statement's recorded
	// database is the same invisible-not-found as an unknown statement —
	// existence never leaks.
	rr := doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=other", fx.savedBase, static.ID), fx.token,
		`{"clientExecutionId":"scope-mismatch"}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("scope mismatch: status = %d, want 404; body=%s", rr.Code, rr.Body.String())
	}

	// Golden static execution through the real saved chain.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, static.ID), fx.token,
		`{"clientExecutionId":"pg-http-saved-1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("saved static: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	resp := decodeExecuteResp(t, rr.Body.String())
	if resp.ExecutionID == 0 || len(resp.Rows) != 2 || resp.Rows[0][0] != "1" || resp.Rows[0][1] != "n1" ||
		resp.Context == nil || resp.Context.Schema != "app" {
		t.Fatalf("saved static response: %+v", resp)
	}
	fx.assertPGLinkedScope(t, "pg-http-saved-1")

	// Parameterized: :q binds the typed integer through CompilePG — never
	// interpolated; the saved SQL/values never reach history preview.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, param.ID), fx.token,
		`{"values":{"q":20},"clientExecutionId":"pg-http-saved-2"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("saved param: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	resp = decodeExecuteResp(t, rr.Body.String())
	if len(resp.Rows) != 1 || resp.Rows[0][0] != "2" {
		t.Fatalf("param rows = %v, want qty=20 → id 2", resp.Rows)
	}
	fx.assertPGLinkedScope(t, "pg-http-saved-2")

	// Replay/conflict on the saved entry shares the same claim domain.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, static.ID), fx.token,
		`{"clientExecutionId":"pg-http-saved-1"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("saved replay: status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	code, _ := decodeError(t, rr)
	if code != "execution_already_exists" {
		t.Fatalf("saved replay code = %q", code)
	}
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost,
		fmt.Sprintf("%s/%d/execute?database=discdb", fx.savedBase, param.ID), fx.token,
		`{"values":{"q":10},"clientExecutionId":"pg-http-saved-2"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("saved conflict: status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	code, _ = decodeError(t, rr)
	if code != "client_execution_id_conflict" {
		t.Fatalf("saved conflict code = %q", code)
	}

	// Protected content: the persisted statement preview for the saved
	// attempt never carries raw SQL or parameter values.
	var preview string
	if err := fx.db.QueryRow(
		`SELECT COALESCE(statement_preview,'') FROM query_executions WHERE target_resource_id = ? AND client_execution_id = 'pg-http-saved-2'`,
		fx.targetID).Scan(&preview); err != nil {
		t.Fatalf("preview read: %v", err)
	}
	if strings.Contains(preview, "20") || strings.Contains(preview, ":q") {
		t.Fatalf("saved preview leaked SQL/values: %q", preview)
	}
}

// ---------------------------------------------------------------------------
// C+D. Navigation HTTP vertical: source.schema required, golden + cross-schema
// relation metadata, replay, and injection-safe locator binding.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_Navigation_Vertical(t *testing.T) {
	fx := setupPGHTTPFixture(t)

	navBody := func(schema, object, fk, local, key string) string {
		schemaPart := ""
		if schema != "-" {
			schemaPart = fmt.Sprintf(`,"schema":%q`, schema)
		}
		return fmt.Sprintf(`{"source":{"database":"discdb"%s,"object":%q,"kind":"table","foreignKey":%q},"localValues":[%q],"clientExecutionId":%q}`,
			schemaPart, object, fk, local, key)
	}

	// source.schema is required on a PG target — missing is a shape
	// rejection before claim occupancy.
	rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.nav, fx.token,
		navBody("-", "order_items", "fk_orders", "1", "nav-shape-1"))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("nav missing schema: status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}

	// Golden same-schema navigation: order_items → orders on local "1".
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.nav, fx.token,
		navBody("app", "order_items", "fk_orders", "1", "pg-http-nav-1"))
	if rr.Code != http.StatusOK {
		t.Fatalf("nav golden: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp struct {
		ExecutionID        uint64   `json:"executionId"`
		Status             string   `json:"status"`
		Engine             string   `json:"engine"`
		Rows               [][]any  `json:"rows"`
		SourceDatabase     string   `json:"sourceDatabase"`
		SourceSchema       string   `json:"sourceSchema"`
		SourceObject       string   `json:"sourceObject"`
		ForeignKey         string   `json:"foreignKey"`
		ReferencedDatabase string   `json:"referencedDatabase"`
		ReferencedSchema   string   `json:"referencedSchema"`
		ReferencedObject   string   `json:"referencedObject"`
		ReferencedColumns  []string `json:"referencedColumns"`
		Context            *struct {
			Database string `json:"database"`
			Schema   string `json:"schema"`
		} `json:"context"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode nav response: %v", err)
	}
	if resp.ExecutionID == 0 || resp.Status != "success" || resp.Engine != "postgresql" {
		t.Fatalf("nav envelope: %+v", resp)
	}
	// Referenced row: orders id=1 → (1, 10, 'n1') as native text.
	if len(resp.Rows) != 1 || resp.Rows[0][0] != "1" || resp.Rows[0][1] != "10" || resp.Rows[0][2] != "n1" {
		t.Fatalf("nav rows = %v, want orders(1,10,n1)", resp.Rows)
	}
	// Relation metadata from the verified binding: both databases report
	// the fixed connection database; schemas carry their own fields;
	// context is the pinned execution scope.
	if resp.SourceDatabase != "discdb" || resp.ReferencedDatabase != "discdb" {
		t.Fatalf("relation databases = %q/%q, want the fixed connection scope", resp.SourceDatabase, resp.ReferencedDatabase)
	}
	if resp.SourceSchema != "app" || resp.ReferencedSchema != "app" ||
		resp.SourceObject != "order_items" || resp.ReferencedObject != "orders" ||
		resp.ForeignKey != "fk_orders" || len(resp.ReferencedColumns) != 1 || resp.ReferencedColumns[0] != "id" {
		t.Fatalf("relation metadata = %+v", resp)
	}
	if resp.Context == nil || resp.Context.Database != "discdb" || resp.Context.Schema != "app" {
		t.Fatalf("nav context = %+v, want pinned discdb/app", resp.Context)
	}
	fx.assertPGLinkedScope(t, "pg-http-nav-1")

	// Cross-schema: app.metric_refs → analytics.metrics — referencedSchema
	// reports analytics while context keeps the pinned app schema.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.nav, fx.token,
		navBody("app", "metric_refs", "fk_metric", "7", "pg-http-nav-x"))
	if rr.Code != http.StatusOK {
		t.Fatalf("nav cross-schema: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var xresp struct {
		ReferencedSchema string  `json:"referencedSchema"`
		ReferencedObject string  `json:"referencedObject"`
		Rows             [][]any `json:"rows"`
		Context          *struct {
			Schema string `json:"schema"`
		} `json:"context"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &xresp); err != nil {
		t.Fatalf("decode cross nav: %v", err)
	}
	if xresp.ReferencedSchema != "analytics" || xresp.ReferencedObject != "metrics" {
		t.Fatalf("cross-schema relation = %+v, want analytics.metrics", xresp)
	}
	if len(xresp.Rows) != 1 || xresp.Rows[0][0] != "7" || xresp.Rows[0][1] != "m7" {
		t.Fatalf("cross-schema rows = %v", xresp.Rows)
	}
	if xresp.Context == nil || xresp.Context.Schema != "app" {
		t.Fatalf("context must stay the pinned app schema, got %+v", xresp.Context)
	}

	// SQL-looking locator binds as an exact text value — never interpolates.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.nav, fx.token,
		navBody("app", "tag_refs", "fk_tag", `'; DROP TABLE app.orders; --x`, "pg-http-nav-inj"))
	if rr.Code != http.StatusOK {
		t.Fatalf("nav injection locator: status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var iresp struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &iresp); err != nil {
		t.Fatalf("decode injection nav: %v", err)
	}
	if len(iresp.Rows) != 1 || iresp.Rows[0][0] != `'; DROP TABLE app.orders; --x` {
		t.Fatalf("injection locator rows = %v, want the literal tag row", iresp.Rows)
	}

	// Replay on the navigation entry shares the claim domain.
	rr = doBearerWithBody(t, fx.enabled, http.MethodPost, fx.nav, fx.token,
		navBody("app", "order_items", "fk_orders", "1", "pg-http-nav-1"))
	if rr.Code != http.StatusConflict {
		t.Fatalf("nav replay: status = %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	code, _ := decodeError(t, rr)
	if code != "execution_already_exists" {
		t.Fatalf("nav replay code = %q", code)
	}
	// audits counts every attempt on the target — the three successful
	// navigations wrote three; the replay added nothing.
	if execs, audits, linked := claimExecCounts(t, fx.db, fx.targetID, "pg-http-nav-1"); execs != 1 || audits != 3 || linked != 1 {
		t.Fatalf("nav replay evidence = %d/%d/%d, want 1/3/1", execs, audits, linked)
	}
}

// ---------------------------------------------------------------------------
// E. Same-key concurrency: parallel POSTs on one key admit exactly one
// claimant; the loser gets the controlled conflict without new evidence.
// ---------------------------------------------------------------------------

func TestPGKeyedHTTP_Concurrency(t *testing.T) {
	fx := setupPGHTTPFixture(t)
	key := "pg-http-race-1"
	body := `{"statement":"SELECT id FROM app.orders ORDER BY id","database":"discdb","clientExecutionId":"` + key + `"}`

	var wg sync.WaitGroup
	codes := make([]int, 2)
	bodies := make([]string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rr := doBearerWithBody(t, fx.enabled, http.MethodPost, fx.execute, fx.token, body)
			codes[i], bodies[i] = rr.Code, rr.Body.String()
		}(i)
	}
	wg.Wait()

	ok, conflict := 0, 0
	for i, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			t.Fatalf("concurrent execute %d: status = %d; body=%s", i, c, bodies[i])
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatalf("same-key race outcomes = %d OK + %d conflict, want exactly 1+1", ok, conflict)
	}
	if execs, _, linked := claimExecCounts(t, fx.db, fx.targetID, key); execs != 1 || linked != 1 {
		t.Fatalf("race evidence = %d executions / %d links, want 1/1", execs, linked)
	}
}
