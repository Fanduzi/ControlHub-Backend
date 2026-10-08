// Package model provides domain entities for the resource management system.
// input: encoding/json, fmt, regexp, time, unicode/utf8 packages
// output: QuerySavedStatement* contracts and shared metadata control-character validation
// pos: Governed saved statements for target-scoped query library
// note: if this file changes, update header and README.md
package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
	"unicode/utf8"
)

// QuerySavedStatementScope is the immutable visibility scope for a saved statement.
type QuerySavedStatementScope string

const (
	QuerySavedStatementPersonal       QuerySavedStatementScope = "personal"
	QuerySavedStatementSharedTemplate QuerySavedStatementScope = "shared_template"
)

// Validate returns nil only for a known scope value.
func (s QuerySavedStatementScope) Validate() error {
	switch s {
	case QuerySavedStatementPersonal, QuerySavedStatementSharedTemplate:
		return nil
	}
	return fmt.Errorf("invalid scope: %s", s)
}

// MaxSavedStatementNameLength bounds the name field.
const MaxSavedStatementNameLength = 120

// MaxSavedStatementSize bounds the statement text at 16 KiB.
const MaxSavedStatementSize = 16 * 1024

// MaxSavedStatementParameters bounds the number of declarations in one saved statement.
const MaxSavedStatementParameters = 20

// MaxSavedStatementParameterNameLength bounds one template parameter name.
const MaxSavedStatementParameterNameLength = 64

// QuerySavedStatementParameterType is the scalar type of a template parameter.
type QuerySavedStatementParameterType string

const (
	QuerySavedStatementParameterString  QuerySavedStatementParameterType = "string"
	QuerySavedStatementParameterInteger QuerySavedStatementParameterType = "integer"
	QuerySavedStatementParameterDecimal QuerySavedStatementParameterType = "decimal"
	QuerySavedStatementParameterBoolean QuerySavedStatementParameterType = "boolean"
)

// QuerySavedStatementParameterDefinition declares one named template parameter.
type QuerySavedStatementParameterDefinition struct {
	Name string                           `json:"name"`
	Type QuerySavedStatementParameterType `json:"type"`
}

// QuerySavedStatement is the persisted saved statement record.
// DatabaseName and SchemaName carry the composite connection identity the
// statement is bound to; both are empty for legacy MySQL/TiDB statements.
type QuerySavedStatement struct {
	ID               uint64                                   `json:"id"`
	TargetResourceID uint64                                   `json:"targetResourceId"`
	OwnerUserID      uint64                                   `json:"-"` // Never exposed in API
	DatabaseName     string                                   `json:"database"`
	SchemaName       string                                   `json:"schema"`
	Name             string                                   `json:"name"`
	Statement        string                                   `json:"statement"`
	Parameters       []QuerySavedStatementParameterDefinition `json:"parameters"`
	Scope            QuerySavedStatementScope                 `json:"scope"`
	CreatedAt        time.Time                                `json:"createdAt"`
	UpdatedAt        time.Time                                `json:"updatedAt"`
}

// QuerySavedStatementCreateRequest is the body for creating a saved statement.
// TargetResourceID comes from the URL path, not the request body. Database and
// Schema pin the statement to a composite (target, database) connection and
// schema namespace; both are empty for legacy MySQL/TiDB targets, and whether
// PostgreSQL requires them is decided by the service after the target lookup.
type QuerySavedStatementCreateRequest struct {
	Name       string                                   `json:"name"`
	Statement  string                                   `json:"statement"`
	Parameters []QuerySavedStatementParameterDefinition `json:"parameters,omitempty"`
	Scope      QuerySavedStatementScope                 `json:"scope"`
	Database   string                                   `json:"database,omitempty"`
	Schema     string                                   `json:"schema,omitempty"`
}

// Validate checks all required fields and bounds.
func (r QuerySavedStatementCreateRequest) Validate() error {
	if err := validateSavedStatementName(r.Name); err != nil {
		return err
	}
	if r.Statement == "" {
		return fmt.Errorf("statement is required")
	}
	if len(r.Statement) > MaxSavedStatementSize {
		return fmt.Errorf("statement exceeds %d bytes", MaxSavedStatementSize)
	}
	if err := r.Scope.Validate(); err != nil {
		return err
	}
	if err := validateSavedStatementParameters(r.Parameters); err != nil {
		return err
	}
	if err := validateOptionalConnectionName("database", r.Database); err != nil {
		return err
	}
	if err := validateOptionalConnectionName("schema", r.Schema); err != nil {
		return err
	}
	return nil
}

// QuerySavedStatementUpdateRequest is the body for updating a saved statement.
// Scope is immutable and never accepted on update. Database and Schema rewrite
// the statement's pinned context under the same rules as create.
type QuerySavedStatementUpdateRequest struct {
	Name       string                                   `json:"name"`
	Statement  string                                   `json:"statement"`
	Parameters []QuerySavedStatementParameterDefinition `json:"parameters,omitempty"`
	Database   string                                   `json:"database,omitempty"`
	Schema     string                                   `json:"schema,omitempty"`
}

