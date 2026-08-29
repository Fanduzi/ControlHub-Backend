// Package service implements business logic for resource management.
// input: context, crypto/sha256, crypto/subtle, database/sql, errors, fmt, strings, time, unicode/utf8, internal/model
// output: MachinePrincipalService lifecycle/authentication boundary and repository contract
// pos: Admin-governed machine-principal application service
// note: if this file changes, update this header and module README.md.
package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fan/controlhub/internal/model"
)

var (
	ErrMachinePrincipalForbidden  = errors.New("machine principal administration forbidden")
	ErrMachinePrincipalValidation = errors.New("machine principal validation failed")
	ErrMachineCredentialNotFound  = errors.New("machine credential not found")
	ErrMachineCredentialInvalid   = errors.New("invalid machine credential")
	ErrMachineCredentialExpired   = errors.New("machine credential expired")
	ErrMachineCredentialRevoked   = errors.New("machine credential revoked")
	ErrMachineScopeDenied         = errors.New("machine credential scope denied")
)

type MachineCredentialInsert struct {
	LookupID                string
	SecretHash              [sha256.Size]byte
	Scopes                  []model.MachineScope
	ExpiresAt               time.Time
	CreatedAt               time.Time
	RotatedFromCredentialID *uint64
}

type MachineCredentialAuthentication struct {
	Principal  model.MachinePrincipal
	Credential model.MachineCredential
	SecretHash [sha256.Size]byte
}

type MachinePrincipalRepository interface {
	Create(context.Context, uint64, string, MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error)
	Rotate(context.Context, uint64, uint64, MachineCredentialInsert) (model.MachinePrincipal, model.MachineCredential, error)
	Revoke(context.Context, uint64, uint64, time.Time) error
	FindCredential(context.Context, string) (MachineCredentialAuthentication, error)
	MarkUsed(context.Context, uint64, time.Time) error
}

func (s *MachinePrincipalService) Rotate(ctx context.Context, actor AuthenticatedUser, oldCredentialID uint64, req model.MachineCredentialRotateRequest) (model.MachineCredentialIssue, error) {
	if err := requireMachinePrincipalAdmin(actor); err != nil {
		return model.MachineCredentialIssue{}, err
	}
	if oldCredentialID == 0 {
		return model.MachineCredentialIssue{}, fmt.Errorf("%w: credential id must be positive", ErrMachinePrincipalValidation)
	}
	now := s.now().UTC()
	scopes, err := model.NormalizeMachineScopes(req.Scopes)
	if err != nil {
		return model.MachineCredentialIssue{}, fmt.Errorf("%w: %v", ErrMachinePrincipalValidation, err)
	}
	expiresAt, err := model.ResolveMachineCredentialExpiry(now, req.ExpiresAt)
	if err != nil {
		return model.MachineCredentialIssue{}, fmt.Errorf("%w: %v", ErrMachinePrincipalValidation, err)
	}
	material, err := generateMachineCredential()
	if err != nil {
		return model.MachineCredentialIssue{}, err
	}
	principal, credential, err := s.repo.Rotate(ctx, actor.ID, oldCredentialID, MachineCredentialInsert{
		LookupID: material.LookupID, SecretHash: material.Hash, Scopes: scopes,
		ExpiresAt: expiresAt, CreatedAt: now, RotatedFromCredentialID: &oldCredentialID,
	})
	if err != nil {
		return model.MachineCredentialIssue{}, err
	}
	return model.MachineCredentialIssue{Principal: principal, Credential: credential, Secret: material.Token}, nil
}

func (s *MachinePrincipalService) Revoke(ctx context.Context, actor AuthenticatedUser, credentialID uint64) error {
	if err := requireMachinePrincipalAdmin(actor); err != nil {
		return err
	}
	if credentialID == 0 {
		return fmt.Errorf("%w: credential id must be positive", ErrMachinePrincipalValidation)
	}
	if err := s.repo.Revoke(ctx, actor.ID, credentialID, s.now().UTC()); errors.Is(err, sql.ErrNoRows) {
		return ErrMachineCredentialNotFound
	} else {
		return err
	}
}

