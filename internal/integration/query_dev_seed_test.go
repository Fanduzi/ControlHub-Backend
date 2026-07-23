//go:build integration

// Package integration provides Testcontainers-backed tests for the local/dev
// query credential seed path (Task B2): the seed service against real MySQL,
// end-to-end readiness derivation, select 1 execution, the no-DSN-stored
// invariant, and a host/port mismatch regression.
package integration

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// devSeedCredentialRef is the opaque ref the dev seed stores. It is deliberately
// a safe [A-Z0-9_]+ key — the DSN it resolves to lives only in the environment.
const devSeedCredentialRef = "LOCAL_QUERY_RO"

// newDevSeedTarget provisions a mysql/staging query target whose connection
// profile host/port optionally MATCH the disposable test MySQL DSN, and
// intentionally writes NO credential row — the dev seed service under test is
// expected to create it. It returns the target resource id, the db, and the DSN
// the credential must resolve to. This mirrors setupQuerySandboxTarget minus
// the seedCredentialRow fixture, so the seed path itself is what is exercised.
func newDevSeedTarget(t *testing.T, matchDSN bool) (uint64, *sql.DB, string) {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()

	// A fixture table so a real SELECT has something to read (defense in depth
	// beyond `select 1`); owned by the test, never ControlHub seed data.
	mustExec(t, db, `drop table if exists qe_sandbox_fixtures`)
	mustExec(t, db, `create table qe_sandbox_fixtures (id bigint unsigned not null primary key, name varchar(64) not null)`)
	mustExec(t, db, `insert into qe_sandbox_fixtures (id, name) values (1,'alpha')`)

	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qe-devseed-target-" + strings.ReplaceAll(t.Name(), "/", "-"),
		DisplayName:     "Dev Seed Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create devseed target resource: %v", err)
	}

	dsnCfg, err := mysqldriver.ParseDSN(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse test dsn: %v", err)
	}
	dsnHost, dsnPortStr, err := net.SplitHostPort(dsnCfg.Addr)
	if err != nil {
		t.Fatalf("split test dsn addr %q: %v", dsnCfg.Addr, err)
	}
	dsnPort, err := strconv.Atoi(dsnPortStr)
	if err != nil {
		t.Fatalf("parse test dsn port %q: %v", dsnPortStr, err)
	}

	host, port := dsnHost, dsnPort
	if !matchDSN {
		// Deliberately mismatched so the resolved credential cannot bind to this
		// target — exercises the seeder's DSN-binding fail-closed check.
		host, port = "mismatch.invalid", 9999
	}
	mustExec(t, db, `insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) values (?, 'mysql', '8.0', ?, ?, 'primary', '{}')`, res.ID, host, port)

	return res.ID, db, globalEnv.dsn
}

// newDevSeeder wires the seeder exactly as cmd/querydev does: the real query
// target read model, the env credential resolver, and the concrete credential
// metadata writer.
func newDevSeeder(db *sql.DB) *service.QueryDevCredentialSeeder {
	return service.NewQueryDevCredentialSeeder(
		mysql.NewQueryTargetRepository(db),
		service.NewEnvCredentialResolver(),
		mysql.NewQueryExecutionRepository(db),
	)
}

// newReadinessService wires the credential-aware query target read model so a
// test can assert the same readiness GET /query-targets would report.
func newReadinessService(db *sql.DB) *service.QueryTargetService {
	return service.NewQueryTargetService(mysql.NewQueryTargetRepository(db)).
		WithCredentialReader(mysql.NewQueryExecutionRepository(db))
}

func newExecutionService(db *sql.DB) *service.QueryExecutionService {
	return service.NewQueryExecutionService(
		mysql.NewQueryTargetRepository(db),
		mysql.NewQueryExecutionRepository(db),
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		wallClock{},
		service.NewMySQLSchemaInspector(),
		service.NewQueryDisclosureService(
			mysql.NewQueryDisclosureRepository(db),
			mysql.NewQueryDisclosureRepository(db),
			service.NewMySQLSchemaInspector(),
			mysql.NewQueryTargetRepository(db),
		),
	)
}

