// Package service provides authentication business logic — credential validation and token generation.
// input: internal/model (UserCredential, LoginRequest, LoginResponse), crypto/hmac, crypto/sha256, encoding/hex, encoding/json
// output: NewAuthService, AuthService.Login, ErrInvalidCredentials, UserRepository interface
// pos: Authentication business logic — credential validation and token generation
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
	"time"

	"github.com/fan/controlhub/internal/model"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type UserCredentialRepository interface {
	FindByEmail(email string) (*model.UserCredential, error)
}

type AuthService struct {
	repo        UserCredentialRepository
	signingKey  []byte
	nowProvider func() time.Time
}

func NewAuthService(repo UserCredentialRepository, signingSecret string) *AuthService {
	return &AuthService{
		repo:        repo,
		signingKey:  []byte(signingSecret),
		nowProvider: time.Now,
	}
}

func (s *AuthService) Login(email string, password string) (*model.LoginResponse, error) {
	user, err := s.repo.FindByEmail(strings.TrimSpace(strings.ToLower(email)))
	if err != nil {
		return nil, err
	}
	if user == nil || hashPassword(password) != user.PasswordHash {
		return nil, ErrInvalidCredentials
	}

	return &model.LoginResponse{
		Token: s.issueToken(user),
		Role:  user.RoleName,
	}, nil
}

func (s *AuthService) issueToken(user *model.UserCredential) string {
	payload := fmt.Sprintf("%d:%s:%d", user.ID, user.RoleName, s.nowProvider().Unix())
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + signature))
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}
