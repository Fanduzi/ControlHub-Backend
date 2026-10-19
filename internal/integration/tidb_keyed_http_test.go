//go:build integration

// Package integration — real-TiDB (standalone UniStore) HTTP proof for the keyed MySQL/TiDB three-entry claim protocol.
// input: shared MySQL metadata fixture (unchanged), an owned pingcap/tidb:v8.5.0 UniStore container via the standard testcontainers lifecycle (pinned tag pulled when the cache is cold, random name, per-run label; scope cleanup is registered the moment a non-nil container returns — including the create/start/readiness error path v0.42 permits — and terminates by owned object/ID only), the real router/auth/service/repository/disclosure/executor/schema-inspector chain, a SELECT-only governed TiDB account
// output: TestTiDBKeyedHTTP/{Facts,Execute,Saved,Navigation,FKNameDedup,CapabilityGate,ClientCancel,MachineActor} — keyed golden paths, claim readbacks, 409 replay/conflict, keyless compatibility, duplicate-FK-name regression, truncation gate, client-cancel taxonomy, typed machine-actor evidence; plus fixture self-tests: StartCleanupInjection (control-flow injection over the factory seam covering container/nil+err/container+err), StartFailureCleansOwnedContainer (real doomed-readiness container provably released when its scope ends), ContainerOwnership (foreign + fixture A + fixture B alive simultaneously, each destroyed by its own ID one at a time; retired fixed name never a destroy handle)
// pos: Real-TiDB HTTP acceptance boundary — ControlHub metadata stays on the shared MySQL TestMain fixture; only the governed TARGET database is real TiDB labeled engine "tidb". The container runs standalone UniStore: this proves the wire/identity/evidence chain against real TiDB dialect, FK metadata, and cancellation; it is NOT a TiKV/PD/distributed-cluster proof. Engine fact recorded by Facts_StandaloneUniStore: TiDB v8.5.0 rejects START TRANSACTION READ ONLY with error 1235 unless the session enables tidb_enable_noop_functions — the governed DSN enables it in-session (a deployment could equally set the instance default — the requirement is the effective session value); the flag is a no-op on TiDB, so the SELECT-only account grant is the actual write denial, proven by the 1142 assertion.
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	mysqldriver "github.com/go-sql-driver/mysql"
	containertypes "github.com/moby/moby/api/types/container"
	mobynet "github.com/moby/moby/api/types/network"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// The matrix pins the exact image verified by the TiDB probe. The tag is
// fixed — never latest — and testcontainers' standard lifecycle pulls this
// same pinned tag when the local cache is cold, so a clean CI host needs no
// manual image preparation; substituting another tag is not permitted.
const (
	tidbKeyedImage   = "pingcap/tidb:v8.5.0"
	tidbKeyedROUser  = "chub_ro"
	tidbKeyedROPass  = "tidb-matrix-fixture-only"
	tidbKeyedCredRef = "TIDB_MATRIX_RO"
)

// tidbKeyedRuntime owns the TiDB UniStore container for the matrix: its
// localhost endpoint, the admin (root) connection used for fixture DDL and
// processlist observation, and the recorded engine facts. containerID plus
// the retained container object are the only handles cleanup uses — teardown
// terminates exactly this run's container, never a name another run or a
// foreign fixture might hold.
type tidbKeyedRuntime struct {
	host        string
	port        int
	version     string
	tidbVer     string
	imageID     string
	containerID string
	container   testcontainers.Container
	admin       *sql.DB
	roUserOpen  func(schema string) (*sql.DB, error)
}

// terminate tears down exactly this runtime's owned container mid-test — used
// by ownership checks that must destroy one fixture while a sibling stays
// alive. The registered scope cleanup still runs at test end; its second
// Terminate on the same owned ID is an expected, only-logged error.
func (rt *tidbKeyedRuntime) terminate(t *testing.T, why string) {
	t.Helper()
	terminateOwnedTiDBContainer(t, rt.container, rt.containerID, why)
}

// dockerTiDB runs one docker CLI call and fails the test on error.
func dockerTiDB(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v — %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out))
}

// dockerTiDBContainerState reports whether nameOrID exists and is running.
// Only the daemon's explicit "No such object" answer counts as absent — a
// nonzero inspect exit for any other reason (daemon down, permissions) is a
// test failure, never proof the object is gone.
func dockerTiDBContainerState(t *testing.T, nameOrID string) (exists, running bool) {
	t.Helper()
	out, err := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", nameOrID).CombinedOutput()
	if err != nil {
		if low := strings.ToLower(string(out)); strings.Contains(low, "no such object") || strings.Contains(low, "no such container") {
			return false, false
		}
		t.Fatalf("docker inspect %s: %v — %s", nameOrID, err, strings.TrimSpace(string(out)))
	}
	return true, strings.TrimSpace(string(out)) == "true"
}

// tidbGenericContainer is the fixture's container factory seam so
// control-flow injection tests can exercise the cleanup ordering on every
// return shape testcontainers-go v0.42 permits — including (non-nil
// container, error) from create/start/readiness failures.
var tidbGenericContainer = testcontainers.GenericContainer

// terminateOwnedTiDBContainer stops exactly the container object this run
// holds — bounded, never a name lookup. Terminate errors are logged, never
// fatal, and never replace the original start error.
func terminateOwnedTiDBContainer(t *testing.T, container testcontainers.Container, id, why string) {
	t.Helper()
	termCtx, termCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer termCancel()
	if err := container.Terminate(termCtx); err != nil {
		t.Logf("terminate owned TiDB container id %s (%s) -> %v", id, why, err)
	}
}

// startOwnedTiDBContainer creates and starts the fixture container and — the
// moment a non-nil container comes back — registers this scope's bounded
// cleanup BEFORE the error is examined. v0.42 explicitly permits returning a
// created container alongside a create/start/readiness error; without this
// ordering that partially-created container would leak until Ryuk. The
// original error is returned unmasked. nil container + error simply returns
// (nil, err) with nothing registered.
func startOwnedTiDBContainer(t *testing.T, ctx context.Context, req testcontainers.ContainerRequest) (testcontainers.Container, error) {
	t.Helper()
	container, err := tidbGenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if container == nil {
		return nil, err
	}
	cid := container.GetContainerID()
	t.Cleanup(func() { terminateOwnedTiDBContainer(t, container, cid, "scope cleanup") })
	return container, err
}

