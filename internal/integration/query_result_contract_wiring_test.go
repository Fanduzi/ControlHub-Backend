//go:build integration

// Package integration provides real-MySQL HTTP proofs for the T8-B result-contract wiring.
// input: shared MySQL fixture, real api router, real execution/disclosure/evidence services, canonical manual-origin target fixtures
// output: TestResultContract* cases covering all three governed result routes — capability gate, byte boundaries, evidence ordering, disclosure, pagination
// pos: Real-DB HTTP acceptance boundary proving the per-cell truncation matrix flows from production scan through disclosure and evidence finalization to the serialized response on MySQL
// note: if this file changes, update this header and module README.md.
package integration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

const rcCredentialRef = "RC_TARGET"

// resultContractFixture is the fully wired HTTP boundary for the T8-B result
// contract: a real router over real execution/disclosure/evidence services
// against disposable MySQL, with an aux schema holding boundary cells.
type resultContractFixture struct {
	router   http.Handler
	token    string
	targetID uint64
	db       *sql.DB
}

// rcPost issues one authenticated JSON POST against the real router.
func (f resultContractFixture) rcPost(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doBearerWithBody(t, f.router, http.MethodPost, path, f.token, body)
}

func (f resultContractFixture) executePath() string {
	return fmt.Sprintf("/query-targets/%d/execute", f.targetID)
}

func (f resultContractFixture) savedExecutePath(statementID uint64) string {
	return fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", f.targetID, statementID)
}

// evidenceCount returns the number of history rows and audit events the
// attempt produced for this target, plus the latest recorded status and the
// latest audit event_type.
func (f resultContractFixture) evidenceCount(t *testing.T) (history, audit int, lastStatus, lastEvent string) {
	t.Helper()
	if err := f.db.QueryRow(`select count(*) from query_executions where target_resource_id = ?`, f.targetID).Scan(&history); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if err := f.db.QueryRow(`select count(*) from audit_events where target_resource_id = ? and event_type in ('query.executed','related_record_navigation')`, f.targetID).Scan(&audit); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	_ = f.db.QueryRow(`select status from query_executions where target_resource_id = ? order by id desc limit 1`, f.targetID).Scan(&lastStatus)
	_ = f.db.QueryRow(`select event_type from audit_events where target_resource_id = ? order by id desc limit 1`, f.targetID).Scan(&lastEvent)
	return history, audit, lastStatus, lastEvent
}

// requireSuccessPairs asserts exactly wantHistory/wantAudit evidence rows
// exist and the latest pair is a committed success of the given event type —
// a post-finalize refusal must not add a second pair per attempt or rewrite
// the committed outcome.
func (f resultContractFixture) requireSuccessPairs(t *testing.T, eventType string, wantHistory, wantAudit int) {
	t.Helper()
	history, audit, status, event := f.evidenceCount(t)
	if history != wantHistory || audit != wantAudit {
		t.Fatalf("evidence = %d history/%d audit, want %d/%d", history, audit, wantHistory, wantAudit)
	}
	if status != string(model.QueryExecutionSuccess) {
		t.Fatalf("recorded status = %q, want success", status)
	}
	if event != eventType {
		t.Fatalf("audit event = %q, want %q", event, eventType)
	}
}

