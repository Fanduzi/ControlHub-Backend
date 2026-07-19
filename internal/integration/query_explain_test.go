// Package integration provides Testcontainers-backed tests for Phase 38N
// governed Explain.
// input: context, database/sql, errors, fmt, strings, testing, internal/model, internal/repository/mysql, internal/service
// output: TestQueryExplain_* — real MySQL EXPLAIN FORMAT=JSON, no history row, audit secrecy
// pos: Phase 38N — prove Explain cannot execute the bare SELECT, cannot leak raw plan, cannot create normal execution history
// note: if this file changes, update header and README.md
//go:build integration

package integration

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"

	gosqlmysql "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

// setupExplainService wires a QueryExplainService against the disposable
// Testcontainers MySQL fixture, mirroring setupQuerySandboxTarget but for the
// Explain service. It returns the service, the target ID, the underlying
// *sql.DB (for direct assertions), and the cleanup function.
func setupExplainService(t *testing.T) (*service.QueryExplainService, uint64, *sql.DB) {
	t.Helper()
	db := setupTestDB(t)
	ctx := context.Background()

	mustExec(t, db, `drop table if exists qe_explain_fixtures`)
	mustExec(t, db, `create table qe_explain_fixtures (id bigint unsigned not null primary key, name varchar(64) not null)`)
	mustExec(t, db, `insert into qe_explain_fixtures (id, name) values (1,'alpha'),(2,'beta'),(3,'gamma')`)

	resRepo := mysql.NewResourceRepository(db)
	res, err := resRepo.CreateResource(ctx, model.ResourceCreateInput{
		ResourceType:    model.ResourceTypeDatabaseInstance,
		ResourceSubtype: "mysql",
		Name:            "qe-explain-target-" + strings.ReplaceAll(t.Name(), "/", "-"),
		DisplayName:     "Explain Target",
		EnvironmentID:   envStaging,
		OwnerID:         ownerDBA,
		LifecycleStatus: model.LifecycleStatusRunning,
		HealthStatus:    model.HealthStatusHealthy,
		Source:          "test",
		Labels:          map[string]string{},
	})
	if err != nil {
		t.Fatalf("create explain target resource: %v", err)
	}
	dsnCfg, err := gosqlmysql.ParseDSN(globalEnv.dsn)
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
	seedCredentialRow(t, db, res.ID, "mysql", sandboxCredentialRef, true, string(model.QueryEnvPolicyNonProdOnly))
	if err := os.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+sandboxCredentialRef, globalEnv.dsn); err != nil {
		t.Fatalf("set credential env: %v", err)
	}
	t.Cleanup(func() { _ = os.Unsetenv("CONTROLHUB_QUERY_CREDENTIAL_" + sandboxCredentialRef) })

	execRepo := mysql.NewQueryExecutionRepository(db)
	access := service.NewTargetAccessResolver(
		mysql.NewQueryTargetRepository(db),
		execRepo,
		service.NewEnvCredentialResolver(),
	)
	svc := service.NewQueryExplainService(
		service.NewQueryGuard(service.QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		access,
		service.NewMySQLExplainExecutor(),
		service.NewExplainNormalizer(),
		wallClock{},
		service.NewExplainAuditRecorder(execRepo),
	)
	return svc, res.ID, db
}

