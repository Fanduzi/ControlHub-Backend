package model

import "testing"

// TestQueryCredentialRuntimeStatusValidate encodes the intent that the runtime
// status enum is closed: every declared value validates, and any unknown or
// empty value fails closed so the API can never emit an undeclared status.
func TestQueryCredentialRuntimeStatusValidate(t *testing.T) {
	valid := []QueryCredentialRuntimeStatus{
		QueryCredentialRuntimeMissingMetadata,
		QueryCredentialRuntimeInvalidRef,
		QueryCredentialRuntimeDisabled,
		QueryCredentialRuntimePolicyBlocked,
		QueryCredentialRuntimeSecretMissing,
		QueryCredentialRuntimeBindingMismatch,
		QueryCredentialRuntimeSecretResolved,
		QueryCredentialRuntimeUnsupportedTarget,
		QueryCredentialRuntimeIncompleteConnection,
	}
	for _, status := range valid {
		if err := status.Validate(); err != nil {
			t.Fatalf("%s should validate: %v", status, err)
		}
	}
	if err := QueryCredentialRuntimeStatus("raw_unknown").Validate(); err == nil {
		t.Fatal("unknown runtime status must fail validation")
	}
	if err := QueryCredentialRuntimeStatus("").Validate(); err == nil {
		t.Fatal("empty runtime status must fail validation")
	}
}

// TestQueryCredentialRuntimeStatusIsResolved encodes the security invariant that
// only secret_resolved can make a target execution-eligible.
func TestQueryCredentialRuntimeStatusIsResolved(t *testing.T) {
	if !QueryCredentialRuntimeSecretResolved.IsResolved() {
		t.Fatal("secret_resolved must be the resolved status")
	}
	nonResolved := []QueryCredentialRuntimeStatus{
		QueryCredentialRuntimeMissingMetadata,
		QueryCredentialRuntimeInvalidRef,
		QueryCredentialRuntimeDisabled,
		QueryCredentialRuntimePolicyBlocked,
		QueryCredentialRuntimeSecretMissing,
		QueryCredentialRuntimeBindingMismatch,
		QueryCredentialRuntimeUnsupportedTarget,
		QueryCredentialRuntimeIncompleteConnection,
	}
	for _, status := range nonResolved {
		if status.IsResolved() {
			t.Fatalf("%s must not be treated as resolved", status)
		}
	}
}

// TestQueryCredentialUpsertRequestValidate encodes the intent that a valid
// metadata upsert passes, production-enabling policy requires explicit
// confirmation, and an invalid credential ref fails closed.
func TestQueryCredentialUpsertRequestValidate(t *testing.T) {
	body := QueryCredentialUpsertRequest{
		CredentialRef:     "ORDER_MYSQL_RO",
		Enabled:           true,
		EnvironmentPolicy: QueryEnvPolicyNonProdOnly,
	}
	if err := body.Validate(); err != nil {
		t.Fatalf("valid request should pass: %v", err)
	}

	// all_environments must require explicit confirmation: production query
	// enablement can never be persisted from a silent/unconfirmed request.
	body.EnvironmentPolicy = QueryEnvPolicyAllEnvironments
	body.ConfirmAllEnvironments = false
	if err := body.Validate(); err == nil {
		t.Fatal("all_environments must require explicit confirmation")
	}
	body.ConfirmAllEnvironments = true
	if err := body.Validate(); err != nil {
		t.Fatalf("all_environments with confirmation should pass: %v", err)
	}

	// An invalid credential ref must fail closed before any resolver lookup.
	body.EnvironmentPolicy = QueryEnvPolicyNonProdOnly
	body.ConfirmAllEnvironments = false
	body.CredentialRef = "lowercase-ro"
	if err := body.Validate(); err == nil {
		t.Fatal("invalid credential_ref must fail validation")
	}

	// An invalid environment policy must fail closed.
	body.CredentialRef = "ORDER_MYSQL_RO"
	body.EnvironmentPolicy = QueryEnvironmentPolicy("prod_plus")
	if err := body.Validate(); err == nil {
		t.Fatal("invalid environment policy must fail validation")
	}
}