// decodeRows decodes a success envelope into rows and the cellTruncated matrix.
func decodeRows(t *testing.T, rec *httptest.ResponseRecorder) (rows [][]any, matrix [][]bool) {
	t.Helper()
	var body struct {
		Rows          [][]any  `json:"rows"`
		CellTruncated [][]bool `json:"cellTruncated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode success body: %v — %s", err, rec.Body.String())
	}
	return body.Rows, body.CellTruncated
}

// decodeError decodes a controlled error envelope.
func decodeError(t *testing.T, rec *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var body struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v — %s", err, rec.Body.String())
	}
	return body.Error, body.Message
}

// requireUpgradeRefusal asserts the controlled 409 contract: the refusal
// code, no rows payload, and the expected committed success-pair totals —
// the refused attempt still wrote exactly one success pair before the gate.
func (f resultContractFixture) requireUpgradeRefusal(t *testing.T, rec *httptest.ResponseRecorder, eventType string, wantHistory, wantAudit int) {
	t.Helper()
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	code, _ := decodeError(t, rec)
	if code != "result_contract_upgrade_required" {
		t.Fatalf("error code = %q, want result_contract_upgrade_required", code)
	}
	if strings.Contains(rec.Body.String(), `"rows"`) {
		t.Fatalf("refusal leaked a rows payload: %s", rec.Body.String())
	}
	f.requireSuccessPairs(t, eventType, wantHistory, wantAudit)
}

// setupResultContractFixture provisions a ready mysql/staging target plus an
// aux schema carrying byte-boundary cells, an oversized FK-referenced row, a
// paged table whose only oversized row falls in the page+1 sentinel slot, and
// a masked column — then wires the real router over real services.
func setupResultContractFixture(t *testing.T) resultContractFixture {
	t.Helper()
	db := setupTestDB(t)

	adminID := insertAuthzTestUser(t, db, "rc-admin@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, adminID) })

	mustExec(t, db, "CREATE DATABASE IF NOT EXISTS query_e2e_aux")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e_aux.rc_child")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e_aux.rc_parent")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e_aux.rc_cells")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e_aux.rc_paged")
	mustExec(t, db, `CREATE TABLE query_e2e_aux.rc_cells (
		id BIGINT NOT NULL,
		pad MEDIUMTEXT CHARACTER SET utf8mb4,
		secret MEDIUMTEXT CHARACTER SET utf8mb4,
		note VARCHAR(64),
		free VARCHAR(16),
		PRIMARY KEY (id)
	) ENGINE=InnoDB`)
	mustExec(t, db, `CREATE TABLE query_e2e_aux.rc_parent (
		id BIGINT NOT NULL,
		parent_code VARCHAR(32) NOT NULL,
		pad MEDIUMTEXT CHARACTER SET utf8mb4,
		PRIMARY KEY (id),
		UNIQUE KEY uq_rc_parent_code (parent_code)
	) ENGINE=InnoDB`)
	mustExec(t, db, `CREATE TABLE query_e2e_aux.rc_child (
		id BIGINT NOT NULL,
		parent_id BIGINT NOT NULL,
		child_name VARCHAR(32) NOT NULL,
		PRIMARY KEY (id),
		CONSTRAINT fk_rc_child_parent FOREIGN KEY (parent_id) REFERENCES query_e2e_aux.rc_parent (id)
	) ENGINE=InnoDB`)
	mustExec(t, db, `CREATE TABLE query_e2e_aux.rc_paged (
		id BIGINT NOT NULL,
		pad MEDIUMTEXT CHARACTER SET utf8mb4,
		PRIMARY KEY (id)
	) ENGINE=InnoDB`)

	// Byte-boundary cells (B): exact 8192 bytes, 8193 bytes, CJK (3-byte) and
	// emoji (4-byte) crossing points, NULL/empty/"NULL" distinctness (G).
	insertCell := func(id int, pad, secret, note string) {
		mustExec(t, db, `INSERT INTO query_e2e_aux.rc_cells (id, pad, secret, note, free) VALUES (?,?,?,?, 'free')`, id, pad, secret, note)
	}
	insertCell(1, strings.Repeat("a", 8192), "s", "edge-8192")
	insertCell(2, strings.Repeat("b", 8193), strings.Repeat("s", 8193), "over-8193")
	insertCell(3, strings.Repeat("中", 2731), "s", "cjk-cross")
	insertCell(4, strings.Repeat("😀", 2049), "s", "emoji-cross")
	insertCell(5, strings.Repeat("中", 2730), "s", "cjk-8190")
	mustExec(t, db, `INSERT INTO query_e2e_aux.rc_cells (id, pad, secret, note, free) VALUES (6, '', 's', 'empty-str', 'free')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux.rc_cells (id, pad, secret, note, free) VALUES (7, NULL, 's', 'sql-null', 'free')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux.rc_cells (id, pad, secret, note, free) VALUES (8, 'NULL', 's', 'text-null', 'free')`)

	// Referenced parent: one row with an oversized pad for navigation.
	mustExec(t, db, `INSERT INTO query_e2e_aux.rc_parent (id, parent_code, pad) VALUES (1, 'P_ALPHA', ?)`, strings.Repeat("w", 9000))
	mustExec(t, db, `INSERT INTO query_e2e_aux.rc_parent (id, parent_code, pad) VALUES (2, 'P_BETA', 'short')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux.rc_child (id, parent_id, child_name) VALUES (1, 1, 'c1'), (2, 2, 'c2')`)

	// Paged table: delivered page rows 1..10 are clean; only the undelivered
	// page+1 sentinel row carries an oversized value (E).
	for i := 1; i <= 10; i++ {
		mustExec(t, db, `INSERT INTO query_e2e_aux.rc_paged (id, pad) VALUES (?, 'v')`, i)
	}
	mustExec(t, db, `INSERT INTO query_e2e_aux.rc_paged (id, pad) VALUES (11, ?)`, strings.Repeat("z", 9000))

	// Target resource + credential binding to the disposable container.
	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(t.Context(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "rc-target-" + t.Name(),
		DisplayName:     "Result Contract Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create rc target resource: %v", err)
	}
	dsnHost, dsnPort, err := service.ParseMySQLDSNHostPort(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse test dsn host/port: %v", err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'mysql', '8.0', ?, ?, 'primary', '{}')`, res.ID, dsnHost, dsnPort)
	seedCredentialRow(t, db, res.ID, "mysql", rcCredentialRef, true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+rcCredentialRef, globalEnv.dsn)

	// Disclosure policies: every projected column needs an explicit policy —
	// raw_copy_allowed except the masked 'secret' column; 'free' deliberately
	// has none (blocked-column proof).
	seedDisclosurePolicies(t, db, res.ID, "query_e2e_aux", "rc_cells", "id", "pad", "note")
	seedDisclosurePolicies(t, db, res.ID, "query_e2e_aux", "rc_parent", "id", "parent_code", "pad")
	seedDisclosurePolicies(t, db, res.ID, "query_e2e_aux", "rc_paged", "id", "pad")
	if _, err := mysql.NewQueryDisclosureRepository(db).Insert(t.Context(), model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: res.ID,
		DatabaseName:     "query_e2e_aux",
		ObjectName:       "rc_cells",
		ColumnName:       "secret",
		Mode:             model.ResultDisclosureMaskedNoCopy,
	}); err != nil {
		t.Fatalf("seed masked disclosure policy: %v", err)
	}

	// Real production assembly: real executor, real disclosure service/repo,
	// real evidence repository, real router — no executor fakes.
	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	savedStmtRepo := mysql.NewQuerySavedStatementRepository(db)
	credentialResolver := service.NewEnvCredentialResolver()
	guard := service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	executionSvc := service.NewQueryExecutionService(
		queryTargetRepo,
		queryExecutionRepo,
		credentialResolver,
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		guard,
		wallClock{},
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(
			mysql.NewQueryDisclosureRepository(db),
			mysql.NewQueryDisclosureRepository(db),
			service.NewMySQLSchemaInspector(),
			queryTargetRepo,
		),
	).WithTemplateExecution(savedStmtRepo, service.NewTemplateStatementCompiler())

	router := api.NewRouter(api.Dependencies{
		AuthService:                service.NewAuthService(mysql.NewUserRepository(db), authzIntegrationSecret),
		AuditService:               service.NewAuditService(mysql.NewAuditRepository(db)),
		QueryExecutionService:      executionSvc,
		QuerySavedStatementService: service.NewQuerySavedStatementService(savedStmtRepo, savedStmtRepo, queryTargetRepo, guard),
		MachineCredentialService:   service.NewMachinePrincipalService(mysql.NewMachinePrincipalRepository(db)),
		QueryExecutionAuth:         api.QueryExecutionAuthConfig{Clock: time.Now},
	})

	token := mustLogin(t, router, "rc-admin@example.com", "secret123")
	return resultContractFixture{router: router, token: token, targetID: res.ID, db: db}
}

