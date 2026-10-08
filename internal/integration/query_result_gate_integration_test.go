//go:build integration

// Package integration provides real-MySQL HTTP coverage for the T8-B result
// contract gate on all three governed result paths.
// input: bytes, database/sql, encoding/json, fmt, io, net/http, strings, testing, internal/model, internal/repository/mysql, internal/service
// output: TestResultGateIntegration_* cases
// pos: Proves execute / saved-statements/execute / related-records deliver the cellTruncated contract end-to-end: post-finalize refusal keeps exactly one committed success pair, persistence failure wins over refusal, and masked cells never strand a truncation flag
// note: if this file changes, update header and README.md
package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// t8bOversizedPayload / t8bFullPayload are the fixed first-path fixture values:
// one byte over the cell cap (truncates) and exactly at the cap (clean).
const (
	t8bOversizedPayloadBytes = service.ResultCellMaxBytes + 1 // 8193
	t8bFullPayloadBytes      = service.ResultCellMaxBytes     // 8192
)

// t8bExecuteBody is the fixed governed statement for the ordinary execute path.
const t8bExecuteBody = `{"statement":"select payload from t8_b_payload where id = 1"}`

// seedT8BTables creates the dedicated fixture objects for this slice on the
// fixture target: one oversized payload row, one boundary row, one masked table
// (payload masked_no_copy), and an FK pair for the related-records path.
// Database objects and policies are test-provisioned — never request data.
func seedT8BTables(t *testing.T, db *sql.DB, targetID uint64) {
	t.Helper()
	dbName := testDBName(t)

	mustExec(t, db, `drop table if exists t8_b_payload`)
	mustExec(t, db, `create table t8_b_payload (id int primary key, payload text)`)
	mustExec(t, db, fmt.Sprintf(
		`insert into t8_b_payload (id, payload) values (1, repeat('a', %d)), (2, repeat('b', %d))`,
		t8bOversizedPayloadBytes, t8bFullPayloadBytes))

	mustExec(t, db, `drop table if exists t8_b_masked`)
	mustExec(t, db, `create table t8_b_masked (id int primary key, payload text)`)
	mustExec(t, db, fmt.Sprintf(
		`insert into t8_b_masked (id, payload) values (1, repeat('c', %d))`, t8bOversizedPayloadBytes))

	// Related-records path: child rows point at parent rows carrying the same
	// oversized/boundary payloads; navigation SELECTs * from the parent.
	mustExec(t, db, `drop table if exists query_e2e_aux.t8b_child`)
	mustExec(t, db, `drop table if exists query_e2e_aux.t8b_parent`)
	mustExec(t, db, `create table query_e2e_aux.t8b_parent (id bigint unsigned primary key, payload text)`)
	mustExec(t, db, `create table query_e2e_aux.t8b_child (
		id bigint unsigned primary key auto_increment,
		parent_id bigint unsigned not null,
		constraint fk_t8b_child_parent foreign key (parent_id) references query_e2e_aux.t8b_parent(id))`)
	mustExec(t, db, fmt.Sprintf(
		`insert into query_e2e_aux.t8b_parent (id, payload) values (1, repeat('a', %d)), (2, repeat('b', %d))`,
		t8bOversizedPayloadBytes, t8bFullPayloadBytes))
	mustExec(t, db, `insert into query_e2e_aux.t8b_child (id, parent_id) values (1,1),(2,2)`)

	// raw_copy_allowed + copyAllowed=true on the delivered columns.
	seedDisclosurePolicies(t, db, targetID, dbName, "t8_b_payload", "id", "payload")
	seedDisclosurePolicies(t, db, targetID, dbName, "t8_b_masked", "id")
	seedDisclosurePolicies(t, db, targetID, "query_e2e_aux", "t8b_parent", "id", "payload")

	// t8_b_masked.payload is governed but not copyable.
	disclosure := mysql.NewQueryDisclosureRepository(db)
	if _, err := disclosure.Insert(context.Background(), model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: targetID,
		DatabaseName:     dbName,
		ObjectName:       "t8_b_masked",
		ColumnName:       "payload",
		Mode:             model.ResultDisclosureMaskedNoCopy,
	}); err != nil {
		t.Fatalf("seed masked policy: %v", err)
	}
}

