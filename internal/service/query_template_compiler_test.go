// Package service provides tests for the governed template statement compiler.
// input: errors, reflect, regexp, strconv, strings, testing, DATA-DOG/go-sqlmock
// output: TestTemplateStatementCompiler* (source-order, guard, rejection, and driver-binding proofs)
// pos: Test-first proof that server-owned named placeholders become positional driver bindings without bypassing the AST guard
package service

import (
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTemplateStatementCompilerBindsValuesInSourceOrderAndPassesGuard(t *testing.T) {
	t.Parallel()

	compiler := NewTemplateStatementCompiler()
	compiled, err := compiler.Compile(TemplateStatementInput{
		Statement: "select id from orders where status = :status and id > :minimum_id",
		Definitions: []TemplateParameterDefinition{
			{Name: "status", Type: TemplateParameterString},
			{Name: "minimum_id", Type: TemplateParameterInteger},
		},
		Values: map[string]any{"status": "paid", "minimum_id": float64(42)},
	})
	if err != nil {
		t.Fatalf("Compile error: %v", err)
	}

	if got, want := compiled.Statement, "select id from orders where `status` = ? and id > ?"; got != want {
		t.Fatalf("compiled statement = %q, want %q", got, want)
	}
	if want := []any{"paid", int64(42)}; !reflect.DeepEqual(compiled.Args, want) {
		t.Fatalf("compiled args = %#v, want %#v", compiled.Args, want)
	}
	if strings.Contains(compiled.Statement, "paid") || strings.Contains(compiled.Statement, "42") {
		t.Fatalf("compiled statement leaks a bound value: %q", compiled.Statement)
	}

	guarded, err := newTestGuard().Guard(compiled.Statement, 100)
	if err != nil {
		t.Fatalf("Guard compiler output: %v", err)
	}
	if !strings.Contains(strings.ToLower(guarded.ExecutableSQL), "limit 101") {
		t.Fatalf("guarded executable SQL = %q, want backend-owned limit 101", guarded.ExecutableSQL)
	}
}

func TestTemplateStatementCompilerRejectsRepeatedPlaceholder(t *testing.T) {
	t.Parallel()

	_, err := NewTemplateStatementCompiler().Compile(TemplateStatementInput{
		Statement: "select id from orders where status = :status or previous_status = :status",
		Definitions: []TemplateParameterDefinition{
			{Name: "status", Type: TemplateParameterString},
		},
		Values: map[string]any{"status": "paid"},
	})
	if !errors.Is(err, ErrTemplateParameterInvalid) {
		t.Fatalf("Compile error = %v, want repeated placeholder rejection", err)
	}
}

func TestTemplateStatementCompilerKeepsGuardedSQLPositionalForDriverBinding(t *testing.T) {
	t.Parallel()

	guarded, err := NewTemplateStatementCompiler().CompileAndGuard(newTestGuard(), TemplateStatementInput{
		Statement:   "select id from orders where status = :status",
		Definitions: []TemplateParameterDefinition{{Name: "status", Type: TemplateParameterString}},
		Values:      map[string]any{"status": "paid"},
	}, 100)
	if err != nil {
		t.Fatalf("CompileAndGuard error: %v", err)
	}
	if got, want := guarded.query.ExecutableSQL, "select id from orders where `status` = ? limit 101"; got != want {
		t.Fatalf("guarded executable SQL = %q, want %q", got, want)
	}
	if want := []any{"paid"}; !reflect.DeepEqual(guarded.args, want) {
		t.Fatalf("guarded args = %#v, want %#v", guarded.args, want)
	}
}

func TestTemplateStatementCompilerKeepsStaticStatementsValid(t *testing.T) {
	t.Parallel()

	compiled, err := NewTemplateStatementCompiler().Compile(TemplateStatementInput{Statement: "select * from orders"})
	if err != nil {
		t.Fatalf("Compile static statement error: %v", err)
	}
	if compiled.Statement != "select * from orders" {
		t.Fatalf("compiled static statement = %q, want unchanged statement", compiled.Statement)
	}
	if len(compiled.Args) != 0 {
		t.Fatalf("compiled static args = %#v, want empty", compiled.Args)
	}
}

func TestTemplateStatementCompilerRejectsInvalidDeclarationsAndValues(t *testing.T) {
	t.Parallel()

	longValue := strings.Repeat("x", templateMaxValueBytes+1)
	tests := []struct {
		name        string
		statement   string
		definitions []TemplateParameterDefinition
		values      map[string]any
	}{
		{
			name:        "missing value",
			statement:   "select * from orders where status = :status",
			definitions: []TemplateParameterDefinition{{Name: "status", Type: TemplateParameterString}},
		},
		{
			name:        "unknown value",
			statement:   "select * from orders where status = :status",
			definitions: []TemplateParameterDefinition{{Name: "status", Type: TemplateParameterString}},
			values:      map[string]any{"status": "paid", "other": "hidden"},
		},
		{
			name:      "duplicate declaration",
			statement: "select * from orders where status = :status",
			definitions: []TemplateParameterDefinition{
				{Name: "status", Type: TemplateParameterString},
				{Name: "status", Type: TemplateParameterString},
			},
			values: map[string]any{"status": "paid"},
		},
		{
			name:        "invalid name",
			statement:   "select * from orders where status = :Status",
			definitions: []TemplateParameterDefinition{{Name: "Status", Type: TemplateParameterString}},
			values:      map[string]any{"Status": "paid"},
		},
		{
			name:        "unsupported type",
			statement:   "select * from orders where status = :status",
			definitions: []TemplateParameterDefinition{{Name: "status", Type: TemplateParameterType("json")}},
			values:      map[string]any{"status": map[string]any{"value": "paid"}},
		},
		{
			name:        "mismatched value",
			statement:   "select * from orders where id = :id",
			definitions: []TemplateParameterDefinition{{Name: "id", Type: TemplateParameterInteger}},
			values:      map[string]any{"id": "42"},
		},
		{
			name:        "oversized string",
			statement:   "select * from orders where status = :status",
			definitions: []TemplateParameterDefinition{{Name: "status", Type: TemplateParameterString}},
			values:      map[string]any{"status": longValue},
		},
		{
			name:        "malformed decimal",
			statement:   "select * from orders where total >= :minimum_total",
			definitions: []TemplateParameterDefinition{{Name: "minimum_total", Type: TemplateParameterDecimal}},
			values:      map[string]any{"minimum_total": "not-a-number"},
		},
		{
			name:      "too many declarations",
			statement: "select * from orders",
			definitions: func() []TemplateParameterDefinition {
				definitions := make([]TemplateParameterDefinition, templateMaxParameters+1)
				for i := range definitions {
					definitions[i] = TemplateParameterDefinition{Name: "parameter_" + strconv.Itoa(i), Type: TemplateParameterString}
				}
				return definitions
			}(),
		},
		{
			name:        "quoted marker",
			statement:   "select ':status'",
			definitions: []TemplateParameterDefinition{{Name: "status", Type: TemplateParameterString}},
			values:      map[string]any{"status": "secret-value"},
		},
		{
			name:        "comment marker",
			statement:   "select 1 -- :status\n",
			definitions: []TemplateParameterDefinition{{Name: "status", Type: TemplateParameterString}},
			values:      map[string]any{"status": "secret-value"},
		},
		{
			name:        "identifier position",
			statement:   "select * from :table",
			definitions: []TemplateParameterDefinition{{Name: "table", Type: TemplateParameterString}},
			values:      map[string]any{"table": "orders"},
		},
		{
			name:        "list marker",
			statement:   "select * from orders where id in (::ids)",
			definitions: []TemplateParameterDefinition{{Name: "ids", Type: TemplateParameterString}},
			values:      map[string]any{"ids": "1,2"},
		},
		{
			name:        "multiple statements",
			statement:   "select * from orders where id = :id; select * from orders",
			definitions: []TemplateParameterDefinition{{Name: "id", Type: TemplateParameterInteger}},
			values:      map[string]any{"id": int64(42)},
		},
	}

	compiler := NewTemplateStatementCompiler()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			_, err := compiler.Compile(TemplateStatementInput{
				Statement:   test.statement,
				Definitions: test.definitions,
				Values:      test.values,
			})
			if !errors.Is(err, ErrTemplateParameterInvalid) && !errors.Is(err, ErrTemplateStatementInvalid) {
				t.Fatalf("Compile error = %v, want controlled template error", err)
			}
			if strings.Contains(err.Error(), "secret-value") || strings.Contains(err.Error(), "hidden") {
				t.Fatalf("Compile error leaks a supplied value: %v", err)
			}
		})
	}
}

