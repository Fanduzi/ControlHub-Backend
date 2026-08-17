// Package service provides tests for governed saved-statement (template) execution.
// input: context, database/sql, encoding/json, errors, strings, testing, time, internal/model
// output: TestExecuteSavedStatement* (reread, authorization matrix, typed values, per-page chain, no-value persistence, atomic Execution Evidence Pair writes)
// pos: Unit tests for the fresh-query-actor template-execution route through the existing governed chain, incl. cancellation-durable evidence (Issue #35)
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

func newTemplateExecutionTestService(statement model.QuerySavedStatement, readerErr error) (*QueryExecutionService, *fakeExecRepo, *fakeExecutor, *fakeDisclosureService) {
	target := mysqlTarget("Staging")
	repo := &fakeExecRepo{credentials: map[uint64]model.QueryCredentialMetadata{9001: enabledCred(model.QueryEnvPolicyNonProdOnly)}}
	executor := &fakeExecutor{result: QueryDatabaseResult{
		Columns:  []model.QueryResultColumn{{Name: "value", DatabaseType: "BIGINT"}},
		Rows:     [][]any{{int64(1)}},
		RowCount: 1,
	}}
	disclosure := &fakeDisclosureService{}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{target}},
		repo,
		&fakeResolver{dsn: testResolverDSN},
		executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)},
		&fakeNavSchemaInspector{},
		disclosure,
	)
	reader := &fakeSavedStatementReader{getResp: statement, getErr: readerErr}
	svc.WithTemplateExecution(reader, NewTemplateStatementCompiler())
	return svc, repo, executor, disclosure
}

func templateStatement(owner uint64, scope model.QuerySavedStatementScope, statement string, params []model.QuerySavedStatementParameterDefinition) model.QuerySavedStatement {
	return model.QuerySavedStatement{
		ID:               7,
		TargetResourceID: 9001,
		OwnerUserID:      owner,
		Name:             "t",
		Statement:        statement,
		Parameters:       params,
		Scope:            scope,
	}
}

func templateExecuteRequest(values map[string]string) model.QuerySavedStatementExecuteRequest {
	raw := make(map[string]json.RawMessage, len(values))
	for name, value := range values {
		raw[name] = json.RawMessage(value)
	}
	return model.QuerySavedStatementExecuteRequest{Values: raw, MaxRows: 100}
}

// TestExecuteSavedStatementUsesSingleAtomicPairWrite proves the template
// execution path records history + audit through one atomic pair write per
// execution (Issue #34 expand step), never the split seams.
func TestExecuteSavedStatementUsesSingleAtomicPairWrite(t *testing.T) {
	svc, repo, _, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select 1", nil), nil,
	)

	_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, templateExecuteRequest(nil))
	if err != nil {
		t.Fatalf("execute saved statement: %v", err)
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair calls = %d, want 1 for template execution", len(repo.pairCalls))
	}
	if repo.pairCalls[0].event != "query.executed" || repo.pairCalls[0].result != "success" {
		t.Fatalf("pair audit params = %q/%q, want query.executed/success", repo.pairCalls[0].event, repo.pairCalls[0].result)
	}
	if repo.splitExecCalls != 0 || repo.splitAuditCalls != 0 {
		t.Fatalf("split writes leaked on template path: %d/%d, want 0/0", repo.splitExecCalls, repo.splitAuditCalls)
	}
}

// TestExecuteSavedStatementValidationRejectionUsesAtomicPairWrite proves the
// template validation-rejection path also records through the atomic pair.
func TestExecuteSavedStatementValidationRejectionUsesAtomicPairWrite(t *testing.T) {
	svc, repo, _, _ := newTemplateExecutionTestService(
		templateStatement(7, model.QuerySavedStatementPersonal, "select 1", []model.QuerySavedStatementParameterDefinition{
			{Name: "v", Type: model.QuerySavedStatementParameterInteger},
		}), nil,
	)

	// A non-integer value for an integer parameter fails validation after the
	// target has been resolved, so the rejected attempt must be pair-recorded.
	_, err := svc.ExecuteSavedStatement(context.Background(), 7, 9001, 7, templateExecuteRequest(map[string]string{"v": "\"oops\""}))
	if err == nil {
		t.Fatal("expected validation failure")
	}
	if len(repo.pairCalls) != 1 {
		t.Fatalf("atomic pair calls = %d, want 1 for rejected template validation", len(repo.pairCalls))
	}
	if repo.pairCalls[0].result != "validation_failed" {
		t.Fatalf("pair audit result = %q, want validation_failed", repo.pairCalls[0].result)
	}
	if repo.splitExecCalls != 0 || repo.splitAuditCalls != 0 {
		t.Fatalf("split writes leaked on template validation path: %d/%d, want 0/0", repo.splitExecCalls, repo.splitAuditCalls)
	}
}

