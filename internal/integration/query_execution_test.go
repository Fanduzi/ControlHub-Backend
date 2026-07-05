//go:build integration

// Package integration provides Testcontainers-backed tests for the Phase 37
// read-only query sandbox (repository, service end-to-end, audit/history).
package integration

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

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
		if _, err := repo.InsertExecution(ctx, model.QueryExecutionRecord{
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

	items, total, err := repo.ListExecutions(ctx, model.QueryExecutionListQuery{TargetResourceID: targetID, Page: 1, PageSize: 20})
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
	mustExec(t, db, `insert into qe_sandbox_fixtures (id, name) values (1,'alpha'),(2,'beta'),(3,'gamma')`)

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

	svc := service.NewQueryExecutionService(
		mysql.NewQueryTargetRepository(db),
		mysql.NewQueryExecutionRepository(db),
		service.NewEnvCredentialResolver(),
		service.NewMySQLQueryExecutor(service.QueryExecutorCaps{}),
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		wallClock{},
	)
	return svc, res.ID, db
}

func mustExec(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
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

	resp, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
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
		_, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{Statement: stmt, MaxRows: 10})
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

	_, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
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

	resp, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
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

	if _, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10}); err != nil {
		t.Fatalf("success Execute: %v", err)
	}
	if _, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{Statement: "delete from qe_sandbox_fixtures", MaxRows: 10}); err == nil {
		t.Fatal("expected rejection for write statement")
	}

	items, total, err := repo.ListExecutions(context.Background(), model.QueryExecutionListQuery{TargetResourceID: targetID, Page: 1, PageSize: 20})
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

	if _, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{Statement: "select 1", MaxRows: 10}); err != nil {
		t.Fatalf("success Execute: %v", err)
	}
	if _, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{Statement: "delete from qe_sandbox_fixtures", MaxRows: 10}); err == nil {
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

	resp, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
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
	svc, targetID, _ := setupQuerySandboxTarget(t)

	resp, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
		Statement: "select cast(NULL as signed) as v",
		MaxRows:   10,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.RowCount != 1 || len(resp.Rows[0]) != 1 {
		t.Fatalf("expected 1 row x 1 col, got rowCount=%d row0=%v", resp.RowCount, resp.Rows)
	}
	if got := resp.Rows[0][0]; got != nil {
		t.Fatalf("cast(NULL as signed) = %v (%T), want nil", got, got)
	}
}

// --- Phase 38C: read-only metadata statement execution tests ---

func TestQueryExecution_ShowTablesSucceeds(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: SHOW TABLES is a safe read-only metadata command that users expect
	// in a database query workbench.
	resp, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
		Statement: "show tables",
		MaxRows:   100,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	// The fixture table qe_sandbox_fixtures must appear in the result.
	found := false
	for _, row := range resp.Rows {
		if len(row) > 0 {
			if name, ok := row[0].(string); ok && name == "qe_sandbox_fixtures" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("SHOW TABLES must include qe_sandbox_fixtures, got rows=%v", resp.Rows)
	}
}

func TestQueryExecution_DescribeTableSucceeds(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: DESCRIBE <table> is a safe metadata introspection command.
	resp, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
		Statement: "describe qe_sandbox_fixtures",
		MaxRows:   100,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.RowCount < 2 {
		t.Fatalf("rowCount = %d, want >= 2 (id and name columns)", resp.RowCount)
	}
	// Verify the column names are present.
	colNames := make(map[string]bool)
	for _, col := range resp.Columns {
		colNames[col.Name] = true
	}
	if !colNames["Field"] || !colNames["Type"] {
		t.Fatalf("DESCRIBE columns must include Field and Type, got %v", colNames)
	}
}

func TestQueryExecution_ExplainSelectSucceeds(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: EXPLAIN SELECT must return the execution plan (EXPLAIN columns like
	// id, select_type, table, type), NOT business data from the table itself.
	resp, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
		Statement: "explain select * from qe_sandbox_fixtures",
		MaxRows:   100,
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.RowCount < 1 {
		t.Fatalf("rowCount = %d, want >= 1", resp.RowCount)
	}
	// Verify we got EXPLAIN output columns, not business data columns.
	// MySQL EXPLAIN always returns columns like id, select_type, table, type.
	colNames := make(map[string]bool)
	for _, col := range resp.Columns {
		colNames[col.Name] = true
	}
	if !colNames["id"] || !colNames["select_type"] || !colNames["table"] {
		t.Fatalf("EXPLAIN must return execution plan columns (id, select_type, table), got %v", colNames)
	}
}

func TestQueryExecution_UpdateRemainsRejected(t *testing.T) {
	svc, targetID, _ := setupQuerySandboxTarget(t)

	// WHY: writes must remain rejected even after the guard widening.
	_, err := svc.Execute(context.Background(), ownerDBA, targetID, model.QueryExecuteRequest{
		Statement: "update qe_sandbox_fixtures set name = 'x'",
		MaxRows:   10,
	})
	if !errors.Is(err, service.ErrQueryValidationFailed) {
		t.Fatalf("Execute(update) error = %v, want ErrQueryValidationFailed", err)
	}
}
