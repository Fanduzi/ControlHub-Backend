// Package model provides domain entities for the resource management system.
// input: fmt package
// output: QueryCredential* request/response/runtime-status types and Validate methods
// pos: Phase 38A query credential metadata management contract (metadata only; never DSN/password/host/port/actor)
// note: if this file changes, update header and README.md
package model

import "fmt"

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
type QueryCredentialStatusResponse struct {
	ResourceID        uint64                       `json:"resourceId"`
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
// It accepts metadata only: credentialRef, enabled, environmentPolicy, and the
// all-environments confirmation. It must never carry a DSN, password, host, port,
// or actor user id — those fields are rejected by strict JSON decoding in the
// handler and intentionally have no home in this struct.
type QueryCredentialUpsertRequest struct {
	CredentialRef          string                 `json:"credentialRef"`
	Enabled                bool                   `json:"enabled"`
	EnvironmentPolicy      QueryEnvironmentPolicy `json:"environmentPolicy"`
	ConfirmAllEnvironments bool                   `json:"confirmAllEnvironments,omitempty"`
}

// Validate enforces the request contract: a valid credential ref, a valid
// environment policy, and explicit confirmation before all_environments can be
// persisted. Production-enabling policy must never be saved silently.
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
	return nil
}
