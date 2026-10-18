// Package model provides domain entities for the resource management system.
// input: errors, fmt, math, time, unicode/utf8 packages
// output: QueryExecution* and QueryResult* types, internal full-statement availability and public restore eligibility/statement response, validated user-or-machine execution identity, pagination/status/remote-state/error contracts, connection-scope request fields, query credential policy/ref validators
// pos: Query sandbox execution requests, responses, history records with server-computed restore eligibility, and owner-reusable statement response
// note: if this file changes, update header and README.md
package model

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// QueryExecutionStatus is the outcome category recorded for every execution
// attempt (success, rejected by guard/policy, backend failure, or timeout).
type QueryExecutionStatus string

const (
	QueryExecutionSuccess   QueryExecutionStatus = "success"
	QueryExecutionRejected  QueryExecutionStatus = "rejected"
	QueryExecutionFailed    QueryExecutionStatus = "failed"
	QueryExecutionTimeout   QueryExecutionStatus = "timeout"
	QueryExecutionCancelled QueryExecutionStatus = "cancelled"
)

type QueryExecutionActorKind string

const (
	QueryExecutionActorUser    QueryExecutionActorKind = "user"
	QueryExecutionActorMachine QueryExecutionActorKind = "machine"
)

// QueryExecutionIdentity is the internal evidence identity for one governed
// execution. ID names either a User or a Machine Principal, never a credential.
type QueryExecutionIdentity struct {
	Kind QueryExecutionActorKind
	ID   uint64
}

func (i QueryExecutionIdentity) Validate() error {
	if i.ID == 0 {
		return fmt.Errorf("query execution identity id must be positive")
	}
	switch i.Kind {
	case QueryExecutionActorUser, QueryExecutionActorMachine:
		return nil
	default:
		return fmt.Errorf("invalid query execution actor kind %q", i.Kind)
	}
}

// AllowedPageSizes lists the page sizes permitted for governed query-result
// paging. Only these values are accepted by ValidatePagination; any other
// pageSize is rejected so the executor never receives an unbounded page shape.
var AllowedPageSizes = []int{10, 25, 50, 100}

// QueryExecutePaginationRequest carries the caller's requested page boundary
// for governed query-result paging. Page is 1-based; PageSize must be one of
// AllowedPageSizes. When omitted from the request body, execution keeps its
// single-response behavior and no pagination metadata is returned.
type QueryExecutePaginationRequest struct {
	Page     int `json:"page"`
	PageSize int `json:"pageSize"`
}

// QueryExecutePaginationResponse carries the server-computed page metadata
// for governed query-result paging. HasPreviousPage and HasNextPage are
// derived from the offset and effective row cap; they never leak total-row
// counts or snapshot identifiers.
type QueryExecutePaginationResponse struct {
	Page            int  `json:"page"`
	PageSize        int  `json:"pageSize"`
	HasPreviousPage bool `json:"hasPreviousPage"`
	HasNextPage     bool `json:"hasNextPage"`
}

// QueryExecuteRequest is the body of POST /query-targets/{id}/execute. The
// actor is never taken from here — it is derived from the verified token.
type QueryExecuteRequest struct {
	Statement  string                         `json:"statement"`
	MaxRows    int                            `json:"maxRows,omitempty"`
	Pagination *QueryExecutePaginationRequest `json:"pagination,omitempty"`
	// Database and Schema are the composite connection-identity selectors.
	// They are accepted in the contract but rejected while non-empty until the
	// PostgreSQL execution path is wired (T7/T12).
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
	// ClientExecutionID is the idempotency key for the claim protocol (G9).
	// Rejected while non-empty until claims are wired (T10).
	ClientExecutionID string `json:"clientExecutionId,omitempty"`
	// Capabilities declares client result-contract capabilities (e.g.
	// "cellTruncated"); checked by the post-finalize delivery gate (T8-B).
	Capabilities []string `json:"capabilities,omitempty"`
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
	ExecutionID      uint64                          `json:"executionId"`
	Status           QueryExecutionStatus            `json:"status"`
	TargetResourceID uint64                          `json:"targetResourceId"`
	Engine           string                          `json:"engine"`
	Columns          []QueryResultColumn             `json:"columns"`
	Rows             [][]any                         `json:"rows"`
	RowCount         int                             `json:"rowCount"`
	Truncated        bool                            `json:"truncated"`
	DurationMs       int64                           `json:"durationMs"`
	LimitApplied     int                             `json:"limitApplied"`
	ExecutedAt       time.Time                       `json:"executedAt"`
	Pagination       *QueryExecutePaginationResponse `json:"pagination,omitempty"`
	// CellTruncated aligns with Rows cell-for-cell; absent means no
	// per-cell truncation information for this page.
	CellTruncated [][]bool `json:"cellTruncated,omitempty"`
	// Context echoes the server-resolved execution context. PostgreSQL
	// executions always carry database+schema; nil for engines without
	// pinned connection context.
	Context *QueryExecutionContext `json:"context,omitempty"`
}

