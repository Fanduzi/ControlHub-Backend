// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, context, errors, internal/model
// output: NewUserRepository, UserRepository struct
// pos: MySQL data access for users table — credential lookup, Authorization Version mutators, UpgradePasswordHash for legacy migration, CountLegacyHashUsers
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/fan/controlhub/internal/model"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) FindByEmail(email string) (*model.UserCredential, error) {
	query := `
	select users.id, users.email, roles.name, users.password_hash,
	       users.is_active, users.authorization_version
	from users
	join roles on roles.id = users.role_id
	where lower(users.email) = lower(?)`

	var item model.UserCredential
	var active int
	err := r.db.QueryRowContext(context.Background(), query, email).Scan(
		&item.ID,
		&item.Email,
		&item.RoleName,
		&item.PasswordHash,
		&active,
		&item.AuthorizationVersion,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.IsActive = active != 0
	return &item, nil
}

func (r *UserRepository) FindByID(id uint64) (*model.UserCredential, error) {
	query := `
	select users.id, users.email, roles.name, users.password_hash,
	       users.is_active, users.authorization_version
	from users
	join roles on roles.id = users.role_id
	where users.id = ?`

	var item model.UserCredential
	var active int
	err := r.db.QueryRowContext(context.Background(), query, id).Scan(
		&item.ID,
		&item.Email,
		&item.RoleName,
		&item.PasswordHash,
		&active,
		&item.AuthorizationVersion,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	item.IsActive = active != 0
	return &item, nil
}

// ChangeRole sets role_id from roles.name and bumps authorization_version in one
// update so prior Backend Bearer Credentials fail on the next protected request.
func (r *UserRepository) ChangeRole(userID uint64, roleName string) error {
	roleName = strings.TrimSpace(roleName)
	res, err := r.db.ExecContext(context.Background(), `
		update users
		set role_id = (select id from roles where name = ? limit 1),
		    authorization_version = authorization_version + 1
		where id = ?
		  and exists (select 1 from roles where name = ?)`,
		roleName, userID, roleName,
	)
	if err != nil {
		return fmt.Errorf("change role: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("change role: user or role not found")
	}
	return nil
}

// SetActive updates is_active and bumps authorization_version.
func (r *UserRepository) SetActive(userID uint64, active bool) error {
	flag := 0
	if active {
		flag = 1
	}
	res, err := r.db.ExecContext(context.Background(), `
		update users
		set is_active = ?,
		    authorization_version = authorization_version + 1
		where id = ?`,
		flag, userID,
	)
	if err != nil {
		return fmt.Errorf("set active: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("set active: user not found")
	}
	return nil
}

// UpdatePasswordHash replaces password_hash and bumps authorization_version.
// Used for password resets where prior tokens must be invalidated.
func (r *UserRepository) UpdatePasswordHash(userID uint64, passwordHash string) error {
	res, err := r.db.ExecContext(context.Background(), `
		update users
		set password_hash = ?,
		    authorization_version = authorization_version + 1
		where id = ?`,
		passwordHash, userID,
	)
	if err != nil {
		return fmt.Errorf("update password hash: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("update password hash: user not found")
	}
	return nil
}

// UpgradePasswordHash atomically replaces password_hash without bumping
// authorization_version, but only if the current hash matches
// expectedOldHash (compare-and-swap). This prevents a stale login from
// overwriting a password reset or concurrent change. Zero rows affected
// returns an error; callers treat this as a failed upgrade.
func (r *UserRepository) UpgradePasswordHash(userID uint64, expectedOldHash, newPasswordHash string) error {
	res, err := r.db.ExecContext(context.Background(), `
		update users
		set password_hash = ?
		where id = ?
		  and password_hash = ?`,
		newPasswordHash, userID, expectedOldHash,
	)
	if err != nil {
		return fmt.Errorf("upgrade password hash: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("upgrade password hash: CAS failed — hash changed since read")
	}
	return nil
}

// CountLegacyHashUsers returns the count of users whose password_hash is
// exactly a 64-character lowercase hexadecimal string — the canonical
// SHA-256 representation from the pre-Argon2id code path. Malformed,
// unknown, or Argon2id hashes are excluded. The query carries no
// identity-bearing information beyond the integer count.
func (r *UserRepository) CountLegacyHashUsers() (int64, error) {
	var count int64
	err := r.db.QueryRowContext(context.Background(),
		`SELECT COUNT(*) FROM users WHERE password_hash COLLATE utf8mb4_bin REGEXP '^[0-9a-f]{64}$'`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count legacy hash users: %w", err)
	}
	return count, nil
}
