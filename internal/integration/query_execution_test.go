//go:build integration

// Package integration provides Testcontainers-backed tests for the Phase 37
// read-only query sandbox (repository, service end-to-end, audit/history).
// input: shared MySQL fixture, query repositories/services, credential environment, governed requests
// output: truthful user-attributed query execution, history, audit, paging, disclosure, and failure tests
// pos: Real-MySQL end-to-end boundary for governed query execution
// note: if this file changes, update this header and module README.md.
package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// seedCredentialRow is a test-only fixture that inserts a credential metadata
// row directly. Phase 37 has no credential write API, so product code never
// inserts these rows; this helper exists solely to exercise repository reads and
// the readiness/execute paths against realistic data.
func seedCredentialRow(t *testing.T, db *sql.DB, resourceID uint64, engine, ref string, enabled bool, policy string) {
	t.Helper()
	_, err := db.Exec(
		`insert into query_target_credentials (resource_id, engine, credential_ref, enabled, environment_policy) values (?, ?, ?, ?, ?)`,
		resourceID, engine, ref, enabled, policy,
	)
	if err != nil {
		t.Fatalf("seed credential row for resource %d: %v", resourceID, err)
	}
}

// seedExecutionHistoryRow inserts one execution-history row directly for
// history-list test fixtures. It intentionally writes NO audit event: these
// fixtures exercise execution-history listing only. Governed execution and
// navigation never use this SQL seam — every Evidence-Bearing Query Attempt
// writes through the repository-owned atomic pair (Issue #36 deleted the
// standalone repository write).
func seedExecutionHistoryRow(t *testing.T, db *sql.DB, rec model.QueryExecutionRecord) (uint64, error) {
	t.Helper()
	var createdAt any
	if !rec.CreatedAt.IsZero() {
		createdAt = rec.CreatedAt.UTC()
	}
	res, err := db.Exec(
		`insert into query_executions
		 (target_resource_id, actor_user_id, engine, statement_digest, statement_preview, status, row_count, duration_ms, error_code, error_message, created_at)
		 values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, COALESCE(?, CURRENT_TIMESTAMP(6)))`,
		rec.TargetResourceID, rec.ActorUserID, rec.Engine, rec.StatementDigest, rec.StatementPreview,
		string(rec.Status), rec.RowCount, rec.DurationMs, rec.ErrorCode, rec.ErrorMessage, createdAt,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return uint64(id), nil
}

// createQueryTargetResource creates a database_instance resource and returns its
// id so a credential row can be attached to it.
func createQueryTargetResource(t *testing.T, db *sql.DB, namePrefix string) uint64 {
	t.Helper()
	repo := mysql.NewResourceRepository(db)
	ctx := context.Background()
	res, err := repo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            namePrefix,
		DisplayName:     namePrefix,
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create query target resource: %v", err)
	}
	return res.ID
}

func TestQueryExecutionRepository_InsertAndListByTarget(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-insert-list")

	for i, status := range []model.QueryExecutionStatus{
		model.QueryExecutionSuccess,
		model.QueryExecutionRejected,
	} {
		if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  "select ?",
			StatementPreview: "select 1",
			Status:           status,
			RowCount:         i,
			DurationMs:       int64(i + 1),
		}); err != nil {
			t.Fatalf("insert execution %d: %v", i, err)
		}
	}

	items, total, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{TargetResourceID: targetID, Page: 1, PageSize: 20, Mode: model.PaginationModeOffset})
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if total != 2 {
		t.Fatalf("total = %d, want 2", total)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	// WHY: history is newest-first so operators see the latest attempt first.
	if items[0].RowCount < items[1].RowCount {
		t.Fatalf("history not newest-first: items[0].RowCount=%d items[1].RowCount=%d", items[0].RowCount, items[1].RowCount)
	}
	if items[0].Status != model.QueryExecutionRejected {
		t.Fatalf("newest status = %q, want rejected", items[0].Status)
	}
	if items[0].TargetResourceID != targetID || items[0].ActorUserID != ownerDBA {
		t.Fatalf("history record mismatched ids: %+v", items[0])
	}
}

func TestQueryExecutionRepository_CredentialMetadataReported(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-cred-ready")

	// An enabled credential with a valid ref and non_prod_only policy is the
	// data that lets the service mark a non-production target ready.
	seedCredentialRow(t, db, targetID, "mysql", "ORDER_MYSQL_RO", true, string(model.QueryEnvPolicyNonProdOnly))

	got, err := repo.GetCredentialByResourceID(ctx, targetID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if !got.Enabled {
		t.Fatalf("Enabled = false, want true (ready credential)")
	}
	if got.CredentialRef != "ORDER_MYSQL_RO" {
		t.Fatalf("CredentialRef = %q, want ORDER_MYSQL_RO", got.CredentialRef)
	}
	if got.EnvironmentPolicy != model.QueryEnvPolicyNonProdOnly {
		t.Fatalf("EnvironmentPolicy = %q, want non_prod_only", got.EnvironmentPolicy)
	}
	if got.Engine != "mysql" || got.ResourceID != targetID {
		t.Fatalf("credential metadata mismatched: %+v", got)
	}
}

func TestQueryExecutionRepository_DisabledCredentialReported(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-cred-disabled")

	// A disabled credential reads back with Enabled=false; the service treats
	// that as locked. The repo reports the flag faithfully.
	seedCredentialRow(t, db, targetID, "mysql", "ORDER_MYSQL_RO", false, string(model.QueryEnvPolicyDisabled))

	got, err := repo.GetCredentialByResourceID(ctx, targetID)
	if err != nil {
		t.Fatalf("get credential: %v", err)
	}
	if got.Enabled {
		t.Fatalf("Enabled = true, want false (disabled credential must stay locked)")
	}
	if got.EnvironmentPolicy != model.QueryEnvPolicyDisabled {
		t.Fatalf("EnvironmentPolicy = %q, want disabled", got.EnvironmentPolicy)
	}
}

func TestQueryExecutionRepository_InvalidCredentialRefFailsClosed(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-cred-badref")

	// WHY: an invalid credential_ref in the metadata must never reach the
	// resolver/env lookup. The repo validates on read and fails closed (returns
	// an error, distinct from a simple not-found), keeping the target locked.
	seedCredentialRow(t, db, targetID, "mysql", "bad-ref", true, string(model.QueryEnvPolicyAllEnvironments))

	_, err := repo.GetCredentialByResourceID(ctx, targetID)
	if err == nil {
		t.Fatal("expected error for invalid credential_ref, got nil")
	}
	if errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("invalid credential_ref must fail closed with a validation error, not sql.ErrNoRows: %v", err)
	}
}

func TestQueryExecutionRepository_RoundTripsTypedEnvironmentPolicy(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()

	for _, tc := range []struct {
		name   string
		policy model.QueryEnvironmentPolicy
	}{
		{"disabled", model.QueryEnvPolicyDisabled},
		{"non_prod_only", model.QueryEnvPolicyNonProdOnly},
		{"all_environments", model.QueryEnvPolicyAllEnvironments},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targetID := createQueryTargetResource(t, db, "qe-policy-"+tc.name)
			seedCredentialRow(t, db, targetID, "mysql", "ORDER_MYSQL_RO", true, string(tc.policy))

			got, err := repo.GetCredentialByResourceID(ctx, targetID)
			if err != nil {
				t.Fatalf("get credential: %v", err)
			}
			if got.EnvironmentPolicy != tc.policy {
				t.Fatalf("EnvironmentPolicy = %q, want %q", got.EnvironmentPolicy, tc.policy)
			}
			if err := got.EnvironmentPolicy.Validate(); err != nil {
				t.Fatalf("round-tripped policy failed Validate: %v", err)
			}
		})
	}
}

