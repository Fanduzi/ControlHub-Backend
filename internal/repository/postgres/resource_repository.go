package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type ResourceRepository struct {
	db *pgxpool.Pool
}

func NewResourceRepository(db *pgxpool.Pool) *ResourceRepository {
	return &ResourceRepository{db: db}
}

func (r *ResourceRepository) ListResources(resourceType string, environmentID string) ([]model.Resource, error) {
	query := `
select id::text, resource_type, resource_subtype, name, display_name,
       environment_id::text, owner_id::text, lifecycle_status, health_status,
       source, external_id, labels, created_at, updated_at
from resources
where ($1 = '' or resource_type = $1)
  and ($2 = '' or environment_id::text = $2)
order by name`

	rows, err := r.db.Query(context.Background(), query, resourceType, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.Resource, 0)
	for rows.Next() {
		item, err := scanResource(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

func (r *ResourceRepository) GetResource(id string) (*model.Resource, error) {
	query := `
select id::text, resource_type, resource_subtype, name, display_name,
       environment_id::text, owner_id::text, lifecycle_status, health_status,
       source, external_id, labels, created_at, updated_at
from resources
where id::text = $1`

	row := r.db.QueryRow(context.Background(), query, id)

	item, err := scanResource(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, service.ErrResourceNotFound
		}
		return nil, err
	}

	return &item, nil
}

type resourceScanner interface {
	Scan(dest ...any) error
}

func scanResource(scanner resourceScanner) (model.Resource, error) {
	var (
		item      model.Resource
		rawLabels []byte
	)

	err := scanner.Scan(
		&item.ID,
		&item.ResourceType,
		&item.ResourceSubtype,
		&item.Name,
		&item.DisplayName,
		&item.EnvironmentID,
		&item.OwnerID,
		&item.LifecycleStatus,
		&item.HealthStatus,
		&item.Source,
		&item.ExternalID,
		&rawLabels,
		&item.CreatedAt,
		&item.UpdatedAt,
	)
	if err != nil {
		return model.Resource{}, err
	}

	if len(rawLabels) == 0 {
		item.Labels = map[string]string{}
		return item, nil
	}

	if err := json.Unmarshal(rawLabels, &item.Labels); err != nil {
		return model.Resource{}, err
	}

	return item, nil
}
