//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/config"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/serverboot"
)

func TestRedisWorksheetLimitsHTTP(t *testing.T) {
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
	if err := redis.expect(":2", "HSET", hashKey, "status", "open", "region", "cn-east"); err != nil {
		t.Fatal(err)
	}

	host, portText, _ := net.SplitHostPort(address)
	port, _ := strconv.Atoi(portText)
	resources := mysql.NewResourceRepository(db)
	target, err := resources.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType: model.ResourceTypeDatabaseInstance, ResourceSubtype: "redis", Name: fmt.Sprintf("redis-worksheet-limits-%d", time.Now().UnixNano()), DisplayName: "Redis Limits Fixture",
		EnvironmentID: envStaging, OwnerID: ownerDBA, LifecycleStatus: model.LifecycleStatusRunning, HealthStatus: model.HealthStatusHealthy, Source: "manual", Labels: map[string]string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := resources.UpsertDatabaseInstanceProfile(ctx, target.ID, "redis", "8.10.3", host, port, "primary"); err != nil {
		t.Fatal(err)
	}
	const email = "redis-limits-editor@example.com"
	const otherEmail = "redis-limits-other@example.com"
	user := insertAuthzTestUser(t, db, email, "editor")
	other := insertAuthzTestUser(t, db, otherEmail, "editor")
	third := insertAuthzTestUser(t, db, "redis-limits-third@example.com", "editor")
	admin := insertAuthzTestUser(t, db, "redis-limits-admin@example.com", "admin")
	t.Cleanup(func() {
		for _, id := range []uint64{user, other, third, admin} {
			deleteAuthzTestUser(t, db, id)
		}
	})

	raw := model.ResultDisclosureRawCopyAllowed
	manifestPath := filepath.Join(t.TempDir(), "active.json")
	manifest := map[string]any{
		"targetId": target.ID, "address": address, "environment": "Staging", "user": "reader",
		"credentialRef": "REDIS_WORKSHEET_FIXTURE", "caPem": ca, "users": []uint64{user, other, third},
		"keys":       map[string]any{"demo:order:note:101": string(raw)},
		"hashFields": map[string]any{hashKey: map[string]any{"status": string(raw), "region": string(raw)}},
		"limits": map[string]any{
			"maxConcurrentRuns":        16,
			"maxConcurrentRunsPerUser": 1,
			"runsPerSecond":            50,
			"runsPerMinutePerUser":     600,
		},
	}
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
	setLimits := func(limits map[string]any) {
		t.Helper()
		if limits == nil {
			delete(manifest, "limits")
		} else {
			manifest["limits"] = limits
		}
		if err := publish(); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { setLimits(nil) })

	dsn := url.URL{Scheme: "rediss", Host: address, User: url.UserPassword("reader", readSecret), Path: "/0"}
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_REDIS_WORKSHEET_FIXTURE", dsn.String())
	t.Setenv("CONTROLHUB_REDIS_WORKSHEET_CONFIG", manifestPath)
	t.Setenv("CONTROLHUB_ENVIRONMENT", "nonproduction")
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	deps, _ := serverboot.Build(db, config.Config{JWTSecret: authzIntegrationSecret, RedisWorksheetPath: cfg.RedisWorksheetPath})
	h := api.NewRouter(deps)
	bearer := mustLogin(t, h, email, "secret123")
	otherBearer := mustLogin(t, h, otherEmail, "secret123")
	thirdBearer := mustLogin(t, h, "redis-limits-third@example.com", "secret123")
	adminBearer := mustLogin(t, h, "redis-limits-admin@example.com", "secret123")
	path := fmt.Sprintf("/query-targets/%d", target.ID)
	if r := doBearerWithBody(t, h, "PUT", path+"/credential", adminBearer,
		`{"credentialRef":"REDIS_WORKSHEET_FIXTURE","enabled":true,"environmentPolicy":"non_prod_only"}`); r.Code != 200 {
		t.Fatalf("credential setup: %d %s", r.Code, r.Body)
	}

	// connCount reads INFO stats fully and parses total_connections_received.
	connCount := func() int {
		t.Helper()
		if _, err := redis.Write([]byte("*2\r\n$4\r\nINFO\r\n$5\r\nstats\r\n")); err != nil {
			t.Fatal(err)
		}
		header, err := redis.line()
		if err != nil || !strings.HasPrefix(header, "$") {
			t.Fatalf("INFO stats header: %q: %v", header, err)
		}
		n, err := strconv.Atoi(header[1:])
		if err != nil || n > 1<<20 {
			t.Fatalf("INFO stats bulk length: %q", header)
		}
		payload := make([]byte, n+2)
		if _, err := io.ReadFull(redis.reader, payload); err != nil {
			t.Fatal(err)
		}
		for line := range strings.Lines(string(payload)) {
			if v, ok := strings.CutPrefix(line, "total_connections_received:"); ok {
				parsed, err := strconv.Atoi(strings.TrimSpace(v))
				if err != nil {
					t.Fatal(err)
				}
				return parsed
			}
		}
		t.Fatal("total_connections_received missing from INFO stats")
		return 0
	}

	run := func(token, statement string) (int, http.Header, string) {
		t.Helper()
		body, err := json.Marshal(model.QueryExecuteRequest{Statement: statement})
		if err != nil {
			t.Fatal(err)
		}
		res := doBearerWithBody(t, h, "POST", path+"/execute", token, string(body))
		return res.Code, res.Header(), res.Body.String()
	}
	// flood fires n parallel Runs and counts HTTP statuses. It never calls
	// t.Fatal from worker goroutines.
	flood := func(token, statement string, n int) map[int]int {
		t.Helper()
		body, err := json.Marshal(model.QueryExecuteRequest{Statement: statement})
		if err != nil {
			t.Fatal(err)
		}
		codes := make([]int, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				res := doBearerWithBody(t, h, "POST", path+"/execute", token, string(body))
				codes[i] = res.Code
			}(i)
		}
		wg.Wait()
		counts := map[int]int{}
		for _, code := range codes {
			counts[code]++
		}
		return counts
	}
	expectCodes := func(got, want map[int]int) {
		t.Helper()
		for code, n := range want {
			if got[code] != n {
				t.Fatalf("HTTP %d count %d, want %d (all: %v)", code, got[code], n, got)
			}
		}
	}

	// Per-user concurrency: one in-flight Run per user; the rest are refused
	// before any Redis connection.
	before := connCount()
	if err := redis.expect("+OK", "CLIENT", "PAUSE", "1200", "ALL"); err != nil {
		t.Fatal(err)
	}
	expectCodes(flood(bearer, "HGET "+hashKey+" status", 10), map[int]int{200: 1, 429: 9})
	if delta := connCount() - before; delta != 1 {
		t.Fatalf("per-user flood opened %d Redis connections, want 1", delta)
	}

	// Target concurrency is shared across users under the same pause.
	setLimits(map[string]any{"maxConcurrentRuns": 2, "maxConcurrentRunsPerUser": 4, "runsPerSecond": 50, "runsPerMinutePerUser": 600})
	before = connCount()
	if err := redis.expect("+OK", "CLIENT", "PAUSE", "1200", "ALL"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var first, second map[int]int
	wg.Add(2)
	go func() { defer wg.Done(); first = flood(bearer, "HGET "+hashKey+" status", 5) }()
	go func() { defer wg.Done(); second = flood(otherBearer, "HGET "+hashKey+" status", 5) }()
	wg.Wait()
	merged := map[int]int{}
	for _, counts := range []map[int]int{first, second} {
		for code, n := range counts {
			merged[code] += n
		}
	}
	expectCodes(merged, map[int]int{200: 2, 429: 8})
	if delta := connCount() - before; delta != 2 {
		t.Fatalf("target flood opened %d Redis connections, want 2", delta)
	}

	// A Run slot held through a timeout is released: the next Run after the
	// pause ends is admitted and reaches Redis.
	setLimits(map[string]any{"maxConcurrentRuns": 1, "maxConcurrentRunsPerUser": 1})
	if err := redis.expect("+OK", "CLIENT", "PAUSE", "3000", "ALL"); err != nil {
		t.Fatal(err)
	}
	if code, _, body := run(bearer, "HGET "+hashKey+" status"); code != 408 || !strings.Contains(body, `"error":"query_timeout"`) {
		t.Fatalf("paused Run: %d %s", code, body)
	}
	// The 2s attempt deadline already freed the slot. CLIENT UNPAUSE is
	// deterministic either way: if it is exempt from the pause it lifts it
	// now, and if it is queued its +OK arrives only once the pause ends.
	if err := redis.expect("+OK", "CLIENT", "UNPAUSE"); err != nil {
		t.Fatal(err)
	}
	if code, _, body := run(bearer, "HGET "+hashKey+" status"); code != 200 || !strings.Contains(body, "open") {
		t.Fatalf("post-pause Run: %d %s", code, body)
	}

	// Per-user minute bucket: three sequential Runs then refusal; the other
	// user's independent bucket is unaffected.
	setLimits(map[string]any{"runsPerMinutePerUser": 3})
	for i := 0; i < 3; i++ {
		if code, _, body := run(bearer, "HGET "+hashKey+" status"); code != 200 {
			t.Fatalf("minute-bucket Run %d: %d %s", i+1, code, body)
		}
	}
	if code, header, body := run(bearer, "HGET "+hashKey+" status"); code != 429 || header.Get("Retry-After") != "1" || !strings.Contains(body, `"error":"query_rate_limited"`) {
		t.Fatalf("minute-bucket refusal: %d %v %s", code, header, body)
	}
	if code, _, body := run(otherBearer, "HGET "+hashKey+" status"); code != 200 {
		t.Fatalf("other user Run after minute refusal: %d %s", code, body)
	}

	// The per-second bucket is shared across users on one target; the third
	// user's full minute bucket proves the refusal lands on the shared bucket.
	setLimits(map[string]any{"runsPerSecond": 2})
	if code, _, body := run(thirdBearer, "HGET "+hashKey+" status"); code != 200 {
		t.Fatalf("second-bucket Run 1: %d %s", code, body)
	}
	if code, _, body := run(thirdBearer, "HGET "+hashKey+" status"); code != 200 {
		t.Fatalf("second-bucket Run 2: %d %s", code, body)
	}
	if code, header, body := run(thirdBearer, "HGET "+hashKey+" status"); code != 429 || header.Get("Retry-After") != "1" || !strings.Contains(body, `"error":"query_rate_limited"`) {
		t.Fatalf("shared second-bucket refusal: %d %v %s", code, header, body)
	}

	// Restore default limits so nothing else in the test is affected;
	// restore-on-republish and refill math are covered by unit tests.
	setLimits(nil)

	// Evidence: every refusal is a rejected/query_rate_limited pair with a
	// fixed preview and no backend contact, and nothing leaked.
	var limited int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND status='rejected' AND error_code='query_rate_limited'`, target.ID).Scan(&limited); err != nil || limited != 9+8+1+1 {
		t.Fatalf("query_rate_limited evidence rows: %d: %v", limited, err)
	}
	var leaks int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND error_code='query_rate_limited' AND (statement_preview NOT IN ('Redis GET','Redis HGET') OR full_statement IS NOT NULL OR statement_digest<>'')`, target.ID).Scan(&leaks); err != nil || leaks != 0 {
		t.Fatalf("rate-limited evidence leaked: %d: %v", leaks, err)
	}
	var remoteContacted int
	if err := db.QueryRow(`SELECT count(*) FROM query_executions WHERE target_resource_id=? AND error_code='query_rate_limited' AND remote_state<>''`, target.ID).Scan(&remoteContacted); err != nil || remoteContacted != 0 {
		t.Fatalf("rate-limited rows claim backend contact: %d: %v", remoteContacted, err)
	}
}
