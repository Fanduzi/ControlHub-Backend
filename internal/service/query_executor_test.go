// Package service provides unit tests for the query executor scan helpers.
// input: context, database/sql, errors, regexp, strings, testing, unicode/utf8, DATA-DOG/go-sqlmock
// output: TestNewScanPointer_*, TestNormalizeScanned_*, TestScanBoundedRows_*, TestMySQLQueryExecutorQueryTemplate* (executor binding and cap behavior)
// pos: Unit tests verifying SQL NULL is preserved as nil, non-null numbers stay numbers, cellTruncated flags align only with kept rows (8192-byte cell cap, rune-safe cuts, sentinel/budget-dropped rows excluded), and compiler-owned template SQL binds through the read-only executor
// note: if this file changes, update header and README.md
package service

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestNormalizeScanned_NullIntegerIsNil(t *testing.T) {
	t.Parallel()
	p := newScanPointer("BIGINT") // *sql.NullInt64, zero value -> Valid=false
	if got := normalizeScanned(p); got != nil {
		t.Fatalf("NULL BIGINT = %v (%T), want nil", got, got)
	}
}

func TestNormalizeScanned_NonNullIntegerIsNumber(t *testing.T) {
	t.Parallel()
	p := newScanPointer("BIGINT")
	n := p.(*sql.NullInt64)
	n.Valid, n.Int64 = true, 42
	got := normalizeScanned(p)
	i, ok := got.(int64)
	if !ok || i != 42 {
		t.Fatalf("non-null BIGINT = %v (%T), want int64(42)", got, got)
	}
}

func TestNormalizeScanned_NullFloatIsNil(t *testing.T) {
	t.Parallel()
	p := newScanPointer("DOUBLE")
	if got := normalizeScanned(p); got != nil {
		t.Fatalf("NULL DOUBLE = %v, want nil", got)
	}
}

func TestNormalizeScanned_NonNullFloatIsNumber(t *testing.T) {
	t.Parallel()
	p := newScanPointer("DOUBLE")
	f := p.(*sql.NullFloat64)
	f.Valid, f.Float64 = true, 1.5
	got := normalizeScanned(p)
	v, ok := got.(float64)
	if !ok || v != 1.5 {
		t.Fatalf("non-null DOUBLE = %v (%T), want float64(1.5)", got, got)
	}
}

func TestNormalizeScanned_NullStringIsNil(t *testing.T) {
	t.Parallel()
	p := newScanPointer("VARCHAR")
	if got := normalizeScanned(p); got != nil {
		t.Fatalf("NULL VARCHAR = %v, want nil", got)
	}
}

func TestNormalizeScanned_NonNullStringIsString(t *testing.T) {
	t.Parallel()
	p := newScanPointer("VARCHAR")
	s := p.(*sql.NullString)
	s.Valid, s.String = true, "alpha"
	if got := normalizeScanned(p); got != "alpha" {
		t.Fatalf("non-null VARCHAR = %v, want alpha", got)
	}
}

func TestNormalizeScanned_NullTimeIsNil(t *testing.T) {
	t.Parallel()
	p := newScanPointer("DATETIME")
	if got := normalizeScanned(p); got != nil {
		t.Fatalf("NULL DATETIME = %v, want nil", got)
	}
}

func TestNormalizeScanned_NonNullTimeIsTime(t *testing.T) {
	t.Parallel()
	p := newScanPointer("DATETIME")
	tm := p.(*sql.NullTime)
	want := time.Date(2026, 6, 22, 8, 0, 0, 0, time.UTC)
	tm.Valid, tm.Time = true, want
	got := normalizeScanned(p)
	if v, ok := got.(time.Time); !ok || !v.Equal(want) {
		t.Fatalf("non-null DATETIME = %v, want %v", got, want)
	}
}

func TestNormalizeScanned_NullBoolIsNil(t *testing.T) {
	t.Parallel()
	p := newScanPointer("BOOL")
	if got := normalizeScanned(p); got != nil {
		t.Fatalf("NULL BOOL = %v, want nil", got)
	}
}

