//go:build integration

// Package integration validates the disposable query target fixture against MySQL.
// input: shared integration harness, query fixture, credential seed, and MySQL
// output: readiness, execution, identity idempotency, and binding regression tests
// pos: real-MySQL coverage for the dev query target fixture
// note: if this file changes, update header and README.md
package integration

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// fixtureName returns a per-test-unique resource name so fixture tests do not
// collide on the shared (non-reset) integration database. The production
// default name (local-mysql-query-dev) is used only by cmd/querydev for local dev.
func fixtureName(t *testing.T) string {
	t.Helper()
	return "local-mysql-query-dev-" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "-")
}

// ensureFixtureTarget creates/ensures the fixture target + profile (host/port
// from the disposable DSN) WITHOUT seeding a credential. Returns the target id.
// Splitting ensure from seed lets a test assert a failed (mismatched) seed.
func ensureFixtureTarget(t *testing.T, db *sql.DB) uint64 {
	t.Helper()
	host, port, err := service.ParseMySQLDSNHostPort(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse dsn host/port: %v", err)
	}
	fixture := service.NewQueryDevTargetFixture(
		mysql.NewDictionaryRepository(db),
		mysql.NewResourceRepository(db),
	)
	targetID, err := fixture.EnsureLocalQueryTarget(context.Background(), service.QueryDevTargetFixtureConfig{
		EnvironmentSlug: "dev",
		OwnerEmail:      "dba@example.com",
		ResourceName:    fixtureName(t),
		DisplayName:     "Local MySQL Query Dev",
		Engine:          "mysql",
		Version:         "8.0",
		Role:            "primary",
		Host:            host,
		Port:            port,
	})
	if err != nil {
		t.Fatalf("ensure local query target: %v", err)
	}
	return targetID
}

// seedFixtureCredential seeds the credential for the target, resolving the
// credential DSN from the env (set here from credentialDSN). Returns the seed
// error so a test can assert binding failure.
func seedFixtureCredential(t *testing.T, db *sql.DB, targetID uint64, credentialDSN string) error {
	t.Helper()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, credentialDSN)
	_, err := newDevSeeder(db).Seed(context.Background(), service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	})
	return err
}

func TestQueryDevTargetFixture_EnsuresLocalTargetAndBecomesReady(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetID := ensureFixtureTarget(t, db)
	if err := seedFixtureCredential(t, db, targetID, globalEnv.dsn); err != nil {
		t.Fatalf("seed: %v", err)
	}
	target := findTargetByID(t, mustList(t, newReadinessService(db), ctx), targetID)
	if target.Readiness != model.ReadinessReady {
		t.Fatalf("readiness = %q, want ready", target.Readiness)
	}
	if !target.AvailableActions.Run {
		t.Fatal("availableActions.run = false, want true")
	}
	if target.Governance.SafetyState != model.SafetyStateReadonlySandboxEnabled {
		t.Fatalf("safetyState = %q, want readonly_sandbox_enabled", target.Governance.SafetyState)
	}
	if !target.Governance.ExecutionEnabled {
		t.Fatal("executionEnabled = false, want true")
	}
}

func TestQueryDevTargetFixture_Idempotent_NoDuplicateRows(t *testing.T) {
	db := setupTestDB(t)
	id1 := ensureFixtureTarget(t, db)
	id2 := ensureFixtureTarget(t, db)
	if id1 != id2 {
		t.Fatalf("ensure returned different ids: %d vs %d (must be idempotent)", id1, id2)
	}
	if err := seedFixtureCredential(t, db, id1, globalEnv.dsn); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Re-seed must not duplicate the credential row.
	if err := seedFixtureCredential(t, db, id1, globalEnv.dsn); err != nil {
		t.Fatalf("re-seed: %v", err)
	}
	var resCount, profCount, credCount int
	if err := db.QueryRow(`
		select count(*)
		from resources r
		join resource_external_identifiers rei on rei.resource_id = r.id
		where r.name = ? and rei.external_system = 'controlhub-dev-fixture' and rei.external_value = ?`,
		fixtureName(t), fixtureName(t),
	).Scan(&resCount); err != nil {
		t.Fatalf("count resources: %v", err)
	}
	if err := db.QueryRow(`select count(*) from resource_profiles_database_instance where resource_id = ?`, id1).Scan(&profCount); err != nil {
		t.Fatalf("count profiles: %v", err)
	}
	if err := db.QueryRow(`select count(*) from query_target_credentials where resource_id = ?`, id1).Scan(&credCount); err != nil {
		t.Fatalf("count credentials: %v", err)
	}
	if resCount != 1 || profCount != 1 || credCount != 1 {
		t.Fatalf("idempotency counts = resource:%d profile:%d credential:%d, want 1/1/1", resCount, profCount, credCount)
	}
}