// createSavedStatement provisions one personal saved statement for the
// fixture admin through the real create route, returning its id.
func (f resultContractFixture) createSavedStatement(t *testing.T) uint64 {
	t.Helper()
	body := `{"name":"rc-cells","statement":"select id, pad from query_e2e_aux.rc_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`
	rec := doBearerWithBody(t, f.router, http.MethodPost,
		fmt.Sprintf("/query-targets/%d/saved-statements", f.targetID), f.token, body)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create saved statement status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 {
		t.Fatalf("decode created statement: %v — %s", err, rec.Body.String())
	}
	return created.ID
}

// navRequestBody is the governed related-records body against the aux FK
// fixture (child parent_id=1 → parent row carrying the oversized pad).
func navRequestBody(capabilities string) string {
	caps := ""
	if capabilities != "" {
		caps = `,"capabilities":` + capabilities
	}
	return fmt.Sprintf(`{
		"source": {"database":"query_e2e_aux","object":"rc_child","kind":"table","foreignKey":"fk_rc_child_parent"},
		"localValues": ["1"]%s
	}`, caps)
}

// ---------------------------------------------------------------------------
// A. Per-route capability gate over real HTTP + real scan
// ---------------------------------------------------------------------------

func TestResultContractHTTP_Execute_CapabilityGate(t *testing.T) {
	fx := setupResultContractFixture(t)

	// Clean page, no capability → 200 with an explicit all-false matrix.
	rec := fx.rcPost(t, fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_cells where id = 1"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("clean status = %d; body=%s", rec.Code, rec.Body.String())
	}
	rows, matrix := decodeRows(t, rec)
	if len(rows) != 1 || len(matrix) != 1 || len(matrix[0]) != 2 || matrix[0][0] || matrix[0][1] {
		t.Fatalf("clean page rows=%v matrix=%v, want 1x2 all-false", len(rows), matrix)
	}
	if got := rows[0][1].(string); len(got) != 8192 {
		t.Fatalf("8192-byte cell retained %d bytes", len(got))
	}
	fx.requireSuccessPairs(t, "query.executed", 1, 1)

	// Oversized page without capability → 409 refusal carrying no rows. The
	// gate runs after finalize, so the refusal attempt's own pair is still a
	// single committed success — two attempts, two pairs, nothing extra.
	rec = fx.rcPost(t, fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_cells where id = 2"}`)
	fx.requireUpgradeRefusal(t, rec, "query.executed", 2, 2)

	// Unknown capability never satisfies the gate.
	rec = fx.rcPost(t, fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_cells where id = 2","capabilities":["csvExport"]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("unknown capability status = %d, want 409", rec.Code)
	}

	// Exact capability → 200 with the safe prefix and the true flag.
	rec = fx.rcPost(t, fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_cells where id = 2","capabilities":["cellTruncated"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("capable status = %d; body=%s", rec.Code, rec.Body.String())
	}
	rows, matrix = decodeRows(t, rec)
	if len(rows) != 1 || len(matrix) != 1 || !matrix[0][1] {
		t.Fatalf("capable page matrix=%v, want flag true on pad", matrix)
	}
	pad := rows[0][1].(string)
	if len(pad) != 8192 || !utf8.ValidString(pad) || !strings.HasPrefix(strings.Repeat("b", 8193), pad) {
		t.Fatalf("retained %d bytes valid=%v, want the exact 8192-byte prefix", len(pad), utf8.ValidString(pad))
	}

	// Duplicate capability declaration behaves like a single one.
	rec = fx.rcPost(t, fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_cells where id = 2","capabilities":["cellTruncated","cellTruncated"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate capability status = %d, want 200", rec.Code)
	}
}