func TestNormalizeScanned_UnknownTypeDefaultsToNullString_NilForNull(t *testing.T) {
	t.Parallel()
	// Unknown/empty database type -> NullString; a NULL stays nil.
	p := newScanPointer("")
	if _, ok := p.(*sql.NullString); !ok {
		t.Fatalf("unknown type pointer = %T, want *sql.NullString", p)
	}
	if got := normalizeScanned(p); got != nil {
		t.Fatalf("NULL unknown type = %v, want nil", got)
	}
}

// payloadRows builds real *sql.Rows over three 40-byte string cells via sqlmock
// so scanBoundedRows exercises the same database/sql scan path as production.
func payloadRows(t *testing.T) *sql.Rows {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mock.ExpectQuery("select payload from t").WillReturnRows(
		sqlmock.NewRows([]string{"payload"}).
			AddRow(strings.Repeat("a", 40)).
			AddRow(strings.Repeat("b", 40)).
			AddRow(strings.Repeat("c", 40)),
	)
	rows, err := db.Query("select payload from t")
	if err != nil {
		t.Fatalf("query mock rows: %v", err)
	}
	return rows
}

func TestScanBoundedRows_PaginatedPayloadOverflowReturnsResultTooLarge(t *testing.T) {
	t.Parallel()
	// Given a paginated window whose third 40-byte row overflows a 100-byte cap.
	e := NewMySQLQueryExecutor(QueryExecutorCaps{MaxResponseBytes: 100})

	// When the paginated window scan hits the response-byte cap.
	result, err := e.scanBoundedRows(payloadRows(t), 10, true)

	// Then the whole window is rejected: a partial page would let the next
	// fixed offset silently skip rows the operator never received.
	if !errors.Is(err, ErrQueryResultTooLarge) {
		t.Fatalf("paginated overflow error = %v, want ErrQueryResultTooLarge", err)
	}
	if len(result.Rows) != 0 || result.RowCount != 0 || result.Truncated {
		t.Fatalf("paginated overflow result = %+v, want no partial rows", result)
	}
}

func TestScanBoundedRows_NonPaginatedPayloadOverflowKeepsTruncatedSuccess(t *testing.T) {
	t.Parallel()
	// Given the same overflow in the non-paginated mode.
	e := NewMySQLQueryExecutor(QueryExecutorCaps{MaxResponseBytes: 100})

	// When the scan hits the response-byte cap.
	result, err := e.scanBoundedRows(payloadRows(t), 10, false)

	// Then the existing bounded-success contract is unchanged: rows before the
	// cap are returned and the result is marked truncated.
	if err != nil {
		t.Fatalf("non-paginated overflow error = %v, want truncated success", err)
	}
	if result.RowCount != 2 || len(result.Rows) != 2 || !result.Truncated {
		t.Fatalf("non-paginated overflow result = %+v, want 2 rows truncated", result)
	}
}

func TestScanBoundedRows_PaginatedRowLimitStopKeepsTruncatedSuccess(t *testing.T) {
	t.Parallel()
	// Given a paginated window that stops on the row limit, not the byte cap.
	e := NewMySQLQueryExecutor(QueryExecutorCaps{})

	// When the scan reads the sentinel row past the two-row window.
	result, err := e.scanBoundedRows(payloadRows(t), 2, true)

	// Then the window is complete: truncation only signals the next page.
	if err != nil {
		t.Fatalf("paginated row-limit stop error = %v, want success", err)
	}
	if result.RowCount != 2 || len(result.Rows) != 2 || !result.Truncated {
		t.Fatalf("paginated row-limit result = %+v, want full 2-row window truncated", result)
	}
}

// --- T8-B: per-cell truncation evidence from the production scanner ---
//
// scanBoundedRows is the single seam shared by Query, QueryTemplate, and
// QueryRelatedRecords, so every retained row must carry an aligned
// cellTruncated flag row produced in the same pass as the delivered value.

