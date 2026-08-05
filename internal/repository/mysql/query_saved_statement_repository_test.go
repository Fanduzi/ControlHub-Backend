package mysql

import (
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/fan/controlhub/internal/model"
)

// --- escapeLike tests ---

func TestEscapeLike(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain text", input: "hello", want: "hello"},
		{name: "escapes percent", input: "100%", want: `100\%`},
		{name: "escapes underscore", input: "my_table", want: `my\_table`},
		{name: "escapes backslash", input: `path\dir`, want: `path\\dir`},
		{name: "escapes all special", input: `%_\`, want: `\%\_\\`},
		{name: "empty string", input: "", want: ""},
		{name: "multiple percents", input: "a%b%c", want: `a\%b\%c`},
		{name: "mixed special chars", input: "100% of _items", want: `100\% of \_items`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := escapeLike(tt.input)
			if got != tt.want {
				t.Errorf("escapeLike(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewQuerySavedStatementRepository(t *testing.T) {
	repo := NewQuerySavedStatementRepository(nil)
	if repo == nil {
		t.Fatal("NewQuerySavedStatementRepository returned nil")
	}
}

// --- ListVisible tests ---

func TestListVisible_ReturnsSharedAndPersonalForOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)

	// Count query
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM query_saved_statements").
		WithArgs(uint64(10), uint64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	// Select query
	rows := sqlmock.NewRows([]string{"id", "target_resource_id", "owner_user_id", "name", "statement", "scope", "created_at", "updated_at"}).
		AddRow(1, uint64(10), uint64(1), "my query", "SELECT 1", "personal", now, now).
		AddRow(2, uint64(10), uint64(2), "shared tpl", "SELECT 2", "shared_template", now, now)
	mock.ExpectQuery("SELECT id, target_resource_id").
		WithArgs(uint64(10), uint64(1), 20, 0).
		WillReturnRows(rows)
	mock.ExpectQuery("SELECT statement_id, name, type, ordinal").
		WithArgs(uint64(1), uint64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"statement_id", "name", "type", "ordinal"}))

	resp, err := repo.ListVisible(t.Context(), model.QuerySavedStatementListQuery{
		TargetResourceID: 10,
		OwnerUserID:      1,
		Page:             1,
		PageSize:         20,
	})
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp.Items))
	}
	if resp.Items[0].Scope != model.QuerySavedStatementPersonal {
		t.Errorf("expected personal scope, got %s", resp.Items[0].Scope)
	}
	if resp.Items[1].Scope != model.QuerySavedStatementSharedTemplate {
		t.Errorf("expected shared_template scope, got %s", resp.Items[1].Scope)
	}
	if resp.PageInfo.TotalItems != 2 {
		t.Errorf("expected total 2, got %d", resp.PageInfo.TotalItems)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestListVisible_ExcludesOtherUsersPersonal(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	// The WHERE clause must include owner_user_id = ? to filter personal scope
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM query_saved_statements").
		WithArgs(uint64(10), uint64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"id", "target_resource_id", "owner_user_id", "name", "statement", "scope", "created_at", "updated_at"}).
		AddRow(2, uint64(10), uint64(2), "shared tpl", "SELECT 2", "shared_template", now, now)
	mock.ExpectQuery("SELECT id, target_resource_id").
		WithArgs(uint64(10), uint64(1), 20, 0).
		WillReturnRows(rows)
	mock.ExpectQuery("SELECT statement_id, name, type, ordinal").
		WithArgs(uint64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"statement_id", "name", "type", "ordinal"}))

	resp, err := repo.ListVisible(t.Context(), model.QuerySavedStatementListQuery{
		TargetResourceID: 10,
		OwnerUserID:      1,
		Page:             1,
		PageSize:         20,
	})
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	// Only shared_template should appear; user 99's personal statement is excluded by the WHERE clause
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item (shared only), got %d", len(resp.Items))
	}
	if resp.Items[0].Scope != model.QuerySavedStatementSharedTemplate {
		t.Errorf("expected shared_template, got %s", resp.Items[0].Scope)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestListVisible_NameSearchEscapesLike(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	// Search term with special LIKE chars: 100% → 100\%
	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM query_saved_statements").
		WithArgs(uint64(10), uint64(1), `%100\%%`).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("SELECT id, target_resource_id").
		WithArgs(uint64(10), uint64(1), `%100\%%`, 20, 0).
		WillReturnRows(sqlmock.NewRows([]string{"id", "target_resource_id", "owner_user_id", "name", "statement", "scope", "created_at", "updated_at"}))

	resp, err := repo.ListVisible(t.Context(), model.QuerySavedStatementListQuery{
		TargetResourceID: 10,
		OwnerUserID:      1,
		Page:             1,
		PageSize:         20,
		Search:           "100%",
	})
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("expected 0 items, got %d", len(resp.Items))
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestListVisible_Pagination(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM query_saved_statements").
		WithArgs(uint64(10), uint64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(50))

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	rows := sqlmock.NewRows([]string{"id", "target_resource_id", "owner_user_id", "name", "statement", "scope", "created_at", "updated_at"}).
		AddRow(3, uint64(10), uint64(1), "page2 item", "SELECT 1", "personal", now, now)
	// Page 2, pageSize 10 → offset 10
	mock.ExpectQuery("SELECT id, target_resource_id").
		WithArgs(uint64(10), uint64(1), 10, 10).
		WillReturnRows(rows)
	mock.ExpectQuery("SELECT statement_id, name, type, ordinal").
		WithArgs(uint64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"statement_id", "name", "type", "ordinal"}))

	resp, err := repo.ListVisible(t.Context(), model.QuerySavedStatementListQuery{
		TargetResourceID: 10,
		OwnerUserID:      1,
		Page:             2,
		PageSize:         10,
	})
	if err != nil {
		t.Fatalf("ListVisible: %v", err)
	}
	if resp.PageInfo.Page != 2 {
		t.Errorf("expected page 2, got %d", resp.PageInfo.Page)
	}
	if resp.PageInfo.PageSize != 10 {
		t.Errorf("expected pageSize 10, got %d", resp.PageInfo.PageSize)
	}
	if resp.PageInfo.TotalPages != 5 {
		t.Errorf("expected totalPages 5, got %d", resp.PageInfo.TotalPages)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// --- GetByID tests ---

func TestGetByID_NotFound(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectQuery("SELECT id, target_resource_id").
		WithArgs(uint64(10), uint64(999)).
		WillReturnError(sql.ErrNoRows)

	_, err = repo.GetByID(t.Context(), 10, 999)
	if err == nil {
		t.Fatal("expected sql.ErrNoRows, got nil")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestGetByID_Found(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	now := time.Date(2026, 7, 28, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery("SELECT id, target_resource_id").
		WithArgs(uint64(10), uint64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "target_resource_id", "owner_user_id", "name", "statement", "scope", "created_at", "updated_at"}).
			AddRow(1, 10, 5, "my query", "SELECT 1", "personal", now, now))
	mock.ExpectQuery("SELECT statement_id, name, type, ordinal").
		WithArgs(uint64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"statement_id", "name", "type", "ordinal"}))

	s, err := repo.GetByID(t.Context(), 10, 1)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if s.ID != 1 {
		t.Errorf("expected ID 1, got %d", s.ID)
	}
	if s.Scope != model.QuerySavedStatementPersonal {
		t.Errorf("expected personal scope, got %s", s.Scope)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// --- CreateWithAudit tests ---

func TestCreateWithAudit_InsertsBoth(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO query_saved_statements").
		WithArgs(uint64(10), uint64(1), "my query", "SELECT 1", "personal").
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(1), uint64(10)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	s, err := repo.CreateWithAudit(t.Context(), 1, 10, model.QuerySavedStatementCreateRequest{
		Name:      "my query",
		Statement: "SELECT 1",
		Scope:     model.QuerySavedStatementPersonal,
	})
	if err != nil {
		t.Fatalf("CreateWithAudit: %v", err)
	}
	if s.ID != 42 {
		t.Errorf("expected ID 42, got %d", s.ID)
	}
	if s.OwnerUserID != 1 {
		t.Errorf("expected owner 1, got %d", s.OwnerUserID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCreateWithAudit_PersistsParameterDefinitionsBeforeAudit(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO query_saved_statements").
		WithArgs(uint64(10), uint64(1), "status query", "SELECT 1 WHERE status = :status", "personal").
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectExec("INSERT INTO query_saved_statement_parameters").
		WithArgs(uint64(42), "status", "string", 0).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(1), uint64(10)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	statement, err := repo.CreateWithAudit(t.Context(), 1, 10, model.QuerySavedStatementCreateRequest{
		Name:      "status query",
		Statement: "SELECT 1 WHERE status = :status",
		Scope:     model.QuerySavedStatementPersonal,
		Parameters: []model.QuerySavedStatementParameterDefinition{
			{Name: "status", Type: model.QuerySavedStatementParameterString},
		},
	})
	if err != nil {
		t.Fatalf("CreateWithAudit: %v", err)
	}
	if len(statement.Parameters) != 1 || statement.Parameters[0].Name != "status" {
		t.Fatalf("created parameters = %#v, want status definition", statement.Parameters)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func TestCreateWithAudit_AuditContainsNoStatementNameOwner(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO query_saved_statements").
		WithArgs(uint64(10), uint64(1), "secret name", "SELECT password FROM users", "personal").
		WillReturnResult(sqlmock.NewResult(1, 1))

	// The audit INSERT must NOT contain statement text, name, or owner ID.
	// Only actor_user_id, target_resource_id, fixed event_type, and fixed result.
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(1), uint64(10)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	_, err = repo.CreateWithAudit(t.Context(), 1, 10, model.QuerySavedStatementCreateRequest{
		Name:      "secret name",
		Statement: "SELECT password FROM users",
		Scope:     model.QuerySavedStatementPersonal,
	})
	if err != nil {
		t.Fatalf("CreateWithAudit: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestCreateWithAudit_AuditFailureRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO query_saved_statements").
		WithArgs(uint64(10), uint64(1), "my query", "SELECT 1", "personal").
		WillReturnResult(sqlmock.NewResult(42, 1))
	// Audit insert fails
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(1), uint64(10)).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	_, err = repo.CreateWithAudit(t.Context(), 1, 10, model.QuerySavedStatementCreateRequest{
		Name:      "my query",
		Statement: "SELECT 1",
		Scope:     model.QuerySavedStatementPersonal,
	})
	if err == nil {
		t.Fatal("expected error from audit failure, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// --- UpdateWithAudit tests ---

func TestUpdateWithAudit_NonOwnerReturnsErrNoRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE query_saved_statements").
		WithArgs("new name", "SELECT 2", uint64(10), uint64(1), uint64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0)) // 0 rows affected = not found or not owned
	mock.ExpectRollback()

	err = repo.UpdateWithAudit(t.Context(), 99, 10, 1, model.QuerySavedStatementUpdateRequest{
		Name:      "new name",
		Statement: "SELECT 2",
	}, false)
	if err == nil {
		t.Fatal("expected sql.ErrNoRows, got nil")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestUpdateWithAudit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE query_saved_statements").
		WithArgs("new name", "SELECT 2", uint64(10), uint64(1), uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM query_saved_statement_parameters").
		WithArgs(uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(1), uint64(10)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = repo.UpdateWithAudit(t.Context(), 1, 10, 1, model.QuerySavedStatementUpdateRequest{
		Name:      "new name",
		Statement: "SELECT 2",
	}, false)
	if err != nil {
		t.Fatalf("UpdateWithAudit: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestUpdateWithAudit_AuditFailureRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE query_saved_statements").
		WithArgs("new name", "SELECT 2", uint64(10), uint64(1), uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM query_saved_statement_parameters").
		WithArgs(uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(1), uint64(10)).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	err = repo.UpdateWithAudit(t.Context(), 1, 10, 1, model.QuerySavedStatementUpdateRequest{
		Name:      "new name",
		Statement: "SELECT 2",
	}, false)
	if err == nil {
		t.Fatal("expected error from audit failure, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// --- DeleteWithAudit tests ---

func TestDeleteWithAudit_NonOwnerReturnsErrNoRows(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM query_saved_statements").
		WithArgs(uint64(10), uint64(1), uint64(99)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	err = repo.DeleteWithAudit(t.Context(), 99, 10, 1, false)
	if err == nil {
		t.Fatal("expected sql.ErrNoRows, got nil")
	}
	if err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDeleteWithAudit_Success(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM query_saved_statements").
		WithArgs(uint64(10), uint64(1), uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM query_saved_statement_parameters").
		WithArgs(uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(1), uint64(10)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err = repo.DeleteWithAudit(t.Context(), 1, 10, 1, false)
	if err != nil {
		t.Fatalf("DeleteWithAudit: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func TestDeleteWithAudit_AuditFailureRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	repo := NewQuerySavedStatementRepository(db)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM query_saved_statements").
		WithArgs(uint64(10), uint64(1), uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("DELETE FROM query_saved_statement_parameters").
		WithArgs(uint64(1)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec("INSERT INTO audit_events").
		WithArgs(uint64(1), uint64(10)).
		WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()

	err = repo.DeleteWithAudit(t.Context(), 1, 10, 1, false)
	if err == nil {
		t.Fatal("expected error from audit failure, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
