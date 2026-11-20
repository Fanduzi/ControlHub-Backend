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
	if executions() != before {
		t.Fatal("save or list executed SQL or wrote query history")
	}
	var audits int
	if err := db.QueryRow(`SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type IN ('query.saved_statement.created','query.saved_statement.updated')`, id).Scan(&audits); err != nil || audits != 2 {
		t.Fatalf("expected only two successful save audits, got %d: %v", audits, err)
	}
	if _, err := db.Exec(`UPDATE query_result_disclosure_policies SET mode='masked_no_copy' WHERE target_resource_id=? AND object_name='plan_events' AND column_name='request_date'`, id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`UPDATE query_result_disclosure_policies SET mode='raw_copy_allowed' WHERE target_resource_id=? AND object_name='plan_events' AND column_name='request_date'`, id); err != nil {
			t.Error(err)
		}
	})
	loaded := list(owner).Items[0]
	raw := send("POST", fmt.Sprintf("/query-targets/%d/execute", id), owner, model.QueryExecuteRequest{Statement: loaded.Statement, ClickHouseGeneration: generation}, 403)
	if strings.Contains(string(raw), `"rows"`) {
		t.Fatal("denied run returned rows")
	}
}