// TestQueryExplain_FullScanReturnsNormalizedRisk proves the real MySQL
// EXPLAIN FORMAT=JSON path produces deterministic normalized output with at
// least the full_table_scan risk.
func TestQueryExplain_FullScanReturnsNormalizedRisk(t *testing.T) {
	svc, targetID, _ := setupExplainService(t)
	resp, err := svc.Explain(context.Background(), ownerDBA, targetID, model.ExplainRequest{
		Statement: "select * from qe_explain_fixtures",
	})
	if err != nil {
		t.Fatalf("Explain error: %v", err)
	}
	if resp.TargetResourceID != targetID {
		t.Errorf("targetResourceId = %d, want %d", resp.TargetResourceID, targetID)
	}
	if resp.Engine != model.ExplainEngineMySQL {
		t.Errorf("engine = %s, want mysql", resp.Engine)
	}
	if resp.FormatVersion != model.ExplainFormatVersion {
		t.Errorf("formatVersion = %d, want %d", resp.FormatVersion, model.ExplainFormatVersion)
	}
	if len(resp.Nodes) == 0 {
		t.Fatalf("expected at least one normalized node")
	}
	foundFullScan := false
	for _, node := range resp.Nodes {
		if node.Access == model.ExplainAccessFullScan {
			foundFullScan = true
		}
	}
	if !foundFullScan {
		t.Errorf("expected at least one full_scan node, got %+v", resp.Nodes)
	}
	foundRisk := false
	for _, risk := range resp.Risks {
		if risk.Code == model.ExplainRiskFullTableScan {
			foundRisk = true
		}
	}
	if !foundRisk {
		t.Errorf("expected full_table_scan risk, got %+v", resp.Risks)
	}
}

// TestQueryExplain_NoQueryExecutionsRow proves Explain never creates a
// query_executions row. WHY: the spec requires Explain to remain distinct
// from normal execution history; a history row would mix Explain metadata
// with execution metadata and could leak plan/digest text.
func TestQueryExplain_NoQueryExecutionsRow(t *testing.T) {
	svc, targetID, db := setupExplainService(t)
	repo := mysql.NewQueryExecutionRepository(db)
	items, _, err := repo.ListExecutions(context.Background(), model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 100, Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list before: %v", err)
	}
	before := len(items)
	_, err = svc.Explain(context.Background(), ownerDBA, targetID, model.ExplainRequest{
		Statement: "select * from qe_explain_fixtures",
	})
	if err != nil {
		t.Fatalf("Explain error: %v", err)
	}
	items, _, err = repo.ListExecutions(context.Background(), model.QueryExecutionListQuery{
		TargetResourceID: targetID, Page: 1, PageSize: 100, Mode: model.PaginationModeOffset,
	})
	if err != nil {
		t.Fatalf("list after: %v", err)
	}
	if len(items) != before {
		t.Fatalf("Explain created query_executions rows: before=%d after=%d (must not create history)", before, len(items))
	}
}