// t8bPost issues one authenticated JSON POST to the fixture server.
func t8bPost(t *testing.T, baseURL, path, token, body string) *httptestBody {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, baseURL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return &httptestBody{status: resp.StatusCode, body: raw}
}

type httptestBody struct {
	status int
	body   []byte
}

// t8bAssertSingleSuccessEvidence reads the real tables and proves the attempt
// committed exactly one success history row plus exactly one audit event of the
// given fixed type — no second row, no rejected rewrite.
func t8bAssertSingleSuccessEvidence(t *testing.T, db *sql.DB, targetID uint64, eventType string) {
	t.Helper()
	var totalHistory, successHistory, auditCount int
	if err := db.QueryRow(`select count(*) from query_executions where target_resource_id = ?`, targetID).Scan(&totalHistory); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if err := db.QueryRow(`select count(*) from query_executions where target_resource_id = ? and status = 'success'`, targetID).Scan(&successHistory); err != nil {
		t.Fatalf("count success history: %v", err)
	}
	if err := db.QueryRow(`select count(*) from audit_events where target_resource_id = ? and event_type = ?`, targetID, eventType).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if totalHistory != 1 || successHistory != 1 {
		t.Fatalf("history rows: total=%d success=%d, want exactly 1 and it must be success", totalHistory, successHistory)
	}
	if auditCount != 1 {
		t.Fatalf("audit events %q = %d, want exactly 1", eventType, auditCount)
	}
}

func t8bAssertUpgradeRefusal(t *testing.T, r *httptestBody) {
	t.Helper()
	if r.status != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", r.status, string(r.body))
	}
	if !strings.Contains(string(r.body), `"error":"result_contract_upgrade_required"`) {
		t.Fatalf("missing result_contract_upgrade_required code: %s", string(r.body))
	}
	if strings.Contains(string(r.body), `"rows"`) || strings.Contains(string(r.body), `"cellTruncated"`) {
		t.Fatalf("refusal envelope must carry no result payload: %s", string(r.body))
	}
}

type t8bRowsResponse struct {
	Status        string   `json:"status"`
	RowCount      int      `json:"rowCount"`
	Rows          [][]any  `json:"rows"`
	CellTruncated [][]bool `json:"cellTruncated"`
}

func t8bDecodeRows(t *testing.T, r *httptestBody) t8bRowsResponse {
	t.Helper()
	if r.status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", r.status, string(r.body))
	}
	var out t8bRowsResponse
	if err := json.Unmarshal(r.body, &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Status != string(model.QueryExecutionSuccess) {
		t.Fatalf("status field = %q, want success", out.Status)
	}
	return out
}

// --- ordinary execute ---

// TestResultGateIntegration_ExecuteTruncatedPage is the fixed first path from
// the ticket: a real 8193-byte cell under raw_copy_allowed disclosure.
func TestResultGateIntegration_ExecuteTruncatedPage(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/execute", targetID)

	// No capability declared: the real MySQL executor reads 8193 bytes, the
	// success pair commits first, then delivery is refused with no rows.
	r := t8bPost(t, baseURL, path, token, t8bExecuteBody)
	t8bAssertUpgradeRefusal(t, r)
	t8bAssertSingleSuccessEvidence(t, db, targetID, "query.executed")
}

func TestResultGateIntegration_ExecuteTruncatedPageCapableClient(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/execute", targetID)

	r := t8bPost(t, baseURL, path, token,
		`{"statement":"select payload from t8_b_payload where id = 1","capabilities":["cellTruncated"]}`)
	out := t8bDecodeRows(t, r)

	if out.RowCount != 1 || len(out.Rows) != 1 {
		t.Fatalf("rows = %v, want exactly the truncated row", out.Rows)
	}
	got, ok := out.Rows[0][0].(string)
	if !ok || len(got) != service.ResultCellMaxBytes || got != strings.Repeat("a", service.ResultCellMaxBytes) {
		t.Fatalf("delivered payload len/prefix wrong: len=%d", len(got))
	}
	if len(out.CellTruncated) != 1 || len(out.CellTruncated[0]) != 1 || !out.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[true]]", out.CellTruncated)
	}
	t8bAssertSingleSuccessEvidence(t, db, targetID, "query.executed")
}