func TestResultContractHTTP_SavedStatement_CapabilityGate(t *testing.T) {
	fx := setupResultContractFixture(t)
	stmtID := fx.createSavedStatement(t)
	path := fx.savedExecutePath(stmtID)

	rec := fx.rcPost(t, path, `{"values":{"cell_id":2}}`)
	fx.requireUpgradeRefusal(t, rec, "query.executed", 1, 1)

	rec = fx.rcPost(t, path, `{"values":{"cell_id":2},"capabilities":["cellTruncated"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("capable status = %d; body=%s", rec.Code, rec.Body.String())
	}
	rows, matrix := decodeRows(t, rec)
	if len(rows) != 1 || len(matrix) != 1 || !matrix[0][1] {
		t.Fatalf("template page matrix=%v, want flag true", matrix)
	}
	if got := rows[0][1].(string); len(got) != 8192 || !utf8.ValidString(got) {
		t.Fatalf("template retained %d bytes valid=%v", len(got), utf8.ValidString(got))
	}
	history, audit, status, _ := fx.evidenceCount(t)
	if status != string(model.QueryExecutionSuccess) || audit != history {
		t.Fatalf("template evidence mismatch: %d/%d %q", history, audit, status)
	}
}

func TestResultContractHTTP_Navigation_CapabilityGate(t *testing.T) {
	fx := setupResultContractFixture(t)
	path := fmt.Sprintf("/query-targets/%d/related-records", fx.targetID)

	rec := fx.rcPost(t, path, navRequestBody(""))
	fx.requireUpgradeRefusal(t, rec, "related_record_navigation", 1, 1)

	rec = fx.rcPost(t, path, navRequestBody(`["cellTruncated"]`))
	if rec.Code != http.StatusOK {
		t.Fatalf("capable status = %d; body=%s", rec.Code, rec.Body.String())
	}
	rows, matrix := decodeRows(t, rec)
	if len(rows) != 1 || len(matrix) != 1 {
		t.Fatalf("nav rows=%d matrix=%v, want one aligned row", len(rows), matrix)
	}
	// pad is the third projected column of rc_parent (id, parent_code, pad).
	if !matrix[0][2] {
		t.Fatalf("nav matrix=%v, want flag true on pad", matrix[0])
	}
	if got := rows[0][2].(string); len(got) != 8192 || !utf8.ValidString(got) {
		t.Fatalf("nav retained %d bytes valid=%v", len(got), utf8.ValidString(got))
	}
}

// ---------------------------------------------------------------------------
// B. Byte boundaries through the real scan → response path
// ---------------------------------------------------------------------------

func TestResultContractHTTP_ByteBoundaries(t *testing.T) {
	fx := setupResultContractFixture(t)

	for _, tc := range []struct {
		name       string
		id         int
		wantLen    int
		wantTrunc  bool
		wantPrefix string
	}{
		{"exact-8192-ascii", 1, 8192, false, ""},
		{"over-8193-ascii", 2, 8192, true, strings.Repeat("b", 8192)},
		{"cjk-2731-cross", 3, 8190, true, strings.Repeat("中", 2730)},
		{"emoji-2049-cross", 4, 8192, true, strings.Repeat("😀", 2048)},
		{"cjk-2730-8190-clean", 5, 8190, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := fx.rcPost(t, fx.executePath(), fmt.Sprintf(`{"statement":"select pad from query_e2e_aux.rc_cells where id = %d","capabilities":["cellTruncated"]}`, tc.id))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
			}
			rows, matrix := decodeRows(t, rec)
			if len(rows) != 1 || len(matrix) != 1 || len(matrix[0]) != 1 {
				t.Fatalf("shape rows=%d matrix=%v", len(rows), matrix)
			}
			if matrix[0][0] != tc.wantTrunc {
				t.Fatalf("flag = %v, want %v", matrix[0][0], tc.wantTrunc)
			}
			got := rows[0][0].(string)
			if len(got) != tc.wantLen || !utf8.ValidString(got) {
				t.Fatalf("retained %d bytes valid=%v, want %d valid bytes", len(got), utf8.ValidString(got), tc.wantLen)
			}
			if tc.wantPrefix != "" && got != tc.wantPrefix {
				t.Fatal("retained value is not the exact original prefix")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// D. Evidence persistence failure wins over the capability gate (real trigger)
// ---------------------------------------------------------------------------

func TestResultContractHTTP_EvidenceFailureBeatsGate(t *testing.T) {
	fx := setupResultContractFixture(t)
	stmtID := fx.createSavedStatement(t)

	mustExec(t, fx.db, `create trigger ch_t8b_force_audit_fail
		before insert on audit_events for each row
		signal sqlstate '45000' set message_text = 'forced audit failure'`)
	t.Cleanup(func() { _, _ = fx.db.Exec(`drop trigger if exists ch_t8b_force_audit_fail`) })

	h0, a0, _, _ := fx.evidenceCount(t)

	for name, tc := range map[string]struct {
		path string
		body string
	}{
		"execute":  {fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_cells where id = 2"}`},
		"template": {fx.savedExecutePath(stmtID), `{"values":{"cell_id":2}}`},
		"navigate": {fmt.Sprintf("/query-targets/%d/related-records", fx.targetID), navRequestBody("")},
	} {
		t.Run(name, func(t *testing.T) {
			rec := fx.rcPost(t, tc.path, tc.body)
			if rec.Code != http.StatusBadGateway {
				t.Fatalf("status = %d, want 502 evidence failure not 409; body=%s", rec.Code, rec.Body.String())
			}
			code, _ := decodeError(t, rec)
			if code != "query_backend_error" {
				t.Fatalf("error code = %q, want query_backend_error", code)
			}
			if strings.Contains(rec.Body.String(), `"rows"`) || strings.Contains(rec.Body.String(), "forced audit") {
				t.Fatalf("response leaked rows or the injected marker: %s", rec.Body.String())
			}
		})
	}

	// The atomic pair rolls back completely: no partial history, no audit.
	h1, a1, _, _ := fx.evidenceCount(t)
	if h1 != h0 || a1 != a0 {
		t.Fatalf("partial evidence landed: %d→%d history, %d→%d audit", h0, h1, a0, a1)
	}
}