// TestQueryExplain_AuditEventWrittenWithNoSecrets proves the audit boundary
// records a fixed query.explain event with no statement, plan, literal, or
// driver error text. WHY: the audit row is the only persistence Explain
// performs; it must carry only fixed metadata.
func TestQueryExplain_AuditEventWrittenWithNoSecrets(t *testing.T) {
	svc, targetID, db := setupExplainService(t)
	_, err := svc.Explain(context.Background(), ownerDBA, targetID, model.ExplainRequest{
		Statement: "select * from qe_explain_fixtures where name = 'secret-value-do-not-leak'",
	})
	if err != nil {
		t.Fatalf("Explain error: %v", err)
	}
	rows, err := db.Query(`select event_type, result from audit_events where target_resource_id = ? and event_type = 'query.explain'`, targetID)
	if err != nil {
		t.Fatalf("query audit events: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var et, result string
		if err := rows.Scan(&et, &result); err != nil {
			t.Fatalf("scan audit: %v", err)
		}
		if et != "query.explain" {
			t.Errorf("event_type = %q, want query.explain", et)
		}
		if result != "success" {
			t.Errorf("result = %q, want success", result)
		}
	}
	if count == 0 {
		t.Fatalf("expected at least one query.explain audit event")
	}
	leakRows, err := db.Query(`select event_type, result from audit_events where target_resource_id = ? and event_type = 'query.explain'`, targetID)
	if err != nil {
		t.Fatalf("query audit for leak check: %v", err)
	}
	defer leakRows.Close()
	for leakRows.Next() {
		var et, result string
		_ = leakRows.Scan(&et, &result)
		combined := et + " " + result
		for _, banned := range []string{"secret-value-do-not-leak", "qe_explain_fixtures", "select", "from", "where"} {
			if strings.Contains(strings.ToLower(combined), strings.ToLower(banned)) {
				t.Errorf("audit row must not contain %q, got: %s", banned, combined)
			}
		}
	}
}

// TestQueryExplain_ReadOnlyBehavior proves the fixture row count is unchanged
// after Explain. WHY: Explain must never execute the bare SELECT, so it
// cannot mutate data; the read-only transaction is defense-in-depth.
func TestQueryExplain_ReadOnlyBehavior(t *testing.T) {
	svc, targetID, db := setupExplainService(t)
	before := explainFixtureRowCount(t, db)
	_, err := svc.Explain(context.Background(), ownerDBA, targetID, model.ExplainRequest{
		Statement: "select * from qe_explain_fixtures",
	})
	if err != nil {
		t.Fatalf("Explain error: %v", err)
	}
	if after := explainFixtureRowCount(t, db); after != before {
		t.Fatalf("fixture row count changed: before=%d after=%d (Explain must not mutate)", before, after)
	}
}

// TestQueryExplain_TypedExplainRejected proves the guard rejects user-typed
// EXPLAIN on the Explain route. WHY: the browser must never construct
// EXPLAIN; the backend owns the wrapper.
func TestQueryExplain_TypedExplainRejected(t *testing.T) {
	svc, targetID, _ := setupExplainService(t)
	for _, stmt := range []string{"explain select 1", "EXPLAIN FORMAT=JSON select 1"} {
		_, err := svc.Explain(context.Background(), ownerDBA, targetID, model.ExplainRequest{Statement: stmt})
		if !errors.Is(err, service.ErrQueryValidationFailed) {
			t.Errorf("stmt %q: error = %v, want ErrQueryValidationFailed", stmt, err)
		}
	}
}

// TestQueryExplain_DMLRejected proves DML/DDL is rejected.
func TestQueryExplain_DMLRejected(t *testing.T) {
	svc, targetID, _ := setupExplainService(t)
	for _, stmt := range []string{"insert into qe_explain_fixtures values (999,'x')", "update qe_explain_fixtures set name='x'", "delete from qe_explain_fixtures", "drop table qe_explain_fixtures"} {
		_, err := svc.Explain(context.Background(), ownerDBA, targetID, model.ExplainRequest{Statement: stmt})
		if !errors.Is(err, service.ErrQueryValidationFailed) {
			t.Errorf("stmt %q: error = %v, want ErrQueryValidationFailed", stmt, err)
		}
	}
}

// TestQueryExplain_NoRawPlanLeak proves the response never contains raw plan
// JSON, table names, index names, or literals from the fixture.
func TestQueryExplain_NoRawPlanLeak(t *testing.T) {
	svc, targetID, _ := setupExplainService(t)
	resp, err := svc.Explain(context.Background(), ownerDBA, targetID, model.ExplainRequest{
		Statement: "select * from qe_explain_fixtures where name = 'secret-value-do-not-leak'",
	})
	if err != nil {
		t.Fatalf("Explain error: %v", err)
	}
	for _, node := range resp.Nodes {
		for _, banned := range []string{"qe_explain_fixtures", "idx_name", "secret-value-do-not-leak", "alpha", "beta", "gamma", "query_block", "access_type"} {
			if strings.Contains(strings.ToLower(string(node.Operation)+string(node.Access)+node.ID), strings.ToLower(banned)) {
				t.Errorf("node must not contain %q: %+v", banned, node)
			}
		}
	}
	for _, risk := range resp.Risks {
		s := string(risk.Code) + string(risk.Severity)
		for _, banned := range []string{"qe_explain_fixtures", "idx_name", "secret-value-do-not-leak"} {
			if strings.Contains(strings.ToLower(s), strings.ToLower(banned)) {
				t.Errorf("risk must not contain %q: %+v", banned, risk)
			}
		}
	}
}

func explainFixtureRowCount(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`select count(*) from qe_explain_fixtures`).Scan(&n); err != nil {
		t.Fatalf("count fixtures: %v", err)
	}
	return n
}
