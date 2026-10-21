//go:build integration

// Package integration — PG-HTTP-T12-R1 real-transport evidence: client disconnects, lock-wait timeouts, and a killed subprocess orphan claim; R2 adds the formal HTTP GET read-back of that claim.
// input: shared MySQL fixture, disposable PG16 with a slow seqscan table and a lockable gate table, the enabled assembly plus an independent reader router, both served over real sockets
// output: TestPGKeyedHTTP_ClientAbort*, TestPGKeyedHTTP_LockWaitTimeout, TestPGKeyedHTTP_OrphanClaimProcessExit; the child entry point pgOrphanChildMain is dispatched from TestMain under env guard; orphan read helpers pgReaderRouter/orphanHTTPGet* and backend-activity probes orphanDrainBackends/countPGBackends
// pos: group A/B of the R1 acceptance plus R2-A — HTTP disconnect and process death are proven as distinct facts, and the orphan's first read is the real GET /query-targets/{id}/executions through an independently wired authenticated router under wall and derived clocks; tampered responders prove the assertions are load-bearing
// note: if this file changes, update this header and module README.md.

package integration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/pgsql"
	mysqlrepo "github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// abortFixture is the shared substrate for group A: the enabled fixture plus
// a slow seqscan target table (planner overrides keep the result statement
// active long enough to observe) and a gate table a parent transaction can
// lock to produce a real lock-wait timeout.
type abortFixture struct {
	pgHTTPFixture
	admin  *pgx.Conn
	server *httptest.Server
}

func setupAbortFixture(t *testing.T) abortFixture {
	t.Helper()
	fx := setupPGHTTPFixture(t)
	admin := connectPG(t, fx.pg.host, fx.pg.port, fx.pg.db, pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE TABLE app.abort_scan (id bigint PRIMARY KEY, note text)`,
		`INSERT INTO app.abort_scan SELECT g, 'n' || g::text FROM generate_series(1, 5000000) g`,
		`CREATE TABLE app.abort_src (id bigint PRIMARY KEY, sid bigint,
		    CONSTRAINT fk_abort FOREIGN KEY (sid) REFERENCES app.abort_scan(id))`,
		`INSERT INTO app.abort_src VALUES (1, 1)`,
		`CREATE TABLE app.abort_gate (id bigint PRIMARY KEY, note text)`,
		`INSERT INTO app.abort_gate VALUES (1, 'gate')`,
		`GRANT SELECT ON app.abort_scan TO ` + pgDiscRole,
		`GRANT SELECT ON app.abort_src TO ` + pgDiscRole,
		`GRANT SELECT ON app.abort_gate TO ` + pgDiscRole,
		// Forced seqscan so a point lookup still scans 5M rows — the
		// rewritten result statement stays observably active.
		`ALTER ROLE ` + pgDiscRole + ` SET enable_indexscan = off`,
		`ALTER ROLE ` + pgDiscRole + ` SET enable_indexonlyscan = off`,
		`ALTER ROLE ` + pgDiscRole + ` SET enable_bitmapscan = off`,
		`ALTER ROLE ` + pgDiscRole + ` SET max_parallel_workers_per_gather = 0`,
	} {
		execPG(t, admin, stmt)
	}
	disc := mysqlrepo.NewQueryDisclosureRepository(fx.db)
	for _, col := range []string{"id", "note"} {
		insertPolicy(t, disc, fx.targetID, "discdb", "app", "abort_scan", col, model.ResultDisclosureRawCopyAllowed)
	}
	insertPolicy(t, disc, fx.targetID, "discdb", "app", "abort_gate", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, fx.targetID, "discdb", "app", "abort_gate", "note", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, fx.targetID, "discdb", "app", "abort_src", "id", model.ResultDisclosureRawCopyAllowed)

	server := httptest.NewServer(fx.enabled)
	t.Cleanup(func() {
		server.Close()
		_ = admin.Close(context.Background())
	})
	return abortFixture{pgHTTPFixture: fx, admin: admin, server: server}
}

// realPost issues a genuine HTTP request to the live server.
func (af abortFixture) realPost(t *testing.T, ctx context.Context, path, body string) (*http.Response, error) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, af.server.URL+path, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+af.token)
	req.Header.Set("Content-Type", "application/json")
	return af.server.Client().Do(req)
}

// awaitCancelled polls the persisted terminal row until the cancelled status
// lands — finalize runs on a detached window, so the row arrives after the
// client has already disconnected.
func (af abortFixture) awaitStatus(t *testing.T, key, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var status string
		err := af.db.QueryRow(
			`SELECT status FROM query_executions WHERE target_resource_id = ? AND client_execution_id = ?`,
			af.targetID, key).Scan(&status)
		if err == nil && status == want {
			return
		}
		if err != nil && err != sql.ErrNoRows {
			t.Fatalf("status poll: %v", err)
		}
		if time.Now().After(deadline) {
			t.Fatalf("key %s never reached status %q", key, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// getPointStatus reads the execution back through a fresh authenticated HTTP
// GET on the real socket and returns the item's persisted status/kind.
func (af abortFixture) getPointStatus(t *testing.T, key string) (kind, status string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/query-targets/%d/executions?clientExecutionId=%s", af.server.URL, af.targetID, key), nil)
	if err != nil {
		t.Fatalf("build get: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+af.token)
	resp, err := af.server.Client().Do(req)
	if err != nil {
		t.Fatalf("point get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("point get status = %d", resp.StatusCode)
	}
	var listed struct {
		Items []struct {
			Kind   string `json:"kind"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("point read items = %d, want exactly 1", len(listed.Items))
	}
	return listed.Items[0].Kind, listed.Items[0].Status
}

