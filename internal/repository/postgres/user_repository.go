package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/controlhub/internal/model"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) FindByEmail(email string) (*model.UserCredential, error) {
	query := `
select users.id::text, users.email, roles.name, users.password_hash
from users
join roles on roles.id = users.role_id
where lower(users.email) = lower($1)`

	var item model.UserCredential
	err := r.db.QueryRow(context.Background(), query, email).Scan(
		&item.ID,
		&item.Email,
		&item.RoleName,
		&item.PasswordHash,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &item, nil
}