func TestExecuteSavedStatementRunsPersonalTemplateThroughGovernedChain(t *testing.T) {
	t.Parallel()
	statement := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status and total > :minimum",
		[]model.QuerySavedStatementParameterDefinition{
			{Name: "status", Type: model.QuerySavedStatementParameterString},
			{Name: "minimum", Type: model.QuerySavedStatementParameterDecimal},
		})
	svc, repo, executor, disclosure := newTemplateExecutionTestService(statement, nil)

	resp, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7,
		templateExecuteRequest(map[string]string{"status": `"paid"`, "minimum": `"100.50"`}))
	if err != nil {
		t.Fatalf("ExecuteSavedStatement error: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if executor.templateCalls != 1 {
		t.Fatalf("QueryTemplate calls = %d, want 1", executor.templateCalls)
	}
	if disclosure.preflightCalls != 1 {
		t.Fatalf("disclosure preflight calls = %d, want 1", disclosure.preflightCalls)
	}
	if len(repo.insertedAttempts) != 1 || len(repo.auditEvents) != 1 {
		t.Fatalf("history = %d, audit = %d, want 1 each", len(repo.insertedAttempts), len(repo.auditEvents))
	}
	record := repo.insertedAttempts[0]
	if record.Status != model.QueryExecutionSuccess {
		t.Fatalf("history status = %q, want success", record.Status)
	}
	// History stores the placeholder SQL only — never bound values.
	for _, leaked := range []string{"paid", "100.50"} {
		if strings.Contains(record.StatementPreview, leaked) || strings.Contains(record.StatementDigest, leaked) {
			t.Fatalf("history leaks value %q: preview=%q digest=%q", leaked, record.StatementPreview, record.StatementDigest)
		}
	}
}

func TestExecuteSavedStatementBindsTypedValuesInSourceOrder(t *testing.T) {
	t.Parallel()
	statement := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status and id > :count and total > :amount and active = :flag",
		[]model.QuerySavedStatementParameterDefinition{
			{Name: "status", Type: model.QuerySavedStatementParameterString},
			{Name: "count", Type: model.QuerySavedStatementParameterInteger},
			{Name: "amount", Type: model.QuerySavedStatementParameterDecimal},
			{Name: "flag", Type: model.QuerySavedStatementParameterBoolean},
		})
	svc, _, _, _ := newTemplateExecutionTestService(statement, nil)

	// Values arrive as RawMessage; integers stay JSON integers, decimals stay
	// JSON strings, booleans stay JSON booleans.
	raw := map[string]json.RawMessage{
		"status": json.RawMessage(`"paid"`),
		"count":  json.RawMessage(`5`),
		"amount": json.RawMessage(`"100.50"`),
		"flag":   json.RawMessage(`true`),
	}
	_, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7,
		model.QuerySavedStatementExecuteRequest{Values: raw, MaxRows: 100})
	if err != nil {
		t.Fatalf("ExecuteSavedStatement error: %v", err)
	}
}

func TestExecuteSavedStatementRejectsForeignPersonalTemplate(t *testing.T) {
	t.Parallel()
	statement := templateStatement(99, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status",
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})
	svc, repo, executor, _ := newTemplateExecutionTestService(statement, nil)

	for _, actor := range []uint64{2, 1} {
		_, err := svc.ExecuteSavedStatement(context.Background(), actor, 9001, 7,
			templateExecuteRequest(map[string]string{"status": `"paid"`}))
		if !errors.Is(err, ErrQuerySavedStatementNotFound) {
			t.Fatalf("actor %d error = %v, want ErrQuerySavedStatementNotFound", actor, err)
		}
	}
	if executor.templateCalls != 0 {
		t.Fatalf("QueryTemplate calls = %d, want 0 for unauthorized personal template", executor.templateCalls)
	}
	if len(repo.insertedAttempts) != 0 {
		t.Fatalf("history rows = %d, want 0 for unauthorized personal template", len(repo.insertedAttempts))
	}
}

func TestExecuteSavedStatementAllowsSharedTemplateForAnyActor(t *testing.T) {
	t.Parallel()
	statement := templateStatement(99, model.QuerySavedStatementSharedTemplate,
		"select id from orders where status = :status",
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})
	svc, _, executor, _ := newTemplateExecutionTestService(statement, nil)

	if _, err := svc.ExecuteSavedStatement(context.Background(), 5, 9001, 7,
		templateExecuteRequest(map[string]string{"status": `"paid"`})); err != nil {
		t.Fatalf("ExecuteSavedStatement shared template error: %v", err)
	}
	if executor.templateCalls != 1 {
		t.Fatalf("QueryTemplate calls = %d, want 1", executor.templateCalls)
	}
}

