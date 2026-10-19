//go:build integration

// Package integration — real-MySQL A–J acceptance matrix for the keyed multi-entry (ordinary/saved/navigation) execution protocol.
// input: shared MySQL fixture, real api router/auth/service/repository/disclosure/executor stack, wire observer, fault triggers, frozen digest goldens
// output: TestMySQLKeyedMatrix* cases proving keyless compatibility, concurrency exactly-once, wire-vs-persistence separation, boundary/failure phases, real HTTP abort, storage faults, privacy/auth, frozen digests, result contracts, and read consistency
// pos: full production-path acceptance boundary for the keyed claim protocol on MySQL — wire frames, claim rows, and evidence pairs are counted and proven separately; TiDB coverage is intentionally out of scope for this packet
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/api"
	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// ---------------------------------------------------------------------------
// Fixture: a per-test keyed matrix target with optional wire observation.
// km_* tables live beside rc_* in query_e2e_aux so the matrix never collides
// with the T8-B fixture rows; disclosure policies scope every projected
// column exactly like production governance.
// ---------------------------------------------------------------------------

const (
	kmDatabase = "query_e2e_aux"
	kmCells    = "km_cells"
	kmParent   = "km_parent"
	kmChild    = "km_child"
	kmPaged    = "km_paged"
	kmBulk     = "km_bulk"
)

// keyedMatrixFixture is the fully wired HTTP boundary for the keyed matrix:
// real router over real execution/disclosure/evidence repositories against
// disposable MySQL, with optional target-wire observation and optional claim/
// finalize store fault injection.
type keyedMatrixFixture struct {
	router         http.Handler
	token          string // admin bearer (owner actor)
	otherToken     string // second user's bearer
	targetID       uint64
	db             *sql.DB
	execRepo       *mysql.QueryExecutionRepository // real repo for direct claim reads/seeds
	faultRepo      *keyedMatrixFaultRepo           // nil unless wrapRepo
	observer       *mysqlKeyedWireObserver         // nil unless observedTable set
	machineSvc     *service.MachinePrincipalService
	adminUserID    uint64
	otherUserID    uint64
	credentialRef  string
	savedPath      string // /query-targets/{id}/saved-statements
	executePath    string // /query-targets/{id}/execute
	navigationPath string // /query-targets/{id}/related-records
	historyPath    string // /query-targets/{id}/executions
}

// keyedMatrixOpts parameterizes setupKeyedMatrixFixture.
type keyedMatrixOpts struct {
	// observedTable, when non-empty, routes the credential DSN through a
	// plaintext wire observer selecting result commands on
	// query_e2e_aux.<observedTable>; the target profile host/port becomes the
	// observer's listen address so validateDSNBinding still holds.
	observedTable string
	// observedMetaTable optionally arms a second observer selector on
	// <observedMetaDatabase>.<observedMetaTable> so schema-metadata
	// (preflight/inspector) commands are counted separately from results.
	observedMetaDatabase string
	observedMetaTable    string
	// credentialEnabled=false starts the target's credential row disabled
	// (denied-access fixture; repair with UPDATE to enable).
	credentialDisabled bool
	// environmentID selects the target environment (envProd tightens the
	// attempt budget and hard maxRows); zero resolves to envStaging.
	environmentID uint64
	// wrapRepo substitutes the executions repository with a fault-injecting
	// wrapper (claim admission / keyed finalize).
	wrapRepo bool
	// statementAPI wires the owner-only execution-statement read route.
	statementAPI bool
	// maskedSecret adds a masked_no_copy policy on km_cells.secret.
	maskedSecret bool
	// childTwo adds a second FK child table (km_child2 → km_parent) in the
	// same aux database for navigation digest/source-object variants.
	childTwo bool
	// auxTwo adds a second aux database query_e2e_aux2 holding its own
	// km_child/km_parent FK pair for source-database digest variants.
	auxTwo bool
}

// keyedMatrixFaultRepo wraps the real executions repository with switchable
// claim-admission and keyed-finalize failures. The fixed error text is a
// test-injected internal fault — it must never cross the API boundary.
type keyedMatrixFaultRepo struct {
	*mysql.QueryExecutionRepository
	failClaimAdmission atomic.Bool
	failClaimFinalize  atomic.Bool
}

func (r *keyedMatrixFaultRepo) TryClaimExecution(ctx context.Context, in model.QueryExecutionClaimInput) error {
	if r.failClaimAdmission.Load() {
		return errors.New("keyed matrix fault: claim admission store")
	}
	return r.QueryExecutionRepository.TryClaimExecution(ctx, in)
}

func (r *keyedMatrixFaultRepo) FinalizeClaimWithAudit(ctx context.Context, rec model.QueryExecutionRecord, eventType, result string) (uint64, error) {
	if r.failClaimFinalize.Load() {
		return 0, errors.New("keyed matrix fault: claim finalize store")
	}
	return r.QueryExecutionRepository.FinalizeClaimWithAudit(ctx, rec, eventType, result)
}

// kmFixtureSeq disambiguates fixtures built inside one test/subtest.
var kmFixtureSeq atomic.Int64

// kmSanitize folds an arbitrary test name into an identifier-safe suffix:
// lowercase alphanumerics stay, everything else becomes one underscore.
func kmSanitize(name string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z'
		if ok {
			b.WriteRune(r)
			lastUnderscore = false
		} else if !lastUnderscore {
			b.WriteByte('_')
			lastUnderscore = true
		}
	}
	return strings.Trim(b.String(), "_")
}

// setupKeyedMatrixFixture provisions a ready mysql/<env> target plus the km_*
// aux schema and disclosure policies, then wires the real router over real
// services — the same production assembly as setupResultContractFixture with
// matrix-specific knobs.
func setupKeyedMatrixFixture(t *testing.T, opts keyedMatrixOpts) *keyedMatrixFixture {
	t.Helper()
	db := setupTestDB(t)

	// The suffix must survive two constraints at once: users.email and
	// resources.name uniqueness (a test may build several fixtures) and the
	// [A-Z0-9_]+ credential-ref rule (subtest names carry ';', '/', ' ').
	uniq := fmt.Sprintf("%d_%s", kmFixtureSeq.Add(1), kmSanitize(t.Name()))
	adminID := insertAuthzTestUser(t, db, "km-admin-"+uniq+"@example.com", "admin")
	otherID := insertAuthzTestUser(t, db, "km-other-"+uniq+"@example.com", "editor")
	t.Cleanup(func() {
		deleteAuthzTestUser(t, db, adminID)
		deleteAuthzTestUser(t, db, otherID)
	})

	seedKeyedMatrixTables(t, db)

	// Target resource + credential binding. The observed variant binds to the
	// observer's listen address; the direct variant binds to the container.
	upstream := keyedMatrixUpstream(t)
	var observer *mysqlKeyedWireObserver
	targetHost, targetPort, targetDSN := upstreamHost(t), upstreamPort(t), upstreamDSN()
	if opts.observedTable != "" {
		observer = newMySQLKeyedWireObserver(t, upstream, kmDatabase, opts.observedTable)
		if opts.observedMetaTable != "" {
			observer.WithMetadataTable(opts.observedMetaDatabase, opts.observedMetaTable)
		}
		host, portText, err := net.SplitHostPort(observer.Address())
		if err != nil {
			t.Fatalf("observer address: %v", err)
		}
		port, err := strconv.Atoi(portText)
		if err != nil {
			t.Fatalf("observer port: %v", err)
		}
		targetHost, targetPort = host, port
		targetDSN = fmt.Sprintf("root:test@tcp(%s)/controlhub_test?parseTime=true&charset=utf8mb4", observer.Address())
	}

	envID := opts.environmentID
	if envID == 0 {
		envID = envStaging
	}
	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(t.Context(), model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "km-target-" + uniq,
		DisplayName:     "Keyed Matrix Target",
		EnvironmentID:   envID,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Origin:          model.ResourceOriginManual,
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create matrix target resource: %v", err)
	}
	mustExec(t, db, `INSERT INTO resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) VALUES (?, 'mysql', '8.0', ?, ?, 'primary', '{}')`, res.ID, targetHost, targetPort)

	credentialRef := "KM_" + strings.ToUpper(uniq)
	if len(credentialRef) > 64 {
		credentialRef = credentialRef[:64]
	}
	// The prod fixture needs the all-environments policy: non-prod-only
	// credentials refuse prod targets before any claim work can run.
	envPolicy := model.QueryEnvPolicyNonProdOnly
	if envID == envProd {
		envPolicy = model.QueryEnvPolicyAllEnvironments
	}
	seedCredentialRow(t, db, res.ID, "mysql", credentialRef, !opts.credentialDisabled, string(envPolicy))
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+credentialRef, targetDSN)

	seedKeyedMatrixPolicies(t, db, res.ID, opts)

	// Real production assembly: real executor, real disclosure service/repo,
	// real evidence repository, real router — no executor fakes.
	queryTargetRepo := mysql.NewQueryTargetRepository(db)
	queryExecutionRepo := mysql.NewQueryExecutionRepository(db)
	savedStmtRepo := mysql.NewQuerySavedStatementRepository(db)
	var executions service.QueryExecutionRepository = queryExecutionRepo
	var faultRepo *keyedMatrixFaultRepo
	if opts.wrapRepo {
		faultRepo = &keyedMatrixFaultRepo{QueryExecutionRepository: queryExecutionRepo}
		executions = faultRepo
	}
	executionSvc := service.NewQueryExecutionService(
		queryTargetRepo,
		executions,
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		wallClock{},
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(
			mysql.NewQueryDisclosureRepository(db),
			mysql.NewQueryDisclosureRepository(db),
			service.NewMySQLSchemaInspector(),
			queryTargetRepo,
		),
	).WithTemplateExecution(savedStmtRepo, service.NewTemplateStatementCompiler())

	machineSvc := service.NewMachinePrincipalService(mysql.NewMachinePrincipalRepository(db))
	deps := api.Dependencies{
		AuthService:                service.NewAuthService(mysql.NewUserRepository(db), authzIntegrationSecret),
		AuditService:               service.NewAuditService(mysql.NewAuditRepository(db)),
		QueryExecutionService:      executionSvc,
		QuerySavedStatementService: service.NewQuerySavedStatementService(savedStmtRepo, savedStmtRepo, queryTargetRepo, service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500})),
		MachineCredentialService:   machineSvc,
		MachinePrincipalService:    machineSvc,
		QueryExecutionAuth:         api.QueryExecutionAuthConfig{Clock: time.Now},
	}
	if opts.statementAPI {
		deps.QueryExecutionStatementService = executionSvc
	}
	router := api.NewRouter(deps)

	fix := &keyedMatrixFixture{
		router:      router,
		targetID:    res.ID,
		db:          db,
		execRepo:    queryExecutionRepo,
		faultRepo:   faultRepo,
		observer:    observer,
		machineSvc:  machineSvc,
		adminUserID: adminID,
		otherUserID: otherID,
	}
	fix.token = mustLogin(t, router, "km-admin-"+uniq+"@example.com", "secret123")
	fix.otherToken = mustLogin(t, router, "km-other-"+uniq+"@example.com", "secret123")
	fix.executePath = fmt.Sprintf("/query-targets/%d/execute", res.ID)
	fix.savedPath = fmt.Sprintf("/query-targets/%d/saved-statements", res.ID)
	fix.navigationPath = fmt.Sprintf("/query-targets/%d/related-records", res.ID)
	fix.historyPath = fmt.Sprintf("/query-targets/%d/executions", res.ID)
	return fix
}

// keyedMatrixUpstream returns the disposable container's host:port for the
// observer's backend dial.
func keyedMatrixUpstream(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("%s:%d", upstreamHost(t), upstreamPort(t))
}

