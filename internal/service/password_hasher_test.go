// Package service provides tests for password hashing and verification.
// input: internal/service (HashPasswordArgon2id, VerifyPassword, IsLegacyHash)
// output: TestHashPasswordArgon2id*, TestVerifyPassword*, TestIsLegacyHash*, TestDecodeArgon2id*
// pos: Validates Argon2id hashing, legacy SHA-256 verification, format detection, and strict parameter enforcement
// note: if this file changes, update header and README.md
package service

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestHashPasswordArgon2id_ProducesValidFormat verifies the encoded output
// follows the standard Argon2id encoded format.
func TestHashPasswordArgon2id_ProducesValidFormat(t *testing.T) {
	hash := HashPasswordArgon2id("test-password")
	if !strings.HasPrefix(hash, "$argon2id$") {
		t.Fatalf("expected Argon2id prefix, got: %s", hash)
	}
	// Format: $argon2id$v=19$m=65536,t=3,p=1$<salt>$<key>
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		t.Fatalf("expected 6 parts in Argon2id hash, got %d: %s", len(parts), hash)
	}
}

// TestHashPasswordArgon2id_RandomSalt verifies that two calls produce
// different hashes due to random salts.
func TestHashPasswordArgon2id_RandomSalt(t *testing.T) {
	h1 := HashPasswordArgon2id("same-password")
	h2 := HashPasswordArgon2id("same-password")
	if h1 == h2 {
		t.Fatal("two HashPasswordArgon2id calls must produce different salts")
	}
}

// TestHashPasswordArgon2id_EncodesCorrectParameters verifies the hash encodes
// the exact supported parameters so decodeArgon2id round-trips them.
func TestHashPasswordArgon2id_EncodesCorrectParameters(t *testing.T) {
	hash := HashPasswordArgon2id("param-test")
	// Must contain the exact supported parameters string.
	wantParams := "m=65536,t=3,p=1"
	if !strings.Contains(hash, wantParams) {
		t.Fatalf("hash %q does not contain expected params %q", hash, wantParams)
	}
	// Must use Argon2id version 0x13 (19 decimal).
	if !strings.Contains(hash, "v=19") {
		t.Fatalf("hash %q does not contain expected version v=19", hash)
	}
}

// TestVerifyPassword_Argon2id verifies Argon2id hashes.
func TestVerifyPassword_Argon2id(t *testing.T) {
	hash := HashPasswordArgon2id("my-secret")
	if !VerifyPassword("my-secret", hash) {
		t.Fatal("VerifyPassword should accept correct Argon2id password")
	}
	if VerifyPassword("wrong-password", hash) {
		t.Fatal("VerifyPassword should reject incorrect Argon2id password")
	}
}

// TestVerifyPassword_LegacySHA256 verifies legacy SHA-256 hex hashes.
func TestVerifyPassword_LegacySHA256(t *testing.T) {
	// The well-known SHA-256 hex of "secret123".
	legacyHash := "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4"
	if !VerifyPassword("secret123", legacyHash) {
		t.Fatal("VerifyPassword should accept correct legacy SHA-256 password")
	}
	if VerifyPassword("wrong-password", legacyHash) {
		t.Fatal("VerifyPassword should reject incorrect legacy SHA-256 password")
	}
}

// TestIsLegacyHash_DetectsFormat verifies format detection for both hash types.
func TestIsLegacyHash_DetectsFormat(t *testing.T) {
	argonHash := HashPasswordArgon2id("test")
	legacyHash := "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4"

	if IsLegacyHash(argonHash) {
		t.Fatal("Argon2id hash should not be detected as legacy")
	}
	if !IsLegacyHash(legacyHash) {
		t.Fatal("SHA-256 hash should be detected as legacy")
	}
	if !IsLegacyHash("") {
		t.Fatal("empty string should be detected as legacy")
	}
}

// TestIsArgon2idHash_PrefixDetection verifies the prefix-based detection.
func TestIsArgon2idHash_PrefixDetection(t *testing.T) {
	if !IsArgon2idHash("$argon2id$v=19$m=65536,t=3,p=1") {
		t.Fatal("should detect Argon2id prefix")
	}
	if IsArgon2idHash("fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4") {
		t.Fatal("SHA-256 hex should not be detected as Argon2id")
	}
}

