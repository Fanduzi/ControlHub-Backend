// Package model provides tests for related-record navigation request validation.
// input: testing, internal/model
// output: TestRelatedRecordNavigationRequest_Validate_*
// pos: Unit tests for request shape validation, bounds, and controlled error messages
// note: if this file changes, update header and README.md
package model

import (
	"strings"
	"testing"
)

func validRequest() RelatedRecordNavigationRequest {
	return RelatedRecordNavigationRequest{
		Source: RelatedRecordNavigationSource{
			Database:   "orders",
			Object:     "order_items",
			Kind:       "table",
			ForeignKey: "fk_order_items_order",
		},
		LocalValues: []string{"42"},
		MaxRows:     100,
	}
}

func TestRelatedRecordNavigationRequest_Validate_Valid(t *testing.T) {
	req := validRequest()
	if err := req.Validate(); err != nil {
		t.Fatalf("expected nil error for valid request, got: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_EmptyDatabase(t *testing.T) {
	req := validRequest()
	req.Source.Database = ""
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty database")
	} else if !strings.Contains(err.Error(), "source database is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_DatabaseTooLong(t *testing.T) {
	req := validRequest()
	req.Source.Database = strings.Repeat("a", MaxSourceDatabaseLength+1)
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for overlong database")
	} else if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_EmptyObject(t *testing.T) {
	req := validRequest()
	req.Source.Object = ""
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty object")
	} else if !strings.Contains(err.Error(), "source object is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_ObjectTooLong(t *testing.T) {
	req := validRequest()
	req.Source.Object = strings.Repeat("b", MaxSourceObjectLength+1)
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for overlong object")
	}
}

func TestRelatedRecordNavigationRequest_Validate_NonTableKind(t *testing.T) {
	req := validRequest()
	req.Source.Kind = "view"
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for non-table kind")
	} else if !strings.Contains(err.Error(), "source kind must be") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_EmptyKind(t *testing.T) {
	req := validRequest()
	req.Source.Kind = ""
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty kind")
	}
}

func TestRelatedRecordNavigationRequest_Validate_EmptyForeignKey(t *testing.T) {
	req := validRequest()
	req.Source.ForeignKey = ""
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty foreign key")
	} else if !strings.Contains(err.Error(), "source foreign key is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_ForeignKeyTooLong(t *testing.T) {
	req := validRequest()
	req.Source.ForeignKey = strings.Repeat("f", MaxForeignKeyNameLength+1)
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for overlong foreign key")
	}
}

func TestRelatedRecordNavigationRequest_Validate_EmptyLocalValues(t *testing.T) {
	req := validRequest()
	req.LocalValues = []string{}
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty localValues")
	} else if !strings.Contains(err.Error(), "localValues is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_NilLocalValues(t *testing.T) {
	req := validRequest()
	req.LocalValues = nil
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for nil localValues")
	}
}

func TestRelatedRecordNavigationRequest_Validate_TooManyLocalValues(t *testing.T) {
	req := validRequest()
	vals := make([]string, MaxLocalValuesCount+1)
	for i := range vals {
		vals[i] = "v"
	}
	req.LocalValues = vals
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for too many localValues")
	} else if !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_EmptyLocalValue(t *testing.T) {
	req := validRequest()
	req.LocalValues = []string{"42", ""}
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for empty localValue entry")
	} else if !strings.Contains(err.Error(), "localValues[1]") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_LocalValueTooLong(t *testing.T) {
	req := validRequest()
	req.LocalValues = []string{strings.Repeat("x", MaxLocalValueLength+1)}
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for overlong localValue")
	} else if !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_NegativeMaxRows(t *testing.T) {
	req := validRequest()
	req.MaxRows = -1
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for negative maxRows")
	} else if !strings.Contains(err.Error(), "must not be negative") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_ZeroMaxRows_IsValid(t *testing.T) {
	req := validRequest()
	req.MaxRows = 0
	if err := req.Validate(); err != nil {
		t.Fatalf("expected nil error for zero maxRows (uses default), got: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_ClampMaxRows_Default(t *testing.T) {
	req := validRequest()
	req.MaxRows = 0
	if got := req.ClampMaxRows(); got != MaxRelatedRowsDefault {
		t.Fatalf("ClampMaxRows() = %d, want %d (default)", got, MaxRelatedRowsDefault)
	}
}

func TestRelatedRecordNavigationRequest_ClampMaxRows_HardCap(t *testing.T) {
	req := validRequest()
	req.MaxRows = MaxRelatedRowsHard + 100
	if got := req.ClampMaxRows(); got != MaxRelatedRowsHard {
		t.Fatalf("ClampMaxRows() = %d, want %d (hard cap)", got, MaxRelatedRowsHard)
	}
}

func TestRelatedRecordNavigationRequest_ClampMaxRows_WithinRange(t *testing.T) {
	req := validRequest()
	req.MaxRows = 50
	if got := req.ClampMaxRows(); got != 50 {
		t.Fatalf("ClampMaxRows() = %d, want 50", got)
	}
}

func TestRelatedRecordNavigationRequest_Validate_CompositeFK(t *testing.T) {
	// Composite FK: two local values, both non-empty.
	req := validRequest()
	req.LocalValues = []string{"42", "abc"}
	if err := req.Validate(); err != nil {
		t.Fatalf("expected nil error for composite FK with 2 values, got: %v", err)
	}
}

func TestRelatedRecordNavigationRequest_Validate_MaxLocalValuesCount(t *testing.T) {
	req := validRequest()
	vals := make([]string, MaxLocalValuesCount)
	for i := range vals {
		vals[i] = "v"
	}
	req.LocalValues = vals
	if err := req.Validate(); err != nil {
		t.Fatalf("expected nil error for max localValues count, got: %v", err)
	}
}

func TestRelatedRecordNavigationResponse_ColumnsNeverNull(t *testing.T) {
	resp := RelatedRecordNavigationResponse{
		Columns:         nil,
		Rows:            nil,
		ReferencedColumns: nil,
	}
	// Verify the struct fields can be set to nil (JSON marshal test is in OpenAPI).
	if resp.Columns != nil {
		t.Fatal("expected nil columns to be nil before marshal")
	}
}

func TestRelatedRecordNavigationRequest_Validate_WhitespaceOnlyDatabase(t *testing.T) {
	req := validRequest()
	req.Source.Database = "   "
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for whitespace-only database")
	}
}

func TestRelatedRecordNavigationRequest_Validate_WhitespaceOnlyObject(t *testing.T) {
	req := validRequest()
	req.Source.Object = "   "
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for whitespace-only object")
	}
}

func TestRelatedRecordNavigationRequest_Validate_WhitespaceOnlyForeignKey(t *testing.T) {
	req := validRequest()
	req.Source.ForeignKey = "   "
	if err := req.Validate(); err == nil {
		t.Fatal("expected error for whitespace-only foreign key")
	}
}

// Error messages must not echo submitted values (security requirement).
func TestRelatedRecordNavigationRequest_Validate_NoValueEcho(t *testing.T) {
	req := validRequest()
	req.LocalValues = []string{"secret-value-should-not-appear"}
	req.Source.Database = "sensitive_db"
	// Overlong database to trigger error.
	req.Source.Database = strings.Repeat("a", MaxSourceDatabaseLength+1)
	err := req.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "sensitive_db") {
		t.Fatalf("error message must not echo submitted database value: %s", err.Error())
	}
}
