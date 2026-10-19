//go:build integration

// Package integration — real-MySQL HTTP proof for keyed MySQL/TiDB ordinary, saved-statement, and related-navigation execution.
// input: shared MySQL fixture, real api router, real auth/repository/disclosure/executor stack, a clientExecutionId key
// output: TestMySQLKeyedHTTP_{Execute,Saved,Navigation} — one keyed golden-path execution each, claim-backed terminal evidence, 409 replay/conflict, keyless compatibility
// pos: Real-DB HTTP acceptance boundary proving the keyed claim protocol occupies exactly one claim per entry, finalizes exactly one keyed terminal row with empty MySQL/TiDB connection scope, and leaves the legacy keyless path untouched
// note: if this file changes, update this header and module README.md.
package integration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

// mysqlKeyedScopeCounts mirrors the four-scope target counts in
// mysql-keyed evidence-queries.sql: history executions, execution-family
// audit rows, claim rows, and claim-linked executions for one target.
type mysqlKeyedScopeCounts struct {
	Executions int
	Audits     int
	Claims     int
	Linked     int
}

// mysqlTargetCounts runs the canonical four-scope target count — the
// verbatim scalar-subquery recipe from evidence-queries.sql.
func mysqlTargetCounts(t *testing.T, db *sql.DB, targetID uint64) mysqlKeyedScopeCounts {
	t.Helper()
	var c mysqlKeyedScopeCounts
	if err := db.QueryRow(`SELECT
  (SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?) AS history_count,
  (SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type IN ('query.executed', 'related_record_navigation')) AS execution_audit_count,
  (SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ?) AS claim_count,
  (SELECT COUNT(*) FROM query_execution_claims WHERE target_resource_id = ? AND execution_id IS NOT NULL) AS linked_claim_count`,
		targetID, targetID, targetID, targetID).
		Scan(&c.Executions, &c.Audits, &c.Claims, &c.Linked); err != nil {
		t.Fatalf("target counts: %v", err)
	}
	return c
}

// assertKeyedLinkedRow reads the canonical claim+execution JOIN for one key
// (the evidence-queries.sql single-view recipe) and asserts the MySQL/TiDB scope:
// connection slots plain empty, attempt budget recorded at admission, no
// backend PID / remote state, and the typed actor matching the
// authenticated user on both rows.
func assertKeyedLinkedRow(t *testing.T, db *sql.DB, targetID uint64, key string, wantTimeoutMs, wantActorUserID int64) {
	t.Helper()
	var (
		execID           int64
		execActorUser    int64
		execActorMachine sql.NullInt64
		execEngine       string
		execDB           string
		execSchema       string
		execStatus       string
		execErrorCode    sql.NullString
		execBackendPID   sql.NullInt64
		execRemoteState  sql.NullString
		execClientKey    sql.NullString
		claimExecID      sql.NullInt64
		claimDB          string
		claimSchema      string
		claimTimeoutMs   sql.NullInt64
		claimActorUser   int64
		claimActorMach   sql.NullInt64
	)
	if err := db.QueryRow(`SELECT e.id, e.actor_user_id, e.actor_machine_principal_id, e.engine,
       e.database_name, e.schema_name, e.status, e.error_code,
       e.backend_pid, e.remote_state, e.client_execution_id,
       c.execution_id, c.database_name, c.schema_name, c.attempt_timeout_ms,
       c.actor_user_id, c.actor_machine_principal_id
FROM query_execution_claims c
LEFT JOIN query_executions e ON e.id = c.execution_id
WHERE c.target_resource_id = ? AND c.client_execution_id = ?`,
		targetID, key).
		Scan(&execID, &execActorUser, &execActorMachine, &execEngine,
			&execDB, &execSchema, &execStatus, &execErrorCode,
			&execBackendPID, &execRemoteState, &execClientKey,
			&claimExecID, &claimDB, &claimSchema, &claimTimeoutMs,
			&claimActorUser, &claimActorMach); err != nil {
		t.Fatalf("claim+execution join read: %v", err)
	}
	if !claimExecID.Valid || claimExecID.Int64 != execID || execID <= 0 {
		t.Fatalf("claim not linked to its terminal execution: claim.execution_id=%v e.id=%d", claimExecID, execID)
	}
	if claimDB != "" || claimSchema != "" {
		t.Fatalf("claim connection scope must stay empty for MySQL/TiDB, got (%q,%q)", claimDB, claimSchema)
	}
	if !claimTimeoutMs.Valid || claimTimeoutMs.Int64 != wantTimeoutMs {
		t.Fatalf("claim attempt_timeout_ms = %v, want %d", claimTimeoutMs, wantTimeoutMs)
	}
	if execDB != "" || execSchema != "" {
		t.Fatalf("linked execution connection scope must stay empty for MySQL/TiDB, got (%q,%q)", execDB, execSchema)
	}
	if execBackendPID.Valid {
		t.Fatalf("backend_pid must be NULL for MySQL/TiDB, got %d", execBackendPID.Int64)
	}
	if execRemoteState.Valid && execRemoteState.String != "" {
		t.Fatalf("remote_state must stay empty for MySQL/TiDB, got %q", execRemoteState.String)
	}
	if execStatus != string(model.QueryExecutionSuccess) {
		t.Fatalf("linked execution status = %q, want success", execStatus)
	}
	if !execClientKey.Valid || execClientKey.String != key {
		t.Fatalf("linked execution must carry the occupied key %q, got %v", key, execClientKey)
	}
	if execActorUser != wantActorUserID || execActorMachine.Valid {
		t.Fatalf("execution actor must be the authenticated user %d (machine NULL): user=%d machine=%v",
			wantActorUserID, execActorUser, execActorMachine)
	}
	if claimActorUser != wantActorUserID || claimActorMach.Valid {
		t.Fatalf("claim actor must be the authenticated user %d (machine NULL): user=%d machine=%v",
			wantActorUserID, claimActorUser, claimActorMach)
	}
}