func TestQueryExecutionRepository_MissingCredentialReturnsNotFound(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-cred-missing")

	_, err := repo.GetCredentialByResourceID(ctx, targetID)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing credential error = %v, want sql.ErrNoRows", err)
	}
}

func TestQueryExecutionRepository_InsertAuditEvent(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "qe-audit")

	if err := repo.InsertAuditEvent(ctx, ownerDBA, targetID, "query.executed", "success"); err != nil {
		t.Fatalf("insert audit event: %v", err)
	}

	var eventType, result string
	err := db.QueryRow(
		`select event_type, result from audit_events where target_resource_id = ? order by id desc limit 1`,
		targetID,
	).Scan(&eventType, &result)
	if err != nil {
		t.Fatalf("read back audit event: %v", err)
	}
	if eventType != "query.executed" || result != "success" {
		t.Fatalf("audit event = (%q,%q), want (query.executed,success)", eventType, result)
	}
}

// --- Phase 37 end-to-end execution tests (service + real MySQL executor) ---

const sandboxCredentialRef = "SANDBOX_TARGET"

// setupQuerySandboxTarget provisions a ready mysql/staging query target whose
// credential_ref resolves back to the disposable test MySQL, plus a fixture
// table the sandbox can safely SELECT. It returns the wired service and the
// target resource id.
func setupQuerySandboxTarget(t *testing.T) (*service.QueryExecutionService, uint64, *sql.DB) {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()

	// Self-contained fixture table owned by the test (never ControlHub seed data).
	mustExec(t, db, `drop table if exists qe_sandbox_fixtures`)
	mustExec(t, db, `create table qe_sandbox_fixtures (id bigint unsigned not null primary key, name varchar(64) not null)`)
	values := make([]string, 0, 25)
	args := make([]any, 0, 50)
	for id := 1; id <= 25; id++ {
		values = append(values, "(?, ?)")
		args = append(args, id, fmt.Sprintf("fixture-%02d", id))
	}
	mustExec(t, db, "insert into qe_sandbox_fixtures (id, name) values "+strings.Join(values, ","), args...)

	// Target resource (mysql, staging) + its connection profile (host/port).
	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qe-sandbox-target-" + strings.ReplaceAll(t.Name(), "/", "-"),
		DisplayName:     "Query Sandbox Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create sandbox target resource: %v", err)
	}
	// The target's connection profile host/port must match the DSN the
	// credential resolves to (the disposable container), otherwise Phase 37's
	// credential-binding check correctly rejects the execution.
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
	mustExec(t, db, `insert into resource_profiles_database_instance (resource_id, engine, version, host, port, role, spec) values (?, 'mysql', '8.0', ?, ?, 'primary', '{}')`, res.ID, dsnHost, dsnPort)

	// Enabled credential allowing non-production execution.
	seedCredentialRow(t, db, res.ID, "mysql", sandboxCredentialRef, true, string(model.QueryEnvPolicyNonProdOnly))

	// Resolve the credential_ref back to the disposable test MySQL DSN.
	if err := os.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+sandboxCredentialRef, globalEnv.dsn); err != nil {
		t.Fatalf("set credential env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("CONTROLHUB_QUERY_CREDENTIAL_" + sandboxCredentialRef) })

	// Seed disclosure policies for the fixture table so the Phase 38Q
	// fail-closed check allows queries against it.
	seedDisclosurePolicies(t, db, res.ID, dsnCfg.DBName, "qe_sandbox_fixtures", "id", "name")

	svc := service.NewQueryExecutionService(
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
	return svc, res.ID, db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// testDBName returns the database name parsed from the shared test DSN.
func testDBName(t *testing.T) string {
	t.Helper()
	cfg, err := mysqldriver.ParseDSN(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse test dsn for db name: %v", err)
	}
	return cfg.DBName
}

// seedDisclosurePolicies inserts raw_copy_allowed disclosure policies for the
// given table columns. Required by the Phase 38Q fail-closed disclosure check:
// any query projection without an exact policy row is blocked before SQL executes.
func seedDisclosurePolicies(t *testing.T, db *sql.DB, targetResourceID uint64, database, object string, columns ...string) {
	t.Helper()
	repo := mysql.NewQueryDisclosureRepository(db)
	ctx := context.Background()
	for _, col := range columns {
		_, err := repo.Insert(ctx, model.ResultDisclosurePolicyUpsertRequest{
			TargetResourceID: targetResourceID,
			DatabaseName:     database,
			ObjectName:       object,
			ColumnName:       col,
			Mode:             model.ResultDisclosureRawCopyAllowed,
		})
		if err != nil {
			t.Fatalf("seed disclosure policy for %s.%s.%s: %v", database, object, col, err)
		}
	}
}

func fixtureRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`select count(*) from qe_sandbox_fixtures`).Scan(&n); err != nil {
		t.Fatalf("count fixtures: %v", err)
	}
	return n
}

func TestQueryExecution_SelectOneReturnsRows(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	resp, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "select id, name from qe_sandbox_fixtures where id = 1",
		MaxRows:   10,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.RowCount != 1 {
		t.Fatalf("rowCount = %d, want 1", resp.RowCount)
	}
	if len(resp.Rows) != 1 || len(resp.Rows[0]) != 2 {
		t.Fatalf("rows shape = %v, want 1 row x 2 cols", resp.Rows)
	}
	if id := fmt.Sprintf("%v", resp.Rows[0][0]); id != "1" {
		t.Fatalf("row id = %v (%T), want numeric 1", resp.Rows[0][0], resp.Rows[0][0])
	}
	if resp.Engine != "mysql" {
		t.Fatalf("engine = %q, want mysql", resp.Engine)
	}
}

func TestQueryExecution_BlockedWriteDoesNotMutate(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	before := fixtureRowCount(t, db)

	for _, stmt := range []string{
		"delete from qe_sandbox_fixtures",
		"update qe_sandbox_fixtures set name = 'x'",
		"insert into qe_sandbox_fixtures (id, name) values (999, 'x')",
		"truncate table qe_sandbox_fixtures",
	} {
		_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: stmt, MaxRows: 10})
		if !errors.Is(err, service.ErrQueryValidationFailed) {
			t.Fatalf("Execute(%q) error = %v, want ErrQueryValidationFailed", stmt, err)
		}
	}

	// WHY: the sandbox is read-only — a blocked write must leave the data intact.
	if after := fixtureRowCount(t, db); after != before {
		t.Fatalf("fixture row count changed: before=%d after=%d (write must not mutate)", before, after)
	}
}

func TestQueryExecution_MultiStatementRejected(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	before := fixtureRowCount(t, db)

	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "select 1; delete from qe_sandbox_fixtures",
		MaxRows:   10,
	})
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("multi-statement error = %v, want ErrQueryValidationFailed", err)
	}
	if after := fixtureRowCount(t, db); after != before {
		t.Fatalf("multi-statement mutated data: before=%d after=%d", before, after)
	}
}