// assertBackendDrained waits until no active backend still runs the aborted
// result statement — the cancel protocol's server-side convergence.
func (af abortFixture) assertBackendDrained(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var n int64
		if err := af.admin.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_catalog.pg_stat_activity
			  WHERE application_name = 'ch-t6' AND state = 'active'
			    AND pid <> pg_backend_pid()
			    AND pg_catalog.strpos(query, $1) > 0`,
			marker).Scan(&n); err == nil && n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("aborted backend still active")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// abortCase drives one entry's real client-abort: the result statement is
// observed active on the server, the client's context is cancelled, and the
// cancelled terminal row is read back through a fresh GET. The key then
// refuses any second execution.
func (af abortFixture) abortCase(t *testing.T, name, key, path, body string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	early := make(chan string, 1)
	go func() {
		resp, err := af.realPost(t, ctx, path, body)
		if resp != nil {
			_ = resp.Body.Close()
		}
		early <- fmt.Sprintf("resp=%v err=%v", resp, err)
		done <- err
	}()
	// The witness marker appears only inside the rewritten result statement.
	pollActiveResultStatement(t, af.admin, "__chub_w", 0, early)
	cancel() // real client disconnect
	select {
	case err := <-done:
		if err == nil {
			t.Fatalf("%s: client request unexpectedly completed", name)
		}
	case <-time.After(30 * time.Second):
		t.Fatalf("%s: client abort never returned", name)
	}
	af.awaitStatus(t, key, string(model.QueryExecutionCancelled))
	if got := countWhere(t, af.db,
		`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id = ?
		 AND status = 'cancelled' AND error_code = 'query_cancelled'`,
		af.targetID, key); got != 1 {
		t.Fatalf("%s: cancelled terminal rows = %d, want 1", name, got)
	}
	kind, status := af.getPointStatus(t, key)
	if kind != "execution" || status != string(model.QueryExecutionCancelled) {
		t.Fatalf("%s: point read = %s/%s, want execution/cancelled", name, kind, status)
	}
	// Same key can never execute a second statement.
	rr := doBearerWithBody(t, af.enabled, http.MethodPost, path, af.token, body)
	if rr.Code != http.StatusConflict {
		t.Fatalf("%s: same-key retry = %d, want 409; body=%s", name, rr.Code, rr.Body.String())
	}
}

// TestPGKeyedHTTP_ClientAbort proves the real HTTP disconnect semantics per
// entry: the server-side result statement is observed active, the client
// connection drops, and the cancelled terminal evidence is what a fresh
// authorized GET reads back.
func TestPGKeyedHTTP_ClientAbort(t *testing.T) {
	af := setupAbortFixture(t)

	saved := createSavedPG(t, af.db, af.userID, af.targetID, model.QuerySavedStatementCreateRequest{
		Name: "abort-scan", Statement: `SELECT count(*) FROM app.abort_scan`,
		Scope: model.QuerySavedStatementPersonal,
	})

	af.abortCase(t, "execute", "r1-abort-x",
		af.execute,
		`{"statement":"SELECT count(*) FROM app.abort_scan","database":"discdb","clientExecutionId":"r1-abort-x"}`)
	af.abortCase(t, "saved", "r1-abort-s",
		fmt.Sprintf("%s/%d/execute?database=discdb", af.savedBase, saved.ID),
		`{"clientExecutionId":"r1-abort-s"}`)
	af.abortCase(t, "navigation", "r1-abort-n",
		af.nav,
		`{"source":{"database":"discdb","schema":"app","object":"abort_src","kind":"table","foreignKey":"fk_abort"},"localValues":["1"],"clientExecutionId":"r1-abort-n"}`)
	af.assertBackendDrained(t, "__chub_w")

	// Pre-send cancel: an already-cancelled context never reaches the wire —
	// no claim, no evidence.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := af.realPost(t, ctx, af.execute,
		`{"statement":"SELECT 1","database":"discdb","clientExecutionId":"r1-abort-pre"}`); err == nil {
		t.Fatal("pre-cancelled request unexpectedly sent")
	}
	if got := countWhere(t, af.db,
		`SELECT COUNT(*) FROM query_execution_claims WHERE client_execution_id = 'r1-abort-pre'`); got != 0 {
		t.Fatalf("pre-cancelled request occupied %d claims", got)
	}

	// Completion-before-cancel control: a fast statement finishes normally.
	rr := doBearerWithBody(t, af.enabled, http.MethodPost, af.execute, af.token,
		`{"statement":"SELECT id FROM app.orders WHERE id = 1","database":"discdb","clientExecutionId":"r1-abort-ok"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("control execute: %d; body=%s", rr.Code, rr.Body.String())
	}
}

