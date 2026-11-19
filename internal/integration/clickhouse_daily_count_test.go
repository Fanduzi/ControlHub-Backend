//go:build integration

package integration

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func testCHDailyCount(t *testing.T, h http.Handler, path, token, generation string, db *sql.DB, target uint64) {
	var catalog struct {
		Tables []struct {
			Name              string
			DailyCountColumns []string
		}
	}
	r := doBearer(t, h, "GET", path, token)
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &catalog) != nil {
		t.Fatal("cannot read catalog")
	}
	found := false
	for _, table := range catalog.Tables {
		if table.Name == "events" {
			found = true
			if !slices.Equal(table.DailyCountColumns, []string{"observed_at", "request_date"}) {
				t.Fatalf("catalog exposed unexpected count axes: %v", table.DailyCountColumns)
			}
		}
	}
	if !found {
		t.Fatal("missing events table")
	}
	call := func(input map[string]any, status int) [][]string {
		t.Helper()
		input["generation"] = generation
		input["table"] = "events"
		body, _ := json.Marshal(input)
		r := doBearerWithBody(t, h, "POST", path, token, string(body))
		if r.Code != status {
			t.Fatalf("status %d want %d: %s", r.Code, status, r.Body.String())
		}
		if status != 200 {
			if strings.Contains(r.Body.String(), `"rows"`) {
				t.Fatal("denial delivered rows")
			}
			return nil
		}
		var out struct{ Rows [][]string }
		if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Rows
	}
	events := call(map[string]any{"columns": []string{"request_id", "observed_at", "request_date", "private_time"}, "limit": 20}, 200)
	if len(events) != 8 {
		t.Fatal("fixture did not return all eight boundary instants")
	}
	for _, row := range events {
		if len(row) != 4 || row[0] != "18446744073709551615" || row[3] != "[MASKED]" {
			t.Fatal("lossy UInt64 or missing mask")
		}
		instant, err := time.Parse(time.RFC3339, row[1])
		if err != nil || !strings.HasSuffix(row[1], "Z") || row[2] != instant.UTC().Format(time.DateOnly) {
			t.Fatal("calendar date or UTC conversion mismatch", row[1:3])
		}
	}
	for _, axis := range []string{"observed_at", "request_date"} {
		before := time.Now().UTC()
		days := call(map[string]any{"dailyCountColumn": axis}, 200)
		after := time.Now().UTC()
		if len(days) != 7 {
			t.Fatal("expected seven buckets")
		}
		last, err := time.Parse(time.DateOnly, days[6][0])
		if err != nil || (last.Format(time.DateOnly) != before.Format(time.DateOnly) && last.Format(time.DateOnly) != after.Format(time.DateOnly)) {
			t.Fatal("incorrect final UTC day")
		}
		for i, row := range days {
			day := last.AddDate(0, 0, i-6).Format(time.DateOnly)
			n := 0
			for _, event := range events {
				at, err := time.Parse(time.RFC3339, event[1])
				if err != nil {
					t.Fatal(err)
				}
				if event[2] == day && (axis == "request_date" || !at.After(after)) {
					n++
				}
			}
			if !reflect.DeepEqual(row, []string{day, strconv.Itoa(n)}) {
				t.Fatalf("%s bucket %d: %v want %s/%d", axis, i, row, day, n)
			}
		}
	}
	for _, col := range []string{"private_time", "missing_date", "preview_only", "request_id", "unknown"} {
		call(map[string]any{"dailyCountColumn": col}, 403)
	}
	call(map[string]any{"dailyCountColumn": "observed_at", "columns": []string{"private_time"}, "limit": 7}, 400)
	// A previously issued generation never substitutes for current disclosure policy.
	if _, err := db.Exec(`UPDATE query_result_disclosure_policies SET mode='masked_no_copy' WHERE target_resource_id=? AND database_name='g2' AND object_name='events' AND column_name='observed_at'`, target); err != nil {
		t.Fatal(err)
	}
	call(map[string]any{"dailyCountColumn": "observed_at"}, 403)
	if _, err := db.Exec(`UPDATE query_result_disclosure_policies SET mode='raw_copy_allowed' WHERE target_resource_id=? AND database_name='g2' AND object_name='events' AND column_name='observed_at'`, target); err != nil {
		t.Fatal(err)
	}
	var beforeH, beforeA, afterH, afterA int
	counts := func(history, audit *int) {
		t.Helper()
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM query_executions WHERE target_resource_id=?),(SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type='query.executed')`, target, target).Scan(history, audit); err != nil {
			t.Fatal(err)
		}
	}
	counts(&beforeH, &beforeA)
	if _, err := db.Exec(`CREATE TRIGGER ch_daily_fail BEFORE INSERT ON audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture failure'`); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(`DROP TRIGGER ch_daily_fail`)
	call(map[string]any{"dailyCountColumn": "request_date"}, 502)
	counts(&afterH, &afterA)
	if afterH != beforeH || afterA != beforeA {
		t.Fatal("daily count audit failure left partial evidence")
	}
}
