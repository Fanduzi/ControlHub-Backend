// Package model provides domain entities for the resource management system.
// input: fmt, strings packages
// output: RelatedRecordNavigationRequest, RelatedRecordNavigationSource, RelatedRecordNavigationResponse, validation constants
// pos: Request/response types for the governed FK record navigation endpoint (Phase 38J)
// note: if this file changes, update header and README.md
package model

import (
	"fmt"
	"strings"
	"time"
)

// Validation bounds for related-record navigation requests. These are tight
// enough to reject abuse but generous enough for real FK names and values.
const (
	MaxSourceDatabaseLength = 128
	MaxSourceObjectLength   = 128
	MaxForeignKeyNameLength = 128
	MaxLocalValueLength     = 1024
	MaxLocalValuesCount     = 16
	MaxRelatedRowsDefault   = 100
	MaxRelatedRowsHard      = 500
	MaxRelatedRowsMin       = 1
)

// RelatedRecordNavigationSource describes the trusted source table and FK
// constraint the browser identifies. The backend resolves referenced
// identifiers from schema metadata — the browser never supplies them.
type RelatedRecordNavigationSource struct {
	Database string `json:"database"`
	// Schema is the pinned-schema segment of the composite connection
	// identity. PostgreSQL sources require it; it must stay empty for
	// engines without schema namespaces until the PG path is wired.
	Schema     string `json:"schema,omitempty"`
	Object     string `json:"object"`
	Kind       string `json:"kind"`
	ForeignKey string `json:"foreignKey"`
}

// RelatedRecordNavigationRequest is the body of POST /query-targets/{id}/related-records.
// It carries only source identity, ordered local FK values, and a row cap.
// The browser never supplies referenced identifiers, SQL, credentials, DSN,
// or an actor identity.
type RelatedRecordNavigationRequest struct {
	Source      RelatedRecordNavigationSource `json:"source"`
	LocalValues []string                      `json:"localValues"`
	MaxRows     int                           `json:"maxRows,omitempty"`
	// ClientExecutionID is the idempotency key for the claim protocol (G9).
	// Rejected while non-empty until claims are wired (T10).
	ClientExecutionID string `json:"clientExecutionId,omitempty"`
	// Capabilities declares client result-contract capabilities (e.g.
	// "cellTruncated"); declaration-only until the truncation gate (T8).
	Capabilities []string `json:"capabilities,omitempty"`
}

// Validate checks structural invariants. It returns a controlled error message
// suitable for a 400 response; it never echoes submitted values.
func (r *RelatedRecordNavigationRequest) Validate() error {
	// Source validation.
	if strings.TrimSpace(r.Source.Database) == "" {
		return fmt.Errorf("source database is required")
	}
	if len(r.Source.Database) > MaxSourceDatabaseLength {
		return fmt.Errorf("source database exceeds %d characters", MaxSourceDatabaseLength)
	}
	if len(r.Source.Schema) > MaxSourceObjectLength {
		return fmt.Errorf("source schema exceeds %d characters", MaxSourceObjectLength)
	}
	if err := ValidateClientExecutionID(r.ClientExecutionID); err != nil {
		return err
	}
	if strings.TrimSpace(r.Source.Object) == "" {
		return fmt.Errorf("source object is required")
	}
	if len(r.Source.Object) > MaxSourceObjectLength {
		return fmt.Errorf("source object exceeds %d characters", MaxSourceObjectLength)
	}
	if r.Source.Kind != string(ObjectKindTable) {
		return fmt.Errorf("source kind must be \"table\"")
	}
	if strings.TrimSpace(r.Source.ForeignKey) == "" {
		return fmt.Errorf("source foreign key is required")
	}
	if len(r.Source.ForeignKey) > MaxForeignKeyNameLength {
		return fmt.Errorf("source foreign key exceeds %d characters", MaxForeignKeyNameLength)
	}

	// LocalValues validation: non-empty, bounded count, no NULLs, bounded length.
	if len(r.LocalValues) == 0 {
		return fmt.Errorf("localValues is required")
	}
	if len(r.LocalValues) > MaxLocalValuesCount {
		return fmt.Errorf("localValues exceeds maximum of %d entries", MaxLocalValuesCount)
	}
	for i, v := range r.LocalValues {
		if len(v) > MaxLocalValueLength {
			return fmt.Errorf("localValues[%d] exceeds %d characters", i, MaxLocalValueLength)
		}
	}

	// MaxRows: optional, clamped server-side but validated here for negativity.
	if r.MaxRows < 0 {
		return fmt.Errorf("maxRows must not be negative")
	}

	return nil
}

// ClampMaxRows returns the server-clamped row limit: defaults to
// MaxRelatedRowsDefault when unset, hard-capped at MaxRelatedRowsHard.
func (r *RelatedRecordNavigationRequest) ClampMaxRows() int {
	if r.MaxRows <= 0 {
		return MaxRelatedRowsDefault
	}
	if r.MaxRows > MaxRelatedRowsHard {
		return MaxRelatedRowsHard
	}
	return r.MaxRows
}

// RelatedRecordNavigationResponse is the body returned for a related-record
// navigation attempt. It carries bounded result data plus relation metadata
// safe to display. It never contains SQL, bound values, DSN, credentials,
// or raw driver errors.
type RelatedRecordNavigationResponse struct {
	// Result fields (same shape as QueryExecuteResponse).
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
	// CellTruncated aligns with Rows cell-for-cell; absent means no
	// per-cell truncation information for this page.
	CellTruncated [][]bool `json:"cellTruncated,omitempty"`

	// Relation metadata — safe to display; no values or SQL.
	SourceDatabase     string   `json:"sourceDatabase"`
	SourceObject       string   `json:"sourceObject"`
	ForeignKey         string   `json:"foreignKey"`
	ReferencedDatabase string   `json:"referencedDatabase"`
	ReferencedObject   string   `json:"referencedObject"`
	ReferencedColumns  []string `json:"referencedColumns"`
}