// startTiDBKeyedRuntime starts a fresh owned UniStore TiDB through the
// standard testcontainers lifecycle: the pinned image is pulled when absent
// (cold-environment path, same tag — never substituted), the container gets
// a random name and a per-run ownership label, binds a dynamic 127.0.0.1
// port, and is terminated by container ID on cleanup (Ryuk reaps it only as
// a backstop). Cleanup is registered by startOwnedTiDBContainer the moment a
// non-nil container returns — even when it returns alongside a start or
// readiness error — so MappedPort, version-read, SQL-prep, or assertion
// failures later in this function are all covered by the same owned-ID
// teardown. Nothing touches pre-existing containers or images: no
// fixed-name removal and no cache wiping. A missing or unpullable image is
// a real test failure, never a silent skip or a MySQL stand-in.
func startTiDBKeyedRuntime(t *testing.T) *tidbKeyedRuntime {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	req := testcontainers.ContainerRequest{
		Image:        tidbKeyedImage,
		Cmd:          []string{"--store=unistore", "--path=/tmp/controlhub-mysql-keyed", "--host=0.0.0.0", "-P=4000"},
		ExposedPorts: []string{"4000/tcp"},
		Labels:       map[string]string{"controlhub.test-scope": "tidb-keyed-matrix"},
		HostConfigModifier: func(hc *containertypes.HostConfig) {
			hc.PortBindings = mobynet.PortMap{
				mobynet.MustParsePort("4000/tcp"): {{HostIP: netip.MustParseAddr("127.0.0.1")}},
			}
		},
		WaitingFor: wait.ForListeningPort("4000/tcp").WithStartupTimeout(3 * time.Minute),
	}
	container, err := startOwnedTiDBContainer(t, ctx, req)
	if err != nil {
		t.Fatalf("start TiDB %s via testcontainers (pulls the pinned tag when the cache is cold): %v", tidbKeyedImage, err)
	}
	cid := container.GetContainerID()

	port, err := container.MappedPort(ctx, "4000/tcp")
	if err != nil {
		t.Fatalf("map TiDB 4000/tcp: %v", err)
	}
	portNum := int(port.Num())
	if portNum <= 0 {
		t.Fatalf("unexpected TiDB host port %q", port.String())
	}
	host := "127.0.0.1"
	portStr := strconv.Itoa(portNum)
	t.Logf("tidb matrix container id=%s started, endpoint 127.0.0.1:%d", cid, portNum)

	// Record the image facts actually running — tag, resolved image id and
	// architecture are logged separately from VERSION()/TIDB_VERSION() so the
	// engine under test stays auditable on both ARM64 and CI amd64 hosts.
	imageID := dockerTiDB(t, "image", "inspect", tidbKeyedImage, "--format", "{{.Id}}")
	arch := dockerTiDB(t, "image", "inspect", tidbKeyedImage, "--format", "{{.Architecture}}")
	t.Logf("tidb image id=%s arch=%s tag=%s", imageID, arch, tidbKeyedImage)

	adminCfg := mysqldriver.NewConfig()
	adminCfg.User = "root"
	adminCfg.Net = "tcp"
	adminCfg.Addr = net.JoinHostPort(host, portStr)
	adminCfg.ParseTime = true
	admin, err := sql.Open("mysql", adminCfg.FormatDSN())
	if err != nil {
		t.Fatalf("open TiDB admin connection: %v", err)
	}
	admin.SetMaxOpenConns(4)
	t.Cleanup(func() { admin.Close() })

	deadline := time.Now().Add(180 * time.Second)
	var lastErr error
	for {
		pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
		lastErr = admin.PingContext(pingCtx)
		cancelPing()
		if lastErr == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("TiDB container never became ready: %v", lastErr)
		}
		time.Sleep(500 * time.Millisecond)
	}

	rt := &tidbKeyedRuntime{host: host, port: portNum, imageID: imageID, containerID: cid, container: container, admin: admin}
	if err := admin.QueryRow("SELECT VERSION(), TIDB_VERSION()").Scan(&rt.version, &rt.tidbVer); err != nil {
		t.Fatalf("read TiDB versions: %v", err)
	}
	t.Logf("tidb VERSION()=%s", rt.version)
	t.Logf("tidb TIDB_VERSION()=%s", rt.tidbVer)

	mustTiDBExec(t, rt.admin, fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", tidbKeyedROUser, tidbKeyedROPass))
	rt.roUserOpen = func(schema string) (*sql.DB, error) {
		cfg := mysqldriver.NewConfig()
		cfg.User = tidbKeyedROUser
		cfg.Passwd = tidbKeyedROPass
		cfg.Net = "tcp"
		cfg.Addr = fmt.Sprintf("%s:%d", rt.host, rt.port)
		cfg.DBName = schema
		cfg.ParseTime = true
		cfg.Collation = "utf8mb4_bin"
		cfg.Params = map[string]string{"tidb_enable_noop_functions": "ON"}
		return sql.Open("mysql", cfg.FormatDSN())
	}
	return rt
}

// roDSN is the governed credential string stored under the credential ref:
// a genuinely SELECT-only account, not the admin root and not a relabeled
// MySQL fixture. What real TiDB requires is a SESSION in which
// tidb_enable_noop_functions is enabled: without it the driver's read-only
// BeginTx ("START TRANSACTION READ ONLY") is rejected with error 1235. This
// fixture satisfies that requirement through the governed DSN — the verified
// configuration under test — while a deployment could equally satisfy it via
// the instance/global default; the requirement is the effective session
// value, not the DSN string itself. On TiDB the flag is a documented no-op —
// READ ONLY being *accepted* does not itself block writes — so the
// SELECT-only grant is the actual write denial (proven by the Facts
// subtest's 1142 assertion), matching the design note that the read-only
// transaction is only defense in depth.
func (rt *tidbKeyedRuntime) roDSN(schema string) string {
	cfg := mysqldriver.NewConfig()
	cfg.User = tidbKeyedROUser
	cfg.Passwd = tidbKeyedROPass
	cfg.Net = "tcp"
	cfg.Addr = fmt.Sprintf("%s:%d", rt.host, rt.port)
	cfg.DBName = schema
	cfg.ParseTime = true
	cfg.Collation = "utf8mb4_bin"
	cfg.Params = map[string]string{"tidb_enable_noop_functions": "ON"}
	return cfg.FormatDSN()
}

func mustTiDBExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("tidb exec %q: %v", query, err)
	}
}

// tidbKeyedFixture is the fully wired HTTP boundary for the TiDB matrix: the
// shared MySQL metadata DB, a TiDB-labeled target resource whose profile and
// credential resolve to the SELECT-only TiDB account, and the real router.
type tidbKeyedFixture struct {
	router   http.Handler
	token    string
	userID   uint64
	targetID uint64
	db       *sql.DB
	schema   string
	rt       *tidbKeyedRuntime
}

