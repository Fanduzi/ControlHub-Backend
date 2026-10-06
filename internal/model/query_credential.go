// Package model provides domain entities for the resource management system.
// input: fmt, unicode/utf8 packages
// output: QueryCredential* request/response/runtime-status types, Validate methods, ValidateConnectionName + composite-identity contract
// pos: Phase 38A query credential metadata management contract (metadata only; never DSN/password/host/port/actor)
// note: if this file changes, update header and README.md
package model

import (
	"fmt"
	"unicode/utf8"
)

// QueryCredentialRuntimeStatus is the backend runtime status of a query target's
// credential binding. Only secret_resolved can make a target execution-eligible;
// every other value keeps the target locked.
type QueryCredentialRuntimeStatus string

const (
	// QueryCredentialRuntimeMissingMetadata: no row in query_target_credentials.
	QueryCredentialRuntimeMissingMetadata QueryCredentialRuntimeStatus = "missing_metadata"
	// QueryCredentialRuntimeInvalidRef: a row exists but credential_ref is invalid; fail closed.
	QueryCredentialRuntimeInvalidRef QueryCredentialRuntimeStatus = "invalid_ref"
	// QueryCredentialRuntimeDisabled: metadata exists but enabled is false.
	QueryCredentialRuntimeDisabled QueryCredentialRuntimeStatus = "disabled"
	// QueryCredentialRuntimePolicyBlocked: metadata exists but environment policy disallows the target environment.
	QueryCredentialRuntimePolicyBlocked QueryCredentialRuntimeStatus = "policy_blocked"
	// QueryCredentialRuntimeSecretMissing: metadata exists but the resolver cannot resolve the DSN.
	QueryCredentialRuntimeSecretMissing QueryCredentialRuntimeStatus = "secret_missing"
	// QueryCredentialRuntimeBindingMismatch: the resolver returns a DSN but host/port does not bind to the target.
	QueryCredentialRuntimeBindingMismatch QueryCredentialRuntimeStatus = "binding_mismatch"
	// QueryCredentialRuntimeSecretResolved: the resolver returns a DSN, policy allows it, and host/port binds to the target.
	QueryCredentialRuntimeSecretResolved QueryCredentialRuntimeStatus = "secret_resolved"
	// QueryCredentialRuntimeUnsupportedTarget: the target engine is not MySQL/TiDB.
	QueryCredentialRuntimeUnsupportedTarget QueryCredentialRuntimeStatus = "unsupported_target"
	// QueryCredentialRuntimeIncompleteConnection: target connection metadata is incomplete.
	QueryCredentialRuntimeIncompleteConnection QueryCredentialRuntimeStatus = "incomplete_connection"
)

// IsResolved reports whether the runtime status means the secret resolved and
// bound. It is the only status that can make a target execution-eligible.
func (s QueryCredentialRuntimeStatus) IsResolved() bool {
	return s == QueryCredentialRuntimeSecretResolved
}

// Validate returns nil only for a declared runtime status. Unknown/empty values
// fail closed so the API can never emit an undeclared status string to clients.
func (s QueryCredentialRuntimeStatus) Validate() error {
	switch s {
	case QueryCredentialRuntimeMissingMetadata,
		QueryCredentialRuntimeInvalidRef,
		QueryCredentialRuntimeDisabled,
		QueryCredentialRuntimePolicyBlocked,
		QueryCredentialRuntimeSecretMissing,
		QueryCredentialRuntimeBindingMismatch,
		QueryCredentialRuntimeSecretResolved,
		QueryCredentialRuntimeUnsupportedTarget,
		QueryCredentialRuntimeIncompleteConnection:
		return nil
	}
	return fmt.Errorf("invalid query credential runtime status: %s", s)
}

// QueryCredentialStatusResponse is the body of GET /query-targets/{id}/credential.
// It is metadata only: it never carries a DSN, password, host, port, or actor.
// Database and DefaultSchema echo the addressed connection row's composite
// identity; both are empty for legacy single-connection MySQL/TiDB rows.
type QueryCredentialStatusResponse struct {
	ResourceID        uint64                       `json:"resourceId"`
	Database          string                       `json:"database"`
	DefaultSchema     string                       `json:"defaultSchema"`
	Configured        bool                         `json:"configured"`
	Engine            string                       `json:"engine"`
	CredentialRef     string                       `json:"credentialRef"`
	Enabled           bool                         `json:"enabled"`
	EnvironmentPolicy QueryEnvironmentPolicy       `json:"environmentPolicy"`
	RuntimeStatus     QueryCredentialRuntimeStatus `json:"runtimeStatus"`
	ExecutionEligible bool                         `json:"executionEligible"`
	Message           string                       `json:"message"`
}

// QueryCredentialUpsertRequest is the body of PUT /query-targets/{id}/credential.
// It accepts metadata only: credentialRef, enabled, environmentPolicy, the
// all-environments confirmation, and defaultSchema (PostgreSQL connections only).
// It must never carry a DSN, password, host, port, or actor user id — those
// fields are rejected by strict JSON decoding in the handler and intentionally
// have no home in this struct. The connection's database segment is carried by
// the ?database= query parameter, not this body.
type QueryCredentialUpsertRequest struct {
	CredentialRef          string                 `json:"credentialRef"`
	Enabled                bool                   `json:"enabled"`
	EnvironmentPolicy      QueryEnvironmentPolicy `json:"environmentPolicy"`
	ConfirmAllEnvironments bool                   `json:"confirmAllEnvironments,omitempty"`
	DefaultSchema          string                 `json:"defaultSchema,omitempty"`
}

// Validate enforces the engine-independent request contract: a valid credential
// ref, a valid environment policy, explicit confirmation before all_environments
// can be persisted, and a bounded defaultSchema. Whether defaultSchema must be
// present (PostgreSQL) or absent (MySQL/TiDB) is engine-dependent and enforced
// by the service after the target lookup — the model cannot see the engine here.
func (r QueryCredentialUpsertRequest) Validate() error {
	if err := ValidateCredentialRef(r.CredentialRef); err != nil {
		return err
	}
	if err := r.EnvironmentPolicy.Validate(); err != nil {
		return err
	}
	if r.EnvironmentPolicy == QueryEnvPolicyAllEnvironments && !r.ConfirmAllEnvironments {
		return fmt.Errorf("all_environments requires confirmAllEnvironments")
	}
	if r.DefaultSchema != "" {
		if err := ValidateConnectionName("defaultSchema", r.DefaultSchema); err != nil {
			return err
		}
	}
	return nil
}

// MaxConnectionNameLength bounds the database segment of the composite
// connection key and the connection's default schema, consistent with the
// VARCHAR(128) columns on query_target_credentials.
const MaxConnectionNameLength = 128

// ValidateConnectionName validates a connection-identity name segment
// (database or default schema): non-empty and within the stored column width.
// Charset is intentionally permissive — PostgreSQL identifiers may carry
// characters outside [a-zA-Z0-9_]; the DSN binding check, not the name shape,
// is what anchors a connection row to a real database.
func ValidateConnectionName(field, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	if utf8.RuneCountInString(value) > MaxConnectionNameLength {
		return fmt.Errorf("%s exceeds %d characters", field, MaxConnectionNameLength)
	}
	return nil
}

// validateOptionalConnectionName applies the same width bound to an optional
// connection-identity segment (an empty value is valid at this layer; the
// service decides whether the target engine requires it).
func validateOptionalConnectionName(field, value string) error {
	if value == "" {
		return nil
	}
	return ValidateConnectionName(field, value)
}