func TestTemplateStatementCompilerLeavesReadOnlyDecisionsToQueryGuard(t *testing.T) {
	t.Parallel()

	tests := []string{
		"update orders set status = 'paid'",
		"delete from orders",
		"select * from orders for update",
		"select sleep(5)",
		"select load_file('/etc/passwd')",
		"select get_lock('name', 1)",
		"set @value = 1",
	}
	compiler := NewTemplateStatementCompiler()
	guard := newTestGuard()
	for _, statement := range tests {
		statement := statement
		t.Run(statement, func(t *testing.T) {
			t.Parallel()
			compiled, err := compiler.Compile(TemplateStatementInput{Statement: statement})
			if err != nil {
				t.Fatalf("Compile error = %v, want compiler to leave statement-shape rejection to guard", err)
			}
			if _, err := guard.Guard(compiled.Statement, 100); !errors.Is(err, ErrQueryStatementNotAllowed) {
				t.Fatalf("Guard(%q) error = %v, want ErrQueryStatementNotAllowed", compiled.Statement, err)
			}
		})
	}
}

func TestGuardedTemplateStatementUsesDatabaseSQLPositionalArguments(t *testing.T) {
	t.Parallel()

	guarded, err := NewTemplateStatementCompiler().CompileAndGuard(newTestGuard(), TemplateStatementInput{
		Statement: "select id from orders where status = :status and id > :minimum_id",
		Definitions: []TemplateParameterDefinition{
			{Name: "status", Type: TemplateParameterString},
			{Name: "minimum_id", Type: TemplateParameterInteger},
		},
		Values: map[string]any{"status": "paid", "minimum_id": int64(42)},
	}, 100)
	if err != nil {
		t.Fatalf("CompileAndGuard error: %v", err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(regexp.QuoteMeta(guarded.query.ExecutableSQL)).
		WithArgs("paid", int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))

	rows, err := db.Query(guarded.query.ExecutableSQL, guarded.args...)
	if err != nil {
		t.Fatalf("database/sql query error: %v", err)
	}
	if !rows.Next() {
		t.Fatal("database/sql query returned no rows")
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("rows.Close error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("driver binding expectations: %v", err)
	}
}

func TestGuardedTemplateStatementBindsDecimalAndBooleanValues(t *testing.T) {
	t.Parallel()

	guarded, err := NewTemplateStatementCompiler().CompileAndGuard(newTestGuard(), TemplateStatementInput{
		Statement: "select id from orders where total >= :minimum_total and enabled = :enabled",
		Definitions: []TemplateParameterDefinition{
			{Name: "minimum_total", Type: TemplateParameterDecimal},
			{Name: "enabled", Type: TemplateParameterBoolean},
		},
		Values: map[string]any{"minimum_total": "100.50", "enabled": true},
	}, 100)
	if err != nil {
		t.Fatalf("CompileAndGuard error: %v", err)
	}

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery(regexp.QuoteMeta(guarded.query.ExecutableSQL)).
		WithArgs("100.50", true).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))

	rows, err := db.Query(guarded.query.ExecutableSQL, guarded.args...)
	if err != nil {
		t.Fatalf("database/sql query error: %v", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("rows.Close error: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("driver binding expectations: %v", err)
	}
}