func TestQueryExecution_LimitCapsRows(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	resp, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "select id from qe_sandbox_fixtures order by id",
		MaxRows:   2,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	// WHY: 3 fixture rows exist; a maxRows of 2 must return 2 rows and flag
	// truncation so the client knows the result was bounded.
	if resp.RowCount != 2 {
		t.Fatalf("rowCount = %d, want 2 (capped)", resp.RowCount)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true (more rows existed than the cap)")
	}
	if resp.LimitApplied != 2 {
		t.Fatalf("limitApplied = %d, want 2", resp.LimitApplied)
	}
}

func TestQueryExecution_HistoryWrittenForSuccessAndRejection(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	repo := mysql.NewQueryExecutionRepository(db)

	if _, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "select id from qe_sandbox_fixtures limit 1", MaxRows: 10}); err != nil {
		t.Fatalf("success Execute: %v", err)
	}
	if _, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "delete from qe_sandbox_fixtures", MaxRows: 10}); err == nil {
		t.Fatal("expected rejection for write statement")
	}

	items, total, err := repo.ListExecutions(context.Background(), model.QueryExecutionListQuery{TargetResourceID: targetID, Page: 1, PageSize: 20, Mode: model.PaginationModeOffset})
	if err != nil {
		t.Fatalf("list executions: %v", err)
	}
	if total != 2 {
		t.Fatalf("total history rows = %d, want 2 (success + rejection)", total)
	}
	// WHY: every attempt must be recorded — a rejected attempt still leaves a
	// history trail for audit, not just successes.
	statuses := map[model.QueryExecutionStatus]bool{}
	for _, item := range items {
		statuses[item.Status] = true
	}
	if !statuses[model.QueryExecutionSuccess] || !statuses[model.QueryExecutionRejected] {
		t.Fatalf("history missing success/rejected: items=%+v", items)
	}
}

func TestQueryExecution_AuditEventWrittenForSuccessAndRejection(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)

	if _, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "select id from qe_sandbox_fixtures limit 1", MaxRows: 10}); err != nil {
		t.Fatalf("success Execute: %v", err)
	}
	if _, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{Statement: "delete from qe_sandbox_fixtures", MaxRows: 10}); err == nil {
		t.Fatal("expected rejection for write statement")
	}

	rows, err := db.Query(`select result from audit_events where target_resource_id = ? and event_type = 'query.executed'`, targetID)
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}
	defer rows.Close()
	results := map[string]bool{}
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			t.Fatalf("scan audit result: %v", err)
		}
		results[r] = true
	}
	// WHY: both outcomes emit a query.executed audit event, distinguished by
	// result, so the audit stream records every execution attempt.
	if !results["success"] || !results["validation_failed"] {
		t.Fatalf("audit events = %v, want both success and validation_failed", results)
	}
}

// --- Finding 3: SQL NULL must surface as JSON null (not a forged 0) ---

func TestQueryExecution_NullPreservedAsJSONNull(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	// A fixture table with nullable columns holding both NULL and real values.
	mustExec(t, db, `drop table if exists qe_null_fixtures`)
	mustExec(t, db, `create table qe_null_fixtures (id bigint unsigned not null primary key, n bigint null, label varchar(64) null)`)
	mustExec(t, db, `insert into qe_null_fixtures (id, n, label) values (1, NULL, NULL), (2, 42, 'real')`)
	seedDisclosurePolicies(t, db, targetID, testDBName(t), "qe_null_fixtures", "id", "n", "label")

	resp, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "select n, label from qe_null_fixtures order by id",
		MaxRows:   10,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.RowCount != 2 {
		t.Fatalf("rowCount = %d, want 2", resp.RowCount)
	}
	// WHY: SQL NULL must serialize as JSON null, never a forged numeric 0 or empty
	// string — otherwise the sandbox would silently misrepresent data.
	nullN, nullLabel := resp.Rows[0][0], resp.Rows[0][1]
	if nullN != nil {
		t.Fatalf("NULL bigint cell = %v (%T), want nil", nullN, nullN)
	}
	if nullLabel != nil {
		t.Fatalf("NULL varchar cell = %v (%T), want nil", nullLabel, nullLabel)
	}
	// Non-null numeric must stay a JSON number, not a string.
	realN, realLabel := resp.Rows[1][0], resp.Rows[1][1]
	if n, ok := realN.(int64); !ok || n != 42 {
		t.Fatalf("non-null bigint cell = %v (%T), want int64(42)", realN, realN)
	}
	if realLabel != "real" {
		t.Fatalf("non-null varchar cell = %v, want \"real\"", realLabel)
	}
}

func TestQueryExecution_CastNullAsSignedIsNil(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)

	// Use a nullable column from a seeded table instead of a bare expression
	// (expressions are correctly blocked by the fail-closed disclosure policy).
	mustExec(t, db, `drop table if exists qe_null_fixtures`)
	mustExec(t, db, `create table qe_null_fixtures (id bigint unsigned not null primary key, n bigint null, label varchar(64) null)`)
	mustExec(t, db, `insert into qe_null_fixtures (id, n, label) values (1, NULL, NULL)`)
	seedDisclosurePolicies(t, db, targetID, testDBName(t), "qe_null_fixtures", "id", "n", "label")

	resp, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "select n from qe_null_fixtures where id = 1",
		MaxRows:   10,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.RowCount != 1 || len(resp.Rows[0]) != 1 {
		t.Fatalf("expected 1 row x 1 col, got rowCount=%d row0=%v", resp.RowCount, resp.Rows)
	}
	if got := resp.Rows[0][0]; got != nil {
		t.Fatalf("NULL bigint column = %v (%T), want nil", got, got)
	}
}

// --- zero-row contract: rows must be [] not null ---

func TestQueryExecution_ZeroRowsReturnsEmptyArray(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: a valid SELECT that matches zero rows must return rows:[] (empty
	// JSON array), not rows:null. The frontend contract depends on this to
	// safely call .length on the rows array without null checks.
	resp, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "select id, name from qe_sandbox_fixtures where 1 = 0",
		MaxRows:   10,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.RowCount != 0 {
		t.Fatalf("rowCount = %d, want 0", resp.RowCount)
	}
	// WHY: a nil slice serializes as JSON null; the contract requires [].
	if resp.Rows == nil {
		t.Fatal("rows = nil, want non-nil empty slice (JSON [] not null)")
	}
	if len(resp.Rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0", len(resp.Rows))
	}
}

// --- Phase 38C: read-only metadata statement execution tests ---

func TestQueryExecution_ShowTablesBlockedByDisclosure(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: SHOW TABLES is a metadata command that produces no resolvable
	// columns for disclosure governance. Phase 38Q's fail-closed disclosure
	// check blocks it before execution.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "show tables",
		MaxRows:   100,
	})
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("Execute(show tables) error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestQueryExecution_DescribeTableBlockedByDisclosure(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: DESCRIBE is a metadata command blocked by Phase 38Q's fail-closed
	// disclosure check (no resolvable columns).
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "describe qe_sandbox_fixtures",
		MaxRows:   100,
	})
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("Execute(describe) error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestQueryExecution_ExplainSelectBlockedByDisclosure(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: EXPLAIN SELECT is blocked by Phase 38Q's fail-closed disclosure
	// check. EXPLAIN output columns (id, select_type, table) are not
	// resolvable against disclosure policies.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "explain select * from qe_sandbox_fixtures",
		MaxRows:   100,
	})
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("Execute(explain) error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestQueryExecution_UpdateRemainsRejected(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: writes must remain rejected even after the guard widening.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "update qe_sandbox_fixtures set name = 'x'",
		MaxRows:   10,
	})
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("Execute(update) error = %v, want ErrQueryValidationFailed", err)
	}
}