// ---------------------------------------------------------------------------
// E. Undelivered sentinel row must not poison the clean page
// ---------------------------------------------------------------------------

func TestResultContractHTTP_PaginatedSentinelNotCounted(t *testing.T) {
	fx := setupResultContractFixture(t)

	rec := fx.rcPost(t, fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_paged order by id","pagination":{"page":1,"pageSize":10}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 — the undelivered oversized sentinel row must not trigger the gate; body=%s", rec.Code, rec.Body.String())
	}
	rows, matrix := decodeRows(t, rec)
	if len(rows) != 10 {
		t.Fatalf("delivered %d rows, want the 10-row page", len(rows))
	}
	if len(matrix) != 10 {
		t.Fatalf("matrix = %d rows, want exactly the delivered 10", len(matrix))
	}
	for i := range matrix {
		for j := range matrix[i] {
			if matrix[i][j] {
				t.Fatalf("matrix[%d][%d] true on a clean page", i, j)
			}
		}
	}
	var body struct {
		Truncated  bool `json:"truncated"`
		Pagination struct {
			HasNextPage bool `json:"hasNextPage"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Truncated || !body.Pagination.HasNextPage {
		t.Fatalf("top-level truncated=%v hasNext=%v, want true/true (sentinel semantics preserved)", body.Truncated, body.Pagination.HasNextPage)
	}
}

// ---------------------------------------------------------------------------
// F. Disclosure: masking keeps the flag, capability never unlocks copying
// ---------------------------------------------------------------------------

func TestResultContractHTTP_MaskedColumnKeepsFlagAndCap(t *testing.T) {
	fx := setupResultContractFixture(t)

	// Masked oversized cell still requires the capability — masking is a
	// display transformation, not truncation-evidence clearing.
	rec := fx.rcPost(t, fx.executePath(), `{"statement":"select id, secret from query_e2e_aux.rc_cells where id = 2"}`)
	fx.requireUpgradeRefusal(t, rec, "query.executed", 1, 1)

	rec = fx.rcPost(t, fx.executePath(), `{"statement":"select id, secret from query_e2e_aux.rc_cells where id = 2","capabilities":["cellTruncated"]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("capable masked status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Columns []struct {
			Name        string `json:"name"`
			DisplayMode string `json:"displayMode"`
			CopyAllowed bool   `json:"copyAllowed"`
		} `json:"columns"`
		Rows          [][]any  `json:"rows"`
		CellTruncated [][]bool `json:"cellTruncated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Rows[0][1].(string) != "[MASKED]" {
		t.Fatalf("masked cell = %v, want [MASKED]", body.Rows[0][1])
	}
	// The flag describes the scanned value, not the masked display string.
	if !body.CellTruncated[0][1] {
		t.Fatalf("masked cell lost its truncation flag: %v", body.CellTruncated)
	}
	if body.Columns[1].CopyAllowed || body.Columns[1].DisplayMode != string(model.ResultDisclosureMaskedNoCopy) {
		t.Fatalf("capability unlocked copy permission: %+v", body.Columns[1])
	}
}

func TestResultContractHTTP_BlockedColumnStaysDisclosureBlocked(t *testing.T) {
	fx := setupResultContractFixture(t)

	// 'free' deliberately has no policy row: the disclosure refusal keeps its
	// own 403 semantics — it must not become a success-then-409 sequence.
	rec := fx.rcPost(t, fx.executePath(), `{"statement":"select id, free from query_e2e_aux.rc_cells where id = 1","capabilities":["cellTruncated"]}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 disclosure blocked; body=%s", rec.Code, rec.Body.String())
	}
	code, _ := decodeError(t, rec)
	if code != "query_result_disclosure_blocked" {
		t.Fatalf("error code = %q, want query_result_disclosure_blocked", code)
	}
	history, _, status, _ := fx.evidenceCount(t)
	if history != 1 || status != string(model.QueryExecutionRejected) {
		t.Fatalf("evidence = %d rows status %q, want one rejected attempt", history, status)
	}
}

// ---------------------------------------------------------------------------
// G. Compatibility: native types, NULL/empty/"NULL" distinction, closed fields
// ---------------------------------------------------------------------------

func TestResultContractHTTP_NativeTypesAndNullSemantics(t *testing.T) {
	fx := setupResultContractFixture(t)

	rec := fx.rcPost(t, fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_cells where id in (6,7,8) order by id"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Rows [][]any `json:"rows"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(body.Rows))
	}
	// id is a BIGINT — JSON must carry a number, not a string.
	if _, ok := body.Rows[0][0].(float64); !ok {
		t.Fatalf("id decoded as %T, want JSON number", body.Rows[0][0])
	}
	if body.Rows[0][1].(string) != "" {
		t.Fatalf("empty string = %q, want ''", body.Rows[0][1])
	}
	if body.Rows[1][1] != nil {
		t.Fatalf("SQL NULL = %v, want null", body.Rows[1][1])
	}
	if body.Rows[2][1].(string) != "NULL" {
		t.Fatalf("text NULL = %v, want 'NULL'", body.Rows[2][1])
	}
}

func TestResultContractHTTP_ZeroRowPageContract(t *testing.T) {
	fx := setupResultContractFixture(t)
	rec := fx.rcPost(t, fx.executePath(), `{"statement":"select id, pad from query_e2e_aux.rc_cells where id = 999"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rec.Code, rec.Body.String())
	}
	// The approved empty-page contract: zero rows, no residual matrix.
	if strings.Contains(rec.Body.String(), `"cellTruncated":`) {
		t.Fatalf("zero-row page carried a residual matrix: %s", rec.Body.String())
	}
	var body struct {
		RowCount int `json:"rowCount"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.RowCount != 0 {
		t.Fatalf("rowCount = %d, want 0", body.RowCount)
	}
}

func TestResultContractHTTP_ClientExecutionIDStaysClosed(t *testing.T) {
	fx := setupResultContractFixture(t)
	rec := fx.rcPost(t, fx.executePath(), `{"statement":"select id from query_e2e_aux.rc_cells where id = 1","clientExecutionId":"abc-123"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 — the claim protocol stays closed; body=%s", rec.Code, rec.Body.String())
	}
}
