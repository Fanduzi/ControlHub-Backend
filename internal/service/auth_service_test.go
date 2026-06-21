// Package service provides tests for auth login flow and token verification.
// input: internal/service (AuthService), internal/model, crypto/sha256, crypto/hmac
// output: TestAuthServiceLogin, TestAuthServiceLoginInvalidPassword, TestVerifyToken*
// pos: Validates credential check, token generation, and token verification
// note: if this file changes, update header and README.md
package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/fan/controlhub/internal/model"
)

type fakeUserCredentialRepo struct {
	user *model.UserCredential
}

func (f fakeUserCredentialRepo) FindByEmail(_ string) (*model.UserCredential, error) {
	return f.user, nil
}

func TestAuthServiceLogin(t *testing.T) {
	svc := NewAuthService(fakeUserCredentialRepo{
		user: &model.UserCredential{
			ID:           1,
			Email:        "admin@example.com",
			RoleName:     "admin",
			PasswordHash: "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4",
		},
	}, "test-secret")

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
	svc := NewAuthService(fakeUserCredentialRepo{
		user: &model.UserCredential{
			ID:           1,
			Email:        "admin@example.com",
			RoleName:     "admin",
			PasswordHash: "fcf730b6d95236ecd3c9fc2d92d7b6b2bb061514961aec041d6c7a7192f592e4",
		},
	}, "test-secret")

	_, err := svc.Login("admin@example.com", "wrong-password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

// newAuthServiceAt builds an AuthService with an injected clock so VerifyToken
// tests can assert the exact embedded issuedAt deterministically.
func newAuthServiceAt(secret string, now time.Time) *AuthService {
	return &AuthService{
		signingKey:  []byte(secret),
		nowProvider: func() time.Time { return now },
	}
}

func TestVerifyTokenReturnsUserIDAndRoleForIssuedToken(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	svc := newAuthServiceAt("test-secret", now)
	token := svc.issueToken(&model.UserCredential{ID: 42, RoleName: "admin"})

	user, err := svc.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken error: %v", err)
	}
	if user.ID != 42 {
		t.Fatalf("ID = %d, want 42", user.ID)
	}
	if user.Role != "admin" {
		t.Fatalf("Role = %q, want admin", user.Role)
	}
}

func TestVerifyTokenReturnsIssuedAt(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	svc := newAuthServiceAt("test-secret", now)
	token := svc.issueToken(&model.UserCredential{ID: 1, RoleName: "admin"})

	user, err := svc.VerifyToken(token)
	if err != nil {
		t.Fatalf("VerifyToken error: %v", err)
	}
	// WHY: the query middleware enforces TTL from IssuedAt, so VerifyToken must
	// surface the embedded issuedAt timestamp exactly as issued. It must NOT
	// evaluate age itself — freshness is enforced solely by the middleware.
	if !user.IssuedAt.Equal(now) {
		t.Fatalf("IssuedAt = %v, want %v", user.IssuedAt, now)
	}
}

func TestVerifyTokenRejectsMalformedToken(t *testing.T) {
	svc := newAuthServiceAt("test-secret", time.Now())
	for _, token := range []string{
		"",                 // empty
		"not-a-real-token", // not base64
		"===",              // invalid base64
		"YWJjZA",           // decodes to "abcd" — no colon, no signature structure
	} {
		_, err := svc.VerifyToken(token)
		if !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("VerifyToken(%q) error = %v, want ErrInvalidToken", token, err)
		}
	}
}

func TestVerifyTokenRejectsBadSignature(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	svc := newAuthServiceAt("test-secret", now)
	token := svc.issueToken(&model.UserCredential{ID: 1, RoleName: "admin"})

	// Tamper: rebuild the same payload but sign it with a different secret so the
	// signature never matches what VerifyToken recomputes.
	payload := fmt.Sprintf("%d:%s:%d", uint64(1), "admin", now.Unix())
	mac := hmac.New(sha256.New, []byte("wrong-secret"))
	mac.Write([]byte(payload))
	badSig := hex.EncodeToString(mac.Sum(nil))
	tampered := base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + badSig))

	_, err := svc.VerifyToken(tampered)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyToken(tampered) error = %v, want ErrInvalidToken", err)
	}

	// Sanity: the legitimately issued token must still verify.
	if _, err := svc.VerifyToken(token); err != nil {
		t.Fatalf("legitimate token unexpectedly rejected: %v", err)
	}
}

func TestVerifyTokenRejectsNonPositiveUserID(t *testing.T) {
	now := time.Date(2026, 6, 21, 8, 0, 0, 0, time.UTC)
	svc := newAuthServiceAt("test-secret", now)
	// WHY: a token whose subject resolves to user id 0 must never authenticate —
	// id 0 means "no user" in this codebase, so it must fail closed.
	token := svc.issueToken(&model.UserCredential{ID: 0, RoleName: "admin"})

	if _, err := svc.VerifyToken(token); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("VerifyToken(id=0) error = %v, want ErrInvalidToken", err)
	}
}