// --- Phase 38D: read-only database metadata exploration tests ---

// setupQueryE2EDatabase creates a dedicated query_e2e database with a
// query_e2e_items table in the disposable MySQL container. This fixture is
// separate from the sandbox target's qe_sandbox_fixtures table so
// cross-database metadata queries can be tested. It uses the raw test DB
// connection because CREATE DATABASE is intentionally rejected by the query
// guard.
func setupQueryE2EDatabase(t *testing.T) {
	t.Helper()
	db := setupTestDB(t)
	mustExec(t, db, "CREATE DATABASE IF NOT EXISTS query_e2e")
	mustExec(t, db, "DROP TABLE IF EXISTS query_e2e.query_e2e_items")
	mustExec(t, db, "CREATE TABLE query_e2e.query_e2e_items (id BIGINT UNSIGNED NOT NULL PRIMARY KEY, name VARCHAR(64) NOT NULL)")
	mustExec(t, db, "INSERT INTO query_e2e.query_e2e_items (id, name) VALUES (1,'alpha'),(2,'beta')")
}

func TestQueryExecution_ShowDatabasesBlockedByDisclosure(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)
	setupQueryE2EDatabase(t)

	// WHY: SHOW DATABASES is a metadata command blocked by Phase 38Q's
	// fail-closed disclosure check.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "show databases",
		MaxRows:   100,
	})
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("Execute(show databases) error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestQueryExecution_ShowTablesFromDatabaseBlockedByDisclosure(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)
	setupQueryE2EDatabase(t)

	// WHY: SHOW TABLES FROM is a metadata command blocked by Phase 38Q.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "show tables from query_e2e",
		MaxRows:   100,
	})
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("Execute(show tables from) error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestQueryExecution_ShowColumnsBlockedByDisclosure(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)
	setupQueryE2EDatabase(t)

	// WHY: SHOW COLUMNS FROM is a metadata command blocked by Phase 38Q.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "show columns from query_e2e.query_e2e_items",
		MaxRows:   100,
	})
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("Execute(show columns) error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestQueryExecution_DescribeQualifiedTableBlockedByDisclosure(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)
	setupQueryE2EDatabase(t)

	// WHY: DESCRIBE <db>.<table> is a metadata command blocked by Phase 38Q.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "describe query_e2e.query_e2e_items",
		MaxRows:   100,
	})
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("Execute(describe qualified) error = %v, want ErrQueryNotAllowed", err)
	}
}

func TestQueryExecution_ShowProcesslistRemainsRejected(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: SHOW PROCESSLIST exposes all connected sessions — not appropriate
	// for a read-only sandbox.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "show processlist",
		MaxRows:   10,
	})
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("Execute(show processlist) error = %v, want ErrQueryValidationFailed", err)
	}
}

func TestQueryExecution_ShowGrantsRemainsRejected(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: SHOW GRANTS exposes privilege information — not appropriate for a
	// read-only sandbox.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "show grants",
		MaxRows:   10,
	})
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("Execute(show grants) error = %v, want ErrQueryValidationFailed", err)
	}
}

func TestQueryExecution_UseDatabaseRemainsRejected(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: USE changes the session database context — a session mutation that
	// must be rejected in a read-only sandbox.
	_, err := svc.Execute(context.Background(), queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "use query_e2e",
		MaxRows:   10,
	})
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("Execute(use query_e2e) error = %v, want ErrQueryValidationFailed", err)
	}
}

// TestQueryExecutionRepository_ActorProjectionAndScope proves LEFT JOIN users
// display names, Unknown user fallback, admin-all vs non-admin-own filtering.
func TestQueryExecutionRepository_ActorProjectionAndScope(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "hist-scope-target")
	// Seed two actors: ownerDBA (seeded user) and a synthetic orphan actor id with no users row.
	adminID := ownerDBA
	orphanID := uint64(9_000_001)
	repo := mysql.NewQueryExecutionRepository(db)
	if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
		TargetResourceID: targetID, ActorUserID: adminID, Engine: "mysql",
		StatementDigest: "select 1", StatementPreview: "select 1",
		Status: model.QueryExecutionSuccess, RowCount: 1,
	}); err != nil {
		t.Fatalf("insert admin: %v", err)
	}
	if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
		TargetResourceID: targetID, ActorUserID: orphanID, Engine: "mysql",
		StatementDigest: "select 2", StatementPreview: "select 2",
		Status: model.QueryExecutionSuccess, RowCount: 1,
	}); err != nil {
		t.Fatalf("insert orphan: %v", err)
	}

	all, total, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{TargetResourceID: targetID, Page: 1, PageSize: 20, Mode: model.PaginationModeOffset})
	if err != nil {
		t.Fatalf("list all: %v", err)
	}
	if total != 2 || len(all) != 2 {
		t.Fatalf("all total/len = %d/%d, want 2/2", total, len(all))
	}
	// Admin seed display_name is "ControlHub Admin"
	var sawAdmin, sawUnknown bool
	for _, item := range all {
		if item.ActorUserID == adminID {
			sawAdmin = true
			if item.Actor.DisplayName == "" || item.Actor.DisplayName == model.UnknownHistoryActorDisplayName {
				t.Fatalf("admin displayName = %q, want real name", item.Actor.DisplayName)
			}
		}
		if item.ActorUserID == orphanID {
			sawUnknown = true
			if item.Actor.DisplayName != model.UnknownHistoryActorDisplayName {
				t.Fatalf("orphan displayName = %q, want Unknown user", item.Actor.DisplayName)
			}
		}
	}
	if !sawAdmin || !sawUnknown {
		t.Fatalf("missing projected actors: admin=%v unknown=%v items=%+v", sawAdmin, sawUnknown, all)
	}

	own, ownTotal, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 20, ActorUserID: &adminID,
		Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list own: %v", err)
	}
	if ownTotal != 1 || len(own) != 1 || own[0].ActorUserID != adminID {
		t.Fatalf("own scope = total=%d items=%+v, want only admin", ownTotal, own)
	}
}

// --- cursor-based pagination integration tests ---

