// Package service provides authentication business logic — credential validation and token generation.
// input: internal/model (UserCredential, LoginRequest, LoginResponse), crypto/hmac, crypto/sha256, encoding/hex, encoding/base64, strconv
// output: NewAuthService, AuthService.Login, AuthService.VerifyToken, AuthService.ChangeUserRole, AuthService.SetUserActive, AuthService.ResetUserPassword, AuthenticatedUser, ErrInvalidCredentials, ErrInvalidToken, UserCredentialRepository
// pos: Authentication business logic — credential validation, versioned token generation, and current-state token verification (Authorization Version)
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

// ErrInvalidAuthorizationMutation is returned when an internal authorization-state
// mutator receives unusable arguments (zero user id, empty role/password). It is
// distinct from ErrInvalidToken so callers never confuse argument checks with
// Backend Bearer Credential verification failures.
var ErrInvalidAuthorizationMutation = errors.New("invalid authorization mutation")

// UserCredentialRepository loads and mutates durable user authorization state.
// FindByID is required on every protected request so current active role and
// Authorization Version are authoritative. Mutators bump Authorization Version
// before returning success so prior Backend Bearer Credentials fail immediately.
type UserCredentialRepository interface {
	FindByEmail(email string) (*model.UserCredential, error)
	FindByID(id uint64) (*model.UserCredential, error)
	// ChangeRole sets the user's role by name and bumps Authorization Version.
	ChangeRole(userID uint64, roleName string) error
	// SetActive sets whether the user may authenticate and bumps Authorization Version.
	SetActive(userID uint64, active bool) error
	// UpdatePasswordHash replaces the password hash and bumps Authorization Version.
	UpdatePasswordHash(userID uint64, passwordHash string) error
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

// WithClock replaces the issuance clock. Used by tests to pin IssuedAt; production
// keeps time.Now. Returns the receiver for fluent wiring.
func (s *AuthService) WithClock(clock func() time.Time) *AuthService {
	if clock != nil {
		s.nowProvider = clock
	}
	return s
}

func (s *AuthService) Login(email string, password string) (*model.LoginResponse, error) {
	user, err := s.repo.FindByEmail(strings.TrimSpace(strings.ToLower(email)))
	if err != nil {
		return nil, err
	}
	// Disabled and missing users share the same generic failure as a bad password
	// so login never discloses account state.
	if user == nil || !user.IsActive || hashPassword(password) != user.PasswordHash {
		return nil, ErrInvalidCredentials
	}

	return &model.LoginResponse{
		Token: s.issueToken(user),
		Role:  user.RoleName,
	}, nil
}

// issueToken builds a Backend Bearer Credential claim:
// user id, Authorization Version, and issuance time. Role is intentionally
// omitted — current role is loaded from server-owned state on every verify.
func (s *AuthService) issueToken(user *model.UserCredential) string {
	payload := fmt.Sprintf("%d:%d:%d", user.ID, user.AuthorizationVersion, s.nowProvider().Unix())
	mac := hmac.New(sha256.New, s.signingKey)
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload + ":" + signature))
}

func hashPassword(password string) string {
	sum := sha256.Sum256([]byte(password))
	return hex.EncodeToString(sum[:])
}

// AuthenticatedUser is the verified identity for a protected request.
// Role is the current server-owned role at verification time, never a
// token-embedded role. IssuedAt is the credential's embedded issuance time;
// callers (query freshness middleware) enforce the fixed eight-hour max age.
type AuthenticatedUser struct {
	ID       uint64
	Role     string
	IssuedAt time.Time
}

// ErrInvalidToken is returned when a token cannot be accepted (malformed,
// bad signature, inactive user, Authorization Version mismatch, or missing
// user). It carries no detail so callers never leak verification internals.
var ErrInvalidToken = errors.New("invalid token")

// VerifyToken decodes and verifies a Backend Bearer Credential. Acceptance
// requires a valid signature, positive user id, a matching current
// Authorization Version, and an active user. The returned Role is loaded from
// current server-owned state — a role that may once have been embedded in a
// signed token is never authoritative. Token age is NOT evaluated here; the
// fixed eight-hour query freshness bound remains enforced solely by the query
// execution middleware.
func (s *AuthService) VerifyToken(token string) (*AuthenticatedUser, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidToken
	}
	decoded := string(raw)

	// Encoded form is "<payload>:<signature>" where payload is
	// "<id>:<authorizationVersion>:<issuedAtUnix>".
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
	tokenVersion, err := strconv.ParseUint(parts[1], 10, 64)
	if err != nil {
		return nil, ErrInvalidToken
	}
	issuedAtUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return nil, ErrInvalidToken
	}

	if s.repo == nil {
		return nil, ErrInvalidToken
	}
	current, err := s.repo.FindByID(id)
	if err != nil {
		return nil, err
	}
	// Missing, disabled, or Authorization Version mismatch all collapse to the
	// same invalid-token outcome — never disclose which check failed.
	if current == nil || !current.IsActive || current.AuthorizationVersion != tokenVersion {
		return nil, ErrInvalidToken
	}

	return &AuthenticatedUser{
		ID:       id,
		Role:     current.RoleName,
		IssuedAt: time.Unix(issuedAtUnix, 0),
	}, nil
}

// ChangeUserRole updates the user's role and invalidates every previously
// issued Backend Bearer Credential via Authorization Version bump. No HTTP
// surface is exposed here; this is the internal seam role-admin paths call.
func (s *AuthService) ChangeUserRole(userID uint64, roleName string) error {
	roleName = strings.TrimSpace(roleName)
	if userID == 0 || roleName == "" {
		return ErrInvalidAuthorizationMutation
	}
	return s.repo.ChangeRole(userID, roleName)
}

// SetUserActive enables or disables the user and invalidates prior credentials.
// Disablement must take effect on the next protected request.
func (s *AuthService) SetUserActive(userID uint64, active bool) error {
	if userID == 0 {
		return ErrInvalidAuthorizationMutation
	}
	return s.repo.SetActive(userID, active)
}

// ResetUserPassword replaces the password hash and invalidates prior credentials
// so a reset cannot leave outstanding Backend Bearer Credentials usable.
func (s *AuthService) ResetUserPassword(userID uint64, newPassword string) error {
	if userID == 0 || newPassword == "" {
		return ErrInvalidAuthorizationMutation
	}
	return s.repo.UpdatePasswordHash(userID, hashPassword(newPassword))
}