var (
	upstreamHostOnce int
	upstreamHostVal  string
	upstreamPortVal  int
)

func upstreamDSN() string { return globalEnv.dsn }

func upstreamHost(t *testing.T) string {
	t.Helper()
	parseUpstream(t)
	return upstreamHostVal
}

func upstreamPort(t *testing.T) int {
	t.Helper()
	parseUpstream(t)
	return upstreamPortVal
}

func parseUpstream(t *testing.T) {
	t.Helper()
	if upstreamHostOnce != 0 {
		return
	}
	host, port, err := service.ParseMySQLDSNHostPort(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse upstream dsn host/port: %v", err)
	}
	upstreamHostVal, upstreamPortVal, upstreamHostOnce = host, port, 1
}

// seedKeyedMatrixTables creates the km_* aux schema once per container and
// reseeds rows per fixture. DELETE is deliberate: a timed-out matrix query
// can keep running server-side after the client disconnect (a CPU-bound join
// never notices a dead socket), and only row-level writes stay unblockable
// by that straggler's metadata lock — a DROP would wait on it.
var kmTablesOnce struct {
	done bool
	mu   sync.Mutex
}

func seedKeyedMatrixTables(t *testing.T, db *sql.DB) {
	t.Helper()
	kmTablesOnce.mu.Lock()
	if !kmTablesOnce.done {
		mustExec(t, db, "CREATE DATABASE IF NOT EXISTS query_e2e_aux")
		mustExec(t, db, "CREATE DATABASE IF NOT EXISTS query_e2e_aux2")
		mustExec(t, db, `CREATE TABLE IF NOT EXISTS query_e2e_aux.km_cells (
			id BIGINT NOT NULL,
			pad MEDIUMTEXT CHARACTER SET utf8mb4,
			secret MEDIUMTEXT CHARACTER SET utf8mb4,
			note VARCHAR(64),
			free VARCHAR(16),
			PRIMARY KEY (id)
		) ENGINE=InnoDB`)
		mustExec(t, db, `CREATE TABLE IF NOT EXISTS query_e2e_aux.km_parent (
			id BIGINT NOT NULL,
			parent_code VARCHAR(32) NOT NULL,
			pad MEDIUMTEXT CHARACTER SET utf8mb4,
			PRIMARY KEY (id),
			UNIQUE KEY uq_km_parent_code (parent_code)
		) ENGINE=InnoDB`)
		mustExec(t, db, `CREATE TABLE IF NOT EXISTS query_e2e_aux.km_child (
			id BIGINT NOT NULL,
			parent_id BIGINT NOT NULL,
			child_name VARCHAR(32) NOT NULL,
			PRIMARY KEY (id),
			CONSTRAINT fk_km_child_parent FOREIGN KEY (parent_id) REFERENCES query_e2e_aux.km_parent (id)
		) ENGINE=InnoDB`)
		mustExec(t, db, `CREATE TABLE IF NOT EXISTS query_e2e_aux.km_child2 (
			id BIGINT NOT NULL,
			parent_id BIGINT NOT NULL,
			child_name VARCHAR(32) NOT NULL,
			PRIMARY KEY (id),
			CONSTRAINT fk_km_child2_parent FOREIGN KEY (parent_id) REFERENCES query_e2e_aux.km_parent (id)
		) ENGINE=InnoDB`)
		mustExec(t, db, `CREATE TABLE IF NOT EXISTS query_e2e_aux.km_paged (
			id BIGINT NOT NULL,
			pad MEDIUMTEXT CHARACTER SET utf8mb4,
			PRIMARY KEY (id)
		) ENGINE=InnoDB`)
		mustExec(t, db, `CREATE TABLE IF NOT EXISTS query_e2e_aux.km_bulk (
			id BIGINT NOT NULL,
			PRIMARY KEY (id)
		) ENGINE=InnoDB`)
		// The aux2 parent deliberately carries an ungoverned `extra` column:
		// SELECT * navigation to it is blocked by the disclosure engine while
		// the same aux2 source tables still serve digest variants.
		mustExec(t, db, `CREATE TABLE IF NOT EXISTS query_e2e_aux2.km_parent (
			id BIGINT NOT NULL,
			parent_code VARCHAR(32) NOT NULL,
			extra VARCHAR(16),
			PRIMARY KEY (id),
			UNIQUE KEY uq_km2_parent_code (parent_code)
		) ENGINE=InnoDB`)
		mustExec(t, db, `CREATE TABLE IF NOT EXISTS query_e2e_aux2.km_child (
			id BIGINT NOT NULL,
			parent_id BIGINT NOT NULL,
			child_name VARCHAR(32) NOT NULL,
			PRIMARY KEY (id),
			CONSTRAINT fk_km2_child_parent FOREIGN KEY (parent_id) REFERENCES query_e2e_aux2.km_parent (id)
		) ENGINE=InnoDB`)
		kmTablesOnce.done = true
	}
	kmTablesOnce.mu.Unlock()

	// Reseed rows per fixture — child rows first so the FK deletes never
	// violate referential integrity.
	for _, stmt := range []string{
		"DELETE FROM query_e2e_aux.km_child2",
		"DELETE FROM query_e2e_aux.km_child",
		"DELETE FROM query_e2e_aux2.km_child",
		"DELETE FROM query_e2e_aux.km_cells",
		"DELETE FROM query_e2e_aux.km_paged",
		"DELETE FROM query_e2e_aux.km_bulk",
		"DELETE FROM query_e2e_aux.km_parent",
		"DELETE FROM query_e2e_aux2.km_parent",
	} {
		mustExec(t, db, stmt)
	}

	insertCell := func(id int, pad, secret, note string) {
		mustExec(t, db, `INSERT INTO query_e2e_aux.km_cells (id, pad, secret, note, free) VALUES (?,?,?,?,'free')`, id, pad, secret, note)
	}
	insertCell(1, strings.Repeat("a", 8192), "s", "edge-8192")
	insertCell(2, strings.Repeat("b", 8193), strings.Repeat("s", 8193), "over-8193")
	insertCell(3, strings.Repeat("中", 2731), "s", "cjk-cross")
	insertCell(4, strings.Repeat("😀", 2049), "s", "emoji-cross")
	insertCell(5, strings.Repeat("中", 2730), "s", "cjk-8190")
	mustExec(t, db, `INSERT INTO query_e2e_aux.km_cells (id, pad, secret, note, free) VALUES (6, '', 's', 'empty-str', 'free')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux.km_cells (id, pad, secret, note, free) VALUES (7, NULL, 's', 'sql-null', 'free')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux.km_cells (id, pad, secret, note, free) VALUES (8, 'NULL', 's', 'text-null', 'free')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux.km_cells (id, pad, secret, note, free) VALUES (9, 'short-pad', 's', 'clean-row', 'free')`)

	mustExec(t, db, `INSERT INTO query_e2e_aux.km_parent (id, parent_code, pad) VALUES (1, 'P_ALPHA', ?)`, strings.Repeat("w", 9000))
	mustExec(t, db, `INSERT INTO query_e2e_aux.km_parent (id, parent_code, pad) VALUES (2, 'P_BETA', 'short')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux.km_child (id, parent_id, child_name) VALUES (1, 1, 'c1'), (2, 2, 'c2')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux.km_child2 (id, parent_id, child_name) VALUES (1, 1, 'c1')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux2.km_parent (id, parent_code, extra) VALUES (1, 'Q_ONE', 'x')`)
	mustExec(t, db, `INSERT INTO query_e2e_aux2.km_child (id, parent_id, child_name) VALUES (1, 1, 'q1')`)

	for i := 1; i <= 10; i++ {
		mustExec(t, db, `INSERT INTO query_e2e_aux.km_paged (id, pad) VALUES (?, 'v')`, i)
	}
	mustExec(t, db, `INSERT INTO query_e2e_aux.km_paged (id, pad) VALUES (11, ?)`, strings.Repeat("z", 9000))

	// 400 bulk rows make the four-way self product ~2.6e10 row evaluations —
	// far beyond the frozen 5s attempt budget — without any forbidden
	// construct. Values stay small so the table itself is cheap.
	var bulk strings.Builder
	bulk.WriteString("INSERT INTO query_e2e_aux.km_bulk (id) VALUES ")
	for i := 1; i <= 400; i++ {
		if i > 1 {
			bulk.WriteByte(',')
		}
		bulk.WriteString(fmt.Sprintf("(%d)", i))
	}
	mustExec(t, db, bulk.String())
}

// kmKillStragglers KILLs any server-side query still chewing on km_bulk rows
// after a timeout case. A CPU-bound join does not notice a dead client socket
// until it tries to write, so the timed-out attempt may keep churning in the
// background; explicit cleanup frees its locks and container CPU before the
// next fixture reseeds.
func kmKillStragglers(t *testing.T, db *sql.DB) {
	t.Helper()
	rows, err := db.Query(`SELECT ID FROM information_schema.PROCESSLIST
		WHERE COMMAND = 'Query' AND INFO LIKE '%km_bulk%' AND ID <> CONNECTION_ID()`)
	if err != nil {
		t.Fatalf("straggler processlist read: %v", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("straggler processlist scan: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("straggler processlist: %v", err)
	}
	for _, id := range ids {
		if _, err := db.Exec(fmt.Sprintf("KILL QUERY %d", id)); err != nil {
			t.Fatalf("kill straggler %d: %v", id, err)
		}
	}
}

// seedKeyedMatrixPolicies installs the exact disclosure policies the fixture
// statements need: every projected column is governed. km_cells.free and the
// km_bulk columns deliberately have none — they power the blocked cases.
func seedKeyedMatrixPolicies(t *testing.T, db *sql.DB, targetID uint64, opts keyedMatrixOpts) {
	t.Helper()
	seedDisclosurePolicies(t, db, targetID, kmDatabase, kmCells, "id", "pad", "note")
	seedDisclosurePolicies(t, db, targetID, kmDatabase, kmParent, "id", "parent_code", "pad")
	seedDisclosurePolicies(t, db, targetID, kmDatabase, kmPaged, "id", "pad")
	if opts.maskedSecret {
		if _, err := mysql.NewQueryDisclosureRepository(db).Insert(t.Context(), model.ResultDisclosurePolicyUpsertRequest{
			TargetResourceID: targetID,
			DatabaseName:     kmDatabase,
			ObjectName:       kmCells,
			ColumnName:       "secret",
			Mode:             model.ResultDisclosureMaskedNoCopy,
		}); err != nil {
			t.Fatalf("seed masked disclosure policy: %v", err)
		}
	}
	if opts.auxTwo {
		seedDisclosurePolicies(t, db, targetID, "query_e2e_aux2", kmParent, "id", "parent_code")
	}
}

// kmPost issues one authenticated JSON POST against the fixture router.
func (f *keyedMatrixFixture) kmPost(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	return doBearerWithBody(t, f.router, http.MethodPost, path, f.token, body)
}

// kmPostAs issues one authenticated JSON POST with an explicit bearer token.
func (f *keyedMatrixFixture) kmPostAs(t *testing.T, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	return doBearerWithBody(t, f.router, http.MethodPost, path, token, body)
}

// kmGet issues one authenticated GET against the fixture router.
func (f *keyedMatrixFixture) kmGet(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return doBearer(t, f.router, http.MethodGet, path, f.token)
}

// kmCounts runs the canonical four-scope target count (verbatim
// evidence-queries.sql recipe).
func (f *keyedMatrixFixture) kmCounts(t *testing.T) mysqlKeyedScopeCounts {
	t.Helper()
	return mysqlTargetCounts(t, f.db, f.targetID)
}

// kmExpectCounts asserts the exact four-scope target counts.
func (f *keyedMatrixFixture) kmExpectCounts(t *testing.T, want mysqlKeyedScopeCounts) {
	t.Helper()
	if got := f.kmCounts(t); got != want {
		t.Fatalf("target counts = %+v, want %+v", got, want)
	}
}

// kmClaimRow reads the claim row for a key (request digest, link, budget,
// typed actors) directly from the claim store.
func (f *keyedMatrixFixture) kmClaimRow(t *testing.T, key string) (digest string, execID sql.NullInt64, timeoutMs sql.NullInt64, actorUser sql.NullInt64, actorMachine sql.NullInt64) {
	t.Helper()
	if err := f.db.QueryRow(`SELECT request_digest, execution_id, attempt_timeout_ms, actor_user_id, actor_machine_principal_id
		FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = ?`,
		f.targetID, key).Scan(&digest, &execID, &timeoutMs, &actorUser, &actorMachine); err != nil {
		t.Fatalf("claim row read for key %q: %v", key, err)
	}
	return digest, execID, timeoutMs, actorUser, actorMachine
}

// kmCreateSaved provisions one saved statement for the fixture admin through
// the real create route and returns its id.
func (f *keyedMatrixFixture) kmCreateSaved(t *testing.T, body string) uint64 {
	t.Helper()
	rec := f.kmPost(t, f.savedPath, body)
	if rec.Code != http.StatusCreated && rec.Code != http.StatusOK {
		t.Fatalf("create saved statement status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || created.ID == 0 {
		t.Fatalf("decode created statement: %v — %s", err, rec.Body.String())
	}
	return created.ID
}

// kmSavedExecPath returns the saved-statement execute route for a statement.
func (f *keyedMatrixFixture) kmSavedExecPath(statementID uint64) string {
	return fmt.Sprintf("%s/%d/execute", f.savedPath, statementID)
}

// kmNavBody builds a governed related-records body against the km FK pair;
// localValue "1" resolves to the oversized-pad parent, "2" to the clean one.
func kmNavBody(localValue, clientKey string) string {
	key := ""
	if clientKey != "" {
		key = fmt.Sprintf(`,"clientExecutionId":%q`, clientKey)
	}
	return fmt.Sprintf(`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":[%q]%s}`, localValue, key)
}

// kmErrorCode decodes the controlled error envelope's error code.
func kmErrorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	code, _ := decodeError(t, rec)
	return code
}

// kmExecSuccess decodes the minimal success envelope fields the matrix
// inspects; rows and matrix are optional in the caller's interest.
type kmExecResponse struct {
	ExecutionID   uint64           `json:"executionId"`
	Status        string           `json:"status"`
	RowCount      int              `json:"rowCount"`
	LimitApplied  int              `json:"limitApplied"`
	Rows          [][]any          `json:"rows"`
	CellTruncated [][]bool         `json:"cellTruncated"`
	Pagination    *json.RawMessage `json:"pagination"`
}

func kmDecodeSuccess(t *testing.T, rec *httptest.ResponseRecorder) kmExecResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp kmExecResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode success body: %v — %s", err, rec.Body.String())
	}
	return resp
}

