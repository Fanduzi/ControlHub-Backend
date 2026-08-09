// Package service provides tests for auth login flow and token verification.
// input: internal/service (AuthService), internal/model, crypto/sha256, crypto/hmac
// output: TestAuthServiceLogin*, TestVerifyToken*, TestAuthorizationVersion*
// pos: Validates credential check, versioned token generation, current-state verification, and invalidation causes
// note: if this file changes, update header and README.md
package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

// SHA-256 hex of the well-known local seed password (same value as migration 0002).
const testPasswordHash = "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4"

func activeAdmin(id uint64) model.UserCredential {
	return model.UserCredential{
		ID:                   id,
		Email:                "admin@example.com",
		RoleName:             "admin",
		PasswordHash:         testPasswordHash,
		IsActive:             true,
		AuthorizationVersion: 1,
	}
}

func newAuthServiceAt(secret string, now time.Time, users ...model.UserCredential) *AuthService {
	store := NewMemoryUserStore(users...)
	svc := NewAuthService(store, secret)
	svc.nowProvider = func() time.Time { return now }
	return svc
}

func TestAuthServiceLogin(t *testing.T) {
	svc := NewAuthService(NewMemoryUserStore(activeAdmin(1)), "test-secret")

	resp, err := svc.Login("admin@example.com", "secret123")
	if err != nil {
		t.Fatalf("expected login success, got error: %v", err)
	}

	if resp.Role != "admin" {
		t.Fatalf("expected admin role, got %s", resp.Role)
	}

	if resp.Token == "" {
		t.Fatal("expected token to be generated")
	}
}

func TestAuthServiceLoginInvalidPassword(t *testing.T) {
	svc := NewAuthService(NewMemoryUserStore(activeAdmin(1)), "test-secret")

	_, err := svc.Login("admin@example.com", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

// TestAuthServiceLoginRejectsDisabledUser proves a disabled account cannot obtain
// a Backend Bearer Credential. WHY: disablement must take effect immediately,
// including at login, without disclosing a distinct "disabled" error.
func TestAuthServiceLoginRejectsDisabledUser(t *testing.T) {
	u := activeAdmin(1)
	u.IsActive = false
	svc := NewAuthService(NewMemoryUserStore(u), "test-secret")

	_, err := svc.Login("admin@example.com", "secret123")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials for disabled user, got %v", err)
	}
}

func TestVerifyTokenReturnsUserIDAndCurrentRole(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	svc := newAuthServiceAt("test-secret", now, activeAdmin(42))
	token := svc.issueToken(&model.UserCredential{ID: 42, AuthorizationVersion: 1})

	user, err := svc.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken error: %v", err)
	}
	if user.ID != 42 {
		t.Fatalf("ID = %d, want 42", user.ID)
	}
	// WHY: role must come from current server-owned state, not the token payload.
	if user.Role != "admin" {
		t.Fatalf("Role = %q, want admin", user.Role)
	}
}

func TestVerifyTokenReturnsIssuedAt(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	svc := newAuthServiceAt("test-secret", now, activeAdmin(1))
	token := svc.issueToken(&model.UserCredential{ID: 1, AuthorizationVersion: 1})

	user, err := svc.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken error: %v", err)
	}
	// WHY: the query middleware enforces the fixed eight-hour TTL from IssuedAt,
	// so VerifyToken must surface the embedded issuedAt exactly as issued and
	// must NOT evaluate age itself.
	if !user.IssuedAt.Equal(now) {
		t.Fatalf("IssuedAt = %v, want %v", user.IssuedAt, now)
	}
}

func TestVerifyTokenRejectsMalformedToken(t *testing.T) {
	svc := newAuthServiceAt("test-secret", time.Now(), activeAdmin(1))
	for _, token := range []string{
		"",
		"not-a-real-token",
		"===",
		"YWJjZA",
	} {
		_, err := svc.VerifyToken(token)
		if !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("VerifyToken(%q) error = %v, want ErrInvalidToken", token, err)
		}
	}
}