// TestPGKeyedHTTP_LockWaitTimeout is the representative lock-wait timeout:
// a parent-held ACCESS EXCLUSIVE lock blocks the attempt's result statement
// until the armed statement budget fires — HTTP 408 and a persisted timeout.
func TestPGKeyedHTTP_LockWaitTimeout(t *testing.T) {
	af := setupAbortFixture(t)

	// ACCESS EXCLUSIVE conflicts with the attempt's ACCESS SHARE read — the
	// plain SELECT blocks without any forbidden locking clause in the request.
	lockConn := connectPG(t, af.pg.host, af.pg.port, af.pg.db, pgMetaAdminUser, pgMetaAdminPassword)
	execPG(t, lockConn, `BEGIN`)
	execPG(t, lockConn, `LOCK TABLE app.abort_gate IN ACCESS EXCLUSIVE MODE`)
	defer func() {
		execPG(t, lockConn, `ROLLBACK`)
		_ = lockConn.Close(context.Background())
	}()

	rr := doBearerWithBody(t, af.enabled, http.MethodPost, af.execute, af.token,
		`{"statement":"SELECT id, note FROM app.abort_gate WHERE id = 1","database":"discdb","clientExecutionId":"r1-timeout"}`)
	// The blocked statement burns the attempt's 5s budget.
	if rr.Code != http.StatusRequestTimeout {
		t.Fatalf("lock-wait timeout: %d, want 408; body=%s", rr.Code, rr.Body.String())
	}
	code, _ := decodeError(t, rr)
	if code != "query_timeout" {
		t.Fatalf("timeout code = %q, want query_timeout", code)
	}
	af.awaitStatus(t, "r1-timeout", string(model.QueryExecutionTimeout))

	// The attempt's lock wait and connection state converge — a later
	// request on the pool executes normally once the gate unlocks.
	execPG(t, lockConn, `ROLLBACK`)
	rr = doBearerWithBody(t, af.enabled, http.MethodPost, af.execute, af.token,
		`{"statement":"SELECT id, note FROM app.abort_gate WHERE id = 1","database":"discdb","clientExecutionId":"r1-timeout-after"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("post-timeout execute: %d; body=%s", rr.Code, rr.Body.String())
	}
}

// ---------------------------------------------------------------------------
// B. Process-exit orphan claim: a test-owned child process serves the real
// enabled router over a real socket; a parent-held table lock parks the
// attempt AFTER the claim commits and BEFORE finalize; SIGKILL leaves the
// claim occupied-but-unlinked. An independent reader then observes the claim
// running, and — under a clock advanced past the recorded budget+grace —
// unknown; no terminal row is ever fabricated.
// ---------------------------------------------------------------------------

// pgOrphanChildMain runs inside the re-executed test binary: it wires the
// production chain (same constructors as the fixture, WithPGHTTPEntries on)
// over the shared MySQL DSN and serves a real listener. It returns only on
// unexpected wiring failure — the parent kills it mid-request.
func pgOrphanChildMain(readyPath string) int {
	ctx := context.Background()
	dsn := os.Getenv("CH_T12_CHILD_MYSQL_DSN")
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return 2
	}
	if err := db.PingContext(ctx); err != nil {
		return 3
	}
	if _, err := pgsql.GuardPG("select 1"); err != nil {
		return 4
	}
	savedRepo := mysqlrepo.NewQuerySavedStatementRepository(db)
	targetRepo := mysqlrepo.NewQueryTargetRepository(db)
	guard := service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	execSvc := service.NewQueryExecutionService(
		targetRepo,
		mysqlrepo.NewQueryExecutionRepository(db),
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		guard,
		wallClock{},
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(disc, disc, service.NewMySQLSchemaInspector(), targetRepo),
	).WithPGClaimOrchestration(disc).
		WithTemplateExecution(savedRepo, service.NewTemplateStatementCompiler()).
		WithPGHTTPEntries()
	handler := api.NewRouter(api.Dependencies{
		AuthService:                service.NewAuthService(mysqlrepo.NewUserRepository(db), authzIntegrationSecret),
		AuditService:               service.NewAuditService(mysqlrepo.NewAuditRepository(db)),
		QueryExecutionService:      execSvc,
		QuerySavedStatementService: service.NewQuerySavedStatementService(savedRepo, savedRepo, targetRepo, guard),
		MachineCredentialService:   service.NewMachinePrincipalService(mysqlrepo.NewMachinePrincipalRepository(db)),
		QueryExecutionAuth:         api.QueryExecutionAuthConfig{Clock: time.Now},
	})
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return 5
	}
	if err := os.WriteFile(readyPath, []byte(strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)), 0o600); err != nil {
		return 6
	}
	srv := &http.Server{Handler: handler}
	if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
		return 7
	}
	return 0
}

// childHTTPLogin authenticates against the child over its real socket.
func childHTTPLogin(t *testing.T, base, email string) string {
	t.Helper()
	resp, err := http.Post(base+"/auth/login", "application/json",
		strings.NewReader(fmt.Sprintf(`{"email":%q,"password":"secret123"}`, email)))
	if err != nil {
		t.Fatalf("child login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("child login status = %d", resp.StatusCode)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || body.Token == "" {
		t.Fatalf("child login decode: %v token=%q", err, body.Token)
	}
	return body.Token
}

// fwdClock is the injected derived clock for the independent reader: its Now
// reports the shifted instant the claim-view derivation consumes.
type fwdClock struct{ t time.Time }

func (c fwdClock) Now() time.Time { return c.t }

// newPGReaderService builds an execution-read assembly — the independent
// reader that observes the orphan claim through the real history path.
func newPGReaderService(db *sql.DB, clock interface{ Now() time.Time }) *service.QueryExecutionService {
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	targetRepo := mysqlrepo.NewQueryTargetRepository(db)
	return service.NewQueryExecutionService(
		targetRepo,
		mysqlrepo.NewQueryExecutionRepository(db),
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		clock,
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(disc, disc, service.NewMySQLSchemaInspector(), targetRepo),
	)
}

// readClaimView reads the history list item for the key through the service
// read path — the same projection the HTTP GET serves.
func readClaimView(t *testing.T, svc *service.QueryExecutionService, actorID, targetID uint64, key string) (kind string, status string) {
	t.Helper()
	page, err := svc.ListHistory(context.Background(),
		model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: actorID}, "admin",
		targetID, model.QueryExecutionListQuery{TargetResourceID: targetID, ClientExecutionID: key})
	if err != nil {
		t.Fatalf("reader ListHistory: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("reader items = %d, want exactly 1", len(page.Items))
	}
	return string(page.Items[0].Kind), string(page.Items[0].Status)
}

func TestPGKeyedHTTP_OrphanClaimProcessExit(t *testing.T) {
	db := setupTestDB(t)
	fix := newPGDiscFixture(t)

	userID := insertAuthzTestUser(t, db, "pg-r1-orphan@example.com", "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })
	targetID := createPGInstance(t, db, "pg-r1-orphan-"+strings.ReplaceAll(t.Name(), "/", "-"), envStaging, fix.host, fix.port)
	ref := "PG_R1_ORPHAN"
	insertPGCredential(t, db, targetID, "discdb", "app", ref, true, string(model.QueryEnvPolicyNonProdOnly))
	dsn := pgKeywordDSN(fix.host, fix.port, fix.db, pgDiscRole, pgStmtPassword, 5)
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+ref, dsn)

	admin := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	for _, stmt := range []string{
		`CREATE TABLE app.orphan_gate (id bigint PRIMARY KEY, note text)`,
		`INSERT INTO app.orphan_gate VALUES (1, 'gate')`,
		`GRANT SELECT ON app.orphan_gate TO ` + pgDiscRole,
	} {
		execPG(t, admin, stmt)
	}
	disc := mysqlrepo.NewQueryDisclosureRepository(db)
	insertPolicy(t, disc, targetID, "discdb", "app", "orphan_gate", "id", model.ResultDisclosureRawCopyAllowed)
	insertPolicy(t, disc, targetID, "discdb", "app", "orphan_gate", "note", model.ResultDisclosureRawCopyAllowed)

	// The parent-held table lock is the reliable barrier: the child's
	// attempt blocks inside the result statement — after its real claim
	// commits, before finalize can ever run.
	lockConn := connectPG(t, fix.host, fix.port, fix.db, pgMetaAdminUser, pgMetaAdminPassword)
	execPG(t, lockConn, `BEGIN`)
	execPG(t, lockConn, `LOCK TABLE app.orphan_gate IN ACCESS EXCLUSIVE MODE`)

	// Spawn the test-owned server process.
	readyPath := filepath.Join(t.TempDir(), "child-ready")
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test binary: %v", err)
	}
	cmd := exec.Command(self, "-test.run=^TestPGKeyedHTTP_OrphanClaimProcessExit$")
	cmd.Env = append(os.Environ(),
		"CH_T12_ORPHAN_CHILD="+readyPath,
		"CH_T12_CHILD_MYSQL_DSN="+globalEnv.dsn,
		"CONTROLHUB_QUERY_CREDENTIAL_"+ref+"="+dsn,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("spawn child: %v", err)
	}
	defer func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
		execPG(t, lockConn, `ROLLBACK`)
		_ = lockConn.Close(context.Background())
		_ = admin.Close(context.Background())
	}()

	// Wait for the child's real listener.
	deadline := time.Now().Add(60 * time.Second)
	var port string
	for {
		if b, err := os.ReadFile(readyPath); err == nil {
			port = string(b)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child never became ready; stderr=%s", stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	base := "http://127.0.0.1:" + port
	token := childHTTPLogin(t, base, "pg-r1-orphan@example.com")

	// Fire the request — it lands the claim, then parks on the locked table.
	reqDone := make(chan error, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost,
			base+fmt.Sprintf("/query-targets/%d/execute", targetID),
			strings.NewReader(`{"statement":"SELECT id, note FROM app.orphan_gate WHERE id = 1","database":"discdb","clientExecutionId":"r1-orphan"}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if resp != nil {
			_ = resp.Body.Close()
		}
		reqDone <- err
	}()

	// The claim row is committed evidence — poll until it exists, then kill.
	deadline = time.Now().Add(30 * time.Second)
	for {
		if claimOccupied(t, db, targetID, "r1-orphan") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child claim never committed; stderr=%s", stderr.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatalf("kill child: %v", err)
	}
	_ = cmd.Wait()
	select {
	case <-reqDone:
	case <-time.After(15 * time.Second):
	}
	execPG(t, lockConn, `ROLLBACK`)

	// Orphan facts: the claim exists, linked to nothing; no terminal row or
	// audit was ever written — the dead process cannot fabricate one.
	var linked sql.NullInt64
	if err := db.QueryRow(
		`SELECT execution_id FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = 'r1-orphan'`,
		targetID).Scan(&linked); err != nil {
		t.Fatalf("read orphan claim: %v", err)
	}
	if linked.Valid {
		t.Fatalf("orphan claim is linked to %d — finalize ran before kill", linked.Int64)
	}
	if execs, audits, _ := claimExecCounts(t, db, targetID, "r1-orphan"); execs != 0 || audits != 0 {
		t.Fatalf("orphan wrote %d executions / %d audits, want 0/0", execs, audits)
	}

	// Wait for the killed attempt's server-side backend to drain so the
	// remote-activity counts around the reads are comparable — a leftover
	// backend would be a stale count, not a reader probe.
	orphanDrainBackends(t, admin)

	// R2-A: the orphan's first read happens through the real HTTP GET path
	// on an independent reader assembly — fresh api.NewRouter + real auth
	// over a live socket. The direct ListHistory call stays only as an
	// auxiliary cross-check, never the acceptance path.
	readerRouter := pgReaderRouter(t, db, newPGReaderService(db, wallClock{}))
	readerSrv := httptest.NewServer(readerRouter)
	defer readerSrv.Close()
	rtok := childHTTPLogin(t, readerSrv.URL, "pg-r1-orphan@example.com")

	// Snapshot the claim row before the reads — the GETs must not mutate it.
	var claimedAt time.Time
	var budget int64
	if err := db.QueryRow(
		`SELECT claimed_at, attempt_timeout_ms FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = 'r1-orphan'`,
		targetID).Scan(&claimedAt, &budget); err != nil {
		t.Fatalf("read claim budget: %v", err)
	}
	if budget <= 0 {
		t.Fatalf("claim recorded no attempt budget: %d", budget)
	}
	execs0, audits0, _ := claimExecCounts(t, db, targetID, "r1-orphan")
	backends0 := countPGBackends(t, admin)

	// Reader #1, wall clock — the first GET after the process exit reports
	// the claim still running (its recorded budget has not yet expired).
	orphanHTTPGet(t, readerSrv, rtok, targetID, "r1-orphan", "discdb", string(model.QueryExecutionViewRunning))

	// Auxiliary service-level cross-check — same projection, not the
	// acceptance path.
	if kind, status := readClaimView(t, newPGReaderService(db, wallClock{}), userID, targetID, "r1-orphan"); kind != string(model.QueryExecutionListItemClaim) || status != string(model.QueryExecutionViewRunning) {
		t.Fatalf("auxiliary service read = %s/%s, want claim/running", kind, status)
	}

	// Reader #2, derived clock advanced past the recorded budget+grace —
	// the same claim reports unknown. This is a clock-controlled
	// derivation, not a remote-query timeout, and unknown does not claim
	// the database stopped.
	reader2Router := pgReaderRouter(t, db, newPGReaderService(db,
		fwdClock{claimedAt.Add(time.Duration(budget)*time.Millisecond + 61*time.Second)}))
	readerSrv2 := httptest.NewServer(reader2Router)
	defer readerSrv2.Close()
	rtok2 := childHTTPLogin(t, readerSrv2.URL, "pg-r1-orphan@example.com")
	orphanHTTPGet(t, readerSrv2, rtok2, targetID, "r1-orphan", "discdb", string(model.QueryExecutionViewUnknown))

	// The reads changed nothing: claim still occupied-but-unlinked, no new
	// terminal/audit rows, and no new remote activity — the GET path never
	// finalizes, releases the key, or probes the backend.
	if !claimOccupied(t, db, targetID, "r1-orphan") {
		t.Fatal("orphan claim released by the GET reads")
	}
	var linked2 sql.NullInt64
	var claimedAt2 time.Time
	if err := db.QueryRow(
		`SELECT execution_id, claimed_at FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = 'r1-orphan'`,
		targetID).Scan(&linked2, &claimedAt2); err != nil {
		t.Fatalf("re-read claim: %v", err)
	}
	if linked2.Valid || !claimedAt2.Equal(claimedAt) {
		t.Fatalf("claim mutated by GETs: linked=%v claimed_at %v->%v", linked2, claimedAt, claimedAt2)
	}
	if execs, audits, _ := claimExecCounts(t, db, targetID, "r1-orphan"); execs != execs0 || audits != audits0 {
		t.Fatalf("GETs wrote evidence: %d/%d -> %d/%d", execs0, audits0, execs, audits)
	}
	if got := countPGBackends(t, admin); got != backends0 {
		t.Fatalf("reader GETs opened remote activity: %d -> %d", backends0, got)
	}

	// Authorization boundary on the real GET: no credential is denied, and
	// a governed-select machine credential does not read the orphan claim —
	// the existing history rule denies machine reads wholesale.
	if resp, err := http.Get(fmt.Sprintf("%s/query-targets/%d/executions?database=discdb&clientExecutionId=r1-orphan", readerSrv.URL, targetID)); err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET = %v/%v, want 401", resp, err)
	} else {
		_ = resp.Body.Close()
	}
	machToken := mkReaderMachine(t, db, userID)
	req, _ := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/query-targets/%d/executions?database=discdb&clientExecutionId=r1-orphan", readerSrv.URL, targetID), nil)
	req.Header.Set("Authorization", "Bearer "+machToken)
	mresp, err := readerSrv.Client().Do(req)
	if err != nil {
		t.Fatalf("machine GET: %v", err)
	}
	_ = mresp.Body.Close()
	if mresp.StatusCode == http.StatusOK {
		t.Fatal("machine credential read the orphan claim — machine reads must stay denied")
	}

	// Load-bearing reverse controls: test-side servers that drop the claim
	// item or corrupt its status serialization must trip the same
	// assertions — proving the read-path checks can catch a regression, not
	// that production emitted such a response.
	tampers := map[string]func(it map[string]any) (keep bool){
		"drop_claim":   func(it map[string]any) bool { return it["kind"] != "claim" },
		"wrong_status": func(it map[string]any) bool { it["status"] = "succeeded"; return true },
		"wrong_id": func(it map[string]any) bool {
			it["id"] = float64(424242) // fabricate a terminal id on the claim
			return true
		},
	}
	for name, tamper := range tampers {
		tamper := tamper
		tampered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := httptest.NewRecorder()
			readerRouter.ServeHTTP(rec, r)
			var page struct {
				Items []map[string]any `json:"items"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &page); err == nil {
				kept := make([]map[string]any, 0, len(page.Items))
				for _, it := range page.Items {
					if tamper(it) {
						kept = append(kept, it)
					}
				}
				page.Items = kept
				body, _ := json.Marshal(page)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(rec.Code)
				_, _ = w.Write(body)
				return
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
		}))
		_, err := orphanHTTPGetErr(t, tampered, rtok, targetID, "r1-orphan", "discdb", string(model.QueryExecutionViewRunning))
		tampered.Close()
		if err == nil {
			t.Fatalf("tampered response %q passed the orphan assertions — the read-path checks are not load-bearing", name)
		}
	}

	// A same-key retry on a fully-wired enabled assembly is refused — the
	// orphan claim never grants a second execution.
	retry := newPGReaderService(db, wallClock{}).
		WithPGClaimOrchestration(disc).
		WithTemplateExecution(mysqlrepo.NewQuerySavedStatementRepository(db), service.NewTemplateStatementCompiler()).
		WithPGHTTPEntries()
	retryRouter := api.NewRouter(api.Dependencies{
		AuthService:                service.NewAuthService(mysqlrepo.NewUserRepository(db), authzIntegrationSecret),
		AuditService:               service.NewAuditService(mysqlrepo.NewAuditRepository(db)),
		QueryExecutionService:      retry,
		QuerySavedStatementService: service.NewQuerySavedStatementService(mysqlrepo.NewQuerySavedStatementRepository(db), mysqlrepo.NewQuerySavedStatementRepository(db), mysqlrepo.NewQueryTargetRepository(db), service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})),
		MachineCredentialService:   service.NewMachinePrincipalService(mysqlrepo.NewMachinePrincipalRepository(db)),
		QueryExecutionAuth:         api.QueryExecutionAuthConfig{Clock: time.Now},
	})
	rtok3 := mustLogin(t, retryRouter, "pg-r1-orphan@example.com", "secret123")
	rr := doBearerWithBody(t, retryRouter, http.MethodPost,
		fmt.Sprintf("/query-targets/%d/execute", targetID), rtok3,
		`{"statement":"SELECT id, note FROM app.orphan_gate WHERE id = 1","database":"discdb","clientExecutionId":"r1-orphan"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-key retry on orphan claim: %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if execs, audits, _ := claimExecCounts(t, db, targetID, "r1-orphan"); execs != 0 || audits != 0 {
		t.Fatalf("retry fabricated terminal evidence: %d/%d", execs, audits)
	}
}

// ---------------------------------------------------------------------------
// R2-A helpers — real-HTTP orphan reads.
// ---------------------------------------------------------------------------

// pgReaderRouter wires an execution-read service into the production router —
// the independent reader for the orphan claim, served over a real socket.
func pgReaderRouter(t *testing.T, db *sql.DB, svc *service.QueryExecutionService) http.Handler {
	t.Helper()
	savedRepo := mysqlrepo.NewQuerySavedStatementRepository(db)
	targetRepo := mysqlrepo.NewQueryTargetRepository(db)
	return api.NewRouter(api.Dependencies{
		AuthService:           service.NewAuthService(mysqlrepo.NewUserRepository(db), authzIntegrationSecret),
		AuditService:          service.NewAuditService(mysqlrepo.NewAuditRepository(db)),
		QueryExecutionService: svc,
		QuerySavedStatementService: service.NewQuerySavedStatementService(
			savedRepo, savedRepo, targetRepo,
			service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})),
		MachineCredentialService: service.NewMachinePrincipalService(mysqlrepo.NewMachinePrincipalRepository(db)),
		QueryExecutionAuth:       api.QueryExecutionAuthConfig{Clock: time.Now},
	})
}