func TestResultGateIntegration_ExecuteCleanPageDeliveredWithoutCapability(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/execute", targetID)

	// Exactly-at-cap cell: clean page stays fully compatible — full value,
	// honest [[false]] matrix (a non-empty page may not omit the matrix).
	r := t8bPost(t, baseURL, path, token,
		`{"statement":"select payload from t8_b_payload where id = 2"}`)
	out := t8bDecodeRows(t, r)

	got, ok := out.Rows[0][0].(string)
	if !ok || len(got) != service.ResultCellMaxBytes {
		t.Fatalf("clean payload len = %d, want %d", len(got), service.ResultCellMaxBytes)
	}
	if len(out.CellTruncated) != 1 || len(out.CellTruncated[0]) != 1 || out.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[false]]", out.CellTruncated)
	}
	t8bAssertSingleSuccessEvidence(t, db, targetID, "query.executed")
}

func TestResultGateIntegration_ExecuteMaskedTruncatedCellDelivered(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/execute", targetID)

	// The oversized cell is masked_no_copy: the delivered value is "[MASKED]",
	// never a truncated raw — so its flag clears and capable-less clients keep
	// working. CSV export is still denied by copyAllowed=false.
	r := t8bPost(t, baseURL, path, token,
		`{"statement":"select payload from t8_b_masked where id = 1"}`)
	out := t8bDecodeRows(t, r)

	if out.Rows[0][0] != "[MASKED]" {
		t.Fatalf("masked cell = %v, want [MASKED]", out.Rows[0][0])
	}
	if len(out.CellTruncated) != 1 || out.CellTruncated[0][0] {
		t.Fatalf("masked cell flag = %v, want [[false]] — the delivered value is not truncated", out.CellTruncated)
	}
	if err := service.DecideCSVExport(
		[]model.QueryResultColumn{{Name: "payload", DisplayMode: model.ResultDisclosureMaskedNoCopy, CopyAllowed: false}},
		out.Rows, out.CellTruncated); err == nil {
		t.Fatal("CSV export must stay refused for a masked (copyAllowed=false) page")
	}
	t8bAssertSingleSuccessEvidence(t, db, targetID, "query.executed")
}

// --- saved-statement execute ---

// seedT8BSavedStatement stores a parameterized template over the payload table
// as a shared statement so the fixture admin actor can execute it.
func seedT8BSavedStatement(t *testing.T, db *sql.DB, targetID uint64) uint64 {
	t.Helper()
	repo := mysql.NewQuerySavedStatementRepository(db)
	created, err := repo.CreateWithAudit(context.Background(), ownerDBA, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "t8b payload lookup",
		Statement: "select payload from t8_b_payload where id = :row_id",
		Scope:     model.QuerySavedStatementSharedTemplate,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "row_id", Type: model.QuerySavedStatementParameterInteger},
		},
	})
	if err != nil {
		t.Fatalf("create saved statement: %v", err)
	}
	return created.ID
}

func TestResultGateIntegration_SavedStatementTruncatedPage(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	statementID := seedT8BSavedStatement(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", targetID, statementID)

	r := t8bPost(t, baseURL, path, token, `{"values":{"row_id":1}}`)
	t8bAssertUpgradeRefusal(t, r)
	t8bAssertSingleSuccessEvidence(t, db, targetID, "query.executed")
}

func TestResultGateIntegration_SavedStatementTruncatedPageCapableClient(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	statementID := seedT8BSavedStatement(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", targetID, statementID)

	r := t8bPost(t, baseURL, path, token,
		`{"values":{"row_id":1},"capabilities":["cellTruncated"]}`)
	out := t8bDecodeRows(t, r)

	got, ok := out.Rows[0][0].(string)
	if !ok || len(got) != service.ResultCellMaxBytes {
		t.Fatalf("saved-statement payload len = %d, want %d", len(got), service.ResultCellMaxBytes)
	}
	if len(out.CellTruncated) != 1 || !out.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[true]]", out.CellTruncated)
	}
	t8bAssertSingleSuccessEvidence(t, db, targetID, "query.executed")
}

func TestResultGateIntegration_SavedStatementCleanPageDelivered(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	statementID := seedT8BSavedStatement(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", targetID, statementID)

	r := t8bPost(t, baseURL, path, token, `{"values":{"row_id":2}}`)
	out := t8bDecodeRows(t, r)
	if len(out.CellTruncated) != 1 || out.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[false]]", out.CellTruncated)
	}
	t8bAssertSingleSuccessEvidence(t, db, targetID, "query.executed")
}

// --- related-record navigation ---

