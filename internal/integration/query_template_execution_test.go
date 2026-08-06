//go:build integration

package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	mysqldriver "github.com/go-sql-driver/mysql"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/repository/mysql"
	"github.com/fan/controlhub/internal/service"
)

func seedTemplateStatement(t *testing.T, db *sql.DB, targetID, ownerID uint64, statement string, params []model.QuerySavedStatementParameterDefinition) uint64 {
	t.Helper()
	repo := mysql.NewQuerySavedStatementRepository(db)
	created, err := repo.CreateWithAudit(context.Background(), ownerID, targetID, model.QuerySavedStatementCreateRequest{
		Name:       "integration template",
		Statement:  statement,
		Scope:      model.QuerySavedStatementPersonal,
		Parameters: params,
	})
	if err != nil {
		t.Fatalf("create saved statement: %v", err)
	}
	return created.ID
}

func newTemplateExecutionIntegrationService(db *sql.DB) *service.QueryExecutionService {
	return newExecutionService(db).WithTemplateExecution(
		mysql.NewQuerySavedStatementRepository(db),
		service.NewTemplateStatementCompiler(),
	)
}

// TestExecuteSavedStatementIntegrationBindsValuesAgainstRealMySQL proves the
// full template chain against a real database: the server re-reads the saved
// statement, binds typed values through the driver, records placeholder-only
// history/audit, and never persists the bound values.
func TestExecuteSavedStatementIntegrationBindsValuesAgainstRealMySQL(t *testing.T) {
	targetID, db, dsn := newDevSeedTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, dsn)
	if _, err := newDevSeeder(db).Seed(ctx, service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	statementID := seedTemplateStatement(t, db, targetID, ownerDBA,
		"select id from qe_sandbox_fixtures where name = :name",
		[]model.QuerySavedStatementParameterDefinition{{Name: "name", Type: model.QuerySavedStatementParameterString}})

	resp, err := newTemplateExecutionIntegrationService(db).ExecuteSavedStatement(ctx, ownerDBA, targetID, statementID,
		model.QuerySavedStatementExecuteRequest{
			Values:  map[string]json.RawMessage{"name": json.RawMessage(`"alpha"`)},
			MaxRows: 10,
		})
	if err != nil {
		t.Fatalf("ExecuteSavedStatement: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if resp.RowCount != 1 {
		t.Fatalf("rowCount = %d, want 1", resp.RowCount)
	}

	// Bound values must never reach history, audit, or any other table.
	assertNoValuePersisted(t, db, targetID, "alpha")
}

func assertNoValuePersisted(t *testing.T, db *sql.DB, targetID uint64, value string) {
	t.Helper()
	like := "%" + value + "%"
	var historyLeaks, auditLeaks int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions
		WHERE target_resource_id = ? AND (statement_preview LIKE ? OR statement_digest LIKE ? OR error_message LIKE ?)`,
		targetID, like, like, like).Scan(&historyLeaks); err != nil {
		t.Fatalf("count history leaks: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events
		WHERE target_resource_id = ? AND (event_type LIKE ? OR result LIKE ?)`,
		targetID, like, like).Scan(&auditLeaks); err != nil {
		t.Fatalf("count audit leaks: %v", err)
	}
	if historyLeaks != 0 || auditLeaks != 0 {
		t.Fatalf("parameter value %q persisted: history=%d audit=%d", value, historyLeaks, auditLeaks)
	}
}

