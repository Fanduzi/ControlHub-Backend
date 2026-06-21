// Package model provides domain entities for the resource management system.
// input: fmt, time packages
// output: QueryExecution*, QueryResult* types, status/error enums, and query credential policy/ref validators
// pos: Query sandbox execution requests, responses, and history records
// note: if this file changes, update header and README.md
package model

import (
	"fmt"
	"time"
)

// QueryExecutionStatus is the outcome category recorded for every execution
// attempt (success, rejected by guard/policy, backend failure, or timeout).
type QueryExecutionStatus string

const (
	QueryExecutionSuccess  QueryExecutionStatus = "success"
	QueryExecutionRejected QueryExecutionStatus = "rejected"
	QueryExecutionFailed   QueryExecutionStatus = "failed"
	QueryExecutionTimeout  QueryExecutionStatus = "timeout"
)

// QueryExecuteRequest is the body of POST /query-targets/{id}/execute. The
// actor is never taken from here — it is derived from the verified token.
type QueryExecuteRequest struct {
	Statement string `json:"statement"`
	MaxRows   int    `json:"maxRows,omitempty"`
}

// QueryResultColumn describes one result column by its name and database type.
type QueryResultColumn struct {
	Name         string `json:"name"`
	DatabaseType string `json:"databaseType"`
	Nullable     bool   `json:"nullable"`
}

// QueryExecuteResponse is the body returned for a query execution attempt. It
// carries result columns and rows only; it never carries credentials or DSNs.
type QueryExecuteResponse struct {
	ExecutionID      uint64               `json:"executionId"`
	Status           QueryExecutionStatus `json:"status"`
	TargetResourceID uint64               `json:"targetResourceId"`
	Engine           string               `json:"engine"`
	Columns          []QueryResultColumn  `json:"columns"`
	Rows             [][]any              `json:"rows"`
	RowCount         int                  `json:"rowCount"`
	Truncated        bool                 `json:"truncated"`
	DurationMs       int64                `json:"durationMs"`
	LimitApplied     int                  `json:"limitApplied"`
	ExecutedAt       time.Time            `json:"executedAt"`
}

// QueryExecutionRecord is the persisted metadata for one execution attempt.
// It stores a statement digest and short preview, never full result rows.
type QueryExecutionRecord struct {
	ID               uint64               `json:"id"`
	TargetResourceID uint64               `json:"targetResourceId"`
	ActorUserID      uint64               `json:"actorUserId"`
	Engine           string               `json:"engine"`
	StatementDigest  string               `json:"statementDigest"`
	StatementPreview string               `json:"statementPreview"`
	Status           QueryExecutionStatus `json:"status"`
	RowCount         int                  `json:"rowCount"`
	DurationMs       int64                `json:"durationMs"`
	ErrorCode        string               `json:"errorCode"`
	ErrorMessage     string               `json:"errorMessage"`
	CreatedAt        time.Time            `json:"createdAt"`
}

// QueryExecutionListQuery carries the pagination for execution history.
type QueryExecutionListQuery struct {
	TargetResourceID uint64
	Page             int
	PageSize         int
}

// QueryExecutionListResponse is the { items: [...] } envelope for execution
// history, carrying metadata only — never result rows.
type QueryExecutionListResponse struct {
	Items    []QueryExecutionRecord `json:"items"`
	PageInfo *PageInfo              `json:"pageInfo"`
}

// QueryEnvironmentPolicy controls which environments a credential may execute
// against. It is a typed enum, not a free string; unknown/empty fails closed.
type QueryEnvironmentPolicy string

const (
	QueryEnvPolicyDisabled        QueryEnvironmentPolicy = "disabled"
	QueryEnvPolicyNonProdOnly     QueryEnvironmentPolicy = "non_prod_only"
	QueryEnvPolicyAllEnvironments QueryEnvironmentPolicy = "all_environments"
)

// Validate returns nil only for a known policy value. Unknown/empty values
// fail closed so a target can never be silently treated as all_environments.
func (p QueryEnvironmentPolicy) Validate() error {
	switch p {
	case QueryEnvPolicyDisabled, QueryEnvPolicyNonProdOnly, QueryEnvPolicyAllEnvironments:
		return nil
	}
	return fmt.Errorf("invalid environment policy: %s", p)
}

// MaxCredentialRefLength bounds credential_ref length (consistent with the
// VARCHAR(128) column but kept tight at 64 to match env-var key sanity).
const MaxCredentialRefLength = 64

// ValidateCredentialRef rejects anything outside [A-Z0-9_]+ or over the length
// cap. Phase 37 has no credential write API, so this is enforced on read/resolve
// and by migration/seed (fail closed), never via a product insert path.
func ValidateCredentialRef(ref string) error {
	if ref == "" {
		return fmt.Errorf("credential_ref is empty")
	}
	if len(ref) > MaxCredentialRefLength {
		return fmt.Errorf("credential_ref exceeds %d characters", MaxCredentialRefLength)
	}
	for _, r := range ref {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
		default:
			return fmt.Errorf("credential_ref %q must match [A-Z0-9_]+", ref)
		}
	}
	return nil
}

// QueryCredentialMetadata is the resolved credential metadata for a query
// target. The DSN/password is never stored here or returned — only the opaque
// credential_ref plus its enabled flag and environment policy.
type QueryCredentialMetadata struct {
	ID                uint64                 `json:"id"`
	ResourceID        uint64                 `json:"resourceId"`
	Engine            string                 `json:"engine"`
	CredentialRef     string                 `json:"credentialRef"`
	Enabled           bool                   `json:"enabled"`
	EnvironmentPolicy QueryEnvironmentPolicy `json:"environmentPolicy"`
}