func TestExecuteSavedStatementRejectsMissingStatement(t *testing.T) {
	t.Parallel()
	svc, _, executor, _ := newTemplateExecutionTestService(model.QuerySavedStatement{}, sql.ErrNoRows)

	_, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7,
		templateExecuteRequest(map[string]string{"status": `"paid"`}))
	if !errors.Is(err, ErrQuerySavedStatementNotFound) {
		t.Fatalf("error = %v, want ErrQuerySavedStatementNotFound", err)
	}
	if executor.templateCalls != 0 {
		t.Fatalf("QueryTemplate calls = %d, want 0", executor.templateCalls)
	}
}

func TestExecuteSavedStatementValidatesTypedValuesWithFieldErrors(t *testing.T) {
	t.Parallel()
	statement := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status and id > :count",
		[]model.QuerySavedStatementParameterDefinition{
			{Name: "status", Type: model.QuerySavedStatementParameterString},
			{Name: "count", Type: model.QuerySavedStatementParameterInteger},
		})

	cases := []struct {
		name   string
		values map[string]string
		want   map[string]string
	}{
		{"missing and unknown", map[string]string{"bogus": `"x"`}, map[string]string{"status": "missing", "count": "missing", "bogus": "unknown"}},
		{"type mismatch", map[string]string{"status": `"paid"`, "count": `"seven"`}, map[string]string{"count": "invalid"}},
		{"float for integer", map[string]string{"status": `"paid"`, "count": `1.5`}, map[string]string{"count": "invalid"}},
		{"bool for integer", map[string]string{"status": `"paid"`, "count": `true`}, map[string]string{"count": "invalid"}},
		{"null value", map[string]string{"status": `null`, "count": `5`}, map[string]string{"status": "invalid"}},
		{"integer for string", map[string]string{"status": `5`, "count": `5`}, map[string]string{"status": "invalid"}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, executor, _ := newTemplateExecutionTestService(statement, nil)
			_, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7, templateExecuteRequest(tc.values))
			var valueErr *TemplateValueValidationError
			if !errors.As(err, &valueErr) {
				t.Fatalf("error = %v, want *TemplateValueValidationError", err)
			}
			for name, code := range tc.want {
				if got := valueErr.Fields[name]; got != code {
					t.Errorf("field %q code = %q, want %q (fields=%v)", name, got, code, valueErr.Fields)
				}
			}
			// Controlled value errors never reach the executor. The rejected
			// attempt is still recorded (fixed metadata, no values) to honor
			// the every-attempt-recorded guarantee.
			if executor.templateCalls != 0 {
				t.Fatalf("value error executed: template calls=%d", executor.templateCalls)
			}
			if len(repo.insertedAttempts) != 1 {
				t.Fatalf("history rows = %d, want 1 rejected attempt", len(repo.insertedAttempts))
			}
			if repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
				t.Fatalf("history status = %q, want rejected", repo.insertedAttempts[0].Status)
			}
			// The recorded rejection carries no supplied value.
			for key := range tc.values {
				if strings.Contains(repo.insertedAttempts[0].StatementPreview, key) ||
					strings.Contains(repo.insertedAttempts[0].ErrorMessage, key) {
					t.Errorf("rejection records a value/name: %+v", repo.insertedAttempts[0])
				}
			}
			// No supplied value is echoed in the error text.
			for key := range tc.values {
				if strings.Contains(valueErr.Error(), strings.Trim(key, `"`)) && valueErr.Fields[key] == "invalid" {
					t.Errorf("error echoes parameter key")
				}
			}
		})
	}
}

func TestExecuteSavedStatementRejectsOversizedStringValue(t *testing.T) {
	t.Parallel()
	statement := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status",
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})
	svc, _, executor, _ := newTemplateExecutionTestService(statement, nil)

	big := `"` + strings.Repeat("x", 4*1024+1) + `"`
	_, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7,
		templateExecuteRequest(map[string]string{"status": big}))
	var valueErr *TemplateValueValidationError
	if !errors.As(err, &valueErr) {
		t.Fatalf("error = %v, want *TemplateValueValidationError", err)
	}
	if valueErr.Fields["status"] != "oversized" {
		t.Fatalf("status code = %q, want oversized", valueErr.Fields["status"])
	}
	if executor.templateCalls != 0 {
		t.Fatalf("QueryTemplate calls = %d, want 0", executor.templateCalls)
	}
}