// QueryExecutionContext is the resolved connection scope echoed back on
// execution responses so clients can correlate results with the connection
// they selected.
type QueryExecutionContext struct {
	Database string `json:"database"`
	Schema   string `json:"schema"`
}

// QueryExecutionActor is the privacy-safe actor projection for read models.
// DisplayName is a server-selected label; raw IDs and credentials stay internal.
type QueryExecutionActor struct {
	Kind        QueryExecutionActorKind `json:"kind"`
	DisplayName string                  `json:"displayName"`
}

// UnknownHistoryActorDisplayName is returned when the actor user row is missing
// or has an empty display name (deleted/orphaned actors).
const UnknownHistoryActorDisplayName = "Unknown user"

const UnknownHistoryMachineActorDisplayName = "Unknown machine principal"

// QueryExecutionViewStatus is the read-model status shown on history items:
// a stored terminal QueryExecutionStatus on execution rows, or a derived
// claim state on claim entries. Derived states are display values only —
// they are never persisted and never pass ValidateStatus.
type QueryExecutionViewStatus string

const (
	// QueryExecutionViewRunning marks an unlinked claim whose expiry
	// threshold (claimed_at + recorded attempt budget + grace) has not
	// elapsed.
	QueryExecutionViewRunning QueryExecutionViewStatus = "running"
	// QueryExecutionViewUnknown marks an unlinked claim past its expiry
	// threshold, or one whose recorded budget is missing or unusable — a
	// missing budget is an evidence gap ("no expiry can be shown"), never a
	// fabricated one. Either way it asserts only "no terminal result was
	// observed" — never that the backend died, the statement failed, or
	// remote work stopped.
	QueryExecutionViewUnknown QueryExecutionViewStatus = "unknown"
)

// QueryExecutionListItemKind discriminates one mixed-history row.
type QueryExecutionListItemKind string

const (
	// QueryExecutionListItemExecution is a persisted terminal execution row.
	QueryExecutionListItemExecution QueryExecutionListItemKind = "execution"
	// QueryExecutionListItemClaim is an entry derived from a claim
	// occupancy row — ID stays null; Status is the derived running/unknown.
	QueryExecutionListItemClaim QueryExecutionListItemKind = "claim"
)

// QueryExecutionListItem is one mixed execution-history read-model row: a
// persisted terminal execution (kind=execution, ID non-null, stored status)
// or a claim-derived entry (kind=claim, ID null, derived status). Exactly
// one of CreatedAt (execution rows) and ClaimedAt (claim entries) is set.
// BackendPID and the claim bookkeeping fields are internal evidence — they
// never serialize to the API.
type QueryExecutionListItem struct {
	Kind              QueryExecutionListItemKind `json:"kind"`
	ID                *uint64                    `json:"id"` // null for claim-derived entries
	TargetResourceID  uint64                     `json:"targetResourceId"`
	ClientExecutionID *string                    `json:"clientExecutionId"` // null = row carries no claim identity
	Actor             QueryExecutionActor        `json:"actor"`
	Engine            string                     `json:"engine"`
	DatabaseName      string                     `json:"database"`
	SchemaName        string                     `json:"schema"`
	StatementDigest   string                     `json:"statementDigest"`
	StatementPreview  string                     `json:"statementPreview"`
	CanRestore        bool                       `json:"canRestore"`
	Status            QueryExecutionViewStatus   `json:"status"`
	// RemoteState is the T10-B backend-state observation on terminal rows;
	// omitted when the attempt never probed or on claim entries.
	RemoteState  QueryExecutionRemoteState `json:"remoteState,omitempty"`
	RowCount     int                       `json:"rowCount"`
	DurationMs   int64                     `json:"durationMs"`
	ErrorCode    string                    `json:"errorCode"`
	ErrorMessage string                    `json:"errorMessage"`
	CreatedAt    *time.Time                `json:"createdAt,omitempty"`
	ClaimedAt    *time.Time                `json:"claimedAt,omitempty"`

	// Internal evidence — never serialized.
	ActorUserID             uint64 `json:"-"`
	ActorMachinePrincipalID uint64 `json:"-"`
	HasFullStatement        bool   `json:"-"`
	BackendPID              *int64 `json:"-"`
	// Claim bookkeeping returned by the consistent snapshot for service-side
	// derivation: the budget recorded at admission (0 = NULL — the budget is
	// unrecoverable on pre-T10-C claims and derives unknown as an evidence
	// gap, not a proven expiry) and the link verdict.
	ClaimAttemptTimeoutMs int64 `json:"-"`
	ClaimLinkBroken       bool  `json:"-"`
}