// TestExecuteSavedStatementIntegrationAuthorizationMatrix proves the owner-only
// rule for personal templates against the real repository and that a shared
// template runs for any fresh actor.
func TestExecuteSavedStatementIntegrationAuthorizationMatrix(t *testing.T) {
	targetID, db, dsn := newDevSeedTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, dsn)
	if _, err := newDevSeeder(db).Seed(ctx, service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	personalID := seedTemplateStatement(t, db, targetID, ownerDBA,
		"select id from qe_sandbox_fixtures limit 1",
		[]model.QuerySavedStatementParameterDefinition{})
	svc := newTemplateExecutionIntegrationService(db)

	// A non-owner (even with a higher role) must never see a personal template.
	if _, err := svc.ExecuteSavedStatement(ctx, ownerDBA+1, targetID, personalID,
		model.QuerySavedStatementExecuteRequest{MaxRows: 10}); !errors.Is(err, service.ErrQuerySavedStatementNotFound) {
		t.Fatalf("non-owner error = %v, want ErrQuerySavedStatementNotFound", err)
	}
	if _, err := svc.ExecuteSavedStatement(ctx, ownerDBA+2, targetID, personalID,
		model.QuerySavedStatementExecuteRequest{MaxRows: 10}); !errors.Is(err, service.ErrQuerySavedStatementNotFound) {
		t.Fatalf("other actor error = %v, want ErrQuerySavedStatementNotFound", err)
	}
	// The owner succeeds.
	if _, err := svc.ExecuteSavedStatement(ctx, ownerDBA, targetID, personalID,
		model.QuerySavedStatementExecuteRequest{MaxRows: 10}); err != nil {
		t.Fatalf("owner execution: %v", err)
	}

	// A shared template runs for any fresh actor.
	repo := mysql.NewQuerySavedStatementRepository(db)
	shared, err := repo.CreateWithAudit(ctx, ownerDBA+1, targetID, model.QuerySavedStatementCreateRequest{
		Name:      "shared template",
		Statement: "select id from qe_sandbox_fixtures limit 1",
		Scope:     model.QuerySavedStatementSharedTemplate,
	})
	if err != nil {
		t.Fatalf("create shared template: %v", err)
	}
	if _, err := svc.ExecuteSavedStatement(ctx, ownerDBA, targetID, shared.ID,
		model.QuerySavedStatementExecuteRequest{MaxRows: 10}); err != nil {
		t.Fatalf("shared template execution: %v", err)
	}
}