func TestExecuteSavedStatementStaticStatementExecutesWithNoValues(t *testing.T) {
	t.Parallel()
	statement := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders", []model.QuerySavedStatementParameterDefinition{})
	svc, _, executor, _ := newTemplateExecutionTestService(statement, nil)

	resp, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7,
		model.QuerySavedStatementExecuteRequest{MaxRows: 100})
	if err != nil {
		t.Fatalf("ExecuteSavedStatement static error: %v", err)
	}
	if resp.Status != model.QueryExecutionSuccess {
		t.Fatalf("status = %q, want success", resp.Status)
	}
	if executor.templateCalls != 1 {
		t.Fatalf("QueryTemplate calls = %d, want 1", executor.templateCalls)
	}
}

func TestExecuteSavedStatementRereadsLatestStatementEveryExecution(t *testing.T) {
	t.Parallel()
	first := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status",
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})
	updated := first
	updated.Statement = "select id from orders where status = :status and region = :region"
	updated.Parameters = []model.QuerySavedStatementParameterDefinition{
		{Name: "status", Type: model.QuerySavedStatementParameterString},
		{Name: "region", Type: model.QuerySavedStatementParameterString},
	}
	reader := &fakeSavedStatementReader{getResp: first}
	svc, _, executor, _ := newTemplateExecutionTestService(first, nil)
	reader.getResp = first
	svc.WithTemplateExecution(reader, NewTemplateStatementCompiler())

	if _, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7,
		templateExecuteRequest(map[string]string{"status": `"paid"`})); err != nil {
		t.Fatalf("first execution error: %v", err)
	}
	reader.getResp = updated
	if _, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7,
		templateExecuteRequest(map[string]string{"status": `"paid"`, "region": `"eu"`})); err != nil {
		t.Fatalf("second execution error: %v", err)
	}
	if executor.templateCalls != 2 {
		t.Fatalf("QueryTemplate calls = %d, want 2", executor.templateCalls)
	}
}

func TestExecuteSavedStatementPagesThroughTemplateRouteWithFreshHistory(t *testing.T) {
	t.Parallel()
	statement := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status",
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})
	svc, repo, executor, disclosure := newTemplateExecutionTestService(statement, nil)

	pageTwo := model.QuerySavedStatementExecuteRequest{
		Values:     map[string]json.RawMessage{"status": json.RawMessage(`"paid"`)},
		MaxRows:    100,
		Pagination: &model.QueryExecutePaginationRequest{Page: 2, PageSize: 10},
	}
	resp, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7, pageTwo)
	if err != nil {
		t.Fatalf("ExecuteSavedStatement page 2 error: %v", err)
	}
	if resp.Pagination == nil || resp.Pagination.Page != 2 {
		t.Fatalf("pagination = %+v, want page 2", resp.Pagination)
	}
	// Every template page is a fresh governed execution with history and audit.
	if executor.templateCalls != 1 || disclosure.preflightCalls != 1 {
		t.Fatalf("page chain: template calls=%d preflight=%d, want 1 each", executor.templateCalls, disclosure.preflightCalls)
	}
	if len(repo.insertedAttempts) != 1 || len(repo.auditEvents) != 1 {
		t.Fatalf("page history = %d, audit = %d, want 1 each", len(repo.insertedAttempts), len(repo.auditEvents))
	}
}

func TestExecuteSavedStatementDisclosureChangeAffectsLaterPage(t *testing.T) {
	t.Parallel()
	statement := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status",
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})
	svc, repo, executor, disclosure := newTemplateExecutionTestService(statement, nil)

	pageOne := model.QuerySavedStatementExecuteRequest{
		Values:     map[string]json.RawMessage{"status": json.RawMessage(`"paid"`)},
		MaxRows:    100,
		Pagination: &model.QueryExecutePaginationRequest{Page: 1, PageSize: 10},
	}
	if _, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7, pageOne); err != nil {
		t.Fatalf("page 1 error: %v", err)
	}
	// A disclosure-policy change now blocks page 2; it must be re-evaluated
	// per page and stop before the executor.
	disclosure.blockErr = ErrQueryDisclosureBlocked
	pageTwo := pageOne
	pageTwo.Pagination = &model.QueryExecutePaginationRequest{Page: 2, PageSize: 10}
	_, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7, pageTwo)
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("page 2 error = %v, want ErrQueryNotAllowed", err)
	}
	if executor.templateCalls != 1 {
		t.Fatalf("QueryTemplate calls = %d, want 1 (page 2 blocked before execution)", executor.templateCalls)
	}
	if len(repo.insertedAttempts) != 2 {
		t.Fatalf("history rows = %d, want 2 (page 2 rejection recorded)", len(repo.insertedAttempts))
	}
	if repo.insertedAttempts[1].Status != model.QueryExecutionRejected {
		t.Fatalf("page 2 history status = %q, want rejected", repo.insertedAttempts[1].Status)
	}
}