// kmFaultTrigger installs a scoped BEFORE-trigger that aborts the write only
// for this fixture's target (claim link additionally requires an execution
// link attempt). Trigger names are unique per test so cleanup is safe.
func kmFaultTrigger(t *testing.T, db *sql.DB, name, kind string, targetID uint64) {
	t.Helper()
	var ddl string
	msg := fmt.Sprintf("keyed matrix fault: %s", kind)
	switch kind {
	case "exec_insert":
		ddl = fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON query_executions FOR EACH ROW BEGIN
			IF NEW.target_resource_id = %d THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '%s'; END IF;
		END`, name, targetID, msg)
	case "audit_insert":
		ddl = fmt.Sprintf(`CREATE TRIGGER %s BEFORE INSERT ON audit_events FOR EACH ROW BEGIN
			IF NEW.target_resource_id = %d THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '%s'; END IF;
		END`, name, targetID, msg)
	case "claim_link":
		ddl = fmt.Sprintf(`CREATE TRIGGER %s BEFORE UPDATE ON query_execution_claims FOR EACH ROW BEGIN
			IF NEW.target_resource_id = %d AND NEW.execution_id IS NOT NULL THEN SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = '%s'; END IF;
		END`, name, targetID, msg)
	default:
		t.Fatalf("unknown fault trigger kind %q", kind)
	}
	mustExec(t, db, "DROP TRIGGER IF EXISTS "+name)
	if _, err := db.Exec(ddl); err != nil {
		t.Fatalf("create fault trigger %s: %v", name, err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP TRIGGER IF EXISTS " + name); err != nil {
			t.Fatalf("drop fault trigger %s: %v", name, err)
		}
	})
}

// kmAwaitWire waits until the observer's selected result count reaches n or
// the deadline passes; used where the wire arrival ordering matters.
func kmAwaitWire(t *testing.T, obs *mysqlKeyedWireObserver, results uint64, timeout time.Duration) mysqlKeyedWireCounts {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c := obs.Counts()
		if c.Results() >= results {
			return c
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("wire results did not reach %d in %s: %+v", results, timeout, obs.Counts())
	return mysqlKeyedWireCounts{}
}

// kmAwaitExecRows polls until the target's query_executions count reaches n.
func kmAwaitExecRows(t *testing.T, db *sql.DB, targetID uint64, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var got int
		if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID).Scan(&got); err == nil && got >= n {
			return
		}
		time.Sleep(15 * time.Millisecond)
	}
	t.Fatalf("query_executions for target %d did not reach %d in %s", targetID, n, timeout)
}

// kmHistoryItems decodes the executions list envelope for one GET.
func kmHistoryItems(t *testing.T, rec *httptest.ResponseRecorder) (items []map[string]any, nextCursor string) {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("history list status = %d; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Items      []map[string]any `json:"items"`
		NextCursor *string          `json:"nextCursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode history body: %v — %s", err, rec.Body.String())
	}
	if body.NextCursor != nil {
		nextCursor = *body.NextCursor
	}
	return body.Items, nextCursor
}

// kmSeedClaim inserts one claim row directly through the real repository —
// the fixture for unlinked-claim read cases. ClaimedAt is caller-controlled
// so the derived running/unknown boundary is exercised without sleeping.
func (f *keyedMatrixFixture) kmSeedClaim(t *testing.T, key, digest string, claimedAt time.Time, timeoutMs int64) {
	t.Helper()
	if timeoutMs > 0 {
		err := f.execRepo.TryClaimExecution(t.Context(), model.QueryExecutionClaimInput{
			TargetResourceID:  f.targetID,
			ClientExecutionID: key,
			Identity:          model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: f.adminUserID},
			DatabaseName:      "",
			SchemaName:        "",
			RequestDigest:     digest,
			AttemptTimeoutMs:  timeoutMs,
			ClaimedAt:         claimedAt,
		})
		if err != nil {
			t.Fatalf("seed claim %q: %v", key, err)
		}
		return
	}
	// NULL budget (pre-T10-C shape): raw INSERT keeps the column NULL.
	if _, err := f.db.Exec(`INSERT INTO query_execution_claims
		(target_resource_id, client_execution_id, actor_user_id, actor_machine_principal_id, database_name, schema_name, request_digest, attempt_timeout_ms, claimed_at)
		VALUES (?, ?, ?, NULL, '', '', ?, NULL, ?)`,
		f.targetID, key, f.adminUserID, digest, claimedAt.UTC()); err != nil {
		t.Fatalf("seed NULL-budget claim %q: %v", key, err)
	}
}

// kmDigestGolden mirrors one frozen vector from mysql-digest-goldens.json.
type kmDigestGolden struct {
	Name           string
	Domain         string
	Statement      string
	StatementID    uint64
	Values         []kmDigestGoldenValue
	SourceDatabase string
	SourceObject   string
	SourceColumns  []string
	ForeignKey     string
	LocalValues    []string
	Page           int
	PageSize       int
	MaxRows        int
	WireHex        string
	SHA256         string
}

// ---------------------------------------------------------------------------
// Group A — keyless compatibility: every legacy attempt produces exactly one
// NULL-key history/audit pair and never touches the claim store. Keyed
// controls alongside prove the claim column only moves for keyed requests.
// ---------------------------------------------------------------------------

// kmSlowSubquery is the deterministic over-budget statement used for timeout
// cases: a WHERE scalar subquery summing a row expression over a 4-way self
// product of km_bulk (~2.6e10 row evaluations). Unlike COUNT(*) — which MySQL
// resolves from per-table counts — an expression aggregate forces the full
// join iteration with no temp table, so the subquery performs real work and
// dies at the frozen 5-second attempt deadline. No SLEEP/BENCHMARK, so it
// passes the guard; disclosure sees only the governed km_cells.id projection.
const kmSlowSubquery = `select id from query_e2e_aux.km_cells where id <= (
	select sum(a.id + b.id + c.id + d.id)
	from query_e2e_aux.km_bulk a, query_e2e_aux.km_bulk b, query_e2e_aux.km_bulk c, query_e2e_aux.km_bulk d)`