func TestMySQLKeyedHTTP_Execute(t *testing.T) {
	fix := setupResultContractFixture(t)
	key := "mysql-golden-1"
	execURL := fmt.Sprintf("/query-targets/%d/execute", fix.targetID)
	listURL := fmt.Sprintf("/query-targets/%d/executions?clientExecutionId=%s", fix.targetID, key)
	body := `{"statement":"select id,note from query_e2e_aux.rc_cells where id = 1","clientExecutionId":"` + key + `"}`

	// The bearer token's authenticated user is the actor both rows must name.
	var actorUserID int64
	if err := fix.db.QueryRow(`SELECT id FROM users WHERE email = 'rc-admin@example.com'`).Scan(&actorUserID); err != nil {
		t.Fatalf("fixture user lookup: %v", err)
	}

	// Golden keyed execute: a real terminal execution carries the occupied key.
	rr := doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("keyed execute: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		ExecutionID   uint64   `json:"executionId"`
		Status        string   `json:"status"`
		Rows          [][]any  `json:"rows"`
		CellTruncated [][]bool `json:"cellTruncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode execute response: %v", err)
	}
	if resp.ExecutionID == 0 || resp.Status != "success" {
		t.Fatalf("keyed execute response: executionId=%v status=%v", resp.ExecutionID, resp.Status)
	}
	if len(resp.Rows) != 1 {
		t.Fatalf("keyed execute rows: %v", resp.Rows)
	}
	if len(resp.Rows[0]) != 2 || resp.Rows[0][0] != float64(1) || resp.Rows[0][1] != "edge-8192" {
		t.Fatalf("keyed execute row payload: %v", resp.Rows[0])
	}
	if len(resp.CellTruncated) != 1 || len(resp.CellTruncated[0]) != 2 ||
		resp.CellTruncated[0][0] || resp.CellTruncated[0][1] {
		t.Fatalf("clean id/note row must carry two false flags, got %v", resp.CellTruncated)
	}

	// Claim-aware point read: exactly one terminal row with the same ID.
	rr = doBearer(t, fix.router, http.MethodGet, listURL, fix.token)
	if rr.Code != http.StatusOK {
		t.Fatalf("executions list: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("claim point read returned %d items, want 1: %v", len(listed.Items), listed.Items)
	}
	item := listed.Items[0]
	if item["kind"] != "execution" || item["id"] != float64(resp.ExecutionID) || item["status"] != "success" {
		t.Fatalf("claim point read item: %v", item)
	}

	// Persisted evidence: one history row, one audit row, one claim, one link.
	counts := mysqlTargetCounts(t, fix.db, fix.targetID)
	if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
		t.Fatalf("keyed scope counts = %+v, want {1 1 1 1}", counts)
	}
	assertKeyedLinkedRow(t, fix.db, fix.targetID, key, 5000, actorUserID)

	// Same-key same-request retry: 409 already-exists, no new evidence.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, body)
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-key replay: got %d want 409: %s", rr.Code, rr.Body.String())
	}
	var errBody struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode replay error: %v", err)
	}
	if errBody.Error != "execution_already_exists" {
		t.Fatalf("same-key replay code = %q, want execution_already_exists", errBody.Error)
	}
	if counts2 := mysqlTargetCounts(t, fix.db, fix.targetID); counts2 != counts {
		t.Fatalf("replay wrote new evidence: %+v -> %+v", counts, counts2)
	}

	// Same-key different request: 409 conflict, no new evidence.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
		`{"statement":"select id,note from query_e2e_aux.rc_cells where id = 2","clientExecutionId":"`+key+`"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-key different digest: got %d want 409: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode conflict error: %v", err)
	}
	if errBody.Error != "client_execution_id_conflict" {
		t.Fatalf("same-key different digest code = %q, want client_execution_id_conflict", errBody.Error)
	}
	if counts3 := mysqlTargetCounts(t, fix.db, fix.targetID); counts3 != counts {
		t.Fatalf("conflict wrote new evidence: %+v -> %+v", counts, counts3)
	}

	// Keyless compatibility on the same target: absent key and explicit
	// empty key both run the legacy path — executions and audits land, the
	// claim count does not move.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
		`{"statement":"select id,note from query_e2e_aux.rc_cells where id = 1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("keyless absent-key execute: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	rr = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
		`{"statement":"select id,note from query_e2e_aux.rc_cells where id = 1","clientExecutionId":""}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("keyless empty-key execute: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	if final := mysqlTargetCounts(t, fix.db, fix.targetID); final != (mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 1, Linked: 1}) {
		t.Fatalf("post-keyless target counts = %+v, want {3 3 1 1}", final)
	}
}

// createKeyedSavedStatement provisions one personal saved statement with the
// given body through the real create route and returns its id.
func createKeyedSavedStatement(t *testing.T, fix resultContractFixture, body string) uint64 {
	t.Helper()
	rec := doBearerWithBody(t, fix.router, http.MethodPost,
		fmt.Sprintf("/query-targets/%d/saved-statements", fix.targetID), fix.token, body)
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

func TestMySQLKeyedHTTP_Saved(t *testing.T) {
	fix := setupResultContractFixture(t)
	key := "mysql-saved-1"

	var actorUserID int64
	if err := fix.db.QueryRow(`SELECT id FROM users WHERE email = 'rc-admin@example.com'`).Scan(&actorUserID); err != nil {
		t.Fatalf("fixture user lookup: %v", err)
	}

	stmtID := createKeyedSavedStatement(t, fix,
		`{"name":"rc-cells-keyed","statement":"select id, note from query_e2e_aux.rc_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
	execURL := fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", fix.targetID, stmtID)
	listURL := fmt.Sprintf("/query-targets/%d/executions?clientExecutionId=%s", fix.targetID, key)
	body := `{"values":{"cell_id":1},"clientExecutionId":"` + key + `"}`

	// Golden keyed saved execute: typed value 1 binds :cell_id, clean row.
	rr := doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, body)
	if rr.Code != http.StatusOK {
		t.Fatalf("keyed saved execute: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		ExecutionID   uint64   `json:"executionId"`
		Status        string   `json:"status"`
		Rows          [][]any  `json:"rows"`
		CellTruncated [][]bool `json:"cellTruncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode saved execute response: %v", err)
	}
	if resp.ExecutionID == 0 || resp.Status != "success" {
		t.Fatalf("keyed saved response: executionId=%v status=%v", resp.ExecutionID, resp.Status)
	}
	if len(resp.Rows) != 1 || len(resp.Rows[0]) != 2 || resp.Rows[0][0] != float64(1) || resp.Rows[0][1] != "edge-8192" {
		t.Fatalf("keyed saved row payload: %v", resp.Rows)
	}
	if len(resp.CellTruncated) != 1 || len(resp.CellTruncated[0]) != 2 ||
		resp.CellTruncated[0][0] || resp.CellTruncated[0][1] {
		t.Fatalf("clean id/note row must carry two false flags, got %v", resp.CellTruncated)
	}

	// Claim-aware point read: exactly one terminal row with the same ID.
	rr = doBearer(t, fix.router, http.MethodGet, listURL, fix.token)
	if rr.Code != http.StatusOK {
		t.Fatalf("executions list: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0]["kind"] != "execution" ||
		listed.Items[0]["id"] != float64(resp.ExecutionID) || listed.Items[0]["status"] != "success" {
		t.Fatalf("claim point read items: %v", listed.Items)
	}

	counts := mysqlTargetCounts(t, fix.db, fix.targetID)
	if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
		t.Fatalf("keyed saved scope counts = %+v, want {1 1 1 1}", counts)
	}
	assertKeyedLinkedRow(t, fix.db, fix.targetID, key, 5000, actorUserID)

	// Same-key same-request replay: 409 already-exists, no new evidence.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, body)
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-key saved replay: got %d want 409: %s", rr.Code, rr.Body.String())
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode saved replay error: %v", err)
	}
	if errBody.Error != "execution_already_exists" {
		t.Fatalf("same-key saved replay code = %q, want execution_already_exists", errBody.Error)
	}
	if counts2 := mysqlTargetCounts(t, fix.db, fix.targetID); counts2 != counts {
		t.Fatalf("replay wrote new evidence: %+v -> %+v", counts, counts2)
	}

	// Same key with a different typed value: 409 conflict, no new evidence.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
		`{"values":{"cell_id":2},"clientExecutionId":"`+key+`"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-key different typed value: got %d want 409: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode saved conflict error: %v", err)
	}
	if errBody.Error != "client_execution_id_conflict" {
		t.Fatalf("same-key different typed value code = %q, want client_execution_id_conflict", errBody.Error)
	}
	if counts3 := mysqlTargetCounts(t, fix.db, fix.targetID); counts3 != counts {
		t.Fatalf("conflict wrote new evidence: %+v -> %+v", counts, counts3)
	}

	// Keyless compatibility: missing and empty keys run the legacy pair path.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
		`{"values":{"cell_id":1}}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("keyless saved execute: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	rr = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
		`{"values":{"cell_id":1},"clientExecutionId":""}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("empty-key saved execute: got %d want 200: %s", rr.Code, rr.Body.String())
	}

	// A separate static saved statement (no parameters) keys and succeeds.
	staticID := createKeyedSavedStatement(t, fix,
		`{"name":"rc-cells-static","statement":"select id, note from query_e2e_aux.rc_cells where id = 1","scope":"personal"}`)
	rr = doBearerWithBody(t, fix.router, http.MethodPost,
		fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", fix.targetID, staticID), fix.token,
		`{"clientExecutionId":"mysql-saved-static-1"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("keyed static saved execute: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	if final := mysqlTargetCounts(t, fix.db, fix.targetID); final != (mysqlKeyedScopeCounts{Executions: 4, Audits: 4, Claims: 2, Linked: 2}) {
		t.Fatalf("post-keyless target counts = %+v, want {4 4 2 2}", final)
	}
	assertKeyedLinkedRow(t, fix.db, fix.targetID, "mysql-saved-static-1", 5000, actorUserID)
}

func TestMySQLKeyedHTTP_Navigation(t *testing.T) {
	fix := setupResultContractFixture(t)
	key := "mysql-nav-1"
	navURL := fmt.Sprintf("/query-targets/%d/related-records", fix.targetID)
	listURL := fmt.Sprintf("/query-targets/%d/executions?clientExecutionId=%s", fix.targetID, key)
	navBody := func(localValue, clientKey string) string {
		return fmt.Sprintf(`{"source":{"database":"query_e2e_aux","object":"rc_child","kind":"table","foreignKey":"fk_rc_child_parent"},"localValues":[%q],"clientExecutionId":%q}`, localValue, clientKey)
	}

	var actorUserID int64
	if err := fix.db.QueryRow(`SELECT id FROM users WHERE email = 'rc-admin@example.com'`).Scan(&actorUserID); err != nil {
		t.Fatalf("fixture user lookup: %v", err)
	}

	// Golden keyed navigation: localValue "2" resolves to the clean P_BETA
	// parent row — SELECT * carries the three trusted columns.
	rr := doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody("2", key))
	if rr.Code != http.StatusOK {
		t.Fatalf("keyed navigation: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		ExecutionID   uint64   `json:"executionId"`
		Status        string   `json:"status"`
		Rows          [][]any  `json:"rows"`
		CellTruncated [][]bool `json:"cellTruncated"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode navigation response: %v", err)
	}
	if resp.ExecutionID == 0 || resp.Status != "success" {
		t.Fatalf("keyed navigation response: executionId=%v status=%v", resp.ExecutionID, resp.Status)
	}
	if len(resp.Rows) != 1 || len(resp.Rows[0]) != 3 || resp.Rows[0][0] != float64(2) || resp.Rows[0][1] != "P_BETA" {
		t.Fatalf("keyed navigation row payload: %v", resp.Rows)
	}
	if len(resp.CellTruncated) != 1 || len(resp.CellTruncated[0]) != 3 ||
		resp.CellTruncated[0][0] || resp.CellTruncated[0][1] || resp.CellTruncated[0][2] {
		t.Fatalf("clean parent row must carry three false flags, got %v", resp.CellTruncated)
	}

	// Claim-aware point read: exactly one terminal row with the same ID.
	rr = doBearer(t, fix.router, http.MethodGet, listURL, fix.token)
	if rr.Code != http.StatusOK {
		t.Fatalf("executions list: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Items) != 1 || listed.Items[0]["kind"] != "execution" ||
		listed.Items[0]["id"] != float64(resp.ExecutionID) || listed.Items[0]["status"] != "success" {
		t.Fatalf("claim point read items: %v", listed.Items)
	}

	counts := mysqlTargetCounts(t, fix.db, fix.targetID)
	if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
		t.Fatalf("keyed navigation scope counts = %+v, want {1 1 1 1}", counts)
	}
	assertKeyedLinkedRow(t, fix.db, fix.targetID, key, 5000, actorUserID)

	// Same-key same-request replay: 409 already-exists, no new evidence.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody("2", key))
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-key navigation replay: got %d want 409: %s", rr.Code, rr.Body.String())
	}
	var errBody struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode navigation replay error: %v", err)
	}
	if errBody.Error != "execution_already_exists" {
		t.Fatalf("same-key navigation replay code = %q, want execution_already_exists", errBody.Error)
	}
	if counts2 := mysqlTargetCounts(t, fix.db, fix.targetID); counts2 != counts {
		t.Fatalf("replay wrote new evidence: %+v -> %+v", counts, counts2)
	}

	// Same key with a changed locator: 409 conflict before any oversized SQL.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody("1", key))
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-key changed locator: got %d want 409: %s", rr.Code, rr.Body.String())
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("decode navigation conflict error: %v", err)
	}
	if errBody.Error != "client_execution_id_conflict" {
		t.Fatalf("same-key changed locator code = %q, want client_execution_id_conflict", errBody.Error)
	}
	if counts3 := mysqlTargetCounts(t, fix.db, fix.targetID); counts3 != counts {
		t.Fatalf("conflict wrote new evidence: %+v -> %+v", counts, counts3)
	}

	// Keyless compatibility: missing and empty keys run the legacy pair path.
	rr = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token,
		`{"source":{"database":"query_e2e_aux","object":"rc_child","kind":"table","foreignKey":"fk_rc_child_parent"},"localValues":["2"]}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("keyless navigation: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	rr = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody("2", ""))
	if rr.Code != http.StatusOK {
		t.Fatalf("empty-key navigation: got %d want 200: %s", rr.Code, rr.Body.String())
	}
	if final := mysqlTargetCounts(t, fix.db, fix.targetID); final != (mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 1, Linked: 1}) {
		t.Fatalf("post-keyless target counts = %+v, want {3 3 1 1}", final)
	}
}
