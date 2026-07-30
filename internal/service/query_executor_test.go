// Package service provides unit tests for the query executor scan helpers.
// input: database/sql, errors, strings, testing, DATA-DOG/go-sqlmock
// output: TestNewScanPointer_*, TestNormalizeScanned_* (NULL correctness + numeric type fidelity), TestScanBoundedRows_* (payload-cap boundary per execution mode)
// pos: Unit tests verifying SQL NULL is preserved as nil, non-null numbers stay numbers, and the response-byte cap rejects paginated windows while truncating non-paged results
// note: if this file changes, update header and README.md
package service

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

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
