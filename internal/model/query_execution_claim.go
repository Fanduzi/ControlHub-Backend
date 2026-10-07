// Package model provides domain entities for the resource management system.
// input: fmt, strings, time, utf8 packages and QueryExecutionIdentity/QueryExecutionRecord
// output: QueryExecutionClaimInput, QueryExecutionClaim, QueryExecutionClaimView types + Validate
// pos: Occupancy-record protocol types for the G9 execution claim (issue #118, T10-A); storage only, no client-facing JSON contract
// note: if this file changes, update this header and module README.md.
package model

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
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
	ClaimedAt     time.Time
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
	if utf8.RuneCountInString(c.RequestDigest) != QueryExecutionClaimDigestLength {
		return fmt.Errorf("request_digest must be exactly %d characters", QueryExecutionClaimDigestLength)
	}
	if strings.IndexByte(c.RequestDigest, 0) >= 0 {
		return fmt.Errorf("request_digest must not contain NUL bytes")
	}
	if c.ClaimedAt.IsZero() {
		return fmt.Errorf("claimed_at is required")
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
	ExecutionID             uint64 // 0 = NULL (unlinked)
	ClaimedAt               time.Time
}

// QueryExecutionClaimView is the single-snapshot point-read result: the claim
// plus, once linked, the real terminal execution row. Execution is nil while
// the claim is unlinked — derived display states (running/unknown) are a
// later read-path concern, not persisted here.
type QueryExecutionClaimView struct {
	Claim     QueryExecutionClaim
	Execution *QueryExecutionRecord
}