func TestQueryExecutionRepository_CursorPagination(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "cursor-page-target")

	baseTime := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  fmt.Sprintf("select %d", i),
			StatementPreview: fmt.Sprintf("select %d", i),
			Status:           model.QueryExecutionSuccess,
			RowCount:         i,
			CreatedAt:        baseTime.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("insert execution %d: %v", i, err)
		}
	}

	// First page: explicit offset mode (Page=1) for legacy callers.
	page1, total, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 2,
		Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if total != 5 {
		t.Fatalf("total = %d, want 5", total)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 items = %d, want 2", len(page1))
	}

	// Encode cursor from last item in page1.
	last := page1[len(page1)-1]
	cursor, err := model.EncodeCursor(last.CreatedAt, last.ID, "test-hash")
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	// Second page: cursor mode (keyset), skip COUNT.
	page2, cursorTotal, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, PageSize: 2, Cursor: &cursor,
		Mode: model.PaginationModeCursor,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if cursorTotal != 0 {
		t.Fatalf("cursor mode total = %d, want 0 (COUNT skipped)", cursorTotal)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 items = %d, want 2", len(page2))
	}

	// Ensure no overlap between page1 and page2.
	page1IDs := map[uint64]bool{}
	for _, item := range page1 {
		page1IDs[item.ID] = true
	}
	for _, item := range page2 {
		if page1IDs[item.ID] {
			t.Fatalf("page2 item ID=%d overlaps with page1", item.ID)
		}
	}
}

// TestQueryExecutionRepository_CursorInitial_NoPageNoCursor proves P1: the
// first cursor page (no Page, no Cursor) uses keyset mode with no boundary
// predicate, fetches PageSize+1, never COUNT, never OFFSET, and returns the
// newest rows in (created_at DESC, id DESC) order with a nextCursor when more
// rows exist. This test FAILS on the old candidate which set Page=1 and fell
// back to offset mode.
func TestQueryExecutionRepository_CursorInitial_NoPageNoCursor(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "cursor-initial-target")

	baseTime := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  fmt.Sprintf("select %d", i),
			StatementPreview: fmt.Sprintf("select %d", i),
			Status:           model.QueryExecutionSuccess,
			RowCount:         i,
			CreatedAt:        baseTime.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("insert execution %d: %v", i, err)
		}
	}

	// WHY: cursor-initial must use keyset mode (Mode=Cursor) with no boundary
	// predicate. Requesting PageSize+1 lets the caller (service) detect a next
	// page without running COUNT.
	items, total, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID,
		PageSize:         3, // request 3 (service would request pageSize+1=3 for pageSize=2)
		Mode:             model.PaginationModeCursor,
		// Cursor intentionally nil: first page has no boundary predicate.
	})
	if err != nil {
		t.Fatalf("cursor-initial: %v", err)
	}

	// WHY: cursor mode must never run COUNT — total stays 0.
	if total != 0 {
		t.Fatalf("cursor-initial total = %d, want 0 (COUNT must be skipped)", total)
	}

	// We requested 3 rows and 5 exist, so we must get all 3.
	if len(items) != 3 {
		t.Fatalf("cursor-initial items = %d, want 3", len(items))
	}

	// WHY: ordering must be created_at DESC, id DESC so the keyset cursor
	// (created_at, id) is deterministic and the newest row comes first.
	for i := 1; i < len(items); i++ {
		if items[i].CreatedAt.After(items[i-1].CreatedAt) {
			t.Fatalf("cursor-initial not ordered DESC by created_at: items[%d]=%v > items[%d]=%v",
				i, items[i].CreatedAt, i-1, items[i-1].CreatedAt)
		}
		if items[i].CreatedAt.Equal(items[i-1].CreatedAt) && items[i].ID >= items[i-1].ID {
			t.Fatalf("cursor-initial same-ts not ordered DESC by id: items[%d].ID=%d >= items[%d].ID=%d",
				i, items[i].ID, i-1, items[i-1].ID)
		}
	}

	// The newest row must be the one with the latest created_at (i=4).
	if items[0].CreatedAt != baseTime.Add(4*time.Second) {
		t.Fatalf("cursor-initial first item = %v, want newest %v", items[0].CreatedAt, baseTime.Add(4*time.Second))
	}
}

// TestQueryExecutionRepository_CursorContinuation proves P1: a continuation
// page uses the strictly-older (created_at, id) predicate and returns the next
// batch of rows with no overlap with page 1.
func TestQueryExecutionRepository_CursorContinuation(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "cursor-cont-target")

	baseTime := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 5; i++ {
		if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  fmt.Sprintf("select %d", i),
			StatementPreview: fmt.Sprintf("select %d", i),
			Status:           model.QueryExecutionSuccess,
			RowCount:         i,
			CreatedAt:        baseTime.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("insert execution %d: %v", i, err)
		}
	}

	// Page 1: cursor-initial, fetch 2 rows.
	page1, _, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, PageSize: 2, Mode: model.PaginationModeCursor,
	})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 items = %d, want 2", len(page1))
	}

	// Encode cursor from last row of page 1.
	last := page1[len(page1)-1]
	cursor, err := model.EncodeCursor(last.CreatedAt, last.ID, "test-hash")
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}

	// Page 2: continuation using strictly-older predicate.
	page2, total2, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, PageSize: 2, Cursor: &cursor,
		Mode: model.PaginationModeCursor,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if total2 != 0 {
		t.Fatalf("page2 total = %d, want 0 (COUNT skipped)", total2)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 items = %d, want 2", len(page2))
	}

	// WHY: every page2 row must be strictly older than the cursor (last row of
	// page1) in the (created_at, id) keyset ordering.
	for _, item := range page2 {
		if item.CreatedAt.After(last.CreatedAt) {
			t.Fatalf("page2 item %d is newer than cursor in created_at", item.ID)
		}
		if item.CreatedAt.Equal(last.CreatedAt) && item.ID >= last.ID {
			t.Fatalf("page2 item %d is not strictly older than cursor in id", item.ID)
		}
	}

	// No overlap between page1 and page2.
	page1IDs := map[uint64]bool{}
	for _, item := range page1 {
		page1IDs[item.ID] = true
	}
	for _, item := range page2 {
		if page1IDs[item.ID] {
			t.Fatalf("page2 item ID=%d overlaps with page1", item.ID)
		}
	}
}

// TestQueryExecutionRepository_CursorNewerInsertBetweenPages proves P1: if a
// newer row is inserted between fetching page1 and page2, cursor continuation
// still returns the correct older rows (keyset is stable under inserts newer
// than the cursor). The newer row is NOT pulled into page2.
func TestQueryExecutionRepository_CursorNewerInsertBetweenPages(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "cursor-newer-insert-target")

	baseTime := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 4; i++ {
		if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  fmt.Sprintf("select %d", i),
			StatementPreview: fmt.Sprintf("select %d", i),
			Status:           model.QueryExecutionSuccess,
			RowCount:         i,
			CreatedAt:        baseTime.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("insert execution %d: %v", i, err)
		}
	}

	// Page 1: fetch 2 rows (the two newest).
	page1, _, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, PageSize: 2, Mode: model.PaginationModeCursor,
	})
	if err != nil {
		t.Fatalf("page1: %v", err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 items = %d, want 2", len(page1))
	}

	// Insert a newer row AFTER page1 was fetched.
	newerTime := baseTime.Add(10 * time.Second)
	if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
		TargetResourceID: targetID,
		ActorUserID:      ownerDBA,
		Engine:           "mysql",
		StatementDigest:  "select newer",
		StatementPreview: "select newer",
		Status:           model.QueryExecutionSuccess,
		RowCount:         99,
		CreatedAt:        newerTime,
	}); err != nil {
		t.Fatalf("insert newer execution: %v", err)
	}

	// Page 2: continuation using cursor from page1's last row.
	last := page1[len(page1)-1]
	cursor, err := model.EncodeCursor(last.CreatedAt, last.ID, "test-hash")
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	page2, _, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, PageSize: 2, Cursor: &cursor,
		Mode: model.PaginationModeCursor,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}

	// WHY: the newer insert must NOT appear in page2 — keyset continuation
	// returns rows strictly older than the cursor, regardless of newer inserts.
	for _, item := range page2 {
		if item.CreatedAt.After(last.CreatedAt) {
			t.Fatalf("page2 contains newer insert (id=%d, created_at=%v) that should not be returned",
				item.ID, item.CreatedAt)
		}
		if item.CreatedAt.Equal(newerTime) {
			t.Fatalf("page2 contains the newer insert row (id=%d)", item.ID)
		}
	}

	// page2 must contain the remaining 2 original rows (i=0, i=1).
	if len(page2) != 2 {
		t.Fatalf("page2 items = %d, want 2", len(page2))
	}
}