func TestMySQLKeyedMatrixA_KeylessOrdinary(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{auxTwo: true})
	post := func(body string) *httptest.ResponseRecorder {
		return fx.kmPost(t, fx.executePath, body)
	}

	// Clean 200s: absent key and explicit empty key are both the legacy path.
	kmDecodeSuccess(t, post(`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9"}`))
	kmDecodeSuccess(t, post(`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":""}`))
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 2, Audits: 2})

	// Paging: page 2 of 10 over 11 rows returns the single tail row id 11.
	resp := kmDecodeSuccess(t, post(`{"statement":"select id from query_e2e_aux.km_paged order by id","pagination":{"page":2,"pageSize":10}}`))
	if len(resp.Rows) != 1 || resp.Rows[0][0] != float64(11) {
		t.Fatalf("keyless paged rows = %v, want tail row id 11", resp.Rows)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3})

	// Guard rejection: a non-SELECT is refused and still records one pair.
	rec := post(`{"statement":"delete from query_e2e_aux.km_cells"}`)
	if rec.Code != http.StatusBadRequest || kmErrorCode(t, rec) != "validation_failed" {
		t.Fatalf("keyless guard rejection = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 4, Audits: 4})

	// Disclosure rejection: a column with no policy is blocked pre-SQL.
	rec = post(`{"statement":"select free from query_e2e_aux.km_cells where id = 1"}`)
	if rec.Code != http.StatusForbidden || kmErrorCode(t, rec) != "query_result_disclosure_blocked" {
		t.Fatalf("keyless disclosure rejection = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 5, Audits: 5})

	// Timeout: the bulk-product subquery outlives the frozen 5s budget and is
	// recorded as one terminal timeout pair. The server-side straggler is
	// killed explicitly — a CPU-bound join never notices the dead socket.
	rec = post(fmt.Sprintf(`{"statement":%q}`, kmSlowSubquery))
	if rec.Code != http.StatusRequestTimeout || kmErrorCode(t, rec) != "query_timeout" {
		t.Fatalf("keyless timeout = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	kmKillStragglers(t, fx.db)
	var lastStatus, lastCode string
	if err := fx.db.QueryRow(`SELECT status, error_code FROM query_executions WHERE target_resource_id = ? ORDER BY id DESC LIMIT 1`, fx.targetID).Scan(&lastStatus, &lastCode); err != nil {
		t.Fatalf("last row read: %v", err)
	}
	if lastStatus != string(model.QueryExecutionTimeout) || lastCode != "query_timeout" {
		t.Fatalf("timeout evidence = %s/%s", lastStatus, lastCode)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 6, Audits: 6})

	// Truncation without capability: 409 refusal but the success pair commits.
	rec = post(`{"statement":"select id, pad from query_e2e_aux.km_cells where id = 2"}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "result_contract_upgrade_required" {
		t.Fatalf("keyless truncation refusal = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 7, Audits: 7})
	if lastStatus := kmLastStatus(t, fx); lastStatus != string(model.QueryExecutionSuccess) {
		t.Fatalf("refusal attempt status = %s, want success", lastStatus)
	}

	// Evidence fault: a scoped insert trigger turns the attempt into a 502
	// with zero partial evidence, and dropping the fault recovers cleanly.
	kmFaultTrigger(t, fx.db, "km_a_execfail", "exec_insert", fx.targetID)
	rec = post(`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9"}`)
	if rec.Code != http.StatusBadGateway || kmErrorCode(t, rec) != "query_backend_error" {
		t.Fatalf("keyless evidence fault = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 7, Audits: 7})
	mustExec(t, fx.db, "DROP TRIGGER IF EXISTS km_a_execfail")
	kmDecodeSuccess(t, post(`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9"}`))
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 8, Audits: 8})

	// Every keyless attempt stored a NULL key.
	if n := countWhere(t, fx.db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NOT NULL`, fx.targetID); n != 0 {
		t.Fatalf("keyless attempts wrote %d keyed rows", n)
	}
}

// kmLastStatus returns the latest execution status for the fixture target.
func kmLastStatus(t *testing.T, fx *keyedMatrixFixture) string {
	t.Helper()
	var status string
	if err := fx.db.QueryRow(`SELECT status FROM query_executions WHERE target_resource_id = ? ORDER BY id DESC LIMIT 1`, fx.targetID).Scan(&status); err != nil {
		t.Fatalf("last status read: %v", err)
	}
	return status
}

func TestMySQLKeyedMatrixA_KeylessSaved(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})

	stmtID := fx.kmCreateSaved(t,
		`{"name":"km-cells-a","statement":"select id, note from query_e2e_aux.km_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
	pagedID := fx.kmCreateSaved(t,
		`{"name":"km-paged-a","statement":"select id from query_e2e_aux.km_paged order by id","scope":"personal"}`)
	blockedID := fx.kmCreateSaved(t,
		`{"name":"km-blocked-a","statement":"select free from query_e2e_aux.km_cells where id = 1","scope":"personal"}`)
	slowID := fx.kmCreateSaved(t,
		`{"name":"km-slow-a","statement":`+strconv.Quote(kmSlowSubquery)+`,"scope":"personal"}`)
	post := func(id uint64, body string) *httptest.ResponseRecorder {
		return fx.kmPost(t, fx.kmSavedExecPath(id), body)
	}

	// Clean 200s: absent and empty keys both run the legacy pair path.
	kmDecodeSuccess(t, post(stmtID, `{"values":{"cell_id":9}}`))
	kmDecodeSuccess(t, post(stmtID, `{"values":{"cell_id":9},"clientExecutionId":""}`))
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 2, Audits: 2})

	// Paging through a saved statement works identically to ordinary.
	resp := kmDecodeSuccess(t, post(pagedID, `{"pagination":{"page":2,"pageSize":10}}`))
	if len(resp.Rows) != 1 || resp.Rows[0][0] != float64(11) {
		t.Fatalf("keyless saved paged rows = %v, want tail row id 11", resp.Rows)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3})

	// Disclosure rejection: a statement projecting a policy-free column is
	// blocked at execute time (create does not run disclosure).
	rec := post(blockedID, `{}`)
	if rec.Code != http.StatusForbidden || kmErrorCode(t, rec) != "query_result_disclosure_blocked" {
		t.Fatalf("keyless saved disclosure rejection = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 4, Audits: 4})

	// Timeout: the same real server-side deadline, through the saved entry.
	rec = post(slowID, `{}`)
	if rec.Code != http.StatusRequestTimeout || kmErrorCode(t, rec) != "query_timeout" {
		t.Fatalf("keyless saved timeout = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	kmKillStragglers(t, fx.db)
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 5, Audits: 5})
	if got := kmLastStatus(t, fx); got != string(model.QueryExecutionTimeout) {
		t.Fatalf("saved timeout evidence = %s", got)
	}

	// Evidence fault: 502 with zero writes, then clean recovery.
	kmFaultTrigger(t, fx.db, "km_a_saved_execfail", "exec_insert", fx.targetID)
	rec = post(stmtID, `{"values":{"cell_id":9}}`)
	if rec.Code != http.StatusBadGateway || kmErrorCode(t, rec) != "query_backend_error" {
		t.Fatalf("keyless saved evidence fault = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 5, Audits: 5})
	mustExec(t, fx.db, "DROP TRIGGER IF EXISTS km_a_saved_execfail")
	kmDecodeSuccess(t, post(stmtID, `{"values":{"cell_id":9}}`))
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 6, Audits: 6})
}

func TestMySQLKeyedMatrixA_KeylessNavigation(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{auxTwo: true, observedTable: kmParent})
	post := func(body string) *httptest.ResponseRecorder {
		return fx.kmPost(t, fx.navigationPath, body)
	}

	// Clean 200s: absent and empty keys are both the legacy path.
	resp := kmDecodeSuccess(t, post(`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["2"]}`))
	if len(resp.Rows) != 1 || resp.Rows[0][1] != "P_BETA" {
		t.Fatalf("keyless nav rows = %v", resp.Rows)
	}
	if resp.LimitApplied != model.MaxRelatedRowsDefault {
		t.Fatalf("keyless nav limitApplied = %d, want %d", resp.LimitApplied, model.MaxRelatedRowsDefault)
	}
	resp = kmDecodeSuccess(t, post(`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["2"],"clientExecutionId":""}`))
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 2, Audits: 2})

	// Native LIMIT clamping: maxRows is server-clamped into the generated SQL.
	resp = kmDecodeSuccess(t, post(`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["2"],"maxRows":1}`))
	if resp.LimitApplied != 1 {
		t.Fatalf("keyless nav clamped limit = %d, want 1", resp.LimitApplied)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3})

	// Rejection: an unknown FK is a pre-claim rejected pair.
	rec := post(`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_missing"},"localValues":["2"]}`)
	if rec.Code != http.StatusBadRequest || kmErrorCode(t, rec) != "validation_failed" {
		t.Fatalf("keyless nav FK rejection = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 4, Audits: 4})

	// Disclosure rejection: the aux2 referenced table projects an ungoverned
	// column — SELECT * is blocked post-claim by the policy engine.
	rec = post(`{"source":{"database":"query_e2e_aux2","object":"km_child","kind":"table","foreignKey":"fk_km2_child_parent"},"localValues":["1"]}`)
	if rec.Code != http.StatusForbidden || kmErrorCode(t, rec) != "query_result_disclosure_blocked" {
		t.Fatalf("keyless nav disclosure rejection = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 5, Audits: 5})

	// Cancellation: a real HTTP abort mid-reply produces one keyless
	// failed/query_canceled pair (the keyed variant is group E's proof).
	arrived, release := fx.observer.HoldNextResultReply(t)
	defer release()
	srv := httptest.NewServer(fx.router)
	defer srv.Close()
	reqCtx, cancelReq := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost,
		srv.URL+fx.navigationPath, strings.NewReader(
			`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["2"],"clientExecutionId":""}`))
	if err != nil {
		t.Fatalf("build nav request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+fx.token)
	req.Header.Set("Content-Type", "application/json")
	postDone := make(chan error, 1)
	go func() {
		resp, err := srv.Client().Do(req)
		if err == nil {
			resp.Body.Close()
		}
		postDone <- err
	}()
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("nav result reply never reached the wire barrier")
	}
	cancelReq()
	select {
	case err := <-postDone:
		if err == nil {
			t.Fatal("canceled keyless nav POST unexpectedly completed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("keyless nav POST did not observe the cancellation")
	}
	kmAwaitExecRows(t, fx.db, fx.targetID, 6, 10*time.Second)
	if got := kmLastStatus(t, fx); got != string(model.QueryExecutionFailed) {
		t.Fatalf("aborted nav status = %s, want failed", got)
	}
	var code string
	if err := fx.db.QueryRow(`SELECT error_code FROM query_executions WHERE target_resource_id = ? ORDER BY id DESC LIMIT 1`, fx.targetID).Scan(&code); err != nil {
		t.Fatalf("aborted nav code read: %v", err)
	}
	if code != "query_canceled" {
		t.Fatalf("aborted nav error_code = %q, want query_canceled", code)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 6, Audits: 6})

	// Truncation without capability: nav oversized pad → 409, pair commits.
	rec = post(`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["1"]}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "result_contract_upgrade_required" {
		t.Fatalf("keyless nav truncation refusal = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 7, Audits: 7})
}

// ---------------------------------------------------------------------------
// Group C — wire-command vs persistence separation: exactly one selected
// result command per post-target attempt, prepares tracked separately, and
// schema-metadata traffic (preflight/inspector) counted under the metadata
// selector — never as a result.
// ---------------------------------------------------------------------------

func TestMySQLKeyedMatrixC_WireCountsOrdinary(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{
		observedTable:        kmCells,
		observedMetaDatabase: "information_schema",
		observedMetaTable:    "COLUMNS",
	})
	key := "km-c-ord-1"
	body := `{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"` + key + `"}`

	base := fx.observer.Counts()
	baseMeta := fx.observer.MetaCounts()

	// Keyed execute: exactly one text-protocol result command and one reply.
	kmDecodeSuccess(t, fx.kmPost(t, fx.executePath, body))
	c := fx.observer.Counts()
	if got := c.Results() - base.Results(); got != 1 {
		t.Fatalf("keyed execute result commands = %d, want 1 (%+v)", got, c)
	}
	if got := c.Queries - base.Queries; got != 1 {
		t.Fatalf("keyed execute text queries = %d, want 1 (ordinary uses the text protocol)", got)
	}
	if got := c.Prepares - base.Prepares + c.Executes - base.Executes; got != 0 {
		t.Fatalf("keyed execute prepared-protocol delta = %d, want 0", got)
	}
	if got := c.Replies - base.Replies; got != 1 || c.ReplyErrors != 0 {
		t.Fatalf("keyed execute replies = %d errors = %d, want 1/0", got, c.ReplyErrors)
	}
	// Disclosure preflight read the table metadata over the same wire — a
	// prepared information_schema.COLUMNS execute, never a result command.
	m := fx.observer.MetaCounts()
	if m.Prepares-baseMeta.Prepares < 1 || m.Executes-baseMeta.Executes < 1 {
		t.Fatalf("preflight metadata traffic missing: %+v -> %+v", baseMeta, m)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})

	// Replay and conflict write no evidence and run no result command.
	rec := fx.kmPost(t, fx.executePath, body)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
		t.Fatalf("replay = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	rec = fx.kmPost(t, fx.executePath, `{"statement":"select id, note from query_e2e_aux.km_cells where id = 1","clientExecutionId":"`+key+`"}`)
	if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
		t.Fatalf("conflict = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	if got := fx.observer.Counts().Results() - base.Results(); got != 1 {
		t.Fatalf("replay/conflict produced %d result commands, want 0 extra", got-1)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})

	// A new key runs exactly one more result command; a keyless request one more.
	kmDecodeSuccess(t, fx.kmPost(t, fx.executePath, `{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"km-c-ord-2"}`))
	kmDecodeSuccess(t, fx.kmPost(t, fx.executePath, `{"statement":"select id, note from query_e2e_aux.km_cells where id = 9"}`))
	final := fx.observer.Counts()
	if got := final.Results() - base.Results(); got != 3 {
		t.Fatalf("total result commands = %d, want 3", got)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 2, Linked: 2})
}

func TestMySQLKeyedMatrixC_WireCountsSaved(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{
		observedTable:        kmCells,
		observedMetaDatabase: "information_schema",
		observedMetaTable:    "COLUMNS",
	})
	stmtID := fx.kmCreateSaved(t,
		`{"name":"km-c-saved","statement":"select id, note from query_e2e_aux.km_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
	path := fx.kmSavedExecPath(stmtID)
	key := "km-c-sav-1"
	body := `{"values":{"cell_id":9},"clientExecutionId":"` + key + `"}`

	base := fx.observer.Counts()
	baseMeta := fx.observer.MetaCounts()

	// The compiled template runs through the prepared protocol: one Prepare
	// plus one Execute of the same selected statement — Results counts the
	// execution frame only.
	kmDecodeSuccess(t, fx.kmPost(t, path, body))
	c := fx.observer.Counts()
	if got := c.Queries - base.Queries; got != 0 {
		t.Fatalf("saved execute text queries = %d, want 0 (templates use the binary protocol)", got)
	}
	if got := c.Prepares - base.Prepares; got != 1 {
		t.Fatalf("saved execute prepares = %d, want 1", got)
	}
	if got := c.Executes - base.Executes; got != 1 {
		t.Fatalf("saved execute executes = %d, want 1", got)
	}
	if got := c.Results() - base.Results(); got != 1 {
		t.Fatalf("saved execute result commands = %d, want 1", got)
	}
	if got := c.Replies - base.Replies; got != 1 || c.ReplyErrors != 0 {
		t.Fatalf("saved execute replies = %d errors = %d", got, c.ReplyErrors)
	}
	m := fx.observer.MetaCounts()
	if m.Executes-baseMeta.Executes < 1 {
		t.Fatalf("saved preflight metadata missing: %+v -> %+v", baseMeta, m)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})

	// Same-key replays and conflicting values run no second command.
	if rec := fx.kmPost(t, path, body); rec.Code != http.StatusConflict {
		t.Fatalf("saved replay = %d, want 409", rec.Code)
	}
	if rec := fx.kmPost(t, path, `{"values":{"cell_id":1},"clientExecutionId":"`+key+`"}`); rec.Code != http.StatusConflict {
		t.Fatalf("saved conflict = %d, want 409", rec.Code)
	}
	if got := fx.observer.Counts().Results() - base.Results(); got != 1 {
		t.Fatalf("saved replay/conflict produced %d result commands", got-1)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
}

func TestMySQLKeyedMatrixC_WireCountsNavigation(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{
		observedTable:        kmParent,
		observedMetaDatabase: "information_schema",
		observedMetaTable:    "COLUMNS",
	})
	key := "km-c-nav-1"
	body := kmNavBody("2", key)

	base := fx.observer.Counts()
	baseMeta := fx.observer.MetaCounts()

	// The generated navigation SELECT is parameterized (local value bound),
	// so it runs the prepared protocol: one Prepare + one Execute.
	kmDecodeSuccess(t, fx.kmPost(t, fx.navigationPath, body))
	c := fx.observer.Counts()
	if got := c.Prepares - base.Prepares; got != 1 {
		t.Fatalf("nav prepares = %d, want 1", got)
	}
	if got := c.Executes - base.Executes; got != 1 {
		t.Fatalf("nav executes = %d, want 1", got)
	}
	if got := c.Results() - base.Results(); got != 1 {
		t.Fatalf("nav result commands = %d, want 1", got)
	}
	if got := c.Replies - base.Replies; got != 1 || c.ReplyErrors != 0 {
		t.Fatalf("nav replies = %d errors = %d", got, c.ReplyErrors)
	}
	// Two inspector reads cross this wire: the pre-claim source-table read
	// (km_child) and the post-claim referenced-table disclosure read
	// (km_parent) — each runs a prepared information_schema.COLUMNS execute.
	m := fx.observer.MetaCounts()
	if got := m.Executes - baseMeta.Executes; got < 2 {
		t.Fatalf("nav inspector metadata executes = %d, want >= 2 (%+v -> %+v)", got, baseMeta, m)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})

	if rec := fx.kmPost(t, fx.navigationPath, body); rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
		t.Fatalf("nav replay = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	if got := fx.observer.Counts().Results() - base.Results(); got != 1 {
		t.Fatalf("nav replay produced %d result commands", got-1)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1, Claims: 1, Linked: 1})
}

// ---------------------------------------------------------------------------
// Group D — boundaries and failure phases: pre-claim failures write at most
// one NULL-key row and never a claim; post-claim outcomes take the single
// keyed finalize path; shape failures write nothing.
// ---------------------------------------------------------------------------

func TestMySQLKeyedMatrixD_RequestBoundaries(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
	post := func(body string) *httptest.ResponseRecorder {
		return fx.kmPost(t, fx.executePath, body)
	}

	// Malformed actor: a garbage bearer is refused before any dispatch.
	rec := doBearerWithBody(t, fx.router, http.MethodPost, fx.executePath, "not-a-real-token",
		`{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":"km-d-1"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("malformed actor = %d, want 401", rec.Code)
	}
	// Machine credentials on the ordinary route are legal — the malformed
	// actor here is an invalid user bearer.
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{})

	// Malformed bodies: truncated JSON, unknown fields, null/invalid keys —
	// the strict decoder rejects every shape before the service runs.
	for _, body := range []string{
		`{`,
		`{"statement":"select id from query_e2e_aux.km_cells","bogus":1}`,
		`{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":null}`,
		`{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":"` + strings.Repeat("k", 65) + `"}`,
	} {
		rec = post(body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("malformed body %q = %d, want 400", body, rec.Code)
		}
	}
	// NUL-containing key: JSON-valid but rejected by key validation.
	rec = post(`{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":"km-\u0000nul"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("NUL key = %d, want 400", rec.Code)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{})

	// Unknown target: 404 writes nothing under any target.
	rec = fx.kmPost(t, fmt.Sprintf("/query-targets/%d/execute", fx.targetID+777777),
		`{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":"km-d-2"}`)
	if rec.Code != http.StatusNotFound || kmErrorCode(t, rec) != "query_target_not_found" {
		t.Fatalf("unknown target = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	// Malformed statement id on the saved route.
	rec = fx.kmPost(t, fmt.Sprintf("%s/abc/execute", fx.savedPath), `{"clientExecutionId":"km-d-3"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("malformed statementId = %d, want 400", rec.Code)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{})
}

func TestMySQLKeyedMatrixD_PreClaimBoundaries(t *testing.T) {
	t.Run("denied target writes one NULL-key rejected pair then repairs", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{credentialDisabled: true})
		key := "km-d-denied"
		rec := fx.kmPost(t, fx.executePath, `{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":"`+key+`"}`)
		if rec.Code != http.StatusForbidden || kmErrorCode(t, rec) != "query_not_allowed" {
			t.Fatalf("denied keyed execute = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1})
		var status, code string
		var execKey sql.NullString
		if err := fx.db.QueryRow(`SELECT status, error_code, client_execution_id FROM query_executions WHERE target_resource_id = ?`, fx.targetID).
			Scan(&status, &code, &execKey); err != nil {
			t.Fatalf("denied row read: %v", err)
		}
		if status != string(model.QueryExecutionRejected) || code != "query_not_allowed" || execKey.Valid {
			t.Fatalf("denied evidence = %s/%s key=%v, want rejected/query_not_allowed/NULL", status, code, execKey)
		}

		// Repair: enabling the credential frees the same key for admission.
		mustExec(t, fx.db, `UPDATE query_target_credentials SET enabled = 1 WHERE resource_id = ?`, fx.targetID)
		kmDecodeSuccess(t, fx.kmPost(t, fx.executePath, `{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":"`+key+`"}`))
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 2, Audits: 2, Claims: 1, Linked: 1})
	})

	t.Run("saved preparation failures stay key-free", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
		stmtID := fx.kmCreateSaved(t,
			`{"name":"km-d-saved","statement":"select id, note from query_e2e_aux.km_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)

		// Missing statement: keyed request → invisible 404, NULL-key pair.
		rec := fx.kmPost(t, fx.kmSavedExecPath(stmtID+9999), `{"clientExecutionId":"km-d-sav-1"}`)
		if rec.Code != http.StatusNotFound || kmErrorCode(t, rec) != "saved_statement_not_found" {
			t.Fatalf("missing saved = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		// Invalid typed value: keyed request → 400, NULL-key rejected pair.
		rec = fx.kmPost(t, fx.kmSavedExecPath(stmtID), `{"values":{"cell_id":"oops"},"clientExecutionId":"km-d-sav-2"}`)
		if rec.Code != http.StatusBadRequest || kmErrorCode(t, rec) != "validation_failed" {
			t.Fatalf("invalid saved value = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		// Non-owner: the personal statement stays invisible → 404.
		rec = fx.kmPostAs(t, fx.kmSavedExecPath(stmtID), `{"values":{"cell_id":9},"clientExecutionId":"km-d-sav-3"}`, fx.otherToken)
		if rec.Code != http.StatusNotFound || kmErrorCode(t, rec) != "saved_statement_not_found" {
			t.Fatalf("non-owner saved = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3})
		// All three failed pre-claim: no claim anywhere on this target.
		if n := countWhere(t, fx.db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NOT NULL`, fx.targetID); n != 0 {
			t.Fatalf("pre-claim failures wrote %d keyed rows", n)
		}
		// The same keys remain free: owner execution succeeds on a reused key.
		kmDecodeSuccess(t, fx.kmPost(t, fx.kmSavedExecPath(stmtID), `{"values":{"cell_id":9},"clientExecutionId":"km-d-sav-2"}`))
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 4, Audits: 4, Claims: 1, Linked: 1})
	})

	t.Run("navigation preparation failures stay key-free", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
		// Unknown FK name: keyed request → 400, NULL-key rejected pair.
		rec := fx.kmPost(t, fx.navigationPath,
			`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_missing"},"localValues":["2"],"clientExecutionId":"km-d-nav-1"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("nav bad FK = %d, want 400", rec.Code)
		}
		// Arity mismatch: two local values for a one-column FK → 400.
		rec = fx.kmPost(t, fx.navigationPath,
			`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["1","2"],"clientExecutionId":"km-d-nav-2"}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("nav arity = %d, want 400", rec.Code)
		}
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 2, Audits: 2})
		if n := countWhere(t, fx.db, `SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ? AND client_execution_id IS NOT NULL`, fx.targetID); n != 0 {
			t.Fatalf("nav pre-claim failures wrote %d keyed rows", n)
		}
		// A valid request on the reused key executes and links normally.
		kmDecodeSuccess(t, fx.kmPost(t, fx.navigationPath,
			`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["2"],"clientExecutionId":"km-d-nav-2"}`))
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 3, Audits: 3, Claims: 1, Linked: 1})
	})

	t.Run("claim admission failure writes one NULL-key failed pair", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{wrapRepo: true})
		key := "km-d-claimfail"
		fx.faultRepo.failClaimAdmission.Store(true)
		rec := fx.kmPost(t, fx.executePath, `{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":"`+key+`"}`)
		if rec.Code != http.StatusBadGateway || kmErrorCode(t, rec) != "query_backend_error" {
			t.Fatalf("claim admission fault = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 1, Audits: 1})
		// The failed admission wrote one NULL-key failed row and NO claim.
		var status, code string
		var execKey sql.NullString
		if err := fx.db.QueryRow(`SELECT status, error_code, client_execution_id FROM query_executions WHERE target_resource_id = ?`, fx.targetID).
			Scan(&status, &code, &execKey); err != nil {
			t.Fatalf("claim-fault row read: %v", err)
		}
		if status != string(model.QueryExecutionFailed) || code != "query_backend_error" || execKey.Valid {
			t.Fatalf("claim-fault evidence = %s/%s key=%v, want failed/query_backend_error/NULL", status, code, execKey)
		}
		fx.faultRepo.failClaimAdmission.Store(false)
		// The key was never occupied: the same key executes cleanly.
		kmDecodeSuccess(t, fx.kmPost(t, fx.executePath, `{"statement":"select id from query_e2e_aux.km_cells","clientExecutionId":"`+key+`"}`))
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 2, Audits: 2, Claims: 1, Linked: 1})
	})
}

func TestMySQLKeyedMatrixD_PostClaimOutcomes(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{auxTwo: true})
	post := func(body string) *httptest.ResponseRecorder {
		return fx.kmPost(t, fx.executePath, body)
	}
	keyedRow := func(key string) (status, code string, linked bool) {
		var execKey sql.NullString
		var link sql.NullInt64
		if err := fx.db.QueryRow(`SELECT status, error_code, client_execution_id FROM query_executions WHERE target_resource_id = ? AND client_execution_id = ?`, fx.targetID, key).
			Scan(&status, &code, &execKey); err != nil {
			t.Fatalf("keyed row %q read: %v", key, err)
		}
		if !execKey.Valid || execKey.String != key {
			t.Fatalf("keyed row %q key = %v", key, execKey)
		}
		if err := fx.db.QueryRow(`SELECT execution_id FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = ?`, fx.targetID, key).
			Scan(&link); err != nil {
			t.Fatalf("claim %q read: %v", key, err)
		}
		return status, code, link.Valid
	}

	// Post-claim guard rejection: the claim admits before the guard, so a
	// multi-statement request finalizes as a keyed rejected row.
	rec := post(`{"statement":"select id from query_e2e_aux.km_cells; delete from query_e2e_aux.km_cells","clientExecutionId":"km-d-guard"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("keyed guard reject = %d, want 400", rec.Code)
	}
	status, code, linked := keyedRow("km-d-guard")
	if status != string(model.QueryExecutionRejected) || code != "validation_failed" || !linked {
		t.Fatalf("guard evidence = %s/%s linked=%v", status, code, linked)
	}

	// Post-claim disclosure rejection: ungoverned column → keyed rejected row.
	rec = post(`{"statement":"select free from query_e2e_aux.km_cells where id = 1","clientExecutionId":"km-d-discl"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("keyed disclosure reject = %d, want 403", rec.Code)
	}
	status, code, linked = keyedRow("km-d-discl")
	if status != string(model.QueryExecutionRejected) || code != "query_result_disclosure_blocked" || !linked {
		t.Fatalf("disclosure evidence = %s/%s linked=%v", status, code, linked)
	}

	// Post-claim driver failure: a subquery cardinality error passes guard and
	// disclosure, fails at MySQL → keyed failed row, query_backend_error.
	rec = post(`{"statement":"select id from query_e2e_aux.km_cells where id = (select 'x' union select 'y')","clientExecutionId":"km-d-driver"}`)
	if rec.Code != http.StatusBadGateway || kmErrorCode(t, rec) != "query_backend_error" {
		t.Fatalf("keyed driver failure = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	status, code, linked = keyedRow("km-d-driver")
	if status != string(model.QueryExecutionFailed) || code != "query_backend_error" || !linked {
		t.Fatalf("driver evidence = %s/%s linked=%v", status, code, linked)
	}

	// Post-claim timeout: the bulk subquery dies at the frozen deadline →
	// keyed timeout row. The server-side straggler is killed explicitly.
	rec = post(fmt.Sprintf(`{"statement":%q,"clientExecutionId":"km-d-timeout"}`, kmSlowSubquery))
	if rec.Code != http.StatusRequestTimeout || kmErrorCode(t, rec) != "query_timeout" {
		t.Fatalf("keyed timeout = %d %q", rec.Code, kmErrorCode(t, rec))
	}
	kmKillStragglers(t, fx.db)
	status, code, linked = keyedRow("km-d-timeout")
	if status != string(model.QueryExecutionTimeout) || code != "query_timeout" || !linked {
		t.Fatalf("timeout evidence = %s/%s linked=%v", status, code, linked)
	}

	// Post-claim success: the keyed finalize happy path — already proven in
	// the golden test, asserted here for the full outcome family.
	kmDecodeSuccess(t, post(`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"km-d-ok"}`))
	status, _, linked = keyedRow("km-d-ok")
	if status != string(model.QueryExecutionSuccess) || !linked {
		t.Fatalf("success evidence = %s linked=%v", status, linked)
	}
	fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 5, Audits: 5, Claims: 5, Linked: 5})
}

// ---------------------------------------------------------------------------
// Group H — frozen claim-digest vectors and live admission equivalence: the
// persisted request_digest equals the canonical digest of server-resolved
// inputs; resolved-window equivalences replay, distinct inputs conflict.
// ---------------------------------------------------------------------------

// kmDigestGoldens are the 12 frozen vectors from
// mysql-digest-goldens.json (claim-digest v1, independent lead encoder),
// embedded so the matrix verifies the implementation against the same
// constants without a filesystem dependency.
var kmDigestGoldens = []kmDigestGolden{
	{Domain: "execute", Name: "ordinary_unpaged_default", Statement: "SELECT id FROM app.orders", Page: 0, PageSize: 0, MaxRows: 100,
		WireHex: "076578656375746500001953454c4543542069642046524f4d206170702e6f7264657273000064",
		SHA256:  "fccc3c232102b49313e913ef0cfc2623b2de0c5889b725834244eca730ce6b3b"},
	{Domain: "execute", Name: "ordinary_explicit_page", Statement: "SELECT id FROM app.orders", Page: 1, PageSize: 25, MaxRows: 100,
		WireHex: "076578656375746500001953454c4543542069642046524f4d206170702e6f7264657273011964",
		SHA256:  "675e29122bf78eb55a63091146160d3d44e95e2779a78888b15c35ae34293bd9"},
	{Domain: "execute", Name: "ordinary_original_bytes", Statement: " SELECT id FROM app.orders;\n", Page: 0, PageSize: 0, MaxRows: 100,
		WireHex: "076578656375746500001c2053454c4543542069642046524f4d206170702e6f72646572733b0a000064",
		SHA256:  "530869ff9b66dfa1e294d1729c13dc29441a19d00aa6cd57879db29e88569474"},
	{Domain: "execute", Name: "ordinary_hard_cap", Statement: "SELECT id FROM app.orders", Page: 0, PageSize: 0, MaxRows: 500,
		WireHex: "076578656375746500001953454c4543542069642046524f4d206170702e6f72646572730000f403",
		SHA256:  "3d8278ef40c06e65445375d648b202c37011b1975e8bc370a153ce3f54c27f5e"},
	{Domain: "saved-statement", Name: "saved_typed_unpaged", StatementID: 19, Page: 0, PageSize: 0, MaxRows: 100,
		Values: []kmDigestGoldenValue{
			{Name: "tag", Kind: "s", Text: "ok"},
			{Name: "minimum", Kind: "i", Text: "-1"},
			{Name: "amount", Kind: "d", Text: "3.1400"},
			{Name: "enabled", Kind: "b", Text: "0"},
		},
		WireHex: "0f73617665642d73746174656d656e740000130406616d6f756e746406332e3134303007656e61626c6564620130076d696e696d756d69022d310374616773026f6b000064",
		SHA256:  "6767e6d67543f4cf4d5bcf7f9fe61ddfbd61e4709ba54f059bbe7bf10ca6f08e"},
	{Domain: "saved-statement", Name: "saved_paged", StatementID: 19, Page: 2, PageSize: 10, MaxRows: 100,
		Values: []kmDigestGoldenValue{
			{Name: "tag", Kind: "s", Text: "ok"},
			{Name: "minimum", Kind: "i", Text: "-1"},
			{Name: "amount", Kind: "d", Text: "3.1400"},
			{Name: "enabled", Kind: "b", Text: "0"},
		},
		WireHex: "0f73617665642d73746174656d656e740000130406616d6f756e746406332e3134303007656e61626c6564620130076d696e696d756d69022d310374616773026f6b020a64",
		SHA256:  "93f70fbd94104db53d689ed6d4382f11563cf269a83c02f1f8fd1006d1f6d114"},
	{Domain: "saved-statement", Name: "saved_string_5", StatementID: 19, Page: 0, PageSize: 0, MaxRows: 100,
		Values:  []kmDigestGoldenValue{{Name: "n", Kind: "s", Text: "5"}},
		WireHex: "0f73617665642d73746174656d656e7400001301016e730135000064",
		SHA256:  "b7b8b4db27788bc37361241af99632557ecb6a04356879f3c36ea38d233f4d68"},
	{Domain: "saved-statement", Name: "saved_integer_5", StatementID: 19, Page: 0, PageSize: 0, MaxRows: 100,
		Values:  []kmDigestGoldenValue{{Name: "n", Kind: "i", Text: "5"}},
		WireHex: "0f73617665642d73746174656d656e7400001301016e690135000064",
		SHA256:  "23d3f6b3c86ba927d5b4197c58fedd39b587da4ecc4a1143228c4c419133daca"},
	{Domain: "related-records", Name: "navigation_unpaged",
		SourceDatabase: "shop", SourceObject: "orders", SourceColumns: []string{"customer_id", "region"},
		ForeignKey: "fk_customer", LocalValues: []string{"7", "west"}, Page: 0, PageSize: 0, MaxRows: 100,
		WireHex: "0f72656c617465642d7265636f72647300000473686f70066f7264657273020b637573746f6d65725f696406726567696f6e0b666b5f637573746f6d65720201370477657374000064",
		SHA256:  "5167caf811690706fa483b4d97a341034f47e3f4449a20f9adb1f899f9fcc0c7"},
	{Domain: "related-records", Name: "navigation_source_database",
		SourceDatabase: "shop2", SourceObject: "orders", SourceColumns: []string{"customer_id", "region"},
		ForeignKey: "fk_customer", LocalValues: []string{"7", "west"}, Page: 0, PageSize: 0, MaxRows: 100,
		WireHex: "0f72656c617465642d7265636f72647300000573686f7032066f7264657273020b637573746f6d65725f696406726567696f6e0b666b5f637573746f6d65720201370477657374000064",
		SHA256:  "74b3b8a5b05b82caccd362bd86d0445272e245702f72658fa42384b5f5cbd1ec"},
	{Domain: "related-records", Name: "navigation_value_order",
		SourceDatabase: "shop", SourceObject: "orders", SourceColumns: []string{"customer_id", "region"},
		ForeignKey: "fk_customer", LocalValues: []string{"west", "7"}, Page: 0, PageSize: 0, MaxRows: 100,
		WireHex: "0f72656c617465642d7265636f72647300000473686f70066f7264657273020b637573746f6d65725f696406726567696f6e0b666b5f637573746f6d65720204776573740137000064",
		SHA256:  "9140ac3c9de731b471844b97bf2dbfa697135e58baa8f921f30da1fddb210d9e"},
	{Domain: "related-records", Name: "navigation_column_order",
		SourceDatabase: "shop", SourceObject: "orders", SourceColumns: []string{"region", "customer_id"},
		ForeignKey: "fk_customer", LocalValues: []string{"7", "west"}, Page: 0, PageSize: 0, MaxRows: 100,
		WireHex: "0f72656c617465642d7265636f72647300000473686f70066f72646572730206726567696f6e0b637573746f6d65725f69640b666b5f637573746f6d65720201370477657374000064",
		SHA256:  "0b00f7f697ffbcd971ecfe00bd73233c9c8264bb0a7c71c56e591fc3a020c3c9"},
}

// kmDigestGoldenValue is one typed saved-statement digest value in the
// frozen vectors (kind letters: n/s/d/i/b).
type kmDigestGoldenValue struct {
	Name string
	Kind string
	Text string
}

// namedValue converts a frozen typed value into the service constructor's
// canonical form (kind + text + declared name).
func (v kmDigestGoldenValue) namedValue(t *testing.T) service.ClaimDigestNamedValue {
	t.Helper()
	var out service.ClaimDigestNamedValue
	switch v.Kind {
	case "n":
		out = service.ClaimDigestNull()
	case "s":
		out = service.ClaimDigestString(v.Text)
	case "d":
		out = service.ClaimDigestDecimal(v.Text)
	case "i":
		parsed, err := strconv.ParseInt(v.Text, 10, 64)
		if err != nil {
			t.Fatalf("golden integer %q: %v", v.Text, err)
		}
		out = service.ClaimDigestInteger(parsed)
	case "b":
		out = service.ClaimDigestBoolean(v.Text == "1")
	default:
		t.Fatalf("unknown golden kind %q", v.Kind)
	}
	out.Name = v.Name
	return out
}

func TestMySQLKeyedMatrixH_DigestGoldens(t *testing.T) {
	for _, g := range kmDigestGoldens {
		t.Run(g.Name, func(t *testing.T) {
			var got string
			switch g.Domain {
			case "execute":
				got = service.DigestOrdinaryExecute(service.OrdinaryExecuteDigestInput{
					Database: "", Schema: "", Statement: g.Statement,
					Page: g.Page, PageSize: g.PageSize, MaxRows: g.MaxRows,
				})
			case "saved-statement":
				values := make([]service.ClaimDigestNamedValue, 0, len(g.Values))
				for _, v := range g.Values {
					values = append(values, v.namedValue(t))
				}
				got = service.DigestSavedStatement(service.SavedStatementDigestInput{
					Database: "", Schema: "", StatementID: g.StatementID,
					Values: values, Page: g.Page, PageSize: g.PageSize, MaxRows: g.MaxRows,
				})
			case "related-records":
				got = service.DigestRelatedRecords(service.RelatedRecordsDigestInput{
					Database: "", Schema: "",
					SourceSchema: g.SourceDatabase, SourceObject: g.SourceObject,
					SourceColumns: g.SourceColumns, ForeignKey: g.ForeignKey,
					LocalValues: g.LocalValues,
					Page:        g.Page, PageSize: g.PageSize, MaxRows: g.MaxRows,
				})
			default:
				t.Fatalf("unknown domain %q", g.Domain)
			}
			if got != g.SHA256 {
				t.Fatalf("%s digest = %s, want %s", g.Name, got, g.SHA256)
			}
			// The frozen wire encoding is independently consistent: hashing
			// the frozen bytes yields the same digest.
			wire, err := hexDecodeString(g.WireHex)
			if err != nil {
				t.Fatalf("%s wire hex: %v", g.Name, err)
			}
			if got := fmt.Sprintf("%x", sha256.Sum256(wire)); got != g.SHA256 {
				t.Fatalf("%s wire-bytes hash = %s, want %s", g.Name, got, g.SHA256)
			}
		})
	}
}

func hexDecodeString(s string) ([]byte, error) {
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(v)
	}
	return out, nil
}

