package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/fan/controlhub/internal/model"
	"github.com/fan/controlhub/internal/service"
)

type ResourceRepository struct {
	db *sql.DB
}

func NewResourceRepository(db *sql.DB) *ResourceRepository {
	return &ResourceRepository{db: db}
}

func (r *ResourceRepository) ListResources(resourceType string, environmentID string) ([]model.Resource, error) {
	query := `
	select id, resource_type, resource_subtype, name, display_name,
	       environment_id, owner_id, lifecycle_status, health_status,
	       source, external_id, labels, created_at, updated_at
	from resources
	where (? = '' or resource_type = ?)
	  and (? = '' or environment_id = ?)
	order by name`

	rows, err := r.db.QueryContext(context.Background(), query,
		resourceType, resourceType,
		environmentID, environmentID,
	)
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
	select id, resource_type, resource_subtype, name, display_name,
	       environment_id, owner_id, lifecycle_status, health_status,
	       source, external_id, labels, created_at, updated_at
	from resources
	where id = ?`

	row := r.db.QueryRowContext(context.Background(), query, id)

	item, err := scanResource(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
		rawLabels string
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

	if rawLabels == "" || rawLabels == "null" {
		item.Labels = map[string]string{}
		return item, nil
	}

	if err := json.Unmarshal([]byte(rawLabels), &item.Labels); err != nil {
		return model.Resource{}, err
	}

	return item, nil
}