func TestQueryExecutionRepository_CursorStatusFilter(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "cursor-status-target")

	baseTime := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	for i, status := range []model.QueryExecutionStatus{
		model.QueryExecutionSuccess,
		model.QueryExecutionRejected,
		model.QueryExecutionSuccess,
	} {
		if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  fmt.Sprintf("select %d", i),
			StatementPreview: fmt.Sprintf("select %d", i),
			Status:           status,
			CreatedAt:        baseTime.Add(time.Duration(i) * time.Second),
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	successStatus := model.QueryExecutionSuccess
	items, _, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 20, Status: &successStatus,
		Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("success items = %d, want 2", len(items))
	}
	for _, item := range items {
		if item.Status != model.QueryExecutionSuccess {
			t.Fatalf("item status = %q, want success", item.Status)
		}
	}
}

func TestQueryExecutionRepository_CursorTimeFilter(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "cursor-time-target")

	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 6, 22, 0, 0, 0, 0, time.UTC)
	for i, ts := range []time.Time{t1, t2, t3} {
		if _, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  fmt.Sprintf("select %d", i),
			StatementPreview: fmt.Sprintf("select %d", i),
			Status:           model.QueryExecutionSuccess,
			CreatedAt:        ts,
		}); err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
	}

	from := time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC)
	items, _, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 20, From: &from, To: &to,
		Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("time-filtered items = %d, want 1", len(items))
	}
}

func TestQueryExecutionRepository_IdenticalTimestampsOrderedByID(t *testing.T) {
	db := setupTestDB(t)
	repo := mysql.NewQueryExecutionRepository(db)
	ctx := context.Background()
	targetID := createQueryTargetResource(t, db, "cursor-same-ts-target")

	sameTime := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	ids := make([]uint64, 3)
	for i := 0; i < 3; i++ {
		id, err := seedExecutionHistoryRow(t, db, model.QueryExecutionRecord{
			TargetResourceID: targetID,
			ActorUserID:      ownerDBA,
			Engine:           "mysql",
			StatementDigest:  fmt.Sprintf("select %d", i),
			StatementPreview: fmt.Sprintf("select %d", i),
			Status:           model.QueryExecutionSuccess,
			CreatedAt:        sameTime,
		})
		if err != nil {
			t.Fatalf("insert %d: %v", i, err)
		}
		ids[i] = id
	}

	items, _, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 20,
		Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("items = %d, want 3", len(items))
	}

	// WHY: when timestamps are identical, rows must be ordered by ID descending
	// so cursor pagination (created_at, id) keyset is deterministic.
	for i := 1; i < len(items); i++ {
		if items[i].CreatedAt.Equal(items[i-1].CreatedAt) && items[i].ID >= items[i-1].ID {
			t.Fatalf("same-timestamp items not ordered by ID desc: items[%d].ID=%d >= items[%d].ID=%d",
				i, items[i].ID, i-1, items[i-1].ID)
		}
	}

	// Cursor pagination with same timestamps must use ID tiebreaker.
	last := items[0]
	cursor, err := model.EncodeCursor(last.CreatedAt, last.ID, "test-hash")
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	page2, _, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, PageSize: 2, Cursor: &cursor,
		Mode: model.PaginationModeCursor,
	})
	if err != nil {
		t.Fatalf("page2: %v", err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 items = %d, want 2", len(page2))
	}
	for _, item := range page2 {
		if item.ID >= last.ID {
			t.Fatalf("page2 item ID=%d should be < cursor ID=%d", item.ID, last.ID)
		}
	}
}

// --- Phase 38S: governed query-result paging contract ---

func assertQueryPageIDs(t *testing.T, resp model.QueryExecuteResponse, expected []int64) {
	t.Helper()
	if len(resp.Rows) != len(expected) || resp.RowCount != len(expected) {
		t.Fatalf("page rows = %d/%d, want %d", len(resp.Rows), resp.RowCount, len(expected))
	}
	for i, row := range resp.Rows {
		if len(row) != 2 {
			t.Fatalf("row %d has %d columns, want 2", i, len(row))
		}
		if id := fmt.Sprintf("%v", row[0]); id != strconv.FormatInt(expected[i], 10) {
			t.Fatalf("row %d id = %v (%T), want %d", i, row[0], row[0], expected[i])
		}
	}
}

func TestQueryExecution_PaginatedSelectReturnsOrderedPagesAndSeparateCaps(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)
	ctx := context.Background()
	statement := "SELECT id, name FROM qe_sandbox_fixtures ORDER BY id"

	page1, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   25,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 1, PageSize: 10,
		},
	})
	if err != nil {
		t.Fatalf("page 1 Execute error: %v", err)
	}
	if page1.Pagination == nil || page1.Pagination.Page != 1 || page1.Pagination.PageSize != 10 {
		t.Fatalf("page 1 pagination = %+v, want page=1 pageSize=10", page1.Pagination)
	}
	if page1.LimitApplied != 25 || !page1.Truncated || !page1.Pagination.HasNextPage || page1.Pagination.HasPreviousPage {
		t.Fatalf("page 1 caps/navigation = limit=%d truncated=%v pagination=%+v", page1.LimitApplied, page1.Truncated, page1.Pagination)
	}
	assertQueryPageIDs(t, page1, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})

	page2, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   25,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 2, PageSize: 10,
		},
	})
	if err != nil {
		t.Fatalf("page 2 Execute error: %v", err)
	}
	if page2.Pagination == nil || page2.Pagination.Page != 2 || page2.Pagination.PageSize != 10 {
		t.Fatalf("page 2 pagination = %+v, want page=2 pageSize=10", page2.Pagination)
	}
	if page2.LimitApplied != 25 || !page2.Truncated || !page2.Pagination.HasNextPage || !page2.Pagination.HasPreviousPage {
		t.Fatalf("page 2 caps/navigation = limit=%d truncated=%v pagination=%+v", page2.LimitApplied, page2.Truncated, page2.Pagination)
	}
	assertQueryPageIDs(t, page2, []int64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20})

	page3, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   25,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 3, PageSize: 10,
		},
	})
	if err != nil {
		t.Fatalf("page 3 Execute error: %v", err)
	}
	if page3.LimitApplied != 25 || page3.Truncated || page3.Pagination == nil || page3.Pagination.HasNextPage || !page3.Pagination.HasPreviousPage {
		t.Fatalf("page 3 caps/navigation = limit=%d truncated=%v pagination=%+v", page3.LimitApplied, page3.Truncated, page3.Pagination)
	}
	assertQueryPageIDs(t, page3, []int64{21, 22, 23, 24, 25})
}