// kmClaimDigest returns the request_digest persisted for one occupied key.
func (f *keyedMatrixFixture) kmClaimDigest(t *testing.T, key string) string {
	t.Helper()
	digest, _, _, _, _ := f.kmClaimRow(t, key)
	return digest
}

func TestMySQLKeyedMatrixH_LiveAdmission(t *testing.T) {
	t.Run("persisted digest equals canonical resolved inputs per entry", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})

		// Ordinary: no pagination, no maxRows — the digest covers the resolved
		// window (0/0 paging, guard-default 100-row cap), never the key.
		ordStmt := "select id, note from query_e2e_aux.km_cells where id = 9"
		kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
			fmt.Sprintf(`{"statement":%q,"clientExecutionId":"km-h-ord"}`, ordStmt)))
		wantOrd := service.DigestOrdinaryExecute(service.OrdinaryExecuteDigestInput{
			Statement: ordStmt, Page: 0, PageSize: 0, MaxRows: 100,
		})
		if got := fx.kmClaimDigest(t, "km-h-ord"); got != wantOrd {
			t.Fatalf("ordinary persisted digest = %s, want %s", got, wantOrd)
		}
		if _, _, timeout, _, _ := fx.kmClaimRow(t, "km-h-ord"); !timeout.Valid || timeout.Int64 != 5000 {
			t.Fatalf("ordinary claim budget = %v, want 5000", timeout)
		}

		// Saved: typed integer value 9 over the real statement id.
		stmtID := fx.kmCreateSaved(t,
			`{"name":"km-h-saved","statement":"select id, note from query_e2e_aux.km_cells where id = :cell_id","parameters":[{"name":"cell_id","type":"integer"}],"scope":"personal"}`)
		kmDecodeSuccess(t, fx.kmPost(t, fx.kmSavedExecPath(stmtID),
			`{"values":{"cell_id":9},"clientExecutionId":"km-h-sav"}`))
		wantSav := service.DigestSavedStatement(service.SavedStatementDigestInput{
			StatementID: stmtID,
			Values:      []service.ClaimDigestNamedValue{{Name: "cell_id", Kind: 'i', Text: "9"}},
			Page:        0, PageSize: 0, MaxRows: 100,
		})
		if got := fx.kmClaimDigest(t, "km-h-sav"); got != wantSav {
			t.Fatalf("saved persisted digest = %s, want %s", got, wantSav)
		}

		// Navigation: resolved limit is the server default (100); the source
		// column order comes from trusted FK metadata.
		kmDecodeSuccess(t, fx.kmPost(t, fx.navigationPath, kmNavBody("2", "km-h-nav")))
		wantNav := service.DigestRelatedRecords(service.RelatedRecordsDigestInput{
			SourceSchema: "query_e2e_aux", SourceObject: "km_child",
			SourceColumns: []string{"parent_id"}, ForeignKey: "fk_km_child_parent",
			LocalValues: []string{"2"},
			Page:        0, PageSize: 0, MaxRows: model.MaxRelatedRowsDefault,
		})
		if got := fx.kmClaimDigest(t, "km-h-nav"); got != wantNav {
			t.Fatalf("nav persisted digest = %s, want %s", got, wantNav)
		}
	})

	t.Run("resolved-window equivalences replay; distinct inputs conflict", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{childTwo: true, auxTwo: true})
		post := func(body string) *httptest.ResponseRecorder {
			return fx.kmPost(t, fx.executePath, body)
		}
		stmt := "select id, note from query_e2e_aux.km_cells where id = 9"

		// maxRows omitted == explicit 100 under the 100/500 guard config.
		kmDecodeSuccess(t, post(fmt.Sprintf(`{"statement":%q,"clientExecutionId":"km-h-e1"}`, stmt)))
		rec := post(fmt.Sprintf(`{"statement":%q,"maxRows":100,"clientExecutionId":"km-h-e1"}`, stmt))
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
			t.Fatalf("omitted-vs-100 replay = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		// Staging maxRows 700 clamps to the 500 hard cap — same digest.
		kmDecodeSuccess(t, post(fmt.Sprintf(`{"statement":%q,"maxRows":700,"clientExecutionId":"km-h-e2"}`, stmt)))
		rec = post(fmt.Sprintf(`{"statement":%q,"maxRows":500,"clientExecutionId":"km-h-e2"}`, stmt))
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
			t.Fatalf("700-vs-500 replay = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		// Capabilities are never digest inputs.
		kmDecodeSuccess(t, post(fmt.Sprintf(`{"statement":%q,"capabilities":["cellTruncated"],"clientExecutionId":"km-h-e3"}`, stmt)))
		rec = post(fmt.Sprintf(`{"statement":%q,"capabilities":["csvExport"],"clientExecutionId":"km-h-e3"}`, stmt))
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
			t.Fatalf("capabilities replay = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		// Absent pagination (0/0) differs from explicit 1/25.
		kmDecodeSuccess(t, post(fmt.Sprintf(`{"statement":%q,"clientExecutionId":"km-h-e4"}`, stmt)))
		rec = post(fmt.Sprintf(`{"statement":%q,"pagination":{"page":1,"pageSize":25},"clientExecutionId":"km-h-e4"}`, stmt))
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
			t.Fatalf("pagination conflict = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		// Original statement bytes distinguish whitespace variants.
		kmDecodeSuccess(t, post(fmt.Sprintf(`{"statement":%q,"clientExecutionId":"km-h-e5"}`, stmt)))
		rec = post(fmt.Sprintf(`{"statement":%q,"clientExecutionId":"km-h-e5"}`, stmt+" "))
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
			t.Fatalf("statement-bytes conflict = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		fx.kmExpectCounts(t, mysqlKeyedScopeCounts{Executions: 5, Audits: 5, Claims: 5, Linked: 5})

		// Saved: JSON member order never reaches the digest; typed text does.
		svID := fx.kmCreateSaved(t,
			`{"name":"km-h-sv","statement":"select id, note from query_e2e_aux.km_cells where id = :p_int and note <> :p_str","parameters":[{"name":"p_int","type":"integer"},{"name":"p_str","type":"string"}],"scope":"personal"}`)
		svPath := fx.kmSavedExecPath(svID)
		kmDecodeSuccess(t, fx.kmPost(t, svPath, `{"values":{"p_int":9,"p_str":"x"},"clientExecutionId":"km-h-sv1"}`))
		rec = fx.kmPost(t, svPath, `{"values":{"p_str":"x","p_int":9},"clientExecutionId":"km-h-sv1"}`)
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
			t.Fatalf("saved order replay = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		rec = fx.kmPost(t, svPath, `{"values":{"p_int":9,"p_str":"y"},"clientExecutionId":"km-h-sv1"}`)
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
			t.Fatalf("saved value conflict = %d %q", rec.Code, kmErrorCode(t, rec))
		}

		// Navigation: source object and source database are digest inputs.
		kmDecodeSuccess(t, fx.kmPost(t, fx.navigationPath, kmNavBody("2", "km-h-n1")))
		// km_child2 in the same database: same FK shape, different source object.
		rec = fx.kmPost(t, fx.navigationPath,
			`{"source":{"database":"query_e2e_aux","object":"km_child2","kind":"table","foreignKey":"fk_km_child2_parent"},"localValues":["2"],"clientExecutionId":"km-h-n1"}`)
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
			t.Fatalf("nav source-object conflict = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		// Same key on the aux2 source database: different source schema digest.
		rec = fx.kmPost(t, fx.navigationPath,
			`{"source":{"database":"query_e2e_aux2","object":"km_child","kind":"table","foreignKey":"fk_km2_child_parent"},"localValues":["2"],"clientExecutionId":"km-h-n1"}`)
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
			t.Fatalf("nav source-database conflict = %d %q", rec.Code, kmErrorCode(t, rec))
		}
		// Locator values are ordered digest inputs.
		rec = fx.kmPost(t, fx.navigationPath,
			`{"source":{"database":"query_e2e_aux","object":"km_child","kind":"table","foreignKey":"fk_km_child_parent"},"localValues":["1"],"clientExecutionId":"km-h-n1"}`)
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "client_execution_id_conflict" {
			t.Fatalf("nav locator conflict = %d %q", rec.Code, kmErrorCode(t, rec))
		}
	})

	t.Run("production resolution clamps to the prod budget", func(t *testing.T) {
		fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{environmentID: envProd})
		stmt := "select id, note from query_e2e_aux.km_cells where id = 9"
		// prod maxRows 700 clamps to 100 before the guard/digest.
		kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
			fmt.Sprintf(`{"statement":%q,"maxRows":700,"clientExecutionId":"km-h-p1"}`, stmt)))
		want := service.DigestOrdinaryExecute(service.OrdinaryExecuteDigestInput{
			Statement: stmt, Page: 0, PageSize: 0, MaxRows: 100,
		})
		if got := fx.kmClaimDigest(t, "km-h-p1"); got != want {
			t.Fatalf("prod persisted digest = %s, want %s", got, want)
		}
		if _, _, timeout, _, _ := fx.kmClaimRow(t, "km-h-p1"); !timeout.Valid || timeout.Int64 != 3000 {
			t.Fatalf("prod claim budget = %v, want 3000", timeout)
		}
		// prod omitted == prod 700 == 100.
		rec := fx.kmPost(t, fx.executePath, fmt.Sprintf(`{"statement":%q,"clientExecutionId":"km-h-p1"}`, stmt))
		if rec.Code != http.StatusConflict || kmErrorCode(t, rec) != "execution_already_exists" {
			t.Fatalf("prod replay = %d %q", rec.Code, kmErrorCode(t, rec))
		}
	})
}