// Validate checks all required fields and bounds.
func (r QuerySavedStatementUpdateRequest) Validate() error {
	if err := validateSavedStatementName(r.Name); err != nil {
		return err
	}
	if r.Statement == "" {
		return fmt.Errorf("statement is required")
	}
	if len(r.Statement) > MaxSavedStatementSize {
		return fmt.Errorf("statement exceeds %d bytes", MaxSavedStatementSize)
	}
	if err := validateSavedStatementParameters(r.Parameters); err != nil {
		return err
	}
	if err := validateOptionalConnectionName("database", r.Database); err != nil {
		return err
	}
	if err := validateOptionalConnectionName("schema", r.Schema); err != nil {
		return err
	}
	return nil
}

// MaxQuerySavedStatementExecuteValuesSize bounds the serialized `values`
// object in a template-execution request at 16 KiB (spec limit).
const MaxQuerySavedStatementExecuteValuesSize = 16 * 1024

// QuerySavedStatementExecuteRequest is the body of
// POST /query-targets/{id}/saved-statements/{statementId}/execute.
// It carries typed parameter values only; SQL text, parameter declarations,
// identities, roles, credentials, DSNs, and policy/audit/result fields are
// rejected by strict decoding. Values are protected content: they are never
// persisted, logged, audited, or returned in errors.
type QuerySavedStatementExecuteRequest struct {
	Values     map[string]json.RawMessage     `json:"values"`
	MaxRows    int                            `json:"maxRows,omitempty"`
	Pagination *QueryExecutePaginationRequest `json:"pagination,omitempty"`
	// ClientExecutionID is the idempotency key for the claim protocol (G9).
	// Rejected while non-empty until claims are wired (T10).
	ClientExecutionID string `json:"clientExecutionId,omitempty"`
	// Capabilities declares client result-contract capabilities (e.g.
	// "cellTruncated"); checked by the post-finalize delivery gate (T8-B).
	Capabilities []string `json:"capabilities,omitempty"`
}

// Validate enforces the request-level limits: non-negative maxRows, the
// governed pagination contract, and the 16 KiB values-object size. Typed
// value/declaration matching is enforced by the service against the stored
// statement.
func (r QuerySavedStatementExecuteRequest) Validate() error {
	if r.MaxRows < 0 {
		return fmt.Errorf("maxRows must not be negative")
	}
	if r.Pagination != nil {
		if err := ValidatePagination(r.Pagination.Page, r.Pagination.PageSize); err != nil {
			return err
		}
	}
	if err := ValidateClientExecutionID(r.ClientExecutionID); err != nil {
		return err
	}
	if len(r.Values) == 0 {
		return nil
	}
	raw, err := json.Marshal(r.Values)
	if err != nil {
		return fmt.Errorf("values are not valid JSON")
	}
	if len(raw) > MaxQuerySavedStatementExecuteValuesSize {
		return fmt.Errorf("values exceed %d bytes", MaxQuerySavedStatementExecuteValuesSize)
	}
	return nil
}

// QuerySavedStatementListQuery carries the filter for listing saved statements.
// Database is the composite connection scope: empty resolves to the legacy
// empty-database identity — which is every MySQL/TiDB row — and a non-empty
// value selects exactly one (target, database) connection's statements.
type QuerySavedStatementListQuery struct {
	TargetResourceID uint64
	OwnerUserID      uint64
	Database         string
	Page             int
	PageSize         int
	Search           string
}

// QuerySavedStatementListResponse is the paginated list response.
type QuerySavedStatementListResponse struct {
	Items                    []QuerySavedStatement `json:"items"`
	PageInfo                 PageInfo              `json:"pageInfo"`
	CanManageSharedTemplates bool                  `json:"canManageSharedTemplates"`
}

// validateSavedStatementName rejects empty, over-long, and control-character names.
func validateSavedStatementName(name string) error {
	name = trimWhitespace(name)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	if utf8.RuneCountInString(name) > MaxSavedStatementNameLength {
		return fmt.Errorf("name exceeds %d characters", MaxSavedStatementNameLength)
	}
	return validateNoControlCharacters("name", name)
}

func validateNoControlCharacters(field, value string) error {
	for _, r := range value {
		if r < 32 {
			return fmt.Errorf("%s contains control characters", field)
		}
	}
	return nil
}

func trimWhitespace(s string) string {
	start := 0
	end := len(s)
	for start < end && s[start] == ' ' {
		start++
	}
	for end > start && s[end-1] == ' ' {
		end--
	}
	return s[start:end]
}

var savedStatementParameterNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validateSavedStatementParameters(parameters []QuerySavedStatementParameterDefinition) error {
	if len(parameters) > MaxSavedStatementParameters {
		return fmt.Errorf("parameters exceed %d definitions", MaxSavedStatementParameters)
	}
	seen := make(map[string]struct{}, len(parameters))
	for _, parameter := range parameters {
		if utf8.RuneCountInString(parameter.Name) > MaxSavedStatementParameterNameLength {
			return fmt.Errorf("parameter name exceeds %d characters", MaxSavedStatementParameterNameLength)
		}
		if !savedStatementParameterNamePattern.MatchString(parameter.Name) {
			return fmt.Errorf("invalid parameter name")
		}
		if _, exists := seen[parameter.Name]; exists {
			return fmt.Errorf("duplicate parameter name")
		}
		seen[parameter.Name] = struct{}{}
		switch parameter.Type {
		case QuerySavedStatementParameterString,
			QuerySavedStatementParameterInteger,
			QuerySavedStatementParameterDecimal,
			QuerySavedStatementParameterBoolean:
		default:
			return fmt.Errorf("invalid parameter type")
		}
	}
	return nil
}