// scanTextRows builds real *sql.Rows over one TEXT column with the given
// string cells via sqlmock, exercising the same database/sql scan path as
// production.
func scanTextRows(t *testing.T, cells ...string) *sql.Rows {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	rows := sqlmock.NewRowsWithColumnDefinition(
		sqlmock.NewColumn("payload").OfType("TEXT", ""),
	)
	for _, c := range cells {
		rows = rows.AddRow(c)
	}
	mock.ExpectQuery("select payload from t").WillReturnRows(rows)
	got, err := db.Query("select payload from t")
	if err != nil {
		t.Fatalf("query mock rows: %v", err)
	}
	return got
}

func TestScanBoundedRows_CellTruncatedMatrixFlagsOversizedCell(t *testing.T) {
	t.Parallel()
	e := NewMySQLQueryExecutor(QueryExecutorCaps{})

	result, err := e.scanBoundedRows(scanTextRows(t, strings.Repeat("a", 8193)), 10, false)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if len(result.CellTruncated) != 1 || len(result.CellTruncated[0]) != 1 || !result.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[true]]", result.CellTruncated)
	}
	got, ok := result.Rows[0][0].(string)
	if !ok || len(got) != ResultCellMaxBytes {
		t.Fatalf("delivered cell len = %d, want %d", len(got), ResultCellMaxBytes)
	}
}

func TestScanBoundedRows_CellBoundaries8191and8192NotTruncated(t *testing.T) {
	t.Parallel()
	e := NewMySQLQueryExecutor(QueryExecutorCaps{})

	result, err := e.scanBoundedRows(scanTextRows(t, strings.Repeat("a", 8191), strings.Repeat("b", 8192)), 10, false)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if len(result.CellTruncated) != 2 || result.CellTruncated[0][0] || result.CellTruncated[1][0] {
		t.Fatalf("cellTruncated = %v, want [[false],[false]]", result.CellTruncated)
	}
	if got := result.Rows[1][0].(string); len(got) != 8192 {
		t.Fatalf("8192-byte cell delivered len = %d, want full 8192", len(got))
	}
}

func TestScanBoundedRows_MultiByteBoundaryNeverSplitsRune(t *testing.T) {
	t.Parallel()
	e := NewMySQLQueryExecutor(QueryExecutorCaps{})

	// 8190 ASCII + one 3-byte rune = 8193 bytes: the rune must not be split,
	// so the delivered prefix is 8190 bytes, not 8192.
	tricky := strings.Repeat("a", 8190) + "界"
	result, err := e.scanBoundedRows(scanTextRows(t, tricky), 10, false)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	got := result.Rows[0][0].(string)
	if len(result.CellTruncated) != 1 || len(result.CellTruncated[0]) != 1 || !result.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[true]]", result.CellTruncated)
	}
	if len(got) != 8190 {
		t.Fatalf("delivered len = %d, want 8190 bytes (rune-safe cut)", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("delivered value is not valid UTF-8: %q...", got[len(got)-8:])
	}
}

func TestScanBoundedRows_FourByteBoundaryNeverSplitsRune(t *testing.T) {
	t.Parallel()
	e := NewMySQLQueryExecutor(QueryExecutorCaps{})

	// 8189 ASCII + one 4-byte rune = 8193 bytes: the emoji must not be split,
	// so the delivered prefix is 8189 bytes, not 8192.
	tricky := strings.Repeat("a", 8189) + "\U0001F600"
	result, err := e.scanBoundedRows(scanTextRows(t, tricky), 10, false)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	got := result.Rows[0][0].(string)
	if len(result.CellTruncated) != 1 || !result.CellTruncated[0][0] {
		t.Fatalf("cellTruncated = %v, want [[true]]", result.CellTruncated)
	}
	if len(got) != 8189 || !utf8.ValidString(got) {
		t.Fatalf("delivered len = %d valid = %v, want 8189 valid UTF-8", len(got), utf8.ValidString(got))
	}
}

