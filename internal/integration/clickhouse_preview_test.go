//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/serverboot"
	"github.com/gofrs/flock"
)

func TestClickHousePreviewHTTP(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	dir := t.TempDir()
	fixture := exec.Command("python3", "testdata/clickhouse/fixture.py", dir)
	fixture.Stderr = os.Stderr
	stdin, err := fixture.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := fixture.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stdin.Close()
		if err := fixture.Wait(); err != nil {
			t.Error(err)
		}
	})
	var remote struct{ Addr, Password string }
	if err := json.NewDecoder(stdout).Decode(&remote); err != nil {
		t.Fatal(err)
	}
	host, portText, _ := net.SplitHostPort(remote.Addr)
	port, _ := strconv.Atoi(portText)
	repo := mysql.NewResourceRepository(db)
	target, err := repo.CreateResource(ctx, model.ResourceCreateInput{ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "clickhouse", Name: "ch-preview-fixture", DisplayName: "ClickHouse Preview Fixture", EnvironmentID: envStaging, OwnerID: ownerDBA, LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual", Labels: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertDatabaseInstanceProfile(ctx, target.ID, "clickhouse", "26.8.34.13", host, port, "primary"); err != nil {
		t.Fatal(err)
	}
	const email = "preview-editor@example.com"
	editor := insertAuthzTestUser(t, db, email, "editor")
	other := insertAuthzTestUser(t, db, "preview-other@example.com", "editor")
	admin := insertAuthzTestUser(t, db, "preview-admin@example.com", "admin")
	t.Cleanup(func() {
		for _, id := range []uint64{editor, other, admin} {
			deleteAuthzTestUser(t, db, id)
		}
	})
	tables := map[string]any{}
	names := []string{"sample", "large_cell", "large_reply", "slow", "late", "server_budget"}
	policies := mysql.NewQueryDisclosureRepository(db)
	for i, name := range names {
		tables[name] = map[string]any{"Worksheet": name == "late", "UUID": fmt.Sprintf("cb200000-0000-4000-8000-%012d", i+1), "Columns": []string{"id", "title", "secret"}}
		for _, c := range []string{"id", "title", "secret"} {
			mode := model.ResultDisclosureRawCopyAllowed
			if c == "secret" {
				mode = model.ResultDisclosureMaskedNoCopy
			}
			_, err := policies.Insert(ctx, model.ResultDisclosurePolicyUpsertRequest{TargetResourceID: target.ID, DatabaseName: "g2", ObjectName: name, ColumnName: c, Mode: mode})
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	eventColumns := []string{"request_id", "observed_at", "request_date", "private_time", "preview_only", "missing_date"}
	tables["events"] = map[string]any{"Worksheet": true, "UUID": "cb200000-0000-4000-8000-000000000007", "Columns": eventColumns, "DailyCountColumns": []string{"observed_at", "request_date", "private_time", "missing_date"}}
	for _, col := range eventColumns[:5] {
		mode := model.ResultDisclosureRawCopyAllowed
		if col == "private_time" {
			mode = model.ResultDisclosureMaskedNoCopy
		}
		_, err := policies.Insert(ctx, model.ResultDisclosurePolicyUpsertRequest{TargetResourceID: target.ID, DatabaseName: "g2", ObjectName: "events", ColumnName: col, Mode: mode})
		if err != nil {
			t.Fatal(err)
		}
	}
	logColumns := []string{"request_date", "service", "private_label"}
	tables["http_log"] = map[string]any{"Worksheet": true, "UUID": "cb200000-0000-4000-8000-000000000008", "Columns": logColumns}
	for _, col := range logColumns {
		mode := model.ResultDisclosureRawCopyAllowed
		if col == "private_label" {
			mode = model.ResultDisclosureMaskedNoCopy
		}
		if _, err := policies.Insert(ctx, model.ResultDisclosurePolicyUpsertRequest{TargetResourceID: target.ID, DatabaseName: "g2", ObjectName: "http_log", ColumnName: col, Mode: mode}); err != nil {
			t.Fatal(err)
		}
	}
	tables["page_events"] = map[string]any{"Worksheet": true, "UUID": "cb200000-0000-4000-8000-000000000009", "Columns": []string{"request_id"}}
	if _, err := policies.Insert(ctx, model.ResultDisclosurePolicyUpsertRequest{TargetResourceID: target.ID, DatabaseName: "g2", ObjectName: "page_events", ColumnName: "request_id", Mode: model.ResultDisclosureRawCopyAllowed}); err != nil {
		t.Fatal(err)
	}
	planCols := []string{"request_date", "id", "private_label"}
	tables["plan_events"] = map[string]any{"Worksheet": true, "UUID": "cb200000-0000-4000-8000-000000000010", "Columns": planCols}
	for _, col := range planCols {
		mode := model.ResultDisclosureRawCopyAllowed
		if col == "private_label" {
			mode = model.ResultDisclosureMaskedNoCopy
		}
		if _, err := policies.Insert(ctx, model.ResultDisclosurePolicyUpsertRequest{TargetResourceID: target.ID, DatabaseName: "g2", ObjectName: "plan_events", ColumnName: col, Mode: mode}); err != nil {
			t.Fatal(err)
		}
	}
	manifest := map[string]any{"TargetID": target.ID, "Environment": "Staging", "Address": remote.Addr, "Database": "g2", "User": "preview_reader", "CredentialRef": "G3_FIXTURE", "Users": []uint64{editor}, "Tables": tables}
	publish := func() {
		b, _ := json.Marshal(manifest)
		if err := os.WriteFile(filepath.Join(dir, "active.json"), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	publish()
	dsn := "clickhouses://preview_reader:" + remote.Password + "@" + remote.Addr + "/g2"
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_G3_FIXTURE", dsn)
	cfg := config.Config{JWTSecret: authzIntegrationSecret, CHPreviewDir: dir}
	deps, _ := serverboot.Build(db, cfg)
	h := api.NewRouter(deps)
	token := mustLogin(t, h, email, "secret123")
	adminToken := mustLogin(t, h, "preview-admin@example.com", "secret123")
	otherToken := mustLogin(t, h, "preview-other@example.com", "secret123")
	path := fmt.Sprintf("/query-targets/%d/clickhouse-preview", target.ID)
	cred := fmt.Sprintf("/query-targets/%d/credential", target.ID)
	rec := doBearerWithBody(t, h, "PUT", cred, adminToken, `{"credentialRef":"G3_FIXTURE","enabled":true,"environmentPolicy":"non_prod_only"}`)
	if rec.Code != 200 {
		t.Fatalf("credential setup: %d %s", rec.Code, rec.Body.String())
	}
	assertActions := func(handler http.Handler, bearer string, preview, run bool) {
		t.Helper()
		listed := doBearer(t, handler, "GET", fmt.Sprintf("/query-targets?targetId=%d", target.ID), bearer)
		var results model.QueryTargetListResponse
		if listed.Code != 200 || json.Unmarshal(listed.Body.Bytes(), &results) != nil || len(results.Items) != 1 || results.Items[0].ResourceID != target.ID {
			t.Fatalf("target discovery returned %d: %s", listed.Code, listed.Body.String())
		}
		actions := results.Items[0].AvailableActions
		if actions.ClickHousePreview != preview || actions.Run != run || actions.Export != run || actions.Explain != run {
			t.Fatalf("actions=%+v; want preview=%t, run/export=%t", actions, preview, run)
		}
	}
	assertActions(h, token, true, true)
	assertActions(h, otherToken, false, false)
	// Table preview by itself must never advertise worksheet export.
	manifest["Tables"] = map[string]any{"sample": tables["sample"]}
	publish()
	assertActions(h, token, true, false)
	manifest["Tables"] = tables
	publish()
	var catalog struct{ Generation string }
	get := func(handler http.Handler, bearer string, want int) {
		t.Helper()
		r := doBearer(t, handler, "GET", path, bearer)
		if r.Code != want {
			t.Fatalf("catalog status %d want %d: %s", r.Code, want, r.Body.String())
		}
		if want == 200 && (json.Unmarshal(r.Body.Bytes(), &catalog) != nil || len(catalog.Generation) != 64) {
			t.Fatal("missing catalog generation")
		}
		if want == 200 && !strings.Contains(r.Body.String(), `"sample"`) {
			t.Fatal("catalog omitted approved table")
		}
	}
	get(h, token, 200)
	get(h, otherToken, 403)
	get(h, "", 401)
	off, _ := serverboot.Build(db, config.Config{JWTSecret: authzIntegrationSecret})
	closedRouter := api.NewRouter(off)
	get(closedRouter, token, 404)
	assertActions(closedRouter, token, false, false)
	lock := flock.New(filepath.Join(dir, "lease"))
	if err := lock.Lock(); err != nil {
		t.Fatal(err)
	}
	get(h, token, 403)
	lock.Unlock()
	manifest["Environment"] = "Production"
	publish()
	get(h, token, 403)
	manifest["Environment"] = "Staging"
	publish()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_G3_FIXTURE", dsn+"-wrong")
	get(h, token, 403)
	assertActions(h, token, true, false)
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_G3_FIXTURE", dsn)
	count := func(table string) int {
		t.Helper()
		where := ""
		if table == "audit_events" {
			where = " AND event_type='query.executed'"
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM "+table+" WHERE target_resource_id=?"+where, target.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	for _, tc := range []struct {
		name, table   string
		limit, status int
	}{
		{"success", "sample", 20, 200}, {"stale_generation", "sample", 20, 403}, {"denied", "sample", 20, 403}, {"missing_policy", "sample", 20, 403}, {"limit", "sample", 21, 400}, {"column_limit", "sample", 20, 400}, {"cell_limit", "large_cell", 20, 400}, {"encoded_budget", "large_reply", 20, 400}, {"server_budget", "server_budget", 20, 400}, {"late_failure", "late", 20, 502}, {"remote_timeout", "slow", 20, 408}, {"cancel", "slow", 20, 502}, {"deadline", "slow", 20, 408}, {"audit_pending", "sample", 20, 200}, {"audit_failure", "sample", 20, 502}} {
		t.Run(tc.name, func(t *testing.T) {
			bearer := token
			cols := []string{"id", "title", "secret"}
			if tc.name == "denied" {
				bearer = otherToken
			}
			if tc.name == "column_limit" {
				cols = make([]string, 9)
			}
			if tc.name == "missing_policy" {
				if err := policies.Delete(ctx, target.ID, "g2", "", "sample", "title"); err != nil {
					t.Fatal(err)
				}
				defer func() {
					_, err := policies.Insert(ctx, model.ResultDisclosurePolicyUpsertRequest{TargetResourceID: target.ID, DatabaseName: "g2", ObjectName: "sample", ColumnName: "title", Mode: model.ResultDisclosureRawCopyAllowed})
					if err != nil {
						t.Error(err)
					}
				}()
			}
			before, events := count("query_executions"), count("audit_events")
			if tc.name == "audit_failure" {
				_, err := db.Exec(`CREATE TRIGGER ch_preview_fail BEFORE INSERT ON audit_events FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture failure'`)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Exec(`DROP TRIGGER ch_preview_fail`)
			}
			var holder *sql.Conn
			if tc.name == "audit_pending" {
				holder, err = db.Conn(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer holder.Close()
				if _, err := holder.ExecContext(ctx, `DO GET_LOCK('ch_preview_gate',0)`); err != nil {
					t.Fatal(err)
				}
				_, err = db.Exec(`CREATE TRIGGER ch_preview_hold BEFORE INSERT ON audit_events FOR EACH ROW BEGIN IF GET_LOCK('ch_preview_gate',4)<>1 THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='fixture timeout'; END IF; DO RELEASE_LOCK('ch_preview_gate'); END`)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Exec(`DROP TRIGGER ch_preview_hold`)
				defer holder.ExecContext(ctx, `DO RELEASE_LOCK('ch_preview_gate')`)
			}
			generation := catalog.Generation
			if tc.name == "stale_generation" {
				generation = strings.Repeat("0", 64)
			}
			body, _ := json.Marshal(map[string]any{"generation": generation, "table": tc.table, "columns": cols, "limit": tc.limit})
			r := httptest.NewRequest("POST", path, strings.NewReader(string(body)))
			r.Header.Set("Authorization", "Bearer "+bearer)
			attempt, cancel := context.WithCancel(ctx)
			defer cancel()
			if tc.name == "deadline" {
				var stop context.CancelFunc
				attempt, stop = context.WithTimeout(attempt, 800*time.Millisecond)
				defer stop()
			}
			w := httptest.NewRecorder()
			done := make(chan struct{})
			go func() { h.ServeHTTP(w, r.WithContext(attempt)); close(done) }()
			if tc.name == "cancel" {
				time.AfterFunc(800*time.Millisecond, cancel)
			}
			if holder != nil {
				for end := time.Now().Add(1500 * time.Millisecond); ; {
					var n int
					if err := db.QueryRow(`SELECT count(*) FROM performance_schema.metadata_locks WHERE object_type='USER LEVEL LOCK' AND object_name='ch_preview_gate' AND lock_status='PENDING'`).Scan(&n); err != nil {
						t.Fatal(err)
					}
					if n == 1 {
						break
					}
					if time.Now().After(end) {
						t.Fatal("audit never blocked")
					}
					time.Sleep(10 * time.Millisecond)
				}
				select {
				case <-done:
					t.Fatal("premature delivery")
				default:
				}
				if count("query_executions") != before || count("audit_events") != events {
					t.Fatal("partial evidence visible")
				}
				holder.ExecContext(ctx, `DO RELEASE_LOCK('ch_preview_gate')`)
			}
			<-done
			if w.Code != tc.status {
				t.Fatalf("status %d want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "synthetic-private") || strings.Contains(w.Body.String(), "fixture failure") {
				t.Fatal("private data in response")
			}
			if w.Code == 200 {
				var data struct {
					Columns []model.QueryResultColumn
					Rows    [][]any
				}
				if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil || len(data.Rows) != 2 || len(data.Columns) != 3 || data.Columns[2].CopyAllowed || !data.Columns[1].CopyAllowed {
					t.Fatal("invalid result")
				}
				found := false
				nulls := false
				for _, row := range data.Rows {
					found = found || (row[0] == float64(1) && row[1] == "研发" && row[2] == "[MASKED]")
					nulls = nulls || (row[0] == float64(2) && row[1] == nil && row[2] == nil)
				}
				if !found || !nulls {
					t.Fatal("incorrect value, masking or NULL")
				}
			} else if strings.Contains(w.Body.String(), `"rows"`) {
				t.Fatal("partial data delivered")
			}
			delta := 1
			if tc.name == "audit_failure" {
				delta = 0
			}
			if count("query_executions") != before+delta || count("audit_events") != events+delta {
				t.Fatal("incorrect evidence delta")
			}
			if delta == 1 {
				var status, remote string
				var n int
				var statement sql.NullString
				err := db.QueryRow(`SELECT status,remote_state,row_count,full_statement FROM query_executions WHERE target_resource_id=? ORDER BY id DESC LIMIT 1`, target.ID).Scan(&status, &remote, &n, &statement)
				if err != nil || statement.Valid || (w.Code != 200 && n != 0) {
					t.Fatal("incorrect history")
				}
				if tc.name == "server_budget" && remote != "stopped" {
					t.Fatal("oversized block must be rejected by the server")
				}
				if tc.name == "cancel" && status != "cancelled" || (tc.name == "remote_timeout" || tc.name == "deadline") && status != "timeout" {
					t.Fatal("unexpected cancellation/timeout status", status)
				}
				if tc.name == "deadline" || tc.name == "cancel" {
					if remote != "unknown" && remote != "stopped" {
						t.Fatal("invalid remote evidence", remote)
					}
				}
				t.Logf("status=%s remote=%q delivered=%t evidence=1/1", status, remote, w.Code == 200)
			}
		})
	}
	t.Run("daily_counts", func(t *testing.T) { testCHDailyCount(t, h, path, token, catalog.Generation, db, target.ID) })
	t.Run("worksheet", func(t *testing.T) { testCHWorksheet(t, h, target.ID, token, otherToken, catalog.Generation, db) })
	t.Run("worksheet_paging", func(t *testing.T) { testCHWorksheetPages(t, h, target.ID, token, catalog.Generation, db) })
	t.Run("explain", func(t *testing.T) { testCHExplain(t, h, target.ID, token, otherToken, catalog.Generation, db) })
	if front := os.Getenv("CONTROLHUB_CH_BROWSER_CHECKOUT"); front != "" && !t.Failed() {
		srv := httptest.NewServer(h)
		defer srv.Close()
		cmd := exec.Command("node", "scripts/e2e/clickhouse-preview.mjs", srv.URL, strconv.FormatUint(target.ID, 10), email)
		cmd.Dir = front
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatal(err)
		}
	}
}
