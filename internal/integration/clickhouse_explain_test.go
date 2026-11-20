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

func testCHExplain(t *testing.T, h http.Handler, id uint64, token, other, generation string, db *sql.DB) {
	t.Helper()
	path := fmt.Sprintf("/query-targets/%d/explain", id)
	count := func(q string, args ...any) int {
		t.Helper()
		var n int
		if err := db.QueryRow(q, args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	histories := func() int { return count(`SELECT count(*) FROM query_executions WHERE target_resource_id=?`, id) }
	audits := func(outcome string) int {
		return count(`SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type='query.explain' AND result=?`, id, outcome)
	}
	call := func(stmt, bearer, gen string, status int, outcome, code string) model.ExplainResponse {
		t.Helper()
		n, a := histories(), audits(outcome)
		body, _ := json.Marshal(model.ExplainRequest{Statement: stmt, ClickHouseGeneration: gen})
		r := doBearerWithBody(t, h, "POST", path, bearer, string(body))
		if r.Code != status {
			t.Fatalf("status=%d want=%d: %s", r.Code, status, r.Body.String())
		}
		if histories() != n || audits(outcome) != a+1 {
			t.Fatal("explain must record its audit outcome without an execution")
		}
		var result model.ExplainResponse
		if status != 200 {
			var failure struct{ Error string }
			if json.Unmarshal(r.Body.Bytes(), &failure) != nil || failure.Error != code || strings.Contains(r.Body.String(), `"nodes"`) {
				t.Fatal("incorrect refusal", r.Body.String())
			}
			return result
		}
		for _, private := range []string{"2026-02-01", "2026-02-08", "fixture-private", "Condition", "private_label", "Selected Parts", "request_date"} {
			if strings.Contains(r.Body.String(), private) {
				t.Fatalf("unexpected plan field/value %q", private)
			}
		}
		if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || result.Validate() != nil || result.TargetResourceID != id || result.Engine != model.ExplainEngineClickHouse {
			t.Fatal("invalid normalized result", r.Body.String())
		}
		return result
	}
	const base = "SELECT request_date, count() AS hits FROM plan_events "
	const group = " GROUP BY request_date ORDER BY hits DESC"
	full := call(base+group, token, generation, 200, "success", "")
	reduced := call(base+"WHERE request_date >= '2026-02-01' AND request_date < '2026-02-08'"+group, token, generation, 200, "success", "")
	evidence := func(plan model.ExplainResponse, filtered bool) model.ExplainGranules {
		t.Helper()
		ops := map[model.ExplainNodeOperation]bool{}
		var g *model.ExplainGranules
		for _, n := range plan.Nodes {
			ops[n.Operation] = true
			if n.PrimaryKeyGranules != nil {
				if n.Operation != model.ExplainOpTableAccess || g != nil {
					t.Fatal("ambiguous read evidence")
				}
				g = n.PrimaryKeyGranules
			}
		}
		if g == nil || !ops[model.ExplainOpAggregate] || !ops[model.ExplainOpSort] || (filtered && !ops[model.ExplainOpFilter]) {
			t.Fatal("missing measured plan evidence", plan)
		}
		return *g
	}
	all, part := evidence(full, false), evidence(reduced, true)
	if all.Initial == 0 || all.Selected != all.Initial || part.Initial != all.Initial || part.Selected == 0 || part.Selected >= all.Selected {
		t.Fatalf("expected primary key pruning: full=%+v range=%+v", all, part)
	}
	t.Logf("primary-key granules: full %d/%d; date range %d/%d", all.Selected, all.Initial, part.Selected, part.Initial)
	call("SELECT id FROM plan_events WHERE private_label='fixture-private'", token, generation, 403, "rejected", "query_result_disclosure_blocked")
	call(base+group, other, generation, 403, "rejected", "query_not_allowed")
	call(base+group, token, "stale", 403, "rejected", "query_not_allowed")
	// This base table's fixture row policy raises a read error during execution.
	// A successful plan for the very same SQL independently demonstrates planning only.
	const throwingRead = "SELECT id FROM late ORDER BY id LIMIT 20"
	stmt, _ := json.Marshal(model.QueryExecuteRequest{Statement: throwingRead, ClickHouseGeneration: generation})
	control := doBearerWithBody(t, h, "POST", fmt.Sprintf("/query-targets/%d/execute", id), token, string(stmt))
	if control.Code != 502 {
		t.Fatal("fixture SELECT must fail during execution", control.Code)
	}
	call(throwingRead, token, generation, 200, "success", "")
	call("EXPLAIN ANALYZE SELECT id FROM plan_events", token, generation, 400, "rejected", "validation_failed")
	call("DELETE FROM plan_events", token, generation, 400, "rejected", "validation_failed")
	call("SELECT id FROM sample", token, generation, 403, "rejected", "query_not_allowed")
}
