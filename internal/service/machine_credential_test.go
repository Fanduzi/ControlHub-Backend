// Package service implements business logic for resource management.
// input: crypto/sha256, strings, and testing
// output: opaque machine-credential generation and parsing contract tests
// pos: Pure credential-material security regression coverage
// note: if this file changes, update this header and module README.md.
package service

import (
	"crypto/sha256"
	"strings"
	"testing"
)

func TestGenerateMachineCredentialUsesOpaqueSecretAndHashOnlyMaterial(t *testing.T) {
	first, err := generateMachineCredential()
	if err != nil {
		t.Fatalf("generateMachineCredential: %v", err)
	}
	second, err := generateMachineCredential()
	if err != nil {
		t.Fatalf("generate second credential: %v", err)
	}
	if first.Token == second.Token {
		t.Fatal("independent credentials must differ")
	}
	if !strings.HasPrefix(first.Token, machineCredentialPrefix) || first.LookupID == "" {
		t.Fatalf("credential %q does not carry its stable lookup id", first.Token)
	}
	if len(first.Token) < 64 {
		t.Fatalf("credential length = %d, want at least 64 characters", len(first.Token))
	}
	wantHash := sha256.Sum256([]byte(first.Token))
	if first.Hash != wantHash {
		t.Fatal("stored material must be the SHA-256 digest of the random credential")
	}

	lookupID, hash, err := parseMachineCredential(first.Token)
	if err != nil {
		t.Fatalf("parseMachineCredential: %v", err)
	}
	if lookupID != first.LookupID || hash != first.Hash {
		t.Fatalf("parsed lookup/hash do not match generated material")
	}
}

func TestParseMachineCredentialRejectsMalformedValues(t *testing.T) {
	for _, token := range []string{
		"",
		"chmp_missing-separator",
		"wrong_lookup.secret",
		"chmp_short.c2hvcnQ",
		" chmp_abcdefghijklmnop.AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
	} {
		t.Run(token, func(t *testing.T) {
			if _, _, err := parseMachineCredential(token); err == nil {
				t.Fatal("expected malformed credential rejection")
			}
		})
	}
}