// TestQueryDevSeed_MakesTargetReadyAndExecutesSelectOne is the Task B2 happy
// path: seeding one mysql/staging target through the dev seed service makes it
// ready (run=true, executionEnabled, readonly_sandbox_enabled), lets a real
// `select 1` execute, stays idempotent on re-seed, and stores no DSN-looking
// value in query_target_credentials.
func TestQueryDevSeed_MakesTargetReadyAndExecutesSelectOne(t *testing.T) {
	targetID, db, dsn := newDevSeedTarget(t, true)
	ctx := context.Background()

	// The credential resolves back to the disposable test MySQL. The DSN lives
	// only in the environment; it is never passed to the seeder directly.
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, dsn)

	seeder := newDevSeeder(db)
	cfg := service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}
	if _, err := seeder.Seed(ctx, cfg); err != nil {
		t.Fatalf("Seed: %v", err)
	}

	// Idempotent: re-seeding the same target refreshes metadata instead of
	// failing on the unique (resource_id) key, and never creates a second row.
	if _, err := seeder.Seed(ctx, cfg); err != nil {
		t.Fatalf("re-seed (idempotency): %v", err)
	}

	// 1. Readiness via the real read model (the same path as GET /query-targets).
	target := findTargetByID(t, mustList(t, newReadinessService(db), ctx), targetID)
	if target.Readiness != model.ReadinessReady {
		t.Fatalf("readiness = %q, want ready", target.Readiness)
	}
	if !target.AvailableActions.Run {
		t.Fatal("availableActions.run = false, want true")
	}
	if !target.Governance.ExecutionEnabled {
		t.Fatal("governance.executionEnabled = false, want true")
	}
	if target.Governance.SafetyState != model.SafetyStateReadonlySandboxEnabled {
		t.Fatalf("governance.safetyState = %q, want readonly_sandbox_enabled", target.Governance.SafetyState)
	}

	// 2. Execute `select 1` through the real execution service.
	resp, err := newExecutionService(db).Execute(ctx, ownerDBA, targetID, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10})
	if err != nil {
		t.Fatalf("Execute select 1: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("execute status = %q, want success", resp.Status)
	}
	if resp.RowCount != 1 {
		t.Fatalf("rowCount = %d, want 1", resp.RowCount)
	}

	// 3. query_target_credentials stores METADATA only — no DSN-looking value.
	assertCredentialRowStoresNoDSN(t, db, targetID, dsn)
}

// TestQueryDevSeed_RejectsMismatchedCredentialAndStaysLocked is the Task B2
// regression: when the credential resolves to a host/port that does not match
// the selected target, the seed is rejected, no metadata is written, the target
// is not runnable, and execution is rejected.
func TestQueryDevSeed_RejectsMismatchedCredentialAndStaysLocked(t *testing.T) {
	targetID, db, dsn := newDevSeedTarget(t, false) // profile host/port != DSN
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, dsn)

	seeder := newDevSeeder(db)
	if _, err := seeder.Seed(ctx, service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err == nil {
		t.Fatal("Seed expected to fail for a credential bound to a mismatched host/port, got nil")
	}

	// No metadata row written → the target stays locked and not runnable.
	repo := mysql.NewQueryExecutionRepository(db)
	if _, err := repo.GetCredentialByResourceID(ctx, targetID); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("credential row must not exist after a failed seed, got err=%v", err)
	}

	target := findTargetByID(t, mustList(t, newReadinessService(db), ctx), targetID)
	if target.AvailableActions.Run {
		t.Fatal("mismatched target must not expose run=true")
	}
	if target.Readiness == model.ReadinessReady {
		t.Fatalf("readiness = ready; a mismatched-credential target must not be ready")
	}

	// Execution is rejected for the non-runnable target.
	if _, err := newExecutionService(db).Execute(ctx, ownerDBA, targetID, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10}); err == nil {
		t.Fatal("Execute expected to be rejected for a non-runnable target, got nil")
	}
}

// mustList lists all query targets through a service, failing the test on error.
func mustList(t *testing.T, svc *service.QueryTargetService, ctx context.Context) []model.QueryTarget {
	t.Helper()
	targets, _, err := svc.List(ctx, model.QueryTargetListQuery{})
	if err != nil {
		t.Fatalf("list query targets: %v", err)
	}
	return targets
}

// assertCredentialRowStoresNoDSN reads the credential metadata row for a target
// and asserts exactly one row exists whose stored columns carry no DSN-looking
// value and do not contain the real DSN. WHY: the seed path's central security
// invariant is that the DSN/password is never persisted — only the opaque ref.
func assertCredentialRowStoresNoDSN(t *testing.T, db *sql.DB, resourceID uint64, dsn string) {
	t.Helper()
	rows, err := db.Query(
		`select resource_id, engine, credential_ref, enabled, environment_policy from query_target_credentials where resource_id = ?`,
		resourceID,
	)
	if err != nil {
		t.Fatalf("query credential row: %v", err)
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		count++
		var (
			gotResource uint64
			engine      string
			ref         string
			enabled     bool
			policy      string
		)
		if err := rows.Scan(&gotResource, &engine, &ref, &enabled, &policy); err != nil {
			t.Fatalf("scan credential row: %v", err)
		}
		for _, val := range []string{engine, ref, policy} {
			if val == dsn {
				t.Fatalf("stored column equals the DSN: %q", val)
			}
			// A MySQL DSN carries user:pass@tcp(host:port); any of these markers
			// in a metadata column would indicate a leaked DSN fragment.
			if strings.Contains(val, "tcp(") || strings.Contains(val, "://") || strings.Contains(val, "@") {
				t.Fatalf("stored column %q looks like a DSN fragment (contains tcp(/:// /@)", val)
			}
		}
		if gotResource != resourceID {
			t.Fatalf("credential row resource_id = %d, want %d", gotResource, resourceID)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate credential rows: %v", err)
	}
	if count != 1 {
		t.Fatalf("credential rows = %d, want exactly 1 (idempotent seed must not duplicate)", count)
	}
}