func (s *MachinePrincipalService) Authenticate(ctx context.Context, token string, requiredScope model.MachineScope) (model.MachinePrincipalIdentity, error) {
	if _, err := model.NormalizeMachineScopes([]model.MachineScope{requiredScope}); err != nil {
		return model.MachinePrincipalIdentity{}, ErrMachineScopeDenied
	}
	lookupID, hash, err := parseMachineCredential(token)
	if err != nil {
		return model.MachinePrincipalIdentity{}, ErrMachineCredentialInvalid
	}
	auth, err := s.repo.FindCredential(ctx, lookupID)
	if errors.Is(err, sql.ErrNoRows) {
		return model.MachinePrincipalIdentity{}, ErrMachineCredentialInvalid
	}
	if err != nil {
		return model.MachinePrincipalIdentity{}, err
	}
	if subtle.ConstantTimeCompare(auth.SecretHash[:], hash[:]) != 1 {
		return model.MachinePrincipalIdentity{}, ErrMachineCredentialInvalid
	}
	now := s.now().UTC()
	if auth.Credential.RevokedAt != nil {
		return model.MachinePrincipalIdentity{}, ErrMachineCredentialRevoked
	}
	if !now.Before(auth.Credential.ExpiresAt) {
		return model.MachinePrincipalIdentity{}, ErrMachineCredentialExpired
	}
	if !machineCredentialHasScope(auth.Credential.Scopes, requiredScope) {
		return model.MachinePrincipalIdentity{}, ErrMachineScopeDenied
	}
	if err := s.repo.MarkUsed(ctx, auth.Credential.ID, now); errors.Is(err, sql.ErrNoRows) {
		return model.MachinePrincipalIdentity{}, ErrMachineCredentialInvalid
	} else if err != nil {
		return model.MachinePrincipalIdentity{}, err
	}
	return model.MachinePrincipalIdentity{
		ID: auth.Principal.ID, Name: auth.Principal.Name,
		CredentialID: auth.Credential.ID, Scopes: append([]model.MachineScope(nil), auth.Credential.Scopes...),
	}, nil
}

func machineCredentialHasScope(scopes []model.MachineScope, required model.MachineScope) bool {
	for _, scope := range scopes {
		if scope == required {
			return true
		}
	}
	return false
}

type MachinePrincipalService struct {
	repo MachinePrincipalRepository
	now  func() time.Time
}

func NewMachinePrincipalService(repo MachinePrincipalRepository) *MachinePrincipalService {
	return &MachinePrincipalService{repo: repo, now: time.Now}
}

func (s *MachinePrincipalService) WithClock(now func() time.Time) *MachinePrincipalService {
	s.now = now
	return s
}

func (s *MachinePrincipalService) Create(ctx context.Context, actor AuthenticatedUser, req model.MachinePrincipalCreateRequest) (model.MachineCredentialIssue, error) {
	if err := requireMachinePrincipalAdmin(actor); err != nil {
		return model.MachineCredentialIssue{}, err
	}
	name, err := normalizeMachinePrincipalName(req.Name)
	if err != nil {
		return model.MachineCredentialIssue{}, err
	}
	now := s.now().UTC()
	scopes, err := model.NormalizeMachineScopes(req.Scopes)
	if err != nil {
		return model.MachineCredentialIssue{}, fmt.Errorf("%w: %v", ErrMachinePrincipalValidation, err)
	}
	expiresAt, err := model.ResolveMachineCredentialExpiry(now, req.ExpiresAt)
	if err != nil {
		return model.MachineCredentialIssue{}, fmt.Errorf("%w: %v", ErrMachinePrincipalValidation, err)
	}
	material, err := generateMachineCredential()
	if err != nil {
		return model.MachineCredentialIssue{}, err
	}
	principal, credential, err := s.repo.Create(ctx, actor.ID, name, MachineCredentialInsert{
		LookupID: material.LookupID, SecretHash: material.Hash, Scopes: scopes,
		ExpiresAt: expiresAt, CreatedAt: now,
	})
	if err != nil {
		return model.MachineCredentialIssue{}, err
	}
	return model.MachineCredentialIssue{Principal: principal, Credential: credential, Secret: material.Token}, nil
}

func requireMachinePrincipalAdmin(actor AuthenticatedUser) error {
	if actor.ID == 0 || actor.Role != "admin" {
		return ErrMachinePrincipalForbidden
	}
	return nil
}

func normalizeMachinePrincipalName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 {
		return "", fmt.Errorf("%w: name must contain 1 to 120 characters", ErrMachinePrincipalValidation)
	}
	for _, r := range name {
		if r < 32 {
			return "", fmt.Errorf("%w: name contains control characters", ErrMachinePrincipalValidation)
		}
	}
	return name, nil
}
