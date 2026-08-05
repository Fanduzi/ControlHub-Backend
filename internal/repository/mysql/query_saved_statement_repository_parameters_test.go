package mysql

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/fan/controlhub/internal/model"
)

func TestLoadParameterDefinitionsDoesNotMutateInputStatements(t *testing.T) {
	// Given: caller-owned statements without loaded parameter definitions.
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	repo := NewQuerySavedStatementRepository(db)
	statements := []model.QuerySavedStatement{{ID: 1}}
	mock.ExpectQuery("SELECT statement_id, name, type, ordinal").
		WithArgs(uint64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"statement_id", "name", "type", "ordinal"}).
			AddRow(1, "status", "string", 0))

	// When: the repository hydrates a returned copy.
	loaded, err := repo.loadParameterDefinitions(t.Context(), statements)

	// Then: the returned statement is populated without changing the caller's slice.
	if err != nil {
		t.Fatalf("loadParameterDefinitions: %v", err)
	}
	if statements[0].Parameters != nil {
		t.Fatalf("input parameters = %#v, want unchanged nil", statements[0].Parameters)
	}
	if got, want := len(loaded[0].Parameters), 1; got != want {
		t.Fatalf("loaded parameter count = %d, want %d", got, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}