func TestScanBoundedRows_CleanPageProducesRealAllFalseMatrix(t *testing.T) {
	t.Parallel()
	e := NewMySQLQueryExecutor(QueryExecutorCaps{})

	result, err := e.scanBoundedRows(scanTextRows(t, "alpha", "", "beta"), 10, false)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	// WHY: a non-empty page must carry an aligned matrix — the scanner emits
	// it honestly (all-false), never synthesized downstream.
	if len(result.CellTruncated) != 3 {
		t.Fatalf("matrix rows = %d, want 3", len(result.CellTruncated))
	}
	for r, flags := range result.CellTruncated {
		if len(flags) != 1 || flags[0] {
			t.Fatalf("matrix[%d] = %v, want [false]", r, flags)
		}
	}
}

func TestScanBoundedRows_NullNumericBoolTimeCellsFlagFalse(t *testing.T) {
	t.Parallel()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	e := NewMySQLQueryExecutor(QueryExecutorCaps{})

	mock.ExpectQuery("select mixed from t").WillReturnRows(
		sqlmock.NewRowsWithColumnDefinition(
			sqlmock.NewColumn("n").OfType("BIGINT", int64(0)),
			sqlmock.NewColumn("f").OfType("DOUBLE", float64(0)),
			sqlmock.NewColumn("b").OfType("BOOL", false),
			sqlmock.NewColumn("d").OfType("DATETIME", time.Time{}),
			sqlmock.NewColumn("s").OfType("VARCHAR", ""),
		).AddRow(int64(7), 1.5, true, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), nil),
	)
	rows, err := db.Query("select mixed from t")
	if err != nil {
		t.Fatalf("query mock rows: %v", err)
	}

	result, err := e.scanBoundedRows(rows, 10, false)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if result.CellTruncated[0][0] || result.CellTruncated[0][1] || result.CellTruncated[0][2] ||
		result.CellTruncated[0][3] || result.CellTruncated[0][4] {
		t.Fatalf("typed/null flags = %v, want all false", result.CellTruncated[0])
	}
	if result.Rows[0][0] != int64(7) || result.Rows[0][1] != 1.5 || result.Rows[0][2] != true || result.Rows[0][4] != nil {
		t.Fatalf("typed values mutated: %+v", result.Rows[0])
	}
}

func TestScanBoundedRows_SentinelRowTruncationNeverReachesPage(t *testing.T) {
	t.Parallel()
	e := NewMySQLQueryExecutor(QueryExecutorCaps{})

	// Paginated window of 2 + a huge sentinel row that is never scanned: the
	// released page's matrix must describe only the two kept rows.
	result, err := e.scanBoundedRows(
		scanTextRows(t, "x", "y", strings.Repeat("a", 99999)), 2, true)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if !result.Truncated {
		t.Fatal("truncated = false, want true (sentinel proves next page exists)")
	}
	if len(result.CellTruncated) != 2 || result.CellTruncated[0][0] || result.CellTruncated[1][0] {
		t.Fatalf("cellTruncated = %v, want [[false],[false]] — sentinel rows never enter the matrix", result.CellTruncated)
	}
}