func TestQueryExecution_PaginatedSelectIgnoresUserLimitAndOffset(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)
	ctx := context.Background()

	resp, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "SELECT id, name FROM qe_sandbox_fixtures ORDER BY id LIMIT 1 OFFSET 24",
		MaxRows:   25,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 1, PageSize: 10,
		},
	})
	if err != nil {
		t.Fatalf("user-window page 1 Execute error: %v", err)
	}
	if resp.LimitApplied != 25 || resp.Pagination == nil || resp.Pagination.Page != 1 {
		t.Fatalf("server page window = limit=%d pagination=%+v, want maxRows=25 page=1", resp.LimitApplied, resp.Pagination)
	}
	assertQueryPageIDs(t, resp, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})

	resp, err = svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: "SELECT id, name FROM qe_sandbox_fixtures ORDER BY id LIMIT 1 OFFSET 0",
		MaxRows:   25,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 2, PageSize: 10,
		},
	})
	if err != nil {
		t.Fatalf("user-window page 2 Execute error: %v", err)
	}
	assertQueryPageIDs(t, resp, []int64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
}

func TestQueryExecution_PaginatedSelectRejectsPageAtMaxRowsBoundaryBeforeTargetExecution(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	ctx := context.Background()
	statement := "SELECT id, name FROM qe_sandbox_fixtures WHERE name = 'page-secret' ORDER BY id"

	_, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   20,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 3, PageSize: 10,
		},
	})
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("cap-boundary error = %v, want ErrQueryValidationFailed", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "page-secret") || strings.Contains(strings.ToLower(err.Error()), "offset") {
		t.Fatalf("cap-boundary error leaked request details: %q", err)
	}

	repo := mysql.NewQueryExecutionRepository(db)
	items, total, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 10, Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list cap-boundary history: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("cap-boundary history total/len = %d/%d, want 1/1", total, len(items))
	}
	if items[0].Status != model.QueryExecutionRejected || items[0].RowCount != 0 || items[0].StatementPreview != "" || items[0].ErrorCode != "validation_failed" {
		t.Fatalf("cap-boundary history = %+v, want rejected with no executable statement", items[0])
	}

	var auditResult string
	if err := db.QueryRow(`select result from audit_events where target_resource_id = ? order by id desc limit 1`, targetID).Scan(&auditResult); err != nil {
		t.Fatalf("read cap-boundary audit: %v", err)
	}
	if auditResult != "validation_failed" {
		t.Fatalf("cap-boundary audit result = %q, want validation_failed", auditResult)
	}
}

func TestQueryExecution_PaginatedSelectCannotBypassHardMaxRows(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)
	ctx := context.Background()
	statement := "SELECT id, name FROM qe_sandbox_fixtures ORDER BY id"

	// Given an absurd requested cap, the guard clamps the release cap to
	// HardMaxRows (500) instead of trusting the caller.
	page1, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   2_000_000_000,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 1, PageSize: 10,
		},
	})
	if err != nil {
		t.Fatalf("huge-cap page 1 Execute error: %v", err)
	}
	if page1.LimitApplied != 500 {
		t.Fatalf("huge-cap LimitApplied = %d, want hard cap 500", page1.LimitApplied)
	}
	assertQueryPageIDs(t, page1, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})

	// And a page whose offset reaches the clamped cap is rejected before any
	// target execution, without leaking the statement or window internals.
	_, err = svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   2_000_000_000,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 51, PageSize: 10,
		},
	})
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("beyond-hard-cap error = %v, want ErrQueryValidationFailed", err)
	}
	lower := strings.ToLower(err.Error())
	for _, secret := range []string{"qe_sandbox_fixtures", "offset", "dsn", "@tcp("} {
		if strings.Contains(lower, secret) {
			t.Fatalf("beyond-hard-cap error leaked %q: %q", secret, err)
		}
	}
}

func TestQueryExecution_PaginatedSelectRecordsEachPageInHistoryAndAudit(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	ctx := context.Background()
	statement := "SELECT id, name FROM qe_sandbox_fixtures ORDER BY id"
	request := func(page int) model.QueryExecuteRequest {
		return model.QueryExecuteRequest{
			Statement: statement,
			MaxRows:   25,
			Pagination: &model.QueryExecutePaginationRequest{
				Page: page, PageSize: 10,
			},
		}
	}

	page1, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, request(1))
	if err != nil {
		t.Fatalf("page 1 Execute error: %v", err)
	}
	page2, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, request(2))
	if err != nil {
		t.Fatalf("page 2 Execute error: %v", err)
	}
	if page1.ExecutionID == 0 || page2.ExecutionID == 0 || page1.ExecutionID == page2.ExecutionID {
		t.Fatalf("page execution IDs = %d,%d, want distinct non-zero attempts", page1.ExecutionID, page2.ExecutionID)
	}

	historyRepo := mysql.NewQueryExecutionRepository(db)
	items, total, err := historyRepo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 10, Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list page history: %v", err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("page history total/len = %d/%d, want 2/2", total, len(items))
	}
	for _, item := range items {
		if item.Status != model.QueryExecutionSuccess || item.RowCount != 10 || item.ErrorCode != "" || item.ErrorMessage != "" {
			t.Fatalf("page history item = %+v, want normal success attempt", item)
		}
		if strings.Contains(strings.ToLower(item.StatementPreview), "offset") || strings.Contains(strings.ToLower(item.StatementPreview), "limit") {
			t.Fatalf("history stored server paging SQL: %q", item.StatementPreview)
		}
	}

	auditRepo := mysql.NewAuditRepository(db)
	auditItems, err := auditRepo.ListByResourceID(targetID)
	if err != nil {
		t.Fatalf("list page audit: %v", err)
	}
	if len(auditItems) != 2 {
		t.Fatalf("page audit items = %d, want 2", len(auditItems))
	}
	for _, item := range auditItems {
		if item.EventType != "query.executed" || item.Result != "success" || item.ActorUserID == nil || *item.ActorUserID != ownerDBA {
			t.Fatalf("page audit item = %+v, want attributed success", item)
		}
	}
}

