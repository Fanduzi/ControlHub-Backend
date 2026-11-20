//go:build integration

package integration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func testCHWorksheetPages(t *testing.T, h http.Handler, id uint64, token, generation string, db *sql.DB) {
	path := fmt.Sprintf("/query-targets/%d/execute", id)
	const stmt = "SELECT request_id FROM page_events ORDER BY request_id"
	cases := []struct {
		name, sql                                    string
		maxRows, page, size, status, first, n, limit int
		next, truncated                              bool
	}{
		{"first", stmt + " LIMIT 12", 100, 1, 5, 200, 1, 5, 12, true, false},
		{"next", stmt + " LIMIT 12", 100, 2, 5, 200, 6, 5, 12, true, false},
		{"final", stmt + " LIMIT 12", 100, 3, 5, 200, 11, 2, 12, false, false},
		{"back", stmt + " LIMIT 12", 100, 2, 5, 200, 6, 5, 12, true, false},
		{"edit", stmt + " LIMIT 3", 100, 1, 5, 200, 1, 3, 3, false, false},
		{"outside user limit", stmt + " LIMIT 12", 100, 4, 5, 400, 0, 0, 0, false, false},
		{"last allowed page", stmt, 100, 4, 5, 200, 16, 5, 20, false, true},
		{"outside platform cap", stmt, 100, 5, 5, 400, 0, 0, 0, false, false},
		{"smaller request cap", stmt + " LIMIT 19", 7, 2, 5, 200, 6, 2, 7, false, true},
		{"large page", stmt + " LIMIT 100", 100, 1, 100, 200, 1, 20, 20, false, true},
		{"short dataset", "SELECT request_id FROM page_events WHERE request_id <= 5 ORDER BY request_id", 20, 1, 5, 200, 1, 5, 20, false, false},
		{"no matches", "SELECT request_id FROM page_events WHERE request_id = 99", 20, 1, 5, 200, 0, 0, 20, false, false},
		{"limit zero", stmt + " LIMIT 0", 20, 1, 5, 200, 0, 0, 0, false, false},
		{"huge page", stmt, 20, math.MaxInt / 5, 5, 400, 0, 0, 0, false, false},
		{"policy changed", stmt + " LIMIT 12", 100, 2, 5, 403, 0, 0, 0, false, false},
	}
	var ids []uint64
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "policy changed" {
				_, err := db.Exec(`UPDATE query_result_disclosure_policies SET mode='masked_no_copy' WHERE target_resource_id=? AND object_name='page_events' AND column_name='request_id'`, id)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					_, err := db.Exec(`UPDATE query_result_disclosure_policies SET mode='raw_copy_allowed' WHERE target_resource_id=? AND object_name='page_events' AND column_name='request_id'`, id)
					if err != nil {
						t.Error(err)
					}
				})
			}
			count := func() (int, int) {
				t.Helper()
				var n, a int
				if err := db.QueryRow(`SELECT (SELECT count(*) FROM query_executions WHERE target_resource_id=?),(SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type='query.executed')`, id, id).Scan(&n, &a); err != nil {
					t.Fatal(err)
				}
				return n, a
			}
			beforeH, beforeA := count()
			body, err := json.Marshal(model.QueryExecuteRequest{Statement: tc.sql, MaxRows: tc.maxRows, Pagination: &model.QueryExecutePaginationRequest{Page: tc.page, PageSize: tc.size}, ClickHouseGeneration: generation})
			if err != nil {
				t.Fatal(err)
			}
			res := doBearerWithBody(t, h, "POST", path, token, string(body))
			if res.Code != tc.status {
				t.Fatalf("status=%d want %d: %s", res.Code, tc.status, res.Body.String())
			}
			afterH, afterA := count()
			if afterH != beforeH+1 || afterA != beforeA+1 {
				t.Fatal("each page must commit exactly one history/audit pair")
			}
			var histID uint64
			var histRows int
			var status, code string
			if err := db.QueryRow(`SELECT id,row_count,status,error_code FROM query_executions WHERE target_resource_id=? ORDER BY id DESC LIMIT 1`, id).Scan(&histID, &histRows, &status, &code); err != nil {
				t.Fatal(err)
			}
			if slices.Contains(ids, histID) || histRows != tc.n {
				t.Fatalf("incorrect evidence id=%d rows=%d", histID, histRows)
			}
			ids = append(ids, histID)
			if res.Code != 200 {
				want := "page_out_of_range"
				if tc.status == 403 {
					want = "query_result_disclosure_blocked"
				}
				var failure struct {
					Error string
					Rows  json.RawMessage
				}
				if err := json.Unmarshal(res.Body.Bytes(), &failure); err != nil || failure.Rows != nil || failure.Error != want || status != "rejected" || code != want {
					t.Fatalf("rejection mismatch: %s, history=%s/%s", res.Body.String(), status, code)
				}
				return
			}
			var out model.QueryExecuteResponse
			if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			p := out.Pagination
			if p == nil || p.Page != tc.page || p.PageSize != tc.size || p.HasNextPage != tc.next || p.HasPreviousPage != (tc.page > 1) || out.LimitApplied != tc.limit || out.Truncated != tc.truncated || out.RowCount != tc.n || len(out.Rows) != tc.n || out.ExecutionID != histID || status != "success" {
				t.Fatalf("unexpected page %s", res.Body.String())
			}
			for i, row := range out.Rows {
				if len(row) != 1 || row[0] != strconv.Itoa(tc.first+i) {
					t.Fatalf("row %d: %v", i, row)
				}
			}
		})
	}
}