func TestQueryDevTargetFixture_SelectOneExecutes(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetID := ensureFixtureTarget(t, db)
	if err := seedFixtureCredential(t, db, targetID, globalEnv.dsn); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Create fixture table and seed disclosure policies for the governed query.
	mustExec(t, db, `drop table if exists qe_sandbox_fixtures`)
	mustExec(t, db, `create table qe_sandbox_fixtures (id bigint unsigned not null primary key, name varchar(64) not null)`)
	mustExec(t, db, `insert into qe_sandbox_fixtures (id, name) values (1,'alpha')`)
	seedDisclosurePolicies(t, db, targetID, testDBName(t), "qe_sandbox_fixtures", "id", "name")

	resp, err := newExecutionService(db).Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "select id from qe_sandbox_fixtures limit 1", MaxRows: 10})
	if err != nil {
		t.Fatalf("execute select: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.RowCount != 1 {
		t.Fatalf("rowCount = %d, want 1", resp.RowCount)
	}
}

func TestQueryDevTargetFixture_UnsafeSQLRejectedAndRecorded(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetID := ensureFixtureTarget(t, db)
	if err := seedFixtureCredential(t, db, targetID, globalEnv.dsn); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := newExecutionService(db).Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "delete from qe_sandbox_fixtures", MaxRows: 10}); err == nil {
		t.Fatal("unsafe statement must be rejected, got nil error")
	}
	items, _, err := mysql.NewQueryExecutionRepository(db).ListExecutions(ctx, model.QueryExecutionListQuery{TargetResourceID: targetID, Page: 1, PageSize: 20, Mode: model.PaginationModeOffset})
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	var rejected bool
	for _, it := range items {
		if it.Status == model.QueryExecutionRejected {
			rejected = true
		}
	}
	if !rejected {
		t.Fatalf("no rejected execution recorded; statuses = %v", statusesOf(items))
	}
}

func TestQueryDevTargetFixture_HistoryRecordsSuccessAndRejection(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetID := ensureFixtureTarget(t, db)
	if err := seedFixtureCredential(t, db, targetID, globalEnv.dsn); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Create fixture table and seed disclosure policies for the governed query.
	mustExec(t, db, `drop table if exists qe_sandbox_fixtures`)
	mustExec(t, db, `create table qe_sandbox_fixtures (id bigint unsigned not null primary key, name varchar(64) not null)`)
	mustExec(t, db, `insert into qe_sandbox_fixtures (id, name) values (1,'alpha')`)
	seedDisclosurePolicies(t, db, targetID, testDBName(t), "qe_sandbox_fixtures", "id", "name")

	if _, err := newExecutionService(db).Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "select id from qe_sandbox_fixtures limit 1", MaxRows: 10}); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := newExecutionService(db).Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "delete from qe_sandbox_fixtures", MaxRows: 10}); err == nil {
		t.Fatal("unsafe statement must be rejected")
	}
	items, _, err := mysql.NewQueryExecutionRepository(db).ListExecutions(ctx, model.QueryExecutionListQuery{TargetResourceID: targetID, Page: 1, PageSize: 20, Mode: model.PaginationModeOffset})
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	seen := map[model.QueryExecutionStatus]bool{}
	for _, it := range items {
		seen[it.Status] = true
		assertHistoryRowHasNoDSN(t, it, globalEnv.dsn) // history is metadata-only
	}
	if !seen[model.QueryExecutionSuccess] || !seen[model.QueryExecutionRejected] {
		t.Fatalf("history missing success/rejection; statuses = %v", statusesOf(items))
	}
}