func TestVerifyTokenRejectsBadSignature(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	svc := newAuthServiceAt("test-secret", now, activeAdmin(1))
	token := svc.issueToken(&model.UserCredential{ID: 1, AuthorizationVersion: 1})

	payload := fmt.Sprintf("%d:%d:%d", uint64(1), uint64(1), now.Unix())
	mac := hmac.New(sha256.New, []byte("wrong-secret"))
	mac.Write([]byte(payload))
	badSig := hex.EncodeToString(mac.Sum(nil))
	tampered := base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + badSig))

	_, err := svc.VerifyToken(tampered)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyToken(tampered) error = %v, want ErrInvalidToken", err)
	}

	if _, err := svc.VerifyToken(token); err != nil {
		t.Fatalf("legitimate token unexpectedly rejected: %v", err)
	}
}

func TestVerifyTokenRejectsNonPositiveUserID(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	svc := newAuthServiceAt("test-secret", now, activeAdmin(1))
	token := svc.issueToken(&model.UserCredential{ID: 0, AuthorizationVersion: 1})

	if _, err := svc.VerifyToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyToken(id=0) error = %v, want ErrInvalidToken", err)
	}
}

// TestVerifyTokenUsesCurrentRoleNotEmbeddedClaim proves role comes only from
// server-owned state. A legacy id:role:issuedAt payload is rejected (fail closed),
// and a valid versioned token still surfaces the DB role even if the operator's
// prior role differed from what a client might hope to assert.
func TestVerifyTokenUsesCurrentRoleNotEmbeddedClaim(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	u := activeAdmin(9)
	u.RoleName = "editor"
	u.Email = "editor@example.com"
	store := NewMemoryUserStore(u)
	svc := NewAuthService(store, "test-secret")
	svc.nowProvider = func() time.Time { return now }

	// Legacy layout with an embedded "admin" role claim must not authenticate.
	legacyPayload := fmt.Sprintf("%d:%s:%d", uint64(9), "admin", now.Unix())
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write([]byte(legacyPayload))
	legacy := base64.RawURLEncoding.EncodeToString([]byte(legacyPayload + ":" + hex.EncodeToString(mac.Sum(nil))))
	if _, err := svc.VerifyToken(legacy); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("legacy role-embedded token error = %v, want ErrInvalidToken", err)
	}

	token := svc.issueToken(&model.UserCredential{ID: 9, AuthorizationVersion: 1})
	user, err := svc.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken error: %v", err)
	}
	if user.Role != "editor" {
		t.Fatalf("Role = %q, want editor from server state", user.Role)
	}
}

// TestVerifyTokenRejectsAuthorizationVersionMismatch proves a previously issued
// credential is rejected after Authorization Version advances.
func TestVerifyTokenRejectsAuthorizationVersionMismatch(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	store := NewMemoryUserStore(activeAdmin(5))
	svc := NewAuthService(store, "test-secret")
	svc.nowProvider = func() time.Time { return now }

	token := svc.issueToken(&model.UserCredential{ID: 5, AuthorizationVersion: 1})
	if _, err := svc.VerifyToken(token); err != nil {
		t.Fatalf("precondition: token must verify before invalidation: %v", err)
	}

	if err := svc.ChangeUserRole(5, "editor"); err != nil {
		t.Fatalf("ChangeUserRole: %v", err)
	}

	if _, err := svc.VerifyToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("after role change VerifyToken error = %v, want ErrInvalidToken", err)
	}
}

// TestRoleChangeInvalidatesPriorCredential is the explicit role-demotion cause.
func TestRoleChangeInvalidatesPriorCredential(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	store := NewMemoryUserStore(activeAdmin(11))
	svc := NewAuthService(store, "test-secret")
	svc.nowProvider = func() time.Time { return now }

	prior := svc.issueToken(&model.UserCredential{ID: 11, AuthorizationVersion: 1})
	if err := svc.ChangeUserRole(11, "editor"); err != nil {
		t.Fatalf("ChangeUserRole: %v", err)
	}
	if _, err := svc.VerifyToken(prior); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("prior credential after demotion: %v, want ErrInvalidToken", err)
	}

	// Fresh credential at the new version must work and carry the new role.
	freshUser, err := store.FindByID(11)
	if err != nil || freshUser == nil {
		t.Fatalf("FindByID after change: user=%v err=%v", freshUser, err)
	}
	fresh := svc.issueToken(freshUser)
	got, err := svc.VerifyToken(fresh)
	if err != nil {
		t.Fatalf("fresh credential: %v", err)
	}
	if got.Role != "editor" {
		t.Fatalf("fresh role = %q, want editor", got.Role)
	}
}

