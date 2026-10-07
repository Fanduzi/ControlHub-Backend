// Package model provides domain entities for the resource management system.
// input: fmt, regexp, strings, time packages
// output: ResultDisclosureMode type, ResultDisclosurePolicy struct, ResultDisclosurePolicyUpsertRequest (five-part canonical key incl. SchemaName), ValidateIdentifierShape, ValidateLegacyScopeIdentifier, ResultDisclosurePolicyListQuery
// pos: Governed result-disclosure policy for per-column query result visibility
// note: if this file changes, update header and README.md
package model

import (
	"fmt"
	"regexp"
	"strings"
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

// MaxIdentifierLength bounds database_name, schema_name, object_name, and
// column_name length, consistent with the VARCHAR(128) columns in the
// migration.
const MaxIdentifierLength = 128

// identifierSyntax matches [a-zA-Z0-9_]+ — the allowed characters for legacy
// MySQL/TiDB database, object, and column names. PostgreSQL canonical names
// are intentionally wider (quoted and Unicode identifiers are legal): the
// engine-conditional gate lives in the service, which applies this stricter
// rule only to non-schema engines.
var identifierSyntax = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)

// ResultDisclosurePolicy is the persisted per-column disclosure mode for a
// query result. Absence of a matching row means the column is blocked
// (fail-closed). SchemaName is the schema segment of the canonical policy key;
// it is empty for legacy MySQL/TiDB policies.
type ResultDisclosurePolicy struct {
	ID               uint64               `json:"id"`
	TargetResourceID uint64               `json:"targetResourceId"`
	DatabaseName     string               `json:"databaseName"`
	SchemaName       string               `json:"schemaName"`
	ObjectName       string               `json:"objectName"`
	ColumnName       string               `json:"columnName"`
	Mode             ResultDisclosureMode `json:"mode"`
	CreatedAt        time.Time            `json:"createdAt"`
	UpdatedAt        time.Time            `json:"updatedAt"`
}

// ResultDisclosurePolicyUpsertRequest is the body for creating or updating a
// disclosure policy. SchemaName is optional at the contract level because the
// engine decides its meaning: PostgreSQL targets require an explicit schema
// (never defaulted to public or a connection default_schema at write time),
// while MySQL/TiDB policies keep the legacy empty schema. The service enforces
// that engine-conditional rule; the model enforces only what is true for every
// engine.
type ResultDisclosurePolicyUpsertRequest struct {
	TargetResourceID uint64               `json:"targetResourceId"`
	DatabaseName     string               `json:"databaseName"`
	SchemaName       string               `json:"schemaName,omitempty"`
	ObjectName       string               `json:"objectName"`
	ColumnName       string               `json:"columnName"`
	Mode             ResultDisclosureMode `json:"mode"`
}

// Validate checks all required fields, identifier lengths, embedded NUL bytes
// (unusable as a PostgreSQL identifier and ambiguous in the store), and mode.
// It returns the first validation error encountered. Engine-specific name
// syntax is enforced by the service, which knows the target's engine.
func (r ResultDisclosurePolicyUpsertRequest) Validate() error {
	if r.TargetResourceID == 0 {
		return fmt.Errorf("target_resource_id is required")
	}
	for _, f := range []struct{ name, value string }{
		{"database_name", r.DatabaseName},
		{"schema_name", r.SchemaName},
		{"object_name", r.ObjectName},
		{"column_name", r.ColumnName},
	} {
		if err := ValidateIdentifierShape(f.name, f.value, f.name != "schema_name"); err != nil {
			return err
		}
	}
	if err := r.Mode.Validate(); err != nil {
		return err
	}
	return nil
}

// ValidateIdentifierShape applies the engine-agnostic identifier contract:
// required (when mustFill), bounded length, and no embedded NUL byte. It is
// shared by upsert validation and scope-only paths such as delete, which carry
// no mode but must enforce the same name shape.
func ValidateIdentifierShape(field, value string, mustFill bool) error {
	if value == "" {
		if mustFill {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	if len([]rune(value)) > MaxIdentifierLength {
		return fmt.Errorf("%s exceeds %d characters", field, MaxIdentifierLength)
	}
	if strings.IndexByte(value, 0) >= 0 {
		return fmt.Errorf("%s must not contain NUL bytes", field)
	}
	return nil
}

// ValidateLegacyScopeIdentifier applies the strict [a-zA-Z0-9_]+ identifier
// rule that MySQL/TiDB policy scopes keep (T9-A): PostgreSQL scopes bypass it
// because quoted and Unicode canonical names are legal there.
func ValidateLegacyScopeIdentifier(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if len([]rune(value)) > MaxIdentifierLength {
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
