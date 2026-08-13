// Package service provides password hashing and verification for the
// authentication boundary.
// input: golang.org/x/crypto/argon2, crypto/rand, crypto/sha256, crypto/subtle, encoding/hex, encoding/base64, fmt, strings
// output: HashPasswordArgon2id, VerifyPassword, IsLegacyHash, IsArgon2idHash, decodeArgon2id
// pos: Argon2id password hashing with strict parameter validation and transparent legacy SHA-256 verification for gradual migration
// note: if this file changes, update header and README.md
package service

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters: 64 MiB memory, 3 iterations, 1 parallelism thread.
// The deployment budget is ~250 ms per verification; these parameters target
// that budget on current-generation server hardware. The verifier rejects any
// hash that does not encode these exact parameters, preventing an attacker
// from storing a cheap-to-verify hash (e.g. m=1,t=1,p=1) and having it
// accepted.
const (
	argon2Memory  = 64 * 1024 // KiB (64 MiB)
	argon2Time    = 3
	argon2Threads = 1
	argon2SaltLen = 16
	argon2KeyLen  = 32
)

// HashPasswordArgon2id hashes a plaintext password with Argon2id and returns
// a self-contained encoded string that includes algorithm parameters and salt.
// The encoded format is: $argon2id$v=19$m=65536,t=3,p=1$<base64-salt>$<base64-key>
func HashPasswordArgon2id(password string) string {
	salt := make([]byte, argon2SaltLen)
	if _, err := rand.Read(salt); err != nil {
		// crypto/rand.Read panics on OS entropy exhaustion; this guard is
		// defensive only. A deterministic fallback would produce unsalted
		// hashes for identical passwords, so we propagate the panic.
		panic(fmt.Sprintf("argon2id: salt generation failed: %v", err))
	}

	key := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, argon2KeyLen)

	saltB64 := base64.RawStdEncoding.EncodeToString(salt)
	keyB64 := base64.RawStdEncoding.EncodeToString(key)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argon2Memory, argon2Time, argon2Threads, saltB64, keyB64)
}

// VerifyPassword checks a plaintext password against a stored hash. It
// detects Argon2id vs legacy SHA-256 format and verifies accordingly.
// Returns true only if the password matches.
func VerifyPassword(password, storedHash string) bool {
	if IsArgon2idHash(storedHash) {
		return verifyArgon2id(password, storedHash)
	}
	return verifyLegacySHA256(password, storedHash)
}

// IsLegacyHash reports whether the stored hash is a legacy SHA-256
// representation that should be upgraded on next successful login.
func IsLegacyHash(storedHash string) bool {
	return !IsArgon2idHash(storedHash)
}

// IsArgon2idHash reports whether the stored hash is an Argon2id
// representation (starts with the standard Argon2id encoded prefix).
func IsArgon2idHash(storedHash string) bool {
	return strings.HasPrefix(storedHash, "$argon2id$")
}

// verifyArgon2id verifies a password against an Argon2id encoded hash.
// The parser rejects any hash whose encoded parameters do not match the
// exact supported configuration (version 0x13, m=65536, t=3, p=1). This
// prevents an attacker from storing a cheap hash that would pass verification.
func verifyArgon2id(password, encodedHash string) bool {
	salt, key, err := decodeArgon2id(encodedHash)
	if err != nil {
		return false
	}

	otherKey := argon2.IDKey([]byte(password), salt, argon2Time, argon2Memory, argon2Threads, uint32(len(key)))
	// crypto/subtle.ConstantTimeCompare prevents timing side-channels on the
	// final comparison. Both slices are the same length (argon2KeyLen) when
	// the hash was produced by HashPasswordArgon2id; the subtle package
	// handles mismatched lengths safely.
	return subtle.ConstantTimeCompare(key, otherKey) == 1
}

// canonicalVersion is the exact version string we accept ("v=19").
const canonicalVersion = "v=19"

// canonicalParams is the exact parameters string we accept
// ("m=65536,t=3,p=1"). Exact string matching prevents fmt.Sscanf from
// silently accepting trailing garbage (e.g. "m=65536,t=3,p=1X").
const canonicalParams = "m=65536,t=3,p=1"

// decodeArgon2id parses an Argon2id encoded hash and enforces that the
// encoded format is exactly canonical: version v=19, parameters m=65536,t=3,p=1,
// salt length 16, key length 32. Any deviation — trailing bytes, wrong lengths,
// non-canonical encoding — is rejected. Returns salt and key on success.
func decodeArgon2id(encodedHash string) (salt, key []byte, err error) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 {
		return nil, nil, fmt.Errorf("argon2id: invalid encoded hash format")
	}

	// Exact string match for version — prevents "v=19garbage".
	if parts[2] != canonicalVersion {
		return nil, nil, fmt.Errorf("argon2id: unsupported version %q", parts[2])
	}

	// Exact string match for parameters — prevents "m=65536,t=3,p=1X".
	if parts[3] != canonicalParams {
		return nil, nil, fmt.Errorf("argon2id: unsupported parameters %q", parts[3])
	}

	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return nil, nil, fmt.Errorf("argon2id: invalid salt encoding: %w", err)
	}
	if len(salt) != argon2SaltLen {
		return nil, nil, fmt.Errorf("argon2id: salt length %d, want %d", len(salt), argon2SaltLen)
	}

	key, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return nil, nil, fmt.Errorf("argon2id: invalid key encoding: %w", err)
	}
	if len(key) != argon2KeyLen {
		return nil, nil, fmt.Errorf("argon2id: key length %d, want %d", len(key), argon2KeyLen)
	}

	return salt, key, nil
}

// verifyLegacySHA256 verifies a password against a legacy SHA-256 hex hash.
func verifyLegacySHA256(password, hexHash string) bool {
	sum := sha256.Sum256([]byte(password))
	computed := hex.EncodeToString(sum[:])
	// subtle.ConstantTimeCompare prevents timing side-channels. The hex
	// strings are fixed-length (64 bytes for SHA-256), so the constant-time
	// comparison is the correct approach here.
	return subtle.ConstantTimeCompare([]byte(hexHash), []byte(computed)) == 1
}