// QueryExecutionRemoteState is the backend-side state probed for cancel and
// verification evidence. Empty means the execution never needed a probe or the
// engine does not expose one.
type QueryExecutionRemoteState string

const (
	QueryExecutionRemoteNone      QueryExecutionRemoteState = ""
	QueryExecutionRemoteStopped   QueryExecutionRemoteState = "stopped"
	QueryExecutionRemoteUnknown   QueryExecutionRemoteState = "unknown"
	QueryExecutionRemoteCompleted QueryExecutionRemoteState = "completed"
)

// Validate returns nil only for a declared remote-state value.
func (s QueryExecutionRemoteState) Validate() error {
	switch s {
	case QueryExecutionRemoteNone,
		QueryExecutionRemoteStopped,
		QueryExecutionRemoteUnknown,
		QueryExecutionRemoteCompleted:
		return nil
	}
	return fmt.Errorf("invalid query execution remote state: %s", s)
}

// MaxClientExecutionIDLength bounds the client-supplied idempotency key.
const MaxClientExecutionIDLength = 64

// MaxStatementLength bounds one user statement in Unicode code points —
// counted like the identifier/key bounds, not UTF-8 bytes. The keyed PG
// entry enforces it at request shape, before any access or claim work.
const MaxStatementLength = 65536

// ValidateClientExecutionID enforces the ≤64-character bound on the
// client-supplied idempotency key, counted in Unicode code points like the
// saved-statement name checks — not UTF-8 bytes. Empty is allowed — the key
// is optional on the wire. Entry-point acceptance is a separate,
// handler-level decision.
func ValidateClientExecutionID(id string) error {
	if utf8.RuneCountInString(id) > MaxClientExecutionIDLength {
		return fmt.Errorf("clientExecutionId exceeds %d characters", MaxClientExecutionIDLength)
	}
	return nil
}

// QueryExecutionRecord is the persisted metadata for one execution attempt.
// It stores a statement digest and short preview, never full result rows.
// User and machine-principal IDs are internal (insert/scan); the public JSON
// shape uses the typed Actor projection only. Exactly one internal ID is set.
// BackendPID, RemoteState, and ClientExecutionID are internal evidence fields —
// they never serialize to the API.
type QueryExecutionRecord struct {
	ID                      uint64                    `json:"id"`
	TargetResourceID        uint64                    `json:"targetResourceId"`
	ActorUserID             uint64                    `json:"-"`
	ActorMachinePrincipalID uint64                    `json:"-"`
	Actor                   QueryExecutionActor       `json:"actor"`
	Engine                  string                    `json:"engine"`
	DatabaseName            string                    `json:"database"`
	SchemaName              string                    `json:"schema"`
	StatementDigest         string                    `json:"statementDigest"`
	StatementPreview        string                    `json:"statementPreview"`
	FullStatement           string                    `json:"-"`
	HasFullStatement        bool                      `json:"-"`
	CanRestore              bool                      `json:"canRestore"`
	Status                  QueryExecutionStatus      `json:"status"`
	RowCount                int                       `json:"rowCount"`
	DurationMs              int64                     `json:"durationMs"`
	ErrorCode               string                    `json:"errorCode"`
	ErrorMessage            string                    `json:"errorMessage"`
	BackendPID              *int64                    `json:"-"`
	RemoteState             QueryExecutionRemoteState `json:"-"`
	ClientExecutionID       *string                   `json:"-"`
	CreatedAt               time.Time                 `json:"createdAt"`
}

