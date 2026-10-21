//go:build integration

// Package integration — PG-HTTP-T12-R1 real-transport evidence: client disconnects, lock-wait timeouts, and a killed subprocess orphan claim.
// input: shared MySQL fixture, disposable PG16 with a slow seqscan table and a lockable gate table, the enabled assembly served over a real socket
// output: TestPGKeyedHTTP_ClientAbort*, TestPGKeyedHTTP_LockWaitTimeout, TestPGKeyedHTTP_OrphanClaimProcessExit; the child entry point pgOrphanChildMain is dispatched from TestMain under env guard
// pos: group A/B of the R1 acceptance — HTTP disconnect and process death are proven as distinct facts; no admin terminate substitutes for the native cancel, and the claim exists because a real request created it
// note: if this file changes, update this header and module README.md.

package integration

import (
	"bytes"
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

	// Independent reader #1, real clock: the claim reports running — the
	// recorded budget has not yet expired.
	reader := newPGReaderService(db, wallClock{})
	kind, status := readClaimView(t, reader, userID, targetID, "r1-orphan")
	if kind != string(model.QueryExecutionListItemClaim) || status != string(model.QueryExecutionViewRunning) {
		t.Fatalf("reader1 view = %s/%s, want claim/running", kind, status)
	}

	// Independent reader #2, derived clock beyond budget+grace: the same
	// claim reports unknown — never a fabricated terminal state.
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
	reader2 := newPGReaderService(db, fwdClock{claimedAt.Add(time.Duration(budget)*time.Millisecond + 61*time.Second)})
	kind, status = readClaimView(t, reader2, userID, targetID, "r1-orphan")
	if kind != string(model.QueryExecutionListItemClaim) || status != string(model.QueryExecutionViewUnknown) {
		t.Fatalf("reader2 view = %s/%s, want claim/unknown", kind, status)
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
	rtok := mustLogin(t, retryRouter, "pg-r1-orphan@example.com", "secret123")
	rr := doBearerWithBody(t, retryRouter, http.MethodPost,
		fmt.Sprintf("/query-targets/%d/execute", targetID), rtok,
		`{"statement":"SELECT id, note FROM app.orphan_gate WHERE id = 1","database":"discdb","clientExecutionId":"r1-orphan"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("same-key retry on orphan claim: %d, want 409; body=%s", rr.Code, rr.Body.String())
	}
	if execs, audits, _ := claimExecCounts(t, db, targetID, "r1-orphan"); execs != 0 || audits != 0 {
		t.Fatalf("retry fabricated terminal evidence: %d/%d", execs, audits)
	}
}
