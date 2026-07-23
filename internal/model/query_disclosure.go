// Package model provides domain entities for the resource management system.
// input: fmt, regexp, time packages
// output: ResultDisclosureMode type, ResultDisclosurePolicy struct, ResultDisclosurePolicyUpsertRequest, ResultDisclosurePolicyListQuery
// pos: Governed result-disclosure policy for per-column query result visibility
// note: if this file changes, update header and README.md
package model

import (
	"fmt"
	"regexp"
	"time"
)

// ResultDisclosureMode is the server-owned disclosure decision for a result column.
// Only two modes are persisted; "blocked" is the fail-closed default when no
// matching row exists in the policy table.
type ResultDisclosureMode string

const (
	// ResultDisclosureRawCopyAllowed permits the full raw value to be displayed
	// and copied by the user.
	ResultDisclosureRawCopyAllowed ResultDisclosureMode = "raw_copy_allowed"

	// ResultDisclosureMaskedNoCopy displays a masked/redacted value and prevents
	// the user from copying the original.
	ResultDisclosureMaskedNoCopy ResultDisclosureMode = "masked_no_copy"

	// ResultDisclosureBlocked is the fail-closed default when no policy row
	// exists. It is never persisted — only derived at read time.
	ResultDisclosureBlocked ResultDisclosureMode = "blocked"
)

// Validate returns nil only for a persistable disclosure mode.
// "blocked" is rejected because it is the implicit default (absence of a row),
// not a value that should ever appear in the database.
func (m ResultDisclosureMode) Validate() error {
	switch m {
	case ResultDisclosureRawCopyAllowed, ResultDisclosureMaskedNoCopy:
		return nil
	}
	return fmt.Errorf("invalid disclosure mode: %s", m)
}

// MaxIdentifierLength bounds database_name, object_name, and column_name length,
// consistent with the VARCHAR(128) columns in the migration.
const MaxIdentifierLength = 128

// identifierSyntax matches [a-zA-Z0-9_]+ — the allowed characters for
// database, object, and column names in disclosure policies.
var identifierSyntax = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ResultDisclosurePolicy is the persisted per-column disclosure mode for a
// query result. Absence of a matching row means the column is blocked
// (fail-closed).
type ResultDisclosurePolicy struct {
	ID               uint64               `json:"id"`
	TargetResourceID uint64               `json:"targetResourceId"`
	DatabaseName     string               `json:"databaseName"`
	ObjectName       string               `json:"objectName"`
	ColumnName       string               `json:"columnName"`
	Mode             ResultDisclosureMode `json:"mode"`
	CreatedAt        time.Time            `json:"createdAt"`
	UpdatedAt        time.Time            `json:"updatedAt"`
}

// ResultDisclosurePolicyUpsertRequest is the body for creating or updating a
// disclosure policy. All fields are required.
type ResultDisclosurePolicyUpsertRequest struct {
	TargetResourceID uint64               `json:"targetResourceId"`
	DatabaseName     string               `json:"databaseName"`
	ObjectName       string               `json:"objectName"`
	ColumnName       string               `json:"columnName"`
	Mode             ResultDisclosureMode `json:"mode"`
}

// Validate checks all required fields, identifier lengths, identifier syntax,
// and mode. It returns the first validation error encountered.
func (r ResultDisclosurePolicyUpsertRequest) Validate() error {
	if r.TargetResourceID == 0 {
		return fmt.Errorf("target_resource_id is required")
	}
	if err := validateIdentifier("database_name", r.DatabaseName); err != nil {
		return err
	}
	if err := validateIdentifier("object_name", r.ObjectName); err != nil {
		return err
	}
	if err := validateIdentifier("column_name", r.ColumnName); err != nil {
		return err
	}
	if err := r.Mode.Validate(); err != nil {
		return err
	}
	return nil
}

// validateIdentifier rejects empty, over-length, and syntax-violating identifiers.
func validateIdentifier(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len(value) > MaxIdentifierLength {
		return fmt.Errorf("%s exceeds %d characters", field, MaxIdentifierLength)
	}
	if !identifierSyntax.MatchString(value) {
		return fmt.Errorf("%s %q must match [a-zA-Z0-9_]+", field, value)
	}
	return nil
}

// ResultDisclosurePolicyListQuery carries the filter for listing disclosure
// policies. TargetResourceID is required.
type ResultDisclosurePolicyListQuery struct {
	TargetResourceID uint64
}