// ---------------------------------------------------------------------------
// Group J — reads: claim-derived history entries, budget-derived running/
// unknown, concurrent-read consistency during finalize, typed-actor and
// key/target isolation, disabled-target reads, cursor binding, and the rule
// that a GET never writes, finalizes, retries, or returns result rows.
// ---------------------------------------------------------------------------

func TestMySQLKeyedMatrixJ_ClaimReadShapes(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})

	// A real unlinked claim surfaces as exactly one claim item: null id,
	// derived running status, the occupied key.
	fx.kmSeedClaim(t, "km-j-fresh", claimDigest("km-j-fresh"), time.Now().UTC(), 5000)
	items, _ := kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId=km-j-fresh"))
	if len(items) != 1 {
		t.Fatalf("unlinked claim read items = %v", items)
	}
	item := items[0]
	if item["kind"] != "claim" || item["id"] != nil || item["status"] != "running" || item["clientExecutionId"] != "km-j-fresh" {
		t.Fatalf("unlinked claim item = %v", item)
	}
	if item["claimedAt"] == nil {
		t.Fatalf("claim item missing claimedAt: %v", item)
	}

	// Aged claim past its recorded budget + grace derives unknown — asserted
	// from the stored attempt_timeout_ms, never a fabricated expiry.
	old := time.Now().Add(-80 * time.Second).UTC()
	fx.kmSeedClaim(t, "km-j-aged", claimDigest("km-j-aged"), old, 5000)
	items, _ = kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId=km-j-aged"))
	if len(items) != 1 || items[0]["status"] != "unknown" {
		t.Fatalf("aged claim item = %v, want unknown", items)
	}
	var storedBudget sql.NullInt64
	if err := fx.db.QueryRow(`SELECT attempt_timeout_ms FROM query_execution_claims WHERE target_resource_id = ? AND client_execution_id = 'km-j-aged'`, fx.targetID).Scan(&storedBudget); err != nil {
		t.Fatalf("stored budget read: %v", err)
	}
	if !storedBudget.Valid || storedBudget.Int64 != 5000 {
		t.Fatalf("aged claim stored budget = %v, want 5000", storedBudget)
	}

	// A NULL-budget claim (pre-T10-C shape) is an evidence gap → unknown.
	fx.kmSeedClaim(t, "km-j-nobudget", claimDigest("km-j-nobudget"), time.Now().UTC(), 0)
	items, _ = kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId=km-j-nobudget"))
	if len(items) != 1 || items[0]["status"] != "unknown" {
		t.Fatalf("NULL-budget claim item = %v, want unknown", items)
	}

	// GET is read-only: no claim finalize/release/retry, no pair writes, and
	// list items never carry result rows.
	beforeWrites := fx.kmCounts(t)
	for i := 0; i < 3; i++ {
		kmHistoryItems(t, fx.kmGet(t, fx.historyPath))
		kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId=km-j-fresh"))
	}
	if got := fx.kmCounts(t); got != beforeWrites {
		t.Fatalf("GETs wrote evidence: %+v -> %+v", beforeWrites, got)
	}
	for _, it := range items {
		if _, hasRows := it["rows"]; hasRows {
			t.Fatalf("history item carried result rows: %v", it)
		}
	}
}

