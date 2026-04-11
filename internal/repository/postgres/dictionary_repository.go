package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/controlhub/internal/model"
)

type DictionaryRepository struct {
	db *pgxpool.Pool
}

func NewDictionaryRepository(db *pgxpool.Pool) *DictionaryRepository {
	return &DictionaryRepository{db: db}
}

func (r *DictionaryRepository) ListEnvironments() ([]model.Environment, error) {
	query := `
	select id::text, name, slug, description, created_at
	from environments
	order by name`

	rows, err := r.db.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Environment, 0)
	for rows.Next() {
		var item model.Environment
		if err := rows.Scan(&item.ID, &item.Name, &item.Slug, &item.Description, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *DictionaryRepository) ListOwners() ([]model.Owner, error) {
	query := `
	select id::text, name, email, created_at
	from owners
	order by name`

	rows, err := r.db.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Owner, 0)
	for rows.Next() {
		var item model.Owner
		if err := rows.Scan(&item.ID, &item.Name, &item.Email, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *DictionaryRepository) ListRoles() ([]model.Role, error) {
	query := `
	select id::text, name, description, created_at
	from roles
	order by name`

	rows, err := r.db.Query(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Role, 0)
	for rows.Next() {
		var item model.Role
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