func TestExecuteSavedStatementRecordsRejectedAttemptForAccessDenial(t *testing.T) {
	t.Parallel()
	statement := templateStatement(1, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status",
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})
	// A repository with no credential row denies target access.
	repo := &fakeExecRepo{}
	executor := &fakeExecutor{}
	disclosure := &fakeDisclosureService{}
	svc := NewQueryExecutionService(
		fakeTargetRepo{targets: []model.QueryTarget{mysqlTarget("Staging")}},
		repo,
		&fakeResolver{},
		executor,
		NewQueryGuard(QueryGuardConfig{DefaultMaxRows: 100, HardMaxRows: 500}),
		&fakeClock{t: time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)},
		&fakeNavSchemaInspector{},
		disclosure,
	)
	svc.WithTemplateExecution(&fakeSavedStatementReader{getResp: statement}, NewTemplateStatementCompiler())

	_, err := svc.ExecuteSavedStatement(context.Background(), 1, 9001, 7,
		templateExecuteRequest(map[string]string{"status": `"paid"`}))
	if !errors.Is(err, ErrQueryNotAllowed) {
		t.Fatalf("error = %v, want ErrQueryNotAllowed", err)
	}
	if len(repo.insertedAttempts) != 1 {
		t.Fatalf("history = %d, want 1 rejected attempt", len(repo.insertedAttempts))
	}
	if repo.insertedAttempts[0].Status != model.QueryExecutionRejected {
		t.Fatalf("history status = %q, want rejected", repo.insertedAttempts[0].Status)
	}
	if executor.templateCalls != 0 {
		t.Fatalf("QueryTemplate calls = %d, want 0", executor.templateCalls)
	}
}

// TestExecuteSavedStatement_ClientCanceledDuringExecution_RecordsFailedCanceled
// proves the template cancellation path (Issue #35 AC: ordinary, paged, and
// template cancellation paths are covered): a canceled template execution
// records failed/query_canceled with the fixed safe message through the
// detached two-second Evidence Persistence Window, never the canceled request
// context, and no template values or statement text are persisted.
func TestExecuteSavedStatement_ClientCanceledDuringExecution_RecordsFailedCanceled(t *testing.T) {
	t.Parallel()
	statement := templateStatement(7, model.QuerySavedStatementPersonal,
		"select id from orders where status = :status",
		[]model.QuerySavedStatementParameterDefinition{{Name: "status", Type: model.QuerySavedStatementParameterString}})
	svc, repo, executor, _ := newTemplateExecutionTestService(statement, nil)
	executor.err = context.Canceled

	// Request context already canceled: the client disconnected mid-query.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.ExecuteSavedStatement(ctx, 7, 9001, statement.ID,
		templateExecuteRequest(map[string]string{"status": `"paid"`}))
	if !errors.Is(err, ErrQueryBackendFailure) {
		t.Fatalf("error = %v, want ErrQueryBackendFailure", err)
	}
	if len(repo.insertedAttempts) != 1 || repo.insertedAttempts[0].Status != model.QueryExecutionFailed {
		t.Fatalf("canceled template attempt must be recorded as failed: %+v", repo.insertedAttempts)
	}
	rec := repo.insertedAttempts[0]
	if rec.ErrorCode != "query_canceled" {
		t.Fatalf("error code = %q, want query_canceled", rec.ErrorCode)
	}
	if rec.ErrorMessage != "query canceled" {
		t.Fatalf("error message = %q, want fixed 'query canceled'", rec.ErrorMessage)
	}
	if len(repo.auditEvents) != 1 || repo.auditEvents[0].result != "failed" {
		t.Fatalf("audit result = %+v, want one failed", repo.auditEvents)
	}
	// Template values never reach the evidence record. The statement digest and
	// safe preview (table/column identifiers, placeholders only) are recorded
	// exactly as on the success path — forbidden content is values, credentials,
	// DSNs, and raw errors.
	if strings.Contains(asString(rec), "paid") || strings.Contains(asString(rec), testResolverDSN) {
		t.Fatal("template value or DSN leaked into canceled-attempt evidence")
	}
	assertDetachedEvidenceWindow(t, repo)
}