func TestMySQLKeyedMatrixJ_ReadDuringFinalize(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{observedTable: kmCells})
	key := "km-j-final"
	srv := httptest.NewServer(fx.router)
	defer srv.Close()

	// The winner's result command parks at the wire: the claim exists but is
	// unlinked, so the point read returns exactly one claim item.
	arrived, release := fx.observer.HoldNextResult(t)
	defer release()
	postDone := make(chan *http.Response, 1)
	go func() {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+fx.executePath,
			strings.NewReader(`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"`+key+`"}`))
		req.Header.Set("Authorization", "Bearer "+fx.token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		postDone <- resp
		_ = err
	}()
	select {
	case <-arrived:
	case <-time.After(15 * time.Second):
		t.Fatal("keyed result command never reached the wire barrier")
	}
	items, _ := kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId="+key))
	if len(items) != 1 || items[0]["kind"] != "claim" || items[0]["id"] != nil || items[0]["status"] != "running" {
		t.Fatalf("mid-finalize read = %v, want one running claim item", items)
	}

	// While the winner finalizes, concurrent reads must each see exactly one
	// consistent item for the key — never a claim-plus-execution pair or a
	// torn state.
	release()
	var resp *http.Response
	select {
	case resp = <-postDone:
		resp.Body.Close()
	case <-time.After(15 * time.Second):
		t.Fatal("held keyed POST did not complete after release")
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("held keyed POST status = %d", resp.StatusCode)
	}
	items, _ = kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId="+key))
	if len(items) != 1 || items[0]["kind"] != "execution" || items[0]["id"] == nil || items[0]["status"] != "success" {
		t.Fatalf("post-finalize read = %v, want one success execution item", items)
	}

	// After finalize the terminal execution row wins every read — the claim
	// is a linked occupant, never a second item.
	reads := make(chan int, 8)
	for i := 0; i < 8; i++ {
		go func() {
			got, _ := kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId="+key))
			reads <- len(got)
		}()
	}
	for i := 0; i < 8; i++ {
		if n := <-reads; n != 1 {
			t.Fatalf("concurrent read returned %d items for one key", n)
		}
	}
}

