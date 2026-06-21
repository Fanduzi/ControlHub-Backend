// Package service provides authentication business logic — credential validation and token generation.
// input: internal/model (UserCredential, LoginRequest, LoginResponse), crypto/hmac, crypto/sha256, encoding/hex, encoding/json, strconv
// output: NewAuthService, AuthService.Login, AuthService.VerifyToken, AuthenticatedUser, ErrInvalidCredentials, ErrInvalidToken, UserRepository interface
// pos: Authentication business logic — credential validation, token generation, and token verification
// note: if this file changes, update header and README.md
package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
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

// AuthenticatedUser is the verified identity extracted from a bearer token.
// IssuedAt is the token's embedded issuance time; callers enforce freshness.
type AuthenticatedUser struct {
	ID       uint64
	Role     string
	IssuedAt time.Time
}

// ErrInvalidToken is returned when a token cannot be verified (malformed,
// bad signature, or non-positive user id). It carries no detail so callers
// never leak signature internals to clients.
var ErrInvalidToken = errors.New("invalid token")

// VerifyToken decodes and verifies a bearer token. It verifies structure,
// signature, and a positive user id, and returns the embedded IssuedAt so
// callers can enforce freshness. It does NOT evaluate token age — TTL is
// enforced solely by the query execution middleware, so existing read/list
// route authentication behavior is unchanged.
func (s *AuthService) VerifyToken(token string) (*AuthenticatedUser, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	decoded := string(raw)

	// The encoded bytes are "<payload>:<signature>" where payload is
	// "<id>:<role>:<issuedAtUnix>". Split the signature off from the right so
	// a role containing a colon could never desynchronize the check.
	lastColon := strings.LastIndex(decoded, ":")
	if lastColon <= 0 {
		return nil, ErrInvalidToken
	}
	payload := decoded[:lastColon]
	signature := decoded[lastColon+1:]

	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return nil, ErrInvalidToken
	}

	parts := strings.Split(payload, ":")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}
	id, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil || id == 0 {
		return nil, ErrInvalidToken
	}
	issuedAtUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, ErrInvalidToken
	}
	return &AuthenticatedUser{
		ID:       id,
		Role:     parts[1],
		IssuedAt: time.Unix(issuedAtUnix, 0),
	}, nil
}
