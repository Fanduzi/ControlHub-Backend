// Package service provides an in-memory UserCredentialRepository for tests.
// input: sync, strings, internal/model
// output: NewMemoryUserStore, MemoryUserStore
// pos: Test/fake durable authorization state — FindByID/FindByEmail plus role/active/password mutators that bump Authorization Version
// note: if this file changes, update header and README.md
package service

import (
	"fmt"
	"strings"
	"sync"

	"github.com/fan/controlhub/internal/model"
)

// MemoryUserStore is a concurrency-safe in-memory UserCredentialRepository.
// Production uses the MySQL implementation; this store backs unit tests and
// handler fakes so VerifyToken exercises current-state checks without a DB.
type MemoryUserStore struct {
	mu    sync.Mutex
	byID  map[uint64]*model.UserCredential
	roles map[string]struct{} // known role names for ChangeRole validation
}

// NewMemoryUserStore returns a store seeded with copies of the given users.
// Zero AuthorizationVersion becomes 1. Callers must set IsActive explicitly
// (Go zero value is inactive).
func NewMemoryUserStore(users ...model.UserCredential) *MemoryUserStore {
	s := &MemoryUserStore{
		byID:  make(map[uint64]*model.UserCredential, len(users)),
		roles: map[string]struct{}{"admin": {}, "editor": {}, "viewer": {}},
	}
	for _, u := range users {
		s.putLocked(cloneUser(u))
	}
	return s
}

// SeedActor inserts or replaces an active actor at Authorization Version 1.
// Intended for test setup (mintToken helpers), not production invalidation paths.
func (s *MemoryUserStore) SeedActor(id uint64, role string) {
	s.SeedActorVersion(id, role, 1)
}

// SeedActorVersion sets role/active/version for mintToken helpers and returns
// the version embedded in the credential. Existing email and password hash are
// preserved so login tests keep working against the same store.
func (s *MemoryUserStore) SeedActorVersion(id uint64, role string, version uint64) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if version == 0 {
		version = 1
	}
	u := &model.UserCredential{
		ID:                   id,
		RoleName:             role,
		IsActive:             true,
		AuthorizationVersion: version,
	}
	if prev, ok := s.byID[id]; ok {
		u.Email = prev.Email
		u.PasswordHash = prev.PasswordHash
	}
	s.putLocked(u)
	return version
}

// Put replaces the full credential record (test helper).
func (s *MemoryUserStore) Put(u model.UserCredential) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.putLocked(cloneUser(u))
}

func (s *MemoryUserStore) putLocked(u *model.UserCredential) {
	if u.AuthorizationVersion == 0 {
		u.AuthorizationVersion = 1
	}
	s.byID[u.ID] = u
	if u.RoleName != "" {
		s.roles[u.RoleName] = struct{}{}
	}
}

func (s *MemoryUserStore) FindByEmail(email string) (*model.UserCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	want := strings.ToLower(strings.TrimSpace(email))
	for _, u := range s.byID {
		if strings.ToLower(u.Email) == want {
			return cloneUser(*u), nil
		}
	}
	return nil, nil
}

func (s *MemoryUserStore) FindByID(id uint64) (*model.UserCredential, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[id]
	if !ok {
		return nil, nil
	}
	return cloneUser(*u), nil
}

func (s *MemoryUserStore) ChangeRole(userID uint64, roleName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	if _, known := s.roles[roleName]; !known {
		return fmt.Errorf("role not found")
	}
	u.RoleName = roleName
	u.AuthorizationVersion++
	return nil
}

func (s *MemoryUserStore) SetActive(userID uint64, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	u.IsActive = active
	u.AuthorizationVersion++
	return nil
}

func (s *MemoryUserStore) UpdatePasswordHash(userID uint64, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	u.PasswordHash = passwordHash
	u.AuthorizationVersion++
	return nil
}

// UpgradePasswordHash replaces the password hash without bumping
// authorization_version. Used for transparent legacy migration at login time.
func (s *MemoryUserStore) UpgradePasswordHash(userID uint64, passwordHash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[userID]
	if !ok {
		return fmt.Errorf("user not found")
	}
	u.PasswordHash = passwordHash
	return nil
}

// CountLegacyHashUsers returns the number of users whose password hash is
// not Argon2id (i.e. legacy SHA-256).
func (s *MemoryUserStore) CountLegacyHashUsers() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var count int64
	for _, u := range s.byID {
		if IsLegacyHash(u.PasswordHash) {
			count++
		}
	}
	return count, nil
}

func cloneUser(u model.UserCredential) *model.UserCredential {
	cp := u
	return &cp
}