// QueryExecutionStatementResponse exposes full SQL only through the dedicated
// owner-only successful-execution lookup boundary.
type QueryExecutionStatementResponse struct {
	Statement string `json:"statement"`
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

// QueryExecutionListQuery carries pagination and optional actor visibility
// scope for execution history. When Actor is non-nil, only rows owned by that
// typed identity are returned (non-admin scope): a user identity matches
// actor_user_id only, a machine identity matches actor_machine_principal_id
// only — user:7 and machine:7 never see each other's rows. Nil means all
// actors for the target (admin). Database and ClientExecutionID are optional
// connection-scope / claim-key filters; ClientExecutionID addresses the
// keyed point read.
//
// The result set mixes persisted terminal execution rows with entries
// derived from unlinked claim occupancy rows — see QueryExecutionListItem.
//
// Mode must be set explicitly by the service layer; the repository does not
// infer it from whether Cursor is nil. In cursor mode the first page has
// Cursor == nil and no boundary predicate; continuation pages have Cursor
// pointing at the last row of the previous page.
type QueryExecutionListQuery struct {
	TargetResourceID  uint64
	Page              int
	PageSize          int
	Actor             *QueryExecutionIdentity
	Database          string
	ClientExecutionID string
	Status            *QueryExecutionStatus
	From              *time.Time
	To                *time.Time
	Cursor            *string
	Mode              PaginationMode
}

// QueryExecutionCursorPage is the envelope for cursor-based execution history.
// When PageInfo is set (offset mode via explicit ?page=), NextCursor is nil.
// When NextCursor is set (cursor mode), PageInfo is nil.
type QueryExecutionCursorPage struct {
	Items      []QueryExecutionListItem `json:"items"`
	NextCursor *string                  `json:"nextCursor"`
	PageInfo   *PageInfo                `json:"pageInfo,omitempty"`
}

// CursorVersion is the current cursor wire format version. Version 1 was the
// terminal-only format: its (created_at, id) ordering position cannot be
// resumed faithfully inside the mixed execution+claim ordering, so v1
// cursors are rejected as unsupported rather than silently reinterpreted.
const CursorVersion = 2

// Cursor row kinds encoded in the payload.
const (
	cursorKindExecution = "e"
	cursorKindClaim     = "c"
)

// CursorPayload is the decoded cursor wire format. The boundary row is
// identified by the mixed ordering — activity timestamp, then row kind
// (execution rows sort before claim entries at the same timestamp), then the
// stable within-kind identity (execution id decimal for "e", the claim key
// for "c").
type CursorPayload struct {
	Version   int       `json:"v"`
	CreatedAt time.Time `json:"t"`
	Kind      string    `json:"k"`
	Ref       string    `json:"r"`
	QueryHash string    `json:"q"`
}

const maxCursorBytes = 1024

// CursorRefKind returns the wire kind a boundary row encodes.
func CursorRefKind(kind QueryExecutionListItemKind) string {
	if kind == QueryExecutionListItemClaim {
		return cursorKindClaim
	}
	return cursorKindExecution
}

// EncodeCursor produces a base64url-encoded cursor from a boundary row's
// mixed-ordering position.
func EncodeCursor(createdAt time.Time, kind QueryExecutionListItemKind, ref, queryHash string) (string, error) {
	payload := CursorPayload{
		Version:   CursorVersion,
		CreatedAt: createdAt.UTC(),
		Kind:      CursorRefKind(kind),
		Ref:       ref,
		QueryHash: queryHash,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodeCursor decodes a base64url cursor and validates version, size, row
// kind, and the boundary reference shape (a canonical positive uint64 for
// execution rows; a non-empty claim key otherwise). A structurally valid but
// tampered or stale-version cursor is rejected here rather than reaching the
// repository — it is never decoded as page one.
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
	switch p.Kind {
	case cursorKindExecution:
		if _, err := strconv.ParseUint(p.Ref, 10, 64); err != nil {
			return CursorPayload{}, fmt.Errorf("cursor ref is not a valid uint64: %w", err)
		}
	case cursorKindClaim:
		if p.Ref == "" {
			return CursorPayload{}, fmt.Errorf("cursor claim ref is empty")
		}
	default:
		return CursorPayload{}, fmt.Errorf("cursor kind is invalid")
	}
	return p, nil
}

// NormalizeFilters produces a canonical string for hash computation.
// Uses RFC3339Nano to preserve fractional seconds so that two timestamps in
// the same second but with different fractional parts produce different hashes.
// Database and ClientExecutionID are connection/claim-key scope; pageSize is
// pinned so a cursor minted for one page shape cannot resume under another.
func NormalizeFilters(status *QueryExecutionStatus, from, to *time.Time, database, clientExecutionID string, pageSize int) string {
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
	sb.WriteByte('|')
	sb.WriteString(strconv.Itoa(len(database)))
	sb.WriteByte(':')
	sb.WriteString(database)
	sb.WriteByte('|')
	sb.WriteString(strconv.Itoa(len(clientExecutionID)))
	sb.WriteByte(':')
	sb.WriteString(clientExecutionID)
	sb.WriteByte('|')
	sb.WriteString(strconv.Itoa(pageSize))
	return sb.String()
}

// ComputeQueryHash returns a SHA256 hex digest of the canonical filter
// string. The scope segment carries the typed actor ("user:7",
// "machine:7", or "all") so a cursor can never cross actor scope.
func ComputeQueryHash(targetID uint64, status *QueryExecutionStatus, from, to *time.Time, database, clientExecutionID string, pageSize int, scope string) string {
	canonical := strconv.FormatUint(targetID, 10) + "|" +
		NormalizeFilters(status, from, to, database, clientExecutionID, pageSize) + "|" +
		scope
	h := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("%x", h)
}

// ValidateStatus accepts exactly the five known stored execution status
// strings. Derived view-only states (running/unknown surfaced from claims)
// are not persisted statuses and are rejected here.
func ValidateStatus(s string) error {
	switch QueryExecutionStatus(s) {
	case QueryExecutionSuccess, QueryExecutionRejected, QueryExecutionFailed, QueryExecutionTimeout, QueryExecutionCancelled:
		return nil
	}
	return fmt.Errorf("invalid status: %s", s)
}

// ValidatePagination rejects page <= 0, pageSize not in AllowedPageSizes,
// and (page-1)*pageSize overflow. The overflow guard uses checked arithmetic
// so a malicious page value cannot wrap the offset negative.
func ValidatePagination(page, pageSize int) error {
	if page <= 0 {
		return fmt.Errorf("page must be > 0, got %d", page)
	}
	allowed := false
	for _, s := range AllowedPageSizes {
		if pageSize == s {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("pageSize must be one of %v, got %d", AllowedPageSizes, pageSize)
	}
	// Checked arithmetic: (page-1) * pageSize must not overflow int.
	if page-1 > math.MaxInt/pageSize {
		return fmt.Errorf("pagination offset overflow: (page=%d-1)*pageSize=%d", page, pageSize)
	}
	return nil
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
// connection row. The DSN/password is never stored here or returned — only the
// opaque credential_ref plus its enabled flag and environment policy.
// (ResourceID, DatabaseName) together identify the connection row; DatabaseName
// is empty for legacy single-connection MySQL/TiDB targets.
type QueryCredentialMetadata struct {
	ID                uint64                 `json:"id"`
	ResourceID        uint64                 `json:"resourceId"`
	DatabaseName      string                 `json:"database"`
	DefaultSchema     string                 `json:"defaultSchema"`
	Engine            string                 `json:"engine"`
	CredentialRef     string                 `json:"credentialRef"`
	Enabled           bool                   `json:"enabled"`
	EnvironmentPolicy QueryEnvironmentPolicy `json:"environmentPolicy"`
}

// ValidateConnectionIdentity enforces the engine-keyed connection identity
// contract on a credential row (frozen spec G1): a postgresql row MUST carry a
// non-empty database_name and default_schema (the composite connection
// identity); every other engine's row MUST leave both empty — the legacy
// MySQL/TiDB identity is (resource_id, ”). Applied on the write path
// (upsert/seed) so a malformed row can never be persisted.
func (m QueryCredentialMetadata) ValidateConnectionIdentity() error {
	if m.Engine == "postgresql" {
		if err := ValidateConnectionName("database", m.DatabaseName); err != nil {
			return err
		}
		if err := ValidateConnectionName("defaultSchema", m.DefaultSchema); err != nil {
			return err
		}
		return nil
	}
	if m.DatabaseName != "" || m.DefaultSchema != "" {
		return fmt.Errorf("database/defaultSchema are only valid for postgresql connections")
	}
	return nil
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