func TestQueryDevTargetFixture_NoDSNStored(t *testing.T) {
	db := setupTestDB(t)
	targetID := ensureFixtureTarget(t, db)
	if err := seedFixtureCredential(t, db, targetID, globalEnv.dsn); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Credential metadata row stores no DSN (existing helper).
	assertCredentialRowStoresNoDSN(t, db, targetID, globalEnv.dsn)
	// Profile row stores connection context (host/port/engine) only — no DSN.
	var engine, version, host, role, spec string
	var port int
	if err := db.QueryRow(`select engine, version, host, port, role, spec from resource_profiles_database_instance where resource_id = ?`, targetID).Scan(&engine, &version, &host, &port, &role, &spec); err != nil {
		t.Fatalf("read profile: %v", err)
	}
	for _, v := range []string{engine, version, host, role, spec} {
		if v == globalEnv.dsn || strings.Contains(v, "tcp(") || strings.Contains(v, "://") || strings.Contains(v, "@") {
			t.Fatalf("profile column carries a DSN fragment: %q", v)
		}
	}
}

func TestQueryDevTargetFixture_ProfileHostPortMatchesDSN(t *testing.T) {
	db := setupTestDB(t)
	targetID := ensureFixtureTarget(t, db)
	wantHost, wantPort, err := service.ParseMySQLDSNHostPort(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	var host string
	var port int
	if err := db.QueryRow(`select host, port from resource_profiles_database_instance where resource_id = ?`, targetID).Scan(&host, &port); err != nil {
		t.Fatalf("read profile host/port: %v", err)
	}
	if host != wantHost || port != wantPort {
		t.Fatalf("profile host:port = %s:%d, want %s:%d (from the credential DSN)", host, port, wantHost, wantPort)
	}
}

func TestQueryDevTargetFixture_FailClosed_OnBadBindingStaysLocked(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetID := ensureFixtureTarget(t, db)
	// Credential resolves to a host:port that cannot bind to the fixture target.
	const mismatchedDSN = "u:p@tcp(mismatch.invalid:9999)/x"
	if err := seedFixtureCredential(t, db, targetID, mismatchedDSN); err == nil {
		t.Fatal("seed must fail for a mismatched credential DSN, got nil")
	}
	// No credential row written → target stays locked and not runnable.
	target := findTargetByID(t, mustList(t, newReadinessService(db), ctx), targetID)
	if target.Readiness == model.ReadinessReady {
		t.Fatal("target must not be ready after a failed (mismatched) seed")
	}
	if target.AvailableActions.Run {
		t.Fatal("target must not expose run=true after a failed seed")
	}
	if _, err := newExecutionService(db).Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "select id from qe_sandbox_fixtures limit 1", MaxRows: 10}); err == nil {
		t.Fatal("execute must be rejected for a locked target")
	}
}

// statusesOf returns the status list of execution records (for failure messages).
func statusesOf(items []model.QueryExecutionRecord) []model.QueryExecutionStatus {
	out := make([]model.QueryExecutionStatus, 0, len(items))
	for _, it := range items {
		out = append(out, it.Status)
	}
	return out
}

// assertHistoryRowHasNoDSN asserts an execution record carries no DSN-looking
// value in any stored text column. WHY: history is metadata-only by contract.
func assertHistoryRowHasNoDSN(t *testing.T, rec model.QueryExecutionRecord, dsn string) {
	t.Helper()
	for _, v := range []string{rec.StatementDigest, rec.StatementPreview, rec.ErrorCode, rec.ErrorMessage} {
		if v == dsn || strings.Contains(v, "tcp(") || strings.Contains(v, "://") || strings.Contains(v, "@") {
			t.Fatalf("history row carries a DSN fragment: %q", v)
		}
	}
}