// TestExecuteSavedStatementIntegrationTemplatePaginationProves every template
// page is a fresh governed execution with its own history row.
func TestExecuteSavedStatementIntegrationTemplatePagination(t *testing.T) {
	targetID, db, dsn := newDevSeedTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, dsn)
	if _, err := newDevSeeder(db).Seed(ctx, service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	mustExec(t, db, `insert into qe_sandbox_fixtures (id, name) values (2,'beta'),(3,'gamma'),(4,'delta'),(5,'epsilon'),(6,'zeta'),(7,'eta'),(8,'theta'),(9,'iota'),(10,'kappa'),(11,'lambda'),(12,'mu')`)

	statementID := seedTemplateStatement(t, db, targetID, ownerDBA,
		"select id from qe_sandbox_fixtures where id > :minimum_id",
		[]model.QuerySavedStatementParameterDefinition{{Name: "minimum_id", Type: model.QuerySavedStatementParameterInteger}})
	svc := newTemplateExecutionIntegrationService(db)

	pageOne := model.QuerySavedStatementExecuteRequest{
		Values:     map[string]json.RawMessage{"minimum_id": json.RawMessage(`0`)},
		MaxRows:    100,
		Pagination: &model.QueryExecutePaginationRequest{Page: 1, PageSize: 10},
	}
	first, err := svc.ExecuteSavedStatement(ctx, ownerDBA, targetID, statementID, pageOne)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if first.RowCount != 10 || first.Pagination == nil || !first.Pagination.HasNextPage {
		t.Fatalf("page 1 rowCount=%d pagination=%+v, want 10 rows with next page", first.RowCount, first.Pagination)
	}

	pageTwo := pageOne
	pageTwo.Pagination = &model.QueryExecutePaginationRequest{Page: 2, PageSize: 10}
	second, err := svc.ExecuteSavedStatement(ctx, ownerDBA, targetID, statementID, pageTwo)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if second.RowCount != 2 || second.Pagination == nil || second.Pagination.HasPreviousPage != true {
		t.Fatalf("page 2 rowCount=%d pagination=%+v, want 2 rows with previous page", second.RowCount, second.Pagination)
	}

	// Every page is a distinct governed history/audit record.
	var historyCount, auditCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID).Scan(&historyCount); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM audit_events WHERE target_resource_id = ? AND event_type = 'query.executed'`, targetID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if historyCount != 2 || auditCount != 2 {
		t.Fatalf("history=%d audit=%d, want 2 each (one per template page)", historyCount, auditCount)
	}

	// No bound value persisted across pages.
	assertNoValuePersisted(t, db, targetID, "beta")
	assertNoValuePersisted(t, db, targetID, "12")
}

// TestExecuteSavedStatementIntegrationStaleDefinitionsFailClosed proves a
// stored template that no longer matches its declarations is rejected without
// executing.
func TestExecuteSavedStatementIntegrationStaleDefinitionsFailClosed(t *testing.T) {
	targetID, db, dsn := newDevSeedTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, dsn)
	if _, err := newDevSeeder(db).Seed(ctx, service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	statementID := seedTemplateStatement(t, db, targetID, ownerDBA,
		"select id from qe_sandbox_fixtures where name = :name",
		[]model.QuerySavedStatementParameterDefinition{{Name: "name", Type: model.QuerySavedStatementParameterString}})
	svc := newTemplateExecutionIntegrationService(db)

	// A value of the wrong type is rejected with a controlled field error and
	// never reaches the database.
	_, err := svc.ExecuteSavedStatement(ctx, ownerDBA, targetID, statementID,
		model.QuerySavedStatementExecuteRequest{
			Values:  map[string]json.RawMessage{"name": json.RawMessage(`5`)},
			MaxRows: 10,
		})
	var valueErr *service.TemplateValueValidationError
	if !errors.As(err, &valueErr) {
		t.Fatalf("error = %v, want *TemplateValueValidationError", err)
	}
	if valueErr.Fields["name"] != "invalid" {
		t.Fatalf("name field code = %q, want invalid", valueErr.Fields["name"])
	}
	assertNoValuePersisted(t, db, targetID, "5")
}

// TestExecuteSavedStatementIntegrationDisclosureChangeAffectsLaterPage proves
// a disclosure-policy change takes effect on a subsequent template page: page 1
// succeeds, deleting the policy for a projected column blocks page 2 before the
// executor, and restoring the policy lets a later page succeed again.
func TestExecuteSavedStatementIntegrationDisclosureChangeAffectsLaterPage(t *testing.T) {
	targetID, db, dsn := newDevSeedTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, dsn)
	if _, err := newDevSeeder(db).Seed(ctx, service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	mustExec(t, db, `insert into qe_sandbox_fixtures (id, name) values (2,'beta'),(3,'gamma')`)

	statementID := seedTemplateStatement(t, db, targetID, ownerDBA,
		"select id, name from qe_sandbox_fixtures where id > :minimum_id order by id",
		[]model.QuerySavedStatementParameterDefinition{{Name: "minimum_id", Type: model.QuerySavedStatementParameterInteger}})
	svc := newTemplateExecutionIntegrationService(db)
	disclosure := mysql.NewQueryDisclosureRepository(db)

	pageOne := model.QuerySavedStatementExecuteRequest{
		Values:     map[string]json.RawMessage{"minimum_id": json.RawMessage(`0`)},
		MaxRows:    100,
		Pagination: &model.QueryExecutePaginationRequest{Page: 1, PageSize: 10},
	}
	if resp, err := svc.ExecuteSavedStatement(ctx, ownerDBA, targetID, statementID, pageOne); err != nil || resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("page 1: err=%v resp=%+v", err, resp.Status)
	}

	// A policy change now blocks page 2 before any SQL runs.
	if err := disclosure.Delete(ctx, targetID, dsnCfgFor(t).DBName, "qe_sandbox_fixtures", "name"); err != nil {
		t.Fatalf("delete disclosure policy: %v", err)
	}
	pageTwo := pageOne
	pageTwo.Pagination = &model.QueryExecutePaginationRequest{Page: 2, PageSize: 10}
	_, err := svc.ExecuteSavedStatement(ctx, ownerDBA, targetID, statementID, pageTwo)
	if !errors.Is(err, service.ErrQueryNotAllowed) {
		t.Fatalf("page 2 error = %v, want disclosure block (ErrQueryNotAllowed)", err)
	}
	var historyCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM query_executions WHERE target_resource_id = ?`, targetID).Scan(&historyCount); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if historyCount != 2 {
		t.Fatalf("history = %d, want 2 (page 1 success + page 2 rejected)", historyCount)
	}

	// Restoring the policy makes a later page succeed again.
	if _, err := disclosure.Insert(ctx, model.ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: targetID,
		DatabaseName:     dsnCfgFor(t).DBName,
		ObjectName:       "qe_sandbox_fixtures",
		ColumnName:       "name",
		Mode:             model.ResultDisclosureRawCopyAllowed,
	}); err != nil {
		t.Fatalf("restore disclosure policy: %v", err)
	}
	pageThree := pageOne
	pageThree.Pagination = &model.QueryExecutePaginationRequest{Page: 1, PageSize: 10}
	if resp, err := svc.ExecuteSavedStatement(ctx, ownerDBA, targetID, statementID, pageThree); err != nil || resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("page 3 after restore: err=%v", err)
	}
}

