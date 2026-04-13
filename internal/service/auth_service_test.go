// Package service provides tests for auth login flow.
// input: internal/service (AuthService), internal/model, crypto/sha256
// output: TestAuthServiceLogin, TestAuthServiceLoginInvalidPassword
// pos: Validates credential check and token generation
// note: if this file changes, update header and README.md
package service

import (
	"errors"
	"testing"

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
			ID:           "user-1",
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
			ID:           "user-1",
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