// setupTiDBKeyedFixture provisions one TiDB schema (CREATE DATABASE +
// SELECT-only GRANT), the MySQL-side metadata (user, tidb-labeled resource,
// tidb-engine profile and credential row), and the real router — mirroring
// setupResultContractFixture but with the governed target pointing at TiDB.
func setupTiDBKeyedFixture(t *testing.T, rt *tidbKeyedRuntime, schema string) tidbKeyedFixture {
	t.Helper()
	db := setupTestDB(t)

	email := fmt.Sprintf("tidb-mx-%s@example.com", schema)
	userID := insertAuthzTestUser(t, db, email, "admin")
	t.Cleanup(func() { deleteAuthzTestUser(t, db, userID) })

	mustTiDBExec(t, rt.admin, fmt.Sprintf("CREATE DATABASE %s", schema))
	mustTiDBExec(t, rt.admin, fmt.Sprintf("GRANT SELECT ON %s.* TO '%s'@'%%'", schema, tidbKeyedROUser))

	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(context.Background(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "tidb",
		Name:            "tidb-mx-" + schema,
		DisplayName:     "TiDB Matrix Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create tidb target resource: %v", err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'tidb', ?, ?, ?, 'primary', '{}')`,
		res.ID, rt.version, rt.host, rt.port)
	seedCredentialRow(t, db, res.ID, "tidb", tidbKeyedCredRef, true, string(model.QueryEnvPolicyNonProdOnly))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+tidbKeyedCredRef, rt.roDSN(schema))

	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	savedStmtRepo := mysql.NewQuerySavedStatementRepository(db)
	credentialResolver := service.NewEnvCredentialResolver()
	guard := service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})
	executionSvc := service.NewQueryExecutionService(
		queryTargetRepo,
		queryExecutionRepo,
		credentialResolver,
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		guard,
		wallClock{},
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(
			mysql.NewQueryDisclosureRepository(db),
			mysql.NewQueryDisclosureRepository(db),
			service.NewMySQLSchemaInspector(),
			queryTargetRepo,
		),
	).WithTemplateExecution(savedStmtRepo, service.NewTemplateStatementCompiler())

	router := api.NewRouter(api.Dependencies{
		AuthService:                service.NewAuthService(mysql.NewUserRepository(db), authzIntegrationSecret),
		AuditService:               service.NewAuditService(mysql.NewAuditRepository(db)),
		QueryExecutionService:      executionSvc,
		QuerySavedStatementService: service.NewQuerySavedStatementService(savedStmtRepo, savedStmtRepo, queryTargetRepo, guard),
		MachineCredentialService:   service.NewMachinePrincipalService(mysql.NewMachinePrincipalRepository(db)),
		QueryExecutionAuth:         api.QueryExecutionAuthConfig{Clock: time.Now},
	})

	token := mustLogin(t, router, email, "secret123")
	return tidbKeyedFixture{router: router, token: token, userID: userID, targetID: res.ID, db: db, schema: schema, rt: rt}
}

// executeResp is the decoded governed-execute envelope plus the engine label
// — asserting engine=="tidb" proves the target chain really resolved TiDB.
type tidbExecuteResp struct {
	ExecutionID   uint64   `json:"executionId"`
	Status        string   `json:"status"`
	Engine        string   `json:"engine"`
	Rows          [][]any  `json:"rows"`
	CellTruncated [][]bool `json:"cellTruncated"`
}

func decodeTiDBExecute(t *testing.T, rec *httptest.ResponseRecorder) tidbExecuteResp {
	t.Helper()
	var resp tidbExecuteResp
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode execute response: %v — %s", err, rec.Body.String())
	}
	return resp
}

// tidbNavResp adds the relation metadata the navigation route returns.
type tidbNavResp struct {
	tidbExecuteResp
	ForeignKey         string   `json:"foreignKey"`
	ReferencedDatabase string   `json:"referencedDatabase"`
	ReferencedObject   string   `json:"referencedObject"`
	ReferencedColumns  []string `json:"referencedColumns"`
}

// tidbAssertPointRead performs the claim-aware GET readback: exactly one
// item, kind=execution, carrying the keyed terminal id and expected status.
func tidbAssertPointRead(t *testing.T, fix tidbKeyedFixture, key string, execID uint64, wantStatus string) {
	t.Helper()
	listURL := fmt.Sprintf("/query-targets/%d/executions?clientExecutionId=%s", fix.targetID, key)
	rec := doBearer(t, fix.router, http.MethodGet, listURL, fix.token)
	if rec.Code != http.StatusOK {
		t.Fatalf("executions list: got %d want 200: %s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listed.Items) != 1 {
		t.Fatalf("claim point read returned %d items, want 1: %v", len(listed.Items), listed.Items)
	}
	item := listed.Items[0]
	if item["kind"] != "execution" || item["id"] != float64(execID) || item["status"] != wantStatus {
		t.Fatalf("claim point read item: %v (want execution id=%d status=%q)", item, execID, wantStatus)
	}
}

// tidbAssertErrorCode asserts a controlled error envelope code.
func tidbAssertErrorCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, wantStatus, rec.Body.String())
	}
	code, _ := decodeError(t, rec)
	if code != wantCode {
		t.Fatalf("error code = %q, want %q (body=%s)", code, wantCode, rec.Body.String())
	}
}

// tidbAssertLinkedRow reads the canonical claim+execution join for one key and
// asserts the full MySQL/TiDB evidence contract: linked, empty connection
// scope, NULL backend PID, empty remote state, recorded attempt budget,
// engine "tidb", the expected terminal status/error code, and the typed actor
// (exactly one of user/machine).
func tidbAssertLinkedRow(t *testing.T, db *sql.DB, targetID uint64, key string,
	wantStatus, wantError string, wantUser, wantMachine int64) {
	t.Helper()
	var (
		execID           int64
		execActorUser    sql.NullInt64
		execActorMachine sql.NullInt64
		execEngine       string
		execDB           string
		execSchema       string
		execStatus       string
		execErrorCode    sql.NullString
		execBackendPID   sql.NullInt64
		execRemoteState  sql.NullString
		execClientKey    sql.NullString
		claimExecID      sql.NullInt64
		claimDB          string
		claimSchema      string
		claimTimeoutMs   sql.NullInt64
		claimActorUser   sql.NullInt64
		claimActorMach   sql.NullInt64
	)
	if err := db.QueryRow(`SELECT e.id, e.actor_user_id, e.actor_machine_principal_id, e.engine,
       e.database_name, e.schema_name, e.status, e.error_code,
       e.backend_pid, e.remote_state, e.client_execution_id,
       c.execution_id, c.database_name, c.schema_name, c.attempt_timeout_ms,
       c.actor_user_id, c.actor_machine_principal_id
FROM query_execution_claims c
LEFT JOIN query_executions e ON e.id = c.execution_id
WHERE c.target_resource_id = ? AND c.client_execution_id = ?`,
		targetID, key).
		Scan(&execID, &execActorUser, &execActorMachine, &execEngine,
			&execDB, &execSchema, &execStatus, &execErrorCode,
			&execBackendPID, &execRemoteState, &execClientKey,
			&claimExecID, &claimDB, &claimSchema, &claimTimeoutMs,
			&claimActorUser, &claimActorMach); err != nil {
		t.Fatalf("claim+execution join read: %v", err)
	}
	if !claimExecID.Valid || claimExecID.Int64 != execID || execID <= 0 {
		t.Fatalf("claim not linked to its terminal execution: claim.execution_id=%v e.id=%d", claimExecID, execID)
	}
	if execEngine != "tidb" {
		t.Fatalf("linked execution engine = %q, want tidb", execEngine)
	}
	if claimDB != "" || claimSchema != "" || execDB != "" || execSchema != "" {
		t.Fatalf("MySQL/TiDB connection scope must stay empty, got claim(%q,%q) exec(%q,%q)",
			claimDB, claimSchema, execDB, execSchema)
	}
	if !claimTimeoutMs.Valid || claimTimeoutMs.Int64 != 5000 {
		t.Fatalf("claim attempt_timeout_ms = %v, want 5000 (staging budget)", claimTimeoutMs)
	}
	if execBackendPID.Valid {
		t.Fatalf("backend_pid must be NULL for MySQL/TiDB, got %d", execBackendPID.Int64)
	}
	if execRemoteState.Valid && execRemoteState.String != "" {
		t.Fatalf("remote_state must stay empty for MySQL/TiDB, got %q", execRemoteState.String)
	}
	if execStatus != wantStatus {
		t.Fatalf("linked execution status = %q, want %q", execStatus, wantStatus)
	}
	// error_code is NOT NULL DEFAULT '' — success carries the empty string.
	if wantError == "" {
		if !execErrorCode.Valid || execErrorCode.String != "" {
			t.Fatalf("success execution error_code must be '', got %v", execErrorCode)
		}
	} else if !execErrorCode.Valid || execErrorCode.String != wantError {
		t.Fatalf("linked execution error_code = %v, want %q", execErrorCode, wantError)
	}
	if !execClientKey.Valid || execClientKey.String != key {
		t.Fatalf("linked execution must carry the occupied key %q, got %v", key, execClientKey)
	}
	switch {
	case wantUser != 0:
		if !execActorUser.Valid || execActorUser.Int64 != wantUser || execActorMachine.Valid {
			t.Fatalf("execution actor must be user %d (machine NULL): user=%v machine=%v",
				wantUser, execActorUser, execActorMachine)
		}
		if !claimActorUser.Valid || claimActorUser.Int64 != wantUser || claimActorMach.Valid {
			t.Fatalf("claim actor must be user %d (machine NULL): user=%v machine=%v",
				wantUser, claimActorUser, claimActorMach)
		}
	case wantMachine != 0:
		if execActorUser.Valid || !execActorMachine.Valid || execActorMachine.Int64 != wantMachine {
			t.Fatalf("execution actor must be machine %d (user NULL): user=%v machine=%v",
				wantMachine, execActorUser, execActorMachine)
		}
		if claimActorUser.Valid || !claimActorMach.Valid || claimActorMach.Int64 != wantMachine {
			t.Fatalf("claim actor must be machine %d (user NULL): user=%v machine=%v",
				wantMachine, claimActorUser, claimActorMach)
		}
	default:
		t.Fatal("tidbAssertLinkedRow requires an expected actor")
	}
}

// doBearerCancelable issues one authenticated request on a caller-owned
// context; the response arrives on the returned channel so the test can
// cancel mid-flight.
func doBearerCancelable(t *testing.T, h http.Handler, ctx context.Context, method, path, token, body string) <-chan *httptest.ResponseRecorder {
	t.Helper()
	ch := make(chan *httptest.ResponseRecorder, 1)
	req := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		ch <- rec
	}()
	return ch
}

func TestTiDBKeyedHTTP(t *testing.T) {
	rt := startTiDBKeyedRuntime(t)

	// Facts pins the engine identity and the two engine-level behaviors the
	// matrix relies on: a real TiDB v8.5.0 STANDALONE UNISTORE runtime (this
	// is not a TiKV/PD/distributed-cluster proof), enforced FK constraints,
	// and a genuinely SELECT-only governed account.
	t.Run("Facts_StandaloneUniStore", func(t *testing.T) {
		if !strings.Contains(rt.version, "TiDB-v8.5.0") {
			t.Fatalf("VERSION()=%q — the matrix requires actual TiDB v8.5.0 (no other tag, no relabel)", rt.version)
		}
		if !strings.Contains(rt.tidbVer, "v8.5.0") || !strings.Contains(strings.ToLower(rt.tidbVer), "unistore") {
			t.Fatalf("TIDB_VERSION()=%q — require v8.5.0 standalone UniStore", rt.tidbVer)
		}
		t.Logf("recorded: image=%s endpoint=127.0.0.1:%d store=unistore (NOT TiKV/PD/distributed proof)", rt.imageID, rt.port)

		var fkChecks int
		if err := rt.admin.QueryRow("SELECT @@foreign_key_checks").Scan(&fkChecks); err != nil {
			t.Fatalf("read @@foreign_key_checks: %v", err)
		}
		if fkChecks != 1 {
			t.Fatalf("@@foreign_key_checks=%d, want 1 (FK enforcement on)", fkChecks)
		}

		mustTiDBExec(t, rt.admin, "CREATE DATABASE IF NOT EXISTS tidb_mx_probe")
		mustTiDBExec(t, rt.admin, "CREATE TABLE IF NOT EXISTS tidb_mx_probe.probe_cells (id BIGINT PRIMARY KEY)")
		mustTiDBExec(t, rt.admin, "CREATE TABLE IF NOT EXISTS tidb_mx_probe.probe_parent (id BIGINT PRIMARY KEY)")
		mustTiDBExec(t, rt.admin, `CREATE TABLE IF NOT EXISTS tidb_mx_probe.probe_child (
			id BIGINT PRIMARY KEY, parent_id BIGINT NOT NULL,
			CONSTRAINT fk_probe_parent FOREIGN KEY (parent_id) REFERENCES tidb_mx_probe.probe_parent(id))`)
		mustTiDBExec(t, rt.admin, fmt.Sprintf("GRANT SELECT ON tidb_mx_probe.* TO '%s'@'%%'", tidbKeyedROUser))

		// Record the noop-function gate: a session WITHOUT the DSN flag gets
		// MySQL error 1235 on the driver's read-only BeginTx. The governed DSN
		// carries tidb_enable_noop_functions=ON, so the real chain accepts the
		// (documented no-op) READ ONLY flag.
		rawCfg, err := mysqldriver.ParseDSN(fmt.Sprintf("%s:%s@tcp(%s:%d)/tidb_mx_probe?parseTime=true", tidbKeyedROUser, tidbKeyedROPass, rt.host, rt.port))
		if err != nil {
			t.Fatalf("build raw DSN: %v", err)
		}
		raw, err := sql.Open("mysql", rawCfg.FormatDSN())
		if err != nil {
			t.Fatalf("open raw SELECT-only connection: %v", err)
		}
		defer raw.Close()
		if _, err := raw.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true}); err == nil {
			t.Fatal("expected TiDB to reject READ ONLY tx without tidb_enable_noop_functions")
		} else {
			var merr *mysqldriver.MySQLError
			if !errors.As(err, &merr) || merr.Number != 1235 {
				t.Fatalf("READ ONLY tx rejection = %v, want MySQL error 1235", err)
			}
			t.Logf("recorded: START TRANSACTION READ ONLY without noop flag -> error 1235 (governed DSN enables it)")
		}

		ro, err := rt.roUserOpen("tidb_mx_probe")
		if err != nil {
			t.Fatalf("open SELECT-only connection: %v", err)
		}
		defer ro.Close()
		var mysqlErr *mysqldriver.MySQLError
		if _, err := ro.Exec("INSERT INTO tidb_mx_probe.probe_cells (id) VALUES (1)"); err == nil {
			t.Fatal("SELECT-only INSERT unexpectedly succeeded — governed account is not write-restricted")
		} else if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1142 {
			t.Fatalf("SELECT-only INSERT denial = %v, want MySQL error 1142", err)
		}
		var cnt int
		if err := ro.QueryRow("SELECT COUNT(*) FROM tidb_mx_probe.probe_cells").Scan(&cnt); err != nil || cnt != 0 {
			t.Fatalf("SELECT-only account must read cleanly: cnt=%d err=%v", cnt, err)
		}
		if _, err := rt.admin.Exec("INSERT INTO tidb_mx_probe.probe_child (id, parent_id) VALUES (1, 999)"); err == nil {
			t.Fatal("dangling FK insert unexpectedly succeeded — TiDB FK enforcement off")
		} else if !errors.As(err, &mysqlErr) || mysqlErr.Number != 1452 {
			t.Fatalf("dangling FK denial = %v, want MySQL error 1452", err)
		}
		t.Logf("recorded: SELECT-only INSERT denied=1142, dangling FK denied=1452, FK checks on")
	})

	// Execute proves the ordinary keyed path end-to-end on real TiDB: 200
	// with real rows, claim-aware point read, exactly one exec/audit/claim/
	// link, empty connection scope, NULL backend PID, typed user actor, and
	// recorded attempt budget — then replay/conflict and keyless parity.
	t.Run("Execute", func(t *testing.T) {
		fix := setupTiDBKeyedFixture(t, rt, "tidb_mx_exec")
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_exec.rc_cells (
			id BIGINT PRIMARY KEY, note VARCHAR(64), pad MEDIUMTEXT)`)
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_exec.rc_cells (id, note, pad) VALUES
			(1, 'edge-8192', ?), (2, 'over-8193', ?), (3, 'clean', 'short')`,
			strings.Repeat("a", 8192), strings.Repeat("b", 8193))
		seedDisclosurePolicies(t, fix.db, fix.targetID, "tidb_mx_exec", "rc_cells", "id", "note")

		key := "tidb-golden-1"
		execURL := fmt.Sprintf("/query-targets/%d/execute", fix.targetID)
		body := `{"statement":"select id,note from tidb_mx_exec.rc_cells where id = 1","clientExecutionId":"` + key + `"}`

		rec := doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("keyed execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		resp := decodeTiDBExecute(t, rec)
		if resp.Engine != "tidb" || resp.ExecutionID == 0 || resp.Status != "success" {
			t.Fatalf("keyed execute response: engine=%q id=%d status=%q", resp.Engine, resp.ExecutionID, resp.Status)
		}
		if len(resp.Rows) != 1 || len(resp.Rows[0]) != 2 || resp.Rows[0][0] != float64(1) || resp.Rows[0][1] != "edge-8192" {
			t.Fatalf("keyed execute row payload: %v", resp.Rows)
		}
		if len(resp.CellTruncated) != 1 || len(resp.CellTruncated[0]) != 2 ||
			resp.CellTruncated[0][0] || resp.CellTruncated[0][1] {
			t.Fatalf("clean row must carry two false flags, got %v", resp.CellTruncated)
		}
		tidbAssertPointRead(t, fix, key, resp.ExecutionID, "success")

		counts := mysqlTargetCounts(t, fix.db, fix.targetID)
		if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
			t.Fatalf("keyed scope counts = %+v, want {1 1 1 1}", counts)
		}
		tidbAssertLinkedRow(t, fix.db, fix.targetID, key, "success", "", int64(fix.userID), 0)

		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, body)
		tidbAssertErrorCode(t, rec, http.StatusConflict, "execution_already_exists")
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != counts {
			t.Fatalf("replay wrote new evidence: %+v -> %+v", counts, got)
		}

		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
			`{"statement":"select id,note from tidb_mx_exec.rc_cells where id = 2","clientExecutionId":"`+key+`"}`)
		tidbAssertErrorCode(t, rec, http.StatusConflict, "client_execution_id_conflict")
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != counts {
			t.Fatalf("conflict wrote new evidence: %+v -> %+v", counts, got)
		}

		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
			`{"statement":"select id,note from tidb_mx_exec.rc_cells where id = 1"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("keyless absent-key execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
			`{"statement":"select id,note from tidb_mx_exec.rc_cells where id = 1","clientExecutionId":""}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("keyless empty-key execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != (mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 1, Linked: 1}) {
			t.Fatalf("post-keyless counts = %+v, want {3 3 1 1}", got)
		}
	})

	// Saved proves the saved-statement keyed path on real TiDB: typed :param
	// binding, a static statement, replay/conflict, and keyless parity.
	t.Run("Saved", func(t *testing.T) {
		fix := setupTiDBKeyedFixture(t, rt, "tidb_mx_saved")
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_saved.rc_cells (
			id BIGINT PRIMARY KEY, note VARCHAR(64), pad MEDIUMTEXT)`)
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_saved.rc_cells (id, note, pad) VALUES
			(1, 'edge-8192', ?), (2, 'over-8193', ?)`, strings.Repeat("a", 8192), strings.Repeat("b", 8193))
		seedDisclosurePolicies(t, fix.db, fix.targetID, "tidb_mx_saved", "rc_cells", "id", "note")

		createURL := fmt.Sprintf("/query-targets/%d/saved-statements", fix.targetID)
		rec := doBearerWithBody(t, fix.router, http.MethodPost, createURL, fix.token,
			`{"name":"tidb-cells-keyed","statement":"select id, note from tidb_mx_saved.rc_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("create saved statement: got %d: %s", rec.Code, rec.Body.String())
		}
		var created struct {
			ID uint64 `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 {
			t.Fatalf("decode created statement: %v — %s", err, rec.Body.String())
		}
		key := "tidb-saved-1"
		execURL := fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", fix.targetID, created.ID)
		body := `{"values":{"cell_id":1},"clientExecutionId":"` + key + `"}`

		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("keyed saved execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		resp := decodeTiDBExecute(t, rec)
		if resp.Engine != "tidb" || resp.Status != "success" || len(resp.Rows) != 1 ||
			resp.Rows[0][0] != float64(1) || resp.Rows[0][1] != "edge-8192" {
			t.Fatalf("keyed saved response: %v engine=%q", resp.Rows, resp.Engine)
		}
		if len(resp.CellTruncated) != 1 || len(resp.CellTruncated[0]) != 2 ||
			resp.CellTruncated[0][0] || resp.CellTruncated[0][1] {
			t.Fatalf("clean saved row must carry two false flags, got %v", resp.CellTruncated)
		}
		tidbAssertPointRead(t, fix, key, resp.ExecutionID, "success")
		counts := mysqlTargetCounts(t, fix.db, fix.targetID)
		if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
			t.Fatalf("keyed saved counts = %+v, want {1 1 1 1}", counts)
		}
		tidbAssertLinkedRow(t, fix.db, fix.targetID, key, "success", "", int64(fix.userID), 0)

		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, body)
		tidbAssertErrorCode(t, rec, http.StatusConflict, "execution_already_exists")
		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
			`{"values":{"cell_id":2},"clientExecutionId":"`+key+`"}`)
		tidbAssertErrorCode(t, rec, http.StatusConflict, "client_execution_id_conflict")
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != counts {
			t.Fatalf("replay/conflict wrote new evidence: %+v -> %+v", counts, got)
		}

		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token, `{"values":{"cell_id":1}}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("keyless saved execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
			`{"values":{"cell_id":1},"clientExecutionId":""}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("empty-key saved execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}

		// Static statement (no params) keyed execute on TiDB.
		rec = doBearerWithBody(t, fix.router, http.MethodPost, createURL, fix.token,
			`{"name":"tidb-cells-static","statement":"select id, note from tidb_mx_saved.rc_cells where id = 2","scope":"personal"}`)
		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("create static saved statement: got %d: %s", rec.Code, rec.Body.String())
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 {
			t.Fatalf("decode static statement: %v — %s", err, rec.Body.String())
		}
		staticURL := fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", fix.targetID, created.ID)
		rec = doBearerWithBody(t, fix.router, http.MethodPost, staticURL, fix.token,
			`{"clientExecutionId":"tidb-saved-static-1"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("keyed static saved execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != (mysqlKeyedScopeCounts{Executions: 4, Audits: 4, Claims: 2, Linked: 2}) {
			t.Fatalf("post-keyless counts = %+v, want {4 4 2 2}", got)
		}
		tidbAssertLinkedRow(t, fix.db, fix.targetID, "tidb-saved-static-1", "success", "", int64(fix.userID), 0)
	})

	// Navigation proves the keyed related-record path on real TiDB FK
	// metadata: localValues bound through ?, replay/conflict, the controlled
	// wrong-count rejection (pre-claim — the key stays free for repair), and
	// keyless parity.
	t.Run("Navigation", func(t *testing.T) {
		fix := setupTiDBKeyedFixture(t, rt, "tidb_mx_nav")
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_nav.rc_parent (
			id BIGINT PRIMARY KEY, parent_code VARCHAR(32) NOT NULL UNIQUE, pad MEDIUMTEXT)`)
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_nav.rc_child (
			id BIGINT PRIMARY KEY, parent_id BIGINT NOT NULL,
			CONSTRAINT fk_rc_child_parent FOREIGN KEY (parent_id) REFERENCES tidb_mx_nav.rc_parent(id))`)
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_nav.rc_parent (id, parent_code, pad) VALUES
			(1, 'P_ALPHA', ?), (2, 'P_BETA', 'short')`, strings.Repeat("w", 9000))
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_nav.rc_child (id, parent_id) VALUES (1, 1), (2, 2)`)
		seedDisclosurePolicies(t, fix.db, fix.targetID, "tidb_mx_nav", "rc_parent", "id", "parent_code", "pad")

		navURL := fmt.Sprintf("/query-targets/%d/related-records", fix.targetID)
		navBody := func(localValuesJSON, key string) string {
			k := ""
			if key != "" {
				k = `,"clientExecutionId":` + strconv.Quote(key)
			}
			return fmt.Sprintf(`{"source":{"database":"tidb_mx_nav","object":"rc_child","kind":"table","foreignKey":"fk_rc_child_parent"},"localValues":%s%s}`, localValuesJSON, k)
		}
		key := "tidb-nav-1"

		rec := doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody(`["2"]`, key))
		if rec.Code != http.StatusOK {
			t.Fatalf("keyed navigation: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		var resp tidbNavResp
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode navigation response: %v", err)
		}
		if resp.Engine != "tidb" || resp.Status != "success" ||
			resp.ReferencedObject != "rc_parent" || resp.ReferencedDatabase != "tidb_mx_nav" ||
			len(resp.ReferencedColumns) != 1 || resp.ReferencedColumns[0] != "id" {
			t.Fatalf("navigation relation metadata: %+v", resp)
		}
		if len(resp.Rows) != 1 || len(resp.Rows[0]) != 3 || resp.Rows[0][0] != float64(2) || resp.Rows[0][1] != "P_BETA" {
			t.Fatalf("keyed navigation row payload: %v", resp.Rows)
		}
		if len(resp.CellTruncated) != 1 || len(resp.CellTruncated[0]) != 3 ||
			resp.CellTruncated[0][0] || resp.CellTruncated[0][1] || resp.CellTruncated[0][2] {
			t.Fatalf("clean parent row must carry three false flags, got %v", resp.CellTruncated)
		}
		tidbAssertPointRead(t, fix, key, resp.ExecutionID, "success")
		counts := mysqlTargetCounts(t, fix.db, fix.targetID)
		if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
			t.Fatalf("keyed navigation counts = %+v, want {1 1 1 1}", counts)
		}
		tidbAssertLinkedRow(t, fix.db, fix.targetID, key, "success", "", int64(fix.userID), 0)

		rec = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody(`["2"]`, key))
		tidbAssertErrorCode(t, rec, http.StatusConflict, "execution_already_exists")
		rec = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody(`["1"]`, key))
		tidbAssertErrorCode(t, rec, http.StatusConflict, "client_execution_id_conflict")
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != counts {
			t.Fatalf("replay/conflict wrote new evidence: %+v -> %+v", counts, got)
		}

		// Wrong local-value count is a controlled pre-claim rejection: the
		// rejected attempt writes a NULL-key pair, and the occupied key stays
		// free so the corrected same-key request still claims it.
		rec = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody(`["2","extra"]`, "tidb-nav-repair"))
		tidbAssertErrorCode(t, rec, http.StatusBadRequest, "validation_failed")
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != (mysqlKeyedScopeCounts{Executions: 2, Audits: 2, Claims: 1, Linked: 1}) {
			t.Fatalf("rejected nav wrote a claim or skipped the evidence pair: %+v", got)
		}
		rec = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody(`["2"]`, "tidb-nav-repair"))
		if rec.Code != http.StatusOK {
			t.Fatalf("repaired same-key navigation: got %d want 200 (key must stay free): %s", rec.Code, rec.Body.String())
		}
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != (mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 2, Linked: 2}) {
			t.Fatalf("post-repair counts = %+v, want {3 3 2 2}", got)
		}

		rec = doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, navBody(`["2"]`, ""))
		if rec.Code != http.StatusOK {
			t.Fatalf("keyless navigation: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != (mysqlKeyedScopeCounts{Executions: 4, Audits: 4, Claims: 2, Linked: 2}) {
			t.Fatalf("post-keyless counts = %+v, want {4 4 2 2}", got)
		}
	})

	// FKNameDedup is the real-engine regression for the schema-inspector fix
	// (AND kcu.TABLE_NAME = rc.TABLE_NAME): TiDB permits the same constraint
	// name on different child tables, and the join must scope rows to the
	// inspected child — without it the first child's FK metadata would leak
	// the second child's column mapping and navigation would mis-bind.
	t.Run("FKNameDedup", func(t *testing.T) {
		fix := setupTiDBKeyedFixture(t, rt, "tidb_mx_fkdup")
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_fkdup.rc_parent (
			id BIGINT PRIMARY KEY, parent_code VARCHAR(32) NOT NULL UNIQUE, pad MEDIUMTEXT)`)
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_fkdup.rc_child (
			id BIGINT PRIMARY KEY, parent_id BIGINT NOT NULL,
			CONSTRAINT fk_rc_child_parent FOREIGN KEY (parent_id) REFERENCES tidb_mx_fkdup.rc_parent(id))`)
		// Same constraint name on a second child, but a DIFFERENT column and
		// DIFFERENT referenced column — leaked metadata would corrupt the
		// first child's mapping or inflate its column count.
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_fkdup.rc_child_other (
			id BIGINT PRIMARY KEY, parent_ref VARCHAR(32) NOT NULL,
			CONSTRAINT fk_rc_child_parent FOREIGN KEY (parent_ref) REFERENCES tidb_mx_fkdup.rc_parent(parent_code))`)
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_fkdup.rc_parent (id, parent_code, pad) VALUES
			(1, 'P_ALPHA', ?), (2, 'P_BETA', 'short')`, strings.Repeat("w", 9000))
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_fkdup.rc_child (id, parent_id) VALUES (1, 1), (2, 2)`)
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_fkdup.rc_child_other (id, parent_ref) VALUES (10, 'P_ALPHA'), (20, 'P_BETA')`)
		seedDisclosurePolicies(t, fix.db, fix.targetID, "tidb_mx_fkdup", "rc_parent", "id", "parent_code", "pad")

		navURL := fmt.Sprintf("/query-targets/%d/related-records", fix.targetID)
		nav := func(object, localValue, key string) tidbNavResp {
			body := fmt.Sprintf(`{"source":{"database":"tidb_mx_fkdup","object":%q,"kind":"table","foreignKey":"fk_rc_child_parent"},"localValues":[%q],"clientExecutionId":%q}`,
				object, localValue, key)
			rec := doBearerWithBody(t, fix.router, http.MethodPost, navURL, fix.token, body)
			if rec.Code != http.StatusOK {
				t.Fatalf("nav on %s: got %d want 200: %s", object, rec.Code, rec.Body.String())
			}
			var resp tidbNavResp
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode nav response: %v", err)
			}
			return resp
		}

		// First child: the inspector must return THIS table's FK — local
		// column parent_id referencing rc_parent.id — not the second child's
		// parent_ref -> parent_code row leaked by the name-only join.
		resp := nav("rc_child", "2", "tidb-fkdedup-1")
		if resp.ReferencedObject != "rc_parent" || resp.ForeignKey != "fk_rc_child_parent" ||
			len(resp.ReferencedColumns) != 1 || resp.ReferencedColumns[0] != "id" {
			t.Fatalf("rc_child FK resolution leaked the sibling mapping: %+v", resp)
		}
		if len(resp.Rows) != 1 || resp.Rows[0][0] != float64(2) || resp.Rows[0][1] != "P_BETA" {
			t.Fatalf("rc_child navigation row: %v", resp.Rows)
		}
		tidbAssertPointRead(t, fix, "tidb-fkdedup-1", resp.ExecutionID, "success")
		tidbAssertLinkedRow(t, fix.db, fix.targetID, "tidb-fkdedup-1", "success", "", int64(fix.userID), 0)

		// Second child resolves its own same-named FK independently.
		resp = nav("rc_child_other", "P_BETA", "tidb-fkdedup-2")
		if len(resp.ReferencedColumns) != 1 || resp.ReferencedColumns[0] != "parent_code" {
			t.Fatalf("rc_child_other FK resolution: %+v", resp)
		}
		if len(resp.Rows) != 1 || resp.Rows[0][0] != float64(2) || resp.Rows[0][1] != "P_BETA" {
			t.Fatalf("rc_child_other navigation row: %v", resp.Rows)
		}
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != (mysqlKeyedScopeCounts{Executions: 2, Audits: 2, Claims: 2, Linked: 2}) {
			t.Fatalf("dedup counts = %+v, want {2 2 2 2}", got)
		}
	})

	// CapabilityGate proves the truncation contract on real TiDB: a truncated
	// cell refused without the capability returns 409 with no rows while the
	// keyed success evidence stays persisted; with the capability the same
	// key space carries the retained prefix and the exact truncated flags.
	t.Run("CapabilityGate", func(t *testing.T) {
		fix := setupTiDBKeyedFixture(t, rt, "tidb_mx_gate")
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_gate.rc_cells (
			id BIGINT PRIMARY KEY, note VARCHAR(64), pad MEDIUMTEXT)`)
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_gate.rc_cells (id, note, pad) VALUES (2, 'over-8193', ?)`, strings.Repeat("b", 8193))
		seedDisclosurePolicies(t, fix.db, fix.targetID, "tidb_mx_gate", "rc_cells", "id", "pad")

		execURL := fmt.Sprintf("/query-targets/%d/execute", fix.targetID)
		stmt := `"select id, pad from tidb_mx_gate.rc_cells where id = 2"`

		// Keyed, no capability: post-finalize refusal — 409, no rows payload,
		// but the success pair and the claim link are already committed.
		rec := doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
			`{"statement":`+stmt+`,"clientExecutionId":"tidb-gate-1"}`)
		tidbAssertErrorCode(t, rec, http.StatusConflict, "result_contract_upgrade_required")
		if strings.Contains(rec.Body.String(), `"rows"`) {
			t.Fatalf("refusal leaked rows: %s", rec.Body.String())
		}
		counts := mysqlTargetCounts(t, fix.db, fix.targetID)
		if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
			t.Fatalf("refusal counts = %+v, want {1 1 1 1}", counts)
		}
		tidbAssertLinkedRow(t, fix.db, fix.targetID, "tidb-gate-1", "success", "", int64(fix.userID), 0)

		// The refused key is occupied: replaying it returns already-exists,
		// while a different key with the capability delivers the truncated row.
		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
			`{"statement":`+stmt+`,"clientExecutionId":"tidb-gate-1","capabilities":["cellTruncated"]}`)
		tidbAssertErrorCode(t, rec, http.StatusConflict, "execution_already_exists")

		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, fix.token,
			`{"statement":`+stmt+`,"clientExecutionId":"tidb-gate-2","capabilities":["cellTruncated"]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("capable execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		resp := decodeTiDBExecute(t, rec)
		if resp.Engine != "tidb" || len(resp.Rows) != 1 || len(resp.CellTruncated) != 1 {
			t.Fatalf("capable response: %+v", resp)
		}
		if len(resp.CellTruncated[0]) != 2 || resp.CellTruncated[0][0] || !resp.CellTruncated[0][1] {
			t.Fatalf("truncation matrix = %v, want [false true]", resp.CellTruncated)
		}
		pad, ok := resp.Rows[0][1].(string)
		if !ok || len(pad) != 8192 || !utf8.ValidString(pad) ||
			!strings.HasPrefix(strings.Repeat("b", 8193), pad) {
			t.Fatalf("retained %d bytes valid=%v, want the exact 8192-byte 'b' prefix", len(pad), utf8.ValidString(pad))
		}
		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != (mysqlKeyedScopeCounts{Executions: 2, Audits: 2, Claims: 2, Linked: 2}) {
			t.Fatalf("capability counts = %+v, want {2 2 2 2}", got)
		}
		// The 409-refused attempt still reads back as its committed success.
		var refusedExecID int64
		if err := fix.db.QueryRow(`SELECT id FROM query_executions WHERE target_resource_id = ? AND client_execution_id = ?`,
			fix.targetID, "tidb-gate-1").Scan(&refusedExecID); err != nil {
			t.Fatalf("refused exec lookup: %v", err)
		}
		tidbAssertPointRead(t, fix, "tidb-gate-1", uint64(refusedExecID), "success")
		tidbAssertPointRead(t, fix, "tidb-gate-2", resp.ExecutionID, "success")
	})

	// ClientCancel proves a real client cancellation mid-statement lands as a
	// terminal failed/query_canceled attempt — never a PostgreSQL cancelled
	// status — with the claim linked. The processlist barrier guarantees the
	// statement was genuinely executing on TiDB when the cancel fired.
	t.Run("ClientCancel", func(t *testing.T) {
		fix := setupTiDBKeyedFixture(t, rt, "tidb_mx_cancel")
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_cancel.rc_slow (
			id BIGINT PRIMARY KEY, note VARCHAR(128))`)
		// Enough rows that the MD5-ordering statement is observably in-flight;
		// a processlist barrier (not a sleep) is the timing source of truth.
		for base := 0; base < 300000; base += 2000 {
			var b strings.Builder
			b.WriteString("INSERT INTO tidb_mx_cancel.rc_slow (id, note) VALUES ")
			for i := 0; i < 2000; i++ {
				if i > 0 {
					b.WriteByte(',')
				}
				fmt.Fprintf(&b, "(%d, 'note-%d')", base+i+1, base+i+1)
			}
			mustTiDBExec(t, rt.admin, b.String())
		}
		seedDisclosurePolicies(t, fix.db, fix.targetID, "tidb_mx_cancel", "rc_slow", "id", "note")

		marker := "tidbmxCancelMarker01"
		key := "tidb-cancel-1"
		execURL := fmt.Sprintf("/query-targets/%d/execute", fix.targetID)
		body := fmt.Sprintf(`{"statement":"select id, note from tidb_mx_cancel.rc_slow where note <> '%s' order by md5(note)","clientExecutionId":%q}`, marker, key)

		reqCtx, cancelReq := context.WithCancel(context.Background())
		defer cancelReq()
		resCh := doBearerCancelable(t, fix.router, reqCtx, http.MethodPost, execURL, fix.token, body)

		// Wait until TiDB's own processlist shows the statement executing —
		// the cancel then provably lands mid-flight on the real engine.
		seen := false
		deadline := time.Now().Add(15 * time.Second)
		for !seen && time.Now().Before(deadline) {
			var n int
			if err := rt.admin.QueryRow(
				`SELECT COUNT(*) FROM information_schema.processlist
				 WHERE INFO LIKE ? AND ID <> CONNECTION_ID()`, "%"+marker+"%").Scan(&n); err == nil && n > 0 {
				seen = true
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		if !seen {
			t.Fatal("marker statement never appeared in TiDB processlist — cannot prove a mid-flight cancel")
		}
		cancelReq()

		var rec *httptest.ResponseRecorder
		select {
		case rec = <-resCh:
		case <-time.After(30 * time.Second):
			t.Fatal("canceled request did not return within 30s")
		}
		tidbAssertErrorCode(t, rec, http.StatusBadGateway, "query_backend_error")

		counts := mysqlTargetCounts(t, fix.db, fix.targetID)
		if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
			t.Fatalf("cancel counts = %+v, want {1 1 1 1}", counts)
		}
		// The persisted terminal must be the MySQL/TiDB taxonomy:
		// status failed + error_code query_canceled — never PG 'cancelled'.
		tidbAssertLinkedRow(t, fix.db, fix.targetID, key, "failed", "query_canceled", int64(fix.userID), 0)
		// Point read returns the same terminal with the failed status.
		var execID int64
		if err := fix.db.QueryRow(`SELECT id FROM query_executions WHERE target_resource_id = ? AND client_execution_id = ?`,
			fix.targetID, key).Scan(&execID); err != nil {
			t.Fatalf("cancel exec lookup: %v", err)
		}
		tidbAssertPointRead(t, fix, key, uint64(execID), "failed")
	})

	// MachineActor proves the ordinary route admits a typed machine identity
	// for keyed execution (persisting actor_machine_principal_id on claim and
	// execution) while saved-statement and navigation routes deny it with the
	// controlled 403 machine_scope_denied.
	t.Run("MachineActor", func(t *testing.T) {
		fix := setupTiDBKeyedFixture(t, rt, "tidb_mx_machine")
		mustTiDBExec(t, rt.admin, `CREATE TABLE tidb_mx_machine.rc_cells (
			id BIGINT PRIMARY KEY, note VARCHAR(64))`)
		mustTiDBExec(t, rt.admin, `INSERT INTO tidb_mx_machine.rc_cells (id, note) VALUES (1, 'machine-row')`)
		seedDisclosurePolicies(t, fix.db, fix.targetID, "tidb_mx_machine", "rc_cells", "id", "note")

		machSvc := service.NewMachinePrincipalService(mysql.NewMachinePrincipalRepository(fix.db))
		issue, err := machSvc.Create(context.Background(),
			service.AuthenticatedUser{ID: fix.userID, Role: "admin"},
			model.MachinePrincipalCreateRequest{
				Name:   "tidb-mx-agent",
				Scopes: []model.MachineScope{model.MachineScopeGovernedSelect},
			})
		if err != nil || issue.Secret == "" {
			t.Fatalf("create machine principal: %v", err)
		}
		mach := issue.Secret

		execURL := fmt.Sprintf("/query-targets/%d/execute", fix.targetID)
		key := "tidb-machine-1"
		rec := doBearerWithBody(t, fix.router, http.MethodPost, execURL, mach,
			`{"statement":"select id,note from tidb_mx_machine.rc_cells where id = 1","clientExecutionId":"`+key+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("machine keyed execute: got %d want 200: %s", rec.Code, rec.Body.String())
		}
		resp := decodeTiDBExecute(t, rec)
		if resp.Engine != "tidb" || resp.Status != "success" || len(resp.Rows) != 1 || resp.Rows[0][1] != "machine-row" {
			t.Fatalf("machine execute response: %+v", resp)
		}
		counts := mysqlTargetCounts(t, fix.db, fix.targetID)
		if counts != (mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1}) {
			t.Fatalf("machine counts = %+v, want {1 1 1 1}", counts)
		}
		// Typed machine actor persisted on both claim and execution.
		tidbAssertLinkedRow(t, fix.db, fix.targetID, key, "success", "", 0, int64(issue.Principal.ID))

		// Same-key occupancy holds for machine actors too.
		rec = doBearerWithBody(t, fix.router, http.MethodPost, execURL, mach,
			`{"statement":"select id,note from tidb_mx_machine.rc_cells where id = 1","clientExecutionId":"`+key+`"}`)
		tidbAssertErrorCode(t, rec, http.StatusConflict, "execution_already_exists")

		// Saved-statement execute is user-only: the machine credential is a
		// controlled 403 machine_scope_denied — never an execution.
		rec = doBearerWithBody(t, fix.router, http.MethodPost, createURLFor(fix), fix.token,
			`{"name":"tidb-mx-deny","statement":"select id, note from tidb_mx_machine.rc_cells where id = 1","scope":"personal"}`)
		if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
			t.Fatalf("create saved stmt: %d %s", rec.Code, rec.Body.String())
		}
		var created struct {
			ID uint64 `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 {
			t.Fatalf("decode saved stmt: %v", err)
		}
		rec = doBearerWithBody(t, fix.router, http.MethodPost,
			fmt.Sprintf("/query-targets/%d/saved-statements/%d/execute", fix.targetID, created.ID),
			mach, `{"clientExecutionId":"tidb-machine-deny-1"}`)
		tidbAssertErrorCode(t, rec, http.StatusForbidden, "machine_scope_denied")

		rec = doBearerWithBody(t, fix.router, http.MethodPost,
			fmt.Sprintf("/query-targets/%d/related-records", fix.targetID),
			mach, `{"source":{"database":"tidb_mx_machine","object":"rc_cells","kind":"table","foreignKey":"none"},"localValues":["1"],"clientExecutionId":"tidb-machine-deny-2"}`)
		tidbAssertErrorCode(t, rec, http.StatusForbidden, "machine_scope_denied")

		if got := mysqlTargetCounts(t, fix.db, fix.targetID); got != counts {
			t.Fatalf("machine denials wrote evidence: %+v -> %+v", counts, got)
		}
	})
}

// createURLFor is the saved-statements collection path for the fixture.
func createURLFor(fix tidbKeyedFixture) string {
	return fmt.Sprintf("/query-targets/%d/saved-statements", fix.targetID)
}

// stubTiDBContainer is the minimal testcontainers.Container stand-in for the
// control-flow injection tests below — only GetContainerID and Terminate are
// exercised; any other call would panic on the embedded nil interface.
type stubTiDBContainer struct {
	testcontainers.Container
	id         string
	termErr    error
	terminates atomic.Int32
}

func (s *stubTiDBContainer) GetContainerID() string { return s.id }

func (s *stubTiDBContainer) Terminate(context.Context, ...testcontainers.TerminateOption) error {
	s.terminates.Add(1)
	return s.termErr
}

// TestTiDBKeyedRuntime_StartCleanupInjection is a CONTROL-FLOW INJECTION
// test: the tidbGenericContainer factory seam is stubbed so each return shape
// testcontainers-go v0.42 permits — (container, nil), (nil, error), and
// (container, error) — can be checked for the ownership contract without
// Docker. Each case runs the call inside a child scope so the registered
// cleanup has provably executed when the parent asserts.
func TestTiDBKeyedRuntime_StartCleanupInjection(t *testing.T) {
	orig := tidbGenericContainer
	t.Cleanup(func() { tidbGenericContainer = orig })

	t.Run("container returned without error", func(t *testing.T) {
		stub := &stubTiDBContainer{id: "stub-normal"}
		tidbGenericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
			return stub, nil
		}
		var got testcontainers.Container
		var gotErr error
		t.Run("owning scope", func(t *testing.T) {
			got, gotErr = startOwnedTiDBContainer(t, context.Background(), testcontainers.ContainerRequest{})
		})
		if gotErr != nil || got != stub {
			t.Fatalf("startOwnedTiDBContainer = (%v, %v), want (stub, nil)", got, gotErr)
		}
		if n := stub.terminates.Load(); n != 1 {
			t.Fatalf("scope cleanup must terminate the owned object exactly once, got %d", n)
		}
	})

	t.Run("nil container with error", func(t *testing.T) {
		injected := errors.New("provider refused create")
		tidbGenericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
			return nil, injected
		}
		var gotErr error
		t.Run("owning scope", func(t *testing.T) {
			got, err := startOwnedTiDBContainer(t, context.Background(), testcontainers.ContainerRequest{})
			if got != nil {
				t.Fatalf("nil-container path must return nil, got %v", got)
			}
			gotErr = err
		})
		if !errors.Is(gotErr, injected) {
			t.Fatalf("error = %v, want the injected %v", gotErr, injected)
		}
	})

	t.Run("container returned alongside a start error", func(t *testing.T) {
		injected := errors.New("readiness wait timed out")
		stub := &stubTiDBContainer{id: "stub-partial", termErr: errors.New("terminate refused")}
		tidbGenericContainer = func(context.Context, testcontainers.GenericContainerRequest) (testcontainers.Container, error) {
			return stub, injected
		}
		var gotErr error
		t.Run("owning scope", func(t *testing.T) {
			got, err := startOwnedTiDBContainer(t, context.Background(), testcontainers.ContainerRequest{})
			if got != stub {
				t.Fatalf("partial-failure path must still return the created container, got %v", got)
			}
			gotErr = err
		})
		// The cleanup's Terminate error is only logged — the original start
		// error reaches the caller unmasked.
		if !errors.Is(gotErr, injected) {
			t.Fatalf("error = %v, want the original start error %v unmasked", gotErr, injected)
		}
		if n := stub.terminates.Load(); n != 1 {
			t.Fatalf("scope cleanup must still terminate the partially-created container once, got %d", n)
		}
	})
}

// TestTiDBKeyedRuntime_StartFailureCleansOwnedContainer is the real-Docker
// proof of the partial-start contract: a request whose readiness wait can
// never succeed still yields a created container, and once its owning scope
// ends that exact container ID is gone — released by the registered scope
// cleanup, not deferred to Ryuk reaping after process exit.
func TestTiDBKeyedRuntime_StartFailureCleansOwnedContainer(t *testing.T) {
	var cid string
	t.Run("doomed readiness scope", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		req := testcontainers.ContainerRequest{
			Image:        tidbKeyedImage,
			Cmd:          []string{"--store=unistore", "--path=/tmp/owned-start-failure", "--host=0.0.0.0", "-P=4000"},
			ExposedPorts: []string{"4000/tcp"},
			Labels:       map[string]string{"controlhub.test-scope": "tidb-keyed-matrix"},
			// 4999/tcp is never exposed or listened on: the container is
			// created and started, then readiness fails — the exact shape in
			// which v0.42 returns a non-nil container alongside the error.
			WaitingFor: wait.ForListeningPort("4999/tcp").WithStartupTimeout(20 * time.Second),
		}
		container, err := startOwnedTiDBContainer(t, ctx, req)
		if err == nil {
			t.Fatal("expected the doomed readiness wait to fail")
		}
		if container == nil {
			t.Fatalf("testcontainers v0.42 must return the created container alongside the readiness error, got nil (err=%v)", err)
		}
		cid = container.GetContainerID()
		t.Logf("created container id=%s then failed readiness as injected: %v", cid, err)
	})
	if cid == "" {
		t.Fatal("the doomed scope never exposed the created container id")
	}
	if exists, _ := dockerTiDBContainerState(t, cid); exists {
		t.Fatalf("owned container %s survived its failed scope — registered cleanup did not release it", cid)
	}
	t.Logf("confirmed: owned container id=%s absent immediately after its failed scope ended", cid)
}

// TestTiDBKeyedFixture_ContainerOwnership proves real overlapping ownership:
// a foreign container (created and owned by THIS run under a unique per-run
// name) plus fixture runtimes A and B are all alive at the SAME time, then
// each is destroyed one at a time and only its own ID disappears while the
// others stay running and reachable. A second phase proves the retired fixed
// name "controlhub-tidb-matrix" is never a destroy handle: an external holder
// is observed read-only, a self-created placeholder is removed by owned ID
// only. (This replaces the R1 version, where A and B merely coexisted with
// the foreign container in sequence — A was already destroyed before B
// started.)
func TestTiDBKeyedFixture_ContainerOwnership(t *testing.T) {
	foreignName := fmt.Sprintf("controlhub-tidb-foreign-%d", time.Now().UnixNano())
	foreignID := dockerTiDB(t, "run", "-d", "--name", foreignName,
		tidbKeyedImage, "--store=unistore", "--path=/tmp/foreign-fixture", "--host=0.0.0.0", "-P=4000")
	// This run owns foreignID, so cleanup removes it — but strictly by the
	// returned ID; the name is display-only and never a destroy handle.
	t.Cleanup(func() {
		out, err := exec.Command("docker", "rm", "-f", foreignID).CombinedOutput()
		t.Logf("foreign %s cleanup by owned id %s: err=%v out=%s", foreignName, foreignID, err, strings.TrimSpace(string(out)))
	})
	if exists, running := dockerTiDBContainerState(t, foreignID); !exists || !running {
		t.Fatalf("foreign container %s (id %s) must be running before fixtures start", foreignName, foreignID)
	}

	// A and B both stay alive — real lifetime overlap with foreign.
	rtA := startTiDBKeyedRuntime(t)
	rtB := startTiDBKeyedRuntime(t)
	idA, idB := rtA.containerID, rtB.containerID
	if idA == "" || idB == "" || idA == idB || idA == foreignID || idB == foreignID {
		t.Fatalf("need three distinct live IDs, got A=%q B=%q foreign=%q", idA, idB, foreignID)
	}
	for name, id := range map[string]string{"fixture A": idA, "fixture B": idB, "foreign": foreignID} {
		if exists, running := dockerTiDBContainerState(t, id); !exists || !running {
			t.Fatalf("%s container %s must exist and be running while all three coexist", name, id)
		}
	}

	// Destroy ONLY A by its owned object: B and foreign stay up, and B's real
	// TiDB endpoint still answers.
	rtA.terminate(t, "ownership check: A torn down first")
	if exists, _ := dockerTiDBContainerState(t, idA); exists {
		t.Fatalf("fixture A container %s still present after its owned terminate", idA)
	}
	if _, running := dockerTiDBContainerState(t, idB); !running {
		t.Fatal("fixture B must survive fixture A's teardown")
	}
	if _, running := dockerTiDBContainerState(t, foreignID); !running {
		t.Fatal("foreign container must survive fixture A's teardown")
	}
	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	pingErr := rtB.admin.PingContext(pingCtx)
	pingCancel()
	if pingErr != nil {
		t.Fatalf("fixture B must stay reachable after A's teardown: %v", pingErr)
	}

	// Destroy ONLY B: the foreign container remains untouched.
	rtB.terminate(t, "ownership check: B torn down second")
	if exists, _ := dockerTiDBContainerState(t, idB); exists {
		t.Fatalf("fixture B container %s still present after its owned terminate", idB)
	}
	if _, running := dockerTiDBContainerState(t, foreignID); !running {
		t.Fatal("foreign container must survive both fixture teardowns")
	}
	// A's and B's registered scope cleanups still fire at test end — a second
	// Terminate on the same owned IDs, expected to log a harmless error.

	t.Run("legacy fixed-name holder is never a destroy target", func(t *testing.T) {
		legacy := "controlhub-tidb-matrix"
		if exists, _ := dockerTiDBContainerState(t, legacy); exists {
			// An EXTERNAL object holds the retired fixed name: record its
			// identity, never adopt it, never delete it.
			holder := dockerTiDB(t, "inspect", "-f", "{{.Id}} {{.State.Status}}", legacy)
			t.Logf("external object occupies %s (%s) — read-only observation, not owned by this run", legacy, holder)
			t.Run("fixture lifecycle beside external holder", func(t *testing.T) {
				startTiDBKeyedRuntime(t)
			})
			if after := dockerTiDB(t, "inspect", "-f", "{{.Id}}", legacy); after != strings.Fields(holder)[0] {
				t.Fatalf("external %s holder changed during the fixture lifecycle: was %q now %q", legacy, holder, after)
			}
			return
		}
		// Name free: occupy it with a placeholder this run owns (created, not
		// started — name occupation is all that is needed). If a race takes
		// the name between check and create, do NOT escalate to a force
		// delete or adopt the newcomer — observe read-only instead.
		out, err := exec.Command("docker", "create", "--name", legacy, tidbKeyedImage,
			"--store=unistore", "--path=/tmp/legacy-holder", "--host=0.0.0.0", "-P=4000").CombinedOutput()
		if err != nil {
			if strings.Contains(string(out), "Conflict") || strings.Contains(string(out), "already in use") {
				t.Logf("%s was taken between check and create — leaving the external object untouched", legacy)
				return
			}
			t.Fatalf("docker create %s: %v — %s", legacy, err, strings.TrimSpace(string(out)))
		}
		legacyID := strings.TrimSpace(string(out))
		t.Cleanup(func() {
			rmOut, rmErr := exec.Command("docker", "rm", legacyID).CombinedOutput()
			t.Logf("legacy placeholder cleanup by owned id %s: err=%v out=%s", legacyID, rmErr, strings.TrimSpace(string(rmOut)))
		})
		t.Run("fixture lifecycle beside owned placeholder", func(t *testing.T) {
			startTiDBKeyedRuntime(t)
		})
		if exists, _ := dockerTiDBContainerState(t, legacyID); !exists {
			t.Fatalf("owned %s placeholder %s vanished during a fixture lifecycle", legacy, legacyID)
		}
	})
}