const t8bNavBodyTruncated = `{"source":{"database":"query_e2e_aux","object":"t8b_child","kind":"table","foreignKey":"fk_t8b_child_parent"},"localValues":["1"]}`
const t8bNavBodyClean = `{"source":{"database":"query_e2e_aux","object":"t8b_child","kind":"table","foreignKey":"fk_t8b_child_parent"},"localValues":["2"]}`

func TestResultGateIntegration_NavigateTruncatedPage(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/related-records", targetID)

	r := t8bPost(t, baseURL, path, token, t8bNavBodyTruncated)
	t8bAssertUpgradeRefusal(t, r)
	t8bAssertSingleSuccessEvidence(t, db, targetID, "related_record_navigation")
}

func TestResultGateIntegration_NavigateTruncatedPageCapableClient(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/related-records", targetID)

	r := t8bPost(t, baseURL, path, token,
		strings.Replace(t8bNavBodyTruncated, `"localValues":["1"]`,
			`"localValues":["1"],"capabilities":["cellTruncated"]`, 1))
	out := t8bDecodeRows(t, r)

	// SELECT * FROM t8b_parent → [id, payload]: payload flag true, id flag false.
	got, ok := out.Rows[0][1].(string)
	if !ok || len(got) != service.ResultCellMaxBytes {
		t.Fatalf("nav payload len = %d, want %d", len(got), service.ResultCellMaxBytes)
	}
	if len(out.CellTruncated) != 1 || len(out.CellTruncated[0]) != 2 ||
		out.CellTruncated[0][0] || !out.CellTruncated[0][1] {
		t.Fatalf("cellTruncated = %v, want [[false,true]]", out.CellTruncated)
	}
	t8bAssertSingleSuccessEvidence(t, db, targetID, "related_record_navigation")
}

func TestResultGateIntegration_NavigateCleanPageDelivered(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/related-records", targetID)

	r := t8bPost(t, baseURL, path, token, t8bNavBodyClean)
	out := t8bDecodeRows(t, r)
	if len(out.CellTruncated) != 1 || len(out.CellTruncated[0]) != 2 ||
		out.CellTruncated[0][0] || out.CellTruncated[0][1] {
		t.Fatalf("cellTruncated = %v, want [[false,false]]", out.CellTruncated)
	}
	t8bAssertSingleSuccessEvidence(t, db, targetID, "related_record_navigation")
}

// --- evidence-ordering: persistence failure outranks the capability refusal ---

func TestResultGateIntegration_ExecutePersistFailureBeatsRefusal(t *testing.T) {
	baseURL, targetID, db := setupNavigateFixture(t)
	seedT8BTables(t, db, targetID)
	token := navLogin(t, baseURL)
	path := fmt.Sprintf("/query-targets/%d/execute", targetID)

	// Force the evidence pair's audit insert to fail inside the repository
	// transaction — the pair rolls back atomically (same technique as the
	// Issue #36 navigation proof).
	mustExec(t, db, `create trigger t8b_force_audit_fail
		before insert on audit_events for each row
		signal sqlstate '45000' set message_text = 'forced audit failure'`)
	t.Cleanup(func() { _, _ = db.Exec(`drop trigger if exists t8b_force_audit_fail`) })

	before := mysql.QueryEvidencePersistenceFailures.Value()
	r := t8bPost(t, baseURL, path, token, t8bExecuteBody)

	if r.status != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 — persistence failure outranks the capability refusal; body=%s", r.status, string(r.body))
	}
	if strings.Contains(string(r.body), "result_contract_upgrade_required") {
		t.Fatalf("capability refusal must not mask the persistence failure: %s", string(r.body))
	}
	var execRows, auditRows int
	if err := db.QueryRow(`select count(*) from query_executions where target_resource_id = ?`, targetID).Scan(&execRows); err != nil {
		t.Fatalf("count executions: %v", err)
	}
	if err := db.QueryRow(`select count(*) from audit_events where target_resource_id = ?`, targetID).Scan(&auditRows); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if execRows != 0 || auditRows != 0 {
		t.Fatalf("evidence pair must roll back together: exec=%d audit=%d", execRows, auditRows)
	}
	if after := mysql.QueryEvidencePersistenceFailures.Value(); after-before != 1 {
		t.Fatalf("persistence-failure counter +%d, want +1", after-before)
	}
}