// --- Strict parameter validation tests (P1 security) ---

// TestDecodeArgon2id_RejectsLowMemory proves a hash with m=1 (cheap to
// compute) is rejected even if the rest of the format is valid.
func TestDecodeArgon2id_RejectsLowMemory(t *testing.T) {
	// Craft a hash with m=1, t=1, p=1 but valid salt/key encoding.
	salt := make([]byte, argon2SaltLen)
	for i := range salt {
		salt[i] = byte(i)
	}
	key := make([]byte, argon2KeyLen)
	for i := range key {
		key[i] = byte(i)
	}
	saltB64 := encodeB64(salt)
	keyB64 := encodeB64(key)
	fake := "$argon2id$v=19$m=1,t=1,p=1$" + saltB64 + "$" + keyB64

	if VerifyPassword("anything", fake) {
		t.Fatal("VerifyPassword must reject hash with m=1,t=1,p=1 (attacker-cheap parameters)")
	}
}

// TestDecodeArgon2id_RejectsWrongVersion proves a hash with an incorrect
// version is rejected.
func TestDecodeArgon2id_RejectsWrongVersion(t *testing.T) {
	salt := make([]byte, argon2SaltLen)
	key := make([]byte, argon2KeyLen)
	saltB64 := encodeB64(salt)
	keyB64 := encodeB64(key)
	fake := "$argon2id$v=18$m=65536,t=3,p=1$" + saltB64 + "$" + keyB64

	if VerifyPassword("anything", fake) {
		t.Fatal("VerifyPassword must reject hash with wrong version")
	}
}

// TestDecodeArgon2id_RejectsWrongTimeCost proves a hash with t=1 (cheap
// iterations) is rejected even if memory is correct.
func TestDecodeArgon2id_RejectsWrongTimeCost(t *testing.T) {
	salt := make([]byte, argon2SaltLen)
	key := make([]byte, argon2KeyLen)
	saltB64 := encodeB64(salt)
	keyB64 := encodeB64(key)
	fake := "$argon2id$v=19$m=65536,t=1,p=1$" + saltB64 + "$" + keyB64

	if VerifyPassword("anything", fake) {
		t.Fatal("VerifyPassword must reject hash with t=1 (attacker-cheap iterations)")
	}
}

// TestDecodeArgon2id_RejectsWrongParallelism proves a hash with p=2 is
// rejected even if memory and time are correct.
func TestDecodeArgon2id_RejectsWrongParallelism(t *testing.T) {
	salt := make([]byte, argon2SaltLen)
	key := make([]byte, argon2KeyLen)
	saltB64 := encodeB64(salt)
	keyB64 := encodeB64(key)
	fake := "$argon2id$v=19$m=65536,t=3,p=2$" + saltB64 + "$" + keyB64

	if VerifyPassword("anything", fake) {
		t.Fatal("VerifyPassword must reject hash with p=2 (non-standard parallelism)")
	}
}

// TestDecodeArgon2id_RejectsMalformedFormat proves various malformed hash
// strings are safely rejected.
func TestDecodeArgon2id_RejectsMalformedFormat(t *testing.T) {
	cases := []string{
		"",
		"not-an-argon-hash",
		"$argon2id$v=19$m=65536,t=3,p=1$", // missing salt/key
		"$argon2id$v=19$m=65536,t=3,p=1$abc$def$ghi", // too many parts
		"$argon2id$v=abc$m=65536,t=3,p=1$abc$def",    // bad version
		"$argon2id$v=19$m=bad,t=3,p=1$abc$def",       // bad memory
		"$argon2id$v=19$m=65536,t=3,p=1$!!!$def",     // bad salt encoding
	}
	for _, tc := range cases {
		if VerifyPassword("password", tc) {
			t.Fatalf("VerifyPassword must reject malformed hash: %q", tc)
		}
	}
}

// encodeB64 is a test helper for base64.RawStdEncoding.
func encodeB64(data []byte) string {
	return base64.RawStdEncoding.EncodeToString(data)
}