func TestMySQLKeyedMatrixJ_IsolationAndFiltering(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})

	// One keyed execution by the admin actor.
	kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
		`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":"km-j-iso"}`))

	// A different user sees nothing under the same key — the claim-aware
	// read is actor-scoped for non-admin roles.
	items, _ := kmHistoryItems(t, fx.kmGetWithToken(t, fx.historyPath+"?clientExecutionId=km-j-iso", fx.otherToken))
	if len(items) != 0 {
		t.Fatalf("other-actor read leaked %d items", len(items))
	}
	// The repository point read enforces the same typed-actor scope.
	_, err := fx.execRepo.GetClaimWithExecution(t.Context(), fx.targetID, "", "km-j-iso",
		model.QueryExecutionIdentity{Kind: model.QueryExecutionActorUser, ID: fx.otherUserID})
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("other-actor claim point read = %v, want sql.ErrNoRows", err)
	}
	// Admin sees the real terminal execution id.
	items, _ = kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId=km-j-iso"))
	if len(items) != 1 || items[0]["kind"] != "execution" || items[0]["status"] != "success" {
		t.Fatalf("admin claim read = %v", items)
	}

	// Byte-exact key isolation: 'KM-J-ISO' is a different claim slot.
	items, _ = kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?clientExecutionId=KM-J-ISO"))
	if len(items) != 0 {
		t.Fatalf("case-variant key leaked %d items", len(items))
	}
	// Target isolation: the same key on a second target is a separate slot.
	fx2 := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
	items, _ = kmHistoryItems(t, fx2.kmGet(t, fx2.historyPath+"?clientExecutionId=km-j-iso"))
	if len(items) != 0 {
		t.Fatalf("cross-target key leaked %d items", len(items))
	}

	// Disabled-target history remains readable: disabling the credential
	// gates execution, never the audit record.
	mustExec(t, fx.db, `UPDATE query_target_credentials SET enabled = 0 WHERE resource_id = ?`, fx.targetID)
	items, _ = kmHistoryItems(t, fx.kmGet(t, fx.historyPath))
	if len(items) != 1 {
		t.Fatalf("disabled-target history = %v", items)
	}
	mustExec(t, fx.db, `UPDATE query_target_credentials SET enabled = 1 WHERE resource_id = ?`, fx.targetID)

	// Cursor flow: three executions, pageSize=1 walks all of them with no
	// overlap; a bogus cursor is a controlled 400.
	for _, key := range []string{"km-j-c1", "km-j-c2"} {
		kmDecodeSuccess(t, fx.kmPost(t, fx.executePath,
			fmt.Sprintf(`{"statement":"select id, note from query_e2e_aux.km_cells where id = 9","clientExecutionId":%q}`, key)))
	}
	items, cursor := kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?pageSize=1"))
	if len(items) != 1 || cursor == "" {
		t.Fatalf("cursor page 1 = %v cursor=%q", items, cursor)
	}
	seen := map[float64]bool{items[0]["id"].(float64): true}
	for cursor != "" {
		items, cursor = kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?pageSize=1&cursor="+url.QueryEscape(cursor)))
		if len(items) == 0 {
			break
		}
		id, ok := items[0]["id"].(float64)
		if ok && seen[id] {
			t.Fatalf("cursor returned duplicate item id %v", id)
		}
		if ok {
			seen[id] = true
		}
	}
	if len(seen) != 3 {
		t.Fatalf("cursor walk saw %d items, want all 3 executions", len(seen))
	}
	rec := fx.kmGet(t, fx.historyPath+"?cursor=not-a-real-cursor")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad cursor = %d, want 400", rec.Code)
	}

	// Status filtering: the failed row appears under ?status=failed, not
	// success.
	fx.kmPost(t, fx.executePath, `{"statement":"select free from query_e2e_aux.km_cells where id = 1","clientExecutionId":"km-j-fail"}`)
	items, _ = kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?status=rejected"))
	if len(items) != 1 || items[0]["status"] != "rejected" {
		t.Fatalf("status=rejected items = %v", items)
	}
	items, _ = kmHistoryItems(t, fx.kmGet(t, fx.historyPath+"?status=success"))
	if len(items) != 3 {
		t.Fatalf("status=success items = %v, want the 3 success rows", items)
	}
}

func TestMySQLKeyedMatrixJ_MachineReadsForbidden(t *testing.T) {
	fx := setupKeyedMatrixFixture(t, keyedMatrixOpts{})
	issued, err := fx.machineSvc.Create(t.Context(), service.AuthenticatedUser{ID: fx.adminUserID, Role: "admin"}, model.MachinePrincipalCreateRequest{
		Name: "km-j-machine", Scopes: []model.MachineScope{model.MachineScopeGovernedSelect},
	})
	if err != nil {
		t.Fatalf("create machine credential: %v", err)
	}
	// The governed-select scope permits ordinary execution but never the
	// user-only history/navigation/saved surfaces.
	for _, tc := range []struct {
		method, path, body string
	}{
		{http.MethodGet, fx.historyPath, ""},
		{http.MethodPost, fx.navigationPath, kmNavBody("2", "km-j-m1")},
		{http.MethodPost, fx.kmSavedExecPath(1), `{"clientExecutionId":"km-j-m2"}`},
		{http.MethodGet, fx.savedPath, ""},
	} {
		rec := doBearerWithBody(t, fx.router, tc.method, tc.path, issued.Secret, tc.body)
		if rec.Code != http.StatusForbidden || kmErrorCode(t, rec) != "machine_scope_denied" {
			t.Fatalf("machine %s %s = %d %q, want 403 machine_scope_denied", tc.method, tc.path, rec.Code, kmErrorCode(t, rec))
		}
	}
}

// kmGetWithToken issues one authenticated GET with an explicit bearer token.
func (f *keyedMatrixFixture) kmGetWithToken(t *testing.T, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	return doBearer(t, f.router, http.MethodGet, path, token)
}