// orphanHTTPGetErr issues the real point-read GET over a live socket and
// returns the single claim item after running the full orphan contract
// assertions — used directly on tampered responders, so every check is
// load-bearing.
func orphanHTTPGetErr(t *testing.T, srv *httptest.Server, token string, targetID uint64, key, database, wantStatus string) (map[string]any, error) {
	t.Helper()
	q := url.Values{}
	q.Set("database", database)
	q.Set("clientExecutionId", key)
	req, err := http.NewRequest(http.MethodGet,
		fmt.Sprintf("%s/query-targets/%d/executions?%s", srv.URL, targetID, q.Encode()), nil)
	if err != nil {
		t.Fatalf("build orphan GET: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("orphan GET transport: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("orphan GET read: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("orphan GET status = %d; body=%s", resp.StatusCode, body)
	}
	var page struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, fmt.Errorf("orphan GET decode: %w", err)
	}
	if len(page.Items) != 1 {
		return nil, fmt.Errorf("items = %d, want exactly 1", len(page.Items))
	}
	item := page.Items[0]
	if item["kind"] != "claim" {
		return nil, fmt.Errorf("kind = %v, want claim", item["kind"])
	}
	if item["status"] != wantStatus {
		return nil, fmt.Errorf("status = %v, want %s", item["status"], wantStatus)
	}
	if v, ok := item["id"]; ok && v != nil {
		return nil, fmt.Errorf("claim item id = %v — must stay null (unassigned)", v)
	}
	if item["clientExecutionId"] != key {
		return nil, fmt.Errorf("clientExecutionId = %v, want %s", item["clientExecutionId"], key)
	}
	if item["database"] != database || item["schema"] != "app" {
		return nil, fmt.Errorf("connection scope = %v/%v, want %s/app", item["database"], item["schema"], database)
	}
	return item, nil
}

func orphanHTTPGet(t *testing.T, srv *httptest.Server, token string, targetID uint64, key, database, wantStatus string) map[string]any {
	t.Helper()
	item, err := orphanHTTPGetErr(t, srv, token, targetID, key, database, wantStatus)
	if err != nil {
		t.Fatalf("orphan HTTP read: %v", err)
	}
	return item
}

// orphanDrainBackends waits until the killed attempt leaves no ACTIVE ch-t6
// backend — the parent's fixture pools stay alive-but-idle until test
// cleanup, so only state='active' marks the child's result query. Bounded,
// so a stuck backend fails the test.
func orphanDrainBackends(t *testing.T, admin *pgx.Conn) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for {
		var n int64
		if err := admin.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_catalog.pg_stat_activity
			  WHERE application_name = 'ch-t6' AND state = 'active'
			    AND pid <> pg_backend_pid()`).Scan(&n); err == nil && n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("orphaned backend never drained")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// countPGBackends returns the number of ch-t6 backends — the reader GET path
// must not open remote activity.
func countPGBackends(t *testing.T, admin *pgx.Conn) int64 {
	t.Helper()
	var n int64
	if err := admin.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_catalog.pg_stat_activity
		  WHERE application_name = 'ch-t6' AND pid <> pg_backend_pid()`).Scan(&n); err != nil {
		t.Fatalf("count backends: %v", err)
	}
	return n
}

// mkReaderMachine mints a governed-select machine credential on the shared
// fixture — history reads must still deny it.
func mkReaderMachine(t *testing.T, db *sql.DB, userID uint64) string {
	t.Helper()
	issued, err := service.NewMachinePrincipalService(mysqlrepo.NewMachinePrincipalRepository(db)).Create(
		context.Background(),
		service.AuthenticatedUser{ID: userID, Role: "admin"},
		model.MachinePrincipalCreateRequest{Name: "pg-r1-reader-mach", Scopes: []model.MachineScope{model.MachineScopeGovernedSelect}})
	if err != nil {
		t.Fatalf("create reader machine: %v", err)
	}
	return issued.Secret
}
