// Package model provides domain entities for the resource management system.
// input: errors, fmt, time packages
// output: QueryExecution*, QueryResult* types, status/error enums, query credential policy/ref validators, ErrInvalidCredentialMetadata
// pos: Query sandbox execution requests, responses, and history records
// note: if this file changes, update header and README.md
package model

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
// DisplayMode and CopyAllowed carry the server-owned disclosure decision for
// this column (derived from the disclosure policy; absent policy = blocked).
type QueryResultColumn struct {
	Name         string               `json:"name"`
	DatabaseType string               `json:"databaseType"`
	Nullable     bool                 `json:"nullable"`
	DisplayMode  ResultDisclosureMode `json:"displayMode"`
	CopyAllowed  bool                 `json:"copyAllowed"`
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

// QueryExecutionActor is the privacy-safe actor projection for history rows.
// Only displayName is public; email and raw numeric user IDs stay internal.
type QueryExecutionActor struct {
	DisplayName string `json:"displayName"`
}

// UnknownHistoryActorDisplayName is returned when the actor user row is missing
// or has an empty display name (deleted/orphaned actors).
const UnknownHistoryActorDisplayName = "Unknown user"

// QueryExecutionRecord is the persisted metadata for one execution attempt.
// It stores a statement digest and short preview, never full result rows.
// ActorUserID is internal (insert/scan); the public JSON shape uses Actor only.
type QueryExecutionRecord struct {
	ID               uint64               `json:"id"`
	TargetResourceID uint64               `json:"targetResourceId"`
	ActorUserID      uint64               `json:"-"`
	Actor            QueryExecutionActor  `json:"actor"`
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

// PaginationMode controls how ListExecutions selects a page.
//
// PaginationModeCursor uses keyset pagination over (created_at, id):
//   - The first cursor page (Cursor == nil) has no boundary predicate and
//     starts from the newest row.
//   - Continuation pages (Cursor != nil) add a strictly-older-than predicate
//     on (created_at, id).
//   - Cursor mode never runs a COUNT query and never uses OFFSET. It requests
//     PageSize+1 rows to detect whether a next page exists.
//
// PaginationModeOffset preserves the legacy page/pageSize/offset behaviour,
// runs COUNT, and returns PageInfo. It is selected only when the caller
// explicitly supplies ?page=.
type PaginationMode int

const (
	// PaginationModeCursor is the default keyset mode for history listing.
	PaginationModeCursor PaginationMode = iota
	// PaginationModeOffset is the legacy offset mode, retained for backward
	// compatibility with callers that explicitly request ?page=.
	PaginationModeOffset
)

// QueryExecutionListQuery carries pagination and optional actor visibility scope
// for execution history. When ActorUserID is non-nil, only that actor's rows for
// the target are returned (non-admin). Nil means all actors for the target (admin).
//
// Mode must be set explicitly by the service layer; the repository does not
// infer it from whether Cursor is nil. In cursor mode the first page has
// Cursor == nil and no boundary predicate; continuation pages have Cursor
// pointing at the last row of the previous page.
type QueryExecutionListQuery struct {
	TargetResourceID uint64
	Page             int
	PageSize         int
	ActorUserID      *uint64
	Status           *QueryExecutionStatus
	From             *time.Time
	To               *time.Time
	Cursor           *string
	Mode             PaginationMode
}

// QueryExecutionCursorPage is the envelope for cursor-based execution history.
// When PageInfo is set (offset mode via explicit ?page=), NextCursor is nil.
// When NextCursor is set (cursor mode), PageInfo is nil.
type QueryExecutionCursorPage struct {
	Items      []QueryExecutionRecord `json:"items"`
	NextCursor *string                `json:"nextCursor"`
	PageInfo   *PageInfo              `json:"pageInfo,omitempty"`
}

// CursorVersion is the current cursor wire format version.
const CursorVersion = 1

// CursorPayload is the decoded cursor wire format.
type CursorPayload struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"t"`
	ID        string    `json:"id"`
	QueryHash string    `json:"q"`
}

const maxCursorBytes = 1024

// EncodeCursor produces a base64url-encoded cursor from execution metadata.
func EncodeCursor(createdAt time.Time, id uint64, queryHash string) (string, error) {
	payload := CursorPayload{
		Version:   CursorVersion,
		CreatedAt: createdAt.UTC(),
		ID:        strconv.FormatUint(id, 10),
		QueryHash: queryHash,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeCursor decodes a base64url cursor, validates version, size, and that
// the ID field is a canonical positive uint64 string. This prevents a
// structurally valid but tampered cursor from reaching the repository and
// causing a strconv.ParseUint failure (which would surface as HTTP 500).
func DecodeCursor(raw string) (CursorPayload, error) {
	if len(raw) > base64.RawURLEncoding.EncodedLen(maxCursorBytes) {
		return CursorPayload{}, fmt.Errorf("cursor exceeds maximum size")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return CursorPayload{}, fmt.Errorf("decode cursor: %w", err)
	}
	if len(b) > maxCursorBytes {
		return CursorPayload{}, fmt.Errorf("cursor exceeds maximum size")
	}
	var p CursorPayload
	if err := json.Unmarshal(b, &p); err != nil {
		return CursorPayload{}, fmt.Errorf("decode cursor payload: %w", err)
	}
	if p.Version != CursorVersion {
		return CursorPayload{}, fmt.Errorf("unsupported cursor version: %d", p.Version)
	}
	if p.CreatedAt.IsZero() {
		return CursorPayload{}, fmt.Errorf("cursor missing created_at")
	}
	if _, err := strconv.ParseUint(p.ID, 10, 64); err != nil {
		return CursorPayload{}, fmt.Errorf("cursor id is not a valid uint64: %w", err)
	}
	return p, nil
}

// NormalizeFilters produces a canonical string for hash computation.
// Uses RFC3339Nano to preserve fractional seconds so that two timestamps in
// the same second but with different fractional parts produce different hashes.
func NormalizeFilters(status *QueryExecutionStatus, from, to *time.Time) string {
	var sb strings.Builder
	if status != nil {
		sb.WriteString(string(*status))
	}
	sb.WriteByte('|')
	if from != nil {
		sb.WriteString(from.UTC().Format(time.RFC3339Nano))
	}
	sb.WriteByte('|')
	if to != nil {
		sb.WriteString(to.UTC().Format(time.RFC3339Nano))
	}
	return sb.String()
}

// ComputeQueryHash returns a SHA256 hex digest of the canonical filter string.
func ComputeQueryHash(targetID uint64, status *QueryExecutionStatus, from, to *time.Time, scope string) string {
	canonical := strconv.FormatUint(targetID, 10) + "|" +
		NormalizeFilters(status, from, to) + "|" +
		scope
	h := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", h)
}

// ValidateStatus accepts exactly the four known execution status strings.
func ValidateStatus(s string) error {
	switch QueryExecutionStatus(s) {
	case QueryExecutionSuccess, QueryExecutionRejected, QueryExecutionFailed, QueryExecutionTimeout:
		return nil
	}
	return fmt.Errorf("invalid status: %s", s)
}

// ParseTimestamp parses an RFC3339 or RFC3339Nano timestamp.
// Both formats require a timezone suffix (Z or ±hh:mm), so date-only and
// timezone-less strings are rejected by the parser itself.
func ParseTimestamp(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Time{}, fmt.Errorf("invalid timestamp %q: must be RFC3339 with timezone", s)
		}
	}
	return t, nil
}

// ValidateTimeWindow rejects from >= to.
func ValidateTimeWindow(from, to *time.Time) error {
	if from != nil && to != nil && !from.Before(*to) {
		return fmt.Errorf("invalid time window: from (%v) must be before to (%v)", from.Format(time.RFC3339), to.Format(time.RFC3339))
	}
	return nil
}

// ValidateCursor decodes a cursor and verifies its query hash matches.
func ValidateCursor(raw string, expectedQueryHash string) error {
	p, err := DecodeCursor(raw)
	if err != nil {
		return err
	}
	if p.QueryHash != expectedQueryHash {
		return fmt.Errorf("cursor query hash mismatch")
	}
	return nil
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

// ErrInvalidCredentialMetadata is the fail-closed signal returned by the
// credential metadata reader when a stored row EXISTS but its credential_ref or
// environment_policy fails validation (e.g. legacy/manual data that bypassed the
// write path). It is distinct from sql.ErrNoRows ("no row"): a row is present,
// so the target must surface runtime status invalid_ref — never missing_metadata.
// Services classify it via errors.Is; it never carries a DSN, host, port, or
// secret. The reader still returns the scanned row alongside this error so the
// status path can report configured=true; callers MUST check the error before
// trusting the row (the execute path rejects on any non-nil error).
var ErrInvalidCredentialMetadata = errors.New("stored credential metadata is invalid")