func TestScanBoundedRows_BudgetDroppedRowLeavesNoFlag(t *testing.T) {
	t.Parallel()
	e := NewMySQLQueryExecutor(QueryExecutorCaps{MaxResponseBytes: 12})

	// Non-paginated: the third row's 8193-byte cell would truncate to an
	// 8192-byte delivered prefix (flag true), but its 8192 bytes overflow the
	// 12-byte response budget after the first two 3-byte rows — the row is
	// dropped entirely and its flag must not linger in the matrix either.
	result, err := e.scanBoundedRows(
		scanTextRows(t, "aaa", "bbb", strings.Repeat("c", 8193)), 10, false)
	if err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if result.RowCount != 2 || len(result.Rows) != 2 ||
		result.Rows[0][0] != "aaa" || result.Rows[1][0] != "bbb" {
		t.Fatalf("rows = %v (count %d), want exactly [[aaa],[bbb]]", result.Rows, result.RowCount)
	}
	if !result.Truncated {
		t.Fatal("truncated = false, want true — the budget-dropped row proves more rows existed")
	}
	if len(result.CellTruncated) != 2 || len(result.CellTruncated[0]) != 1 ||
		len(result.CellTruncated[1]) != 1 ||
		result.CellTruncated[0][0] || result.CellTruncated[1][0] {
		t.Fatalf("cellTruncated = %v, want exactly [[false],[false]] — the dropped row's true flag must not linger", result.CellTruncated)
	}
	// The retained page carries no truncated cell, so a capability-less client
	// must still pass the delivery gate — budget drops never cause refusal.
	decision, derr := DecideResultDelivery(ResultDeliveryInput{
		Columns:       result.Columns,
		Rows:          result.Rows,
		CellTruncated: result.CellTruncated,
	})
	if derr != nil {
		t.Fatalf("delivery gate refused a clean budget-truncated page: %v", derr)
	}
	if len(decision.Rows) != 2 {
		t.Fatalf("decision rows = %v, want the two kept rows", decision.Rows)
	}

	// Paginated control with the same input: the whole window is refused —
	// no half page, no rows, no matrix.
	paged, perr := e.scanBoundedRows(
		scanTextRows(t, "aaa", "bbb", strings.Repeat("c", 8193)), 10, true)
	if !errors.Is(perr, ErrQueryResultTooLarge) {
		t.Fatalf("paginated overflow error = %v, want ErrQueryResultTooLarge", perr)
	}
	if len(paged.Rows) != 0 || len(paged.CellTruncated) != 0 || paged.RowCount != 0 {
		t.Fatalf("paginated overflow result = %+v, want empty result — no partial page", paged)
	}
}

func TestMySQLQueryExecutorQueryTemplateBindsCompilerOwnedStatement(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	executor := NewMySQLQueryExecutor(QueryExecutorCaps{})
	executor.openDB = func(string) (*sql.DB, error) { return db, nil }
	statement := GuardedTemplateStatement{
		query: GuardedQuery{
			ExecutableSQL: "select id from orders where `status` = ? limit 101",
			LimitApplied:  100,
		},
		args: []any{"paid"},
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(statement.query.ExecutableSQL)).
		WithArgs("paid").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(7)))
	mock.ExpectRollback()

	var queryExecutor QueryDatabaseExecutor = executor
	result, err := queryExecutor.QueryTemplate(context.Background(), "ignored-dsn", statement)
	if err != nil {
		t.Fatalf("QueryTemplate error: %v", err)
	}
	if result.RowCount != 1 || len(result.Rows) != 1 || result.Rows[0][0] != "7" {
		t.Fatalf("QueryTemplate result = %+v, want one row containing string(7)", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("executor expectations: %v", err)
	}
}

func TestMySQLQueryExecutorQueryTemplatePreservesPaginatedPayloadCap(t *testing.T) {
	t.Parallel()

	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New error: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	executor := NewMySQLQueryExecutor(QueryExecutorCaps{MaxResponseBytes: 100})
	executor.openDB = func(string) (*sql.DB, error) { return db, nil }
	statement := GuardedTemplateStatement{
		query: GuardedQuery{
			ExecutableSQL: "select payload from orders where `status` = ? limit 11",
			LimitApplied:  10,
			ResultLimit:   10,
		},
		args: []any{"paid"},
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(statement.query.ExecutableSQL)).
		WithArgs("paid").
		WillReturnRows(sqlmock.NewRows([]string{"payload"}).
			AddRow(strings.Repeat("a", 40)).
			AddRow(strings.Repeat("b", 40)).
			AddRow(strings.Repeat("c", 40)))
	mock.ExpectRollback()

	_, err = executor.QueryTemplate(context.Background(), "ignored-dsn", statement)
	if !errors.Is(err, ErrQueryResultTooLarge) {
		t.Fatalf("QueryTemplate paginated overflow error = %v, want ErrQueryResultTooLarge", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("executor expectations: %v", err)
	}
}