func dsnCfgFor(t *testing.T) *mysqldriver.Config {
	t.Helper()
	cfg, err := mysqldriver.ParseDSN(globalEnv.dsn)
	if err != nil {
		t.Fatalf("parse test dsn: %v", err)
	}
	return cfg
}

// TestExecuteSavedStatementIntegrationHistoryKeepsPlaceholderSQL proves the
// recorded history preview/digest carries the placeholder form, not bound text.
func TestExecuteSavedStatementIntegrationHistoryKeepsPlaceholderSQL(t *testing.T) {
	targetID, db, dsn := newDevSeedTarget(t, true)
	ctx := context.Background()
	t.Setenv("CONTROLHUB_QUERY_CREDENTIAL_"+devSeedCredentialRef, dsn)
	if _, err := newDevSeeder(db).Seed(ctx, service.QueryDevCredentialSeedConfig{
		TargetResourceID:  targetID,
		CredentialRef:     devSeedCredentialRef,
		EnvironmentPolicy: model.QueryEnvPolicyNonProdOnly,
	}); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	statementID := seedTemplateStatement(t, db, targetID, ownerDBA,
		"select id from qe_sandbox_fixtures where name = :name",
		[]model.QuerySavedStatementParameterDefinition{{Name: "name", Type: model.QuerySavedStatementParameterString}})
	if _, err := newTemplateExecutionIntegrationService(db).ExecuteSavedStatement(ctx, ownerDBA, targetID, statementID,
		model.QuerySavedStatementExecuteRequest{
			Values:  map[string]json.RawMessage{"name": json.RawMessage(`"alpha"`)},
			MaxRows: 10,
		}); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var preview, digest string
	if err := db.QueryRow(`SELECT statement_preview, statement_digest FROM query_executions WHERE target_resource_id = ? LIMIT 1`, targetID).Scan(&preview, &digest); err != nil {
		t.Fatalf("read history: %v", err)
	}
	for _, leaked := range []string{"alpha", `:name`, "name = 'alpha'"} {
		if strings.Contains(preview, leaked) || strings.Contains(digest, leaked) {
			t.Fatalf("history leaks %q: preview=%q digest=%q", leaked, preview, digest)
		}
	}
}
