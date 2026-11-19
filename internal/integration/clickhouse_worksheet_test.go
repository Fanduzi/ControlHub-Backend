//go:build integration

package integration

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
)

func testCHWorksheet(t *testing.T, h http.Handler, id uint64, token, other, generation string, db *sql.DB) {
	path := fmt.Sprintf("/query-targets/%d/execute", id)
	for _, tc := range []struct {
		name, stmt string
		want       int
		rows       [][]any
	}{
		{"detail", "SELECT request_id FROM events WHERE request_id = 42 LIMIT 10", 200, [][]any{{"42"}}},
		{"edit", "SELECT request_id FROM events WHERE request_id = 43 LIMIT 10", 200, [][]any{{"43"}}},
		{"exact", "SELECT request_id FROM events WHERE request_id = 18446744073709551615 LIMIT 1", 200, [][]any{{"18446744073709551615"}}},
		{"masked projection", "SELECT request_id, private_time FROM events WHERE request_id=42", 200, [][]any{{"42", "[MASKED]"}}},
		{"groups", "SELECT request_id, count() AS hits FROM events GROUP BY request_id ORDER BY hits DESC LIMIT 1", 200, [][]any{{"18446744073709551615", "6"}}},
		{"date window", "SELECT request_date, count() AS requests FROM http_log WHERE request_date >= '2026-11-13' AND request_date < '2026-11-20' GROUP BY request_date ORDER BY request_date", 200, [][]any{{"2026-11-13", "2"}, {"2026-11-19", "1"}}},
		{"edited window", "SELECT request_date, count() AS requests FROM http_log WHERE request_date >= '2026-11-14' AND request_date < '2026-11-20' GROUP BY request_date ORDER BY request_date", 200, [][]any{{"2026-11-19", "1"}}},
		{"date window top service", "SELECT service,count(*) AS requests FROM http_log WHERE request_date BETWEEN '2026-11-13' AND '2026-11-19' GROUP BY service ORDER BY requests DESC LIMIT 1", 200, [][]any{{"api", "2"}}},
		{"boolean precedence", "SELECT count() FROM http_log WHERE service='api' OR service='worker' AND request_date<'2026-11-19'", 200, [][]any{{"4"}}},
		{"explicit grouping", "SELECT count() FROM http_log WHERE (service='api' OR service='worker') AND request_date<'2026-11-19'", 200, [][]any{{"3"}}},
		{"not between", "SELECT count(*) FROM events WHERE request_id NOT BETWEEN 42 AND 43", 200, [][]any{{"6"}}},
		{"unsigned range", "SELECT request_id FROM events WHERE request_id > 9223372036854775808 AND request_id <= 18446744073709551615 LIMIT 1", 200, [][]any{{"18446744073709551615"}}},
		{"nested policy denial", "SELECT service FROM http_log WHERE service='api' OR (request_date >= '2026-11-13' AND private_label='test')", 403, nil},
		{"masked range denial", "SELECT request_id FROM events WHERE private_time BETWEEN '2026-11-13T00:00:00Z' AND '2026-11-20T00:00:00Z'", 403, nil},
		{"unknown nested column", "SELECT service FROM http_log WHERE service='worker' AND absent_column<3", 403, nil},
		{"empty", "SELECT request_id FROM events WHERE request_id=44", 200, [][]any{}},
		{"zero limit", "SELECT request_id FROM events LIMIT 0", 200, [][]any{}},
		{"masked filter", "SELECT request_id FROM events WHERE private_time = '2026-11-19T00:00:00Z'", 403, nil},
		{"masked group", "SELECT private_time,count() FROM events GROUP BY private_time", 403, nil},
		{"masked sort", "SELECT private_time FROM events ORDER BY private_time", 403, nil},
		{"missing policy", "SELECT request_id FROM events WHERE missing_date = '2026-11-19'", 403, nil},
		{"write", "DELETE FROM events", 400, nil},
		{"unsupported", "SELECT * FROM events", 400, nil},
		{"preview only", "SELECT id FROM sample", 403, nil},
		{"unauthorized", "SELECT request_id FROM events", 403, nil},
		{"stale", "SELECT request_id FROM events", 403, nil},
		{"failed audit", "SELECT request_id FROM events WHERE request_id=42", 502, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, user := generation, token
			if tc.name == "stale" {
				key = strings.Repeat("0", 64)
			}
			if tc.name == "unauthorized" {
				user = other
			}
			counts := func() (int, int) {
				var n, a int
				if err := db.QueryRow(`SELECT (SELECT count(*) FROM query_executions WHERE target_resource_id=?),(SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type='query.executed')`, id, id).Scan(&n, &a); err != nil {
					t.Fatal(err)
				}
				return n, a
			}
			n, a := counts()
			if tc.name == "failed audit" {
				if _, err := db.Exec(`CREATE TRIGGER ch_ws_fail BEFORE INSERT ON audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture failure'`); err != nil {
					t.Fatal(err)
				}
				defer db.Exec(`DROP TRIGGER ch_ws_fail`)
			}
			body, _ := json.Marshal(model.QueryExecuteRequest{Statement: tc.stmt, ClickHouseGeneration: key})
			r := doBearerWithBody(t, h, "POST", path, user, string(body))
			if r.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", r.Code, tc.want, r.Body.String())
			}
			nn, aa := counts()
			if tc.name == "failed audit" {
				if nn != n || aa != a {
					t.Fatal("partial audit commit")
				}
			} else if nn != n+1 || aa != a+1 {
				t.Fatal("expected exactly one execution and one audit")
			}
			if r.Code != 200 {
				if strings.Contains(r.Body.String(), `"rows"`) {
					t.Fatal("refusal delivered data")
				}
				return
			}
			var result model.QueryExecuteResponse
			if err := json.Unmarshal(r.Body.Bytes(), &result); err != nil || result.Engine != "clickhouse" || result.Status != model.QueryExecutionSuccess || !reflect.DeepEqual(result.Rows, tc.rows) {
				t.Fatalf("unexpected result %s", r.Body.String())
			}
			var rc int
			var digest string
			var full sql.NullString
			if err := db.QueryRow(`SELECT row_count,statement_digest,full_statement FROM query_executions WHERE id=? AND target_resource_id=? AND status='success'`, result.ExecutionID, id).Scan(&rc, &digest, &full); err != nil || rc != len(tc.rows) || len(digest) != 64 || full.Valid {
				t.Fatal("incorrect worksheet evidence")
			}
		})
	}
}
