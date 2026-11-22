//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/serverboot"
)

func TestRedisWorksheetHTTP(t *testing.T) {
	db := setupTestDB(t)
	ctx := t.Context()
	address, adminSecret, readSecret, ca, tlsConfig := startRedisFixture(t)
	seedCtx, cancelSeed := context.WithTimeout(ctx, 20*time.Second)
	defer cancelSeed()
	redis, err := dialRedisFixture(seedCtx, address, "fixture", adminSecret, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = redis.Close() })
	const hashKey = "demo:order:101"
	const key1, key2 = "demo:order:note:101", "demo:order:note:102"
	for key, value := range map[string]string{key1: "fixture-open", key2: "fixture-shipped", "demo:order:empty": "", "demo:order:masked": "fixture-mask-secret", "demo:order:large": strings.Repeat("x", 4096), "demo:order:binary": "\xff", "demo:private:101": "fixture-private"} {
		if err := redis.expect("+OK", "SET", key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := redis.expect(":7", "HSET", hashKey,
		"status", "open", "region", "cn-east", "empty", "", "masked", "fixture-mask-secret",
		"large", strings.Repeat("x", 4096), "binary", "\xff", "secret", "fixture-private"); err != nil {
		t.Fatal(err)
	}
	reader, err := dialRedisFixture(seedCtx, address, "reader", readSecret, tlsConfig)
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"GETRANGE", "demo:private:101", "0", "3"}, {"SET", key1, "changed"}, {"HSET", hashKey, "status", "x"}, {"HGETALL", hashKey}, {"HGET", "demo:private:101", "status"}, {"SELECT", "1"}, {"SCAN", "0"}} {
		if reply, err := reader.command(args...); err != nil || !strings.HasPrefix(reply, "-NOPERM ") {
			t.Fatal("Redis reader ACL did not refuse", args[0], err)
		}
	}
	reader.Close()

	host, portText, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portText)
	resources := mysql.NewResourceRepository(db)
	target, err := resources.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "redis", Name: "redis-worksheet-fixture", DisplayName: "Redis Worksheet Fixture",
		EnvironmentID: envStaging, OwnerID: ownerDBA, LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual", Labels: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.UpsertDatabaseInstanceProfile(ctx, target.ID, "redis", "8.10.3", host, port, "primary"); err != nil {
		t.Fatal(err)
	}
	const email = "redis-worksheet-editor@example.com"
	user := insertAuthzTestUser(t, db, email, "editor")
	other := insertAuthzTestUser(t, db, "redis-worksheet-other@example.com", "editor")
	admin := insertAuthzTestUser(t, db, "redis-worksheet-admin@example.com", "admin")
	t.Cleanup(func() {
		for _, id := range []uint64{user, other, admin} {
			deleteAuthzTestUser(t, db, id)
		}
	})

	raw := model.ResultDisclosureRawCopyAllowed
	// keys and hashFields never authorize each other: demo:order:101 is approved
	// for GET only so the statement can reach Redis and hit WRONGTYPE.
	keys := map[string]model.ResultDisclosureMode{hashKey: raw, key1: raw, key2: raw, "demo:order:empty": raw, "demo:order:missing": raw, "demo:order:large": raw, "demo:order:binary": raw, "demo:order:masked": model.ResultDisclosureMaskedNoCopy}
	hashFields := map[string]map[string]model.ResultDisclosureMode{
		hashKey: {"status": raw, "region": raw, "empty": raw, "missing": raw, "large": raw, "binary": raw, "masked": model.ResultDisclosureMaskedNoCopy},
		key1:    {"status": raw},
	}
	manifestPath := filepath.Join(t.TempDir(), "active.json")
	manifest := map[string]any{"targetId": target.ID, "address": address, "environment": "Staging", "user": "reader", "credentialRef": "REDIS_WORKSHEET_FIXTURE", "caPem": ca, "users": []uint64{user}, "keys": keys, "hashFields": hashFields}
	publish := func() error {
		contents, err := json.Marshal(manifest)
		if err != nil {
			return err
		}
		next := manifestPath + ".next"
		if err := os.WriteFile(next, contents, 0600); err != nil {
			return err
		}
		return os.Rename(next, manifestPath)
	}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	dsn := url.URL{Scheme: "rediss", Host: address, User: url.UserPassword("reader", readSecret), Path: "/0"}
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_REDIS_WORKSHEET_FIXTURE", dsn.String())
	t.Setenv("CONTROLHUB_REDIS_WORKSHEET_CONFIG", manifestPath)
	t.Setenv("CONTROLHUB_ENVIRONMENT", "nonproduction")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	// Wire just the tested feature against the disposable database.
	deps, _ := serverboot.Build(db, config.Config{JWTSecret: authzIntegrationSecret, RedisWorksheetPath: cfg.RedisWorksheetPath})
	h := api.NewRouter(deps)
	bearer := mustLogin(t, h, email, "secret123")
	otherBearer := mustLogin(t, h, "redis-worksheet-other@example.com", "secret123")
	adminBearer := mustLogin(t, h, "redis-worksheet-admin@example.com", "secret123")
	path := fmt.Sprintf("/query-targets/%d", target.ID)
	const cred = `{"credentialRef":"REDIS_WORKSHEET_FIXTURE","enabled":true,"environmentPolicy":"non_prod_only"}`
	put := func(body string, status int) {
		t.Helper()
		r := doBearerWithBody(t, h, "PUT", path+"/credential", adminBearer, body)
		if r.Code != status {
			t.Fatalf("credential setup %d, want %d: %s", r.Code, status, r.Body)
		}
	}
	put(strings.Replace(cred, "non_prod_only", "all_environments", 1), 400)
	put(cred, 200)
	assertTarget := func(handler http.Handler, token string, run bool) {
		t.Helper()
		r := doBearer(t, handler, "GET", fmt.Sprintf("/query-targets?targetId=%d", target.ID), token)
		var list model.QueryTargetListResponse
		if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
			t.Fatalf("Redis target discovery failed: %d %s", r.Code, r.Body)
		}
		item := list.Items[0]
		a := item.AvailableActions
		if a.Run != run || a.Export || a.Explain || a.SaveSheet || item.Capability.EditorMode != "redis" {
			t.Fatalf("unexpected Redis target actions: %+v", a)
		}
		if !run {
			if len(item.SchemaPreview) != 0 {
				t.Fatal("Redis object list did not honor approval")
			}
			return
		}
		wantKeys := len(keys)
		for key := range hashFields {
			if _, dup := keys[key]; !dup {
				wantKeys++
			}
		}
		if len(item.SchemaPreview) != wantKeys || item.SchemaPreview[0].Name != hashKey || item.SchemaPreview[0].Kind != "redis_key" {
			t.Fatal("Redis object list did not honor approval")
		}
		wantChildren := map[string][]string{hashKey: {"binary", "empty", "large", "masked", "missing", "region", "status"}, key1: {"status"}}
		for _, node := range item.SchemaPreview {
			want, approved := wantChildren[node.Name]
			var got []string
			for _, child := range node.Children {
				if child.Kind != "redis_hash_field" {
					t.Fatalf("unexpected Redis object child kind: %+v", child)
				}
				got = append(got, child.Name)
			}
			if !slices.Equal(got, want) || (!approved && len(got) != 0) {
				t.Fatalf("Redis hash field list for %s: %v", node.Name, got)
			}
		}
	}
	assertTarget(h, bearer, true)
	assertTarget(h, otherBearer, false)
	off, _ := serverboot.Build(db, config.Config{JWTSecret: authzIntegrationSecret})
	closed := api.NewRouter(off)
	assertTarget(closed, bearer, false)
	run := func(handler http.Handler, token string, request model.QueryExecuteRequest, status int, code string) model.QueryExecuteResponse {
		t.Helper()
		body, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		r := doBearerWithBody(t, handler, "POST", path+"/execute", token, string(body))
		if r.Code != status {
			t.Fatalf("Redis Run %d, want %d: %s", r.Code, status, r.Body)
		}
		var out model.QueryExecuteResponse
		if status == 200 {
			if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil || out.ExecutionID == 0 || out.Engine != "redis" || len(out.Rows) != 1 || len(out.Rows[0]) != 1 || len(out.Columns) != 1 || out.Pagination != nil ||
				len(out.CellTruncated) != 1 || len(out.CellTruncated[0]) != 1 || out.CellTruncated[0][0] {
				t.Fatalf("unexpected Redis result contract: %s", r.Body)
			}
		} else if !strings.Contains(r.Body.String(), `"error":"`+code+`"`) || strings.Contains(r.Body.String(), `"rows"`) || strings.Contains(r.Body.String(), "demo:") || strings.Contains(r.Body.String(), "fixture-") {
			t.Fatalf("unexpected Redis error contract: %s", r.Body)
		}
		return out
	}
	request := func(command string) model.QueryExecuteRequest { return model.QueryExecuteRequest{Statement: command} }
	run(closed, bearer, request("GET "+key1), 403, "query_not_allowed")
	run(h, otherBearer, request("GET "+key1), 403, "query_not_allowed")
	for _, tc := range []struct {
		key   string
		value any
	}{{key1, "fixture-open"}, {key2, "fixture-shipped"}, {"demo:order:empty", ""}, {"demo:order:missing", nil}, {"demo:order:masked", "[MASKED]"}} {
		result := run(h, bearer, request("get\t"+tc.key+"\n"), 200, "")
		if result.Rows[0][0] != tc.value || (result.Columns[0].CopyAllowed != (tc.value != "[MASKED]")) {
			t.Fatal("unexpected Redis value or disclosure")
		}
	}
	for _, tc := range []struct {
		field string
		value any
	}{{"status", "open"}, {"region", "cn-east"}, {"empty", ""}, {"missing", nil}, {"masked", "[MASKED]"}} {
		result := run(h, bearer, request("hget\t"+hashKey+"\t"+tc.field+"\n"), 200, "")
		if result.Rows[0][0] != tc.value || (result.Columns[0].CopyAllowed != (tc.value != "[MASKED]")) {
			t.Fatal("unexpected Redis hash field value or disclosure")
		}
	}
	for _, statement := range []string{"GET demo:order:large", "GET demo:order:binary", "GET " + hashKey, "SET " + key1 + " changed", "GET " + key1 + "\nGET " + key2,
		"HGET " + hashKey + " large", "HGET " + hashKey + " binary", "HGET " + key1 + " status",
		"HSET " + hashKey + " status x", "HGETALL " + hashKey, "HGET " + hashKey + " status\nHGET " + hashKey + " region", "HGET " + hashKey} {
		run(h, bearer, request(statement), 400, "validation_failed")
	}
	run(h, bearer, request("GET demo:private:101"), 403, "query_result_disclosure_blocked")
	run(h, bearer, request("HGET "+hashKey+" secret"), 403, "query_result_disclosure_blocked")
	run(h, bearer, request("HGET demo:private:101 status"), 403, "query_result_disclosure_blocked")
	run(h, bearer, model.QueryExecuteRequest{Statement: "GET " + key1, Database: "1"}, 400, "validation_failed")
	run(h, bearer, model.QueryExecuteRequest{Statement: "GET " + key1, Pagination: &model.QueryExecutePaginationRequest{Page: 1, PageSize: 5}}, 400, "validation_failed")

	bad := dsn
	bad.Path = "/1"
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_REDIS_WORKSHEET_FIXTURE", bad.String())
	assertTarget(h, bearer, false)
	run(h, bearer, request("GET "+key1), 403, "query_not_allowed")
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_REDIS_WORKSHEET_FIXTURE", dsn.String())
	put(strings.Replace(cred, `"enabled":true`, `"enabled":false`, 1), 200)
	assertTarget(h, bearer, false)
	run(h, bearer, request("GET "+key1), 403, "query_not_allowed")
	put(cred, 200)
	if err := redis.expect("+OK", "ACL", "SETUSER", "reader", "resetkeys", "%R~demo:other:*"); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, request("GET "+key1), 403, "query_not_allowed")
	run(h, bearer, request("HGET "+hashKey+" status"), 403, "query_not_allowed")
	if err := redis.expect("+OK", "ACL", "SETUSER", "reader", "resetkeys", "%R~demo:order:*"); err != nil {
		t.Fatal(err)
	}

	if err := redis.expect("+OK", "CLIENT", "PAUSE", "3000", "ALL"); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, request("GET "+key1), 408, "query_timeout")
	if err := redis.expect("+OK", "CLIENT", "UNPAUSE"); err != nil {
		t.Fatal(err)
	}

	setKey := func(allow bool) error {
		if allow {
			keys[key1] = raw
		} else {
			delete(keys, key1)
		}
		return publish()
	}
	if err := setKey(false); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, request("GET "+key1), 403, "query_result_disclosure_blocked")
	if err := setKey(true); err != nil {
		t.Fatal(err)
	}
	delete(hashFields[hashKey], "status")
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, request("HGET "+hashKey+" status"), 403, "query_result_disclosure_blocked")
	hashFields[hashKey]["status"] = raw
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	manifest["users"] = []uint64{}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, request("HGET "+hashKey+" status"), 403, "query_not_allowed")
	manifest["users"] = []uint64{user}
	if err := publish(); err != nil {
		t.Fatal(err)
	}
	result := run(h, bearer, request("HGET "+hashKey+" status"), 200, "")
	if result.Rows[0][0] != "open" {
		t.Fatal("restored hash field approval did not run")
	}

	dropTrigger := func() error {
		_, err := db.Exec(`DROP TRIGGER IF EXISTS redis_worksheet_audit_failure`)
		return err
	}
	breakAudit := func() error {
		_, err := db.Exec(fmt.Sprintf(`CREATE TRIGGER redis_worksheet_audit_failure BEFORE INSERT ON audit_events FOR EACH ROW BEGIN IF NEW.target_resource_id = %d AND NEW.event_type = 'query.executed' THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'fixture audit failure'; END IF; END`, target.ID))
		return err
	}
	t.Cleanup(func() {
		if err := dropTrigger(); err != nil {
			t.Error(err)
		}
	})
	countPair := func() int {
		t.Helper()
		var hCount, aCount int
		if err := db.QueryRow(`SELECT (SELECT count(*) FROM query_executions WHERE target_resource_id=?),(SELECT count(*) FROM audit_events WHERE target_resource_id=? AND event_type='query.executed')`, target.ID, target.ID).Scan(&hCount, &aCount); err != nil || hCount != aCount {
			t.Fatalf("Redis evidence mismatch: history=%d audit=%d err=%v", hCount, aCount, err)
		}
		return hCount
	}
	before := countPair()
	if err := breakAudit(); err != nil {
		t.Fatal(err)
	}
	run(h, bearer, request("GET "+key1), 502, "query_backend_error")
	run(h, bearer, request("HGET "+hashKey+" status"), 502, "query_backend_error")
	if countPair() != before {
		t.Fatal("audit refusal left partial Redis evidence")
	}
	if err := dropTrigger(); err != nil {
		t.Fatal(err)
	}

	if front := os.Getenv("CONTROLHUB_REDIS_BROWSER_CHECKOUT"); front != "" && !t.Failed() {
		controlSecret := fmt.Sprintf("%x", randomRedisFixtureSecret(t))
		mux := http.NewServeMux()
		mux.HandleFunc("POST /redis-fixture/{action}", func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Fixture-Control") != controlSecret {
				http.NotFound(w, r)
				return
			}
			var err error
			switch r.PathValue("action") {
			case "revoke":
				delete(hashFields[hashKey], "status")
				err = setKey(false)
			case "restore":
				hashFields[hashKey]["status"] = raw
				err = setKey(true)
			case "break-audit":
				err = breakAudit()
			case "restore-audit":
				err = dropTrigger()
			default:
				http.NotFound(w, r)
				return
			}
			if err != nil {
				http.Error(w, "fixture control failed", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		})
		mux.Handle("/", h)
		server := httptest.NewServer(mux)
		defer server.Close()
		browserCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(browserCtx, "node", "scripts/e2e/redis-worksheet.mjs", server.URL, strconv.FormatUint(target.ID, 10), email)
		cmd.Dir = front
		cmd.Env = append(os.Environ(), "CONTROLHUB_REDIS_FIXTURE_SECRET="+controlSecret)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("Redis Chromium worksheet: %v", err)
		}
		if countPair() != before+13 {
			t.Fatal("browser Run outcomes did not match Redis evidence")
		}
	}
	var privateEvidence int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND (statement_preview LIKE '%demo:%' OR full_statement IS NOT NULL OR error_message LIKE '%fixture-%' OR statement_digest<>'')`, target.ID).Scan(&privateEvidence); err != nil || privateEvidence != 0 {
		t.Fatalf("Redis persisted protected evidence: %d: %v", privateEvidence, err)
	}
	var unlabeled int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND statement_preview NOT IN ('Redis GET','Redis HGET') AND NOT (statement_preview='' AND status='rejected' AND error_code='query_not_allowed')`, target.ID).Scan(&unlabeled); err != nil || unlabeled != 0 {
		t.Fatalf("Redis statement previews left the fixed labels: %d: %v", unlabeled, err)
	}
	var hgetRuns int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND statement_preview='Redis HGET'`, target.ID).Scan(&hgetRuns); err != nil || hgetRuns == 0 {
		t.Fatalf("Redis HGET history rows missing: %d: %v", hgetRuns, err)
	}
}
