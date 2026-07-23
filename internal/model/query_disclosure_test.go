// Package model provides tests for result-disclosure policy domain validators.
// input: strings, testing
// output: TestResultDisclosureMode_*, TestResultDisclosurePolicyUpsertRequest_*
// pos: Unit tests for disclosure mode and upsert-request fail-closed validators
// note: if this file changes, update header and README.md
package model

import (
	"strings"
	"testing"
)

func TestResultDisclosureMode_ValidPersistablePasses(t *testing.T) {
	t.Parallel()
	// WHY: only raw_copy_allowed and masked_no_copy are persisted; these two
	// values must pass validation so policies can be written to the database.
	for _, m := range []ResultDisclosureMode{
		ResultDisclosureRawCopyAllowed,
		ResultDisclosureMaskedNoCopy,
	} {
		if err := m.Validate(); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", m, err)
		}
	}
}

func TestResultDisclosureMode_BlockedAndUnknownFailClosed(t *testing.T) {
	t.Parallel()
	// WHY: "blocked" is the implicit default (absence of a row) and must never
	// be persisted. Unknown/empty values also fail closed so a policy can never
	// be silently treated as raw_copy_allowed.
	for _, m := range []ResultDisclosureMode{
		ResultDisclosureBlocked,
		"",
		"RAW_COPY_ALLOWED",
		"Masked_No_Copy",
		"allow",
		"deny",
		"blocked ",
	} {
		if err := m.Validate(); err == nil {
			t.Errorf("Validate(%q) = nil, want error (fail closed)", m)
		}
	}
}

func TestResultDisclosurePolicyUpsertRequest_ValidPasses(t *testing.T) {
	t.Parallel()
	// WHY: a well-formed request with all required fields and valid identifiers
	// must pass validation so the policy can be persisted.
	req := ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 42,
		DatabaseName:     "my_db",
		ObjectName:       "users",
		ColumnName:       "email",
		Mode:             ResultDisclosureRawCopyAllowed,
	}
	if err := req.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

func TestResultDisclosurePolicyUpsertRequest_MissingTargetResourceID(t *testing.T) {
	t.Parallel()
	// WHY: target_resource_id=0 means no target was specified; this must be
	// rejected to prevent orphan policies.
	req := ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 0,
		DatabaseName:     "my_db",
		ObjectName:       "users",
		ColumnName:       "email",
		Mode:             ResultDisclosureRawCopyAllowed,
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate(target_resource_id=0) = nil, want error")
	}
}

func TestResultDisclosurePolicyUpsertRequest_EmptyIdentifiersRejected(t *testing.T) {
	t.Parallel()
	// WHY: empty identifiers would match too broadly or be meaningless; every
	// field must be explicitly specified.
	base := ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 1,
		DatabaseName:     "db",
		ObjectName:       "tbl",
		ColumnName:       "col",
		Mode:             ResultDisclosureRawCopyAllowed,
	}
	tests := []struct {
		name   string
		mutate func(*ResultDisclosurePolicyUpsertRequest)
	}{
		{"empty_database_name", func(r *ResultDisclosurePolicyUpsertRequest) { r.DatabaseName = "" }},
		{"empty_object_name", func(r *ResultDisclosurePolicyUpsertRequest) { r.ObjectName = "" }},
		{"empty_column_name", func(r *ResultDisclosurePolicyUpsertRequest) { r.ColumnName = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := base
			tt.mutate(&req)
			if err := req.Validate(); err == nil {
				t.Errorf("Validate(%s) = nil, want error", tt.name)
			}
		})
	}
}

func TestResultDisclosurePolicyUpsertRequest_IdentifierTooLongRejected(t *testing.T) {
	t.Parallel()
	// WHY: identifiers exceeding VARCHAR(128) would be truncated by the database,
	// causing silent mismatches. Validation must reject them before the write.
	long := strings.Repeat("a", MaxIdentifierLength+1)
	base := ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 1,
		DatabaseName:     "db",
		ObjectName:       "tbl",
		ColumnName:       "col",
		Mode:             ResultDisclosureRawCopyAllowed,
	}
	tests := []struct {
		name   string
		mutate func(*ResultDisclosurePolicyUpsertRequest)
	}{
		{"long_database_name", func(r *ResultDisclosurePolicyUpsertRequest) { r.DatabaseName = long }},
		{"long_object_name", func(r *ResultDisclosurePolicyUpsertRequest) { r.ObjectName = long }},
		{"long_column_name", func(r *ResultDisclosurePolicyUpsertRequest) { r.ColumnName = long }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := base
			tt.mutate(&req)
			if err := req.Validate(); err == nil {
				t.Errorf("Validate(%s) = nil, want error", tt.name)
			}
		})
	}
}

func TestResultDisclosurePolicyUpsertRequest_InvalidIdentifierCharsRejected(t *testing.T) {
	t.Parallel()
	// WHY: identifiers with spaces, dashes, dots, or special characters would
	// break SQL queries or cause injection risks. Only [a-zA-Z0-9_] is allowed.
	base := ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 1,
		DatabaseName:     "db",
		ObjectName:       "tbl",
		ColumnName:       "col",
		Mode:             ResultDisclosureRawCopyAllowed,
	}
	tests := []struct {
		name   string
		mutate func(*ResultDisclosurePolicyUpsertRequest)
	}{
		{"space_in_database", func(r *ResultDisclosurePolicyUpsertRequest) { r.DatabaseName = "my db" }},
		{"dash_in_object", func(r *ResultDisclosurePolicyUpsertRequest) { r.ObjectName = "my-table" }},
		{"dot_in_column", func(r *ResultDisclosurePolicyUpsertRequest) { r.ColumnName = "col.name" }},
		{"special_char", func(r *ResultDisclosurePolicyUpsertRequest) { r.DatabaseName = "db!" }},
		{"unicode", func(r *ResultDisclosurePolicyUpsertRequest) { r.ObjectName = "täble" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			req := base
			tt.mutate(&req)
			if err := req.Validate(); err == nil {
				t.Errorf("Validate(%s) = nil, want error", tt.name)
			}
		})
	}
}

func TestResultDisclosurePolicyUpsertRequest_InvalidModeRejected(t *testing.T) {
	t.Parallel()
	// WHY: the mode must be one of the two persistable values; blocked and
	// unknown modes must be rejected at the validation boundary.
	req := ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 1,
		DatabaseName:     "db",
		ObjectName:       "tbl",
		ColumnName:       "col",
		Mode:             ResultDisclosureBlocked,
	}
	if err := req.Validate(); err == nil {
		t.Error("Validate(mode=blocked) = nil, want error")
	}
}

func TestResultDisclosurePolicyUpsertRequest_MaxLengthIdentifiersPass(t *testing.T) {
	t.Parallel()
	// WHY: identifiers at exactly the max length must be accepted; this is the
	// boundary case that ensures we don't off-by-one reject valid names.
	maxLen := strings.Repeat("A", MaxIdentifierLength)
	req := ResultDisclosurePolicyUpsertRequest{
		TargetResourceID: 1,
		DatabaseName:     maxLen,
		ObjectName:       maxLen,
		ColumnName:       maxLen,
		Mode:             ResultDisclosureMaskedNoCopy,
	}
	if err := req.Validate(); err != nil {
		t.Errorf("Validate(max_length_identifiers) = %v, want nil", err)
	}
}
