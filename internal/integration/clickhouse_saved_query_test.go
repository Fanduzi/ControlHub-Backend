//go:build integration

package integration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func testCHSavedQuery(t *testing.T, h http.Handler, id uint64, owner, other, admin, generation string, db *sql.DB) {
	t.Helper()
	path := fmt.Sprintf("/query-targets/%d/saved-statements", id)
	const stmt = "SELECT request_date, count() AS hits FROM plan_events WHERE request_date >= '2026-02-01' AND request_date < '2026-02-08' GROUP BY request_date ORDER BY request_date"
	req := model.QuerySavedStatementCreateRequest{Name: "private date range", Statement: stmt, Scope: model.QuerySavedStatementPersonal, ClickHouseGeneration: generation}
	send := func(method, url, token string, payload any, want int) []byte {
		t.Helper()
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		r := doBearerWithBody(t, h, method, url, token, string(body))
		if r.Code != want {
			t.Fatalf("%s %s status=%d want=%d: %s", method, url, r.Code, want, r.Body.String())
		}
		return r.Body.Bytes()
	}
	executions := func() int {
		t.Helper()
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=?`, id).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	before := executions()
	var saved model.QuerySavedStatement
	savedBody := send("POST", path, owner, req, 201)
	if err := json.Unmarshal(savedBody, &saved); err != nil || saved.ID == 0 || saved.TargetResourceID != id || saved.Statement != stmt || saved.Scope != model.QuerySavedStatementPersonal || strings.Contains(string(savedBody), generation) {
		t.Fatal("incorrect persisted record", err)
	}
	list := func(token string) model.QuerySavedStatementListResponse {
		t.Helper()
		r := doBearer(t, h, "GET", path, token)
		var out model.QuerySavedStatementListResponse
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &out) != nil {
			t.Fatal("saved query list failed", r.Body.String())
		}
		return out
	}
	if got := list(owner); len(got.Items) != 1 || got.Items[0].Statement != stmt {
		t.Fatal("owner cannot reload saved SQL")
	}
	for _, token := range []string{other, admin} {
		got := list(token)
		if len(got.Items) != 0 || got.CanManageSharedTemplates {
			t.Fatal("personal query or template affordance leaked")
		}
	}
	row := fmt.Sprintf("%s/%d", path, saved.ID)
	edit := model.QuerySavedStatementUpdateRequest{Name: saved.Name, Statement: strings.Replace(stmt, "2026-02-01", "2026-02-02", 1), ClickHouseGeneration: generation}
	send("PUT", row, other, edit, 404)
	send("PUT", row, owner, edit, 204)
	if got := list(owner); len(got.Items) != 1 || got.Items[0].Statement != edit.Statement {
		t.Fatal("updated SQL not reloadable")
	}
	denied := req
	denied.Scope = model.QuerySavedStatementSharedTemplate
	send("POST", path, admin, denied, 403)
	denied = req
	denied.Parameters = []model.QuerySavedStatementParameterDefinition{{Name: "day", Type: model.QuerySavedStatementParameterString}}
	send("POST", path, owner, denied, 400)
	denied = req
	denied.ClickHouseGeneration = "expired"
	send("POST", path, owner, denied, 403)
	send("POST", path, other, req, 403)
	denied = req
	denied.Statement = "SELECT id FROM plan_events WHERE private_label='fixture-private'"
	send("POST", path, owner, denied, 403)
	denied = req
	denied.Statement = "DELETE FROM plan_events"
	send("POST", path, owner, denied, 400)
	paramSQL := "SELECT request_date FROM plan_events WHERE request_date >= :start_date AND request_date < :end_date ORDER BY request_date"
	paramReq := model.QuerySavedStatementCreateRequest{
		Name: "private parameterized range", Statement: paramSQL, Scope: model.QuerySavedStatementPersonal, ClickHouseGeneration: generation,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "start_date", Type: model.QuerySavedStatementParameterString},
			{Name: "end_date", Type: model.QuerySavedStatementParameterString},
		},
	}
	for _, badSQL := range []string{
		strings.Replace(paramSQL, ":end_date", ":start_date", 1),
		strings.Replace(paramSQL, ":start_date", "':start_date'", 1),
		strings.Replace(paramSQL, "ORDER BY request_date", "ORDER BY :end_date", 1),
		strings.Replace(paramSQL, ":start_date", "{start_date:Date}", 1),
	} {
		bad := paramReq
		bad.Statement = badSQL
		send("POST", path, owner, bad, 400)
	}
	var template model.QuerySavedStatement
	if err := json.Unmarshal(send("POST", path, owner, paramReq, 201), &template); err != nil || template.ID == 0 || template.Statement != paramSQL || len(template.Parameters) != 2 {
		t.Fatalf("saved template mismatch: %v", err)
	}
	if executions() != before {
		t.Fatal("save or list executed SQL or wrote query history")
	}
	var audits int
	if err := db.QueryRow(`SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type IN ('query.saved_statement.created','query.saved_statement.updated')`, id).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("expected only three successful save audits, got %d: %v", audits, err)
	}
	templatePath := fmt.Sprintf("%s/%d/execute", path, template.ID)
	run := model.QuerySavedStatementExecuteRequest{
		ClickHouseGeneration: generation,
		Pagination:           &model.QueryExecutePaginationRequest{Page: 1, PageSize: 5},
		Values:               map[string]json.RawMessage{"start_date": json.RawMessage(`"2026-07-11"`), "end_date": json.RawMessage(`"2026-07-19"`)},
	}
	send("POST", templatePath, admin, run, 403)
	missingTemplate := fmt.Sprintf("%s/%d/execute", path, template.ID+1)
	send("POST", missingTemplate, owner, run, 404)
	stale := run
	stale.ClickHouseGeneration = "stale"
	send("POST", templatePath, owner, stale, 403)
	var result model.QueryExecuteResponse
	if err := json.Unmarshal(send("POST", templatePath, owner, run, 200), &result); err != nil || len(result.Rows) != 5 || result.Rows[0][0] != "2026-07-11" || !result.Pagination.HasNextPage {
		t.Fatalf("first template page mismatch: %v", err)
	}
	run.Pagination.Page = 2
	if err := json.Unmarshal(send("POST", templatePath, owner, run, 200), &result); err != nil || len(result.Rows) != 3 || result.Rows[0][0] != "2026-07-16" || result.Pagination.HasNextPage {
		t.Fatalf("second template page mismatch: %v", err)
	}
	const invalidDate = "private-invalid-date-fixture"
	invalid := run
	invalid.Values = map[string]json.RawMessage{"start_date": json.RawMessage(`"` + invalidDate + `"`)}
	if body := string(send("POST", templatePath, owner, invalid, 400)); strings.Contains(body, invalidDate) || !strings.Contains(body, `"end_date":"missing"`) {
		t.Fatal("incorrect missing-value validation response")
	}
	invalid.Values["end_date"] = run.Values["end_date"]
	if body := string(send("POST", templatePath, owner, invalid, 400)); strings.Contains(body, invalidDate) || !strings.Contains(body, `"start_date":"invalid"`) {
		t.Fatal("incorrect invalid-date validation response")
	}
	var evidence, auditPairs int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND id>=? AND statement_digest='' AND statement_preview='ClickHouse worksheet' AND full_statement IS NULL AND (error_message IS NULL OR error_message='')`, id, result.ExecutionID-1).Scan(&evidence); err != nil || evidence != 4 {
		t.Fatalf("expected four literal-free evidence rows, got %d: %v", evidence, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type='query.executed' AND changes IS NOT NULL`, id).Scan(&auditPairs); err != nil || auditPairs != 0 {
		t.Fatalf("unexpected query execution audit payload rows: %d: %v", auditPairs, err)
	}
	for _, saved := range list(owner).Items {
		if saved.ID == template.ID && (saved.Statement != paramSQL || len(saved.Parameters) != 2) {
			t.Fatal("running changed the saved SQL or its declarations")
		}
	}
	if _, err := db.Exec(`UPDATE query_result_disclosure_policies SET mode='masked_no_copy' WHERE target_resource_id=? AND object_name='plan_events' AND column_name='request_date'`, id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`UPDATE query_result_disclosure_policies SET mode='raw_copy_allowed' WHERE target_resource_id=? AND object_name='plan_events' AND column_name='request_date'`, id); err != nil {
			t.Error(err)
		}
	})
	if body := send("POST", templatePath, owner, run, 403); strings.Contains(string(body), `"rows"`) {
		t.Fatal("denied template page returned rows")
	}
	raw := send("POST", fmt.Sprintf("/query-targets/%d/execute", id), owner, model.QueryExecuteRequest{Statement: edit.Statement, ClickHouseGeneration: generation}, 403)
	if strings.Contains(string(raw), `"rows"`) {
		t.Fatal("denied run returned rows")
	}
}
