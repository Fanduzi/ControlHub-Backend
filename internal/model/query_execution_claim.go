// Package model provides domain entities for the resource management system.
// input: fmt, strings, time, utf8 packages and QueryExecutionIdentity/QueryExecutionRecord
// output: QueryExecutionClaimInput, QueryExecutionClaim, QueryExecutionClaimView types + Validate
// pos: Occupancy-record protocol types for the G9 execution claim (issue #118, T10-A); storage only, no client-facing JSON contract
// note: if this file changes, update this header and module README.md.
package model

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Claim-protocol sentinels shared by the repository writer and the service
// orchestrator: admission refusals are not persistence failures — they never
// write history or audit rows.
var (
	// ErrQueryExecutionClaimInvalid marks a malformed admission or finalize
	// input (bad key/digest/actor/scope shape, non-terminal status, unknown
	// remote-state enum).
	ErrQueryExecutionClaimInvalid = errors.New("invalid query execution claim")
	// ErrQueryExecutionClaimExists marks a same-key, same-digest admission:
	// the attempt was already recorded; the requester is not the occupant.
	ErrQueryExecutionClaimExists = errors.New("query execution claim already exists")
	// ErrQueryExecutionClaimConflict marks a same-key, different-digest
	// admission: a different request already owns the key.
	ErrQueryExecutionClaimConflict = errors.New("query execution claim conflict")
	// ErrQueryExecutionClaimNotLinked marks a finalize whose (target, key)
	// claim is missing, already linked, or whose actor/connection-scope
	// columns do not match the stored occupancy — the input is not the
	// occupant, so nothing is written.
	ErrQueryExecutionClaimNotLinked = errors.New("query execution claim not linked to this input")
	// ErrQueryExecutionClaimBrokenLink marks a structural violation: a claim
	// whose execution_id is set but whose execution row is absent or whose
	// stored identity (target, actor, scope, key) does not match the claim.
	// Readers get an evidence error, never a fabricated running state or a
	// forged id.
	ErrQueryExecutionClaimBrokenLink = errors.New("query execution claim link is broken")
)

// QueryExecutionClaimDigestLength is the fixed request-digest width: a
// canonical SHA-256 hex fingerprint stored in CHAR(64).
const QueryExecutionClaimDigestLength = 64

// QueryExecutionClaimInput is the admission record for the atomic occupancy
// gate (G9). The caller supplies the already-resolved connection scope and
// the already-computed canonical request digest; the repository validates
// shape and consistency but never recomputes them.
type QueryExecutionClaimInput struct {
	TargetResourceID  uint64
	ClientExecutionID string
	// Identity is the typed actor: exactly one of user/machine may be set.
	Identity      QueryExecutionIdentity
	DatabaseName  string
	SchemaName    string
	RequestDigest string
	// AttemptTimeoutMs is the statement-timeout budget the claimant resolved
	// before admission, in milliseconds. The running/unknown read threshold
	// derives from it plus the fixed grace — never from a mutable environment
	// value read later. Positive on every new claim.
	AttemptTimeoutMs int64
	ClaimedAt        time.Time
}

// Validate enforces the storage shape of an occupancy admission: positive
// target, non-empty bounded idempotency key, exactly-one typed actor, bounded
// connection scope, a 64-character digest, and a server-provided claim time.
func (c QueryExecutionClaimInput) Validate() error {
	if c.TargetResourceID == 0 {
		return fmt.Errorf("target_resource_id is required")
	}
	if c.ClientExecutionID == "" {
		return fmt.Errorf("clientExecutionId is required")
	}
	if err := ValidateClientExecutionID(c.ClientExecutionID); err != nil {
		return err
	}
	if strings.IndexByte(c.ClientExecutionID, 0) >= 0 {
		return fmt.Errorf("clientExecutionId must not contain NUL bytes")
	}
	if err := c.Identity.Validate(); err != nil {
		return err
	}
	if utf8.RuneCountInString(c.DatabaseName) > MaxIdentifierLength {
		return fmt.Errorf("database_name exceeds %d characters", MaxIdentifierLength)
	}
	if utf8.RuneCountInString(c.SchemaName) > MaxIdentifierLength {
		return fmt.Errorf("schema_name exceeds %d characters", MaxIdentifierLength)
	}
	if err := validateClaimDigest(c.RequestDigest); err != nil {
		return err
	}
	if c.AttemptTimeoutMs <= 0 {
		return fmt.Errorf("attempt_timeout_ms must be positive")
	}
	if c.ClaimedAt.IsZero() {
		return fmt.Errorf("claimed_at is required")
	}
	return nil
}

// validateClaimDigest enforces the digest shape: exactly 64 ASCII hexadecimal
// characters — the canonical SHA-256 hex fingerprint. The digest is stored
// verbatim: case is significant (the claim identity is byte-exact), so the
// input is never normalized, and whitespace, trailing space, non-hex ASCII,
// and non-ASCII are all rejected.
func validateClaimDigest(d string) error {
	if len(d) != QueryExecutionClaimDigestLength {
		return fmt.Errorf("request_digest must be exactly %d ASCII hex characters", QueryExecutionClaimDigestLength)
	}
	for i := 0; i < len(d); i++ {
		c := d[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return fmt.Errorf("request_digest contains a non-hex character at offset %d", i)
		}
	}
	return nil
}

// QueryExecutionClaim is the persisted occupancy row. It is not a history row
// and not an audit event: it only proves which attempt owns
// (target_resource_id, client_execution_id). ExecutionID stays NULL until the
// keyed finalize links the real terminal execution.
type QueryExecutionClaim struct {
	TargetResourceID        uint64
	ClientExecutionID       string
	ActorUserID             uint64 // 0 = NULL (machine-attributed claim)
	ActorMachinePrincipalID uint64 // 0 = NULL (user-attributed claim)
	DatabaseName            string
	SchemaName              string
	RequestDigest           string
	// AttemptTimeoutMs is the budget recorded at admission. 0 = NULL on
	// claims written before the column existed: with the resolved budget
	// unrecoverable such a claim derives unknown — the missing budget is an
	// evidence gap, never a proven expiry.
	AttemptTimeoutMs int64
	ExecutionID      uint64 // 0 = NULL (unlinked)
	ClaimedAt        time.Time
}

// QueryExecutionClaimView is the single-snapshot point-read result: the claim
// plus, once linked, the real terminal execution row. Execution is nil while
// the claim is unlinked — derived display states (running/unknown) are a
// later read-path concern, not persisted here.
type QueryExecutionClaimView struct {
	Claim     QueryExecutionClaim
	Execution *QueryExecutionRecord
}