func TestQueryExecution_PaginatedDisclosurePolicyChangeBetweenPagesMasksPageTwo(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	ctx := context.Background()
	statement := "SELECT id, name FROM qe_sandbox_fixtures ORDER BY id"

	page1, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   25,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 1, PageSize: 10,
		},
	})
	if err != nil {
		t.Fatalf("page 1 Execute error: %v", err)
	}
	if page1.Rows[0][1] != "fixture-01" {
		t.Fatalf("page 1 name = %v, want raw fixture-01", page1.Rows[0][1])
	}

	mustExec(t, db, `update query_result_disclosure_policies
		set mode = ?
		where target_resource_id = ? and database_name = ? and object_name = ? and column_name = ?`,
		string(model.ResultDisclosureMaskedNoCopy), targetID, testDBName(t), "qe_sandbox_fixtures", "name")

	page2, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   25,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 2, PageSize: 10,
		},
	})
	if err != nil {
		t.Fatalf("page 2 Execute error after policy change: %v", err)
	}
	if page2.Columns[0].DisplayMode != model.ResultDisclosureRawCopyAllowed || !page2.Columns[0].CopyAllowed {
		t.Fatalf("page 2 id disclosure = %+v, want raw copy allowed", page2.Columns[0])
	}
	if page2.Columns[1].DisplayMode != model.ResultDisclosureMaskedNoCopy || page2.Columns[1].CopyAllowed {
		t.Fatalf("page 2 name disclosure = %+v, want masked no-copy", page2.Columns[1])
	}
	assertQueryPageIDs(t, page2, []int64{11, 12, 13, 14, 15, 16, 17, 18, 19, 20})
	for _, row := range page2.Rows {
		if row[1] != "[MASKED]" {
			t.Fatalf("page 2 name = %v, want [MASKED] after policy change", row[1])
		}
	}
}

func TestQueryExecution_PaginatedControlledErrorsAndRecordsDoNotLeakSecrets(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	ctx := context.Background()
	statement := "SELECT id, name FROM qe_sandbox_fixtures WHERE name = 'page-secret' ORDER BY id"

	_, executionErr := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   20,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 3, PageSize: 10,
		},
	})
	if executionErr == nil {
		t.Fatal("expected cap-boundary error")
	}

	repo := mysql.NewQueryExecutionRepository(db)
	history, _, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 10, Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list secret-safety history: %v", err)
	}
	audit, err := mysql.NewAuditRepository(db).ListByResourceID(targetID)
	if err != nil {
		t.Fatalf("list secret-safety audit: %v", err)
	}
	historyJSON, err := json.Marshal(history)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}
	auditJSON, err := json.Marshal(audit)
	if err != nil {
		t.Fatalf("marshal audit: %v", err)
	}
	for _, raw := range []string{executionErr.Error(), string(historyJSON), string(auditJSON)} {
		for _, forbidden := range []string{
			"page-secret", "fixture-01", globalEnv.dsn, sandboxCredentialRef,
			"tcp(", "root:test", "offset", "result rows", "query result",
		} {
			if strings.Contains(strings.ToLower(raw), strings.ToLower(forbidden)) {
				t.Fatalf("secret %q leaked in governed output %q", forbidden, raw)
			}
		}
	}
	if strings.Contains(string(historyJSON), `"actorUserId"`) {
		t.Fatalf("history JSON exposed internal actor ID: %s", historyJSON)
	}
}

// setupOversizedPagePayloadFixture seeds a deterministic wide fixture whose
// first paginated window exceeds the executor's 1 MiB response cap: 10 ordered
// rows of 13 cell-capped (8192-byte) payload columns each, plus disclosure
// policies for every projected column. It returns the projection statement.
func setupOversizedPagePayloadFixture(t *testing.T, db *sql.DB, targetID uint64) string {
	t.Helper()
	columns := make([]string, 0, 13)
	definitions := make([]string, 0, 13)
	repeats := make([]string, 0, 13)
	for i := 1; i <= 13; i++ {
		name := fmt.Sprintf("p%02d", i)
		columns = append(columns, name)
		definitions = append(definitions, name+" mediumtext not null")
		repeats = append(repeats, "repeat('x', 8192)")
	}
	mustExec(t, db, `drop table if exists qe_paging_payload_fixtures`)
	mustExec(t, db, "create table qe_paging_payload_fixtures (id bigint unsigned not null primary key, "+strings.Join(definitions, ", ")+")")
	for id := 1; id <= 10; id++ {
		mustExec(t, db, fmt.Sprintf(
			"insert into qe_paging_payload_fixtures (id, %s) values (%d, %s)",
			strings.Join(columns, ", "), id, strings.Join(repeats, ", ")))
	}
	seedDisclosurePolicies(t, db, targetID, testDBName(t), "qe_paging_payload_fixtures", append([]string{"id"}, columns...)...)
	return "SELECT id, " + strings.Join(columns, ", ") + " FROM qe_paging_payload_fixtures ORDER BY id"
}

func TestQueryExecution_PaginatedOversizedPagePayloadIsControlledRejection(t *testing.T) {
	svc, targetID, db := setupQuerySandboxTarget(t)
	ctx := context.Background()
	statement := setupOversizedPagePayloadFixture(t, db, targetID)

	// When page one of the oversized window executes.
	resp, executionErr := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   20,
		Pagination: &model.QueryExecutePaginationRequest{
			Page: 1, PageSize: 10,
		},
	})

	// Then the page is a controlled rejection, never a partial success whose
	// next fixed offset would skip rows the operator never received.
	if !errors.Is(executionErr, service.ErrQueryValidationFailed) {
		t.Fatalf("oversized page error = %v, want ErrQueryValidationFailed", executionErr)
	}
	if !strings.Contains(executionErr.Error(), "result set exceeds configured limits") {
		t.Fatalf("oversized page error = %q, want fixed safe message", executionErr)
	}
	if resp.RowCount != 0 || len(resp.Rows) != 0 || resp.Pagination != nil || resp.Status == model.QueryExecutionSuccess {
		t.Fatalf("oversized page response = %+v, want no rows and no pagination metadata", resp)
	}

	// And the rejection never leaks payload, SQL, or credential material.
	lower := strings.ToLower(executionErr.Error())
	for _, forbidden := range []string{"qe_paging_payload_fixtures", "xxxx", "tcp(", "offset", strings.ToLower(globalEnv.dsn)} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("oversized page error leaked %q: %q", forbidden, executionErr)
		}
	}

	// And the rejected attempt is recorded to history + audit.
	repo := mysql.NewQueryExecutionRepository(db)
	items, total, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 10, Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list oversized-page history: %v", err)
	}
	if total != 1 || len(items) != 1 {
		t.Fatalf("oversized-page history total/len = %d/%d, want 1/1", total, len(items))
	}
	if items[0].Status != model.QueryExecutionRejected || items[0].RowCount != 0 || items[0].ErrorCode != "validation_failed" {
		t.Fatalf("oversized-page history = %+v, want rejected validation_failed attempt", items[0])
	}
	var auditResult string
	if err := db.QueryRow(`select result from audit_events where target_resource_id = ? order by id desc limit 1`, targetID).Scan(&auditResult); err != nil {
		t.Fatalf("read oversized-page audit: %v", err)
	}
	if auditResult != "validation_failed" {
		t.Fatalf("oversized-page audit result = %q, want validation_failed", auditResult)
	}

	// And the same statement without pagination keeps the existing bounded
	// truncated-success contract.
	nonPaged, err := svc.Execute(ctx, queryUserIdentity(ownerDBA), targetID, model.QueryExecuteRequest{
		Statement: statement,
		MaxRows:   20,
	})
	if err != nil {
		t.Fatalf("non-paged oversized Execute error: %v", err)
	}
	if !nonPaged.Truncated || nonPaged.RowCount != 9 {
		t.Fatalf("non-paged oversized result = truncated=%v rows=%d, want truncated 9-row success", nonPaged.Truncated, nonPaged.RowCount)
	}
}