// TestDisablementInvalidatesPriorCredential covers user disablement.
func TestDisablementInvalidatesPriorCredential(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	store := NewMemoryUserStore(activeAdmin(12))
	svc := NewAuthService(store, "test-secret")
	svc.nowProvider = func() time.Time { return now }

	prior := svc.issueToken(&model.UserCredential{ID: 12, AuthorizationVersion: 1})
	if err := svc.SetUserActive(12, false); err != nil {
		t.Fatalf("SetUserActive: %v", err)
	}
	if _, err := svc.VerifyToken(prior); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("prior credential after disablement: %v, want ErrInvalidToken", err)
	}
}

// TestPasswordResetInvalidatesPriorCredential covers password reset.
func TestPasswordResetInvalidatesPriorCredential(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	store := NewMemoryUserStore(activeAdmin(13))
	svc := NewAuthService(store, "test-secret")
	svc.nowProvider = func() time.Time { return now }

	prior := svc.issueToken(&model.UserCredential{ID: 13, AuthorizationVersion: 1})
	if err := svc.ResetUserPassword(13, "new-secret-password"); err != nil {
		t.Fatalf("ResetUserPassword: %v", err)
	}
	if _, err := svc.VerifyToken(prior); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("prior credential after password reset: %v, want ErrInvalidToken", err)
	}

	// New password can log in; old password cannot.
	if _, err := svc.Login("admin@example.com", "secret123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password after reset: %v, want ErrInvalidCredentials", err)
	}
	// Re-enable email on store for login — ResetUserPassword keeps email.
	resp, err := svc.Login("admin@example.com", "new-secret-password")
	if err != nil {
		t.Fatalf("login with new password: %v", err)
	}
	if resp.Token == "" {
		t.Fatal("expected token after password reset login")
	}
}

// TestInvalidTokenErrorsAreGeneric proves failure paths share ErrInvalidToken
// and never embed Authorization Version, passwords, or raw causes in the error text.
func TestInvalidTokenErrorsAreGeneric(t *testing.T) {
	now := time.Date(2026, 8, 9, 8, 0, 0, 0, time.UTC)
	store := NewMemoryUserStore(activeAdmin(14))
	svc := NewAuthService(store, "test-secret")
	svc.nowProvider = func() time.Time { return now }

	good := svc.issueToken(&model.UserCredential{ID: 14, AuthorizationVersion: 1})
	_ = svc.ChangeUserRole(14, "editor") // version mismatch path

	cases := []struct {
		name  string
		token string
	}{
		{"malformed", "not-a-token"},
		{"version_mismatch", good},
		{"empty", ""},
	}
	// disabled path
	u := activeAdmin(15)
	store.Put(u)
	disabledTok := svc.issueToken(&model.UserCredential{ID: 15, AuthorizationVersion: 1})
	_ = svc.SetUserActive(15, false)
	cases = append(cases, struct {
		name  string
		token string
	}{"disabled", disabledTok})

	sensitive := []string{"authorization", "version", "password", "disabled", "role", "secret123", testPasswordHash}
	for _, tc := range cases {
		_, err := svc.VerifyToken(tc.token)
		if !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("%s: error = %v, want ErrInvalidToken", tc.name, err)
		}
		msg := err.Error()
		for _, leak := range sensitive {
			if strings.Contains(strings.ToLower(msg), leak) {
				t.Fatalf("%s: error %q leaks %q", tc.name, msg, leak)
			}
		}
	}
}
