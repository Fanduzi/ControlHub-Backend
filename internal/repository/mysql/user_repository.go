// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewUserRepository, UserRepository struct
// pos: MySQL data access for users table (credential lookup by email)
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"
	"errors"

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
	select users.id, users.email, roles.name, users.password_hash
	from users
	join roles on roles.id = users.role_id
	where lower(users.email) = lower(?)`

	var item model.UserCredential
	err := r.db.QueryRowContext(context.Background(), query, email).Scan(
		&item.ID,
		&item.Email,
		&item.RoleName,
		&item.PasswordHash,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &item, nil
}
