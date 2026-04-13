// Package mysql provides MySQL-backed repository implementations.
// input: database/sql, internal/model
// output: NewDictionaryRepository, DictionaryRepository struct
// pos: Hybrid dictionary data access — DB-backed for reference data, static for taxonomy dictionaries
// note: if this file changes, update header and README.md
package mysql

import (
	"context"
	"database/sql"

	"github.com/fan/controlhub/internal/model"
)

type DictionaryRepository struct {
	db *sql.DB
}

func NewDictionaryRepository(db *sql.DB) *DictionaryRepository {
	return &DictionaryRepository{db: db}
}

func (r *DictionaryRepository) ListEnvironments() ([]model.Environment, error) {
	query := `
	select id, name, slug, description, created_at
	from environments
	order by name`

	rows, err := r.db.QueryContext(context.Background(), query)
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
	select id, name, email, created_at
	from owners
	order by name`

	rows, err := r.db.QueryContext(context.Background(), query)
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
	select id, name, description, created_at
	from roles
	order by name`

	rows, err := r.db.QueryContext(context.Background(), query)
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

func (r *DictionaryRepository) ListLifecycleStatuses() ([]model.DictionaryItem, error) {
	return model.LifecycleStatusDictionary(), nil
}

func (r *DictionaryRepository) ListHealthStatuses() ([]model.DictionaryItem, error) {
	return model.HealthStatusDictionary(), nil
}

func (r *DictionaryRepository) ListResourceTypes() ([]model.DictionaryItem, error) {
	return model.ResourceTypeDictionary(), nil
}

func (r *DictionaryRepository) ListRelationTypes() ([]model.DictionaryItem, error) {
	return model.RelationTypeDictionary(), nil
}
